---
description: Set up the Knowledge Broker MCP server for Claude Code, Codex, Cursor, Windsurf, and other MCP-compatible AI agents. Stdio and SSE transports for local and team use.
---

# MCP Server

Knowledge Broker includes an [MCP](https://modelcontextprotocol.io) server that any MCP-compatible client can use to query and explore the knowledge base. `kb serve` runs the HTTP API, MCP stdio, and MCP SSE transports in a single process.

## Setup

The typical deployment: one KB instance runs on a shared machine with your org's sources already ingested. Developers and agents connect to it via MCP or HTTP.

### Connecting MCP clients

Each developer adds KB to their MCP client config (Claude Code, Cursor, Windsurf, etc.):

```json
{
  "mcpServers": {
    "knowledge-broker": {
      "command": "/path/to/kb",
      "args": ["serve", "--no-http", "--no-sse"]
    }
  }
}
```

If `kb` is on your PATH, you can use `"command": "kb"` directly. This launches KB as a subprocess via stdio.

### SSE (remote access)

`kb serve` also starts an SSE transport on `:8082` by default, so remote clients can connect without running the binary locally:

```bash
kb serve                        # HTTP on :8080, MCP SSE on :8082
kb serve --mcp-addr :9090       # custom MCP SSE port
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

**Synthesis mode (default):** Returns a synthesised answer with confidence signals and source citations. Requires an LLM provider (`ANTHROPIC_API_KEY` for Claude, or configure `KB_LLM_PROVIDER`).

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

## Agent prompts

MCP clients discover KB's tools automatically, but agents won't reach for them unless they know the knowledge base exists. Adding a short prompt to your project config makes the difference between agents that guess and agents that check.

### Claude Code

Add to your project's `CLAUDE.md`:

```markdown
## Knowledge base

This project is indexed in Knowledge Broker, a shared knowledge base that
spans our repos, docs, and internal sources. It's available via MCP as the
"knowledge-broker" server (tools: `query` and `list-sources`).

Use Knowledge Broker:
- Before asking me for context about the codebase, architecture, or how things work
- When you encounter unfamiliar patterns, services, or conventions
- When you need to understand why something was built a certain way
- Instead of grepping across repos for answers that span multiple files

Start with `list-sources` to see what's indexed. Use `query` for answers.
Check the confidence score — if it's below 0.5, tell me you're uncertain
rather than treating the answer as fact. If sources contradict each other,
surface both claims with their dates.
```

### Codex

Add to your project's `AGENTS.md` (also read by Jules, Aider, and other agents that support the format):

```markdown
## Knowledge base

This project is indexed in Knowledge Broker, a shared knowledge base
available via MCP (server: "knowledge-broker", tools: "query" and
"list-sources"). Before making assumptions about the codebase,
architecture, or project conventions, use Knowledge Broker's query
tool. Start with list-sources to see what's indexed.

Check the confidence score in the response — if it's below 0.5, flag
the uncertainty. If sources contradict, surface both claims with dates.
```

### Cursor

Add to `.cursor/rules`:

```
This project is indexed in Knowledge Broker, a shared knowledge base
available via MCP (server: "knowledge-broker", tools: "query" and
"list-sources"). Before making assumptions about the codebase,
architecture, or project conventions, use Knowledge Broker's query
tool. Start with list-sources to see what's indexed. Check the
confidence score in the response — if it's below 0.5, flag the
uncertainty. If sources contradict, surface both claims with dates.
```

### Windsurf

Add to `.windsurfrules`:

```
This project is indexed in Knowledge Broker, a shared knowledge base
available via MCP (server: "knowledge-broker", tools: "query" and
"list-sources"). Before making assumptions about the codebase,
architecture, or project conventions, use Knowledge Broker's query
tool. Start with list-sources to see what's indexed. Check the
confidence score in the response — if it's below 0.5, flag the
uncertainty. If sources contradict, surface both claims with dates.
```

### Generic (any MCP client)

The core instruction is the same everywhere. Adapt to your client's prompt format:

```
This project is indexed in Knowledge Broker, a shared knowledge base
available via MCP (server: "knowledge-broker"). Use the "query" tool
to search for answers about the codebase, architecture, and project
conventions before making assumptions. Use "list-sources" to discover
what's indexed. Pay attention to confidence scores in the response —
flag anything below 0.5 as uncertain. When sources contradict each
other, surface both claims with their dates so the user can judge
which is current.
```

## Environment variables

| Variable | Required | Description |
|----------|----------|-------------|
| `ANTHROPIC_API_KEY` | For synthesis with Claude (default) | Not needed when `raw=true` or using another provider |
| `KB_OLLAMA_URL` | No (default `http://localhost:11434`) | Embedding server URL |
| `KB_EMBEDDING_MODEL` | No (default `nomic-embed-text`) | Embedding model |
| `KB_DB` | No (default `kb.db`) | Database path |

## Typical setup

1. **Deploy KB** on a shared machine. Ingest your org's sources: `kb ingest --confluence ENGINEERING --git https://github.com/org/repo --slack C0ABC123DEF`
2. **Start the server**: `kb serve`
3. **Each developer** adds KB to their MCP client config (see above)
4. Agents call `query` for answers with confidence signals, or `list-sources` to discover what's available
5. The `kb-instructions` prompt bootstraps agent context automatically, no manual prompt engineering needed
