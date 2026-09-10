package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"sync/atomic"
)

var receivedCount atomic.Int64

func main() {
	port := flag.Int("port", 8083, "Port for mock webhook receiver")
	flag.Parse()

	mux := http.NewServeMux()

	mux.HandleFunc("/webhook/generic", func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		sig := r.Header.Get("X-Aegis-Signature")
		count := receivedCount.Add(1)
		log.Printf("[MOCK-WEBHOOK] [GENERIC] #%d Received Sig: %s | Payload: %s", count, sig, string(body))
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"status":"delivered"}`))
	})

	mux.HandleFunc("/webhook/discord", func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		count := receivedCount.Add(1)
		log.Printf("[MOCK-WEBHOOK] [DISCORD] #%d Embed Received: %s", count, string(body))
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"status":"discord_embed_accepted"}`))
	})

	mux.HandleFunc("/webhook/slack", func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		count := receivedCount.Add(1)
		log.Printf("[MOCK-WEBHOOK] [SLACK] #%d Blocks Received: %s", count, string(body))
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"status":"slack_blocks_accepted"}`))
	})

	mux.HandleFunc("/webhook/failing", func(w http.ResponseWriter, r *http.Request) {
		log.Printf("[MOCK-WEBHOOK] [FAILING] Intentionally rejecting webhook to trigger DLQ!")
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "sink temporarily down"})
	})

	addr := fmt.Sprintf(":%d", *port)
	log.Printf("[WEBHOOK-RECEIVER] Mock Webhook Receiver listening on http://0.0.0.0:%d", *port)
	if err := http.ListenAndServe(addr, mux); err != nil {
		log.Fatalf("[WEBHOOK-RECEIVER] Server failed: %v", err)
	}
}
