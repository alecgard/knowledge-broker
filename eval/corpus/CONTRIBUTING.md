# Contributing to Acme Widget Service

Thank you for your interest in contributing to the Acme Widget Service. This
document outlines the development workflow, coding standards, and review process.

## Prerequisites

- **Go 1.21 or later** (we test against 1.21 and 1.22)
- **PostgreSQL 15+** running locally or via Docker
- **Redis 7+** for caching and rate limiting
- **golang-migrate** CLI for database migrations
- **golangci-lint** for static analysis

## Setting Up the Development Environment

1. Fork and clone the repository:

   ```bash
   git clone https://github.com/YOUR_USERNAME/widget-service.git
   cd widget-service
   ```

2. Install dependencies:

   ```bash
   go mod download
   ```

3. Start the required services using Docker Compose:

   ```bash
   docker compose up -d postgres redis
   ```

4. Run database migrations:

   ```bash
   make migrate-up
   ```

5. Copy the example environment file and fill in your local values:

   ```bash
   cp .env.example .env
   ```

6. Run the test suite to verify your setup:

   ```bash
   make test
   ```

## Running Tests

We use the standard Go test runner. Tests are organized as follows:

- **Unit tests** (`go test ./...`): No external dependencies, use mocks.
- **Integration tests** (`go test -tags=integration ./...`): Require a running
  PostgreSQL and Redis instance. These test real database queries and cache
  behavior.
- **End-to-end tests** (`make test-e2e`): Spin up the full service and exercise
  the HTTP API. Requires Docker.

Run the full suite before submitting a pull request:

```bash
# Unit tests with race detection
go test -race ./...

# Integration tests
go test -tags=integration -race ./...

# Linting
golangci-lint run ./...
```

All tests must pass and the linter must report zero issues before a PR will be
reviewed.

## Branch Naming Conventions

Use the following prefixes for branch names:

- `feat/` — new features (e.g., `feat/batch-create`)
- `fix/` — bug fixes (e.g., `fix/cache-invalidation-race`)
- `refactor/` — code restructuring without behavior changes
- `docs/` — documentation-only changes
- `test/` — adding or updating tests
- `chore/` — dependency updates, CI changes, etc.

## Code Review Process

1. Open a pull request against the `main` branch.
2. At least two approvals are required from maintainers.
3. All CI checks must pass (unit tests, integration tests, lint).
4. The PR description must include:
   - A summary of what changed and why
   - Steps to test the change manually (if applicable)
   - Any migration or deployment notes
5. Squash-merge is the default merge strategy.

## Coding Standards

- Follow the [Effective Go](https://go.dev/doc/effective_go) guidelines.
- Use `gofmt` for formatting (enforced by CI).
- Export only what is necessary; keep the public API surface small.
- Add doc comments to all exported types, functions, and methods.
- Error messages should start with a lowercase letter and not end with
  punctuation, following Go conventions.
- Use table-driven tests where possible.

## Database Migrations

- Migrations live in the `migrations/` directory.
- Each migration has an `up.sql` and `down.sql` file.
- Never modify an existing migration that has been merged to `main`. Always
  create a new migration.
- Test rollback before submitting: run `migrate down 1` then `migrate up`.

## Reporting Issues

Use GitHub Issues for bug reports and feature requests. Include:

- Steps to reproduce (for bugs)
- Expected vs. actual behavior
- Service version and environment details
