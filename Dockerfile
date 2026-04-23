# syntax=docker/dockerfile:1
# ─────────────────────────────────────────────────────────────────────────────
# Build stage
# ─────────────────────────────────────────────────────────────────────────────
FROM golang:1.23-alpine AS builder

WORKDIR /workspace

COPY go.mod go.sum ./
RUN go mod download

COPY . .

# Build a static binary.
RUN CGO_ENABLED=0 GOOS=linux go build \
    -ldflags="-s -w" \
    -o /argus \
    ./cmd/argus

# ─────────────────────────────────────────────────────────────────────────────
# Runtime stage
# ─────────────────────────────────────────────────────────────────────────────
FROM scratch

# CA certs for HTTPS calls to Slack / PagerDuty / Prometheus.
COPY --from=builder /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/

# Binary.
COPY --from=builder /argus /argus

# Default config (can be overridden via volume or env vars).
COPY configs/config.yaml /configs/config.yaml

# Runbooks shipped with the image.
COPY runbooks/ /runbooks/

EXPOSE 8080

ENV ARGUS_CONFIG=/configs/config.yaml

ENTRYPOINT ["/argus"]
