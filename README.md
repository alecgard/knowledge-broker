# Knowledge Broker

A knowledge engine that ingests documents from multiple sources, embeds them for semantic retrieval, and answers questions with confidence signals.

Connect it to your repos, docs, and knowledge bases — then ask it questions. It tells you not just the answer, but how much to trust it.

## Quick start

### Prerequisites

- Go 1.21+
- [Ollama](https://ollama.com) running locally with an embedding model
- An [Anthropic API key](https://console.anthropic.com/) for query synthesis (optional — raw mode works without it)

### Install

```bash
# From source
make build

# Or install to GOPATH/bin
make install
```

### Set up Ollama

```bash
ollama pull nomic-embed-text
```

### Configure

```bash
cp .env.example .env
```

Edit `.env` and add your API keys:

```
ANTHROPIC_API_KEY=sk-ant-...
GITHUB_TOKEN=ghp_...          # optional, for GitHub connector
```

See `.env.example` for all available options.

### Ingest and query

```bash
# Ingest a local directory
kb ingest --source ./my-repo

# Ingest a GitHub repo
kb ingest --github owner/repo

# Ask a question
kb query "how does retry logic work?"
```

## How it works

1. **Connectors** pull content from sources (local filesystem, GitHub)
2. **Extractors** chunk files at semantic boundaries (headings for markdown, functions for code)
3. **Embeddings** (via Ollama) convert chunks to vectors for semantic search
4. **Storage** (SQLite + sqlite-vec) persists fragments and enables vector similarity search
5. **Query engine** embeds your question, finds relevant fragments, and synthesises an answer via Claude
6. **Confidence signals** assess how much to trust the answer across four dimensions

## Confidence signals

Every answer includes four independent confidence scores (0.0–1.0):

| Signal | What it measures |
|--------|-----------------|
| **Freshness** | How recently were the sources modified, relative to the corpus |
| **Corroboration** | How many independent sources support the answer |
| **Consistency** | Do the sources agree, or are there contradictions |
| **Authority** | How authoritative are the source types for this kind of question |

Contradictions between sources are flagged rather than hidden.

## Commands

### `kb ingest`

Ingest documents from a source into the knowledge base.

```bash
kb ingest --source ./path/to/dir    # Local directory
kb ingest --github owner/repo       # GitHub repository
kb ingest --db myproject.db         # Custom database path
```

Ingestion is incremental — unchanged files are skipped based on checksums.

### `kb query`

Ask a question and get an answer with confidence signals.

```bash
# Synthesised answer (requires ANTHROPIC_API_KEY)
kb query "what is the billing retry policy?"
kb query --human "how does auth work?"    # streamed, human-readable

# Raw retrieval — returns ranked fragments, no LLM needed
kb query --raw "how does auth work?"
kb query --raw --json "explain the deployment process"
kb query --raw --limit 10 --topics "billing,payments" "retry policy"
```

Raw mode (`--raw`) returns fragments with per-fragment confidence signals, source metadata, and content previews. Add `--json` for structured output. This is the primary mode for MCP consumers and tool integrations — no Anthropic API key required.

### `kb serve`

Start an HTTP API server.

```bash
kb serve --addr :8080 --db kb.db
```

Endpoints:
- `POST /v1/query` — query with optional SSE streaming (`{"messages": [{"role": "user", "content": "..."}]}`)
- `POST /v1/query` with `"mode": "raw"` — raw retrieval, returns ranked fragments without LLM synthesis
- `POST /v1/feedback` — submit feedback (`{"fragment_id": "...", "type": "correction", "content": "..."}`)
- `POST /v1/ingest` — receive fragments from remote ingestion (`kb ingest --remote`)
- `GET /v1/health` — health check

### `kb mcp`

Start an MCP (Model Context Protocol) server on stdio, for integration with AI tools like Claude Code.

```bash
kb mcp --db kb.db
```

Exposes tools: `query`, `feedback`, `list-sources`. Defaults to raw retrieval mode (no API key needed). See [docs/mcp.md](docs/mcp.md) for setup and tool reference.

### `kb feedback`

Submit feedback on a fragment to improve knowledge quality over time.

```bash
kb feedback --fragment-id abc123 --type correction --content "It actually retries 5 times"
kb feedback --fragment-id abc123 --type challenge
kb feedback --fragment-id abc123 --type confirmation
```

Feedback types:
- **correction** — "that's wrong, it's actually X" (degrades confidence, stores correction)
- **challenge** — "I don't think that's right" (degrades confidence)
- **confirmation** — "that's correct" (boosts confidence)

### `kb eval`

Run the evaluation framework to measure retrieval quality.

```bash
make eval                                    # one-command eval
kb eval --db eval.db --testset eval/testset.json  # manual
kb eval --db eval.db --ingest --json         # ingest corpus + JSON output
```

Reports recall@K, precision@K, MRR, and chunking statistics. See [docs/eval.md](docs/eval.md) for details.

## Configuration

Copy `.env.example` to `.env` and configure. Environment variables and `.env` are both supported — env vars take precedence.

| Variable | Default | Description |
|----------|---------|-------------|
| `KB_DB` | `kb.db` | SQLite database path |
| `KB_OLLAMA_URL` | `http://localhost:11434` | Ollama API URL |
| `KB_OLLAMA_MODEL` | `nomic-embed-text` | Ollama embedding model |
| `KB_EMBEDDING_DIM` | `768` | Embedding vector dimension |
| `ANTHROPIC_API_KEY` | — | Anthropic API key (required for synthesis, not needed for raw mode) |
| `KB_CLAUDE_MODEL` | `claude-sonnet-4-20250514` | Claude model for synthesis |
| `KB_LISTEN_ADDR` | `:8080` | HTTP server listen address |
| `KB_WORKERS` | `4` | Parallel ingestion workers |
| `KB_DEFAULT_LIMIT` | `20` | Default fragment retrieval limit |
| `GITHUB_TOKEN` | — | GitHub token (for GitHub connector) |

## Architecture

```
Connectors (filesystem, GitHub)
  → Extractors (markdown, code, plaintext)
  → Embed via Ollama
  → Store in SQLite + sqlite-vec

Query
  → Embed question via Ollama
  → Vector search (sqlite-vec)
  → Synthesise via Claude
  → Stream answer + confidence signals
```

See [knowledge-broker.md](knowledge-broker.md) for the full spec and design decisions.

## License

[BSL 1.1](LICENSE) — free to use and self-host. Converts to Apache 2.0 after 4 years.
