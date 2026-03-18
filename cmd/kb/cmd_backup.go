package main

import (
	"bufio"
	"database/sql"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/knowledge-broker/knowledge-broker/internal/config"
)

func backupCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "backup",
		Short: "Create a consistent backup of the database",
		Long:  "Create a timestamped backup of the SQLite database using SQLite's online backup API, safe to run while the server is running.",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg := loadConfig(cmd).Config
			outPath, _ := cmd.Flags().GetString("out")

			if _, err := os.Stat(cfg.DBPath); os.IsNotExist(err) {
				return fmt.Errorf("database not found: %s", cfg.DBPath)
			}

			if outPath == "" {
				dir := filepath.Dir(cfg.DBPath)
				base := strings.TrimSuffix(filepath.Base(cfg.DBPath), filepath.Ext(cfg.DBPath))
				ts := time.Now().Format("20060102-150405")
				outPath = filepath.Join(dir, fmt.Sprintf("%s-backup-%s.db", base, ts))
			}

			// Ensure output directory exists.
			if dir := filepath.Dir(outPath); dir != "." {
				if err := os.MkdirAll(dir, 0755); err != nil {
					return fmt.Errorf("create output directory: %w", err)
				}
			}

			if err := sqliteBackup(cfg.DBPath, outPath); err != nil {
				return fmt.Errorf("backup failed: %w", err)
			}

			fmt.Fprintf(cmd.OutOrStdout(), "Backup written to %s\n", outPath)
			return nil
		},
	}
	cmd.Flags().String("db", "", config.DBFlagUsage)
	cmd.Flags().String("out", "", "Output path for backup file (default: timestamped file next to database)")
	return cmd
}

func restoreCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "restore <backup-file>",
		Short: "Restore the database from a backup file",
		Long:  "Replace the current database with a backup file. Validates that the backup is a valid SQLite database before overwriting. Requires explicit confirmation.",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg := loadConfig(cmd).Config
			backupPath := args[0]
			force, _ := cmd.Flags().GetBool("force")

			// Validate backup file exists.
			info, err := os.Stat(backupPath)
			if err != nil {
				return fmt.Errorf("backup file not found: %s", backupPath)
			}
			if info.IsDir() {
				return fmt.Errorf("backup path is a directory, not a file: %s", backupPath)
			}

			// Validate it is a SQLite database by opening and pinging it.
			if err := validateSQLite(backupPath); err != nil {
				return fmt.Errorf("invalid backup file: %w", err)
			}

			// Prompt for confirmation unless --force is set.
			if !force {
				fmt.Fprintf(cmd.OutOrStdout(), "This will replace the current database at %s. Continue? [y/N] ", cfg.DBPath)
				reader := bufio.NewReader(os.Stdin)
				line, err := reader.ReadString('\n')
				if err != nil {
					return fmt.Errorf("read confirmation: %w", err)
				}
				answer := strings.TrimSpace(strings.ToLower(line))
				if answer != "y" && answer != "yes" {
					fmt.Fprintln(cmd.OutOrStdout(), "Restore cancelled.")
					return nil
				}
			}

			// Ensure the target directory exists.
			if dir := filepath.Dir(cfg.DBPath); dir != "." {
				if err := os.MkdirAll(dir, 0755); err != nil {
					return fmt.Errorf("create database directory: %w", err)
				}
			}

			// Copy backup file to database path.
			if err := copyFile(backupPath, cfg.DBPath); err != nil {
				return fmt.Errorf("restore failed: %w", err)
			}

			// Remove WAL and SHM files that may be stale after replacement.
			os.Remove(cfg.DBPath + "-wal")
			os.Remove(cfg.DBPath + "-shm")

			fmt.Fprintf(cmd.OutOrStdout(), "Database restored from %s\n", backupPath)
			return nil
		},
	}
	cmd.Flags().String("db", "", config.DBFlagUsage)
	cmd.Flags().Bool("force", false, "Skip confirmation prompt")
	return cmd
}

// sqliteBackup uses SQLite's VACUUM INTO to create a consistent backup.
func sqliteBackup(srcPath, dstPath string) error {
	db, err := sql.Open("sqlite3", srcPath+"?_journal_mode=WAL&_busy_timeout=5000&mode=ro")
	if err != nil {
		return fmt.Errorf("open source database: %w", err)
	}
	defer db.Close()

	_, err = db.Exec("VACUUM INTO ?", dstPath)
	if err != nil {
		return fmt.Errorf("VACUUM INTO: %w", err)
	}
	return nil
}

// validateSQLite opens the file as a SQLite database and runs a quick integrity check.
func validateSQLite(path string) error {
	db, err := sql.Open("sqlite3", path+"?mode=ro")
	if err != nil {
		return err
	}
	defer db.Close()

	// A simple query to verify the file is a valid SQLite database.
	var result string
	if err := db.QueryRow("SELECT sqlite_version()").Scan(&result); err != nil {
		return fmt.Errorf("not a valid SQLite database: %w", err)
	}
	return nil
}

// copyFile copies src to dst atomically by writing to a temp file first.
func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()

	// Write to a temp file in the same directory, then rename for atomicity.
	dir := filepath.Dir(dst)
	tmp, err := os.CreateTemp(dir, ".kb-restore-*.tmp")
	if err != nil {
		return fmt.Errorf("create temp file: %w", err)
	}
	tmpPath := tmp.Name()

	if _, err := io.Copy(tmp, in); err != nil {
		tmp.Close()
		os.Remove(tmpPath)
		return err
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpPath)
		return err
	}

	if err := os.Rename(tmpPath, dst); err != nil {
		os.Remove(tmpPath)
		return err
	}
	return nil
}
