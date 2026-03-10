package llm

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/knowledge-broker/knowledge-broker/pkg/model"
)

func TestOllamaLLMStreamAnswer(t *testing.T) {
	// Mock NDJSON response from Ollama.
	ndjson := `{"message":{"content":"Hello"},"done":false}
{"message":{"content":" world"},"done":false}
{"message":{"content":"!"},"done":false}
{"message":{"content":""},"done":true}
`

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/chat" {
			t.Errorf("expected path /api/chat, got %q", r.URL.Path)
		}
		if r.Header.Get("Content-Type") != "application/json" {
			t.Errorf("expected Content-Type application/json, got %q", r.Header.Get("Content-Type"))
		}
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(ndjson))
	}))
	defer srv.Close()

	client := NewOllamaLLMClient(srv.URL, "llama3.2", srv.Client())

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
		t.Errorf("expected 3 chunks, got %d: %v", len(chunks), chunks)
	}
}

func TestOllamaLLMStreamAnswer_ErrorResponse(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte(`{"error":"model not found"}`))
	}))
	defer srv.Close()

	client := NewOllamaLLMClient(srv.URL, "nonexistent", srv.Client())

	_, err := client.StreamAnswer(context.Background(), "system", []model.Message{
		{Role: "user", Content: "hi"},
	}, nil)

	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "500") {
		t.Errorf("expected 500 in error, got %q", err.Error())
	}
}

func TestOllamaLLMStreamAnswer_NilOnText(t *testing.T) {
	ndjson := `{"message":{"content":"ok"},"done":false}
{"message":{"content":""},"done":true}
`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(ndjson))
	}))
	defer srv.Close()

	client := NewOllamaLLMClient(srv.URL, "", srv.Client())

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

func TestConvertToOllama(t *testing.T) {
	messages := []model.Message{
		{Role: "user", Content: "Hello"},
		{Role: "assistant", Content: "Hi"},
		{Role: "system", Content: "should be skipped"},
		{Role: "user", Content: "Bye"},
	}

	result := convertToOllama("You are a bot.", messages)

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

func TestParseOllamaNDJSON(t *testing.T) {
	input := `{"message":{"content":"A"},"done":false}
{"message":{"content":"B"},"done":false}
{"message":{"content":""},"done":true}
`
	var chunks []string
	result, err := parseOllamaNDJSON(strings.NewReader(input), func(s string) {
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

func TestNewOllamaLLMClient_Defaults(t *testing.T) {
	client := NewOllamaLLMClient("", "", nil)
	if client.baseURL != defaultOllamaLLMURL {
		t.Errorf("expected default URL %q, got %q", defaultOllamaLLMURL, client.baseURL)
	}
	if client.model != defaultOllamaLLMModel {
		t.Errorf("expected default model %q, got %q", defaultOllamaLLMModel, client.model)
	}
}

func TestNewOllamaLLMClient_TrailingSlash(t *testing.T) {
	client := NewOllamaLLMClient("http://localhost:11434/", "test", nil)
	if client.baseURL != "http://localhost:11434" {
		t.Errorf("expected trailing slash stripped, got %q", client.baseURL)
	}
}
