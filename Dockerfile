# ==============================================================================
# AegisPulse Gateway - Multi-Stage Zero-CGO Minimal Container
# ==============================================================================
FROM golang:1.22-alpine AS builder

WORKDIR /src
COPY go.mod ./
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-w -s" -o /bin/gateway ./cmd/gateway

FROM alpine:3.20
RUN apk add --no-cache ca-certificates tzdata curl && \
    addgroup -g 10001 -S aegis && \
    adduser -u 10001 -S aegis -G aegis

WORKDIR /app
COPY --from=builder /bin/gateway /app/gateway
COPY config/config.yaml /app/config/config.yaml

USER aegis:aegis
EXPOSE 8080

HEALTHCHECK --interval=10s --timeout=3s --start-period=5s --retries=3 \
  CMD curl -f http://localhost:8080/healthz || exit 1

ENTRYPOINT ["/app/gateway", "-config", "config/config.yaml", "-headless"]
