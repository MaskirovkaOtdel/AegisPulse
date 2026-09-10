package webhook

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"aegispulse/internal/crypto"
)

func TestAdapters(t *testing.T) {
	event := &UniversalEvent{
		EventID:     "evt_100",
		EventType:   "BURST_OVERAGE",
		APIKey:      "key_test",
		Tier:        "pro",
		Timestamp:   time.Now().Unix(),
		Title:       "Burst Overage Incurred",
		Description: "API key exceeded soft limit",
		Metadata: map[string]string{
			"Overages": "5",
		},
	}

	// Generic
	genBytes, err := AdaptGeneric(event)
	if err != nil {
		t.Fatalf("AdaptGeneric error: %v", err)
	}
	var genEvt UniversalEvent
	if err := json.Unmarshal(genBytes, &genEvt); err != nil || genEvt.EventID != event.EventID {
		t.Fatalf("AdaptGeneric unmarshal mismatch")
	}

	// Discord
	discBytes, err := AdaptDiscord(event)
	if err != nil {
		t.Fatalf("AdaptDiscord error: %v", err)
	}
	var discPayload DiscordPayload
	if err := json.Unmarshal(discBytes, &discPayload); err != nil {
		t.Fatalf("AdaptDiscord unmarshal error: %v", err)
	}
	if len(discPayload.Embeds) == 0 || discPayload.Embeds[0].Color != 0xF39C12 {
		t.Fatalf("expected amber color for BURST_OVERAGE in Discord embed")
	}

	// Slack
	slackBytes, err := AdaptSlack(event)
	if err != nil {
		t.Fatalf("AdaptSlack error: %v", err)
	}
	var slackPayload SlackPayload
	if err := json.Unmarshal(slackBytes, &slackPayload); err != nil {
		t.Fatalf("AdaptSlack unmarshal error: %v", err)
	}
	if len(slackPayload.Blocks) < 3 {
		t.Fatalf("expected at least 3 blocks in Slack payload")
	}
}

func TestDLQ_ReSignAndRetry(t *testing.T) {
	received := false
	var receivedSig string

	tsServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		received = true
		receivedSig = r.Header.Get("X-Aegis-Signature")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"status":"received"}`))
	}))
	defer tsServer.Close()

	dlq := NewDLQManager(nil, "stream:gateway:dlq")

	oldTimestamp := time.Now().Unix() - 1000 // Very old timestamp
	event := UniversalEvent{
		EventID:     "evt_failed_01",
		EventType:   "BURST_OVERAGE",
		APIKey:      "test_key",
		Tier:        "enterprise",
		Timestamp:   oldTimestamp,
		Title:       "Test DLQ",
		Description: "Failed delivery",
	}

	initialPayload, _ := AdaptGeneric(&event)
	item := &DLQItem{
		ID:          "dlq_item_01",
		Event:       event,
		SinkType:    "generic",
		TargetURL:   tsServer.URL,
		Secret:      "super_secret_retry_key",
		RetryCount:  3,
		LastError:   "initial timeout",
		FailedAt:    time.Now(),
		PayloadJSON: initialPayload,
	}

	err := dlq.Enqueue(context.Background(), item)
	if err != nil {
		t.Fatalf("Enqueue error: %v", err)
	}

	if dlq.GetPendingCount(context.Background()) != 1 {
		t.Fatalf("expected 1 pending item in DLQ")
	}

	// Perform ReSignAndRetry
	ok, err := dlq.ReSignAndRetry(context.Background(), item.ID)
	if err != nil || !ok {
		t.Fatalf("ReSignAndRetry failed: %v", err)
	}

	if !received {
		t.Fatalf("expected server to receive retried webhook")
	}

	// Verify the signature on the server has a FRESH timestamp (within last 5 seconds)
	if receivedSig == "" {
		t.Fatalf("expected X-Aegis-Signature header")
	}

	// Verify using crypto helper with complete validation
	valid, err := crypto.VerifySignature(item.PayloadJSON, receivedSig, []byte(item.Secret), 10, time.Now().Unix())
	if !valid || err != nil {
		t.Fatalf("re-signed signature verification failed: valid=%v, err=%v", valid, err)
	}

	// Pending count should now be 0
	if dlq.GetPendingCount(context.Background()) != 0 {
		t.Fatalf("expected 0 pending items after successful retry")
	}
}

func TestDLQ_ListAndLoad(t *testing.T) {
	dlq := NewDLQManager(nil, "stream:gateway:dlq")

	item1 := &DLQItem{
		ID:        "item_1",
		TargetURL: "http://example.com/wh1",
	}
	item2 := &DLQItem{
		ID:        "item_2",
		TargetURL: "http://example.com/wh2",
	}

	_ = dlq.Enqueue(context.Background(), item1)
	_ = dlq.Enqueue(context.Background(), item2)

	list := dlq.List(context.Background())
	if len(list) != 2 {
		t.Fatalf("expected 2 items, got %d", len(list))
	}

	// LoadFromRedis with nil client returns nil without panic
	if err := dlq.LoadFromRedis(context.Background()); err != nil {
		t.Fatalf("expected nil error for LoadFromRedis with nil client, got %v", err)
	}
}
