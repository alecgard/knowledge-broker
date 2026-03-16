package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

func TestResolveConfigPath(t *testing.T) {
	tests := []struct {
		name   string
		client mcpClientType
		scope  mcpScope
		home   string
		cwd    string
		want   string
	}{
		{
			name:   "claude code global",
			client: clientClaudeCode,
			scope:  scopeGlobal,
			home:   "/home/alice",
			cwd:    "/projects/myapp",
			want:   "/home/alice/.claude/claude_code_config.json",
		},
		{
			name:   "claude code local",
			client: clientClaudeCode,
			scope:  scopeLocal,
			home:   "/home/alice",
			cwd:    "/projects/myapp",
			want:   "/projects/myapp/.mcp.json",
		},
		{
			name:   "cursor global",
			client: clientCursor,
			scope:  scopeGlobal,
			home:   "/home/alice",
			cwd:    "/projects/myapp",
			want:   "/home/alice/.cursor/mcp.json",
		},
		{
			name:   "cursor local",
			client: clientCursor,
			scope:  scopeLocal,
			home:   "/home/alice",
			cwd:    "/projects/myapp",
			want:   "/projects/myapp/.cursor/mcp.json",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := resolveConfigPath(tt.client, tt.scope, tt.home, tt.cwd)
			if got != tt.want {
				t.Errorf("got %q, want %q", got, tt.want)
			}
		})
	}
}

func TestBuildMCPEntry(t *testing.T) {
	entry := buildMCPEntry("/usr/local/bin/kb")
	if entry.Command != "/usr/local/bin/kb" {
		t.Errorf("command = %q, want /usr/local/bin/kb", entry.Command)
	}
	want := []string{"serve", "--no-http", "--no-sse"}
	if len(entry.Args) != len(want) || entry.Args[0] != want[0] || entry.Args[1] != want[1] || entry.Args[2] != want[2] {
		t.Errorf("args = %v, want %v", entry.Args, want)
	}
}

func TestMergeMCPConfig_EmptyFile(t *testing.T) {
	entry := buildMCPEntry("/usr/local/bin/kb")
	result, err := mergeMCPConfig(nil, entry)
	if err != nil {
		t.Fatal(err)
	}

	var parsed map[string]interface{}
	if err := json.Unmarshal(result, &parsed); err != nil {
		t.Fatal(err)
	}

	servers := parsed["mcpServers"].(map[string]interface{})
	kb := servers["knowledge-broker"].(map[string]interface{})
	if kb["command"] != "/usr/local/bin/kb" {
		t.Errorf("command = %v", kb["command"])
	}
}

func TestMergeMCPConfig_PreservesExistingServers(t *testing.T) {
	existing := []byte(`{
  "mcpServers": {
    "other-server": {
      "command": "/usr/bin/other",
      "args": ["serve"]
    }
  }
}`)

	entry := buildMCPEntry("/usr/local/bin/kb")
	result, err := mergeMCPConfig(existing, entry)
	if err != nil {
		t.Fatal(err)
	}

	var parsed map[string]interface{}
	if err := json.Unmarshal(result, &parsed); err != nil {
		t.Fatal(err)
	}

	servers := parsed["mcpServers"].(map[string]interface{})
	if _, ok := servers["other-server"]; !ok {
		t.Error("other-server was removed")
	}
	if _, ok := servers["knowledge-broker"]; !ok {
		t.Error("knowledge-broker was not added")
	}
}

func TestMergeMCPConfig_UpdatesExistingEntry(t *testing.T) {
	existing := []byte(`{
  "mcpServers": {
    "knowledge-broker": {
      "command": "/old/path/kb",
      "args": ["serve", "--db", "/old/kb.db"]
    }
  }
}`)

	entry := buildMCPEntry("/new/path/kb")
	result, err := mergeMCPConfig(existing, entry)
	if err != nil {
		t.Fatal(err)
	}

	var parsed map[string]interface{}
	if err := json.Unmarshal(result, &parsed); err != nil {
		t.Fatal(err)
	}

	servers := parsed["mcpServers"].(map[string]interface{})
	kb := servers["knowledge-broker"].(map[string]interface{})
	if kb["command"] != "/new/path/kb" {
		t.Errorf("command = %v, want /new/path/kb", kb["command"])
	}
}

func TestMergeMCPConfig_PreservesUnknownTopLevelFields(t *testing.T) {
	existing := []byte(`{
  "someOtherSetting": true,
  "mcpServers": {}
}`)

	entry := buildMCPEntry("/usr/local/bin/kb")
	result, err := mergeMCPConfig(existing, entry)
	if err != nil {
		t.Fatal(err)
	}

	var parsed map[string]interface{}
	if err := json.Unmarshal(result, &parsed); err != nil {
		t.Fatal(err)
	}

	if parsed["someOtherSetting"] != true {
		t.Error("someOtherSetting was removed")
	}
}

func TestMergeMCPConfig_InvalidJSON(t *testing.T) {
	_, err := mergeMCPConfig([]byte(`not json`), mcpServerEntry{})
	if err == nil {
		t.Error("expected error for invalid JSON")
	}
}

func TestResolveKBBinary(t *testing.T) {
	// Create a temp binary path (no symlink).
	tmp := t.TempDir()
	fakeBin := filepath.Join(tmp, "kb")
	if err := os.WriteFile(fakeBin, []byte("fake"), 0755); err != nil {
		t.Fatal(err)
	}

	got, err := resolveKBBinary(func() (string, error) {
		return fakeBin, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	// Resolve symlinks on the expected path too (macOS /var -> /private/var).
	wantBin, _ := filepath.EvalSymlinks(fakeBin)
	if got != wantBin {
		t.Errorf("got %q, want %q", got, wantBin)
	}
}

func TestResolveKBBinary_Symlink(t *testing.T) {
	tmp := t.TempDir()
	realBin := filepath.Join(tmp, "kb-real")
	linkBin := filepath.Join(tmp, "kb")
	if err := os.WriteFile(realBin, []byte("fake"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(realBin, linkBin); err != nil {
		t.Fatal(err)
	}

	got, err := resolveKBBinary(func() (string, error) {
		return linkBin, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	// Resolve symlinks on the expected path too (macOS /var -> /private/var).
	wantBin, _ := filepath.EvalSymlinks(realBin)
	if got != wantBin {
		t.Errorf("got %q, want %q", got, wantBin)
	}
}

func TestWriteConfigFile_CreatesDirectories(t *testing.T) {
	tmp := t.TempDir()
	path := filepath.Join(tmp, "a", "b", "config.json")

	if err := writeConfigFile(path, []byte(`{"test": true}`)); err != nil {
		t.Fatal(err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "{\"test\": true}\n" {
		t.Errorf("got %q", string(data))
	}
}

func TestFormatEntryJSON(t *testing.T) {
	entry := buildMCPEntry("/usr/local/bin/kb")
	out := formatEntryJSON(entry)
	if !strings.Contains(out, `"knowledge-broker"`) {
		t.Errorf("missing knowledge-broker key in output: %s", out)
	}
	if !strings.Contains(out, `/usr/local/bin/kb`) {
		t.Errorf("missing binary path in output: %s", out)
	}
}

func TestAgentHintFile(t *testing.T) {
	if got := agentHintFile(clientClaudeCode); got != "CLAUDE.md" {
		t.Errorf("claude code: got %q, want CLAUDE.md", got)
	}
	if got := agentHintFile(clientCursor); got != "AGENTS.md" {
		t.Errorf("cursor: got %q, want AGENTS.md", got)
	}
}

func TestHandleAgentHint_ClaudeLocalCreatesFile(t *testing.T) {
	tmp := t.TempDir()
	cmd := &cobra.Command{}
	var buf strings.Builder
	cmd.SetOut(&buf)

	if err := handleAgentHint(cmd, clientClaudeCode, scopeLocal, tmp); err != nil {
		t.Fatal(err)
	}

	data, err := os.ReadFile(filepath.Join(tmp, "CLAUDE.md"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), agentHintHeading) {
		t.Error("CLAUDE.md missing Knowledge Broker heading")
	}
	if !strings.Contains(buf.String(), "Appended Knowledge Broker section") {
		t.Error("expected confirmation message")
	}
}

func TestHandleAgentHint_CursorLocalCreatesAgentsMD(t *testing.T) {
	tmp := t.TempDir()
	cmd := &cobra.Command{}
	var buf strings.Builder
	cmd.SetOut(&buf)

	if err := handleAgentHint(cmd, clientCursor, scopeLocal, tmp); err != nil {
		t.Fatal(err)
	}

	data, err := os.ReadFile(filepath.Join(tmp, "AGENTS.md"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), agentHintHeading) {
		t.Error("AGENTS.md missing Knowledge Broker heading")
	}
	if !strings.Contains(buf.String(), "Appended Knowledge Broker section") {
		t.Error("expected confirmation message")
	}
}

func TestHandleAgentHint_LocalAppendsToExisting(t *testing.T) {
	tmp := t.TempDir()
	existing := "# My Project\n\nSome info.\n"
	if err := os.WriteFile(filepath.Join(tmp, "CLAUDE.md"), []byte(existing), 0644); err != nil {
		t.Fatal(err)
	}

	cmd := &cobra.Command{}
	cmd.SetOut(&strings.Builder{})

	if err := handleAgentHint(cmd, clientClaudeCode, scopeLocal, tmp); err != nil {
		t.Fatal(err)
	}

	data, err := os.ReadFile(filepath.Join(tmp, "CLAUDE.md"))
	if err != nil {
		t.Fatal(err)
	}
	content := string(data)
	if !strings.HasPrefix(content, existing) {
		t.Error("existing content was not preserved")
	}
	if !strings.Contains(content, agentHintHeading) {
		t.Error("CLAUDE.md missing Knowledge Broker heading")
	}
}

func TestHandleAgentHint_LocalSkipsDuplicate(t *testing.T) {
	tmp := t.TempDir()
	existing := "# My Project\n\n## Knowledge Broker\n\nAlready here.\n"
	if err := os.WriteFile(filepath.Join(tmp, "AGENTS.md"), []byte(existing), 0644); err != nil {
		t.Fatal(err)
	}

	cmd := &cobra.Command{}
	var buf strings.Builder
	cmd.SetOut(&buf)

	if err := handleAgentHint(cmd, clientCursor, scopeLocal, tmp); err != nil {
		t.Fatal(err)
	}

	data, err := os.ReadFile(filepath.Join(tmp, "AGENTS.md"))
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != existing {
		t.Error("file was modified when it should have been skipped")
	}
	if !strings.Contains(buf.String(), "already contains") {
		t.Error("expected skip message")
	}
}

func TestHandleAgentHint_GlobalPrintsSnippet(t *testing.T) {
	cmd := &cobra.Command{}
	var buf strings.Builder
	cmd.SetOut(&buf)

	if err := handleAgentHint(cmd, clientClaudeCode, scopeGlobal, t.TempDir()); err != nil {
		t.Fatal(err)
	}

	if !strings.Contains(buf.String(), agentHintHeading) {
		t.Error("expected snippet in output")
	}
	if !strings.Contains(buf.String(), "CLAUDE.md") {
		t.Error("expected CLAUDE.md filename in tip")
	}
	if !strings.Contains(buf.String(), "Tip:") {
		t.Error("expected tip prefix")
	}
}

func TestHandleAgentHint_GlobalCursorPrintsAgentsMD(t *testing.T) {
	cmd := &cobra.Command{}
	var buf strings.Builder
	cmd.SetOut(&buf)

	if err := handleAgentHint(cmd, clientCursor, scopeGlobal, t.TempDir()); err != nil {
		t.Fatal(err)
	}

	if !strings.Contains(buf.String(), "AGENTS.md") {
		t.Error("expected AGENTS.md filename in tip")
	}
}
