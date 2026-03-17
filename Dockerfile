# ---- Build stage ----
FROM golang:1.24-bookworm AS builder

WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .

ARG VERSION=dev
RUN CGO_ENABLED=1 CGO_CFLAGS="-Wno-deprecated-declarations" \
    go build -tags sqlite_fts5 \
    -ldflags "-s -w -X main.version=${VERSION}" \
    -o /kb ./cmd/kb

# ---- Runtime stage ----
FROM debian:bookworm-slim

RUN apt-get update && \
    apt-get install -y --no-install-recommends ca-certificates curl && \
    rm -rf /var/lib/apt/lists/*

# Install Ollama
RUN curl -fsSL https://ollama.com/install.sh | sh

COPY --from=builder /kb /usr/local/bin/kb

# Entrypoint script to start Ollama and then run kb
COPY docker-entrypoint.sh /usr/local/bin/docker-entrypoint.sh
RUN chmod +x /usr/local/bin/docker-entrypoint.sh

# Data volume for the KB database
VOLUME /data
ENV KB_DB_PATH=/data/kb.db

EXPOSE 8080

ENTRYPOINT ["docker-entrypoint.sh"]
CMD ["serve"]
