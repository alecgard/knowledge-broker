# --- Builder stage ---
FROM golang:1.24 AS builder

RUN apt-get update && apt-get install -y --no-install-recommends \
    gcc \
    libc6-dev \
    libsqlite3-dev \
    && rm -rf /var/lib/apt/lists/*

WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=1 CGO_CFLAGS="-Wno-deprecated-declarations" \
    go build -ldflags "-s -w -X main.version=0.1.0" -o /kb ./cmd/kb

# --- Runtime stage ---
FROM debian:bookworm-slim

RUN apt-get update && apt-get install -y --no-install-recommends \
    ca-certificates \
    && rm -rf /var/lib/apt/lists/*

COPY --from=builder /kb /usr/local/bin/kb

RUN mkdir -p /data
VOLUME /data

EXPOSE 8080

ENTRYPOINT ["kb"]
CMD ["serve", "--db", "/data/kb.db", "--addr", ":8080"]
