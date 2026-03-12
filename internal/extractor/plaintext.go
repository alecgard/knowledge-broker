package extractor

import (
	"fmt"
	"strings"

	"github.com/knowledge-broker/knowledge-broker/pkg/model"
)

// PlaintextExtractor splits arbitrary text files into fixed-size chunks.
type PlaintextExtractor struct {
	maxChunkSize int
	overlap      int
}

// NewPlaintextExtractor creates a PlaintextExtractor.
// Defaults: maxChunkSize=1500, overlap=150.
func NewPlaintextExtractor(maxChunkSize, overlap int) *PlaintextExtractor {
	if maxChunkSize <= 0 {
		maxChunkSize = 1500
	}
	if overlap < 0 {
		overlap = 150
	}
	return &PlaintextExtractor{maxChunkSize: maxChunkSize, overlap: overlap}
}

func (p *PlaintextExtractor) FileTypes() []string {
	return []string{
		".txt", ".text", ".log", ".cfg", ".conf", ".ini",
		".yaml", ".yml", ".json", ".toml", ".xml", ".csv",
	}
}

func (p *PlaintextExtractor) Extract(content []byte, opts ExtractOptions) (*ExtractResult, error) {
	text := string(content)
	if strings.TrimSpace(text) == "" {
		return &ExtractResult{Chunks: []model.Chunk{{Content: "", Metadata: map[string]string{"offset": "0"}}}}, nil
	}

	// Try splitting at paragraph boundaries first.
	paragraphs := splitParagraphs(text)
	var chunks []model.Chunk
	offset := 0

	if len(paragraphs) > 1 {
		// Merge paragraphs into chunks that fit within maxChunkSize.
		var current strings.Builder
		currentOffset := 0

		for _, para := range paragraphs {
			para = strings.TrimSpace(para)
			if para == "" {
				continue
			}

			// If adding this paragraph would exceed the limit, flush current.
			if current.Len() > 0 && current.Len()+len(para)+2 > p.maxChunkSize {
				chunks = append(chunks, model.Chunk{
					Content: strings.TrimSpace(current.String()),
					Metadata: map[string]string{
						"offset": fmt.Sprintf("%d", currentOffset),
					},
				})
				// Apply overlap: keep the tail of the current chunk.
				tail := overlapTail(current.String(), p.overlap)
				current.Reset()
				currentOffset = offset - len(tail)
				if tail != "" {
					current.WriteString(tail)
				}
			}

			if current.Len() == 0 {
				currentOffset = offset
			} else {
				current.WriteString("\n\n")
			}
			current.WriteString(para)
			offset += len(para) + 2 // account for the \n\n separator
		}

		if current.Len() > 0 {
			chunks = append(chunks, model.Chunk{
				Content: strings.TrimSpace(current.String()),
				Metadata: map[string]string{
					"offset": fmt.Sprintf("%d", currentOffset),
				},
			})
		}

		// If paragraph merging produced chunks that are still too large, re-split them.
		var result []model.Chunk
		for _, ch := range chunks {
			if len(ch.Content) <= p.maxChunkSize {
				result = append(result, ch)
			} else {
				sub := fixedSizeChunksWithOverlap(ch.Content, p.maxChunkSize, p.overlap)
				for i, s := range sub {
					result = append(result, model.Chunk{
						Content: s,
						Metadata: map[string]string{
							"offset": fmt.Sprintf("%d", i), // sub-chunk index as offset
						},
					})
				}
			}
		}
		return &ExtractResult{Chunks: result}, nil
	}

	// No paragraph boundaries; use fixed-size splitting.
	parts := fixedSizeChunksWithOverlap(text, p.maxChunkSize, p.overlap)
	for i, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		chunks = append(chunks, model.Chunk{
			Content: part,
			Metadata: map[string]string{
				"offset": fmt.Sprintf("%d", i*p.maxChunkSize),
			},
		})
	}

	if len(chunks) == 0 {
		chunks = append(chunks, model.Chunk{Content: "", Metadata: map[string]string{"offset": "0"}})
	}
	return &ExtractResult{Chunks: chunks}, nil
}

// splitParagraphs splits text at double-newline boundaries.
func splitParagraphs(text string) []string {
	return strings.Split(text, "\n\n")
}

// overlapTail returns up to n characters from the end of s.
func overlapTail(s string, n int) string {
	if n <= 0 || len(s) == 0 {
		return ""
	}
	if len(s) <= n {
		return s
	}
	// Try to break at a word boundary.
	tail := s[len(s)-n:]
	if idx := strings.Index(tail, " "); idx != -1 && idx < len(tail)/2 {
		tail = tail[idx+1:]
	}
	return tail
}

// fixedSizeChunksWithOverlap splits text into chunks with overlap.
func fixedSizeChunksWithOverlap(text string, maxSize, overlap int) []string {
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
