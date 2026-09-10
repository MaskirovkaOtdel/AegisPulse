package storage

import (
	"context"
	"fmt"
	"log"
	"strconv"
	"strings"
	"time"

	"github.com/redis/go-redis/v9"
)

type RedisClient struct {
	Client redis.UniversalClient
}

func NewRedisClient(addr, password string, db int) (*RedisClient, error) {
	if addr == "" {
		addr = "localhost:6379"
	}

	opts := &redis.Options{
		Addr:         addr,
		Password:     password,
		DB:           db,
		MaxRetries:   3,
		DialTimeout:  2 * time.Second,
		ReadTimeout:  2 * time.Second,
		WriteTimeout: 2 * time.Second,
		PoolSize:     50,
		MinIdleConns: 10,
	}

	rdb := redis.NewClient(opts)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	if err := rdb.Ping(ctx).Err(); err != nil {
		log.Printf("[STORAGE] Redis connection to %s failed: %v. Rate limiter will use local fallback.", addr, err)
		return &RedisClient{Client: nil}, nil
	}

	log.Printf("[STORAGE] Redis connected successfully to %s", addr)
	return &RedisClient{Client: rdb}, nil
}

func (r *RedisClient) FreezeKey(ctx context.Context, apiKey string) error {
	if r.Client == nil {
		return nil
	}
	key := fmt.Sprintf("key_frozen:%s", apiKey)
	return r.Client.Set(ctx, key, "1", 24*time.Hour).Err()
}

func (r *RedisClient) IsKeyFrozen(ctx context.Context, apiKey string) bool {
	if r.Client == nil {
		return false
	}
	key := fmt.Sprintf("key_frozen:%s", apiKey)
	val, err := r.Client.Get(ctx, key).Result()
	return err == nil && val == "1"
}

func (r *RedisClient) GetMemoryUsage(ctx context.Context) (int64, error) {
	if r.Client == nil {
		return 0, nil
	}
	info, err := r.Client.Info(ctx, "memory").Result()
	if err != nil {
		return 0, err
	}
	for _, line := range strings.Split(info, "\r\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "used_memory:") {
			parts := strings.Split(line, ":")
			if len(parts) == 2 {
				return strconv.ParseInt(parts[1], 10, 64)
			}
		}
	}
	return 0, nil
}

func (r *RedisClient) Close() error {
	if r.Client != nil {
		return r.Client.Close()
	}
	return nil
}
