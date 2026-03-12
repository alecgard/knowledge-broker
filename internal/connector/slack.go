package connector

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/knowledge-broker/knowledge-broker/pkg/model"
)

// SourceTypeSlack is the source type identifier for Slack sources.
const SourceTypeSlack = "slack"

func init() {
	Register(SourceTypeSlack, func(config map[string]string) (Connector, error) {
		channels := strings.Split(config["channels"], ",")
		return NewSlackConnector(config["token"], channels, config["workspace"]), nil
	})
}

// defaultLookbackDays is the default number of days to look back for messages.
const defaultLookbackDays = 90

// maxRetries is the maximum number of retries for rate-limited requests.
const maxRetries = 5

// SlackConnector fetches messages from Slack channels via the Web API.
type SlackConnector struct {
	token         string
	channelIDs    []string
	httpClient    HTTPClient
	lookbackDays  int
	workspaceName string
	baseURL       string
}

// NewSlackConnector creates a new Slack connector for the given channels.
// If workspaceName is empty, SourceName falls back to "slack:" joined with channel IDs.
func NewSlackConnector(token string, channelIDs []string, workspaceName string) *SlackConnector {
	return &SlackConnector{
		token:         token,
		channelIDs:    channelIDs,
		httpClient:    http.DefaultClient,
		lookbackDays:  defaultLookbackDays,
		workspaceName: workspaceName,
		baseURL:       "https://slack.com",
	}
}

// Name returns the connector type identifier.
func (c *SlackConnector) Name() string {
	return SourceTypeSlack
}

// SourceName returns a human-readable name for this Slack source.
// If a workspace name was provided, it is returned; otherwise falls back to
// "slack:" joined with the configured channel IDs.
func (c *SlackConnector) SourceName() string {
	if c.workspaceName != "" {
		return c.workspaceName
	}
	return "slack:" + strings.Join(c.channelIDs, ",")
}

// Config returns the connector's configuration for source registration.
func (c *SlackConnector) Config(mode string) map[string]string {
	cfg := map[string]string{
		"channels": strings.Join(c.channelIDs, ","),
	}
	if mode != model.SourceModePush {
		cfg["token"] = c.token
	}
	return cfg
}

// Scan fetches messages from configured Slack channels and returns them as documents.
func (c *SlackConnector) Scan(ctx context.Context, opts ScanOptions) ([]model.RawDocument, []string, error) {
	known := opts.Known
	oldest := strconv.FormatInt(time.Now().AddDate(0, 0, -c.lookbackDays).Unix(), 10)

	var docs []model.RawDocument
	for _, channelID := range c.channelIDs {
		channelName, err := c.getChannelName(ctx, channelID)
		if err != nil {
			return nil, nil, fmt.Errorf("get channel info for %s: %w", channelID, err)
		}

		messages, err := c.fetchAllMessages(ctx, channelID, oldest)
		if err != nil {
			return nil, nil, fmt.Errorf("fetch messages for channel %s: %w", channelID, err)
		}

		channelDocs := c.buildDocuments(ctx, channelName, channelID, messages, known)
		docs = append(docs, channelDocs...)
	}

	// Deleted is always nil for Slack. Messages naturally age out of the
	// lookback window, so explicit deletion detection is unnecessary —
	// documents that fall outside the window simply won't be returned on
	// the next scan, and the ingestion layer handles cleanup.
	return docs, nil, nil
}

// slackMessage represents a single Slack message from the API.
type slackMessage struct {
	TS        string `json:"ts"`
	User      string `json:"user"`
	Text      string `json:"text"`
	ThreadTS  string `json:"thread_ts"`
	ReplyCount int   `json:"reply_count"`
}

// slackHistoryResponse is the response from conversations.history.
type slackHistoryResponse struct {
	OK               bool           `json:"ok"`
	Error            string         `json:"error"`
	Messages         []slackMessage `json:"messages"`
	ResponseMetadata struct {
		NextCursor string `json:"next_cursor"`
	} `json:"response_metadata"`
}

// slackRepliesResponse is the response from conversations.replies.
type slackRepliesResponse struct {
	OK               bool           `json:"ok"`
	Error            string         `json:"error"`
	Messages         []slackMessage `json:"messages"`
	ResponseMetadata struct {
		NextCursor string `json:"next_cursor"`
	} `json:"response_metadata"`
}

// slackChannelInfoResponse is the response from conversations.info.
type slackChannelInfoResponse struct {
	OK      bool `json:"ok"`
	Error   string `json:"error"`
	Channel struct {
		Name string `json:"name"`
	} `json:"channel"`
}

// getChannelName fetches the channel name from the Slack API.
func (c *SlackConnector) getChannelName(ctx context.Context, channelID string) (string, error) {
	url := c.baseURL + "/api/conversations.info?channel=" + channelID
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Authorization", "Bearer "+c.token)

	resp, err := c.doWithRetry(ctx, req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("API returned status %d: %s", resp.StatusCode, string(body))
	}

	var result slackChannelInfoResponse
	if err := json.Unmarshal(body, &result); err != nil {
		return "", fmt.Errorf("parse response: %w", err)
	}
	if !result.OK {
		return "", fmt.Errorf("Slack API error: %s", result.Error)
	}

	return result.Channel.Name, nil
}

// fetchAllMessages retrieves all messages from a channel, handling pagination.
func (c *SlackConnector) fetchAllMessages(ctx context.Context, channelID, oldest string) ([]slackMessage, error) {
	var allMessages []slackMessage
	cursor := ""

	for {
		url := c.baseURL + "/api/conversations.history?channel=" + channelID + "&oldest=" + oldest + "&limit=200"
		if cursor != "" {
			url += "&cursor=" + cursor
		}

		req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
		if err != nil {
			return nil, err
		}
		req.Header.Set("Authorization", "Bearer "+c.token)

		resp, err := c.doWithRetry(ctx, req)
		if err != nil {
			return nil, err
		}

		body, err := io.ReadAll(resp.Body)
		resp.Body.Close()
		if err != nil {
			return nil, fmt.Errorf("read response: %w", err)
		}

		if resp.StatusCode != http.StatusOK {
			return nil, fmt.Errorf("API returned status %d: %s", resp.StatusCode, string(body))
		}

		var result slackHistoryResponse
		if err := json.Unmarshal(body, &result); err != nil {
			return nil, fmt.Errorf("parse response: %w", err)
		}
		if !result.OK {
			return nil, fmt.Errorf("Slack API error: %s", result.Error)
		}

		allMessages = append(allMessages, result.Messages...)

		if result.ResponseMetadata.NextCursor == "" {
			break
		}
		cursor = result.ResponseMetadata.NextCursor
	}

	return allMessages, nil
}

// fetchThreadReplies retrieves all replies for a thread.
func (c *SlackConnector) fetchThreadReplies(ctx context.Context, channelID, threadTS string) ([]slackMessage, error) {
	var allReplies []slackMessage
	cursor := ""

	for {
		url := c.baseURL + "/api/conversations.replies?channel=" + channelID + "&ts=" + threadTS + "&limit=200"
		if cursor != "" {
			url += "&cursor=" + cursor
		}

		req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
		if err != nil {
			return nil, err
		}
		req.Header.Set("Authorization", "Bearer "+c.token)

		resp, err := c.doWithRetry(ctx, req)
		if err != nil {
			return nil, err
		}

		body, err := io.ReadAll(resp.Body)
		resp.Body.Close()
		if err != nil {
			return nil, fmt.Errorf("read response: %w", err)
		}

		if resp.StatusCode != http.StatusOK {
			return nil, fmt.Errorf("API returned status %d: %s", resp.StatusCode, string(body))
		}

		var result slackRepliesResponse
		if err := json.Unmarshal(body, &result); err != nil {
			return nil, fmt.Errorf("parse response: %w", err)
		}
		if !result.OK {
			return nil, fmt.Errorf("Slack API error: %s", result.Error)
		}

		allReplies = append(allReplies, result.Messages...)

		if result.ResponseMetadata.NextCursor == "" {
			break
		}
		cursor = result.ResponseMetadata.NextCursor
	}

	return allReplies, nil
}

// buildDocuments groups messages into RawDocuments: threads become single documents,
// non-threaded messages are grouped by day.
func (c *SlackConnector) buildDocuments(ctx context.Context, channelName, channelID string, messages []slackMessage, known map[string]string) []model.RawDocument {
	var docs []model.RawDocument

	// Separate threaded (parent) messages from non-threaded messages.
	// dailyMessages collects non-threaded messages grouped by date string.
	dailyMessages := make(map[string][]slackMessage)

	for _, msg := range messages {
		if msg.ReplyCount > 0 {
			// This is a thread parent — fetch replies and build a thread document.
			replies, err := c.fetchThreadReplies(ctx, channelID, msg.TS)
			if err != nil {
				// Skip threads we can't fetch replies for.
				continue
			}
			doc := c.buildThreadDocument(channelName, channelID, replies)
			if doc == nil {
				continue
			}
			// Check if unchanged.
			if prev, ok := known[doc.Path]; ok && prev == doc.Checksum {
				continue
			}
			docs = append(docs, *doc)
		} else if msg.ThreadTS == "" || msg.ThreadTS == msg.TS {
			// Non-threaded message (no thread_ts, or thread_ts == ts with no replies).
			date := tsToTime(msg.TS).Format("2006-01-02")
			dailyMessages[date] = append(dailyMessages[date], msg)
		}
		// Messages with thread_ts != ts and ReplyCount == 0 are replies
		// fetched as part of history — skip them here since they'll be
		// fetched via conversations.replies for the parent.
	}

	// Build daily digest documents.
	for date, msgs := range dailyMessages {
		doc := c.buildDailyDocument(channelName, channelID, date, msgs)
		if doc == nil {
			continue
		}
		if prev, ok := known[doc.Path]; ok && prev == doc.Checksum {
			continue
		}
		docs = append(docs, *doc)
	}

	return docs
}

// buildThreadDocument creates a RawDocument from a thread (parent + replies).
func (c *SlackConnector) buildThreadDocument(channelName, channelID string, messages []slackMessage) *model.RawDocument {
	if len(messages) == 0 {
		return nil
	}

	// Sort by timestamp.
	sort.Slice(messages, func(i, j int) bool {
		return messages[i].TS < messages[j].TS
	})

	var sb strings.Builder
	var latestTime time.Time
	author := messages[0].User
	threadTS := messages[0].TS

	for _, msg := range messages {
		t := tsToTime(msg.TS)
		if t.After(latestTime) {
			latestTime = t
		}
		sb.WriteString(fmt.Sprintf("[%s] %s: %s\n", t.Format(time.RFC3339), msg.User, msg.Text))
	}

	content := sb.String()
	hash := sha256.Sum256([]byte(content))
	checksum := fmt.Sprintf("%x", hash)
	path := channelName + "/" + threadTS

	return &model.RawDocument{
		Path:         path,
		Content:      []byte(content),
		ContentDate: latestTime,
		Author:       author,
		SourceURI:    fmt.Sprintf("slack://channel/%s/p%s", channelID, strings.Replace(threadTS, ".", "", 1)),
		SourceType:   SourceTypeSlack,
		SourceName:   c.SourceName(),
		Checksum:     checksum,
	}
}

// buildDailyDocument creates a RawDocument from a day's non-threaded messages.
func (c *SlackConnector) buildDailyDocument(channelName, channelID, date string, messages []slackMessage) *model.RawDocument {
	if len(messages) == 0 {
		return nil
	}

	// Sort by timestamp.
	sort.Slice(messages, func(i, j int) bool {
		return messages[i].TS < messages[j].TS
	})

	var sb strings.Builder
	var latestTime time.Time
	author := messages[0].User

	for _, msg := range messages {
		t := tsToTime(msg.TS)
		if t.After(latestTime) {
			latestTime = t
		}
		sb.WriteString(fmt.Sprintf("[%s] %s: %s\n", t.Format(time.RFC3339), msg.User, msg.Text))
	}

	content := sb.String()
	hash := sha256.Sum256([]byte(content))
	checksum := fmt.Sprintf("%x", hash)
	path := channelName + "/" + date

	return &model.RawDocument{
		Path:         path,
		Content:      []byte(content),
		ContentDate: latestTime,
		Author:       author,
		SourceURI:    fmt.Sprintf("slack://channel/%s/p%s", channelID, strings.Replace(messages[0].TS, ".", "", 1)),
		SourceType:   SourceTypeSlack,
		SourceName:   c.SourceName(),
		Checksum:     checksum,
	}
}

// doWithRetry executes an HTTP request, handling rate limits (429) by
// respecting the Retry-After header. It retries up to maxRetries times
// before returning an error.
func (c *SlackConnector) doWithRetry(ctx context.Context, req *http.Request) (*http.Response, error) {
	retries := 0
	for {
		resp, err := c.httpClient.Do(req)
		if err != nil {
			return nil, err
		}

		if resp.StatusCode != http.StatusTooManyRequests {
			return resp, nil
		}

		retries++
		if retries > maxRetries {
			resp.Body.Close()
			return nil, fmt.Errorf("rate limit exceeded after %d retries", maxRetries)
		}

		// Parse Retry-After header.
		retryAfter := resp.Header.Get("Retry-After")
		resp.Body.Close()

		waitSeconds := 1
		if retryAfter != "" {
			if s, err := strconv.Atoi(retryAfter); err == nil && s > 0 {
				waitSeconds = s
			}
		}

		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(time.Duration(waitSeconds) * time.Second):
		}

		// Recreate the request for retry (body may have been consumed).
		req = req.Clone(ctx)
	}
}

// tsToTime converts a Slack timestamp (e.g., "1234567890.123456") to a time.Time.
// Handles edge cases: empty strings, missing dot separator, non-numeric values.
func tsToTime(ts string) time.Time {
	if ts == "" {
		return time.Time{}
	}
	secStr := ts
	if i := strings.IndexByte(ts, '.'); i >= 0 {
		secStr = ts[:i]
	}
	sec, err := strconv.ParseInt(secStr, 10, 64)
	if err != nil {
		return time.Time{}
	}
	return time.Unix(sec, 0).UTC()
}
