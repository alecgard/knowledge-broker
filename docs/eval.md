---
description: Measure retrieval quality with Knowledge Broker's evaluation framework. Recall, precision, MRR metrics across test categories.
---

# Evaluation Framework

A repeatable eval harness for measuring retrieval quality. Run evals before and after changes to chunking, embedding models, or prompts.

## Quick start

```bash
make eval
```

This ingests the eval corpus, runs the test set, and prints a summary table. Requires Ollama running locally — no Anthropic API key needed.

## Manual usage

```bash
# Ingest the eval corpus into a fresh database
kb ingest --source eval/corpus --db eval.db

# Run evaluation
kb eval --db eval.db

# Run with custom options
kb eval --db eval.db --testset eval/testset.json --limit 10 --json

# Ingest and eval in one step
kb eval --db eval.db --corpus eval/corpus --ingest
```

### Flags

| Flag | Default | Description |
|------|---------|-------------|
| `--db` | `kb.db` | Database path |
| `--testset` | `eval/testset.json` | Path to query/answer test set |
| `--corpus` | `eval/corpus` | Path to eval corpus directory |
| `--limit` | `20` | K value for retrieval (top-K fragments) |
| `--ingest` | false | Ingest the corpus before running eval |
| `--json` | false | Output structured JSON instead of a table |

## Metrics

### Retrieval metrics (reported at K=5, K=10, K=20)

- **Hit@K** — Was at least one expected source file found in the top-K results?
- **Recall@K** — Did the expected source files appear in the top-K retrieved fragments?
- **Precision@K** — What fraction of top-K fragments came from expected source files?
- **MRR** — Mean reciprocal rank of the first relevant fragment, averaged across queries.
- **Avg confidence** — Mean confidence score across retrieved fragments.
- **Avg freshness** — Mean freshness score across retrieved fragments.

### Chunking stats

- Total fragment count
- Fragments per file
- Mean, median, and P95 token length (whitespace-approximated)

These track chunking quality over time — a change that produces 3x more fragments or halves average length is immediately visible.

## Eval corpus

Located in `eval/corpus/`. A set of fictional files about an "Acme Widget Service" designed to exercise different retrieval scenarios:

| File | Purpose |
|------|---------|
| `README.md` | Project overview, features, quick start |
| `config.go` | Go config structs and validation |
| `api.go` | HTTP API handlers |
| `architecture.md` | System design (intentionally contradicts README on some details) |
| `runbook.md` | Operational procedures and troubleshooting |
| `CHANGELOG.md` | Version history and release notes |
| `CONTRIBUTING.md` | Contribution guidelines |
| `deploy/kubernetes.yaml` | Kubernetes deployment manifests |
| `design-review.md` | Design review discussion |
| `incident-review.md` | Incident post-mortem |

The corpus is checked into the repo and should not change between eval runs unless you're intentionally updating it.

## Test set

Located in `eval/testset.json`. Each entry has:

```json
{
  "id": "q01",
  "query": "What database does the widget service use?",
  "expected_sources": ["config.go", "architecture.md"],
  "reference_answer": "PostgreSQL, configured via DATABASE_URL.",
  "category": "direct_extraction"
}
```

Categories:
- **direct_extraction** — single-source factual lookup
- **cross_document** — requires information from multiple files
- **knowledge_update** — tests whether newer information is preferred
- **abstention** — no good answer exists in the corpus
- **pronoun_resolution** — requires resolving references across context
- **vocabulary_mismatch** — query uses different terms than the corpus

## Extending the eval

**Adding queries:** Edit `eval/testset.json`. Include `expected_sources` (filenames that should appear in results) and a `reference_answer` for human comparison.

**Adding corpus files:** Add files to `eval/corpus/` and write questions that reference them. Re-run `make eval` to see the impact.

**Comparing configurations:** Run eval with different embedding models or chunk sizes by changing `KB_EMBEDDING_MODEL` or `KB_MAX_CHUNK_SIZE`, then compare the summary tables.
