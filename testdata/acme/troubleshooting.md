# ACME Nexus Platform -- Troubleshooting Guide

This guide covers common issues encountered by the support, engineering, and customer success teams. Each entry follows a standard format: Symptom, Common Causes, Diagnosis Steps, Resolution, and Prevention.

For runbooks related to production incidents (high latency, failover, rollback), see the main internal knowledge base.

---

## TS-001: Relay Sync Shows "Partial Failure"

**Symptom:** A Relay connector sync completes but reports status `partial_failure`. The sync log shows some records processed successfully while others failed. Customer may notice missing or stale data for specific SKUs or transactions.

**Common Causes (ranked by likelihood):**

1. **Schema mismatch**: Customer changed a field in their ERP (renamed a custom field, added a new plant code, changed a UoM) without notifying ACME. The schema mapping in Relay no longer matches the source data.
2. **Data validation failure**: Source records contain values outside expected ranges (negative quantities, future dates, invalid location codes) that fail Nexus validation rules.
3. **Character encoding issues**: Non-UTF-8 characters in product descriptions or address fields, particularly common with SAP systems using legacy codepages (e.g., SAP code page 1100 for Japanese).
4. **Rate limiting on source system**: The source ERP throttled Relay mid-sync, causing some batch requests to fail.

**Diagnosis Steps:**

1. Check the sync log details: `GET /v1/relay/sync-logs?connector_id={id}&status=partial_failure`
2. Examine the `errors` array in the sync log response. Each error includes the `record_id`, `entity_type`, and a human-readable `error` message.
3. For schema mismatches, compare the current schema mapping in Relay Studio (`relay-studio.internal.acme.dev/connectors/{id}/mapping`) against the actual source data structure. Ask the CSM to confirm recent ERP changes with the customer.
4. For data validation failures, check the specific records in the source system. Pull the raw payload from the dead letter queue: `GET /admin/relay/dlq?connector_id={id}&limit=10`
5. For encoding issues, look for error messages containing "invalid UTF-8 sequence" or "encoding error". Check Loki logs: `{app="relay-agent", connector_id="{id}"} |= "encoding"`

**Resolution:**

- **Schema mismatch**: Update the schema mapping in Relay Studio. Test with a dry-run sync: `POST /v1/relay/connectors/{id}/sync` with `{"full_sync": false, "dry_run": true}`. Deploy the updated mapping and trigger a re-sync of failed records.
- **Data validation**: If the source data is genuinely invalid, work with the customer to fix it upstream. If Nexus validation is too strict (e.g., rejecting a valid new plant code), update the validation rules in the connector config.
- **Encoding**: Configure the connector's character encoding setting. For SAP, set `source_encoding: "SAP-1100"` in the connector config. Relay will transcode to UTF-8 automatically.
- **Rate limiting**: Reduce the connector's `batch_size` (default 500, try 200) and increase `request_delay_ms` (default 100, try 500). These settings are in the connector config in Relay Studio.

**Prevention:**

- Require customers to notify their CSM before making ERP schema changes. Include this in the onboarding checklist.
- Set up schema drift detection alerts in Relay (available since Relay 3.1). This compares the source schema on each sync and alerts if new fields appear or existing fields change type.
- Enable automatic encoding detection in the connector config: `auto_detect_encoding: true`.

---

## TS-002: Customer Sees Duplicate Inventory Records

**Symptom:** The customer reports seeing the same SKU listed multiple times for the same location in the Inventory Positions view or API response. Quantities appear inflated.

**Common Causes (ranked by likelihood):**

1. **CDC replay**: A Kafka consumer offset was reset (manually or due to a crash), causing change data capture events to be reprocessed. The inventory service's idempotency check failed because the replayed events had different Kafka message IDs.
2. **Multiple connectors ingesting the same data**: Customer has both a direct ERP connector and an SFTP-based connector feeding the same inventory data.
3. **Location ID aliasing**: The same physical warehouse has different IDs in different source systems (e.g., `WH-01` in SAP vs. `WAREHOUSE_01` in the WMS). Relay treats them as separate locations.
4. **Timezone-related double counting**: Inventory snapshots taken at midnight in different timezones result in the same position being recorded twice with slightly different timestamps.

**Diagnosis Steps:**

1. Query the inventory positions API for the affected SKU: `GET /v1/inventory/positions?sku={sku}&location_id={location}`
2. Check the transaction history for duplicate entries: `GET /v1/inventory/transactions?sku={sku}&location_id={location}&sort=-created_at&per_page=100`
3. Look for entries with the same `reference` field but different transaction IDs -- this indicates replay.
4. In Launchpad, check how many active Relay connectors the customer has. Look for overlapping `entity_types`.
5. Check the location mapping table: `SELECT * FROM location_mappings WHERE customer_id = '{id}'` (via admin DB access).

**Resolution:**

- **CDC replay**: Run the inventory reconciliation job to de-duplicate: `POST /admin/inventory/reconcile?customer={id}`. This compares Nexus positions against the source system and corrects discrepancies. Takes 5-30 minutes depending on catalog size.
- **Multiple connectors**: Disable the redundant connector in Relay Studio. If both are needed for different entity types, configure entity type filters so they do not overlap.
- **Location aliasing**: Add a location alias mapping in Relay Studio (Connector Config > Location Mapping). Map `WAREHOUSE_01` to `WH-01` so records are consolidated.
- **Timezone issue**: Set the connector's `snapshot_timezone` to match the source system's timezone. Ensure CDC mode is used instead of snapshot mode where possible.

**Prevention:**

- During onboarding, the implementation team should audit all data sources and document which connectors feed which entity types. Maintain this in the customer's Launchpad profile.
- Enable the duplicate detection alert in inventory service (config flag `enable_duplicate_detection: true`). This flags positions that have multiple updates with the same source reference within a 5-minute window.
- Require location mapping review as part of Relay connector certification.

---

## TS-003: Beacon Dashboard Shows "No Data" for a Widget

**Symptom:** A customer opens their Beacon dashboard and one or more widgets display "No data available" even though the customer has active data that should populate those widgets.

**Common Causes (ranked by likelihood):**

1. **ClickHouse materialized view lag**: The materialized view that feeds the widget has not refreshed since the last data ingestion. Views refresh on a 5-minute interval but can fall behind during high-load periods.
2. **Widget filter misconfiguration**: The widget has a filter (date range, location, SKU) that excludes all current data. Common after a customer renames locations or changes SKU conventions.
3. **ClickHouse query timeout**: The underlying query exceeds the 30-second timeout. The widget silently shows "No data" instead of an error.
4. **Permission issue**: The user does not have access to the data source the widget queries. Row-level security in Beacon may be filtering out all rows.
5. **Metabase cache stale**: Embedded Metabase serves a cached empty result from a period when the data genuinely had no results.

**Diagnosis Steps:**

1. Open the widget in edit mode (Dashboard > Edit > click widget > View Query). Examine the generated SQL query.
2. Run the same query in Beacon SQL Playground to see if it returns results. Note the execution time.
3. Check ClickHouse materialized view freshness: query `system.mutations` and `system.parts` tables for the relevant table.
4. Check the user's role and data permissions in Launchpad under the customer's user management section.
5. Check Metabase question cache: in the Beacon admin panel at `beacon-admin.internal.acme.dev`, navigate to the question ID and view cache status.

**Resolution:**

- **Materialized view lag**: Force a refresh: `SYSTEM REFRESH VIEW {view_name}` via ClickHouse admin. For repeated issues, increase the refresh interval or add more ClickHouse resources.
- **Filter misconfiguration**: Update the widget filters to match current data. Walk the customer through the dashboard editor to correct the configuration.
- **Query timeout**: Optimize the query. Common fixes: narrow the date range, add pre-aggregation, or create a dedicated materialized view for the widget. For immediate relief, increase the customer's query timeout (Enterprise only, up to 120 seconds).
- **Permission issue**: Check and update the user's Beacon role in Launchpad. Beacon roles are separate from Nexus roles.
- **Metabase cache**: Clear the cache for the affected question via Beacon admin panel, or wait for the cache TTL (default 1 hour) to expire.

**Prevention:**

- Add monitoring for ClickHouse materialized view freshness. Alert if any view is more than 15 minutes behind.
- Include widget validation in the dashboard builder -- warn users if a filter combination returns zero results at save time.
- Document that Beacon roles must be configured separately from Nexus roles during onboarding.

---

## TS-004: Scheduled Report Emails Not Being Delivered

**Symptom:** Customer has configured scheduled reports (daily, weekly, or monthly) in Beacon, but recipients are not receiving the emails. Reports may show as `complete` in the Reports list but no email arrives.

**Common Causes (ranked by likelihood):**

1. **Email blocked by recipient's spam filter**: ACME report emails come from `reports@notifications.acme.dev`. Some corporate email gateways flag automated emails with PDF/Excel attachments.
2. **SES sending quota exceeded**: Amazon SES has a per-account sending rate. During peak report generation (Monday mornings, first of the month), the queue can back up.
3. **Report generation succeeded but email delivery failed**: The report-generator service handles PDF/Excel generation separately from email delivery. The report file exists in S3 but the email step errored.
4. **Recipient email address typo**: The email address in the report configuration has a typo or the employee has left the organization.
5. **S3 pre-signed URL expired**: For large reports (>10MB), the email contains a download link instead of an attachment. If the email is delayed in queue, the pre-signed URL may expire before the recipient clicks it.

**Diagnosis Steps:**

1. Check the report status in Beacon: `GET /v1/reports?status=complete&type={type}&created_after={date}`
2. Check the email delivery logs in the report-generator service: `{app="report-generator"} |= "email" |= "{report_id}"`
3. Check SES sending statistics in AWS Console > SES > Account dashboard. Look for bounces, complaints, or throttling.
4. Ask the customer to check their spam/junk folder and search for emails from `reports@notifications.acme.dev`.
5. Verify the recipient email addresses in the report configuration: `GET /v1/reports/{id}` and check the `recipients` field.

**Resolution:**

- **Spam filter**: Ask the customer's IT team to allowlist `reports@notifications.acme.dev` and the sending IP range (available in ACME's SPF record). Provide DKIM/DMARC records for verification.
- **SES quota**: If quota is being hit, contact the SRE team to request a limit increase from AWS. Short-term mitigation: stagger report generation times so they do not all fire at the same hour.
- **Email delivery failure**: Retry the email delivery without regenerating the report: `POST /admin/reports/{id}/resend`. Check the report-generator logs for the specific SMTP error.
- **Typo in email**: Update the report recipient list via `PUT /admin/reports/{id}` or have the customer update it in the Beacon UI.
- **Expired URL**: Regenerate the download link: `GET /v1/reports/{id}/download`. The customer can also download from the Beacon UI directly.

**Prevention:**

- Implement email delivery confirmation tracking with SES delivery notifications. Surface delivery status in the Reports UI.
- Set up monitoring for SES bounce rate. Alert if bounce rate exceeds 3%.
- Add email address validation (MX record check) when customers configure report recipients.
- For large reports, generate the download link at click time rather than at email send time.

---

## TS-005: Webhook Delivery Failures (Retries Exhausting)

**Symptom:** Customer reports not receiving webhook notifications. The webhook shows `failure_count > 0` and may have been automatically deactivated after 5 consecutive failures.

**Common Causes (ranked by likelihood):**

1. **Customer endpoint is down or unreachable**: The webhook URL returns 5xx errors, connection timeouts, or DNS resolution failures.
2. **SSL certificate issue**: The customer's webhook endpoint has an expired or self-signed SSL certificate. ACME's webhook delivery requires valid TLS.
3. **Customer endpoint returning non-2xx**: The endpoint processes the webhook but returns a 301 redirect, 401, or 403. ACME treats any non-2xx as a failure.
4. **Payload size exceeds customer's request limit**: Some webhook endpoints have request body size limits. Large payloads (e.g., shipment events with many items) may be rejected.
5. **IP allowlisting**: Customer's firewall blocks ACME's webhook delivery IPs.

**Diagnosis Steps:**

1. Check webhook status: `GET /v1/webhooks` -- look at `failure_count` and `active` status.
2. Send a test event: `POST /v1/webhooks/{id}/test` with `{"event": "shipment.status_changed"}`. Check the response for `delivery_status`, `response_code`, and `error`.
3. Check webhook delivery logs: in Loki, `{app="nexus-api"} |= "webhook_delivery" |= "{webhook_id}"`
4. If SSL issue is suspected, test the endpoint externally: `curl -vI https://customer.example.com/webhooks/acme` to inspect the certificate chain.
5. Verify ACME's webhook delivery IPs are allowlisted: current IPs are `52.40.128.0/24` and `52.41.64.0/24` (documented at `docs.acme.dev/webhooks/ip-allowlist`).

**Resolution:**

- **Endpoint down**: Notify the customer via their CSM. Once the endpoint is restored, reactivate the webhook: `PUT /admin/webhooks/{id}/activate` and trigger a test delivery.
- **SSL certificate**: Inform the customer their SSL certificate needs renewal. ACME does not support self-signed certificates for webhook delivery. Customer must use a certificate from a trusted CA.
- **Non-2xx response**: Work with the customer's engineering team to ensure their endpoint returns HTTP 200 for successful processing. Redirects (3xx) should be avoided; use the final URL directly.
- **Payload size**: Reduce payload size by configuring webhook payload filters (available in webhook settings). Alternatively, the customer can increase their endpoint's request size limit.
- **IP allowlist**: Provide the customer with ACME's current webhook delivery IP ranges for their firewall configuration.

**Prevention:**

- During webhook setup, always test with `POST /v1/webhooks/{id}/test` before relying on the webhook for production events.
- Document ACME's webhook delivery IP ranges in the customer onboarding materials.
- Implement webhook health dashboard in Beacon so customers can monitor their webhook delivery success rate proactively.
- Consider adding mTLS support for webhook delivery (on the roadmap for Q3 2026).

---

## TS-006: Sentinel Classification Returns "Low Confidence" for Everything

**Symptom:** A customer submits products for HS code classification and all results come back with confidence scores below 0.7, triggering manual review for every item. This defeats the purpose of AI-assisted classification.

**Common Causes (ranked by likelihood):**

1. **Poor product descriptions**: The product descriptions submitted are too vague, use internal codes instead of descriptive text, or are in a non-English language that the model handles less well.
2. **Unusual product category**: The customer deals in niche products (specialty chemicals, complex machinery) that the classification model has limited training data for.
3. **Destination country mismatch**: The HS code system has country-specific variations. If the destination country requires a sub-heading that differs significantly from the general 6-digit HS code, confidence drops.
4. **Model version regression**: A recent model update (Sentinel uses Claude claude-sonnet-4-5-20250514) may have degraded performance for certain product categories.
5. **Claude API degradation**: If the Claude API is experiencing latency or errors, the classification service may fall back to a simpler heuristic model with inherently lower confidence.

**Diagnosis Steps:**

1. Pull a sample of recent low-confidence classifications: `GET /v1/classifications?confidence_max=0.7&page=1&per_page=20`
2. Review the `product_description` fields. Are they descriptive enough? Compare against high-confidence classifications from other customers for similar product types.
3. Check if the issue is specific to a destination country: `GET /v1/classifications?confidence_max=0.7&country={code}`
4. Check the classification service health: Grafana dashboard `Sentinel Classification` > look for increased latency or error rate on the Claude API integration.
5. Check the current model version in the classification response: `model_version` field. Compare against the last known good version.

**Resolution:**

- **Poor descriptions**: Work with the customer to enrich their product descriptions. Provide the "Classification Best Practices" guide (available at `docs.acme.dev/sentinel/classification-guide`). Key improvements: include material composition, intended use, dimensions, and weight.
- **Niche products**: Submit the customer's product catalog to the Sentinel team for model fine-tuning evaluation. In the interim, use the Sentinel Review Queue for manual classification by trained customs brokers.
- **Country-specific issues**: Verify the correct country-specific HS schedule is configured. Some countries (e.g., India, Brazil) have extensive sub-headings that require additional product details.
- **Model regression**: Roll back to the previous model version: `PUT /admin/classification/model?version={previous_version}`. File a bug with the Sentinel team.
- **Claude API issues**: Check Claude API status at `status.anthropic.com`. If degraded, the classification service should automatically queue requests for retry. Force a retry: `POST /admin/classifications/retry?batch_id={id}`

**Prevention:**

- Include product description quality checks in the classification submission flow. Reject submissions that are too short (<20 characters) or lack key attributes.
- Set up monitoring for average classification confidence by customer. Alert if a customer's 7-day rolling average drops below 0.75.
- Maintain a classification golden dataset for regression testing when the Claude model is updated.

---

## TS-007: Customer API Returns 401 After Key Rotation

**Symptom:** Customer was using the API normally, rotated their API key (either manually or via automated rotation), and now all API requests return HTTP 401 Unauthorized. The new key does not work.

**Common Causes (ranked by likelihood):**

1. **Key not propagated to all integration points**: Customer rotated the key in the Nexus UI but forgot to update it in their ERP integration, scripts, or CI/CD pipelines.
2. **Redis cache stale**: The auth service caches API key validations in Redis with a 5-minute TTL. After key rotation, the old key may still be cached as valid and the new key as unknown for up to 5 minutes.
3. **Key copied with leading/trailing whitespace**: When copying the new key from the UI, invisible whitespace characters were included.
4. **Key scope mismatch**: The new key was created with a more restrictive scope (e.g., `read` instead of `read_write`) than the old key.
5. **IP allowlist on new key**: The new key was created with an `allowed_ips` restriction and the customer's request is coming from a different IP.

**Diagnosis Steps:**

1. Ask the customer to confirm which API key they are using: `GET /v1/api-keys` (using a working admin session in the UI).
2. Check the key's status: is it `active` or `expired`?
3. Check auth-service logs for the rejected request: `{app="auth-service"} |= "401" |= "{customer_id}"`. The log will include the rejection reason (invalid key, wrong scope, IP not allowed).
4. Ask the customer to try the request with `curl -v` and share the full response headers. Look for `X-Auth-Error` header which contains a machine-readable error code.
5. If Redis cache is suspected, check the cache entry: `redis-cli GET "auth:apikey:{key_prefix}"`.

**Resolution:**

- **Not propagated**: Help the customer identify all places their old API key was configured. Provide a list of recent API calls grouped by source IP to help them find integration points: `GET /admin/api-keys/{id}/usage?group_by=source_ip`
- **Redis cache**: Wait 5 minutes for the cache to expire, or flush the specific cache entry: `redis-cli DEL "auth:apikey:{old_key_prefix}"`. Do not flush the entire auth cache.
- **Whitespace**: Ask the customer to trim the key. Suggest they use the "Copy" button in the UI rather than manual selection.
- **Scope mismatch**: Check the new key's scope and compare with the endpoints being called. Update the key scope if needed (requires creating a new key -- scopes cannot be changed after creation).
- **IP allowlist**: Check the key's `allowed_ips` configuration and the customer's actual source IPs.

**Prevention:**

- Add a "Test Key" button in the API Keys UI that makes a simple authenticated request and confirms the key works.
- Display a warning during key rotation listing all active integrations that will be affected.
- Include IP allowlist information prominently in the key creation flow.
- Add trailing/leading whitespace stripping in the auth service key validation.

---

## TS-008: Mobile App Shows Blank Screen After Update

**Symptom:** Customer users update the ACME Nexus mobile app (React Native) to the latest version and see a blank white screen on launch. The app does not crash but nothing renders.

**Common Causes (ranked by likelihood):**

1. **Stale JavaScript bundle cache**: React Native's Hermes engine cached the old JS bundle and the new bundle fails to load cleanly. Common on Android where the bundle is cached aggressively.
2. **API version mismatch**: The new app version expects a response field that the backend has not deployed yet (or vice versa -- a breaking backend change that the old app handled but the new app does not).
3. **Authentication token format changed**: The app update changed the JWT token format or storage location, and the locally stored token is incompatible.
4. **Feature flag misconfiguration**: A feature flag that gates the new UI is not enabled for the customer's account, causing the rendering logic to hit an unhandled code path.
5. **CodePush OTA update corruption**: If the update was delivered via CodePush (OTA), the downloaded bundle may be corrupted or incomplete.

**Diagnosis Steps:**

1. Check if the issue is widespread or limited to specific devices/OS versions. Ask the customer how many users are affected.
2. Ask an affected user to: force-quit the app, clear the app cache (Settings > Apps > ACME Nexus > Clear Cache on Android, or delete and reinstall on iOS), and relaunch.
3. Check mobile app crash reporting in Sentry (project: `acme-mobile`). Filter by the new app version. Look for JavaScript errors.
4. Check the feature flag configuration for the customer's account in Launchpad: Customers > {name} > Feature Flags.
5. If CodePush is involved, check the deployment status: `appcenter codepush deployment list -a acme/nexus-mobile-ios` (and android equivalent).

**Resolution:**

- **Stale cache**: Have users clear the app cache and restart. If widespread, push a CodePush update that forces a cache clear on startup.
- **API mismatch**: Coordinate with the backend team. If the backend change is already deployed, the mobile app needs a hotfix to handle the new response format. If the backend change is pending, expedite the deploy.
- **Auth token**: Clear the app's local storage. Users will need to log in again. For a permanent fix, add token migration logic in the next app update.
- **Feature flag**: Enable the required feature flag for the customer, or if the flag should not be enabled, roll back the mobile app update via CodePush.
- **CodePush corruption**: Force a fresh CodePush download: roll out the same bundle version again (CodePush treats it as a new release). Users will get a clean download on next launch.

**Prevention:**

- Include a "minimum API version" check on app launch. If the backend API version is incompatible, show a human-readable message instead of a blank screen.
- Test new app versions against all active feature flag combinations in staging before release.
- Add a "health check" screen that loads first and verifies API connectivity, auth token validity, and feature flag state before rendering the main app.

---

## TS-009: Grafana Dashboard Showing Gaps in Metrics

**Symptom:** Internal Grafana dashboards show gaps (missing data points) in time series charts. This affects monitoring accuracy and can mask real issues.

**Common Causes (ranked by likelihood):**

1. **Prometheus scrape failures**: Prometheus failed to scrape metrics from one or more targets during the gap period. Common cause: target pods were being rolled out (deployment update) and were temporarily unavailable.
2. **Pod restarts reset counters**: Prometheus counters reset to zero when pods restart. If the restart happens between scrapes, the rate() function produces gaps or negative spikes.
3. **Grafana query timeout**: The PromQL query is too expensive and times out for the selected time range. Grafana shows gaps where query fragments timed out.
4. **Prometheus storage issue**: Prometheus local storage filled up or experienced write-ahead log corruption. Check if gaps affect all metrics or just some.
5. **Network partition**: A brief network issue between Prometheus and the scrape targets caused missed scrapes.

**Diagnosis Steps:**

1. Check the Prometheus Targets page (`prometheus.internal.acme.dev/targets`). Look for targets in `DOWN` state or with high scrape duration.
2. Check if the gaps correlate with deployment times. Cross-reference with the #deployments Slack channel or ArgoCD deployment history.
3. Run the Grafana query manually in Prometheus's expression browser. If it times out, the query needs optimization.
4. Check Prometheus storage: `promtool tsdb analyze /prometheus/data`. Look for compaction errors or high churn.
5. Check node-level network metrics during the gap period for packet loss or connectivity issues.

**Resolution:**

- **Scrape failures during rollout**: Configure `maxUnavailable: 0` in the deployment's rolling update strategy so old pods are not terminated until new ones are ready and being scraped.
- **Counter resets**: Use `rate()` with a range that spans at least 2x the scrape interval (e.g., `rate(metric[2m])` for a 30s scrape interval). Consider using recording rules for expensive rate calculations.
- **Query timeout**: Add recording rules to pre-compute expensive aggregations. Reduce the cardinality of high-cardinality labels. Increase Grafana's query timeout for specific dashboards if needed.
- **Storage issue**: If WAL is corrupted, restart Prometheus with `--storage.tsdb.wal-recovery-mode=best-effort`. If disk is full, increase the PVC size or reduce retention (current: 30 days).
- **Network partition**: No immediate fix needed if transient. If recurring, investigate the network path between Prometheus and the affected targets.

**Prevention:**

- Use `PodDisruptionBudget` resources to ensure minimum availability during rollouts.
- Set up Prometheus self-monitoring alerts: alert if scrape success rate drops below 99%.
- Use Thanos or Prometheus remote write to a durable backend for long-term metric storage with gap filling.
- Regularly review recording rules and cardinality to keep queries performant.

---

## TS-010: Customer Data Appearing in Wrong Region

**Symptom:** An EU customer's data (inventory positions, shipment records, or screening results) appears to be served from the US region. This is a potential GDPR compliance issue.

**Common Causes (ranked by likelihood):**

1. **Incorrect region tag on customer account**: The customer's account was provisioned with `region: us-west-2` instead of `region: eu-west-1` in the account configuration.
2. **DNS routing issue**: The customer's API requests are being routed to the US endpoint instead of the EU endpoint. The customer should use `eu.api.acme.dev` but is using `api.acme.dev`.
3. **Cross-region replication misconfiguration**: Data replication between regions is copying data that should remain in EU to the US cluster.
4. **Relay agent pointing to wrong region**: The Relay agent installed at the customer site is configured to connect to `relay.api.acme.dev` (US) instead of `eu.relay.api.acme.dev`.

**Diagnosis Steps:**

1. Check the customer's account configuration in Launchpad: Customers > {name} > Settings > Region. Verify it shows `eu-west-1`.
2. Check which API endpoint the customer is hitting. Ask for the base URL they are using. Check the `X-Served-By` response header which includes the region identifier.
3. Query the database directly in both regions to see where the customer's data actually resides. In the EU cluster: `SELECT count(*) FROM inventory_positions WHERE customer_id = '{id}'`. Repeat for the US cluster.
4. Check the Relay agent configuration: `GET /admin/relay/agents/{customer_id}` -- look at the `control_plane_url` field.
5. Check CloudFront distribution routing rules for any geo-routing misconfigurations.

**Resolution:**

- **Incorrect region tag**: Update the account region in Launchpad (requires super-admin). Trigger a data migration: `POST /admin/customers/{id}/migrate-region?target=eu-west-1`. This is a multi-hour process that copies data to the correct region and then purges it from the wrong region.
- **DNS routing**: Provide the customer with the correct EU endpoint URLs. Update their API key configuration to be region-scoped.
- **Replication misconfiguration**: Check the Kafka MirrorMaker configuration. Ensure the customer's topics are excluded from cross-region replication. Fix the topic filter pattern.
- **Relay agent**: Push a configuration update to the agent via the control plane: `PUT /admin/relay/agents/{customer_id}/config` with the correct `control_plane_url`.

**Prevention:**

- Add region validation at account creation time. Require explicit confirmation for EU customers.
- Implement data residency checks in CI/CD: automated tests that verify EU customer data does not exist in US databases.
- Add a `X-Data-Region` header to all API responses so customers can verify their data is being served from the correct region.
- Tag Kafka topics with region metadata and add MirrorMaker filters that prevent cross-region replication of region-specific topics.

---

## TS-011: Slow Initial Sync for New Relay Connector

**Symptom:** A newly onboarded customer's initial Relay sync is taking significantly longer than the estimated 6-8 hours. The sync may be running for 24+ hours with no sign of completion.

**Common Causes (ranked by likelihood):**

1. **Large catalog size**: Customer has 100K+ SKUs and the initial sync must process all historical inventory positions, transactions, and shipment records. The estimated time was based on a typical 10K-50K SKU catalog.
2. **Source system rate limiting**: The customer's ERP or WMS is throttling Relay's data extraction requests. NetSuite is particularly aggressive (10 req/sec SuiteTalk limit).
3. **Network bandwidth limitation**: The customer's network connection between the Relay agent and their ERP is slow (common with on-premise ERPs connected via VPN).
4. **Inefficient data extraction queries**: The connector is pulling data in small pages when the source system supports bulk export.
5. **Kafka consumer lag**: The sync is extracting data fast but the downstream Kafka consumers cannot keep up with the ingestion volume.

**Diagnosis Steps:**

1. Check the sync progress: `GET /v1/relay/connectors/{id}/status`. Look at `records_synced_24h` and `throughput_records_per_sec`.
2. Check for rate limiting errors in sync logs: `GET /v1/relay/sync-logs?connector_id={id}&status=partial_failure`
3. Check the Relay agent's resource utilization: `GET /v1/relay/connectors/{id}/status` -- look at `agent.memory_usage_mb` and `agent.cpu_percent`.
4. Check Kafka consumer lag for the customer's ingestion topics: Grafana dashboard `Kafka Consumer Lag` filtered by customer ID.
5. Estimate total data volume: ask the customer or the implementation team for total record counts in the source system.

**Resolution:**

- **Large catalog**: Increase the Relay agent's parallelism: set `sync_workers: 8` (default 4) and `batch_size: 1000` (default 500) in the connector config. If the source system can handle it, this roughly doubles throughput.
- **Rate limiting**: Reduce request rate to stay within limits. For NetSuite, use the SuiteQL bulk query endpoint instead of individual record fetches. For SAP, use IDoc batch export instead of RFC calls.
- **Network bandwidth**: If VPN is the bottleneck, work with the customer's IT team to increase VPN bandwidth or set up a dedicated connection. Alternatively, enable compression in the Relay agent: `compression: gzip`.
- **Inefficient queries**: Engage the Relay team to optimize the connector's extraction queries for the specific source system version. This may require a connector config change or a connector code update.
- **Kafka lag**: Scale up the Kafka consumer group: `kubectl scale deployment inventory-consumer --replicas=10 -n nexus`. Temporarily increase the consumer's batch size.

**Prevention:**

- During the sales process, capture the customer's catalog size and data volume. Use this to provide accurate initial sync time estimates.
- Pre-provision extra Kafka consumer capacity before large customer onboarding.
- Document per-connector initial sync benchmarks (records/second by source system type) so implementation teams can set accurate expectations.
- Consider offering a "bulk import" path for initial loads that bypasses the real-time pipeline.

---

## TS-012: Forecast Showing Negative Demand Values

**Symptom:** The forecast engine (Oracle) generates daily forecasts with negative predicted demand values for some SKUs. Demand should always be >= 0.

**Common Causes (ranked by likelihood):**

1. **Returns exceeding sales**: If the historical data includes product returns as negative demand signals, periods with high return rates can cause the model to predict negative net demand.
2. **Data quality issue in training data**: The CDC pipeline ingested negative quantities (e.g., inventory adjustments recorded as negative demand) that corrupted the training data.
3. **Seasonal decomposition artifact**: The model's seasonal component can produce negative values when deseasonalized demand is very low and the seasonal adjustment is large.
4. **Model extrapolation for new SKUs**: SKUs with fewer than 90 days of history have insufficient data for the model to learn stable patterns. The model may extrapolate into negative territory.

**Diagnosis Steps:**

1. Query the forecast results for the affected SKU: `GET /v1/forecast/{forecast_id}` and look at the `daily_forecast` array for negative `predicted_demand` values.
2. Check the historical sales data for the SKU: pull the last 6 months of demand data from the forecast engine's training dataset (via Beacon SQL Playground): `SELECT date, demand FROM forecast_training_data WHERE sku = '{sku}' ORDER BY date DESC LIMIT 180`
3. Look for negative demand values in the training data. If found, trace them to the source transaction in the inventory transaction log.
4. Check the SKU's history length: if less than 90 days, the model may not have enough data.
5. Check the model version and whether a recent update introduced a regression.

**Resolution:**

- **Returns in data**: Configure the forecast engine to separate returns from demand. Set `returns_handling: separate` in the customer's forecast config. This treats returns as an independent signal rather than negative demand.
- **Data quality**: Correct the bad data in the training dataset. Run `POST /admin/forecast/clean-training-data?customer={id}&sku={sku}` to remove negative entries. Trigger a model retrain.
- **Seasonal artifact**: Apply a floor of zero to forecast output. This is configurable per customer: `POST /admin/forecast/config?customer={id}` with `{"demand_floor": 0}`. The model will clamp all predictions to >= 0.
- **New SKU**: Switch new SKUs (< 90 days history) to the baseline model (exponential smoothing) which is more stable with limited data. Set `new_sku_model: baseline` in the forecast config.

**Prevention:**

- Enable the demand floor (value = 0) by default for all customers. Negative demand forecasts are almost never actionable.
- Add data validation in the CDC pipeline to flag and quarantine negative demand values before they enter the training dataset.
- Add a minimum history threshold (default 90 days) before using the Oracle ML model. Use baseline model for SKUs below the threshold.

---

## TS-013: Shipment Stuck in "Booked" Status for >48 Hours

**Symptom:** A shipment was created (either via API or through a Relay connector) and has been in "Booked" status for more than 48 hours with no tracking events. The carrier has not picked up the shipment.

**Common Causes (ranked by likelihood):**

1. **Carrier API not returning tracking data**: The shipment was booked but the carrier has not yet scanned the package. This is normal for pre-generated labels where the physical handoff has not happened.
2. **Invalid tracking number**: The tracking number provided does not match any shipment in the carrier's system. Typo in the tracking number or the carrier's system has not registered it yet.
3. **Carrier integration disabled or erroring**: The carrier polling job for this specific carrier is failing. Check if other shipments with the same carrier are also stuck.
4. **Shipment was cancelled at the carrier but not in Nexus**: The shipper cancelled with the carrier directly, but the cancellation was not propagated back to Nexus.
5. **Time zone issue**: The shipment was booked with a future `expected_ship_date` and the system is waiting for that date to start polling the carrier.

**Diagnosis Steps:**

1. Check the shipment details: `GET /v1/shipments/{id}`. Note the `carrier`, `tracking_number`, and `expected_ship_date`.
2. Verify the tracking number directly with the carrier's tracking website or API.
3. Check the carrier polling job status: `kubectl get cronjobs -n nexus | grep carrier-poll-{carrier}`. Check the job's recent runs and logs.
4. Check if other shipments with the same carrier are also stuck: `GET /v1/shipments?carrier={carrier}&status=booked&created_before={48h_ago}`
5. Check the `expected_ship_date` -- if it is in the future, the system may be intentionally not polling yet.

**Resolution:**

- **Not yet scanned**: This is normal behavior. Inform the customer that tracking updates will appear once the carrier scans the package. If the customer confirms the package was picked up, manually trigger a tracking check: `POST /admin/shipments/{id}/check-tracking`.
- **Invalid tracking number**: Update the tracking number: `PUT /v1/shipments/{id}` with the correct `tracking_number`. If the shipment does not exist at the carrier, contact the customer to confirm the shipment status.
- **Carrier integration failing**: Follow the carrier tracking troubleshooting guide. Restart the polling job if needed: `kubectl create job carrier-poll-{carrier}-manual --from=cronjob/carrier-poll-{carrier} -n nexus`.
- **Cancelled at carrier**: Update the shipment status to cancelled: `POST /v1/shipments/{id}/cancel` with reason `carrier_issue`.
- **Future ship date**: No action needed if the ship date has not arrived. If the customer wants immediate tracking, remove the `expected_ship_date` or set it to today.

**Prevention:**

- Add an alert for shipments stuck in "Booked" status for more than 72 hours. Surface this in the Nexus dashboard as an exception.
- Validate tracking numbers against carrier format patterns at creation time (e.g., UPS tracking numbers follow a specific format).
- Start carrier polling immediately regardless of `expected_ship_date`, but at a reduced frequency (once per hour instead of every 5-10 minutes).

---

## TS-014: ClickHouse Query Returning Different Results Than PostgreSQL

**Symptom:** A customer runs a query in Beacon's SQL Playground (backed by ClickHouse) and gets different numbers than what they see in the Nexus API (backed by PostgreSQL). For example, inventory counts or shipment totals do not match.

**Common Causes (ranked by likelihood):**

1. **Replication lag**: ClickHouse is populated from PostgreSQL via a CDC pipeline. There is an inherent delay (typically 1-5 minutes, but can grow to 30+ minutes during high load). The customer is comparing real-time PostgreSQL data with slightly stale ClickHouse data.
2. **Aggregation differences**: ClickHouse uses approximate algorithms for some aggregation functions (`uniq()` instead of `COUNT(DISTINCT)`). Results may differ by up to 2% for high-cardinality counts.
3. **Timezone handling**: ClickHouse stores timestamps in UTC and converts at query time. If the customer's query does not specify a timezone, they may get different date-boundary results than PostgreSQL which uses the customer's configured timezone.
4. **Soft deletes not replicated**: Nexus API filters out soft-deleted records automatically. The ClickHouse replica may still include them if the CDC pipeline does not replicate soft-delete events.
5. **Schema drift**: A new column was added to PostgreSQL but the ClickHouse table has not been updated. Queries that depend on the new column fail silently or use default values.

**Diagnosis Steps:**

1. Check the ClickHouse replication lag: Grafana dashboard `Beacon Data Pipeline` > look at `replication_lag_seconds` metric.
2. Ask the customer for the exact queries they are running in both systems. Compare the SQL.
3. Check if the discrepancy is consistent (always off by the same amount) or variable (changes over time). A consistent offset suggests a data issue; a variable offset suggests a timing issue.
4. Run the ClickHouse query with `FINAL` keyword to force merge of all parts: `SELECT ... FROM table FINAL WHERE ...`. If this matches PostgreSQL, the issue is unmerged parts.
5. Check for soft-deleted records: compare `SELECT count(*) FROM table WHERE customer_id = '{id}'` in both databases. Then compare `SELECT count(*) FROM table WHERE customer_id = '{id}' AND deleted_at IS NULL`.

**Resolution:**

- **Replication lag**: Inform the customer that Beacon data has a small delay. For real-time counts, direct them to the Nexus API. If lag is unusually high, check the CDC pipeline: `kubectl logs deployment/clickhouse-cdc-consumer -n beacon`.
- **Aggregation differences**: If the customer needs exact counts, advise them to use `COUNT(DISTINCT column)` instead of `uniq(column)` in ClickHouse. Exact functions are slower but precise.
- **Timezone**: Add explicit timezone conversion in ClickHouse queries: `toDateTime(timestamp, 'America/Los_Angeles')`. Document this difference in the SQL Playground help text.
- **Soft deletes**: Add a `WHERE deleted_at IS NULL` filter to ClickHouse queries. Long-term fix: update the CDC pipeline to replicate soft-delete events properly.
- **Schema drift**: Run the ClickHouse schema migration: `POST /admin/beacon/migrate-schema`. This compares PostgreSQL and ClickHouse schemas and adds missing columns.

**Prevention:**

- Display the replication lag prominently in the Beacon SQL Playground UI.
- Add automated schema drift detection: a nightly job that compares PostgreSQL and ClickHouse schemas and alerts if they diverge.
- Document known behavioral differences between the Nexus API (PostgreSQL) and Beacon SQL Playground (ClickHouse) in the Beacon onboarding materials.

---

## TS-015: Feature Flag Not Taking Effect for Specific Customer

**Symptom:** A feature flag was enabled for a specific customer in Launchpad, but the customer does not see the new feature. Other customers with the same flag enabled see it correctly.

**Common Causes (ranked by likelihood):**

1. **Redis cache stale**: Feature flag values are cached in Redis with a 2-minute TTL. The customer's session may be hitting a cached value from before the flag was enabled.
2. **Flag scoped to wrong entity**: The flag was enabled for the customer's organization ID but the feature checks against the user ID or the account ID (which may differ in multi-tenant setups like HomeBase Co-op).
3. **Multiple flag layers conflicting**: There are three layers of flag evaluation: global default, customer override, user override. A user-level override set to `false` takes precedence over a customer-level `true`.
4. **Frontend not refreshing flags**: The React frontend fetches feature flags on initial page load and caches them in memory. The customer has not refreshed their browser since the flag was enabled.
5. **Percentage rollout targeting**: The flag is configured for gradual rollout (e.g., 50%) and this customer happens to be in the un-enabled cohort based on their account ID hash.

**Diagnosis Steps:**

1. Check the flag configuration in Launchpad: Customers > {name} > Feature Flags. Verify the flag name, value, and scope.
2. Check for user-level overrides: Launchpad > Customers > {name} > Users > {user} > Feature Flag Overrides.
3. Check the flag evaluation API directly: `GET /admin/feature-flags/evaluate?flag={name}&customer_id={id}&user_id={user_id}`. This returns the resolved value and the reason (which layer determined the result).
4. Check Redis for the cached flag value: `redis-cli GET "ff:{customer_id}:{flag_name}"`.
5. If percentage rollout, check the customer's position in the rollout: `GET /admin/feature-flags/{name}/rollout?customer_id={id}`.

**Resolution:**

- **Redis cache**: Wait 2 minutes for the cache to expire, or flush the specific flag cache: `redis-cli DEL "ff:{customer_id}:{flag_name}"`.
- **Wrong entity scope**: Update the flag scope to match the entity the feature checks. This may require a code change if the feature is checking the wrong entity type.
- **Conflicting layers**: Remove the conflicting user-level override in Launchpad. If the override was intentional, discuss with the customer or CSM about the desired behavior.
- **Frontend cache**: Ask the customer to hard-refresh their browser (Ctrl+Shift+R or Cmd+Shift+R). For a systemic fix, implement a flag change event that pushes updates to connected clients via WebSocket.
- **Percentage rollout**: If the customer needs to be included, add an explicit customer-level override set to `true`. This bypasses the percentage rollout logic.

**Prevention:**

- Add a "Test as Customer" feature in Launchpad that simulates feature flag evaluation for a specific customer and shows all active flags and their resolved values.
- Reduce Redis cache TTL for feature flags to 30 seconds (trade-off: slightly more Redis load).
- Add a flag change notification via WebSocket so frontends update in real-time without requiring a page refresh.
- Document the flag evaluation precedence order (user override > customer override > percentage rollout > global default) in the internal wiki.

---

## TS-016: Webhook Signature Verification Failing on Customer Side

**Symptom:** The customer is receiving webhook deliveries (HTTP 200 from ACME's perspective) but their signature verification logic rejects every payload. The customer claims the computed HMAC does not match the `X-Acme-Signature-256` header.

**Common Causes (ranked by likelihood):**

1. **Wrong secret used for verification**: The customer is using their API key instead of the webhook secret to compute the HMAC.
2. **Payload encoding mismatch**: The customer is computing the HMAC over a re-serialized JSON body instead of the raw request body bytes. JSON re-serialization may change field order or whitespace.
3. **Timestamp not included in signature computation**: ACME's signature includes the timestamp (`X-Acme-Timestamp` header value prepended to the body). Customers who only hash the body will get a different result.
4. **Character encoding issue**: The customer's framework decodes the body as a string (potentially altering encoding) before passing it to the HMAC function.

**Diagnosis Steps:**

1. Ask the customer to share their signature verification code (sanitized, without the secret).
2. Compare their implementation against the reference implementation in ACME's docs: `docs.acme.dev/webhooks/verification`.
3. Send a test webhook (`POST /v1/webhooks/{id}/test`) and ask the customer to log: the raw body bytes, the `X-Acme-Timestamp` header, and the `X-Acme-Signature-256` header.
4. Compute the expected signature server-side: `HMAC-SHA256(webhook_secret, timestamp + "." + raw_body)` and compare.

**Resolution:**

- Provide the customer with the correct verification algorithm:
  1. Get the raw request body as bytes (do not parse/re-serialize).
  2. Get the `X-Acme-Timestamp` value.
  3. Compute: `HMAC-SHA256(webhook_secret, "{timestamp}.{raw_body}")`.
  4. Compare the hex digest with the value after `sha256=` in `X-Acme-Signature-256`.
- Point them to the SDK helper: `acme.VerifyWebhookSignature(secret, timestamp, body, signature)` available in Go, Python, and Node.js SDKs.

**Prevention:**

- Include a working code snippet in every supported language on the webhook setup page.
- Add a "Verify Signature" tool in the developer portal where customers can paste a payload, timestamp, secret, and expected signature to debug interactively.

---

## TS-017: Grafana Alerts Firing But No Actual Issue (Alert Noise)

**Symptom:** Engineering teams receive PagerDuty or Slack alerts that fire and then auto-resolve within minutes. The alerted condition (e.g., high latency, error rate spike) does not correspond to a real customer-facing issue.

**Common Causes (ranked by likelihood):**

1. **Alert threshold too sensitive**: The alert fires on a single data point exceeding the threshold rather than a sustained condition. A brief spike (e.g., during garbage collection or a deployment) triggers the alert.
2. **Synthetic monitoring noise**: Checkly or internal health checks experience transient failures (network blip, DNS resolution delay) that trigger alerts.
3. **Prometheus scrape gap**: A missed Prometheus scrape causes a gap that `rate()` functions interpret as zero, followed by a catch-up scrape that shows a spike.
4. **Alert on raw metrics instead of SLI**: The alert fires on raw error count instead of error ratio. A brief burst of errors during low-traffic periods triggers the alert even though the error rate is still within SLO.

**Diagnosis Steps:**

1. Review the alert history in Grafana Alerting > Alert Rules > {rule} > History. Count how many times it fired and auto-resolved in the last 7 days.
2. Check the alert rule configuration: look at the evaluation interval, `for` duration (how long the condition must be true), and threshold.
3. Cross-reference alert times with deployment times, maintenance windows, and Prometheus scrape failures.
4. Calculate the actual SLI (e.g., error rate) during the alert window to determine if the alert was genuinely warranted.

**Resolution:**

- **Too sensitive**: Increase the `for` duration. For example, change from `for: 0m` (fires immediately) to `for: 5m` (must be true for 5 consecutive minutes). Adjust threshold if the current value is too aggressive.
- **Synthetic monitoring**: Add retry logic to synthetic checks (Checkly supports 1 retry before alerting). Exclude synthetic check failures from the primary alert if they are not correlated with real customer impact.
- **Scrape gaps**: Use `rate()` with a range that is at least 4x the scrape interval. Add `absent()` handling to distinguish between zero traffic and missing data.
- **Raw metrics vs. SLI**: Rewrite the alert to use an error ratio: `rate(errors[5m]) / rate(requests[5m]) > 0.01` instead of `rate(errors[5m]) > 10`.

**Prevention:**

- Conduct a quarterly alert hygiene review: for each alert that fired in the past quarter, categorize as "actionable" or "noise". Tune or remove noisy alerts.
- Adopt SLO-based alerting using error budgets rather than raw thresholds.
- Set a team goal for alert quality: each on-call rotation should have >80% actionable alert rate.

---

## TS-018: Customer Sees "Service Unavailable" After Region-Specific Maintenance

**Symptom:** After a planned maintenance window for the EU region (`eu-west-1`), some EU customers continue to see HTTP 503 errors for 15-30 minutes after the maintenance window officially ends.

**Common Causes (ranked by likelihood):**

1. **Kubernetes pod startup time**: After maintenance, pods are restarting. Services with large dependency chains (e.g., nexus-api which connects to PostgreSQL, Redis, Kafka) take 30-60 seconds to become ready. If all pods restart simultaneously, the service is unavailable until enough pods are healthy.
2. **Database connection pool exhaustion**: All services try to establish database connections simultaneously after maintenance. The connection pool limit is reached and some services fail health checks.
3. **DNS cache**: Customer-side DNS caches may still point to the old IP addresses if infrastructure was re-provisioned during maintenance.
4. **Load balancer health check delay**: The ALB health checks take 30 seconds (3 checks x 10-second interval) to mark targets as healthy. During this window, requests are still routed to unhealthy targets.

**Diagnosis Steps:**

1. Check pod status across the EU cluster: `kubectl get pods -n nexus --field-selector status.phase!=Running` (via the admin kubeconfig for eu-west-1).
2. Check database connection pool metrics in Grafana: `db_pool_active_connections` and `db_pool_wait_count`.
3. Check ALB target group health in AWS Console > EC2 > Target Groups.
4. Check the customer's DNS resolution: ask them to run `nslookup eu.api.acme.dev` and compare against the expected IPs.

**Resolution:**

- **Pod startup**: Wait for pods to become ready. If some pods are stuck in CrashLoopBackOff, check their logs for dependency connection errors and restart them once dependencies are confirmed healthy.
- **Connection pool exhaustion**: Restart services in a staggered order rather than all at once. Start with databases and caches, then core services, then API gateways.
- **DNS cache**: Ask the customer to flush their DNS cache. On the infrastructure side, ensure DNS TTL is set to 60 seconds for service endpoints.
- **Health check delay**: Temporarily reduce the health check interval to speed up target registration. Restore normal interval after maintenance.

**Prevention:**

- Implement a staged startup sequence in the maintenance playbook: databases first, then caches, then backend services, then API gateways.
- Configure pod readiness probes to check all downstream dependencies before marking the pod as ready.
- Send a "maintenance complete" notification to customers only after ALB health checks confirm all targets are healthy, not when the infrastructure work finishes.
- Use connection pool warm-up: services gradually open connections over 30 seconds instead of all at once on startup.

---

## TS-019: Bulk Import via SFTP Connector Silently Drops Records

**Symptom:** A customer uploads a CSV file to their SFTP drop directory for Relay ingestion. The sync reports success, but the number of records created in Nexus is lower than the number of rows in the CSV file. No errors are reported.

**Common Causes (ranked by likelihood):**

1. **Duplicate detection filtering**: Relay's SFTP connector has built-in duplicate detection based on a composite key (typically SKU + location + date). If the CSV contains duplicate rows, they are silently deduplicated.
2. **Header row counted as data**: The customer includes a header row in their count but Relay skips it during processing. The "missing" record is the header.
3. **Empty rows or trailing whitespace**: The CSV contains blank rows (common at the end of Excel-generated CSVs) that are silently skipped.
4. **Encoding issue**: Rows with non-UTF-8 characters are silently dropped instead of being reported as errors. This is a known bug (tracked as REL-4521).
5. **File not fully uploaded**: The SFTP connector starts processing the file before the upload is complete. This is a race condition when files are large (>100MB).

**Diagnosis Steps:**

1. Compare the CSV row count (excluding header) with the sync log record count: `GET /v1/relay/sync-logs?connector_id={id}&sort=-created_at&per_page=1`
2. Check for duplicate rows in the CSV. Count unique values of the key columns.
3. Open the CSV in a hex editor or run `file -bi {filename}` to check the encoding.
4. Check for empty rows: `wc -l {file}` vs. `grep -c '[^[:space:]]' {file}`.
5. Check the SFTP connector's processing log for any skip messages: Loki `{app="relay-agent", connector_type="sftp"} |= "skip"`

**Resolution:**

- **Duplicates**: If duplicates are intentional (e.g., multiple transactions for the same SKU+location on the same day), configure the connector to use a different deduplication key that includes a transaction ID. Update in Relay Studio > Connector Config > Dedup Key.
- **Header row**: No fix needed -- this is expected. Help the customer reconcile their counts.
- **Empty rows**: Clean the CSV before upload. Provide the customer with a pre-processing script or recommend they use the API for programmatic uploads instead of SFTP.
- **Encoding bug**: Convert the file to UTF-8 before upload. Escalate to the Relay team for the bug fix (REL-4521).
- **Race condition**: Configure the SFTP connector's `file_stable_seconds` setting to 60 (default 10). This waits for the file to be unchanged for 60 seconds before processing, ensuring the upload is complete.

**Prevention:**

- Add a reconciliation report to the sync response that shows: total rows, duplicates skipped, errors, and successfully processed. This makes discrepancies immediately visible.
- Fix the encoding bug (REL-4521) to either report encoding errors or automatically transcode.
- Add file integrity checking: require a companion `.sha256` file for each CSV upload. Relay validates the checksum before processing.
- Document the SFTP connector's deduplication behavior in the onboarding materials.

---

## TS-020: Customer Reports Intermittent Timeouts on Specific API Endpoints

**Symptom:** A customer reports that certain API calls (usually `GET /v1/inventory/positions` with complex filters or `GET /v1/shipments` with large date ranges) intermittently time out (HTTP 504) while simpler queries work fine.

**Common Causes (ranked by likelihood):**

1. **Query hitting cold PostgreSQL cache**: The specific filter combination produces a query plan that requires a sequential scan on a large table. If the relevant data pages are not in PostgreSQL's shared_buffers, the query takes 10-30 seconds.
2. **Missing database index**: The customer is using a filter combination that does not have a supporting composite index. Common with multi-column filters like `sku + location_type + created_after`.
3. **Gateway timeout too short**: The API gateway (Kong/Envoy) has a 30-second timeout. Queries that take 20-30 seconds occasionally exceed this under load.
4. **Connection pool contention**: During peak traffic, the customer's queries wait in the connection pool queue, adding latency that pushes the total request time over the timeout.
5. **Large result set serialization**: The query returns quickly but JSON serialization of a large response (e.g., 200 records with nested objects) takes significant time.

**Diagnosis Steps:**

1. Ask the customer for the exact API call (URL, parameters) that times out.
2. Check the slow query log in PostgreSQL: `SELECT * FROM pg_stat_statements WHERE query LIKE '%inventory_positions%' ORDER BY mean_exec_time DESC LIMIT 10`.
3. Run `EXPLAIN ANALYZE` on the equivalent SQL query to see the query plan and actual execution time.
4. Check the gateway access logs for 504 responses: `{app="gateway"} |= "504" |= "{customer_id}"`. Note the upstream response time.
5. Check connection pool metrics during the timeout window: `db_pool_wait_time_seconds` in Grafana.

**Resolution:**

- **Cold cache**: Pre-warm the cache by running common query patterns periodically. Add `pg_prewarm` for frequently accessed tables.
- **Missing index**: Add the appropriate composite index. Example: `CREATE INDEX CONCURRENTLY idx_inventory_sku_loctype_created ON inventory_positions (customer_id, sku, location_type, created_at)`. Use `CONCURRENTLY` to avoid table locks.
- **Gateway timeout**: Increase the gateway timeout for specific endpoints to 60 seconds. This is configurable per route in the gateway config.
- **Connection pool**: Increase the pool size for the affected service. Current default is 20 connections; increase to 40 if the database instance can handle it. Check `max_connections` on the RDS instance.
- **Serialization**: Enable response streaming for large result sets. Alternatively, encourage the customer to use smaller `per_page` values and paginate through results.

**Prevention:**

- Run query plan analysis as part of the CI/CD pipeline for any migration that changes indexes or table structure.
- Set up slow query monitoring: alert if any query exceeds 5 seconds average execution time over a 15-minute window.
- Implement query complexity limits: reject queries with filter combinations that would produce full table scans for tables over 1M rows.
- Add server-side query timeout (distinct from gateway timeout) set to 25 seconds so the API returns a meaningful error before the gateway times out.
