package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"github.com/spf13/cobra"

	"github.com/knowledge-broker/knowledge-broker/internal/config"
	"github.com/knowledge-broker/knowledge-broker/internal/enrich"
	"github.com/knowledge-broker/knowledge-broker/pkg/model"
)

func enrichCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "enrich",
		Short: "Run LLM enrichment on already-ingested fragments, then re-embed",
		Long: `Enrich runs LLM-based chunk enrichment on fragments that have already been
ingested. After enrichment, fragments are re-embedded with the enriched content.

Requires at least one --source flag in the format type/name (e.g. git/owner/repo,
filesystem/path/to/dir). Multiple --source flags are supported.

By default, fragments already enriched with the same model and prompt version
are skipped. Use --force to re-enrich all fragments.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg := loadConfig(cmd).Config
			debugMode := isDebug(cmd)
			logger := newLogger(debugMode)
			client := httpClient(logger, debugMode)

			sources, _ := cmd.Flags().GetStringArray("source")
			force, _ := cmd.Flags().GetBool("force")
			enrichModel, _ := cmd.Flags().GetString("enrich-model")
			promptVersion, _ := cmd.Flags().GetString("prompt-version")

			if len(sources) == 0 {
				return fmt.Errorf("at least one --source flag is required (format: type/name, e.g. git/owner/repo)")
			}

			// Parse source specs into type/name pairs.
			type sourceSpec struct {
				sourceType string
				sourceName string
			}
			var specs []sourceSpec
			for _, s := range sources {
				idx := strings.Index(s, "/")
				if idx < 1 {
					return fmt.Errorf("invalid --source format %q: expected type/name (e.g. git/owner/repo)", s)
				}
				specs = append(specs, sourceSpec{
					sourceType: s[:idx],
					sourceName: s[idx+1:],
				})
			}

			s, err := openStore(cfg)
			if err != nil {
				return fmt.Errorf("open store: %w", err)
			}
			defer s.Close()

			ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
			defer cancel()

			if err := ensureOllama(ctx, cmd, cfg, true); err != nil {
				return err
			}

			emb := newEmbedder(cfg, client)

			if enrichModel == "" {
				enrichModel = cfg.EnrichModel
			}
			enricher := enrich.NewOllamaEnricher(cfg.OllamaURL, enrichModel, promptVersion, client, logger)
			modelName := enricher.Model()
			// Set the global prompt version for cache keying.
			if promptVersion == "" {
				promptVersion = enrich.DefaultPromptVersion
			}

			for _, spec := range specs {
				// Verify the source exists.
				src, err := s.GetSource(ctx, spec.sourceType, spec.sourceName)
				if err != nil {
					return fmt.Errorf("get source %s/%s: %w", spec.sourceType, spec.sourceName, err)
				}
				if src == nil {
					return fmt.Errorf("source %s/%s not found", spec.sourceType, spec.sourceName)
				}

				frags, err := s.GetFragmentsBySource(ctx, spec.sourceName)
				if err != nil {
					return fmt.Errorf("get fragments for %s/%s: %w", spec.sourceType, spec.sourceName, err)
				}

				if len(frags) == 0 {
					fmt.Fprintf(os.Stderr, "No fragments found for %s/%s, skipping\n", spec.sourceType, spec.sourceName)
					continue
				}

				// Filter out already-enriched fragments unless --force.
				if !force {
					var toEnrich []model.SourceFragment
					skipped := 0
					for _, f := range frags {
						if f.EnrichedContent != "" &&
							f.EnrichmentModel == modelName &&
							f.EnrichmentVersion == promptVersion {
							skipped++
							continue
						}
						toEnrich = append(toEnrich, f)
					}
					if skipped > 0 {
						fmt.Fprintf(os.Stderr, "  Skipping %d already-enriched fragments (model=%s, version=%s)\n",
							skipped, modelName, promptVersion)
					}
					frags = toEnrich
				}

				if len(frags) == 0 {
					fmt.Fprintf(os.Stderr, "All fragments for %s/%s already enriched, skipping (use --force to re-enrich)\n",
						spec.sourceType, spec.sourceName)
					continue
				}

				fmt.Fprintf(os.Stderr, "Enriching %d fragments from %s/%s...\n",
					len(frags), spec.sourceType, spec.sourceName)

				if err := reEnrichFragments(ctx, s, emb, enricher, frags, logger); err != nil {
					return fmt.Errorf("enrich %s/%s: %w", spec.sourceType, spec.sourceName, err)
				}
			}

			return nil
		},
	}
	cmd.Flags().StringArray("source", nil, "Source to enrich in type/name format (e.g. git/owner/repo, repeatable)")
	cmd.Flags().Bool("force", false, "Re-enrich all fragments, even if already enriched with same model and prompt version")
	cmd.Flags().String("enrich-model", "", "Ollama model for chunk enrichment (default: from config)")
	cmd.Flags().String("prompt-version", "", "Enrichment prompt version: v1 (full rewrite), v2 (append keywords)")
	cmd.Flags().String("db", "", config.DBFlagUsage)
	return cmd
}
