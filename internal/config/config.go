package config

import (
	"bufio"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
)

// Config holds all configuration for Knowledge Broker.
type Config struct {
	// Database
	DBPath string

	// Ollama
	OllamaURL      string
	EmbeddingModel string
	EmbeddingDim   int
	EnrichModel    string

	// LLM Provider
	LLMProvider string // "ollama" (default)

	// Ollama LLM (separate from embedding model)
	OllamaLLMModel string

	// Server
	ListenAddr string

	// Ingestion
	MaxFileSize  int64 // bytes
	MaxChunkSize int   // characters
	ChunkOverlap int   // characters
	WorkerCount  int

	// Query
	DefaultLimit int

	// Runtime
	SkipSetup bool // from KB_SKIP_SETUP
}

// ValueInfo records where a config value came from.
type ValueInfo struct {
	Value  string
	Source string // human-readable source description
}

// LoadOptions controls config loading behavior.
type LoadOptions struct {
	ConfigFile string // from --config flag; empty if not set
}

// ResolvedConfig wraps Config with provenance information.
type ResolvedConfig struct {
	Config
	Origins map[string]ValueInfo // env var name -> where the value came from
	Files   []FileStatus         // which config files were checked
}

// FileStatus records whether a config file was found.
type FileStatus struct {
	Path  string
	Found bool
}

// FieldDescriptor maps a Config field to its env var and default value.
type FieldDescriptor struct {
	EnvVar       string
	DefaultValue string
}

// Fields returns the ordered list of config field descriptors.
func Fields() []FieldDescriptor {
	return []FieldDescriptor{
		{"KB_DB", DefaultDBPath()},
		{"KB_OLLAMA_URL", "http://localhost:11434"},
		{"KB_EMBEDDING_MODEL", "nomic-embed-text"},
		{"KB_EMBEDDING_DIM", "768"},
		{"KB_ENRICH_MODEL", "qwen2.5:0.5b"},
		{"KB_LLM_PROVIDER", "ollama"},
		{"KB_OLLAMA_LLM_MODEL", ""},
		{"KB_LISTEN_ADDR", ":8080"},
		{"KB_MAX_FILE_SIZE", "1048576"},
		{"KB_MAX_CHUNK_SIZE", "2000"},
		{"KB_CHUNK_OVERLAP", "150"},
		{"KB_WORKERS", strconv.Itoa(runtime.NumCPU())},
		{"KB_DEFAULT_LIMIT", "20"},
		{"KB_SKIP_SETUP", "false"},
	}
}

// Default returns a config with sensible defaults, overridden by .env file
// and environment variables. Env vars take precedence over .env.
func Default() Config {
	return Load(LoadOptions{}).Config
}

// Load loads config with full search path and provenance tracking.
// Precedence (later wins): defaults < XDG config < .env < --config file < env vars.
func Load(opts LoadOptions) ResolvedConfig {
	fields := Fields()

	// Build default values map.
	defaults := make(map[string]string, len(fields))
	for _, f := range fields {
		defaults[f.EnvVar] = f.DefaultValue
	}

	// Track which layer provided each value.
	// Start with defaults.
	origins := make(map[string]ValueInfo, len(fields))
	for _, f := range fields {
		origins[f.EnvVar] = ValueInfo{Value: f.DefaultValue, Source: "default"}
	}

	var files []FileStatus

	// 1. XDG config file.
	xdgPath := xdgConfigPath()
	xdgVals, xdgErr := parseDotEnv(xdgPath)
	files = append(files, FileStatus{Path: xdgPath, Found: xdgErr == nil})
	if xdgErr == nil {
		for k, v := range xdgVals {
			origins[k] = ValueInfo{Value: v, Source: xdgPath}
		}
	}

	// 2. .env in CWD.
	dotEnvVals, dotEnvErr := parseDotEnv(".env")
	files = append(files, FileStatus{Path: ".env", Found: dotEnvErr == nil})
	if dotEnvErr == nil {
		for k, v := range dotEnvVals {
			origins[k] = ValueInfo{Value: v, Source: ".env"}
		}
	}

	// 3. --config file.
	var flagVals map[string]string
	if opts.ConfigFile != "" {
		var flagErr error
		flagVals, flagErr = parseDotEnv(opts.ConfigFile)
		files = append(files, FileStatus{Path: opts.ConfigFile, Found: flagErr == nil})
		if flagErr == nil {
			for k, v := range flagVals {
				origins[k] = ValueInfo{Value: v, Source: "--config " + opts.ConfigFile}
			}
		}
	} else {
		files = append(files, FileStatus{Path: "--config", Found: false})
	}

	// 4. Merge all file values (later layers win), then set env vars.
	merged := make(map[string]string)
	if xdgErr == nil {
		for k, v := range xdgVals {
			merged[k] = v
		}
	}
	if dotEnvErr == nil {
		for k, v := range dotEnvVals {
			merged[k] = v
		}
	}
	for k, v := range flagVals {
		merged[k] = v
	}

	// Set env vars. Real env vars (already set) always take precedence.
	for k, v := range merged {
		if _, exists := os.LookupEnv(k); !exists {
			os.Setenv(k, v)
		}
	}

	// 5. Check env vars — they override everything.
	for _, f := range fields {
		if v, exists := os.LookupEnv(f.EnvVar); exists {
			// If the value differs from what we set from files, it's a real env var.
			if fileVal, ok := merged[f.EnvVar]; !ok || v != fileVal {
				origins[f.EnvVar] = ValueInfo{Value: v, Source: "env"}
			}
		}
	}

	// 6. Build the Config struct using envOr/envOrInt (which now reads from env).
	cfg := Config{
		DBPath:          envOr("KB_DB", DefaultDBPath()),
		OllamaURL:       envOr("KB_OLLAMA_URL", "http://localhost:11434"),
		EmbeddingModel:  envOr("KB_EMBEDDING_MODEL", "nomic-embed-text"),
		EnrichModel:     envOr("KB_ENRICH_MODEL", "qwen2.5:0.5b"),
		EmbeddingDim:    envOrInt("KB_EMBEDDING_DIM", 768),
		LLMProvider:     envOr("KB_LLM_PROVIDER", "ollama"),
		OllamaLLMModel:  envOr("KB_OLLAMA_LLM_MODEL", ""),
		ListenAddr:      envOr("KB_LISTEN_ADDR", ":8080"),
		MaxFileSize:     int64(envOrInt("KB_MAX_FILE_SIZE", 1_048_576)),
		MaxChunkSize:    envOrInt("KB_MAX_CHUNK_SIZE", 2000),
		ChunkOverlap:    envOrInt("KB_CHUNK_OVERLAP", 150),
		WorkerCount:     envOrInt("KB_WORKERS", runtime.NumCPU()),
		DefaultLimit:    envOrInt("KB_DEFAULT_LIMIT", 20),
		SkipSetup:       envOr("KB_SKIP_SETUP", "false") == "true",
	}

	// Update origin values to reflect what was actually used (after envOr).
	for _, f := range fields {
		if info, ok := origins[f.EnvVar]; ok {
			actual := os.Getenv(f.EnvVar)
			if actual != "" {
				info.Value = actual
			} else if f.DefaultValue != "" {
				info.Value = f.DefaultValue
			}
			origins[f.EnvVar] = info
		}
	}

	return ResolvedConfig{
		Config:  cfg,
		Origins: origins,
		Files:   files,
	}
}

// parseDotEnv reads a .env-format file and returns key-value pairs
// WITHOUT calling os.Setenv.
func parseDotEnv(path string) (map[string]string, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	result := make(map[string]string)
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())

		// Skip empty lines and comments.
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		key, value, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}

		key = strings.TrimSpace(key)
		value = strings.TrimSpace(value)

		// Strip surrounding quotes.
		if len(value) >= 2 {
			if (value[0] == '"' && value[len(value)-1] == '"') ||
				(value[0] == '\'' && value[len(value)-1] == '\'') {
				value = value[1 : len(value)-1]
			}
		}

		result[key] = value
	}
	return result, scanner.Err()
}

// xdgConfigPath returns the path to the XDG config file for kb.
func xdgConfigPath() string {
	xdgHome := os.Getenv("XDG_CONFIG_HOME")
	if xdgHome == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return filepath.Join(".config", "kb", "config")
		}
		xdgHome = filepath.Join(home, ".config")
	}
	return filepath.Join(xdgHome, "kb", "config")
}

// DBFlagUsage is the help text for the --db flag across all commands.
var DBFlagUsage = "Path to SQLite database (default: " + DefaultDBPath() + ")"

// DefaultDBPath returns the default database path under XDG_DATA_HOME.
func DefaultDBPath() string {
	xdgData := os.Getenv("XDG_DATA_HOME")
	if xdgData == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "kb.db"
		}
		xdgData = filepath.Join(home, ".local", "share")
	}
	return filepath.Join(xdgData, "kb", "kb.db")
}

// MigrateDB checks for a legacy ./kb.db in the current directory and migrates
// it to the global path if needed. It moves kb.db, kb.db-shm, and kb.db-wal.
// Returns the final DB path to use and any warning message for the user.
//
// Cases:
//   - User set KB_DB explicitly → no migration, use what they set
//   - ./kb.db exists, global doesn't → move all files, return global path
//   - Both exist → warn, use global path
//   - Neither exists → use global path (will be created on first ingest)
func MigrateDB(dbPath string, dbExplicit bool) (finalPath string, warn string) {
	if dbExplicit {
		return dbPath, ""
	}

	globalPath := DefaultDBPath()
	localPath := "kb.db"

	_, localErr := os.Stat(localPath)
	localExists := localErr == nil

	_, globalErr := os.Stat(globalPath)
	globalExists := globalErr == nil

	if !localExists {
		// No local db — use global (may or may not exist yet).
		return globalPath, ""
	}

	if localExists && globalExists {
		// Both exist — warn, use global.
		absLocal, _ := filepath.Abs(localPath)
		return globalPath, "Warning: found database at both " + absLocal + " and " + globalPath + "\n" +
			"Using " + globalPath + ". Remove the local copy or set KB_DB to choose explicitly."
	}

	// Local exists, global doesn't — migrate.
	if err := os.MkdirAll(filepath.Dir(globalPath), 0755); err != nil {
		// Can't create directory — fall back to local.
		return localPath, ""
	}

	absLocal, _ := filepath.Abs(localPath)
	var moved []string
	for _, suffix := range []string{"", "-shm", "-wal"} {
		src := localPath + suffix
		dst := globalPath + suffix
		if _, err := os.Stat(src); err == nil {
			if err := os.Rename(src, dst); err != nil {
				// Rename failed (cross-device?) — fall back to local.
				return localPath, "Warning: could not move " + src + " to " + dst + ": " + err.Error() + "\nUsing local database."
			}
			moved = append(moved, src)
		}
	}

	msg := "Migrated database from " + absLocal + " to " + globalPath
	if len(moved) > 1 {
		msg += " (" + strings.Join(moved, ", ") + ")"
	}
	return globalPath, msg
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func envOrInt(key string, fallback int) int {
	if v := os.Getenv(key); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
	}
	return fallback
}
