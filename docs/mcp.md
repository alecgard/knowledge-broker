# MCP Server

Knowledge Broker exposes an [MCP](https://modelcontextprotocol.io) server that any MCP-compatible client can use to query, give feedback on, and explore the knowledge base.

## Setup

### stdio mode (local)

Run `kb mcp` as a subprocess. Add to your MCP client config:

```json
{
  "mcpServers": {
    "knowledge-broker": {
      "command": "kb",
      "args": ["mcp", "--db", "/path/to/kb.db"]
    }
  }
}
```

If `kb` is not in your PATH, use the full path to the binary.

### HTTP mode (shared server)

For team use, run the HTTP server and point MCP clients at it. Developers push content via `kb ingest --remote`, and clients query the shared instance.

```bash
# Start the server
kb serve --db /shared/kb.db --addr :8080

# Push content from a local checkout
kb ingest --source ./my-repo --remote http://server:8080
```

MCP clients that support HTTP transports can connect to the server's `/v1/query` endpoint directly. For clients that only support stdio, run `kb mcp` locally and configure it to proxy to the shared server (not yet implemented — use the HTTP API directly for now).

## Tools

### query

Retrieve relevant knowledge fragments, optionally with LLM synthesis.

| Parameter | Type | Required | Default | Description |
|-----------|------|----------|---------|-------------|
| `question` | string | yes | — | The question to ask |
| `topics` | string | no | — | Comma-separated topics to boost relevance |
| `limit` | number | no | 20 | Max fragments to retrieve |
| `raw` | boolean | no | true | Return raw fragments (true) or LLM-synthesised answer (false) |

**Raw mode (default):** Returns fragments with content, source metadata, and per-fragment confidence signals. No `ANTHROPIC_API_KEY` required.

**Synthesis mode (raw=false):** Requires `ANTHROPIC_API_KEY`. Returns a synthesised answer with confidence signals and source citations.

### feedback

Adjust fragment confidence based on human knowledge.

| Parameter | Type | Required | Description |
|-----------|------|----------|-------------|
| `fragment_id` | string | yes | The fragment ID (from query results) |
| `type` | string | yes | `correction`, `challenge`, or `confirmation` |
| `content` | string | for corrections | The corrected information |
| `evidence` | string | no | Supporting evidence |

### list-sources

List all ingested sources with fragment counts and last sync time. Takes no parameters.

Returns an array of sources with `source_type`, `source_name`, `fragment_count`, and `last_ingest`.

## Environment variables

| Variable | Required | Description |
|----------|----------|-------------|
| `ANTHROPIC_API_KEY` | Only for synthesis mode | Not needed when `raw=true` (default) |
| `KB_OLLAMA_URL` | No (default `http://localhost:11434`) | Ollama server for embeddings |
| `KB_OLLAMA_MODEL` | No (default `nomic-embed-text`) | Embedding model |
| `KB_DB` | No (default `kb.db`) | Database path |

## Workflow

1. Ingest content: `kb ingest --source /path/to/project`
2. Configure your MCP client to run `kb mcp`
3. Query the knowledge base — raw mode returns fragments for the client to reason over
4. Submit feedback to improve confidence scores over time
5. Use `list-sources` to see what's been ingested
