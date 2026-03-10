package extractor

import (
	"fmt"
	"strings"

	"github.com/knowledge-broker/knowledge-broker/pkg/model"
)

// MarkdownExtractor splits markdown files at heading boundaries.
type MarkdownExtractor struct {
	maxChunkSize int
}

// NewMarkdownExtractor creates a MarkdownExtractor with the given max chunk size.
// If maxChunkSize is 0, it defaults to 2000.
func NewMarkdownExtractor(maxChunkSize int) *MarkdownExtractor {
	if maxChunkSize <= 0 {
		maxChunkSize = 2000
	}
	return &MarkdownExtractor{maxChunkSize: maxChunkSize}
}

func (m *MarkdownExtractor) FileTypes() []string {
	return []string{".md"}
}

func (m *MarkdownExtractor) Extract(content []byte, opts ExtractOptions) ([]model.Chunk, error) {
	text := string(content)

	// Strip frontmatter (--- delimited block at the start).
	text = stripFrontmatter(text)

	sections := splitMarkdownSections(text)

	var chunks []model.Chunk
	for _, sec := range sections {
		body := strings.TrimSpace(sec.body)
		if body == "" && sec.heading == "" {
			continue
		}

		// Prefix the heading into the chunk content for context.
		fullContent := body
		if sec.heading != "" {
			fullContent = sec.heading + "\n" + body
		}
		fullContent = strings.TrimSpace(fullContent)

		if len(fullContent) <= m.maxChunkSize {
			if fullContent == "" {
				continue
			}
			chunks = append(chunks, model.Chunk{
				Content: fullContent,
				Metadata: map[string]string{
					"heading": sec.heading,
				},
			})
		} else {
			// Fall back to fixed-size splitting within the section.
			sub := fixedSizeChunks(fullContent, m.maxChunkSize, 0)
			for i, s := range sub {
				chunks = append(chunks, model.Chunk{
					Content: s,
					Metadata: map[string]string{
						"heading": sec.heading,
						"part":    fmt.Sprintf("%d", i+1),
					},
				})
			}
		}
	}

	if len(chunks) == 0 {
		// Return one empty chunk rather than nil so callers don't need nil checks.
		chunks = append(chunks, model.Chunk{
			Content:  "",
			Metadata: map[string]string{"heading": ""},
		})
	}

	return chunks, nil
}

type markdownSection struct {
	heading string
	body    string
}

// splitMarkdownSections splits markdown text at ## and ### headings.
func splitMarkdownSections(text string) []markdownSection {
	lines := strings.Split(text, "\n")
	var sections []markdownSection
	currentHeading := ""
	var bodyLines []string

	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if isHeading(trimmed) {
			// Save previous section.
			sections = append(sections, markdownSection{
				heading: currentHeading,
				body:    strings.Join(bodyLines, "\n"),
			})
			currentHeading = trimmed
			bodyLines = nil
		} else {
			bodyLines = append(bodyLines, line)
		}
	}
	// Save last section.
	sections = append(sections, markdownSection{
		heading: currentHeading,
		body:    strings.Join(bodyLines, "\n"),
	})
	return sections
}

// isHeading returns true for lines starting with ## or ### (but not # or ####+).
func isHeading(line string) bool {
	if strings.HasPrefix(line, "### ") {
		return true
	}
	if strings.HasPrefix(line, "## ") {
		return true
	}
	return false
}

// stripFrontmatter removes YAML frontmatter delimited by --- at the start of the document.
func stripFrontmatter(text string) string {
	if !strings.HasPrefix(text, "---") {
		return text
	}
	// Find the closing ---.
	rest := text[3:]
	idx := strings.Index(rest, "\n---")
	if idx == -1 {
		return text
	}
	// Skip past the closing --- and the newline after it.
	after := rest[idx+4:]
	if strings.HasPrefix(after, "\n") {
		after = after[1:]
	}
	return after
}

// fixedSizeChunks splits text into chunks of at most maxSize characters.
// overlap specifies how many characters to repeat between chunks.
func fixedSizeChunks(text string, maxSize, overlap int) []string {
	if len(text) <= maxSize {
		return []string{text}
	}
	var chunks []string
	start := 0
	for start < len(text) {
		end := start + maxSize
		if end >= len(text) {
			chunks = append(chunks, text[start:])
			break
		}
		// Try to split at a paragraph, sentence, or word boundary.
		chunk := text[start:end]
		splitAt := findBestSplit(chunk)
		actual := start + splitAt
		chunks = append(chunks, strings.TrimSpace(text[start:actual]))
		next := actual - overlap
		if next <= start {
			next = actual
		}
		start = next
	}
	return chunks
}

// findBestSplit finds the best split point within chunk, preferring paragraph,
// sentence, then word boundaries. Returns index within chunk.
func findBestSplit(chunk string) int {
	n := len(chunk)

	// Look for paragraph boundary (double newline) in the last quarter.
	searchStart := n * 3 / 4
	if idx := strings.LastIndex(chunk[searchStart:], "\n\n"); idx != -1 {
		return searchStart + idx + 2
	}

	// Look for sentence boundary in the last half.
	searchStart = n / 2
	for i := n - 1; i >= searchStart; i-- {
		if chunk[i] == '.' || chunk[i] == '!' || chunk[i] == '?' {
			if i+1 < n && (chunk[i+1] == ' ' || chunk[i+1] == '\n') {
				return i + 1
			}
			if i+1 == n {
				return n
			}
		}
	}

	// Look for word boundary (space or newline) in the last quarter.
	searchStart = n * 3 / 4
	for i := n - 1; i >= searchStart; i-- {
		if chunk[i] == ' ' || chunk[i] == '\n' {
			return i + 1
		}
	}

	return n
}
