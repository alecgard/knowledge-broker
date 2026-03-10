# ACME Org -- Architecture Decision Records

This document contains the canonical set of Architecture Decision Records (ADRs)
for ACME Org engineering. ADRs are numbered sequentially and once accepted are
immutable -- only their status may change (e.g., Superseded, Deprecated).

Maintained by the Platform Team. Last reviewed: 2026-01-15.

---

## ADR-001: Go as Primary Backend Language

**Status:** Accepted
**Date:** 2019-06-12
**Deciders:** Dana Chen (CTO), Marcus Webb (Platform Lead), Alex Rivera (CEO)

### Context

When ACME was founded in 2019, the founding engineering team (5 people) needed to
choose a primary backend language for the initial platform build. The two serious
contenders were Go and Java. The team had experience in both languages. We
evaluated them against these criteria:

- **Time to first production deploy:** We had 4 months of runway before our first
  design partner (UrbanThreads) expected a working integration.
- **Operational simplicity:** As a small team, we could not afford complex
  deployment pipelines or JVM tuning sessions.
- **Concurrency model:** Supply chain software is inherently I/O-heavy -- we poll
  carrier APIs, stream ERP change data, and fan out notifications. We needed a
  language that made concurrent I/O straightforward.
- **Hiring:** Portland has a decent Go community (CloudFlare, HashiCorp remote
  folks) but Java would give us a larger general talent pool.

We also briefly considered Rust and TypeScript (Node.js). Rust was dismissed due
to learning curve for the team and slower iteration speed. Node.js was dismissed
due to concerns about CPU-bound forecast workloads and the team's preference for
static typing.

### Decision

We chose **Go** as the primary backend language for all services.

### Consequences

**Positive:**
- Single static binary per service made containerization trivial. Our Docker
  images are typically 15-25 MB (scratch base), which helped enormously when we
  later deployed Relay agents on customer infrastructure with limited bandwidth.
- Goroutines and channels mapped naturally to our carrier polling and event
  streaming patterns. The shipment-service processes 50K+ concurrent carrier
  status checks using a simple worker pool pattern.
- Fast compile times kept developer feedback loops tight. Even today, with ~800K
  lines of Go across all repos, a full `go build` for any service takes < 10
  seconds.
- Go's standard library and net/http were sufficient for our REST APIs without
  heavy frameworks. We use chi for routing but very little else.
- Hiring has been positive. Go developers tend to value simplicity, which aligns
  with our engineering culture.

**Negative:**
- Error handling verbosity is a constant gripe, especially from engineers who
  join from Python or TypeScript backgrounds. We've adopted some internal
  conventions (error wrapping with `fmt.Errorf("operation: %w", err)`) but it
  remains noisy.
- Generics (added in Go 1.18) arrived after we'd already written a lot of
  type-specific utility code. Our `acme-go-kit` library has several functions
  that could be consolidated with generics, but the refactor hasn't been
  prioritized.
- The forecast-engine is the one exception -- it's written in Python because the
  ML ecosystem in Go was not mature enough in 2020. This creates a boundary
  where the Intelligence Squad needs to maintain Python expertise alongside Go.

**Regrets:**
- None major. If we were starting today we would still choose Go. The one thing
  we'd do differently is establish stricter interface patterns earlier -- some of
  our older services have concrete type dependencies that make testing harder.

---

## ADR-002: PostgreSQL as Primary Relational Database

**Status:** Accepted
**Date:** 2019-06-20
**Deciders:** Dana Chen (CTO), Marcus Webb (Platform Lead)

### Context

We needed a relational database for transactional data: inventory positions,
shipment records, user accounts, and configuration. The evaluation was between
PostgreSQL and MySQL (specifically Aurora MySQL on AWS).

Key considerations:
- **JSON support:** Our carrier integrations produce semi-structured event
  payloads that vary by carrier. We needed a way to store these without
  maintaining 200+ carrier-specific schemas.
- **Concurrency:** MVCC implementation quality matters when you have high-write
  inventory position updates competing with high-read dashboard queries.
- **Extensibility:** We anticipated needing full-text search, geospatial
  queries (for location-based inventory), and possibly custom types.
- **AWS support:** Both PostgreSQL and MySQL are well-supported on RDS.
- **Row-level security:** We were already thinking about multi-tenancy and
  PostgreSQL's RLS was a differentiator (see ADR-007).

### Decision

We chose **PostgreSQL** (initially version 12, now running 16 on RDS).

### Consequences

**Positive:**
- JSONB columns have been invaluable. The `shipment_events` table stores raw
  carrier payloads in a JSONB column alongside normalized fields. This lets us
  debug carrier-specific issues without losing data fidelity.
- PostgreSQL's MVCC and connection handling has scaled well. Our busiest instance
  (inventory-service) handles ~15K transactions/second during peak hours.
- Row-level security (adopted later, ADR-007) has been a cornerstone of our
  multi-tenancy strategy for Sentinel.
- The ecosystem of extensions has been useful: pg_stat_statements for query
  analysis, PostGIS for the location-based inventory features we launched in
  2024, and pg_cron for scheduled maintenance tasks.
- Foreign data wrappers let the beacon-api query PostgreSQL tables directly
  for real-time metrics that don't need to go through ClickHouse.

**Negative:**
- RDS PostgreSQL autovacuum tuning has been a recurring pain point. We've had
  two production incidents (one SEV-2) caused by bloated tables where
  autovacuum couldn't keep up with our write rate on inventory_transactions.
  We now run a custom vacuum schedule during off-peak hours.
- PostgreSQL's logical replication is less mature than MySQL's binary log
  replication. Setting up CDC feeds to Kafka required Debezium, which added
  operational complexity.
- Connection pooling with pgbouncer has been necessary since day one. Direct
  connections from our microservices would exhaust PostgreSQL's connection
  limits quickly. PgBouncer is another piece of infrastructure to manage.

**Future considerations:**
- We are evaluating Aurora PostgreSQL for the inventory-service database to
  get better read scaling via Aurora replicas. Current read replica lag on
  standard RDS (async replication) occasionally causes stale reads on the
  dashboard. Target evaluation date: Q3 2026.

---

## ADR-003: Apache Kafka for Event Streaming

**Status:** Accepted
**Date:** 2020-02-15
**Deciders:** Dana Chen (CTO), Marcus Webb (Platform Lead), Priya Sharma (Nexus Lead)

### Context

As the platform grew beyond a single monolith into discrete services, we needed
an event streaming layer for asynchronous communication. Primary use cases:

1. **CDC events** from customer ERPs flowing through Relay into Nexus
2. **Shipment status updates** from carrier polling distributed to downstream
   consumers (alerts, dashboards, analytics)
3. **Inventory position changes** propagated to the forecast engine and
   replenishment engine

We evaluated Apache Kafka (via Amazon MSK) and RabbitMQ.

### Decision

We chose **Apache Kafka** via Amazon MSK (Managed Streaming for Kafka).

**Rationale:**
- Kafka's log-based architecture with configurable retention was critical. We
  need the ability to replay events -- when a new Beacon customer is onboarded,
  we replay 90 days of shipment events to populate their analytics warehouse.
  RabbitMQ's queue model (consume-and-ack) would require us to build a separate
  replay mechanism.
- Kafka's partitioning model maps naturally to our multi-tenant architecture.
  We partition by customer_id, which gives us per-customer ordering guarantees
  and natural isolation for consumer lag.
- MSK provided managed Kafka without the operational burden of running ZooKeeper
  (we later migrated to KRaft mode when MSK supported it in 2024).
- Kafka's consumer group model lets us scale consumers independently per
  service. The alert-service can have 5 consumers while the analytics pipeline
  has 20, all reading from the same topics.

### Consequences

**Positive:**
- Event replay has been used dozens of times -- for new customer onboarding,
  for rebuilding search indexes, and for backfilling data after bug fixes.
  This alone justified Kafka over RabbitMQ.
- Throughput has never been a bottleneck. Our 6-broker MSK cluster handles
  ~50K messages/second at peak with plenty of headroom.
- The ecosystem is mature. We use the Sarama Go client library (now replaced
  by franz-go as of 2025 after Sarama was deprecated) and Debezium for CDC.

**Negative:**
- Kafka's operational complexity is real even with MSK. Partition rebalancing,
  consumer lag monitoring, topic configuration (retention, compaction), and
  broker upgrades require dedicated attention from the Platform team.
- MSK costs are significant -- roughly $8K/month for our production cluster.
  RabbitMQ on EC2 or Amazon MQ would have been cheaper for our initial
  volume, though we've grown into the capacity.
- Debugging consumer lag issues is non-trivial. We've built custom Grafana
  dashboards and alerting, but when a consumer falls behind it can take time
  to determine if the issue is slow processing, rebalancing, or a stuck
  partition.
- Our `acme-kafka` library wraps franz-go but has accumulated tech debt around
  error handling and retry semantics. The Relay team and Nexus Core team have
  slightly different retry patterns, which has caused confusion.

**Regrets:**
- We should have invested in a schema registry (Confluent Schema Registry or
  AWS Glue Schema Registry) from the start. We've had several incidents
  caused by producer/consumer schema mismatches. We adopted Glue Schema
  Registry in 2024, but migrating existing topics was painful.

---

## ADR-004: ClickHouse for Analytics Warehouse

**Status:** Accepted
**Date:** 2023-04-10
**Deciders:** Sarah Kim (Beacon Lead), Dana Chen (CTO), Chen Wei (Data Team Lead)

### Context

The Beacon analytics product needed a columnar analytics database. For the first
two years, Beacon queries ran against PostgreSQL read replicas, which was
adequate for small customers but became untenable as data volumes grew. Our
largest customer (GlobalMart) has 500M+ shipment events and the aggregate
queries for their dashboards took 30+ seconds on PostgreSQL.

We evaluated ClickHouse (self-hosted and ClickHouse Cloud) and Snowflake.

Criteria:
- **Query latency:** Sub-second for typical dashboard queries (aggregations
  over time-series data with filters on customer_id, date range, SKU, location).
- **Ingestion rate:** Ability to ingest 100K+ events/second for real-time
  dashboards (Enterprise tier feature).
- **Cost:** At our data volumes (currently ~2TB, projected 10TB in 2 years),
  warehouse costs matter.
- **SQL compatibility:** Our Beacon SQL Playground exposes raw SQL access to
  customers, so we need standard SQL support.
- **Operational complexity:** How much DBA effort does this require?

### Decision

We chose **ClickHouse Cloud** (managed service).

**Rationale:**
- ClickHouse's columnar compression and vectorized query execution gave us
  10-100x query performance improvement over PostgreSQL for our analytical
  workloads. Queries that took 30 seconds on PostgreSQL complete in 200ms
  on ClickHouse.
- Cost comparison at our projected 10TB scale: ClickHouse Cloud estimated at
  $3,500/month vs. Snowflake estimated at $12,000/month (based on our query
  patterns and warehouse size). Snowflake's per-query compute pricing model
  penalizes the interactive, exploratory query patterns our SQL Playground
  enables.
- ClickHouse's MergeTree engine family provided exactly the right trade-offs:
  excellent compression for time-series data, efficient partition pruning by
  date, and good support for our multi-tenant query patterns via
  customer_id in the primary key.
- Real-time ingestion via Kafka engine tables allowed us to build the
  streaming analytics pipeline for Enterprise customers without an
  intermediate ETL layer.

### Consequences

**Positive:**
- Dashboard load times dropped from 5-15 seconds to under 1 second for 95%
  of customers. This was the single biggest driver of Beacon NPS improvement
  (from +32 to +51 in H2 2023).
- The SQL Playground became viable as a product feature. Running customer
  ad-hoc queries against ClickHouse is fast and cost-effective, whereas on
  Snowflake each query would have incurred compute costs that made the
  feature economically questionable.
- ClickHouse's materialized views handle our pre-aggregation needs for common
  dashboard widgets, reducing query complexity and latency further.

**Negative:**
- ClickHouse SQL is not fully standard. Several customers have reported issues
  with their SQL Playground queries that work in PostgreSQL but fail in
  ClickHouse (e.g., certain JOIN patterns, window function edge cases). We
  maintain a "ClickHouse SQL Guide" in our docs.
- The Beacon team needed to learn ClickHouse-specific concepts: MergeTree
  variants, partition management, mutation operations (which are async and
  heavy). This was a 2-3 month learning curve.
- ClickHouse Cloud had some early reliability issues (2 outages in our first
  6 months). Reliability has improved significantly since then.
- Backfill operations are slow. When we need to reprocess historical data
  (e.g., after a bug in the ingestion pipeline), ClickHouse's mutation
  mechanism is not designed for bulk updates. We end up dropping and
  recreating partitions.

**Future considerations:**
- We're evaluating whether to move internal analytics (Data Team's Snowflake
  instance) to ClickHouse as well to consolidate the analytics stack. Decision
  expected Q2 2026.

---

## ADR-005: Multi-Repo Architecture

**Status:** Accepted (Revisited 2024-09, reaffirmed)
**Date:** 2019-07-01 (original), 2024-09-15 (revisit)
**Deciders:** Dana Chen (CTO), Marcus Webb (Platform Lead), all team leads

### Context (2019)

At founding, we chose between a monorepo (single repository for all services)
and a multi-repo approach (one repository per service/library). At the time we
had 3 services and 5 engineers.

### Decision (2019)

We chose **multi-repo** -- each service gets its own Git repository.

**Rationale (2019):**
- Simpler CI/CD: each repo has its own pipeline, no need for build graph
  analysis to determine what changed.
- Clearer ownership: repository-level CODEOWNERS maps directly to team
  ownership.
- Independent release cadence: teams can deploy without coordinating with
  other teams' changes.
- Familiarity: the team was more familiar with multi-repo workflows.

### Revisit Context (2024)

By 2024 we had grown to 16 repositories and 120 engineers. Several pain points
prompted a revisit:

- **Shared library versioning:** Our internal Go libraries (`acme-go-kit`,
  `acme-kafka`, `acme-pgx`) require version bumps and separate PRs in every
  consuming repo when updated. A breaking change in `acme-go-kit` v2.3 took
  3 weeks to fully propagate across all services.
- **Cross-cutting refactors:** Updating the OpenTelemetry SDK across all
  services required 16 separate PRs. Some repos lagged for months.
- **Inconsistent tooling:** Each repo had slightly different CI configurations,
  linter settings, and Makefile targets despite our template repo.
- **Dependency drift:** Services ran different versions of shared dependencies,
  occasionally causing subtle compatibility issues.

### Decision (2024 Revisit)

We **reaffirmed multi-repo** but introduced mitigations:

1. **Renovate Bot:** Automated dependency update PRs across all repos,
   including internal library version bumps. SLA: all repos must be within
   1 minor version of latest internal libraries.
2. **Template sync:** GitHub Actions workflow that propagates CI config,
   linter config, and Makefile changes from a template repo to all service
   repos. Runs weekly, creates PRs automatically.
3. **Shared CI actions:** Common CI steps extracted into reusable GitHub
   Actions stored in `acme/github-actions` repo.

### Consequences

**Positive:**
- Teams retain autonomy. The Relay team can do 3 deploys/day without
  worrying about Nexus Core's test suite.
- Repository-level permissions and audit trails are straightforward.
- Git history is clean and per-service, making bisection and blame easy.
- The 2024 mitigations (Renovate, template sync) have significantly reduced
  the shared library version drift problem.

**Negative:**
- Developer experience for cross-service work is poor. An engineer working on
  a feature that spans inventory-service and shipment-service needs two PRs,
  two CI runs, and careful deployment ordering.
- The template sync mechanism is imperfect -- repos sometimes diverge from
  the template due to legitimate customizations, and the sync PR requires
  manual conflict resolution.
- Onboarding new engineers takes longer because they need to understand the
  repo landscape and which service owns what.
- Code search across all repos requires GitHub Code Search or a local
  multi-repo checkout. Neither is as seamless as a monorepo `grep`.

**Future considerations:**
- If we exceed 25 repositories, we should revisit again. The overhead of
  multi-repo tooling increases non-linearly with repo count. A hybrid
  approach (monorepo for core platform, separate repos for Relay connectors)
  might make sense at that scale.

---

## ADR-006: REST for Service-to-Service Communication

**Status:** Accepted
**Date:** 2020-03-01
**Deciders:** Dana Chen (CTO), Marcus Webb (Platform Lead), Priya Sharma (Nexus Lead)

### Context

As we decomposed the initial monolith into microservices, we needed a standard
for synchronous service-to-service communication. The main candidates were REST
(HTTP/JSON) and gRPC (HTTP/2, Protocol Buffers).

Use cases for synchronous calls:
- `nexus-api` calling `inventory-service` and `shipment-service` to assemble
  dashboard data
- `screening-service` calling `auth-service` to validate tokens
- `relay-control-plane` calling `relay-agent` for health checks and config
  pushes
- `report-generator` calling `beacon-api` for dashboard data

### Decision

We chose **REST (HTTP/JSON)** for all service-to-service communication.

**Rationale:**
- **Simplicity:** Every engineer on the team was already proficient with REST.
  gRPC would have required training on Protocol Buffers, code generation
  pipelines, and gRPC-specific debugging tools.
- **Debuggability:** HTTP/JSON is human-readable. We can curl any internal
  endpoint, inspect payloads in logs, and use standard HTTP tooling (Postman,
  httpie) for testing. gRPC binary encoding makes ad-hoc debugging harder.
- **Customer API consistency:** Our external API is REST. Using REST internally
  means the same patterns, middleware, and tooling (auth, rate limiting,
  tracing) apply uniformly. With gRPC internally, we'd maintain two protocol
  stacks.
- **Load balancer compatibility:** Standard HTTP load balancing on AWS ALB/NLB
  is straightforward. gRPC requires HTTP/2-aware load balancing which had
  limited support on AWS in 2020.
- **Relay agent constraints:** Relay agents run on customer infrastructure
  behind corporate firewalls. HTTP/443 is universally allowed; HTTP/2 and
  gRPC ports sometimes are not.

### Consequences

**Positive:**
- Developer productivity is high. New endpoints are quick to build and test.
  Our chi-based routing with middleware for auth, tracing, and logging is
  well-understood by all engineers.
- Troubleshooting is straightforward. Service mesh observability (we don't
  actually use a service mesh -- just OpenTelemetry) captures HTTP method,
  path, status code, and latency natively.
- The external and internal API surfaces use identical patterns, reducing
  cognitive overhead.

**Negative:**
- **Performance:** JSON serialization/deserialization is measurably slower than
  Protocol Buffers. For the `nexus-api` -> `inventory-service` path, which is
  the hottest internal call (~10K RPS), we estimate ~2ms overhead per request
  from JSON encoding. This is acceptable but not free.
- **Type safety:** REST APIs defined with OpenAPI specs are loosely typed
  compared to gRPC's protobuf contracts. We've had incidents where a service
  changed a response field type (integer to string) without updating
  consumers. We now lint OpenAPI specs in CI, but this is less rigorous than
  protobuf compilation.
- **Streaming:** REST doesn't natively support bidirectional streaming. For the
  real-time dashboard updates feature, we use WebSockets alongside REST, which
  adds complexity. gRPC streaming would have been a more natural fit.
- **Payload size:** For bulk data transfer between services (e.g., forecast
  engine returning 90 days x 10K SKUs of forecast data), JSON payloads can
  be large. We use gzip compression, but protobuf would be ~3x smaller on
  the wire.

**Regrets:**
- The performance concern has grown as traffic increased. In hindsight, using
  gRPC for high-throughput internal paths (nexus-api <-> inventory-service,
  nexus-api <-> shipment-service) while keeping REST for everything else
  would have been a pragmatic hybrid approach. Converting now would be a large
  effort with unclear ROI, so we've opted to accept the overhead.

---

## ADR-007: Row-Level Security for Multi-Tenancy

**Status:** Accepted
**Date:** 2021-05-20
**Deciders:** Dana Chen (CTO), Raj Patel (Sentinel Lead), Aisha Mohammed (Security Lead)

### Context

The Sentinel product (compliance & audit) has strict data isolation requirements.
Customers performing denied party screening must not be able to see each other's
screening requests or results, even accidentally. Regulatory requirements (SOC 2,
upcoming ISO 27001) demand demonstrable tenant isolation.

We evaluated three multi-tenancy approaches:
1. **Separate databases per tenant** -- strongest isolation but operationally
   expensive at scale (1,200 customers = 1,200 database instances).
2. **Separate schemas per tenant** -- one database, separate PostgreSQL schemas.
   Good isolation, moderate operational overhead.
3. **Shared schema with row-level security** -- single schema, PostgreSQL RLS
   policies filter rows by tenant_id. Simplest operationally but relies on
   RLS policy correctness.

### Decision

We chose **row-level security (RLS)** in PostgreSQL for multi-tenant data
isolation in Sentinel. Other services (Nexus, Relay, Beacon) use application-
level tenant filtering but without RLS enforcement.

**Rationale:**
- At 1,200 customers (and growing), separate databases or schemas per tenant
  would create massive operational overhead. Database migrations alone would
  require running against 1,200 instances/schemas.
- PostgreSQL RLS provides database-enforced isolation. Even if application code
  has a bug that omits a WHERE clause, the RLS policy prevents cross-tenant
  data access. This defense-in-depth was a key requirement from our Security
  team for SOC 2 compliance.
- RLS policies are defined once per table and apply transparently to all
  queries. Application code sets `SET app.current_tenant = '{tenant_id}'` on
  each connection, and RLS policies filter automatically.
- Performance impact of RLS is minimal for our query patterns. The tenant_id
  column is the leading column in our composite primary keys and indexes, so
  the RLS filter aligns with index scans.

### Consequences

**Positive:**
- Data isolation is enforced at the database level. During our SOC 2 audit,
  the auditors specifically called out RLS as a strong control. This was a
  differentiator vs. competitors who rely solely on application-level filtering.
- Schema management is simple -- one set of tables, one set of migrations.
  Adding a new column to `screening_results` requires one migration, not 1,200.
- Query patterns are clean. Application code doesn't need to include
  `WHERE tenant_id = ?` in every query -- RLS handles it transparently.
- We can still run cross-tenant analytics (e.g., for internal metrics) by
  using a superuser role that bypasses RLS.

**Negative:**
- RLS adds complexity to database debugging. When an engineer connects to the
  database directly, they must remember to SET the tenant context, otherwise
  queries return no rows. This has confused new engineers multiple times. We
  added a warning banner to our database access tool (pgAdmin) as a reminder.
- Testing RLS policies requires explicit test cases. We have a dedicated test
  suite that verifies tenant isolation -- it inserts data for tenant A, sets
  context to tenant B, and asserts zero rows returned. This test suite runs
  on every migration.
- Connection pooling with RLS requires careful handling. PgBouncer in
  transaction mode works, but we must SET the tenant context at the start of
  every transaction, not just at connection time. This added complexity to
  our `acme-pgx` library.
- RLS is PostgreSQL-specific. If we ever need to migrate away from PostgreSQL,
  we'd need to re-implement tenant isolation at the application layer.

**Future considerations:**
- We're evaluating extending RLS to Nexus Core services (inventory-service,
  shipment-service) to provide the same defense-in-depth. The main blocker is
  the `acme-pgx` library changes needed to support SET tenant context in the
  connection pool for high-throughput services. Target decision: Q2 2026.

---

## ADR-008: Custom Feature Flag System

**Status:** Accepted
**Date:** 2022-08-15
**Deciders:** Viktor Nowak (QA Lead), Dana Chen (CTO), Marcus Webb (Platform Lead)

### Context

We needed a feature flag system for graduated rollouts, A/B testing, and
per-customer feature enablement. The primary contender was LaunchDarkly, the
market-leading SaaS feature flag platform. We also considered building a
custom system.

Requirements:
- **Per-customer flags:** Ability to enable features for specific customer
  accounts (critical for beta programs and Enterprise-only features).
- **Percentage rollouts:** Gradual rollout from 0% to 100% with the ability to
  roll back instantly.
- **Targeting rules:** Enable by customer tier, region, or custom attributes.
- **Evaluation latency:** < 1ms per flag evaluation. Flags are evaluated in the
  hot path of every API request (for feature gating).
- **Audit trail:** Who changed a flag and when. Required for SOC 2.
- **Cost:** LaunchDarkly pricing at our scale (120 engineers, 1,200 customer
  contexts, ~500 flags) was quoted at $48K/year.

### Decision

We chose to **build a custom feature flag system** backed by Redis.

**Rationale:**
- **Cost:** $48K/year for LaunchDarkly vs. incremental Redis usage (~$200/month)
  plus 3 weeks of engineering time to build. Payback period: < 6 months.
- **Simplicity of our requirements:** We don't need LaunchDarkly's advanced
  features (experimentation, multi-armed bandit, complex targeting trees). Our
  use cases are straightforward: customer-level flags, percentage rollouts,
  and tier-based targeting.
- **Integration depth:** Building custom allowed us to integrate deeply with our
  existing auth middleware. Flag evaluation uses the same customer context
  (tenant_id, tier, region) that's already on every request.
- **Control:** We can customize flag evaluation semantics exactly to our needs.
  For example, our "kill switch" flags have a different evaluation path that
  short-circuits all other logic.
- **Latency:** Redis GET with our flag data model gives us ~0.2ms evaluation
  time. LaunchDarkly's SDK would add a local cache layer with similar
  performance, but our approach has fewer moving parts.

### Consequences

**Positive:**
- The system has been rock-solid. Zero downtime in 3.5 years. Flag evaluation
  p99 latency is consistently under 0.5ms.
- Cost savings have been substantial -- we've avoided ~$170K in LaunchDarkly
  fees over 3.5 years.
- The Launchpad admin portal integrates directly with our flag system, giving
  CSMs the ability to toggle customer-specific flags without engineering
  involvement.
- Simple mental model for engineers: flags are key-value pairs in Redis with
  JSON rule definitions. No SDK versioning, no external service dependency.

**Negative:**
- **Maintenance burden:** Viktor's QA team owns this system, but it competes
  with their primary responsibilities (test infrastructure, release
  engineering). When the flag system needs work, it's always lower priority
  than release blockers.
- **No UI polish:** Our flag management UI in Launchpad is functional but
  basic. LaunchDarkly's UI is significantly better for exploring flag state,
  viewing evaluation logs, and managing complex targeting rules. Engineers
  often resort to Redis CLI for debugging.
- **Limited experimentation:** We don't have A/B testing or experimentation
  capabilities. When the Product team wanted to A/B test pricing page
  layouts in 2024, we had to hack together a basic experiment framework on
  top of our flag system. LaunchDarkly would have had this out of the box.
- **Bus factor:** The flag evaluation engine (500 lines of Go) was primarily
  written by one engineer who has since left the company. The code is tested
  but not well-documented. Viktor has been meaning to document it.

**Regrets:**
- The build-vs-buy math was correct for our initial needs, but as Product's
  appetite for experimentation grows, we may end up spending more engineering
  time maintaining and extending our custom system than we saved. We should
  reassess annually.

---

## ADR-009: Claude API for Sentinel HS Code Classification

**Status:** Accepted
**Date:** 2025-03-20
**Deciders:** Raj Patel (Sentinel Lead), Dana Chen (CTO), Lisa Nakamura (VP Product)

### Context

The Sentinel product's HS Code Classification feature requires an LLM to analyze
product descriptions and classify them into 6-digit Harmonized System codes for
customs declarations. Accuracy is critical -- misclassification can result in
fines, shipment delays, or regulatory action for our customers.

We evaluated:
1. **OpenAI GPT-4o** -- most established LLM API, strong general reasoning.
2. **Anthropic Claude (claude-sonnet-4-5-20250514)** -- strong structured output, known for
   careful reasoning and fewer hallucinations.
3. **Fine-tuned open-source model (Llama 3)** -- lower cost, self-hosted,
   full control.
4. **Traditional ML classifier** -- supervised model trained on historical
   classification data.

Evaluation criteria:
- **Classification accuracy:** Measured against a test set of 5,000 product
  descriptions with known HS codes. Target: >92% accuracy at 6-digit level.
- **Structured output:** Must return valid HS codes in a specific JSON format
  with confidence scores and reasoning.
- **Consistency:** Same product description should receive the same
  classification across multiple invocations.
- **Latency:** < 3 seconds per classification (batch mode, not real-time).
- **Cost:** Per-classification cost at projected volume (500K/month).
- **Compliance:** Data processing terms compatible with our SOC 2 and GDPR
  requirements. Customer product data cannot be used for model training.

### Decision

We chose **Anthropic Claude API (claude-sonnet-4-5-20250514)** for HS code classification.

**Evaluation results:**
| Model              | 6-digit accuracy | Consistency | Cost/1K | Latency |
|--------------------|------------------|-------------|---------|---------|
| Claude claude-sonnet-4-5-20250514    | 94.2%            | 97.1%       | $3.80   | 1.8s    |
| GPT-4o             | 92.8%            | 93.4%       | $4.20   | 2.1s    |
| Llama 3 70B (FT)   | 89.5%            | 95.2%       | $0.90   | 3.5s    |
| Traditional ML      | 78.3%            | 99.8%       | $0.01   | 0.05s   |

**Rationale:**
- Claude scored highest on accuracy (94.2%) and consistency (97.1%). In our
  domain, consistency is critical -- if the same product gets different HS
  codes on different days, customers lose trust.
- Claude's structured output (tool use / function calling) reliably produces
  valid JSON with HS codes, confidence scores, and classification reasoning.
  GPT-4o had a 3.2% malformed output rate vs. Claude's 0.4%.
- Anthropic's data processing terms explicitly state that API inputs are not
  used for model training. This was a key requirement from Legal and our
  enterprise customers (especially FreshDirect Europe for GDPR).
- The cost difference vs. GPT-4o is minor at our volume (~$200/month savings
  for 500K classifications).
- Fine-tuned Llama 3 had lower accuracy and required us to manage GPU
  infrastructure (or use a hosting provider), adding operational complexity.
  The cost savings didn't justify the accuracy gap for a compliance product.
- Traditional ML was too inaccurate for a compliance use case. The 78.3%
  accuracy would require human review of >20% of classifications, defeating
  the purpose of automation.

### Consequences

**Positive:**
- Classification accuracy in production has been even higher than our eval:
  95.1% at 6-digit level over the first 6 months (measured against human
  reviewer corrections).
- Human review queue volume is manageable. Only ~5% of classifications go to
  human review (below confidence threshold of 0.85). Our 3 contract reviewers
  handle this comfortably.
- Customer feedback has been very positive. FreshDirect Europe reports 60%
  reduction in customs delays since adopting Sentinel's classification.
- Claude's reasoning output (explaining why a product was classified under a
  specific HS heading) is included in the audit trail, which customers value
  for compliance documentation.

**Negative:**
- **Vendor dependency:** We're dependent on Anthropic's API availability and
  pricing. A 2-hour Claude API outage in September 2025 caused classification
  to queue up, though our async architecture handled it gracefully (items
  were classified once the API recovered, no data loss).
- **Cost at scale:** At $3.80 per 1K classifications, our largest customer
  (Pacific Rim Distributors, 200K classifications/month) costs us $760/month
  in API fees for classification alone. As volume grows, we may need to
  revisit the fine-tuned model approach for high-volume, lower-complexity
  classifications.
- **Model version pinning:** We pin to a specific model version (claude-sonnet-4-5-20250514)
  for consistency. When Anthropic releases new versions, we need to re-run
  our evaluation suite before upgrading. This creates a lag in adopting
  improvements.

**Future considerations:**
- Evaluate a tiered approach: use the fine-tuned Llama model for "easy"
  classifications (confidence > 0.95 on the open-source model) and route
  only complex items to Claude. Could reduce API costs by 60-70%.
- Explore Claude's batch API for non-real-time classification workloads to
  reduce per-request cost.

---

## ADR-010: Amazon EKS for Container Orchestration

**Status:** Accepted
**Date:** 2020-01-10
**Deciders:** Dana Chen (CTO), Marcus Webb (Platform Lead), Tom Bradley (SRE Lead)

### Context

As we moved from a single EC2-based deployment to a microservices architecture,
we needed a container orchestration platform. The candidates:

1. **Amazon EKS (Elastic Kubernetes Service)** -- managed Kubernetes on AWS.
2. **Self-managed Kubernetes on EC2** -- full control, kOps or kubeadm for
   cluster management.
3. **Amazon ECS (Elastic Container Service)** -- AWS-native container
   orchestration, simpler than Kubernetes.
4. **HashiCorp Nomad** -- simpler alternative to Kubernetes, good Go ecosystem
   integration.

### Decision

We chose **Amazon EKS** for container orchestration.

**Rationale:**
- **Ecosystem:** Kubernetes has the largest ecosystem of tooling, documentation,
  and community support. Our choices for CI/CD (ArgoCD), monitoring
  (Prometheus), logging (Loki), and secret management (External Secrets
  Operator) all assume Kubernetes. Choosing ECS or Nomad would have limited
  our tooling options.
- **Managed control plane:** EKS manages the Kubernetes control plane (API
  server, etcd, scheduler), eliminating a significant operational burden
  compared to self-managed. Our SRE team (initially 2 people) could not have
  managed control plane upgrades and etcd backups on top of everything else.
- **AWS integration:** EKS integrates natively with IAM (IRSA for pod-level
  IAM roles), ALB (AWS Load Balancer Controller), and EBS/EFS for storage.
  These integrations reduce the amount of glue code and configuration we
  need to maintain.
- **Hiring signal:** Kubernetes experience is common and expected. Engineers
  joining ACME already know kubectl, Helm, and Kubernetes concepts. ECS and
  Nomad are niche by comparison.
- **Multi-region capability:** We run production clusters in us-west-2 and
  eu-west-1. EKS provides a consistent experience across regions. Self-managed
  Kubernetes across regions would have been significantly more complex.

We chose EKS over self-managed Kubernetes specifically for the managed control
plane. The cost premium (~$73/month per cluster for EKS) was negligible compared
to the engineering time saved on control plane operations.

We chose EKS over ECS because ECS's task definition model was less flexible than
Kubernetes manifests for our use cases. Specifically, sidecar containers (used
by our OpenTelemetry collector, relay agent proxies, and PgBouncer) are first-
class in Kubernetes pods but were only added to ECS later and with limitations.

We did not choose Nomad despite its simplicity and Go heritage. The ecosystem
gap was too wide -- we would have had to build or adapt monitoring, deployment,
and secret management tooling that comes free with Kubernetes.

### Consequences

**Positive:**
- EKS has been reliable. We've experienced zero control plane outages in 6
  years of operation. Control plane upgrades (we do one major version per
  year, with quarterly patches) have been smooth with managed node group
  rolling updates.
- The Kubernetes ecosystem has paid dividends. ArgoCD for GitOps, External
  Secrets Operator for secret injection, Karpenter for node autoscaling
  (adopted 2023, replaced Cluster Autoscaler), and cert-manager for TLS
  certificates all integrate seamlessly.
- Karpenter in particular has been a game-changer. It reduced our average
  node provisioning time from ~5 minutes (Cluster Autoscaler) to ~30 seconds
  and our compute costs by ~25% through better bin-packing and spot instance
  utilization.
- Kubernetes knowledge transfers well. Engineers moving between teams at ACME
  don't need to learn new deployment or operations tooling.

**Negative:**
- **Complexity:** Kubernetes is inherently complex. New engineers (especially
  those without prior K8s experience) face a steep learning curve. We run a
  mandatory "K8s at ACME" onboarding session (4 hours) for all new backend
  engineers.
- **YAML sprawl:** Each service has Kubernetes manifests (Deployment, Service,
  Ingress, HPA, PDB, ConfigMap, ServiceAccount, IRSA role). We've partially
  addressed this with Helm charts and a base chart in `acme/helm-charts`, but
  the total amount of YAML across all services is substantial.
- **EKS upgrade friction:** While EKS upgrades are smoother than self-managed,
  they still require testing. The EKS 1.25 upgrade (which removed PSP in
  favor of PSA) took the Platform team 2 weeks of preparation and testing.
  EKS 1.29 (current) required updating several deprecated APIs.
- **Cost visibility:** Kubernetes makes it hard to attribute compute costs to
  individual services or customers. We deployed Kubecost in 2024 to address
  this, but cost allocation is still approximate due to shared nodes and
  overprovisioning.
- **Managed node group limitations:** Early on, managed node groups had
  restrictions on custom AMIs and launch templates. We worked around these,
  and most limitations have been resolved in recent EKS versions.

**Future considerations:**
- Evaluate EKS Fargate for low-traffic internal services (report-generator,
  classification-service) to simplify node management for bursty workloads.
- Consider EKS Anywhere if we ever need to run Kubernetes on customer premises
  (currently Relay agents run as standalone Docker containers, not K8s pods).
