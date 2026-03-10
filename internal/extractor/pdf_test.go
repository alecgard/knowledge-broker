package extractor

import (
	"testing"
)

func TestPDFInvalidContent(t *testing.T) {
	ext := NewPDFExtractor(2000)
	_, err := ext.Extract([]byte("not a pdf"), ExtractOptions{Path: "bad.pdf"})
	if err == nil {
		t.Error("expected error for invalid PDF content")
	}
}

func TestPDFEmptyContent(t *testing.T) {
	ext := NewPDFExtractor(2000)
	_, err := ext.Extract([]byte(""), ExtractOptions{Path: "empty.pdf"})
	if err == nil {
		t.Error("expected error for empty PDF content")
	}
}

func TestPDFFileTypes(t *testing.T) {
	ext := NewPDFExtractor(2000)
	types := ext.FileTypes()
	if len(types) != 1 || types[0] != ".pdf" {
		t.Errorf("expected [\".pdf\"], got %v", types)
	}
}

func TestPDFDefaultMaxChunkSize(t *testing.T) {
	ext := NewPDFExtractor(0)
	if ext.maxChunkSize != 2000 {
		t.Errorf("expected default maxChunkSize 2000, got %d", ext.maxChunkSize)
	}
}
