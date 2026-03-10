## 1. Raw Retrieval Mode (no LLM required)

Add a query mode that returns ranked fragments with confidence signals, skipping LLM synthesis entirely. Claude Code becomes the synthesis layer — KB just provides the retrieval and trust scoring.

### Tasks

- [ ] **Add `--raw` flag to `kb query`**
  Define a `RawResult` response type that includes the top-N fragments, their confidence signals, source metadata, and any detected contradictions — but no synthesised answer. Wire it through the existing query command.

- [ ] **Skip LLM synthesis when `--raw` is set**
  Short-circuit the query engine after vector search and confidence scoring. Return fragments directly without calling the Anthropic API. Ensure the command doesn't error when `ANTHROPIC_API_KEY` is unset and `--raw` is used.

- [ ] **Format raw output for terminal and JSON**
  Print fragments in a readable format for CLI use (source path, confidence scores, content preview). Support `--json` for structured output that tools can consume.

- [ ] **Expose raw mode in HTTP API**
  Add a query parameter or request field (e.g. `"mode": "raw"`) to `POST /v1/query` that returns fragments without synthesis. This is what the MCP server will use.

- [ ] **Validate confidence signals work without synthesis**
  Test that freshness, corroboration, consistency, and authority scores are computed and returned correctly in raw mode across a non-trivial test corpus.

---

## 2. Harden MCP Server

Make `kb mcp` the primary interface for team use via Claude Code. The MCP server needs to return fragments in a format that's immediately useful as LLM context.

### Tasks

- [ ] **Audit MCP tool schema for `query`**
  Review the current tool definition. Ensure the input schema accepts useful options: `limit`, `raw` (default true since MCP consumers do their own synthesis), and optional scope/filter parameters.

- [ ] **Structure MCP query response for LLM consumption**
  Return fragments with: content, file path, source URI, last modified date, confidence signals, and contradiction flags. Format so that an LLM receiving this as tool output can reason over it without further transformation.

- [ ] **Add raw mode as default for MCP**
  MCP consumers are LLMs — they do their own synthesis. Default to raw retrieval in the MCP server so it works without an Anthropic API key.

- [ ] **Test `feedback` tool via MCP**
  Verify that corrections, challenges, and confirmations submitted through the MCP interface are persisted and affect subsequent confidence scores. Test the round-trip: query → get fragment → submit correction → re-query → see updated confidence.

- [ ] **Add `list-sources` MCP tool**
  Expose a tool that lists ingested sources with their fragment counts, last sync time, and overall freshness. Useful for Claude Code users to understand what KB knows about before querying.

- [ ] **Document Claude Code integration**
  Write a short setup guide covering both modes: local `kb mcp` via stdio for solo use, and connecting to a shared server's HTTP API for team use. Include mcp.json config examples, available tools, and example usage patterns. For the shared server setup, document how `kb ingest --remote` fits into the developer workflow alongside the MCP query interface.

---

## 3. Evaluation Framework

Build a repeatable eval harness so that changes to chunking, clustering, embedding models, and prompts can be measured rather than eyeballed. Run evals before and after any tuning change.

### Tasks

- [ ] **Define eval corpus**
  Assemble a fixed test corpus — a repo (or subset) with known content that won't change between eval runs. Check it into the KB repo or reference a pinned commit. This is the ground truth that all evals run against.

- [ ] **Build a question-answer test set**
  Write 20-30 question/expected-answer pairs against the eval corpus. Cover a range: simple factual lookups, cross-file questions, questions where sources contradict, questions with no good answer. Store as a JSON or YAML file in the repo. Each entry should include the question, expected relevant source files, and a human-written reference answer.

- [ ] **Implement `kb eval` command**
  Add a command that runs the test set against a KB database, collects results, and outputs a summary. For each question: which fragments were retrieved, whether the expected sources appeared in the top-N, and confidence signal values.

- [ ] **Retrieval metrics**
  Compute and report: recall@k (did the expected sources appear in the top-k fragments), precision@k (what fraction of returned fragments were relevant), and MRR (mean reciprocal rank of the first relevant fragment). These measure retrieval quality independently of synthesis.

- [ ] **Confidence signal calibration**
  For each test case, record the four confidence scores. Over the full test set, check whether high-confidence answers are actually correct more often than low-confidence ones. Flag cases where confidence is high but the answer is wrong (overconfident) or low but correct (underconfident).

- [ ] **Chunking quality metrics**
  Measure fragment statistics across the eval corpus: count, mean/median/p95 token length, fragments per file. Track these over time — if a chunking change produces 3x more fragments or cuts average length in half, that's visible immediately.

- [ ] **LLM-as-judge for synthesis quality (optional, requires API key)**
  For runs where synthesis is available, use a separate LLM call to score the synthesised answer against the reference answer on relevance, accuracy, and completeness. This is a nice-to-have — retrieval metrics are the priority since raw mode is the primary path.

- [ ] **CI integration**
  Add eval run to CI (or a Makefile target). Evals don't need to run on every commit, but `make eval` should be a one-command operation that ingests the eval corpus, runs the test set, and prints a summary table.

---

## 4. Ingest Team Repos

Get KB running against real Chainlink repos to validate ingestion, chunking, and retrieval quality at realistic scale. Use `--remote` to push to a shared server and the eval framework from section 3 to measure retrieval quality as you tune.

### Tasks

- [ ] **Test local ingestion against a real repo**
  Ingest a substantial repo (CRE Data Feeds or equivalent) locally first. Measure: ingestion time, fragment count, embedding time, database size. Identify any failures or edge cases.

- [ ] **Test remote ingestion against the same repo**
  Push the same repo via `kb ingest --source ./repo --remote http://server:8080`. Verify fragment counts match local ingestion, embedding happens server-side, and checksums are tracked locally for incremental re-ingestion.

- [ ] **Validate Go code chunking quality**
  Review how the code extractor chunks `.go` files. Check that functions, types, and package-level declarations are split at sensible boundaries. Verify that chunk sizes are reasonable for embedding — not too fine (single lines) or too coarse (entire files).

- [ ] **Validate markdown chunking quality**
  Review chunking of READMEs, ADRs, runbooks. Ensure heading-based splitting preserves enough context per fragment. Check that code blocks within markdown aren't split mid-block.

- [ ] **Test incremental re-ingestion**
  Run ingestion twice on the same repo. Verify that unchanged files are skipped (checksum-based), new/modified files are processed, and deleted files are handled (or flagged as a known limitation).

- [ ] **Benchmark query relevance**
  Write 10-15 realistic questions a team member might ask about the codebase. Run them through raw retrieval and score with `kb eval`. Assess whether the returned fragments are relevant and whether the confidence signals are directionally correct.

- [ ] **Test at multi-repo scale**
  Ingest 2-3 repos into the same database. Verify that cross-repo queries work and that corroboration signals correctly identify when multiple repos reference the same concept.

---

## 5. Harden Shared Instance Deployment

Remote ingestion already works — `kb ingest --remote` pushes extracted fragments to a server which handles embedding and storage. The team deployment model is: one shared KB server running `kb serve` with Ollama, developers push from their local checkouts via `--remote`, and Claude Code connects to the server's HTTP API. The tasks here are about making that robust for real team use.

### Tasks

- [ ] **Test concurrent pushes from multiple developers**
  Have two or more `kb ingest --remote` processes push to the same server simultaneously. Verify that the server handles concurrent embedding and storage correctly — no lost fragments, no SQLite lock contention causing failures, no corrupt state.

- [ ] **Handle same repo pushed by different developers**
  Two developers ingesting the same repo will have slightly different local state (different branches, uncommitted changes, different git blame). Define the expected behaviour: last-write-wins per file path? Deduplicate by checksum? Test and document what actually happens.

- [ ] **Test checksum tracking consistency across machines**
  The client tracks checksums locally in its own SQLite DB for incremental behaviour. Verify this works correctly when the same developer pushes from different machines, or when a developer re-clones a repo. Identify any scenarios where local tracking drifts from server state.

- [ ] **Add `--remote` support documentation**
  Document the team setup: how to run the server, what env vars to set, how to configure `--remote` as a default (e.g. shell alias or env var), and how to wire Claude Code's MCP config to point at the shared server's HTTP API rather than a local `kb mcp` process.
