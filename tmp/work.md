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
