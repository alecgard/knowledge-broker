package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
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
	if len(entry.Args) != 1 || entry.Args[0] != "mcp" {
		t.Errorf("args = %v, want [mcp]", entry.Args)
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
      "args": ["mcp", "--db", "/old/kb.db"]
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
	if !contains(out, `"knowledge-broker"`) {
		t.Errorf("missing knowledge-broker key in output: %s", out)
	}
	if !contains(out, `/usr/local/bin/kb`) {
		t.Errorf("missing binary path in output: %s", out)
	}
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && searchString(s, substr)
}

func searchString(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
