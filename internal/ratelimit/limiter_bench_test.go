package ratelimit

import (
	"context"
	"testing"
)

func BenchmarkInMemoryLimiter_Check(b *testing.B) {
	limiter := NewInMemoryLimiter()
	ctx := context.Background()
	apiKey := "bench-key-01"

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = limiter.Check(ctx, apiKey, "enterprise", 100000, 150000, 60)
	}
}

func BenchmarkInMemoryLimiter_Parallel(b *testing.B) {
	limiter := NewInMemoryLimiter()
	ctx := context.Background()

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		i := 0
		for pb.Next() {
			i++
			key := "bench-parallel-key"
			_, _ = limiter.Check(ctx, key, "enterprise", 1000000, 1500000, 60)
		}
	})
}

func BenchmarkTokenBucketLimiter_Check(b *testing.B) {
	limiter := NewTokenBucketLimiter()
	ctx := context.Background()
	apiKey := "bench-tb-key-01"

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = limiter.Check(ctx, apiKey, "enterprise", 100000, 150000, 60)
	}
}

func BenchmarkTokenBucketLimiter_Parallel(b *testing.B) {
	limiter := NewTokenBucketLimiter()
	ctx := context.Background()

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			key := "bench-tb-parallel"
			_, _ = limiter.Check(ctx, key, "enterprise", 1000000, 1500000, 60)
		}
	})
}
