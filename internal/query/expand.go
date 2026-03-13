package query

import (
	"context"
	"fmt"
	"strings"

	"github.com/knowledge-broker/knowledge-broker/pkg/model"
)

const expandSystemPrompt = "You are a search query expansion assistant. Return only the alternative queries, one per line, with no numbering, bullets, or explanation."

const expandUserPromptWithContext = `Generate 3 to 5 alternative phrasings of this search query. Each rephrasing should use different vocabulary or approach the topic from a different angle. Include domain-specific terms, synonyms, and more concrete restatements.

Here are excerpts from documents in the corpus to help you use the right vocabulary:
%s

Query: %s`

const expandUserPromptPlain = `Generate 3 to 5 alternative phrasings of this search query. Each rephrasing should use different vocabulary or approach the topic from a different angle. Include domain-specific terms, synonyms, and more concrete restatements.

Query: %s`

// expandQuery uses the LLM to generate alternative phrasings of the query.
// It first does a quick BM25 lookup to extract domain vocabulary, then asks the
// LLM to rephrase using those terms. Returns alternative queries (not including
// the original).
func expandQuery(ctx context.Context, llm LLM, query string, vocabHints []string) ([]string, error) {
	var prompt string
	if len(vocabHints) > 0 {
		prompt = fmt.Sprintf(expandUserPromptWithContext, strings.Join(vocabHints, "\n"), query)
	} else {
		prompt = fmt.Sprintf(expandUserPromptPlain, query)
	}
	msgs := []model.Message{{Role: model.RoleUser, Content: prompt}}

	response, err := llm.StreamAnswer(ctx, expandSystemPrompt, msgs, nil)
	if err != nil {
		return nil, fmt.Errorf("expand query: %w", err)
	}

	return parseExpansions(response), nil
}

// extractVocabHints returns excerpts from fragments to seed query expansion
// with domain-specific vocabulary. Uses larger snippets to give the LLM
// enough context to identify domain terms.
func extractVocabHints(fragments []model.SourceFragment, maxHints int) []string {
	var hints []string
	for _, f := range fragments {
		if len(hints) >= maxHints {
			break
		}
		content := f.RawContent
		if len(content) > 500 {
			content = content[:500]
		}
		hints = append(hints, fmt.Sprintf("[%s] %s", f.SourcePath, content))
	}
	return hints
}

// dedup removes duplicate fragments by ID, preserving order.
func dedup(fragments []model.SourceFragment) []model.SourceFragment {
	seen := make(map[string]bool)
	var result []model.SourceFragment
	for _, f := range fragments {
		if !seen[f.ID] {
			seen[f.ID] = true
			result = append(result, f)
		}
	}
	return result
}

// parseExpansions splits the LLM response into individual queries, capped at 5.
func parseExpansions(response string) []string {
	var expansions []string
	for _, line := range strings.Split(response, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		// Strip common numbering prefixes (e.g., "1. ", "- ", "* ")
		if len(line) > 2 {
			if line[0] >= '1' && line[0] <= '9' && (line[1] == '.' || line[1] == ')') {
				line = strings.TrimSpace(line[2:])
			} else if line[0] == '-' || line[0] == '*' {
				line = strings.TrimSpace(line[1:])
			}
		}
		if line != "" {
			expansions = append(expansions, line)
		}
	}
	if len(expansions) > 5 {
		expansions = expansions[:5]
	}
	return expansions
}
