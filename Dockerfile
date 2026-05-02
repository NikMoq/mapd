# Multi-stage Dockerfile for mapd-russia
# Builds ARM64 binary for sunnypilot / embedded Linux

# ============================================
# Stage 1: Builder
# ============================================
FROM golang:1.21-alpine AS builder

RUN apk add --no-cache \
    capnproto \
    capnproto-dev \
    wget \
    git \
    build-base

WORKDIR /app

# Copy module files first for better caching
COPY go.mod go.sum ./
RUN go mod download

# Copy source
COPY . .

# Generate capnp Go code
RUN cd cereal/offline && \
    capnp compile -I /usr/include -ogo offline.capnp
RUN cd cereal/custom && \
    capnp compile -I /usr/include -ogo custom.capnp

# Build static binary for ARM64 (sunpilot target)
RUN GOOS=linux GOARCH=arm64 CGO_ENABLED=0 go build \
    -o /app/mapd \
    -ldflags "-s -w -extldflags '-static'" \
    ./...

# Verify
RUN file /app/mapd && ls -lh /app/mapd

# ============================================
# Stage 2: Runtime (minimal)
# ============================================
FROM alpine:latest

RUN apk add --no-cache ca-certificates

# Copy binary
COPY --from=builder /app/mapd /usr/local/bin/mapd

# Create data directory
RUN mkdir -p /data/mapd/offline

ENTRYPOINT ["/usr/local/bin/mapd"]
CMD ["version"]
