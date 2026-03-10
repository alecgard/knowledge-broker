# ACME Org — Core Runbooks (RB-001 through RB-006)

## RB-001: Nexus API High Latency

**Trigger:** P99 latency > 500ms for > 5 minutes on `nexus-api`

**Impact:** Customers experience slow dashboard loads and delayed API responses.

**Diagnosis steps:**
1. Check Grafana dashboard: `Nexus API Overview` → look at latency breakdown by endpoint
2. If `/inventory/positions` is slow: likely PostgreSQL query performance. Check `pg_stat_statements` for long-running queries on `inventory-service` DB.
3. If `/shipments/track` is slow: likely carrier API latency. Check `Carrier API Latency` dashboard. If a specific carrier is slow, check their status page.
4. If all endpoints are slow: check Kubernetes node CPU/memory utilization. If nodes are saturated, scale up the node group via Terraform (min nodes is 80, max is 200).
5. Check Kafka consumer lag on `shipment-events` and `inventory-updates` topics. If lag > 100K, the pipeline is backed up.

**Remediation:**
- For DB issues: kill long-running queries (`SELECT pg_terminate_backend(pid) FROM pg_stat_activity WHERE duration > interval '30 seconds'`), then investigate root cause (usually missing index or query plan regression after PostgreSQL minor version update).
- For carrier API issues: enable circuit breaker for the affected carrier (`PUT /admin/carriers/{id}/circuit-breaker?state=open`). Customers will see "Tracking temporarily unavailable" for that carrier.
- For Kafka lag: scale up consumer group replicas. `kubectl scale deployment shipment-consumer --replicas=10 -n nexus`
- For node saturation: `terraform apply -var="min_nodes=120"` in `acme/terraform-infra/eks-prod-usw2/`

**Escalation:** If not resolved in 30 minutes, page SRE secondary. If not resolved in 1 hour, page VP Engineering.

---

## RB-002: Relay Agent Disconnection

**Trigger:** Relay control plane reports agent offline for > 10 minutes. Alert in #relay-alerts Slack channel.

**Impact:** Customer data sync stops. Inventory positions become stale, shipment updates delayed.

**Diagnosis steps:**
1. Check Relay control plane dashboard for the affected customer: `relay-control.internal.acme.dev/customers/{id}`
2. Check agent logs in Loki: `{app="relay-agent", customer_id="{id}"}`
3. Common causes:
   - Customer firewall change blocked outbound connection to `relay.api.acme.dev:443`
   - Agent VM ran out of disk space (logs or temp files)
   - Customer ERP/WMS maintenance window (check customer's maintenance calendar in Launchpad)
   - Agent crash due to OOM (large IDoc processing — see Known Issues above)

**Remediation:**
- If firewall: Contact customer IT via CSM. Provide IP allowlist: `52.40.x.x/24, 52.41.x.x/24` (documented in Relay setup guide).
- If disk space: SSH to agent VM (credentials in Secrets Manager under `relay/customer/{id}/ssh`), clear `/tmp/relay-*` and old log files. Consider increasing log rotation.
- If maintenance: No action needed. Verify agent reconnects after maintenance window. Set reminder.
- If OOM: Restart agent, increase memory limit in agent config (`max_memory_mb: 4096`, default is 2048). File bug for IDoc processing improvement.

**Escalation:** If agent doesn't reconnect within 1 hour and customer is Enterprise tier, page Relay team lead.

---

## RB-003: Forecast Engine Accuracy Degradation

**Trigger:** Weekly forecast accuracy report shows MAPE > 15% for any customer segment. Alert in #intelligence-alerts.

**Impact:** Poor replenishment recommendations, potential stockouts or overstock.

**Diagnosis steps:**
1. Check forecast accuracy dashboard in Beacon: `Forecast Accuracy by Segment`
2. Identify affected customer segment and product categories
3. Common causes:
   - Promotional calendar not updated (customer didn't input upcoming promotions)
   - External signal feed disruption (weather API, events API)
   - Model drift after seasonal transition (e.g., summer → fall)
   - Data quality issue: duplicate or missing sales data from customer ERP

**Remediation:**
- If promotional calendar: Contact CSM to work with customer on updating their promo calendar in Nexus
- If external signal: Check third-party API status. Restart the signal ingestion pipeline: `kubectl rollout restart deployment/forecast-signals -n nexus`
- If model drift: Trigger model retraining for affected segment. `POST /admin/forecast/retrain?segment={id}`. Retraining takes 2–4 hours. Use baseline model (simple exponential smoothing) as fallback during retraining.
- If data quality: Check CDC pipeline health in Relay. Compare record counts between source ERP and Nexus. Run reconciliation job: `POST /admin/inventory/reconcile?customer={id}`

**Escalation:** If MAPE > 20% for an Enterprise customer, escalate to Intelligence Squad lead and CSM immediately.

---

## RB-004: Sentinel Screening Service Latency

**Trigger:** P99 screening latency > 200ms or screening error rate > 0.1%.

**Impact:** Customers experience delays in denied party screening. Could cause shipment holds.

**Diagnosis steps:**
1. Check `Sentinel Screening` Grafana dashboard
2. Check Elasticsearch cluster health: `GET _cluster/health` (should be green)
3. Check if sanctions list update is running (runs daily at 02:00 UTC). During updates, query latency increases.
4. Check Elasticsearch JVM heap usage. If > 85%, GC pressure causes latency spikes.

**Remediation:**
- If during sanctions list update: Wait for completion (usually < 30 minutes). If taking longer, check the update pipeline logs.
- If ES cluster unhealthy: Check for unassigned shards (`GET _cat/shards?v&h=index,shard,prirep,state,unassigned.reason`). If nodes are down, AWS OpenSearch should auto-replace, but may take 15–20 minutes.
- If JVM heap pressure: Increase instance type via Terraform. Current: `r6g.xlarge.search` (32GB heap). Can scale to `r6g.2xlarge.search`.
- If error rate spike: Check if a specific sanctions list source is returning errors. Can temporarily disable that source: `PUT /admin/screening/sources/{id}/disable`

**Escalation:** If not resolved in 15 minutes, page Sentinel team lead. Screening delays have compliance implications.

---

## RB-005: Database Failover

**Trigger:** RDS Multi-AZ failover detected. Alert from AWS CloudWatch and PagerDuty.

**Impact:** 30–60 seconds of database unavailability during failover. Services will experience connection errors.

**Diagnosis steps:**
1. Check RDS events in AWS Console for the affected instance
2. Check service health dashboards — all services depending on the failed-over DB will show error spikes
3. Verify DNS propagation: `dig +short {db-instance}.{region}.rds.amazonaws.com` — should return new IP
4. Check service connection pools are recovering (metrics in Grafana: `db_pool_active_connections`)

**Remediation:**
- Most services have automatic connection pool recovery with pgbouncer. Wait 2–3 minutes after failover for connections to stabilize.
- If connections don't recover: restart affected services (`kubectl rollout restart deployment/{service} -n {namespace}`)
- Investigate root cause of failover: check RDS event log for hardware failure, maintenance window, or storage issue
- Post-failover: verify replication is caught up on new standby (`SELECT * FROM pg_stat_replication`)

**Escalation:** If database doesn't come back within 5 minutes, page SRE lead and Platform team lead.

---

## RB-006: Production Deployment Rollback

**Trigger:** Error rate > 1% or latency regression > 2x baseline within 30 minutes of deployment. Detected by Grafana deployment annotations.

**Impact:** Varies by service. Could affect any product area.

**Diagnosis steps:**
1. Identify which service was deployed: check #deployments Slack channel or ArgoCD dashboard
2. Compare error logs before/after deployment in Loki
3. Check if the issue is in the new code or a dependency that changed

**Remediation:**
1. Immediate rollback via ArgoCD: `argocd app rollback {app-name}` or revert the commit in the GitOps repo and let ArgoCD sync
2. Alternatively: `kubectl rollout undo deployment/{service} -n {namespace}`
3. Notify #engineering Slack channel of the rollback
4. Create incident ticket and assign to the team that owns the service
5. Post-mortem required for any production rollback (template in Confluence: `Engineering/Post-Mortems/Template`)

**Escalation:** Release Engineering lead (Viktor Nowak) should be notified of all rollbacks.
