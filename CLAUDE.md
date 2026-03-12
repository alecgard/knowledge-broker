# Knowledge Broker

## Build & Test

```bash
make build    # Build the kb binary
make test     # Run all tests
make test-v   # Run all tests (verbose)
make lint     # go vet
```

CGO flag required on macOS: `CGO_CFLAGS="-Wno-deprecated-declarations"`

## Rules

- **Always run `make test` before pushing.** A pre-push git hook enforces this automatically.
- Keep tests passing at all times. Do not push with failing tests.
- When modifying MCP, HTTP, or CLI code, run the relevant server tests:
  - `go test ./internal/server/ -count=1 -v` for HTTP and MCP
  - `go test ./cmd/kb/ -count=1 -v` for CLI
