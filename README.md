# Knowledge Broker

[![Go](https://img.shields.io/badge/Go-1.24+-00ADD8?logo=go&logoColor=white)](https://go.dev)
[![License](https://img.shields.io/badge/License-BSL_1.1-blue)](LICENSE)

> **Pre-release** — under active development. APIs and defaults may change.

Your team's knowledge is scattered across repos, wikis, Confluence, and Slack. The answer to any question exists — spread across three sources that partially contradict each other. Traditional search finds documents. Knowledge Broker finds answers, tells you how much to trust them, and shows you where sources disagree.

Deploy a single instance for your org. Ingest your Confluence spaces, Slack channels, Git repos, and wikis into one knowledge base. Developers and AI agents query it via MCP or HTTP — no one needs to set up their own ingestion or manage their own database.

**Open-source RAG with a trust layer.** Hybrid search (BM25 + semantic vectors), structured confidence signals, contradiction detection, and an MCP server for AI agent integration. Built in Go with SQLite. Self-hosted. No data leaves your environment.

**[Documentation](https://knowledgebroker.dev)** | **[Getting Started](https://knowledgebroker.dev/quickstart/)** | **[Architecture](https://knowledgebroker.dev/architecture/)**

```jsonc
$ kb query "What database does the inventory service use and what port does it run on?"
{
  "answer": "The inventory service runs on port 8081 and uses PostgreSQL ...",
  "confidence": {
    "overall": 0.93,          // agents can branch on this
    "breakdown": {
      "freshness": 0.94,      // how recent are the sources
      "corroboration": 0.85,  // how many independent sources agree
      "consistency": 1.00,    // do sources contradict each other
      "authority": 0.95       // how authoritative are the source types
    }
  },
  "sources": [
    { "source_type": "confluence", "source_name": "ACME", "source_path": "Internal Services & Infrastructure" },
    { "source_type": "slack", "source_name": "acme-haf5895", "source_path": "#platform-engineering/2026-03-06" }
  ],
  "contradictions": []        // surfaced explicitly, never hidden
}
```

## Quick start

**Prerequisites:** Go 1.24+, [Ollama](https://ollama.com) running locally

```bash
make install
ollama pull nomic-embed-text      # embedding model
ollama pull qwen2.5:0.5b           # enrichment model (optional)
```

### Set up your org's knowledge base

Ingest your team's sources into a single instance. Run this on the machine that will host the knowledge base:

```bash
kb ingest --source ./my-repo --description "Backend API"
kb ingest --git https://github.com/acme/platform --description "Platform services"
kb ingest --confluence ENGINEERING --description "Engineering wiki"
kb ingest --slack C0ABC123DEF --description "Platform engineering channel"
```

Start the server so others can query it:

```bash
kb serve                  # HTTP API on :8080
kb mcp                    # MCP server (stdio + SSE on :8082)
```

### Query

```bash
# Synthesised answer (requires ANTHROPIC_API_KEY in .env)
kb query "how does retry logic work?"

# Raw retrieval — ranked fragments, no API key needed
kb query --raw "how does retry logic work?"
```

## Agent integration

Point your team's MCP clients at the shared instance. Each developer adds this to their MCP config:

```json
{
  "mcpServers": {
    "knowledge-broker": {
      "command": "kb",
      "args": ["mcp"]
    }
  }
}
```

Agents receive structured JSON with confidence scores, source attribution, and contradictions — so they can reason about reliability rather than treating all retrieved knowledge as equally trustworthy. A `kb-instructions` prompt teaches agents when and how to query, including a dynamic list of available sources. See [MCP Server](https://knowledgebroker.dev/mcp/) for the full setup guide.

An **HTTP API** is also available via `kb serve` for non-MCP integrations.

## How it works

1. **Connectors** pull content from sources (local filesystem, Git, Confluence, Slack, GitHub Wiki — see [Connectors](https://knowledgebroker.dev/connectors/))
2. **Extractors** chunk files at semantic boundaries (headings for markdown, functions for code)
3. **Enrichment** (optional) annotates chunks with entities and keywords using a local LLM
4. **Embeddings** (via Ollama) convert chunks to vectors; raw text is indexed with FTS5 for keyword search
5. **Query expansion** (optional) generates alternative phrasings grounded in corpus vocabulary
6. **Hybrid search** runs both vector similarity and BM25 keyword search, merged via Reciprocal Rank Fusion
7. **Synthesis** (optional) produces an answer via Claude, or returns ranked fragments directly in raw mode
8. **Confidence signals** assess how much to trust each result across four dimensions

## Confidence signals

Every result includes four trust dimensions (0.0–1.0) combined into a weighted **overall** score: freshness (0.20), corroboration (0.25), consistency (0.30), authority (0.25). In raw mode, these are computed per fragment using local heuristics. In synthesis mode, the LLM assesses them across the full context. Contradictions between sources are always flagged explicitly.

Agents can branch on the overall score — answering confidently above 0.85, hedging between 0.6–0.85, or escalating to the user below 0.6. See [Architecture](https://knowledgebroker.dev/architecture/) for full details on how each signal is computed.

## Commands

| Command | Description |
|---------|-------------|
| `kb ingest --source ./dir` | Ingest a local directory |
| `kb ingest --git <url>` | Ingest a Git repo by URL |
| `kb ingest --confluence SPACE` | Ingest a Confluence space |
| `kb ingest --slack <channel-id>` | Ingest a Slack channel |
| `kb ingest --wiki <repo-url>` | Ingest a GitHub Wiki |
| `kb ingest --all` | Re-ingest all registered sources |
| `kb query "question"` | Synthesised answer (requires API key) |
| `kb query --raw "question"` | Raw fragments (no API key needed) |
| `kb query --human "question"` | Streamed human-readable answer |
| `kb serve` | Start HTTP API on `:8080` |
| `kb mcp` | Start MCP server (stdio + SSE on `:8082`) |
| `kb sources list` | List registered sources |
| `kb cluster` | K-means clustering on fragments |
| `kb eval` | Run retrieval quality evaluation |
| `kb export --out ./dir/` | Export embeddings for TensorBoard |

Ingestion is incremental — unchanged files are skipped via checksums. All connector flags can be combined in a single command. Use `--description` to annotate sources for agents.

See the [CLI Reference](https://knowledgebroker.dev/cli/) for full flags and options, [Connectors](https://knowledgebroker.dev/connectors/) for source setup, and [MCP Server](https://knowledgebroker.dev/mcp/) for agent integration.

## Configuration

Copy `.env.example` to `.env` and fill in your values. Environment variables also work and take precedence over `.env`.

| Variable | Default | Description |
|----------|---------|-------------|
| `KB_DB` | `kb.db` | SQLite database path |
| `KB_OLLAMA_URL` | `http://localhost:11434` | Ollama API URL |
| `KB_EMBEDDING_MODEL` | `nomic-embed-text` | Ollama embedding model |
| `KB_ENRICH_MODEL` | `qwen2.5:0.5b` | Ollama model for chunk enrichment |
| `KB_EMBEDDING_DIM` | `768` | Embedding vector dimension |
| `ANTHROPIC_API_KEY` | — | Anthropic API key (only needed for synthesis mode) |
| `KB_CLAUDE_MODEL` | `claude-sonnet-4-20250514` | Claude model for synthesis |
| `KB_LISTEN_ADDR` | `:8080` | HTTP server listen address |
| `KB_MAX_CHUNK_SIZE` | `2000` | Max chunk size in characters |
| `KB_CHUNK_OVERLAP` | `150` | Chunk overlap in characters |
| `KB_WORKERS` | `4` | Parallel ingestion workers |
| `KB_LLM_PROVIDER` | `claude` | LLM provider (`claude`, `openai`, `ollama`) |
| `KB_DEFAULT_LIMIT` | `5` | Default fragment retrieval limit |

Connector-specific variables (Git, Confluence, Slack, etc.) are documented in [Connectors](https://knowledgebroker.dev/connectors/).

## Architecture

```
Ingest:  Source → Connector → Extractor → Enrichment → Embedding → SQLite + sqlite-vec + FTS5
Query:   User query → Query expansion → Embedding → Hybrid search (vector + BM25) → RRF merge → Synthesis
```

- **Connectors** pull content from sources (filesystem, Git, Confluence, Slack, GitHub Wiki). Incremental via checksums.
- **Extractors** chunk at semantic boundaries per file type (headings for markdown, functions for code).
- **Hybrid search** runs vector similarity and BM25 keyword search, merged via Reciprocal Rank Fusion.
- **Synthesis** sends top fragments to Claude for answer generation, or returns raw fragments directly.

See [Architecture](https://knowledgebroker.dev/architecture/) for the full design, including the trust layer, query expansion, and enrichment pipeline.

## What requires an API key

KB works with only Ollama (local, free). An API key adds synthesis but is never required for core retrieval.

| Without API key | With `ANTHROPIC_API_KEY` |
|----------------|:------------------------:|
| Ingestion, embedding, hybrid search, raw retrieval, per-fragment confidence, clustering, enrichment | All of the above, plus: **multi-query expansion**, **answer synthesis**, **cross-fragment confidence**, **contradiction detection** |

See the [full documentation](https://knowledgebroker.dev) for connector setup, architecture details, and the evaluation framework.

## License

[BSL 1.1](LICENSE) — free to use and self-host. Converts to Apache 2.0 after 4 years.