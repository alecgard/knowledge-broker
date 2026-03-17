package main

import (
	"bufio"
	"context"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/spf13/cobra"
)

func exportCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "export",
		Short: "Export fragment embeddings for TensorBoard Embedding Projector",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg := loadConfig(cmd).Config
			cfg.DBPath, _ = cmd.Flags().GetString("db")
			outDir, _ := cmd.Flags().GetString("out")

			remote, _ := cmd.Flags().GetString("remote")
			if remote != "" {
				remote = strings.TrimRight(remote, "/")
				return exportRemote(context.Background(), remote, outDir)
			}

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
	cmd.Flags().String("remote", "", "URL of a remote KB server")
	return cmd
}

// exportRemote fetches fragments from a remote server and writes TSV files locally.
func exportRemote(ctx context.Context, remote, outDir string) error {
	var fragments []struct {
		SourceType string    `json:"source_type"`
		SourceName string    `json:"source_name"`
		SourcePath string    `json:"source_path"`
		Content    string    `json:"content"`
		Embedding  []float32 `json:"embedding"`
	}
	if err := remoteJSON(ctx, http.MethodGet, remote+"/v1/export", nil, &fragments); err != nil {
		return err
	}

	if len(fragments) == 0 {
		fmt.Fprintln(os.Stderr, "No fragments with embeddings found.")
		return nil
	}

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
	mw.WriteString("source_name\tsource_path\tsource_type\tcontent\n")
	for _, f := range fragments {
		fmt.Fprintf(mw, "%s\t%s\t%s\t%s\n",
			sanitizeTSV(f.SourceName),
			sanitizeTSV(f.SourcePath),
			sanitizeTSV(f.SourceType),
			sanitizeTSV(f.Content),
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
}
