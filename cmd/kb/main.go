package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/spf13/cobra"

	"github.com/knowledge-broker/knowledge-broker/internal/cluster"
	"github.com/knowledge-broker/knowledge-broker/internal/config"
	"github.com/knowledge-broker/knowledge-broker/internal/connector"
	"github.com/knowledge-broker/knowledge-broker/internal/debug"
	"github.com/knowledge-broker/knowledge-broker/internal/embedding"
	"github.com/knowledge-broker/knowledge-broker/internal/enrich"
	"github.com/knowledge-broker/knowledge-broker/internal/eval"
	"github.com/knowledge-broker/knowledge-broker/internal/extractor"
	"github.com/knowledge-broker/knowledge-broker/internal/ingest"
	"github.com/knowledge-broker/knowledge-broker/internal/llm"
	"github.com/knowledge-broker/knowledge-broker/pkg/model"
	"github.com/knowledge-broker/knowledge-broker/internal/query"
	"github.com/knowledge-broker/knowledge-broker/internal/server"
	"github.com/knowledge-broker/knowledge-broker/internal/store"
)

var version = "0.1.0"

func main() {
	root := &cobra.Command{
		Use:   "kb",
		Short: "Knowledge Broker — ingest documents, query for answers with confidence signals",
		CompletionOptions: cobra.CompletionOptions{
			DisableDefaultCmd: true,
		},
	}

	root.PersistentFlags().Bool("debug", false, "Enable debug mode (log all API calls)")

	root.AddCommand(versionCmd())
	root.AddCommand(ingestCmd())
	root.AddCommand(queryCmd())
	root.AddCommand(serveCmd())
	root.AddCommand(mcpCmd())
	root.AddCommand(exportCmd())
	root.AddCommand(sourcesCmd())
	root.AddCommand(evalCmd())
	root.AddCommand(clusterCmd())
	root.AddCommand(setupCmd())

	if err := root.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func versionCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print version",
		Run: func(cmd *cobra.Command, args []string) {
			fmt.Printf("kb %s\n", version)
		},
	}
}

func isDebug(cmd *cobra.Command) bool {
	d, _ := cmd.Flags().GetBool("debug")
	return d
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

// reEnrichFragments re-runs enrichment on a set of fragments, groups them by source path
// for correct sliding window context, re-embeds, and upserts.
func reEnrichFragments(ctx context.Context, s store.Store, emb *embedding.OllamaEmbedder, enricher enrich.Enricher, frags []model.SourceFragment, logger *slog.Logger) error {
	if len(frags) == 0 {
		return nil
	}

	// Group by source path.
	type group struct {
		indices []int
		chunks  []model.Chunk
	}
	groups := make(map[string]*group)
	var order []string
	for i, f := range frags {
		key := f.SourcePath
		g, ok := groups[key]
		if !ok {
			g = &group{}
			groups[key] = g
			order = append(order, key)
		}
		g.indices = append(g.indices, i)
		g.chunks = append(g.chunks, model.Chunk{Content: f.RawContent})
	}

	// Enrich all chunks.
	for _, key := range order {
		g := groups[key]
		enriched, err := enrich.EnrichChunks(ctx, enricher, g.chunks, 3, 1, 4)
		if err != nil {
			logger.Warn("enrichment failed for path, skipping", "path", key, "error", err)
			continue
		}
		for j, idx := range g.indices {
			frags[idx].EnrichedContent = enriched[j]
			frags[idx].EnrichmentModel = enricher.Model()
			frags[idx].EnrichmentVersion = enrich.PromptVersion
		}
	}

	// Re-embed using enriched content.
	texts := make([]string, len(frags))
	for i, f := range frags {
		texts[i] = f.Content()
	}
	embeddings, err := emb.EmbedBatch(ctx, texts)
	if err != nil {
		return fmt.Errorf("re-embed: %w", err)
	}
	for i := range frags {
		frags[i].Embedding = embeddings[i]
	}

	// Upsert.
	if err := s.UpsertFragments(ctx, frags); err != nil {
		return fmt.Errorf("upsert re-enriched: %w", err)
	}
	fmt.Fprintf(os.Stderr, "  Re-enriched and re-embedded %d fragments\n", len(frags))
	return nil
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

func ingestCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "ingest",
		Short: "Ingest documents from one or more sources",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg := config.Default()
			cfg.DBPath, _ = cmd.Flags().GetString("db")
			debugMode := isDebug(cmd)
			logger := newLogger(debugMode)
			client := httpClient(logger, debugMode)
			remote, _ := cmd.Flags().GetString("remote")
			all, _ := cmd.Flags().GetBool("all")
			parallel, _ := cmd.Flags().GetBool("parallel")

			watchMode, _ := cmd.Flags().GetBool("watch")
			description, _ := cmd.Flags().GetString("description")
			skipEnrichment, _ := cmd.Flags().GetBool("skip-enrichment")
			enrichModel, _ := cmd.Flags().GetString("enrich-model")
			reEnrich, _ := cmd.Flags().GetBool("re-enrich")
			promptVersion, _ := cmd.Flags().GetString("prompt-version")

			if all && remote != "" {
				return fmt.Errorf("--all and --remote cannot be combined")
			}
			if watchMode && remote != "" {
				return fmt.Errorf("--watch and --remote cannot be combined")
			}
			if watchMode && all {
				return fmt.Errorf("--watch and --all cannot be combined")
			}

			s, err := openStore(cfg)
			if err != nil {
				return fmt.Errorf("open store: %w", err)
			}
			defer s.Close()

			reg := newExtractorRegistry(cfg)

			ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
			defer cancel()

			// --re-enrich: re-run enrichment on existing fragments, then re-embed.
			if reEnrich {
				sourcePaths, _ := cmd.Flags().GetStringArray("source")

				emb := newEmbedder(cfg, client)
				if err := emb.CheckHealth(ctx); err != nil {
					return err
				}
				enricher := enrich.NewOllamaEnricher(cfg.OllamaURL, enrichModel, promptVersion, client, logger)

				// Determine which source names to re-enrich.
				var sourceNames []string
				if len(sourcePaths) > 0 {
					for _, p := range sourcePaths {
						abs, _ := filepath.Abs(p)
						sourceNames = append(sourceNames, abs)
					}
				}

				if len(sourceNames) == 0 {
					// Re-enrich all fragments.
					frags, err := s.ExportFragments(ctx)
					if err != nil {
						return fmt.Errorf("export fragments: %w", err)
					}
					return reEnrichFragments(ctx, s, emb, enricher, frags, logger)
				}

				for _, name := range sourceNames {
					frags, err := s.GetFragmentsBySource(ctx, name)
					if err != nil {
						return fmt.Errorf("get fragments for %s: %w", name, err)
					}
					fmt.Fprintf(os.Stderr, "Re-enriching %d fragments from %s...\n", len(frags), name)
					if err := reEnrichFragments(ctx, s, emb, enricher, frags, logger); err != nil {
						return err
					}
				}
				return nil
			}

			// --all: re-ingest all registered local sources.
			if all {
				emb := newEmbedder(cfg, client)
				if err := emb.CheckHealth(ctx); err != nil {
					return err
				}

				sources, err := s.ListSources(ctx)
				if err != nil {
					return fmt.Errorf("list sources: %w", err)
				}
				var localSources []model.Source
				for _, src := range sources {
					if src.Config["mode"] == model.SourceModeLocal {
						localSources = append(localSources, src)
					}
				}
				if len(localSources) == 0 {
					return fmt.Errorf("no local sources registered; use --source or --git to ingest first")
				}
				var errs []string

				if parallel {
					type reIngestResult struct {
						src    model.Source
						result *ingest.Result
						err    error
					}

					resultCh := make(chan reIngestResult, len(localSources))
					var wg sync.WaitGroup

					for _, src := range localSources {
						wg.Add(1)
						go func(src model.Source) {
							defer wg.Done()
							conn, err := connector.FromSource(src)
							if err != nil {
								resultCh <- reIngestResult{src: src, err: fmt.Errorf("reconstruct %s/%s: %w", src.SourceType, src.SourceName, err)}
								return
							}
							fmt.Fprintf(os.Stderr, "Re-ingesting %s/%s...\n", src.SourceType, src.SourceName)
							srcLabel := src.SourceType + "/" + src.SourceName
							pipeline := ingest.NewPipeline(s, emb, reg, cfg.WorkerCount, logger)
							configureEnrichment(pipeline, cfg, client, logger, skipEnrichment, enrichModel, promptVersion)
							pipeline.OnProgress = makeProgressFunc(srcLabel, true)
							pipeline.OnBatchDone = makeBatchFunc()
							r, err := pipeline.Run(ctx, conn)
							resultCh <- reIngestResult{src: src, result: r, err: err}
						}(src)
					}

					go func() {
						wg.Wait()
						close(resultCh)
					}()

					for ir := range resultCh {
						if ir.err != nil {
							errs = append(errs, fmt.Sprintf("ingest %s/%s: %v", ir.src.SourceType, ir.src.SourceName, ir.err))
							continue
						}
						ir.src.LastIngest = time.Now()
						if regErr := s.RegisterSource(ctx, ir.src); regErr != nil {
							logger.Warn("failed to update source timestamp", "error", regErr)
						}
						fmt.Fprintf(os.Stderr, "  %s/%s: %d added, %d deleted, %d skipped, %d errors\n",
							ir.src.SourceType, ir.src.SourceName, ir.result.Added, ir.result.Deleted, ir.result.Skipped, ir.result.Errors)
					}
				} else {
					for _, src := range localSources {
						conn, err := connector.FromSource(src)
						if err != nil {
							errs = append(errs, fmt.Sprintf("reconstruct %s/%s: %v", src.SourceType, src.SourceName, err))
							continue
						}
						fmt.Fprintf(os.Stderr, "Re-ingesting %s/%s...\n", src.SourceType, src.SourceName)
						srcLabel := src.SourceType + "/" + src.SourceName
						pipeline := ingest.NewPipeline(s, emb, reg, cfg.WorkerCount, logger)
						configureEnrichment(pipeline, cfg, client, logger, skipEnrichment, enrichModel, promptVersion)
						pipeline.OnProgress = makeProgressFunc(srcLabel, false)
						pipeline.OnBatchDone = makeBatchFunc()
						r, err := pipeline.Run(ctx, conn)
						if err != nil {
							errs = append(errs, fmt.Sprintf("ingest %s/%s: %v", src.SourceType, src.SourceName, err))
							continue
						}
						src.LastIngest = time.Now()
						if regErr := s.RegisterSource(ctx, src); regErr != nil {
							logger.Warn("failed to update source timestamp", "error", regErr)
						}
						fmt.Fprintf(os.Stderr, "  %s/%s: %d added, %d deleted, %d skipped, %d errors\n",
							src.SourceType, src.SourceName, r.Added, r.Deleted, r.Skipped, r.Errors)
					}
				}

				if len(errs) > 0 {
					return fmt.Errorf("%s", strings.Join(errs, "; "))
				}
				return nil
			}

			// Build list of sources from flags.
			gitURLs, _ := cmd.Flags().GetStringArray("git")
			sourcePaths, _ := cmd.Flags().GetStringArray("source")
			confluenceSpaces, _ := cmd.Flags().GetStringArray("confluence")
			slackChannels, _ := cmd.Flags().GetStringArray("slack")
			wikiURLs, _ := cmd.Flags().GetStringArray("wiki")

			var connectors []connector.Connector

			for _, u := range gitURLs {
				connectors = append(connectors, connector.NewGitConnector(u, "", cfg.GitHubClientID))
			}
			for _, p := range sourcePaths {
				connectors = append(connectors, connector.NewFilesystemConnector(p))
			}
			for _, space := range confluenceSpaces {
				baseURL := os.Getenv("KB_CONFLUENCE_BASE_URL")
				email := os.Getenv("KB_CONFLUENCE_EMAIL")
				token := os.Getenv("KB_CONFLUENCE_TOKEN")
				if baseURL == "" || email == "" || token == "" {
					return fmt.Errorf("--confluence requires KB_CONFLUENCE_BASE_URL, KB_CONFLUENCE_EMAIL, and KB_CONFLUENCE_TOKEN")
				}
				connectors = append(connectors, connector.NewConfluenceConnector(baseURL, space, email, token))
			}
			if len(slackChannels) > 0 {
				token := os.Getenv("KB_SLACK_TOKEN")
				if token == "" {
					return fmt.Errorf("--slack requires KB_SLACK_TOKEN")
				}
				workspace := os.Getenv("KB_SLACK_WORKSPACE")
				connectors = append(connectors, connector.NewSlackConnector(token, slackChannels, workspace))
			}
			for _, u := range wikiURLs {
				connectors = append(connectors, connector.NewGitHubWikiConnector(u, "", cfg.GitHubClientID))
			}

			// Default: ingest current directory if no explicit flags.
			if len(connectors) == 0 {
				connectors = append(connectors, connector.NewFilesystemConnector("."))
			}

			remote = strings.TrimRight(remote, "/")

			if remote != "" {
				var errs []string

				if parallel {
					var wg sync.WaitGroup
					errCh := make(chan error, len(connectors))

					for _, conn := range connectors {
						wg.Add(1)
						go func(conn connector.Connector) {
							defer wg.Done()
							name := conn.SourceName()
							fmt.Fprintf(os.Stderr, "Ingesting %s/%s...\n", conn.Name(), name)

							if err := remoteIngest(ctx, conn, remote, s, reg, logger, client); err != nil {
								errCh <- fmt.Errorf("ingest %s: %w", name, err)
								return
							}

							srcConfig := conn.Config(model.SourceModePush)
							srcConfig["mode"] = model.SourceModePush
							if regErr := s.RegisterSource(ctx, model.Source{
								SourceType:  conn.Name(),
								SourceName:  name,
								Description: description,
								Config:      srcConfig,
								LastIngest:  time.Now(),
							}); regErr != nil {
								logger.Warn("failed to register source", "error", regErr)
							}
						}(conn)
					}

					wg.Wait()
					close(errCh)

					for err := range errCh {
						errs = append(errs, err.Error())
					}
				} else {
					for _, conn := range connectors {
						name := conn.SourceName()
						fmt.Fprintf(os.Stderr, "Ingesting %s/%s...\n", conn.Name(), name)

						if err := remoteIngest(ctx, conn, remote, s, reg, logger, client); err != nil {
							errs = append(errs, fmt.Sprintf("ingest %s: %v", name, err))
							continue
						}

						srcConfig := conn.Config(model.SourceModePush)
						srcConfig["mode"] = model.SourceModePush
						if regErr := s.RegisterSource(ctx, model.Source{
							SourceType:  conn.Name(),
							SourceName:  name,
							Description: description,
							Config:      srcConfig,
							LastIngest:  time.Now(),
						}); regErr != nil {
							logger.Warn("failed to register source", "error", regErr)
						}
					}
				}

				if len(errs) > 0 {
					return fmt.Errorf("%s", strings.Join(errs, "; "))
				}
				return nil
			}

			// Local ingestion — sequential by default, parallel with --parallel.
			emb := newEmbedder(cfg, client)
			if err := emb.CheckHealth(ctx); err != nil {
				return err
			}

			var errs []string

			if parallel {
				type ingestResult struct {
					name        string
					connType    string
					description string
					config      map[string]string
					result      *ingest.Result
					err         error
				}

				resultCh := make(chan ingestResult, len(connectors))
				var wg sync.WaitGroup

				for _, conn := range connectors {
					wg.Add(1)
					go func(conn connector.Connector) {
						defer wg.Done()
						name := conn.SourceName()
						fmt.Fprintf(os.Stderr, "Ingesting %s/%s...\n", conn.Name(), name)

						srcLabel := conn.Name() + "/" + name
						pipeline := ingest.NewPipeline(s, emb, reg, cfg.WorkerCount, logger)
						configureEnrichment(pipeline, cfg, client, logger, skipEnrichment, enrichModel, promptVersion)
						pipeline.OnProgress = makeProgressFunc(srcLabel, true)
						pipeline.OnBatchDone = makeBatchFunc()
						r, err := pipeline.Run(ctx, conn)
						srcConfig := conn.Config(model.SourceModeLocal)
						srcConfig["mode"] = model.SourceModeLocal
						resultCh <- ingestResult{
							name:        name,
							connType:    conn.Name(),
							description: description,
							config:      srcConfig,
							result:      r,
							err:         err,
						}
					}(conn)
				}

				go func() {
					wg.Wait()
					close(resultCh)
				}()

				for ir := range resultCh {
					if ir.err != nil {
						errs = append(errs, fmt.Sprintf("ingest %s: %v", ir.name, ir.err))
						continue
					}

					fmt.Fprintf(os.Stderr, "  %s: %d added, %d deleted, %d skipped, %d errors\n",
						ir.name, ir.result.Added, ir.result.Deleted, ir.result.Skipped, ir.result.Errors)

					if regErr := s.RegisterSource(ctx, model.Source{
						SourceType:  ir.connType,
						SourceName:  ir.name,
						Description: ir.description,
						Config:      ir.config,
						LastIngest:  time.Now(),
					}); regErr != nil {
						logger.Warn("failed to register source", "error", regErr)
					}
				}
			} else {
				for _, conn := range connectors {
					name := conn.SourceName()
					fmt.Fprintf(os.Stderr, "Ingesting %s/%s...\n", conn.Name(), name)

					srcLabel := conn.Name() + "/" + name
					pipeline := ingest.NewPipeline(s, emb, reg, cfg.WorkerCount, logger)
					configureEnrichment(pipeline, cfg, client, logger, skipEnrichment, enrichModel, promptVersion)
					pipeline.OnProgress = makeProgressFunc(srcLabel, false)
					pipeline.OnBatchDone = makeBatchFunc()
					r, err := pipeline.Run(ctx, conn)
					if err != nil {
						errs = append(errs, fmt.Sprintf("ingest %s: %v", name, err))
						continue
					}

					fmt.Fprintf(os.Stderr, "  %s: %d added, %d deleted, %d skipped, %d errors\n",
						name, r.Added, r.Deleted, r.Skipped, r.Errors)

					srcConfig := conn.Config(model.SourceModeLocal)
					srcConfig["mode"] = model.SourceModeLocal
					if regErr := s.RegisterSource(ctx, model.Source{
						SourceType:  conn.Name(),
						SourceName:  name,
						Description: description,
						Config:      srcConfig,
						LastIngest:  time.Now(),
					}); regErr != nil {
						logger.Warn("failed to register source", "error", regErr)
					}
				}
			}

			if len(errs) > 0 {
				return fmt.Errorf("%s", strings.Join(errs, "; "))
			}

			// Enter watch mode if requested.
			if watchMode {
				if len(gitURLs) > 0 {
					return fmt.Errorf("--watch is only supported for local filesystem sources, not --git")
				}
				// Resolve watch paths: use explicit --source paths or default to ".".
				watchPaths := sourcePaths
				if len(watchPaths) == 0 {
					watchPaths = []string{"."}
				}
				fmt.Fprintln(os.Stderr, "Watching for changes... (press Ctrl+C to stop)")
				watchPipeline := ingest.NewPipeline(s, emb, reg, cfg.WorkerCount, logger)
				configureEnrichment(watchPipeline, cfg, client, logger, skipEnrichment, enrichModel, promptVersion)
				watcher := ingest.NewWatcher(watchPipeline, logger)
				return watcher.Watch(ctx, watchPaths)
			}

			return nil
		},
	}
	cmd.Flags().StringArray("source", nil, "Local directory to ingest (repeatable)")
	cmd.Flags().StringArray("git", nil, "Git repo URL to ingest (repeatable)")
	cmd.Flags().StringArray("confluence", nil, "Confluence space key to ingest (repeatable, requires KB_CONFLUENCE_* env vars)")
	cmd.Flags().StringArray("slack", nil, "Slack channel ID to ingest (repeatable, requires KB_SLACK_TOKEN)")
	cmd.Flags().StringArray("wiki", nil, "GitHub repo URL whose wiki to ingest (repeatable)")
	cmd.Flags().String("db", "kb.db", "Path to SQLite database")
	cmd.Flags().String("remote", "", "URL of a remote KB server to push fragments to")
	cmd.Flags().Bool("all", false, "Re-ingest all registered local sources")
	cmd.Flags().Bool("watch", false, "Watch for file changes and re-ingest automatically (local sources only)")
	cmd.Flags().Bool("parallel", false, "Ingest multiple sources in parallel (default: sequential)")
	cmd.Flags().String("description", "", "Human-readable description of this source (shown to agents via MCP)")
	cmd.Flags().Bool("skip-enrichment", false, "Skip LLM chunk enrichment (faster ingestion)")
	cmd.Flags().String("enrich-model", "", "Ollama model for chunk enrichment (default: qwen2.5:0.5b)")
	cmd.Flags().Bool("re-enrich", false, "Re-run enrichment on already-ingested chunks, then re-embed")
	cmd.Flags().String("prompt-version", "", "Enrichment prompt version: v1 (full rewrite), v2 (append keywords)")
	return cmd
}

// makeProgressFunc returns a progress callback that writes an in-place
// progress line to stderr. When prefixed is true (multiple sources running
// in parallel), the source label is included so the user can tell which
// source each line belongs to.
func makeProgressFunc(label string, prefixed bool) ingest.ProgressFunc {
	return func(completed, total int) {
		pct := 0
		if total > 0 {
			pct = completed * 100 / total
		}
		if prefixed {
			fmt.Fprintf(os.Stderr, "\r  [%s] Embedding: %d/%d docs (%d%%)", label, completed, total, pct)
		} else {
			fmt.Fprintf(os.Stderr, "\r  Embedding: %d/%d docs (%d%%)", completed, total, pct)
		}
		if completed == total {
			fmt.Fprintln(os.Stderr)
		}
	}
}

func makeBatchFunc() ingest.BatchFunc {
	return func(batch, totalBatches, added int) {
		fmt.Fprintf(os.Stderr, "\r  Stored batch %d/%d (%d fragments)\n", batch, totalBatches, added)
	}
}

// remoteIngest pushes extracted fragments to a remote KB server and tracks
// checksums locally for incremental behavior.
func remoteIngest(ctx context.Context, conn connector.Connector, remote string, s *store.SQLiteStore, reg *extractor.Registry, logger *slog.Logger, client *http.Client) error {
	// Get known checksums for incremental ingestion.
	known, err := s.GetChecksums(ctx, conn.Name(), conn.SourceName())
	if err != nil {
		return fmt.Errorf("get checksums: %w", err)
	}

	// Scan for new/changed documents and deleted paths.
	fmt.Fprintf(os.Stderr, "Scanning %s...\n", conn.Name())
	docs, deleted, err := conn.Scan(ctx, connector.ScanOptions{Known: known})
	if err != nil {
		return fmt.Errorf("scan: %w", err)
	}

	fmt.Fprintf(os.Stderr, "Found %d new/changed files, %d deleted\n", len(docs), len(deleted))

	// Build fragments from documents using extractors (without embedding).
	fmt.Fprintf(os.Stderr, "Extracting chunks...\n")
	var allFragments []model.IngestFragment
	for _, doc := range docs {
		result, err := ingest.ExtractChunks(doc, reg)
		if err != nil {
			logger.Warn("extract failed", "path", doc.Path, "error", err)
			continue
		}

		fileType := filepath.Ext(doc.Path)

		for _, chunk := range result.Chunks {
			allFragments = append(allFragments, model.IngestFragment{
				Content:      chunk.Content,
				SourceType:   doc.SourceType,
				SourceName:   doc.SourceName,
				SourcePath:   doc.Path,
				SourceURI:    doc.SourceURI,
				ContentDate: doc.ContentDate,
				Author:       doc.Author,
				FileType:     fileType,
				Checksum:     doc.Checksum,
			})
		}
	}

	// Build deleted paths.
	var deletedPaths []model.IngestDeletedPath
	for _, p := range deleted {
		deletedPaths = append(deletedPaths, model.IngestDeletedPath{
			SourceType: conn.Name(),
			SourceName: conn.SourceName(),
			Path:       p,
		})
	}

	fmt.Fprintf(os.Stderr, "Pushing %d fragments to %s...\n", len(allFragments), remote)

	totalIngested := 0

	const maxPayloadBytes = 10 << 20
	batches, err := splitBySize(allFragments, deletedPaths, maxPayloadBytes)
	if err != nil {
		return fmt.Errorf("prepare batches: %w", err)
	}

	for i, bodyBytes := range batches {
		if ctx.Err() != nil {
			return ctx.Err()
		}

		if len(batches) > 1 {
			fmt.Fprintf(os.Stderr, "  Sending batch %d/%d...\n", i+1, len(batches))
		}

		req, err := http.NewRequestWithContext(ctx, http.MethodPost, remote+"/v1/ingest", bytes.NewReader(bodyBytes))
		if err != nil {
			return fmt.Errorf("create request: %w", err)
		}
		req.Header.Set("Content-Type", "application/json")

		resp, err := client.Do(req)
		if err != nil {
			return fmt.Errorf("push to remote: %w", err)
		}

		respBody, err := io.ReadAll(resp.Body)
		resp.Body.Close()
		if err != nil {
			return fmt.Errorf("read response: %w", err)
		}

		if resp.StatusCode != http.StatusOK {
			return fmt.Errorf("remote returned %d: %s", resp.StatusCode, string(respBody))
		}

		var result pushIngestResponse
		if err := json.Unmarshal(respBody, &result); err != nil {
			return fmt.Errorf("parse response: %w", err)
		}
		totalIngested += result.Ingested
	}

	// Track the ingested checksums locally so next ingest is incremental.
	if len(docs) > 0 {
		var trackFragments []model.SourceFragment
		for _, doc := range docs {
			trackFragments = append(trackFragments, model.SourceFragment{
				ID:           model.FragmentID(doc.SourceType, doc.Path, 0),
				RawContent:   "",
				SourceType:   doc.SourceType,
				SourceName:   doc.SourceName,
				SourcePath:   doc.Path,
				SourceURI:    doc.SourceURI,
				ContentDate: doc.ContentDate,
				Author:       doc.Author,
				FileType:     filepath.Ext(doc.Path),
				Checksum:     doc.Checksum,
			})
		}
		if err := s.UpsertFragments(ctx, trackFragments); err != nil {
			return fmt.Errorf("track checksums: %w", err)
		}
	}

	// Handle local deletions tracking.
	if len(deleted) > 0 {
		if err := s.DeleteByPaths(ctx, conn.Name(), conn.SourceName(), deleted); err != nil {
			return fmt.Errorf("track deletions: %w", err)
		}
	}

	fmt.Fprintf(os.Stderr, "Push complete: %d fragments ingested, %d paths deleted\n",
		totalIngested, len(deleted))
	return nil
}


func queryCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "query [text]",
		Short: "Query the knowledge base",
		Args:  cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg := config.Default()
			cfg.DBPath, _ = cmd.Flags().GetString("db")
			debugMode := isDebug(cmd)
			human, _ := cmd.Flags().GetBool("human")
			rawMode, _ := cmd.Flags().GetBool("raw")
			llmFlag, _ := cmd.Flags().GetString("llm")
			logger := newLogger(debugMode)
			client := httpClient(logger, debugMode)

			s, err := openStore(cfg)
			if err != nil {
				return fmt.Errorf("open store: %w", err)
			}
			defer s.Close()

			emb := newEmbedder(cfg, client)

			// When --raw is set, LLM is not needed.
			var llmClient query.LLM
			if !rawMode {
				llmClient = newLLMClient(cfg, llmFlag, client, logger)
				if llmClient == nil {
					return fmt.Errorf("synthesis mode requires ANTHROPIC_API_KEY. Set it in .env, or use --raw for retrieval without LLM")
				}
			}

			limit, _ := cmd.Flags().GetInt("limit")
			if limit <= 0 {
				limit = cfg.DefaultLimit
			}
			engine := query.NewEngine(s, emb, llmClient, limit, logger)
			engine.SetDiskCache(s)

			topicsRaw, _ := cmd.Flags().GetString("topics")
			var topics []string
			if topicsRaw != "" {
				for _, t := range strings.Split(topicsRaw, ",") {
					t = strings.TrimSpace(t)
					if t != "" {
						topics = append(topics, t)
					}
				}
			}

			sources, _ := cmd.Flags().GetStringArray("source")
			sourceTypes, _ := cmd.Flags().GetStringArray("source-type")

			question := strings.Join(args, " ")
			req := model.QueryRequest{
				Messages: []model.Message{
					{Role: model.RoleUser, Content: question},
				},
				Limit:       limit,
				Concise:     !human,
				Topics:      topics,
				Sources:     sources,
				SourceTypes: sourceTypes,
			}

			ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
			defer cancel()

			if err := emb.CheckHealth(ctx); err != nil {
				return err
			}

			if rawMode {
				return queryRaw(ctx, engine, req)
			}
			if human {
				return queryHuman(ctx, engine, req)
			}
			return queryCompact(ctx, engine, req)
		},
	}
	cmd.Flags().String("db", "kb.db", "Path to SQLite database")
	cmd.Flags().Int("limit", 0, "Max fragments to retrieve (default from KB_DEFAULT_LIMIT)")
	cmd.Flags().Bool("human", false, "Human-readable output (streamed text + formatted metadata)")
	cmd.Flags().Bool("raw", false, "Raw retrieval mode: return fragments as JSON without LLM synthesis (no API key needed)")
	cmd.Flags().String("topics", "", "Comma-separated topics to boost relevance (e.g., 'authentication,deployment')")
	cmd.Flags().StringArray("source", nil, "Filter results to this source name (repeatable, e.g., --source owner/repo)")
	cmd.Flags().StringArray("source-type", nil, "Filter results to this source type (repeatable: filesystem, git, confluence, slack, github_wiki)")
	cmd.Flags().String("llm", "", "LLM provider override: claude, openai, ollama (default from KB_LLM_PROVIDER or claude)")
	return cmd
}

// queryCompact outputs a single JSON object — optimised for AI consumption.
func queryCompact(ctx context.Context, engine *query.Engine, req model.QueryRequest) error {
	answer, err := engine.Query(ctx, req, nil)
	if err != nil {
		return err
	}

	out, _ := json.Marshal(answer)
	fmt.Println(string(out))
	return nil
}

// queryHuman streams the answer text and prints formatted metadata — for humans.
func queryHuman(ctx context.Context, engine *query.Engine, req model.QueryRequest) error {
	answer, err := engine.Query(ctx, req, func(text string) {
		fmt.Print(text)
	})
	if err != nil {
		return err
	}
	fmt.Println()

	fmt.Fprintf(os.Stderr, "\n--- Confidence ---\n")
	fmt.Fprintf(os.Stderr, "Overall:       %.2f\n", answer.Confidence.Overall)
	fmt.Fprintf(os.Stderr, "Freshness:     %.2f\n", answer.Confidence.Breakdown.Freshness)
	fmt.Fprintf(os.Stderr, "Corroboration: %.2f\n", answer.Confidence.Breakdown.Corroboration)
	fmt.Fprintf(os.Stderr, "Consistency:   %.2f\n", answer.Confidence.Breakdown.Consistency)
	fmt.Fprintf(os.Stderr, "Authority:     %.2f\n", answer.Confidence.Breakdown.Authority)

	if len(answer.Sources) > 0 {
		fmt.Fprintf(os.Stderr, "\n--- Sources ---\n")
		for _, src := range answer.Sources {
			fmt.Fprintf(os.Stderr, "  [%s] %s\n", src.FragmentID, src.SourcePath)
		}
	}

	if len(answer.Contradictions) > 0 {
		fmt.Fprintf(os.Stderr, "\n--- Contradictions ---\n")
		for _, c := range answer.Contradictions {
			fmt.Fprintf(os.Stderr, "  %s: %s\n", c.Claim, c.Explanation)
		}
	}

	return nil
}

// queryRaw outputs raw retrieval results as JSON without LLM synthesis.
func queryRaw(ctx context.Context, engine *query.Engine, req model.QueryRequest) error {
	result, err := engine.QueryRaw(ctx, req)
	if err != nil {
		return err
	}

	out, _ := json.MarshalIndent(result, "", "  ")
	fmt.Println(string(out))
	return nil
}

func serveCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "serve",
		Short: "Start HTTP API server",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg := config.Default()
			cfg.DBPath, _ = cmd.Flags().GetString("db")
			cfg.ListenAddr, _ = cmd.Flags().GetString("addr")
			debugMode := isDebug(cmd)
			logger := newLogger(debugMode)
			client := httpClient(logger, debugMode)

			s, err := openStore(cfg)
			if err != nil {
				return fmt.Errorf("open store: %w", err)
			}
			defer s.Close()

			emb := newEmbedder(cfg, client)

			ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
			defer cancel()

			if err := emb.CheckHealth(ctx); err != nil {
				return err
			}

			llmClient := newLLMClient(cfg, "", client, logger)
			engine := query.NewEngine(s, emb, llmClient, cfg.DefaultLimit, logger)
			engine.SetDiskCache(s)

			httpServer := server.NewHTTPServer(engine, emb, s, logger)
			return httpServer.ListenAndServe(ctx, cfg.ListenAddr)
		},
	}
	cmd.Flags().String("addr", ":8080", "Listen address")
	cmd.Flags().String("db", "kb.db", "Path to SQLite database")
	return cmd
}

func mcpCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "mcp",
		Short: "Start MCP server (stdio + SSE)",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg := config.Default()
			cfg.DBPath, _ = cmd.Flags().GetString("db")
			addr, _ := cmd.Flags().GetString("addr")
			debugMode := isDebug(cmd)
			logger := newLogger(debugMode)
			client := httpClient(logger, debugMode)

			s, err := openStore(cfg)
			if err != nil {
				return fmt.Errorf("open store: %w", err)
			}
			defer s.Close()

			emb := newEmbedder(cfg, client)
			if err := emb.CheckHealth(context.Background()); err != nil {
				return err
			}

			// LLM client — synthesis is the default; raw mode is opt-in via raw=true parameter.
			llmClient := newLLMClient(cfg, "", client, logger)
			engine := query.NewEngine(s, emb, llmClient, cfg.DefaultLimit, logger)
			engine.SetDiskCache(s)

			ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
			defer cancel()

			mcpServer := server.NewMCPServer(engine, s, logger)
			return mcpServer.Serve(ctx, addr)
		},
	}
	cmd.Flags().String("db", "kb.db", "Path to SQLite database")
	cmd.Flags().String("addr", ":8082", "SSE listen address")
	return cmd
}

// sanitizeTSV replaces tab and newline characters with spaces.
func sanitizeTSV(s string) string {
	r := strings.NewReplacer("\t", " ", "\n", " ", "\r", " ")
	return r.Replace(s)
}

func exportCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "export",
		Short: "Export fragment embeddings for TensorBoard Embedding Projector",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg := config.Default()
			cfg.DBPath, _ = cmd.Flags().GetString("db")

			s, err := openStore(cfg)
			if err != nil {
				return fmt.Errorf("open store: %w", err)
			}
			defer s.Close()

			fragments, err := s.ExportFragments(context.Background())
			if err != nil {
				return fmt.Errorf("export fragments: %w", err)
			}

			if len(fragments) == 0 {
				fmt.Fprintln(os.Stderr, "No fragments with embeddings found.")
				return nil
			}

			outDir, _ := cmd.Flags().GetString("out")

			// Write tensors.tsv
			tensorsPath := filepath.Join(outDir, "tensors.tsv")
			tf, err := os.Create(tensorsPath)
			if err != nil {
				return fmt.Errorf("create tensors.tsv: %w", err)
			}
			tw := bufio.NewWriter(tf)
			for _, f := range fragments {
				for i, v := range f.Embedding {
					if i > 0 {
						tw.WriteByte('\t')
					}
					tw.WriteString(strconv.FormatFloat(float64(v), 'f', 6, 32))
				}
				tw.WriteByte('\n')
			}
			if err := tw.Flush(); err != nil {
				tf.Close()
				return fmt.Errorf("write tensors.tsv: %w", err)
			}
			if err := tf.Close(); err != nil {
				return fmt.Errorf("close tensors.tsv: %w", err)
			}

			// Write metadata.tsv
			metadataPath := filepath.Join(outDir, "metadata.tsv")
			mf, err := os.Create(metadataPath)
			if err != nil {
				return fmt.Errorf("create metadata.tsv: %w", err)
			}
			mw := bufio.NewWriter(mf)
			mw.WriteString("id\tsource_name\tsource_path\tsource_type\tfile_type\tauthor\tcontent_date\n")
			for _, f := range fragments {
				fmt.Fprintf(mw, "%s\t%s\t%s\t%s\t%s\t%s\t%s\n",
					sanitizeTSV(f.ID),
					sanitizeTSV(f.SourceName),
					sanitizeTSV(f.SourcePath),
					sanitizeTSV(f.SourceType),
					sanitizeTSV(f.FileType),
					sanitizeTSV(f.Author),
					sanitizeTSV(f.ContentDate.Format("2006-01-02T15:04:05Z")),
				)
			}
			if err := mw.Flush(); err != nil {
				mf.Close()
				return fmt.Errorf("write metadata.tsv: %w", err)
			}
			if err := mf.Close(); err != nil {
				return fmt.Errorf("close metadata.tsv: %w", err)
			}

			fmt.Fprintf(os.Stderr, "Exported %d fragments to %s and %s\n",
				len(fragments), tensorsPath, metadataPath)
			return nil
		},
	}
	cmd.Flags().String("db", "kb.db", "Path to SQLite database")
	cmd.Flags().String("out", ".", "Output directory for TSV files")
	return cmd
}

type pushIngestResponse struct {
	Ingested int `json:"ingested"`
}

// splitBySize splits fragments into batches that fit within maxBytes when
// JSON-encoded. Each returned []byte is a pre-marshaled JSON request body
// ready to POST. Deletions are included only in the first batch. Most repos
// will produce a single batch.
func splitBySize(fragments []model.IngestFragment, deleted []model.IngestDeletedPath, maxBytes int) ([][]byte, error) {
	// Try everything in one request first.
	all := model.IngestRequest{Fragments: fragments, Deleted: deleted}
	data, err := json.Marshal(all)
	if err != nil {
		return nil, fmt.Errorf("marshal ingest request: %w", err)
	}
	if len(data) <= maxBytes {
		return [][]byte{data}, nil
	}

	// Estimate per-fragment size and split accordingly.
	avgSize := len(data) / max(len(fragments), 1)
	perBatch := max(maxBytes/max(avgSize, 1), 1)

	var batches [][]byte
	for i := 0; i < len(fragments); i += perBatch {
		end := i + perBatch
		if end > len(fragments) {
			end = len(fragments)
		}
		batch := model.IngestRequest{Fragments: fragments[i:end]}
		if i == 0 {
			batch.Deleted = deleted
		}
		batchBytes, err := json.Marshal(batch)
		if err != nil {
			return nil, fmt.Errorf("marshal ingest batch: %w", err)
		}
		batches = append(batches, batchBytes)
	}
	return batches, nil
}

func evalCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "eval",
		Short: "Run retrieval evaluation against a test set",
		RunE: func(cmd *cobra.Command, args []string) error {
			dbPath, _ := cmd.Flags().GetString("db")
			testsetPath, _ := cmd.Flags().GetString("testset")
			corpusPath, _ := cmd.Flags().GetString("corpus")
			limit, _ := cmd.Flags().GetInt("limit")
			doIngest, _ := cmd.Flags().GetBool("ingest")
			jsonOutput, _ := cmd.Flags().GetBool("json")
			skipEnrichment, _ := cmd.Flags().GetBool("skip-enrichment")
			enrichModel, _ := cmd.Flags().GetString("enrich-model")
			promptVersion, _ := cmd.Flags().GetString("prompt-version")
			debugMode := isDebug(cmd)
			logger := newLogger(debugMode)
			client := httpClient(logger, debugMode)

			cfg := config.Default()
			cfg.DBPath = dbPath

			s, err := openStore(cfg)
			if err != nil {
				return fmt.Errorf("open store: %w", err)
			}
			defer s.Close()

			emb := newEmbedder(cfg, client)

			ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
			defer cancel()

			// Optionally ingest the eval corpus first.
			var enrichTimeMS int64
			if doIngest {
				absCorpus, err := filepath.Abs(corpusPath)
				if err != nil {
					return fmt.Errorf("resolve corpus path: %w", err)
				}
				fmt.Fprintf(os.Stderr, "Ingesting eval corpus from %s...\n", absCorpus)

				reg := newExtractorRegistry(cfg)
				pipeline := ingest.NewPipeline(s, emb, reg, cfg.WorkerCount, logger)
				configureEnrichment(pipeline, cfg, client, logger, skipEnrichment, enrichModel, promptVersion)
				conn := connector.NewFilesystemConnector(absCorpus)

				result, err := pipeline.Run(ctx, conn)
				if err != nil {
					return fmt.Errorf("ingest eval corpus: %w", err)
				}
				fmt.Fprintf(os.Stderr, "Ingested: %d added, %d deleted, %d skipped, %d errors\n",
					result.Added, result.Deleted, result.Skipped, result.Errors)
				enrichTimeMS = result.EnrichmentTimeMS
				if enrichTimeMS > 0 {
					fmt.Fprintf(os.Stderr, "Enrichment time: %dms (%.1fs)\n", enrichTimeMS, float64(enrichTimeMS)/1000)
				}
			}

			noSave, _ := cmd.Flags().GetBool("no-save")

			// Resolve the effective enrichment model name for filenames and metadata.
			effectiveEnrichModel := enrichModel
			if !skipEnrichment && effectiveEnrichModel == "" {
				effectiveEnrichModel = cfg.EnrichModel
			}

			// Load previous results for delta comparison.
			resultsFile := "results.json"
			if !skipEnrichment {
				if enrichModel != "" {
					resultsFile = fmt.Sprintf("results-enriched-%s.json", enrichModel)
				} else {
					resultsFile = "results-enriched.json"
				}
				// Include prompt version in filename only when explicitly overridden.
				if promptVersion != "" {
					resultsFile = strings.TrimSuffix(resultsFile, ".json") + "-" + promptVersion + ".json"
				}
			}
			resultsPath := filepath.Join(filepath.Dir(testsetPath), resultsFile)
			var previous *eval.Summary
			if prev, err := eval.LoadResults(resultsPath); err == nil {
				previous = prev
				fmt.Fprintf(os.Stderr, "Loaded previous results from %s\n", resultsPath)
			}

			// Load test set.
			cases, err := eval.LoadTestSet(testsetPath)
			if err != nil {
				return fmt.Errorf("load test set: %w", err)
			}
			fmt.Fprintf(os.Stderr, "Loaded %d test cases from %s\n", len(cases), testsetPath)

			// Run evaluation.
			runner := eval.NewRunner(s, emb)
			summary, err := runner.Run(ctx, cases, limit)
			if err != nil {
				return fmt.Errorf("run eval: %w", err)
			}

			// Compute chunking stats.
			chunkStats, err := eval.ComputeChunkingStats(ctx, s)
			if err != nil {
				fmt.Fprintf(os.Stderr, "Warning: could not compute chunking stats: %v\n", err)
			} else {
				summary.Chunking = chunkStats
			}

			// Populate enrichment, embedding, and system metadata.
			summary.EmbeddingModel = cfg.EmbeddingModel
			sysInfo := eval.GetSystemInfo()
			summary.System = &sysInfo
			if !skipEnrichment {
				summary.EnrichmentModel = effectiveEnrichModel
				summary.EnrichmentVersion = enrich.PromptVersion
				summary.EnrichmentTimeMS = enrichTimeMS
			}

			if jsonOutput {
				out, _ := json.MarshalIndent(summary, "", "  ")
				fmt.Println(string(out))
			} else {
				fmt.Print(eval.FormatSummaryTableWithDelta(summary, previous))
			}

			// Auto-save results unless --no-save.
			if !noSave {
				if err := eval.SaveResults(summary, resultsPath); err != nil {
					fmt.Fprintf(os.Stderr, "Warning: could not save results: %v\n", err)
				} else {
					fmt.Fprintf(os.Stderr, "Results saved to %s\n", resultsPath)
				}
			}

			return nil
		},
	}
	cmd.Flags().String("db", "kb.db", "Path to SQLite database")
	cmd.Flags().String("testset", "eval/testset.json", "Path to test set JSON file")
	cmd.Flags().String("corpus", "eval/corpus", "Path to eval corpus directory")
	cmd.Flags().Int("limit", 20, "Max fragments to retrieve per query")
	cmd.Flags().Bool("ingest", false, "Ingest the eval corpus before running evaluation")
	cmd.Flags().Bool("json", false, "Output results as JSON")
	cmd.Flags().Bool("no-save", false, "Do not save results to results.json")
	cmd.Flags().Bool("skip-enrichment", false, "Skip LLM chunk enrichment during eval ingestion")
	cmd.Flags().String("enrich-model", "", "Ollama model for chunk enrichment (default: qwen2.5:0.5b)")
	cmd.Flags().String("prompt-version", "", "Enrichment prompt version: v1 (full rewrite), v2 (append keywords)")
	return cmd
}

func sourcesCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "sources",
		Short: "Manage registered sources",
	}
	cmd.PersistentFlags().String("db", "kb.db", "Path to SQLite database")
	cmd.AddCommand(sourcesListCmd())
	cmd.AddCommand(sourcesRemoveCmd())
	cmd.AddCommand(sourcesDescribeCmd())
	cmd.AddCommand(sourcesExportCmd())
	cmd.AddCommand(sourcesImportCmd())
	return cmd
}

func sourcesListCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List registered sources",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg := config.Default()
			cfg.DBPath, _ = cmd.Flags().GetString("db")

			s, err := openStore(cfg)
			if err != nil {
				return fmt.Errorf("open store: %w", err)
			}
			defer s.Close()

			sources, err := s.ListSources(context.Background())
			if err != nil {
				return fmt.Errorf("list sources: %w", err)
			}

			if len(sources) == 0 {
				fmt.Fprintln(os.Stderr, "No sources registered.")
				return nil
			}

			out, _ := json.MarshalIndent(sources, "", "  ")
			fmt.Println(string(out))
			return nil
		},
	}
}

func sourcesDescribeCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "describe <type/name> <description>",
		Short: "Set description for an existing source",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg := config.Default()
			cfg.DBPath, _ = cmd.Flags().GetString("db")

			s, err := openStore(cfg)
			if err != nil {
				return fmt.Errorf("open store: %w", err)
			}
			defer s.Close()

			parts := strings.SplitN(args[0], "/", 2)
			if len(parts) != 2 {
				return fmt.Errorf("source must be in type/name format (e.g. git/myrepo)")
			}

			force, _ := cmd.Flags().GetBool("force")

			if err := s.UpdateSourceDescription(context.Background(), parts[0], parts[1], args[1], force); err != nil {
				return err
			}

			fmt.Fprintf(os.Stderr, "Updated description for %s\n", args[0])
			return nil
		},
	}
	cmd.Flags().Bool("force", false, "Overwrite existing description")
	return cmd
}

func sourcesExportCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "export [file]",
		Short: "Export registered sources to a JSON file",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			outFile := "sources.json"
			if len(args) > 0 {
				outFile = args[0]
			}

			cfg := config.Default()
			cfg.DBPath, _ = cmd.Flags().GetString("db")

			s, err := openStore(cfg)
			if err != nil {
				return fmt.Errorf("open store: %w", err)
			}
			defer s.Close()

			sources, err := s.ListSources(context.Background())
			if err != nil {
				return fmt.Errorf("list sources: %w", err)
			}

			// Use a local struct to omit last_ingest from export.
			type exportSource struct {
				SourceType  string            `json:"source_type"`
				SourceName  string            `json:"source_name"`
				Description string            `json:"description,omitempty"`
				Config      map[string]string `json:"config"`
			}

			// Build map of current DB sources keyed by "type/name".
			dbMap := make(map[string]exportSource, len(sources))
			for _, src := range sources {
				key := src.SourceType + "/" + src.SourceName
				dbMap[key] = exportSource{
					SourceType:  src.SourceType,
					SourceName:  src.SourceName,
					Description: src.Description,
					Config:      src.Config,
				}
			}

			// Merge with existing file to avoid dropping entries.
			var existing []exportSource
			if existingData, err := os.ReadFile(outFile); err == nil {
				_ = json.Unmarshal(existingData, &existing)
			}

			merged := make(map[string]exportSource, len(existing)+len(dbMap))
			for _, e := range existing {
				key := e.SourceType + "/" + e.SourceName
				merged[key] = e
			}
			// DB sources override existing entries.
			for k, v := range dbMap {
				merged[k] = v
			}

			out := make([]exportSource, 0, len(merged))
			for _, v := range merged {
				out = append(out, v)
			}
			// Sort for deterministic output.
			sort.Slice(out, func(i, j int) bool {
				if out[i].SourceType != out[j].SourceType {
					return out[i].SourceType < out[j].SourceType
				}
				return out[i].SourceName < out[j].SourceName
			})

			data, err := json.MarshalIndent(out, "", "  ")
			if err != nil {
				return fmt.Errorf("marshal sources: %w", err)
			}

			if err := os.WriteFile(outFile, append(data, '\n'), 0644); err != nil {
				return fmt.Errorf("write file: %w", err)
			}

			added := len(merged) - len(existing)
			if added < 0 {
				added = 0
			}
			fmt.Fprintf(os.Stderr, "Exported %d sources to %s (%d new, %d total)\n",
				len(sources), outFile, added, len(out))
			return nil
		},
	}
}

func sourcesImportCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "import [file]",
		Short: "Import sources from a JSON file",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			inFile := "sources.json"
			if len(args) > 0 {
				inFile = args[0]
			}

			data, err := os.ReadFile(inFile)
			if err != nil {
				return fmt.Errorf("read file: %w", err)
			}

			var sources []model.Source
			if err := json.Unmarshal(data, &sources); err != nil {
				return fmt.Errorf("parse sources: %w", err)
			}

			cfg := config.Default()
			cfg.DBPath, _ = cmd.Flags().GetString("db")

			s, err := openStore(cfg)
			if err != nil {
				return fmt.Errorf("open store: %w", err)
			}
			defer s.Close()

			ctx := context.Background()
			for _, src := range sources {
				// Clear LastIngest so the source is treated as not-yet-ingested.
				src.LastIngest = time.Time{}
				if err := s.RegisterSource(ctx, src); err != nil {
					return fmt.Errorf("register source %s/%s: %w", src.SourceType, src.SourceName, err)
				}
			}

			fmt.Fprintf(os.Stderr, "Imported %d sources from %s\n", len(sources), inFile)
			return nil
		},
	}
}

func clusterCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "cluster",
		Short: "Run k-means clustering on fragment embeddings",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg := config.Default()
			cfg.DBPath, _ = cmd.Flags().GetString("db")
			k, _ := cmd.Flags().GetInt("k")

			s, err := openStore(cfg)
			if err != nil {
				return fmt.Errorf("open store: %w", err)
			}
			defer s.Close()

			ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
			defer cancel()

			clusters, err := cluster.RunClustering(ctx, s, k)
			if err != nil {
				return err
			}

			if len(clusters) == 0 {
				fmt.Fprintln(os.Stderr, "No fragments with embeddings found.")
				return nil
			}

			// Print summary table.
			fmt.Printf("%-8s  %-6s  %-30s  %-30s  %-10s\n", "CLUSTER", "SIZE", "TOPIC", "SOURCE", "CONFIDENCE")
			fmt.Printf("%-8s  %-6s  %-30s  %-30s  %-10s\n", "-------", "----", "-----", "------", "----------")
			for _, ci := range clusters {
				topic := ci.Topic
				if len(topic) > 30 {
					topic = topic[:27] + "..."
				}
				source := dominantSource(ci.Members)
				if len(source) > 30 {
					source = source[:27] + "..."
				}
				fmt.Printf("%-8d  %-6d  %-30s  %-30s  %.2f\n", ci.Index, len(ci.Members), topic, source, ci.Confidence.Overall)
			}

			return nil
		},
	}
	cmd.PersistentFlags().String("db", "kb.db", "Path to SQLite database")
	cmd.PersistentFlags().Int("k", 0, "Number of clusters (default: sqrt(n/2))")
	cmd.AddCommand(clusterVizCmd())
	return cmd
}

func clusterVizCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "viz",
		Short: "Generate interactive HTML cluster visualization",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg := config.Default()
			cfg.DBPath, _ = cmd.Flags().GetString("db")
			k, _ := cmd.Flags().GetInt("k")
			outPath, _ := cmd.Flags().GetString("out")

			s, err := openStore(cfg)
			if err != nil {
				return fmt.Errorf("open store: %w", err)
			}
			defer s.Close()

			ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
			defer cancel()

			slog.Info("starting cluster visualization...")

			clusters, err := cluster.RunClustering(ctx, s, k)
			if err != nil {
				return err
			}

			if len(clusters) == 0 {
				fmt.Fprintln(os.Stderr, "No fragments with embeddings found.")
				return nil
			}

			slog.Info("clustering done", "clusters", len(clusters))

			// Collect all embeddings and build VizPoints.
			var allEmb [][]float32
			type fragRef struct {
				clusterIdx int
				topic      string
				frag       model.SourceFragment
			}
			var refs []fragRef
			for _, ci := range clusters {
				for _, m := range ci.Members {
					allEmb = append(allEmb, m.Embedding)
					refs = append(refs, fragRef{clusterIdx: ci.Index, topic: ci.Topic, frag: m})
				}
			}

			slog.Info("projecting to 3D...", "points", len(allEmb))

			xs, ys, zs := cluster.PCA3D(allEmb)

			slog.Info("projection complete")

			points := make([]cluster.VizPoint, len(refs))
			for i, ref := range refs {
				snippet := ref.frag.RawContent
				if len(snippet) > 120 {
					snippet = snippet[:120]
				}
				points[i] = cluster.VizPoint{
					X:       xs[i],
					Y:       ys[i],
					Z:       zs[i],
					Cluster: ref.clusterIdx,
					Topic:   ref.topic,
					Source:  ref.frag.SourceType + "/" + ref.frag.SourceName,
					Path:    ref.frag.SourcePath,
					Snippet: snippet,
					ID:      ref.frag.ID,
				}
			}

			f, err := os.Create(outPath)
			if err != nil {
				return fmt.Errorf("create output file: %w", err)
			}
			defer f.Close()

			if err := cluster.GenerateVizHTML(points, f); err != nil {
				return fmt.Errorf("generate viz: %w", err)
			}

			fmt.Fprintf(os.Stderr, "Wrote %d points (%d clusters) to %s\n", len(points), len(clusters), outPath)
			return nil
		},
	}
	cmd.Flags().String("out", "clusters.html", "Output HTML file path")
	return cmd
}

// dominantSource returns the most common source_type/source_name among members.
func dominantSource(members []model.SourceFragment) string {
	counts := make(map[string]int)
	for _, m := range members {
		key := m.SourceType + "/" + m.SourceName
		counts[key]++
	}
	best, bestCount := "", 0
	for k, c := range counts {
		if c > bestCount || (c == bestCount && k < best) {
			best = k
			bestCount = c
		}
	}
	if len(counts) > 1 {
		return fmt.Sprintf("%s (+%d more)", best, len(counts)-1)
	}
	return best
}

func sourcesRemoveCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "remove <type/name>",
		Short: "Remove a registered source and all its fragments",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			parts := strings.SplitN(args[0], "/", 2)
			if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
				return fmt.Errorf("argument must be in the form <type>/<name>")
			}
			sourceType, sourceName := parts[0], parts[1]

			cfg := config.Default()
			cfg.DBPath, _ = cmd.Flags().GetString("db")

			s, err := openStore(cfg)
			if err != nil {
				return fmt.Errorf("open store: %w", err)
			}
			defer s.Close()

			ctx := context.Background()

			// Verify the source exists.
			sources, err := s.ListSources(ctx)
			if err != nil {
				return fmt.Errorf("list sources: %w", err)
			}
			found := false
			for _, src := range sources {
				if src.SourceType == sourceType && src.SourceName == sourceName {
					found = true
					break
				}
			}
			if !found {
				return fmt.Errorf("source %s/%s not found", sourceType, sourceName)
			}

			// Count fragments before deletion.
			counts, err := s.CountFragmentsBySource(ctx)
			if err != nil {
				return fmt.Errorf("count fragments: %w", err)
			}
			key := sourceType + "/" + sourceName
			fragCount := counts[key]

			// Delete fragments first, then the source registration.
			if err := s.DeleteFragmentsBySource(ctx, sourceType, sourceName); err != nil {
				return fmt.Errorf("delete fragments: %w", err)
			}
			if err := s.DeleteSource(ctx, sourceType, sourceName); err != nil {
				return fmt.Errorf("delete source: %w", err)
			}

			fmt.Fprintf(cmd.OutOrStdout(), "Removed source %s: deleted %d fragments\n", key, fragCount)
			return nil
		},
	}
}
