# Acme Widget Service — Operational Runbook

## Service Health Check

The service exposes a health endpoint at `GET /healthz` that returns:
- `200 OK` when the service is healthy
- `503 Service Unavailable` when PostgreSQL or Redis is unreachable

```bash
curl -s http://localhost:8080/healthz | jq .
```

## Common Operational Procedures

### Restarting the Service

```bash
# Graceful restart (allows in-flight requests to complete)
systemctl restart widget-service

# The service has a 30-second graceful shutdown timeout.
# After 30 seconds, remaining connections are forcibly closed.
```

### Database Migrations

Migrations are managed with `golang-migrate`. Always take a backup first.

```bash
# Run pending migrations
migrate -path ./migrations -database "$WIDGET_DB_URL" up

# Rollback last migration
migrate -path ./migrations -database "$WIDGET_DB_URL" down 1
```

### Rotating API Keys

```bash
# Generate a new API key for an organization
curl -X POST http://localhost:8080/admin/api-keys \
  -H "X-Admin-Token: $ADMIN_TOKEN" \
  -d '{"org_id": "uuid-here", "expires_in": "90d"}'

# Revoke an existing key (takes effect immediately)
curl -X DELETE http://localhost:8080/admin/api-keys/KEY_ID \
  -H "X-Admin-Token: $ADMIN_TOKEN"
```

## Troubleshooting

### High Latency

1. Check Prometheus dashboard for `widget_request_duration_seconds` p99
2. Check Redis connectivity: `redis-cli ping`
3. Check PostgreSQL active connections: `SELECT count(*) FROM pg_stat_activity`
4. If cache miss rate is high, verify Redis memory usage with `redis-cli info memory`

### Database Connection Exhaustion

The service uses a connection pool with max 25 connections. If connections
are exhausted:

1. Check for long-running queries: `SELECT * FROM pg_stat_activity WHERE state = 'active'`
2. Kill long-running queries if necessary: `SELECT pg_terminate_backend(pid)`
3. Consider increasing `WIDGET_DB_MAX_CONNS` (default: 25)

### Cache Inconsistency

If widgets show stale data after updates:

1. Flush the specific cache key: `redis-cli DEL widget:{org_id}:{widget_id}`
2. Flush all widget caches: `redis-cli KEYS "widget:*" | xargs redis-cli DEL`
3. Check that write-through invalidation is working in the application logs

### Service Won't Start

Common causes:
- PostgreSQL is unreachable (check `WIDGET_DB_URL`)
- Port 8080 is already in use (check with `lsof -i :8080`)
- Missing required env var `WIDGET_DB_PASSWORD`
- Migration version mismatch (run migrations first)

## Alerting Thresholds

| Metric                              | Warning    | Critical   |
|--------------------------------------|------------|------------|
| Request latency p99                  | > 500ms    | > 2s       |
| Error rate (5xx)                     | > 1%       | > 5%       |
| Database connections                 | > 20       | > 24       |
| Cache hit rate                       | < 80%      | < 50%      |
| Disk usage                          | > 80%      | > 95%      |

## Backup and Recovery

Daily automated backups via `pg_dump` are stored in S3 with 30-day retention.

```bash
# Manual backup
pg_dump -Fc $WIDGET_DB_URL > backup_$(date +%Y%m%d).dump

# Restore from backup
pg_restore -d $WIDGET_DB_URL backup_20250101.dump
```
