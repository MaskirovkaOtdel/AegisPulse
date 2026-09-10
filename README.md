# 🛡️ AegisPulse: Community Preview Edition

[![Go Version](https://img.shields.io/badge/Go-1.22+-00ADD8?style=flat&logo=go)](https://golang.org)
[![License](https://img.shields.io/badge/License-Apache%202.0-blue.svg)](LICENSE)
[![Rate Limiting](https://img.shields.io/badge/Benchmark-52ns%2Fop-brightgreen)](internal/ratelimit)
[![TUI](https://img.shields.io/badge/Cockpit-Bubbletea%2FLipgloss-purple)](cmd/tui)
[![Prometheus](https://img.shields.io/badge/Telemetry-Prometheus%20Native-E6522C?logo=prometheus)](cmd/gateway)

**AegisPulse Community Preview** is the open-source sample edition of AegisPulse — an enterprise-grade, ultra-low-latency API Gateway and Security Middleware engineered in Go 1.22+ to protect upstream microservices against distributed traffic storms, credential attacks, and cascading infrastructure failures.

This Public Preview edition showcases the core high-throughput reverse proxy, sub-microsecond rate limiters (Dual-Threshold Sliding Window Log & Token Bucket), L1 thread-safe smart cache fallback, the interactive Cyber-Deck TUI Cockpit, and Prometheus telemetry.

---

## 🏛️ System Architecture

```
                                  +---------------------------------------------+
                                  |         Interactive TUI Cyber-Deck          |
                                  |   (Bubbletea, Dracula/Mocha/Matrix Themes,  |
                                  |       Live Sparklines, Panic Safety Modals) |
                                  +----------------------+----------------------+
                                                         |
                                                         v
Incoming Requests                 +---------------------------------------------+
================> [ X-API-Key ] ->|             AegisPulse Gateway Core         |
                  [ Signature ]   |                                             |
                                  |  * Dual-Threshold Sliding Window Log (Lua)  |
                                  |  * Sub-Microsecond Token Bucket (52 ns/op)  |
                                  |  * Upstream Latency Jitter Simulation       |
                                  +------+-------------------------------+------+
                                         |                               |
                                         v                               v (Failure / Latency)
                                  +--------------+             +-------------------------+
                                  | Redis v9 /   |             |  L1 Smart Fallback Cache|
                                  | In-Memory    |             |  (sync.RWMutex, TTL,    |
                                  +-------+------+             |   Circuit Breaker)      |
                                          |                    +------------+------------+
                                          v (Burst Events)                  |
                                  +------------------------+                v (Stale Response)
                                  | Prometheus /metrics    |      Client receives:
                                  | Telemetry Exporter     |      X-Cache-Status: STALE-FALLBACK
                                  +------------------------+
```

---

## ⚖️ Community Preview vs. Enterprise Edition

AegisPulse follows an **Open-Core** architecture. The **Community Preview** provides the high-performance standalone gateway core and TUI cockpit for developers and evaluation, while the **Enterprise Edition** adds distributed cluster synchronization, PostgreSQL envelope encryption, multi-sink webhooks, and automated Stripe metered billing.

| Capability | Community Preview (Open Sample) | Enterprise Edition (Commercial / SaaS) |
| :--- | :---: | :---: |
| **High-Throughput Reverse Proxy** | ✅ Included (`httputil.ReverseProxy`) | ✅ Included + Dynamic Service Discovery |
| **Sliding Window Rate Limiter** | ✅ Dual-Threshold (Free / Pro) | ✅ Dynamic Unlimited Tiers + Redis Cluster |
| **Token Bucket Schedulers** | ✅ High-Speed In-Memory (52 ns/op) | ✅ Distributed Token Bucket / GCRA |
| **L1 Smart Cache Fallback** | ✅ Thread-Safe Cache + Circuit Breaker | ✅ Distributed Redis L2 Fallback Cache |
| **Cyber-Deck TUI Cockpit** | ✅ Dracula / Mocha / Matrix Themes | ✅ Multi-Node Cluster Fleet Dashboard |
| **Prometheus Telemetry** | ✅ Native `/metrics` Exporter | ✅ Grafana Provisioning + Tempo Tracing |
| **Upstream Jitter Simulator** | ✅ Included (200ms–500ms) | ✅ Automated Chaos Drill Pipelines |
| **AES-256-GCM Envelope Encryption** | ❌ *(Sample Mock)* | ✅ Full PostgreSQL Vault + KMS Rotation |
| **Universal Multi-Sink Webhooks** | ❌ *(Sample Mock)* | ✅ Full Discord / Slack Hooks + DLQ Re-signing |
| **Distributed Kill-Switch Sync** | ❌ *(Local Only)* | ✅ Instant Multi-Region Cluster Freeze |
| **Stripe Metered Billing Integration**| ❌ | ✅ Automated End-of-Month Usage Sync |
| **Production Helm & Terraform** | ❌ | ✅ Turnkey Kubernetes & AWS/GCP Modules |
| **Support & SLA** | Community GitHub Issues | 24/7 Enterprise SLA + Architecture Support |

---

## ⚡ Performance Benchmarks

Micro-benchmarks conducted on Go 1.22+ (AMD Ryzen 8-Core Processor):

| Component | Benchmark | Latency | Operations / Sec |
| :--- | :--- | :---: | :---: |
| **Token Bucket Limiter** | `BenchmarkTokenBucketLimiter_Check` | **52.41 ns/op** | ~19,000,000 ops/sec |
| **Sliding Window Log** | `BenchmarkInMemoryLimiter_Check` | **64.69 ns/op** | ~15,500,000 ops/sec |
| **Smart Cache Read** | `BenchmarkSmartCache_Parallel` | **26.84 ns/op** | ~37,200,000 ops/sec |

Run the benchmark suite locally:
```bash
go test -bench Benchmark ./...
```

---

## 🚀 Quickstart (Run in 10 Seconds)

### 1. Prerequisites
- Go 1.22+ installed
- *(Optional)* Docker & Docker Compose if testing with Redis

### 2. Start the Gateway
```bash
# Clone the repository
git clone https://github.com/MaskirovkaOtdel/AegisPulse.git
cd AegisPulse

# Launch the standalone gateway (runs with zero external database dependencies!)
go run ./cmd/gateway
```
The gateway will start on `http://localhost:8080` and expose:
- `GET /healthz`: Health check & diagnostics.
- `GET /metrics`: Prometheus metrics exporter.

### 3. Launch the Interactive Cyber-Deck Cockpit (TUI)
In a separate terminal window:
```bash
go run ./cmd/tui -url="http://localhost:8080"
```

#### Cockpit Keyboard Shortcuts:
- `[T]`: Hot-swap themes between **Dracula**, **Catppuccin Mocha**, **Matrix Cyberpunk**, and **Catppuccin Latte (Light)**.
- `[Space]`: Unleash sudden traffic bursts (50 to 1,000 concurrent RPS).
- `[L]`: Toggle upstream simulated jitter (200ms–500ms).
- `[A]`: Inject simulated replay attack with expired timestamps.
- `[Ctrl+K]`: Emergency Key Kill-Switch confirmation modal.
- `[Ctrl+B]`: Global Burst Freeze confirmation modal.
- `[Ctrl+U]`: Emergency Upstream Cutoff confirmation modal.

---

## 🧪 Automated Chaos Load & Verification Drills

The repository includes a standalone Chaos CLI to test rate limiting thresholds and circuit breakers:

```bash
# Run the 60-Second Guided Demo scenario
go run ./cmd/chaos -mode=demo -url="http://localhost:8080"

# Run the 150-request chaos load & replay attack suite
go run ./cmd/chaos -mode=attack -url="http://localhost:8080"

# Test the emergency kill-switch drill
go run ./cmd/chaos -mode=kill-drill -url="http://localhost:8080"
```

---

## 💼 Upgrading to AegisPulse Enterprise / Managed Cloud

If your organization requires:
1. **Multi-Region Distributed Redis Synchronization & Clustering**.
2. **PostgreSQL Envelope Encryption Vault** for secure client secrets.
3. **Automated Stripe Usage-Based Metered Billing Webhooks**.
4. **Universal Multi-Sink Webhooks** with Dead-Letter Queues and timestamp re-signing.
5. **Turnkey Kubernetes Helm Charts and Terraform Cloud Modules**.

📬 **Commercial Inquiries & Licensing:**
Contact: `thod.efstathiadis@gmail.com`  
GitHub Organization: [@MaskirovkaOtdel](https://github.com/MaskirovkaOtdel)

---

## 📄 License
AegisPulse Community Preview is open-source under the [Apache 2.0 License](LICENSE).  
Commercial editions are subject to the AegisPulse Commercial Software License.
