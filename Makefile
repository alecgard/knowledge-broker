BINARY := kb
VERSION := 0.1.0
LDFLAGS := -ldflags "-s -w -X main.version=$(VERSION)"

.PHONY: build install clean test lint run-ingest run-serve run-mcp eval

## Build

build:
	CGO_CFLAGS="-Wno-deprecated-declarations" go build $(LDFLAGS) -o $(BINARY) ./cmd/kb

install:
	CGO_CFLAGS="-Wno-deprecated-declarations" go install $(LDFLAGS) ./cmd/kb
	@if ! command -v kb >/dev/null 2>&1; then \
		GOBIN=$$(go env GOPATH)/bin; \
		LINE="export PATH=\"$$GOBIN:\$$PATH\""; \
		case "$$SHELL" in \
			*/zsh)  RC=~/.zshrc ;; \
			*/bash) RC=~/.bashrc ;; \
			*/fish) RC=~/.config/fish/config.fish; LINE="fish_add_path $$GOBIN" ;; \
			*)      RC="" ;; \
		esac; \
		if [ -n "$$RC" ] && ! grep -qF "$$GOBIN" "$$RC" 2>/dev/null; then \
			printf "kb is not on your PATH. Add to $$RC? [Y/n] "; \
			read ans; \
			case "$$ans" in \
				[nN]*) echo "Skipped. Add manually: $$LINE" ;; \
				*)     echo "" >> "$$RC"; echo "$$LINE" >> "$$RC"; \
				       echo "Added to $$RC — restart your shell or run: source $$RC" ;; \
			esac; \
		elif [ -z "$$RC" ]; then \
			echo "kb installed to $$GOBIN/kb but is not on your PATH."; \
			echo "Add to your shell profile: $$LINE"; \
		fi; \
	fi

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

## Eval

eval:
	@echo "Ingesting eval corpus..."
	CGO_CFLAGS="-Wno-deprecated-declarations" go run ./cmd/kb ingest --source eval/corpus --db eval.db
	@echo "Running evaluation..."
	CGO_CFLAGS="-Wno-deprecated-declarations" go run ./cmd/kb eval --db eval.db --testset eval/testset.json
	@rm -f eval.db eval.db-shm eval.db-wal

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
	@echo "  make eval         Ingest eval corpus and run evaluation"
