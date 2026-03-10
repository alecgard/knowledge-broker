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
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/spf13/cobra"

	"github.com/knowledge-broker/knowledge-broker/internal/config"
	"github.com/knowledge-broker/knowledge-broker/internal/connector"
	"github.com/knowledge-broker/knowledge-broker/internal/debug"
	"github.com/knowledge-broker/knowledge-broker/internal/embedding"
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
		Short: "Knowledge Broker — ingest documents, ask questions, get answers with confidence signals",
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
	return embedding.NewOllamaEmbedder(cfg.OllamaURL, cfg.OllamaModel, cfg.EmbeddingDim, client)
}

func newExtractorRegistry(cfg config.Config) *extractor.Registry {
	reg := extractor.NewRegistry(extractor.NewPlaintextExtractor(cfg.MaxChunkSize, cfg.ChunkOverlap))
	reg.Register(extractor.NewMarkdownExtractor(cfg.MaxChunkSize))
	reg.Register(extractor.NewCodeExtractor(cfg.MaxChunkSize))
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

			if all && remote != "" {
				return fmt.Errorf("--all and --remote cannot be combined")
			}

			s, err := openStore(cfg)
			if err != nil {
				return fmt.Errorf("open store: %w", err)
			}
			defer s.Close()

			reg := newExtractorRegistry(cfg)

			ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
			defer cancel()

			// --all: re-ingest all registered local sources.
			if all {
				emb := newEmbedder(cfg, client)
				if err := emb.CheckHealth(ctx); err != nil {
					return err
				}
				pipeline := ingest.NewPipeline(s, emb, reg, cfg.WorkerCount, logger)

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
				for _, src := range localSources {
					conn, err := connector.FromSource(src, cfg.GitHubClientID)
					if err != nil {
						return fmt.Errorf("reconstruct %s/%s: %w", src.SourceType, src.SourceName, err)
					}
					fmt.Fprintf(os.Stderr, "Re-ingesting %s/%s...\n", src.SourceType, src.SourceName)
					result, err := pipeline.Run(ctx, conn)
					if err != nil {
						return fmt.Errorf("ingest %s/%s: %w", src.SourceType, src.SourceName, err)
					}
					src.LastIngest = time.Now()
					if regErr := s.RegisterSource(ctx, src); regErr != nil {
						logger.Warn("failed to update source timestamp", "error", regErr)
					}
					fmt.Fprintf(os.Stderr, "  %s/%s: %d added, %d deleted, %d skipped, %d errors\n",
						src.SourceType, src.SourceName, result.Added, result.Deleted, result.Skipped, result.Errors)
				}
				return nil
			}

			// Build list of sources from flags.
			gitURLs, _ := cmd.Flags().GetStringArray("git")
			sourcePaths, _ := cmd.Flags().GetStringArray("source")

			var connectors []connector.Connector

			for _, u := range gitURLs {
				connectors = append(connectors, connector.NewGitConnector(u, "", cfg.GitHubClientID))
			}
			for _, p := range sourcePaths {
				connectors = append(connectors, connector.NewFilesystemConnector(p))
			}

			// Default: ingest current directory if no explicit flags.
			if len(connectors) == 0 {
				connectors = append(connectors, connector.NewFilesystemConnector("."))
			}

			remote = strings.TrimRight(remote, "/")

			if remote != "" {
				// Remote ingests run in parallel — each scans independently and POSTs to the server.
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
							SourceType: conn.Name(),
							SourceName: name,
							Config:     srcConfig,
							LastIngest: time.Now(),
						}); regErr != nil {
							logger.Warn("failed to register source", "error", regErr)
						}
					}(conn)
				}

				wg.Wait()
				close(errCh)

				var errs []string
				for err := range errCh {
					errs = append(errs, err.Error())
				}
				if len(errs) > 0 {
					return fmt.Errorf("%s", strings.Join(errs, "; "))
				}
				return nil
			}

			// Local ingests run sequentially (shared SQLite writer).
			emb := newEmbedder(cfg, client)
			if err := emb.CheckHealth(ctx); err != nil {
				return err
			}
			pipeline := ingest.NewPipeline(s, emb, reg, cfg.WorkerCount, logger)

			for _, conn := range connectors {
				name := conn.SourceName()
				fmt.Fprintf(os.Stderr, "Ingesting %s/%s...\n", conn.Name(), name)

				result, err := pipeline.Run(ctx, conn)
				if err != nil {
					return fmt.Errorf("ingest %s: %w", name, err)
				}

				srcConfig := conn.Config(model.SourceModeLocal)
				srcConfig["mode"] = model.SourceModeLocal
				fmt.Fprintf(os.Stderr, "  %s: %d added, %d deleted, %d skipped, %d errors\n",
					name, result.Added, result.Deleted, result.Skipped, result.Errors)

				if regErr := s.RegisterSource(ctx, model.Source{
					SourceType: conn.Name(),
					SourceName: name,
					Config:     srcConfig,
					LastIngest: time.Now(),
				}); regErr != nil {
					logger.Warn("failed to register source", "error", regErr)
				}
			}
			return nil
		},
	}
	cmd.Flags().StringArray("source", nil, "Local directory to ingest (repeatable)")
	cmd.Flags().StringArray("git", nil, "Git repo URL to ingest (repeatable)")
	cmd.Flags().String("db", "kb.db", "Path to SQLite database")
	cmd.Flags().String("remote", "", "URL of a remote KB server to push fragments to")
	cmd.Flags().Bool("all", false, "Re-ingest all registered local sources")
	return cmd
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
		chunks, err := ingest.ExtractChunks(doc, reg)
		if err != nil {
			logger.Warn("extract failed", "path", doc.Path, "error", err)
			continue
		}

		fileType := filepath.Ext(doc.Path)

		for _, chunk := range chunks {
			allFragments = append(allFragments, model.IngestFragment{
				Content:      chunk.Content,
				SourceType:   doc.SourceType,
				SourceName:   doc.SourceName,
				SourcePath:   doc.Path,
				SourceURI:    doc.SourceURI,
				LastModified: doc.LastModified,
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
				Content:      "",
				SourceType:   doc.SourceType,
				SourceName:   doc.SourceName,
				SourcePath:   doc.Path,
				SourceURI:    doc.SourceURI,
				LastModified: doc.LastModified,
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
		Use:   "query [question]",
		Short: "Ask a question",
		Args:  cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg := config.Default()
			cfg.DBPath, _ = cmd.Flags().GetString("db")
			debugMode := isDebug(cmd)
			human, _ := cmd.Flags().GetBool("human")
			rawMode, _ := cmd.Flags().GetBool("raw")
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
				llmClient = llm.NewClaudeClient(cfg.AnthropicAPIKey, cfg.ClaudeModel, client)
			}

			limit, _ := cmd.Flags().GetInt("limit")
			engine := query.NewEngine(s, emb, llmClient, limit)

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

			question := strings.Join(args, " ")
			req := model.QueryRequest{
				Messages: []model.Message{
					{Role: model.RoleUser, Content: question},
				},
				Limit:   limit,
				Concise: !human,
				Topics:  topics,
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
	cmd.Flags().Int("limit", 20, "Max fragments to retrieve")
	cmd.Flags().Bool("human", false, "Human-readable output (streamed text + formatted metadata)")
	cmd.Flags().Bool("raw", false, "Raw retrieval mode: return fragments as JSON without LLM synthesis (no API key needed)")
	cmd.Flags().String("topics", "", "Comma-separated topics to boost relevance (e.g., 'authentication,octroi')")
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
	fmt.Fprintf(os.Stderr, "Freshness:     %.2f\n", answer.Confidence.Freshness)
	fmt.Fprintf(os.Stderr, "Corroboration: %.2f\n", answer.Confidence.Corroboration)
	fmt.Fprintf(os.Stderr, "Consistency:   %.2f\n", answer.Confidence.Consistency)
	fmt.Fprintf(os.Stderr, "Authority:     %.2f\n", answer.Confidence.Authority)

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

			claude := llm.NewClaudeClient(cfg.AnthropicAPIKey, cfg.ClaudeModel, client)
			engine := query.NewEngine(s, emb, claude, cfg.DefaultLimit)

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
		Short: "Start MCP server on stdio",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg := config.Default()
			cfg.DBPath, _ = cmd.Flags().GetString("db")
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

			// LLM is optional for MCP — raw mode works without it.
			var llmClient query.LLM
			if cfg.AnthropicAPIKey != "" {
				llmClient = llm.NewClaudeClient(cfg.AnthropicAPIKey, cfg.ClaudeModel, client)
			}
			engine := query.NewEngine(s, emb, llmClient, cfg.DefaultLimit)

			mcpServer := server.NewMCPServer(engine, s, logger)
			return mcpServer.ServeStdio()
		},
	}
	cmd.Flags().String("db", "kb.db", "Path to SQLite database")
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
			mw.WriteString("id\tsource_name\tsource_path\tsource_type\tfile_type\tauthor\tlast_modified\n")
			for _, f := range fragments {
				fmt.Fprintf(mw, "%s\t%s\t%s\t%s\t%s\t%s\t%s\n",
					sanitizeTSV(f.ID),
					sanitizeTSV(f.SourceName),
					sanitizeTSV(f.SourcePath),
					sanitizeTSV(f.SourceType),
					sanitizeTSV(f.FileType),
					sanitizeTSV(f.Author),
					sanitizeTSV(f.LastModified.Format("2006-01-02T15:04:05Z")),
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
			if doIngest {
				absCorpus, err := filepath.Abs(corpusPath)
				if err != nil {
					return fmt.Errorf("resolve corpus path: %w", err)
				}
				fmt.Fprintf(os.Stderr, "Ingesting eval corpus from %s...\n", absCorpus)

				reg := newExtractorRegistry(cfg)
				pipeline := ingest.NewPipeline(s, emb, reg, cfg.WorkerCount, logger)
				conn := connector.NewFilesystemConnector(absCorpus)

				result, err := pipeline.Run(ctx, conn)
				if err != nil {
					return fmt.Errorf("ingest eval corpus: %w", err)
				}
				fmt.Fprintf(os.Stderr, "Ingested: %d added, %d deleted, %d skipped, %d errors\n",
					result.Added, result.Deleted, result.Skipped, result.Errors)
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

			if jsonOutput {
				out, _ := json.MarshalIndent(summary, "", "  ")
				fmt.Println(string(out))
			} else {
				fmt.Print(eval.FormatSummaryTable(summary))
			}

			return nil
		},
	}
	cmd.Flags().String("db", "kb.db", "Path to SQLite database")
	cmd.Flags().String("testset", "eval/testset.json", "Path to test set JSON file")
	cmd.Flags().String("corpus", "eval/corpus", "Path to eval corpus directory")
	cmd.Flags().Int("limit", 20, "Max fragments to retrieve per question")
	cmd.Flags().Bool("ingest", false, "Ingest the eval corpus before running evaluation")
	cmd.Flags().Bool("json", false, "Output results as JSON")
	return cmd
}

func sourcesCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "sources",
		Short: "Manage registered sources",
	}
	cmd.PersistentFlags().String("db", "kb.db", "Path to SQLite database")
	cmd.AddCommand(sourcesListCmd())
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
