package extractor

import (
	"strings"
	"testing"
)

func TestYAMLExtractTopLevelKeys(t *testing.T) {
	yamlContent := `
database:
  host: localhost
  port: 5432
  name: mydb

server:
  host: 0.0.0.0
  port: 8080

logging:
  level: info
  format: json
`
	ext := NewYAMLExtractor(2000)
	result, err := ext.Extract([]byte(yamlContent), ExtractOptions{Path: "config.yaml"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	chunks := result.Chunks

	if len(chunks) != 3 {
		t.Fatalf("expected 3 chunks (one per top-level key), got %d", len(chunks))
	}

	// Check that we have all three keys.
	keys := make(map[string]bool)
	for _, ch := range chunks {
		keys[ch.Metadata["key"]] = true
	}
	for _, expected := range []string{"database", "server", "logging"} {
		if !keys[expected] {
			t.Errorf("missing chunk for key %q", expected)
		}
	}

	// Check that database chunk contains host info.
	for _, ch := range chunks {
		if ch.Metadata["key"] == "database" {
			if !strings.Contains(ch.Content, "localhost") {
				t.Errorf("database chunk should contain 'localhost', got: %s", ch.Content)
			}
			if !strings.Contains(ch.Content, "5432") {
				t.Errorf("database chunk should contain '5432', got: %s", ch.Content)
			}
		}
	}
}

func TestYAMLExtractYML(t *testing.T) {
	yml := `name: test
version: 1.0
`
	ext := NewYAMLExtractor(2000)
	result, err := ext.Extract([]byte(yml), ExtractOptions{Path: "config.yml"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	chunks := result.Chunks
	if len(chunks) != 2 {
		t.Fatalf("expected 2 chunks, got %d", len(chunks))
	}
}

func TestJSONExtractTopLevelKeys(t *testing.T) {
	jsonContent := `{
  "name": "my-project",
  "version": "1.0.0",
  "dependencies": {
    "express": "^4.18.0",
    "lodash": "^4.17.21"
  },
  "scripts": {
    "start": "node index.js",
    "test": "jest"
  }
}`
	ext := NewYAMLExtractor(2000)
	result, err := ext.Extract([]byte(jsonContent), ExtractOptions{Path: "package.json"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	chunks := result.Chunks

	if len(chunks) != 4 {
		t.Fatalf("expected 4 chunks, got %d", len(chunks))
	}

	keys := make(map[string]bool)
	for _, ch := range chunks {
		keys[ch.Metadata["key"]] = true
	}
	for _, expected := range []string{"name", "version", "dependencies", "scripts"} {
		if !keys[expected] {
			t.Errorf("missing chunk for key %q", expected)
		}
	}
}

func TestJSONArrayFallback(t *testing.T) {
	jsonArray := `[1, 2, 3, 4, 5]`
	ext := NewYAMLExtractor(2000)
	result, err := ext.Extract([]byte(jsonArray), ExtractOptions{Path: "data.json"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	chunks := result.Chunks
	// Non-object JSON falls back to line-based chunking.
	if len(chunks) == 0 {
		t.Fatal("expected at least 1 chunk")
	}
}

func TestTOMLExtractSections(t *testing.T) {
	toml := `# Global settings
title = "My App"
debug = false

[database]
host = "localhost"
port = 5432

[server]
host = "0.0.0.0"
port = 8080
`
	ext := NewYAMLExtractor(2000)
	result, err := ext.Extract([]byte(toml), ExtractOptions{Path: "config.toml"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	chunks := result.Chunks

	// Expect: global section + database + server = 3 chunks.
	if len(chunks) != 3 {
		t.Fatalf("expected 3 chunks, got %d", len(chunks))
	}

	// Check section names.
	keys := make(map[string]bool)
	for _, ch := range chunks {
		keys[ch.Metadata["key"]] = true
	}
	if !keys["database"] {
		t.Error("missing chunk for [database] section")
	}
	if !keys["server"] {
		t.Error("missing chunk for [server] section")
	}
}

func TestINIExtractSections(t *testing.T) {
	ini := `[general]
name = test
debug = true

[database]
host = localhost
port = 5432
`
	ext := NewYAMLExtractor(2000)
	result, err := ext.Extract([]byte(ini), ExtractOptions{Path: "config.ini"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	chunks := result.Chunks

	if len(chunks) != 2 {
		t.Fatalf("expected 2 chunks, got %d", len(chunks))
	}

	keys := make(map[string]bool)
	for _, ch := range chunks {
		keys[ch.Metadata["key"]] = true
	}
	if !keys["general"] {
		t.Error("missing chunk for [general]")
	}
	if !keys["database"] {
		t.Error("missing chunk for [database]")
	}
}

func TestEnvExtract(t *testing.T) {
	env := `# Database settings
DB_HOST=localhost
DB_PORT=5432
DB_NAME=mydb

# Server settings
SERVER_HOST=0.0.0.0
SERVER_PORT=8080
`
	ext := NewYAMLExtractor(2000)
	result, err := ext.Extract([]byte(env), ExtractOptions{Path: ".env"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	chunks := result.Chunks

	if len(chunks) != 2 {
		t.Fatalf("expected 2 chunks (2 groups), got %d", len(chunks))
	}

	// First group should have DB_HOST as key.
	if chunks[0].Metadata["key"] != "DB_HOST" {
		t.Errorf("expected first chunk key 'DB_HOST', got %q", chunks[0].Metadata["key"])
	}
}

func TestPropertiesExtract(t *testing.T) {
	props := `# App config
app.name=MyApp
app.version=1.0

# DB config
db.host=localhost
db.port=5432
`
	ext := NewYAMLExtractor(2000)
	result, err := ext.Extract([]byte(props), ExtractOptions{Path: "app.properties"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	chunks := result.Chunks

	if len(chunks) != 2 {
		t.Fatalf("expected 2 chunks, got %d", len(chunks))
	}
}

func TestYAMLEmptyContent(t *testing.T) {
	ext := NewYAMLExtractor(2000)
	result, err := ext.Extract([]byte(""), ExtractOptions{Path: "empty.yaml"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	chunks := result.Chunks
	if len(chunks) != 1 {
		t.Fatalf("expected 1 chunk for empty content, got %d", len(chunks))
	}
}

func TestYAMLLargeValue(t *testing.T) {
	// Create a YAML with a large value that exceeds maxChunkSize.
	largeValue := strings.Repeat("x", 300)
	yaml := "small: value\nlarge: " + largeValue + "\n"
	ext := NewYAMLExtractor(200)
	result, err := ext.Extract([]byte(yaml), ExtractOptions{Path: "big.yaml"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	chunks := result.Chunks

	// Should have at least 3 chunks: small key + large key split into parts.
	if len(chunks) < 3 {
		t.Fatalf("expected at least 3 chunks, got %d", len(chunks))
	}

	// The large key chunks should have "part" metadata.
	foundPart := false
	for _, ch := range chunks {
		if ch.Metadata["key"] == "large" && ch.Metadata["part"] != "" {
			foundPart = true
		}
	}
	if !foundPart {
		t.Error("expected large value to be split into parts")
	}
}

func TestExtFromPath(t *testing.T) {
	tests := []struct {
		path string
		want string
	}{
		{"config.yaml", ".yaml"},
		{"path/to/file.json", ".json"},
		{"noext", ""},
		{".hidden", ".hidden"},
	}
	for _, tt := range tests {
		got := extFromPath(tt.path)
		if got != tt.want {
			t.Errorf("extFromPath(%q) = %q, want %q", tt.path, got, tt.want)
		}
	}
}
