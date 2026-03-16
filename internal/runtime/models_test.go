package runtime

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestEnsureModels_AllPresent(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/tags" {
			fmt.Fprint(w, `{"models":[{"name":"nomic-embed-text:latest"},{"name":"qwen2.5:0.5b"}]}`)
			return
		}
		t.Errorf("unexpected request to %s", r.URL.Path)
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	mandatory := map[string]bool{"nomic-embed-text": true}
	err := EnsureModels(context.Background(), srv.URL, []string{"nomic-embed-text", "qwen2.5:0.5b"}, mandatory, false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestEnsureModels_PullsMissing(t *testing.T) {
	var pulledModels []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/tags":
			fmt.Fprint(w, `{"models":[]}`)
		case "/api/pull":
			var req pullRequest
			json.NewDecoder(r.Body).Decode(&req)
			pulledModels = append(pulledModels, req.Name)
			fmt.Fprint(w, `{"status":"pulling manifest"}
{"status":"success"}`)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()

	mandatory := map[string]bool{"nomic-embed-text": true}
	err := EnsureModels(context.Background(), srv.URL, []string{"nomic-embed-text"}, mandatory, false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(pulledModels) != 1 || pulledModels[0] != "nomic-embed-text" {
		t.Errorf("expected [nomic-embed-text] to be pulled, got %v", pulledModels)
	}
}

func TestEnsureModels_MandatoryPullFails(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/tags":
			fmt.Fprint(w, `{"models":[]}`)
		case "/api/pull":
			w.WriteHeader(http.StatusInternalServerError)
			fmt.Fprint(w, `{"error":"model not found"}`)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()

	mandatory := map[string]bool{"nomic-embed-text": true}
	err := EnsureModels(context.Background(), srv.URL, []string{"nomic-embed-text"}, mandatory, false)
	if err == nil {
		t.Fatal("expected error when mandatory model pull fails")
	}
}

func TestEnsureModels_OptionalPullFails(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/tags":
			fmt.Fprint(w, `{"models":[]}`)
		case "/api/pull":
			w.WriteHeader(http.StatusInternalServerError)
			fmt.Fprint(w, `{"error":"model not found"}`)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()

	mandatory := map[string]bool{} // enrichment model is optional
	err := EnsureModels(context.Background(), srv.URL, []string{"qwen2.5:0.5b"}, mandatory, false)
	if err != nil {
		t.Fatalf("expected no error for optional model pull failure, got: %v", err)
	}
}

func TestStripTag(t *testing.T) {
	tests := []struct {
		input, want string
	}{
		{"nomic-embed-text:latest", "nomic-embed-text"},
		{"nomic-embed-text", "nomic-embed-text"},
		{"qwen2.5:0.5b", "qwen2.5:0.5b"},
		{"model:v2", "model:v2"},
	}
	for _, tc := range tests {
		got := stripTag(tc.input)
		if got != tc.want {
			t.Errorf("stripTag(%q) = %q, want %q", tc.input, got, tc.want)
		}
	}
}

func TestRenderProgressBar(t *testing.T) {
	bar := renderProgressBar(50, 10)
	if len([]rune(bar)) != 10 {
		t.Errorf("expected 10 rune bar, got %d runes: %q", len([]rune(bar)), bar)
	}

	bar0 := renderProgressBar(0, 10)
	bar100 := renderProgressBar(100, 10)
	if bar0 == bar100 {
		t.Error("0% and 100% bars should differ")
	}
}

func TestEnsureModels_StripsLatestForComparison(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/tags" {
			// Server reports with :latest suffix
			fmt.Fprint(w, `{"models":[{"name":"nomic-embed-text:latest"}]}`)
			return
		}
		t.Errorf("unexpected request to %s (should not try to pull)", r.URL.Path)
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	// Request without :latest suffix -- should still match.
	mandatory := map[string]bool{"nomic-embed-text": true}
	err := EnsureModels(context.Background(), srv.URL, []string{"nomic-embed-text"}, mandatory, false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestEnsureModels_ProgressParsing(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/tags":
			fmt.Fprint(w, `{"models":[]}`)
		case "/api/pull":
			// Simulate streaming progress.
			fmt.Fprint(w, `{"status":"pulling manifest"}
{"status":"downloading","completed":100000000,"total":274000000}
{"status":"downloading","completed":200000000,"total":274000000}
{"status":"success"}`)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()

	mandatory := map[string]bool{"test-model": true}
	err := EnsureModels(context.Background(), srv.URL, []string{"test-model"}, mandatory, true)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}
