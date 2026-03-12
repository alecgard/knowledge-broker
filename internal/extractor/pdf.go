package extractor

import (
	"bytes"
	"fmt"
	"regexp"
	"strings"
	"time"

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

func (p *PDFExtractor) Extract(content []byte, opts ExtractOptions) (*ExtractResult, error) {
	reader := bytes.NewReader(content)
	pdfReader, err := pdf.NewReader(reader, int64(len(content)))
	if err != nil {
		return nil, fmt.Errorf("open PDF: %w", err)
	}

	// Extract document-level metadata dates.
	metadata := extractPDFMetadata(pdfReader)

	numPages := pdfReader.NumPage()
	if numPages == 0 {
		return &ExtractResult{
			Chunks: []model.Chunk{{
				Content:  "",
				Metadata: map[string]string{"page": "0"},
			}},
			Metadata: metadata,
		}, nil
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
		return &ExtractResult{
			Chunks: []model.Chunk{{
				Content:  "",
				Metadata: map[string]string{"page": "0"},
			}},
			Metadata: metadata,
		}, nil
	}

	return &ExtractResult{Chunks: chunks, Metadata: metadata}, nil
}

// extractPDFMetadata reads the PDF Info dictionary for ModDate or CreationDate
// and returns document-level metadata with content_date in ISO8601 format.
func extractPDFMetadata(r *pdf.Reader) map[string]string {
	meta := make(map[string]string)

	trailer := r.Trailer()
	info := trailer.Key("Info")
	if info.IsNull() {
		return meta
	}

	// Prefer ModDate over CreationDate.
	for _, key := range []string{"ModDate", "CreationDate"} {
		val := info.Key(key)
		if val.IsNull() {
			continue
		}
		dateStr := val.RawString()
		if dateStr == "" {
			continue
		}
		t, err := parsePDFDate(dateStr)
		if err == nil {
			meta["content_date"] = t.UTC().Format(time.RFC3339)
			break
		}
	}

	return meta
}

// pdfDateRe matches the PDF date format: D:YYYYMMDDHHmmSS with optional timezone.
var pdfDateRe = regexp.MustCompile(`^D:(\d{4})(\d{2})?(\d{2})?(\d{2})?(\d{2})?(\d{2})?([+-Z])?(\d{2})?'?(\d{2})?'?$`)

// parsePDFDate parses a PDF date string in the format D:YYYYMMDDHHmmSS+HH'mm'.
func parsePDFDate(s string) (time.Time, error) {
	s = strings.TrimSpace(s)

	m := pdfDateRe.FindStringSubmatch(s)
	if m == nil {
		return time.Time{}, fmt.Errorf("unrecognised PDF date format: %s", s)
	}

	year := m[1]
	month := "01"
	day := "01"
	hour := "00"
	min := "00"
	sec := "00"
	if m[2] != "" {
		month = m[2]
	}
	if m[3] != "" {
		day = m[3]
	}
	if m[4] != "" {
		hour = m[4]
	}
	if m[5] != "" {
		min = m[5]
	}
	if m[6] != "" {
		sec = m[6]
	}

	tzSign := m[7]
	tzHour := m[8]
	tzMin := m[9]

	tz := "Z"
	if tzSign == "+" || tzSign == "-" {
		if tzHour == "" {
			tzHour = "00"
		}
		if tzMin == "" {
			tzMin = "00"
		}
		tz = tzSign + tzHour + ":" + tzMin
	}

	dateStr := fmt.Sprintf("%s-%s-%sT%s:%s:%s%s", year, month, day, hour, min, sec, tz)
	return time.Parse(time.RFC3339, dateStr)
}
