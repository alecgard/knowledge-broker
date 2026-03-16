---
description: Open-source CLI for team knowledge retrieval. Go + SQLite, hybrid search, MCP server, confidence signals. Zero infrastructure, self-hosted.
---

# Knowledge Broker

Your AI agents are guessing at things your org already knows, because the answer is buried across three repos, a Confluence page, and a Slack thread from February. Knowledge Broker searches all of them at once and gives back one answer with sources, confidence scores, and a heads-up when things contradict each other.

Run it for your whole org or just on your laptop — either way, agents query it over MCP or HTTP, developers use the CLI, and nobody needs to already know where to look.

## Why Knowledge Broker

Your team's knowledge is scattered across repos, wikis, Confluence, Slack, and local docs. The answer to any question usually exists somewhere, spread across three sources that partially contradict each other. Traditional search finds documents. Knowledge Broker finds answers, tells you how much to trust them, and shows you where sources disagree.

It runs on SQLite with local embedding models, no Postgres, no Elasticsearch, no cloud dependencies. One binary, one database file, everything managed automatically. The only external call is to Claude for answer synthesis, and even that's optional (raw mode does retrieval and confidence scoring entirely locally).

The MCP server gives AI agents structured access to the knowledge base with confidence scores they can branch on. When sources disagree, the contradiction is surfaced explicitly, not silently resolved.

## What it looks like

```jsonc
$ kb query "What database does the inventory service use?"
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
    { "source_type": "confluence", "source_name": "ACME", "source_path": "Internal Services" },
    { "source_type": "slack", "source_name": "acme", "source_path": "#platform-engineering/2026-03-06" }
  ],
  "contradictions": []
}
```

The answer is synthesised from Confluence docs and Slack history. Every response includes a confidence breakdown and source attribution.

## Who it's for

Engineering orgs that want a shared, queryable knowledge base across all their repos, docs, and chat history. Platform teams that want to give every developer (and every AI agent) access to the same organizational knowledge without each person setting up their own tooling.

## Get started

```bash
curl -fsSL https://knowledgebroker.dev/install.sh | sh
```

Install and run your first query in under 5 minutes: [Getting Started](quickstart.md)

## How it works

1. **[Connectors](connectors.md)** pull content from sources: local filesystem, Git, Confluence, Slack, GitHub Wiki
2. **Extractors** chunk files at semantic boundaries (headings for markdown, functions for code)
3. **Embeddings** convert chunks to vectors locally; raw text is indexed with FTS5 for keyword search
4. **Hybrid search** runs vector similarity and BM25 keyword search, merged via Reciprocal Rank Fusion
5. **[Confidence signals](architecture.md)** assess trust across four dimensions: freshness, corroboration, consistency, authority
6. **Synthesis** (optional) produces an answer via Claude, or returns ranked fragments directly in raw mode

Read the full [architecture](architecture.md) for details on the trust layer and query pipeline.

## License

[BSL 1.1](https://github.com/alecgard/knowledge-broker/blob/main/LICENSE), free to use and self-host. Converts to Apache 2.0 after 4 years.
