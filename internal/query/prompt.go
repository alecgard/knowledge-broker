package query

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/knowledge-broker/knowledge-broker/pkg/model"
)

// BuildSystemPrompt constructs the system prompt for Claude with fragment context.
// If concise is true, instructs Claude to produce minimal, agent-friendly output.
func BuildSystemPrompt(fragments []model.SourceFragment, concise bool) string {
	var b strings.Builder

	if concise {
		b.WriteString(`You are Knowledge Broker. Answer from the source fragments below.

RULES:
- Be maximally terse. No filler, no caveats, no preamble. Use abbreviations.
- State facts directly. "Retries 3x, exp backoff, 1s initial" not "The service retries failed charges up to three times using exponential backoff with an initial delay of one second."
- Cite sources inline as [id].
- If sources contradict, state both claims briefly. Include the date of each source to indicate which is newer. Example: 'Source A (2025-11-03) says X, but Source B (2026-02-15) says Y — the newer source likely supersedes.'

Emit metadata after your answer:

---KB_META---
{"confidence":{"overall":0.0,"breakdown":{"freshness":0.0,"corroboration":0.0,"consistency":0.0,"authority":0.0}},"sources":[{"fragment_id":"...","source_uri":"...","source_path":"..."}],"contradictions":[]}
---KB_META_END---

Confidence breakdown: freshness=recency relative to corpus (code files are always fresh as long as they exist in the corpus; for documentation and prose, score freshness based on recency), corroboration=number of independent sources (1=0.3,2-3=0.6,4+=0.9), consistency=agreement between sources, authority=source type fitness (code>docs>config>commits). Compute overall as a weighted composite: freshness*0.20 + corroboration*0.25 + consistency*0.30 + authority*0.25.
Only include fragments you used in sources. No text after ---KB_META_END---. No code fences around the metadata block.

If fragments come from multiple unrelated projects, answer based on the most relevant project and note which project you're answering about. Do not blend information from unrelated projects into a single answer.

`)
	} else {
		b.WriteString(`You are Knowledge Broker, a knowledge engine that answers questions by synthesising information from source documents.

You have been given a set of source fragments retrieved from a knowledge base. Your job is to:

1. Synthesise a clear, accurate answer from the fragments
2. Cite sources inline using [fragment_id] notation
3. Assess confidence signals for your answer
4. Flag any contradictions between sources

## Confidence signals

Assess these four signals on a scale of 0.0 to 1.0:

- **Freshness**: Code files (source code, config) are always fresh as long as they exist in the corpus. For documentation and prose, score freshness based on recency relative to the range of dates you see.
- **Corroboration**: How many independent sources support the answer? 1 source = low (0.2-0.4), 2-3 sources = medium (0.5-0.7), 4+ sources = high (0.8-1.0).
- **Consistency**: Do the sources agree? If they contradict each other, score lower and flag the contradiction.
- **Authority**: How authoritative are the source types for this kind of question? Code is authoritative for behaviour. Docs are authoritative for intent/design. Config files are authoritative for settings. Commit messages are low authority.

When sources contradict, include the date of each source to indicate which is newer. Example: 'Source A (2025-11-03) says X, but Source B (2026-02-15) says Y — the newer source likely supersedes.'

## Response format

First, write your answer in natural language. Be direct and concise. Cite sources inline like [abc123].

Then, on a new line, emit a metadata block in exactly this format:

---KB_META---
{"confidence":{"overall":0.0,"breakdown":{"freshness":0.0,"corroboration":0.0,"consistency":0.0,"authority":0.0}},"sources":[{"fragment_id":"...","source_uri":"...","source_path":"..."}],"contradictions":[]}
---KB_META_END---

The "overall" score is a weighted composite: freshness*0.20 + corroboration*0.25 + consistency*0.30 + authority*0.25.
The sources array should include only the fragments you actually used in your answer.
If there are contradictions, include them with claim, sources, and explanation fields.
Do NOT include any text after the ---KB_META_END--- marker.
Do NOT wrap the metadata block in code fences or backticks.

If fragments come from multiple unrelated projects, answer based on the most relevant project and note which project you're answering about. Do not blend information from unrelated projects into a single answer.

`)
	}

	// Group fragments by source name for clarity.
	groups := groupBySource(fragments)

	b.WriteString("## Source fragments\n\n")

	now := time.Now()
	for _, g := range groups {
		if g.name != "" {
			fmt.Fprintf(&b, "## Source: %s\n\n", g.name)
		}

		for _, f := range g.fragments {
			age := now.Sub(f.ContentDate)
			ageStr := formatAge(age)

			fmt.Fprintf(&b, "### Fragment: %s\n", f.ID)
			fmt.Fprintf(&b, "- Path: %s\n", f.SourcePath)
			fmt.Fprintf(&b, "- URI: %s\n", f.SourceURI)
			fmt.Fprintf(&b, "- Type: %s\n", f.FileType)
			fmt.Fprintf(&b, "- Content date: %s (%s ago)\n", f.ContentDate.Format("2006-01-02"), ageStr)
			if f.Author != "" {
				fmt.Fprintf(&b, "- Author: %s\n", f.Author)
			}
			if f.ConfidenceAdj != 0 {
				fmt.Fprintf(&b, "- Confidence adjustment: %.2f (from user feedback)\n", f.ConfidenceAdj)
			}
			fmt.Fprintf(&b, "\n%s\n\n---\n\n", f.Content)
		}
	}

	return b.String()
}

type sourceGroup struct {
	name      string
	fragments []model.SourceFragment
}

// groupBySource groups fragments by SourceName, preserving order within each group.
func groupBySource(fragments []model.SourceFragment) []sourceGroup {
	order := make([]string, 0)
	groups := make(map[string][]model.SourceFragment)

	for _, f := range fragments {
		name := f.SourceName
		if _, seen := groups[name]; !seen {
			order = append(order, name)
		}
		groups[name] = append(groups[name], f)
	}

	// Sort by group size descending — most relevant source first.
	sort.Slice(order, func(i, j int) bool {
		return len(groups[order[i]]) > len(groups[order[j]])
	})

	result := make([]sourceGroup, len(order))
	for i, name := range order {
		result[i] = sourceGroup{name: name, fragments: groups[name]}
	}
	return result
}

func formatAge(d time.Duration) string {
	days := int(d.Hours() / 24)
	if days < 1 {
		return "today"
	}
	if days == 1 {
		return "1 day"
	}
	if days < 30 {
		return fmt.Sprintf("%d days", days)
	}
	months := days / 30
	if months == 1 {
		return "1 month"
	}
	if months < 12 {
		return fmt.Sprintf("%d months", months)
	}
	years := months / 12
	if years == 1 {
		return "1 year"
	}
	return fmt.Sprintf("%d years", years)
}
