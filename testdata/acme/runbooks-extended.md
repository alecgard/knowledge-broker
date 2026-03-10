# ACME Org -- Extended Runbooks

These runbooks supplement the core runbook set (RB-001 through RB-006) documented in the main knowledge base. They follow the same format and escalation conventions. All runbooks assume the responder has `kubectl` access to the relevant cluster and Grafana/Loki access via `observability.internal.acme.dev`.

---

## RB-007: Kafka Consumer Lag Spike

**Trigger:** Consumer lag on any production Kafka topic exceeds 500K messages for more than 10 minutes. Alert fires from Grafana rule `kafka_consumer_lag_critical` and pages the owning team via PagerDuty.

**Impact:** Downstream data processing is delayed. Depending on the affected topic, this can cause stale inventory positions (`inventory-updates`), delayed shipment tracking (`shipment-events`), or backed-up screening results (`screening-requests`). Customers may see outdated data in the Nexus dashboard.

**Diagnosis steps:**
1. Open the `Kafka Consumer Lag` Grafana dashboard. Identify which consumer group and topic are affected.
2. Check the consumer group status:
   ```
   kafka-consumer-groups --bootstrap-server msk-prod.internal.acme.dev:9092 \
     --describe --group {consumer-group-name}
   ```
3. Look at the `CURRENT-OFFSET` vs `LOG-END-OFFSET` for each partition. Identify if lag is uniform across partitions or concentrated on specific partitions (hot partition problem).
4. Check if the consumer pods are running:
   ```
   kubectl get pods -n {namespace} -l app={consumer-service} -o wide
   ```
5. Check for repeated rebalances in consumer logs (indicates pods crashing or network issues):
   ```
   kubectl logs -n {namespace} -l app={consumer-service} --since=30m | grep -i "rebalance\|partition revoked\|partition assigned"
   ```
6. Check the broker health in the MSK console. Look for under-replicated partitions or offline partitions.
7. Check if message size has spiked (a customer sending unusually large payloads can slow processing):
   ```
   kafka-run-class kafka.tools.GetOffsetShell \
     --broker-list msk-prod.internal.acme.dev:9092 \
     --topic {topic-name} --time -1
   ```
8. Verify that the consumer service is not CPU or memory constrained:
   ```
   kubectl top pods -n {namespace} -l app={consumer-service}
   ```

**Remediation:**
- If consumer pods are crash-looping, check the logs for the root cause (OOM, deserialization error, downstream dependency timeout) and address accordingly.
- If lag is uniform and consumers are healthy but slow, scale up the consumer deployment:
  ```
  kubectl scale deployment/{consumer-service} --replicas={current + 4} -n {namespace}
  ```
  Ensure replica count does not exceed the number of partitions on the topic (currently 12 for most topics, 24 for `inventory-updates`).
- If lag is concentrated on specific partitions (hot partition), this usually indicates a key skew problem. Check if a single customer is generating disproportionate traffic. Temporary mitigation: increase consumer processing threads:
  ```
  kubectl set env deployment/{consumer-service} -n {namespace} KAFKA_CONSUMER_THREADS=8
  ```
- If the MSK cluster itself is degraded, check AWS Health Dashboard. If a broker is offline, MSK will auto-recover but it may take 15-30 minutes. No manual action needed unless it persists.
- If the lag is caused by a downstream dependency timeout (e.g., PostgreSQL slow queries), fix the dependency first. Scaling consumers will not help and may make it worse.
- After remediation, monitor the lag dashboard to confirm lag is decreasing. It should recover at approximately `(consumer throughput - producer throughput)` messages per second.
- Once lag is cleared, scale consumers back to normal replica count to avoid unnecessary resource usage:
  ```
  kubectl scale deployment/{consumer-service} --replicas={normal-count} -n {namespace}
  ```

**Escalation:** If lag exceeds 2M messages or does not begin recovering within 30 minutes, page the Platform team lead (Marcus Webb). If the affected topic is `inventory-updates` or `shipment-events` and an Enterprise customer is impacted, also notify the relevant CSM.

---

## RB-008: TLS Certificate Expiration

**Trigger:** Checkly synthetic monitor reports TLS certificate expiration within 14 days for any ACME domain. Alert fires in `#security-alerts` Slack channel and pages the Platform team primary on-call.

**Impact:** If a certificate expires, all HTTPS traffic to the affected domain will fail. Browsers will show security warnings, API clients will reject connections, and customer integrations will break. This is a SEV-1 if it affects `api.acme.dev` or `app.acme.dev`.

**Diagnosis steps:**
1. Identify the affected domain from the Checkly alert. Check the certificate details:
   ```
   echo | openssl s_client -servername {domain} -connect {domain}:443 2>/dev/null | openssl x509 -noout -dates -subject -issuer
   ```
2. Determine where the certificate is managed:
   - `*.acme.dev` and `*.api.acme.dev`: AWS Certificate Manager (ACM) with auto-renewal. If ACM auto-renewal failed, check the ACM console for validation status.
   - `*.acmelogistics.com`: Let's Encrypt via cert-manager in Kubernetes. Check cert-manager logs.
   - Internal domains (`*.internal.acme.dev`): cert-manager with internal CA.
3. For ACM certificates, check DNS validation records:
   ```
   aws acm describe-certificate --certificate-arn {arn} --region us-west-2 | jq '.Certificate.DomainValidationOptions'
   ```
4. For cert-manager certificates, check the Certificate resource:
   ```
   kubectl get certificate -A | grep -i {domain-fragment}
   kubectl describe certificate {cert-name} -n {namespace}
   ```
5. Check cert-manager controller logs for errors:
   ```
   kubectl logs -n cert-manager deployment/cert-manager --since=1h | grep -i "error\|failed\|challenge"
   ```
6. If using Let's Encrypt, verify the ACME challenge solver is working. DNS challenges require Route 53 access; HTTP challenges require the solver pod to be reachable.

**Remediation:**
- For ACM auto-renewal failures: Usually caused by a missing or incorrect DNS CNAME validation record in Route 53. Re-create the validation record:
  ```
  aws route53 change-resource-record-sets --hosted-zone-id {zone-id} --change-batch '{
    "Changes": [{
      "Action": "UPSERT",
      "ResourceRecordSet": {
        "Name": "{validation-name}",
        "Type": "CNAME",
        "TTL": 300,
        "ResourceRecords": [{"Value": "{validation-value}"}]
      }
    }]
  }'
  ```
  ACM will re-attempt validation within 30 minutes.
- For cert-manager failures: Restart cert-manager and the solver:
  ```
  kubectl rollout restart deployment/cert-manager -n cert-manager
  kubectl rollout restart deployment/cert-manager-webhook -n cert-manager
  ```
- If certificate is expiring within 24 hours and auto-renewal is not working, manually issue a certificate:
  ```
  kubectl delete certificate {cert-name} -n {namespace}
  kubectl apply -f - <<EOF
  apiVersion: cert-manager.io/v1
  kind: Certificate
  metadata:
    name: {cert-name}
    namespace: {namespace}
  spec:
    secretName: {secret-name}
    issuerRef:
      name: letsencrypt-prod
      kind: ClusterIssuer
    dnsNames:
      - {domain}
  EOF
  ```
- If the certificate has already expired and traffic is failing, immediately switch to a backup certificate stored in AWS Secrets Manager:
  ```
  aws secretsmanager get-secret-value --secret-id acme/certs/{domain}/backup --region us-west-2 | jq -r '.SecretString' > /tmp/cert-backup.json
  kubectl create secret tls {secret-name} --cert=/tmp/tls.crt --key=/tmp/tls.key -n {namespace} --dry-run=client -o yaml | kubectl apply -f -
  kubectl rollout restart deployment/gateway -n platform
  ```
  Clean up the temp files immediately after.

**Escalation:** If a public-facing certificate expires or will expire within 4 hours and remediation is not working, page the Security team lead (Aisha Mohammed) and SRE lead (Tom Bradley). Expired certificates on `api.acme.dev` or `app.acme.dev` are automatically SEV-1.

---

## RB-009: Memory Leak in Go Service (OOM Kill Pattern)

**Trigger:** Kubernetes OOMKilled events detected on any production Go service pod, or memory usage exceeding 85% of pod limits with steady upward trend over 2+ hours. Alert fires from Grafana rule `container_oom_killed` or `memory_usage_high_trend`.

**Impact:** Repeated OOM kills cause service restarts, brief availability gaps, connection drops, and potential data loss for in-flight requests. If multiple pods OOM simultaneously, the service may become unavailable.

**Diagnosis steps:**
1. Identify the affected service and check OOM kill events:
   ```
   kubectl get events -n {namespace} --field-selector reason=OOMKilled --sort-by='.lastTimestamp' | head -20
   ```
2. Check the pod memory usage trend over the last 24 hours in Grafana dashboard `Container Resources by Pod`. Look for a sawtooth pattern (steady increase followed by sharp drop = OOM + restart).
3. Verify the current memory limits for the deployment:
   ```
   kubectl get deployment {service} -n {namespace} -o jsonpath='{.spec.template.spec.containers[0].resources}'
   ```
4. Enable Go pprof profiling on the affected service (all ACME Go services expose pprof on the admin port):
   ```
   kubectl port-forward deployment/{service} -n {namespace} 6060:6060
   ```
   Then capture a heap profile:
   ```
   curl -s http://localhost:6060/debug/pprof/heap > /tmp/{service}-heap.prof
   go tool pprof -top /tmp/{service}-heap.prof
   ```
5. Compare with a baseline heap profile (captured during last release qualification, stored in S3):
   ```
   aws s3 cp s3://acme-profiles/{service}/baseline-heap.prof /tmp/baseline-heap.prof
   go tool pprof -diff_base=/tmp/baseline-heap.prof /tmp/{service}-heap.prof
   ```
6. Check if the leak correlates with specific customer traffic patterns. Look at request rate by customer in Grafana:
   ```
   sum by (customer_id) (rate(http_requests_total{service="{service}"}[5m]))
   ```
7. Check goroutine count for goroutine leaks:
   ```
   curl -s http://localhost:6060/debug/pprof/goroutine?debug=1 | head -50
   ```
   Normal goroutine count for most services is 50-200. If it is in the thousands, there is likely a goroutine leak.
8. Check if the leak was introduced in a recent deployment. Compare the OOM timeline with deployment timestamps in ArgoCD.

**Remediation:**
- Immediate mitigation: increase the memory limit for the affected deployment. Standard memory limits are `512Mi` for lightweight services, `1Gi` for mid-weight, `2Gi` for heavy. Increase by 50%:
  ```
  kubectl patch deployment {service} -n {namespace} --type='json' \
    -p='[{"op": "replace", "path": "/spec/template/spec/containers/0/resources/limits/memory", "value": "3Gi"}]'
  ```
  This buys time but does not fix the leak.
- If the leak is in a known dependency (common culprits: `pgx` connection pool not closing, `net/http` client without response body close, Kafka consumer not committing offsets), apply the known fix from the `acme-go-kit` troubleshooting guide.
- If the leak was introduced in the latest deployment, roll back:
  ```
  kubectl rollout undo deployment/{service} -n {namespace}
  ```
- For goroutine leaks, check for missing context cancellation or channel receivers. Common pattern in ACME services:
  ```
  // BAD: goroutine leak if ctx is never cancelled
  go func() {
      for {
          select {
          case msg := <-ch:
              process(msg)
          }
      }
  }()
  ```
- File a bug with the heap profile attached. Include the pprof diff output and the timeframe of the leak.
- Schedule a rolling restart as a temporary workaround if the fix will take more than a day:
  ```
  kubectl rollout restart deployment/{service} -n {namespace}
  ```
  This resets memory but the leak will recur.

**Escalation:** If the OOM kills are affecting service availability (error rate > 1%), escalate to the owning team lead immediately. If the service is `nexus-api` or `gateway`, also page SRE lead.

---

## RB-010: ClickHouse Disk Space Alert

**Trigger:** ClickHouse Cloud disk usage exceeds 80% on any node. Alert fires from ClickHouse Cloud monitoring and is forwarded to `#beacon-alerts` Slack channel.

**Impact:** If disk reaches 95%, ClickHouse will stop accepting inserts, causing Beacon analytics data pipeline to back up. Existing queries will continue to work until disk is 100% full. Customers will not see recent data in Beacon dashboards and reports.

**Diagnosis steps:**
1. Check current disk usage across nodes:
   ```sql
   SELECT
       hostName() AS host,
       formatReadableSize(total_space) AS total,
       formatReadableSize(free_space) AS free,
       round((1 - free_space / total_space) * 100, 2) AS used_pct
   FROM system.disks
   ```
2. Identify which tables are consuming the most space:
   ```sql
   SELECT
       database,
       table,
       formatReadableSize(sum(bytes_on_disk)) AS size,
       sum(rows) AS total_rows,
       min(min_time) AS oldest_data,
       max(max_time) AS newest_data
   FROM system.parts
   WHERE active
   GROUP BY database, table
   ORDER BY sum(bytes_on_disk) DESC
   LIMIT 20
   ```
3. Check if there is an unexpected data growth spike (bulk import, misconfigured pipeline, duplicate data):
   ```sql
   SELECT
       toStartOfHour(max_time) AS hour,
       table,
       formatReadableSize(sum(bytes_on_disk)) AS size_added,
       sum(rows) AS rows_added
   FROM system.parts
   WHERE active AND modification_time > now() - INTERVAL 24 HOUR
   GROUP BY hour, table
   ORDER BY hour DESC, sum(bytes_on_disk) DESC
   LIMIT 50
   ```
4. Check if TTL policies are executing correctly (Beacon data has a 24-month retention policy):
   ```sql
   SELECT
       database,
       table,
       engine,
       partition_key,
       sorting_key
   FROM system.tables
   WHERE database = 'beacon'
   ```
5. Check for unmerged parts (ClickHouse merge backlog can temporarily inflate disk usage):
   ```sql
   SELECT
       table,
       count() AS parts_count,
       formatReadableSize(sum(bytes_on_disk)) AS total_size
   FROM system.parts
   WHERE active AND table IN (SELECT name FROM system.tables WHERE database = 'beacon')
   GROUP BY table
   HAVING parts_count > 100
   ORDER BY parts_count DESC
   ```
6. Check mutation and alter progress (running mutations consume additional disk):
   ```sql
   SELECT * FROM system.mutations WHERE is_done = 0
   ```

**Remediation:**
- If TTL is not executing, manually trigger TTL cleanup:
  ```sql
  ALTER TABLE beacon.{table_name} MATERIALIZE TTL
  ```
  This can take significant time for large tables. Monitor with:
  ```sql
  SELECT * FROM system.mutations WHERE table = '{table_name}' AND is_done = 0
  ```
- If a specific customer has disproportionate data, check for data pipeline issues (duplicate events, misconfigured CDC):
  ```sql
  SELECT
      customer_id,
      formatReadableSize(sum(length(data))) AS data_size,
      count() AS event_count
  FROM beacon.events
  WHERE created_at > now() - INTERVAL 7 DAY
  GROUP BY customer_id
  ORDER BY sum(length(data)) DESC
  LIMIT 20
  ```
- Drop old partitions that should have been TTL'd:
  ```sql
  ALTER TABLE beacon.{table_name} DROP PARTITION '{partition_id}'
  ```
- If immediate space is needed, optimize tables to force merges and reclaim space:
  ```sql
  OPTIMIZE TABLE beacon.{table_name} FINAL
  ```
  Warning: this is resource-intensive and can affect query performance. Run during low-traffic hours (02:00-06:00 UTC).
- For persistent growth, request a ClickHouse Cloud tier upgrade via Terraform:
  ```
  cd /Users/acme/terraform-infra/clickhouse-prod
  # Update the storage_gib variable in terraform.tfvars
  terraform plan -var="storage_gib=4096"
  terraform apply -var="storage_gib=4096"
  ```
- Verify data pipeline health after cleanup by checking that new data is flowing:
  ```sql
  SELECT max(created_at) AS latest_event FROM beacon.events
  ```

**Escalation:** If disk exceeds 90%, page the Beacon team lead (Sarah Kim). If ClickHouse stops accepting inserts, this becomes SEV-2. Notify SRE and Platform team.

---

## RB-011: Customer SSO SAML Integration Failure

**Trigger:** Customer reports inability to log in via SSO, or `auth-service` logs show repeated SAML assertion validation failures for a specific customer. Alert may also fire from Checkly synthetic login monitor if the test customer account fails.

**Impact:** All users for the affected customer organization cannot access Nexus, Beacon, or Sentinel via SSO. API key-based access is unaffected.

**Diagnosis steps:**
1. Check auth-service logs for the customer's SAML errors:
   ```
   kubectl logs -n platform -l app=auth-service --since=1h | grep -i "saml\|{customer_id}" | tail -50
   ```
2. Identify the specific SAML error type. Common errors:
   - `InvalidSignature`: Customer's IdP signing certificate has changed.
   - `AudienceRestriction`: Our entity ID does not match what the customer configured in their IdP.
   - `NotOnOrAfter`: Clock skew between customer IdP and our auth-service.
   - `InvalidDestination`: ACS URL mismatch.
   - `NoAuthnContext`: Customer IdP is not sending the required authentication context class.
3. Retrieve the current SAML configuration for the customer:
   ```
   curl -s -H "Authorization: Bearer {admin-token}" \
     https://auth.internal.acme.dev/admin/sso/{customer_id} | jq .
   ```
4. Compare the stored IdP metadata with the customer's current metadata. Ask the customer (via CSM) to provide their current IdP metadata URL or XML.
5. Check if the issue started after a recent auth-service deployment:
   ```
   kubectl rollout history deployment/auth-service -n platform
   ```
6. If clock skew is suspected, check the time on auth-service pods:
   ```
   kubectl exec -n platform deployment/auth-service -- date -u
   ```
   Compare with `date -u` on your local machine. More than 30 seconds of drift can cause SAML assertion expiry issues.

**Remediation:**
- For `InvalidSignature` (most common, ~60% of SAML issues): The customer rotated their IdP signing certificate without notifying us. Update the stored certificate:
  ```
  curl -X PUT -H "Authorization: Bearer {admin-token}" \
    -H "Content-Type: application/json" \
    https://auth.internal.acme.dev/admin/sso/{customer_id} \
    -d '{"idp_certificate": "{new-base64-cert}"}'
  ```
  Alternatively, if the customer provides a metadata URL that auto-updates:
  ```
  curl -X PUT -H "Authorization: Bearer {admin-token}" \
    -H "Content-Type: application/json" \
    https://auth.internal.acme.dev/admin/sso/{customer_id} \
    -d '{"metadata_url": "https://customer-idp.example.com/metadata"}'
  ```
- For `AudienceRestriction` or `InvalidDestination`: Provide the customer with the correct ACME SAML configuration:
  - Entity ID: `https://auth.acme.dev/saml/{customer_id}`
  - ACS URL: `https://auth.acme.dev/saml/{customer_id}/callback`
  - Logout URL: `https://auth.acme.dev/saml/{customer_id}/logout`
- For clock skew: Increase the allowed clock skew tolerance (default is 30 seconds):
  ```
  kubectl set env deployment/auth-service -n platform SAML_CLOCK_SKEW_SECONDS=120
  ```
  This is a temporary fix. The customer should fix their IdP clock.
- For any SAML issue as an immediate workaround, enable temporary password login for the customer:
  ```
  curl -X POST -H "Authorization: Bearer {admin-token}" \
    https://auth.internal.acme.dev/admin/customers/{customer_id}/enable-password-fallback \
    -d '{"duration_hours": 24}'
  ```
  This allows users to set a temporary password via email. Communicate this to the CSM.

**Escalation:** If the customer is Enterprise tier and login is blocked for more than 30 minutes, page the Platform team lead. For SSO issues involving HIPAA or SOC 2 audit implications, also notify the Security team.

---

## RB-012: Dead Letter Queue Overflow

**Trigger:** DLQ topic message count exceeds 10K messages on any Kafka DLQ topic (pattern: `{topic-name}.dlq`). Alert fires from Grafana rule `kafka_dlq_overflow` to `#platform-alerts` Slack channel.

**Impact:** Messages in the DLQ represent processing failures. A DLQ overflow indicates a systemic processing issue -- not just occasional bad messages. The original data has been consumed from the source topic but not processed successfully. Until DLQ messages are resolved, the corresponding data is missing from the system (stale inventory, missing shipment events, unprocessed screenings, etc.).

**Diagnosis steps:**
1. Identify which DLQ topics have high message counts:
   ```
   kafka-consumer-groups --bootstrap-server msk-prod.internal.acme.dev:9092 \
     --list | xargs -I{} kafka-consumer-groups --bootstrap-server msk-prod.internal.acme.dev:9092 \
     --describe --group {} 2>/dev/null | grep "\.dlq"
   ```
2. Sample messages from the DLQ to understand the failure pattern:
   ```
   kafka-console-consumer --bootstrap-server msk-prod.internal.acme.dev:9092 \
     --topic {topic-name}.dlq --from-beginning --max-messages 10 \
     --property print.headers=true --property print.timestamp=true
   ```
3. Check the DLQ message headers for error information. ACME services include `X-Error-Message`, `X-Error-Code`, `X-Original-Topic`, and `X-Retry-Count` headers.
4. Categorize the errors. Common patterns:
   - `DESERIALIZATION_ERROR`: Schema changed in producer without consumer update (schema registry mismatch).
   - `PROCESSING_ERROR`: Downstream dependency failure (DB constraint violation, API error).
   - `TIMEOUT_ERROR`: Consumer processing took too long (default 30s per message).
   - `VALIDATION_ERROR`: Message payload fails validation (unexpected nulls, invalid enum values).
5. Check if the DLQ growth started at a specific time and correlate with deployments or customer events:
   ```
   kafka-run-class kafka.tools.GetOffsetShell \
     --broker-list msk-prod.internal.acme.dev:9092 \
     --topic {topic-name}.dlq --time -2
   ```
6. Check the consumer service that feeds this DLQ for error logs:
   ```
   kubectl logs -n {namespace} -l app={consumer-service} --since=2h | grep -i "dlq\|dead.letter\|error" | tail -30
   ```

**Remediation:**
- If caused by schema mismatch: Deploy the consumer with the updated schema. Then replay the DLQ messages:
  ```
  kubectl exec -n {namespace} deployment/{consumer-service} -- \
    /app/tools/dlq-replay --topic {topic-name}.dlq --target-topic {topic-name} --batch-size 500
  ```
- If caused by downstream dependency failure (most common): Fix the dependency issue first (e.g., restore DB connectivity, increase API rate limits). Then replay:
  ```
  kubectl exec -n {namespace} deployment/{consumer-service} -- \
    /app/tools/dlq-replay --topic {topic-name}.dlq --target-topic {topic-name} --batch-size 500 --delay-ms 100
  ```
- If caused by bad data from a specific customer, quarantine those messages and replay the rest:
  ```
  kubectl exec -n {namespace} deployment/{consumer-service} -- \
    /app/tools/dlq-replay --topic {topic-name}.dlq --target-topic {topic-name} \
    --filter-exclude "customer_id={bad_customer_id}" --batch-size 500
  ```
  The quarantined messages need manual investigation. Export them:
  ```
  kafka-console-consumer --bootstrap-server msk-prod.internal.acme.dev:9092 \
    --topic {topic-name}.dlq --from-beginning --max-messages 10000 \
    --property print.headers=true > /tmp/dlq-export-$(date +%Y%m%d).json
  ```
- If the DLQ has grown very large (>100K messages), do not attempt a bulk replay during business hours. Schedule it for the maintenance window (Sunday 02:00-06:00 UTC) and rate-limit the replay:
  ```
  kubectl exec -n {namespace} deployment/{consumer-service} -- \
    /app/tools/dlq-replay --topic {topic-name}.dlq --target-topic {topic-name} \
    --batch-size 100 --delay-ms 500 --max-messages 50000
  ```
- After replay, verify the DLQ is drained:
  ```
  kafka-run-class kafka.tools.GetOffsetShell \
    --broker-list msk-prod.internal.acme.dev:9092 \
    --topic {topic-name}.dlq --time -1
  ```

**Escalation:** If the DLQ overflow affects `inventory-updates.dlq` or `shipment-events.dlq` and an Enterprise customer is impacted, page the owning team lead and notify the CSM. DLQ overflow on `screening-requests.dlq` has compliance implications -- page Sentinel team lead (Raj Patel) immediately.

---

## RB-013: CDN Cache Invalidation After Bad Deployment

**Trigger:** Customers report seeing stale or broken frontend assets after a deployment. Symptoms include broken UI layouts, JavaScript errors in the browser console, or old versions of the application appearing. Reported via Support tickets or `#frontend-alerts` Slack channel.

**Impact:** Customers see a broken or outdated version of the Nexus/Beacon/Sentinel web application. The application may be partially functional with JavaScript errors, or it may fail to load entirely if old HTML references new asset hashes that do not exist in the CDN.

**Diagnosis steps:**
1. Confirm the issue is CDN-related by checking if the asset URLs return correct content:
   ```
   curl -I https://cdn.acme.dev/app/main.{hash}.js
   ```
   Look at the `X-Cache` header. `Hit from cloudfront` means the CDN is serving a cached (possibly stale) version.
2. Check the CloudFront distribution status in AWS Console or via CLI:
   ```
   aws cloudfront list-distributions --query "DistributionList.Items[?Comment=='acme-frontend-prod'].{Id:Id,Status:Status,DomainName:DomainName}"
   ```
3. Check if a recent invalidation was submitted:
   ```
   aws cloudfront list-invalidations --distribution-id {distribution-id} --query "InvalidationList.Items[0:5]"
   ```
4. Verify the S3 origin bucket has the correct assets:
   ```
   aws s3 ls s3://acme-frontend-prod/app/ --region us-west-2 | tail -20
   ```
5. Check the deployment pipeline logs in GitHub Actions for the `forge-ui` repository. Verify that the S3 sync and CloudFront invalidation steps both completed successfully.
6. Check if the issue is regional. CloudFront edge caches update independently. The issue might be resolved in some regions but not others:
   ```
   for region in us-west-2 eu-west-1 ap-southeast-1; do
     echo "=== $region ==="
     curl -s -H "Host: cdn.acme.dev" -I "https://cdn.acme.dev/app/index.html" --resolve "cdn.acme.dev:443:$(dig +short cdn.acme.dev @8.8.8.8 | head -1)" | grep -i "x-cache\|age\|etag"
   done
   ```

**Remediation:**
- Submit a wildcard cache invalidation:
  ```
  aws cloudfront create-invalidation \
    --distribution-id {distribution-id} \
    --paths "/app/*" "/static/*"
  ```
  Invalidation typically completes in 2-5 minutes across all edge locations. Monitor status:
  ```
  aws cloudfront get-invalidation --distribution-id {distribution-id} --id {invalidation-id}
  ```
- If the S3 origin is also stale (deployment failed to upload new assets), re-trigger the deployment:
  ```
  # In the forge-ui repository
  gh workflow run deploy-production.yml --ref main
  ```
- If the issue is urgent and invalidation is slow, temporarily bypass the CDN by directing customers to the origin:
  1. Update Route 53 to point `app.acme.dev` directly to the ALB origin (bypass CloudFront).
  2. This should be a last resort as it increases latency and load on the origin servers.
- If the broken deployment included bad JavaScript, roll back the S3 bucket to the previous version:
  ```
  # List previous versions
  aws s3api list-object-versions --bucket acme-frontend-prod --prefix app/index.html --max-keys 5

  # Restore previous version
  aws s3api copy-object --bucket acme-frontend-prod \
    --copy-source "acme-frontend-prod/app/index.html?versionId={previous-version-id}" \
    --key app/index.html
  ```
  Then invalidate the cache as described above.
- After resolution, verify from multiple browsers/locations that the correct version is being served. Check the `build-id` meta tag in the HTML source matches the expected deployment.

**Escalation:** If the broken frontend affects all customers for more than 15 minutes, escalate to Frontend team lead (Emma Torres) and Release Engineering lead (Viktor Nowak). If the issue is caused by a CDN configuration problem rather than a deployment issue, also involve Platform team.

---

## RB-014: Secrets Rotation Procedure (Quarterly)

**Trigger:** Scheduled quarterly secrets rotation. Calendar event on the first Monday of each quarter (January, April, July, October). Also triggered ad-hoc if a secret is suspected to be compromised.

**Impact:** During rotation, there is a brief window where services may use the old secret until they pick up the new one. Most services support zero-downtime rotation via dual-secret acceptance windows. If rotation is not performed on schedule, we fall out of SOC 2 compliance.

**Diagnosis steps (pre-rotation checklist):**
1. Verify the list of secrets due for rotation:
   ```
   aws secretsmanager list-secrets --filters Key=tag-key,Values=rotation-quarterly \
     --query "SecretList[].{Name:Name,LastRotated:LastRotatedDate,NextRotation:NextRotationDate}" \
     --output table
   ```
2. Identify which services depend on each secret by checking the secret tags:
   ```
   aws secretsmanager describe-secret --secret-id {secret-name} \
     --query "{Name:Name,Tags:Tags}" --output table
   ```
3. Verify that all services support graceful secret reload (check for the `SECRET_RELOAD=true` environment variable):
   ```
   kubectl get deployment -A -o jsonpath='{range .items[*]}{.metadata.namespace}/{.metadata.name}: {range .spec.template.spec.containers[*].env[*]}{.name}={.value} {end}{"\n"}{end}' | grep SECRET_RELOAD
   ```
4. Check the rotation runbook in Confluence for any service-specific notes or dependencies.

**Remediation (rotation procedure):**
- **Step 1: Database credentials** (via HashiCorp Vault, automatic):
  ```
  vault write database/rotate-role/{service-name}
  ```
  Vault issues new credentials and the services pick them up automatically via the Vault agent sidecar. Verify:
  ```
  vault read database/creds/{service-name}
  ```

- **Step 2: API keys for third-party services** (manual):
  - **Carrier API keys** (FedEx, UPS, DHL):
    1. Generate new key in each carrier's developer portal.
    2. Store in Secrets Manager:
       ```
       aws secretsmanager update-secret --secret-id acme/carriers/{carrier}/api-key \
         --secret-string '{"api_key":"{new-key}","api_secret":"{new-secret}"}'
       ```
    3. Restart the carrier integration pods:
       ```
       kubectl rollout restart deployment/carrier-poller-{carrier} -n nexus
       ```
    4. Verify tracking is working: `GET /admin/carriers/{carrier}/status`

  - **Stripe API key:**
    1. Generate new restricted key in Stripe Dashboard.
    2. Update Secrets Manager:
       ```
       aws secretsmanager update-secret --secret-id acme/stripe/api-key \
         --secret-string '{"publishable_key":"{pk}","secret_key":"{sk}"}'
       ```
    3. Restart billing service:
       ```
       kubectl rollout restart deployment/billing-service -n platform
       ```
    4. Verify by checking Stripe webhook test: `POST /admin/billing/test-webhook`

  - **Okta API token:**
    1. Generate new token in Okta admin console.
    2. Update Secrets Manager:
       ```
       aws secretsmanager update-secret --secret-id acme/okta/api-token \
         --secret-string '{"token":"{new-token}"}'
       ```
    3. Restart auth-service:
       ```
       kubectl rollout restart deployment/auth-service -n platform
       ```
    4. Verify SSO login works for a test account.

  - **Claude API key** (for Sentinel classification):
    1. Generate new key in Anthropic Console.
    2. Update Secrets Manager:
       ```
       aws secretsmanager update-secret --secret-id acme/anthropic/api-key \
         --secret-string '{"api_key":"{new-key}"}'
       ```
    3. Restart classification service:
       ```
       kubectl rollout restart deployment/classification-service -n sentinel
       ```
    4. Test classification: `POST /admin/classification/test`

- **Step 3: Internal service-to-service tokens:**
  ```
  kubectl exec -n platform deployment/auth-service -- /app/tools/rotate-service-tokens --all
  ```
  This generates new JWT signing keys and distributes them. Services have a 1-hour grace period to accept tokens signed with the old key.

- **Step 4: Verify all services are healthy after rotation:**
  ```
  kubectl get pods -A | grep -v "Running\|Completed" | grep -v "NAME"
  ```
  Run the E2E test suite against production:
  ```
  gh workflow run e2e-production.yml --ref main
  ```

- **Step 5: Update the rotation log:**
  ```
  aws secretsmanager tag-resource --secret-id {each-secret} \
    --tags Key=last-rotated,Value=$(date -u +%Y-%m-%dT%H:%M:%SZ) \
           Key=rotated-by,Value={your-email}
  ```

**Escalation:** If any service fails to pick up new secrets and is down for more than 10 minutes, page the owning team lead. If rotation is not completed by end of the quarter, notify Security team lead (Aisha Mohammed) for SOC 2 compliance tracking.

---

## RB-015: Elasticsearch Index Corruption / Red Cluster Status

**Trigger:** AWS OpenSearch cluster health status is `red`, or Sentinel screening queries return errors with `index_not_found_exception` or `shard_not_available`. Alert fires from CloudWatch and PagerDuty.

**Impact:** Red cluster status means at least one primary shard is unassigned. Screening queries against the affected index will fail or return incomplete results. If the screening index is affected, denied party screenings will fail, which has compliance implications -- shipments cannot be cleared.

**Diagnosis steps:**
1. Check cluster health:
   ```
   curl -s https://sentinel-es.internal.acme.dev:9200/_cluster/health?pretty
   ```
   Note `status`, `unassigned_shards`, `active_primary_shards`.
2. Identify unassigned shards:
   ```
   curl -s https://sentinel-es.internal.acme.dev:9200/_cat/shards?v&h=index,shard,prirep,state,unassigned.reason,node | grep UNASSIGNED
   ```
3. Check the allocation explanation for each unassigned shard:
   ```
   curl -s -XGET https://sentinel-es.internal.acme.dev:9200/_cluster/allocation/explain?pretty -H 'Content-Type: application/json' -d '{
     "index": "{index-name}",
     "shard": {shard-number},
     "primary": true
   }'
   ```
4. Check node health and disk usage:
   ```
   curl -s https://sentinel-es.internal.acme.dev:9200/_cat/nodes?v&h=name,heap.percent,disk.used_percent,ram.percent,cpu,master,node.role
   ```
5. Check recent cluster events:
   ```
   curl -s https://sentinel-es.internal.acme.dev:9200/_cat/pending_tasks?v
   ```
6. Check AWS OpenSearch console for any ongoing maintenance or node replacements.
7. Check if the issue correlates with the daily sanctions list update (runs at 02:00 UTC):
   ```
   kubectl logs -n sentinel deployment/screening-indexer --since=4h | grep -i "error\|exception\|timeout"
   ```

**Remediation:**
- If a data node is offline and AWS is replacing it, wait 15-20 minutes for auto-recovery. OpenSearch will reassign shards once the replacement node is available.
- If shards are unassigned due to disk watermark (disk > 85%), free up space by deleting old indices:
  ```
  # List indices sorted by size
  curl -s https://sentinel-es.internal.acme.dev:9200/_cat/indices?v&s=store.size:desc&h=index,store.size,docs.count,creation.date.string

  # Delete old audit indices (retain last 90 days)
  curl -XDELETE https://sentinel-es.internal.acme.dev:9200/sentinel-audit-2025.10.*
  ```
  Then reset the disk watermark to allow allocation:
  ```
  curl -XPUT https://sentinel-es.internal.acme.dev:9200/_cluster/settings -H 'Content-Type: application/json' -d '{
    "transient": {
      "cluster.routing.allocation.disk.watermark.low": "87%",
      "cluster.routing.allocation.disk.watermark.high": "90%",
      "cluster.routing.allocation.disk.watermark.flood_stage": "95%"
    }
  }'
  ```
  Remember to reset these to defaults after resolving the disk issue.
- If the index is corrupted and shards cannot be recovered, re-index from the source of truth:
  ```
  # Close the corrupted index
  curl -XPOST https://sentinel-es.internal.acme.dev:9200/{index-name}/_close

  # Delete it
  curl -XDELETE https://sentinel-es.internal.acme.dev:9200/{index-name}

  # Trigger a full reindex from the sanctions list sources
  kubectl exec -n sentinel deployment/screening-indexer -- /app/tools/reindex --full --index={index-name}
  ```
  Full reindex of the screening index takes approximately 20-30 minutes.
- If the cluster is in a bad state and allocation is stuck, try a reroute:
  ```
  curl -XPOST https://sentinel-es.internal.acme.dev:9200/_cluster/reroute?retry_failed=true
  ```
- For persistent cluster issues, scale up the cluster via Terraform:
  ```
  cd /Users/acme/terraform-infra/opensearch-sentinel
  terraform plan -var="data_node_count=5" -var="data_node_instance_type=r6g.2xlarge.search"
  terraform apply
  ```
  Note: OpenSearch blue/green deployment takes 20-40 minutes.
- During any screening index outage, notify the Sentinel team to enable the fallback screening mode (direct API calls to sanctions list providers, slower but functional):
  ```
  curl -XPUT https://sentinel.internal.acme.dev/admin/screening/fallback-mode -H 'Content-Type: application/json' -d '{"enabled": true}'
  ```

**Escalation:** Red cluster status on the screening index is automatically SEV-2 due to compliance implications. Page Sentinel team lead (Raj Patel) and SRE lead (Tom Bradley) immediately. If screening is fully unavailable for more than 15 minutes, escalate to SEV-1 and notify VP Engineering.

---

## RB-016: Cross-Region Failover Procedure (us-west-2 to eu-west-1)

**Trigger:** AWS us-west-2 region experiences a significant outage affecting EKS, RDS, or MSK. Decision to fail over is made by the Incident Commander in consultation with the SRE lead and VP Engineering. This is a manual procedure -- ACME does not have automatic cross-region failover in v1.

**Impact:** During failover, there will be a service disruption of approximately 15-45 minutes depending on complexity. After failover, US customers will experience higher latency (traffic routes to EU). Some data loss is possible for events that occurred after the last RDS cross-region replica sync (replication lag is typically < 1 minute, but can be higher during region degradation).

**Diagnosis steps (pre-failover assessment):**
1. Confirm the us-west-2 outage is regional and not a localized issue:
   - Check AWS Health Dashboard: https://health.aws.amazon.com/
   - Check AWS status page: https://status.aws.amazon.com/
   - Verify from multiple sources (DownDetector, Twitter/X, peer companies on Slack communities).
2. Assess the scope of the outage. If only one AZ is affected, EKS and RDS Multi-AZ failover should handle it without cross-region failover (see RB-005).
3. Check the current state of the eu-west-1 secondary cluster:
   ```
   kubectl --context acme-prod-euw1 get nodes
   kubectl --context acme-prod-euw1 get pods -A | grep -v Running | grep -v Completed | grep -v NAME
   ```
4. Check RDS cross-region read replica lag:
   ```
   aws rds describe-db-instances --region eu-west-1 \
     --query "DBInstances[?ReadReplicaSourceDBInstanceIdentifier!=null].{Id:DBInstanceIdentifier,ReplicaLag:StatusInfos[0].Normal}" \
     --output table
   ```
5. Check MSK Mirror Maker replication lag for cross-region Kafka topics:
   ```
   kubectl --context acme-prod-euw1 exec -n kafka deployment/mirror-maker -- \
     kafka-consumer-groups --bootstrap-server localhost:9092 --describe --group mirror-maker
   ```
6. Estimate the data loss window by checking the replication lag values.

**Remediation (failover procedure):**

**Phase 1: DNS Failover (estimated time: 5 minutes)**
1. Update Route 53 health checks to mark us-west-2 endpoints as unhealthy:
   ```
   aws route53 update-health-check --health-check-id {usw2-api-health-check-id} \
     --inverted --region us-east-1
   ```
2. For any records not using health-check-based routing, manually update DNS:
   ```
   aws route53 change-resource-record-sets --hosted-zone-id {zone-id} --change-batch '{
     "Changes": [{
       "Action": "UPSERT",
       "ResourceRecordSet": {
         "Name": "api.acme.dev",
         "Type": "A",
         "AliasTarget": {
           "HostedZoneId": "{euw1-alb-zone-id}",
           "DNSName": "{euw1-alb-dns}",
           "EvaluateTargetHealth": true
         }
       }
     }]
   }'
   ```
3. Flush CloudFront cache to pick up new DNS:
   ```
   aws cloudfront create-invalidation --distribution-id {distribution-id} --paths "/*"
   ```

**Phase 2: Database Promotion (estimated time: 10-15 minutes)**
1. Promote RDS cross-region read replicas to standalone instances:
   ```
   for db in nexus-api inventory-service shipment-service auth-service relay-control-plane sentinel-api screening-service classification-service; do
     aws rds promote-read-replica-db-instance \
       --db-instance-identifier "${db}-euw1-replica" \
       --region eu-west-1 &
   done
   wait
   ```
   Monitor promotion status:
   ```
   aws rds describe-db-instances --region eu-west-1 \
     --query "DBInstances[].{Id:DBInstanceIdentifier,Status:DBInstanceStatus}" --output table
   ```
2. Update the database connection strings in Kubernetes secrets for the eu-west-1 cluster:
   ```
   kubectl --context acme-prod-euw1 get secrets -A -o name | grep db-credentials | while read secret; do
     # Each secret has a failover variant stored as a secondary key
     ns=$(echo $secret | cut -d/ -f1)
     name=$(echo $secret | cut -d/ -f2)
     kubectl --context acme-prod-euw1 patch secret $name -n $ns \
       --type='json' -p='[{"op": "replace", "path": "/data/DATABASE_URL", "value": "'"$(kubectl --context acme-prod-euw1 get secret $name -n $ns -o jsonpath='{.data.DATABASE_URL_FAILOVER}')"'"}]'
   done
   ```

**Phase 3: Service Activation (estimated time: 5-10 minutes)**
1. Scale up the eu-west-1 services to handle full production traffic (normally they run at reduced capacity for EU-only customers):
   ```
   kubectl --context acme-prod-euw1 apply -f /Users/acme/k8s-configs/failover/full-capacity-euw1.yaml
   ```
   This manifest increases replica counts for all services to match us-west-2 production levels.
2. Restart all deployments to pick up new database connections:
   ```
   kubectl --context acme-prod-euw1 get deployments -A -o custom-columns=NS:.metadata.namespace,NAME:.metadata.name --no-headers | while read ns name; do
     kubectl --context acme-prod-euw1 rollout restart deployment/$name -n $ns
   done
   ```
3. Verify all pods are running:
   ```
   kubectl --context acme-prod-euw1 get pods -A | grep -v Running | grep -v Completed | grep -v NAME
   ```

**Phase 4: Verification (estimated time: 5-10 minutes)**
1. Run the smoke test suite against eu-west-1:
   ```
   ACME_API_URL=https://api-euw1.acme.dev ACME_ENV=failover go test ./tests/smoke/... -v -count=1
   ```
2. Verify key customer flows:
   - Inventory API returns data: `curl -H "Authorization: Bearer {test-token}" https://api.acme.dev/v1/inventory/positions?per_page=1`
   - Shipment tracking works: `curl -H "Authorization: Bearer {test-token}" https://api.acme.dev/v1/shipments?per_page=1`
   - Screening service responds: `POST /v1/screening/check` with test payload
   - SSO login works (test with internal account)
3. Check Grafana dashboards for error rates and latency on the eu-west-1 cluster.
4. Update the status page (status.acme.dev) with the failover status and any data loss window.

**Phase 5: Communication**
1. Comms Lead sends customer notification via StatusPage with:
   - Confirmation that services are restored.
   - Any data loss window (e.g., "events between 14:32 UTC and 14:47 UTC may need to be resent").
   - Expected higher latency for US-based customers.
   - ETA for failback to us-west-2 (usually 24-48 hours after AWS confirms region stability).

**Post-failover: Failback procedure** is documented separately in Confluence (`Engineering/DR/Failback-Procedure`). Failback requires re-establishing RDS replication, syncing any data created during failover, and reversing the DNS changes. It is a planned activity, not an emergency procedure.

**Escalation:** Cross-region failover is automatically SEV-1. The Incident Commander must be the SRE lead (Tom Bradley) or VP Engineering (Dana Chen). All team leads are notified. CEO (Alex Rivera) is notified for customer communication purposes. The decision to fail over requires approval from at least two of: SRE lead, VP Engineering, CTO.
