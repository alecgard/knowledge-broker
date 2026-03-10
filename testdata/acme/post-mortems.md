# ACME Org -- Incident Post-Mortems

This document contains blameless post-mortem reports for past incidents at ACME Org. All post-mortems follow the standard ACME template established by the SRE team. Post-mortems are required for all SEV-1 and SEV-2 incidents, and must be completed within 48 hours of incident resolution. Action items are tracked in Jira under the `INCIDENT` project with SLAs: SEV-1 action items due within 1 week, SEV-2 action items due within 2 weeks.

**Post-mortem principles (from ACME Engineering Handbook):**
- Blameless: We focus on systemic causes, not individual fault.
- Thorough: We investigate until we understand the full causal chain, not just the proximate cause.
- Actionable: Every post-mortem produces concrete action items with owners and deadlines.
- Transparent: Post-mortems are shared with all of Engineering and relevant stakeholders.

**Incident severity definitions:**
- SEV-1 (Critical): Complete system outage or data loss. All hands. CEO notified. Target resolution: 1 hour.
- SEV-2 (Major): Significant degradation affecting >50% of customers. IC + relevant team. VP Eng notified. Target resolution: 4 hours.

---

## INC-2025-042: GlobalMart Inventory Sync Outage

**Severity:** SEV-1
**Duration:** 2 hours 14 minutes
**Date:** July 17, 2025
**Incident Commander:** Tom Bradley (SRE Lead)
**Technical Lead:** James Okafor (Relay Team Lead)
**Comms Lead:** Jennifer Walsh (GlobalMart CSM)

### Summary

The Relay agent serving GlobalMart (ACME's largest customer, $450K ACV) experienced an out-of-memory crash while processing a large batch of IDocs from GlobalMart's SAP S/4HANA system. The crash caused a complete data sync interruption for 2 hours and 14 minutes, during which GlobalMart's inventory positions in Nexus became stale. Approximately 47,000 inventory update events were lost during the outage window and had to be re-synced. GlobalMart's supply chain operations team was unable to rely on Nexus for real-time inventory visibility during this period and reverted to manual SAP checks.

### Timeline

All times are in Pacific Time (PT).

| Time | Event |
|------|-------|
| 06:12 | GlobalMart's SAP system initiates a bulk IDoc export as part of their weekly full inventory reconciliation. The batch contains 128,000 IDocs totaling approximately 85MB. |
| 06:14 | Relay agent begins processing the IDoc batch. Memory usage climbs from baseline 1.1GB to 1.6GB within 2 minutes. |
| 06:17 | Memory usage reaches 1.9GB (agent memory limit is 2048MB). The SAP RFC connector's IDoc parser loads the entire batch into memory for schema validation before chunking. |
| 06:18 | Relay agent is OOM-killed by the Kubernetes kubelet. Pod status changes to `OOMKilled`. The automatic restart begins. |
| 06:18 | Relay control plane detects the agent disconnection. Alert fires in `#relay-alerts` Slack channel. PagerDuty pages the Relay team primary on-call (Dev Kapoor). |
| 06:22 | Dev Kapoor acknowledges the page and begins investigation. Checks Relay control plane dashboard and identifies GlobalMart agent as offline. |
| 06:25 | Relay agent pod restarts successfully. Memory baseline returns to 1.1GB. However, the SAP system immediately re-sends the same 85MB IDoc batch (SAP retries unacknowledged batches). |
| 06:26 | Second OOM kill. Pod enters `CrashLoopBackOff` as SAP continues retrying the batch on each restart. |
| 06:30 | Dev Kapoor identifies the crash-loop pattern. Checks agent logs and sees the OOM kill correlated with the large IDoc batch. |
| 06:35 | Dev Kapoor pages James Okafor (Relay Team Lead) for assistance. This is a known issue (SAP RFC connector memory leak with IDocs > 50MB, documented in Known Issues). |
| 06:40 | James Okafor joins. They decide to temporarily block the SAP retry to break the crash loop. They update the agent config to set `max_idoc_batch_size_mb: 40` which causes the agent to reject batches over 40MB and request SAP to send smaller batches. |
| 06:45 | Config update pushed to the agent via Relay control plane. Agent restarts. SAP sends the 85MB batch, agent rejects it with a chunking request. SAP does not support dynamic batch splitting. Agent remains online but no inventory data flows. |
| 06:50 | Team realizes the SAP-side batch splitting approach will not work without GlobalMart SAP admin involvement. They escalate to Jennifer Walsh (CSM) to contact GlobalMart's IT team. |
| 07:00 | Jennifer Walsh reaches GlobalMart's IT on-call. Explains the situation. GlobalMart IT says they cannot modify the IDoc batch configuration without a change request, which requires manager approval. |
| 07:15 | James Okafor proposes an alternative: temporarily increase the Relay agent memory limit to 8GB to accommodate the large batch, while also implementing an in-memory streaming parser as a hotfix. |
| 07:20 | Memory limit increase applied: `kubectl patch deployment relay-agent-globalmart -n relay --type='json' -p='[{"op":"replace","path":"/spec/template/spec/containers/0/resources/limits/memory","value":"8Gi"}]'` |
| 07:25 | Agent restarts with 8GB limit. Picks up the IDoc batch. Memory peaks at 3.2GB but does not OOM. Batch processes successfully in 4 minutes. |
| 07:30 | Normal data flow resumes. Relay agent begins catching up on the 2+ hour backlog of incremental updates. |
| 07:45 | Backlog fully processed. Inventory positions in Nexus are current. |
| 08:00 | Team runs inventory reconciliation job to verify data integrity: `POST /admin/inventory/reconcile?customer=globalmart`. Reconciliation finds 312 discrepancies (0.02% of total positions), all caused by events during the crash-loop window. |
| 08:15 | Reconciliation auto-corrects the 312 discrepancies by re-pulling current state from SAP. |
| 08:26 | Incident resolved. All GlobalMart inventory data is current and accurate. |

### Root Cause

The SAP RFC connector in the Relay agent loads entire IDoc batches into memory for XML schema validation before chunking and processing. The processing pipeline works as follows:
1. Receive IDoc batch from SAP via RFC connection.
2. Parse the entire batch into an in-memory DOM tree for schema validation.
3. Validate each IDoc against the ACME canonical schema.
4. Chunk validated IDocs into individual messages.
5. Publish messages to Kafka topic `inventory-updates`.

When GlobalMart's SAP system sent an 85MB IDoc batch (weekly full reconciliation), step 2 attempted to allocate approximately 3x the batch size in memory: 85MB for the raw XML, ~120MB for the DOM tree, and ~50MB for validation structures and intermediate buffers. The total ~255MB allocation, combined with the service's baseline memory usage (~1.1GB for the Go runtime, HTTP server, Kafka producer, and other connector state), exceeded the agent's 2GB memory limit.

This is a known issue documented in the product Known Issues section since March 2025 (RELAY-892). The fix (streaming SAX parser that validates and chunks incrementally without loading the full batch) was scoped for Relay 3.2 but deprioritized in the Relay 3.1 release in favor of the NetSuite SuiteTalk rate limiting improvements (requested by 8 customers). The workaround (splitting large batches on the SAP side) was documented in the Relay integration guide but required customer-side SAP configuration changes that GlobalMart's IT team had not implemented because it required a SAP change request.

Contributing factors:
1. GlobalMart's weekly reconciliation batch grew from ~50MB to ~85MB over the past 3 months as they added 2 new distribution centers (Dallas and Memphis) and onboarded 15,000 new SKUs. This growth was not monitored -- the Relay control plane dashboard does not track batch sizes over time.
2. The Relay agent memory limit of 2GB was set as a default during initial deployment (August 2024) and never tuned for GlobalMart's specific workload. GlobalMart is the only customer sending IDoc batches > 50MB.
3. The crash-loop pattern (SAP retries the same oversized batch on each restart) was not anticipated in the runbook. The SAP RFC protocol sends unacknowledged batches on reconnection, which means a batch that causes a crash will be re-sent on every restart, preventing recovery.
4. The known issue (RELAY-892) was filed in March 2025 but had been on the backlog for 4 months without a clear timeline. No severity escalation process existed for known issues that could cause customer-facing incidents.

### Impact

- **Customer impact:** GlobalMart's supply chain operations team lost real-time inventory visibility for 2 hours 14 minutes during their morning peak (06:18 - 08:26 PT, corresponding to 09:18 - 11:26 ET for their East Coast operations center). GlobalMart has 2,500 stores and relies on Nexus for real-time inventory positions to drive replenishment decisions. During the outage, their operations team reverted to direct SAP transaction SE16N queries, which provide data but without the aggregation and alerting that Nexus provides.
- **Data impact:** 47,000 incremental inventory update events were not processed during the outage. 312 inventory position discrepancies (0.02%) were identified and corrected via the automated reconciliation job. No data was permanently lost -- the reconciliation pulled current state from SAP to overwrite the stale positions.
- **Replenishment impact:** 14 automated purchase order recommendations were generated during the outage window based on stale inventory data. The GlobalMart operations team caught and held these POs before submission to suppliers. No incorrect POs were placed.
- **Business impact:** GlobalMart's VP of Supply Chain (Sandra Mitchell) called Robert Kim (ACME VP of Customer Success) directly. A make-good meeting was scheduled for July 22 with ACME CEO Alex Rivera, VP CS Robert Kim, and Relay Team Lead James Okafor. No SLA credit was formally requested, but GlobalMart referenced the incident during their annual contract renewal discussion in September 2025. This incident was a key motivator for accelerating the streaming IDoc parser fix.

### Resolution

1. Immediate: Increased Relay agent memory limit to 8GB for GlobalMart to accommodate the large IDoc batch.
2. Short-term: Reconciliation job corrected all 312 data discrepancies by re-pulling current state from SAP.
3. Long-term: Streaming IDoc parser implemented in Relay 3.1.2 (completed August 15, 2025), which processes IDocs incrementally without loading the full batch into memory. Memory usage for GlobalMart's 85MB batch dropped from ~255MB peak to ~15MB sustained.

### Action Items

| ID | Action | Owner | Deadline | Status |
|----|--------|-------|----------|--------|
| INC-042-01 | Implement streaming IDoc parser to avoid loading entire batch into memory | James Okafor | August 15, 2025 | Completed (Relay 3.1.2) |
| INC-042-02 | Add per-customer Relay agent memory monitoring and alerting (alert when usage > 70% of limit) | Dev Kapoor | August 1, 2025 | Completed |
| INC-042-03 | Review and right-size memory limits for all Enterprise customer Relay agents | James Okafor | August 8, 2025 | Completed |
| INC-042-04 | Add crash-loop detection for Relay agents with automatic batch-rejection circuit breaker | Relay Team | September 30, 2025 | Completed (Relay 3.2) |
| INC-042-05 | Work with GlobalMart to schedule SAP IDoc batch splitting as preventive measure | Jennifer Walsh | August 30, 2025 | Completed |
| INC-042-06 | Add IDoc batch size monitoring to Relay control plane dashboard | Dev Kapoor | August 15, 2025 | Completed |

### Lessons Learned

1. **Known issues need timely fixes, not just workarounds.** The IDoc memory issue (RELAY-892) was documented for 4 months before it caused a SEV-1. Known issues affecting Enterprise customers should be prioritized even if the workaround exists, because the workaround requires customer action that may not happen. Going forward, all known issues tagged `enterprise-risk` will be reviewed monthly by the VP of Engineering and must have a fix timeline within 2 sprints.
2. **Default resource limits need per-customer tuning.** Enterprise customers with large data volumes need customized resource allocation. The default 2GB memory limit was appropriate for 95% of customers but insufficient for GlobalMart's workload. We should make resource profiling part of the implementation process, and the Implementation Team should document expected data volumes during onboarding to inform resource allocation.
3. **Retry storms can turn a recoverable crash into an extended outage.** The SAP retry behavior created a crash loop that was harder to resolve than the original OOM. The agent OOM'd, restarted, received the same large batch, and OOM'd again -- a positive feedback loop that prevented recovery. Relay agents should have circuit breakers that detect oversized payloads and reject them with a backpressure signal rather than attempting to process and crashing.
4. **Incident response for single-customer SEV-1 needs a communication playbook.** The 20-minute delay between identifying the issue and reaching GlobalMart's IT team highlights the need for pre-established emergency communication channels for Enterprise customers. The post-incident action to create an emergency contact directory for all Enterprise customers was completed in August 2025.

---

## INC-2025-067: Forecast Engine Memory Leak

**Severity:** SEV-2
**Duration:** Recurring over 3 weeks (September 3-24, 2025), with cumulative customer-facing impact of approximately 18 hours of degraded forecast performance.
**Date:** September 3-24, 2025
**Incident Commander:** Tom Bradley (SRE Lead)
**Technical Lead:** Priya Sharma (Nexus Core Team Lead)
**Comms Lead:** Michael Torres (Nexus PM)

### Summary

The forecast engine (Oracle), a Python service using pandas for data manipulation, developed a memory leak after the v3.1.0 release on September 2, 2025. The leak caused gradual memory growth over 6-8 hours of operation, leading to increased P99 latency as the system entered GC pressure, followed by eventual OOM kills. The issue was initially misdiagnosed as a traffic spike, then as an infrastructure issue, before the root cause was identified as a pandas DataFrame copy-on-write bug in the new demand signal correlation feature. Over the 3-week period, the forecast engine pods were OOM-killed a total of 37 times, causing intermittent forecast generation failures and P99 latency spikes up to 12 seconds (baseline: 800ms).

### Timeline

All times are in UTC.

**Week 1 (September 3-9):**

| Time | Event |
|------|-------|
| Sep 2, 14:00 | Forecast engine v3.1.0 deployed to production. Release includes new demand signal correlation feature (correlates external market signals with historical demand for improved accuracy). Deployment via ArgoCD, green across all health checks. |
| Sep 2, 14:30 | Post-deployment smoke tests pass. Forecast accuracy spot checks for 5 reference customers all within expected MAPE range. No anomalies detected. |
| Sep 3, 08:15 | First OOM kill observed on forecast engine pod `forecast-engine-7b4f8d-xk2mn`. PagerDuty alert fires. SRE on-call (Lisa Park) acknowledges. |
| Sep 3, 08:20 | Lisa checks pod status. Pod has restarted automatically. Memory usage back to baseline (600MB). No customer-visible impact as other pods handled requests during the restart. Lisa marks alert as resolved. |
| Sep 3, 16:30 | Second OOM kill on a different pod (`forecast-engine-7b4f8d-r9vlp`). Same pattern: memory grew steadily from 600MB to 4GB limit over 8 hours. Lisa notes the pattern but attributes it to increased forecast request volume from a new batch of customer onboardings (12 new mid-market customers activated that week). |
| Sep 4, 11:00 | Third OOM kill. Lisa discusses with SRE team in standup. Team agrees this is unusual but not urgent -- the pod restarts quickly and other pods absorb the load. |
| Sep 5, 09:00 | Third and fourth OOM kills on the same morning (different pods). Lisa opens a JIRA ticket (NEX-4521) and assigns it to the Intelligence Squad. Priority P3. |
| Sep 5, 14:00 | Intelligence Squad picks up NEX-4521 in sprint planning. Initial hypothesis: large customer forecast requests from GlobalMart (12M shipments/year, largest forecast workloads) are consuming excessive memory. They add request-level memory tracking using Python's `resource` module. |
| Sep 6-8 | OOM kills continue at a rate of 2-3 per day. Request-level memory tracking shows no single request consuming more than 150MB, which is within expected bounds. The per-request analysis does not reveal the leak because the memory is not freed between requests, so each request appears normal. |
| Sep 9, 10:00 | Intelligence Squad reviews the request-level data. No correlation with specific customers or SKU counts. They pivot to investigating infrastructure causes (container runtime, kernel memory accounting). This is a dead end but consumes 2 days.

**Week 2 (September 10-16):**

| Time | Event |
|------|-------|
| Sep 10, 07:00 | After weekend (pods restarted during Saturday maintenance), all pods are at baseline memory (~600MB). Memory begins climbing immediately as forecast requests come in during Monday morning traffic. |
| Sep 10, 09:00 | Memory on oldest pod already at 1.8GB (3 hours of operation). Intelligence Squad is focused on a sprint deliverable and not actively monitoring. |
| Sep 10, 14:00 | Two pods OOM-killed simultaneously (both had been running since 07:00, ~7 hours). 15% of forecast requests fail during the 90-second recovery window. 3 customers open support tickets about failed forecast generation. UrbanThreads submits a frustrated product feedback note. |
| Sep 10, 14:15 | Support escalates the tickets to Engineering. Intelligence Squad lead sees the pattern now affecting customers. |
| Sep 10, 14:30 | Priya Sharma escalates NEX-4521 from P3 to P2. Assigns senior engineer (Tomoko Hayashi) to investigate full-time. Tomoko has deep Python profiling experience from her previous role. |
| Sep 11, 09:00 | Tomoko sets up a dedicated profiling environment. Deploys a forecast-engine pod with `tracemalloc` enabled and routes 10% of traffic to it. Configures hourly heap snapshots stored to S3. |
| Sep 11, 18:00 | After 9 hours of profiling data, Tomoko has 9 snapshots showing steady memory growth. Top allocator: `pandas/core/frame.py` -- DataFrame creation is responsible for 78% of memory growth. |
| Sep 12, 10:00 | Tomoko captures detailed heap dumps with object reference tracking. Identifies that `pandas.DataFrame` objects are not being garbage collected after forecast computation. Objects accumulate in memory, referenced by thread pool executor internals. |
| Sep 12, 11:00 | Tomoko narrows the leak to the `correlate_signals()` function in `forecast/signals/correlator.py`. The v3.0.x code processed signals sequentially; v3.1.0 parallelized this using `concurrent.futures.ThreadPoolExecutor`. |
| Sep 12, 14:00 | Root cause confirmed: the ThreadPoolExecutor submits tasks as lambda closures that capture the input DataFrame by reference. The executor's internal Future tracking keeps these references alive. In v3.0.x, sequential processing meant each signal was correlated and the DataFrame reference was released before the next signal started. In v3.1.0, all 3 signal tasks are submitted at once, and the executor retains references to all closures until the worker pool's internal deque rotates them out -- which depends on the `max_workers` setting and task submission rate, not on task completion. |
| Sep 12, 16:00 | Temporary mitigation deployed: increase pod memory limit from 4GB to 8GB and add scheduled worker recycling every 4 hours via `gunicorn --max-requests 1000`. The max-requests setting forces gunicorn workers to restart after 1000 requests, releasing all accumulated memory. |
| Sep 12, 17:00 | Mitigation takes effect. OOM frequency drops from ~5/day to ~1/day (workers that happen to get a burst of large-customer requests can still OOM before hitting 1000 requests). |

**Week 3 (September 17-24):**

| Time | Event |
|------|-------|
| Sep 17, 09:00 | Temporary mitigation reducing OOM frequency from ~5/day to ~1/day, but does not eliminate the issue. Memory growth is slower with higher limits but the leak persists. One additional OOM kill on Sep 15 (Pacific Rim Distributors forecast batch). |
| Sep 17, 10:00 | Tomoko begins implementing the fix. Two approaches considered: (a) revert to sequential signal correlation (safe but loses the parallelism performance gain), or (b) fix the parallel implementation to properly release DataFrame references. Tomoko chooses (b) with (a) as fallback. |
| Sep 18, 14:00 | Tomoko writes the fix: replaces lambda closures with explicit function calls that receive deep-copied DataFrames. After each signal correlation completes, the copied DataFrame is explicitly deleted and `gc.collect()` is called. The ThreadPoolExecutor is wrapped in a context manager that calls `shutdown(wait=True)` after all futures complete, releasing all internal references. |
| Sep 19, 11:00 | Tomoko submits fix PR (NEX-4521-fix-signal-correlator-memory-leak). PR includes: (a) the correlation fix, (b) a memory leak regression test that processes 1500 sequential forecast requests and asserts RSS memory stays within 2x baseline, and (c) updated comments explaining the memory management requirements for parallel DataFrame processing. |
| Sep 19, 12:00 | Code review by Priya Sharma and Chen Wei (Data Team lead, reviewing the pandas-specific patterns). Two review rounds. Chen suggests also explicitly calling `DataFrame.drop()` on intermediate merge results within the correlation function. |
| Sep 19, 14:00 | Fix passes code review and E2E tests. Deployed to staging at 14:30. Tomoko sets up a 24-hour memory observation with `tracemalloc` snapshots every 30 minutes. |
| Sep 20, 14:30 | 24-hour staging observation complete. Memory profile shows stable memory at ~800MB (peak 950MB during large forecast batch). Zero memory growth trend. All 9,200 staging forecast requests processed successfully. |
| Sep 22, 10:00 | Fix deployed to production as forecast engine v3.1.1. Priya, Tomoko, and Tom Bradley monitor the rollout in a dedicated Slack thread. |
| Sep 22, 18:00 | 8 hours post-deploy. All pods at 650-850MB memory. No OOM kills. P99 latency at 780ms (back to baseline). |
| Sep 24, 10:00 | 48 hours of production observation complete. Memory usage stable at 600-900MB across all pods. Zero OOM kills. Forecast accuracy metrics unchanged. Incident formally closed. |

### Root Cause

The forecast engine v3.1.0 introduced a demand signal correlation feature that parallelized the correlation of external signals (weather, events, economic data) with historical demand data. The implementation used `concurrent.futures.ThreadPoolExecutor` to process signals in parallel.

The bug: each thread received a reference to the input pandas DataFrame via a closure. The relevant code pattern was:

```python
def correlate_signals(df, signal_sources):
    with ThreadPoolExecutor(max_workers=3) as executor:
        futures = {
            executor.submit(lambda src=src: correlate_single(df, src)): src
            for src in signal_sources
        }
        results = {src: f.result() for f, src in futures.items()}
    return merge_results(df, results)
```

The `lambda src=src: correlate_single(df, src)` closure captures `df` by reference. The `ThreadPoolExecutor` keeps references to submitted `Future` objects (and their associated closures) in an internal work queue and result set. Although the `with` block calls `executor.shutdown(wait=True)`, the CPython implementation of `ThreadPoolExecutor` retains internal references to completed futures until the executor object itself is garbage collected. Because the `correlate_signals` function returns its result to the request handler (a gunicorn worker), and the worker processes multiple requests sequentially, the executor objects and their closure references accumulate in the worker's local scope chain.

Each forecast request for a customer with all 3 signal types (weather, events, economic indicators) would leak approximately 3 copies of the input DataFrame (typically 5-15MB each, depending on the customer's historical data volume and SKU count). With approximately 200 forecast requests per hour spread across 4 pods, this accumulated to approximately 3-9GB of leaked memory per pod over 8 hours of continuous operation.

The leak was not caught in pre-production testing because:
1. Unit tests process single requests and then terminate the Python process. Closures and their referenced objects are collected at process exit, so memory profiling per-test shows no leak.
2. Integration tests in staging ran with lower request volume (~20 requests/hour vs 200/hour in production), so the leak took 24+ hours to manifest in staging. Additionally, the staging pod memory limit was 8GB (vs 4GB in production), so the leak had to accumulate 4x more memory before causing an OOM -- which required ~32+ hours of continuous operation at staging traffic levels.
3. There were no load tests specifically targeting the new signal correlation feature. The quarterly k6 load tests exercise the forecast API but with synthetic data that produces smaller DataFrames (1,000 rows vs 50,000-200,000 rows for real Enterprise customers).

### Impact

- **Customer impact:** 37 OOM kills over 3 weeks caused intermittent forecast generation failures. Approximately 1,200 forecast requests failed (out of ~100,000 total during the 3-week period). The failure rate was approximately 1.2%, but was not uniformly distributed -- failures clustered during the 30-60 minute window before each OOM kill when GC pressure caused extreme latency. 8 customers opened support tickets, including 3 Enterprise customers.
- **Latency impact:** P99 forecast generation latency spiked to 12 seconds during high-memory periods (baseline: 800ms). This affected all customers using the forecast feature, not just those whose requests failed. The Nexus dashboard's "Forecast" tab showed loading spinners for 10+ seconds during degraded periods.
- **Accuracy impact:** No direct impact on forecast accuracy. Failed requests returned HTTP 503 errors and were retried by customers (either manually via the UI or automatically via API clients with retry logic). Retries succeeded after pod restart.
- **Downstream impact:** The replenishment engine depends on forecast data. During periods when the forecast engine was degraded, replenishment recommendations were generated using the most recent cached forecast (up to 24 hours old), which reduced recommendation quality for fast-moving SKUs.
- **Business impact:** 2 Enterprise customers (GlobalMart, Pacific Rim Distributors) mentioned the forecast instability in their September QBRs. UrbanThreads (mid-market reference customer) submitted a product feedback note expressing frustration. Intelligence Squad lost approximately 2 engineer-weeks of sprint velocity to investigation and remediation, delaying the anomaly detection feature (pushed from Q3 to Q4 2025).

### Resolution

1. Root cause fix in v3.1.1 (deployed September 22): Refactored signal correlation to use explicit DataFrame copies passed by value to thread tasks, with explicit `del` and `gc.collect()` calls after each signal correlation completes. ThreadPoolExecutor wrapped in a context manager with proper shutdown.
2. Temporary mitigation (in place from September 12-22): Increased pod memory limits from 4GB to 8GB and added gunicorn worker recycling (`--max-requests 1000`) to periodically release accumulated memory.
3. Prevention: Memory leak regression test added that runs 1,500 sequential forecast requests and asserts RSS memory stays within 2x baseline. This test runs as part of the CI pipeline for every PR to the forecast-engine repository.

### Action Items

| ID | Action | Owner | Deadline | Status |
|----|--------|-------|----------|--------|
| INC-067-01 | Add memory leak regression tests that simulate sustained load (1000+ requests) and assert stable memory | Tomoko Hayashi | October 10, 2025 | Completed |
| INC-067-02 | Add `tracemalloc` snapshots as a standard diagnostic in Python service runbooks | Priya Sharma | October 3, 2025 | Completed |
| INC-067-03 | Implement memory growth rate alerting for all Python services (alert if RSS grows > 100MB/hour sustained) | SRE Team | October 15, 2025 | Completed |
| INC-067-04 | Run load tests for all new features that change data processing patterns before production release | Viktor Nowak (QA) | Ongoing | In progress (added to release checklist) |
| INC-067-05 | Ensure staging resource limits match production to catch resource issues in pre-production | SRE Team | October 31, 2025 | Completed |
| INC-067-06 | Investigate replacing pandas with polars for large DataFrame operations (better memory management) | Intelligence Squad | Q1 2026 | Deferred to Q2 2026 |

### Lessons Learned

1. **Memory leaks in garbage-collected languages are insidious.** Python's GC cannot collect objects that are still referenced, even unintentionally. Thread pool executors and closures are common sources of hidden references because they create implicit reference chains that are not visible in the code structure. The `concurrent.futures.ThreadPoolExecutor` in particular retains internal references to Future objects and their closures in a way that is not documented and not intuitive. All Python services should have memory growth alerting, and code reviews for Python services should specifically check for closure-based reference leaks in concurrent code.
2. **Staging must mirror production resource constraints.** The 8GB staging limit masked a bug that manifested at 4GB in production. This is the second time a resource limit mismatch between staging and production has delayed bug detection (the first was a Redis connection pool issue in Q1 2025). Going forward, all production resource limits (memory, CPU, connection pools) must be replicated in staging via a shared Terraform module. The SRE team implemented this change in October 2025 (INC-067-05).
3. **Gradual degradation is harder to detect than sudden failure.** The leak took 6-8 hours to cause an OOM, making it look like a traffic pattern issue rather than a code bug. The initial responders (Week 1) reasonably attributed the OOMs to traffic spikes because the per-request memory looked normal. We need better trending alerts that detect memory growth rate, not just absolute thresholds. The new `memory_growth_rate` alert (implemented per INC-067-03) fires when RSS grows > 100MB/hour sustained over 2 hours, which would have caught this leak within 3 hours of the v3.1.0 deployment.
4. **Multi-week SEV-2 incidents need escalation checkpoints.** This incident persisted for 3 weeks as a P3 before being escalated to P2. The delay was partly because the OOM kills were intermittent and the service recovered automatically. We now have a policy that any recurring production issue (same alert firing > 5 times in a week) is automatically escalated to P2 and assigned a dedicated investigator within 48 hours.

---

## INC-2025-089: UPS Tracking Outage

**Severity:** SEV-2
**Duration:** 4 hours 12 minutes
**Date:** November 8, 2025
**Incident Commander:** Tom Bradley (SRE Lead)
**Technical Lead:** Priya Sharma (Nexus Core Team Lead)
**Comms Lead:** Michael Torres (Nexus PM)

### Summary

All UPS shipment tracking updates stopped for 4 hours and 12 minutes because the UPS API key stored in AWS Secrets Manager had expired and was not rotated. The quarterly secrets rotation in October 2025 missed the UPS API key because it was tagged with `rotation-annual` instead of `rotation-quarterly`. During the outage, approximately 180,000 UPS shipments across all customers showed stale tracking data. Shipment status updates for UPS packages were delayed by up to 4 hours, causing customer confusion and support ticket volume.

### Timeline

All times are in UTC.

| Time | Event |
|------|-------|
| Nov 8, 02:00 | UPS API key (stored in Secrets Manager as `acme/carriers/ups/api-key`) expires. The key was generated on August 8, 2025, with a 90-day validity. |
| Nov 8, 02:05 | Carrier polling job (`carrier-poller-ups`) begins receiving `401 Unauthorized` responses from the UPS Tracking API. The poller has retry logic with exponential backoff (3 retries, max 60s backoff). |
| Nov 8, 02:08 | After 3 retries, the poller logs the error and enters circuit breaker state (open). Circuit breaker is configured to retry every 5 minutes. |
| Nov 8, 02:10 | Grafana alert fires: `carrier_ups_error_rate > 50%`. Alert goes to `#nexus-alerts` Slack channel. Severity is P3 (carrier API issues are common and usually transient). PagerDuty is not paged for P3 after hours. |
| Nov 8, 02:15 | Circuit breaker retries. Still 401. Circuit breaker remains open. |
| Nov 8, 02:15 - 05:30 | Circuit breaker retries every 5 minutes, all fail with 401. Slack channel accumulates alerts but no one is monitoring at this hour (02:00-05:30 UTC is 18:00-21:30 PT the prior evening, after business hours). |
| Nov 8, 05:45 | East Coast customer support team (starting at 06:00 ET / 05:00 PT) begins receiving tickets from customers reporting stale UPS tracking. |
| Nov 8, 06:00 | Support L1 agent (Maria Rodriguez) checks the carrier status dashboard and sees UPS has been in circuit breaker state for ~4 hours. She checks the UPS status page -- no reported issues. She escalates to L2. |
| Nov 8, 06:10 | L2 support agent (Kevin Park) checks the carrier-poller-ups logs and sees consistent 401 errors. He suspects an API key issue and escalates to the Logistics Squad on-call (Anand Gupta). |
| Nov 8, 06:15 | Anand Gupta checks the UPS API key in Secrets Manager. Sees the key was created on August 8 and UPS keys have 90-day validity. The key expired 4 hours ago. |
| Nov 8, 06:20 | Anand logs into the UPS Developer Portal and generates a new API key. |
| Nov 8, 06:25 | New key stored in Secrets Manager: `aws secretsmanager update-secret --secret-id acme/carriers/ups/api-key --secret-string '{"api_key":"...","client_id":"...","client_secret":"..."}'` |
| Nov 8, 06:28 | Carrier poller restarted: `kubectl rollout restart deployment/carrier-poller-ups -n nexus` |
| Nov 8, 06:30 | Poller picks up new key, successfully authenticates to UPS API. Circuit breaker closes. |
| Nov 8, 06:30 - 06:45 | Poller begins catching up on 4 hours of stale tracking data. Processes backlog of ~180,000 shipment status updates. |
| Nov 8, 06:45 | All UPS tracking data is current. Spot-checks on 10 random UPS shipments confirm statuses are accurate and match UPS.com tracking. |
| Nov 8, 06:50 | Anand verifies no data was lost -- the Kafka topics retained the tracking poll results (7-day retention), and the backfill processed all pending updates. |
| Nov 8, 07:00 | Support team sends bulk update to the 23 customers who opened tickets, confirming the issue is resolved and explaining that tracking data is now current. |
| Nov 8, 07:05 | Tom Bradley updates the status page: "UPS tracking updates have been restored. Tracking data is current for all shipments. We will publish a post-mortem within 48 hours." |
| Nov 8, 07:12 | Incident formally closed. Anand files a JIRA ticket (NEX-5102) to track the post-mortem action items. |
| Nov 8, 09:00 | Anand and James Okafor conduct a quick audit of all other carrier API key expiration dates. They discover the DHL API key is also tagged `rotation-annual` and expires in 6 weeks. They proactively rotate it. |

### Root Cause

The UPS API key expired because it was not included in the quarterly secrets rotation procedure. During the initial setup of the UPS integration (February 2024), the key was tagged as `rotation-annual` in Secrets Manager because UPS keys had a 1-year validity at that time. In June 2025, UPS changed their API key validity from 1 year to 90 days as part of a security upgrade. ACME received an email notification about this change, but the email went to a shared `integrations@acme.dev` inbox that is not actively monitored.

When the key was manually rotated in August 2025 (after the previous key expired, in a similar but shorter incident that was handled informally), the `rotation-annual` tag was not updated to `rotation-quarterly`. The October 2025 quarterly rotation procedure only rotated secrets tagged `rotation-quarterly`, so the UPS key was skipped.

Contributing factors:
1. No automated monitoring for API key expiration dates.
2. The shared `integrations@acme.dev` inbox has no SLA for processing and is checked sporadically.
3. The August 2025 key rotation was done ad-hoc without updating the rotation metadata or creating a process improvement ticket.
4. P3 alerts after business hours are not paged, leading to a 4-hour detection delay.

### Impact

- **Customer impact:** Approximately 180,000 UPS shipments across 850+ customers showed stale tracking data for up to 4 hours. Customers relying on real-time tracking for logistics decisions (routing, delivery scheduling) were operating on outdated information. Shipment statuses were frozen at whatever state they were in at 02:00 UTC -- packages that were delivered during the outage window still showed "In Transit."
- **Exception alert impact:** The Nexus exception alerting system generates alerts when shipments deviate from expected timelines. During the outage, exception alerts for UPS shipments were suppressed (the system correctly detected that tracking data was stale and did not generate false exception alerts). However, this meant that genuine delivery exceptions during the 4-hour window were also not flagged, and customers discovered them only when tracking resumed.
- **Support impact:** 23 support tickets were opened during the outage window, primarily from US East Coast customers whose business day started during the outage. Support team SLA was met for all tickets (P2 response within 2 hours), but resolution was delayed until the engineering fix was in place.
- **Financial impact:** No SLA credits requested. 3 customers mentioned the outage in subsequent check-ins. One mid-market customer (LakeCity Outdoors) cited UPS tracking reliability as a concern during their renewal discussion in December 2025.
- **Volume context:** UPS is the second most-used carrier by ACME customers (28% of total tracked shipments, behind FedEx at 35%). The 180,000 affected shipments represented approximately 2 days of UPS shipment volume across all customers.

### Resolution

1. New UPS API key generated in UPS Developer Portal and stored in AWS Secrets Manager.
2. Carrier poller deployment restarted to pick up new key from Secrets Manager.
3. Tracking backlog of ~180,000 shipment status updates processed within 15 minutes.
4. Proactive audit: DHL API key (also tagged `rotation-annual`) discovered to be 6 weeks from expiration and rotated the same day.
5. All carrier API key rotation tags corrected in Secrets Manager within 24 hours of the incident.

### Action Items

| ID | Action | Owner | Deadline | Status |
|----|--------|-------|----------|--------|
| INC-089-01 | Audit all secrets in Secrets Manager and correct rotation tags to match actual vendor key validity periods | Aisha Mohammed (Security) | November 22, 2025 | Completed |
| INC-089-02 | Implement automated API key expiration monitoring: alert 30 days, 14 days, and 7 days before expiration for all carrier and third-party API keys | Marcus Webb (Platform) | December 6, 2025 | Completed |
| INC-089-03 | Set up the `integrations@acme.dev` inbox with Slack forwarding to `#relay-team` and assign a weekly rotation for inbox triage | James Okafor (Relay) | November 15, 2025 | Completed |
| INC-089-04 | Upgrade carrier API error alerts from P3 to P2 for errors persisting > 30 minutes (trigger PagerDuty page) | Tom Bradley (SRE) | November 22, 2025 | Completed |
| INC-089-05 | Add carrier API key validity metadata to Secrets Manager tags (`expiry-date`, `vendor-validity-period`) | Relay Team | December 1, 2025 | Completed |
| INC-089-06 | Investigate automated key rotation for carrier APIs that support it (FedEx, DHL have rotation APIs) | Relay Team | Q1 2026 | In progress |

### Lessons Learned

1. **Secrets rotation must be systematic, not tag-based.** Relying on manual tagging to determine which secrets need rotation is fragile. A single incorrect tag caused a 4-hour outage. We need a secrets inventory with vendor-specific validity periods and automated expiration tracking. The new secret expiration monitoring system (INC-089-02) alerts at 30, 14, and 7 days before expiration, providing multiple opportunities to catch upcoming expirations regardless of tags.
2. **Vendor policy changes need a reliable intake process.** The UPS key validity change from 1 year to 90 days was communicated via email to `integrations@acme.dev`, which is checked sporadically at best. This shared inbox receives approximately 200 emails per month from various integration partners, and there was no process for triaging important policy changes. Going forward, the inbox has Slack forwarding to `#relay-team` (INC-089-03), and a weekly rotation assigns a team member to triage the inbox.
3. **After-hours P3 alerts need duration-based escalation.** The carrier polling error was initially classified as P3 (carrier API issues are common and usually transient, resolving within minutes). But a P3 that persists for 4 hours is not a P3 anymore -- it affects all customers using that carrier. We now have duration-based escalation: carrier API errors persisting > 30 minutes are automatically upgraded to P2 and page the on-call engineer (INC-089-04).
4. **Ad-hoc fixes need process follow-through.** The August 2025 key rotation was done ad-hoc when the previous UPS key expired (a smaller incident that was not formally tracked). The engineer who rotated the key did not update the Secrets Manager tags or file a process improvement ticket. Going forward, all ad-hoc secret rotations must be logged in the `#secrets-rotation` Slack channel and the Secrets Manager tags must be updated as part of the rotation procedure (added to the RB-014 runbook).

---

## INC-2026-003: EU Cluster Failover Cascade

**Severity:** SEV-1
**Duration:** 45 minutes
**Date:** January 14, 2026
**Incident Commander:** Tom Bradley (SRE Lead)
**Technical Lead:** Marcus Webb (Platform Team Lead)
**Comms Lead:** Thomas Eriksson (FreshDirect Europe CSM)

### Summary

An AWS eu-west-1 availability zone (AZ) failure caused simultaneous RDS Multi-AZ failover and EKS node drain, triggering a cascade of connection pool exhaustion across all services in the EU production cluster. The RDS failover (30-60 second DNS propagation) coincided with a flood of new connection attempts from services that were being rescheduled to healthy nodes, exhausting the pgbouncer connection pools and causing a 45-minute outage for all EU customers (primarily FreshDirect Europe, ACME's first Sentinel customer and a key reference account).

### Timeline

All times are in UTC.

| Time | Event |
|------|-------|
| Jan 14, 09:12 | AWS eu-west-1a availability zone begins experiencing degraded network connectivity. Packet loss exceeds 80% on inter-AZ links from eu-west-1a. AWS does not post to the status page for another 8 minutes. |
| Jan 14, 09:12:30 | Checkly synthetic monitors for EU endpoints begin failing from the Dublin probe location. Alert fires in `#sre-alerts`. |
| Jan 14, 09:13 | EKS nodes in eu-west-1a become NotReady. Kubernetes node controller begins draining pods from affected nodes (14 of 40 nodes are in eu-west-1a). This affects 47 pods across all namespaces. |
| Jan 14, 09:13:30 | RDS connection errors spike across all EU services as the primary DB instances in eu-west-1a become unreachable. Services begin logging `connection refused` and `timeout` errors. |
| Jan 14, 09:14 | RDS instances in eu-west-1a begin Multi-AZ failover to eu-west-1b standby replicas. RDS event: "Multi-AZ instance failover started." 4 database instances fail over simultaneously. |
| Jan 14, 09:14 | PagerDuty fires 11 simultaneous alerts: `rds_failover_detected` (x4), `eks_node_not_ready` (x1), `service_error_rate_critical` (x6, one per service). Tom Bradley (SRE lead) is paged with critical urgency. |
| Jan 14, 09:15 | Kubernetes scheduler begins placing pods from drained eu-west-1a nodes onto eu-west-1b and eu-west-1c nodes. 47 pods are rescheduled within 60 seconds. |
| Jan 14, 09:15 | RDS failover completes. New primary is in eu-west-1b. DNS propagation begins (TTL was 5 seconds, but some services cache DNS). |
| Jan 14, 09:16 | Rescheduled pods start up and immediately attempt to establish database connections. pgbouncer connection pools (max 100 connections per service) are overwhelmed by the burst of 47 pods simultaneously opening connections. |
| Jan 14, 09:16 | pgbouncer begins rejecting connections with "no more connections allowed." Services enter error state. Health checks fail. Kubernetes kills pods and reschedules them, creating a secondary crash loop. |
| Jan 14, 09:18 | Tom Bradley acknowledges alerts and begins investigation. Sees a wall of alerts across all EU services. Declares SEV-1 in `#incidents`. |
| Jan 14, 09:20 | Tom identifies the connection pool exhaustion pattern. pgbouncer logs show max_client_conn reached across all service pools. |
| Jan 14, 09:22 | Marcus Webb joins (paged as Platform team lead). They decide on a two-pronged approach: (1) increase pgbouncer connection limits and (2) implement a rolling restart with staggered startup. |
| Jan 14, 09:25 | Marcus patches pgbouncer config to increase max_client_conn from 100 to 300 for all service pools: |
|  | `kubectl --context acme-prod-euw1 edit configmap pgbouncer-config -n platform` |
|  | `kubectl --context acme-prod-euw1 rollout restart deployment/pgbouncer -n platform` |
| Jan 14, 09:28 | pgbouncer restarts with new limits. However, the RDS instance (r6g.2xlarge) has a max_connections of 5000, and with 300 connections x 8 services = 2400 potential connections, this is within limits. |
| Jan 14, 09:30 | Pods begin successfully establishing connections. But many pods are still crash-looping from the earlier failures and health check timeouts. Kubernetes backoff timers prevent immediate recovery. |
| Jan 14, 09:32 | Marcus manually resets the crash-looping deployments with forced rollouts: |
|  | `for deploy in nexus-api inventory-service shipment-service auth-service sentinel-api screening-service beacon-api gateway; do kubectl --context acme-prod-euw1 rollout restart deployment/$deploy -n $deploy; sleep 10; done` |
|  | The 10-second sleep between restarts staggers the connection pool demand. |
| Jan 14, 09:40 | Services begin coming back online one by one. Auth-service recovers first, allowing SSO logins. |
| Jan 14, 09:45 | All core services (nexus-api, inventory-service, shipment-service, gateway) are healthy. Screening-service and sentinel-api recovering. |
| Jan 14, 09:50 | Screening-service requires additional time -- its Elasticsearch connection pool also needs to reconnect, and OpenSearch is still recovering from the AZ failure (reassigning shards from eu-west-1a nodes). |
| Jan 14, 09:52 | All services healthy. Tom confirms via Grafana dashboards: error rates back to baseline across all EU services, latency within normal range. |
| Jan 14, 09:53 | Tom runs the EU production smoke test suite: `ACME_API_URL=https://api-euw1.acme.dev go test ./tests/smoke/... -v -count=1` -- all 42 tests pass. |
| Jan 14, 09:55 | Thomas Eriksson updates FreshDirect Europe with resolution confirmation. FreshDirect operations team confirms they can access Nexus and Sentinel normally. |
| Jan 14, 09:56 | Nadia Hassan (Sentinel PM) coordinates with FreshDirect compliance team to identify any shipments that were cleared during the outage without Sentinel screening. 23 shipments identified. |
| Jan 14, 09:57 | Incident resolved. Total customer-facing outage: 45 minutes (09:12 - 09:57). Status page updated. |
| Jan 14, 10:00 | SRE team begins monitoring eu-west-1a recovery. Nodes in the AZ begin returning to Ready state. |
| Jan 14, 10:15 | AWS posts to status page confirming eu-west-1a network connectivity issues, resolved at 09:35. ACME's outage lasted 22 minutes beyond the AWS resolution due to the cascading recovery issues. |
| Jan 14, 10:30 | Post-incident, Tom and Marcus review the pgbouncer connection limit change and the staggered restart approach. They agree both should be made permanent configuration changes. |
| Jan 14, 14:00 | Raj Patel coordinates retroactive screening for the 23 FreshDirect shipments. All 23 clear screening. Documentation filed in audit trail with incident reference. |

### Root Cause

The outage was caused by a cascading failure triggered by an AWS eu-west-1a availability zone network disruption. Three factors combined to create the cascade:

1. **Simultaneous RDS failover + EKS pod rescheduling:** The AZ failure caused both database failover and pod rescheduling at the same time. Normally, RDS failover is a brief (30-60s) disruption that services handle via connection pool retry logic. But when pods are also being rescheduled, the new pods attempt to open connections during the DNS propagation window, getting stale DNS responses.

2. **Connection pool thundering herd:** 47 pods starting simultaneously all attempted to establish their full connection pool allocation at once (each pod pre-warms 10 connections). This created a burst of 470 simultaneous connection attempts against pgbouncer, which had a limit of 100 connections per service pool.

3. **Health check cascade:** When pods could not establish database connections, their health checks failed. Kubernetes killed and rescheduled them, creating another wave of connection attempts. This positive feedback loop prevented recovery.

The EU cluster was more vulnerable to this cascade for several reasons:
1. Fewer nodes (40 vs 120 in US), so a single AZ failure affects a larger percentage of capacity (35% vs ~33% in US). But more critically, the absolute headroom was much smaller: the US cluster had ~40 spare nodes of capacity, while the EU cluster had approximately 6-8 spare nodes.
2. The EU cluster runs all products (Nexus, Relay, Beacon, Sentinel) for EU data residency compliance. This means all services were affected simultaneously, creating a broader blast radius.
3. pgbouncer in the EU cluster had never been load-tested for the burst scenario. The US cluster's pgbouncer had been scaled up after an unrelated incident in Q2 2025 and had higher connection limits (150 vs 100 in EU).
4. The EU cluster does not have PodDisruptionBudgets (PDBs) configured, so Kubernetes was free to evict any number of pods simultaneously. The US cluster had PDBs added as a preventive measure in November 2025 (after a capacity planning review), but the same change was not applied to the EU cluster.

### Impact

- **Customer impact:** All EU customers experienced a complete service outage for 45 minutes (09:12 - 09:57 UTC). EU cluster serves 85 customers, of which 12 are Enterprise tier. FreshDirect Europe (largest EU customer, $280K ACV, processing 500K screenings/month) could not access Nexus, Beacon, or Sentinel. Their morning operations team (approximately 40 people across 8 EU countries) was unable to perform daily tasks including inventory checks, shipment tracking, and denied party screenings.
- **Screening/compliance impact:** FreshDirect Europe's compliance team flagged that 23 cross-border shipments were cleared during the outage window using their internal manual fallback process (paper-based screening against a static sanctions list PDF). These 23 shipments required retroactive Sentinel screening and audit trail documentation, which was completed the same day with no compliance violations found.
- **Data impact:** No data loss. RDS cross-region replica lag at the time of the AZ failure was < 500ms. All transactions that were committed before the failure were preserved on the failover replica. In-flight transactions at the moment of failure (estimated 12-15 API requests) were lost and returned errors to clients. These were retried automatically by the Nexus web app.
- **Business impact:** FreshDirect Europe's CTO (Lars Eriksson) requested a post-mortem meeting with ACME executive team, which was held on January 20 with Dana Chen (CTO) and Tom Bradley (SRE Lead). No SLA credit was formally invoked, but the incident was referenced in their contract renewal discussions (renewal due April 2026). This incident was a primary motivator for accelerating the cross-region failover automation project (currently manual per RB-016).

### Resolution

1. Increased pgbouncer `max_client_conn` from 100 to 300 per service pool to accommodate burst connection demand during pod rescheduling events.
2. Staggered service restarts (10-second delay between deployments) to avoid thundering herd on database connection pools. This pattern was later automated in the Platform team's deployment tooling.
3. Manual pod restart via `kubectl rollout restart` to break the crash-loop cycle caused by health check failures cascading into further rescheduling.
4. Post-incident: EU cluster node count increased from 40 to 60 (Terraform change applied January 17), providing greater capacity headroom for AZ failure scenarios.

### Action Items

| ID | Action | Owner | Deadline | Status |
|----|--------|-------|----------|--------|
| INC-003-01 | Implement connection pool backoff with jitter on service startup (random 1-10s delay before establishing DB connections) | Marcus Webb | February 7, 2026 | Completed |
| INC-003-02 | Increase default pgbouncer max_client_conn to 250 in all clusters and add auto-scaling based on pod count | Platform Team | February 14, 2026 | Completed |
| INC-003-03 | Add pod disruption budgets (PDBs) to all critical services to prevent more than 25% of pods being evicted simultaneously | Platform Team | January 28, 2026 | Completed |
| INC-003-04 | Implement readiness gate that checks DB connectivity before marking pod as ready (prevents health check cascade) | Platform Team | February 28, 2026 | Completed |
| INC-003-05 | Increase EU cluster node count from 40 to 60, distributed across 3 AZs to improve resilience | Tom Bradley (SRE) | January 31, 2026 | Completed |
| INC-003-06 | Implement automated screening fallback mode for Sentinel that activates when primary service is unavailable | Raj Patel (Sentinel) | March 15, 2026 | In progress |
| INC-003-07 | Conduct quarterly chaos engineering exercise simulating AZ failure in EU cluster | SRE Team | Quarterly | Ongoing |
| INC-003-08 | Review and test cross-region failover procedure (RB-016) -- this incident was resolved before failover was needed but it was discussed | Tom Bradley | February 28, 2026 | Completed |

### Lessons Learned

1. **AZ failures cause correlated failures across multiple infrastructure layers.** We tested RDS failover (RB-005) and pod rescheduling separately, but never together. The combination created a cascading failure we had not anticipated. The RDS failover changed the database endpoint DNS while pods were simultaneously being rescheduled, so new pods connected to stale DNS entries. Chaos testing must include multi-component failure scenarios -- specifically, simultaneous database failover and pod rescheduling.
2. **Connection pool limits must account for burst scenarios, not just steady state.** Steady-state pgbouncer connection usage was approximately 40 connections per service pool (well within the 100 limit). But the thundering herd pattern during recovery (47 pods starting simultaneously, each pre-warming 10 connections) created a burst of 470 connections in under 60 seconds. Connection pool sizing should account for worst-case pod rescheduling scenarios, which we now calculate as: `max_connections = num_pods * connections_per_pod * 1.5` (1.5x safety factor for rolling restarts).
3. **Smaller clusters are disproportionately impacted by AZ failures.** The EU cluster with 40 nodes had only 35% of nodes in eu-west-1a, but the loss of those 14 nodes triggered a cascade that affected the entire cluster. The US cluster (120 nodes) has more headroom to absorb a similar AZ loss. Right-sizing must account for N-1 AZ failure scenarios, not just steady-state traffic. The new EU cluster size (60 nodes across 3 AZs) provides sufficient headroom for a single-AZ failure.
4. **Recovery procedures need to account for cascading failures.** The existing RB-005 (Database Failover) runbook describes the RDS failover in isolation. It does not address the scenario where RDS failover coincides with pod rescheduling. We have updated RB-005 to reference this post-mortem and added a section on staggered service restart for cascade recovery.
5. **Health check configuration matters during recovery.** The default Kubernetes liveness probe configuration (failure threshold: 3, period: 10s) was too aggressive during the recovery window. Pods that could not establish database connections within 30 seconds were killed and rescheduled, adding to the thundering herd. The post-incident action (INC-003-04) implemented a startup probe with a longer timeout specifically for database connectivity, giving pods more time to establish connections during recovery scenarios.

---

## INC-2026-011: Sentinel Screening False Positive Spike

**Severity:** SEV-2
**Duration:** 6 hours 28 minutes
**Date:** February 19, 2026
**Incident Commander:** Raj Patel (Sentinel Team Lead)
**Technical Lead:** Sanjay Mehta (Senior Engineer, Sentinel Team)
**Comms Lead:** Nadia Hassan (Sentinel PM)

### Summary

An OFAC Specially Designated Nationals (SDN) list update on February 18, 2026, included new entries with high fuzzy-match potential against common company names (e.g., "Global Trading", "Pacific Logistics", "East West Import Export"). The Sentinel screening service's similarity matching algorithm generated false positives for 15.3% of screening requests (baseline: 0.4%), causing over 4,200 shipments to be placed on compliance hold across 180+ customers. The issue was not detected by automated monitoring because the false positive rate threshold was set at 20%. Customers began reporting the issue via support tickets starting at their business-day opening.

### Timeline

All times are in UTC.

| Time | Event |
|------|-------|
| Feb 18, 02:00 | Automated sanctions list update pipeline runs. Pulls latest OFAC SDN list, BIS Entity List, EU Consolidated List, and 15 other lists. Pipeline completes successfully at 02:47. |
| Feb 18, 02:47 | Screening Elasticsearch index is updated with 342 new entries across all lists. Of these, 28 are new OFAC SDN entries from a recent designation action targeting trade-based money laundering networks. |
| Feb 18, 02:50 | Legal team is notified of new entries via automated email (per standard process, Legal reviews additions within 24 hours). Email subject: "342 new sanctions list entries indexed - review required." |
| Feb 18, 03:00 | First screening requests against updated index begin. False positive rate at 2.1% (baseline is 0.4%). Early requests are from APAC customers (business hours in Singapore/Tokyo). |
| Feb 18, 06:00 | EU business day begins. Higher request volume. False positive rate climbs to 8% as more diverse company names are screened against the new entries. |
| Feb 18, 10:00 | US East Coast business day traffic begins. False positive rate climbs to 12%. Approximately 1,800 shipments are now on compliance hold. |
| Feb 18, 14:00 | US West Coast fully online. False positive rate reaches 15.3%. Over 3,000 shipments on hold. The automated alert threshold is 20%, so no alert fires. |
| Feb 18, 15:00 | Legal team member (Jason Rivera) begins reviewing the 342 new entries as part of the 24-hour review SLA. He notices several entries with generic names but does not flag them for engineering review (the Legal review checklist does not include an algorithmic impact assessment step). |
| Feb 18, 18:00 - Feb 19, 06:00 | Overnight. Lower request volume but false positive rate stays steady at ~15%. Cumulative compliance holds reach 4,200 shipments. No alert fires. |
| Feb 19, 06:15 | US East Coast customers begin their business day. Multiple customers notice that a large number of shipments are in "Compliance Hold" status. First support ticket opens at 06:15 from GlobalMart. |
| Feb 19, 06:30 | 5 support tickets from different customers in 15 minutes. L1 support agent (Maria Rodriguez) notices the pattern and escalates to L2. |
| Feb 19, 06:45 | L2 support agent (Kevin Park) checks the Sentinel screening dashboard and sees the elevated false positive rate. He escalates to Raj Patel (Sentinel team lead). |
| Feb 19, 07:00 | Raj Patel reviews the screening logs. Identifies that the false positive spike correlates with the February 18 sanctions list update. Declares SEV-2 incident in `#incidents`. |
| Feb 19, 07:15 | Raj assigns Sanjay Mehta as Technical Lead. Sanjay begins analyzing the false positive matches. |
| Feb 19, 07:30 | Sanjay identifies the problematic entries. 28 new OFAC SDN entries include entities with names like: |
|  | - "Global Trading Network" (matches "Global Trading Corp", "Global Trading LLC", etc.) |
|  | - "Pacific Logistics International" (matches "Pacific Logistics Co", "Pacific Logistics Group", etc.) |
|  | - "Eastern Import Export Company" (matches "East Import Export", "Eastern Imports", etc.) |
|  | The screening service uses a Levenshtein distance-based fuzzy match with a similarity threshold of 0.85. These new entries have high similarity scores against very common company names used by legitimate businesses. |
| Feb 19, 07:45 | Sanjay proposes two options: (1) temporarily increase the similarity threshold to 0.92 to reduce false positives, or (2) add exemptions for the specific problematic SDN entries. Raj chooses option 1 as the fastest mitigation while the team works on a proper fix. |
| Feb 19, 08:00 | Similarity threshold updated: |
|  | `curl -XPUT https://sentinel.internal.acme.dev/admin/screening/config -H 'Content-Type: application/json' -d '{"similarity_threshold": 0.92}'` |
| Feb 19, 08:05 | False positive rate drops from 15.3% to 3.2%. Still elevated above baseline (0.4%) but significantly better. |
| Feb 19, 08:15 | Sanjay begins implementing the proper fix: adding a "common name" exclusion list that requires exact match (not fuzzy) for entity names that appear in more than N screening requests per day (indicating they are common legitimate business names). |
| Feb 19, 08:30 | Nadia Hassan (Sentinel PM) sends customer communication via StatusPage explaining the issue and resolution timeline. |
| Feb 19, 09:00 | Legal team (Patricia Nguyen, General Counsel) reviews the situation. Confirms that the temporary threshold increase is acceptable from a compliance perspective because (a) all true positives at 0.92+ threshold are still caught, and (b) the 0.85-0.92 range entries will be retrospectively screened once the proper fix is in place. |
| Feb 19, 10:30 | Sanjay deploys the "common name" exclusion fix to staging. Testing shows false positive rate returns to 0.5% (slightly above baseline due to genuinely new matches from the legitimate SDN additions). |
| Feb 19, 11:00 | Fix deployed to production. Similarity threshold reverted to 0.85. |
| Feb 19, 11:30 | False positive rate stabilizes at 0.5%. |
| Feb 19, 11:45 | Sanjay runs a batch re-screening of all 4,200 shipments that were placed on compliance hold: |
|  | `kubectl exec -n sentinel deployment/screening-service -- /app/tools/batch-rescreen --since="2026-02-18T02:47:00Z" --reason="INC-2026-011-remediation"` |
| Feb 19, 12:00 | Re-screening completes. Results: 4,147 shipments cleared (false positives), 53 shipments remain on hold (true matches requiring human review). The 53 are routed to the Sentinel Review Queue for the 3 customs broker contractors to review. |
| Feb 19, 12:15 | Sanjay verifies the common-name exclusion logic is working correctly by spot-checking 50 random screening requests. All produce expected results -- genuine matches caught, common-name false positives eliminated. |
| Feb 19, 12:30 | Nadia Hassan sends customer communication via StatusPage and direct email to all Enterprise customers confirming full resolution. Customers instructed to check their shipment holds, which should now be released for false positives. |
| Feb 19, 13:00 | Support team begins working through the 47 open support tickets, confirming resolution with each customer individually. |
| Feb 19, 13:15 | Sentinel Review Queue completes review of the 53 true-positive holds. 48 are confirmed matches against newly sanctioned entities (customers' trading partners). 5 are borderline cases escalated to Legal for determination. |
| Feb 19, 13:28 | All false-positive holds released. All affected customers confirmed cleared. Incident formally closed. |
| Feb 19, 14:00 | Raj Patel schedules individual calls with FreshDirect Europe, Pacific Rim Distributors, and the onboarding customer to walk through the incident, root cause, and prevention measures. |
| Feb 19, 16:00 | Patricia Nguyen (General Counsel) confirms that the 3-hour window with the elevated similarity threshold (0.92 vs 0.85) does not constitute a compliance gap, as all entities with similarity >= 0.92 were still flagged, and the 0.85-0.92 range entries were retroactively screened. Documentation filed for SOC 2 audit trail. |

### Root Cause

The Sentinel screening service uses Levenshtein distance-based fuzzy matching to identify potential matches against sanctions lists. The similarity threshold of 0.85 was calibrated during initial development based on analysis of historical sanctions list entries, which tended to have distinctive names (specific individuals, uniquely named front companies, etc.).

The February 18, 2026, OFAC SDN update included 28 new entities related to a trade-based money laundering network. The Treasury Department's designation action (published as Federal Register notice 2026-03291) targeted a network that used shell companies with intentionally generic names to obscure their identity. Several of these entities used generic business names:

- "Global Trading Network" (0.89 similarity to "Global Trading Corp", a legitimate trading partner used by 340+ ACME customers)
- "Pacific Logistics International" (0.91 similarity to "Pacific Logistics Co", used by 180+ customers)
- "Eastern Import Export Company" (0.87 similarity to "East Import Export LLC", used by 95+ customers)
- "Continental Supply Services" (0.86 similarity to "Continental Supply Co", used by 220+ customers)
- "TransWorld Shipping Group" (0.88 similarity to "TransWorld Shipping Inc", used by 150+ customers)

These entries, combined with 23 other similarly named entities, generated high-confidence false positive matches against a large percentage of ACME customers' trading partner databases.

The automated sanctions list update pipeline does not analyze new entries for false positive potential before indexing them. The pipeline's job is to keep the screening index current -- it fetches, parses, and indexes new entries without any impact simulation. The Legal team review (within 24 hours) focuses on regulatory implications (which programs are affected, which countries, what restrictions apply), not on matching algorithm impact. There was no mechanism to detect or prevent a sudden spike in false positives caused by new list entries with generic names.

Contributing factors:
1. The false positive rate alert threshold was set at 20%, which was calibrated based on the initial Sentinel beta period (October-November 2025) when the customer base was small and the variance was high. As the customer base grew, the baseline stabilized at 0.4% and the 20% threshold became meaninglessly high. Nobody updated it.
2. The similarity matching algorithm uses a single Levenshtein distance threshold (0.85) for all entity names regardless of name frequency. A common name like "Global Trading" should require a higher similarity for a match than a distinctive name like "Zhongtian Fidelity Holdings," because the prior probability of a legitimate entity having a common name is much higher.
3. The 24-hour Legal review window does not include an algorithmic impact assessment step. The Legal review checklist covers: regulatory program identification, jurisdictional analysis, and customer notification requirements -- but not "will these entries cause false positives in our matching system."
4. Sentinel had no customer self-service mechanism for reviewing and releasing false positive holds. Every held shipment required a support ticket, creating a 47-ticket backlog that overwhelmed the support team.

### Impact

- **Customer impact:** 4,200 shipments across 180+ customers were placed on compliance hold. Compliance holds in Sentinel are blocking -- the shipment cannot proceed through the logistics pipeline until the hold is released by either (a) human review confirming a false positive, or (b) the customer's compliance officer authorizing release. Without a self-service release mechanism, every hold required a support ticket. 47 support tickets were opened, overwhelming the 5-person support team.
- **Geographic distribution:** US customers accounted for 68% of holds (2,856 shipments), EU customers 24% (1,008 shipments), APAC customers 8% (336 shipments). The impact was felt most acutely by customers with high screening volumes: FreshDirect Europe (890 holds), Pacific Rim Distributors (620 holds), and GlobalMart (415 holds).
- **Financial impact:** Estimated $150K in aggregate customer logistics delays across all affected customers. This includes: late delivery penalties charged by retailers to ACME customers ($45K estimated), warehouse holding costs for shipments awaiting release ($35K), and expedited shipping costs when customers paid for faster shipping to compensate for the delay ($70K). These costs were borne by ACME's customers, not by ACME directly. No SLA credits were requested, though ACME offered a one-month screening fee waiver to the 10 most affected customers.
- **Compliance impact:** The temporary threshold increase (0.85 to 0.92) technically reduced screening sensitivity for 3 hours. During this window, entities with similarity scores between 0.85 and 0.91 would not have been flagged. Legal analysis (conducted by Patricia Nguyen, General Counsel) confirmed this was acceptable because: (a) all known high-risk entities on current sanctions lists have distinctive enough names to match at 0.92+, (b) the 0.85-0.92 range was retrospectively screened via the batch re-screening job, and (c) no shipments to sanctioned entities were released during the window. Documentation was filed in the SOC 2 audit trail.
- **Reputation impact:** 3 Enterprise customers raised concerns about Sentinel reliability. FreshDirect Europe (first Sentinel customer, key reference account) expressed concern that false positives could disrupt their cross-border operations. Pacific Rim Distributors (most complex Sentinel setup, EAR screening) asked for guarantees about screening accuracy. A new customer in onboarding (NorthStar Retail, $95K ACV) delayed their Sentinel go-live by 3 weeks pending resolution. Nadia Hassan (Sentinel PM) conducted individual calls with each customer to explain the root cause, the fix, and the prevention measures.
- **Operational impact:** The Sentinel Review Queue (3 customs broker contractors) was overwhelmed by the 53 true-positive matches that required human review, in addition to their normal daily queue of ~30 reviews. The backlog was cleared by end of day February 19 with overtime.

### Resolution

1. Temporary mitigation (08:00-11:00 Feb 19): Increased similarity threshold from 0.85 to 0.92, reducing false positive rate from 15.3% to 3.2%. Legal confirmed this did not create a compliance gap.
2. Permanent fix (deployed 11:00 Feb 19): Implemented "common name" frequency-weighted similarity scoring. The algorithm now calculates the frequency of each entity name across all screening requests in the past 30 days. Names appearing in more than 50 unique screening requests are classified as "common" and require a similarity score of 0.95 or higher for a fuzzy match (vs 0.85 for rare names). This reduces false positives for generic business names while maintaining sensitivity for distinctive sanctioned entity names.
3. Batch re-screening of all 4,200 affected shipments using the corrected algorithm. 4,147 shipments cleared, 53 true positives routed to the Sentinel Review Queue for human review.
4. Retroactive documentation filed in the audit trail for the 3-hour threshold change window, satisfying SOC 2 audit requirements.

### Action Items

| ID | Action | Owner | Deadline | Status |
|----|--------|-------|----------|--------|
| INC-011-01 | Lower false positive rate alert threshold from 20% to 5% | Raj Patel | February 21, 2026 | Completed |
| INC-011-02 | Implement name frequency-weighted similarity scoring (common names require higher similarity for match) | Sanjay Mehta | March 15, 2026 | Completed |
| INC-011-03 | Add pre-indexing impact analysis to sanctions list update pipeline: simulate new entries against last 30 days of screening requests and alert if projected false positive rate exceeds 2% | Sentinel Team | March 31, 2026 | In progress |
| INC-011-04 | Add Legal review step specifically for algorithmic impact when new sanctions entries contain generic business names | Raj Patel + Patricia Nguyen | March 7, 2026 | Completed |
| INC-011-05 | Create customer self-service tool to release false positive holds with reason code (reduces support ticket volume) | Sentinel Team + Frontend Team | April 30, 2026 | In progress |
| INC-011-06 | Implement customer-specific screening allowlists: known trading partners pre-approved by customer compliance team are exempt from fuzzy matching (exact match only) | Sentinel Team | Q2 2026 | Planned |
| INC-011-07 | Conduct quarterly review of similarity threshold calibration against recent sanctions list composition | Raj Patel | Quarterly | Ongoing |

### Lessons Learned

1. **Automated pipelines need impact analysis, not just execution monitoring.** The sanctions list update pipeline ran successfully (no errors), but the data it ingested had an outsized operational impact. Pipelines that update matching/scoring indices should simulate the impact of new data before committing it to production.
2. **Alert thresholds must reflect customer impact, not just statistical anomalies.** A 15% false positive rate sounds like a modest number, but it translated to 4,200 shipment holds and 47 support tickets. Thresholds should be calibrated based on customer impact modeling, not just historical variance.
3. **Fuzzy matching needs contextual tuning.** A single similarity threshold for all entity names does not account for the distribution of name commonality. "Global Trading" should not match at the same threshold as "Zhongtian Fidelity Holdings." Name frequency weighting is essential for reducing false positives without compromising true positive detection.
4. **Compliance tooling needs graceful degradation.** When screening generates excessive false positives, customers should have a self-service path to review and release holds, rather than requiring individual support tickets. The compliance hold should include the match details so customers can make informed decisions.
5. **Cross-incident pattern: monitoring thresholds decay.** This is the third incident in 6 months where an alert threshold was set during initial development and never updated as the system matured. The pattern:
   - **INC-2025-089 (UPS tracking):** P3 carrier alert did not escalate based on duration. Fixed by adding duration-based escalation.
   - **INC-2025-067 (Forecast memory leak):** No memory growth rate alert existed; only absolute threshold. Fixed by adding growth rate monitoring.
   - **INC-2026-011 (This incident):** False positive rate threshold at 20% was meaninglessly high. Fixed by lowering to 5%.
   We now have a quarterly monitoring threshold review (added to the SRE team's quarterly checklist) where all alerting thresholds are evaluated against current baseline metrics and customer impact models. The first review was conducted in March 2026 and resulted in 14 threshold adjustments across all services.
6. **Sanctions list updates are a high-risk operation.** Daily automated list updates are necessary for compliance, but they change the behavior of a customer-facing system without human review. The pre-indexing impact analysis (INC-011-03) will simulate the effect of new entries before they are indexed, providing a safety net similar to what canary deployments provide for code changes. This is essentially a "canary deployment for data."

---

## Appendix: Incident Metrics Summary

| Incident | Severity | Duration | Customers Affected | Support Tickets | Data Loss | SLA Credit |
|----------|----------|----------|-------------------|-----------------|-----------|------------|
| INC-2025-042 | SEV-1 | 2h 14m | 1 (GlobalMart) | 0 | 312 records (corrected) | None |
| INC-2025-067 | SEV-2 | ~3 weeks | All forecast users (~400) | 8 | None | None |
| INC-2025-089 | SEV-2 | 4h 12m | 850+ (all UPS users) | 23 | None | None |
| INC-2026-003 | SEV-1 | 45m | 85 (all EU customers) | 3 | 12-15 in-flight requests | None |
| INC-2026-011 | SEV-2 | 6h 28m | 180+ (Sentinel users) | 47 | None | None |

**Trailing 12-month reliability (March 2025 - March 2026):**
- Total SEV-1 incidents: 3 (INC-2025-042, INC-2026-003, and one unrelated networking issue in May 2025)
- Total SEV-2 incidents: 7
- Nexus API uptime: 99.97% (target: 99.95%)
- Mean time to detect (MTTD) for SEV-1: 6 minutes
- Mean time to resolve (MTTR) for SEV-1: 67 minutes (target: 60 minutes)
- Mean time to resolve (MTTR) for SEV-2: 3.8 hours (target: 4 hours)

**Common themes across incidents:**

1. **Monitoring gaps:** 4 of 5 incidents involved monitoring that was insufficient to detect the issue promptly. Alert thresholds were either too high (INC-2026-011), missing entirely (INC-2025-067 memory growth rate), not escalating based on duration (INC-2025-089), or not covering the specific failure mode (INC-2026-003 cascading connection exhaustion).

2. **Known issues causing incidents:** 2 of 5 incidents (INC-2025-042, INC-2025-089) involved known issues or known risks that were documented but not acted upon. The IDoc memory leak was a known bug for 4 months. The UPS key rotation mismatch was an artifact of an undocumented vendor policy change. Both could have been prevented with more systematic tracking of known risks.

3. **Recovery cascades:** 2 of 5 incidents (INC-2025-042, INC-2026-003) involved cascading failures during recovery. The SAP retry storm in INC-2025-042 and the connection pool thundering herd in INC-2026-003 both turned recoverable failures into extended outages. Services need circuit breakers and graceful degradation during recovery, not just during initial failure.

4. **Enterprise customer concentration risk:** GlobalMart, FreshDirect Europe, and Pacific Rim Distributors appear in multiple incidents. As ACME's largest and most complex customers, they exercise the system in ways that smaller customers do not. Their usage patterns (large data volumes, complex integrations, strict compliance requirements) expose edge cases that are not covered by standard testing. Dedicated monitoring dashboards for top-10 Enterprise customers were implemented in Q1 2026.

5. **Data pipeline changes need canary mechanisms:** 2 of 5 incidents (INC-2025-042, INC-2026-011) were caused by data-driven changes (large IDoc batch, new sanctions list entries) rather than code deployments. ACME has robust canary deployment practices for code changes but no equivalent for data changes. The sanctions list pre-indexing impact analysis (INC-011-03) is a step in the right direction, and similar "data canary" mechanisms should be considered for other data pipelines (CDC ingestion, carrier data feeds, forecast model training data).
