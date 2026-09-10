package webhook

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"sync"
	"time"

	"aegispulse/internal/crypto"
	"github.com/redis/go-redis/v9"
)

type DLQItem struct {
	ID          string         `json:"id"`
	Event       UniversalEvent `json:"event"`
	SinkType    string         `json:"sink_type"`
	TargetURL   string         `json:"target_url"`
	Secret      string         `json:"secret"`
	RetryCount  int            `json:"retry_count"`
	LastError   string         `json:"last_error"`
	FailedAt    time.Time      `json:"failed_at"`
	PayloadJSON []byte         `json:"payload_json"`
}

type DLQManager struct {
	mu          sync.RWMutex
	redisClient redis.UniversalClient
	streamDLQ   string
	inMemory    map[string]*DLQItem
	httpClient  *http.Client
}

func NewDLQManager(client redis.UniversalClient, streamDLQ string) *DLQManager {
	if streamDLQ == "" {
		streamDLQ = "stream:gateway:dlq"
	}
	return &DLQManager{
		redisClient: client,
		streamDLQ:   streamDLQ,
		inMemory:    make(map[string]*DLQItem),
		httpClient: &http.Client{
			Timeout: 5 * time.Second,
		},
	}
}

func (m *DLQManager) LoadFromRedis(ctx context.Context) error {
	if m.redisClient == nil {
		return nil
	}

	msgs, err := m.redisClient.XRange(ctx, m.streamDLQ, "-", "+").Result()
	if err != nil {
		return err
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	for _, msg := range msgs {
		if itemData, ok := msg.Values["item_data"].(string); ok {
			var item DLQItem
			if err := json.Unmarshal([]byte(itemData), &item); err == nil {
				m.inMemory[item.ID] = &item
			}
		}
	}
	return nil
}

func (m *DLQManager) Enqueue(ctx context.Context, item *DLQItem) error {
	m.mu.Lock()
	m.inMemory[item.ID] = item
	m.mu.Unlock()

	itemBytes, err := json.Marshal(item)
	if err != nil {
		return err
	}

	if m.redisClient != nil {
		err := m.redisClient.XAdd(ctx, &redis.XAddArgs{
			Stream: m.streamDLQ,
			Values: map[string]interface{}{
				"dlq_id":    item.ID,
				"item_data": string(itemBytes),
			},
		}).Err()
		if err != nil {
			log.Printf("[DLQ] Error persisting to Redis stream: %v", err)
		}
	}

	log.Printf("[DLQ] Enqueued failed webhook ID=%s Target=%s Error=%s", item.ID, item.TargetURL, item.LastError)
	return nil
}

func (m *DLQManager) GetPendingCount(ctx context.Context) int64 {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return int64(len(m.inMemory))
}

func (m *DLQManager) List(ctx context.Context) []*DLQItem {
	m.mu.RLock()
	defer m.mu.RUnlock()

	items := make([]*DLQItem, 0, len(m.inMemory))
	for _, v := range m.inMemory {
		items = append(items, v)
	}
	return items
}

// ReSignAndRetry dispatches the failed payload again with a freshly minted timestamp and HMAC signature
func (m *DLQManager) ReSignAndRetry(ctx context.Context, id string) (bool, error) {
	m.mu.Lock()
	item, ok := m.inMemory[id]
	if !ok {
		m.mu.Unlock()
		return false, fmt.Errorf("dlq item with id %s not found", id)
	}
	m.mu.Unlock()

	// Update timestamp on event
	now := time.Now().Unix()
	item.Event.Timestamp = now

	// Re-adapt payload for sink type
	var payload []byte
	var err error
	switch item.SinkType {
	case "discord":
		payload, err = AdaptDiscord(&item.Event)
	case "slack":
		payload, err = AdaptSlack(&item.Event)
	default:
		payload, err = AdaptGeneric(&item.Event)
	}
	if err != nil {
		return false, fmt.Errorf("failed to adapt payload during re-sign: %w", err)
	}
	item.PayloadJSON = payload

	// Sign with fresh timestamp
	secret := []byte(item.Secret)
	if len(secret) == 0 {
		secret = []byte("aegis_default_webhook_secret")
	}
	signature := crypto.GenerateSignature(payload, secret, now)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, item.TargetURL, bytes.NewReader(payload))
	if err != nil {
		return false, fmt.Errorf("failed to build retry request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Aegis-Signature", signature)
	req.Header.Set("User-Agent", "AegisPulse-DLQ-Retry/1.0")

	resp, err := m.httpClient.Do(req)
	if err != nil {
		item.LastError = fmt.Sprintf("retry network error: %v", err)
		item.RetryCount++
		return false, err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		// Delivery successful! Remove from DLQ
		m.mu.Lock()
		delete(m.inMemory, id)
		m.mu.Unlock()
		log.Printf("[DLQ] Successfully delivered and cleared DLQ item %s (HTTP %d)", id, resp.StatusCode)
		return true, nil
	}

	body, _ := io.ReadAll(resp.Body)
	item.LastError = fmt.Sprintf("HTTP %d: %s", resp.StatusCode, string(body))
	item.RetryCount++
	return false, fmt.Errorf("endpoint returned HTTP %d", resp.StatusCode)
}
