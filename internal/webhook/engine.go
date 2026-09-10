package webhook

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"io"
	"log"
	"net/http"
	"sync"
	"time"

	"aegispulse/config"
	"aegispulse/internal/crypto"
	"aegispulse/internal/metrics"
	"github.com/redis/go-redis/v9"
)

type Engine struct {
	cfgManager  *config.ConfigManager
	redisClient redis.UniversalClient
	dlqManager  *DLQManager
	metrics     *metrics.Metrics
	httpClient  *http.Client
	eventsChan  chan *UniversalEvent
	stopCh      chan struct{}
	pollCancel  context.CancelFunc
	wg          sync.WaitGroup
}

func NewEngine(cfgManager *config.ConfigManager, redisClient redis.UniversalClient, dlqManager *DLQManager, m *metrics.Metrics) *Engine {
	return &Engine{
		cfgManager:  cfgManager,
		redisClient: redisClient,
		dlqManager:  dlqManager,
		metrics:     m,
		httpClient: &http.Client{
			Timeout: 4 * time.Second,
		},
		eventsChan: make(chan *UniversalEvent, 5000),
		stopCh:     make(chan struct{}),
	}
}

func (e *Engine) Start(ctx context.Context) {
	cfg := e.cfgManager.Get()
	workers := cfg.Webhooks.WorkerConcurrency
	if workers <= 0 {
		workers = 4
	}

	// Start local channel workers
	for i := 0; i < workers; i++ {
		e.wg.Add(1)
		go e.worker(ctx, i)
	}

	// Start Redis Stream poller if redis client is available
	if e.redisClient != nil {
		pollCtx, pollCancel := context.WithCancel(ctx)
		e.pollCancel = pollCancel
		e.wg.Add(1)
		go e.streamPoller(pollCtx)
	}

	log.Printf("[WEBHOOK] Universal Webhook Engine started with %d workers.", workers)
}

func (e *Engine) Stop() {
	if e.pollCancel != nil {
		e.pollCancel()
	}
	close(e.stopCh)
	e.wg.Wait()
}

func (e *Engine) Publish(event *UniversalEvent) {
	select {
	case e.eventsChan <- event:
	default:
		log.Printf("[WEBHOOK] Buffer full, dropped event: %s", event.EventID)
	}
}

func (e *Engine) DLQ() *DLQManager {
	return e.dlqManager
}

func (e *Engine) worker(ctx context.Context, id int) {
	defer e.wg.Done()

	for {
		select {
		case <-e.stopCh:
			return
		case <-ctx.Done():
			return
		case event := <-e.eventsChan:
			e.processEvent(ctx, event)
		}
	}
}

func (e *Engine) processEvent(ctx context.Context, event *UniversalEvent) {
	cfg := e.cfgManager.Get()
	sinks := cfg.Webhooks.Sinks

	for sinkType, detail := range sinks {
		if !detail.Enabled || detail.Endpoint == "" {
			continue
		}

		var payload []byte
		var err error
		switch sinkType {
		case "discord":
			payload, err = AdaptDiscord(event)
		case "slack":
			payload, err = AdaptSlack(event)
		default:
			payload, err = AdaptGeneric(event)
		}

		if err != nil {
			log.Printf("[WEBHOOK] Payload adaptation error for sink %s: %v", sinkType, err)
			continue
		}

		go e.deliverWithRetry(ctx, event, sinkType, detail.Endpoint, payload)
	}
}

func (e *Engine) deliverWithRetry(ctx context.Context, event *UniversalEvent, sinkType, endpoint string, payload []byte) {
	cfg := e.cfgManager.Get()
	maxRetries := cfg.Webhooks.MaxRetries
	if maxRetries <= 0 {
		maxRetries = 3
	}
	baseDelay := time.Duration(cfg.Webhooks.RetryBaseDelayMs) * time.Millisecond
	if baseDelay <= 0 {
		baseDelay = 1000 * time.Millisecond
	}

	secret := []byte("aegis_master_secret_webhook_signing_key_2026")

	var lastErr error
	for attempt := 0; attempt < maxRetries; attempt++ {
		if attempt > 0 {
			// Exponential backoff
			backoff := baseDelay * (1 << (attempt - 1))
			select {
			case <-ctx.Done():
				return
			case <-time.After(backoff):
			}
		}

		now := time.Now().Unix()
		sig := crypto.GenerateSignature(payload, secret, now)

		req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(payload))
		if err != nil {
			lastErr = err
			continue
		}

		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-Aegis-Signature", sig)
		req.Header.Set("User-Agent", "AegisPulse-Webhook/1.0")

		resp, err := e.httpClient.Do(req)
		if err != nil {
			lastErr = err
			continue
		}

		_, _ = io.Copy(io.Discard, resp.Body)
		resp.Body.Close()

		if resp.StatusCode >= 200 && resp.StatusCode < 300 {
			// Success!
			return
		}

		lastErr = fmt.Errorf("http status %d", resp.StatusCode)
	}

	// All retries exhausted: send to Dead-Letter Queue (DLQ)
	dlqItem := &DLQItem{
		ID:          fmt.Sprintf("dlq_%s_%s", event.EventID, randHex(4)),
		Event:       *event,
		SinkType:    sinkType,
		TargetURL:   endpoint,
		Secret:      string(secret),
		RetryCount:  maxRetries,
		LastError:   lastErr.Error(),
		FailedAt:    time.Now(),
		PayloadJSON: payload,
	}

	_ = e.dlqManager.Enqueue(ctx, dlqItem)
	count := e.dlqManager.GetPendingCount(ctx)
	if e.metrics != nil {
		e.metrics.SetDLQCount(count)
	}
}

func (e *Engine) streamPoller(ctx context.Context) {
	defer e.wg.Done()
	cfg := e.cfgManager.Get()
	stream := cfg.Redis.StreamEvents
	group := "cg:gateway:webhooks"
	consumer := fmt.Sprintf("consumer-%s", randHex(4))

	// Ensure consumer group exists
	_ = e.redisClient.XGroupCreateMkStream(ctx, stream, group, "$").Err()

	for {
		select {
		case <-e.stopCh:
			return
		case <-ctx.Done():
			return
		default:
			entries, err := e.redisClient.XReadGroup(ctx, &redis.XReadGroupArgs{
				Group:    group,
				Consumer: consumer,
				Streams:  []string{stream, ">"},
				Count:    10,
				Block:    2 * time.Second,
			}).Result()

			if err != nil && err != redis.Nil {
				select {
				case <-ctx.Done():
					return
				case <-e.stopCh:
					return
				case <-time.After(500 * time.Millisecond):
				}
				continue
			}

			for _, streamStream := range entries {
				for _, msg := range streamStream.Messages {
					event := &UniversalEvent{
						EventID:     msg.ID,
						EventType:   fmt.Sprintf("%v", msg.Values["event_type"]),
						APIKey:      fmt.Sprintf("%v", msg.Values["api_key"]),
						Tier:        fmt.Sprintf("%v", msg.Values["tier"]),
						Timestamp:   time.Now().Unix(),
						Title:       fmt.Sprintf("%v event", msg.Values["event_type"]),
						Description: fmt.Sprintf("Triggered by %v", msg.Values["api_key"]),
					}
					e.Publish(event)
					_ = e.redisClient.XAck(ctx, stream, group, msg.ID).Err()
				}
			}
		}
	}
}

func randHex(n int) string {
	b := make([]byte, n)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}
