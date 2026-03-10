# Acme Widget Service

The Acme Widget Service (AWS) is a high-performance widget management platform
built by Acme Corp. It provides a RESTful API for creating, reading, updating,
and deleting widgets, along with real-time event streaming.

## Features

- **Widget CRUD**: Full lifecycle management for widgets
- **Event Streaming**: Real-time widget change notifications via WebSocket
- **Multi-tenant**: Isolated widget namespaces per organization
- **Rate Limiting**: Token bucket algorithm, 100 requests per minute per API key
- **Caching**: Redis-backed response cache with 5-minute TTL

## Quick Start

```bash
# Clone the repository
git clone https://github.com/acme/widget-service.git
cd widget-service

# Configure environment
cp .env.example .env
# Edit .env with your database credentials

# Start the service
go run ./cmd/server --port 8080
```

## Configuration

The service reads configuration from environment variables:

| Variable           | Default           | Description                    |
|--------------------|-------------------|--------------------------------|
| `WIDGET_DB_URL`    | `localhost:5432`  | PostgreSQL connection string   |
| `WIDGET_REDIS_URL` | `localhost:6379`  | Redis connection string        |
| `WIDGET_PORT`      | `8080`            | HTTP listen port               |
| `WIDGET_LOG_LEVEL` | `info`            | Log level (debug/info/warn)    |
| `WIDGET_API_KEY`   | (required)        | API key for authentication     |

## API Overview

All endpoints require an `X-API-Key` header. Responses use JSON.

```
GET    /api/v1/widgets          List widgets (paginated)
POST   /api/v1/widgets          Create a widget
GET    /api/v1/widgets/:id      Get a single widget
PUT    /api/v1/widgets/:id      Update a widget
DELETE /api/v1/widgets/:id      Delete a widget
GET    /api/v1/widgets/stream   WebSocket event stream
```

## Architecture

The service follows a clean architecture pattern with three layers:

1. **Handler Layer** — HTTP routing and request validation
2. **Service Layer** — Business logic and orchestration
3. **Repository Layer** — Database access and caching

All inter-layer communication uses typed Go interfaces.

## Database Schema

Widgets are stored in PostgreSQL with the following schema:

```sql
CREATE TABLE widgets (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    org_id      UUID NOT NULL REFERENCES organizations(id),
    name        VARCHAR(255) NOT NULL,
    description TEXT,
    status      VARCHAR(50) DEFAULT 'draft',
    metadata    JSONB DEFAULT '{}',
    created_at  TIMESTAMPTZ DEFAULT NOW(),
    updated_at  TIMESTAMPTZ DEFAULT NOW()
);

CREATE INDEX idx_widgets_org ON widgets(org_id);
CREATE INDEX idx_widgets_status ON widgets(org_id, status);
```

## Testing

```bash
# Run unit tests
go test ./...

# Run integration tests (requires running database)
go test ./... -tags=integration

# Run with coverage
go test ./... -coverprofile=coverage.out
```

## License

Copyright 2025 Acme Corp. All rights reserved. Proprietary software.
