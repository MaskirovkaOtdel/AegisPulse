# 🛡️ AegisPulse: Ultra-Low-Latency Resilient API Gateway & Security Middleware

[![Go Version](https://img.shields.io/badge/Go-1.22+-00ADD8?style=flat&logo=go)](https://golang.org)
[![Redis](https://img.shields.io/badge/Redis-v9-DC382D?style=flat&logo=redis)](https://redis.io)
[![PostgreSQL](https://img.shields.io/badge/PostgreSQL-16-4169E1?style=flat&logo=postgresql)](https://www.postgresql.org)
[![Prometheus](https://img.shields.io/badge/Prometheus-Client-E6522C?style=flat&logo=prometheus)](https://prometheus.io)
[![License](https://img.shields.io/badge/License-Apache%202.0-blue.svg)](LICENSE)

**AegisPulse** is an enterprise-grade, ultra-low-latency, resilient API Gateway and Security Middleware designed in Go 1.22+ to shield upstream microservices against distributed denial-of-service, traffic spikes, credential replay attacks, and upstream cascading failures.

---

## 🏛️ System Architecture

```
                                      +---------------------------------------------+
                                      |             Interactive TUI Cockpit         |
                                      |   (Bubbletea, Dracula/Mocha/Matrix Themes,  |
                                      |     ASCII Sparklines, Panic Safety Modals)  |
                                      +----------------------+----------------------+
                                                             | (Hotkeys & Panic Telemetry)
                                                             v
Incoming Requests                     +---------------------------------------------+
================> [ X-API-Key ] ----> |              AegisPulse Gateway             |
                  [ Signature ]       |                                             |
                                      |  * AES-256-GCM Envelope Encryption (Secrets)|
                                      |  * Anti-Replay Defense Pipeline (Constant-T)|
                                      |  * Upstream Latency Jitter Simulation       |
                                      +------+-------------------------------+------+
                                             |                               |
                                             v (Sliding Window Log Lua)      v (Failure / High Latency)
                                      +--------------+             +-------------------------+
                                      | Redis v9 /   |             |  L1 Smart Fallback Cache|
                                      | Billing Hash |             |  (sync.RWMutex, TTL,    |
                                      +-------+------+             |   Circuit Breaker)      |
                                              |                    +------------+------------+
                                              v (Events Stream)                 |
                                      +------------------------+                v (Stale Response)
                                      | Universal Webhook Eng. |      Client receives:
                                      | (Generic, Discord,     |      X-Cache-Status: STALE-FALLBACK
                                      |  Slack, DLQ, Re-Sign)  |
                                      +------------------------+
```

---

## 🚀 Key Architectural Highlights

### 📌 Milestone 1: High-Throughput Core, Dual-Threshold Sliding Window & L1 Smart Cache
1. **Reverse Proxy & Header Validation**:
   - Intercepts requests, validates `X-API-Key`, proxies to upstream using Go `httputil.ReverseProxy`.
   - **Simulated Jitter**: Configurable upstream latency jitter (200ms - 500ms) on a percentage of requests to test system resilience.
2. **Hot-Reloadable Static Tiers (`config.yaml`)**:
   - Monitored by `fsnotify` for zero-downtime hot-reloading.
   - **Free Tier**: Soft Limit = 20 req/min, Hard Limit = 25 req/min, Replay Window = 60s.
   - **Pro Tier**: Soft Limit = 100 req/min, Hard Limit = 140 req/min, Replay Window = 300s.
   - **Enterprise Tier**: Soft Limit = 1000 req/min, Hard Limit = 1300 req/min, Replay Window = 600s.
   - **Monetized Burst Rate**: `cost_per_credit: 0.005` ($).
3. **Atomic Redis Lua Sliding Window Log**:
   - Uses `ratelimit:{api_key}` sorted sets.
   - Executes atomic score pruning, count evaluation, and conditional burst billing.
   - Dynamic headers: `X-RateLimit-Limit`, `X-RateLimit-Remaining`, `X-RateLimit-Burst-Limit`, `X-RateLimit-Burst-Remaining`, `X-RateLimit-Status`.
4. **L1 Go In-Memory Smart Fallback Cache & Circuit Breaker**:
   - Concurrency-safe local cache (`sync.RWMutex` + TTL eviction).
   - Serves stale cached responses with header `X-Cache-Status: STALE-FALLBACK` when upstream trips the circuit breaker or latency threshold.

---

### 📌 Milestone 2: Zero-Trust Security, Envelope Encryption & Universal Webhooks
1. **AES-256-GCM Envelope Encryption**:
   - API keys and webhook client secrets stored encrypted in PostgreSQL.
   - Nonce-prepended authenticated ciphertext with 128-bit integrity tags.
2. **Anti-Replay Defense Pipeline**:
   - Signature header: `X-Aegis-Signature: t={timestamp},v1={hex_hmac_sha256}`.
   - Enforces constant-time verification with `subtle.ConstantTimeCompare`.
   - Rejects stale timestamps beyond per-tier tolerance windows and future clock drift (>10s).
3. **Universal Webhook Engine & DLQ**:
   - Asynchronous worker consuming from Redis Stream `stream:gateway:events`.
   - Adapters for **Generic JSON**, **Discord Rich Embeds**, and **Slack Block Kit**.
   - **Dead-Letter Queue (DLQ)** with exponential backoff and **Re-Signing Engine** for fresh timestamp retries.

---

### 📌 Milestone 3: TUI Cyber-Deck Cockpit, Chaos Drills & Safety Panic Modals
1. **Interactive Terminal Interface (`bubbletea` & `lipgloss`)**:
   - **Smart TTY Detection**: Automatically detects interactive terminal vs. headless/Docker environments.
   - **Theme Hot-Swapping (`[T]`)**: Real-time switching between **Dracula**, **Catppuccin Mocha**, and **Matrix Cyberpunk**.
   - **ASCII Sparklines**: Live p95/p99 latency tracking (` ▂▃▄▅▆▇█`), request breakdown meters, and DLQ counter.
2. **Embedded Chaos Engine**:
   - `[Space]`: Unleash sudden traffic bursts (50 to 1000 RPS).
   - `[L]`: Toggle upstream simulated jitter (200-500ms).
   - `[A]`: Inject simulated Replay Attack with expired timestamps.
3. **Two-Step Safety Panic Modals**:
   - `[Ctrl+K]`: Key Kill-Switch (Freeze compromised API key in Redis & Postgres).
   - `[Ctrl+B]`: Global Burst Freeze (Temporarily disable burst platform-wide).
   - `[Ctrl+U]`: Emergency Upstream Cutoff (Route all traffic immediately to L1 Smart Fallback).
   - Dispatches signed HMAC Audit Event to `#security-incidents`.

---

### 📌 Milestone 4: Cloud-Native Telemetry, Monetization Dashboard & Automation
1. **Prometheus Metrics (`/metrics`)**:
   - `aegispulse_requests_total{tier, status}`
   - `aegispulse_burst_overages_total{tier, api_key}`
   - `aegispulse_upstream_latency_seconds` (histogram)
   - `aegispulse_replay_attacks_blocked_total{tier}`
   - `aegispulse_cache_fallback_total`
   - `aegispulse_circuit_breaker_state{state}`
   - `aegispulse_dlq_pending_total`
   - `aegispulse_monetized_burst_revenue_dollars`
2. **Pre-Configured Grafana Dashboard**:
   - Real-time monetized burst revenue calculator (`burst_units * 0.005`).
   - Gauges for Circuit Breaker status and Redis memory.

---

## ⚡ Quick Start & Turnkey Execution

### Prerequisites
- Docker & Docker Compose
- Go 1.22+ (optional for local non-containerized execution)
- Make (GNU Makefile)

### 1. Spin up the Full Stack
```bash
make up
```
This spins up:
- **AegisPulse Gateway**: `http://localhost:8080` (Proxy & Telemetry)
- **Mock Upstream Service**: `http://localhost:8081`
- **Mock Webhook Receiver**: `http://localhost:8083`
- **Redis v7**: `localhost:6379`
- **PostgreSQL 16**: `localhost:5432` (with migrations & seeds)
- **Prometheus**: `http://localhost:9090`
- **Grafana**: `http://localhost:3000` (User: `admin`, Pass: `admin`)

---

### 2. Launch Interactive TUI Cyber-Deck Cockpit
```bash
make tui
```
*Hotkeys:*
- `[Space]`: Unleash sudden traffic burst
- `[L]`: Toggle upstream simulated jitter
- `[A]`: Inject simulated replay attack
- `[T]`: Cycle themes (Dracula / Mocha / Cyberpunk)
- `[Ctrl+K]`: Key Kill-Switch confirmation modal
- `[Ctrl+B]`: Global Burst Freeze confirmation modal
- `[Ctrl+U]`: Emergency Upstream Cutoff confirmation modal
- `[q]`: Quit

---

### 3. Run Automated 60-Second Guided Demo Scenario
```bash
make demo
```
Demonstrates:
1. Gateway health check & baseline metrics
2. Normal tier traffic (Under soft limit)
3. Burst traffic exceeding soft limit -> Incurring overage & revenue calculation
4. Hard limit threshold enforcement -> HTTP 429 BLOCKED
5. Anti-replay defense -> Expired signature rejection (HTTP 401)
6. Emergency upstream cutoff -> L1 smart cache stale fallback (`X-Cache-Status: STALE-FALLBACK`)

---

### 4. Run Automated Chaos & Attack Suite
```bash
make attack
```

### 5. Run Safety Panic Kill-Switch Drill
```bash
make kill-drill
```

---

## 🧪 Testing

Execute the comprehensive Go test suite:
```bash
go test -v -race -timeout 30s ./...
```
Tests cover:
- AES-256-GCM encryption & tamper detection
- Constant-time HMAC anti-replay verification & window tolerance
- Redis Lua sliding window logic & burst freeze
- L1 smart cache concurrency & stale fallback
- Circuit breaker state transitions & emergency cutoff
- Reverse proxy authentication, rate headers, and kill-switch
- Webhook adapters (Generic, Discord, Slack) & DLQ re-signing engine

---

## 📖 API Usage & Curl Examples

### Standard Request (Normal Tier)
```bash
curl -i -X GET http://localhost:8080/api/resource \
  -H "X-API-Key: aegis_live_free_key_1001"
```

### Signed Request (Anti-Replay Defense)
```bash
TIMESTAMP=$(date +%s)
SECRET="aegis_live_free_key_1001"
PAYLOAD='{"hello":"world"}'
SIG=$(echo -n "${TIMESTAMP}.${PAYLOAD}" | openssl dgst -sha256 -hmac "${SECRET}" | awk '{print $2}')

curl -i -X POST http://localhost:8080/api/resource \
  -H "X-API-Key: ${SECRET}" \
  -H "X-Aegis-Signature: t=${TIMESTAMP},v1=${SIG}" \
  -H "Content-Type: application/json" \
  -d "${PAYLOAD}"
```

### Trigger Key Kill-Switch via Admin API
```bash
curl -i -X POST http://localhost:8080/api/v1/panic/kill \
  -H "Content-Type: application/json" \
  -d '{"api_key":"aegis_live_compromised_key_9999"}'
```

### Retry Failed Webhook from Dead-Letter Queue (DLQ)
```bash
curl -i -X POST http://localhost:8080/api/v1/dlq/retry \
  -H "Content-Type: application/json" \
  -d '{"dlq_id":"dlq_12345"}'
```
