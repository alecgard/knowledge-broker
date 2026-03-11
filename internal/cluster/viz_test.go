package cluster

import (
	"bytes"
	"strings"
	"testing"
)

func TestGenerateVizHTML_ContainsPlotlyAndData(t *testing.T) {
	points := []VizPoint{
		{X: 1.0, Y: 2.0, Cluster: 0, Topic: "auth", Path: "auth/login.go", Snippet: "login handler", ID: "abc123"},
		{X: 5.0, Y: 6.0, Cluster: 1, Topic: "db", Path: "db/schema.sql", Snippet: "CREATE TABLE", ID: "def456"},
	}

	var buf bytes.Buffer
	if err := GenerateVizHTML(points, &buf); err != nil {
		t.Fatalf("GenerateVizHTML: %v", err)
	}

	html := buf.String()

	if !strings.Contains(html, "plotly") {
		t.Error("output should contain plotly script reference")
	}
	if !strings.Contains(html, "<div id=\"plot\">") {
		t.Error("output should contain plot div")
	}
	if !strings.Contains(html, "abc123") {
		t.Error("output should contain point ID data")
	}
	if !strings.Contains(html, "auth/login.go") {
		t.Error("output should contain point path data")
	}
	if !strings.Contains(html, "def456") {
		t.Error("output should contain second point ID")
	}
}

func TestGenerateVizHTML_EmptyPoints(t *testing.T) {
	var buf bytes.Buffer
	if err := GenerateVizHTML(nil, &buf); err != nil {
		t.Fatalf("GenerateVizHTML with nil points: %v", err)
	}

	html := buf.String()
	if !strings.Contains(html, "var data = null;") && !strings.Contains(html, "var data = [];") {
		// nil marshals to "null", empty slice to "[]" — both are valid
		if !strings.Contains(html, "var data =") {
			t.Error("output should still contain data variable")
		}
	}
}
