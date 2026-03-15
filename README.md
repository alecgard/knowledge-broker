# Knowledge Broker

[![Go](https://img.shields.io/badge/Go-1.24+-00ADD8?logo=go&logoColor=white)](https://go.dev)
[![License](https://img.shields.io/badge/License-BSL_1.1-blue)](LICENSE)

> **Pre-release** — under active development. APIs and defaults may change.

Knowledge Broker is an open-source CLI tool for team knowledge retrieval, built in Go with SQLite. It provides hybrid search (BM25 + vector) over ingested documents, an MCP server for integration with AI coding tools like Claude Code, and a trust layer that surfaces confidence signals — freshness, corroboration, consistency, and authority — rather than hiding uncertainty.

Zero infrastructure. Self-hosted. No data leaves your environment.

**[Documentation](https://knowledgebroker.dev)** | **[Getting Started](https://knowledgebroker.dev/quickstart/)** | **[Architecture](https://knowledgebroker.dev/architecture/)**

```jsonc
$ kb query "What database does the inventory service use and what port does it run on?"
{
  "answer": "The inventory service runs on port 8081 and uses PostgreSQL as its primary database, along with Kafka and Redis. The PostgreSQL instance is version 16 on RDS with Multi-AZ deployment (r6g.2xlarge). Each service has a separate database instance.",
  "confidence": {
    "overall": 0.93,          // agents can branch on this — e.g. < 0.7 triggers clarification
    "breakdown": {
      "freshness": 0.94,
      "corroboration": 0.85,
      "consistency": 1.00,
      "authority": 0.95
    }
  },
  "sources": [
    { "source_type": "confluence", "source_name": "ACME", "source_path": "Internal Services & Infrastructure" },
    { "source_type": "slack", "source_name": "acme-haf5895", "source_path": "#platform-engineering/2026-03-06" }
  ],
  "contradictions": []        // surfaced explicitly rather than silently resolved
}
```

The answer is synthesised from Confluence docs and Slack history. Contradictions between sources are flagged rather than hidden — agents can decide what to do with ambiguity rather than receiving false certainty.

## Agent integration

Knowledge Broker exposes an **MCP server** for direct integration with any MCP-compatible agent runtime:

```bash
kb mcp                  # stdio + SSE on :8082
kb mcp --addr :9090     # custom port
```

The SSE endpoint is at `http://<addr>/sse`. Exposed tools: `query`, `list-sources`.

Agents receive the same structured JSON response shown above — a synthesised answer with confidence scores, source attribution, and any contradictions — so they can reason about reliability rather than treating all retrieved knowledge as equally trustworthy.

Raw mode is also available for cases where you want fragments without synthesis — useful for debugging retrieval, feeding a separate pipeline, or when no API key is configured. Pass `raw=true` to the `query` tool, or use `--raw` on the CLI.

See [MCP Server](https://knowledgebroker.dev/mcp/) for full setup and tool reference.

KB also exposes a `kb-instructions` prompt that teaches agents when and how to query it — including a dynamically generated list of available sources with descriptions. MCP clients that support prompts will pick this up automatically.

Knowledge Broker can also expose an **HTTP server** for HTTP API access - expose via `kb serve`. 

## Quick start

**Prerequisites:** Go 1.24+, [Ollama](https://ollama.com) running locally

```bash
make install
ollama pull nomic-embed-text      # embedding model
ollama pull qwen2.5:0.5b           # enrichment model (optional)

# Ingest a local directory
kb ingest --source ./my-repo

# Ingest a Git repo by URL
kb ingest --git https://github.com/owner/repo

# Synthesised answer (requires ANTHROPIC_API_KEY in .env)
kb query "how does retry logic work?"

# Raw retrieval — ranked fragments, no API key needed
kb query --raw "how does retry logic work?"

# or expose via HTTP
kb serve

curl -s -X POST localhost:8080/v1/query \
  -H 'Content-Type: application/json' \
  -d '{"messages":[{"role":"user","content":"how does retry logic work?"}]}'

curl -s -X POST localhost:8080/v1/query \
  -H 'Content-Type: application/json' \
  -d '{"messages":[{"role":"user","content":"how does retry logic work?"}],"mode":"raw"}'
```

### Running without an API key

Raw mode (`--raw`) runs the full retrieval pipeline — embedding, vector search, confidence scoring — using only Ollama. No Anthropic API key is needed.

```bash
kb query --raw "how does auth work?"

curl -X POST localhost:8080/v1/query \
  -d '{"messages":[{"role":"user","content":"how does auth work?"}],"mode":"raw"}'
```

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

Every result includes a composite **overall** trust score and four independent confidence dimensions (0.0–1.0):

| Signal | Weight | What it measures |
|--------|--------|-----------------|
| **Freshness** | 0.20 | How recently were the sources modified, relative to the corpus |
| **Corroboration** | 0.25 | How many independent sources support the answer |
| **Consistency** | 0.30 | Do the sources agree, or are there contradictions |
| **Authority** | 0.25 | How authoritative are the source types for this kind of query |

The **overall** score is a weighted composite: `freshness*0.20 + corroboration*0.25 + consistency*0.30 + authority*0.25`.

In raw mode, these are computed per fragment using local heuristics. In synthesis mode, the LLM assesses them across the full context. Contradictions between sources are flagged rather than resolved silently.

Agents can use the overall score to decide how to proceed — for example, answering confidently above 0.85, hedging between 0.6–0.85, or surfacing the contradiction to the user below 0.6.

## Commands

### `kb ingest`

Ingest documents from a source into the knowledge base.

```bash
kb ingest --source ./path/to/dir                                          # local directory
kb ingest --git https://github.com/owner/repo                             # Git repo by URL
kb ingest --source ./repo --description "Payment processing microservice" # with description for agents
kb ingest --source ./repo-a --source ./repo-b                             # multiple sources
kb ingest --all                                                           # re-ingest all registered local sources
```

Connectors are also available for Confluence, Slack, and GitHub Wiki. See [Connectors](https://knowledgebroker.dev/connectors/) for setup instructions.

Ingestion is incremental — unchanged files are skipped based on checksums.

Use `--description` to give agents context about what a source contains. Descriptions appear in `list-sources` results and in the `kb-instructions` prompt. When omitted, a label is derived from the source type and name (e.g. "Git repository: owner/repo").

#### Remote ingestion

Send content to a KB server for embedding and storage:

```bash
kb ingest --source ./my-repo --remote http://server:8080
```

The client extracts and chunks locally, then POSTs fragments to the server which handles embedding and storage. This is how the Docker quickstart works — the CLI runs on the host while Ollama runs in a container.

### `kb query`

Query the knowledge base and get an answer with confidence signals.

```bash
# Raw retrieval — returns ranked fragments, no LLM needed
kb query --raw "how does auth work?"
kb query --raw "explain the deployment process"
kb query --raw --limit 10 --topics "billing,payments" "retry policy"

# Synthesised answer (requires ANTHROPIC_API_KEY)
kb query "what is the billing retry policy?"
kb query --human "how does auth work?"    # streamed, human-readable
kb query --raw --source-type git "deployment process"
```

Raw mode (`--raw`) returns full fragments as JSON with per-fragment confidence signals and source metadata. Useful for debugging retrieval, feeding a separate pipeline, or when no API key is available.

### `kb serve`

Start an HTTP API server.

```bash
kb serve --addr :8080
```

Endpoints:
- `POST /v1/query` — query with optional SSE streaming (`{"messages": [{"role": "user", "content": "..."}]}`)
- `POST /v1/query` with `"mode": "raw"` — raw retrieval, returns ranked fragments without LLM synthesis
- `POST /v1/ingest` — receive fragments from remote ingestion (`kb ingest --remote`)
- `GET /v1/health` — health check (verifies Ollama connectivity, returns 503 if unreachable)

### `kb mcp`

Start an MCP server. Both stdio and HTTP/SSE transports run simultaneously, sharing the same server instance.

```bash
kb mcp                  # stdio + SSE on :8082
kb mcp --addr :9090     # stdio + SSE on custom port
```

| Flag | Default | Description |
|------|---------|-------------|
| `--addr` | `:8082` | SSE listen address |
| `--db` | `kb.db` | Path to SQLite database |

The SSE endpoint is available at `http://<addr>/sse` and accepts messages at `http://<addr>/message`.

Exposes tools: `query`, `list-sources`. Synthesis is the default; pass `raw=true` for retrieval without LLM synthesis. See [MCP Server](https://knowledgebroker.dev/mcp/) for setup and tool reference.

### `kb sources list`

List all registered ingestion sources.

```bash
kb sources list
```

Returns JSON with source type, name, config, and last ingest time for each registered source.

### `kb export`

Export fragment embeddings for visualization with TensorBoard Embedding Projector.

```bash
kb export --out ./export/
```

Produces `tensors.tsv` and `metadata.tsv` files.

### `kb eval`

Run the evaluation framework to measure retrieval quality.

```bash
make eval                                          # one-command eval
kb eval --db eval.db --testset eval/testset.json   # manual
kb eval --db eval.db --corpus eval/corpus --ingest  # ingest corpus first
kb eval --db eval.db --json                        # structured output
```

Reports recall@K, precision@K, MRR, and chunking statistics. See [Evaluation](https://knowledgebroker.dev/eval/) for details.

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
| `KB_DEFAULT_LIMIT` | `5` | Default fragment retrieval limit |

Connector-specific variables (Git, Confluence, Slack, etc.) are documented in [Connectors](https://knowledgebroker.dev/connectors/).

## Architecture

### Ingestion pipeline

```
Source (filesystem, Git, Confluence, Slack, GitHub Wiki)
  → Connector        Pull content, track what's changed via checksums
  → Extractor         Chunk at semantic boundaries per file type
  → Enrichment        LLM adds context annotations to each chunk (optional)
  → Embedding         Ollama converts chunks to vectors
  → Storage           SQLite + sqlite-vec (vectors) + FTS5 (keyword index)
```

**Connectors** are pluggable adapters that pull content. Each source is registered with a type and name — the connector handles authentication, pagination, and change detection. Ingestion is incremental: unchanged files (by checksum) are skipped.

**Extractors** split files into chunks at semantic boundaries — markdown splits on headings, code on function/class boundaries, plaintext on paragraphs. Oversized chunks get a fixed-size fallback with overlap.

**Enrichment** (optional, requires Ollama) runs a small local LLM over each chunk with a sliding window of neighboring chunks. It appends entity and keyword annotations that improve retrieval without modifying the original text.

**Embedding** converts each chunk to a vector via Ollama (`nomic-embed-text` by default). Vectors are stored in sqlite-vec for similarity search. The raw text is also indexed in an FTS5 table for keyword search.

### Query pipeline

```
User query
  → Multi-query expansion     LLM generates 3-5 alternative phrasings (optional)
  → Embedding                 Ollama embeds original + expanded queries
  → Hybrid search             Vector similarity (sqlite-vec) + BM25 keyword (FTS5)
  → RRF merge                 Reciprocal Rank Fusion across all result lists
  → Synthesis / Raw return    Claude synthesises an answer, or return fragments directly
```

**Multi-query expansion** (optional, requires API key) does a quick scout retrieval to extract domain vocabulary, then asks the LLM to rephrase the query using those terms. This helps when the user's phrasing doesn't match the corpus vocabulary.

**Hybrid search** runs every query through both vector similarity and BM25 keyword search. Each expanded query variant is searched independently. Results are merged via **Reciprocal Rank Fusion** (RRF), which boosts fragments that appear in multiple result lists.

**Synthesis mode** sends the top fragments to Claude with a system prompt that instructs it to assess confidence signals, cite sources, and flag contradictions. **Raw mode** returns fragments directly with per-fragment confidence scores computed locally.

### Knowledge clustering (optional)

```
All fragments
  → K-means clustering on embeddings
  → Topic labeling per cluster
  → Knowledge units with centroid embeddings
```

`kb compute-units` groups fragments into topic clusters, producing knowledge units with summaries and confidence signals. These are searchable alongside individual fragments.

## What requires an API key

KB is designed to be useful with only Ollama (local, free). An Anthropic API key unlocks additional capabilities but is never required for core retrieval.

| Capability | Ollama only | With `ANTHROPIC_API_KEY` |
|------------|:-----------:|:------------------------:|
| Ingestion (connectors, extraction, chunking) | Yes | Yes |
| Embedding | Yes | Yes |
| Enrichment (chunk annotations) | Yes | Yes |
| Vector + BM25 hybrid search | Yes | Yes |
| Per-fragment confidence signals | Yes | Yes |
| Raw retrieval (`--raw`) | Yes | Yes |
| Knowledge clustering | Yes | Yes |
| **Multi-query expansion** | — | Yes |
| **Answer synthesis** | — | Yes |
| **Cross-fragment confidence assessment** | — | Yes |
| **Contradiction detection** | — | Yes |

Without an API key, queries run in raw mode by default — you get ranked fragments with metadata and confidence scores, but no synthesised answer or query expansion. Multi-query expansion and synthesis are the only features that require an external API.

See the [full documentation](https://knowledgebroker.dev) for architecture details, connector setup, and the evaluation framework.

## License

[BSL 1.1](LICENSE) — free to use and self-host. Converts to Apache 2.0 after 4 years.