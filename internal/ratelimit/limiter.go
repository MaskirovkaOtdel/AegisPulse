package ratelimit

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"net/http"
	"sort"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	"github.com/redis/go-redis/v9"
)

type RateLimitResult struct {
	Status         string // "NORMAL", "BURST", "BLOCKED"
	Count          int64
	Limit          int64
	Remaining      int64
	BurstLimit     int64
	BurstRemaining int64
}

func (r *RateLimitResult) ApplyHeaders(h http.Header) {
	h.Set("X-RateLimit-Limit", strconv.FormatInt(r.Limit, 10))
	h.Set("X-RateLimit-Remaining", strconv.FormatInt(r.Remaining, 10))
	h.Set("X-RateLimit-Burst-Limit", strconv.FormatInt(r.BurstLimit, 10))
	h.Set("X-RateLimit-Burst-Remaining", strconv.FormatInt(r.BurstRemaining, 10))
	h.Set("X-RateLimit-Status", r.Status)
}

type Limiter interface {
	Check(ctx context.Context, apiKey string, tier string, softLimit, hardLimit, windowSec int64) (*RateLimitResult, error)
	SetGlobalBurstFreeze(frozen bool)
	IsGlobalBurstFrozen() bool
	GetOverageUnits(ctx context.Context, apiKey string) (int64, error)
	GetAllOverages(ctx context.Context) (map[string]int64, error)
}

type RedisLimiter struct {
	client            redis.UniversalClient
	streamEvents      string
	luaScript         *redis.Script
	globalBurstFreeze atomic.Bool

	// In-memory fallback if Redis is unavailable or unconfigured
	memFallback *InMemoryLimiter
}

func NewLimiter(client redis.UniversalClient, streamEvents string) *RedisLimiter {
	if streamEvents == "" {
		streamEvents = "stream:gateway:events"
	}

	return &RedisLimiter{
		client:       client,
		streamEvents: streamEvents,
		luaScript:    redis.NewScript(SlidingWindowLuaScript),
		memFallback:  NewInMemoryLimiter(),
	}
}

func (r *RedisLimiter) SetGlobalBurstFreeze(frozen bool) {
	r.globalBurstFreeze.Store(frozen)
	r.memFallback.SetGlobalBurstFreeze(frozen)
}

func (r *RedisLimiter) IsGlobalBurstFrozen() bool {
	return r.globalBurstFreeze.Load()
}

func (r *RedisLimiter) Check(ctx context.Context, apiKey string, tier string, softLimit, hardLimit, windowSec int64) (*RateLimitResult, error) {
	if r.client == nil {
		return r.memFallback.Check(ctx, apiKey, tier, softLimit, hardLimit, windowSec)
	}

	nowSec := time.Now().Unix()
	nowMonth := time.Now().Format("2006-01")
	reqID := randomHex(8)

	ratelimitKey := fmt.Sprintf("ratelimit:%s", apiKey)
	billingKey := fmt.Sprintf("billing:overage:%s:%s", nowMonth, apiKey)

	burstFrozenFlag := 0
	if r.globalBurstFreeze.Load() {
		burstFrozenFlag = 1
	}

	keys := []string{ratelimitKey, billingKey, r.streamEvents}
	args := []interface{}{
		nowSec,
		windowSec,
		softLimit,
		hardLimit,
		reqID,
		apiKey,
		tier,
		burstFrozenFlag,
	}

	res, err := r.luaScript.Run(ctx, r.client, keys, args...).Slice()
	if err != nil {
		// If Redis fails, gracefully fall back to local in-memory limiter
		return r.memFallback.Check(ctx, apiKey, tier, softLimit, hardLimit, windowSec)
	}

	if len(res) < 6 {
		return r.memFallback.Check(ctx, apiKey, tier, softLimit, hardLimit, windowSec)
	}

	status := fmt.Sprintf("%v", res[0])
	count, _ := strconv.ParseInt(fmt.Sprintf("%v", res[1]), 10, 64)
	limit, _ := strconv.ParseInt(fmt.Sprintf("%v", res[2]), 10, 64)
	hard, _ := strconv.ParseInt(fmt.Sprintf("%v", res[3]), 10, 64)
	rem, _ := strconv.ParseInt(fmt.Sprintf("%v", res[4]), 10, 64)
	burstRem, _ := strconv.ParseInt(fmt.Sprintf("%v", res[5]), 10, 64)

	burstLimit := hard - limit
	if burstLimit < 0 {
		burstLimit = 0
	}

	return &RateLimitResult{
		Status:         status,
		Count:          count,
		Limit:          limit,
		Remaining:      rem,
		BurstLimit:     burstLimit,
		BurstRemaining: burstRem,
	}, nil
}

func (r *RedisLimiter) GetOverageUnits(ctx context.Context, apiKey string) (int64, error) {
	if r.client == nil {
		return r.memFallback.GetOverageUnits(ctx, apiKey)
	}
	nowMonth := time.Now().Format("2006-01")
	billingKey := fmt.Sprintf("billing:overage:%s:%s", nowMonth, apiKey)
	val, err := r.client.HGet(ctx, billingKey, "units").Int64()
	if err == redis.Nil {
		return 0, nil
	}
	return val, err
}

func (r *RedisLimiter) GetAllOverages(ctx context.Context) (map[string]int64, error) {
	if r.client == nil {
		return r.memFallback.GetAllOverages(ctx)
	}

	nowMonth := time.Now().Format("2006-01")
	pattern := fmt.Sprintf("billing:overage:%s:*", nowMonth)
	keys, err := r.client.Keys(ctx, pattern).Result()
	if err != nil {
		return nil, err
	}

	results := make(map[string]int64)
	for _, k := range keys {
		val, err := r.client.HGet(ctx, k, "units").Int64()
		if err == nil {
			results[k] = val
		}
	}
	return results, nil
}

// InMemoryLimiter provides local thread-safe sliding window log
type InMemoryLimiter struct {
	mu                sync.RWMutex
	windows           map[string][]int64
	overages          map[string]int64
	globalBurstFreeze atomic.Bool
}

func NewInMemoryLimiter() *InMemoryLimiter {
	return &InMemoryLimiter{
		windows:  make(map[string][]int64),
		overages: make(map[string]int64),
	}
}

func (m *InMemoryLimiter) SetGlobalBurstFreeze(frozen bool) {
	m.globalBurstFreeze.Store(frozen)
}

func (m *InMemoryLimiter) IsGlobalBurstFrozen() bool {
	return m.globalBurstFreeze.Load()
}

func (m *InMemoryLimiter) Check(ctx context.Context, apiKey string, tier string, softLimit, hardLimit, windowSec int64) (*RateLimitResult, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	now := time.Now().Unix()
	minTime := now - windowSec

	timestamps := m.windows[apiKey]
	// Prune expired
	idx := sort.Search(len(timestamps), func(i int) bool {
		return timestamps[i] > minTime
	})
	if idx > 0 {
		timestamps = timestamps[idx:]
	}

	count := int64(len(timestamps))
	burstLimit := hardLimit - softLimit
	if burstLimit < 0 {
		burstLimit = 0
	}

	if count < softLimit {
		timestamps = append(timestamps, now)
		m.windows[apiKey] = timestamps
		remaining := softLimit - (count + 1)
		return &RateLimitResult{
			Status:         "NORMAL",
			Count:          count + 1,
			Limit:          softLimit,
			Remaining:      remaining,
			BurstLimit:     burstLimit,
			BurstRemaining: burstLimit,
		}, nil
	}

	if count < hardLimit && !m.globalBurstFreeze.Load() {
		timestamps = append(timestamps, now)
		m.windows[apiKey] = timestamps
		m.overages[apiKey]++
		burstRem := hardLimit - (count + 1)
		return &RateLimitResult{
			Status:         "BURST",
			Count:          count + 1,
			Limit:          softLimit,
			Remaining:      0,
			BurstLimit:     burstLimit,
			BurstRemaining: burstRem,
		}, nil
	}

	return &RateLimitResult{
		Status:         "BLOCKED",
		Count:          count,
		Limit:          softLimit,
		Remaining:      0,
		BurstLimit:     burstLimit,
		BurstRemaining: 0,
	}, nil
}

func (m *InMemoryLimiter) GetOverageUnits(ctx context.Context, apiKey string) (int64, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.overages[apiKey], nil
}

func (m *InMemoryLimiter) GetAllOverages(ctx context.Context) (map[string]int64, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	res := make(map[string]int64, len(m.overages))
	for k, v := range m.overages {
		res[k] = v
	}
	return res, nil
}

func randomHex(bytesLen int) string {
	b := make([]byte, bytesLen)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}
