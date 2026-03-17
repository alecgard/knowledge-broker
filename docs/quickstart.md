---
description: Install Knowledge Broker and run your first query in under 5 minutes.
---

# Getting Started

Install KB and run your first query locally. For shared team setups, see [Team Deployment](deployment.md).

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

## Ingest

Point KB at your sources. Descriptions help agents understand what each source contains:

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

For synthesised answers with cross-fragment confidence assessment and contradiction detection. Configure an API key for your preferred provider:

```bash
# Save to your persistent config (recommended — survives new shells)
mkdir -p ~/.config/kb
echo 'ANTHROPIC_API_KEY=sk-ant-...' >> ~/.config/kb/config

# Or export for the current session
export ANTHROPIC_API_KEY=sk-ant-...
```

Other providers work too:

```bash
# OpenAI
KB_LLM_PROVIDER=openai
OPENAI_API_KEY=sk-...

# Local model via Ollama (no API key needed)
KB_LLM_PROVIDER=ollama
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

## Tell your agents about KB

If you use an AI coding agent (Claude Code, Cursor, etc.), add a prompt to your project config telling it when and how to use KB. Without this, agents won't know the knowledge base exists.

We provide ready-made prompt templates you can drop into your `CLAUDE.md`, `.cursorrules`, or equivalent — see [Agent prompts](mcp.md#agent-prompts).

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

Run `kb config` at any time to see where your settings are coming from. See [CLI Reference — Configuration](cli.md#configuration) for the full search path.

## Next steps

- [Deploy for your team](deployment.md) — shared server, HTTP API, remote MCP
- [MCP Server](mcp.md) — connect AI agents to your local or shared KB instance
- [Connect more sources](connectors.md) — Confluence, Slack, GitHub Wiki
- [Understand the trust layer](architecture.md) — how confidence signals work
- [CLI Reference](cli.md) — all commands and flags
