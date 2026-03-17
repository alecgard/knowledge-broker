# Knowledge Broker

[![Version](https://img.shields.io/github/v/tag/alecgard/knowledge-broker?label=version&sort=semver)](https://github.com/alecgard/knowledge-broker/releases)
[![License](https://img.shields.io/badge/License-BSL_1.1-blue)](LICENSE)

Your team's knowledge is scattered across repos, wikis, Confluence, and Slack. Traditional search finds documents. Knowledge Broker finds answers, tells you how much to trust them, and shows you where sources disagree.

AI agents query it over MCP, people use the CLI, and teams get a shared HTTP API. Hybrid search, structured confidence signals, and contradiction detection. Open-source and self-hosted - no data leaves your environment.

**[Docs](https://knowledgebroker.dev)** | **[Quick Start](https://knowledgebroker.dev/quickstart/)** | **[Architecture](https://knowledgebroker.dev/architecture/)**

## Install

```bash
curl -fsSL https://knowledgebroker.dev/install.sh | sh
```

Or build from source (requires Go 1.24+):

```bash
git clone https://github.com/alecgard/knowledge-broker.git
cd knowledge-broker
make install
```

Ollama is installed and configured automatically on first run.

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
kb query --raw "What database does the inventory service use?"

# Synthesised answer (requires an LLM provider — Claude by default)
export ANTHROPIC_API_KEY=sk-ant-...
kb query "What database does the inventory service use?"
```

```jsonc
{
  "answer": "The inventory service uses PostgreSQL (v16 on RDS, r6g.2xlarge).",
  "confidence": {
    "overall": 0.93,
    "breakdown": {
      "freshness": 0.94,
      "corroboration": 0.85,
      "consistency": 1.00,
      "authority": 0.95
    }
  },
  "sources": [
    { "source_type": "confluence", "source_name": "ENGINEERING", "source_path": "Internal Services" },
    { "source_type": "slack", "source_name": "engineering", "source_path": "#platform-engineering/2026-03-06" }
  ],
  "contradictions": []
}
```

## Serve

```bash
kb serve # HTTP API on :8080, MCP on :8082 (stdio + SSE)
```

See the **[full documentation](https://knowledgebroker.dev)** for connector setup, MCP integration, CLI reference, configuration, and the evaluation framework.

## License

[BSL 1.1](LICENSE), free to use and self-host. Converts to Apache 2.0 after 4 years.
