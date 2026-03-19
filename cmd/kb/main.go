package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/knowledge-broker/knowledge-broker/internal/config"
	"github.com/knowledge-broker/knowledge-broker/internal/debug"
	"github.com/knowledge-broker/knowledge-broker/internal/embedding"
	"github.com/knowledge-broker/knowledge-broker/internal/enrich"
	"github.com/knowledge-broker/knowledge-broker/internal/extractor"
	"github.com/knowledge-broker/knowledge-broker/internal/ingest"
	"github.com/knowledge-broker/knowledge-broker/internal/llm"
	"github.com/knowledge-broker/knowledge-broker/internal/query"
	ollamaRT "github.com/knowledge-broker/knowledge-broker/internal/runtime"
	"github.com/knowledge-broker/knowledge-broker/internal/store"
)

var version = "0.1.0"

func main() {
	root := &cobra.Command{
		Use:   "kb",
		Short: "Knowledge Broker — ingest documents, query for answers with confidence signals",
		SilenceUsage: true,
		CompletionOptions: cobra.CompletionOptions{
			DisableDefaultCmd: true,
		},
	}

	root.PersistentFlags().Bool("debug", false, "Enable debug mode (log all API calls)")
	root.PersistentFlags().Bool("no-setup", false, "Skip automatic Ollama management")
	root.PersistentFlags().String("config", "", "Path to config file")

	root.AddCommand(versionCmd())
	root.AddCommand(ingestCmd())
	root.AddCommand(enrichCmd())
	root.AddCommand(queryCmd())
	root.AddCommand(chatCmd())
	root.AddCommand(serveCmd())
	root.AddCommand(exportCmd())
	root.AddCommand(sourcesCmd())
	root.AddCommand(evalCmd())
	root.AddCommand(clusterCmd())
	root.AddCommand(newSetupCmd())
	root.AddCommand(configCmd())
	root.AddCommand(backupCmd())
	root.AddCommand(restoreCmd())

	if err := root.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func loadConfig(cmd *cobra.Command) config.ResolvedConfig {
	configFile, _ := cmd.Flags().GetString("config")
	resolved := config.Load(config.LoadOptions{ConfigFile: configFile})

	// If --db was explicitly set, use it; otherwise run migration logic.
	if cmd.Flags().Changed("db") {
		resolved.Config.DBPath, _ = cmd.Flags().GetString("db")
	} else {
		// Check if KB_DB was explicitly configured (not just the default).
		dbExplicit := resolved.Origins["KB_DB"].Source != "default"
		finalPath, warn := config.MigrateDB(resolved.Config.DBPath, dbExplicit)
		if warn != "" {
			fmt.Fprintln(os.Stderr, warn)
		}
		resolved.Config.DBPath = finalPath
	}

	return resolved
}

func isDebug(cmd *cobra.Command) bool {
	d, _ := cmd.Flags().GetBool("debug")
	return d
}

func ensureOllama(ctx context.Context, cmd *cobra.Command, cfg config.Config, verbose bool) error {
	noSetup, _ := cmd.Flags().GetBool("no-setup")
	rtCfg := ollamaRT.Config{
		OllamaURL:      cfg.OllamaURL,
		EmbeddingModel: cfg.EmbeddingModel,
		EnrichModel:    cfg.EnrichModel,
		SkipSetup:      cfg.SkipSetup || noSetup,
		Verbose:        verbose,
	}
	if cfg.LLMProvider == "ollama" {
		rtCfg.LLMModel = cfg.OllamaLLMModel
	}
	return ollamaRT.EnsureReady(ctx, rtCfg)
}

func newSetupCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "setup",
		Short: "Verify Ollama installation and pull required models",
		RunE:  runSetupOllama,
	}
	cmd.AddCommand(setupMCPCmd())
	return cmd
}

func runSetupOllama(cmd *cobra.Command, args []string) error {
	cfg := loadConfig(cmd).Config
	ctx := context.Background()

	rtCfg := ollamaRT.Config{
		OllamaURL:      cfg.OllamaURL,
		EmbeddingModel: cfg.EmbeddingModel,
		EnrichModel:    cfg.EnrichModel,
		Verbose:        true,
	}
	if cfg.LLMProvider == "ollama" {
		rtCfg.LLMModel = cfg.OllamaLLMModel
	}
	return ollamaRT.EnsureReady(ctx, rtCfg)
}

func newLogger(debugMode bool) *slog.Logger {
	level := slog.LevelInfo
	if debugMode {
		level = slog.LevelDebug
	}
	return slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: level}))
}

func httpClient(logger *slog.Logger, debugMode bool) *http.Client {
	return debug.NewLoggingClient(logger, debugMode)
}

func openStore(cfg config.Config) (*store.SQLiteStore, error) {
	// Ensure the parent directory exists (e.g. ~/.local/share/kb/).
	if dir := filepath.Dir(cfg.DBPath); dir != "." {
		if err := os.MkdirAll(dir, 0755); err != nil {
			return nil, fmt.Errorf("create db directory: %w", err)
		}
	}
	return store.NewSQLiteStore(cfg.DBPath, cfg.EmbeddingDim)
}

func newEmbedder(cfg config.Config, client *http.Client) *embedding.OllamaEmbedder {
	return embedding.NewOllamaEmbedder(cfg.OllamaURL, cfg.EmbeddingModel, cfg.EmbeddingDim, client)
}

// configureEnrichment sets up enrichment on a pipeline if not skipped.
func configureEnrichment(pipeline *ingest.Pipeline, cfg config.Config, client *http.Client, logger *slog.Logger, skipEnrichment bool, enrichModel string, promptVersion string) {
	if skipEnrichment {
		return
	}
	if enrichModel == "" {
		enrichModel = cfg.EnrichModel
	}
	enricher := enrich.NewOllamaEnricher(cfg.OllamaURL, enrichModel, promptVersion, client, logger)
	pipeline.SetEnrichment(ingest.EnrichmentConfig{
		Enricher: enricher,
	})
}

// newLLMClient creates the appropriate LLM client based on the configured provider.
// If provider is empty, it defaults to "claude". Returns nil if the provider
// requires an API key that is not set (allowing raw mode to work without one).
func newLLMClient(cfg config.Config, provider string, client *http.Client, logger ...*slog.Logger) query.LLM {
	var lg *slog.Logger
	if len(logger) > 0 {
		lg = logger[0]
	}
	if provider == "" {
		provider = cfg.LLMProvider
	}
	switch provider {
	case "openai":
		if cfg.OpenAIAPIKey == "" {
			return nil
		}
		return llm.NewOpenAIClient(cfg.OpenAIAPIKey, cfg.OpenAIModel, client)
	case "ollama":
		return llm.NewOllamaLLMClient(cfg.OllamaURL, cfg.OllamaLLMModel, client)
	default: // "claude"
		if cfg.AnthropicAPIKey == "" {
			return nil
		}
		return llm.NewClaudeClient(cfg.AnthropicAPIKey, cfg.ClaudeModel, client, lg)
	}
}

func newExtractorRegistry(cfg config.Config) *extractor.Registry {
	reg := extractor.NewRegistry(extractor.NewPlaintextExtractor(cfg.MaxChunkSize, cfg.ChunkOverlap))
	reg.Register(extractor.NewMarkdownExtractor(cfg.MaxChunkSize))
	reg.Register(extractor.NewCodeExtractor(cfg.MaxChunkSize))
	reg.Register(extractor.NewYAMLExtractor(cfg.MaxChunkSize))
	reg.Register(extractor.NewJupyterExtractor(cfg.MaxChunkSize))
	reg.Register(extractor.NewPDFExtractor(cfg.MaxChunkSize))
	return reg
}

// remoteRequest makes an HTTP request to a remote KB server. If body is non-nil
// it is JSON-encoded. Returns the response for the caller to read and close.
func remoteRequest(ctx context.Context, method, url string, body any) (*http.Response, error) {
	var bodyReader io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			return nil, err
		}
		bodyReader = bytes.NewReader(data)
	}
	req, err := http.NewRequestWithContext(ctx, method, url, bodyReader)
	if err != nil {
		return nil, err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	return http.DefaultClient.Do(req)
}

// remoteJSON makes a remote request and decodes the JSON response into dest.
func remoteJSON(ctx context.Context, method, url string, body any, dest any) error {
	resp, err := remoteRequest(ctx, method, url, body)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("read response: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("remote returned %d: %s", resp.StatusCode, string(respBody))
	}
	if dest != nil {
		if err := json.Unmarshal(respBody, dest); err != nil {
			return fmt.Errorf("parse response: %w", err)
		}
	}
	return nil
}

// sanitizeTSV replaces tab and newline characters with spaces.
func sanitizeTSV(s string) string {
	r := strings.NewReplacer("\t", " ", "\n", " ", "\r", " ")
	return r.Replace(s)
}
