package main

import (
	"bytes"
	"os"
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

// newTestRoot builds the same command tree as main() for testing.
func newTestRoot() *cobra.Command {
	root := &cobra.Command{
		Use:   "kb",
		Short: "Knowledge Broker — ingest documents, query for answers with confidence signals",
		CompletionOptions: cobra.CompletionOptions{
			DisableDefaultCmd: true,
		},
	}
	root.PersistentFlags().Bool("debug", false, "Enable debug mode (log all API calls)")
	root.PersistentFlags().Bool("no-setup", false, "Skip automatic Ollama management")

	root.AddCommand(versionCmd())
	root.AddCommand(ingestCmd())
	root.AddCommand(queryCmd())
	root.AddCommand(serveCmd())
	root.AddCommand(exportCmd())
	root.AddCommand(sourcesCmd())
	root.AddCommand(evalCmd())
	root.AddCommand(clusterCmd())
	root.AddCommand(newSetupCmd())

	return root
}

func TestVersionCommand(t *testing.T) {
	// versionCmd uses fmt.Printf which writes to os.Stdout, so capture it.
	oldStdout := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stdout = w

	cmd := newTestRoot()
	cmd.SetArgs([]string{"version"})
	if err := cmd.Execute(); err != nil {
		os.Stdout = oldStdout
		t.Fatal(err)
	}

	w.Close()
	os.Stdout = oldStdout

	var buf bytes.Buffer
	buf.ReadFrom(r)
	out := buf.String()

	if !strings.Contains(out, "kb ") {
		t.Errorf("version output should contain 'kb ', got: %q", out)
	}
	if !strings.Contains(out, version) {
		t.Errorf("version output should contain version %q, got: %q", version, out)
	}
}

func TestRootCommandHelp(t *testing.T) {
	cmd := newTestRoot()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetArgs([]string{"--help"})

	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}

	out := buf.String()

	expected := []string{"ingest", "query", "serve", "setup", "version", "sources"}
	for _, sub := range expected {
		if !strings.Contains(out, sub) {
			t.Errorf("help output should list %q subcommand, got:\n%s", sub, out)
		}
	}
}

func TestQueryCommandRequiresDB(t *testing.T) {
	// Point --db at a path under /dev/null so creating the directory fails.
	dbPath := "/dev/null/impossible/kb.db"

	cmd := newTestRoot()
	cmd.SetArgs([]string{"query", "--db", dbPath, "what is this?"})
	// Silence usage printing on error.
	cmd.SilenceUsage = true

	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected error when DB path is unreachable")
	}
	// The error should mention creating the directory or the database.
	errMsg := err.Error()
	if !strings.Contains(errMsg, "store") && !strings.Contains(errMsg, "database") &&
		!strings.Contains(errMsg, "open") && !strings.Contains(errMsg, "no such") &&
		!strings.Contains(errMsg, "directory") && !strings.Contains(errMsg, "not a directory") {
		t.Errorf("expected store/database-related error, got: %q", errMsg)
	}
}

func TestIngestCommandRequiresSource(t *testing.T) {
	// Point --db at an impossible path so creating the directory fails.
	dbPath := "/dev/null/impossible/kb.db"

	cmd := newTestRoot()
	cmd.SetArgs([]string{"ingest", "--db", dbPath})
	cmd.SilenceUsage = true

	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected error when DB path is unreachable")
	}
	errMsg := err.Error()
	if !strings.Contains(errMsg, "store") && !strings.Contains(errMsg, "open") &&
		!strings.Contains(errMsg, "no such") && !strings.Contains(errMsg, "database") &&
		!strings.Contains(errMsg, "directory") && !strings.Contains(errMsg, "not a directory") {
		t.Errorf("expected store-related error, got: %q", errMsg)
	}
}

func TestServeSetsDefaults(t *testing.T) {
	cmd := newTestRoot()

	// Find the serve subcommand.
	var serve *cobra.Command
	for _, c := range cmd.Commands() {
		if c.Name() == "serve" {
			serve = c
			break
		}
	}
	if serve == nil {
		t.Fatal("serve subcommand not found")
	}

	addrFlag := serve.Flags().Lookup("addr")
	if addrFlag == nil {
		t.Fatal("serve command missing --addr flag")
	}
	if addrFlag.DefValue != ":8080" {
		t.Errorf("serve --addr default = %q, want %q", addrFlag.DefValue, ":8080")
	}

	dbFlag := serve.Flags().Lookup("db")
	if dbFlag == nil {
		t.Fatal("serve command missing --db flag")
	}
	if dbFlag.DefValue != "" {
		t.Errorf("serve --db default = %q, want %q", dbFlag.DefValue, "")
	}
}
