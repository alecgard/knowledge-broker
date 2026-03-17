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
| `--remote` | URL of a remote KB server to push fragments to |
| `--description` | Human-readable description of the source (shown to agents) |
| `--db` | SQLite database path (default: `kb.db`) |

All connector flags can be combined in a single command. Ingestion is incremental, unchanged files are skipped based on checksums.

See [Connectors](connectors.md) for detailed setup instructions per source type.

## kb query

Query the knowledge base.

```bash
# Raw retrieval (no API key needed)
kb query --raw "how does auth work?"

# Synthesised answer (requires an LLM provider, Claude by default)
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
| `--remote` | URL of a remote KB server to query |

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
| `/v1/sources` | PATCH | Update source description |
| `/v1/sources` | DELETE | Remove a source and its fragments |
| `/v1/sources/import` | POST | Import sources from JSON |
| `/v1/export` | GET | Export fragment embeddings as JSON |
| `/v1/version` | GET | Server version |
| `/v1/health` | GET | Health check |

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
- The `messages` array follows the same format as the Anthropic Messages API. Pass conversation history for multi-turn queries.

## kb sources

Manage registered ingestion sources. All subcommands accept `--remote` to operate on a remote KB server.

### kb sources list

List all registered sources with type, name, description, fragment count, and last ingest time.

```bash
kb sources list
kb sources list --remote http://server:8080
```

### kb sources describe

Set a description for an existing source. Descriptions appear in `list-sources` results and the `kb-instructions` prompt.

```bash
kb sources describe filesystem/my-repo "Payment processing microservice"
kb sources describe git/owner/repo "Main backend API"
kb sources describe --remote http://server:8080 git/owner/repo "Main backend API"
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
kb sources remove --remote http://server:8080 git/owner/repo
```

## kb export

Export fragment embeddings for visualization with TensorBoard Embedding Projector.

```bash
kb export --out ./export/
kb export --remote http://server:8080 --out ./export/
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

Verify runtime dependencies and pull required models. Useful for checking everything works before first use, or re-running setup after problems.

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
kb version --remote http://server:8080
```

## kb config

Show the resolved configuration: where each value comes from, which config files were loaded, and the current value of every setting (secrets are masked).

```bash
kb config
kb config --config /etc/kb/config
```

```
Config files:
  ~/.config/kb/config                      found
  .env                                     not found
  --config                                 (not specified)

KEY                       VALUE                               SOURCE
KB_DB                     kb.db                               default
KB_OLLAMA_URL             http://localhost:11434              ~/.config/kb/config
ANTHROPIC_API_KEY         sk-ant-a****                        env
...
```

## Global flags

| Flag | Description |
|------|-------------|
| `--config` | Path to config file (overrides `.env` and `~/.config/kb/config`) |
| `--debug` | Enable debug mode (log all API calls) |
| `--no-setup` | Skip automatic runtime management (useful for CI or custom deployments) |

## Configuration

KB loads configuration from multiple sources. Later sources override earlier ones:

| Precedence | Source | Description |
|:----------:|--------|-------------|
| 1 (lowest) | Defaults | Built-in defaults |
| 2 | `~/.config/kb/config` | Persistent user config (respects `$XDG_CONFIG_HOME`) |
| 3 | `.env` in working directory | Project-local overrides (useful during development) |
| 4 | `--config <path>` | Explicit file path (useful for server deployments) |
| 5 (highest) | Environment variables | Always take precedence |

All config files use the same `KEY=VALUE` format (same as `.env`). Run `kb config` to see which source each value comes from.

### Environment variables

| Variable | Default | Description |
|----------|---------|-------------|
| `KB_DB` | `kb.db` | SQLite database path |
| `KB_OLLAMA_URL` | `http://localhost:11434` | Embedding server URL |
| `KB_EMBEDDING_MODEL` | `nomic-embed-text` | Embedding model name |
| `KB_ENRICH_MODEL` | `qwen2.5:0.5b` | Enrichment model name |
| `KB_SKIP_SETUP` | `false` | Skip automatic runtime management |
| `KB_LLM_PROVIDER` | `claude` | LLM provider: `claude`, `openai`, or `ollama` |
| `ANTHROPIC_API_KEY` | — | API key for Claude (default LLM provider) |
| `KB_CLAUDE_MODEL` | `claude-sonnet-4-20250514` | Claude model for synthesis |
| `OPENAI_API_KEY` | — | API key for OpenAI |
| `KB_LISTEN_ADDR` | `:8080` | Default HTTP listen address |
| `KB_MAX_CHUNK_SIZE` | `2000` | Max chunk size in characters |
| `KB_CHUNK_OVERLAP` | `150` | Chunk overlap in characters |
| `KB_WORKERS` | `4` | Parallel ingestion workers |
| `KB_DEFAULT_LIMIT` | `20` | Default fragment retrieval limit |
