---
description: How Knowledge Broker's trust layer, hybrid search, and confidence signals work.
---

# Architecture

## Design principles

Most knowledge tools give you an answer and hope it's right. KB tells you how much to trust the answer and why. When sources disagree, it flags the contradiction rather than silently picking one.

Embeddings and search run on your machine via Ollama and SQLite. The only external call is to Claude for synthesis, and that's optional.

Connectors, extractors, embedding models, and LLM providers are all swappable. Adding a new source type or file format doesn't touch core code.

## System overview

```
 INGESTION                                          QUERY
 ─────────                                          ─────

 ┌───────────┐  ┌──────────┐  ┌──────────┐
 │   Local   │  │   Git    │  │Confluence│  ...
 │Filesystem │  │  Repos   │  │  Slack   │
 └────┬──────┘  └────┬─────┘  └────┬─────┘
      │              │              │
      ▼              ▼              ▼
 ┌────────────────────────────────────────┐
 │            Connectors                  │         ┌─────────────────┐
 │  Pull content, detect changes (SHA-256)│         │   User Query    │
 └──────────────────┬─────────────────────┘         └────────┬────────┘
                    │                                        │
                    ▼                                        ▼
 ┌────────────────────────────────────────┐    ┌──────────────────────────┐
 │            Extractors                  │    │  Multi-Query Expansion   │
 │  Chunk at semantic boundaries per type │    │  LLM rephrases using     │
 │  (headings, functions, paragraphs)     │    │  corpus vocabulary       │
 └──────────────────┬─────────────────────┘    │  (optional, needs API)   │
                    │                          └────────────┬─────────────┘
                    ▼                                       │
 ┌────────────────────────────────────────┐                 ▼
 │         Enrichment (optional)          │    ┌──────────────────────────┐
 │  Local LLM annotates chunks with       │    │       Embedding          │
 │  entities and keywords                 │    │  Ollama embeds original  │
 └──────────────────┬─────────────────────┘    │  + expanded queries      │
                    │                          └────────────┬─────────────┘
                    ▼                                       │
 ┌────────────────────────────────────────┐                 ▼
 │            Embedding                   │    ┌──────────────────────────┐
 │  Ollama (nomic-embed-text, 768d)       │    │     Hybrid Search        │
 └──────────────────┬─────────────────────┘    │                          │
                    │                          │  ┌────────┐ ┌──────────┐ │
                    ▼                          │  │Vector  │ │  BM25    │ │
          ┌─────────────────┐                  │  │sqlite- │ │  FTS5    │ │
          │                 │                  │  │vec     │ │  keyword │ │
          │  ┌───────────┐  │                  │  └───┬────┘ └────┬─────┘ │
          │  │sqlite-vec │  │                  │      └─────┬─────┘       │
          │  │ (vectors) │  │◄─────────────────│            │             │
          │  └───────────┘  │  search          │      RRF Merge           │
          │  ┌───────────┐  │                  └────────────┬─────────────┘
          │  │   FTS5    │  │                               │
          │  │(keywords) │  │                               ▼
          │  └───────────┘  │                  ┌──────────────────────────┐
          │                 │                  │   Synthesis (Claude)     │
          │  SQLite (.db)   │                  │   or Raw Fragments       │
          │                 │                  └────────────┬─────────────┘
          └─────────────────┘                               │
                                                            ▼
                                               ┌──────────────────────────┐
                                               │        Response          │
                                               │  ┌────────────────────┐  │
                                               │  │ Answer + Sources   │  │
                                               │  ├────────────────────┤  │
                                               │  │ Confidence Signals │  │
                                               │  │ (fresh/corr/cons/  │  │
                                               │  │  auth → overall)   │  │
                                               │  ├────────────────────┤  │
                                               │  │ Contradictions     │  │
                                               │  └────────────────────┘  │
                                               └──────────────────────────┘
```

## Ingestion pipeline

```
Source → Connector → Extractor → Enrichment → Embedding → SQLite (sqlite-vec + FTS5)
```

### Connectors

Pluggable adapters that pull content from sources. Each source is registered with a type and name. The connector handles authentication, pagination, and change detection.

Ingestion is incremental: unchanged files (by SHA-256 checksum) are skipped. Documents that no longer exist at the source are removed from the database.

Supported connectors: local filesystem, Git (GitHub, GitLab, any Git host), Confluence Cloud, Slack, GitHub Wiki. See [Connectors](connectors.md) for setup details.

### Extractors

Files are chunked at semantic boundaries based on file type:

| File type | Strategy |
|-----------|----------|
| Markdown (`.md`) | Split on headings |
| Code (`.go`, `.py`, `.js`, `.ts`, `.jsx`, `.tsx`, `.java`, `.rs`, `.rb`) | Split on function/class boundaries |
| PDF (`.pdf`) | Text extraction |
| Jupyter (`.ipynb`) | Cell boundaries |
| Config (`.yaml`, `.yml`, `.toml`, `.json`, `.ini`, `.conf`, `.env`, `.properties`) | Logical sections |
| Everything else | Paragraph-based fallback |

Oversized chunks get a fixed-size fallback with configurable overlap (`KB_MAX_CHUNK_SIZE`, `KB_CHUNK_OVERLAP`).

### Enrichment (optional)

A small local LLM (`qwen2.5:0.5b` by default) runs over each chunk with a sliding window of neighboring chunks. It appends entity and keyword annotations that improve retrieval without modifying the original text.

Enrichment runs entirely on Ollama — no external API calls. Enable it by having the enrichment model pulled in Ollama.

### Embedding and storage

Each chunk is embedded via Ollama (`nomic-embed-text` by default, 768 dimensions). Vectors are stored in **sqlite-vec** for similarity search. The raw text is also indexed in an **FTS5** table for BM25 keyword search.

Everything lives in a single SQLite database file. No external database infrastructure.

## Query pipeline

```
Query → Expansion → Embedding → Hybrid Search (vector + BM25) → RRF Merge → Synthesis/Raw
```

### Multi-query expansion

When an API key is available, KB does a quick scout retrieval to extract domain vocabulary from the corpus, then asks the LLM to rephrase the query using those terms. This bridges vocabulary mismatch — when the user says "auth" but the docs say "authentication middleware."

Each expanded query variant is searched independently. Results are merged in the RRF step.

### Hybrid search

Every query runs through both **vector similarity** (semantic meaning) and **BM25 keyword search** (exact term matching). This catches both conceptual matches and precise terminology.

Results from all search paths are merged via **Reciprocal Rank Fusion** (RRF), which boosts fragments that appear in multiple result lists without requiring score normalization.

### Synthesis vs raw mode

**Synthesis mode** (default, requires API key) sends the top fragments to Claude with a system prompt that instructs it to:

- Synthesise a direct answer from the retrieved fragments
- Assess confidence signals across the full context
- Cite specific sources
- Flag contradictions between sources explicitly

**Raw mode** (no API key needed) returns fragments directly with per-fragment confidence scores computed locally. Useful for debugging retrieval, feeding a separate pipeline, or when no API key is configured.

## The trust layer

Every response includes a composite trust score built from four independent dimensions.

### Confidence signals

| Signal | Weight | What it measures |
|--------|--------|-----------------|
| **Freshness** | 0.20 | How recently were the sources modified, relative to the corpus age distribution |
| **Corroboration** | 0.25 | How many independent sources support the answer |
| **Consistency** | 0.30 | Do the sources agree, or are there contradictions |
| **Authority** | 0.25 | How authoritative are the source types for this kind of query |

The **overall** score is a weighted composite:

```
overall = freshness × 0.20 + corroboration × 0.25 + consistency × 0.30 + authority × 0.25
```

### How confidence is computed

In **raw mode**, confidence is computed per fragment using local heuristics:

- **Freshness** is scored relative to the corpus age distribution — a document modified last week scores higher than one modified last year, calibrated to how old the corpus is overall
- **Corroboration** reflects how many distinct sources contain similar information
- **Consistency** is based on embedding similarity between fragments about the same topic
- **Authority** weights source types based on query characteristics (e.g., code repos are more authoritative for implementation questions, Confluence for process questions)

In **synthesis mode**, the LLM assesses confidence across the full retrieved context, considering cross-fragment agreement, source diversity, and information completeness.

### Contradictions

When sources disagree, Knowledge Broker flags the contradiction explicitly in the response. The `contradictions` array contains natural-language descriptions of what the sources disagree about and which sources are involved.

Most knowledge tools silently pick one answer. KB surfaces the disagreement so agents can escalate to a human and humans can figure out which source is actually right.

### Using confidence signals

Agents can use the overall score to decide how to proceed:

| Score range | Suggested behavior |
|-------------|-------------------|
| 0.85+ | Answer confidently |
| 0.6–0.85 | Answer with caveats, note uncertainty |
| Below 0.6 | Surface the contradiction or uncertainty to the user |

These thresholds are suggestions — agents and applications can define their own logic based on the confidence breakdown.

## Configuration

| Variable | Default | Description |
|----------|---------|-------------|
| `KB_DB` | `kb.db` | SQLite database path |
| `KB_OLLAMA_URL` | `http://localhost:11434` | Ollama API URL |
| `KB_EMBEDDING_MODEL` | `nomic-embed-text` | Ollama embedding model |
| `KB_ENRICH_MODEL` | `qwen2.5:0.5b` | Ollama model for chunk enrichment |
| `KB_EMBEDDING_DIM` | `768` | Embedding vector dimension |
| `KB_LLM_PROVIDER` | `claude` | LLM provider (`claude`, `openai`, `ollama`) |
| `ANTHROPIC_API_KEY` | — | Anthropic API key (synthesis mode only) |
| `KB_CLAUDE_MODEL` | `claude-sonnet-4-20250514` | Claude model for synthesis |
| `KB_LISTEN_ADDR` | `:8080` | HTTP server listen address |
| `KB_MAX_CHUNK_SIZE` | `2000` | Max chunk size in characters |
| `KB_CHUNK_OVERLAP` | `150` | Chunk overlap in characters |
| `KB_WORKERS` | `4` | Parallel ingestion workers |
| `KB_DEFAULT_LIMIT` | `5` | Default fragment retrieval limit |
