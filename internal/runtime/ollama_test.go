package runtime

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os/exec"
	"testing"
)

func TestIsReachable_True(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/tags" {
			w.WriteHeader(http.StatusOK)
			fmt.Fprint(w, `{"models":[]}`)
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	if !IsReachable(srv.URL) {
		t.Error("expected IsReachable to return true for healthy server")
	}
}

func TestIsReachable_False(t *testing.T) {
	if IsReachable("http://127.0.0.1:1") {
		t.Error("expected IsReachable to return false for unreachable server")
	}
}

func TestIsReachable_NonOK(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	if IsReachable(srv.URL) {
		t.Error("expected IsReachable to return false for 500 response")
	}
}

func TestEnsureReady_AlreadyRunning(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/tags" {
			w.WriteHeader(http.StatusOK)
			fmt.Fprint(w, `{"models":[{"name":"nomic-embed-text:latest"},{"name":"qwen2.5:0.5b"}]}`)
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	cfg := Config{
		OllamaURL:      srv.URL,
		EmbeddingModel: "nomic-embed-text",
		EnrichModel:    "qwen2.5:0.5b",
		Verbose:        false,
	}

	if err := EnsureReady(context.Background(), cfg); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestEnsureReady_SkipSetup_Unreachable(t *testing.T) {
	cfg := Config{
		OllamaURL:      "http://127.0.0.1:1",
		EmbeddingModel: "nomic-embed-text",
		SkipSetup:      true,
	}

	err := EnsureReady(context.Background(), cfg)
	if err == nil {
		t.Fatal("expected error when Ollama unreachable with SkipSetup")
	}
	if got := err.Error(); !contains(got, "auto-setup disabled") {
		t.Errorf("unexpected error message: %s", got)
	}
}

func TestEnsureReady_NonDefaultURL_Unreachable(t *testing.T) {
	cfg := Config{
		OllamaURL:      "http://remote-host:11434",
		EmbeddingModel: "nomic-embed-text",
	}

	err := EnsureReady(context.Background(), cfg)
	if err == nil {
		t.Fatal("expected error when non-default URL is unreachable")
	}
	if got := err.Error(); !contains(got, "custom URL") {
		t.Errorf("unexpected error message: %s", got)
	}
}

func TestEnsureReady_BinaryNotFound(t *testing.T) {
	// Save and restore package-level functions.
	origLookPath := lookPathFn
	origExecCommand := execCommandFn
	origIsReachable := isReachableFn
	defer func() {
		lookPathFn = origLookPath
		execCommandFn = origExecCommand
		isReachableFn = origIsReachable
	}()

	// Simulate Ollama not reachable.
	isReachableFn = func(url string) bool { return false }

	// Binary not found — should return install instructions.
	lookPathFn = func(name string) (string, error) {
		return "", exec.ErrNotFound
	}

	cfg := Config{
		OllamaURL:      defaultOllamaURL,
		EmbeddingModel: "nomic-embed-text",
	}

	err := EnsureReady(context.Background(), cfg)
	if err == nil {
		t.Fatal("expected error when binary not found")
	}
	if got := err.Error(); !contains(got, "not installed") {
		t.Errorf("unexpected error message: %s", got)
	}
}

func TestEnsureReady_MissingModels_Pulled(t *testing.T) {
	pullCalled := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/tags":
			if pullCalled {
				fmt.Fprint(w, `{"models":[{"name":"nomic-embed-text:latest"}]}`)
			} else {
				fmt.Fprint(w, `{"models":[]}`)
			}
		case "/api/pull":
			pullCalled = true
			fmt.Fprint(w, `{"status":"success"}`)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()

	cfg := Config{
		OllamaURL:      srv.URL,
		EmbeddingModel: "nomic-embed-text",
		Verbose:        false,
	}

	if err := EnsureReady(context.Background(), cfg); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !pullCalled {
		t.Error("expected pull to be called for missing model")
	}
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && searchSubstring(s, substr)
}

func searchSubstring(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
