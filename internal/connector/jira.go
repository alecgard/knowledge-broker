package connector

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/knowledge-broker/knowledge-broker/pkg/model"
)

// SourceTypeJira is the source type identifier for Jira sources.
const SourceTypeJira = "jira"

// Default configuration values for the Jira connector.
const (
	jiraDefaultConcurrency = 2
	jiraDefaultPageSize    = 50
	jiraMaxPageSize        = 100
	jiraMaxRetries         = 5
)

// jiraDefaultFields are the metadata fields included by default.
var jiraDefaultFields = []string{"assignee", "status", "labels", "created", "updated"}

func init() {
	Register(SourceTypeJira, func(config map[string]string) (Connector, error) {
		if config["base_url"] == "" {
			return nil, fmt.Errorf("jira source missing 'base_url' in config")
		}
		if config["email"] == "" {
			return nil, fmt.Errorf("jira source missing 'email' in config")
		}
		if config["token"] == "" {
			return nil, fmt.Errorf("jira source missing 'token' in config")
		}
		if config["project"] == "" && config["jql"] == "" {
			return nil, fmt.Errorf("jira source requires at least one of 'project' or 'jql' in config")
		}

		concurrency := jiraDefaultConcurrency
		if v, err := strconv.Atoi(config["concurrency"]); err == nil && v > 0 {
			concurrency = v
		}

		pageSize := jiraDefaultPageSize
		if v, err := strconv.Atoi(config["page_size"]); err == nil && v > 0 {
			pageSize = v
			if pageSize > jiraMaxPageSize {
				pageSize = jiraMaxPageSize
			}
		}

		var fields []string
		if config["fields"] != "" {
			fields = strings.Split(config["fields"], ",")
		} else {
			fields = jiraDefaultFields
		}

		return NewJiraConnector(
			config["base_url"],
			config["email"],
			config["token"],
			config["project"],
			config["jql"],
			concurrency,
			pageSize,
			fields,
		), nil
	})
}

// JiraConnector scans Jira Cloud for issues.
type JiraConnector struct {
	baseURL     string
	email       string
	token       string
	project     string
	jql         string
	concurrency int
	pageSize    int
	fields      []string
	client      HTTPClient
}

// NewJiraConnector creates a connector for the given Jira instance.
func NewJiraConnector(baseURL, email, token, project, jql string, concurrency, pageSize int, fields []string) *JiraConnector {
	return &JiraConnector{
		baseURL:     strings.TrimRight(baseURL, "/"),
		email:       email,
		token:       token,
		project:     project,
		jql:         jql,
		concurrency: concurrency,
		pageSize:    pageSize,
		fields:      fields,
		client:      http.DefaultClient,
	}
}

// Name returns the connector type identifier.
func (c *JiraConnector) Name() string {
	return SourceTypeJira
}

// SourceName returns a human-readable name for this source.
func (c *JiraConnector) SourceName() string {
	if c.project != "" {
		return c.project
	}
	return "jira-jql"
}

// Config returns the connector's configuration for source registration.
func (c *JiraConnector) Config(mode string) map[string]string {
	cfg := map[string]string{
		"base_url": c.baseURL,
		"email":    c.email,
	}
	if c.project != "" {
		cfg["project"] = c.project
	}
	if c.jql != "" {
		cfg["jql"] = c.jql
	}
	if c.concurrency != jiraDefaultConcurrency {
		cfg["concurrency"] = strconv.Itoa(c.concurrency)
	}
	if c.pageSize != jiraDefaultPageSize {
		cfg["page_size"] = strconv.Itoa(c.pageSize)
	}
	if len(c.fields) > 0 {
		cfg["fields"] = strings.Join(c.fields, ",")
	}
	if mode == model.SourceModeLocal {
		cfg["token"] = c.token
	}
	return cfg
}

// jiraSearchResponse is the JSON response from the Jira search endpoint.
type jiraSearchResponse struct {
	StartAt    int         `json:"startAt"`
	MaxResults int         `json:"maxResults"`
	Total      int         `json:"total"`
	Issues     []jiraIssue `json:"issues"`
}

// jiraIssue represents a Jira issue from the API.
type jiraIssue struct {
	Key    string          `json:"key"`
	Fields jiraIssueFields `json:"fields"`
}

// jiraIssueFields holds the fields of a Jira issue.
type jiraIssueFields struct {
	Summary  string           `json:"summary"`
	Status   *jiraNameField   `json:"status"`
	Assignee *jiraNameField   `json:"assignee"`
	Labels   []string         `json:"labels"`
	Created  string           `json:"created"`
	Updated  string           `json:"updated"`
	Description json.RawMessage `json:"description"`
	Comment  *jiraCommentPage `json:"comment"`
}

// jiraNameField is a Jira field with a displayName (or name for status).
type jiraNameField struct {
	DisplayName string `json:"displayName"`
	Name        string `json:"name"`
}

// jiraCommentPage is the paginated comment response embedded in an issue.
type jiraCommentPage struct {
	Comments []jiraComment `json:"comments"`
	Total    int           `json:"total"`
}

// jiraComment is a single comment on a Jira issue.
type jiraComment struct {
	Author *jiraNameField  `json:"author"`
	Body   json.RawMessage `json:"body"`
	Created string         `json:"created"`
}

// Scan fetches issues from Jira and returns new/changed documents and deleted paths.
func (c *JiraConnector) Scan(ctx context.Context, opts ScanOptions) ([]model.RawDocument, []string, error) {
	known := opts.Known
	seen := make(map[string]bool, len(known))

	jql := c.buildJQL(opts.LastIngest)

	var docs []model.RawDocument
	startAt := 0

	for {
		select {
		case <-ctx.Done():
			return nil, nil, ctx.Err()
		default:
		}

		result, err := c.searchIssues(ctx, jql, startAt)
		if err != nil {
			return nil, nil, err
		}

		for _, issue := range result.Issues {
			path := issue.Key
			seen[path] = true

			content := c.buildIssueContent(issue)
			hash := sha256.Sum256([]byte(content))
			checksum := fmt.Sprintf("%x", hash)

			// Skip unchanged issues.
			if prev, ok := known[path]; ok && prev == checksum {
				continue
			}

			var contentDate time.Time
			if issue.Fields.Updated != "" {
				contentDate = parseJiraTimestamp(issue.Fields.Updated)
			}

			var author string
			if issue.Fields.Assignee != nil {
				author = issue.Fields.Assignee.DisplayName
			}

			sourceURI := fmt.Sprintf("%s/browse/%s", c.baseURL, issue.Key)

			docs = append(docs, model.RawDocument{
				Path:        path,
				Content:     []byte(content),
				ContentDate: contentDate,
				Author:      author,
				SourceURI:   sourceURI,
				SourceType:  SourceTypeJira,
				SourceName:  c.SourceName(),
				Checksum:    checksum,
			})
		}

		// Check if we've fetched all issues.
		startAt += len(result.Issues)
		if startAt >= result.Total || len(result.Issues) == 0 {
			break
		}
	}

	// Detect deleted issues: paths in known that were not seen.
	// Only do full deletion detection when not doing incremental time-based scan,
	// since incremental scans only return recently updated issues.
	var deleted []string
	if opts.LastIngest == nil {
		for knownPath := range known {
			if !seen[knownPath] {
				deleted = append(deleted, knownPath)
			}
		}
	}

	return docs, deleted, nil
}

// buildJQL constructs the JQL query string from project, custom JQL, and last ingest time.
func (c *JiraConnector) buildJQL(lastIngest *time.Time) string {
	var parts []string

	if c.project != "" {
		parts = append(parts, fmt.Sprintf("project = %q", c.project))
	}
	if c.jql != "" {
		parts = append(parts, "("+c.jql+")")
	}
	if lastIngest != nil {
		// Jira JQL date format: "yyyy-MM-dd HH:mm"
		ts := lastIngest.UTC().Format("2006-01-02 15:04")
		parts = append(parts, fmt.Sprintf("updated > %q", ts))
	}

	if len(parts) == 0 {
		return "ORDER BY updated DESC"
	}
	return strings.Join(parts, " AND ") + " ORDER BY updated DESC"
}

// searchIssues fetches a page of issues from the Jira search API.
func (c *JiraConnector) searchIssues(ctx context.Context, jql string, startAt int) (*jiraSearchResponse, error) {
	endpoint := fmt.Sprintf("%s/rest/api/3/search?jql=%s&startAt=%d&maxResults=%d&fields=summary,status,assignee,labels,created,updated,description,comment",
		c.baseURL, url.QueryEscape(jql), startAt, c.pageSize)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}

	req.SetBasicAuth(c.email, c.token)
	req.Header.Set("Accept", "application/json")

	resp, err := c.doWithRetry(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("jira API request: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read response body: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("jira API error (status %d): %s", resp.StatusCode, string(body))
	}

	var result jiraSearchResponse
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, fmt.Errorf("parse response: %w", err)
	}

	return &result, nil
}

// buildIssueContent constructs the full text content for a Jira issue,
// including metadata header, summary, description, and comments.
func (c *JiraConnector) buildIssueContent(issue jiraIssue) string {
	var sb strings.Builder

	// Metadata header.
	sb.WriteString(fmt.Sprintf("[%s] %s\n", issue.Key, issue.Fields.Summary))

	for _, field := range c.fields {
		switch field {
		case "assignee":
			if issue.Fields.Assignee != nil {
				sb.WriteString(fmt.Sprintf("Assignee: %s\n", issue.Fields.Assignee.DisplayName))
			}
		case "status":
			if issue.Fields.Status != nil {
				sb.WriteString(fmt.Sprintf("Status: %s\n", issue.Fields.Status.Name))
			}
		case "labels":
			if len(issue.Fields.Labels) > 0 {
				sb.WriteString(fmt.Sprintf("Labels: %s\n", strings.Join(issue.Fields.Labels, ", ")))
			}
		case "created":
			if issue.Fields.Created != "" {
				sb.WriteString(fmt.Sprintf("Created: %s\n", issue.Fields.Created))
			}
		case "updated":
			if issue.Fields.Updated != "" {
				sb.WriteString(fmt.Sprintf("Updated: %s\n", issue.Fields.Updated))
			}
		}
	}

	sb.WriteString("\n")

	// Description (ADF to plaintext).
	if len(issue.Fields.Description) > 0 && string(issue.Fields.Description) != "null" {
		desc := adfToPlaintext(issue.Fields.Description)
		if desc != "" {
			sb.WriteString("Description:\n")
			sb.WriteString(desc)
			sb.WriteString("\n\n")
		}
	}

	// Comments.
	if issue.Fields.Comment != nil && len(issue.Fields.Comment.Comments) > 0 {
		sb.WriteString("Comments:\n")
		for _, comment := range issue.Fields.Comment.Comments {
			var authorName string
			if comment.Author != nil {
				authorName = comment.Author.DisplayName
			}
			body := adfToPlaintext(comment.Body)
			if body != "" {
				if authorName != "" {
					sb.WriteString(fmt.Sprintf("[%s] %s:\n%s\n\n", comment.Created, authorName, body))
				} else {
					sb.WriteString(fmt.Sprintf("[%s]\n%s\n\n", comment.Created, body))
				}
			}
		}
	}

	return sb.String()
}

// doWithRetry executes an HTTP request with exponential backoff on 429 responses.
func (c *JiraConnector) doWithRetry(ctx context.Context, req *http.Request) (*http.Response, error) {
	retries := 0
	backoff := time.Second

	for {
		resp, err := c.client.Do(req)
		if err != nil {
			return nil, err
		}

		if resp.StatusCode != http.StatusTooManyRequests {
			return resp, nil
		}

		retries++
		if retries > jiraMaxRetries {
			resp.Body.Close()
			return nil, fmt.Errorf("rate limit exceeded after %d retries", jiraMaxRetries)
		}

		// Check Retry-After header.
		retryAfter := resp.Header.Get("Retry-After")
		resp.Body.Close()

		wait := backoff
		if retryAfter != "" {
			if s, err := strconv.Atoi(retryAfter); err == nil && s > 0 {
				wait = time.Duration(s) * time.Second
			}
		}

		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(wait):
		}

		// Exponential backoff for next iteration.
		backoff *= 2

		// Recreate the request for retry.
		req = req.Clone(ctx)
	}
}

// adfToPlaintext recursively extracts plaintext from an Atlassian Document Format JSON node.
func adfToPlaintext(data json.RawMessage) string {
	if len(data) == 0 {
		return ""
	}

	var node adfNode
	if err := json.Unmarshal(data, &node); err != nil {
		return ""
	}

	return extractADFText(&node)
}

// adfNode represents a node in the Atlassian Document Format.
type adfNode struct {
	Type    string          `json:"type"`
	Text    string          `json:"text"`
	Content json.RawMessage `json:"content"`
}

// extractADFText recursively extracts text from an ADF node tree.
func extractADFText(node *adfNode) string {
	if node == nil {
		return ""
	}

	// Text nodes contain the actual text content.
	if node.Type == "text" {
		return node.Text
	}

	// Block-level types that should have newlines after them.
	blockTypes := map[string]bool{
		"paragraph":  true,
		"heading":    true,
		"listItem":   true,
		"blockquote": true,
		"codeBlock":  true,
		"mediaGroup": true,
		"table":      true,
		"tableRow":   true,
		"tableCell":  true,
		"rule":       true,
	}

	// Recurse into content array.
	if len(node.Content) == 0 {
		return ""
	}

	var children []adfNode
	if err := json.Unmarshal(node.Content, &children); err != nil {
		return ""
	}

	var sb strings.Builder
	for _, child := range children {
		text := extractADFText(&child)
		sb.WriteString(text)

		if blockTypes[child.Type] {
			sb.WriteString("\n")
		}
	}

	return sb.String()
}

// jiraTimestampFormats lists timestamp formats used by Jira, tried in order.
var jiraTimestampFormats = []string{
	time.RFC3339,
	"2006-01-02T15:04:05.000+0000",
	"2006-01-02T15:04:05.000-0700",
	"2006-01-02T15:04:05.000Z0700",
}

// parseJiraTimestamp tries multiple known Jira timestamp formats
// and returns the parsed time, or zero time if none match.
func parseJiraTimestamp(s string) time.Time {
	for _, layout := range jiraTimestampFormats {
		if t, err := time.Parse(layout, s); err == nil {
			return t
		}
	}
	return time.Time{}
}
