package extractor

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/knowledge-broker/knowledge-broker/pkg/model"
	"gopkg.in/yaml.v3"
)

// YAMLExtractor splits YAML, JSON, TOML, INI, .env, and .properties files
// into chunks by top-level keys or sections.
type YAMLExtractor struct {
	maxChunkSize int
}

// NewYAMLExtractor creates a YAMLExtractor with the given max chunk size.
// If maxChunkSize is 0, it defaults to 2000.
func NewYAMLExtractor(maxChunkSize int) *YAMLExtractor {
	if maxChunkSize <= 0 {
		maxChunkSize = 2000
	}
	return &YAMLExtractor{maxChunkSize: maxChunkSize}
}

func (y *YAMLExtractor) FileTypes() []string {
	return []string{".yaml", ".yml", ".toml", ".json", ".ini", ".conf", ".env", ".properties"}
}

func (y *YAMLExtractor) Extract(content []byte, opts ExtractOptions) ([]model.Chunk, error) {
	text := strings.TrimSpace(string(content))
	if text == "" {
		return []model.Chunk{{Content: "", Metadata: map[string]string{"key": ""}}}, nil
	}

	ext := strings.ToLower(extFromPath(opts.Path))

	switch ext {
	case ".yaml", ".yml":
		return y.extractYAML(content)
	case ".json":
		return y.extractJSON(content)
	case ".toml":
		return y.extractTOML(text)
	case ".ini", ".conf":
		return y.extractINI(text)
	case ".env", ".properties":
		return y.extractKeyValue(text)
	default:
		return y.extractYAML(content)
	}
}

// extractYAML parses YAML and chunks by top-level keys.
func (y *YAMLExtractor) extractYAML(content []byte) ([]model.Chunk, error) {
	var data map[string]interface{}
	if err := yaml.Unmarshal(content, &data); err != nil {
		// If YAML parsing fails, fall back to line-based chunking.
		return y.fallbackChunk(string(content))
	}

	if len(data) == 0 {
		return []model.Chunk{{Content: strings.TrimSpace(string(content)), Metadata: map[string]string{"key": ""}}}, nil
	}

	return y.chunkMap(data)
}

// extractJSON parses JSON and chunks by top-level keys.
func (y *YAMLExtractor) extractJSON(content []byte) ([]model.Chunk, error) {
	var data map[string]interface{}
	if err := json.Unmarshal(content, &data); err != nil {
		// Could be a JSON array or non-object; fall back.
		return y.fallbackChunk(string(content))
	}

	if len(data) == 0 {
		return []model.Chunk{{Content: strings.TrimSpace(string(content)), Metadata: map[string]string{"key": ""}}}, nil
	}

	return y.chunkMap(data)
}

// chunkMap produces one chunk per top-level key, serialised as YAML for readability.
func (y *YAMLExtractor) chunkMap(data map[string]interface{}) ([]model.Chunk, error) {
	// Sort keys for deterministic output.
	keys := make([]string, 0, len(data))
	for k := range data {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	var chunks []model.Chunk
	for _, key := range keys {
		val := data[key]
		// Render this key-value pair as YAML.
		single := map[string]interface{}{key: val}
		out, err := yaml.Marshal(single)
		if err != nil {
			out = []byte(fmt.Sprintf("%s: %v", key, val))
		}
		text := strings.TrimSpace(string(out))

		if len(text) <= y.maxChunkSize {
			chunks = append(chunks, model.Chunk{
				Content:  text,
				Metadata: map[string]string{"key": key},
			})
		} else {
			// Large value: split with fixed-size chunking.
			sub := fixedSizeChunks(text, y.maxChunkSize, 0)
			for i, s := range sub {
				chunks = append(chunks, model.Chunk{
					Content: s,
					Metadata: map[string]string{
						"key":  key,
						"part": fmt.Sprintf("%d", i+1),
					},
				})
			}
		}
	}

	if len(chunks) == 0 {
		return []model.Chunk{{Content: "", Metadata: map[string]string{"key": ""}}}, nil
	}
	return chunks, nil
}

// extractTOML splits TOML content by [section] headers.
func (y *YAMLExtractor) extractTOML(text string) ([]model.Chunk, error) {
	return y.extractSectionBased(text, func(line string) (string, bool) {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "[") && strings.Contains(trimmed, "]") {
			// Extract section name between brackets.
			end := strings.Index(trimmed, "]")
			section := trimmed[1:end]
			return section, true
		}
		return "", false
	})
}

// extractINI splits INI/conf content by [section] headers.
func (y *YAMLExtractor) extractINI(text string) ([]model.Chunk, error) {
	return y.extractSectionBased(text, func(line string) (string, bool) {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "[") && strings.HasSuffix(trimmed, "]") {
			section := trimmed[1 : len(trimmed)-1]
			return section, true
		}
		return "", false
	})
}

// extractSectionBased splits text by section headers detected by the given function.
func (y *YAMLExtractor) extractSectionBased(text string, isSection func(string) (string, bool)) ([]model.Chunk, error) {
	lines := strings.Split(text, "\n")

	type section struct {
		name  string
		lines []string
	}

	var sections []section
	current := section{name: ""}

	for _, line := range lines {
		if name, ok := isSection(line); ok {
			// Save previous section.
			if len(current.lines) > 0 || current.name != "" {
				sections = append(sections, current)
			}
			current = section{name: name, lines: []string{line}}
		} else {
			current.lines = append(current.lines, line)
		}
	}
	// Save last section.
	if len(current.lines) > 0 || current.name != "" {
		sections = append(sections, current)
	}

	var chunks []model.Chunk
	for _, sec := range sections {
		content := strings.TrimSpace(strings.Join(sec.lines, "\n"))
		if content == "" && sec.name == "" {
			continue
		}

		if len(content) <= y.maxChunkSize {
			chunks = append(chunks, model.Chunk{
				Content:  content,
				Metadata: map[string]string{"key": sec.name},
			})
		} else {
			sub := fixedSizeChunks(content, y.maxChunkSize, 0)
			for i, s := range sub {
				chunks = append(chunks, model.Chunk{
					Content: s,
					Metadata: map[string]string{
						"key":  sec.name,
						"part": fmt.Sprintf("%d", i+1),
					},
				})
			}
		}
	}

	if len(chunks) == 0 {
		return []model.Chunk{{Content: "", Metadata: map[string]string{"key": ""}}}, nil
	}
	return chunks, nil
}

// extractKeyValue handles .env and .properties files. Each group of
// related lines (separated by blank lines or comments) becomes a chunk.
func (y *YAMLExtractor) extractKeyValue(text string) ([]model.Chunk, error) {
	lines := strings.Split(text, "\n")
	var chunks []model.Chunk
	var group []string

	flush := func() {
		content := strings.TrimSpace(strings.Join(group, "\n"))
		if content == "" {
			return
		}
		// Extract the first key as the chunk key.
		key := ""
		for _, l := range group {
			l = strings.TrimSpace(l)
			if l == "" || strings.HasPrefix(l, "#") || strings.HasPrefix(l, "!") {
				continue
			}
			if idx := strings.IndexAny(l, "=:"); idx > 0 {
				key = strings.TrimSpace(l[:idx])
				break
			}
		}
		chunks = append(chunks, model.Chunk{
			Content:  content,
			Metadata: map[string]string{"key": key},
		})
	}

	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			if len(group) > 0 {
				flush()
				group = nil
			}
			continue
		}
		group = append(group, line)
	}
	if len(group) > 0 {
		flush()
	}

	if len(chunks) == 0 {
		return []model.Chunk{{Content: "", Metadata: map[string]string{"key": ""}}}, nil
	}
	return chunks, nil
}

// fallbackChunk splits content using fixed-size chunking when parsing fails.
func (y *YAMLExtractor) fallbackChunk(text string) ([]model.Chunk, error) {
	parts := fixedSizeChunks(text, y.maxChunkSize, 0)
	var chunks []model.Chunk
	for i, p := range parts {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		chunks = append(chunks, model.Chunk{
			Content: p,
			Metadata: map[string]string{
				"key":  "",
				"part": fmt.Sprintf("%d", i+1),
			},
		})
	}
	if len(chunks) == 0 {
		chunks = append(chunks, model.Chunk{Content: "", Metadata: map[string]string{"key": ""}})
	}
	return chunks, nil
}

// extFromPath returns the file extension from a path.
func extFromPath(path string) string {
	for i := len(path) - 1; i >= 0; i-- {
		if path[i] == '.' {
			return path[i:]
		}
		if path[i] == '/' || path[i] == '\\' {
			break
		}
	}
	return ""
}
