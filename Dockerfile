# syntax=docker/dockerfile:1.7

FROM golang:1.23-bookworm AS builder

RUN apt-get update \
    && apt-get install -y --no-install-recommends \
        ca-certificates \
        gcc \
        libc6-dev \
        libsqlite3-dev \
    && rm -rf /var/lib/apt/lists/*

WORKDIR /src

COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=1 GOOS=linux go build \
    -trimpath \
    -ldflags='-w -s' \
    -o /out/server \
    cmd/server/main.go

FROM debian:bookworm-slim

RUN apt-get update \
    && apt-get install -y --no-install-recommends \
        ca-certificates \
        sqlite3 \
        tzdata \
        wget \
    && rm -rf /var/lib/apt/lists/* \
    && update-ca-certificates

WORKDIR /root

COPY --from=builder /out/server /root/server

ENV MCP_LOGGING_CONFIG=/root/config.yaml

EXPOSE 8080 8081

HEALTHCHECK --interval=30s --timeout=10s --start-period=10s --retries=3 \
    CMD wget -qO- http://localhost:8080/health >/dev/null || exit 1

CMD ["./server", "--config", "/root/config.yaml"]
