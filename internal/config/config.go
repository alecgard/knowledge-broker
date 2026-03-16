package config

import (
	"bufio"
	"os"
	"strconv"
	"strings"
)

// Config holds all configuration for Knowledge Broker.
type Config struct {
	// Database
	DBPath string

	// Ollama
	OllamaURL      string
	EmbeddingModel string
	EmbeddingDim   int
	EnrichModel    string

	// LLM Provider
	LLMProvider string // "claude" (default), "openai", "ollama"

	// Claude
	AnthropicAPIKey string
	ClaudeModel     string

	// OpenAI
	OpenAIAPIKey string
	OpenAIModel  string

	// Ollama LLM (separate from embedding model)
	OllamaLLMModel string

	// Server
	ListenAddr string

	// Ingestion
	MaxFileSize  int64 // bytes
	MaxChunkSize int   // characters
	ChunkOverlap int   // characters
	WorkerCount  int

	// Query
	DefaultLimit int

	// GitHub OAuth
	GitHubClientID string

	// Runtime
	SkipSetup bool // from KB_SKIP_SETUP
}

// Default returns a config with sensible defaults, overridden by .env file
// and environment variables. Env vars take precedence over .env.
func Default() Config {
	// Load .env file if present (does not overwrite existing env vars).
	loadDotEnv(".env")

	return Config{
		DBPath:          envOr("KB_DB", "kb.db"),
		OllamaURL:       envOr("KB_OLLAMA_URL", "http://localhost:11434"),
		EmbeddingModel:  envOr("KB_EMBEDDING_MODEL", "nomic-embed-text"),
		EnrichModel:     envOr("KB_ENRICH_MODEL", "qwen2.5:0.5b"),
		EmbeddingDim:    envOrInt("KB_EMBEDDING_DIM", 768),
		LLMProvider:     envOr("KB_LLM_PROVIDER", "claude"),
		AnthropicAPIKey: os.Getenv("ANTHROPIC_API_KEY"),
		ClaudeModel:     envOr("KB_CLAUDE_MODEL", "claude-sonnet-4-20250514"),
		OpenAIAPIKey:    os.Getenv("OPENAI_API_KEY"),
		OpenAIModel:     envOr("KB_OPENAI_MODEL", ""),
		OllamaLLMModel:  envOr("KB_OLLAMA_LLM_MODEL", ""),
		ListenAddr:      envOr("KB_LISTEN_ADDR", ":8080"),
		MaxFileSize:     int64(envOrInt("KB_MAX_FILE_SIZE", 1_048_576)),
		MaxChunkSize:    envOrInt("KB_MAX_CHUNK_SIZE", 2000),
		ChunkOverlap:    envOrInt("KB_CHUNK_OVERLAP", 150),
		WorkerCount:     envOrInt("KB_WORKERS", 4),
		DefaultLimit:    envOrInt("KB_DEFAULT_LIMIT", 5),
		GitHubClientID:  os.Getenv("KB_GITHUB_CLIENT_ID"),
		SkipSetup:       envOr("KB_SKIP_SETUP", "false") == "true",
	}
}

// loadDotEnv reads a .env file and sets environment variables for any keys
// not already set. This means real env vars always take precedence.
func loadDotEnv(path string) {
	f, err := os.Open(path)
	if err != nil {
		return // no .env file, that's fine
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())

		// Skip empty lines and comments.
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		key, value, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}

		key = strings.TrimSpace(key)
		value = strings.TrimSpace(value)

		// Strip surrounding quotes.
		if len(value) >= 2 {
			if (value[0] == '"' && value[len(value)-1] == '"') ||
				(value[0] == '\'' && value[len(value)-1] == '\'') {
				value = value[1 : len(value)-1]
			}
		}

		// Don't overwrite existing env vars.
		if _, exists := os.LookupEnv(key); !exists {
			os.Setenv(key, value)
		}
	}
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func envOrInt(key string, fallback int) int {
	if v := os.Getenv(key); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
	}
	return fallback
}
