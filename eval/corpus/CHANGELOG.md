# Changelog

All notable changes to the Acme Widget Service are documented in this file.

## [2.5.0] - 2025-09-15

### Added
- Batch widget creation endpoint: `POST /api/v1/widgets/batch` accepts up to 50
  widgets in a single request.
- Audit log for all write operations, stored in the `audit_events` table.

### Changed
- **Breaking:** The `WIDGET_DB_URL` environment variable has been removed. Use the
  individual `WIDGET_DB_HOST`, `WIDGET_DB_PORT`, `WIDGET_DB_USER`, `WIDGET_DB_PASSWORD`,
  `WIDGET_DB_NAME`, and `WIDGET_DB_SSLMODE` variables instead (see config.go).
- Minimum Go version bumped from 1.21 to 1.22.

### Deprecated
- The `/api/v1/widgets/stream` WebSocket endpoint is deprecated in favor of the
  new Server-Sent Events endpoint at `/api/v1/widgets/events`. The WebSocket
  endpoint will be removed in v3.0.

### Fixed
- Fixed a race condition in the cache invalidation path that could cause stale
  reads after rapid update-then-read sequences.

## [2.4.0] - 2025-07-01

### Added
- ETag support for conditional GET requests on individual widgets.
- `X-Request-ID` header propagation for distributed tracing.

### Changed
- **Breaking:** Widget metadata values are now limited to 1024 characters each.
  Previously there was no enforced limit.
- Connection pool default increased from 25 to 50 connections. The environment
  variable `WIDGET_DB_MAX_CONNS` now defaults to 50.

### Fixed
- Fixed incorrect pagination when filtering by status with large result sets.

## [2.3.0] - 2025-04-10

### Changed
- Rate limit increased from 100 to 200 requests per minute per API key.
- Upgraded from `lib/pq` to `pgx` for PostgreSQL driver.

### Fixed
- Fixed memory leak in WebSocket connection handler when clients disconnect
  without sending a close frame.

## [2.2.0] - 2025-02-20

### Added
- Organization-level widget count quota (default 10,000 widgets per org).
- `/api/v1/widgets/export` endpoint for CSV export of widget data.

### Changed
- Cache TTL increased from 5 minutes to 10 minutes for all GET endpoints.

## [2.1.0] - 2025-01-05

### Changed
- **Breaking:** Rate limiting algorithm changed from token bucket to sliding
  window. Clients relying on burst behavior of token bucket will see different
  throttling patterns.
- Soft-deleted widgets are now retained for 90 days (previously 30 days) before
  permanent deletion by the cleanup job.

### Fixed
- Fixed bcrypt cost factor being set too low (was 10, now 12).

## [2.0.0] - 2024-11-01

### Changed
- **Breaking:** API version prefix changed from `/api/v0` to `/api/v1`.
- **Breaking:** Widget `type` field renamed to `status` across all endpoints.
- Migrated from `gorilla/mux` to standard library `net/http` router (Go 1.22+).

### Removed
- Removed legacy XML response format. All responses are JSON only.
- Removed deprecated `/widgets` endpoints (use `/api/v1/widgets`).
