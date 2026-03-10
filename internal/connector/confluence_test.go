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
var _ Connector = (*ConfluenceConnector)(nil)

// mockHTTPClient implements HTTPClient for testing.
type mockHTTPClient struct {
	responses []*http.Response
	errors    []error
	requests  []*http.Request
	callIndex int
}

func (m *mockHTTPClient) Do(req *http.Request) (*http.Response, error) {
	m.requests = append(m.requests, req)
	idx := m.callIndex
	m.callIndex++
	if idx < len(m.errors) && m.errors[idx] != nil {
		return nil, m.errors[idx]
	}
	if idx < len(m.responses) {
		return m.responses[idx], nil
	}
	return nil, fmt.Errorf("no mock response for call %d", idx)
}

func makeConfluenceResponse(t *testing.T, pages []confluencePage, nextLink string) *http.Response {
	t.Helper()
	resp := confluenceResponse{
		Results: pages,
	}
	if nextLink != "" {
		resp.Links.Next = nextLink
	}
	body, err := json.Marshal(resp)
	if err != nil {
		t.Fatal(err)
	}
	return &http.Response{
		StatusCode: http.StatusOK,
		Body:       io.NopCloser(strings.NewReader(string(body))),
	}
}

func makePage(id, title, content, author, when string) confluencePage {
	p := confluencePage{
		ID:    id,
		Title: title,
	}
	p.Body.Storage.Value = content
	p.Version.By.DisplayName = author
	p.Version.When = when
	return p
}

func TestConfluenceConnector_Name(t *testing.T) {
	c := NewConfluenceConnector("https://example.atlassian.net", "ENG", "user", "token")
	if got := c.Name(); got != "confluence" {
		t.Errorf("Name() = %q, want %q", got, "confluence")
	}
}

func TestConfluenceConnector_SourceName(t *testing.T) {
	c := NewConfluenceConnector("https://example.atlassian.net", "ENG", "user", "token")
	if got := c.SourceName(); got != "ENG" {
		t.Errorf("SourceName() = %q, want %q", got, "ENG")
	}
}

func TestConfluenceConnector_Config(t *testing.T) {
	c := NewConfluenceConnector("https://example.atlassian.net", "ENG", "user", "token")

	t.Run("local mode includes api_token", func(t *testing.T) {
		cfg := c.Config(model.SourceModeLocal)
		if cfg["base_url"] != "https://example.atlassian.net" {
			t.Errorf("base_url = %q", cfg["base_url"])
		}
		if cfg["space_key"] != "ENG" {
			t.Errorf("space_key = %q", cfg["space_key"])
		}
		if cfg["username"] != "user" {
			t.Errorf("username = %q", cfg["username"])
		}
		if cfg["api_token"] != "token" {
			t.Errorf("api_token = %q", cfg["api_token"])
		}
	})

	t.Run("push mode omits api_token", func(t *testing.T) {
		cfg := c.Config(model.SourceModePush)
		if _, ok := cfg["api_token"]; ok {
			t.Error("push mode should not include api_token")
		}
	})
}

func TestConfluenceConnector_BasicScan(t *testing.T) {
	pages := []confluencePage{
		makePage("123", "Getting Started", "<p>Hello world</p>", "Alice", "2025-01-15T10:30:00Z"),
		makePage("456", "Architecture", "<p>System design</p>", "Bob", "2025-02-20T14:00:00Z"),
	}

	mock := &mockHTTPClient{
		responses: []*http.Response{
			makeConfluenceResponse(t, pages, ""),
		},
	}

	c := NewConfluenceConnector("https://example.atlassian.net", "ENG", "user", "token")
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
	if doc.Path != "123" {
		t.Errorf("Path = %q, want %q", doc.Path, "123")
	}
	if string(doc.Content) != "<p>Hello world</p>" {
		t.Errorf("Content = %q", string(doc.Content))
	}
	if doc.Author != "Alice" {
		t.Errorf("Author = %q", doc.Author)
	}
	if doc.SourceType != SourceTypeConfluence {
		t.Errorf("SourceType = %q", doc.SourceType)
	}
	if doc.SourceName != "ENG" {
		t.Errorf("SourceName = %q", doc.SourceName)
	}
	expectedURI := "https://example.atlassian.net/wiki/spaces/ENG/pages/123"
	if doc.SourceURI != expectedURI {
		t.Errorf("SourceURI = %q, want %q", doc.SourceURI, expectedURI)
	}

	expectedChecksum := fmt.Sprintf("%x", sha256.Sum256([]byte("<p>Hello world</p>")))
	if doc.Checksum != expectedChecksum {
		t.Errorf("Checksum = %q, want %q", doc.Checksum, expectedChecksum)
	}

	if doc.LastModified.IsZero() {
		t.Error("LastModified should not be zero")
	}
}

func TestConfluenceConnector_Pagination(t *testing.T) {
	page1 := []confluencePage{
		makePage("1", "Page One", "<p>one</p>", "Alice", "2025-01-01T00:00:00Z"),
	}
	page2 := []confluencePage{
		makePage("2", "Page Two", "<p>two</p>", "Bob", "2025-01-02T00:00:00Z"),
	}

	mock := &mockHTTPClient{
		responses: []*http.Response{
			makeConfluenceResponse(t, page1, "/wiki/rest/api/content?spaceKey=ENG&start=25&limit=25"),
			makeConfluenceResponse(t, page2, ""),
		},
	}

	c := NewConfluenceConnector("https://example.atlassian.net", "ENG", "user", "token")
	c.client = mock

	docs, _, err := c.Scan(context.Background(), ScanOptions{})
	if err != nil {
		t.Fatalf("Scan() error: %v", err)
	}
	if len(docs) != 2 {
		t.Fatalf("expected 2 docs across pages, got %d", len(docs))
	}
	if docs[0].Path != "1" || docs[1].Path != "2" {
		t.Errorf("unexpected page order: %q, %q", docs[0].Path, docs[1].Path)
	}

	// Should have made 2 HTTP requests.
	if len(mock.requests) != 2 {
		t.Errorf("expected 2 requests, got %d", len(mock.requests))
	}
}

func TestConfluenceConnector_IncrementalScan(t *testing.T) {
	content := "<p>unchanged</p>"
	checksum := fmt.Sprintf("%x", sha256.Sum256([]byte(content)))

	pages := []confluencePage{
		makePage("100", "Unchanged Page", content, "Alice", "2025-01-01T00:00:00Z"),
		makePage("200", "New Page", "<p>new content</p>", "Bob", "2025-01-02T00:00:00Z"),
	}

	mock := &mockHTTPClient{
		responses: []*http.Response{
			makeConfluenceResponse(t, pages, ""),
		},
	}

	c := NewConfluenceConnector("https://example.atlassian.net", "ENG", "user", "token")
	c.client = mock

	known := map[string]string{
		"100": checksum, // unchanged
	}

	docs, deleted, err := c.Scan(context.Background(), ScanOptions{Known: known})
	if err != nil {
		t.Fatalf("Scan() error: %v", err)
	}

	// Only the new page should be returned.
	if len(docs) != 1 {
		t.Fatalf("expected 1 doc (skipping unchanged), got %d", len(docs))
	}
	if docs[0].Path != "200" {
		t.Errorf("expected new page 200, got %q", docs[0].Path)
	}
	if len(deleted) != 0 {
		t.Errorf("expected no deletions, got %d", len(deleted))
	}
}

func TestConfluenceConnector_DeletionDetection(t *testing.T) {
	// API returns only page 1; page 2 was previously known.
	pages := []confluencePage{
		makePage("1", "Still Here", "<p>content</p>", "Alice", "2025-01-01T00:00:00Z"),
	}

	mock := &mockHTTPClient{
		responses: []*http.Response{
			makeConfluenceResponse(t, pages, ""),
		},
	}

	c := NewConfluenceConnector("https://example.atlassian.net", "ENG", "user", "token")
	c.client = mock

	known := map[string]string{
		"1": "old-checksum",
		"2": "some-checksum",
	}

	_, deleted, err := c.Scan(context.Background(), ScanOptions{Known: known})
	if err != nil {
		t.Fatalf("Scan() error: %v", err)
	}

	if len(deleted) != 1 {
		t.Fatalf("expected 1 deletion, got %d", len(deleted))
	}
	if deleted[0] != "2" {
		t.Errorf("expected deleted path %q, got %q", "2", deleted[0])
	}
}

func TestConfluenceConnector_EmptySpace(t *testing.T) {
	mock := &mockHTTPClient{
		responses: []*http.Response{
			makeConfluenceResponse(t, []confluencePage{}, ""),
		},
	}

	c := NewConfluenceConnector("https://example.atlassian.net", "ENG", "user", "token")
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

func TestConfluenceConnector_APIError(t *testing.T) {
	mock := &mockHTTPClient{
		responses: []*http.Response{
			{
				StatusCode: http.StatusForbidden,
				Body:       io.NopCloser(strings.NewReader(`{"message":"Forbidden"}`)),
			},
		},
	}

	c := NewConfluenceConnector("https://example.atlassian.net", "ENG", "user", "token")
	c.client = mock

	_, _, err := c.Scan(context.Background(), ScanOptions{})
	if err == nil {
		t.Fatal("expected error for non-200 response")
	}
	if !strings.Contains(err.Error(), "403") {
		t.Errorf("error should mention status code 403, got: %v", err)
	}
}

func TestConfluenceConnector_AuthHeader(t *testing.T) {
	mock := &mockHTTPClient{
		responses: []*http.Response{
			makeConfluenceResponse(t, []confluencePage{}, ""),
		},
	}

	c := NewConfluenceConnector("https://example.atlassian.net", "ENG", "myuser", "mytoken")
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
	if user != "myuser" {
		t.Errorf("auth username = %q, want %q", user, "myuser")
	}
	if pass != "mytoken" {
		t.Errorf("auth password = %q, want %q", pass, "mytoken")
	}

	accept := req.Header.Get("Accept")
	if accept != "application/json" {
		t.Errorf("Accept header = %q, want %q", accept, "application/json")
	}
}

func TestConfluenceConnector_TrailingSlashBaseURL(t *testing.T) {
	mock := &mockHTTPClient{
		responses: []*http.Response{
			makeConfluenceResponse(t, []confluencePage{}, ""),
		},
	}

	// Trailing slash should be trimmed.
	c := NewConfluenceConnector("https://example.atlassian.net/", "ENG", "user", "token")
	c.client = mock

	_, _, err := c.Scan(context.Background(), ScanOptions{})
	if err != nil {
		t.Fatalf("Scan() error: %v", err)
	}

	req := mock.requests[0]
	if strings.Contains(req.URL.String(), "//wiki") {
		t.Errorf("URL has double slash: %s", req.URL.String())
	}
}

func TestConfluenceConnector_HTTPTestServer(t *testing.T) {
	requestCount := 0
	var capturedRequests []*http.Request

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedRequests = append(capturedRequests, r)
		requestCount++

		w.Header().Set("Content-Type", "application/json")

		if requestCount == 1 {
			// First page: verify query params
			q := r.URL.Query()
			if q.Get("spaceKey") != "ENG" {
				t.Errorf("spaceKey = %q, want %q", q.Get("spaceKey"), "ENG")
			}
			if q.Get("expand") != "body.storage,version" {
				t.Errorf("expand = %q, want %q", q.Get("expand"), "body.storage,version")
			}

			resp := confluenceResponse{
				Results: []confluencePage{
					makePage("10", "First", "<p>first</p>", "Alice", "2025-01-15T10:30:00Z"),
				},
				Links: confluenceLinks{
					Next: "/wiki/rest/api/content?spaceKey=ENG&start=25&limit=25",
				},
			}
			json.NewEncoder(w).Encode(resp)
		} else {
			// Second page: no next link
			resp := confluenceResponse{
				Results: []confluencePage{
					makePage("20", "Second", "<p>second</p>", "Bob", "2025-02-01T00:00:00Z"),
				},
			}
			json.NewEncoder(w).Encode(resp)
		}
	}))
	defer ts.Close()

	c := NewConfluenceConnector(ts.URL, "ENG", "myuser", "mytoken")

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
	if docs[0].Path != "10" || docs[1].Path != "20" {
		t.Errorf("unexpected paths: %q, %q", docs[0].Path, docs[1].Path)
	}

	// Verify 2 HTTP requests were made.
	if requestCount != 2 {
		t.Errorf("expected 2 requests, got %d", requestCount)
	}

	// Verify first request URL path.
	if !strings.HasPrefix(capturedRequests[0].URL.Path, "/wiki/rest/api/content") {
		t.Errorf("unexpected path: %s", capturedRequests[0].URL.Path)
	}

	// Verify basic auth on first request.
	user, pass, ok := capturedRequests[0].BasicAuth()
	if !ok {
		t.Fatal("expected Basic auth")
	}
	if user != "myuser" || pass != "mytoken" {
		t.Errorf("auth = (%q, %q), want (%q, %q)", user, pass, "myuser", "mytoken")
	}
}

func TestConfluenceConnector_SpecialCharsSpaceKey(t *testing.T) {
	tests := []struct {
		name     string
		spaceKey string
		wantKey  string
	}{
		{"space in key", "MY SPACE", "MY+SPACE"},
		{"ampersand in key", "ENG&DEV", "ENG%26DEV"},
		{"plus in key", "A+B", "A%2BB"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var capturedURL string
			ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				capturedURL = r.URL.String()
				w.Header().Set("Content-Type", "application/json")
				json.NewEncoder(w).Encode(confluenceResponse{})
			}))
			defer ts.Close()

			c := NewConfluenceConnector(ts.URL, tt.spaceKey, "user", "token")
			_, _, err := c.Scan(context.Background(), ScanOptions{})
			if err != nil {
				t.Fatalf("Scan() error: %v", err)
			}

			if !strings.Contains(capturedURL, "spaceKey="+tt.wantKey) {
				t.Errorf("URL %q does not contain spaceKey=%s", capturedURL, tt.wantKey)
			}
		})
	}
}

func TestConfluenceConnector_ContextCancellation(t *testing.T) {
	// Set up a server that returns a next link on page 1, so pagination continues.
	requestCount := 0
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestCount++
		w.Header().Set("Content-Type", "application/json")
		resp := confluenceResponse{
			Results: []confluencePage{
				makePage(fmt.Sprintf("%d", requestCount), "Page", "<p>x</p>", "A", "2025-01-01T00:00:00Z"),
			},
			Links: confluenceLinks{
				Next: "/wiki/rest/api/content?spaceKey=ENG&start=25&limit=25",
			},
		}
		json.NewEncoder(w).Encode(resp)
	}))
	defer ts.Close()

	ctx, cancel := context.WithCancel(context.Background())

	// Use mock client so we can cancel between pages.
	mock := &mockHTTPClient{
		responses: []*http.Response{
			// Page 1 response.
			func() *http.Response {
				resp := confluenceResponse{
					Results: []confluencePage{
						makePage("1", "Page One", "<p>one</p>", "A", "2025-01-01T00:00:00Z"),
					},
					Links: confluenceLinks{
						Next: "/wiki/rest/api/content?spaceKey=ENG&start=25&limit=25",
					},
				}
				body, _ := json.Marshal(resp)
				return &http.Response{
					StatusCode: http.StatusOK,
					Body:       io.NopCloser(strings.NewReader(string(body))),
				}
			}(),
		},
	}

	c := NewConfluenceConnector("https://example.atlassian.net", "ENG", "user", "token")
	c.client = mock

	// Cancel after the first page fetch completes but before the second page.
	originalDo := mock.Do
	mock2 := &cancellingMockClient{
		inner:       mock,
		cancelAfter: 1,
		cancelFunc:  cancel,
	}
	_ = originalDo
	c.client = mock2

	_, _, err := c.Scan(ctx, ScanOptions{})
	if err == nil {
		t.Fatal("expected context cancellation error")
	}
	if err != context.Canceled {
		t.Errorf("expected context.Canceled, got: %v", err)
	}
}

// cancellingMockClient wraps a mock and cancels context after N calls.
type cancellingMockClient struct {
	inner       *mockHTTPClient
	cancelAfter int
	cancelFunc  context.CancelFunc
	calls       int
}

func (c *cancellingMockClient) Do(req *http.Request) (*http.Response, error) {
	c.calls++
	resp, err := c.inner.Do(req)
	if c.calls >= c.cancelAfter {
		c.cancelFunc()
	}
	return resp, err
}

func TestConfluenceConnector_TimestampFormats(t *testing.T) {
	tests := []struct {
		name     string
		when     string
		wantZero bool
		wantYear int
	}{
		{"RFC3339", "2025-01-15T10:30:00Z", false, 2025},
		{"Confluence Cloud +0000", "2025-01-15T10:30:00.000+0000", false, 2025},
		{"Timezone offset -0500", "2025-01-15T10:30:00.000-0500", false, 2025},
		{"empty string", "", true, 0},
		{"malformed", "not-a-date", true, 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := parseConfluenceTimestamp(tt.when)
			if tt.wantZero {
				if tt.when == "" {
					// Empty string: the Scan method doesn't call parseConfluenceTimestamp
					// for empty strings, so just verify the function returns zero.
					if !got.IsZero() {
						t.Errorf("expected zero time for %q, got %v", tt.when, got)
					}
					return
				}
				if !got.IsZero() {
					t.Errorf("expected zero time for %q, got %v", tt.when, got)
				}
				return
			}
			if got.IsZero() {
				t.Fatalf("expected non-zero time for %q", tt.when)
			}
			if got.Year() != tt.wantYear {
				t.Errorf("year = %d, want %d", got.Year(), tt.wantYear)
			}
		})
	}

	// Also verify via full Scan that timestamps are parsed into documents.
	t.Run("via Scan with Confluence Cloud timestamp", func(t *testing.T) {
		pages := []confluencePage{
			makePage("1", "Page", "<p>content</p>", "Alice", "2025-01-15T10:30:00.000+0000"),
		}
		mock := &mockHTTPClient{
			responses: []*http.Response{
				makeConfluenceResponse(t, pages, ""),
			},
		}
		c := NewConfluenceConnector("https://example.atlassian.net", "ENG", "user", "token")
		c.client = mock

		docs, _, err := c.Scan(context.Background(), ScanOptions{})
		if err != nil {
			t.Fatalf("Scan() error: %v", err)
		}
		if len(docs) != 1 {
			t.Fatalf("expected 1 doc, got %d", len(docs))
		}
		if docs[0].LastModified.IsZero() {
			t.Error("LastModified should not be zero for Confluence Cloud timestamp")
		}
		expected := time.Date(2025, 1, 15, 10, 30, 0, 0, time.UTC)
		if !docs[0].LastModified.Equal(expected) {
			t.Errorf("LastModified = %v, want %v", docs[0].LastModified, expected)
		}
	})
}

func TestConfluenceConnector_EmptyPageContent(t *testing.T) {
	pages := []confluencePage{
		makePage("1", "Empty Page", "", "Alice", "2025-01-01T00:00:00Z"),
	}
	mock := &mockHTTPClient{
		responses: []*http.Response{
			makeConfluenceResponse(t, pages, ""),
		},
	}

	c := NewConfluenceConnector("https://example.atlassian.net", "ENG", "user", "token")
	c.client = mock

	docs, _, err := c.Scan(context.Background(), ScanOptions{})
	if err != nil {
		t.Fatalf("Scan() error: %v", err)
	}
	if len(docs) != 1 {
		t.Fatalf("expected 1 doc, got %d", len(docs))
	}
	if string(docs[0].Content) != "" {
		t.Errorf("Content = %q, want empty", string(docs[0].Content))
	}
	// Checksum should still be computed (of empty string).
	expectedChecksum := fmt.Sprintf("%x", sha256.Sum256([]byte("")))
	if docs[0].Checksum != expectedChecksum {
		t.Errorf("Checksum = %q, want %q", docs[0].Checksum, expectedChecksum)
	}
}

func TestConfluenceConnector_MalformedJSON(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{not valid json`))
	}))
	defer ts.Close()

	c := NewConfluenceConnector(ts.URL, "ENG", "user", "token")

	_, _, err := c.Scan(context.Background(), ScanOptions{})
	if err == nil {
		t.Fatal("expected error for malformed JSON")
	}
	if !strings.Contains(err.Error(), "parse response") {
		t.Errorf("error should mention parse, got: %v", err)
	}
}

func TestConfluenceConnector_NetworkError(t *testing.T) {
	mock := &mockHTTPClient{
		errors: []error{fmt.Errorf("connection refused")},
	}

	c := NewConfluenceConnector("https://example.atlassian.net", "ENG", "user", "token")
	c.client = mock

	_, _, err := c.Scan(context.Background(), ScanOptions{})
	if err == nil {
		t.Fatal("expected error for network failure")
	}
	if !strings.Contains(err.Error(), "confluence API request") {
		t.Errorf("error should mention API request, got: %v", err)
	}
	if !strings.Contains(err.Error(), "connection refused") {
		t.Errorf("error should contain underlying cause, got: %v", err)
	}
}
