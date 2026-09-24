package proxy

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"math/rand"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strings"
	"sync/atomic"
	"time"

	"aegispulse/config"
	"aegispulse/internal/cache"
	"aegispulse/internal/crypto"
	"aegispulse/internal/demo"
	"aegispulse/internal/metrics"
	"aegispulse/internal/middleware"
	"aegispulse/internal/ratelimit"
	"aegispulse/internal/storage"
	"aegispulse/internal/webhook"
)

type GatewayProxy struct {
	cfgManager     *config.ConfigManager
	repo           storage.Repository
	redisClient    *storage.RedisClient
	limiter        ratelimit.Limiter
	smartCache     *cache.SmartCache
	circuitBreaker *cache.CircuitBreaker
	webhookEngine  *webhook.Engine
	metrics        *metrics.Metrics
	reverseProxy   *httputil.ReverseProxy
	upstreamURL    *url.URL
	jitterOverride atomic.Int32 // -1: use config, 0: disabled, 1: enabled
	cors           *middleware.CORSMiddleware
}

func NewGatewayProxy(
	cfgManager *config.ConfigManager,
	repo storage.Repository,
	limiter ratelimit.Limiter,
	smartCache *cache.SmartCache,
	circuitBreaker *cache.CircuitBreaker,
	webhookEngine *webhook.Engine,
	m *metrics.Metrics,
	redisClient *storage.RedisClient,
) (*GatewayProxy, error) {
	cfg := cfgManager.Get()
	targetURL, err := url.Parse(cfg.Upstream.TargetURL)
	if err != nil {
		return nil, fmt.Errorf("invalid upstream target url: %w", err)
	}

	gp := &GatewayProxy{
		cfgManager:     cfgManager,
		repo:           repo,
		redisClient:    redisClient,
		limiter:        limiter,
		smartCache:     smartCache,
		circuitBreaker: circuitBreaker,
		webhookEngine:  webhookEngine,
		metrics:        m,
		upstreamURL:    targetURL,
		cors:           middleware.NewCORSMiddleware(middleware.DefaultCORSConfig()),
	}
	gp.jitterOverride.Store(-1) // default to config

	// Initialize reverse proxy transport
	transport := &http.Transport{
		MaxIdleConns:          100,
		MaxIdleConnsPerHost:   50,
		IdleConnTimeout:       90 * time.Second,
		ResponseHeaderTimeout: time.Duration(cfg.Upstream.TimeoutMs) * time.Millisecond,
	}

	rp := httputil.NewSingleHostReverseProxy(targetURL)
	rp.Transport = transport
	defaultDirector := rp.Director
	rp.Director = func(req *http.Request) {
		currentCfg := gp.cfgManager.Get()
		if currentTarget, err := url.Parse(currentCfg.Upstream.TargetURL); err == nil && currentTarget.Host != "" {
			req.URL.Scheme = currentTarget.Scheme
			req.URL.Host = currentTarget.Host
			req.Host = currentTarget.Host
		} else {
			defaultDirector(req)
		}
	}
	rp.ErrorHandler = gp.handleProxyError

	gp.reverseProxy = rp
	return gp, nil
}

func (gp *GatewayProxy) ToggleJitter() bool {
	curr := gp.jitterOverride.Load()
	var newState int32
	if curr == 1 {
		newState = 0
	} else if curr == 0 {
		newState = 1
	} else {
		// Currently config default
		cfg := gp.cfgManager.Get()
		if cfg.Upstream.Jitter.Enabled {
			newState = 0
		} else {
			newState = 1
		}
	}
	gp.jitterOverride.Store(newState)
	return newState == 1
}

func (gp *GatewayProxy) IsJitterActive() bool {
	curr := gp.jitterOverride.Load()
	if curr == 1 {
		return true
	}
	if curr == 0 {
		return false
	}
	return gp.cfgManager.Get().Upstream.Jitter.Enabled
}

func (gp *GatewayProxy) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if gp.cors != nil {
		gp.cors.Wrap(http.HandlerFunc(gp.serveInternal)).ServeHTTP(w, r)
		return
	}
	gp.serveInternal(w, r)
}

func (gp *GatewayProxy) serveInternal(w http.ResponseWriter, r *http.Request) {
	// 1. Health check & Diagnostics
	if r.URL.Path == "/healthz" {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"status":"UP","gateway":"AegisPulse"}`))
		return
	}

	// 2. Metrics Endpoint
	if r.URL.Path == "/metrics" {
		gp.metrics.Handler().ServeHTTP(w, r)
		return
	}

	// 2b. Interactive Demo Playground
	if r.URL.Path == "/demo" || strings.HasPrefix(r.URL.Path, "/demo/") {
		demo.PlaygroundHandler().ServeHTTP(w, r)
		return
	}

	// 2c. OpenAPI 3.0 & Swagger Interactive Documentation
	if r.URL.Path == "/docs" || strings.HasPrefix(r.URL.Path, "/docs/") {
		demo.SwaggerHandler().ServeHTTP(w, r)
		return
	}

	// 3. Admin & Panic Controls (accept both /api/v1/... and /api/...)
	if strings.HasPrefix(r.URL.Path, "/api/v1/panic/") || strings.HasPrefix(r.URL.Path, "/api/panic/") ||
		strings.HasPrefix(r.URL.Path, "/api/v1/dlq") || strings.HasPrefix(r.URL.Path, "/api/dlq") {
		gp.handleAdminAPI(w, r)
		return
	}

	// 4. Authenticate API Key
	apiKeyHeader := r.Header.Get("X-API-Key")
	if apiKeyHeader == "" {
		gp.respondJSON(w, http.StatusUnauthorized, map[string]string{
			"error": "missing X-API-Key header",
		})
		return
	}

	// Immediate check if key was frozen across distributed instances via Redis
	if gp.redisClient != nil && gp.redisClient.IsKeyFrozen(r.Context(), apiKeyHeader) {
		gp.respondJSON(w, http.StatusForbidden, map[string]string{
			"error": "API key frozen by emergency security kill-switch",
		})
		return
	}

	keyRec, err := gp.repo.GetAPIKey(r.Context(), apiKeyHeader)
	if err != nil {
		if errors.Is(err, storage.ErrKeyFrozen) {
			gp.respondJSON(w, http.StatusForbidden, map[string]string{
				"error": "API key frozen by emergency security kill-switch",
			})
			return
		}
		gp.respondJSON(w, http.StatusUnauthorized, map[string]string{
			"error": "invalid or revoked API key",
		})
		return
	}

	tierName := strings.ToLower(keyRec.Tier)
	tierCfg, ok := gp.cfgManager.GetTier(tierName)
	if !ok {
		tierCfg = config.TierConfig{
			SoftLimit:           20,
			HardLimit:           25,
			WindowSeconds:       60,
			ReplayWindowSeconds: 60,
		}
	}

	// 5. Anti-Replay Defense Pipeline (Milestone 2)
	// If incoming request includes X-Aegis-Signature, verify it
	sigHeader := r.Header.Get("X-Aegis-Signature")
	if sigHeader != "" {
		var bodyBytes []byte
		if r.Body != nil {
			var err error
			bodyBytes, err = io.ReadAll(r.Body)
			if err != nil {
				gp.respondJSON(w, http.StatusBadRequest, map[string]string{"error": "failed to read body"})
				return
			}
			// Reset body for proxy forwarding
			r.Body = io.NopCloser(bytes.NewReader(bodyBytes))
		}

		rawKey := apiKeyHeader
		valid, sigErr := crypto.VerifySignature(bodyBytes, sigHeader, []byte(rawKey), tierCfg.ReplayWindowSeconds, time.Now().Unix())
		if !valid || sigErr != nil {
			gp.metrics.RecordReplayBlocked(tierName)
			gp.respondJSON(w, http.StatusUnauthorized, map[string]string{
				"error":   "anti-replay defense rejected signature",
				"details": sigErr.Error(),
			})
			return
		}
	}

	// 6. Dual-Threshold Rate Limiter (Milestone 1)
	rateRes, err := gp.limiter.Check(
		r.Context(),
		apiKeyHeader,
		tierName,
		tierCfg.SoftLimit,
		tierCfg.HardLimit,
		tierCfg.WindowSeconds,
	)
	if err != nil {
		log.Printf("[RATE_LIMIT] Limiter error: %v", err)
	}

	// Apply dynamic rate limit headers
	rateRes.ApplyHeaders(w.Header())
	gp.metrics.RecordRequest(tierName, rateRes.Status)

	if rateRes.Status == "BLOCKED" || rateRes.Status == "BURST_FROZEN" {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusTooManyRequests)
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"error":       "rate limit hard threshold exceeded",
			"status":      rateRes.Status,
			"soft_limit":  rateRes.Limit,
			"hard_limit":  rateRes.Limit + rateRes.BurstLimit,
			"retry_after": tierCfg.WindowSeconds,
		})
		return
	}

	if rateRes.Status == "BURST" {
		gp.metrics.RecordBurstOverage(tierName, apiKeyHeader)
		// Calculate monetized burst revenue
		cfg := gp.cfgManager.Get()
		overageUnits, _ := gp.limiter.GetOverageUnits(r.Context(), apiKeyHeader)
		earnedRevenue := float64(overageUnits) * cfg.Monetization.CostPerCredit
		gp.metrics.SetRevenue(earnedRevenue)

		nowMonth := time.Now().Format("2006-01")
		_ = gp.repo.RecordBillingOverage(r.Context(), apiKeyHeader, nowMonth, overageUnits, earnedRevenue)

		// When Redis is not in use (local in-memory limiter), publish event directly.
		// When Redis is active, the Lua script publishes to stream:gateway:events, which streamPoller consumes.
		if gp.redisClient == nil || gp.redisClient.Client == nil {
			gp.webhookEngine.Publish(&webhook.UniversalEvent{
				EventID:     fmt.Sprintf("burst_%d", time.Now().UnixNano()),
				EventType:   "BURST_OVERAGE",
				APIKey:      apiKeyHeader,
				Tier:        tierName,
				Timestamp:   time.Now().Unix(),
				Title:       fmt.Sprintf("Burst Overage: Tier %s", tierName),
				Description: fmt.Sprintf("API Key %s entered burst capacity. Total overage units: %d.", apiKeyHeader, overageUnits),
				Metadata: map[string]string{
					"CostPerCredit": fmt.Sprintf("%.4f", cfg.Monetization.CostPerCredit),
					"TotalRevenue":  fmt.Sprintf("%.4f USD", earnedRevenue),
				},
			})
		}
	}

	// 7. Upstream Jitter Simulation (Milestone 1)
	if gp.IsJitterActive() {
		cfg := gp.cfgManager.Get()
		pct := cfg.Upstream.Jitter.Percentage
		if pct <= 0 {
			pct = 25
		}
		if rand.Intn(100) < pct {
			minDelay := cfg.Upstream.Jitter.MinDelayMs
			maxDelay := cfg.Upstream.Jitter.MaxDelayMs
			if minDelay <= 0 {
				minDelay = 200
			}
			if maxDelay < minDelay {
				maxDelay = 500
			}
			jitterDuration := time.Duration(minDelay+rand.Intn(maxDelay-minDelay+1)) * time.Millisecond
			time.Sleep(jitterDuration)
		}
	}

	// 8. Circuit Breaker & Fallback Cache (Milestone 1)
	allowed, state := gp.circuitBreaker.AllowRequest()
	gp.metrics.SetCircuitBreakerStateMetric(state.String())

	cacheKey := r.Method + ":" + r.URL.RequestURI()

	if !allowed {
		// Circuit Breaker is OPEN or Emergency Cutoff is active: serve L1 Cache fallback
		if entry, ok := gp.smartCache.Get(cacheKey); ok {
			gp.serveCachedResponse(w, entry, "STALE-FALLBACK")
			gp.metrics.RecordCacheFallback()
			return
		}

		gp.respondJSON(w, http.StatusServiceUnavailable, map[string]string{
			"error":  "upstream circuit breaker is OPEN and no cache fallback is available",
			"status": "CIRCUIT_BREAKER_OPEN",
		})
		return
	}

	// 9. Forward request through reverse proxy with fallback interceptor
	start := time.Now()
	rec := NewResponseRecorder(w)
	gp.reverseProxy.ServeHTTP(rec, r)
	latency := time.Since(start)

	gp.metrics.RecordLatency(latency.Seconds())

	// If upstream encountered network connection error / timeout
	if rec.ProxyError != nil {
		gp.circuitBreaker.RecordFailure(latency)
		if entry, ok := gp.smartCache.Get(cacheKey); ok {
			gp.serveCachedResponse(w, entry, "STALE-FALLBACK")
			gp.metrics.RecordCacheFallback()
			return
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadGateway)
		_ = json.NewEncoder(w).Encode(map[string]string{
			"error":   "upstream unreachable",
			"details": rec.ProxyError.Error(),
		})
		return
	}

	// If upstream failed with 5xx, timeout, or extreme latency -> record failure in circuit breaker
	if rec.StatusCode >= 500 || latency >= gp.circuitBreaker.LatencyThreshold() {
		gp.circuitBreaker.RecordFailure(latency)
		// Check fallback cache
		if entry, ok := gp.smartCache.Get(cacheKey); ok {
			gp.serveCachedResponse(w, entry, "STALE-FALLBACK")
			gp.metrics.RecordCacheFallback()
			return
		}
	} else if rec.StatusCode >= 200 && rec.StatusCode < 400 {
		// Upstream success! Cache response in L1 smart cache
		gp.circuitBreaker.RecordSuccess()
		cfg := gp.cfgManager.Get()
		ttl := time.Duration(cfg.Cache.TTLSeconds) * time.Second
		if ttl <= 0 {
			ttl = 300 * time.Second
		}
		gp.smartCache.Set(cacheKey, rec.StatusCode, rec.Header(), rec.Body.Bytes(), ttl)
	}

	// Flush recorder to actual response writer
	rec.Flush()
}

func (gp *GatewayProxy) serveCachedResponse(w http.ResponseWriter, entry *cache.CacheEntry, statusHeader string) {
	for k, vv := range entry.Header {
		for _, v := range vv {
			w.Header().Add(k, v)
		}
	}
	w.Header().Set("X-Cache-Status", statusHeader)
	w.WriteHeader(entry.StatusCode)
	_, _ = w.Write(entry.Body)
}

func (gp *GatewayProxy) handleProxyError(w http.ResponseWriter, r *http.Request, err error) {
	if rec, ok := w.(*ResponseRecorder); ok {
		rec.ProxyError = err
	}
}

func (gp *GatewayProxy) handleAdminAPI(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	cleanPath := r.URL.Path
	if strings.HasPrefix(cleanPath, "/api/v1") {
		cleanPath = strings.TrimPrefix(cleanPath, "/api/v1")
	} else if strings.HasPrefix(cleanPath, "/api") {
		cleanPath = strings.TrimPrefix(cleanPath, "/api")
	}
	cleanPath = strings.TrimSuffix(cleanPath, "/")

	// POST /panic/kill: Freeze API key across Redis & Postgres
	if cleanPath == "/panic/kill" && r.Method == http.MethodPost {
		var req struct {
			APIKey string `json:"api_key"`
		}
		_ = json.NewDecoder(r.Body).Decode(&req)
		if req.APIKey == "" {
			w.WriteHeader(http.StatusBadRequest)
			_ = json.NewEncoder(w).Encode(map[string]string{"error": "api_key is required"})
			return
		}

		_ = gp.repo.FreezeAPIKey(r.Context(), req.APIKey)
		if gp.redisClient != nil {
			_ = gp.redisClient.FreezeKey(r.Context(), req.APIKey)
			_ = gp.redisClient.FreezeKey(r.Context(), crypto.HashKey(req.APIKey))
		}

		// Dispatch security audit event
		auditPayload := fmt.Sprintf(`{"action":"KILL_SWITCH","target_key":"%s","operator":"admin"}`, req.APIKey)
		nowUnix := time.Now().Unix()
		sig := crypto.GenerateSignature([]byte(auditPayload), []byte("aegis_master_key"), nowUnix)
		_ = gp.repo.CreateAuditLog(r.Context(), "KILL_SWITCH_ACTIVATED", auditPayload, sig)

		gp.webhookEngine.Publish(&webhook.UniversalEvent{
			EventID:     fmt.Sprintf("kill_%d", time.Now().UnixNano()),
			EventType:   "KILL_SWITCH",
			APIKey:      req.APIKey,
			Tier:        "SYSTEM",
			Timestamp:   nowUnix,
			Title:       "Emergency API Key Kill-Switch Activated",
			Description: fmt.Sprintf("API Key %s has been frozen platform-wide due to suspected compromise.", req.APIKey),
			Channel:     "#security-incidents",
			Signature:   sig,
		})

		_ = json.NewEncoder(w).Encode(map[string]string{"status": "frozen", "api_key": req.APIKey})
		return
	}

	// POST /panic/burst-freeze: Toggle Global Burst Freeze
	if cleanPath == "/panic/burst-freeze" && r.Method == http.MethodPost {
		var req struct {
			Frozen bool `json:"frozen"`
		}
		_ = json.NewDecoder(r.Body).Decode(&req)
		gp.limiter.SetGlobalBurstFreeze(req.Frozen)

		nowUnix := time.Now().Unix()
		auditPayload := fmt.Sprintf(`{"action":"BURST_FREEZE","frozen":%v}`, req.Frozen)
		sig := crypto.GenerateSignature([]byte(auditPayload), []byte("aegis_master_key"), nowUnix)
		_ = gp.repo.CreateAuditLog(r.Context(), "GLOBAL_BURST_FREEZE", auditPayload, sig)

		gp.webhookEngine.Publish(&webhook.UniversalEvent{
			EventID:     fmt.Sprintf("freeze_%d", time.Now().UnixNano()),
			EventType:   "SECURITY_INCIDENT",
			APIKey:      "SYSTEM",
			Tier:        "GLOBAL",
			Timestamp:   nowUnix,
			Title:       "GLOBAL BURST FREEZE TOGGLED",
			Description: fmt.Sprintf("Global burst freeze state set to %v", req.Frozen),
			Channel:     "#security-incidents",
			Signature:   sig,
		})

		_ = json.NewEncoder(w).Encode(map[string]interface{}{"burst_frozen": req.Frozen})
		return
	}

	// POST /panic/emergency-cutoff: Route all traffic to L1 cache
	if cleanPath == "/panic/emergency-cutoff" && r.Method == http.MethodPost {
		var req struct {
			Cutoff bool `json:"cutoff"`
		}
		_ = json.NewDecoder(r.Body).Decode(&req)
		gp.circuitBreaker.SetEmergencyCutoff(req.Cutoff)

		nowUnix := time.Now().Unix()
		auditPayload := fmt.Sprintf(`{"action":"EMERGENCY_UPSTREAM_CUTOFF","cutoff":%v}`, req.Cutoff)
		sig := crypto.GenerateSignature([]byte(auditPayload), []byte("aegis_master_key"), nowUnix)
		_ = gp.repo.CreateAuditLog(r.Context(), "EMERGENCY_UPSTREAM_CUTOFF", auditPayload, sig)

		gp.webhookEngine.Publish(&webhook.UniversalEvent{
			EventID:     fmt.Sprintf("cutoff_%d", time.Now().UnixNano()),
			EventType:   "SECURITY_INCIDENT",
			APIKey:      "SYSTEM",
			Tier:        "GATEWAY",
			Timestamp:   nowUnix,
			Title:       "EMERGENCY UPSTREAM CUTOFF TOGGLED",
			Description: fmt.Sprintf("Emergency cutoff state set to %v", req.Cutoff),
			Channel:     "#security-incidents",
			Signature:   sig,
		})

		_ = json.NewEncoder(w).Encode(map[string]interface{}{"emergency_cutoff": req.Cutoff})
		return
	}

	// POST /panic/jitter: Toggle or set simulated upstream jitter
	if cleanPath == "/panic/jitter" && r.Method == http.MethodPost {
		var req struct {
			Enabled *bool `json:"enabled"`
		}
		_ = json.NewDecoder(r.Body).Decode(&req)
		var active bool
		if req.Enabled != nil {
			if *req.Enabled {
				gp.jitterOverride.Store(1)
				active = true
			} else {
				gp.jitterOverride.Store(0)
				active = false
			}
		} else {
			active = gp.ToggleJitter()
		}
		_ = json.NewEncoder(w).Encode(map[string]interface{}{"jitter_active": active})
		return
	}

	// GET /panic/status: Return live resilience and safety status
	if cleanPath == "/panic/status" && r.Method == http.MethodGet {
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"circuit_breaker":  gp.circuitBreaker.GetState().String(),
			"emergency_cutoff": gp.circuitBreaker.IsEmergencyCutoff(),
			"burst_frozen":     gp.limiter.IsGlobalBurstFrozen(),
			"jitter_active":    gp.IsJitterActive(),
		})
		return
	}

	// POST /dlq/retry: Re-sign and retry DLQ item
	if cleanPath == "/dlq/retry" && r.Method == http.MethodPost {
		var req struct {
			DLQID string `json:"dlq_id"`
		}
		_ = json.NewDecoder(r.Body).Decode(&req)
		ok, err := gp.webhookEngine.DLQ().ReSignAndRetry(r.Context(), req.DLQID)
		if err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			_ = json.NewEncoder(w).Encode(map[string]interface{}{"success": false, "error": err.Error()})
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]interface{}{"success": ok})
		return
	}

	// GET /dlq: List DLQ items
	if cleanPath == "/dlq" && r.Method == http.MethodGet {
		items := gp.webhookEngine.DLQ().List(r.Context())
		_ = json.NewEncoder(w).Encode(map[string]interface{}{"items": items, "count": len(items)})
		return
	}

	w.WriteHeader(http.StatusNotFound)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": "not found"})
}

func (gp *GatewayProxy) respondJSON(w http.ResponseWriter, code int, payload interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(payload)
}

// ResponseRecorder captures reverse proxy output for caching and inspection
type ResponseRecorder struct {
	w          http.ResponseWriter
	StatusCode int
	HeaderMap  http.Header
	Body       *bytes.Buffer
	ProxyError error
}

func NewResponseRecorder(w http.ResponseWriter) *ResponseRecorder {
	return &ResponseRecorder{
		w:          w,
		StatusCode: http.StatusOK,
		HeaderMap:  make(http.Header),
		Body:       new(bytes.Buffer),
	}
}

func (r *ResponseRecorder) Header() http.Header {
	return r.HeaderMap
}

func (r *ResponseRecorder) Write(b []byte) (int, error) {
	return r.Body.Write(b)
}

func (r *ResponseRecorder) WriteHeader(code int) {
	r.StatusCode = code
}

func (r *ResponseRecorder) Flush() {
	for k, vv := range r.HeaderMap {
		for _, v := range vv {
			r.w.Header().Add(k, v)
		}
	}
	r.w.WriteHeader(r.StatusCode)
	_, _ = r.w.Write(r.Body.Bytes())
}
