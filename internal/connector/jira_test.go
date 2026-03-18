package connector

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/knowledge-broker/knowledge-broker/pkg/model"
)

// Compile-time interface compliance check.
var _ Connector = (*JiraConnector)(nil)

func makeJiraSearchResponse(t *testing.T, issues []jiraIssue, total, startAt int) *http.Response {
	t.Helper()
	resp := jiraSearchResponse{
		StartAt:    startAt,
		MaxResults: 50,
		Total:      total,
		Issues:     issues,
	}
	body, err := json.Marshal(resp)
	if err != nil {
		t.Fatal(err)
	}
	return &http.Response{
		StatusCode: http.StatusOK,
		Body:       io.NopCloser(strings.NewReader(string(body))),
		Header:     http.Header{},
	}
}

func makeJiraIssue(key, summary, description, status, assignee, created, updated string, labels []string, comments []jiraComment) jiraIssue {
	issue := jiraIssue{
		Key: key,
		Fields: jiraIssueFields{
			Summary: summary,
			Created: created,
			Updated: updated,
			Labels:  labels,
		},
	}
	if status != "" {
		issue.Fields.Status = &jiraNameField{Name: status}
	}
	if assignee != "" {
		issue.Fields.Assignee = &jiraNameField{DisplayName: assignee}
	}
	if description != "" {
		issue.Fields.Description = json.RawMessage(description)
	}
	if len(comments) > 0 {
		issue.Fields.Comment = &jiraCommentPage{
			Comments: comments,
			Total:    len(comments),
		}
	}
	return issue
}

// makeADFDoc creates a simple ADF document with a single paragraph containing the given text.
func makeADFDoc(text string) string {
	doc := map[string]interface{}{
		"type":    "doc",
		"version": 1,
		"content": []map[string]interface{}{
			{
				"type": "paragraph",
				"content": []map[string]interface{}{
					{
						"type": "text",
						"text": text,
					},
				},
			},
		},
	}
	b, _ := json.Marshal(doc)
	return string(b)
}

func TestJiraConnector_FactoryValidation(t *testing.T) {
	tests := []struct {
		name    string
		config  map[string]string
		wantErr string
	}{
		{
			name:    "missing base_url",
			config:  map[string]string{"email": "x", "token": "y", "project": "P"},
			wantErr: "missing 'base_url'",
		},
		{
			name:    "missing email",
			config:  map[string]string{"base_url": "x", "token": "y", "project": "P"},
			wantErr: "missing 'email'",
		},
		{
			name:    "missing token",
			config:  map[string]string{"base_url": "x", "email": "y", "project": "P"},
			wantErr: "missing 'token'",
		},
		{
			name:    "missing project and jql",
			config:  map[string]string{"base_url": "x", "email": "y", "token": "z"},
			wantErr: "requires at least one of 'project' or 'jql'",
		},
		{
			name:   "valid with project",
			config: map[string]string{"base_url": "https://x.atlassian.net", "email": "y", "token": "z", "project": "P"},
		},
		{
			name:   "valid with jql",
			config: map[string]string{"base_url": "https://x.atlassian.net", "email": "y", "token": "z", "jql": "sprint = 1"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			factory := registry[SourceTypeJira]
			_, err := factory(tt.config)
			if tt.wantErr != "" {
				if err == nil {
					t.Fatalf("expected error containing %q, got nil", tt.wantErr)
				}
				if !strings.Contains(err.Error(), tt.wantErr) {
					t.Errorf("error %q does not contain %q", err.Error(), tt.wantErr)
				}
			} else if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
		})
	}
}

func TestJiraConnector_Name(t *testing.T) {
	c := NewJiraConnector("https://example.atlassian.net", "user@example.com", "token", "PROJ", "", 2, 50, jiraDefaultFields)
	if got := c.Name(); got != "jira" {
		t.Errorf("Name() = %q, want %q", got, "jira")
	}
}

func TestJiraConnector_SourceName(t *testing.T) {
	t.Run("with project", func(t *testing.T) {
		c := NewJiraConnector("https://example.atlassian.net", "user@example.com", "token", "PROJ", "", 2, 50, jiraDefaultFields)
		if got := c.SourceName(); got != "PROJ" {
			t.Errorf("SourceName() = %q, want %q", got, "PROJ")
		}
	})

	t.Run("with jql only", func(t *testing.T) {
		c := NewJiraConnector("https://example.atlassian.net", "user@example.com", "token", "", "sprint=1", 2, 50, jiraDefaultFields)
		if got := c.SourceName(); got != "jira-jql" {
			t.Errorf("SourceName() = %q, want %q", got, "jira-jql")
		}
	})
}

func TestJiraConnector_Config(t *testing.T) {
	c := NewJiraConnector("https://example.atlassian.net", "user@example.com", "token", "PROJ", "", 2, 50, jiraDefaultFields)

	t.Run("local mode includes token", func(t *testing.T) {
		cfg := c.Config(model.SourceModeLocal)
		if cfg["base_url"] != "https://example.atlassian.net" {
			t.Errorf("base_url = %q", cfg["base_url"])
		}
		if cfg["email"] != "user@example.com" {
			t.Errorf("email = %q", cfg["email"])
		}
		if cfg["token"] != "token" {
			t.Errorf("token = %q", cfg["token"])
		}
		if cfg["project"] != "PROJ" {
			t.Errorf("project = %q", cfg["project"])
		}
	})

	t.Run("push mode omits token", func(t *testing.T) {
		cfg := c.Config(model.SourceModePush)
		if _, ok := cfg["token"]; ok {
			t.Error("push mode should not include token")
		}
	})
}

func TestJiraConnector_BasicScan(t *testing.T) {
	issues := []jiraIssue{
		makeJiraIssue("PROJ-1", "First issue", makeADFDoc("Description text"), "Open", "Alice", "2025-01-15T10:30:00.000+0000", "2025-01-16T10:30:00.000+0000", []string{"bug"}, nil),
		makeJiraIssue("PROJ-2", "Second issue", makeADFDoc("Another description"), "In Progress", "Bob", "2025-02-20T14:00:00.000+0000", "2025-02-21T14:00:00.000+0000", nil, nil),
	}

	mock := &mockHTTPClient{
		responses: []*http.Response{
			makeJiraSearchResponse(t, issues, 2, 0),
		},
	}

	c := NewJiraConnector("https://example.atlassian.net", "user@example.com", "token", "PROJ", "", 2, 50, jiraDefaultFields)
	c.client = mock

	docs, deleted, err := c.Scan(context.Background(), ScanOptions{})
	if err != nil {
		t.Fatalf("Scan() error: %v", err)
	}
	if len(deleted) != 0 {
		t.Errorf("expected no deletions, got %d", len(deleted))
	}
	if len(docs) != 2 {
		t.Fatalf("expected 2 docs, got %d", len(docs))
	}

	// Verify first document.
	doc := docs[0]
	if doc.Path != "PROJ-1" {
		t.Errorf("Path = %q, want %q", doc.Path, "PROJ-1")
	}
	if doc.Author != "Alice" {
		t.Errorf("Author = %q, want %q", doc.Author, "Alice")
	}
	if doc.SourceType != SourceTypeJira {
		t.Errorf("SourceType = %q", doc.SourceType)
	}
	if doc.SourceName != "PROJ" {
		t.Errorf("SourceName = %q", doc.SourceName)
	}
	expectedURI := "https://example.atlassian.net/browse/PROJ-1"
	if doc.SourceURI != expectedURI {
		t.Errorf("SourceURI = %q, want %q", doc.SourceURI, expectedURI)
	}
	if doc.ContentDate.IsZero() {
		t.Error("ContentDate should not be zero")
	}

	// Content should include summary, metadata, and description.
	content := string(doc.Content)
	if !strings.Contains(content, "PROJ-1") {
		t.Error("content should contain issue key")
	}
	if !strings.Contains(content, "First issue") {
		t.Error("content should contain summary")
	}
	if !strings.Contains(content, "Description text") {
		t.Error("content should contain description")
	}
	if !strings.Contains(content, "Assignee: Alice") {
		t.Error("content should contain assignee")
	}
	if !strings.Contains(content, "Status: Open") {
		t.Error("content should contain status")
	}
	if !strings.Contains(content, "Labels: bug") {
		t.Error("content should contain labels")
	}
}

func TestJiraConnector_ScanWithJQL(t *testing.T) {
	issues := []jiraIssue{
		makeJiraIssue("PROJ-5", "Sprint issue", makeADFDoc("Sprint work"), "Done", "Carol", "2025-03-01T00:00:00.000+0000", "2025-03-02T00:00:00.000+0000", nil, nil),
	}

	var capturedURL string
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedURL = r.URL.String()
		w.Header().Set("Content-Type", "application/json")
		resp := jiraSearchResponse{Total: 1, Issues: issues}
		json.NewEncoder(w).Encode(resp)
	}))
	defer ts.Close()

	c := NewJiraConnector(ts.URL, "user@example.com", "token", "", "sprint = 42", 2, 50, jiraDefaultFields)

	docs, _, err := c.Scan(context.Background(), ScanOptions{})
	if err != nil {
		t.Fatalf("Scan() error: %v", err)
	}
	if len(docs) != 1 {
		t.Fatalf("expected 1 doc, got %d", len(docs))
	}

	// Verify JQL was included in the request.
	if !strings.Contains(capturedURL, "sprint") {
		t.Errorf("URL should contain JQL query, got: %s", capturedURL)
	}
}

func TestJiraConnector_IncrementalScan(t *testing.T) {
	issue := makeJiraIssue("PROJ-10", "Updated issue", makeADFDoc("New content"), "Open", "Alice",
		"2025-01-01T00:00:00.000+0000", "2025-03-15T10:00:00.000+0000", nil, nil)

	var capturedURL string
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedURL = r.URL.String()
		w.Header().Set("Content-Type", "application/json")
		resp := jiraSearchResponse{Total: 1, Issues: []jiraIssue{issue}}
		json.NewEncoder(w).Encode(resp)
	}))
	defer ts.Close()

	c := NewJiraConnector(ts.URL, "user@example.com", "token", "PROJ", "", 2, 50, jiraDefaultFields)

	lastIngest := time.Date(2025, 3, 1, 0, 0, 0, 0, time.UTC)
	docs, deleted, err := c.Scan(context.Background(), ScanOptions{
		Known:      map[string]string{"PROJ-1": "old-checksum"},
		LastIngest: &lastIngest,
	})
	if err != nil {
		t.Fatalf("Scan() error: %v", err)
	}

	// Should have the updated issue.
	if len(docs) != 1 {
		t.Fatalf("expected 1 doc, got %d", len(docs))
	}
	if docs[0].Path != "PROJ-10" {
		t.Errorf("Path = %q, want %q", docs[0].Path, "PROJ-10")
	}

	// Incremental scan should not report deletions.
	if len(deleted) != 0 {
		t.Errorf("incremental scan should not report deletions, got %d", len(deleted))
	}

	// Verify the JQL includes updated > timestamp.
	if !strings.Contains(capturedURL, "updated") {
		t.Errorf("URL should contain updated filter, got: %s", capturedURL)
	}
}

func TestJiraConnector_DeletionDetection(t *testing.T) {
	// API returns only PROJ-1; PROJ-2 was previously known.
	issues := []jiraIssue{
		makeJiraIssue("PROJ-1", "Still here", makeADFDoc("content"), "Open", "", "2025-01-01T00:00:00.000+0000", "2025-01-01T00:00:00.000+0000", nil, nil),
	}

	mock := &mockHTTPClient{
		responses: []*http.Response{
			makeJiraSearchResponse(t, issues, 1, 0),
		},
	}

	c := NewJiraConnector("https://example.atlassian.net", "user@example.com", "token", "PROJ", "", 2, 50, jiraDefaultFields)
	c.client = mock

	known := map[string]string{
		"PROJ-1": "old-checksum",
		"PROJ-2": "some-checksum",
	}

	_, deleted, err := c.Scan(context.Background(), ScanOptions{Known: known})
	if err != nil {
		t.Fatalf("Scan() error: %v", err)
	}
	if len(deleted) != 1 {
		t.Fatalf("expected 1 deletion, got %d", len(deleted))
	}
	if deleted[0] != "PROJ-2" {
		t.Errorf("expected deleted path %q, got %q", "PROJ-2", deleted[0])
	}
}

func TestJiraConnector_Pagination(t *testing.T) {
	page1Issues := []jiraIssue{
		makeJiraIssue("PROJ-1", "Page 1", makeADFDoc("one"), "Open", "", "2025-01-01T00:00:00.000+0000", "2025-01-01T00:00:00.000+0000", nil, nil),
	}
	page2Issues := []jiraIssue{
		makeJiraIssue("PROJ-2", "Page 2", makeADFDoc("two"), "Open", "", "2025-01-02T00:00:00.000+0000", "2025-01-02T00:00:00.000+0000", nil, nil),
	}

	mock := &mockHTTPClient{
		responses: []*http.Response{
			makeJiraSearchResponse(t, page1Issues, 2, 0),
			makeJiraSearchResponse(t, page2Issues, 2, 1),
		},
	}

	c := NewJiraConnector("https://example.atlassian.net", "user@example.com", "token", "PROJ", "", 2, 1, jiraDefaultFields)
	c.client = mock

	docs, _, err := c.Scan(context.Background(), ScanOptions{})
	if err != nil {
		t.Fatalf("Scan() error: %v", err)
	}
	if len(docs) != 2 {
		t.Fatalf("expected 2 docs across pages, got %d", len(docs))
	}
	if len(mock.requests) != 2 {
		t.Errorf("expected 2 requests for pagination, got %d", len(mock.requests))
	}
}

func TestJiraConnector_ADFConversion(t *testing.T) {
	tests := []struct {
		name string
		adf  string
		want string
	}{
		{
			name: "simple paragraph",
			adf:  makeADFDoc("Hello world"),
			want: "Hello world\n",
		},
		{
			name: "multiple paragraphs",
			adf: func() string {
				doc := map[string]interface{}{
					"type":    "doc",
					"version": 1,
					"content": []map[string]interface{}{
						{
							"type": "paragraph",
							"content": []map[string]interface{}{
								{"type": "text", "text": "First paragraph"},
							},
						},
						{
							"type": "paragraph",
							"content": []map[string]interface{}{
								{"type": "text", "text": "Second paragraph"},
							},
						},
					},
				}
				b, _ := json.Marshal(doc)
				return string(b)
			}(),
			want: "First paragraph\nSecond paragraph\n",
		},
		{
			name: "heading and paragraph",
			adf: func() string {
				doc := map[string]interface{}{
					"type":    "doc",
					"version": 1,
					"content": []map[string]interface{}{
						{
							"type": "heading",
							"content": []map[string]interface{}{
								{"type": "text", "text": "Title"},
							},
						},
						{
							"type": "paragraph",
							"content": []map[string]interface{}{
								{"type": "text", "text": "Body text"},
							},
						},
					},
				}
				b, _ := json.Marshal(doc)
				return string(b)
			}(),
			want: "Title\nBody text\n",
		},
		{
			name: "inline formatting preserved as text",
			adf: func() string {
				doc := map[string]interface{}{
					"type":    "doc",
					"version": 1,
					"content": []map[string]interface{}{
						{
							"type": "paragraph",
							"content": []map[string]interface{}{
								{"type": "text", "text": "normal "},
								{"type": "text", "text": "bold", "marks": []map[string]interface{}{{"type": "strong"}}},
								{"type": "text", "text": " text"},
							},
						},
					},
				}
				b, _ := json.Marshal(doc)
				return string(b)
			}(),
			want: "normal bold text\n",
		},
		{
			name: "null description",
			adf:  "null",
			want: "",
		},
		{
			name: "empty",
			adf:  "",
			want: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := adfToPlaintext(json.RawMessage(tt.adf))
			if got != tt.want {
				t.Errorf("adfToPlaintext() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestJiraConnector_Comments(t *testing.T) {
	comments := []jiraComment{
		{
			Author:  &jiraNameField{DisplayName: "Alice"},
			Body:    json.RawMessage(makeADFDoc("This is a comment")),
			Created: "2025-01-15T10:30:00.000+0000",
		},
		{
			Author:  &jiraNameField{DisplayName: "Bob"},
			Body:    json.RawMessage(makeADFDoc("Another comment")),
			Created: "2025-01-16T10:30:00.000+0000",
		},
	}

	issue := makeJiraIssue("PROJ-1", "Issue with comments", makeADFDoc("Description"), "Open", "Alice",
		"2025-01-15T00:00:00.000+0000", "2025-01-16T10:30:00.000+0000", nil, comments)

	mock := &mockHTTPClient{
		responses: []*http.Response{
			makeJiraSearchResponse(t, []jiraIssue{issue}, 1, 0),
		},
	}

	c := NewJiraConnector("https://example.atlassian.net", "user@example.com", "token", "PROJ", "", 2, 50, jiraDefaultFields)
	c.client = mock

	docs, _, err := c.Scan(context.Background(), ScanOptions{})
	if err != nil {
		t.Fatalf("Scan() error: %v", err)
	}
	if len(docs) != 1 {
		t.Fatalf("expected 1 doc, got %d", len(docs))
	}

	content := string(docs[0].Content)
	if !strings.Contains(content, "Comments:") {
		t.Error("content should contain Comments section")
	}
	if !strings.Contains(content, "This is a comment") {
		t.Error("content should contain first comment text")
	}
	if !strings.Contains(content, "Another comment") {
		t.Error("content should contain second comment text")
	}
	if !strings.Contains(content, "Alice") {
		t.Error("content should contain comment author")
	}
}

func TestJiraConnector_RateLimitHandling(t *testing.T) {
	callCount := 0
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		if callCount == 1 {
			w.Header().Set("Retry-After", "1")
			w.WriteHeader(http.StatusTooManyRequests)
			w.Write([]byte(`{"error":"rate limited"}`))
			return
		}
		w.Header().Set("Content-Type", "application/json")
		resp := jiraSearchResponse{
			Total:  1,
			Issues: []jiraIssue{makeJiraIssue("PROJ-1", "Rate limited issue", makeADFDoc("content"), "Open", "", "2025-01-01T00:00:00.000+0000", "2025-01-01T00:00:00.000+0000", nil, nil)},
		}
		json.NewEncoder(w).Encode(resp)
	}))
	defer ts.Close()

	c := NewJiraConnector(ts.URL, "user@example.com", "token", "PROJ", "", 2, 50, jiraDefaultFields)

	docs, _, err := c.Scan(context.Background(), ScanOptions{})
	if err != nil {
		t.Fatalf("Scan() error: %v", err)
	}
	if len(docs) != 1 {
		t.Fatalf("expected 1 doc after retry, got %d", len(docs))
	}
	if callCount != 2 {
		t.Errorf("expected 2 HTTP calls (1 retry), got %d", callCount)
	}
}

func TestJiraConnector_RateLimitExceeded(t *testing.T) {
	// Verify that persistent 429s eventually produce an error (via context timeout).
	var responses []*http.Response
	for i := 0; i < 20; i++ {
		resp := &http.Response{
			StatusCode: http.StatusTooManyRequests,
			Header:     http.Header{"Retry-After": []string{"1"}},
			Body:       io.NopCloser(strings.NewReader(`{"error":"rate limited"}`)),
		}
		responses = append(responses, resp)
	}
	mock := &mockHTTPClient{responses: responses}

	c := NewJiraConnector("https://example.atlassian.net", "user@example.com", "token", "PROJ", "", 2, 50, jiraDefaultFields)
	c.client = mock

	// Cancel quickly to avoid waiting through real backoff sleeps.
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	_, _, err := c.Scan(ctx, ScanOptions{})
	if err == nil {
		t.Fatal("expected error from rate limiting or context timeout")
	}
}

func TestJiraConnector_APIError(t *testing.T) {
	mock := &mockHTTPClient{
		responses: []*http.Response{
			{
				StatusCode: http.StatusForbidden,
				Body:       io.NopCloser(strings.NewReader(`{"errorMessages":["Forbidden"]}`)),
				Header:     http.Header{},
			},
		},
	}

	c := NewJiraConnector("https://example.atlassian.net", "user@example.com", "token", "PROJ", "", 2, 50, jiraDefaultFields)
	c.client = mock

	_, _, err := c.Scan(context.Background(), ScanOptions{})
	if err == nil {
		t.Fatal("expected error for non-200 response")
	}
	if !strings.Contains(err.Error(), "403") {
		t.Errorf("error should mention status code 403, got: %v", err)
	}
}

func TestJiraConnector_AuthHeader(t *testing.T) {
	mock := &mockHTTPClient{
		responses: []*http.Response{
			makeJiraSearchResponse(t, []jiraIssue{}, 0, 0),
		},
	}

	c := NewJiraConnector("https://example.atlassian.net", "user@example.com", "mytoken", "PROJ", "", 2, 50, jiraDefaultFields)
	c.client = mock

	_, _, err := c.Scan(context.Background(), ScanOptions{})
	if err != nil {
		t.Fatalf("Scan() error: %v", err)
	}

	if len(mock.requests) != 1 {
		t.Fatalf("expected 1 request, got %d", len(mock.requests))
	}

	req := mock.requests[0]
	user, pass, ok := req.BasicAuth()
	if !ok {
		t.Fatal("expected Basic auth header")
	}
	if user != "user@example.com" {
		t.Errorf("auth username = %q, want %q", user, "user@example.com")
	}
	if pass != "mytoken" {
		t.Errorf("auth password = %q, want %q", pass, "mytoken")
	}

	accept := req.Header.Get("Accept")
	if accept != "application/json" {
		t.Errorf("Accept header = %q, want %q", accept, "application/json")
	}
}

func TestJiraConnector_IncrementalSkipUnchanged(t *testing.T) {
	issue := makeJiraIssue("PROJ-1", "Unchanged", makeADFDoc("same content"), "Open", "",
		"2025-01-01T00:00:00.000+0000", "2025-01-01T00:00:00.000+0000", nil, nil)

	// Build the content the same way the connector would to get the matching checksum.
	c := NewJiraConnector("https://example.atlassian.net", "user@example.com", "token", "PROJ", "", 2, 50, jiraDefaultFields)
	content := c.buildIssueContent(issue)
	hash := sha256.Sum256([]byte(content))
	checksum := fmt.Sprintf("%x", hash)

	mock := &mockHTTPClient{
		responses: []*http.Response{
			makeJiraSearchResponse(t, []jiraIssue{issue}, 1, 0),
		},
	}
	c.client = mock

	known := map[string]string{
		"PROJ-1": checksum,
	}

	docs, _, err := c.Scan(context.Background(), ScanOptions{Known: known})
	if err != nil {
		t.Fatalf("Scan() error: %v", err)
	}
	if len(docs) != 0 {
		t.Errorf("expected 0 docs (unchanged), got %d", len(docs))
	}
}

func TestJiraConnector_BuildJQL(t *testing.T) {
	tests := []struct {
		name       string
		project    string
		jql        string
		lastIngest *time.Time
		wantParts  []string
	}{
		{
			name:      "project only",
			project:   "PROJ",
			wantParts: []string{`project = "PROJ"`, "ORDER BY updated DESC"},
		},
		{
			name:      "jql only",
			jql:       "sprint = 42",
			wantParts: []string{"(sprint = 42)", "ORDER BY updated DESC"},
		},
		{
			name:    "project and jql",
			project: "PROJ",
			jql:     "sprint = 42",
			wantParts: []string{`project = "PROJ"`, "(sprint = 42)", "ORDER BY updated DESC"},
		},
		{
			name:       "with lastIngest",
			project:    "PROJ",
			lastIngest: func() *time.Time { t := time.Date(2025, 3, 1, 12, 0, 0, 0, time.UTC); return &t }(),
			wantParts:  []string{`project = "PROJ"`, `updated > "2025-03-01 12:00"`, "ORDER BY updated DESC"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := &JiraConnector{project: tt.project, jql: tt.jql}
			got := c.buildJQL(tt.lastIngest)
			for _, part := range tt.wantParts {
				if !strings.Contains(got, part) {
					t.Errorf("buildJQL() = %q, missing part %q", got, part)
				}
			}
		})
	}
}

func TestJiraConnector_ContextCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())

	mock := &cancellingMockClient{
		inner: &mockHTTPClient{
			responses: []*http.Response{
				makeJiraSearchResponse(t, []jiraIssue{
					makeJiraIssue("PROJ-1", "Page", makeADFDoc("x"), "Open", "", "2025-01-01T00:00:00.000+0000", "2025-01-01T00:00:00.000+0000", nil, nil),
				}, 100, 0), // Total=100 to force pagination
			},
		},
		cancelAfter: 1,
		cancelFunc:  cancel,
	}

	c := NewJiraConnector("https://example.atlassian.net", "user@example.com", "token", "PROJ", "", 2, 50, jiraDefaultFields)
	c.client = mock

	_, _, err := c.Scan(ctx, ScanOptions{})
	if err == nil {
		t.Fatal("expected context cancellation error")
	}
	if err != context.Canceled {
		t.Errorf("expected context.Canceled, got: %v", err)
	}
}

func TestJiraConnector_EmptyProject(t *testing.T) {
	mock := &mockHTTPClient{
		responses: []*http.Response{
			makeJiraSearchResponse(t, []jiraIssue{}, 0, 0),
		},
	}

	c := NewJiraConnector("https://example.atlassian.net", "user@example.com", "token", "PROJ", "", 2, 50, jiraDefaultFields)
	c.client = mock

	docs, deleted, err := c.Scan(context.Background(), ScanOptions{})
	if err != nil {
		t.Fatalf("Scan() error: %v", err)
	}
	if len(docs) != 0 {
		t.Errorf("expected 0 docs, got %d", len(docs))
	}
	if len(deleted) != 0 {
		t.Errorf("expected 0 deletions, got %d", len(deleted))
	}
}

func TestJiraConnector_TimestampParsing(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		wantZero bool
	}{
		{"RFC3339", "2025-01-15T10:30:00Z", false},
		{"Jira Cloud +0000", "2025-01-15T10:30:00.000+0000", false},
		{"Timezone offset", "2025-01-15T10:30:00.000-0500", false},
		{"empty", "", true},
		{"malformed", "not-a-date", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := parseJiraTimestamp(tt.input)
			if tt.wantZero && !got.IsZero() {
				t.Errorf("expected zero time, got %v", got)
			}
			if !tt.wantZero && got.IsZero() {
				t.Errorf("expected non-zero time for %q", tt.input)
			}
		})
	}
}
