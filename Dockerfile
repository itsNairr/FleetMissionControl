# ==============================================================================
# Stage 1: Build static Go binary
# ==============================================================================
FROM golang:1.24-alpine AS builder

WORKDIR /src

# Install CA certificates for secure outgoing requests
RUN apk add --no-cache ca-certificates tzdata

# Cache Go dependency layers
COPY go.mod go.sum ./
RUN go mod download

# Copy source tree and compile minimal static binary
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build \
    -trimpath \
    -ldflags="-s -w" \
    -o /app/gateway \
    ./cmd/gateway

# ==============================================================================
# Stage 2: Minimal runtime image
# ==============================================================================
FROM alpine:3.21

# Install CA certs and tzdata for TLS/time support
RUN apk add --no-cache ca-certificates tzdata

WORKDIR /app

# Copy binary from builder
COPY --from=builder /app/gateway /app/gateway

# Default environment configuration
# 0.0.0.0:8081 is required so the container accepts traffic from outside
ENV FLEET_API_ADDR="0.0.0.0:8081"
ENV FLEET_ALLOWED_ORIGINS="*"

# Port 8080: TCP vehicle telemetry ingestion & command framing
# Port 8081: HTTP REST API & WebSocket live streaming
EXPOSE 8080 8081

# Volume for persistent ECDSA cryptographic root-of-trust keypair
VOLUME ["/app/certs"]

ENTRYPOINT ["/app/gateway"]
