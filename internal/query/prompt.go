package query

import (
	"fmt"
	"strings"
	"time"

	"github.com/knowledge-broker/knowledge-broker/internal/model"
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
- If sources contradict, state both claims briefly.

Emit metadata after your answer:

---KB_META---
{"confidence":{"freshness":0.0,"corroboration":0.0,"consistency":0.0,"authority":0.0},"sources":[{"fragment_id":"...","source_uri":"...","source_path":"..."}],"contradictions":[]}
---KB_META_END---

Confidence: freshness=recency relative to corpus, corroboration=number of independent sources (1=0.3,2-3=0.6,4+=0.9), consistency=agreement between sources, authority=source type fitness (code>docs>config>commits).
Only include fragments you used in sources. No text after ---KB_META_END---. No code fences around the metadata block.

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

- **Freshness**: How recently were the sources modified? Score relative to the range of dates you see. Recent sources score higher.
- **Corroboration**: How many independent sources support the answer? 1 source = low (0.2-0.4), 2-3 sources = medium (0.5-0.7), 4+ sources = high (0.8-1.0).
- **Consistency**: Do the sources agree? If they contradict each other, score lower and flag the contradiction.
- **Authority**: How authoritative are the source types for this kind of question? Code is authoritative for behaviour. Docs are authoritative for intent/design. Config files are authoritative for settings. Commit messages are low authority.

## Response format

First, write your answer in natural language. Be direct and concise. Cite sources inline like [abc123].

Then, on a new line, emit a metadata block in exactly this format:

---KB_META---
{"confidence":{"freshness":0.0,"corroboration":0.0,"consistency":0.0,"authority":0.0},"sources":[{"fragment_id":"...","source_uri":"...","source_path":"..."}],"contradictions":[]}
---KB_META_END---

The sources array should include only the fragments you actually used in your answer.
If there are contradictions, include them with claim, sources, and explanation fields.
Do NOT include any text after the ---KB_META_END--- marker.
Do NOT wrap the metadata block in code fences or backticks.

`)
	}

	b.WriteString("## Source fragments\n\n")

	now := time.Now()
	for _, f := range fragments {
		age := now.Sub(f.LastModified)
		ageStr := formatAge(age)

		fmt.Fprintf(&b, "### Fragment: %s\n", f.ID)
		fmt.Fprintf(&b, "- Source: %s\n", f.SourcePath)
		fmt.Fprintf(&b, "- URI: %s\n", f.SourceURI)
		fmt.Fprintf(&b, "- Type: %s\n", f.FileType)
		fmt.Fprintf(&b, "- Last modified: %s (%s ago)\n", f.LastModified.Format("2006-01-02"), ageStr)
		if f.Author != "" {
			fmt.Fprintf(&b, "- Author: %s\n", f.Author)
		}
		if f.ConfidenceAdj != 0 {
			fmt.Fprintf(&b, "- Confidence adjustment: %.2f (from user feedback)\n", f.ConfidenceAdj)
		}
		fmt.Fprintf(&b, "\n%s\n\n---\n\n", f.Content)
	}

	return b.String()
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
