package proxy

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"aegispulse/config"
	"aegispulse/internal/cache"
	"aegispulse/internal/crypto"
	"aegispulse/internal/metrics"
	"aegispulse/internal/ratelimit"
	"aegispulse/internal/storage"
	"aegispulse/internal/webhook"
)

func setupTestProxy(t *testing.T, upstreamHandler http.HandlerFunc) (*GatewayProxy, *httptest.Server, *cache.SmartCache, *cache.CircuitBreaker) {
	upstreamServer := httptest.NewServer(upstreamHandler)

	cfgMgr, err := config.NewConfigManager("")
	if err != nil {
		t.Fatalf("config error: %v", err)
	}
	cfg := cfgMgr.Get()
	cfg.Upstream.TargetURL = upstreamServer.URL
	cfg.Tiers["free"] = config.TierConfig{
		SoftLimit:           3,
		HardLimit:           5,
		WindowSeconds:       60,
		ReplayWindowSeconds: 60,
	}
	cfgMgr.Update(cfg)

	cipher, _ := crypto.NewEnvelopeCipher("0123456789abcdef0123456789abcdef")
	repo, _ := storage.NewPostgresRepo(context.Background(), "", cipher, nil)

	limiter := ratelimit.NewLimiter(nil, "stream:gateway:events")
	smartCache := cache.NewSmartCache(time.Minute)
	circuitBreaker := cache.NewCircuitBreaker(2, 500*time.Millisecond, time.Second)
	promMetrics := metrics.NewMetrics(nil)
	dlq := webhook.NewDLQManager(nil, "")
	whEngine := webhook.NewEngine(cfgMgr, nil, dlq, promMetrics)

	proxy, err := NewGatewayProxy(cfgMgr, repo, limiter, smartCache, circuitBreaker, whEngine, promMetrics, nil)
	if err != nil {
		t.Fatalf("proxy init error: %v", err)
	}

	return proxy, upstreamServer, smartCache, circuitBreaker
}

func TestProxy_AuthenticationAndRateLimiting(t *testing.T) {
	proxy, upstream, _, _ := setupTestProxy(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"data":"success"}`))
	})
	defer upstream.Close()

	apiKey := "aegis_live_free_key_1001"

	// 1. Missing API Key -> 401
	req := httptest.NewRequest(http.MethodGet, "/api/data", nil)
	rr := httptest.NewRecorder()
	proxy.ServeHTTP(rr, req)
	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 for missing key, got %d", rr.Code)
	}

	// 2. Normal requests (1, 2, 3) -> 200 with NORMAL status
	for i := 1; i <= 3; i++ {
		req = httptest.NewRequest(http.MethodGet, "/api/data", nil)
		req.Header.Set("X-API-Key", apiKey)
		rr = httptest.NewRecorder()
		proxy.ServeHTTP(rr, req)

		if rr.Code != http.StatusOK {
			t.Fatalf("req %d: expected 200, got %d", i, rr.Code)
		}
		status := rr.Header().Get("X-RateLimit-Status")
		if status != "NORMAL" {
			t.Fatalf("req %d: expected X-RateLimit-Status NORMAL, got %s", i, status)
		}
	}

	// 3. Burst requests (4, 5) -> 200 with BURST status
	for i := 4; i <= 5; i++ {
		req = httptest.NewRequest(http.MethodGet, "/api/data", nil)
		req.Header.Set("X-API-Key", apiKey)
		rr = httptest.NewRecorder()
		proxy.ServeHTTP(rr, req)

		if rr.Code != http.StatusOK {
			t.Fatalf("req %d: expected 200 in burst, got %d", i, rr.Code)
		}
		status := rr.Header().Get("X-RateLimit-Status")
		if status != "BURST" {
			t.Fatalf("req %d: expected BURST, got %s", i, status)
		}
	}

	// 4. Blocked request (6) -> 429 with BLOCKED status
	req = httptest.NewRequest(http.MethodGet, "/api/data", nil)
	req.Header.Set("X-API-Key", apiKey)
	rr = httptest.NewRecorder()
	proxy.ServeHTTP(rr, req)

	if rr.Code != http.StatusTooManyRequests {
		t.Fatalf("expected 429 when hard limit exceeded, got %d", rr.Code)
	}
	status := rr.Header().Get("X-RateLimit-Status")
	if status != "BLOCKED" {
		t.Fatalf("expected BLOCKED, got %s", status)
	}
}

func TestProxy_SmartCacheFallbackAndEmergencyCutoff(t *testing.T) {
	upstreamUp := true
	proxy, upstream, _, cb := setupTestProxy(t, func(w http.ResponseWriter, r *http.Request) {
		if !upstreamUp {
			http.Error(w, "service down", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"upstream":"live_payload"}`))
	})
	defer upstream.Close()

	apiKey := "aegis_live_free_key_1001"

	// 1. Initial successful request primes the cache
	req := httptest.NewRequest(http.MethodGet, "/api/cached-resource", nil)
	req.Header.Set("X-API-Key", apiKey)
	rr := httptest.NewRecorder()
	proxy.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200 priming cache, got %d", rr.Code)
	}

	// 2. Simulate upstream failure
	upstreamUp = false

	// Next request should hit upstream failure, trip breaker and serve fallback cache
	req = httptest.NewRequest(http.MethodGet, "/api/cached-resource", nil)
	req.Header.Set("X-API-Key", apiKey)
	rr = httptest.NewRecorder()
	proxy.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected fallback 200, got %d", rr.Code)
	}
	if rr.Header().Get("X-Cache-Status") != "STALE-FALLBACK" {
		t.Fatalf("expected X-Cache-Status STALE-FALLBACK, got %s", rr.Header().Get("X-Cache-Status"))
	}

	// 3. Test Emergency Cutoff [Ctrl+U]
	upstreamUp = true // upstream recovered
	cb.SetEmergencyCutoff(true)

	req = httptest.NewRequest(http.MethodGet, "/api/cached-resource", nil)
	req.Header.Set("X-API-Key", apiKey)
	rr = httptest.NewRecorder()
	proxy.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected fallback 200 during emergency cutoff, got %d", rr.Code)
	}
	if rr.Header().Get("X-Cache-Status") != "STALE-FALLBACK" {
		t.Fatalf("expected STALE-FALLBACK header during emergency cutoff")
	}
}

func TestProxy_KillSwitchAndAntiReplay(t *testing.T) {
	proxy, upstream, _, _ := setupTestProxy(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"ok":true}`))
	})
	defer upstream.Close()

	apiKey := "aegis_live_compromised_key_9999"

	// Test Kill Switch via admin endpoint
	killReqBody := fmt.Sprintf(`{"api_key":"%s"}`, apiKey)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/panic/kill", strings.NewReader(killReqBody))
	rr := httptest.NewRecorder()
	proxy.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200 for kill switch, got %d", rr.Code)
	}

	// Now try request with the killed API key -> 403 Forbidden
	req = httptest.NewRequest(http.MethodGet, "/api/secure", nil)
	req.Header.Set("X-API-Key", apiKey)
	rr = httptest.NewRecorder()
	proxy.ServeHTTP(rr, req)

	if rr.Code != http.StatusForbidden {
		t.Fatalf("expected 403 for killed key, got %d", rr.Code)
	}

	// Test Anti-Replay with active key
	activeKey := "aegis_live_pro_key_2002"
	body := []byte(`{"action":"transfer"}`)

	// Expired signature (e.g. 500 seconds ago, pro tolerance is 300s)
	expiredTimestamp := time.Now().Unix() - 500
	expiredSig := crypto.GenerateSignature(body, []byte(activeKey), expiredTimestamp)

	req = httptest.NewRequest(http.MethodPost, "/api/transact", bytes.NewReader(body))
	req.Header.Set("X-API-Key", activeKey)
	req.Header.Set("X-Aegis-Signature", expiredSig)
	rr = httptest.NewRecorder()
	proxy.ServeHTTP(rr, req)

	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 for expired signature, got %d", rr.Code)
	}
}

func TestProxy_NetworkFailureCircuitBreakerAndStaleCache(t *testing.T) {
	proxy, upstream, _, cb := setupTestProxy(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"upstream":"live_data"}`))
	})

	apiKey := "aegis_live_free_key_1001"

	// 1. Prime cache
	req := httptest.NewRequest(http.MethodGet, "/api/crash-resource", nil)
	req.Header.Set("X-API-Key", apiKey)
	rr := httptest.NewRecorder()
	proxy.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200 when priming cache, got %d", rr.Code)
	}

	// 2. Shut down upstream server completely to simulate network drop / connection failure
	upstream.Close()

	// 3. First failure should serve stale cache and record failure on circuit breaker
	req = httptest.NewRequest(http.MethodGet, "/api/crash-resource", nil)
	req.Header.Set("X-API-Key", apiKey)
	rr = httptest.NewRecorder()
	proxy.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200 stale fallback on connection failure, got %d", rr.Code)
	}
	if rr.Header().Get("X-Cache-Status") != "STALE-FALLBACK" {
		t.Fatalf("expected STALE-FALLBACK, got %s", rr.Header().Get("X-Cache-Status"))
	}

	// 4. Second failure should trip breaker to OPEN (threshold is 2 in setupTestProxy)
	req = httptest.NewRequest(http.MethodGet, "/api/crash-resource", nil)
	req.Header.Set("X-API-Key", apiKey)
	rr = httptest.NewRecorder()
	proxy.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200 stale fallback on 2nd failure, got %d", rr.Code)
	}
	if cb.GetState() != cache.CircuitOpen {
		t.Fatalf("expected CircuitBreaker to be OPEN after 2 connection failures, got %s", cb.GetState())
	}

	// 5. Subsequent request served from cache without attempting upstream
	req = httptest.NewRequest(http.MethodGet, "/api/crash-resource", nil)
	req.Header.Set("X-API-Key", apiKey)
	rr = httptest.NewRecorder()
	proxy.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK || rr.Header().Get("X-Cache-Status") != "STALE-FALLBACK" {
		t.Fatalf("expected 200 STALE-FALLBACK when breaker is OPEN")
	}
}

func TestProxy_AntiReplayNilBody(t *testing.T) {
	proxy, upstream, _, _ := setupTestProxy(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"ok":true}`))
	})
	defer upstream.Close()

	apiKey := "aegis_live_pro_key_2002"
	now := time.Now().Unix()
	sig := crypto.GenerateSignature(nil, []byte(apiKey), now)

	req := httptest.NewRequest(http.MethodGet, "/api/nil-body", nil)
	req.Body = nil // Explicitly test nil Body
	req.Header.Set("X-API-Key", apiKey)
	req.Header.Set("X-Aegis-Signature", sig)

	rr := httptest.NewRecorder()
	// Must not panic
	proxy.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200 for valid nil-body signature, got %d", rr.Code)
	}
}

func TestProxy_DynamicTargetHotReload(t *testing.T) {
	upstream1 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("server_1"))
	}))
	defer upstream1.Close()

	upstream2 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("server_2"))
	}))
	defer upstream2.Close()

	cfgMgr, _ := config.NewConfigManager("")
	cfg := cfgMgr.Get()
	cfg.Upstream.TargetURL = upstream1.URL
	cfgMgr.Update(cfg)

	cipher, _ := crypto.NewEnvelopeCipher("0123456789abcdef0123456789abcdef")
	repo, _ := storage.NewPostgresRepo(context.Background(), "", cipher, nil)
	limiter := ratelimit.NewLimiter(nil, "")
	smartCache := cache.NewSmartCache(time.Minute)
	cb := cache.NewCircuitBreaker(3, time.Second, time.Second)
	metrics := metrics.NewMetrics(nil)
	wh := webhook.NewEngine(cfgMgr, nil, webhook.NewDLQManager(nil, ""), metrics)

	gp, err := NewGatewayProxy(cfgMgr, repo, limiter, smartCache, cb, wh, metrics, nil)
	if err != nil {
		t.Fatalf("proxy init failed: %v", err)
	}

	apiKey := "aegis_live_free_key_1001"

	// Request 1 targets upstream1
	req := httptest.NewRequest(http.MethodGet, "/api/route", nil)
	req.Header.Set("X-API-Key", apiKey)
	rr := httptest.NewRecorder()
	gp.ServeHTTP(rr, req)
	if rr.Body.String() != "server_1" {
		t.Fatalf("expected server_1, got %s", rr.Body.String())
	}

	// Hot-reload config with new target URL
	updatedCfg := cfgMgr.Get()
	updatedCfg.Upstream.TargetURL = upstream2.URL
	cfgMgr.Update(updatedCfg)

	// Request 2 should dynamically route to upstream2 without restarting proxy
	req2 := httptest.NewRequest(http.MethodGet, "/api/route2", nil)
	req2.Header.Set("X-API-Key", apiKey)
	rr2 := httptest.NewRecorder()
	gp.ServeHTTP(rr2, req2)
	if rr2.Body.String() != "server_2" {
		t.Fatalf("expected dynamic routing to server_2 after hot-reload, got %s", rr2.Body.String())
	}
}

func TestProxy_PanicAdminEndpointsAndWebhooks(t *testing.T) {
	proxy, upstream, _, cb := setupTestProxy(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"ok":true}`))
	})
	defer upstream.Close()

	// 1. Burst Freeze
	req := httptest.NewRequest(http.MethodPost, "/api/v1/panic/burst-freeze", strings.NewReader(`{"frozen":true}`))
	rr := httptest.NewRecorder()
	proxy.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200 on burst freeze, got %d", rr.Code)
	}
	if !proxy.limiter.IsGlobalBurstFrozen() {
		t.Fatalf("expected limiter burst to be frozen")
	}

	// 2. Emergency Cutoff
	req = httptest.NewRequest(http.MethodPost, "/api/v1/panic/emergency-cutoff", strings.NewReader(`{"cutoff":true}`))
	rr = httptest.NewRecorder()
	proxy.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200 on emergency cutoff, got %d", rr.Code)
	}
	if !cb.IsEmergencyCutoff() {
		t.Fatalf("expected cb emergency cutoff to be true")
	}

	// 3. Status
	req = httptest.NewRequest(http.MethodGet, "/api/v1/panic/status", nil)
	rr = httptest.NewRecorder()
	proxy.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200 on panic status, got %d", rr.Code)
	}
	if !strings.Contains(rr.Body.String(), `"burst_frozen":true`) {
		t.Fatalf("expected status to report burst_frozen:true")
	}
}

func TestProxy_DemoPlayground_And_SwaggerDocs(t *testing.T) {
	proxy, upstream, _, _ := setupTestProxy(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("UPSTREAM_OK"))
	})
	defer upstream.Close()

	// 1. Test /demo
	reqDemo := httptest.NewRequest(http.MethodGet, "/demo", nil)
	rrDemo := httptest.NewRecorder()
	proxy.ServeHTTP(rrDemo, reqDemo)

	if rrDemo.Code != http.StatusOK {
		t.Fatalf("expected 200 for /demo, got %d", rrDemo.Code)
	}
	if !strings.Contains(rrDemo.Header().Get("Content-Type"), "text/html") {
		t.Fatalf("expected text/html for /demo, got %s", rrDemo.Header().Get("Content-Type"))
	}
	if !strings.Contains(rrDemo.Body.String(), "Dual-Threshold Token Bucket") {
		t.Fatalf("expected token bucket visualizer in /demo response")
	}

	// 2. Test /docs
	reqDocs := httptest.NewRequest(http.MethodGet, "/docs", nil)
	rrDocs := httptest.NewRecorder()
	proxy.ServeHTTP(rrDocs, reqDocs)

	if rrDocs.Code != http.StatusOK {
		t.Fatalf("expected 200 for /docs, got %d", rrDocs.Code)
	}
	if !strings.Contains(rrDocs.Header().Get("Content-Type"), "text/html") {
		t.Fatalf("expected text/html for /docs, got %s", rrDocs.Header().Get("Content-Type"))
	}

	// 3. Test /docs/openapi.json
	reqOpenAPI := httptest.NewRequest(http.MethodGet, "/docs/openapi.json", nil)
	rrOpenAPI := httptest.NewRecorder()
	proxy.ServeHTTP(rrOpenAPI, reqOpenAPI)

	if rrOpenAPI.Code != http.StatusOK {
		t.Fatalf("expected 200 for /docs/openapi.json, got %d", rrOpenAPI.Code)
	}
	if !strings.Contains(rrOpenAPI.Header().Get("Content-Type"), "application/json") {
		t.Fatalf("expected application/json for /docs/openapi.json, got %s", rrOpenAPI.Header().Get("Content-Type"))
	}
	if !strings.Contains(rrOpenAPI.Body.String(), `"openapi": "3.0.3"`) {
		t.Fatalf("expected OpenAPI 3.0.3 spec, got %s", rrOpenAPI.Body.String()[:100])
	}
}
