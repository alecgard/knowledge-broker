package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/spf13/cobra"

	"github.com/knowledge-broker/knowledge-broker/internal/cluster"
	"github.com/knowledge-broker/knowledge-broker/pkg/model"
)

func clusterCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "cluster",
		Short: "Run k-means clustering on fragment embeddings",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg := loadConfig(cmd).Config
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
			cfg := loadConfig(cmd).Config
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
