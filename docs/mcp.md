---
description: Set up the Knowledge Broker MCP server for Claude Code and other MCP clients. Stdio and SSE transports for local and team use.
---

# MCP Server

Knowledge Broker exposes an [MCP](https://modelcontextprotocol.io) server that any MCP-compatible client can use to query and explore the knowledge base. Both stdio and SSE transports run simultaneously.

## Setup

### stdio (local)

Most MCP clients launch servers as subprocesses via stdio. Point your client at `kb mcp`:

```json
{
  "mcpServers": {
    "knowledge-broker": {
      "command": "/path/to/kb",
      "args": ["mcp"]
    }
  }
}
```

If `kb` is on your PATH, you can use `"command": "kb"` directly.

### SSE (remote)

`kb mcp` also starts an SSE transport on `:8082` by default:

```bash
kb mcp                  # stdio + SSE on :8082
kb mcp --addr :9090     # custom SSE port
```

The SSE endpoint is at `http://<addr>/sse` with messages at `http://<addr>/message`. For remote access over HTTPS, put a reverse proxy or tunnel in front.

### Shared server

For team use, run the HTTP server and push content from local checkouts:

```bash
kb serve --addr :8080

# Push content from a local checkout
kb ingest --source ./my-repo --remote http://server:8080
```

## Tools

### query

Query the knowledge base and get an answer.

| Parameter | Type | Required | Default | Description |
|-----------|------|----------|---------|-------------|
| `query` | string | yes | — | The query to search for |
| `topics` | string | no | — | Comma-separated topics to boost relevance |
| `limit` | number | no | 20 | Max fragments to retrieve |
| `raw` | boolean | no | false | Return raw fragments instead of synthesised answer |
| `sources` | string | no | — | Comma-separated source names to filter results |
| `source_types` | string | no | — | Comma-separated source types to filter results |

**Synthesis mode (default):** Returns a synthesised answer with confidence signals and source citations. Requires `ANTHROPIC_API_KEY`.

**Raw mode (raw=true):** Returns fragments with content, source metadata, and per-fragment confidence signals. No API key required.

### list-sources

List all ingested sources with fragment counts and last sync time. Takes no parameters.

Returns an array of sources with `source_type`, `source_name`, `description`, `fragment_count`, and `last_ingest`.

## Prompts

### kb-instructions

A prompt that returns instructions teaching the agent when and how to use the knowledge base. Takes no arguments.

The response includes:
- When to query the knowledge base (missing context, unfamiliar patterns, before making assumptions)
- A dynamically generated list of available sources with descriptions and fragment counts
- Tips for using synthesis vs raw mode, topics, and source filtering

MCP clients that support prompts will show this in their prompt list. Use it to bootstrap agent context without manually writing instructions.

## Environment variables

| Variable | Required | Description |
|----------|----------|-------------|
| `ANTHROPIC_API_KEY` | For synthesis mode | Not needed when `raw=true` |
| `KB_OLLAMA_URL` | No (default `http://localhost:11434`) | Ollama server for embeddings |
| `KB_EMBEDDING_MODEL` | No (default `nomic-embed-text`) | Embedding model |
| `KB_DB` | No (default `kb.db`) | Database path |

## Workflow

1. Ingest content: `kb ingest --source /path/to/project`
2. Configure your MCP client to run `kb mcp`
3. Query the knowledge base — synthesis mode returns answers, raw mode returns fragments
4. Use `list-sources` to see what's been ingested
