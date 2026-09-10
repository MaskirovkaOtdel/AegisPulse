package metrics

import (
	"net/http"
	"sort"
	"sync"
	"sync/atomic"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

type Metrics struct {
	reg                       *prometheus.Registry
	RequestsTotal             *prometheus.CounterVec
	BurstOveragesTotal        *prometheus.CounterVec
	UpstreamLatencySeconds    prometheus.Histogram
	ReplayAttacksBlockedTotal *prometheus.CounterVec
	CacheFallbackTotal        prometheus.Counter
	CircuitBreakerState       *prometheus.GaugeVec
	DLQPendingTotal           prometheus.Gauge
	MonetizedBurstRevenue     prometheus.Gauge
	RedisMemoryBytes          prometheus.Gauge

	// Internal snapshot counters for fast TUI dashboard synchronization
	mu              sync.RWMutex
	normalCount     atomic.Int64
	burstCount      atomic.Int64
	blockedCount    atomic.Int64
	replayBlocked   atomic.Int64
	cacheFallbacks  atomic.Int64
	dlqPending      atomic.Int64
	redisMemory     atomic.Int64
	recentLatencies []float64
	latencyMu       sync.Mutex
}

func NewMetrics(customReg *prometheus.Registry) *Metrics {
	reg := customReg
	if reg == nil {
		reg = prometheus.NewRegistry()
		// Register standard Go runtime metrics
		// Note: DefaultRegisterer can be used or custom reg
	}

	m := &Metrics{
		reg: reg,
		RequestsTotal: prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Name: "aegispulse_requests_total",
				Help: "Total count of requests processed by AegisPulse categorized by tier and rate limit status.",
			},
			[]string{"tier", "status"},
		),
		BurstOveragesTotal: prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Name: "aegispulse_burst_overages_total",
				Help: "Total count of burst overages incurred per tier and API key.",
			},
			[]string{"tier", "api_key"},
		),
		UpstreamLatencySeconds: prometheus.NewHistogram(
			prometheus.HistogramOpts{
				Name:    "aegispulse_upstream_latency_seconds",
				Help:    "Upstream response latency in seconds.",
				Buckets: []float64{0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1.0, 2.5, 5.0},
			},
		),
		ReplayAttacksBlockedTotal: prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Name: "aegispulse_replay_attacks_blocked_total",
				Help: "Total number of replay attacks intercepted and blocked by the anti-replay pipeline.",
			},
			[]string{"tier"},
		),
		CacheFallbackTotal: prometheus.NewCounter(
			prometheus.CounterOpts{
				Name: "aegispulse_cache_fallback_total",
				Help: "Total number of requests served via the L1 Smart Fallback Cache.",
			},
		),
		CircuitBreakerState: prometheus.NewGaugeVec(
			prometheus.GaugeOpts{
				Name: "aegispulse_circuit_breaker_state",
				Help: "Current state of the upstream circuit breaker (0=closed, 1=half-open, 2=open).",
			},
			[]string{"state"},
		),
		DLQPendingTotal: prometheus.NewGauge(
			prometheus.GaugeOpts{
				Name: "aegispulse_dlq_pending_total",
				Help: "Current count of failed webhook deliveries stored in Dead-Letter Queue.",
			},
		),
		MonetizedBurstRevenue: prometheus.NewGauge(
			prometheus.GaugeOpts{
				Name: "aegispulse_monetized_burst_revenue_dollars",
				Help: "Calculated real-time burst overage revenue in USD.",
			},
		),
		RedisMemoryBytes: prometheus.NewGauge(
			prometheus.GaugeOpts{
				Name: "aegispulse_redis_memory_bytes",
				Help: "Current Redis memory consumption in bytes.",
			},
		),
		recentLatencies: make([]float64, 0, 100),
	}

	reg.MustRegister(
		m.RequestsTotal,
		m.BurstOveragesTotal,
		m.UpstreamLatencySeconds,
		m.ReplayAttacksBlockedTotal,
		m.CacheFallbackTotal,
		m.CircuitBreakerState,
		m.DLQPendingTotal,
		m.MonetizedBurstRevenue,
		m.RedisMemoryBytes,
	)

	return m
}

func (m *Metrics) Handler() http.Handler {
	return promhttp.HandlerFor(m.reg, promhttp.HandlerOpts{})
}

func (m *Metrics) RecordRequest(tier, status string) {
	m.RequestsTotal.WithLabelValues(tier, status).Inc()
	switch status {
	case "NORMAL":
		m.normalCount.Add(1)
	case "BURST":
		m.burstCount.Add(1)
	case "BLOCKED", "BURST_FROZEN":
		m.blockedCount.Add(1)
	}
}

func (m *Metrics) RecordBurstOverage(tier, apiKey string) {
	m.BurstOveragesTotal.WithLabelValues(tier, apiKey).Inc()
}

func (m *Metrics) RecordLatency(seconds float64) {
	m.UpstreamLatencySeconds.Observe(seconds)

	m.latencyMu.Lock()
	if len(m.recentLatencies) >= 100 {
		m.recentLatencies = m.recentLatencies[1:]
	}
	m.recentLatencies = append(m.recentLatencies, seconds)
	m.latencyMu.Unlock()
}

func (m *Metrics) RecordReplayBlocked(tier string) {
	m.ReplayAttacksBlockedTotal.WithLabelValues(tier).Inc()
	m.replayBlocked.Add(1)
}

func (m *Metrics) RecordCacheFallback() {
	m.CacheFallbackTotal.Inc()
	m.cacheFallbacks.Add(1)
}

func (m *Metrics) SetCircuitBreakerStateMetric(state string) {
	m.CircuitBreakerState.Reset()
	switch state {
	case "CLOSED":
		m.CircuitBreakerState.WithLabelValues("closed").Set(0)
	case "HALF-OPEN":
		m.CircuitBreakerState.WithLabelValues("half_open").Set(1)
	case "OPEN":
		m.CircuitBreakerState.WithLabelValues("open").Set(2)
	}
}

func (m *Metrics) SetDLQCount(count int64) {
	m.dlqPending.Store(count)
	m.DLQPendingTotal.Set(float64(count))
}

func (m *Metrics) SetRevenue(amount float64) {
	m.MonetizedBurstRevenue.Set(amount)
}

func (m *Metrics) SetRedisMemory(bytes float64) {
	m.redisMemory.Store(int64(bytes))
	m.RedisMemoryBytes.Set(bytes)
}

type Snapshot struct {
	NormalCount      int64
	BurstCount       int64
	BlockedCount     int64
	ReplayBlocked    int64
	CacheFallbacks   int64
	DLQPending       int64
	RedisMemoryBytes int64
	P95LatencyMs     float64
	P99LatencyMs     float64
	RecentSamples    []float64
}

func (m *Metrics) GetSnapshot() Snapshot {
	m.latencyMu.Lock()
	defer m.latencyMu.Unlock()

	p95, p99 := calculatePercentiles(m.recentLatencies)
	samplesCopy := append([]float64(nil), m.recentLatencies...)

	return Snapshot{
		NormalCount:      m.normalCount.Load(),
		BurstCount:       m.burstCount.Load(),
		BlockedCount:     m.blockedCount.Load(),
		ReplayBlocked:    m.replayBlocked.Load(),
		CacheFallbacks:   m.cacheFallbacks.Load(),
		DLQPending:       m.dlqPending.Load(),
		RedisMemoryBytes: m.redisMemory.Load(),
		P95LatencyMs:     p95 * 1000,
		P99LatencyMs:     p99 * 1000,
		RecentSamples:    samplesCopy,
	}
}

func calculatePercentiles(latencies []float64) (float64, float64) {
	n := len(latencies)
	if n == 0 {
		return 0, 0
	}
	sorted := append([]float64(nil), latencies...)
	sort.Float64s(sorted)

	p95Idx := int(float64(n) * 0.95)
	p99Idx := int(float64(n) * 0.99)
	if p95Idx >= n {
		p95Idx = n - 1
	}
	if p99Idx >= n {
		p99Idx = n - 1
	}
	return sorted[p95Idx], sorted[p99Idx]
}
