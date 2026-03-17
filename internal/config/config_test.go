package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestParseDotEnv(t *testing.T) {
	content := `# This is a comment
DB_PATH=test.db

OLLAMA_URL=http://localhost:11434
QUOTED_VALUE="hello world"
SINGLE_QUOTED='single quoted'
EMPTY_VALUE=
SPACED_KEY = spaced_value
`
	tmp := t.TempDir()
	path := filepath.Join(tmp, ".env")
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	vals, err := parseDotEnv(path)
	if err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		key, want string
	}{
		{"DB_PATH", "test.db"},
		{"OLLAMA_URL", "http://localhost:11434"},
		{"QUOTED_VALUE", "hello world"},
		{"SINGLE_QUOTED", "single quoted"},
		{"EMPTY_VALUE", ""},
		{"SPACED_KEY", "spaced_value"},
	}

	for _, tt := range tests {
		got, ok := vals[tt.key]
		if !ok {
			t.Errorf("key %q not found", tt.key)
			continue
		}
		if got != tt.want {
			t.Errorf("key %q: got %q, want %q", tt.key, got, tt.want)
		}
	}

	// Comments should not be parsed.
	if _, ok := vals["# This is a comment"]; ok {
		t.Error("comment line should not be parsed as a key")
	}
}

func TestParseDotEnvMissing(t *testing.T) {
	_, err := parseDotEnv("/nonexistent/path/.env")
	if err == nil {
		t.Error("expected error for missing file")
	}
}

func TestLoadPrecedence(t *testing.T) {
	// Set up temp dirs for config files.
	tmp := t.TempDir()

	// XDG config.
	xdgDir := filepath.Join(tmp, "xdg", "kb")
	if err := os.MkdirAll(xdgDir, 0755); err != nil {
		t.Fatal(err)
	}
	xdgFile := filepath.Join(xdgDir, "config")
	if err := os.WriteFile(xdgFile, []byte("KB_DB=xdg.db\nKB_OLLAMA_URL=http://xdg:11434\nKB_LISTEN_ADDR=:9090\n"), 0644); err != nil {
		t.Fatal(err)
	}

	// .env file in a temp CWD.
	cwdDir := filepath.Join(tmp, "cwd")
	if err := os.MkdirAll(cwdDir, 0755); err != nil {
		t.Fatal(err)
	}
	dotEnvFile := filepath.Join(cwdDir, ".env")
	if err := os.WriteFile(dotEnvFile, []byte("KB_DB=dotenv.db\nKB_OLLAMA_URL=http://dotenv:11434\n"), 0644); err != nil {
		t.Fatal(err)
	}

	// --config file.
	flagFile := filepath.Join(tmp, "custom.conf")
	if err := os.WriteFile(flagFile, []byte("KB_DB=flag.db\n"), 0644); err != nil {
		t.Fatal(err)
	}

	// Save and restore env and CWD.
	origXDG := os.Getenv("XDG_CONFIG_HOME")
	origCWD, _ := os.Getwd()
	origDB := os.Getenv("KB_DB")
	origURL := os.Getenv("KB_OLLAMA_URL")
	origAddr := os.Getenv("KB_LISTEN_ADDR")

	// Clear env vars we're testing so file layers apply.
	os.Unsetenv("KB_DB")
	os.Unsetenv("KB_OLLAMA_URL")
	os.Unsetenv("KB_LISTEN_ADDR")

	t.Cleanup(func() {
		os.Setenv("XDG_CONFIG_HOME", origXDG)
		os.Chdir(origCWD)
		if origDB != "" {
			os.Setenv("KB_DB", origDB)
		} else {
			os.Unsetenv("KB_DB")
		}
		if origURL != "" {
			os.Setenv("KB_OLLAMA_URL", origURL)
		} else {
			os.Unsetenv("KB_OLLAMA_URL")
		}
		if origAddr != "" {
			os.Setenv("KB_LISTEN_ADDR", origAddr)
		} else {
			os.Unsetenv("KB_LISTEN_ADDR")
		}
	})

	os.Setenv("XDG_CONFIG_HOME", filepath.Join(tmp, "xdg"))
	os.Chdir(cwdDir)

	// Test: --config > .env > XDG > default.
	resolved := Load(LoadOptions{ConfigFile: flagFile})

	// KB_DB: --config wins (flag.db).
	if resolved.Config.DBPath != "flag.db" {
		t.Errorf("KB_DB: got %q, want %q", resolved.Config.DBPath, "flag.db")
	}

	// KB_OLLAMA_URL: .env wins over XDG (http://dotenv:11434).
	if resolved.Config.OllamaURL != "http://dotenv:11434" {
		t.Errorf("KB_OLLAMA_URL: got %q, want %q", resolved.Config.OllamaURL, "http://dotenv:11434")
	}

	// KB_LISTEN_ADDR: XDG wins over default (:9090).
	if resolved.Config.ListenAddr != ":9090" {
		t.Errorf("KB_LISTEN_ADDR: got %q, want %q", resolved.Config.ListenAddr, ":9090")
	}

	// Test: env var overrides everything.
	os.Setenv("KB_DB", "env.db")
	resolved2 := Load(LoadOptions{ConfigFile: flagFile})
	if resolved2.Config.DBPath != "env.db" {
		t.Errorf("KB_DB with env: got %q, want %q", resolved2.Config.DBPath, "env.db")
	}
	os.Unsetenv("KB_DB")
}

func TestLoadDefaultsOnly(t *testing.T) {
	// Save and restore env.
	tmp := t.TempDir()
	origXDG := os.Getenv("XDG_CONFIG_HOME")
	origCWD, _ := os.Getwd()

	// Point XDG to empty dir, chdir to empty dir (no .env).
	os.Setenv("XDG_CONFIG_HOME", filepath.Join(tmp, "empty-xdg"))
	os.Chdir(tmp)

	// Clear all KB_ env vars to ensure defaults apply.
	envVars := []string{
		"KB_DB", "KB_OLLAMA_URL", "KB_EMBEDDING_MODEL", "KB_EMBEDDING_DIM",
		"KB_ENRICH_MODEL", "KB_LLM_PROVIDER", "ANTHROPIC_API_KEY",
		"KB_CLAUDE_MODEL", "OPENAI_API_KEY", "KB_OPENAI_MODEL",
		"KB_OLLAMA_LLM_MODEL", "KB_LISTEN_ADDR", "KB_MAX_FILE_SIZE",
		"KB_MAX_CHUNK_SIZE", "KB_CHUNK_OVERLAP", "KB_WORKERS",
		"KB_DEFAULT_LIMIT", "KB_GITHUB_CLIENT_ID", "KB_SKIP_SETUP",
	}
	origVals := make(map[string]string)
	for _, k := range envVars {
		origVals[k] = os.Getenv(k)
		os.Unsetenv(k)
	}

	t.Cleanup(func() {
		os.Setenv("XDG_CONFIG_HOME", origXDG)
		os.Chdir(origCWD)
		for k, v := range origVals {
			if v != "" {
				os.Setenv(k, v)
			}
		}
	})

	resolved := Load(LoadOptions{})

	if resolved.Config.DBPath != DefaultDBPath() {
		t.Errorf("DBPath: got %q, want %q", resolved.Config.DBPath, DefaultDBPath())
	}
	if resolved.Config.OllamaURL != "http://localhost:11434" {
		t.Errorf("OllamaURL: got %q, want %q", resolved.Config.OllamaURL, "http://localhost:11434")
	}
	if resolved.Config.EmbeddingDim != 768 {
		t.Errorf("EmbeddingDim: got %d, want %d", resolved.Config.EmbeddingDim, 768)
	}
	if resolved.Config.WorkerCount != 4 {
		t.Errorf("WorkerCount: got %d, want %d", resolved.Config.WorkerCount, 4)
	}

	// Check that all origins show "default".
	for _, f := range Fields() {
		info, ok := resolved.Origins[f.EnvVar]
		if !ok {
			t.Errorf("missing origin for %s", f.EnvVar)
			continue
		}
		if info.Source != "default" {
			t.Errorf("origin for %s: got %q, want %q", f.EnvVar, info.Source, "default")
		}
	}
}

func TestDefaultBackwardCompat(t *testing.T) {
	// Save and restore env.
	tmp := t.TempDir()
	origXDG := os.Getenv("XDG_CONFIG_HOME")
	origCWD, _ := os.Getwd()

	os.Setenv("XDG_CONFIG_HOME", filepath.Join(tmp, "empty-xdg"))
	os.Chdir(tmp)

	origDB := os.Getenv("KB_DB")
	os.Unsetenv("KB_DB")

	t.Cleanup(func() {
		os.Setenv("XDG_CONFIG_HOME", origXDG)
		os.Chdir(origCWD)
		if origDB != "" {
			os.Setenv("KB_DB", origDB)
		}
	})

	cfg := Default()
	if cfg.DBPath != DefaultDBPath() {
		t.Errorf("Default().DBPath: got %q, want %q", cfg.DBPath, DefaultDBPath())
	}
}
