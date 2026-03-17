package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/knowledge-broker/knowledge-broker/pkg/model"
)

func sourcesCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "sources",
		Short: "Manage registered sources",
	}
	cmd.PersistentFlags().String("db", "kb.db", "Path to SQLite database")
	cmd.PersistentFlags().String("remote", "", "URL of a remote KB server")
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
			remote, _ := cmd.Flags().GetString("remote")
			if remote != "" {
				remote = strings.TrimRight(remote, "/")
				var sources []model.Source
				if err := remoteJSON(context.Background(), http.MethodGet, remote+"/v1/sources", nil, &sources); err != nil {
					return err
				}
				if len(sources) == 0 {
					fmt.Fprintln(os.Stderr, "No sources registered.")
					return nil
				}
				out, _ := json.MarshalIndent(sources, "", "  ")
				fmt.Println(string(out))
				return nil
			}

			cfg := loadConfig(cmd).Config
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
			parts := strings.SplitN(args[0], "/", 2)
			if len(parts) != 2 {
				return fmt.Errorf("source must be in type/name format (e.g. git/myrepo)")
			}
			force, _ := cmd.Flags().GetBool("force")

			remote, _ := cmd.Flags().GetString("remote")
			if remote != "" {
				remote = strings.TrimRight(remote, "/")
				reqBody := map[string]interface{}{
					"source_type": parts[0],
					"source_name": parts[1],
					"description": args[1],
					"force":       force,
				}
				var resp map[string]string
				if err := remoteJSON(context.Background(), http.MethodPatch, remote+"/v1/sources", reqBody, &resp); err != nil {
					return err
				}
				fmt.Fprintf(os.Stderr, "Updated description for %s\n", args[0])
				return nil
			}

			cfg := loadConfig(cmd).Config
			cfg.DBPath, _ = cmd.Flags().GetString("db")

			s, err := openStore(cfg)
			if err != nil {
				return fmt.Errorf("open store: %w", err)
			}
			defer s.Close()

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

			remote, _ := cmd.Flags().GetString("remote")
			if remote != "" {
				remote = strings.TrimRight(remote, "/")
				var sources []model.Source
				if err := remoteJSON(context.Background(), http.MethodGet, remote+"/v1/sources", nil, &sources); err != nil {
					return err
				}

				// Use a local struct to omit last_ingest from export.
				type exportSource struct {
					SourceType  string            `json:"source_type"`
					SourceName  string            `json:"source_name"`
					Description string            `json:"description,omitempty"`
					Config      map[string]string `json:"config"`
				}
				out := make([]exportSource, len(sources))
				for i, src := range sources {
					out[i] = exportSource{
						SourceType:  src.SourceType,
						SourceName:  src.SourceName,
						Description: src.Description,
						Config:      src.Config,
					}
				}
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
				fmt.Fprintf(os.Stderr, "Exported %d sources to %s\n", len(sources), outFile)
				return nil
			}

			cfg := loadConfig(cmd).Config
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

			remote, _ := cmd.Flags().GetString("remote")
			if remote != "" {
				remote = strings.TrimRight(remote, "/")
				var resp map[string]int
				if err := remoteJSON(context.Background(), http.MethodPost, remote+"/v1/sources/import", sources, &resp); err != nil {
					return err
				}
				fmt.Fprintf(os.Stderr, "Imported %d sources to %s\n", resp["imported"], remote)
				return nil
			}

			cfg := loadConfig(cmd).Config
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

			remote, _ := cmd.Flags().GetString("remote")
			if remote != "" {
				remote = strings.TrimRight(remote, "/")
				reqBody := map[string]string{
					"source_type": sourceType,
					"source_name": sourceName,
				}
				var resp map[string]interface{}
				if err := remoteJSON(context.Background(), http.MethodDelete, remote+"/v1/sources", reqBody, &resp); err != nil {
					return err
				}
				deletedFragments := 0
				if df, ok := resp["deleted_fragments"].(float64); ok {
					deletedFragments = int(df)
				}
				fmt.Fprintf(cmd.OutOrStdout(), "Removed source %s/%s: deleted %d fragments\n", sourceType, sourceName, deletedFragments)
				return nil
			}

			cfg := loadConfig(cmd).Config
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
