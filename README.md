# Knowledge Broker

A knowledge engine that ingests documents from multiple sources, embeds them for semantic retrieval, and answers questions with confidence signals.

Connect it to your repos, docs, and knowledge bases and then ask it questions. Returns both the answer, and how much to trust it.

```
$ kb query --human "What should I check if the Nexus API is slow?"

The Nexus API high latency runbook (RB-001) outlines the following diagnostic steps:

1. Check the Grafana dashboard "Nexus API Overview" for latency breakdown by endpoint
2. If /inventory/positions is slow — likely PostgreSQL. Check pg_stat_statements
   for long-running queries on the inventory-service DB
3. If /shipments/track is slow — likely carrier API latency. Check the
   "Carrier API Latency" dashboard and the carrier's status page
4. If all endpoints are slow — check Kubernetes node CPU/memory. If saturated,
   scale up via Terraform (min 80, max 200 nodes)
5. Check Kafka consumer lag on shipment-events and inventory-updates topics

For remediation: DB issues → kill long-running queries; carrier issues → enable
circuit breaker; Kafka lag → scale consumer replicas; node pressure → terraform apply.

Escalate to SRE secondary after 30 min, VP Engineering after 1 hour.

--- Confidence ---
Freshness:     0.92
Corroboration: 0.85
Consistency:   1.00
Authority:     0.95

--- Sources ---
  [confluence:ACME/runbooks/RB-001]      Confluence — ACME space
  [slack:acme-haf5895/C0AKB4GRELF/2026-03-08] Slack — #platform-engineering
  [confluence:ACME/infrastructure]        Confluence — ACME space
```

Sources are cross-referenced: the runbook comes from Confluence, corroborated by a Slack discussion where the team walked through the same steps during an incident. Contradictions between sources are flagged rather than hidden.

## Quick start

### Docker (recommended)

```bash
docker compose up -d

# Pull the embedding model (first time only)
docker compose exec ollama ollama pull nomic-embed-text

# Ingest a local directory into the server
kb ingest --source ./my-repo --remote http://localhost:8080

# Query via HTTP API
curl -s -X POST localhost:8080/v1/query \
  -H 'Content-Type: application/json' \
  -d '{"messages":[{"role":"user","content":"how does auth work?"}],"mode":"raw"}'
```

For LLM-synthesised answers, set your API key before starting:

```bash
ANTHROPIC_API_KEY=sk-ant-... docker compose up -d
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

# Raw retrieval — ranked fragments, no API key needed
kb query --raw "how does retry logic work?"

# Synthesised answer (requires ANTHROPIC_API_KEY)
ANTHROPIC_API_KEY=sk-ant-... kb query "how does retry logic work?"
```

### Running without an API key

Raw mode (`--raw`) handles the full retrieval pipeline — embedding, vector search, confidence scoring — using only Ollama. No Anthropic API key is needed. This is the default mode for MCP consumers and is sufficient for most tool integrations where the calling LLM handles synthesis.

```bash
# CLI — returns JSON
kb query --raw "how does auth work?"

# MCP server (raw by default)
kb mcp --db kb.db

# HTTP API
curl -X POST localhost:8080/v1/query \
  -d '{"messages":[{"role":"user","content":"how does auth work?"}],"mode":"raw"}'
```

Set `ANTHROPIC_API_KEY` only when you want KB to synthesise answers via Claude.

## How it works

1. **Connectors** pull content from sources (local filesystem, Git, Confluence, Slack, GitHub Wiki — see [docs/connectors.md](docs/connectors.md))
2. **Extractors** chunk files at semantic boundaries (headings for markdown, functions for code)
3. **Embeddings** (via Ollama) convert chunks to vectors for semantic search
4. **Storage** (SQLite + sqlite-vec) persists fragments and enables vector similarity search
5. **Query engine** embeds your question, finds relevant fragments, and either returns them directly (raw mode) or synthesises an answer via Claude
6. **Confidence signals** assess how much to trust each fragment across four dimensions

## Confidence signals

Every result includes four independent confidence scores (0.0–1.0):

| Signal | What it measures |
|--------|-----------------|
| **Freshness** | How recently were the sources modified, relative to the corpus |
| **Corroboration** | How many independent sources support the answer |
| **Consistency** | Do the sources agree, or are there contradictions |
| **Authority** | How authoritative are the source types for this kind of question |

In raw mode, these are computed per fragment using local heuristics. In synthesis mode, the LLM assesses them across the full context. Contradictions between sources are flagged rather than hidden.

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

Push fragments to a shared KB server instead of embedding locally:

```bash
kb ingest --source ./my-repo --remote http://server:8080
```

The client extracts and chunks locally, then POSTs fragments to the server which handles embedding and storage. Checksums are tracked locally for incremental re-ingestion.

### `kb query`

Ask a question and get an answer with confidence signals.

```bash
# Raw retrieval — returns ranked fragments, no LLM needed
kb query --raw "how does auth work?"
kb query --raw "explain the deployment process"
kb query --raw --limit 10 --topics "billing,payments" "retry policy"

# Synthesised answer (requires ANTHROPIC_API_KEY)
kb query "what is the billing retry policy?"
kb query --human "how does auth work?"    # streamed, human-readable
```

Raw mode (`--raw`) returns full fragments as JSON with per-fragment confidence signals and source metadata.

### `kb serve`

Start an HTTP API server.

```bash
kb serve --addr :8080 --db kb.db
```

Endpoints:
- `POST /v1/query` — query with optional SSE streaming (`{"messages": [{"role": "user", "content": "..."}]}`)
- `POST /v1/query` with `"mode": "raw"` — raw retrieval, returns ranked fragments without LLM synthesis
- `POST /v1/ingest` — receive fragments from remote ingestion (`kb ingest --remote`)
- `GET /v1/health` — health check (verifies Ollama connectivity, returns 503 if unreachable)

### `kb mcp`

Start an MCP (Model Context Protocol) server on stdio.

```bash
kb mcp --db kb.db
```

Exposes tools: `query`, `list-sources`. Defaults to raw retrieval mode (no API key needed). See [docs/mcp.md](docs/mcp.md) for setup and tool reference.

### `kb sources list`

List all registered ingestion sources.

```bash
kb sources list --db kb.db
```

Returns JSON with source type, name, config, and last ingest time for each registered source.

### `kb export`

Export fragment embeddings for visualization with TensorBoard Embedding Projector.

```bash
kb export --db kb.db --out ./export/
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

Environment variables and `.env` are both supported — env vars take precedence.

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
| `KB_GITHUB_CLIENT_ID` | — | GitHub OAuth client ID (for Git connector) |

Connector-specific variables (Confluence, Slack, etc.) are documented in [docs/connectors.md](docs/connectors.md).

## Architecture

```
Connectors (filesystem, Git, Confluence, Slack, GitHub Wiki)
  → Extractors (markdown, code, plaintext)
  → Embed via Ollama
  → Store in SQLite + sqlite-vec

Query (raw mode)
  → Embed question via Ollama
  → Vector search (sqlite-vec)
  → Compute per-fragment confidence signals
  → Return ranked fragments

Query (synthesis mode)
  → Embed question via Ollama
  → Vector search (sqlite-vec)
  → Synthesise via Claude
  → Stream answer + confidence signals
```

See [knowledge-broker.md](knowledge-broker.md) for the full spec and design decisions.

## License

[BSL 1.1](LICENSE) — free to use and self-host. Converts to Apache 2.0 after 4 years.
