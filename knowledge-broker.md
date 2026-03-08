# Knowledge Broker

A knowledge engine that ingests documents from multiple sources, embeds them for semantic retrieval, and answers questions with confidence signals. Connect it to your repos, docs, and knowledge bases — then ask it questions.

## What it does

Knowledge Broker reads your documents, understands how they relate to each other, and answers questions by retrieving and synthesising relevant information. It tracks how fresh and trustworthy its answers are — and tells you when it's uncertain.

When sources agree, confidence is high. When they contradict each other, the contradiction is surfaced rather than hidden.

## Core concepts

### Source fragments

A source fragment is a chunk of content extracted from a single source. It carries metadata about where it came from:

- Source connector and path
- Last modified timestamp (from filesystem, git, or API)
- Author (from git blame or source metadata where available)
- Source URI (for linking back to the original)
- Checksum (for incremental re-ingestion)

Source fragments are the raw input. They're ingested, embedded, and stored for retrieval.

### Connectors

Connectors are pluggable adapters that pull content from different sources. The core engine doesn't care where content comes from — connectors normalise everything into source fragments.

V1 connectors:
- **Local filesystem** — reads files from disk, extracts git metadata where available
- **GitHub** — pulls repo file content and git metadata via API

The local filesystem connector is not special — it's just another connector, same interface as GitHub or any future connector (Confluence, Drive, Slack, etc.).

### Confidence signals

Rather than a single confidence score, Knowledge Broker tracks four independent signals, assessed by the LLM at query time:

**Freshness** — how recently were the retrieved sources modified? Scored relative to the age distribution of the corpus.

**Corroboration** — how many independent sources support this answer? A claim backed by documentation, code, and a commit message is more trustworthy than one backed by a single file.

**Consistency** — do the retrieved sources agree with each other? If two sources make contradictory claims about the same concept, consistency drops and the contradiction is flagged.

**Authority** — how authoritative is each source type for this kind of claim? Code is authoritative for system behaviour. Documentation is authoritative for intent and design rationale. Commit messages are low authority for almost everything. Sensible defaults are provided per file type, and can be overridden in configuration.

## Architecture

### V1 — simplified pipeline

```
Connectors (local filesystem, GitHub)
  → extract text + metadata
  → chunk (semantic boundaries per file type, fixed-size fallback)
  → embed via Ollama (local)
  → store in SQLite (content + metadata) + sqlite-vec (vectors)

Query
  → embed query via Ollama
  → vector search for nearest fragments (sqlite-vec)
  → check in-memory cache (exact query + fragment checksums)
  → on miss: pass fragments + query to Claude (Anthropic API)
  → Claude synthesises answer + assesses confidence signals on the fly
  → cache result, stream response back via CLI / HTTP API / MCP
```

No clustering, no knowledge units, no manifest in v1. The LLM handles synthesis, contradiction detection, and confidence assessment at query time from raw fragments.

### Boundaries

**Connector → Storage.** Each connector produces source fragments in a standard format. The storage layer doesn't know or care what kind of source the fragment came from.

```
SourceFragment {
  id:            string
  content:       string
  source_type:   string      // "filesystem", "github"
  source_path:   string      // file path within the source
  source_uri:    string      // linkable URI back to original
  last_modified: timestamp
  author:        string      // optional
  file_type:     string
  checksum:      string
  embedding:     []float32
}
```

**Storage → Query engine.** The query engine interacts with storage through:

```
searchFragments(query_embedding, limit) → []SourceFragment
getFragments(ids: []string)             → []SourceFragment
reportFeedback(fragment_id, type, content?, evidence?) → void
```

**Query engine → Consumer.** The external interface is a stateless query operation. The caller can pass conversation history for multi-turn context (same pattern as the Claude API messages array).

```
query(messages: []Message, options?) → streamed Answer {
  content:         string    // streamed
  confidence: {
    freshness:     float
    corroboration: float
    consistency:   float
    authority:     float
  }
  sources:         []SourceRef
  contradictions:  []Contradiction  // optional
}
```

Consumers don't know about embeddings, storage, or connectors. They send messages and get a streamed answer with metadata about how much to trust it.

### Extractors

Each file type has a pluggable extractor that handles chunking:

- **Markdown** — splits at headings (##, ###)
- **Code** (Go, Python, JS, etc.) — splits at function/type/class boundaries
- **Plain text** — fixed-size chunks with overlap

Extractors are responsible for turning a raw file into one or more content chunks. Adding support for a new file type (e.g., PDF) means adding a new extractor — no core changes needed.

## Feedback

Knowledge Broker accepts feedback to improve its knowledge over time:

**Corrections** — "that's wrong, it's actually X." The correction is stored as a new high-authority source fragment and the contradicted sources are confidence-degraded.

**Challenges** — "I don't think that's right." Degrades confidence on related fragments. Repeated challenges surface the content for attention.

**Confirmations** — "that's correct." Boosts confidence on the retrieved fragments.

Feedback takes effect immediately without requiring review. If a correction is itself wrong, subsequent feedback corrects it. The system is self-correcting through use.

Feedback is anonymous in v1.

```
report(fragment_id, type, content?, evidence?) → void

type: correction | challenge | confirmation
```

## Decisions

Resolved decisions for v1:

| Area | Decision |
|------|----------|
| Language | Go — single binary, fast, minimal dependencies |
| Embeddings | Local via Ollama — zero API cost, semantic search |
| LLM | Claude via Anthropic API — synthesis and confidence assessment |
| Storage | SQLite + sqlite-vec — single file, zero-dependency vector search |
| History | Current state + changelog per fragment |
| Ingestion | Pluggable connectors — local filesystem + GitHub for v1 |
| File types | Plaintext + code for v1 (new types = new extractor, no core changes) |
| Chunking | Semantic boundaries per file type, fixed-size fallback |
| Clustering | Deferred to v2 — query-time synthesis is sufficient for v1 |
| Knowledge units | Deferred to v2 — fragments are the primary data model for v1 |
| Manifest | Deferred to v2 — vector search replaces manifest-based retrieval |
| Freshness | Relative to corpus age distribution |
| Contradictions | Detected at query time by the LLM |
| Confidence | Four independent signals, assessed by LLM at query time |
| Query model | Stateless — caller passes conversation history if needed |
| Streaming | Yes, from the start |
| Latency | 2-3s acceptable for v1, optimize later |
| Scoping | Full corpus for v1 |
| Feedback | Anonymous for v1 |
| Interface | CLI + HTTP API + MCP server |
| Target scale | Large — multi-repo, org-wide |
| Re-ingestion | Incremental via checksums |
| Language support | English only for v1 |
| Caching | In-memory exact-match, invalidated by fragment checksums (active in serve/mcp modes) |
| License | BSL 1.1 → Apache 2.0 |

## Roadmap

### V2 and beyond

- Clustering engine — group related fragments into knowledge units
- Knowledge units with pre-computed confidence signals
- Manifest for LLM-driven retrieval decisions
- Additional connectors (Confluence, Google Drive, Slack)
- Query scoping (interface ready, implementation pending)
- Incremental ingestion watch mode
- Pluggable LLM providers
- Configurable clustering thresholds
- Multi-language support

## License

BSL 1.1 with conversion to Apache 2.0 after 4 years. Free to use and self-host. Cannot be offered as a competing hosted service.