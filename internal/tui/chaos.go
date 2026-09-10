package tui

import (
	"bytes"
	"context"
	"fmt"
	"math/rand"
	"net/http"
	"sync"
	"time"

	"aegispulse/internal/crypto"
)

type ChaosEngine struct {
	gatewayURL string
	httpClient *http.Client
	running    bool
	mu         sync.Mutex
}

func NewChaosEngine(gatewayURL string) *ChaosEngine {
	if gatewayURL == "" {
		gatewayURL = "http://localhost:8080"
	}
	return &ChaosEngine{
		gatewayURL: gatewayURL,
		httpClient: &http.Client{Timeout: 3 * time.Second},
	}
}

// UnleashBurst fires a sudden traffic burst of 50 to 1000 requests concurrently towards the gateway
func (ce *ChaosEngine) UnleashBurst(count int, onLog func(string)) {
	if count <= 0 {
		count = 50 + rand.Intn(951) // 50 to 1000 requests
	}

	onLog(fmt.Sprintf("🚀 [CHAOS] Unleashing burst of %d requests (50-1000 RPS range) across Free, Pro, and Enterprise keys...", count))

	keys := []string{
		"aegis_live_free_key_1001",
		"aegis_live_pro_key_2002",
		"aegis_live_enterprise_key_3003",
	}

	go func() {
		var wg sync.WaitGroup
		sem := make(chan struct{}, 100) // concurrency throttle capable of up to 1000 RPS

		for i := 0; i < count; i++ {
			wg.Add(1)
			go func(idx int) {
				defer wg.Done()
				sem <- struct{}{}
				defer func() { <-sem }()

				k := keys[idx%len(keys)]
				req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, ce.gatewayURL+"/api/resource", nil)
				if err == nil {
					req.Header.Set("X-API-Key", k)
					resp, err := ce.httpClient.Do(req)
					if err == nil {
						_ = resp.Body.Close()
					}
				}
			}(i)
		}
		wg.Wait()
		onLog(fmt.Sprintf("✅ [CHAOS] Traffic burst of %d requests finished.", count))
	}()
}

// InjectReplayAttack sends a request with an expired signature header
func (ce *ChaosEngine) InjectReplayAttack(onLog func(string)) {
	apiKey := "aegis_live_free_key_1001"
	onLog(fmt.Sprintf("⚔️ [CHAOS] Injecting simulated Replay Attack with expired timestamp using key [%s]...", apiKey))

	go func() {
		// Timestamp expired by 500 seconds (free tier window is 60s)
		expiredTimestamp := time.Now().Unix() - 500
		body := []byte(`{"action":"replay_exploit_attempt","amount":1000000}`)
		sig := crypto.GenerateSignature(body, []byte(apiKey), expiredTimestamp)

		req, err := http.NewRequestWithContext(context.Background(), http.MethodPost, ce.gatewayURL+"/api/transfer", bytes.NewReader(body))
		if err != nil {
			onLog(fmt.Sprintf("❌ [CHAOS] Error creating replay request: %v", err))
			return
		}

		req.Header.Set("X-API-Key", apiKey)
		req.Header.Set("X-Aegis-Signature", sig)
		req.Header.Set("Content-Type", "application/json")

		resp, err := ce.httpClient.Do(req)
		if err != nil {
			onLog(fmt.Sprintf("⚠️ [CHAOS] Replay request failed: %v", err))
			return
		}
		defer resp.Body.Close()

		if resp.StatusCode == http.StatusUnauthorized {
			onLog("🛡️ [CHAOS] Anti-Replay Defense SUCCESS: Request intercepted and rejected (HTTP 401)!")
		} else {
			onLog(fmt.Sprintf("⚠️ [CHAOS] Replay request returned unexpected status HTTP %d", resp.StatusCode))
		}
	}()
}
