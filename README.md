# Knowledge Broker

[![Go](https://img.shields.io/badge/Go-1.24+-00ADD8?logo=go&logoColor=white)](https://go.dev)
[![License](https://img.shields.io/badge/License-BSL_1.1-blue)](LICENSE)

Your team's knowledge is scattered across repos, wikis, Confluence, and Slack. Traditional search finds documents. Knowledge Broker finds answers, tells you how much to trust them, and shows you where sources disagree.

**Open-source RAG with a trust layer.** Hybrid search, structured confidence signals, contradiction detection, and an MCP server for AI agents. Built in Go with SQLite. Self-hosted. No data leaves your environment.

**[Docs](https://knowledgebroker.dev)** | **[Quick Start](https://knowledgebroker.dev/quickstart/)** | **[Architecture](https://knowledgebroker.dev/architecture/)**

## Install

**Prerequisites:** Go 1.24+, [Ollama](https://ollama.com) running locally

```bash
git clone https://github.com/alecgard/knowledge-broker.git
cd knowledge-broker
make install
ollama pull nomic-embed-text
```

## Ingest

```bash
kb ingest --source ./my-repo --description "Backend API"
kb ingest --git https://github.com/acme/platform --description "Platform services"
kb ingest --confluence ENGINEERING --description "Engineering wiki"
kb ingest --slack C0ABC123DEF --description "Platform engineering channel"
```

## Query

```bash
# Raw retrieval — no API key needed
kb query --raw "how does retry logic work?"

# Synthesised answer (requires ANTHROPIC_API_KEY)
kb query "how does retry logic work?"
```

```jsonc
{
  "answer": "The inventory service runs on port 8081 and uses PostgreSQL ...",
  "confidence": {
    "overall": 0.93,
    "breakdown": {
      "freshness": 0.94,
      "corroboration": 0.85,
      "consistency": 1.00,
      "authority": 0.95
    }
  },
  "sources": [ ... ],
  "contradictions": []
}
```

## Serve

```bash
kb serve                  # HTTP API on :8080
kb mcp                    # MCP server (stdio + SSE on :8082)
```

See the **[full documentation](https://knowledgebroker.dev)** for connector setup, MCP integration, CLI reference, configuration, and the evaluation framework.

## License

[BSL 1.1](LICENSE), free to use and self-host. Converts to Apache 2.0 after 4 years.
