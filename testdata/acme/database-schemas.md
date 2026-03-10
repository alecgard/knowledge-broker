# ACME Org -- Database Schema Reference

This document describes the key PostgreSQL table schemas for ACME's core services.
All services use PostgreSQL 16 on RDS unless otherwise noted. Beacon analytics
tables are in ClickHouse (noted separately).

Last updated: 2026-02-20
Maintained by: Platform Team (Marcus Webb)

---

## inventory-service

Owner: Inventory Squad (Nexus Core)
Database: `inventory_prod` (RDS, r6g.2xlarge, Multi-AZ)

### inventory_positions

The primary table for real-time inventory state. Updated via CDC events from
customer ERPs through the Relay pipeline. This is the most heavily queried
table in the system (~15K reads/second at peak for dashboard loads).

```sql
CREATE TABLE inventory_positions (
    id              BIGSERIAL       NOT NULL,
    tenant_id       UUID            NOT NULL,
    sku             VARCHAR(64)     NOT NULL,
    location_id     VARCHAR(32)     NOT NULL,
    location_type   VARCHAR(16)     NOT NULL CHECK (location_type IN ('warehouse', 'store', 'in_transit')),
    quantity_on_hand    INTEGER     NOT NULL DEFAULT 0,
    quantity_reserved   INTEGER     NOT NULL DEFAULT 0,
    quantity_available  INTEGER     NOT NULL GENERATED ALWAYS AS (quantity_on_hand - quantity_reserved) STORED,
    safety_stock    INTEGER         NOT NULL DEFAULT 0,
    unit_cost       NUMERIC(12, 4),  -- nullable; not all customers provide cost data
    currency_code   VARCHAR(3)      DEFAULT 'USD',
    last_updated    TIMESTAMPTZ     NOT NULL DEFAULT NOW(),
    source_system   VARCHAR(32),     -- e.g. 'sap_s4hana', 'netsuite', 'dynamics365'
    source_ref      VARCHAR(128),    -- external system reference/document ID
    created_at      TIMESTAMPTZ     NOT NULL DEFAULT NOW(),
    updated_at      TIMESTAMPTZ     NOT NULL DEFAULT NOW(),

    PRIMARY KEY (tenant_id, sku, location_id)
);

-- Primary lookup pattern: tenant + optional SKU/location filters
CREATE INDEX idx_positions_tenant_sku ON inventory_positions (tenant_id, sku);
CREATE INDEX idx_positions_tenant_location ON inventory_positions (tenant_id, location_id, location_type);

-- For the "below safety stock" alert query
CREATE INDEX idx_positions_below_safety ON inventory_positions (tenant_id)
    WHERE quantity_available < safety_stock;

-- For freshness monitoring — find stale positions
CREATE INDEX idx_positions_last_updated ON inventory_positions (tenant_id, last_updated);
```

**Notes:**
- The `id` column (BIGSERIAL) exists for internal reference but is NOT the
  primary key. The natural key is (tenant_id, sku, location_id). This was a
  deliberate decision to support upserts from the CDC pipeline.
- `quantity_available` is a generated column. We considered computing it at
  query time but the index on the partial expression (below safety stock)
  requires a stored column.
- **Tech debt:** The `unit_cost` and `currency_code` columns were added in
  2024 for the replenishment engine's cost optimization feature. About 40%
  of customers don't provide cost data, so these are nullable. We should
  probably move cost data to a separate table to keep this table lean.
- **Planned change:** Adding `lot_number` and `expiry_date` columns for
  perishable goods tracking (Q3 2026, requested by FreshDirect Europe).

### inventory_transactions

Append-only log of every inventory change. Used for audit trail, reconciliation,
and feeding the analytics pipeline. Grows at ~2M rows/day across all tenants.

```sql
CREATE TABLE inventory_transactions (
    id              BIGSERIAL       PRIMARY KEY,
    tenant_id       UUID            NOT NULL,
    sku             VARCHAR(64)     NOT NULL,
    location_id     VARCHAR(32)     NOT NULL,
    transaction_type VARCHAR(24)    NOT NULL CHECK (transaction_type IN (
        'receipt', 'shipment', 'adjustment', 'transfer_in', 'transfer_out',
        'return', 'write_off', 'cycle_count', 'reservation', 'release'
    )),
    quantity_change  INTEGER        NOT NULL,  -- positive for additions, negative for removals
    quantity_after   INTEGER        NOT NULL,  -- snapshot of quantity_on_hand after this txn
    reference_type   VARCHAR(32),   -- 'purchase_order', 'sales_order', 'transfer_order', etc.
    reference_id     VARCHAR(128),  -- PO number, SO number, etc.
    source_system    VARCHAR(32),
    source_ref       VARCHAR(128),
    metadata         JSONB,         -- carrier-specific or ERP-specific extra fields
    created_at       TIMESTAMPTZ    NOT NULL DEFAULT NOW()
);

-- Partition by month for manageability. Oldest partitions archived to S3 after 18 months.
-- (Partitioning is done via pg_partman, not shown here)

CREATE INDEX idx_txn_tenant_sku ON inventory_transactions (tenant_id, sku, created_at DESC);
CREATE INDEX idx_txn_tenant_created ON inventory_transactions (tenant_id, created_at DESC);
CREATE INDEX idx_txn_reference ON inventory_transactions (tenant_id, reference_type, reference_id);
```

**Notes:**
- This table is partitioned by month using pg_partman. Each partition is
  roughly 60M rows. Partitions older than 18 months are detached and archived
  to S3 via pg_dump. The archival job runs the 1st of each month.
- **Tech debt:** The `metadata` JSONB column has grown into a dumping ground.
  Different transaction types store different fields in it with no schema
  validation. We've discussed adding a JSON Schema check constraint but
  haven't prioritized it.
- **Known issue:** Autovacuum struggles with this table during peak write
  periods. We run a manual VACUUM ANALYZE at 03:00 UTC daily (see ADR-002
  for context on the SEV-2 incident this caused).

### locations

Reference table for warehouses, stores, and transit hubs. Relatively small
(~50K rows total) and rarely updated.

```sql
CREATE TABLE locations (
    id              VARCHAR(32)     NOT NULL,  -- e.g. 'WH-PDX-01', 'ST-SEA-05'
    tenant_id       UUID            NOT NULL,
    name            VARCHAR(128)    NOT NULL,
    location_type   VARCHAR(16)     NOT NULL CHECK (location_type IN ('warehouse', 'store', 'distribution_center', 'cross_dock')),
    address_line1   VARCHAR(256),
    address_line2   VARCHAR(256),
    city            VARCHAR(128),
    state_province  VARCHAR(64),
    postal_code     VARCHAR(16),
    country_code    VARCHAR(2)      NOT NULL,
    latitude        NUMERIC(10, 7),
    longitude       NUMERIC(10, 7),
    timezone        VARCHAR(48)     DEFAULT 'UTC',
    capacity_units  INTEGER,        -- max storage units; null if not tracked
    is_active       BOOLEAN         NOT NULL DEFAULT TRUE,
    metadata        JSONB,          -- tenant-specific custom fields
    created_at      TIMESTAMPTZ     NOT NULL DEFAULT NOW(),
    updated_at      TIMESTAMPTZ     NOT NULL DEFAULT NOW(),

    PRIMARY KEY (tenant_id, id)
);

CREATE INDEX idx_locations_tenant_type ON locations (tenant_id, location_type) WHERE is_active = TRUE;
CREATE INDEX idx_locations_geo ON locations USING GIST (
    ST_SetSRID(ST_MakePoint(longitude, latitude), 4326)
) WHERE latitude IS NOT NULL;
```

**Notes:**
- The GiST index requires the PostGIS extension. Used by the "nearest warehouse"
  feature in the replenishment engine.
- `id` is tenant-assigned (e.g., their warehouse code). Combined with tenant_id
  forms the primary key.

---

## shipment-service

Owner: Logistics Squad (Nexus Core)
Database: `shipment_prod` (RDS, r6g.2xlarge, Multi-AZ)

### shipments

Core shipment tracking table. One row per shipment. Updated by the carrier
polling pipeline and webhook receivers.

```sql
CREATE TABLE shipments (
    id              VARCHAR(32)     NOT NULL,  -- ACME shipment ID, e.g. 'SHP-2026-0048291'
    tenant_id       UUID            NOT NULL,
    tracking_number VARCHAR(64),    -- carrier tracking number; nullable for pre-booking
    carrier_code    VARCHAR(16)     NOT NULL,
    carrier_service VARCHAR(32),    -- e.g. 'ground', 'express', '2day'
    status          VARCHAR(24)     NOT NULL DEFAULT 'booked' CHECK (status IN (
        'booked', 'label_created', 'picked_up', 'in_transit',
        'out_for_delivery', 'delivered', 'exception', 'returned', 'cancelled'
    )),
    origin_location_id      VARCHAR(32),
    destination_location_id VARCHAR(32),
    origin_address          JSONB,       -- full address object
    destination_address     JSONB,       -- full address object
    estimated_delivery      TIMESTAMPTZ,
    actual_delivery         TIMESTAMPTZ,
    weight_kg               NUMERIC(10, 3),
    dimensions_cm           JSONB,       -- {"length": 30, "width": 20, "height": 15}
    declared_value          NUMERIC(12, 2),
    currency_code           VARCHAR(3)   DEFAULT 'USD',
    item_count              INTEGER      NOT NULL DEFAULT 0,
    ship_date               DATE,
    delivery_date           DATE,
    transit_days_estimated  INTEGER,
    transit_days_actual     INTEGER,
    exception_code          VARCHAR(32),  -- carrier-specific exception reason
    exception_detail        TEXT,
    source_system           VARCHAR(32),
    source_ref              VARCHAR(128), -- customer's order number or reference
    raw_carrier_data        JSONB,        -- full carrier response, for debugging
    created_at              TIMESTAMPTZ   NOT NULL DEFAULT NOW(),
    updated_at              TIMESTAMPTZ   NOT NULL DEFAULT NOW(),

    PRIMARY KEY (tenant_id, id)
);

CREATE INDEX idx_shipments_tracking ON shipments (tracking_number) WHERE tracking_number IS NOT NULL;
CREATE INDEX idx_shipments_tenant_status ON shipments (tenant_id, status, created_at DESC);
CREATE INDEX idx_shipments_tenant_carrier ON shipments (tenant_id, carrier_code, created_at DESC);
CREATE INDEX idx_shipments_tenant_dates ON shipments (tenant_id, ship_date DESC);
CREATE INDEX idx_shipments_exception ON shipments (tenant_id, status, exception_code)
    WHERE status = 'exception';

-- For delivery SLA monitoring
CREATE INDEX idx_shipments_delivery ON shipments (tenant_id, estimated_delivery)
    WHERE status IN ('in_transit', 'out_for_delivery');
```

**Notes:**
- `raw_carrier_data` stores the full JSON response from the carrier API. This
  is invaluable for debugging carrier-specific issues but makes the table
  wide. We considered moving it to a separate table but the JOIN cost on
  high-frequency queries discouraged it.
- **Tech debt:** The `origin_address` and `destination_address` JSONB columns
  should have been normalized into a shared `addresses` table. Several queries
  do JSONB field extraction which is slower than indexed column lookups. This
  is on the Logistics Squad's backlog but keeps getting deprioritized.
- **Planned change:** Adding `carbon_emissions_kg` column for sustainability
  reporting feature (Q2 2026 roadmap item).

### shipment_events

Append-only event log for shipment lifecycle. Each carrier status update
creates a row. Feeds the real-time tracking UI and the analytics pipeline.

```sql
CREATE TABLE shipment_events (
    id              BIGSERIAL       PRIMARY KEY,
    tenant_id       UUID            NOT NULL,
    shipment_id     VARCHAR(32)     NOT NULL,
    event_type      VARCHAR(32)     NOT NULL,  -- mirrors shipment status values plus sub-events
    event_timestamp TIMESTAMPTZ     NOT NULL,   -- when the event occurred (carrier time)
    received_at     TIMESTAMPTZ     NOT NULL DEFAULT NOW(),  -- when we received it
    location_city   VARCHAR(128),
    location_state  VARCHAR(64),
    location_country VARCHAR(2),
    location_postal VARCHAR(16),
    carrier_status_code VARCHAR(32),  -- raw carrier status code
    carrier_status_desc VARCHAR(256), -- raw carrier status description
    details         TEXT,
    raw_event       JSONB,            -- full carrier event payload
    created_at      TIMESTAMPTZ      NOT NULL DEFAULT NOW()
);

-- Partitioned by month (pg_partman)
CREATE INDEX idx_sevents_shipment ON shipment_events (tenant_id, shipment_id, event_timestamp DESC);
CREATE INDEX idx_sevents_tenant_time ON shipment_events (tenant_id, event_timestamp DESC);
CREATE INDEX idx_sevents_type ON shipment_events (tenant_id, event_type, event_timestamp DESC);
```

**Notes:**
- Partitioned monthly, same strategy as inventory_transactions. ~100M rows/month
  across all tenants.
- `event_timestamp` vs `received_at`: carrier events often arrive out of order or
  delayed. We track both the carrier's reported time and our receipt time.
  Dashboard sorting uses `event_timestamp`; SLA monitoring uses `received_at`.

### carriers

Reference table for supported carriers. Managed by the Relay/Logistics team.

```sql
CREATE TABLE carriers (
    code            VARCHAR(16)     PRIMARY KEY,  -- e.g. 'fedex', 'ups', 'dhl'
    name            VARCHAR(128)    NOT NULL,
    tracking_url_template VARCHAR(512),  -- e.g. 'https://www.fedex.com/fedextrack/?trknbr={tracking_number}'
    api_type        VARCHAR(16)     NOT NULL CHECK (api_type IN ('rest', 'soap', 'edi', 'scrape')),
    polling_interval_sec INTEGER    NOT NULL DEFAULT 600,
    webhook_enabled BOOLEAN         NOT NULL DEFAULT FALSE,
    status_mapping  JSONB           NOT NULL,  -- maps carrier-specific statuses to ACME lifecycle
    rate_limit_rps  INTEGER         DEFAULT 10,
    auth_type       VARCHAR(16)     CHECK (auth_type IN ('api_key', 'oauth2', 'basic', 'certificate')),
    is_active       BOOLEAN         NOT NULL DEFAULT TRUE,
    notes           TEXT,
    created_at      TIMESTAMPTZ     NOT NULL DEFAULT NOW(),
    updated_at      TIMESTAMPTZ     NOT NULL DEFAULT NOW()
);
```

**Notes:**
- `status_mapping` is the critical field. Each carrier reports statuses in their
  own format (FedEx has ~60 status codes, UPS has ~40, DHL has ~30). This JSONB
  column maps each carrier code to our normalized lifecycle. Maintained manually
  by the Logistics Squad -- updating this mapping is one of the most common tasks
  when onboarding a new carrier.
- `api_type = 'scrape'` is used for 3 small regional carriers that don't offer
  APIs. We scrape their tracking pages. This is fragile and breaks periodically
  when they redesign their sites.

---

## forecast-engine

Owner: Intelligence Squad (Nexus Core)
Database: `forecast_prod` (RDS, r6g.xlarge, Multi-AZ)
Note: The forecast-engine itself is Python, but it uses the same PostgreSQL
infrastructure as Go services.

### forecasts

Metadata for forecast jobs. One row per forecast generation request.

```sql
CREATE TABLE forecasts (
    id              VARCHAR(32)     PRIMARY KEY,  -- e.g. 'FC-2026-00123'
    tenant_id       UUID            NOT NULL,
    status          VARCHAR(16)     NOT NULL DEFAULT 'pending' CHECK (status IN (
        'pending', 'processing', 'complete', 'failed', 'expired'
    )),
    requested_by    UUID,           -- user ID; null for scheduled forecasts
    sku_count       INTEGER         NOT NULL,
    horizon_days    INTEGER         NOT NULL DEFAULT 90,
    model_version   VARCHAR(32)     NOT NULL,  -- e.g. 'oracle-v3.2.1'
    include_confidence BOOLEAN      NOT NULL DEFAULT TRUE,
    processing_started_at TIMESTAMPTZ,
    processing_completed_at TIMESTAMPTZ,
    error_message   TEXT,
    created_at      TIMESTAMPTZ     NOT NULL DEFAULT NOW(),
    updated_at      TIMESTAMPTZ     NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_forecasts_tenant_status ON forecasts (tenant_id, status, created_at DESC);
CREATE INDEX idx_forecasts_tenant_recent ON forecasts (tenant_id, created_at DESC);
```

### forecast_results

Individual SKU-level forecast outputs. This is the large table -- each forecast
job produces (sku_count * horizon_days) rows.

```sql
CREATE TABLE forecast_results (
    id              BIGSERIAL       PRIMARY KEY,
    forecast_id     VARCHAR(32)     NOT NULL REFERENCES forecasts(id),
    tenant_id       UUID            NOT NULL,
    sku             VARCHAR(64)     NOT NULL,
    forecast_date   DATE            NOT NULL,
    predicted_demand    NUMERIC(12, 2) NOT NULL,
    confidence_lower    NUMERIC(12, 2),
    confidence_upper    NUMERIC(12, 2),
    confidence_level    NUMERIC(4, 3)  DEFAULT 0.90,
    seasonality_factor  NUMERIC(6, 4),  -- multiplier applied for seasonal adjustment
    trend_component     NUMERIC(12, 2), -- decomposed trend value
    created_at      TIMESTAMPTZ     NOT NULL DEFAULT NOW()
);

-- Partitioned by month on forecast_date (pg_partman)
CREATE INDEX idx_fresults_forecast ON forecast_results (forecast_id);
CREATE INDEX idx_fresults_tenant_sku ON forecast_results (tenant_id, sku, forecast_date);
CREATE INDEX idx_fresults_tenant_date ON forecast_results (tenant_id, forecast_date);
```

**Notes:**
- This table grows fast: a customer with 10K SKUs and a 90-day horizon produces
  900K rows per forecast run. We keep the latest 3 forecast runs per customer
  and archive older ones.
- **Tech debt:** The `forecast_id` foreign key creates a dependency on the
  `forecasts` table which makes partition management awkward. We've discussed
  removing the FK and relying on application-level consistency, but the DBA
  (on the Platform team) objects on principle.

### forecast_overrides

Manual overrides entered by customers through the Nexus UI. These take
precedence over model-generated forecasts.

```sql
CREATE TABLE forecast_overrides (
    id              BIGSERIAL       PRIMARY KEY,
    tenant_id       UUID            NOT NULL,
    sku             VARCHAR(64)     NOT NULL,
    override_date   DATE            NOT NULL,
    original_demand NUMERIC(12, 2),  -- the model's prediction that was overridden
    override_demand NUMERIC(12, 2)  NOT NULL,
    reason          TEXT,           -- customer's justification; optional
    created_by      UUID            NOT NULL,  -- user ID
    approved_by     UUID,           -- null if auto-approved (customer admins can self-approve)
    is_active       BOOLEAN         NOT NULL DEFAULT TRUE,
    created_at      TIMESTAMPTZ     NOT NULL DEFAULT NOW(),
    updated_at      TIMESTAMPTZ     NOT NULL DEFAULT NOW(),

    UNIQUE (tenant_id, sku, override_date)
);

CREATE INDEX idx_foverrides_tenant_sku ON forecast_overrides (tenant_id, sku, override_date)
    WHERE is_active = TRUE;
```

**Notes:**
- Overrides are relatively rare (~2% of forecast values have active overrides
  across all customers). The replenishment engine checks for overrides before
  using model predictions.
- The `reason` field is optional but we encourage customers to fill it in. It
  helps the Intelligence Squad understand override patterns and improve the
  model.

---

## screening-service (Sentinel)

Owner: Sentinel Team
Database: `sentinel_prod` (RDS, r6g.xlarge, Multi-AZ)
Row-level security enabled (see ADR-007).

### screening_requests

Inbound screening requests from customers. Each denied party check creates
one row.

```sql
CREATE TABLE screening_requests (
    id              VARCHAR(32)     PRIMARY KEY,  -- e.g. 'SCR-2026-99201'
    tenant_id       UUID            NOT NULL,
    entity_name     VARCHAR(512)    NOT NULL,
    entity_type     VARCHAR(16)     NOT NULL CHECK (entity_type IN ('individual', 'organization', 'vessel', 'aircraft')),
    country         VARCHAR(2),
    address         TEXT,
    additional_info JSONB,          -- optional: date of birth, ID numbers, aliases
    lists_requested TEXT[]          NOT NULL,  -- array of list codes, e.g. {'bis_entity', 'ofac_sdn'}
    status          VARCHAR(16)     NOT NULL DEFAULT 'pending' CHECK (status IN (
        'pending', 'processing', 'complete', 'failed'
    )),
    priority        VARCHAR(8)      NOT NULL DEFAULT 'normal' CHECK (priority IN ('low', 'normal', 'high', 'critical')),
    requested_by    UUID,
    api_key_id      UUID,           -- which API key was used (for audit)
    created_at      TIMESTAMPTZ     NOT NULL DEFAULT NOW(),
    completed_at    TIMESTAMPTZ
);

-- RLS policy: tenants can only see their own requests
ALTER TABLE screening_requests ENABLE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON screening_requests
    USING (tenant_id = current_setting('app.current_tenant')::UUID);

CREATE INDEX idx_screq_tenant_created ON screening_requests (tenant_id, created_at DESC);
CREATE INDEX idx_screq_status ON screening_requests (status) WHERE status IN ('pending', 'processing');
CREATE INDEX idx_screq_entity ON screening_requests (tenant_id, entity_name);
```

### screening_results

Results of each screening check. One row per (request, list) combination.
A single screening request against 3 lists produces 3 result rows.

```sql
CREATE TABLE screening_results (
    id              BIGSERIAL       PRIMARY KEY,
    request_id      VARCHAR(32)     NOT NULL REFERENCES screening_requests(id),
    tenant_id       UUID            NOT NULL,
    list_code       VARCHAR(32)     NOT NULL,  -- e.g. 'ofac_sdn', 'bis_entity', 'eu_consolidated'
    result          VARCHAR(16)     NOT NULL CHECK (result IN ('clear', 'potential_match', 'confirmed_match', 'error')),
    match_count     INTEGER         NOT NULL DEFAULT 0,
    matches         JSONB,          -- array of match objects with similarity scores, entry details
    processing_time_ms INTEGER     NOT NULL,
    screened_at     TIMESTAMPTZ     NOT NULL DEFAULT NOW(),
    reviewed_by     UUID,           -- null unless human review occurred
    review_decision VARCHAR(16)     CHECK (review_decision IN ('confirmed', 'false_positive', NULL)),
    review_notes    TEXT,
    reviewed_at     TIMESTAMPTZ
);

-- RLS policy
ALTER TABLE screening_results ENABLE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON screening_results
    USING (tenant_id = current_setting('app.current_tenant')::UUID);

CREATE INDEX idx_scres_request ON screening_results (request_id);
CREATE INDEX idx_scres_tenant_result ON screening_results (tenant_id, result, screened_at DESC);
CREATE INDEX idx_scres_review_pending ON screening_results (tenant_id)
    WHERE result = 'potential_match' AND review_decision IS NULL;
CREATE INDEX idx_scres_tenant_list ON screening_results (tenant_id, list_code, screened_at DESC);
```

**Notes:**
- The `matches` JSONB column stores the full match detail including similarity
  scores, matched name, entry ID, programs, and remarks. This is denormalized
  intentionally -- we never query individual match fields, we always return
  the full match set.
- The `reviewed_by` / `review_decision` / `reviewed_at` fields track human
  review of potential matches. The Sentinel Review Queue UI writes to these
  columns.
- **Compliance requirement:** Screening results must be retained for 7 years per
  US EAR and OFAC guidance. We do NOT archive or delete these partitions.

### sanctions_entries

Local cache of sanctions list entries. Populated by the daily sanctions list
update pipeline. The Elasticsearch index is the primary query target; this
table is the source of truth for rebuilding the index.

```sql
CREATE TABLE sanctions_entries (
    id              BIGSERIAL       PRIMARY KEY,
    list_code       VARCHAR(32)     NOT NULL,
    entry_id        VARCHAR(64)     NOT NULL,  -- list-specific ID (e.g., OFAC SDN number)
    entity_name     VARCHAR(512)    NOT NULL,
    entity_type     VARCHAR(16)     NOT NULL CHECK (entity_type IN ('individual', 'organization', 'vessel', 'aircraft')),
    aliases         TEXT[],
    country         VARCHAR(2),
    addresses       JSONB,
    identifiers     JSONB,          -- passport numbers, tax IDs, etc.
    programs        TEXT[],         -- e.g. {'SDGT', 'IRAN'}
    remarks         TEXT,
    source_url      VARCHAR(512),
    list_added_date DATE,
    list_updated_date DATE,
    is_active       BOOLEAN         NOT NULL DEFAULT TRUE,
    ingested_at     TIMESTAMPTZ     NOT NULL DEFAULT NOW(),
    updated_at      TIMESTAMPTZ     NOT NULL DEFAULT NOW(),

    UNIQUE (list_code, entry_id)
);

CREATE INDEX idx_sanctions_list ON sanctions_entries (list_code, is_active);
CREATE INDEX idx_sanctions_name ON sanctions_entries USING GIN (to_tsvector('simple', entity_name));
CREATE INDEX idx_sanctions_country ON sanctions_entries (country) WHERE is_active = TRUE;
CREATE INDEX idx_sanctions_updated ON sanctions_entries (updated_at DESC);
```

**Notes:**
- Currently ~850K active entries across all 18 sanctions lists. The daily
  update pipeline processes diffs (additions, removals, modifications) rather
  than full reloads.
- The GIN text search index on `entity_name` is used as a fallback when the
  Elasticsearch cluster is unavailable. Performance is ~10x slower than ES
  but acceptable for degraded-mode operation.
- **Tech debt:** The `aliases` field is stored as a PostgreSQL text array but
  the Elasticsearch index tokenizes and normalizes aliases differently. This
  causes occasional discrepancies between PG fallback results and ES results.
  We should normalize in the ingestion pipeline, not at query time.

---

## auth-service

Owner: Platform Team
Database: `auth_prod` (RDS, r6g.xlarge, Multi-AZ)

### users

User accounts for the Nexus web application and APIs.

```sql
CREATE TABLE users (
    id              UUID            PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id       UUID            NOT NULL,
    email           VARCHAR(256)    NOT NULL,
    display_name    VARCHAR(128)    NOT NULL,
    auth_provider   VARCHAR(16)     NOT NULL DEFAULT 'local' CHECK (auth_provider IN ('local', 'okta', 'google', 'saml')),
    external_id     VARCHAR(256),   -- IdP subject/user ID for SSO users
    password_hash   VARCHAR(256),   -- bcrypt; null for SSO-only users
    mfa_enabled     BOOLEAN         NOT NULL DEFAULT FALSE,
    mfa_secret      VARCHAR(64),    -- TOTP secret; encrypted at rest via application layer
    status          VARCHAR(16)     NOT NULL DEFAULT 'active' CHECK (status IN ('active', 'inactive', 'locked', 'pending_activation')),
    role_id         UUID            NOT NULL REFERENCES roles(id),
    last_login_at   TIMESTAMPTZ,
    login_count     INTEGER         NOT NULL DEFAULT 0,
    failed_login_count INTEGER     NOT NULL DEFAULT 0,
    locked_until    TIMESTAMPTZ,    -- set on 5 consecutive failed logins
    preferences     JSONB           DEFAULT '{}',  -- UI preferences, timezone, locale
    created_at      TIMESTAMPTZ     NOT NULL DEFAULT NOW(),
    updated_at      TIMESTAMPTZ     NOT NULL DEFAULT NOW(),

    UNIQUE (tenant_id, email)
);

CREATE INDEX idx_users_email ON users (email);  -- for login lookups (email is unique per tenant, not globally)
CREATE INDEX idx_users_tenant ON users (tenant_id, status);
CREATE INDEX idx_users_external ON users (auth_provider, external_id) WHERE external_id IS NOT NULL;
```

**Notes:**
- Email uniqueness is per-tenant, not global. A user with email@example.com
  can exist in multiple tenants (e.g., a consultant who works with multiple
  ACME customers).
- `mfa_secret` is encrypted at the application layer using AES-256 before
  storage. The column stores the base64-encoded ciphertext.
- **Tech debt:** The `password_hash` column uses bcrypt with cost factor 10.
  We should upgrade to cost factor 12 (or migrate to argon2id) but this
  requires rehashing on next login for all local-auth users. Ticket exists,
  not prioritized.
- **Planned change:** Adding `last_active_at` column to track session
  activity for idle timeout (security audit finding from January 2026).

### api_keys

API keys for programmatic access. Each key is scoped to a tenant and has
configurable permissions.

```sql
CREATE TABLE api_keys (
    id              UUID            PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id       UUID            NOT NULL,
    name            VARCHAR(128)    NOT NULL,  -- user-friendly name, e.g. "Production Integration Key"
    key_hash        VARCHAR(256)    NOT NULL,  -- SHA-256 hash of the actual key
    key_prefix      VARCHAR(8)      NOT NULL,  -- first 8 chars of key, for identification in logs
    permissions     TEXT[]          NOT NULL DEFAULT '{}',  -- e.g. {'inventory:read', 'shipments:read', 'shipments:write'}
    rate_limit_rpm  INTEGER         NOT NULL DEFAULT 1000,  -- requests per minute
    allowed_ips     INET[],         -- IP allowlist; null means all IPs allowed
    expires_at      TIMESTAMPTZ,    -- null means no expiry (not recommended)
    last_used_at    TIMESTAMPTZ,
    usage_count     BIGINT          NOT NULL DEFAULT 0,
    created_by      UUID            NOT NULL REFERENCES users(id),
    is_active       BOOLEAN         NOT NULL DEFAULT TRUE,
    created_at      TIMESTAMPTZ     NOT NULL DEFAULT NOW(),
    updated_at      TIMESTAMPTZ     NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_apikeys_hash ON api_keys (key_hash) WHERE is_active = TRUE;
CREATE INDEX idx_apikeys_tenant ON api_keys (tenant_id, is_active);
CREATE INDEX idx_apikeys_expiry ON api_keys (expires_at) WHERE is_active = TRUE AND expires_at IS NOT NULL;
```

**Notes:**
- The actual API key is only shown once at creation time. We store only the
  SHA-256 hash. The `key_prefix` allows identifying which key was used in
  logs without exposing the full key.
- `allowed_ips` uses PostgreSQL's INET array type for IP allowlisting. The
  auth middleware checks `source_ip = ANY(allowed_ips)` if the array is
  non-null.
- **Tech debt:** The `usage_count` is updated on every API call via
  `UPDATE api_keys SET usage_count = usage_count + 1`. At high request rates
  this creates row-level contention. We should move usage tracking to Redis
  and batch-update PG periodically. This has caused ~50ms latency spikes
  under load.

### roles

Predefined roles for RBAC.

```sql
CREATE TABLE roles (
    id              UUID            PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id       UUID,           -- null for system-wide roles (e.g., super_admin)
    name            VARCHAR(32)     NOT NULL,  -- 'viewer', 'editor', 'admin', 'super_admin'
    description     TEXT,
    is_system       BOOLEAN         NOT NULL DEFAULT FALSE,  -- system roles cannot be modified
    created_at      TIMESTAMPTZ     NOT NULL DEFAULT NOW(),
    updated_at      TIMESTAMPTZ     NOT NULL DEFAULT NOW(),

    UNIQUE (tenant_id, name)
);
```

### permissions

Granular permissions assigned to roles. Uses a resource:action pattern.

```sql
CREATE TABLE permissions (
    id              UUID            PRIMARY KEY DEFAULT gen_random_uuid(),
    role_id         UUID            NOT NULL REFERENCES roles(id) ON DELETE CASCADE,
    resource        VARCHAR(64)     NOT NULL,  -- e.g. 'inventory', 'shipments', 'forecasts', 'screening', 'admin'
    action          VARCHAR(16)     NOT NULL CHECK (action IN ('read', 'write', 'delete', 'admin')),
    conditions      JSONB,          -- optional: e.g. {"location_type": "warehouse"} for scoped access
    created_at      TIMESTAMPTZ     NOT NULL DEFAULT NOW(),

    UNIQUE (role_id, resource, action)
);

CREATE INDEX idx_permissions_role ON permissions (role_id);
```

**Notes:**
- The `conditions` JSONB column was added in 2025 for fine-grained access
  control (e.g., a user who can only view inventory for specific locations).
  Only 2 customers use it so far, both Enterprise tier.
- The permission check hot path is cached in Redis (TTL 5 minutes). Database
  lookups happen only on cache miss or after role changes.

---

## relay-control-plane

Owner: Relay Team
Database: `relay_prod` (RDS, r6g.large, Multi-AZ)

### relay_agents

Registry of deployed Relay agents across all customer environments.

```sql
CREATE TABLE relay_agents (
    id              UUID            PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id       UUID            NOT NULL,
    hostname        VARCHAR(256)    NOT NULL,
    agent_version   VARCHAR(16)     NOT NULL,
    os              VARCHAR(32),     -- e.g. 'linux/amd64', 'windows/amd64'
    status          VARCHAR(16)     NOT NULL DEFAULT 'unknown' CHECK (status IN (
        'online', 'offline', 'degraded', 'unknown', 'decommissioned'
    )),
    last_heartbeat  TIMESTAMPTZ,
    last_sync_at    TIMESTAMPTZ,    -- last successful data sync
    ip_address      INET,
    cpu_cores       INTEGER,
    memory_mb       INTEGER,
    disk_total_gb   INTEGER,
    disk_used_gb    INTEGER,
    config_version  INTEGER         NOT NULL DEFAULT 0,  -- incremented on config push
    error_message   TEXT,           -- last error, if status is 'degraded' or 'offline'
    metadata        JSONB,          -- agent-reported custom fields
    registered_at   TIMESTAMPTZ     NOT NULL DEFAULT NOW(),
    updated_at      TIMESTAMPTZ     NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_agents_tenant ON relay_agents (tenant_id);
CREATE INDEX idx_agents_status ON relay_agents (status) WHERE status != 'decommissioned';
CREATE INDEX idx_agents_heartbeat ON relay_agents (last_heartbeat)
    WHERE status NOT IN ('offline', 'decommissioned');
```

**Notes:**
- Most customers have a single agent. Larger customers (GlobalMart, Pacific Rim)
  run multiple agents for redundancy or to connect to different ERP instances.
- The `last_heartbeat` column is updated every 30 seconds via the agent's
  heartbeat endpoint. If no heartbeat is received for 10 minutes, the
  monitoring pipeline sets status to 'offline' and triggers an alert.
- **Tech debt:** The `disk_total_gb` and `disk_used_gb` columns were added
  after the disk space incident mentioned in RB-002. They're only populated
  for agents running version 2.8+. Older agents report NULL.

### relay_configs

Configuration documents pushed to relay agents. Includes connector configs,
schema mappings, and sync schedules.

```sql
CREATE TABLE relay_configs (
    id              UUID            PRIMARY KEY DEFAULT gen_random_uuid(),
    agent_id        UUID            NOT NULL REFERENCES relay_agents(id),
    tenant_id       UUID            NOT NULL,
    config_type     VARCHAR(32)     NOT NULL CHECK (config_type IN (
        'connector', 'schema_mapping', 'sync_schedule', 'auth_credentials', 'agent_settings'
    )),
    name            VARCHAR(128)    NOT NULL,
    version         INTEGER         NOT NULL DEFAULT 1,
    config_data     JSONB           NOT NULL,    -- the actual configuration
    is_active       BOOLEAN         NOT NULL DEFAULT TRUE,
    deployed_at     TIMESTAMPTZ,    -- null if not yet deployed to agent
    deployed_version INTEGER,       -- last version successfully deployed
    created_by      UUID,
    created_at      TIMESTAMPTZ     NOT NULL DEFAULT NOW(),
    updated_at      TIMESTAMPTZ     NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_rconfigs_agent ON relay_configs (agent_id, config_type, is_active);
CREATE INDEX idx_rconfigs_tenant ON relay_configs (tenant_id);
CREATE INDEX idx_rconfigs_pending ON relay_configs (agent_id)
    WHERE is_active = TRUE AND (deployed_version IS NULL OR deployed_version < version);
```

**Notes:**
- `config_data` for `auth_credentials` type is encrypted at the application
  layer before storage. Contains customer ERP/WMS credentials for the
  connector to use.
- The `deployed_version` vs `version` comparison identifies configs that need
  to be pushed to the agent. The control plane polls this index every 60
  seconds.
- **Planned change:** Adding a `validation_status` column to track whether a
  config has been validated against the schema for its `config_type`. Currently
  validation is done only at the API layer and invalid configs have made it
  into the table via direct DB inserts during incident remediation.

### sync_logs

Log of data synchronization operations performed by relay agents.

```sql
CREATE TABLE sync_logs (
    id              BIGSERIAL       PRIMARY KEY,
    agent_id        UUID            NOT NULL,
    tenant_id       UUID            NOT NULL,
    connector_name  VARCHAR(64)     NOT NULL,  -- e.g. 'sap_s4hana_inventory', 'netsuite_orders'
    sync_type       VARCHAR(16)     NOT NULL CHECK (sync_type IN ('full', 'incremental', 'retry')),
    status          VARCHAR(16)     NOT NULL CHECK (status IN ('started', 'running', 'success', 'partial', 'failed')),
    records_read    INTEGER         DEFAULT 0,
    records_written INTEGER         DEFAULT 0,
    records_failed  INTEGER         DEFAULT 0,
    bytes_transferred BIGINT        DEFAULT 0,
    error_message   TEXT,
    error_details   JSONB,          -- structured error info, stack traces
    duration_ms     INTEGER,
    started_at      TIMESTAMPTZ     NOT NULL DEFAULT NOW(),
    completed_at    TIMESTAMPTZ
);

-- Partitioned by month
CREATE INDEX idx_synclogs_agent ON sync_logs (agent_id, started_at DESC);
CREATE INDEX idx_synclogs_tenant ON sync_logs (tenant_id, started_at DESC);
CREATE INDEX idx_synclogs_failed ON sync_logs (tenant_id, status)
    WHERE status IN ('failed', 'partial');
```

**Notes:**
- `status = 'partial'` means some records were synced but others failed. The
  `error_details` JSONB contains per-record error information for partial
  failures.
- This table is useful for debugging "my data is stale" customer complaints
  (see troubleshooting guide). CSMs can view sync history in Launchpad.
- Retention: 90 days. Older records are archived to S3 monthly.

---

## beacon-api

Owner: Beacon Team
Database: `beacon_prod` (RDS, r6g.large, Multi-AZ) for metadata.
Analytics data lives in ClickHouse (separate cluster, not documented here).

### reports

Metadata for generated reports (PDF/Excel). The actual report files are stored
in S3.

```sql
CREATE TABLE reports (
    id              UUID            PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id       UUID            NOT NULL,
    name            VARCHAR(256)    NOT NULL,
    report_type     VARCHAR(32)     NOT NULL CHECK (report_type IN (
        'inventory_summary', 'shipment_performance', 'carrier_scorecard',
        'demand_forecast', 'exception_analysis', 'custom'
    )),
    format          VARCHAR(8)      NOT NULL CHECK (format IN ('pdf', 'xlsx', 'csv')),
    status          VARCHAR(16)     NOT NULL DEFAULT 'pending' CHECK (status IN (
        'pending', 'generating', 'complete', 'failed', 'expired'
    )),
    parameters      JSONB           NOT NULL,   -- date range, filters, grouping, etc.
    s3_bucket       VARCHAR(64),
    s3_key          VARCHAR(256),   -- path to generated report file
    file_size_bytes BIGINT,
    page_count      INTEGER,        -- for PDF reports
    generated_by    UUID,           -- user who requested; null for scheduled
    schedule_id     UUID,           -- FK to scheduled_exports if auto-generated
    generation_time_ms INTEGER,
    error_message   TEXT,
    expires_at      TIMESTAMPTZ,    -- report files are cleaned up after 30 days
    created_at      TIMESTAMPTZ     NOT NULL DEFAULT NOW(),
    updated_at      TIMESTAMPTZ     NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_reports_tenant ON reports (tenant_id, created_at DESC);
CREATE INDEX idx_reports_status ON reports (status) WHERE status IN ('pending', 'generating');
CREATE INDEX idx_reports_expiry ON reports (expires_at) WHERE status = 'complete';
CREATE INDEX idx_reports_schedule ON reports (schedule_id) WHERE schedule_id IS NOT NULL;
```

**Notes:**
- Report files in S3 are stored at:
  `s3://acme-reports-{region}/{tenant_id}/{year}/{month}/{report_id}.{format}`
- EU tenant reports go to the `acme-reports-euw1` bucket (data residency).
- The `expires_at` column triggers a cleanup job that deletes S3 objects and
  marks the report as 'expired'. Default retention is 30 days; Enterprise
  customers can configure up to 365 days.
- **Tech debt:** The `parameters` JSONB has no schema validation. Two bugs in
  2025 were caused by report generation failing on malformed parameters that
  passed API validation but not the report-generator's internal parsing.

### dashboards

Customer-created dashboards in the Beacon dashboard builder.

```sql
CREATE TABLE dashboards (
    id              UUID            PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id       UUID            NOT NULL,
    name            VARCHAR(256)    NOT NULL,
    description     TEXT,
    layout          JSONB           NOT NULL,   -- grid positions, widget sizes
    widgets         JSONB           NOT NULL,   -- array of widget definitions (type, data source, filters)
    is_default      BOOLEAN         NOT NULL DEFAULT FALSE,  -- shows on Beacon home
    is_shared       BOOLEAN         NOT NULL DEFAULT FALSE,  -- visible to all tenant users
    created_by      UUID            NOT NULL,
    last_viewed_at  TIMESTAMPTZ,
    view_count      INTEGER         NOT NULL DEFAULT 0,
    thumbnail_s3_key VARCHAR(256),  -- screenshot thumbnail for dashboard listing
    created_at      TIMESTAMPTZ     NOT NULL DEFAULT NOW(),
    updated_at      TIMESTAMPTZ     NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_dashboards_tenant ON dashboards (tenant_id, is_shared, updated_at DESC);
CREATE INDEX idx_dashboards_default ON dashboards (tenant_id) WHERE is_default = TRUE;
```

**Notes:**
- The `widgets` JSONB column contains the full widget configuration including
  ClickHouse query templates, visualization type, color scheme, and refresh
  interval. Widget definitions are versioned -- the Beacon team increments
  a `schema_version` field inside the JSONB when widget config format changes.
- Embedded Metabase dashboards are NOT stored here. Metabase has its own
  database (separate PostgreSQL instance managed by Beacon team). This table
  is for the custom dashboard builder only.
- **Planned change:** Adding `folder_id` column for dashboard organization.
  Customers with 50+ dashboards have requested folders/categories. Targeted
  for Q2 2026.

### scheduled_exports

Configuration for recurring report generation and data exports.

```sql
CREATE TABLE scheduled_exports (
    id              UUID            PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id       UUID            NOT NULL,
    name            VARCHAR(256)    NOT NULL,
    export_type     VARCHAR(16)     NOT NULL CHECK (export_type IN ('report', 'data_export')),
    schedule_cron   VARCHAR(64)     NOT NULL,  -- cron expression, e.g. '0 8 * * 1' (Mon 8am UTC)
    timezone        VARCHAR(48)     NOT NULL DEFAULT 'UTC',
    report_type     VARCHAR(32),    -- for export_type='report'; references reports.report_type
    parameters      JSONB           NOT NULL,
    destination     JSONB           NOT NULL,  -- {"type": "email", "recipients": [...]} or {"type": "s3", "bucket": "..."}
    format          VARCHAR(8)      NOT NULL DEFAULT 'pdf',
    is_active       BOOLEAN         NOT NULL DEFAULT TRUE,
    last_run_at     TIMESTAMPTZ,
    last_run_status VARCHAR(16),
    next_run_at     TIMESTAMPTZ,
    failure_count   INTEGER         NOT NULL DEFAULT 0,  -- consecutive failures
    max_failures    INTEGER         NOT NULL DEFAULT 3,   -- disable after N consecutive failures
    created_by      UUID            NOT NULL,
    created_at      TIMESTAMPTZ     NOT NULL DEFAULT NOW(),
    updated_at      TIMESTAMPTZ     NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_schedexports_tenant ON scheduled_exports (tenant_id, is_active);
CREATE INDEX idx_schedexports_next ON scheduled_exports (next_run_at)
    WHERE is_active = TRUE;
```

**Notes:**
- The scheduler runs every minute, queries for exports where
  `next_run_at <= NOW()`, and enqueues them for generation. After each run,
  `next_run_at` is computed from the cron expression.
- If `failure_count >= max_failures`, the export is automatically disabled and
  an alert is sent to the customer and their CSM. This prevents silent
  failures where a broken export runs (and fails) indefinitely.
- `destination` supports email (via SES), S3, Snowflake, and BigQuery. The
  Snowflake and BigQuery destinations are Enterprise-only.
- **Tech debt:** The cron parsing is done in Go using a third-party library
  (robfig/cron). Edge cases around DST transitions have caused exports to
  run at unexpected times for customers in timezones with DST. A fix is in
  progress using a timezone-aware cron library.
