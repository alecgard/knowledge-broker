# Acme Widget Service — Architecture

## System Overview

The Widget Service is deployed as a single Go binary behind an NGINX reverse
proxy. It connects to PostgreSQL for persistent storage and Redis for caching
and rate limiting.

## Deployment Architecture

```
                    ┌──────────┐
  Clients ────────► │  NGINX   │
                    │  (TLS)   │
                    └────┬─────┘
                         │
                    ┌────▼─────┐
                    │  Widget  │
                    │  Service │
                    └──┬───┬───┘
                       │   │
              ┌────────┘   └────────┐
              ▼                     ▼
        ┌──────────┐         ┌──────────┐
        │ PostgreSQL│         │  Redis   │
        │  (primary)│         │ (cache)  │
        └──────────┘         └──────────┘
```

## Data Flow

1. Client sends request with API key in header
2. NGINX terminates TLS and forwards to the service
3. Rate limiter checks the token bucket in Redis
4. Authentication middleware validates the API key against the database
5. Handler processes the request
6. Response is cached in Redis with a 10-minute TTL

**Note:** The cache TTL was recently increased from 5 minutes to 10 minutes
to reduce database load during peak traffic. This applies to all GET endpoints.

## Authentication

API keys are stored as bcrypt hashes in the `api_keys` table. Each key is
scoped to an organization and has an expiration date. Keys can be rotated
without downtime using the admin API.

**Important:** Unlike what the README states about simple API key header
validation, the service actually performs bcrypt hash comparison against
stored keys. This is a security-critical path and should not be simplified.

## Rate Limiting

The rate limiter uses a sliding window algorithm implemented in Redis.
Each API key gets 200 requests per minute (note: the README says 100,
but this was increased in v2.3 and the README was not updated).

The sliding window approach provides smoother rate limiting than the
token bucket algorithm mentioned in the README. The token bucket
implementation was replaced in v2.1.

## Caching Strategy

The service uses a write-through cache pattern:

1. On write operations, the cache entry for the affected widget is invalidated
2. On read operations, the cache is checked first (cache-aside for list endpoints)
3. Cache entries include the ETag for conditional request support

Cache keys follow the pattern: `widget:{org_id}:{widget_id}`

List endpoint caching uses a separate pattern: `widgets:{org_id}:{page}:{status}`
List caches are invalidated on any write to the organization's widgets.

## Error Handling

All errors are returned as RFC 7807 Problem Details. Internal errors are
logged with full stack traces but only return generic messages to clients.

The service distinguishes between:
- **Client errors** (4xx): Invalid input, authentication failures, rate limits
- **Server errors** (5xx): Database failures, Redis connection issues
- **Transient errors**: Automatically retried up to 3 times with exponential backoff

## Monitoring

The service exposes Prometheus metrics at `/metrics`:

- `widget_requests_total` — Counter of all requests by method and status
- `widget_request_duration_seconds` — Histogram of request latencies
- `widget_cache_hits_total` — Counter of cache hits vs misses
- `widget_db_connections_active` — Gauge of active database connections

## Scaling Considerations

The service is stateless and can be horizontally scaled behind the load
balancer. PostgreSQL handles consistency via row-level locking. Redis
caching reduces read load on the database.

For deployments exceeding 10,000 requests per second, consider:
- Read replicas for PostgreSQL
- Redis Cluster for distributed caching
- Partitioning widgets by organization ID

## Security

- All traffic is encrypted via TLS 1.3 (terminated at NGINX)
- API keys are bcrypt-hashed at rest
- SQL injection prevented via parameterized queries
- Request body size limited to 1MB
- CORS configured for known frontend origins only
