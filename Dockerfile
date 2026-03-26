# ---- Build stage ----
FROM golang:1.24-bookworm AS builder

RUN apt-get update && \
    apt-get install -y --no-install-recommends libsqlite3-dev && \
    rm -rf /var/lib/apt/lists/*

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
    apt-get install -y --no-install-recommends ca-certificates git && \
    rm -rf /var/lib/apt/lists/*

COPY --from=builder /kb /usr/local/bin/kb

VOLUME /data
ENV KB_DB=/data/kb.db \
    KB_OLLAMA_URL=http://ollama:11434 \
    KB_SKIP_SETUP=true

EXPOSE 8080

ENTRYPOINT ["kb"]
CMD ["serve"]
