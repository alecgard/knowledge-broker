package main

import (
	"bufio"
	"bytes"
	"context"
	"crypto/sha256"
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
	"syscall"
	"time"

	"github.com/spf13/cobra"

	"github.com/knowledge-broker/knowledge-broker/internal/config"
	"github.com/knowledge-broker/knowledge-broker/internal/connector"
	"github.com/knowledge-broker/knowledge-broker/internal/debug"
	"github.com/knowledge-broker/knowledge-broker/internal/embedding"
	"github.com/knowledge-broker/knowledge-broker/internal/extractor"
	"github.com/knowledge-broker/knowledge-broker/internal/feedback"
	"github.com/knowledge-broker/knowledge-broker/internal/ingest"
	"github.com/knowledge-broker/knowledge-broker/internal/llm"
	"github.com/knowledge-broker/knowledge-broker/internal/model"
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
	root.AddCommand(feedbackCmd())
	root.AddCommand(exportCmd())
	root.AddCommand(pushCmd())

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
		Short: "Ingest documents from a source",
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
			reg := newExtractorRegistry(cfg)
			pipeline := ingest.NewPipeline(s, emb, reg, cfg.WorkerCount, logger)

			// Determine which connector to use.
			source, _ := cmd.Flags().GetString("source")
			gitURL, _ := cmd.Flags().GetString("git")

			var conn connector.Connector
			if gitURL != "" {
				conn = connector.NewGitConnector(gitURL, "", cfg.GitHubClientID)
			} else {
				conn = connector.NewFilesystemConnector(source)
			}

			ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
			defer cancel()

			result, err := pipeline.Run(ctx, conn)
			if err != nil {
				return err
			}

			fmt.Fprintf(os.Stderr, "Ingestion complete: %d added, %d deleted, %d skipped, %d errors\n",
				result.Added, result.Deleted, result.Skipped, result.Errors)
			return nil
		},
	}
	cmd.Flags().String("source", ".", "Local directory to ingest")
	cmd.Flags().String("git", "", "Git repo URL to ingest (any remote)")
	cmd.Flags().String("db", "kb.db", "Path to SQLite database")
	return cmd
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
			logger := newLogger(debugMode)
			client := httpClient(logger, debugMode)

			s, err := openStore(cfg)
			if err != nil {
				return fmt.Errorf("open store: %w", err)
			}
			defer s.Close()

			emb := newEmbedder(cfg, client)
			claude := llm.NewClaudeClient(cfg.AnthropicAPIKey, cfg.ClaudeModel, client)

			limit, _ := cmd.Flags().GetInt("limit")
			engine := query.NewEngine(s, emb, claude, limit)

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
					{Role: "user", Content: question},
				},
				Limit:   limit,
				Concise: !human,
				Topics:  topics,
			}

			ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
			defer cancel()

			if human {
				return queryHuman(ctx, engine, req)
			}
			return queryCompact(ctx, engine, req)
		},
	}
	cmd.Flags().String("db", "kb.db", "Path to SQLite database")
	cmd.Flags().Int("limit", 20, "Max fragments to retrieve")
	cmd.Flags().Bool("human", false, "Human-readable output (streamed text + formatted metadata)")
	cmd.Flags().String("topics", "", "Comma-separated topics to boost relevance (e.g., 'authentication,octroi')")
	return cmd
}

// queryCompact outputs a single JSON object — optimised for AI consumption.
func queryCompact(ctx context.Context, engine *query.Engine, req model.QueryRequest) error {
	var fullText string
	answer, err := engine.Query(ctx, req, func(text string) {
		fullText += text
	})
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
			claude := llm.NewClaudeClient(cfg.AnthropicAPIKey, cfg.ClaudeModel, client)
			engine := query.NewEngine(s, emb, claude, cfg.DefaultLimit)
			fbService := feedback.NewService(s)

			ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
			defer cancel()

			httpServer := server.NewHTTPServer(engine, fbService, emb, s, logger)
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
			claude := llm.NewClaudeClient(cfg.AnthropicAPIKey, cfg.ClaudeModel, client)
			engine := query.NewEngine(s, emb, claude, cfg.DefaultLimit)
			fbService := feedback.NewService(s)

			mcpServer := server.NewMCPServer(engine, fbService, logger)
			return mcpServer.ServeStdio()
		},
	}
	cmd.Flags().String("db", "kb.db", "Path to SQLite database")
	return cmd
}

func feedbackCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "feedback",
		Short: "Submit feedback on a fragment",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg := config.Default()
			cfg.DBPath, _ = cmd.Flags().GetString("db")

			s, err := openStore(cfg)
			if err != nil {
				return fmt.Errorf("open store: %w", err)
			}
			defer s.Close()

			fragmentID, _ := cmd.Flags().GetString("fragment-id")
			fbType, _ := cmd.Flags().GetString("type")
			content, _ := cmd.Flags().GetString("content")
			evidence, _ := cmd.Flags().GetString("evidence")

			fb := model.Feedback{
				FragmentID: fragmentID,
				Type:       model.FeedbackType(fbType),
				Content:    content,
				Evidence:   evidence,
			}

			fbService := feedback.NewService(s)
			if err := fbService.Submit(context.Background(), fb); err != nil {
				return err
			}

			result, _ := json.Marshal(map[string]string{"status": "ok", "fragment_id": fragmentID})
			fmt.Println(string(result))
			return nil
		},
	}
	cmd.Flags().String("fragment-id", "", "Fragment ID to give feedback on")
	cmd.Flags().String("type", "", "Feedback type: correction, challenge, confirmation")
	cmd.Flags().String("content", "", "Feedback content (for corrections)")
	cmd.Flags().String("evidence", "", "Supporting evidence")
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

// pushIngestRequest matches server.IngestRequest for JSON serialization.
type pushIngestRequest struct {
	Fragments []pushFragment  `json:"fragments"`
	Deleted   []pushDeletedPath `json:"deleted,omitempty"`
}

type pushFragment struct {
	Content      string    `json:"content"`
	SourceType   string    `json:"source_type"`
	SourceName   string    `json:"source_name,omitempty"`
	SourcePath   string    `json:"source_path"`
	SourceURI    string    `json:"source_uri"`
	LastModified time.Time `json:"last_modified"`
	Author       string    `json:"author"`
	FileType     string    `json:"file_type"`
	Checksum     string    `json:"checksum"`
}

type pushDeletedPath struct {
	SourceType string `json:"source_type"`
	Path       string `json:"path"`
}

type pushIngestResponse struct {
	Ingested int `json:"ingested"`
}

// splitBySize splits fragments into batches that fit within maxBytes when JSON-encoded.
// Deletions are included only in the first batch. Most repos will produce a single batch.
func splitBySize(fragments []pushFragment, deleted []pushDeletedPath, maxBytes int) []pushIngestRequest {
	// Try everything in one request first.
	all := pushIngestRequest{Fragments: fragments, Deleted: deleted}
	data, err := json.Marshal(all)
	if err != nil || len(data) <= maxBytes {
		return []pushIngestRequest{all}
	}

	// Estimate per-fragment size and split accordingly.
	avgSize := len(data) / max(len(fragments), 1)
	perBatch := max(maxBytes/max(avgSize, 1), 1)

	var batches []pushIngestRequest
	for i := 0; i < len(fragments); i += perBatch {
		end := i + perBatch
		if end > len(fragments) {
			end = len(fragments)
		}
		batch := pushIngestRequest{Fragments: fragments[i:end]}
		if i == 0 {
			batch.Deleted = deleted
		}
		batches = append(batches, batch)
	}
	return batches
}

func pushCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "push",
		Short: "Push documents to a remote KB server",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg := config.Default()
			dbPath, _ := cmd.Flags().GetString("db")
			cfg.DBPath = dbPath
			remote, _ := cmd.Flags().GetString("remote")
			source, _ := cmd.Flags().GetString("source")
			gitURL, _ := cmd.Flags().GetString("git")
			debugMode := isDebug(cmd)
			logger := newLogger(debugMode)

			if remote == "" {
				return fmt.Errorf("--remote is required")
			}
			remote = strings.TrimRight(remote, "/")

			// Open a local store for tracking checksums (incremental ingestion).
			s, err := openStore(cfg)
			if err != nil {
				return fmt.Errorf("open store: %w", err)
			}
			defer s.Close()

			reg := newExtractorRegistry(cfg)

			// Determine which connector to use.
			var conn connector.Connector
			if gitURL != "" {
				conn = connector.NewGitConnector(gitURL, "", cfg.GitHubClientID)
			} else {
				conn = connector.NewFilesystemConnector(source)
			}

			ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
			defer cancel()

			// Get known checksums for incremental ingestion.
			known, err := s.GetChecksums(ctx, conn.Name())
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
			var allFragments []pushFragment
			for _, doc := range docs {
				// Use pre-provided chunks when available; otherwise run the extractor.
				var chunks []model.Chunk
				if len(doc.Chunks) > 0 {
					chunks = doc.Chunks
				} else {
					ext := filepath.Ext(doc.Path)
					e := reg.Get(ext)
					var err error
					chunks, err = e.Extract(doc.Content, extractor.ExtractOptions{Path: doc.Path})
					if err != nil {
						logger.Warn("extract failed", "path", doc.Path, "error", err)
						continue
					}
				}

				// Derive file type from path extension.
				fileType := filepath.Ext(doc.Path)

				for i, chunk := range chunks {
					_ = i // chunk index used for ID generation on server side
					allFragments = append(allFragments, pushFragment{
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
			var deletedPaths []pushDeletedPath
			for _, p := range deleted {
				deletedPaths = append(deletedPaths, pushDeletedPath{
					SourceType: conn.Name(),
					Path:       p,
				})
			}

			fmt.Fprintf(os.Stderr, "Pushing %d fragments to %s...\n", len(allFragments), remote)

			totalIngested := 0
			httpCl := &http.Client{Timeout: 300 * time.Second}

			// Split into batches only if the payload exceeds 10MB.
			const maxPayloadBytes = 10 << 20
			batches := splitBySize(allFragments, deletedPaths, maxPayloadBytes)

			for i, batch := range batches {
				if ctx.Err() != nil {
					return ctx.Err()
				}

				bodyBytes, err := json.Marshal(batch)
				if err != nil {
					return fmt.Errorf("marshal request: %w", err)
				}

				if len(batches) > 1 {
					fmt.Fprintf(os.Stderr, "  Sending batch %d/%d (%d fragments)...\n", i+1, len(batches), len(batch.Fragments))
				}

				req, err := http.NewRequestWithContext(ctx, http.MethodPost, remote+"/v1/ingest", bytes.NewReader(bodyBytes))
				if err != nil {
					return fmt.Errorf("create request: %w", err)
				}
				req.Header.Set("Content-Type", "application/json")

				resp, err := httpCl.Do(req)
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

			// Track the ingested checksums locally so next push is incremental.
			// We store minimal fragments (just source_path and checksum) to track state.
			if len(docs) > 0 {
				var trackFragments []model.SourceFragment
				for _, doc := range docs {
					// Generate a tracking fragment ID based on source + path.
					idInput := fmt.Sprintf("%s:%s:0", doc.SourceType, doc.Path)
					id := fmt.Sprintf("%x", sha256.Sum256([]byte(idInput)))[:16]

					trackFragments = append(trackFragments, model.SourceFragment{
						ID:           id,
						Content:      "", // No content needed for tracking
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
				if err := s.DeleteByPaths(ctx, conn.Name(), deleted); err != nil {
					return fmt.Errorf("track deletions: %w", err)
				}
			}

			fmt.Fprintf(os.Stderr, "Push complete: %d fragments ingested, %d paths deleted\n",
				totalIngested, len(deleted))
			return nil
		},
	}
	cmd.Flags().String("remote", "", "URL of the central KB server")
	cmd.Flags().String("source", ".", "Local directory to ingest")
	cmd.Flags().String("git", "", "Git repo URL to ingest (any remote)")
	cmd.Flags().String("db", "kb.db", "Local DB path for tracking checksums")
	_ = cmd.MarkFlagRequired("remote")
	return cmd
}
