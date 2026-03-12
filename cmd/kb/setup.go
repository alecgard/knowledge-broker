package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/spf13/cobra"
)

// mcpClientType identifies which MCP client to configure.
type mcpClientType int

const (
	clientClaudeCode mcpClientType = iota
	clientCursor
)

func (c mcpClientType) String() string {
	switch c {
	case clientClaudeCode:
		return "Claude Code"
	case clientCursor:
		return "Cursor"
	default:
		return "unknown"
	}
}

// mcpScope identifies global vs local config.
type mcpScope int

const (
	scopeGlobal mcpScope = iota
	scopeLocal
)

// mcpServerEntry is the JSON structure for an MCP server entry.
type mcpServerEntry struct {
	Command string   `json:"command"`
	Args    []string `json:"args"`
}

// resolveConfigPath returns the absolute path to the config file for the given
// client and scope. homeDir is the user's home directory, cwd is the current
// working directory.
func resolveConfigPath(client mcpClientType, scope mcpScope, homeDir, cwd string) string {
	switch client {
	case clientClaudeCode:
		if scope == scopeGlobal {
			return filepath.Join(homeDir, ".claude", "claude_code_config.json")
		}
		return filepath.Join(cwd, ".mcp.json")
	case clientCursor:
		if scope == scopeGlobal {
			return filepath.Join(homeDir, ".cursor", "mcp.json")
		}
		return filepath.Join(cwd, ".cursor", "mcp.json")
	}
	return ""
}

// buildMCPEntry constructs the server entry for the knowledge-broker MCP config.
func buildMCPEntry(kbBinary string) mcpServerEntry {
	return mcpServerEntry{
		Command: kbBinary,
		Args:    []string{"mcp"},
	}
}

// mergeMCPConfig reads existing JSON from data (may be empty/nil), inserts or
// updates the "knowledge-broker" entry under "mcpServers", and returns the
// merged JSON with 2-space indentation.
func mergeMCPConfig(data []byte, entry mcpServerEntry) ([]byte, error) {
	config := make(map[string]interface{})
	if len(data) > 0 {
		if err := json.Unmarshal(data, &config); err != nil {
			return nil, fmt.Errorf("parse existing config: %w", err)
		}
	}

	servers, ok := config["mcpServers"].(map[string]interface{})
	if !ok {
		servers = make(map[string]interface{})
	}

	// Convert the entry to a map so it merges naturally with the rest of the JSON.
	servers["knowledge-broker"] = map[string]interface{}{
		"command": entry.Command,
		"args":    entry.Args,
	}
	config["mcpServers"] = servers

	return json.MarshalIndent(config, "", "  ")
}

// resolveKBBinary returns the absolute, symlink-resolved path to the running
// kb binary. The executableFunc parameter allows injection for testing.
func resolveKBBinary(executableFunc func() (string, error)) (string, error) {
	exe, err := executableFunc()
	if err != nil {
		return "", fmt.Errorf("resolve executable: %w", err)
	}
	resolved, err := filepath.EvalSymlinks(exe)
	if err != nil {
		return "", fmt.Errorf("resolve symlinks: %w", err)
	}
	return resolved, nil
}

// writeConfigFile writes data to path, creating parent directories as needed.
func writeConfigFile(path string, data []byte) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("create directory %s: %w", dir, err)
	}
	return os.WriteFile(path, append(data, '\n'), 0644)
}

// formatEntryJSON returns a pretty-printed JSON snippet showing just the
// knowledge-broker entry (for display to the user).
func formatEntryJSON(entry mcpServerEntry) string {
	m := map[string]interface{}{
		"command": entry.Command,
		"args":    entry.Args,
	}
	b, _ := json.MarshalIndent(m, "  ", "  ")
	return fmt.Sprintf("  \"knowledge-broker\": %s", string(b))
}

func setupCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "setup",
		Short: "Configure MCP settings for Claude Code or Cursor",
		RunE:  runSetup,
	}
	cmd.Flags().String("client", "", "MCP client to configure: claude or cursor")
	cmd.Flags().Bool("global", false, "Write to global (home directory) config")
	cmd.Flags().Bool("local", false, "Write to local (project directory) config")
	return cmd
}

func runSetup(cmd *cobra.Command, args []string) error {
	reader := bufio.NewReader(os.Stdin)

	// Resolve client.
	clientFlag, _ := cmd.Flags().GetString("client")
	client, err := resolveClient(clientFlag, reader)
	if err != nil {
		return err
	}

	// Resolve scope.
	globalFlag, _ := cmd.Flags().GetBool("global")
	localFlag, _ := cmd.Flags().GetBool("local")
	scope, err := resolveScope(globalFlag, localFlag, client, reader)
	if err != nil {
		return err
	}

	// Resolve paths.
	kbBinary, err := resolveKBBinary(os.Executable)
	if err != nil {
		return err
	}

	cwd, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("get working directory: %w", err)
	}

	homeDir, err := os.UserHomeDir()
	if err != nil {
		return fmt.Errorf("get home directory: %w", err)
	}

	configPath := resolveConfigPath(client, scope, homeDir, cwd)
	entry := buildMCPEntry(kbBinary)

	// Read existing config if present.
	var existing []byte
	if data, err := os.ReadFile(configPath); err == nil {
		existing = data
	}

	merged, err := mergeMCPConfig(existing, entry)
	if err != nil {
		return fmt.Errorf("merge config: %w", err)
	}

	if err := writeConfigFile(configPath, merged); err != nil {
		return fmt.Errorf("write config: %w", err)
	}

	fmt.Fprintf(cmd.OutOrStdout(), "\n✓ Wrote MCP config to %s\n\n%s\n", configPath, formatEntryJSON(entry))

	if err := handleAgentHint(cmd, client, scope, cwd); err != nil {
		return err
	}

	return nil
}

func resolveClient(flag string, reader *bufio.Reader) (mcpClientType, error) {
	switch strings.ToLower(strings.TrimSpace(flag)) {
	case "claude":
		return clientClaudeCode, nil
	case "cursor":
		return clientCursor, nil
	case "":
		// Interactive prompt.
		fmt.Println("Select client:")
		fmt.Println("  1) Claude Code")
		fmt.Println("  2) Cursor")
		fmt.Print("> ")
		line, err := reader.ReadString('\n')
		if err != nil {
			return 0, fmt.Errorf("read client selection: %w", err)
		}
		choice, err := strconv.Atoi(strings.TrimSpace(line))
		if err != nil || choice < 1 || choice > 2 {
			return 0, fmt.Errorf("invalid selection: %s", strings.TrimSpace(line))
		}
		if choice == 1 {
			return clientClaudeCode, nil
		}
		return clientCursor, nil
	default:
		return 0, fmt.Errorf("unknown client %q (use \"claude\" or \"cursor\")", flag)
	}
}

func resolveScope(globalFlag, localFlag bool, client mcpClientType, reader *bufio.Reader) (mcpScope, error) {
	if globalFlag && localFlag {
		return 0, fmt.Errorf("cannot specify both --global and --local")
	}
	if globalFlag {
		return scopeGlobal, nil
	}
	if localFlag {
		return scopeLocal, nil
	}

	// Interactive prompt.
	homeDir, _ := os.UserHomeDir()
	var globalPath, localPath string
	switch client {
	case clientClaudeCode:
		globalPath = filepath.Join(homeDir, ".claude", "claude_code_config.json")
		localPath = ".mcp.json in current directory"
	case clientCursor:
		globalPath = filepath.Join(homeDir, ".cursor", "mcp.json")
		localPath = ".cursor/mcp.json in current directory"
	}

	fmt.Println("\nSelect scope:")
	fmt.Printf("  1) Global (%s)\n", globalPath)
	fmt.Printf("  2) Local (%s)\n", localPath)
	fmt.Print("> ")
	line, err := reader.ReadString('\n')
	if err != nil {
		return 0, fmt.Errorf("read scope selection: %w", err)
	}
	choice, err := strconv.Atoi(strings.TrimSpace(line))
	if err != nil || choice < 1 || choice > 2 {
		return 0, fmt.Errorf("invalid selection: %s", strings.TrimSpace(line))
	}
	if choice == 1 {
		return scopeGlobal, nil
	}
	return scopeLocal, nil
}

const agentHintSection = `## Knowledge Broker

A knowledge base is available via the ` + "`query`" + ` and ` + "`list-sources`" + ` MCP tools. Use these when grep and local file search don't surface the answer — they search across indexed documentation, repos, and project knowledge that may not be in the local codebase.
`

const agentHintHeading = "## Knowledge Broker"

// agentHintFile returns the instructions filename for the given client.
// Claude Code reads CLAUDE.md; Cursor and Codex read AGENTS.md.
func agentHintFile(client mcpClientType) string {
	switch client {
	case clientClaudeCode:
		return "CLAUDE.md"
	default:
		return "AGENTS.md"
	}
}

// handleAgentHint appends a usage hint to the client's agent instructions file
// (local scope) or prints the snippet to stdout (global scope).
func handleAgentHint(cmd *cobra.Command, client mcpClientType, scope mcpScope, cwd string) error {
	filename := agentHintFile(client)

	if scope == scopeGlobal {
		fmt.Fprintf(cmd.OutOrStdout(), "\nTip: add the following to your project's %s so the agent knows about the knowledge base:\n\n%s\n", filename, agentHintSection)
		return nil
	}

	// Local scope: append to the instructions file in cwd.
	mdPath := filepath.Join(cwd, filename)
	existing, err := os.ReadFile(mdPath)
	if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("read %s: %w", mdPath, err)
	}

	if strings.Contains(string(existing), agentHintHeading) {
		fmt.Fprintf(cmd.OutOrStdout(), "\n✓ %s already contains Knowledge Broker section, skipping.\n", filename)
		return nil
	}

	f, err := os.OpenFile(mdPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		return fmt.Errorf("open %s: %w", mdPath, err)
	}
	defer f.Close()

	content := agentHintSection
	if len(existing) > 0 && !strings.HasSuffix(string(existing), "\n\n") {
		if strings.HasSuffix(string(existing), "\n") {
			content = "\n" + content
		} else {
			content = "\n\n" + content
		}
	}

	if _, err := f.WriteString(content); err != nil {
		return fmt.Errorf("write %s: %w", mdPath, err)
	}

	fmt.Fprintf(cmd.OutOrStdout(), "\n✓ Appended Knowledge Broker section to %s\n", mdPath)
	return nil
}
