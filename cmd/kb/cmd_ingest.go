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
	"os/signal"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/spf13/cobra"

	"github.com/knowledge-broker/knowledge-broker/internal/connector"
	"github.com/knowledge-broker/knowledge-broker/internal/embedding"
	"github.com/knowledge-broker/knowledge-broker/internal/enrich"
	"github.com/knowledge-broker/knowledge-broker/internal/extractor"
	"github.com/knowledge-broker/knowledge-broker/internal/ingest"
	"github.com/knowledge-broker/knowledge-broker/internal/store"
	"github.com/knowledge-broker/knowledge-broker/pkg/model"
)

func ingestCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "ingest",
		Short: "Ingest documents from one or more sources",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg := loadConfig(cmd).Config
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
			forceMode, _ := cmd.Flags().GetBool("force")

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
				if err := ensureOllama(ctx, cmd, cfg, true); err != nil {
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
				if err := ensureOllama(ctx, cmd, cfg, true); err != nil {
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
							r, err := pipeline.Run(ctx, conn, ingest.Options{Force: forceMode})
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
						r, err := pipeline.Run(ctx, conn, ingest.Options{Force: forceMode})
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
					return fmt.Errorf("--confluence requires KB_CONFLUENCE_BASE_URL, KB_CONFLUENCE_EMAIL, and KB_CONFLUENCE_TOKEN (set in environment or .env file)")
				}
				connectors = append(connectors, connector.NewConfluenceConnector(baseURL, space, email, token))
			}
			if len(slackChannels) > 0 {
				token := os.Getenv("KB_SLACK_TOKEN")
				if token == "" {
					return fmt.Errorf("--slack requires KB_SLACK_TOKEN (set in environment or .env file)")
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
			if err := ensureOllama(ctx, cmd, cfg, true); err != nil {
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

						srcConfig := conn.Config(model.SourceModeLocal)
						srcConfig["mode"] = model.SourceModeLocal

						// Register source immediately so it appears in kb sources list.
						if regErr := s.RegisterSource(ctx, model.Source{
							SourceType:  conn.Name(),
							SourceName:  name,
							Description: description,
							Config:      srcConfig,
						}); regErr != nil {
							logger.Warn("failed to register source", "error", regErr)
						}

						srcLabel := conn.Name() + "/" + name
						pipeline := ingest.NewPipeline(s, emb, reg, cfg.WorkerCount, logger)
						configureEnrichment(pipeline, cfg, client, logger, skipEnrichment, enrichModel, promptVersion)
						pipeline.OnProgress = makeProgressFunc(srcLabel, true)
						pipeline.OnBatchDone = makeBatchFunc()
						r, err := pipeline.Run(ctx, conn, ingest.Options{Force: forceMode})
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

					// Update last_ingest timestamp on success.
					if regErr := s.RegisterSource(ctx, model.Source{
						SourceType:  ir.connType,
						SourceName:  ir.name,
						Description: ir.description,
						Config:      ir.config,
						LastIngest:  time.Now(),
					}); regErr != nil {
						logger.Warn("failed to update source timestamp", "error", regErr)
					}
				}
			} else {
				for _, conn := range connectors {
					name := conn.SourceName()
					fmt.Fprintf(os.Stderr, "Ingesting %s/%s...\n", conn.Name(), name)

					srcConfig := conn.Config(model.SourceModeLocal)
					srcConfig["mode"] = model.SourceModeLocal

					// Register source immediately so it appears in kb sources list.
					if regErr := s.RegisterSource(ctx, model.Source{
						SourceType:  conn.Name(),
						SourceName:  name,
						Description: description,
						Config:      srcConfig,
					}); regErr != nil {
						logger.Warn("failed to register source", "error", regErr)
					}

					srcLabel := conn.Name() + "/" + name
					pipeline := ingest.NewPipeline(s, emb, reg, cfg.WorkerCount, logger)
					configureEnrichment(pipeline, cfg, client, logger, skipEnrichment, enrichModel, promptVersion)
					pipeline.OnProgress = makeProgressFunc(srcLabel, false)
					pipeline.OnBatchDone = makeBatchFunc()
					r, err := pipeline.Run(ctx, conn, ingest.Options{Force: forceMode})
					if err != nil {
						errs = append(errs, fmt.Sprintf("ingest %s: %v", name, err))
						continue
					}

					fmt.Fprintf(os.Stderr, "  %s: %d added, %d deleted, %d skipped, %d errors\n",
						name, r.Added, r.Deleted, r.Skipped, r.Errors)

					// Update last_ingest timestamp on success.
					if regErr := s.RegisterSource(ctx, model.Source{
						SourceType:  conn.Name(),
						SourceName:  name,
						Description: description,
						Config:      srcConfig,
						LastIngest:  time.Now(),
					}); regErr != nil {
						logger.Warn("failed to update source timestamp", "error", regErr)
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
				Content:     chunk.Content,
				SourceType:  doc.SourceType,
				SourceName:  doc.SourceName,
				SourcePath:  doc.Path,
				SourceURI:   doc.SourceURI,
				ContentDate: doc.ContentDate,
				Author:      doc.Author,
				FileType:    fileType,
				Checksum:    doc.Checksum,
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
				ID:          model.FragmentID(doc.SourceType, doc.Path, 0),
				RawContent:  "",
				SourceType:  doc.SourceType,
				SourceName:  doc.SourceName,
				SourcePath:  doc.Path,
				SourceURI:   doc.SourceURI,
				ContentDate: doc.ContentDate,
				Author:      doc.Author,
				FileType:    filepath.Ext(doc.Path),
				Checksum:    doc.Checksum,
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
