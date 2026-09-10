package cache

import (
	"net/http"
	"sync"
	"sync/atomic"
	"time"
)

type CircuitState int32

const (
	CircuitClosed CircuitState = iota
	CircuitHalfOpen
	CircuitOpen
)

func (s CircuitState) String() string {
	switch s {
	case CircuitClosed:
		return "CLOSED"
	case CircuitHalfOpen:
		return "HALF-OPEN"
	case CircuitOpen:
		return "OPEN"
	default:
		return "UNKNOWN"
	}
}

type CacheEntry struct {
	StatusCode int
	Header     http.Header
	Body       []byte
	ExpiresAt  time.Time
	CachedAt   time.Time
}

func (e *CacheEntry) IsExpired() bool {
	return time.Now().After(e.ExpiresAt)
}

type SmartCache struct {
	mu      sync.RWMutex
	entries map[string]*CacheEntry
	stopCh  chan struct{}
}

func NewSmartCache(cleanupInterval time.Duration) *SmartCache {
	sc := &SmartCache{
		entries: make(map[string]*CacheEntry),
		stopCh:  make(chan struct{}),
	}

	if cleanupInterval > 0 {
		go sc.cleanupLoop(cleanupInterval)
	}

	return sc
}

func (sc *SmartCache) Close() {
	select {
	case <-sc.stopCh:
	default:
		close(sc.stopCh)
	}
}

func (sc *SmartCache) cleanupLoop(interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-sc.stopCh:
			return
		case <-ticker.C:
			sc.EvictExpired()
		}
	}
}

func (sc *SmartCache) EvictExpired() {
	sc.mu.Lock()
	defer sc.mu.Unlock()
	now := time.Now()
	for k, v := range sc.entries {
		// Only remove if expired more than 2 hours ago (so stale cache remains available during extended outages)
		if now.Sub(v.ExpiresAt) > 2*time.Hour {
			delete(sc.entries, k)
		}
	}
}

func (sc *SmartCache) Set(key string, statusCode int, header http.Header, body []byte, ttl time.Duration) {
	sc.mu.Lock()
	defer sc.mu.Unlock()

	headerCopy := make(http.Header)
	for k, vv := range header {
		headerCopy[k] = append([]string(nil), vv...)
	}

	bodyCopy := make([]byte, len(body))
	copy(bodyCopy, body)

	now := time.Now()
	sc.entries[key] = &CacheEntry{
		StatusCode: statusCode,
		Header:     headerCopy,
		Body:       bodyCopy,
		ExpiresAt:  now.Add(ttl),
		CachedAt:   now,
	}
}

func (sc *SmartCache) Get(key string) (*CacheEntry, bool) {
	sc.mu.RLock()
	defer sc.mu.RUnlock()

	entry, ok := sc.entries[key]
	if !ok {
		return nil, false
	}

	return entry, true
}

func (sc *SmartCache) GetLive(key string) (*CacheEntry, bool) {
	entry, ok := sc.Get(key)
	if !ok || entry.IsExpired() {
		return nil, false
	}
	return entry, true
}

func (sc *SmartCache) Len() int {
	sc.mu.RLock()
	defer sc.mu.RUnlock()
	return len(sc.entries)
}

type CircuitBreaker struct {
	mu                   sync.Mutex
	state                CircuitState
	consecutiveFailures  int
	consecutiveSuccesses int
	failureThreshold     int
	cooldown             time.Duration
	latencyThreshold     time.Duration
	lastStateChange      time.Time
	emergencyCutoff      atomic.Bool
	halfOpenProbing      atomic.Bool
}

func NewCircuitBreaker(failureThreshold int, latencyThreshold time.Duration, cooldown time.Duration) *CircuitBreaker {
	if failureThreshold <= 0 {
		failureThreshold = 3
	}
	if cooldown <= 0 {
		cooldown = 10 * time.Second
	}
	if latencyThreshold <= 0 {
		latencyThreshold = 1000 * time.Millisecond
	}

	return &CircuitBreaker{
		state:            CircuitClosed,
		failureThreshold: failureThreshold,
		cooldown:         cooldown,
		latencyThreshold: latencyThreshold,
		lastStateChange:  time.Now(),
	}
}

func (cb *CircuitBreaker) SetEmergencyCutoff(enabled bool) {
	cb.emergencyCutoff.Store(enabled)
}

func (cb *CircuitBreaker) IsEmergencyCutoff() bool {
	return cb.emergencyCutoff.Load()
}

func (cb *CircuitBreaker) AllowRequest() (bool, CircuitState) {
	if cb.emergencyCutoff.Load() {
		return false, CircuitOpen
	}

	cb.mu.Lock()
	defer cb.mu.Unlock()

	now := time.Now()

	switch cb.state {
	case CircuitClosed:
		return true, CircuitClosed
	case CircuitOpen:
		if now.Sub(cb.lastStateChange) >= cb.cooldown {
			cb.state = CircuitHalfOpen
			cb.lastStateChange = now
			cb.consecutiveSuccesses = 0
			cb.consecutiveFailures = 0
			cb.halfOpenProbing.Store(true)
			return true, CircuitHalfOpen
		}
		return false, CircuitOpen
	case CircuitHalfOpen:
		if cb.halfOpenProbing.CompareAndSwap(false, true) {
			return true, CircuitHalfOpen
		}
		return false, CircuitHalfOpen
	default:
		return true, CircuitClosed
	}
}

func (cb *CircuitBreaker) RecordSuccess() {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	if cb.state == CircuitHalfOpen {
		cb.consecutiveSuccesses++
		cb.halfOpenProbing.Store(false)
		if cb.consecutiveSuccesses >= 2 {
			cb.state = CircuitClosed
			cb.consecutiveFailures = 0
			cb.lastStateChange = time.Now()
		}
	} else if cb.state == CircuitClosed {
		cb.consecutiveFailures = 0
	}
}

func (cb *CircuitBreaker) RecordFailure(latency time.Duration) {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	cb.consecutiveFailures++
	cb.halfOpenProbing.Store(false)

	if cb.state == CircuitHalfOpen {
		// Immediate trip back to Open
		cb.state = CircuitOpen
		cb.lastStateChange = time.Now()
		return
	}

	if cb.consecutiveFailures >= cb.failureThreshold || (latency > 0 && latency >= cb.latencyThreshold) {
		cb.state = CircuitOpen
		cb.lastStateChange = time.Now()
	}
}

func (cb *CircuitBreaker) GetState() CircuitState {
	if cb.emergencyCutoff.Load() {
		return CircuitOpen
	}
	cb.mu.Lock()
	defer cb.mu.Unlock()
	return cb.state
}

func (cb *CircuitBreaker) LatencyThreshold() time.Duration {
	return cb.latencyThreshold
}
