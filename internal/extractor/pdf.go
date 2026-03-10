package extractor

import (
	"bytes"
	"fmt"
	"strings"

	"github.com/knowledge-broker/knowledge-broker/pkg/model"
	"github.com/ledongthuc/pdf"
)

// PDFExtractor extracts text from PDF files and chunks it by page.
type PDFExtractor struct {
	maxChunkSize int
}

// NewPDFExtractor creates a PDFExtractor with the given max chunk size.
// If maxChunkSize is 0, it defaults to 2000.
func NewPDFExtractor(maxChunkSize int) *PDFExtractor {
	if maxChunkSize <= 0 {
		maxChunkSize = 2000
	}
	return &PDFExtractor{maxChunkSize: maxChunkSize}
}

func (p *PDFExtractor) FileTypes() []string {
	return []string{".pdf"}
}

func (p *PDFExtractor) Extract(content []byte, opts ExtractOptions) ([]model.Chunk, error) {
	reader := bytes.NewReader(content)
	pdfReader, err := pdf.NewReader(reader, int64(len(content)))
	if err != nil {
		return nil, fmt.Errorf("open PDF: %w", err)
	}

	numPages := pdfReader.NumPage()
	if numPages == 0 {
		return []model.Chunk{{
			Content:  "",
			Metadata: map[string]string{"page": "0"},
		}}, nil
	}

	var chunks []model.Chunk

	for pageNum := 1; pageNum <= numPages; pageNum++ {
		page := pdfReader.Page(pageNum)
		if page.V.IsNull() {
			continue
		}

		text, err := page.GetPlainText(nil)
		if err != nil {
			// Skip pages that fail to extract; log silently.
			continue
		}

		text = strings.TrimSpace(text)
		if text == "" {
			continue
		}

		pageLabel := fmt.Sprintf("%d", pageNum)

		if len(text) <= p.maxChunkSize {
			chunks = append(chunks, model.Chunk{
				Content: text,
				Metadata: map[string]string{
					"page": pageLabel,
				},
			})
		} else {
			// Split large pages with fixed-size chunking.
			sub := fixedSizeChunks(text, p.maxChunkSize, 0)
			for i, s := range sub {
				chunks = append(chunks, model.Chunk{
					Content: s,
					Metadata: map[string]string{
						"page": pageLabel,
						"part": fmt.Sprintf("%d", i+1),
					},
				})
			}
		}
	}

	if len(chunks) == 0 {
		return []model.Chunk{{
			Content:  "",
			Metadata: map[string]string{"page": "0"},
		}}, nil
	}

	return chunks, nil
}
