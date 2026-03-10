package llm

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/knowledge-broker/knowledge-broker/pkg/model"
)

func TestOpenAIStreamAnswer(t *testing.T) {
	// Mock SSE response from OpenAI.
	ssePayload := `data: {"choices":[{"delta":{"content":"Hello"}}]}

data: {"choices":[{"delta":{"content":" world"}}]}

data: {"choices":[{"delta":{"content":"!"}}]}

data: [DONE]

`

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer test-key" {
			t.Errorf("expected Authorization header 'Bearer test-key', got %q", r.Header.Get("Authorization"))
		}
		if r.Header.Get("Content-Type") != "application/json" {
			t.Errorf("expected Content-Type application/json, got %q", r.Header.Get("Content-Type"))
		}
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(ssePayload))
	}))
	defer srv.Close()

	client := NewOpenAIClient("test-key", "gpt-4o", srv.Client())
	// Override the API URL by using a custom HTTP client that redirects.
	// Instead, we need to point the client at our test server.
	// We'll use a wrapper approach.
	client.httpClient = &http.Client{
		Transport: &rewriteTransport{base: srv.Client().Transport, target: srv.URL},
	}

	var chunks []string
	result, err := client.StreamAnswer(context.Background(), "You are helpful.", []model.Message{
		{Role: "user", Content: "Say hello"},
	}, func(text string) {
		chunks = append(chunks, text)
	})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != "Hello world!" {
		t.Errorf("expected 'Hello world!', got %q", result)
	}
	if len(chunks) != 3 {
		t.Errorf("expected 3 chunks, got %d", len(chunks))
	}
}

func TestOpenAIStreamAnswer_ErrorResponse(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		w.Write([]byte(`{"error":{"message":"Invalid API key"}}`))
	}))
	defer srv.Close()

	client := NewOpenAIClient("bad-key", "", srv.Client())
	client.httpClient = &http.Client{
		Transport: &rewriteTransport{base: srv.Client().Transport, target: srv.URL},
	}

	_, err := client.StreamAnswer(context.Background(), "system", []model.Message{
		{Role: "user", Content: "hi"},
	}, nil)

	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "401") {
		t.Errorf("expected 401 in error, got %q", err.Error())
	}
}

func TestOpenAIStreamAnswer_NilOnText(t *testing.T) {
	ssePayload := `data: {"choices":[{"delta":{"content":"ok"}}]}

data: [DONE]

`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(ssePayload))
	}))
	defer srv.Close()

	client := NewOpenAIClient("key", "", srv.Client())
	client.httpClient = &http.Client{
		Transport: &rewriteTransport{base: srv.Client().Transport, target: srv.URL},
	}

	result, err := client.StreamAnswer(context.Background(), "sys", []model.Message{
		{Role: "user", Content: "hi"},
	}, nil)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != "ok" {
		t.Errorf("expected 'ok', got %q", result)
	}
}

func TestConvertToOpenAI(t *testing.T) {
	messages := []model.Message{
		{Role: "user", Content: "Hello"},
		{Role: "assistant", Content: "Hi"},
		{Role: "system", Content: "should be skipped"},
		{Role: "user", Content: "Bye"},
	}

	result := convertToOpenAI("You are a bot.", messages)

	// system prompt + user + assistant + user (system in messages is skipped)
	if len(result) != 4 {
		t.Fatalf("expected 4 messages, got %d", len(result))
	}
	if result[0].Role != "system" || result[0].Content != "You are a bot." {
		t.Errorf("expected system prompt first, got %+v", result[0])
	}
	if result[1].Role != "user" || result[1].Content != "Hello" {
		t.Errorf("expected user Hello, got %+v", result[1])
	}
	if result[2].Role != "assistant" || result[2].Content != "Hi" {
		t.Errorf("expected assistant Hi, got %+v", result[2])
	}
	if result[3].Role != "user" || result[3].Content != "Bye" {
		t.Errorf("expected user Bye, got %+v", result[3])
	}
}

func TestParseOpenAISSE(t *testing.T) {
	input := `data: {"choices":[{"delta":{"content":"A"}}]}

data: {"choices":[{"delta":{"content":"B"}}]}

data: {"choices":[{"delta":{}}]}

data: [DONE]

`
	var chunks []string
	result, err := parseOpenAISSE(strings.NewReader(input), func(s string) {
		chunks = append(chunks, s)
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != "AB" {
		t.Errorf("expected 'AB', got %q", result)
	}
	if len(chunks) != 2 {
		t.Errorf("expected 2 chunks, got %d", len(chunks))
	}
}

// rewriteTransport redirects all requests to the test server URL.
type rewriteTransport struct {
	base   http.RoundTripper
	target string
}

func (t *rewriteTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	req.URL.Scheme = "http"
	req.URL.Host = strings.TrimPrefix(t.target, "http://")
	if t.base != nil {
		return t.base.RoundTrip(req)
	}
	return http.DefaultTransport.RoundTrip(req)
}
