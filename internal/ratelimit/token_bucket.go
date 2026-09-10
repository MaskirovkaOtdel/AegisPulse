package ratelimit

import (
	"context"
	"math"
	"sync"
	"sync/atomic"
	"time"
)

// TokenBucket represents a single client token bucket
type TokenBucket struct {
	mu         sync.Mutex
	tokens     float64
	capacity   float64
	refillRate float64 // tokens per second
	lastRefill time.Time
}

// TokenBucketLimiter implements high-performance token bucket rate limiting
type TokenBucketLimiter struct {
	mu                sync.RWMutex
	buckets           map[string]*TokenBucket
	overages          map[string]int64
	globalBurstFreeze atomic.Bool
}

// NewTokenBucketLimiter creates a new TokenBucketLimiter
func NewTokenBucketLimiter() *TokenBucketLimiter {
	return &TokenBucketLimiter{
		buckets:  make(map[string]*TokenBucket),
		overages: make(map[string]int64),
	}
}

func (tbl *TokenBucketLimiter) SetGlobalBurstFreeze(frozen bool) {
	tbl.globalBurstFreeze.Store(frozen)
}

func (tbl *TokenBucketLimiter) IsGlobalBurstFrozen() bool {
	return tbl.globalBurstFreeze.Load()
}

func (tbl *TokenBucketLimiter) Check(ctx context.Context, apiKey string, tier string, softLimit, hardLimit, windowSec int64) (*RateLimitResult, error) {
	tbl.mu.Lock()
	bucket, exists := tbl.buckets[apiKey]
	if !exists {
		refillRate := float64(softLimit) / float64(windowSec)
		bucket = &TokenBucket{
			tokens:     float64(hardLimit),
			capacity:   float64(hardLimit),
			refillRate: refillRate,
			lastRefill: time.Now(),
		}
		tbl.buckets[apiKey] = bucket
	}
	tbl.mu.Unlock()

	bucket.mu.Lock()
	defer bucket.mu.Unlock()

	now := time.Now()
	elapsed := now.Sub(bucket.lastRefill).Seconds()
	bucket.lastRefill = now

	// Refill tokens
	bucket.tokens = math.Min(bucket.capacity, bucket.tokens+elapsed*bucket.refillRate)

	burstCapacity := float64(hardLimit - softLimit)
	if burstCapacity < 0 {
		burstCapacity = 0
	}

	burstLimit := hardLimit - softLimit
	if burstLimit < 0 {
		burstLimit = 0
	}

	// Case 1: Can consume token in Normal zone (above burst threshold)
	if bucket.tokens >= (burstCapacity + 1.0) {
		bucket.tokens -= 1.0
		rem := int64(bucket.tokens - burstCapacity)
		if rem < 0 {
			rem = 0
		}
		return &RateLimitResult{
			Status:         "NORMAL",
			Count:          softLimit - rem,
			Limit:          softLimit,
			Remaining:      rem,
			BurstLimit:     burstLimit,
			BurstRemaining: burstLimit,
		}, nil
	}

	// Case 2: In Burst zone (tokens between 1.0 and burstCapacity + 1.0)
	if bucket.tokens >= 1.0 && !tbl.globalBurstFreeze.Load() {
		bucket.tokens -= 1.0
		tbl.mu.Lock()
		tbl.overages[apiKey]++
		tbl.mu.Unlock()

		burstRem := int64(bucket.tokens)
		return &RateLimitResult{
			Status:         "BURST",
			Count:          hardLimit - burstRem,
			Limit:          softLimit,
			Remaining:      0,
			BurstLimit:     burstLimit,
			BurstRemaining: burstRem,
		}, nil
	}

	// Case 3: Exhausted / Blocked
	burstRem := int64(bucket.tokens)
	if burstRem < 0 {
		burstRem = 0
	}
	return &RateLimitResult{
		Status:         "BLOCKED",
		Count:          hardLimit,
		Limit:          softLimit,
		Remaining:      0,
		BurstLimit:     burstLimit,
		BurstRemaining: 0,
	}, nil
}

func (tbl *TokenBucketLimiter) GetOverageUnits(ctx context.Context, apiKey string) (int64, error) {
	tbl.mu.RLock()
	defer tbl.mu.RUnlock()
	return tbl.overages[apiKey], nil
}

func (tbl *TokenBucketLimiter) GetAllOverages(ctx context.Context) (map[string]int64, error) {
	tbl.mu.RLock()
	defer tbl.mu.RUnlock()
	res := make(map[string]int64, len(tbl.overages))
	for k, v := range tbl.overages {
		res[k] = v
	}
	return res, nil
}
