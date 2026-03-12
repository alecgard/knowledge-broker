package extractor

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/knowledge-broker/knowledge-broker/pkg/model"
)

// JupyterExtractor splits Jupyter notebook (.ipynb) files into chunks by cell.
type JupyterExtractor struct {
	maxChunkSize int
}

// NewJupyterExtractor creates a JupyterExtractor with the given max chunk size.
// If maxChunkSize is 0, it defaults to 2000.
func NewJupyterExtractor(maxChunkSize int) *JupyterExtractor {
	if maxChunkSize <= 0 {
		maxChunkSize = 2000
	}
	return &JupyterExtractor{maxChunkSize: maxChunkSize}
}

func (j *JupyterExtractor) FileTypes() []string {
	return []string{".ipynb"}
}

// jupyterNotebook represents the top-level structure of a Jupyter notebook.
type jupyterNotebook struct {
	Cells []jupyterCell `json:"cells"`
}

// jupyterCell represents a single cell in a Jupyter notebook.
type jupyterCell struct {
	CellType string   `json:"cell_type"` // "code", "markdown", "raw"
	Source   []string `json:"source"`     // lines of source content
}

func (j *JupyterExtractor) Extract(content []byte, opts ExtractOptions) (*ExtractResult, error) {
	var nb jupyterNotebook
	if err := json.Unmarshal(content, &nb); err != nil {
		return nil, fmt.Errorf("parse notebook JSON: %w", err)
	}

	var chunks []model.Chunk
	cellNum := 0

	for _, cell := range nb.Cells {
		// Skip raw cells.
		if cell.CellType != "code" && cell.CellType != "markdown" {
			continue
		}

		cellNum++
		text := strings.TrimSpace(strings.Join(cell.Source, ""))
		if text == "" {
			continue
		}

		if len(text) <= j.maxChunkSize {
			chunks = append(chunks, model.Chunk{
				Content: text,
				Metadata: map[string]string{
					"cell_type":   cell.CellType,
					"cell_number": fmt.Sprintf("%d", cellNum),
				},
			})
		} else {
			// Split large cells with fixed-size chunking.
			sub := fixedSizeChunks(text, j.maxChunkSize, 0)
			for i, s := range sub {
				chunks = append(chunks, model.Chunk{
					Content: s,
					Metadata: map[string]string{
						"cell_type":   cell.CellType,
						"cell_number": fmt.Sprintf("%d", cellNum),
						"part":        fmt.Sprintf("%d", i+1),
					},
				})
			}
		}
	}

	// Merge small consecutive cells of the same type to reduce fragment count.
	chunks = j.mergeSmallChunks(chunks)

	if len(chunks) == 0 {
		return &ExtractResult{Chunks: []model.Chunk{{
			Content:  "",
			Metadata: map[string]string{"cell_type": "", "cell_number": "0"},
		}}}, nil
	}

	return &ExtractResult{Chunks: chunks}, nil
}

// mergeSmallChunks combines consecutive chunks of the same cell type when
// their combined size fits within maxChunkSize.
func (j *JupyterExtractor) mergeSmallChunks(chunks []model.Chunk) []model.Chunk {
	if len(chunks) <= 1 {
		return chunks
	}

	var merged []model.Chunk
	current := chunks[0]

	for i := 1; i < len(chunks); i++ {
		next := chunks[i]

		// Only merge if same cell type, no "part" metadata (not already split),
		// and combined size fits.
		sameCellType := current.Metadata["cell_type"] == next.Metadata["cell_type"]
		currentNotSplit := current.Metadata["part"] == ""
		nextNotSplit := next.Metadata["part"] == ""
		fits := len(current.Content)+len(next.Content)+2 <= j.maxChunkSize

		if sameCellType && currentNotSplit && nextNotSplit && fits {
			separator := "\n\n"
			if current.Metadata["cell_type"] == "code" {
				separator = "\n\n"
			}
			current = model.Chunk{
				Content: current.Content + separator + next.Content,
				Metadata: map[string]string{
					"cell_type":   current.Metadata["cell_type"],
					"cell_number": current.Metadata["cell_number"] + "-" + next.Metadata["cell_number"],
				},
			}
		} else {
			merged = append(merged, current)
			current = next
		}
	}
	merged = append(merged, current)
	return merged
}
