package query

import (
	"context"
	"testing"

	"github.com/knowledge-broker/knowledge-broker/pkg/model"
)

type mockLLM struct {
	response string
	err      error
}

func (m *mockLLM) StreamAnswer(_ context.Context, _ string, _ []model.Message, onText func(string)) (string, error) {
	if m.err != nil {
		return "", m.err
	}
	if onText != nil {
		onText(m.response)
	}
	return m.response, nil
}

func TestParseExpansions_Basic(t *testing.T) {
	input := "database selection decision\nPostgreSQL storage choice\ndata layer architecture"
	got := parseExpansions(input)
	if len(got) != 3 {
		t.Fatalf("expected 3, got %d: %v", len(got), got)
	}
	if got[0] != "database selection decision" {
		t.Fatalf("unexpected first expansion: %s", got[0])
	}
}

func TestParseExpansions_StripNumbering(t *testing.T) {
	input := "1. database selection\n2. storage choice\n3. data layer"
	got := parseExpansions(input)
	if len(got) != 3 {
		t.Fatalf("expected 3, got %d: %v", len(got), got)
	}
	if got[0] != "database selection" {
		t.Fatalf("expected numbering stripped, got: %s", got[0])
	}
}

func TestParseExpansions_StripBullets(t *testing.T) {
	input := "- database selection\n- storage choice"
	got := parseExpansions(input)
	if len(got) != 2 {
		t.Fatalf("expected 2, got %d", len(got))
	}
	if got[0] != "database selection" {
		t.Fatalf("expected bullet stripped, got: %s", got[0])
	}
}

func TestParseExpansions_CapsAt5(t *testing.T) {
	input := "a\nb\nc\nd\ne\nf\ng"
	got := parseExpansions(input)
	if len(got) != 5 {
		t.Fatalf("expected capped at 5, got %d", len(got))
	}
}

func TestParseExpansions_EmptyLines(t *testing.T) {
	input := "\n\nfoo\n\nbar\n\n"
	got := parseExpansions(input)
	if len(got) != 2 {
		t.Fatalf("expected 2, got %d", len(got))
	}
}

func TestParseExpansions_Empty(t *testing.T) {
	got := parseExpansions("")
	if len(got) != 0 {
		t.Fatalf("expected 0, got %d", len(got))
	}
}

func TestExpandQuery(t *testing.T) {
	llm := &mockLLM{response: "alternative one\nalternative two\nalternative three"}
	got, err := expandQuery(context.Background(), llm, "test query", nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != 3 {
		t.Fatalf("expected 3, got %d", len(got))
	}
}

func TestExpandQueryWithHints(t *testing.T) {
	llm := &mockLLM{response: "connectors pipeline\nextractors chunking"}
	hints := []string{"[README.md] Connectors pull content from sources"}
	got, err := expandQuery(context.Background(), llm, "data flow", hints)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("expected 2, got %d", len(got))
	}
}
