package extractor

import (
	"fmt"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/knowledge-broker/knowledge-broker/pkg/model"
)

// CodeExtractor splits source code files at function/class/type boundaries.
type CodeExtractor struct {
	maxChunkSize int
}

// NewCodeExtractor creates a CodeExtractor with the given max chunk size.
// If maxChunkSize is 0, it defaults to 2000.
func NewCodeExtractor(maxChunkSize int) *CodeExtractor {
	if maxChunkSize <= 0 {
		maxChunkSize = 2000
	}
	return &CodeExtractor{maxChunkSize: maxChunkSize}
}

func (c *CodeExtractor) FileTypes() []string {
	return []string{".go", ".py", ".js", ".ts", ".jsx", ".tsx", ".java", ".rs", ".rb"}
}

// languagePatterns maps file extensions to boundary-detection regexes.
var languagePatterns = map[string][]*regexp.Regexp{
	".go": {
		regexp.MustCompile(`^func `),
		regexp.MustCompile(`^type `),
	},
	".py": {
		regexp.MustCompile(`^def `),
		regexp.MustCompile(`^class `),
	},
	".js": {
		regexp.MustCompile(`^function `),
		regexp.MustCompile(`^class `),
		regexp.MustCompile(`^export\s+(default\s+)?(function|class|const|async\s+function)\b`),
	},
	".ts": {
		regexp.MustCompile(`^function `),
		regexp.MustCompile(`^class `),
		regexp.MustCompile(`^export\s+(default\s+)?(function|class|const|async\s+function)\b`),
	},
	".jsx": {
		regexp.MustCompile(`^function `),
		regexp.MustCompile(`^class `),
		regexp.MustCompile(`^export\s+(default\s+)?(function|class|const|async\s+function)\b`),
	},
	".tsx": {
		regexp.MustCompile(`^function `),
		regexp.MustCompile(`^class `),
		regexp.MustCompile(`^export\s+(default\s+)?(function|class|const|async\s+function)\b`),
	},
	".java": {
		regexp.MustCompile(`(public|private|protected).*\b(class|interface|void|int|String|boolean|static)\b`),
	},
	".rs": {
		regexp.MustCompile(`^(pub\s+)?(fn|struct|enum|impl|trait)\s+`),
	},
	".rb": {
		regexp.MustCompile(`^(def |class |module )`),
	},
}

func (c *CodeExtractor) Extract(content []byte, opts ExtractOptions) ([]model.Chunk, error) {
	text := string(content)
	ext := strings.ToLower(filepath.Ext(opts.Path))

	patterns, ok := languagePatterns[ext]
	if !ok {
		// Unknown extension, fall back to plaintext chunking.
		return plaintextChunk(text, c.maxChunkSize, 0)
	}

	lines := strings.Split(text, "\n")

	// Extract preamble (package/import lines) for context.
	preamble := extractPreamble(lines, ext)

	// Find boundary lines.
	type boundary struct {
		lineIdx  int
		declLine string
	}
	var boundaries []boundary
	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		for _, pat := range patterns {
			if pat.MatchString(trimmed) {
				boundaries = append(boundaries, boundary{lineIdx: i, declLine: trimmed})
				break
			}
		}
	}

	if len(boundaries) == 0 {
		// No recognizable boundaries; fall back to plaintext chunking.
		return plaintextChunk(text, c.maxChunkSize, 0)
	}

	var chunks []model.Chunk

	// If there's content before the first boundary, include it.
	if boundaries[0].lineIdx > 0 {
		pre := strings.TrimSpace(strings.Join(lines[:boundaries[0].lineIdx], "\n"))
		if pre != "" {
			chunkContent := pre
			if len(chunkContent) <= c.maxChunkSize {
				chunks = append(chunks, model.Chunk{
					Content:  chunkContent,
					Metadata: map[string]string{"type": "preamble", "name": ""},
				})
			} else {
				sub := fixedSizeChunks(chunkContent, c.maxChunkSize, 0)
				for j, s := range sub {
					chunks = append(chunks, model.Chunk{
						Content: s,
						Metadata: map[string]string{
							"type": "preamble",
							"name": "",
							"part": fmt.Sprintf("%d", j+1),
						},
					})
				}
			}
		}
	}

	// Create a chunk for each boundary section.
	for i, b := range boundaries {
		startLine := b.lineIdx
		var endLine int
		if i+1 < len(boundaries) {
			endLine = boundaries[i+1].lineIdx
		} else {
			endLine = len(lines)
		}

		sectionText := strings.TrimRight(strings.Join(lines[startLine:endLine], "\n"), "\n ")
		declType, declName := parseDecleration(b.declLine, ext)

		// Add preamble as prefix for the first real code chunk if not already included.
		if i == 0 && preamble != "" && len(chunks) == 0 {
			sectionText = preamble + "\n\n" + sectionText
		}

		if len(sectionText) <= c.maxChunkSize {
			chunks = append(chunks, model.Chunk{
				Content: sectionText,
				Metadata: map[string]string{
					"type": declType,
					"name": declName,
				},
			})
		} else {
			sub := fixedSizeChunks(sectionText, c.maxChunkSize, 0)
			for j, s := range sub {
				chunks = append(chunks, model.Chunk{
					Content: s,
					Metadata: map[string]string{
						"type": declType,
						"name": declName,
						"part": fmt.Sprintf("%d", j+1),
					},
				})
			}
		}
	}

	if len(chunks) == 0 {
		return plaintextChunk(text, c.maxChunkSize, 0)
	}

	return chunks, nil
}

// extractPreamble returns package/import lines for context.
func extractPreamble(lines []string, ext string) string {
	var preambleLines []string
	inImportBlock := false

	for _, line := range lines {
		trimmed := strings.TrimSpace(line)

		switch ext {
		case ".go":
			if strings.HasPrefix(trimmed, "package ") {
				preambleLines = append(preambleLines, line)
				continue
			}
			if strings.HasPrefix(trimmed, "import ") || trimmed == "import (" {
				inImportBlock = trimmed == "import ("
				preambleLines = append(preambleLines, line)
				continue
			}
			if inImportBlock {
				preambleLines = append(preambleLines, line)
				if trimmed == ")" {
					inImportBlock = false
				}
				continue
			}
		case ".py":
			if strings.HasPrefix(trimmed, "import ") || strings.HasPrefix(trimmed, "from ") {
				preambleLines = append(preambleLines, line)
				continue
			}
		case ".js", ".ts", ".jsx", ".tsx":
			if strings.HasPrefix(trimmed, "import ") || strings.HasPrefix(trimmed, "const ") ||
				strings.HasPrefix(trimmed, "require(") {
				// Only include import statements, not const declarations in general.
				if strings.HasPrefix(trimmed, "import ") {
					preambleLines = append(preambleLines, line)
				}
				continue
			}
		case ".java":
			if strings.HasPrefix(trimmed, "package ") || strings.HasPrefix(trimmed, "import ") {
				preambleLines = append(preambleLines, line)
				continue
			}
		case ".rs":
			if strings.HasPrefix(trimmed, "use ") || strings.HasPrefix(trimmed, "mod ") {
				preambleLines = append(preambleLines, line)
				continue
			}
		case ".rb":
			if strings.HasPrefix(trimmed, "require ") || strings.HasPrefix(trimmed, "require_relative ") {
				preambleLines = append(preambleLines, line)
				continue
			}
		}

		// Stop collecting preamble once we hit non-preamble, non-blank lines.
		if trimmed != "" && !strings.HasPrefix(trimmed, "//") && !strings.HasPrefix(trimmed, "#") &&
			!strings.HasPrefix(trimmed, "/*") && !strings.HasPrefix(trimmed, "*") {
			break
		}
	}

	return strings.TrimSpace(strings.Join(preambleLines, "\n"))
}

// parseDecleration extracts the type and name from a declaration line.
func parseDecleration(line string, ext string) (declType, name string) {
	switch ext {
	case ".go":
		if strings.HasPrefix(line, "func ") {
			return "function", extractName(line, "func ")
		}
		if strings.HasPrefix(line, "type ") {
			return "type", extractName(line, "type ")
		}
	case ".py":
		if strings.HasPrefix(line, "def ") {
			return "function", extractName(line, "def ")
		}
		if strings.HasPrefix(line, "class ") {
			return "class", extractName(line, "class ")
		}
	case ".js", ".ts", ".jsx", ".tsx":
		if strings.HasPrefix(line, "class ") {
			return "class", extractName(line, "class ")
		}
		if strings.HasPrefix(line, "function ") {
			return "function", extractName(line, "function ")
		}
		if strings.Contains(line, "class ") {
			return "class", extractNameAfter(line, "class ")
		}
		if strings.Contains(line, "function ") {
			return "function", extractNameAfter(line, "function ")
		}
		if strings.Contains(line, "const ") {
			return "function", extractNameAfter(line, "const ")
		}
		if strings.Contains(line, "async function ") {
			return "function", extractNameAfter(line, "async function ")
		}
	case ".java":
		if strings.Contains(line, "class ") {
			return "class", extractNameAfter(line, "class ")
		}
		if strings.Contains(line, "interface ") {
			return "interface", extractNameAfter(line, "interface ")
		}
		return "function", extractJavaMethodName(line)
	case ".rs":
		for _, kw := range []string{"fn ", "struct ", "enum ", "impl ", "trait "} {
			if strings.Contains(line, kw) {
				typ := strings.TrimSpace(kw)
				return typ, extractNameAfter(line, kw)
			}
		}
	case ".rb":
		if strings.HasPrefix(line, "def ") {
			return "function", extractName(line, "def ")
		}
		if strings.HasPrefix(line, "class ") {
			return "class", extractName(line, "class ")
		}
		if strings.HasPrefix(line, "module ") {
			return "module", extractName(line, "module ")
		}
	}
	return "unknown", ""
}

// extractName returns the first word after the prefix.
func extractName(line, prefix string) string {
	rest := strings.TrimPrefix(line, prefix)
	return firstIdentifier(rest)
}

// extractNameAfter returns the first word after the keyword in the line.
func extractNameAfter(line, keyword string) string {
	idx := strings.Index(line, keyword)
	if idx == -1 {
		return ""
	}
	rest := line[idx+len(keyword):]
	return firstIdentifier(rest)
}

// firstIdentifier extracts the first identifier (letters, digits, underscore) from s.
func firstIdentifier(s string) string {
	s = strings.TrimSpace(s)
	var name []byte
	for i := 0; i < len(s); i++ {
		c := s[i]
		if (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') || c == '_' {
			name = append(name, c)
		} else {
			break
		}
	}
	return string(name)
}

// extractJavaMethodName tries to pull a method name from a Java declaration.
func extractJavaMethodName(line string) string {
	// Look for the pattern: word( which is the method name.
	re := regexp.MustCompile(`(\w+)\s*\(`)
	matches := re.FindStringSubmatch(line)
	if len(matches) > 1 {
		return matches[1]
	}
	return ""
}

// plaintextChunk is a helper that splits text into fixed-size chunks.
func plaintextChunk(text string, maxSize, overlap int) ([]model.Chunk, error) {
	parts := fixedSizeChunks(text, maxSize, overlap)
	var chunks []model.Chunk
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		chunks = append(chunks, model.Chunk{
			Content:  p,
			Metadata: map[string]string{},
		})
	}
	if len(chunks) == 0 {
		chunks = append(chunks, model.Chunk{Content: "", Metadata: map[string]string{}})
	}
	return chunks, nil
}
