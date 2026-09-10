package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"net/http"
	"time"
)

func main() {
	port := flag.Int("port", 8081, "Port for mock upstream service")
	flag.Parse()

	mux := http.NewServeMux()

	// Normal healthy endpoint
	mux.HandleFunc("/api/resource", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"service":   "MockUpstreamService",
			"status":    "healthy",
			"timestamp": time.Now().Unix(),
			"path":      r.URL.Path,
			"method":    r.Method,
			"headers": map[string]string{
				"X-API-Key": r.Header.Get("X-API-Key"),
			},
		})
	})

	// Slow endpoint to trigger circuit breaker latency threshold
	mux.HandleFunc("/api/slow", func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(1200 * time.Millisecond)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(map[string]string{
			"message": "delayed upstream response",
		})
	})

	// Fault endpoint to trigger circuit breaker 500 error
	mux.HandleFunc("/api/fault", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		_ = json.NewEncoder(w).Encode(map[string]string{
			"error": "upstream database connection failure simulation",
		})
	})

	// Catch-all mock endpoint
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"message":   "AegisPulse Mock Upstream target reachable",
			"path":      r.URL.Path,
			"timestamp": time.Now().Unix(),
		})
	})

	addr := fmt.Sprintf(":%d", *port)
	log.Printf("[UPSTREAM] Mock Upstream Microservice listening on http://0.0.0.0:%d", *port)
	if err := http.ListenAndServe(addr, mux); err != nil {
		log.Fatalf("[UPSTREAM] Server failed: %v", err)
	}
}
