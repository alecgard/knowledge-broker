# Knowledge Broker

## Build & Test

```bash
make build    # Build the kb binary
make test     # Run all tests
make test-v   # Run all tests (verbose)
make lint     # go vet
```

CGO flag required on macOS: `CGO_CFLAGS="-Wno-deprecated-declarations"`

## Rules

- **Always run `make test` before pushing.** A pre-push git hook enforces this automatically.
- Keep tests passing at all times. Do not push with failing tests.
- When modifying MCP, HTTP, or CLI code, run the relevant server tests:
  - `go test ./internal/server/ -count=1 -v` for HTTP and MCP
  - `go test ./cmd/kb/ -count=1 -v` for CLI

## Eval-First Development

Before starting any work that could affect retrieval quality, ranking, ingestion, chunking, embedding, query expansion, or synthesis:

1. **Check whether new evals are needed.** Review `eval/testset.json` and `eval/corpus/` — do existing test cases cover the behavior you're about to change? If the change introduces a new capability or is expected to improve a specific class of query, there must be eval cases that measure that improvement.
2. **Add new evals first.** Write new test cases (and corpus files if needed) that target the expected improvement. Run `make eval` to capture baseline scores. Commit the new evals before starting feature work. This ensures you have a clean before/after comparison.
3. **Run `make eval` after the feature work.** Verify that the new eval cases improve and that existing cases don't regress.

Evals run automatically in CI when the `eval` label is added to a pull request (see `.github/workflows/eval.yml`). The workflow runs `make eval`, commits updated results back to the PR branch, and posts a summary comment with the metrics table.

Eval test cases live in `eval/testset.json`. Corpus files live in `eval/corpus/`. Categories: `direct_extraction`, `cross_document`, `knowledge_update`, `abstention`, `pronoun_resolution`, `vocabulary_mismatch`.

When finishing a task that could affect eval scores, include a before/after summary table showing how key metrics changed (Hit@1, Hit@3, Hit@5, MRR) at both the overall and per-category level. Highlight regressions.
