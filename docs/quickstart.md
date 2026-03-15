---
description: Install Knowledge Broker and run your first query in under 5 minutes.
---

# Getting Started

## Prerequisites

- **Go 1.24+** — [install Go](https://go.dev/doc/install)
- **Ollama** running locally — [install Ollama](https://ollama.com)

## Install

```bash
# Clone and install
git clone https://github.com/alecgard/knowledge-broker.git
cd knowledge-broker
make install

# Pull the embedding model
ollama pull nomic-embed-text
```

`make install` builds the `kb` binary and adds it to your PATH.

## Ingest your first source

Point KB at a local directory or a Git repo:

```bash
# Local directory
kb ingest --source ./my-project

# Git repo by URL
kb ingest --git https://github.com/owner/repo

# Add a description so agents know what this source contains
kb ingest --source ./my-project --description "Payment processing service"
```

KB walks the source, chunks files at semantic boundaries (headings for markdown, functions for code), embeds them via Ollama, and stores everything in a local SQLite database.

Ingestion is incremental — re-running the same command only processes new or changed files.

## Query

### Raw mode (no API key needed)

Raw mode runs the full retrieval pipeline — embedding, hybrid search, confidence scoring — using only Ollama. No external API key required.

```bash
kb query --raw "how does authentication work?"
```

Returns ranked fragments with content, source metadata, and per-fragment confidence scores.

### Synthesis mode (requires Anthropic API key)

For synthesised answers with cross-fragment confidence assessment and contradiction detection:

```bash
export ANTHROPIC_API_KEY=sk-ant-...
kb query "how does authentication work?"
```

Returns a natural-language answer with an overall confidence score, source citations, and any contradictions between sources.

### Human-readable streaming

```bash
kb query --human "how does authentication work?"
```

Streams the answer to the terminal as it's generated.

## Start the MCP server

Connect KB to Claude Code or any MCP-compatible client:

```bash
kb mcp
```

This starts both stdio and SSE transports. Configure your MCP client:

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

See [MCP Server](mcp.md) for the full setup guide.

## Start the HTTP server

```bash
kb serve

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

KB is designed to work with only Ollama. An Anthropic API key unlocks additional capabilities but is never required for core retrieval.

| Capability | Ollama only | With API key |
|------------|:-----------:|:------------:|
| Ingestion, embedding, hybrid search | :material-check: | :material-check: |
| Raw retrieval with confidence signals | :material-check: | :material-check: |
| Chunk enrichment (entity/keyword annotations) | :material-check: | :material-check: |
| **Multi-query expansion** | | :material-check: |
| **Answer synthesis** | | :material-check: |
| **Cross-fragment confidence assessment** | | :material-check: |
| **Contradiction detection** | | :material-check: |

## Next steps

- [Connect more sources](connectors.md) — Confluence, Slack, GitHub Wiki
- [Understand the trust layer](architecture.md) — how confidence signals work
- [Set up MCP for your team](mcp.md) — shared knowledge server
- [CLI Reference](cli.md) — all commands and flags
