package runtime

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"time"
)

// Config holds configuration for the Ollama auto-setup feature.
type Config struct {
	OllamaURL      string // from KB_OLLAMA_URL, default http://localhost:11434
	EmbeddingModel string // from KB_EMBEDDING_MODEL, default nomic-embed-text
	EnrichModel    string // from KB_ENRICH_MODEL, default qwen2.5:0.5b
	SkipSetup      bool   // from KB_SKIP_SETUP or --no-setup flag
	Verbose        bool   // show progress output
}

const defaultOllamaURL = "http://localhost:11434"

// lookPathFn is a package-level variable for testing.
var lookPathFn = exec.LookPath

// execCommandFn is a package-level variable for testing.
var execCommandFn = exec.CommandContext

// isReachableFn is a package-level variable for testing.
var isReachableFn = IsReachable

// EnsureReady is the main entry point. Call before any operation needing
// embeddings/enrichment. Idempotent -- fast health check on subsequent calls.
//
// Sequence:
//  1. Try to reach Ollama at configured URL
//  2. If unreachable and SkipSetup, fail with clear error
//  3. If unreachable and URL is non-default, fail (don't install/start for remote)
//  4. If binary missing, install Ollama
//  5. Start ollama serve as background process
//  6. Once reachable, check required models and pull missing ones
func EnsureReady(ctx context.Context, cfg Config) error {
	if cfg.OllamaURL == "" {
		cfg.OllamaURL = defaultOllamaURL
	}

	// Fast path: already running.
	if isReachableFn(cfg.OllamaURL) {
		return ensureModelsReady(ctx, cfg)
	}

	// SkipSetup: don't install or start, just fail.
	if cfg.SkipSetup {
		return fmt.Errorf("Ollama is not reachable at %s (auto-setup disabled via --no-setup or KB_SKIP_SETUP)", cfg.OllamaURL)
	}

	// Non-default URL: don't try to install/start locally, but still fail clearly.
	if cfg.OllamaURL != defaultOllamaURL {
		return fmt.Errorf("Ollama is not reachable at %s (custom URL; install/start skipped)", cfg.OllamaURL)
	}

	// Check if ollama binary exists.
	_, err := lookPathFn("ollama")
	if err != nil {
		// Binary not found, install it.
		if installErr := installOllama(ctx); installErr != nil {
			return installErr
		}
		// Verify it's now on PATH.
		if _, err := lookPathFn("ollama"); err != nil {
			return fmt.Errorf("ollama binary not found on PATH after installation; please add it to your PATH and retry")
		}
	}

	// Start the server.
	if err := startServer(ctx, cfg.Verbose); err != nil {
		return err
	}

	return ensureModelsReady(ctx, cfg)
}

func ensureModelsReady(ctx context.Context, cfg Config) error {
	var models []string
	if cfg.EmbeddingModel != "" {
		models = append(models, cfg.EmbeddingModel)
	}
	if cfg.EnrichModel != "" {
		models = append(models, cfg.EnrichModel)
	}
	if len(models) == 0 {
		return nil
	}
	mandatoryModels := map[string]bool{}
	if cfg.EmbeddingModel != "" {
		mandatoryModels[cfg.EmbeddingModel] = true
	}
	return EnsureModels(ctx, cfg.OllamaURL, models, mandatoryModels, cfg.Verbose)
}

// IsReachable checks whether the Ollama server is reachable at the given URL.
func IsReachable(baseURL string) bool {
	client := &http.Client{Timeout: 2 * time.Second}
	resp, err := client.Get(baseURL + "/api/tags")
	if err != nil {
		return false
	}
	resp.Body.Close()
	return resp.StatusCode == http.StatusOK
}

// installOllama downloads and runs the Ollama install script.
func installOllama(ctx context.Context) error {
	fmt.Fprintf(os.Stderr, "KB requires Ollama for local AI model inference. Installing now...\n")

	cmd := execCommandFn(ctx, "sh", "-c", "curl -fsSL https://ollama.com/install.sh | sh")
	cmd.Stdout = os.Stderr
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("install Ollama: %w", err)
	}
	return nil
}

// startServer starts ollama serve as a detached background process.
func startServer(ctx context.Context, verbose bool) error {
	if verbose {
		fmt.Fprintf(os.Stderr, "Starting Ollama server...\n")
	}

	// Create ~/.kb/ directory for log file.
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return fmt.Errorf("get home directory: %w", err)
	}
	kbDir := filepath.Join(homeDir, ".kb")
	if err := os.MkdirAll(kbDir, 0755); err != nil {
		return fmt.Errorf("create ~/.kb directory: %w", err)
	}

	logPath := filepath.Join(kbDir, "ollama.log")
	logFile, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		return fmt.Errorf("open Ollama log file: %w", err)
	}

	cmd := execCommandFn(ctx, "ollama", "serve")
	cmd.Stdout = logFile
	cmd.Stderr = logFile
	detachProcess(cmd)

	if err := cmd.Start(); err != nil {
		logFile.Close()
		return fmt.Errorf("start Ollama server: %w", err)
	}

	// Release the process so it survives after KB exits.
	if cmd.Process != nil {
		cmd.Process.Release()
	}
	logFile.Close()

	// Poll health endpoint until ready.
	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		if IsReachable(defaultOllamaURL) {
			if verbose {
				fmt.Fprintf(os.Stderr, "Ollama server is ready.\n")
			}
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(500 * time.Millisecond):
		}
	}

	return fmt.Errorf("Ollama server did not become ready within 15 seconds (check %s for details)", logPath)
}
