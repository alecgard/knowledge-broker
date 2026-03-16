---
description: Complete command reference for the Knowledge Broker CLI.
---

# CLI Reference

## kb ingest

Ingest documents from one or more sources into the knowledge base.

```bash
kb ingest --source ./path/to/dir
kb ingest --git https://github.com/owner/repo
kb ingest --confluence ENGINEERING
kb ingest --slack C0ABC123DEF
kb ingest --wiki https://github.com/owner/repo
kb ingest --all
```

| Flag | Description |
|------|-------------|
| `--source` | Local directory path (repeatable) |
| `--git` | Git repository URL (repeatable) |
| `--confluence` | Confluence space key (repeatable) |
| `--slack` | Slack channel ID (repeatable) |
| `--wiki` | GitHub Wiki repository URL (repeatable) |
| `--all` | Re-ingest all registered local sources |
| `--remote` | Send fragments to a remote KB server for embedding |
| `--description` | Human-readable description of the source (shown to agents) |
| `--db` | SQLite database path (default: `kb.db`) |

All connector flags can be combined in a single command. Ingestion is incremental, unchanged files are skipped based on checksums.

See [Connectors](connectors.md) for detailed setup instructions per source type.

## kb query

Query the knowledge base.

```bash
# Raw retrieval (no API key needed)
kb query --raw "how does auth work?"

# Synthesised answer (requires ANTHROPIC_API_KEY)
kb query "what is the billing retry policy?"

# Human-readable streaming
kb query --human "how does deployment work?"

# With filters
kb query --raw --limit 10 --topics "billing,payments" "retry policy"
kb query --raw --source-type git "deployment process"
```

| Flag | Description |
|------|-------------|
| `--raw` | Return ranked fragments without LLM synthesis |
| `--human` | Stream the answer in human-readable format |
| `--limit` | Maximum number of fragments to retrieve |
| `--topics` | Comma-separated topics to boost relevance |
| `--source-type` | Filter by source type (`filesystem`, `git`, `confluence`, `slack`, `github_wiki`) |
| `--db` | SQLite database path (default: `kb.db`) |

## kb serve

Start the HTTP API and MCP server. Runs the HTTP API, MCP stdio transport, and MCP SSE transport in a single process.

```bash
kb serve
kb serve --addr :9090 --mcp-addr :9091
```

| Flag | Default | Description |
|------|---------|-------------|
| `--addr` | `:8080` | HTTP listen address |
| `--mcp-addr` | `:8082` | MCP SSE listen address |
| `--db` | `kb.db` | SQLite database path |
| `--no-http` | `false` | Disable HTTP API server |
| `--no-sse` | `false` | Disable MCP SSE transport |
| `--no-stdio` | `false` | Disable MCP stdio transport |

Use `--no-*` flags to run only the transports you need:

```bash
kb serve                              # all transports (default)
kb serve --no-http --no-sse           # stdio only (for MCP client configs)
kb serve --no-stdio                   # HTTP + SSE (headless server deployment)
```

### Endpoints

| Endpoint | Method | Description |
|----------|--------|-------------|
| `/v1/query` | POST | Query with optional SSE streaming |
| `/v1/ingest` | POST | Receive fragments from remote ingestion |
| `/v1/sources` | GET | List registered sources |
| `/v1/health` | GET | Health check (verifies Ollama connectivity) |

### Query request format

```json
{
  "messages": [{"role": "user", "content": "how does auth work?"}],
  "limit": 20,
  "mode": "raw",
  "stream": true,
  "topics": ["billing", "payments"],
  "sources": ["my-repo"],
  "source_types": ["git", "confluence"],
  "no_expand": false
}
```

- Omit `mode` for synthesis (default). Set `"mode": "raw"` for raw retrieval.
- Set `"stream": true` for SSE streaming (synthesis mode only).
- The `messages` array follows the same format as the Claude API. Pass conversation history for multi-turn queries.

## kb sources

Manage registered ingestion sources.

### kb sources list

List all registered sources with type, name, description, fragment count, and last ingest time.

```bash
kb sources list
```

### kb sources describe

Set a description for an existing source. Descriptions appear in `list-sources` results and the `kb-instructions` prompt.

```bash
kb sources describe filesystem/my-repo "Payment processing microservice"
kb sources describe git/owner/repo "Main backend API"
```

### kb sources export

Export registered sources to a JSON file.

```bash
kb sources export sources.json
```

### kb sources import

Import sources from a JSON file.

```bash
kb sources import sources.json
```

### kb sources remove

Remove a registered source and all its fragments from the database.

```bash
kb sources remove confluence/ENGINEERING
kb sources remove git/owner/repo
```

## kb export

Export fragment embeddings for visualization with TensorBoard Embedding Projector.

```bash
kb export --out ./export/
```

Produces `tensors.tsv` and `metadata.tsv` files that can be loaded into the [Embedding Projector](https://projector.tensorflow.org/).

## kb eval

Run the evaluation framework to measure retrieval quality.

```bash
make eval                                            # one-command eval
kb eval --db eval.db --testset eval/testset.json     # manual
kb eval --db eval.db --corpus eval/corpus --ingest   # ingest corpus first
kb eval --db eval.db --json                          # structured output
```

| Flag | Default | Description |
|------|---------|-------------|
| `--db` | `kb.db` | Database path |
| `--testset` | `eval/testset.json` | Path to test set |
| `--corpus` | `eval/corpus` | Path to eval corpus |
| `--limit` | `20` | Top-K retrieval limit |
| `--ingest` | `false` | Ingest corpus before running eval |
| `--json` | `false` | Output structured JSON |
| `--skip-enrichment` | `false` | Skip chunk enrichment during ingestion |

See [Evaluation](eval.md) for details on metrics, test cases, and extending the eval suite.

## kb cluster

Run k-means clustering on fragment embeddings to discover topic groups.

```bash
kb cluster
```

### kb cluster viz

Generate an interactive HTML visualization of fragment clusters.

```bash
kb cluster viz
```

## kb setup

Verify Ollama installation and pull required models. Useful for checking everything works before first use, or re-running setup after problems.

```bash
kb setup
```

```
Checking Ollama... running at http://localhost:11434
Checking models...
  nomic-embed-text... available
  qwen2.5:0.5b... available
Ready.
```

### kb setup mcp

Configure MCP settings for Claude Code or Cursor.

```bash
kb setup mcp
kb setup mcp --client claude --global
kb setup mcp --client cursor --local
```

## kb version

Print the KB version.

```bash
kb version
```

## Global flags

| Flag | Description |
|------|-------------|
| `--debug` | Enable debug mode (log all API calls) |
| `--no-setup` | Skip automatic Ollama management (useful for CI or remote Ollama) |

## Environment variables

| Variable | Default | Description |
|----------|---------|-------------|
| `KB_OLLAMA_URL` | `http://localhost:11434` | Ollama server URL |
| `KB_EMBEDDING_MODEL` | `nomic-embed-text` | Embedding model name |
| `KB_ENRICH_MODEL` | `qwen2.5:0.5b` | Enrichment model name |
| `KB_SKIP_SETUP` | `false` | Skip automatic Ollama management |
| `KB_LLM_PROVIDER` | `claude` | LLM provider: `claude`, `openai`, or `ollama` |
| `KB_DB` | `kb.db` | Default database path |
| `KB_LISTEN_ADDR` | `:8080` | Default HTTP listen address |
