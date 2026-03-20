package ui

import (
	"strings"
	"testing"
)

func TestEmbeddedIndex(t *testing.T) {
	data, err := staticFS.ReadFile("index.html")
	if err != nil {
		t.Fatalf("read embedded index.html: %v", err)
	}
	if len(data) == 0 {
		t.Fatal("embedded index.html is empty")
	}
	if !strings.Contains(string(data), "Knowledge Broker") {
		t.Fatal("index.html doesn't contain expected title")
	}
}
