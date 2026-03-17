---
title: Architecture — Trust Layer, Hybrid Search, and Confidence Signals
description: How Knowledge Broker's AI retrieval pipeline works — hybrid search (vector + BM25), confidence signals, contradiction detection, and the trust layer.
---

# Architecture

## Design principles

Most knowledge tools give you an answer and hope it's right. KB tells you how much to trust the answer and why. When sources disagree, it flags the contradiction rather than silently picking one.

Embeddings and search run entirely on your machine. The only external call is to an LLM for synthesis, and that's optional.

Connectors, extractors, embedding models, and LLM providers are all swappable. Adding a new source type or file format doesn't touch core code.

## System overview

```
 INGESTION                                          QUERY
 ─────────                                          ─────

 ┌───────────┐  ┌──────────┐  ┌──────────┐
 │   Local   │  │   Git    │  │Confluence│  ...
 │Filesystem │  │  Repos   │  │  Slack   │
 └────┬──────┘  └────┬─────┘  └─────┬────┘
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
 │  entities and keywords                 │    │  Embed original          │
 └──────────────────┬─────────────────────┘    │  + expanded queries      │
                    │                          └────────────┬─────────────┘
                    ▼                                       │
 ┌────────────────────────────────────────┐                 ▼
 │            Embedding                   │    ┌──────────────────────────┐
 │  Local model (nomic-embed-text, 768d)  │    │     Hybrid Search        │
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
          │                 │                  │   Synthesis (LLM)        │
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

Enrichment runs entirely locally, no external API calls. The enrichment model is pulled automatically on first run.

#### What enrichment produces

The enrichment LLM reads each chunk (plus its neighbors for context) and generates entity names, keywords, and domain terms that are relevant but may not appear verbatim in the text. These annotations are appended to the chunk before embedding. The original chunk text is preserved separately so the raw content is never modified.

#### Choosing a model

The enrichment model is configured via `KB_ENRICH_MODEL` or the `--enrich-model` flag.

| Model | Speed | Quality | Memory |
|-------|-------|---------|--------|
| `qwen2.5:0.5b` (default) | Fast | Basic keyword extraction | ~500 MB |
| `qwen2.5:3b` | Slower | Better entity recognition, more accurate keywords | ~2 GB |

Smaller models are fine for most corpora. Use a larger model if you notice retrieval missing results that should match on entity names or domain-specific terminology.

#### How enrichment affects retrieval

Enriched terms are embedded alongside the chunk text, so they influence both **vector similarity** and **BM25 keyword search**. This is especially useful for vocabulary mismatch: if a chunk discusses "k8s pod autoscaling" but the user searches for "Kubernetes horizontal scaling," enrichment can bridge the gap by adding both phrasings.

#### Prompt versions

The `--prompt-version` flag (or built-in default) controls how the enrichment LLM formats its output:

- **v1** — Full rewrite: the LLM produces a rewritten version of the chunk with annotations inline.
- **v2** — Append keywords: the LLM appends a keyword/entity block to the original text. Generally preferred for preserving the original content while adding searchable terms.

#### When to re-enrich

Enrichment metadata is stored with each fragment. You need to re-enrich when:

- **Changing the enrichment model** — different models produce different annotations.
- **Changing the prompt version** — the annotation format changes.

To re-enrich, either re-ingest with `--force` or use `--re-enrich` to update enrichment on existing fragments without re-scanning sources:

```bash
# Re-enrich all fragments with a new model
kb ingest --re-enrich --enrich-model qwen2.5:3b

# Re-enrich only a specific source
kb ingest --re-enrich --source ./my-docs
```

#### Skipping enrichment

Pass `--skip-enrichment` to `kb ingest` if you want faster ingestion and don't need the keyword boost. This is useful for quick iteration during development or when your corpus already uses consistent terminology that matches how users search.

#### Troubleshooting

- **Ollama OOM with larger models** — If Ollama crashes or returns errors during enrichment with `qwen2.5:3b` or larger, your machine may not have enough memory. Fall back to `qwen2.5:0.5b` or skip enrichment entirely.
- **Slow enrichment on large corpora** — Enrichment adds an LLM call per chunk. For large corpora (thousands of files), expect enrichment to take significantly longer than embedding alone. Use `--parallel` to speed up multi-source ingestion, or `--skip-enrichment` for the initial ingest and run `--re-enrich` later.
- **Enrichment model not found** — KB auto-pulls the enrichment model on first run via `kb setup`. If this fails (e.g., no internet), pull it manually: `ollama pull qwen2.5:0.5b`.

### Embedding and storage

Each chunk is embedded locally (`nomic-embed-text` by default, 768 dimensions). Vectors are stored in **sqlite-vec** for similarity search. The raw text is also indexed in an **FTS5** table for BM25 keyword search.

Everything lives in a single SQLite database file. No external database infrastructure.

## Query pipeline

```
Query → Expansion → Embedding → Hybrid Search (vector + BM25) → RRF Merge → Synthesis/Raw
```

### Multi-query expansion

When an API key is available, KB does a quick scout retrieval to extract domain vocabulary from the corpus, then asks the LLM to rephrase the query using those terms. This bridges vocabulary mismatch, like when the user says "auth" but the docs say "authentication middleware."

Each expanded query variant is searched independently. Results are merged in the RRF step.

### Hybrid search

Every query runs through both **vector similarity** (semantic meaning) and **BM25 keyword search** (exact term matching). This catches both conceptual matches and precise terminology.

Results from all search paths are merged via **Reciprocal Rank Fusion** (RRF), which boosts fragments that appear in multiple result lists without requiring score normalization.

### Synthesis vs raw mode

**Synthesis mode** (default, requires an LLM provider) sends the top fragments to the configured LLM with a system prompt that instructs it to:

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

- **Freshness** is scored relative to the corpus age distribution, so a document modified last week scores higher than one modified last year, calibrated to how old the corpus is overall
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

These thresholds are suggestions. Agents and applications can define their own logic based on the confidence breakdown.

## Configuration

KB loads settings from multiple sources (later overrides earlier):

1. **Defaults** — sensible built-in values
2. **`~/.config/kb/config`** — persistent user config (respects `$XDG_CONFIG_HOME`)
3. **`.env` in working directory** — project-local overrides
4. **`--config <path>`** — explicit file (useful for server deployments)
5. **Environment variables** — always highest precedence

All config files use `KEY=VALUE` format. Run `kb config` to see the resolved values and where each one comes from. See the [CLI Reference](cli.md#configuration) for the full variable list.
