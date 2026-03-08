BINARY := kb
VERSION := 0.1.0
LDFLAGS := -ldflags "-s -w -X main.version=$(VERSION)"

.PHONY: build install clean test lint run-ingest run-serve run-mcp

## Build

build:
	CGO_CFLAGS="-Wno-deprecated-declarations" go build $(LDFLAGS) -o $(BINARY) ./cmd/kb

install:
	go install $(LDFLAGS) ./cmd/kb

clean:
	rm -f $(BINARY)
	rm -f *.db

## Test

test:
	CGO_CFLAGS="-Wno-deprecated-declarations" go test ./... -count=1

test-v:
	CGO_CFLAGS="-Wno-deprecated-declarations" go test ./... -count=1 -v

## Lint

lint:
	go vet ./...

## Run

run-ingest: build
	./$(BINARY) ingest --source . --db kb.db

run-serve: build
	./$(BINARY) serve --db kb.db --addr :8080

run-mcp: build
	./$(BINARY) mcp --db kb.db

## Dependencies

deps:
	go mod tidy
	go mod verify

## Help

help:
	@echo "Usage:"
	@echo "  make build        Build the kb binary"
	@echo "  make install      Install kb to GOPATH/bin"
	@echo "  make test         Run all tests"
	@echo "  make test-v       Run all tests (verbose)"
	@echo "  make lint         Run go vet"
	@echo "  make clean        Remove binary and database files"
	@echo "  make deps         Tidy and verify Go modules"
	@echo "  make run-ingest   Build and ingest current directory"
	@echo "  make run-serve    Build and start HTTP server"
	@echo "  make run-mcp      Build and start MCP server"
