package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"

	"github.com/spf13/cobra"

	"github.com/knowledge-broker/knowledge-broker/internal/connector"
	"github.com/knowledge-broker/knowledge-broker/internal/enrich"
	"github.com/knowledge-broker/knowledge-broker/internal/eval"
	"github.com/knowledge-broker/knowledge-broker/internal/ingest"
	"github.com/knowledge-broker/knowledge-broker/internal/llm"
	"github.com/knowledge-broker/knowledge-broker/internal/query"
)

func evalCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "eval",
		Short: "Run retrieval evaluation against a test set",
		RunE: func(cmd *cobra.Command, args []string) error {
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

			cfg := loadConfig(cmd).Config

			s, err := openStore(cfg)
			if err != nil {
				return fmt.Errorf("open store: %w", err)
			}
			defer s.Close()

			emb := newEmbedder(cfg, client)

			ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
			defer cancel()

			if err := ensureOllama(ctx, cmd, cfg, true); err != nil {
				return err
			}

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

			// Always create the engine so evals use the same retrieval pipeline
			// (expansion + hybrid search + RRF) as real queries. LLM is optional
			// — without it, expansion is skipped but BM25 hybrid still runs.
			var llmClient query.LLM
			apiKey := os.Getenv("ANTHROPIC_API_KEY")
			if apiKey != "" {
				llmClient = llm.NewClaudeClient(apiKey, cfg.ClaudeModel, client, logger)
			}
			engine := query.NewEngine(s, emb, llmClient, limit, logger)
			runner.SetQueryEngine(engine)
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
				summaryPath := strings.TrimSuffix(resultsPath, ".json") + "-summary.json"
				if err := eval.SaveResultsSummary(summary, summaryPath); err != nil {
					fmt.Fprintf(os.Stderr, "Warning: could not save results summary: %v\n", err)
				}
			}

			return nil
		},
	}
	cmd.Flags().String("db", "", "Path to SQLite database (default: ~/.local/share/kb/kb.db)")
	cmd.Flags().String("testset", "eval/testset.json", "Path to test set JSON file")
	cmd.Flags().String("corpus", "eval/corpus", "Path to eval corpus directory")
	cmd.Flags().Int("limit", 20, "Max fragments to retrieve per query")
	cmd.Flags().Bool("ingest", false, "Ingest the eval corpus before running evaluation")
	cmd.Flags().Bool("json", false, "Output results as JSON")
	cmd.Flags().Bool("no-save", false, "Do not save results to results.json")
	cmd.Flags().Bool("skip-enrichment", false, "Skip LLM chunk enrichment during eval ingestion")
	cmd.Flags().String("enrich-model", "", "Ollama model for chunk enrichment (default: qwen2.5:0.5b)")
	cmd.Flags().String("prompt-version", "", "Enrichment prompt version: v1 (full rewrite), v2 (append keywords)")
	cmd.Flags().Bool("force", false, "Force full re-ingestion, bypassing checksum-based skipping")
	return cmd
}
