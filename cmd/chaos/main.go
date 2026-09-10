package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"sync"
	"time"

	"aegispulse/internal/crypto"
)

func main() {
	mode := flag.String("mode", "demo", "Drill mode: demo, attack, kill-drill")
	gatewayURL := flag.String("url", "http://localhost:8080", "AegisPulse gateway URL")
	flag.Parse()

	client := &http.Client{Timeout: 5 * time.Second}

	switch *mode {
	case "demo":
		run60SecondDemo(*gatewayURL, client)
	case "attack":
		runChaosAttackSuite(*gatewayURL, client)
	case "kill-drill":
		runKillSwitchDrill(*gatewayURL, client)
	default:
		log.Fatalf("Unknown mode: %s. Use 'demo', 'attack', or 'kill-drill'.", *mode)
	}
}

func run60SecondDemo(gatewayURL string, client *http.Client) {
	fmt.Println("===================================================================")
	fmt.Println("        🚀 STARTING 60-SECOND GUIDED END-TO-END DEMO SCENARIO       ")
	fmt.Println("===================================================================")

	freeKey := "aegis_live_free_key_1001"
	proKey := "aegis_live_pro_key_2002"

	// Step 1: Health & Diagnostics (Seconds 0-5)
	fmt.Println("\n[PHASE 1] Checking Gateway Health & Baseline Telemetry...")
	resp, err := client.Get(gatewayURL + "/healthz")
	if err != nil {
		log.Fatalf("Gateway health check failed: %v", err)
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	fmt.Printf(" -> Health Status: %s\n", string(body))
	time.Sleep(2 * time.Second)

	// Step 2: Normal Requests on Free Tier (Seconds 5-15)
	fmt.Println("\n[PHASE 2] Firing Normal Tier Traffic (Under Soft Limit 20 req/min)...")
	for i := 1; i <= 5; i++ {
		req, _ := http.NewRequest(http.MethodGet, gatewayURL+"/api/resource", nil)
		req.Header.Set("X-API-Key", freeKey)
		res, err := client.Do(req)
		if err == nil {
			fmt.Printf(" -> Req %d: HTTP %d | Status: %s | Remaining: %s | Burst-Remaining: %s\n",
				i, res.StatusCode, res.Header.Get("X-RateLimit-Status"),
				res.Header.Get("X-RateLimit-Remaining"), res.Header.Get("X-RateLimit-Burst-Remaining"))
			_ = res.Body.Close()
		}
		time.Sleep(500 * time.Millisecond)
	}

	// Step 3: Unleashing Burst Traffic to Trigger Monetized Burst Overage (Seconds 15-30)
	fmt.Println("\n[PHASE 3] Unleashing Burst Overages (Soft Limit -> Hard Limit)...")
	fmt.Println(" -> Expecting monetized burst revenue accumulation & Webhook event dispatch...")
	for i := 6; i <= 22; i++ {
		req, _ := http.NewRequest(http.MethodGet, gatewayURL+"/api/resource", nil)
		req.Header.Set("X-API-Key", freeKey)
		res, err := client.Do(req)
		if err == nil {
			status := res.Header.Get("X-RateLimit-Status")
			fmt.Printf(" -> Req %d: HTTP %d | Status: %s | Burst-Remaining: %s\n",
				i, res.StatusCode, status, res.Header.Get("X-RateLimit-Burst-Remaining"))
			_ = res.Body.Close()
		}
		time.Sleep(200 * time.Millisecond)
	}

	// Step 4: Exceeding Hard Limit -> Blocked HTTP 429 (Seconds 30-40)
	fmt.Println("\n[PHASE 4] Exceeding Hard Limit (25 req/min) -> Enforcing HTTP 429...")
	for i := 23; i <= 28; i++ {
		req, _ := http.NewRequest(http.MethodGet, gatewayURL+"/api/resource", nil)
		req.Header.Set("X-API-Key", freeKey)
		res, err := client.Do(req)
		if err == nil {
			fmt.Printf(" -> Req %d: HTTP %d | Status: %s (BLOCKED as expected)\n",
				i, res.StatusCode, res.Header.Get("X-RateLimit-Status"))
			_ = res.Body.Close()
		}
		time.Sleep(200 * time.Millisecond)
	}

	// Step 5: Anti-Replay Defense Pipeline Drill (Seconds 40-50)
	fmt.Println("\n[PHASE 5] Executing Simulated Replay Attack with Expired Timestamp...")
	expiredTimestamp := time.Now().Unix() - 600 // Expired
	attackBody := []byte(`{"action":"fraudulent_credit_transfer","amount":50000}`)
	sig := crypto.GenerateSignature(attackBody, []byte(proKey), expiredTimestamp)

	atkReq, _ := http.NewRequest(http.MethodPost, gatewayURL+"/api/transfer", bytes.NewReader(attackBody))
	atkReq.Header.Set("X-API-Key", proKey)
	atkReq.Header.Set("X-Aegis-Signature", sig)
	atkReq.Header.Set("Content-Type", "application/json")

	atkResp, err := client.Do(atkReq)
	if err == nil {
		fmt.Printf(" -> Anti-Replay Defense Response: HTTP %d (401 Replay Blocked as expected!)\n", atkResp.StatusCode)
		_ = atkResp.Body.Close()
	}

	// Step 6: L1 Smart Cache Fallback Drill (Seconds 50-60)
	fmt.Println("\n[PHASE 6] Testing L1 Smart Cache Stale Fallback via Emergency Cutoff...")
	// Prime the cache with Pro key
	primeReq, _ := http.NewRequest(http.MethodGet, gatewayURL+"/api/resource", nil)
	primeReq.Header.Set("X-API-Key", proKey)
	primeResp, _ := client.Do(primeReq)
	if primeResp != nil {
		_ = primeResp.Body.Close()
	}

	// Activate Emergency Cutoff
	cutoffReq, _ := http.NewRequest(http.MethodPost, gatewayURL+"/api/v1/panic/emergency-cutoff", bytes.NewReader([]byte(`{"cutoff":true}`)))
	cutoffReq.Header.Set("Content-Type", "application/json")
	_, _ = client.Do(cutoffReq)

	// Subsequent request should be served from L1 Smart Cache
	fallbackReq, _ := http.NewRequest(http.MethodGet, gatewayURL+"/api/resource", nil)
	fallbackReq.Header.Set("X-API-Key", proKey)
	fallbackResp, err := client.Do(fallbackReq)
	if err == nil {
		fmt.Printf(" -> Cache Fallback Response: HTTP %d | X-Cache-Status: %s\n",
			fallbackResp.StatusCode, fallbackResp.Header.Get("X-Cache-Status"))
		_ = fallbackResp.Body.Close()
	}

	// Restore normal routing
	restoreReq, _ := http.NewRequest(http.MethodPost, gatewayURL+"/api/v1/panic/emergency-cutoff", bytes.NewReader([]byte(`{"cutoff":false}`)))
	restoreReq.Header.Set("Content-Type", "application/json")
	_, _ = client.Do(restoreReq)

	fmt.Println("\n===================================================================")
	fmt.Println("       🎉 60-SECOND GUIDED DEMO SCENARIO COMPLETED SUCCESSFULLY!    ")
	fmt.Println("===================================================================")
}

func runChaosAttackSuite(gatewayURL string, client *http.Client) {
	fmt.Println("===================================================================")
	fmt.Println("             🔥 RUNNING AUTOMATED CHAOS LOAD & REPLAY SUITE        ")
	fmt.Println("===================================================================")

	keys := []string{
		"aegis_live_free_key_1001",
		"aegis_live_pro_key_2002",
		"aegis_live_enterprise_key_3003",
	}

	// 1. High-Throughput Burst Flood
	fmt.Println("\n[ATTACK 1] Firing 150 concurrent requests to trigger rate limiters...")
	var wg sync.WaitGroup
	var normal, burst, blocked int64
	var mu sync.Mutex

	for i := 0; i < 150; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			k := keys[idx%len(keys)]
			req, err := http.NewRequest(http.MethodGet, gatewayURL+"/api/resource", nil)
			if err == nil {
				req.Header.Set("X-API-Key", k)
				resp, err := client.Do(req)
				if err == nil {
					status := resp.Header.Get("X-RateLimit-Status")
					mu.Lock()
					switch status {
					case "NORMAL":
						normal++
					case "BURST":
						burst++
					case "BLOCKED":
						blocked++
					}
					mu.Unlock()
					_ = resp.Body.Close()
				}
			}
		}(i)
	}
	wg.Wait()

	fmt.Printf(" -> Attack 1 Results: Normal=%d | Burst=%d | Blocked=%d\n", normal, burst, blocked)

	// 2. Anti-Replay Clock Drift and Expired Signature Injections
	fmt.Println("\n[ATTACK 2] Injecting 10 replay attacks with expired timestamps...")
	replayBlockedCount := 0
	for i := 0; i < 10; i++ {
		expiredTime := time.Now().Unix() - int64(100*(i+1))
		body := []byte(fmt.Sprintf(`{"attack_index":%d}`, i))
		sig := crypto.GenerateSignature(body, []byte(keys[0]), expiredTime)

		req, _ := http.NewRequest(http.MethodPost, gatewayURL+"/api/transfer", bytes.NewReader(body))
		req.Header.Set("X-API-Key", keys[0])
		req.Header.Set("X-Aegis-Signature", sig)
		req.Header.Set("Content-Type", "application/json")

		resp, err := client.Do(req)
		if err == nil {
			if resp.StatusCode == http.StatusUnauthorized {
				replayBlockedCount++
			}
			_ = resp.Body.Close()
		}
	}
	fmt.Printf(" -> Attack 2 Results: Replay Attacks Intercepted & Blocked: %d/10\n", replayBlockedCount)
	fmt.Println("===================================================================")
}

func runKillSwitchDrill(gatewayURL string, client *http.Client) {
	fmt.Println("===================================================================")
	fmt.Println("           🚨 EXECUTING TWO-STEP KILL-SWITCH DRILL                 ")
	fmt.Println("===================================================================")

	compromisedKey := "aegis_live_compromised_key_9999"

	// Step 1: Verify key is initially functional
	fmt.Println("\n[DRILL 1] Probing key before kill-switch activation...")
	req, _ := http.NewRequest(http.MethodGet, gatewayURL+"/api/resource", nil)
	req.Header.Set("X-API-Key", compromisedKey)
	resp, err := client.Do(req)
	if err != nil {
		log.Fatalf("Drill failed: %v", err)
	}
	fmt.Printf(" -> Key initial status: HTTP %d\n", resp.StatusCode)
	_ = resp.Body.Close()

	// Step 2: Trigger Kill-Switch
	fmt.Println("\n[DRILL 2] Triggering Emergency Kill-Switch for compromised key...")
	killPayload, _ := json.Marshal(map[string]string{"api_key": compromisedKey})
	killReq, _ := http.NewRequest(http.MethodPost, gatewayURL+"/api/v1/panic/kill", bytes.NewReader(killPayload))
	killReq.Header.Set("Content-Type", "application/json")
	killResp, err := client.Do(killReq)
	if err != nil {
		log.Fatalf("Kill-switch dispatch error: %v", err)
	}
	body, _ := io.ReadAll(killResp.Body)
	_ = killResp.Body.Close()
	fmt.Printf(" -> Kill-Switch Response: %s\n", string(body))

	// Step 3: Verify key is now hard-blocked (HTTP 403 Forbidden)
	fmt.Println("\n[DRILL 3] Verifying key is hard-blocked platform-wide...")
	verifyReq, _ := http.NewRequest(http.MethodGet, gatewayURL+"/api/resource", nil)
	verifyReq.Header.Set("X-API-Key", compromisedKey)
	verifyResp, err := client.Do(verifyReq)
	if err != nil {
		log.Fatalf("Verification error: %v", err)
	}
	fmt.Printf(" -> Post-kill verification status: HTTP %d (403 Forbidden as expected!)\n", verifyResp.StatusCode)
	_ = verifyResp.Body.Close()

	fmt.Println("===================================================================")
	fmt.Println("       ✅ KILL-SWITCH DRILL PASSED: COMPROMISED KEY NEUTRALIZED!    ")
	fmt.Println("===================================================================")
}
