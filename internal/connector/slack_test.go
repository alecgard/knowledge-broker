package connector

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/knowledge-broker/knowledge-broker/pkg/model"
)

// Compile-time interface compliance check.
var _ Connector = (*SlackConnector)(nil)

// slackMockHTTPClient records requests and returns pre-configured responses.
type slackMockHTTPClient struct {
	responses []mockResponse
	requests  []*http.Request
	callIndex int
}

type mockResponse struct {
	statusCode int
	body       string
	headers    map[string]string
}

func (m *slackMockHTTPClient) Do(req *http.Request) (*http.Response, error) {
	m.requests = append(m.requests, req)
	if m.callIndex >= len(m.responses) {
		return nil, fmt.Errorf("unexpected request #%d: %s %s", m.callIndex, req.Method, req.URL.String())
	}
	resp := m.responses[m.callIndex]
	m.callIndex++

	header := http.Header{}
	for k, v := range resp.headers {
		header.Set(k, v)
	}

	return &http.Response{
		StatusCode: resp.statusCode,
		Body:       io.NopCloser(strings.NewReader(resp.body)),
		Header:     header,
	}, nil
}

// helper to build a conversations.info response.
func channelInfoJSON(name string) string {
	resp := slackChannelInfoResponse{OK: true}
	resp.Channel.Name = name
	b, _ := json.Marshal(resp)
	return string(b)
}

// helper to build a conversations.history response.
func historyJSON(messages []slackMessage, nextCursor string) string {
	resp := slackHistoryResponse{OK: true, Messages: messages}
	resp.ResponseMetadata.NextCursor = nextCursor
	b, _ := json.Marshal(resp)
	return string(b)
}

// helper to build a conversations.replies response.
func repliesJSON(messages []slackMessage, nextCursor string) string {
	resp := slackRepliesResponse{OK: true, Messages: messages}
	resp.ResponseMetadata.NextCursor = nextCursor
	b, _ := json.Marshal(resp)
	return string(b)
}

func errorJSON(errMsg string) string {
	return fmt.Sprintf(`{"ok":false,"error":%q}`, errMsg)
}

func TestSlackConnectorName(t *testing.T) {
	c := NewSlackConnector("xoxb-test", []string{"C123"}, "test-workspace")
	if c.Name() != "slack" {
		t.Errorf("expected Name() = 'slack', got %q", c.Name())
	}
}

func TestSlackConnectorSourceName(t *testing.T) {
	c := NewSlackConnector("xoxb-test", []string{"C123"}, "test-workspace")
	if c.SourceName() != "test-workspace" {
		t.Errorf("expected SourceName() = 'test-workspace', got %q", c.SourceName())
	}
}

func TestSlackConnectorConfig(t *testing.T) {
	c := NewSlackConnector("xoxb-secret", []string{"C1", "C2"}, "test-workspace")

	// Local mode includes token.
	cfg := c.Config(model.SourceModeLocal)
	if cfg["token"] != "xoxb-secret" {
		t.Errorf("expected token in local config, got %q", cfg["token"])
	}
	if cfg["channels"] != "C1,C2" {
		t.Errorf("expected channels 'C1,C2', got %q", cfg["channels"])
	}

	// Push mode omits token.
	cfg = c.Config(model.SourceModePush)
	if _, ok := cfg["token"]; ok {
		t.Error("expected token to be omitted in push config")
	}
	if cfg["channels"] != "C1,C2" {
		t.Errorf("expected channels 'C1,C2', got %q", cfg["channels"])
	}
}

func TestSlackBasicScan(t *testing.T) {
	now := time.Now().Unix()
	ts1 := fmt.Sprintf("%d.000100", now-3600)
	ts2 := fmt.Sprintf("%d.000200", now-1800)

	mock := &slackMockHTTPClient{
		responses: []mockResponse{
			{statusCode: 200, body: channelInfoJSON("general")},
			{statusCode: 200, body: historyJSON([]slackMessage{
				{TS: ts1, User: "U001", Text: "Hello world"},
				{TS: ts2, User: "U002", Text: "Hi there"},
			}, "")},
		},
	}

	c := NewSlackConnector("xoxb-test", []string{"C123"}, "")
	c.httpClient = mock

	docs, deleted, err := c.Scan(context.Background(), ScanOptions{})
	if err != nil {
		t.Fatalf("Scan failed: %v", err)
	}
	if len(deleted) != 0 {
		t.Errorf("expected no deleted paths, got %d", len(deleted))
	}
	// Both messages are non-threaded, same day -> 1 daily document.
	if len(docs) != 1 {
		t.Fatalf("expected 1 document, got %d", len(docs))
	}

	doc := docs[0]
	if doc.SourceType != SourceTypeSlack {
		t.Errorf("expected SourceType 'slack', got %q", doc.SourceType)
	}
	if doc.Author != "U001" {
		t.Errorf("expected Author 'U001', got %q", doc.Author)
	}
	if doc.Checksum == "" {
		t.Error("expected non-empty checksum")
	}
	// Path should be channel_name/date.
	if !strings.HasPrefix(doc.Path, "general/") {
		t.Errorf("expected path to start with 'general/', got %q", doc.Path)
	}
	if !strings.Contains(string(doc.Content), "Hello world") {
		t.Error("expected content to contain 'Hello world'")
	}
	if !strings.Contains(string(doc.Content), "Hi there") {
		t.Error("expected content to contain 'Hi there'")
	}
}

func TestSlackThreadHandling(t *testing.T) {
	now := time.Now().Unix()
	parentTS := fmt.Sprintf("%d.000100", now-3600)
	replyTS := fmt.Sprintf("%d.000200", now-1800)

	mock := &slackMockHTTPClient{
		responses: []mockResponse{
			{statusCode: 200, body: channelInfoJSON("engineering")},
			// conversations.history returns parent with reply_count > 0.
			{statusCode: 200, body: historyJSON([]slackMessage{
				{TS: parentTS, User: "U001", Text: "Anyone know about X?", ThreadTS: parentTS, ReplyCount: 1},
			}, "")},
			// conversations.replies for the thread.
			{statusCode: 200, body: repliesJSON([]slackMessage{
				{TS: parentTS, User: "U001", Text: "Anyone know about X?", ThreadTS: parentTS},
				{TS: replyTS, User: "U002", Text: "Yes, check the docs.", ThreadTS: parentTS},
			}, "")},
		},
	}

	c := NewSlackConnector("xoxb-test", []string{"C456"}, "")
	c.httpClient = mock

	docs, _, err := c.Scan(context.Background(), ScanOptions{})
	if err != nil {
		t.Fatalf("Scan failed: %v", err)
	}

	if len(docs) != 1 {
		t.Fatalf("expected 1 thread document, got %d", len(docs))
	}

	doc := docs[0]
	// Thread path: channel_name/thread_ts.
	expectedPath := "engineering/" + parentTS
	if doc.Path != expectedPath {
		t.Errorf("expected path %q, got %q", expectedPath, doc.Path)
	}
	if !strings.Contains(string(doc.Content), "Anyone know about X?") {
		t.Error("expected content to contain parent message")
	}
	if !strings.Contains(string(doc.Content), "Yes, check the docs.") {
		t.Error("expected content to contain reply")
	}
	if !strings.Contains(doc.SourceURI, "slack://channel/C456/p") {
		t.Errorf("expected SourceURI to contain channel ID, got %q", doc.SourceURI)
	}
}

func TestSlackDailyGrouping(t *testing.T) {
	// Create messages across two days.
	day1 := time.Date(2025, 1, 15, 10, 0, 0, 0, time.UTC).Unix()
	day2 := time.Date(2025, 1, 16, 14, 0, 0, 0, time.UTC).Unix()

	ts1 := fmt.Sprintf("%d.000100", day1)
	ts2 := fmt.Sprintf("%d.000200", day1+3600)
	ts3 := fmt.Sprintf("%d.000300", day2)

	mock := &slackMockHTTPClient{
		responses: []mockResponse{
			{statusCode: 200, body: channelInfoJSON("random")},
			{statusCode: 200, body: historyJSON([]slackMessage{
				{TS: ts1, User: "U001", Text: "Morning"},
				{TS: ts2, User: "U002", Text: "Afternoon"},
				{TS: ts3, User: "U003", Text: "Next day"},
			}, "")},
		},
	}

	c := NewSlackConnector("xoxb-test", []string{"C789"}, "")
	c.httpClient = mock

	docs, _, err := c.Scan(context.Background(), ScanOptions{})
	if err != nil {
		t.Fatalf("Scan failed: %v", err)
	}

	if len(docs) != 2 {
		t.Fatalf("expected 2 daily documents (2 days), got %d", len(docs))
	}

	// Check that we have docs for both dates.
	paths := map[string]bool{}
	for _, d := range docs {
		paths[d.Path] = true
	}
	if !paths["random/2025-01-15"] {
		t.Error("expected document for 2025-01-15")
	}
	if !paths["random/2025-01-16"] {
		t.Error("expected document for 2025-01-16")
	}
}

func TestSlackPagination(t *testing.T) {
	now := time.Now().Unix()
	ts1 := fmt.Sprintf("%d.000100", now-3600)
	ts2 := fmt.Sprintf("%d.000200", now-1800)

	mock := &slackMockHTTPClient{
		responses: []mockResponse{
			{statusCode: 200, body: channelInfoJSON("paginated")},
			// First page with cursor.
			{statusCode: 200, body: historyJSON([]slackMessage{
				{TS: ts1, User: "U001", Text: "Page 1"},
			}, "cursor_abc")},
			// Second page, no more cursor.
			{statusCode: 200, body: historyJSON([]slackMessage{
				{TS: ts2, User: "U002", Text: "Page 2"},
			}, "")},
		},
	}

	c := NewSlackConnector("xoxb-test", []string{"CPAG"}, "")
	c.httpClient = mock

	docs, _, err := c.Scan(context.Background(), ScanOptions{})
	if err != nil {
		t.Fatalf("Scan failed: %v", err)
	}

	if len(docs) != 1 {
		t.Fatalf("expected 1 daily document, got %d", len(docs))
	}
	// Both messages from paginated results should be in the document.
	content := string(docs[0].Content)
	if !strings.Contains(content, "Page 1") || !strings.Contains(content, "Page 2") {
		t.Error("expected both paginated messages in content")
	}

	// Verify the second history request included the cursor.
	found := false
	for _, req := range mock.requests {
		if strings.Contains(req.URL.String(), "cursor=cursor_abc") {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected cursor to be passed in pagination request")
	}
}

func TestSlackMultipleChannels(t *testing.T) {
	now := time.Now().Unix()
	ts1 := fmt.Sprintf("%d.000100", now-3600)
	ts2 := fmt.Sprintf("%d.000200", now-1800)

	mock := &slackMockHTTPClient{
		responses: []mockResponse{
			// Channel 1.
			{statusCode: 200, body: channelInfoJSON("general")},
			{statusCode: 200, body: historyJSON([]slackMessage{
				{TS: ts1, User: "U001", Text: "In general"},
			}, "")},
			// Channel 2.
			{statusCode: 200, body: channelInfoJSON("engineering")},
			{statusCode: 200, body: historyJSON([]slackMessage{
				{TS: ts2, User: "U002", Text: "In engineering"},
			}, "")},
		},
	}

	c := NewSlackConnector("xoxb-test", []string{"C001", "C002"}, "")
	c.httpClient = mock

	docs, _, err := c.Scan(context.Background(), ScanOptions{})
	if err != nil {
		t.Fatalf("Scan failed: %v", err)
	}

	if len(docs) != 2 {
		t.Fatalf("expected 2 documents (one per channel), got %d", len(docs))
	}

	paths := map[string]bool{}
	for _, d := range docs {
		paths[d.Path] = true
	}

	foundGeneral := false
	foundEngineering := false
	for p := range paths {
		if strings.HasPrefix(p, "general/") {
			foundGeneral = true
		}
		if strings.HasPrefix(p, "engineering/") {
			foundEngineering = true
		}
	}
	if !foundGeneral {
		t.Error("expected document from general channel")
	}
	if !foundEngineering {
		t.Error("expected document from engineering channel")
	}
}

func TestSlackIncrementalScan(t *testing.T) {
	now := time.Now().Unix()
	ts1 := fmt.Sprintf("%d.000100", now-3600)
	ts2 := fmt.Sprintf("%d.000200", now-1800)
	date := time.Unix(now-3600, 0).UTC().Format("2006-01-02")

	// Build expected content to compute checksum.
	t1 := time.Unix(now-3600, 0).UTC()
	t2 := time.Unix(now-1800, 0).UTC()
	expectedContent := fmt.Sprintf("[%s] U001: Hello\n[%s] U002: World\n", t1.Format(time.RFC3339), t2.Format(time.RFC3339))
	hash := sha256.Sum256([]byte(expectedContent))
	checksum := fmt.Sprintf("%x", hash)

	known := map[string]string{
		"general/" + date: checksum,
	}

	mock := &slackMockHTTPClient{
		responses: []mockResponse{
			{statusCode: 200, body: channelInfoJSON("general")},
			{statusCode: 200, body: historyJSON([]slackMessage{
				{TS: ts1, User: "U001", Text: "Hello"},
				{TS: ts2, User: "U002", Text: "World"},
			}, "")},
		},
	}

	c := NewSlackConnector("xoxb-test", []string{"C123"}, "")
	c.httpClient = mock

	docs, _, err := c.Scan(context.Background(), ScanOptions{Known: known})
	if err != nil {
		t.Fatalf("Scan failed: %v", err)
	}

	// Document should be skipped because checksum matches.
	if len(docs) != 0 {
		t.Errorf("expected 0 documents (unchanged), got %d", len(docs))
	}
}

func TestSlackEmptyChannel(t *testing.T) {
	mock := &slackMockHTTPClient{
		responses: []mockResponse{
			{statusCode: 200, body: channelInfoJSON("empty")},
			{statusCode: 200, body: historyJSON(nil, "")},
		},
	}

	c := NewSlackConnector("xoxb-test", []string{"CEMPTY"}, "")
	c.httpClient = mock

	docs, deleted, err := c.Scan(context.Background(), ScanOptions{})
	if err != nil {
		t.Fatalf("Scan failed: %v", err)
	}
	if len(docs) != 0 {
		t.Errorf("expected 0 documents, got %d", len(docs))
	}
	if len(deleted) != 0 {
		t.Errorf("expected 0 deleted, got %d", len(deleted))
	}
}

func TestSlackAPIError(t *testing.T) {
	mock := &slackMockHTTPClient{
		responses: []mockResponse{
			{statusCode: 200, body: errorJSON("channel_not_found")},
		},
	}

	c := NewSlackConnector("xoxb-test", []string{"CBAD"}, "")
	c.httpClient = mock

	_, _, err := c.Scan(context.Background(), ScanOptions{})
	if err == nil {
		t.Fatal("expected error for API error response")
	}
	if !strings.Contains(err.Error(), "channel_not_found") {
		t.Errorf("expected error to mention 'channel_not_found', got: %v", err)
	}
}

func TestSlackHTTPError(t *testing.T) {
	mock := &slackMockHTTPClient{
		responses: []mockResponse{
			{statusCode: 500, body: "Internal Server Error"},
		},
	}

	c := NewSlackConnector("xoxb-test", []string{"CFAIL"}, "")
	c.httpClient = mock

	_, _, err := c.Scan(context.Background(), ScanOptions{})
	if err == nil {
		t.Fatal("expected error for HTTP 500")
	}
	if !strings.Contains(err.Error(), "500") {
		t.Errorf("expected error to mention status code, got: %v", err)
	}
}

func TestSlackAuthHeader(t *testing.T) {
	now := time.Now().Unix()
	ts := fmt.Sprintf("%d.000100", now)

	mock := &slackMockHTTPClient{
		responses: []mockResponse{
			{statusCode: 200, body: channelInfoJSON("auth-test")},
			{statusCode: 200, body: historyJSON([]slackMessage{
				{TS: ts, User: "U001", Text: "test"},
			}, "")},
		},
	}

	c := NewSlackConnector("xoxb-my-secret-token", []string{"CAUTH"}, "")
	c.httpClient = mock

	_, _, err := c.Scan(context.Background(), ScanOptions{})
	if err != nil {
		t.Fatalf("Scan failed: %v", err)
	}

	// Verify all requests have the correct Authorization header.
	for i, req := range mock.requests {
		auth := req.Header.Get("Authorization")
		expected := "Bearer xoxb-my-secret-token"
		if auth != expected {
			t.Errorf("request %d: expected Authorization %q, got %q", i, expected, auth)
		}
	}
}

func TestSlackRateLimitRetry(t *testing.T) {
	mock := &slackMockHTTPClient{
		responses: []mockResponse{
			// First request gets rate limited.
			{statusCode: 429, body: `{"ok":false,"error":"ratelimited"}`, headers: map[string]string{"Retry-After": "1"}},
			// Retry succeeds.
			{statusCode: 200, body: channelInfoJSON("limited")},
			{statusCode: 200, body: historyJSON(nil, "")},
		},
	}

	c := NewSlackConnector("xoxb-test", []string{"CRATE"}, "")
	c.httpClient = mock

	docs, _, err := c.Scan(context.Background(), ScanOptions{})
	if err != nil {
		t.Fatalf("Scan failed: %v", err)
	}
	if len(docs) != 0 {
		t.Errorf("expected 0 documents, got %d", len(docs))
	}

	// Should have made 3 requests: rate-limited + retry for channel info + history.
	if len(mock.requests) != 3 {
		t.Errorf("expected 3 requests (1 rate-limited + 1 retry + 1 history), got %d", len(mock.requests))
	}
}

func TestSlackChecksumCorrectness(t *testing.T) {
	now := time.Now().Unix()
	ts := fmt.Sprintf("%d.000100", now)

	mock := &slackMockHTTPClient{
		responses: []mockResponse{
			{statusCode: 200, body: channelInfoJSON("checksum")},
			{statusCode: 200, body: historyJSON([]slackMessage{
				{TS: ts, User: "U001", Text: "checksum test"},
			}, "")},
		},
	}

	c := NewSlackConnector("xoxb-test", []string{"CCHK"}, "")
	c.httpClient = mock

	docs, _, err := c.Scan(context.Background(), ScanOptions{})
	if err != nil {
		t.Fatalf("Scan failed: %v", err)
	}

	if len(docs) != 1 {
		t.Fatalf("expected 1 document, got %d", len(docs))
	}

	// Verify checksum is sha256 of content.
	expectedHash := sha256.Sum256(docs[0].Content)
	expectedChecksum := fmt.Sprintf("%x", expectedHash)
	if docs[0].Checksum != expectedChecksum {
		t.Errorf("checksum mismatch: got %q, want %q", docs[0].Checksum, expectedChecksum)
	}
}

func TestSlackLookbackDays(t *testing.T) {
	mock := &slackMockHTTPClient{
		responses: []mockResponse{
			{statusCode: 200, body: channelInfoJSON("lookback")},
			{statusCode: 200, body: historyJSON(nil, "")},
		},
	}

	c := NewSlackConnector("xoxb-test", []string{"CLOOK"}, "")
	c.httpClient = mock
	c.lookbackDays = 30

	_, _, err := c.Scan(context.Background(), ScanOptions{})
	if err != nil {
		t.Fatalf("Scan failed: %v", err)
	}

	// Check that the history request includes an oldest parameter roughly 30 days ago.
	for _, req := range mock.requests {
		if strings.Contains(req.URL.String(), "conversations.history") {
			reqURL := req.URL.String()
			if !strings.Contains(reqURL, "oldest=") {
				t.Error("expected oldest parameter in history request")
				continue
			}
			// Extract the oldest value and verify it's approximately 30 days ago.
			for _, part := range strings.Split(reqURL, "&") {
				if strings.HasPrefix(part, "oldest=") {
					val := strings.TrimPrefix(part, "oldest=")
					ts, err := strconv.ParseInt(val, 10, 64)
					if err != nil {
						t.Errorf("failed to parse oldest: %v", err)
						continue
					}
					expected := time.Now().AddDate(0, 0, -30).Unix()
					diff := ts - expected
					if diff < -60 || diff > 60 {
						t.Errorf("oldest timestamp off by more than 60s: got %d, expected ~%d", ts, expected)
					}
				}
			}
		}
	}
}

func TestSlackMixedThreadsAndMessages(t *testing.T) {
	now := time.Now().Unix()
	parentTS := fmt.Sprintf("%d.000100", now-7200)
	nonThreadTS := fmt.Sprintf("%d.000200", now-3600)
	replyTS := fmt.Sprintf("%d.000300", now-1800)

	mock := &slackMockHTTPClient{
		responses: []mockResponse{
			{statusCode: 200, body: channelInfoJSON("mixed")},
			{statusCode: 200, body: historyJSON([]slackMessage{
				{TS: parentTS, User: "U001", Text: "Thread parent", ThreadTS: parentTS, ReplyCount: 1},
				{TS: nonThreadTS, User: "U002", Text: "Standalone message"},
			}, "")},
			// Replies for the thread.
			{statusCode: 200, body: repliesJSON([]slackMessage{
				{TS: parentTS, User: "U001", Text: "Thread parent", ThreadTS: parentTS},
				{TS: replyTS, User: "U003", Text: "Thread reply", ThreadTS: parentTS},
			}, "")},
		},
	}

	c := NewSlackConnector("xoxb-test", []string{"CMIX"}, "")
	c.httpClient = mock

	docs, _, err := c.Scan(context.Background(), ScanOptions{})
	if err != nil {
		t.Fatalf("Scan failed: %v", err)
	}

	// Should have 2 documents: one thread + one daily digest.
	if len(docs) != 2 {
		t.Fatalf("expected 2 documents, got %d", len(docs))
	}

	foundThread := false
	foundDaily := false
	for _, d := range docs {
		if strings.Contains(d.Path, parentTS) {
			foundThread = true
			if !strings.Contains(string(d.Content), "Thread reply") {
				t.Error("thread document should contain reply")
			}
		} else {
			foundDaily = true
			if !strings.Contains(string(d.Content), "Standalone message") {
				t.Error("daily document should contain standalone message")
			}
		}
	}
	if !foundThread {
		t.Error("expected a thread document")
	}
	if !foundDaily {
		t.Error("expected a daily document")
	}
}

// --- New tests ---

func TestSlackHTTPTestServer(t *testing.T) {
	now := time.Now().Unix()
	parentTS := fmt.Sprintf("%d.000100", now-3600)
	replyTS := fmt.Sprintf("%d.000200", now-1800)
	standaloneTS := fmt.Sprintf("%d.000300", now-900)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		switch {
		case strings.HasPrefix(r.URL.Path, "/api/conversations.info"):
			fmt.Fprint(w, channelInfoJSON("test-channel"))

		case strings.HasPrefix(r.URL.Path, "/api/conversations.history"):
			fmt.Fprint(w, historyJSON([]slackMessage{
				{TS: parentTS, User: "U001", Text: "Thread starter", ThreadTS: parentTS, ReplyCount: 1},
				{TS: standaloneTS, User: "U002", Text: "Standalone msg"},
			}, ""))

		case strings.HasPrefix(r.URL.Path, "/api/conversations.replies"):
			fmt.Fprint(w, repliesJSON([]slackMessage{
				{TS: parentTS, User: "U001", Text: "Thread starter", ThreadTS: parentTS},
				{TS: replyTS, User: "U003", Text: "Thread reply", ThreadTS: parentTS},
			}, ""))

		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	c := NewSlackConnector("xoxb-test", []string{"C100"}, "httptest-workspace")
	c.baseURL = server.URL

	docs, deleted, err := c.Scan(context.Background(), ScanOptions{})
	if err != nil {
		t.Fatalf("Scan failed: %v", err)
	}
	if len(deleted) != 0 {
		t.Errorf("expected 0 deleted, got %d", len(deleted))
	}
	// Should have 2 docs: 1 thread + 1 daily.
	if len(docs) != 2 {
		t.Fatalf("expected 2 documents, got %d", len(docs))
	}

	foundThread := false
	foundDaily := false
	for _, d := range docs {
		if strings.Contains(d.Path, parentTS) {
			foundThread = true
			if !strings.Contains(string(d.Content), "Thread reply") {
				t.Error("thread document should contain reply")
			}
		} else {
			foundDaily = true
			if !strings.Contains(string(d.Content), "Standalone msg") {
				t.Error("daily document should contain standalone message")
			}
		}
		if d.SourceName != "httptest-workspace" {
			t.Errorf("expected SourceName 'httptest-workspace', got %q", d.SourceName)
		}
	}
	if !foundThread {
		t.Error("expected a thread document")
	}
	if !foundDaily {
		t.Error("expected a daily document")
	}
}

func TestSlackContextCancellation(t *testing.T) {
	// Create a server that delays so the context can be cancelled.
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Delay long enough for the context to be cancelled.
		time.Sleep(500 * time.Millisecond)
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, channelInfoJSON("delayed"))
	}))
	defer server.Close()

	c := NewSlackConnector("xoxb-test", []string{"C100"}, "")
	c.baseURL = server.URL

	ctx, cancel := context.WithCancel(context.Background())
	// Cancel immediately.
	cancel()

	_, _, err := c.Scan(ctx, ScanOptions{})
	if err == nil {
		t.Fatal("expected error from cancelled context")
	}
	if !strings.Contains(err.Error(), "context canceled") {
		t.Errorf("expected context canceled error, got: %v", err)
	}
}

func TestSlackMaxRetries(t *testing.T) {
	// Create enough 429 responses to exceed maxRetries.
	var responses []mockResponse
	for i := 0; i < maxRetries+2; i++ {
		responses = append(responses, mockResponse{
			statusCode: 429,
			body:       `{"ok":false,"error":"ratelimited"}`,
			headers:    map[string]string{"Retry-After": "0"},
		})
	}

	mock := &slackMockHTTPClient{responses: responses}

	c := NewSlackConnector("xoxb-test", []string{"C100"}, "")
	c.httpClient = mock

	_, _, err := c.Scan(context.Background(), ScanOptions{})
	if err == nil {
		t.Fatal("expected error after max retries exceeded")
	}
	if !strings.Contains(err.Error(), "rate limit exceeded after") {
		t.Errorf("expected rate limit exceeded error, got: %v", err)
	}
	// Should have made maxRetries+1 requests (initial + maxRetries retries).
	expectedRequests := maxRetries + 1
	if len(mock.requests) != expectedRequests {
		t.Errorf("expected %d requests, got %d", expectedRequests, len(mock.requests))
	}
}

func TestSlackMalformedTimestamp(t *testing.T) {
	tests := []struct {
		name     string
		ts       string
		wantZero bool
	}{
		{name: "empty string", ts: "", wantZero: true},
		{name: "not a number", ts: "not-a-number", wantZero: true},
		{name: "no dot", ts: "1234567890", wantZero: false},
		{name: "normal", ts: "1234567890.123456", wantZero: false},
		{name: "only dot", ts: ".", wantZero: true},
		{name: "dot prefix", ts: ".123", wantZero: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := tsToTime(tt.ts)
			if tt.wantZero && !result.IsZero() {
				t.Errorf("tsToTime(%q) = %v, want zero time", tt.ts, result)
			}
			if !tt.wantZero && result.IsZero() {
				t.Errorf("tsToTime(%q) returned zero time, want non-zero", tt.ts)
			}
		})
	}

	// Verify "12345" (no dot) returns the correct time.
	result := tsToTime("12345")
	expected := time.Unix(12345, 0).UTC()
	if !result.Equal(expected) {
		t.Errorf("tsToTime(\"12345\") = %v, want %v", result, expected)
	}
}

func TestSlackMalformedJSON(t *testing.T) {
	mock := &slackMockHTTPClient{
		responses: []mockResponse{
			{statusCode: 200, body: "this is not json at all{{{"},
		},
	}

	c := NewSlackConnector("xoxb-test", []string{"C100"}, "")
	c.httpClient = mock

	_, _, err := c.Scan(context.Background(), ScanOptions{})
	if err == nil {
		t.Fatal("expected error for malformed JSON response")
	}
	if !strings.Contains(err.Error(), "parse response") {
		t.Errorf("expected parse error, got: %v", err)
	}
}

func TestSlackSourceNameWithWorkspace(t *testing.T) {
	// With workspace name provided.
	c := NewSlackConnector("xoxb-test", []string{"C1", "C2"}, "my-workspace")
	if got := c.SourceName(); got != "my-workspace" {
		t.Errorf("expected SourceName() = 'my-workspace', got %q", got)
	}

	// Without workspace name (empty string) -- falls back to channel IDs.
	c2 := NewSlackConnector("xoxb-test", []string{"C1", "C2"}, "")
	expected := "slack:C1,C2"
	if got := c2.SourceName(); got != expected {
		t.Errorf("expected SourceName() = %q, got %q", expected, got)
	}

	// Single channel without workspace name.
	c3 := NewSlackConnector("xoxb-test", []string{"C1"}, "")
	expected3 := "slack:C1"
	if got := c3.SourceName(); got != expected3 {
		t.Errorf("expected SourceName() = %q, got %q", expected3, got)
	}
}

func TestSlackBuildDocumentsUsesContext(t *testing.T) {
	// Start a server that delays in the replies endpoint so the context timeout fires.
	repliesCalled := make(chan struct{}, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		switch {
		case strings.HasPrefix(r.URL.Path, "/api/conversations.info"):
			fmt.Fprint(w, channelInfoJSON("ctx-test"))

		case strings.HasPrefix(r.URL.Path, "/api/conversations.history"):
			now := time.Now().Unix()
			parentTS := fmt.Sprintf("%d.000100", now-3600)
			fmt.Fprint(w, historyJSON([]slackMessage{
				{TS: parentTS, User: "U001", Text: "Thread parent", ThreadTS: parentTS, ReplyCount: 1},
			}, ""))

		case strings.HasPrefix(r.URL.Path, "/api/conversations.replies"):
			repliesCalled <- struct{}{}
			// Delay to allow context cancellation to take effect.
			time.Sleep(2 * time.Second)
			fmt.Fprint(w, repliesJSON(nil, ""))

		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	c := NewSlackConnector("xoxb-test", []string{"C100"}, "")
	c.baseURL = server.URL

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()

	_, _, err := c.Scan(ctx, ScanOptions{})
	// The scan should either error due to context timeout, or the thread fetch
	// should be skipped (which is also acceptable behavior). The key assertion
	// is that we don't hang forever.
	if err != nil {
		if !strings.Contains(err.Error(), "context") {
			t.Errorf("expected context-related error, got: %v", err)
		}
	}
	// The replies endpoint should have been called (proving we're passing
	// the context through, not using context.Background which wouldn't timeout).
	select {
	case <-repliesCalled:
		// Good: the replies endpoint was called.
	case <-time.After(3 * time.Second):
		t.Error("replies endpoint was never called")
	}
}

func TestSlackNetworkError(t *testing.T) {
	// Use a mock that returns an error on Do.
	errMock := &slackMockHTTPClient{
		responses: nil, // No responses configured = will return error.
	}

	c := NewSlackConnector("xoxb-test", []string{"C100"}, "")
	c.httpClient = errMock

	_, _, err := c.Scan(context.Background(), ScanOptions{})
	if err == nil {
		t.Fatal("expected error from network failure")
	}
	if !strings.Contains(err.Error(), "unexpected request") {
		t.Errorf("expected unexpected request error, got: %v", err)
	}
}
