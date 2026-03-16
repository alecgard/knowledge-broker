---
description: Install Knowledge Broker and run your first query in under 5 minutes.
---

# Getting Started

The typical setup: one person (or a CI job) deploys a KB instance and ingests the org's sources. Everyone else queries it via MCP or HTTP.

## Install

```bash
curl -fsSL https://knowledgebroker.dev/install.sh | sh
```

This downloads the latest `kb` binary for your platform (macOS or Linux) and places it on your PATH.

All runtime dependencies are managed automatically on first run.

??? note "Build from source"
    Requires Go 1.24+:

    ```bash
    git clone https://github.com/alecgard/knowledge-broker.git
    cd knowledge-broker
    make install
    ```

    `make install` builds the `kb` binary and adds it to your PATH.

## Ingest your org's sources

Point KB at your team's repos, Confluence spaces, Slack channels, etc. Descriptions help agents understand what each source contains:

```bash
kb ingest --source ./my-project --description "Payment processing service"
kb ingest --git https://github.com/acme/platform --description "Platform services"
kb ingest --confluence ENGINEERING --description "Engineering wiki"
kb ingest --slack C0ABC123DEF --description "Platform engineering channel"
```

KB walks each source, chunks files at semantic boundaries (headings for markdown, functions for code), embeds them locally, and stores everything in a single SQLite database.

Ingestion is incremental, so re-running the same command only processes new or changed files. Set this up as a cron job or CI step to keep the knowledge base current.

## Query

### Raw mode (no API key needed)

Raw mode runs the full retrieval pipeline (embedding, hybrid search, confidence scoring) entirely locally. No external API key required.

```bash
kb query --raw "how does authentication work?"
```

Returns ranked fragments with content, source metadata, and per-fragment confidence scores.

### Synthesis mode (requires an LLM provider)

For synthesised answers with cross-fragment confidence assessment and contradiction detection. Set an API key for your preferred provider:

```bash
# Claude (default)
export ANTHROPIC_API_KEY=sk-ant-...

# Or OpenAI
export KB_LLM_PROVIDER=openai
export OPENAI_API_KEY=sk-...

# Or use a local model via Ollama (no API key needed)
export KB_LLM_PROVIDER=ollama
```

```bash
kb query "how does authentication work?"
```

Returns a natural-language answer with an overall confidence score, source citations, and any contradictions between sources.

### Human-readable streaming

```bash
kb query --human "how does authentication work?"
```

Streams the answer to the terminal as it's generated.

## Start the server

Start the server so your team can query:

```bash
kb serve                  # HTTP API on :8080, MCP on :8082 (stdio + SSE)
```

### Connect your team's MCP clients

Each developer adds this to their MCP client config (Claude Code, Cursor, etc.):

```json
{
  "mcpServers": {
    "knowledge-broker": {
      "command": "kb",
      "args": ["serve", "--no-http", "--no-sse"]
    }
  }
}
```

For remote access via SSE, point clients at `http://<server>:8082/sse`. See [MCP Server](mcp.md) for the full tool reference and client configuration.

### HTTP API

```bash
# Query via HTTP
curl -s -X POST localhost:8080/v1/query \
  -H 'Content-Type: application/json' \
  -d '{"messages":[{"role":"user","content":"how does auth work?"}]}'

# Raw mode via HTTP
curl -s -X POST localhost:8080/v1/query \
  -H 'Content-Type: application/json' \
  -d '{"messages":[{"role":"user","content":"how does auth work?"}],"mode":"raw"}'
```

## What requires an API key

KB works entirely locally out of the box. An LLM provider (Claude, OpenAI, or local via Ollama) unlocks additional capabilities but is never required for core retrieval.

| Capability | Local only | With API key |
|------------|:-----------:|:------------:|
| Ingestion, embedding, hybrid search | :material-check: | :material-check: |
| Raw retrieval with confidence signals | :material-check: | :material-check: |
| Chunk enrichment (entity/keyword annotations) | :material-check: | :material-check: |
| **Multi-query expansion** | | :material-check: |
| **Answer synthesis** | | :material-check: |
| **Cross-fragment confidence assessment** | | :material-check: |
| **Contradiction detection** | | :material-check: |

## Next steps

- [Deploy for your team](deployment.md) — shared server, connect from developer machines
- [Connect more sources](connectors.md) — Confluence, Slack, GitHub Wiki
- [Understand the trust layer](architecture.md) — how confidence signals work
- [MCP Server](mcp.md) — tools and prompts for AI agents
- [CLI Reference](cli.md) — all commands and flags
