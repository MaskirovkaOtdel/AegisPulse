package cache

import (
	"fmt"
	"net/http"
	"sync"
	"testing"
	"time"
)

func TestSmartCache_ConcurrencyAndStale(t *testing.T) {
	c := NewSmartCache(50 * time.Millisecond)
	defer c.Close()

	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			key := fmt.Sprintf("/api/resource/%d", id%5)
			header := make(http.Header)
			header.Set("Content-Type", "application/json")
			c.Set(key, 200, header, []byte(`{"status":"ok"}`), 20*time.Millisecond)
			_, _ = c.Get(key)
		}(i)
	}
	wg.Wait()

	// Wait for TTL to pass
	time.Sleep(35 * time.Millisecond)

	// Live entry should be nil/false
	if _, ok := c.GetLive("/api/resource/0"); ok {
		t.Fatalf("expected live entry to be expired")
	}

	// Stale entry should still exist for fallback
	entry, ok := c.Get("/api/resource/0")
	if !ok || entry == nil {
		t.Fatalf("expected stale entry to be accessible for fallback")
	}
	if entry.StatusCode != 200 {
		t.Fatalf("expected cached status 200, got %d", entry.StatusCode)
	}
}

func TestCircuitBreaker_StateTransitions(t *testing.T) {
	cb := NewCircuitBreaker(3, 500*time.Millisecond, 50*time.Millisecond)

	if cb.GetState() != CircuitClosed {
		t.Fatalf("expected initial state CLOSED")
	}

	// Record 2 failures (threshold is 3)
	cb.RecordFailure(10 * time.Millisecond)
	cb.RecordFailure(10 * time.Millisecond)
	if cb.GetState() != CircuitClosed {
		t.Fatalf("expected still CLOSED after 2 failures")
	}

	// 3rd failure trips to OPEN
	cb.RecordFailure(10 * time.Millisecond)
	if cb.GetState() != CircuitOpen {
		t.Fatalf("expected state OPEN after 3 failures, got %s", cb.GetState())
	}

	allowed, _ := cb.AllowRequest()
	if allowed {
		t.Fatalf("expected request blocked when OPEN")
	}

	// Wait for cooldown
	time.Sleep(60 * time.Millisecond)

	// Next request should transition to HALF-OPEN
	allowed, state := cb.AllowRequest()
	if !allowed || state != CircuitHalfOpen {
		t.Fatalf("expected HALF-OPEN probe allowed, got allowed=%v, state=%s", allowed, state)
	}

	// Record 2 successes to close circuit
	cb.RecordSuccess()
	cb.RecordSuccess()
	if cb.GetState() != CircuitClosed {
		t.Fatalf("expected CLOSED after 2 successes in HALF-OPEN, got %s", cb.GetState())
	}
}

func TestCircuitBreaker_LatencyTripAndEmergencyCutoff(t *testing.T) {
	cb := NewCircuitBreaker(3, 200*time.Millisecond, 50*time.Millisecond)

	// Single extreme latency >= threshold trips breaker
	cb.RecordFailure(250 * time.Millisecond)
	if cb.GetState() != CircuitOpen {
		t.Fatalf("expected latency trip to OPEN")
	}

	// Reset
	time.Sleep(60 * time.Millisecond)
	cb.AllowRequest()
	cb.RecordSuccess()
	cb.RecordSuccess()
	if cb.GetState() != CircuitClosed {
		t.Fatalf("expected CLOSED")
	}

	// Emergency cutoff
	cb.SetEmergencyCutoff(true)
	allowed, state := cb.AllowRequest()
	if allowed || state != CircuitOpen {
		t.Fatalf("expected cutoff to force OPEN")
	}

	cb.SetEmergencyCutoff(false)
	allowed, state = cb.AllowRequest()
	if !allowed || state != CircuitClosed {
		t.Fatalf("expected normal state restored when cutoff is false")
	}
}
