---
description: Deploy a shared Knowledge Broker instance for your org and connect from developer machines.
---

# Team Deployment

The typical setup: one KB instance runs on a shared server with your org's sources ingested. Developers and AI agents connect to it from their own machines via CLI, HTTP, or MCP.

## Server setup

### 1. Install

On the server:

```bash
curl -fsSL https://knowledgebroker.dev/install.sh | sh
```

### 2. Ingest your org's sources

```bash
kb ingest --git https://github.com/acme/platform --description "Platform services"
kb ingest --confluence ENGINEERING --description "Engineering wiki"
kb ingest --slack C0ABC123DEF --description "Platform engineering channel"
```

Set up a cron job or CI step to re-run ingestion periodically. Only new or changed files are processed.

### 3. Configure synthesis (optional)

Set an API key on the server for answer synthesis. Without one, raw retrieval still works.

```bash
# Claude (default)
export ANTHROPIC_API_KEY=sk-ant-...

# Or OpenAI
export KB_LLM_PROVIDER=openai
export OPENAI_API_KEY=sk-...
```

### 4. Start the server

```bash
kb serve
```

This starts three things in one process:

| Transport | Address | Purpose |
|-----------|---------|---------|
| HTTP API | `:8080` | REST queries, ingestion, source management |
| MCP SSE | `:8082` | Remote MCP clients |
| MCP stdio | — | Local MCP clients (subprocess) |

Customize ports with `--addr` and `--mcp-addr`. For a headless deployment with no stdio:

```bash
kb serve --no-stdio
```

## Connecting from developer machines

Developers don't need to run their own KB instance. They connect to the shared server.

### CLI with --remote

Install `kb` locally, then point any command at the server:

```bash
# Query
kb query --remote http://server:8080 "how does auth work?"
kb query --remote http://server:8080 --raw "retry logic"

# List sources
kb sources list --remote http://server:8080

# Push a local repo to the shared instance
kb ingest --source ./my-repo --remote http://server:8080

# Export
kb export --remote http://server:8080 --out ./export/
```

Every CLI command accepts `--remote`. When set, it talks to the server over HTTP instead of using a local database.

### MCP clients (Claude Code, Cursor, Windsurf)

There are two ways to connect MCP clients to a shared KB instance:

**Option A: Local subprocess (recommended)**

Each developer installs `kb` and configures it as a local MCP subprocess. The subprocess connects to the shared server internally.

Run `kb setup mcp` to configure automatically, or add to your MCP client config manually:

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

This gives the MCP client a local `kb` process communicating over stdio. The `kb` process uses its own local database. To share the org's knowledge base, each developer would need a local copy of the database, or use Option B.

**Option B: SSE (remote, no local install needed)**

Point MCP clients directly at the server's SSE endpoint. No local `kb` binary required.

For Claude Code, add to `.mcp.json`:

```json
{
  "mcpServers": {
    "knowledge-broker": {
      "type": "sse",
      "url": "http://server:8082/sse"
    }
  }
}
```

This is the simplest setup for teams -- one server, no local installs, every developer's agent queries the same knowledge base.

### HTTP API

Query the server directly from scripts, CI, or custom integrations:

```bash
# Synthesised answer
curl -s -X POST http://server:8080/v1/query \
  -H 'Content-Type: application/json' \
  -d '{"messages":[{"role":"user","content":"how does auth work?"}]}'

# Raw retrieval
curl -s -X POST http://server:8080/v1/query \
  -H 'Content-Type: application/json' \
  -d '{"messages":[{"role":"user","content":"how does auth work?"}],"mode":"raw"}'

# List sources
curl -s http://server:8080/v1/sources

# Health check
curl -s http://server:8080/v1/health
```

See [CLI Reference](cli.md#endpoints) for the full API.

## Keeping the knowledge base fresh

### Cron

Re-ingest all registered sources on a schedule:

```bash
# Every hour
0 * * * * /usr/local/bin/kb ingest --all
```

### CI/CD

Add ingestion to your deploy pipeline so the knowledge base updates whenever docs or code ship:

```bash
# In your CI job
kb ingest --source . --remote http://kb-server:8080 --description "My service"
```

### Developer contributions

Any developer can push their local repo to the shared instance:

```bash
kb ingest --source ./my-project --remote http://server:8080 --description "Payment service"
```

## Network and security

KB does not include authentication. For production deployments:

- Run behind a reverse proxy (nginx, Caddy) or VPN
- Use HTTPS termination at the proxy level
- Restrict access by IP or network
- The SSE endpoint at `:8082` should be similarly protected

## Architecture

```
                    ┌─────────────────────────────────┐
                    │         KB Server                │
                    │                                  │
                    │  kb serve                        │
                    │  ├── HTTP API    (:8080)         │
                    │  ├── MCP SSE    (:8082)         │
                    │  └── SQLite DB  (kb.db)         │
                    └──────┬──────────────┬───────────┘
                           │              │
              ┌────────────┴──┐    ┌──────┴───────────┐
              │  HTTP / CLI   │    │   MCP SSE        │
              │  --remote     │    │                   │
              ├───────────────┤    ├───────────────────┤
              │ kb query      │    │ Claude Code       │
              │ kb sources    │    │ Cursor            │
              │ kb ingest     │    │ Windsurf          │
              │ curl / scripts│    │ Any MCP client    │
              └───────────────┘    └───────────────────┘
```
