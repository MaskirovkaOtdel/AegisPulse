package ratelimit

import (
	"context"
	"testing"
)

func TestInMemoryLimiter_DualThreshold(t *testing.T) {
	ctx := context.Background()
	limiter := NewInMemoryLimiter()

	apiKey := "test-key-1"
	tier := "free"
	softLimit := int64(3)
	hardLimit := int64(5)
	windowSec := int64(60)

	// Requests 1, 2, 3 should be NORMAL
	for i := 1; i <= 3; i++ {
		res, err := limiter.Check(ctx, apiKey, tier, softLimit, hardLimit, windowSec)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if res.Status != "NORMAL" {
			t.Fatalf("req %d expected NORMAL, got %s", i, res.Status)
		}
		if res.Count != int64(i) {
			t.Fatalf("req %d expected count %d, got %d", i, i, res.Count)
		}
	}

	// Request 4 and 5 should be BURST
	for i := 4; i <= 5; i++ {
		res, err := limiter.Check(ctx, apiKey, tier, softLimit, hardLimit, windowSec)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if res.Status != "BURST" {
			t.Fatalf("req %d expected BURST, got %s", i, res.Status)
		}
		if res.Remaining != 0 {
			t.Fatalf("req %d expected Remaining 0, got %d", i, res.Remaining)
		}
	}

	// Request 6 should be BLOCKED (429)
	res, err := limiter.Check(ctx, apiKey, tier, softLimit, hardLimit, windowSec)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.Status != "BLOCKED" {
		t.Fatalf("expected BLOCKED after hard limit, got %s", res.Status)
	}
	if res.BurstRemaining != 0 {
		t.Fatalf("expected BurstRemaining 0, got %d", res.BurstRemaining)
	}

	// Verify overage units count
	units, err := limiter.GetOverageUnits(ctx, apiKey)
	if err != nil {
		t.Fatalf("unexpected error getting overage: %v", err)
	}
	if units != 2 {
		t.Fatalf("expected 2 overage units, got %d", units)
	}
}

func TestInMemoryLimiter_GlobalBurstFreeze(t *testing.T) {
	ctx := context.Background()
	limiter := NewInMemoryLimiter()

	apiKey := "test-key-freeze"
	tier := "free"
	softLimit := int64(2)
	hardLimit := int64(4)
	windowSec := int64(60)

	// Consume normal quota
	_, _ = limiter.Check(ctx, apiKey, tier, softLimit, hardLimit, windowSec)
	_, _ = limiter.Check(ctx, apiKey, tier, softLimit, hardLimit, windowSec)

	// Turn on Global Burst Freeze
	limiter.SetGlobalBurstFreeze(true)

	// 3rd request would normally be BURST, but freeze forces BLOCKED
	res, err := limiter.Check(ctx, apiKey, tier, softLimit, hardLimit, windowSec)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.Status != "BLOCKED" {
		t.Fatalf("expected BLOCKED when burst is frozen, got %s", res.Status)
	}

	// Unfreeze
	limiter.SetGlobalBurstFreeze(false)
	res, err = limiter.Check(ctx, apiKey, tier, softLimit, hardLimit, windowSec)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.Status != "BURST" {
		t.Fatalf("expected BURST after unfreezing, got %s", res.Status)
	}
}
