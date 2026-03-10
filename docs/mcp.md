# MCP Server

Knowledge Broker exposes an [MCP](https://modelcontextprotocol.io) server that any MCP-compatible client can use to query and explore the knowledge base. Both stdio and SSE transports run simultaneously.

## Setup

### Claude Desktop

Add to `~/Library/Application Support/Claude/claude_desktop_config.json` (macOS) or `%APPDATA%\Claude\claude_desktop_config.json` (Windows):

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

This uses the stdio transport — no ports or HTTPS needed. Restart Claude after editing.

If `kb` is on your PATH (e.g. after `make install`), you can use `"command": "kb"` directly.

### Claude Code / Cursor

Same config format as above. These editors launch MCP servers as subprocesses via stdio.

### Remote / SSE

`kb mcp` also starts an SSE transport on `:8082` by default:

```bash
kb mcp                  # stdio + SSE on :8082
kb mcp --addr :9090     # custom SSE port
```

The SSE endpoint is at `http://<addr>/sse` with messages at `http://<addr>/message`. For the Claude app's remote MCP feature, you'll need HTTPS — put a reverse proxy or tunnel (e.g. Cloudflare Tunnel, ngrok) in front.

### Shared server

For team use, run the HTTP server and push content from local checkouts:

```bash
kb serve --addr :8080

# Push content from a local checkout
kb ingest --source ./my-repo --remote http://server:8080
```

## Tools

### query

Ask a question and get an answer from the knowledge base.

| Parameter | Type | Required | Default | Description |
|-----------|------|----------|---------|-------------|
| `question` | string | yes | — | The question to ask |
| `topics` | string | no | — | Comma-separated topics to boost relevance |
| `limit` | number | no | 20 | Max fragments to retrieve |
| `raw` | boolean | no | false | Return raw fragments instead of synthesised answer |
| `sources` | string | no | — | Comma-separated source names to filter results |

**Synthesis mode (default):** Returns a synthesised answer with confidence signals and source citations. Requires `ANTHROPIC_API_KEY`.

**Raw mode (raw=true):** Returns fragments with content, source metadata, and per-fragment confidence signals. No API key required.

### list-sources

List all ingested sources with fragment counts and last sync time. Takes no parameters.

Returns an array of sources with `source_type`, `source_name`, `fragment_count`, and `last_ingest`.

## Environment variables

| Variable | Required | Description |
|----------|----------|-------------|
| `ANTHROPIC_API_KEY` | For synthesis mode | Not needed when `raw=true` |
| `KB_OLLAMA_URL` | No (default `http://localhost:11434`) | Ollama server for embeddings |
| `KB_OLLAMA_MODEL` | No (default `nomic-embed-text`) | Embedding model |
| `KB_DB` | No (default `kb.db`) | Database path |

## Workflow

1. Ingest content: `kb ingest --source /path/to/project`
2. Configure your MCP client to run `kb mcp`
3. Query the knowledge base — synthesis mode returns answers, raw mode returns fragments
4. Use `list-sources` to see what's been ingested
