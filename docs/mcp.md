---
description: Set up the Knowledge Broker MCP server for Claude Code, Codex, Cursor, Windsurf, and other MCP-compatible AI agents. Stdio and SSE transports for local and team use.
---

# MCP Server

Knowledge Broker exposes an [MCP](https://modelcontextprotocol.io) server that any MCP-compatible client can use to query and explore the knowledge base. Both stdio and SSE transports run simultaneously.

## Setup

The typical deployment: one KB instance runs on a shared machine (or in CI), with your org's sources already ingested. Developers and agents connect to it via MCP or HTTP.

### Connecting MCP clients

Each developer adds KB to their MCP client config (Claude Code, Cursor, Windsurf, etc.):

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

If `kb` is on your PATH, you can use `"command": "kb"` directly. This launches KB as a subprocess via stdio.

### SSE (remote access)

`kb mcp` also starts an SSE transport on `:8082` by default, so remote clients can connect without running the binary locally:

```bash
kb mcp                  # stdio + SSE on :8082
kb mcp --addr :9090     # custom SSE port
```

The SSE endpoint is at `http://<addr>/sse` with messages at `http://<addr>/message`. For HTTPS, put a reverse proxy or tunnel in front.

### Remote ingestion

Team members can push content from their local checkouts to the shared instance:

```bash
# On the server
kb serve --addr :8080

# From a developer's machine
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
| `no_expand` | boolean | no | false | Disable multi-query expansion |

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

## Typical setup

1. **Deploy KB** on a shared machine or in CI. Ingest your org's sources: `kb ingest --confluence ENGINEERING --git https://github.com/org/repo --slack C0ABC123DEF`
2. **Start the server**: `kb mcp` (and/or `kb serve` for HTTP)
3. **Each developer** adds KB to their MCP client config (see above)
4. Agents call `query` for answers with confidence signals, or `list-sources` to discover what's available
5. The `kb-instructions` prompt bootstraps agent context automatically — no manual prompt engineering needed
