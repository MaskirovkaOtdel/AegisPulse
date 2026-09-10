package cache

import (
	"net/http"
	"testing"
	"time"
)

func BenchmarkSmartCache_GetSet(b *testing.B) {
	c := NewSmartCache(time.Minute)
	defer c.Close()

	header := http.Header{"Content-Type": []string{"application/json"}}
	body := []byte(`{"status":"ok","payload":"cached_benchmark_response"}`)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		key := "GET:/api/resource"
		c.Set(key, 200, header, body, 5*time.Minute)
		_, _ = c.Get(key)
	}
}

func BenchmarkSmartCache_Parallel(b *testing.B) {
	c := NewSmartCache(time.Minute)
	defer c.Close()

	header := http.Header{"Content-Type": []string{"application/json"}}
	body := []byte(`{"status":"ok","payload":"cached_benchmark_response"}`)
	key := "GET:/api/resource"
	c.Set(key, 200, header, body, 5*time.Minute)

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			_, _ = c.Get(key)
		}
	})
}
