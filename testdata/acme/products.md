# ACME Org — Product Documentation

Last updated: 2026-03-07 by Jennifer Liu (Product Ops)

This document covers detailed architecture, configuration, changelogs, migration guides, known limitations, and roadmap for all four ACME products. For org structure, runbooks, and API docs, see `acme-org.md`.

---

# Nexus (Core Platform)

## Architecture Overview

Nexus is built as a set of Go microservices running on AWS EKS. The core services are `nexus-api`, `inventory-service`, `shipment-service`, `forecast-engine`, `alert-service`, and `replenishment-engine`. They communicate primarily through Kafka event streams, with synchronous gRPC calls for low-latency request paths.

### Service Interaction Map

```
                        +-----------+
                        |  gateway  | :443
                        +-----+-----+
                              |
              +---------------+---------------+
              |               |               |
        +-----+-----+  +-----+-----+  +------+------+
        | nexus-api  |  | beacon-api|  |sentinel-api |
        |   :8080    |  |   :8100   |  |   :8110     |
        +-----+-----+  +-----------+  +-------------+
              |
    +---------+---------+---------+
    |         |         |         |
+---+---+ +---+---+ +---+---+ +--+---+
|invent | |shipm  | |forec  | |alert |
|ory    | |ent    | |ast    | |svc   |
|:8081  | |:8082  | |:8083  | |:8084 |
+---+---+ +---+---+ +---+---+ +--+---+
    |         |         |         |
    +----+----+----+----+    +----+
         |         |         |
     +---+---+ +---+---+ +--+----+
     | Kafka | | Postgres| | Redis |
     +-------+ +---------+ +-------+
```

### Data Flows

**Inventory Pipeline:**
1. Relay agent picks up CDC events from customer ERP (e.g., SAP IDoc, NetSuite SuiteScript webhook)
2. Events land on Kafka topic `inventory-cdc-{customer_id}`
3. `inventory-service` consumes events, applies schema mapping, deduplicates, and writes to PostgreSQL
4. Materialized inventory positions are updated in real time and cached in Redis (60-second TTL)
5. `nexus-api` reads from Redis cache for dashboard queries, falls back to PostgreSQL on cache miss

**Shipment Pipeline:**
1. Carrier polling jobs run on per-carrier schedules (FedEx 5m, UPS 10m, DHL 15m, others 15m default)
2. Raw carrier events land on Kafka topic `shipment-events-raw`
3. `shipment-service` normalizes carrier-specific statuses into the ACME lifecycle model (Booked -> Picked Up -> In Transit -> Out for Delivery -> Delivered -> Exception)
4. Normalized events are written to PostgreSQL and published to `shipment-events-normalized`
5. `alert-service` consumes normalized events and fires exception alerts when shipments deviate from expected timelines

**Forecast Pipeline:**
1. `forecast-engine` (Python, the one non-Go service in the stack) runs batch training jobs nightly at 01:00 UTC
2. Training data pulled from PostgreSQL: 18 months of historical sales, promotional calendars, external signals (weather from OpenWeatherMap API, events from PredictHQ API)
3. Model artifacts stored in S3 under `s3://acme-ml-artifacts/forecast/`
4. Inference runs on-demand via gRPC call from `nexus-api` or on schedule (daily at 06:00 UTC for all active customers)
5. Forecast results written to PostgreSQL and cached in Redis

**Alert Pipeline:**
1. `alert-service` subscribes to multiple Kafka topics: `shipment-events-normalized`, `inventory-positions`, `forecast-results`
2. Alert rules are stored per-customer in PostgreSQL (configurable via Nexus UI)
3. When a rule fires, `alert-service` dispatches to configured channels via adapters: email (SES), Slack (webhook), PagerDuty (Events API v2), generic webhook
4. Alert history stored in PostgreSQL with 90-day retention

**Replenishment Pipeline:**
1. `replenishment-engine` runs nightly at 07:00 UTC after forecast results are available
2. Pulls current inventory positions, forecast data, supplier lead times, MOQ constraints, and safety stock levels
3. Generates purchase order recommendations using a modified (s,Q) inventory model
4. Recommendations written to PostgreSQL, surfaced in Nexus UI
5. For customers with ERP write-back enabled (SAP, Oracle, NetSuite), PO recommendations can be pushed directly to the ERP via Relay

### Database Schema (Key Tables)

The `inventory-service` DB has grown to about 45 tables. The big ones:

- `inventory_positions` — ~500M rows across all customers. Partitioned by `customer_id`. Hot table, gets hammered.
- `inventory_events` — append-only CDC event log. ~2B rows. Partitioned by month. Older partitions archived to S3 quarterly (Marcus's team handles this).
- `locations` — warehouse/store/DC metadata. ~200K rows total.
- `skus` — product catalog mirror. ~15M rows. Synced from customer ERP via Relay.

The `shipment-service` DB is similarly large:
- `shipments` — ~800M rows. Partitioned by `customer_id` and `created_at` (monthly).
- `shipment_events` — ~4B rows. The biggest table we have. Partitioned by month, older than 6 months archived.
- `carriers` — carrier configuration and API credentials. ~250 rows.

TODO: Priya has been pushing for us to move shipment_events to ClickHouse (or at least the historical portion). It would dramatically improve query performance for Beacon's carrier analytics dashboards. On the roadmap for Q3 2026 but no one has scoped it yet.

## Configuration Reference

### Environment Variables

#### nexus-api

| Variable | Default | Description |
|----------|---------|-------------|
| `NEXUS_PORT` | `8080` | HTTP listen port |
| `NEXUS_GRPC_PORT` | `9080` | gRPC listen port |
| `NEXUS_DB_URL` | (required) | PostgreSQL connection string |
| `NEXUS_REDIS_URL` | (required) | Redis connection string |
| `NEXUS_KAFKA_BROKERS` | (required) | Comma-separated Kafka broker addresses |
| `NEXUS_LOG_LEVEL` | `info` | Log level: debug, info, warn, error |
| `NEXUS_LOG_FORMAT` | `json` | Log format: json, text |
| `NEXUS_CORS_ORIGINS` | `https://app.acme.dev` | Allowed CORS origins, comma-separated |
| `NEXUS_AUTH_SERVICE_URL` | (required) | URL of auth-service for token validation |
| `NEXUS_RATE_LIMIT_DEFAULT` | `1000` | Default requests/minute per API key |
| `NEXUS_RATE_LIMIT_ENTERPRISE` | `5000` | Enterprise tier requests/minute per API key |
| `NEXUS_CACHE_TTL_SECONDS` | `60` | Redis cache TTL for inventory positions |
| `NEXUS_MAX_PAGE_SIZE` | `200` | Maximum page size for list endpoints |
| `NEXUS_METRICS_ENABLED` | `true` | Expose /metrics endpoint for Prometheus |
| `NEXUS_TRACING_ENABLED` | `true` | Enable OpenTelemetry tracing |
| `NEXUS_TRACING_SAMPLE_RATE` | `0.01` | Trace sampling rate (production) |
| `NEXUS_TRACING_ENDPOINT` | (required if tracing enabled) | Jaeger collector endpoint |
| `NEXUS_FEATURE_FLAG_REDIS_PREFIX` | `ff:nexus:` | Redis key prefix for feature flags |

#### inventory-service

| Variable | Default | Description |
|----------|---------|-------------|
| `INVENTORY_PORT` | `8081` | HTTP listen port |
| `INVENTORY_DB_URL` | (required) | PostgreSQL connection string |
| `INVENTORY_KAFKA_BROKERS` | (required) | Kafka broker addresses |
| `INVENTORY_KAFKA_CONSUMER_GROUP` | `inventory-service` | Kafka consumer group ID |
| `INVENTORY_KAFKA_TOPIC_PREFIX` | `inventory-cdc-` | Topic prefix for CDC events |
| `INVENTORY_REDIS_URL` | (required) | Redis for caching |
| `INVENTORY_RECONCILIATION_CRON` | `0 3 * * *` | Cron schedule for reconciliation job |
| `INVENTORY_RECONCILIATION_BATCH_SIZE` | `10000` | Batch size for reconciliation queries |
| `INVENTORY_CDC_DLQ_TOPIC` | `inventory-cdc-dlq` | Dead letter queue topic |
| `INVENTORY_DEDUP_WINDOW_SECONDS` | `300` | Window for event deduplication |
| `INVENTORY_POSITION_CACHE_TTL` | `60` | Cache TTL in seconds |
| `INVENTORY_MAX_BATCH_SIZE` | `5000` | Max records per batch write |
| `INVENTORY_ENABLE_ARCHIVE` | `false` | Enable automatic S3 archival of old events |
| `INVENTORY_ARCHIVE_S3_BUCKET` | `acme-inventory-archive` | S3 bucket for event archives |
| `INVENTORY_ARCHIVE_RETENTION_MONTHS` | `6` | Keep events in PostgreSQL for N months |

#### shipment-service

| Variable | Default | Description |
|----------|---------|-------------|
| `SHIPMENT_PORT` | `8082` | HTTP listen port |
| `SHIPMENT_DB_URL` | (required) | PostgreSQL connection string |
| `SHIPMENT_KAFKA_BROKERS` | (required) | Kafka broker addresses |
| `SHIPMENT_CARRIER_POLL_ENABLED` | `true` | Enable carrier API polling |
| `SHIPMENT_CARRIER_POLL_CONCURRENCY` | `20` | Max concurrent carrier API calls |
| `SHIPMENT_CARRIER_CIRCUIT_BREAKER_THRESHOLD` | `5` | Failures before opening circuit breaker |
| `SHIPMENT_CARRIER_CIRCUIT_BREAKER_TIMEOUT` | `300` | Seconds before half-open retry |
| `SHIPMENT_CARRIER_TIMEOUT_MS` | `10000` | Per-carrier API call timeout |
| `SHIPMENT_NORMALIZATION_WORKERS` | `10` | Number of normalization worker goroutines |
| `SHIPMENT_EVENT_RETENTION_MONTHS` | `6` | Keep events in PostgreSQL for N months |
| `SHIPMENT_REDIS_URL` | (required) | Redis for ETA caching and circuit breaker state |

#### forecast-engine

| Variable | Default | Description |
|----------|---------|-------------|
| `FORECAST_PORT` | `8083` | HTTP listen port (healthcheck + admin) |
| `FORECAST_GRPC_PORT` | `9083` | gRPC listen port for inference |
| `FORECAST_DB_URL` | (required) | PostgreSQL connection string |
| `FORECAST_REDIS_URL` | (required) | Redis for result caching |
| `FORECAST_S3_BUCKET` | `acme-ml-artifacts` | S3 bucket for model artifacts |
| `FORECAST_TRAINING_SCHEDULE` | `0 1 * * *` | Cron schedule for nightly training |
| `FORECAST_INFERENCE_SCHEDULE` | `0 6 * * *` | Cron schedule for daily batch inference |
| `FORECAST_HISTORY_MONTHS` | `18` | Months of historical data for training |
| `FORECAST_HORIZON_DAYS` | `90` | Default forecast horizon |
| `FORECAST_CONFIDENCE_LEVEL` | `0.90` | Default confidence interval level |
| `FORECAST_WEATHER_API_KEY` | (required) | OpenWeatherMap API key |
| `FORECAST_EVENTS_API_KEY` | (required) | PredictHQ API key |
| `FORECAST_MAX_TRAINING_HOURS` | `4` | Kill training job if exceeds this |
| `FORECAST_GPU_ENABLED` | `false` | Use GPU for training (requires CUDA) |
| `FORECAST_FALLBACK_MODEL` | `exponential_smoothing` | Model to use when primary model is unavailable |
| `FORECAST_MAPE_ALERT_THRESHOLD` | `0.15` | Alert if MAPE exceeds this |

#### alert-service

| Variable | Default | Description |
|----------|---------|-------------|
| `ALERT_PORT` | `8084` | HTTP listen port |
| `ALERT_DB_URL` | (required) | PostgreSQL connection string |
| `ALERT_KAFKA_BROKERS` | (required) | Kafka broker addresses |
| `ALERT_REDIS_URL` | (required) | Redis for deduplication |
| `ALERT_SES_REGION` | `us-west-2` | AWS SES region for email alerts |
| `ALERT_SES_FROM` | `alerts@acme.dev` | From address for email alerts |
| `ALERT_PAGERDUTY_INTEGRATION_KEY` | (optional) | Default PagerDuty integration key |
| `ALERT_DEDUP_WINDOW_MINUTES` | `15` | Don't re-fire same alert within this window |
| `ALERT_BATCH_WINDOW_SECONDS` | `60` | Batch alerts within this window before sending |
| `ALERT_MAX_BATCH_SIZE` | `50` | Max alerts per batch notification |
| `ALERT_RETENTION_DAYS` | `90` | Alert history retention |
| `ALERT_WEBHOOK_TIMEOUT_MS` | `5000` | Timeout for customer webhook delivery |
| `ALERT_WEBHOOK_MAX_RETRIES` | `3` | Retry count for failed webhook delivery |

### Config Files

Most services also accept a YAML config file at `/etc/acme/{service}/config.yaml`. Environment variables take precedence over config file values.

Example `nexus-api` config:

```yaml
# /etc/acme/nexus-api/config.yaml
server:
  port: 8080
  grpc_port: 9080
  read_timeout: 30s
  write_timeout: 30s
  max_request_size: 10MB

database:
  url: postgres://nexus:***@nexus-db.internal:5432/nexus?sslmode=require
  max_open_conns: 100
  max_idle_conns: 25
  conn_max_lifetime: 5m

redis:
  url: redis://redis.internal:6379/0
  pool_size: 50
  min_idle_conns: 10

kafka:
  brokers:
    - msk-1.internal:9092
    - msk-2.internal:9092
    - msk-3.internal:9092
  consumer_group: nexus-api
  auto_offset_reset: latest

auth:
  service_url: http://auth-service.internal:8000
  token_cache_ttl: 5m
  # NOTE: jwks endpoint is fetched from auth-service on startup
  # and refreshed every 15 minutes. If auth-service is down during
  # nexus-api startup, the pod will crash-loop. This has bitten us
  # twice. TODO: Add graceful degradation. — Marcus, 2026-01

rate_limiting:
  enabled: true
  default_rpm: 1000
  enterprise_rpm: 5000
  # backed by Redis, uses sliding window algorithm
  window_size: 60s

features:
  # Viktor's feature flag system reads from Redis
  # flags are namespaced per service
  flag_prefix: "ff:nexus:"
  default_enabled: false
```

### Feature Flags

All feature flags are managed through Viktor Nowak's custom system built on Redis. Flags follow the format `ff:{service}:{flag_name}` and support graduated rollout.

Current active flags for Nexus (as of 2026-03-07):

| Flag | Status | Description |
|------|--------|-------------|
| `ff:nexus:bulk_inventory_api` | 100% rollout | Bulk inventory position query endpoint (shipped in 5.2) |
| `ff:nexus:websocket_updates` | 50% rollout | Real-time WebSocket push for inventory updates |
| `ff:nexus:carrier_v2_normalization` | 10% rollout | New carrier status normalization engine |
| `ff:nexus:oracle_v4_model` | internal only | Oracle v4 forecast model (testing with Intelligence Squad) |
| `ff:nexus:replenishment_multi_supplier` | 25% rollout | Multi-supplier optimization in replenishment engine |
| `ff:nexus:graphql_api` | disabled | Experimental GraphQL API (parked — Priya wants to revisit in Q3) |

## Changelog / Release History

### Nexus 5.3.0 (2026-02-25)

**Release manager:** Viktor Nowak
**Highlights:** Multi-supplier replenishment, WebSocket inventory updates

- **feat:** Multi-supplier optimization in replenishment engine. When multiple suppliers can fulfill a PO, the engine now considers lead time, cost, reliability score, and MOQ to recommend optimal split across suppliers. Behind flag `replenishment_multi_supplier`, currently at 25% rollout. (NEX-4521, Intelligence Squad)
- **feat:** WebSocket endpoint `/ws/v1/inventory/positions` for real-time inventory push updates. Replaces polling for supported clients. Reduces API load by ~30% for customers that adopt it. Behind flag `websocket_updates`, currently at 50%. (NEX-4480, Inventory Squad)
- **fix:** Fixed race condition in inventory position cache invalidation that could cause stale reads for up to 5 minutes after a CDC event. Root cause: Redis MULTI/EXEC block wasn't including the cache key deletion. Affected ~2% of inventory reads. (NEX-4555, Inventory Squad)
- **fix:** Carrier polling job now respects per-carrier rate limits even during catch-up after a polling job failure. Previously, if a polling job failed and restarted, it would attempt to catch up all missed polls at once, overwhelming carrier APIs. (NEX-4530, Logistics Squad)
- **fix:** Forecast confidence intervals now correctly account for promotional periods. Previously, confidence intervals during promo weeks were too narrow because the model wasn't factoring in promo demand variance. (NEX-4498, Intelligence Squad)
- **chore:** Upgraded Go to 1.22.1 across all Nexus services. (NEX-4540, Platform Team)
- **chore:** Migrated nexus-api from `acme-pgx` v2 to v3. No functional changes, but connection pool metrics are now exposed via Prometheus. (NEX-4538)

### Nexus 5.2.0 (2026-01-28)

**Release manager:** Viktor Nowak
**Highlights:** Bulk inventory API, forecast accuracy improvements

- **feat:** New bulk inventory API: `POST /v1/inventory/positions/bulk` accepts up to 1,000 SKU+location pairs in a single request. Reduces N+1 API calls for customers with large catalogs. (NEX-4410)
- **feat:** Oracle forecast model v3.2 with improved seasonality detection. Reduced average MAPE from 11.2% to 9.8% across all customers. Big win — Chen Wei's team spent 6 weeks on this. (NEX-4389)
- **fix:** Fixed pagination bug where `per_page=200` (the max) would return 199 results for certain query patterns. Off-by-one in the LIMIT/OFFSET calculation. Embarrassing but only 1 customer reported it. (NEX-4425)
- **fix:** Alert deduplication window was being applied globally instead of per-customer. Two different customers with the same alert rule could suppress each other's alerts. In production for 3 weeks before we caught it. Post-mortem: PM-2026-003. (NEX-4431)
- **refactor:** Moved carrier API credential management from environment variables to AWS Secrets Manager with auto-rotation. Breaking change for self-hosted deployments (none currently, but Amy Zhang asked us to plan for it). (NEX-4400)
- **chore:** Updated all Go dependencies. Notable: upgraded `google.golang.org/grpc` to 1.61 for HTTP/2 rapid reset vulnerability fix. (NEX-4445)

### Nexus 5.1.1 (2026-01-14)

**Release manager:** Viktor Nowak
**Highlights:** Hotfix release

- **fix:** Critical fix for inventory reconciliation job that was incorrectly marking in-transit inventory as on-hand after a Relay agent reconnection. Affected 12 customers. Root cause: the reconciliation job wasn't checking the `location_type` field when merging CDC events received during an agent outage. Customer impact: inflated on-hand quantities for 4-8 hours until next full sync. (NEX-4460)
- **fix:** Fixed memory leak in shipment-service WebSocket handler (from the experimental WebSocket feature, not yet GA). Goroutine leak when clients disconnected without proper close frame. Found by Tom Bradley's team during load testing. (NEX-4462)

### Nexus 5.1.0 (2025-12-17)

**Release manager:** Viktor Nowak
**Highlights:** Delivery estimation ML model, carrier performance scoring

- **feat:** ML-based delivery estimation that predicts actual delivery date for in-transit shipments. Uses historical carrier performance, route data, weather, and day-of-week patterns. Replaces the simple "carrier says N days" approach. Shows estimated delivery range in Nexus UI. (NEX-4301)
- **feat:** Carrier performance scoring system. Each carrier gets a reliability score (0-100) based on on-time delivery rate, damage claims, and tracking data quality. Visible in Nexus UI under Carriers tab. Used as input signal for replenishment engine supplier selection. (NEX-4288)
- **feat:** Custom alert rule builder in the UI. Customers can now create complex alert rules using a visual builder (e.g., "alert me when SKU-X inventory at WH-PDX drops below 200 AND there's no open PO for SKU-X"). Previously this required a support ticket to configure. (NEX-4315)
- **fix:** Fixed timezone handling in forecast engine batch job. Customers in Asia-Pacific timezones were getting forecasts generated with UTC dates, causing off-by-one-day issues. (NEX-4350)
- **fix:** Exception alert emails now include direct link to the affected shipment in Nexus. Previously just included shipment ID which customers had to search for. Small change, big customer satisfaction improvement. (NEX-4342)

### Nexus 5.0.0 (2025-11-12)

**Release manager:** Viktor Nowak
**Highlights:** Major version bump — new API versioning, multi-region support, breaking changes

- **breaking:** API path prefix changed from `/api/` to `/v1/`. Old paths will return 301 redirects for 6 months (until 2026-05-12), then 404. Migration guide below.
- **breaking:** Shipment status values changed from UPPER_CASE to snake_case (e.g., `IN_TRANSIT` -> `in_transit`). API returns both formats during transition period when `X-ACME-Compat: v0` header is present.
- **feat:** Multi-region support. Customers can now be assigned to specific AWS regions for data residency. EU customers automatically routed to eu-west-1. Required major refactoring of tenant routing in the gateway. (NEX-4100)
- **feat:** Oracle v3 forecast model. Complete rewrite of the forecast engine using transformer architecture. 22% improvement in MAPE over v2. Chen Wei's team spent 4 months on this. (NEX-4150)
- **feat:** Replenishment engine now supports safety stock optimization. Instead of static safety stock levels, the engine can calculate optimal safety stock based on demand variability and target service level. (NEX-4180)
- **refactor:** Migrated from monolithic `nexus-api` to separate inventory-service and shipment-service. The old `nexus-api` is now a thin API gateway that routes to the appropriate backend service. (NEX-4050)
- **infra:** PostgreSQL upgraded from 15 to 16. Transparent to customers but required careful migration planning — Marcus's team ran the migration over a weekend with 8 minutes of downtime. (NEX-4200)

## Known Limitations

1. **Inventory position cache staleness**: In rare cases (CDC event processing latency spike), inventory positions displayed in the dashboard can be up to 5 minutes stale. The cache TTL is 60 seconds, but if the CDC pipeline backs up, the underlying data is already stale when cached. Workaround: customers can force-refresh via the UI (the reload button bypasses cache) or use the `?no_cache=true` query parameter on the API.

2. **Forecast engine cold start**: New customers don't get useful forecasts until they have at least 6 months of historical data ingested. With less than 6 months, the engine falls back to simple exponential smoothing which has significantly higher MAPE (~25%). Workaround: customers can upload historical data CSVs during onboarding to bootstrap the model. Implementation team handles this.

3. **Maximum SKU count**: Nexus is tested up to 500K SKUs per customer. Beyond that, some dashboard queries start to degrade (inventory position page load > 3 seconds). Two Enterprise customers are approaching this limit (GlobalMart at 380K, Pacific Rim at 420K). Tech debt: need to add cursor-based pagination and lazy-loading in the UI. Priya has this on the Nexus Core backlog.

4. **Carrier integration limit**: Maximum 50 carrier integrations per customer. Hardcoded in the shipment-service config. No customer has come close to this yet (max is Pacific Rim at 18), but it's a tech debt item.

5. **Alert rule complexity**: The custom alert rule builder supports up to 5 conditions per rule. More complex rules require direct SQL-based rules configured by ACME support. About 8 Enterprise customers have custom SQL rules that are maintained by the Intelligence Squad.

6. **Replenishment PO push**: The ERP write-back for purchase orders only works with SAP, Oracle, and NetSuite. Customers on other ERPs get recommendations in the UI but have to manually create POs. James Okafor's team is working on a generic webhook-based PO push for Relay 3.2.

7. **WebSocket connection limit**: The new WebSocket feature (behind flag) supports up to 100 concurrent connections per customer. This is a limitation of our current WebSocket gateway implementation. Tom Bradley is investigating using AWS API Gateway WebSocket APIs to remove this limit.

## Migration Guide: Nexus 4.x to 5.x

### Overview

Nexus 5.0 was a major release with breaking API changes. This guide covers what customers and internal teams need to do.

**Timeline:**
- 5.0 released: 2025-11-12
- Compatibility mode (old + new paths): until 2026-05-12
- Old paths return 404: 2026-05-12 onwards

### API Path Changes

All API paths changed from `/api/` prefix to `/v1/` prefix:

| Old Path | New Path |
|----------|----------|
| `/api/inventory/positions` | `/v1/inventory/positions` |
| `/api/shipments` | `/v1/shipments` |
| `/api/shipments/:id` | `/v1/shipments/:id` |
| `/api/forecast/generate` | `/v1/forecast/generate` |
| `/api/forecast/:id` | `/v1/forecast/:id` |
| `/api/alerts` | `/v1/alerts` |
| `/api/alerts/rules` | `/v1/alerts/rules` |

During the transition period, old paths return `301 Moved Permanently` with `Location` header pointing to the new path. Most HTTP clients follow redirects automatically, so this should be transparent. However, POST requests may be converted to GET by some clients (per HTTP spec), so customers using POST endpoints should update their paths proactively.

### Status Value Changes

Shipment status values changed from UPPER_CASE to snake_case:

| Old Value | New Value |
|-----------|-----------|
| `BOOKED` | `booked` |
| `PICKED_UP` | `picked_up` |
| `IN_TRANSIT` | `in_transit` |
| `OUT_FOR_DELIVERY` | `out_for_delivery` |
| `DELIVERED` | `delivered` |
| `EXCEPTION` | `exception` |

To ease migration, include `X-ACME-Compat: v0` header in your requests to receive responses with both old and new status values:

```json
{
  "status": "in_transit",
  "legacy_status": "IN_TRANSIT"
}
```

The compatibility header will be supported until 2026-05-12.

### Database Migration

If you have any custom queries against Nexus data (via Beacon SQL Playground or data exports), note that the following table names changed:

| Old Table | New Table |
|-----------|-----------|
| `shipments_v3` | `shipments` |
| `inventory_current` | `inventory_positions` |
| `forecast_results_v2` | `forecast_results` |

Views with the old names are available during the transition period.

### Webhook Payload Changes

Alert webhook payloads now include a `version` field and use snake_case for all field names:

```json
{
  "version": "2.0",
  "alert_id": "ALT-2026-001",
  "alert_type": "inventory_below_safety_stock",
  "customer_id": "CUST-001",
  "triggered_at": "2026-01-15T10:30:00Z",
  "details": { ... }
}
```

Old format (without `version` field, with camelCase) is sent when `X-ACME-Compat: v0` is configured for the customer's webhook endpoint in Launchpad.

### Action Items for Customer Success

1. Identify all customers with custom API integrations (check API key usage in the analytics dashboard)
2. Send migration notification email (template in Confluence: CS/Templates/Nexus-5-Migration)
3. For Enterprise customers: schedule a migration call with their engineering team
4. For Mid-Market customers: share the self-service migration guide (docs.acme.dev/migration/nexus-5)
5. Track migration progress in Launchpad — there's a "V5 Migration Status" field per customer
6. After 2026-05-12: verify no customers are still hitting old paths (check gateway access logs)

---

# Relay (Integration Hub)

## Architecture Overview

Relay has a split architecture: the **Relay Agent** runs in the customer's environment (on-prem VM, customer's cloud, or ACME-hosted), and the **Relay Control Plane** runs in ACME's infrastructure.

### Relay Agent

The agent is a single Go binary (~45MB) that handles:
- Connecting to customer systems (ERP, WMS, eCommerce, carrier APIs)
- Running connector plugins (each connector is a Go plugin loaded at runtime)
- Performing data extraction, transformation, and schema mapping
- Pushing data to ACME's Kafka cluster over a TLS tunnel
- Receiving configuration updates from the control plane
- Local buffering when the connection to ACME is interrupted (SQLite-based WAL, up to 10GB)

The agent phones home to `relay.api.acme.dev:443` every 30 seconds for heartbeat and config sync. If the agent loses connectivity, it buffers locally and replays when reconnected. The replay mechanism guarantees at-least-once delivery but NOT exactly-once — downstream services (inventory-service, shipment-service) handle deduplication.

### Relay Control Plane

The control plane (`relay-control-plane` service, port 8091) manages:
- Agent registration and lifecycle (install, upgrade, decommission)
- Connector configuration and versioning
- Schema mapping definitions (customer ERP schema -> ACME canonical schema)
- Health monitoring and alerting (feeds into #relay-alerts Slack channel)
- Agent remote management (restart, log collection, config push)

### Connector Architecture

Each connector is a Go plugin that implements the `Connector` interface:

```go
type Connector interface {
    Name() string
    Version() string
    Init(config ConnectorConfig) error
    Extract(ctx context.Context, since time.Time) ([]Record, error)
    Transform(records []Record, mapping SchemaMapping) ([]CanonicalRecord, error)
    Validate(records []CanonicalRecord) []ValidationError
    Close() error
}
```

Connectors are versioned independently of the agent. The control plane pushes connector updates to agents, which hot-reload them without restart (most of the time — some connectors require restart, noted in their manifests).

Current connector inventory:

| Connector | Version | Type | Hot-reload? | Notes |
|-----------|---------|------|-------------|-------|
| `sap-s4hana` | 3.4.2 | ERP | No | Uses RFC protocol, requires SAP JCo library |
| `oracle-netsuite` | 2.8.1 | ERP | Yes | SuiteTalk REST API |
| `dynamics-365` | 2.3.0 | ERP | Yes | Dataverse API |
| `sage-intacct` | 1.5.0 | ERP | Yes | SOAP API (yeah, I know) |
| `manhattan-wms` | 2.1.0 | WMS | No | Custom TCP protocol |
| `blue-yonder-wms` | 1.9.2 | WMS | Yes | REST API |
| `korber-wms` | 1.4.0 | WMS | Yes | REST API |
| `fedex` | 4.1.0 | Carrier | Yes | FedEx Track API v1 |
| `ups` | 3.8.1 | Carrier | Yes | UPS Tracking API |
| `dhl` | 3.2.0 | Carrier | Yes | DHL Unified Tracking |
| `usps` | 2.5.0 | Carrier | Yes | USPS Web Tools |
| `maersk` | 1.6.0 | Carrier | Yes | Maersk Tracking API |
| `db-schenker` | 1.3.0 | Carrier | Yes | Schenker eServices |
| `sf-express` | 1.1.0 | Carrier | Yes | SF Express API (added for APAC customers) |
| `shopify` | 3.0.1 | eCommerce | Yes | Admin GraphQL API |
| `bigcommerce` | 2.2.0 | eCommerce | Yes | V3 REST API |
| `woocommerce` | 1.7.0 | eCommerce | Yes | WP REST API |
| `magento` | 1.5.0 | eCommerce | Yes | Adobe Commerce REST API |
| `sftp` | 2.0.0 | Generic | Yes | CSV/JSON/XML file polling |
| `edi-x12` | 1.8.0 | Generic | No | X12 850/856/810 parser |
| `rest-generic` | 2.1.0 | Generic | Yes | Configurable REST API poller |

### Data Flow Detail

```
Customer ERP/WMS                ACME Infrastructure
+------------------+            +-------------------+
|                  |            |                   |
| SAP / NetSuite / |  TLS 1.3  | relay-control-    |
| Dynamics / etc.  |<--------->| plane :8091       |
|                  |            |                   |
+--------+---------+            +-------------------+
         |                              |
         | (local)                      | (config, heartbeat)
         v                              |
+--------+---------+            +-------v-----------+
|                  |            |                   |
| Relay Agent      |  TLS 1.3  | Kafka (MSK)       |
| - connector(s)   |---------->| inventory-cdc-*   |
| - schema mapper  |            | shipment-events-* |
| - local buffer   |            | relay-metadata    |
|                  |            |                   |
+------------------+            +-------------------+
```

## Configuration Reference

### Agent Configuration

The agent config lives at `/etc/relay/agent.yaml` on the agent host:

```yaml
# /etc/relay/agent.yaml
agent:
  id: "agent-{customer_id}-{seq}"      # assigned by control plane
  name: "GlobalMart PDX Agent"          # human-readable
  customer_id: "CUST-GLOBALMART"
  region: "us-west-2"

control_plane:
  url: "https://relay.api.acme.dev"
  heartbeat_interval: 30s
  config_sync_interval: 60s
  auth_token: "${RELAY_AUTH_TOKEN}"     # rotated quarterly

buffer:
  enabled: true
  path: "/var/lib/relay/buffer"
  max_size_gb: 10
  flush_interval: 5s
  flush_batch_size: 1000

kafka:
  brokers:
    - "msk-1.relay.acme.dev:9094"       # port 9094 = TLS
    - "msk-2.relay.acme.dev:9094"
    - "msk-3.relay.acme.dev:9094"
  tls:
    enabled: true
    cert_file: "/etc/relay/certs/client.pem"
    key_file: "/etc/relay/certs/client-key.pem"
    ca_file: "/etc/relay/certs/ca.pem"
  producer:
    acks: "all"
    compression: "lz4"
    batch_size: 65536
    linger_ms: 50

connectors:
  - name: "sap-s4hana"
    version: "3.4.2"
    config:
      host: "sap.globalmart.internal"
      system_number: "00"
      client: "100"
      user: "${SAP_USER}"
      password: "${SAP_PASSWORD}"
      poll_interval: 60s
      idoc_types:
        - "MATMAS05"     # material master
        - "WMMBXY"       # warehouse movement
        - "DESADV"       # despatch advice
      max_batch_size: 10000
      # IMPORTANT: keep max_idoc_size_mb below 50 to avoid the memory leak
      # bug (see Known Issues). Fix coming in Relay 3.2.
      max_idoc_size_mb: 40

logging:
  level: "info"
  file: "/var/log/relay/agent.log"
  max_size_mb: 100
  max_backups: 5
  compress: true

metrics:
  enabled: true
  port: 9100
  path: "/metrics"

# Resource limits — important for on-prem deployments where
# the agent shares resources with other apps
resources:
  max_memory_mb: 2048          # increase to 4096 for large SAP setups
  max_cpu_percent: 50
  max_goroutines: 500
```

### Control Plane Environment Variables

| Variable | Default | Description |
|----------|---------|-------------|
| `RELAY_CP_PORT` | `8091` | HTTP listen port |
| `RELAY_CP_DB_URL` | (required) | PostgreSQL connection string |
| `RELAY_CP_REDIS_URL` | (required) | Redis for agent session tracking |
| `RELAY_CP_KAFKA_BROKERS` | (required) | Kafka broker addresses |
| `RELAY_CP_AGENT_AUTH_SECRET` | (required) | Secret for agent auth token generation |
| `RELAY_CP_HEARTBEAT_TIMEOUT` | `120s` | Consider agent offline after this |
| `RELAY_CP_CONFIG_PUSH_ENABLED` | `true` | Push config changes to agents |
| `RELAY_CP_CONNECTOR_REGISTRY_URL` | `s3://acme-relay-connectors` | Where connector binaries are stored |
| `RELAY_CP_ALERT_SLACK_WEBHOOK` | (required) | Webhook for #relay-alerts |
| `RELAY_CP_MAX_AGENTS_PER_CUSTOMER` | `10` | Max agents per customer |
| `RELAY_CP_LOG_COLLECTION_S3_BUCKET` | `acme-relay-logs` | S3 bucket for collected agent logs |

### Feature Flags

| Flag | Status | Description |
|------|--------|-------------|
| `ff:relay:agent_auto_upgrade` | 50% rollout | Auto-upgrade agents when new connector versions are available |
| `ff:relay:schema_mapper_v2` | internal only | New schema mapping engine with JSONPath support |
| `ff:relay:sftp_streaming` | 100% rollout | Stream large SFTP files instead of downloading entirely |
| `ff:relay:edi_nested_loops` | disabled | Support for >4 level nested loops in EDI X12 856 |

## Changelog / Release History

### Relay 3.1.0 (2026-02-18)

**Release manager:** Viktor Nowak
**Highlights:** Agent auto-upgrade, schema mapper improvements

- **feat:** Agent auto-upgrade capability. The control plane can now push connector updates and trigger graceful agent upgrades without manual intervention. Behind flag `agent_auto_upgrade`, rolling out to 50% of agents. James Okafor's team has been wanting this forever. (REL-2201)
- **feat:** Schema mapper now supports conditional mappings (if field X has value Y, map to Z). Previously required custom connector code for any conditional logic. (REL-2185)
- **feat:** New `rest-generic` connector v2.1 with OAuth 2.0 client credentials flow support. Several customers have been asking for this to connect to their internal APIs. (REL-2178)
- **fix:** Fixed the SFTP connector hanging when the remote server closes the connection during a large file transfer. Now uses a 30-second idle timeout and retries from the last successfully read offset. (REL-2210)
- **fix:** Control plane was not correctly deregistering agents that were manually decommissioned. Ghost agents would show in the dashboard and trigger false "agent offline" alerts. (REL-2195)
- **fix:** NetSuite connector now handles the `INVALID_SESSION` error from SuiteTalk by refreshing the token instead of failing the entire sync. Was causing 1-2 sync failures per day for ~15% of NetSuite customers. (REL-2190)

### Relay 3.0.0 (2026-01-07)

**Release manager:** Viktor Nowak
**Highlights:** Major version — new agent architecture, breaking config changes

- **breaking:** Agent config format changed from JSON to YAML. Existing agents need config migration (script provided: `relay-agent migrate-config`). See migration guide below.
- **breaking:** Connector plugin interface v2. All connectors recompiled. Custom connectors (3 customers have them) need to implement the new `Validate()` method.
- **feat:** Local buffer redesigned to use SQLite WAL instead of flat files. 10x better replay performance and crash recovery. (REL-2100)
- **feat:** Agent now supports running multiple connectors simultaneously (previously sequential). Cuts full sync time in half for customers with multiple integrations. (REL-2088)
- **feat:** New connector: `sf-express` for SF Express carrier tracking. Requested by Pacific Rim Distributors for their APAC shipments. (REL-2120)
- **feat:** Schema mapping engine now supports array unwinding and nested object flattening. (REL-2095)
- **refactor:** Moved from polling-based config sync to WebSocket-based push. Reduces config propagation time from 60 seconds to < 1 second. (REL-2110)
- **infra:** Agent binary size reduced from 80MB to 45MB by stripping debug symbols and using UPX compression. (REL-2130)

### Relay 2.9.0 (2025-11-26)

**Release manager:** Viktor Nowak

- **feat:** SFTP connector now supports streaming mode for large files (>1GB). Processes records as they're read instead of loading entire file into memory. Behind flag `sftp_streaming`. (REL-2050)
- **feat:** Connector health dashboard in Relay Studio showing real-time metrics per connector: records/sec, error rate, last sync time. (REL-2035)
- **fix:** SAP connector: fixed IDoc parsing error for MATMAS05 with custom segments. Was silently dropping records with non-standard segment names. (REL-2060)
- **fix:** Dynamics 365 connector: pagination bug where the last page of results was being skipped if the total count was exactly a multiple of the page size. (REL-2055)
- **chore:** Renewed TLS certificates for relay.api.acme.dev. Old certs were expiring in 30 days. Automated cert renewal via Let's Encrypt is on the TODO list (assigned to Marcus's team). (REL-2070)

### Relay 2.8.1 (2025-11-05)

**Release manager:** Viktor Nowak
**Highlights:** Hotfix for NetSuite rate limiting

- **fix:** Improved NetSuite SuiteTalk rate limiting. The token bucket was using a fixed 10 req/sec limit but NetSuite recently started returning dynamic rate limit headers. Connector now reads `X-RateLimit-Remaining` and adjusts accordingly. Reduces throttling errors by ~80% for high-volume NetSuite customers. (REL-2042)
- **fix:** Agent heartbeat was sending the full config on every heartbeat (every 30 seconds), wasting bandwidth and causing issues for agents on slow connections. Now sends only a config hash, with full config only when hash changes. (REL-2045)

### Relay 2.8.0 (2025-10-15)

**Release manager:** Viktor Nowak

- **feat:** EDI X12 connector now supports 810 (Invoice) documents in addition to 850 (Purchase Order) and 856 (ASN). (REL-2010)
- **feat:** New monitoring endpoint on the agent: `GET /healthz/connectors` returns health status of each running connector. Used by customer ops teams to monitor their agents. (REL-2005)
- **feat:** Agent now supports proxy configuration for outbound connections. Requested by 4 Enterprise customers whose security policies require all outbound traffic through a proxy. (REL-2020)
- **fix:** Blue Yonder WMS connector: fixed date parsing for non-US date formats. European customers were getting parse errors on `DD/MM/YYYY` dates. (REL-2025)
- **fix:** Kafka producer was silently dropping messages when the broker was temporarily unreachable (instead of buffering locally). Root cause: incorrect error handling in the `acme-kafka` library. Fix applied in both the library and the agent. (REL-2028)

## Known Limitations

1. **SAP IDoc memory leak**: The SAP RFC connector leaks memory when processing IDocs larger than 50MB. The agent's memory grows unbounded and eventually gets OOM-killed. Workaround: set `max_idoc_size_mb: 40` in the connector config and configure SAP to split large IDoc batches. Fix scheduled for Relay 3.2 (Q2 2026). James's team has identified the root cause (the SAP JCo library's byte buffer isn't being freed correctly in our cgo bridge) but the fix is complex.

2. **EDI X12 856 nesting limit**: The EDI parser doesn't handle hierarchical loop nesting deeper than 4 levels. Affects about 3% of customers using complex ASN structures (mostly automotive and electronics distributors). Workaround: flatten the ASN structure on the customer side before sending to Relay. This is a hard problem — Amy Zhang has been reluctant to prioritize it because the fix requires a rewrite of the HL loop parser.

3. **NetSuite rate limiting**: Despite improvements in 2.8.1, customers with very large catalogs (100K+ SKUs) still experience throttling during initial full sync. The initial sync can take 12-48 hours depending on catalog size. Workaround: run initial sync over a weekend, or ask the customer to temporarily increase their NetSuite API concurrency limit (if they have that option on their NetSuite plan).

4. **Connector hot-reload failures**: About 5% of connector hot-reloads fail with "plugin already loaded" errors, requiring a full agent restart. This is a Go plugin system limitation. We've been talking about switching to HashiCorp's go-plugin (gRPC-based) but it's a major refactor. On the radar for Relay 4.x.

5. **Single-agent throughput**: A single Relay agent tops out at about 10K records/second sustained throughput. For customers with higher volume (GlobalMart during Black Friday), we deploy multiple agents with topic partitioning. This works but requires manual configuration. Auto-scaling agents is on the roadmap.

6. **Schema mapping limitations**: The schema mapper doesn't support computed fields (e.g., concatenating two source fields into one target field). Workaround: use a custom connector with pre-processing logic, or request the customer to create a computed field in their source system. The v2 schema mapper (behind flag, internal only) will support this.

## Migration Guide: Relay 2.x to 3.x

### Overview

Relay 3.0 was a major release that changed the agent config format and connector plugin interface. All agents running 2.x need to be upgraded.

**Timeline:**
- Relay 3.0 released: 2026-01-07
- 2.x agents still supported: until 2026-07-07 (6 months)
- After 2026-07-07: 2.x agents will not receive security patches

### Config Migration

Run the migration script on each agent host:

```bash
# Backup existing config
cp /etc/relay/agent.json /etc/relay/agent.json.bak

# Run migration
relay-agent migrate-config --input /etc/relay/agent.json --output /etc/relay/agent.yaml

# Verify
relay-agent validate-config --config /etc/relay/agent.yaml

# Restart agent with new config
systemctl restart relay-agent
```

Key config changes:
- Format changed from JSON to YAML
- `kafka.ssl` section renamed to `kafka.tls`
- `connector.polling_interval_seconds` renamed to `connector.poll_interval` (accepts duration strings like `60s`, `5m`)
- `buffer.max_size_bytes` renamed to `buffer.max_size_gb` (now in GB, not bytes)
- New required field: `agent.region` (set to the AWS region your data should be routed to)

### Custom Connector Migration

If you have a custom connector (only 3 customers: GlobalMart, Pacific Rim, HomeBase), the `Connector` interface has a new required method:

```go
// New in v2 — validates transformed records before sending to Kafka
Validate(records []CanonicalRecord) []ValidationError
```

For a quick migration, you can return nil (skip validation):

```go
func (c *MyConnector) Validate(records []CanonicalRecord) []ValidationError {
    return nil
}
```

But we strongly recommend implementing actual validation. See the `rest-generic` connector source for a good example.

### Agent Upgrade Process

For each customer agent:

1. Notify customer CS team of upcoming upgrade window
2. Stop the agent: `systemctl stop relay-agent`
3. Download new binary: `curl -O https://releases.acme.dev/relay/3.1.0/relay-agent-linux-amd64`
4. Replace binary: `mv relay-agent-linux-amd64 /usr/local/bin/relay-agent && chmod +x /usr/local/bin/relay-agent`
5. Migrate config (see above)
6. Start agent: `systemctl start relay-agent`
7. Verify in control plane dashboard: agent should show version 3.1.0 and all connectors green

Estimated downtime per agent: 5-10 minutes. During downtime, data will not sync but the source system continues operating normally. After restart, the agent will catch up from the last checkpoint.

NOTE: For customers with the `agent_auto_upgrade` flag enabled, steps 2-6 are handled automatically by the control plane. The agent will gracefully drain, upgrade, and restart with zero downtime. This is why we want to get the flag to 100% rollout ASAP.

---

# Beacon (Analytics & Reporting)

## Architecture Overview

Beacon is the analytics layer on top of Nexus data. It has three main components:

### Analytics Warehouse (ClickHouse)

All Nexus data is replicated to ClickHouse Cloud for analytics. The replication pipeline:

1. A Kafka consumer (`beacon-warehouse-sync`) reads from all relevant Nexus Kafka topics
2. Data is transformed into ClickHouse's columnar format with appropriate partitioning
3. Inserted into ClickHouse via the native protocol (not HTTP, for performance)

ClickHouse schema is optimized for analytical queries — heavy use of materialized views, projection tables, and pre-aggregated rollups. Sarah Kim's team maintains about 40 materialized views.

Current ClickHouse stats:
- 3-node cluster on ClickHouse Cloud
- ~2TB storage (growing ~50GB/month)
- ~1.5 billion rows across all tables
- Average query time: 200ms for dashboard queries, 2-5 seconds for complex ad-hoc queries

Tech debt note: The beacon-warehouse-sync consumer was written early and doesn't use the `acme-kafka` library. It has its own Kafka client code that's diverged significantly. Sarah's team wants to refactor this but it works and nobody wants to touch it. Classic "if it ain't broke" situation. — Marcus

### Dashboard Service (Metabase)

Beacon embeds Metabase (white-labeled as "Beacon Analytics") for dashboard rendering. We run Metabase 0.49 (the latest OSS version) with custom themes and branding.

The Metabase instance connects to ClickHouse via the ClickHouse JDBC driver. Each customer gets their own Metabase "collection" with row-level security enforced at the ClickHouse level (each customer's data is in a separate database).

Dashboard interactions:
1. User opens Beacon in the Nexus web app
2. Nexus frontend loads the Beacon iframe with a signed JWT
3. Metabase validates the JWT and shows the customer's dashboards
4. Dashboard queries hit ClickHouse directly

We have 40+ pre-built dashboard widgets that customers can drag-and-drop:
- Inventory heatmap (by location, by SKU category)
- Carrier performance scorecard (on-time %, damage rate, cost per shipment)
- Demand vs. actual chart (forecast accuracy visualization)
- Stockout risk matrix (SKUs likely to stockout in next 7/14/30 days)
- Replenishment pipeline (POs in progress, expected arrival dates)
- Shipment volume trends (by carrier, by route, by time period)
- Exception analysis (top exception types, root cause breakdown)
- Inventory turnover by category
- Lead time distribution by supplier
- Fill rate by location

### Report Generator

The `report-generator` service generates scheduled PDF and Excel reports. It uses chromedp (headless Chrome) for PDF rendering of dashboard widgets.

Flow:
1. Cron-based scheduler checks for due reports every minute
2. For each due report, it constructs a headless Chrome session
3. Chrome loads the Metabase dashboard with the customer's data
4. chromedp waits for rendering, then captures as PDF
5. PDF is stored in S3 and emailed to the configured recipients via SES

This is honestly the most fragile part of Beacon. Chrome rendering is unpredictable — sometimes widgets take longer to load, sometimes Chrome OOMs on large dashboards. We've added retry logic and widget count limits but it's still the #1 source of Beacon support tickets.

TODO: David Park (Beacon PM) is exploring alternatives to chromedp. The leading option is server-side rendering with a Go PDF library (gofpdf or similar) but that means reimplementing all widget rendering logic. Another option is wkhtmltopdf. No decision yet.

Excel report generation is simpler — direct data query from ClickHouse, formatted into XLSX using the `excelize` Go library. Very reliable.

## Configuration Reference

### beacon-api

| Variable | Default | Description |
|----------|---------|-------------|
| `BEACON_PORT` | `8100` | HTTP listen port |
| `BEACON_DB_URL` | (required) | PostgreSQL connection string (Beacon metadata) |
| `BEACON_CLICKHOUSE_URL` | (required) | ClickHouse native protocol URL |
| `BEACON_CLICKHOUSE_DB_PREFIX` | `customer_` | Database name prefix in ClickHouse |
| `BEACON_METABASE_URL` | (required) | Internal Metabase URL |
| `BEACON_METABASE_SECRET_KEY` | (required) | Secret for signing Metabase embed JWTs |
| `BEACON_JWT_EXPIRY` | `10m` | Embed JWT expiry time |
| `BEACON_SQL_PLAYGROUND_ENABLED` | `true` | Enable SQL Playground (Enterprise only) |
| `BEACON_SQL_QUERY_TIMEOUT` | `30s` | Max query execution time for SQL Playground |
| `BEACON_SQL_QUERIES_PER_HOUR` | `100` | Rate limit for SQL Playground queries |
| `BEACON_DATA_EXPORT_ENABLED` | `true` | Enable data export (Enterprise only) |
| `BEACON_EXPORT_S3_BUCKET` | `acme-beacon-exports` | S3 bucket for data exports |
| `BEACON_EXPORT_S3_REGION` | `us-west-2` | S3 region for data exports |
| `BEACON_EXPORT_EU_S3_BUCKET` | `acme-beacon-exports-eu` | S3 bucket for EU customer exports |
| `BEACON_EXPORT_EU_S3_REGION` | `eu-west-1` | S3 region for EU exports |

### report-generator

| Variable | Default | Description |
|----------|---------|-------------|
| `REPORT_PORT` | `8101` | HTTP listen port |
| `REPORT_DB_URL` | (required) | PostgreSQL for report metadata and schedules |
| `REPORT_CLICKHOUSE_URL` | (required) | ClickHouse for Excel reports |
| `REPORT_METABASE_URL` | (required) | Metabase URL for PDF rendering |
| `REPORT_CHROME_PATH` | `/usr/bin/chromium` | Path to Chrome/Chromium binary |
| `REPORT_CHROME_TIMEOUT` | `60s` | Max time for Chrome to render a page |
| `REPORT_CHROME_CONCURRENCY` | `5` | Max concurrent Chrome instances |
| `REPORT_S3_BUCKET` | `acme-beacon-reports` | S3 bucket for generated reports |
| `REPORT_SES_REGION` | `us-west-2` | AWS SES region |
| `REPORT_SES_FROM` | `reports@acme.dev` | From address for report emails |
| `REPORT_MAX_WIDGETS_PER_PAGE` | `8` | Max widgets per PDF page (prevents timeout) |
| `REPORT_RETRY_MAX` | `3` | Max retries for failed report generation |
| `REPORT_RETRY_DELAY` | `5m` | Delay between retries |

### Feature Flags

| Flag | Status | Description |
|------|--------|-------------|
| `ff:beacon:clickhouse_query_cache` | 100% rollout | Query result caching for repeated dashboard queries |
| `ff:beacon:streaming_export` | Enterprise only | Real-time data streaming export to Snowflake/BigQuery |
| `ff:beacon:custom_sql_widgets` | 25% rollout | Allow customers to create widgets using custom SQL |
| `ff:beacon:excel_pivot_tables` | 50% rollout | Auto-generate pivot tables in Excel exports |
| `ff:beacon:dark_mode` | internal only | Dark mode for Beacon dashboards (Emma's team working on it) |

### Metabase Configuration

Metabase is deployed as a Docker container alongside beacon-api. Key settings:

```yaml
# metabase-config.yaml (passed via env vars to Metabase container)
MB_DB_TYPE: postgres
MB_DB_HOST: metabase-db.internal
MB_DB_PORT: 5432
MB_DB_DBNAME: metabase
MB_DB_USER: metabase
MB_EMBEDDING_SECRET_KEY: "${BEACON_METABASE_SECRET_KEY}"
MB_ENABLE_EMBEDDING: true
MB_ENABLE_PUBLIC_SHARING: false
MB_ANON_TRACKING_ENABLED: false
MB_CHECK_FOR_UPDATES: false
# Custom branding
MB_APPLICATION_NAME: "Beacon Analytics"
MB_APPLICATION_LOGO_URL: "https://cdn.acme.dev/beacon-logo.svg"
MB_APPLICATION_FAVICON_URL: "https://cdn.acme.dev/favicon.ico"
MB_SHOW_METABASE_LINKS: false
# Performance tuning
MB_ASYNC_QUERY_THREAD_POOL_SIZE: 50
MB_QUERY_CACHING_TTL_RATIO: 10
MB_QUERY_CACHING_MIN_TTL: 60
```

Quarterly Metabase upgrades are Sarah Kim's team's responsibility. The last upgrade (0.48 -> 0.49 in January 2026) took 2 days and broke the custom theme CSS. Emma Torres's frontend team had to fix the theme. We should really automate the theme testing.

## Changelog / Release History

### Beacon 2.5.0 (2026-02-11)

**Release manager:** Viktor Nowak
**Highlights:** Custom SQL widgets, Excel pivot tables

- **feat:** Custom SQL widgets allow Enterprise customers to create dashboard widgets backed by their own SQL queries. The SQL runs against ClickHouse with the same rate limits as SQL Playground. Behind flag `custom_sql_widgets`, 25% rollout. (BCN-890)
- **feat:** Excel exports now include auto-generated pivot tables for common report types (inventory summary, carrier performance). Behind flag `excel_pivot_tables`, 50% rollout. (BCN-875)
- **feat:** New widget: "Inventory Aging" showing inventory by age bucket (0-30, 31-60, 61-90, 90+ days). Requested by 15+ customers. (BCN-882)
- **fix:** Fixed ClickHouse query cache returning stale results after a data refresh. Cache key now includes the latest data timestamp. (BCN-895)
- **fix:** Report generator: increased Chrome heap size from 512MB to 1GB. Reduces OOM crashes on reports with many chart widgets. (BCN-892)
- **fix:** SQL Playground was allowing `DROP TABLE` and `ALTER TABLE` queries. Now restricted to SELECT-only. How did we miss this? Security team flagged it during audit. No customer exploited it. (BCN-901)

### Beacon 2.4.0 (2026-01-21)

**Release manager:** Viktor Nowak

- **feat:** Streaming data export to Snowflake. Enterprise customers can configure real-time data replication from Beacon's ClickHouse to their Snowflake instance. Uses Snowflake's Snowpipe Streaming SDK. Behind flag `streaming_export`. (BCN-850)
- **feat:** Dashboard sharing: customers can generate a signed link to share a dashboard (read-only) with external parties. Link expires after configurable duration (default 7 days). (BCN-840)
- **feat:** New widget: "Fill Rate Trend" showing order fill rate over time by location. (BCN-845)
- **fix:** Scheduled reports in PDF format were rendering with incorrect page margins on A4 paper size (default was US Letter). Now respects the customer's configured paper size. (BCN-860)
- **fix:** Data export to BigQuery was failing for customers with column names containing periods. ClickHouse allows periods in column names, BigQuery doesn't. Exporter now sanitizes column names. (BCN-855)
- **chore:** Upgraded ClickHouse JDBC driver from 0.4.6 to 0.5.0. Fixes connection pool exhaustion under high concurrency. (BCN-865)

### Beacon 2.3.1 (2025-12-30)

**Release manager:** Viktor Nowak
**Highlights:** Hotfix for Metabase embed

- **fix:** After the Metabase 0.49 upgrade, the embed JWT validation started rejecting tokens with more than 10 claims. Our JWTs have 12 claims (including custom customer metadata). Metabase introduced a max claims limit we didn't know about. Workaround: moved 3 custom claims into a nested `metadata` object. (BCN-852)
- **fix:** Dark mode CSS regression from Metabase upgrade. Temporarily reverted dark mode flag to disabled while Emma's team fixes the CSS. (BCN-853)

### Beacon 2.3.0 (2025-12-10)

**Release manager:** Viktor Nowak

- **feat:** ClickHouse query result caching. Repeated dashboard queries within the cache TTL (60 seconds) return cached results instead of hitting ClickHouse. Reduces ClickHouse load by ~40% during peak hours. Behind flag `clickhouse_query_cache`, now at 100%. (BCN-810)
- **feat:** New scheduled report frequency: "first business day of month". Previously only supported daily/weekly/monthly (on a fixed date). Requested by several finance teams. (BCN-800)
- **feat:** Dashboard templates: Sarah's team created 5 industry-specific dashboard templates (retail, grocery, fashion, electronics, general) that customers can use as starting points. (BCN-815)
- **fix:** Fixed timezone display in dashboard widgets. All times were showing in UTC even when customer configured a different timezone. Now respects the customer's configured timezone. (BCN-825)
- **chore:** Upgraded Metabase from 0.48 to 0.49. Two-day effort including theme fixes. (BCN-820)

### Beacon 2.2.0 (2025-11-05)

**Release manager:** Viktor Nowak

- **feat:** SQL Playground — direct SQL access to ClickHouse for Enterprise customers. Rate limited to 100 queries/hour with 30-second timeout. Includes query history and saved queries. (BCN-750)
- **feat:** Data export to BigQuery (in addition to existing S3 and Snowflake). (BCN-760)
- **feat:** Custom branding for scheduled reports: customers can upload their logo and choose brand colors for PDF reports. (BCN-770)
- **fix:** Inventory heatmap widget was miscounting items when a single SKU was present in multiple locations. Was summing quantity_on_hand instead of displaying per-location. (BCN-780)
- **refactor:** Moved report scheduling from in-memory cron to PostgreSQL-backed scheduler. Reports now survive service restarts without missing scheduled deliveries. (BCN-775)

## Known Limitations

1. **ClickHouse query complexity**: Complex ad-hoc queries in SQL Playground can consume significant ClickHouse resources. We've seen a few cases where a customer ran a massive cross-join that saturated the ClickHouse cluster for 20+ seconds, affecting other customers' dashboards. The 30-second timeout helps, but a single heavy query can still degrade shared performance. Sarah's team is investigating per-customer query resource limits in ClickHouse.

2. **PDF report widget limit**: Maximum 8 widgets per page in PDF reports to prevent Chrome timeout. Customers with complex dashboards (15+ widgets) need to split into multiple report pages. Not a technical limitation per se — we could increase the timeout, but Chrome memory usage grows linearly with widget count and we'd need bigger pods.

3. **Metabase version lag**: We're on Metabase 0.49 (January 2026 release) and Metabase ships monthly. We upgrade quarterly, so we're always 1-2 versions behind. This means customers don't get the latest Metabase features right away. We accept this tradeoff for stability.

4. **No real-time dashboards**: Dashboard data refreshes every 5 minutes (the ClickHouse sync lag). Customers who need real-time data have to use the Nexus API directly. David Park has gotten multiple requests for sub-minute dashboard refresh but the ClickHouse sync pipeline would need significant work.

5. **Excel export row limit**: Excel exports are limited to 1 million rows (Excel's own limit for XLSX). For larger datasets, customers need to use the S3 or Snowflake export (CSV format, no row limit). About 5 Enterprise customers regularly hit this limit.

6. **Dashboard sharing link security**: Shared dashboard links are signed JWTs but don't support IP restriction or additional authentication. If a link is leaked, anyone with the link can view the dashboard until it expires. Aisha Mohammed's security team has flagged this and we're adding optional password protection in Q2 2026.

---

# Sentinel (Compliance & Audit)

## Architecture Overview

Sentinel is the newest product (GA October 2025) and is still evolving rapidly. Raj Patel's team ships faster than any other team at ACME — they've averaged a release every 2 weeks since GA.

### Core Services

**sentinel-api** (port 8110): Main API gateway for Sentinel. Handles auth, request routing, and customer configuration management. PostgreSQL-backed with row-level security for multi-tenancy.

**screening-service** (port 8111): The denied party screening engine. Core of Sentinel's value prop. Uses Elasticsearch for fuzzy name matching against sanctions lists.

**classification-service** (port 8112): HS code classification powered by Claude API. Takes product descriptions and suggests harmonized system codes. Uses a human review queue for low-confidence predictions.

### Screening Service Architecture

The screening service is optimized for low latency (target: < 200ms p99):

```
                +-------------------+
                | screening-service |
                |      :8111        |
                +--------+----------+
                         |
           +-------------+-------------+
           |                           |
  +--------v---------+     +-----------v--------+
  | Elasticsearch    |     | Redis               |
  | (sanctions index)|     | (screening cache +  |
  | 500GB, 3 nodes   |     |  rate limiting)     |
  +------------------+     +---------------------+
```

**Sanctions list ingestion pipeline:**
1. Daily at 02:00 UTC, a cron job fetches the latest lists from 17 sources (BIS, OFAC, EU, UN, and 13 others)
2. Each list is parsed from its native format (XML, CSV, JSON — every agency has a different format, it's a nightmare)
3. Parsed entries are normalized into a canonical format and indexed into Elasticsearch
4. The index uses custom analyzers for fuzzy name matching: phonetic analysis (Metaphone), character folding (handles diacritics), and n-gram indexing
5. After indexing, a verification query runs against a set of known test entities to ensure the index is working correctly
6. Legal team (Patricia Nguyen's team) reviews new additions within 24 hours

**Screening query flow:**
1. Customer sends entity to screen via API
2. screening-service checks Redis cache first (cache key = normalized entity name + country + lists)
3. On cache miss, constructs an Elasticsearch query with fuzzy matching, phonetic matching, and alias matching
4. Results above similarity threshold (0.85 default, configurable per customer) are returned as potential matches
5. Results cached in Redis with 24-hour TTL (shorter TTL for lists that update more frequently)

The big design decision was caching: Raj's team debated whether screening results should ever be cached, since sanctions lists can change daily. The compromise is 24-hour TTL which means in the worst case, a result is 24 hours stale. For most customers this is fine. For customers that need real-time screening (e.g., FreshDirect Europe for high-value shipments), we offer a `?no_cache=true` parameter.

### Classification Service Architecture

The HS code classification service uses Claude API (claude-sonnet-4-5-20250514 currently) for initial classification:

```
                +------------------------+
                | classification-service |
                |        :8112           |
                +--------+---------------+
                         |
           +-------------+-------------+
           |             |             |
  +--------v---+  +------v------+  +---v-----------+
  | PostgreSQL |  | Claude API  |  | Review Queue  |
  | (history + |  | (sonnet-4-5)|  | (low-conf     |
  |  training) |  |             |  |  items)       |
  +------------+  +-------------+  +---------------+
```

**Classification flow:**
1. Customer submits product description, weight, material, intended use
2. classification-service first checks PostgreSQL for a previous classification of the same or very similar product (using trigram similarity matching)
3. If no match, sends to Claude API with a carefully crafted prompt including:
   - Product details from the customer
   - Relevant HS code section/chapter context
   - Examples of similar past classifications (few-shot)
   - Country-specific classification rules (US HTS vs. EU CN vs. others)
4. Claude returns suggested HS code with confidence score and reasoning
5. If confidence > 0.85: auto-classified, stored in PostgreSQL, returned to customer
6. If confidence 0.60 - 0.85: flagged for human review, returned as "pending_review"
7. If confidence < 0.60: flagged as "needs_manual_classification", not returned to customer

Human reviewers are 3 contracted customs brokers managed by Raj's team. They work out of a dedicated review queue UI (the "Sentinel Review Queue" internal tool). Average review time is 4 minutes per item. Current backlog is about 200 items (1-2 day turnaround).

The classification prompt is the most important piece of IP in Sentinel. Raj's team iterates on it constantly. Current prompt is version 47 (yes, forty-seven). They A/B test prompt changes against a golden set of 500 pre-classified items.

Tech debt: We're using claude-sonnet-4-5-20250514 because the classification prompt was tuned for it. We should evaluate newer models but prompt migration is expensive. Nadia Hassan (Sentinel PM) has it on the Q3 roadmap.

### Audit Trail

The audit trail is an append-only log of all Sentinel actions:
- Every screening request and result
- Every classification decision (auto and human-reviewed)
- Every certificate upload, update, and expiration alert
- Every configuration change

Stored in PostgreSQL with immutable rows (no UPDATE or DELETE permitted — enforced at the database level via triggers). 7-year retention as required for trade compliance. Current volume: ~5M records/month, growing 15% MoM.

SOC 2 Type II audit confirmed the integrity of the audit trail in November 2025.

## Configuration Reference

### sentinel-api

| Variable | Default | Description |
|----------|---------|-------------|
| `SENTINEL_PORT` | `8110` | HTTP listen port |
| `SENTINEL_DB_URL` | (required) | PostgreSQL connection string |
| `SENTINEL_AUTH_SERVICE_URL` | (required) | auth-service URL for token validation |
| `SENTINEL_SCREENING_SERVICE_URL` | `http://screening-service:8111` | Internal screening service URL |
| `SENTINEL_CLASSIFICATION_SERVICE_URL` | `http://classification-service:8112` | Internal classification service URL |
| `SENTINEL_CORS_ORIGINS` | `https://app.acme.dev` | Allowed CORS origins |
| `SENTINEL_LOG_LEVEL` | `info` | Log level |
| `SENTINEL_AUDIT_RETENTION_YEARS` | `7` | Audit trail retention period |
| `SENTINEL_CERT_EXPIRY_ALERT_DAYS` | `30,14,7,1` | Days before cert expiry to send alerts |
| `SENTINEL_MAX_CERTS_PER_CUSTOMER` | `10000` | Max certificates per customer |

### screening-service

| Variable | Default | Description |
|----------|---------|-------------|
| `SCREENING_PORT` | `8111` | HTTP listen port |
| `SCREENING_ES_URL` | (required) | Elasticsearch URL |
| `SCREENING_ES_INDEX` | `sanctions` | Elasticsearch index name |
| `SCREENING_REDIS_URL` | (required) | Redis for caching and rate limiting |
| `SCREENING_CACHE_TTL` | `24h` | Screening result cache TTL |
| `SCREENING_CACHE_ENABLED` | `true` | Enable result caching |
| `SCREENING_SIMILARITY_THRESHOLD` | `0.85` | Default match similarity threshold |
| `SCREENING_MAX_RESULTS` | `10` | Max matches returned per screening |
| `SCREENING_LIST_UPDATE_SCHEDULE` | `0 2 * * *` | Cron for sanctions list update |
| `SCREENING_LIST_SOURCES` | (see below) | Comma-separated list source IDs |
| `SCREENING_PHONETIC_ENABLED` | `true` | Enable phonetic name matching |
| `SCREENING_NGRAM_MIN` | `3` | Minimum n-gram length |
| `SCREENING_NGRAM_MAX` | `5` | Maximum n-gram length |
| `SCREENING_ES_TIMEOUT` | `5s` | Elasticsearch query timeout |
| `SCREENING_VERIFICATION_ENTITIES` | (internal) | Test entities for post-update verification |

### classification-service

| Variable | Default | Description |
|----------|---------|-------------|
| `CLASSIFICATION_PORT` | `8112` | HTTP listen port |
| `CLASSIFICATION_DB_URL` | (required) | PostgreSQL connection string |
| `CLASSIFICATION_ANTHROPIC_API_KEY` | (required) | Anthropic API key for Claude |
| `CLASSIFICATION_MODEL` | `claude-sonnet-4-5-20250514` | Claude model to use |
| `CLASSIFICATION_MAX_TOKENS` | `2048` | Max tokens for Claude response |
| `CLASSIFICATION_TEMPERATURE` | `0.1` | Temperature for Claude (low for consistency) |
| `CLASSIFICATION_AUTO_THRESHOLD` | `0.85` | Auto-classify above this confidence |
| `CLASSIFICATION_REVIEW_THRESHOLD` | `0.60` | Send to review above this, reject below |
| `CLASSIFICATION_PROMPT_VERSION` | `47` | Current prompt version |
| `CLASSIFICATION_FEW_SHOT_COUNT` | `5` | Number of few-shot examples in prompt |
| `CLASSIFICATION_SIMILARITY_SEARCH_LIMIT` | `10` | Max similar past classifications to check |
| `CLASSIFICATION_RATE_LIMIT_RPM` | `100` | Max classifications per minute (Claude API cost control) |
| `CLASSIFICATION_REVIEW_QUEUE_URL` | (required) | URL of the review queue service |
| `CLASSIFICATION_COUNTRY_RULES` | `US,EU,GB,JP,AU,CA` | Countries with specific classification rules loaded |

### Sanctions List Sources

The screening service ingests from these sources (configured in `screening-service`):

| Source ID | Name | Format | Update Freq | Entries |
|-----------|------|--------|-------------|---------|
| `ofac_sdn` | OFAC SDN List | XML | Daily | ~12,000 |
| `ofac_consolidated` | OFAC Consolidated | XML | Daily | ~15,000 |
| `bis_entity` | BIS Entity List | CSV | Weekly | ~600 |
| `bis_denied` | BIS Denied Persons | CSV | Weekly | ~800 |
| `eu_consolidated` | EU Consolidated List | XML | Daily | ~9,000 |
| `un_consolidated` | UN Consolidated List | XML | Weekly | ~1,200 |
| `uk_sanctions` | UK Sanctions List | CSV | Daily | ~4,500 |
| `au_sanctions` | Australia DFAT | CSV | Weekly | ~1,000 |
| `ca_sanctions` | Canada OSFI List | CSV | Weekly | ~2,000 |
| `jp_meti` | Japan METI End User | CSV | Monthly | ~500 |
| `hk_sanctions` | Hong Kong MAS List | CSV | Weekly | ~300 |
| `sg_mas` | Singapore MAS List | CSV | Monthly | ~800 |
| `ch_seco` | Switzerland SECO | XML | Weekly | ~3,500 |
| `nz_sanctions` | New Zealand | CSV | Monthly | ~400 |
| `fr_tresor` | France Tresor | XML | Weekly | ~2,000 |
| `world_bank` | World Bank Debarment | JSON | Monthly | ~1,500 |
| `interpol_red` | Interpol Red Notices | JSON | Daily | ~7,000 |

Total: ~62,100 entries across all lists. After deduplication (many entities appear on multiple lists): ~48,000 unique entities.

### Feature Flags

| Flag | Status | Description |
|------|--------|-------------|
| `ff:sentinel:batch_screening` | 100% rollout | Batch screening API (up to 100 entities per request) |
| `ff:sentinel:classification_v2_prompt` | 10% rollout | New classification prompt v48 (A/B testing) |
| `ff:sentinel:cert_ocr` | internal only | OCR-based certificate data extraction |
| `ff:sentinel:real_time_list_updates` | disabled | Push sanctions list updates in real-time (instead of daily batch) |
| `ff:sentinel:dual_model_classification` | disabled | Use two Claude models and compare results for high-value items |

## Changelog / Release History

### Sentinel 1.5.0 (2026-02-28)

**Release manager:** Viktor Nowak
**Highlights:** Batch screening, certificate OCR alpha

- **feat:** Batch screening API: `POST /v1/screening/batch` accepts up to 100 entities in a single request. Returns results asynchronously via webhook or polling. Reduces latency for customers who screen large shipments. Behind flag `batch_screening`, now at 100%. (SEN-445)
- **feat:** Alpha of certificate OCR. Upload a scanned certificate of origin (JPEG/PNG/PDF) and Sentinel will extract the key fields (issuer, product description, HS code, country of origin, validity dates) using Claude vision. Behind flag `cert_ocr`, internal only. Raj's team is testing accuracy. (SEN-430)
- **feat:** Screening result detail page in the UI now shows the specific sanctions list entry that matched, including the list source, date added, and any aliases. Previously just showed the similarity score. (SEN-440)
- **fix:** Fixed false positive screening matches for very short names (< 4 characters). The n-gram matching was too aggressive for short strings. Added a minimum name length check and boosted exact match scoring. (SEN-450)
- **fix:** Audit trail export was truncating entries at 10,000 rows. Switched to streaming CSV export with no row limit. (SEN-448)
- **chore:** Updated all sanctions list parsers for 2026 format changes. OFAC changed their XML schema (again). (SEN-455)

### Sentinel 1.4.0 (2026-02-04)

**Release manager:** Viktor Nowak
**Highlights:** Certificate management improvements

- **feat:** Certificate expiration dashboard: new widget showing all certificates expiring in the next 30/60/90 days, grouped by type and urgency. (SEN-400)
- **feat:** Certificate bulk upload: customers can now upload multiple certificates in a ZIP file. Previously limited to one at a time. (SEN-395)
- **feat:** Classification service now supports EU Combined Nomenclature (CN) codes in addition to US HTS codes. Prompt v47 includes EU-specific classification rules. Critical for FreshDirect Europe. (SEN-410)
- **fix:** Screening service was returning inconsistent results when the same entity was screened with different capitalizations. Normalized all entity names to lowercase before matching. (SEN-415)
- **fix:** Classification confidence scores were slightly inflated compared to actual accuracy. Recalibrated the confidence model using the golden test set. Average confidence dropped from 0.88 to 0.83 which is more honest. (SEN-420)
- **fix:** Audit trail timestamp was using the application server's timezone instead of UTC. All timestamps now consistently UTC. (SEN-425)

### Sentinel 1.3.0 (2026-01-14)

**Release manager:** Viktor Nowak

- **feat:** Custom screening rules: Enterprise customers can define additional screening criteria beyond sanctions lists. For example, Pacific Rim Distributors screens against their own internal restricted entities list. Rules stored in PostgreSQL, applied post-ES-query. (SEN-360)
- **feat:** Screening analytics: monthly summary of screening volume, match rate, false positive rate, average latency. Visible in Beacon (new Sentinel-specific dashboard template). (SEN-350)
- **feat:** Added 3 new sanctions list sources: France Tresor, World Bank Debarment List, Interpol Red Notices. Total now 17. (SEN-370)
- **fix:** Elasticsearch index rotation was not happening correctly. Old indices were never deleted, causing storage growth. Added an index lifecycle policy: indices older than 90 days are moved to warm storage, older than 365 days are deleted (raw data is archived to S3 first). (SEN-375)
- **refactor:** Moved classification prompt management from hardcoded Go strings to PostgreSQL-backed prompt templates. Raj's team can now iterate on prompts without code deployments. (SEN-365)

### Sentinel 1.2.0 (2025-12-03)

**Release manager:** Viktor Nowak

- **feat:** Classification history: customers can view all past HS code classifications with their confidence scores and reviewer notes. Searchable by product description, HS code, or classification date. (SEN-300)
- **feat:** Screening result webhook: customers can configure a webhook to receive screening results instead of polling. Useful for automated compliance workflows. (SEN-310)
- **feat:** Added Certificate Management module: track certificates of origin, phytosanitary certificates, FDA prior notice, and custom certificate types. Expiration alerting via email and Slack. (SEN-320)
- **fix:** Claude API timeout handling: if Claude doesn't respond within 30 seconds, classification now falls back to keyword-based HS code suggestion (lower confidence, but at least returns something). Previously would return a 504 error. (SEN-330)
- **fix:** Screening cache was not being invalidated when sanctions lists were updated. Cache now includes a list version hash in the key, so updates automatically invalidate relevant cached results. (SEN-335)

### Sentinel 1.1.0 (2025-11-05)

**Release manager:** Viktor Nowak
**Highlights:** First post-GA release

- **feat:** Configurable similarity threshold per customer. Some customers want more sensitive screening (lower threshold = more potential matches, more false positives). Default remains 0.85. (SEN-250)
- **feat:** Classification service now includes reasoning in the response: why the model chose a particular HS code. Customers love this — it helps them understand and validate the classification. (SEN-260)
- **feat:** API endpoint for bulk classification: `POST /v1/classification/batch` accepts up to 50 items. (SEN-270)
- **fix:** Fixed a bug where the screening service would return duplicate matches if an entity appeared on multiple sanctions lists. Now deduplicates by entity ID and returns which lists matched. (SEN-280)
- **fix:** Audit trail was not recording failed screening attempts (API errors). Now records all attempts regardless of outcome. (SEN-285)
- **perf:** Optimized Elasticsearch query for screening. Reduced average latency from 120ms to 45ms by switching from `bool` query with nested `should` clauses to a single `multi_match` with cross-fields. (SEN-275)

## Known Limitations

1. **Classification language support**: The HS code classification currently only works well for English product descriptions. Non-English descriptions are auto-translated using Claude before classification, but accuracy drops significantly for technical/specialized terms in languages with limited training data (particularly CJK languages). Accuracy for Japanese product descriptions is about 65% vs. 88% for English. Raj's team is building language-specific prompt variants for Q2 2026.

2. **Screening name matching for non-Latin scripts**: The phonetic matching (Metaphone) only works for Latin-script names. Arabic, Chinese, Japanese, and Korean names use n-gram matching only, which has a higher false positive rate (~8% vs. ~2% for Latin names). Raj is evaluating specialized phonetic algorithms for non-Latin scripts.

3. **Classification throughput**: The classification service is rate-limited to 100 requests/minute due to Claude API cost control. For customers with large product catalogs (10K+ items) that need initial bulk classification, the queue can back up significantly. FreshDirect Europe's initial classification took 3 days. Workaround: ACME runs bulk classification jobs overnight at reduced priority during customer onboarding.

4. **Audit trail query performance**: The audit trail table is append-only and growing fast (5M rows/month). Complex queries against the audit trail (especially date range + entity search) are getting slower. Raj's team is evaluating moving the audit trail to ClickHouse for better query performance while keeping the immutable PostgreSQL as the source of truth.

5. **Certificate management storage**: Certificates are stored as files in S3 with metadata in PostgreSQL. No full-text search of certificate content yet. Customers have to search by metadata fields (issuer, country, product, expiry date). The OCR feature (alpha) will eventually enable content-based search.

6. **Single-region screening**: The screening service currently runs only in us-west-2. EU customers' screening requests cross the Atlantic, adding ~80ms latency. FreshDirect Europe has complained. Deploying screening to eu-west-1 is on the Q2 2026 roadmap but requires replicating the Elasticsearch cluster.

---

# Cross-Product Roadmap: Q2-Q3 2026

This is a summary pulled from Lisa Nakamura's roadmap deck (last updated 2026-03-01). Subject to change based on Q1 results and customer feedback.

## Q2 2026 (April - June)

### Nexus
- **WebSocket inventory updates GA** (currently 50% rollout, target 100% by end of April). Priya Sharma's team.
- **Carrier v2 normalization engine** — complete rewrite of carrier status normalization using ML classification instead of rule-based mapping. Handles edge cases better (currently 4% of carrier events fail normalization and get manual review). Logistics Squad.
- **Multi-supplier replenishment GA** (currently 25%, target 100% by end of May). Intelligence Squad.
- **Oracle v4 forecast model** — next-gen forecast model with attention-based architecture. Chen Wei's data team is training it now. Expected 15% MAPE improvement over v3.2. Target: internal testing in May, beta in June.
- **GraphQL API** — Priya wants to revisit this. Michael Torres (Nexus PM) has been getting requests from 8 Enterprise customers who want more flexible queries. Decision: evaluate in April, build if justified.

### Relay
- **SAP IDoc memory leak fix** (REL-2301) — James Okafor's team has the fix for the cgo bridge memory issue. Target: Relay 3.2 in April.
- **Generic webhook PO push** — allow replenishment POs to be pushed to any system via configurable webhook, not just SAP/Oracle/NetSuite. (REL-2280)
- **Agent auto-upgrade GA** (currently 50%, target 100% by end of May).
- **Schema mapper v2** with JSONPath support and computed fields. Will unblock several customer requests. (REL-2250)
- **New connectors**: Coupa (procurement), Infor WMS. Amy Zhang has been getting requests.

### Beacon
- **Real-time dashboards** (sub-minute refresh). Requires rearchitecting the ClickHouse sync pipeline to use materialized views with near-real-time inserts. David Park is scoping this — it's a big effort.
- **Dashboard sharing password protection** — security requirement from Aisha's team. (BCN-920)
- **Report generator rewrite** — investigate alternatives to chromedp for PDF rendering. Decision point: April. If we go with Go-native PDF, this becomes a Q2-Q3 effort.
- **Dark mode GA** — Emma Torres's team is fixing the CSS. Target: April release. (BCN-910)

### Sentinel
- **EU-region screening deployment** — replicate screening service and Elasticsearch to eu-west-1. FreshDirect Europe has been asking for months. Raj Patel + Marcus Webb (Platform). (SEN-500)
- **Classification language support** — Japanese and Chinese product description support with language-specific prompts. (SEN-480)
- **Certificate OCR beta** — move from alpha to beta. Improve extraction accuracy, support more certificate formats. (SEN-470)
- **Real-time sanctions list updates** — instead of daily batch, push updates as they're published. Requires partnerships with list providers for real-time feeds. Nadia Hassan is investigating which providers offer this. (SEN-490)

### Platform / Cross-Cutting
- **Hire dedicated VP Engineering** — Dana Chen is currently dual-hatting as CTO and VP Eng. Target: have someone in seat by end of Q2. Alex Rivera (CEO) is leading this search.
- **ISO 27001 certification push** — target certification by Q3. Aisha Mohammed is leading the gap analysis.
- **Self-serve onboarding pilot** — currently all onboarding is implementation-team-assisted (6-8 weeks). Goal: mid-market customers can self-onboard for Nexus + Relay in under 2 weeks. Lisa Nakamura + Robert Kim are scoping this.

## Q3 2026 (July - September)

### Nexus
- **Shipment events migration to ClickHouse** — move historical shipment events (the 4B row table) to ClickHouse. Priya Sharma has wanted this for a year. Should dramatically improve Beacon's carrier analytics queries. Nexus Core + Beacon teams.
- **Oracle v4 forecast model GA** (if beta goes well in Q2).
- **SKU limit increase** — cursor-based pagination and UI lazy-loading to support 1M+ SKUs per customer. Needed for GlobalMart (approaching 500K).
- **Mobile app v2** — significant refresh of the React Native mobile app. Emma Torres's frontend team. Carlos Mendez (Design Lead) has mockups ready.

### Relay
- **go-plugin migration** — begin migration from Go's native plugin system to HashiCorp go-plugin (gRPC-based). Fixes the hot-reload reliability issues. Major refactor, will span Q3-Q4.
- **Agent auto-scaling** — automatically spin up additional agents when throughput exceeds single-agent capacity. Requires Kubernetes-based agent deployment. (REL-2400)
- **EDI X12 856 deep nesting support** — Amy Zhang finally agreed to prioritize the HL loop parser rewrite. (REL-2350)

### Beacon
- **Report generator v2** (if decision made in Q2 to rewrite, this is the build phase).
- **Alerting from dashboards** — set threshold alerts directly on dashboard widgets. Currently alerts are Nexus-only. (BCN-950)
- **Beacon API v2** — formal versioned API for Beacon, enabling third-party BI tool integration. (BCN-960)

### Sentinel
- **Claude model upgrade evaluation** — evaluate newer Claude models for classification. Need to re-tune the prompt. Nadia Hassan is planning a month-long evaluation. (SEN-550)
- **Automated compliance reporting** — generate compliance reports (screening summaries, classification audits) for customer submission to regulatory agencies. Different format per country. (SEN-530)
- **Dual-model classification** — use two Claude models and compare results for high-value items (>$50K). If they disagree, route to human review. Reduces classification errors for expensive shipments. (SEN-540)

### Platform / Cross-Cutting
- **ISO 27001 certification** (target: September 2026).
- **Multi-cloud exploration** — Alex Rivera wants to explore GCP or Azure as secondary cloud for customer flexibility. Marcus Webb to lead technical evaluation. No commitment to build, just evaluate.
- **Observability platform upgrade** — Tom Bradley's SRE team wants to migrate from self-hosted Prometheus/Grafana to Grafana Cloud. Reduces operational overhead. Evaluation in Q2, migration in Q3 if approved.
- **Developer portal relaunch** — Hiroshi Tanaka's DevRel team is rebuilding docs.acme.dev with better API reference, tutorials, and code samples. Target launch: August 2026.

---

# Appendix: Cross-Product Integration Points

This section documents how the four products connect to each other. Useful for debugging issues that cross product boundaries.

| Source | Destination | Integration | Notes |
|--------|-------------|-------------|-------|
| Relay | Nexus | Kafka topics (`inventory-cdc-*`, `shipment-events-raw`) | Primary data flow. If Relay stops, Nexus data goes stale. |
| Nexus | Beacon | Kafka topics (all normalized events) + PostgreSQL read replica | Beacon's ClickHouse is populated from Nexus data. |
| Nexus | Sentinel | API calls from Nexus UI to Sentinel API | Sentinel is embedded in the Nexus UI. Auth token passed through. |
| Sentinel | Nexus | Webhook callbacks for classification results | When a classification completes (auto or human-reviewed), Sentinel notifies Nexus. |
| Beacon | Sentinel | ClickHouse screening analytics | Sentinel screening data is replicated to ClickHouse for the screening analytics dashboard. |
| Relay | Sentinel | Shipment data enriched with HS codes | Relay can trigger Sentinel screening during data ingestion if configured. |
| Nexus | Relay | Replenishment PO push | replenishment-engine sends PO recommendations to Relay for ERP write-back. |
| All | auth-service | JWT validation | All services validate tokens against auth-service. |
| All | gateway | Inbound traffic | All external traffic routes through the gateway. |

## Common Cross-Product Issues

**Issue: Nexus dashboards show stale data but Relay agent is green**
Check the Kafka consumer lag for the customer's CDC topic. The agent may be publishing fine, but inventory-service might have a slow consumer. Look at `inventory-service` pod logs for errors.

**Issue: Beacon dashboard loads but shows "No data"**
The ClickHouse sync might be lagging. Check `beacon-warehouse-sync` consumer lag. Also check if the customer's ClickHouse database was created (it's created on first data sync — new customers may not have it yet if Relay hasn't completed initial sync).

**Issue: Sentinel classification stuck in "pending_review" for days**
Check the review queue backlog. If it's over 300 items, the 3 contract reviewers are overwhelmed. Escalate to Raj Patel. Also check if the classification confidence is consistently low for this customer — might indicate their product descriptions are too vague and need enrichment.

**Issue: Replenishment PO push fails silently**
Check the Relay agent logs for the ERP write-back connector. Most common cause: the customer's ERP API credentials expired. Check Secrets Manager. Also verify the connector version supports PO push (SAP connector >= 3.2.0, NetSuite >= 2.7.0, Dynamics >= 2.2.0).

---

*This document is maintained by Product Ops (Jennifer Liu). If you notice anything wrong or outdated, ping her on Slack or submit a PR to `acme/internal-docs`.*

*Last review: 2026-03-07 by Lisa Nakamura, Dana Chen*
