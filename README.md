# Knowledge Broker

A knowledge engine that gives AI agents reliable, structured access to your team's knowledge. Connect it to your repos, docs, and knowledge bases — agents can query it over MCP or HTTP and get back answers with confidence signals, so they know when to act, when to hedge, and when to escalate.

Works equally well as a human-facing CLI tool.

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

Knowledge Broker exposes an **MCP server** for direct integration with Claude, Cursor, and any MCP-compatible agent runtime:

```bash
kb mcp                  # stdio + SSE on :8082
kb mcp --addr :9090     # custom port
```

The SSE endpoint is at `http://<addr>/sse`. Exposed tools: `query`, `list-sources`.

Agents receive the same structured JSON response shown above — a synthesised answer with confidence scores, source attribution, and any contradictions — so they can reason about reliability rather than treating all retrieved knowledge as equally trustworthy.

Raw mode is also available for cases where you want fragments without synthesis — useful for debugging retrieval, feeding a separate pipeline, or when no API key is configured. Pass `raw=true` to the `query` tool, or use `--raw` on the CLI.

See [docs/mcp.md](docs/mcp.md) for full setup and tool reference.

## Quick start

### Docker (recommended)

```bash
docker compose up -d

# Ingest a local directory into the server
kb ingest --source ./my-repo --remote http://localhost:8080

# Query via HTTP API
curl -s -X POST localhost:8080/v1/query \
  -H 'Content-Type: application/json' \
  -d '{"messages":[{"role":"user","content":"how does auth work?"}],"mode":"raw"}'
```

For LLM-synthesised answers, add your API key to `.env` before starting:

```bash
cp .env.example .env
# Edit .env and set ANTHROPIC_API_KEY
docker compose up -d
```

### From source

**Prerequisites:** Go 1.24+, [Ollama](https://ollama.com) running locally

```bash
make build
ollama pull nomic-embed-text

# Ingest a local directory
kb ingest --source ./my-repo

# Ingest a Git repo by URL
kb ingest --git https://github.com/owner/repo

# Synthesised answer (set ANTHROPIC_API_KEY in .env)
kb query "how does retry logic work?"

# Raw retrieval — ranked fragments, no API key needed
kb query --raw "how does retry logic work?"
```

### Running without an API key

Raw mode (`--raw`) runs the full retrieval pipeline — embedding, vector search, confidence scoring — using only Ollama. No Anthropic API key is needed.

```bash
kb query --raw "how does auth work?"

curl -X POST localhost:8080/v1/query \
  -d '{"messages":[{"role":"user","content":"how does auth work?"}],"mode":"raw"}'
```

## How it works

1. **Connectors** pull content from sources (local filesystem, Git, Confluence, Slack, GitHub Wiki — see [docs/connectors.md](docs/connectors.md))
2. **Extractors** chunk files at semantic boundaries (headings for markdown, functions for code)
3. **Embeddings** (via Ollama) convert chunks to vectors for semantic search
4. **Storage** (SQLite + sqlite-vec) persists fragments and enables vector similarity search
5. **Query engine** embeds your query, finds relevant fragments, and either returns them directly (raw mode) or synthesises an answer via Claude
6. **Confidence signals** assess how much to trust each fragment across four dimensions

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
kb ingest --source ./path/to/dir              # local directory
kb ingest --git https://github.com/owner/repo  # Git repo by URL
kb ingest --source ./repo-a --source ./repo-b  # multiple sources
kb ingest --all                                # re-ingest all registered local sources
```

Connectors are also available for Confluence, Slack, and GitHub Wiki. See [docs/connectors.md](docs/connectors.md) for setup instructions.

Ingestion is incremental — unchanged files are skipped based on checksums.

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

Exposes tools: `query`, `list-sources`. Synthesis is the default; pass `raw=true` for retrieval without LLM synthesis. See [docs/mcp.md](docs/mcp.md) for setup and tool reference.

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

Reports recall@K, precision@K, MRR, and chunking statistics. See [docs/eval.md](docs/eval.md) for details.

## Configuration

Copy `.env.example` to `.env` and fill in your values. Environment variables also work and take precedence over `.env`.

| Variable | Default | Description |
|----------|---------|-------------|
| `KB_DB` | `kb.db` | SQLite database path |
| `KB_OLLAMA_URL` | `http://localhost:11434` | Ollama API URL |
| `KB_OLLAMA_MODEL` | `nomic-embed-text` | Ollama embedding model |
| `KB_EMBEDDING_DIM` | `768` | Embedding vector dimension |
| `ANTHROPIC_API_KEY` | — | Anthropic API key (only needed for synthesis mode) |
| `KB_CLAUDE_MODEL` | `claude-sonnet-4-20250514` | Claude model for synthesis |
| `KB_LISTEN_ADDR` | `:8080` | HTTP server listen address |
| `KB_MAX_CHUNK_SIZE` | `2000` | Max chunk size in characters |
| `KB_CHUNK_OVERLAP` | `150` | Chunk overlap in characters |
| `KB_WORKERS` | `4` | Parallel ingestion workers |
| `KB_DEFAULT_LIMIT` | `20` | Default fragment retrieval limit |

Connector-specific variables (Git, Confluence, Slack, etc.) are documented in [docs/connectors.md](docs/connectors.md).

## Architecture

```
Connectors (filesystem, Git, Confluence, Slack, GitHub Wiki)
  → Extractors (markdown, code, plaintext)
  → Embed via Ollama
  → Store in SQLite + sqlite-vec

Query (raw mode)
  → Embed query via Ollama
  → Vector search (sqlite-vec)
  → Compute per-fragment confidence signals
  → Return ranked fragments

Query (synthesis mode)
  → Embed query via Ollama
  → Vector search (sqlite-vec)
  → Synthesise via Claude
  → Stream answer + confidence signals
```

See [knowledge-broker.md](knowledge-broker.md) for the full spec and design decisions.

## License

[BSL 1.1](LICENSE) — free to use and self-host. Converts to Apache 2.0 after 4 years.