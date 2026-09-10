package config

import (
	"context"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"
)

func TestConfigManager_DefaultsAndClone(t *testing.T) {
	cm, err := NewConfigManager("")
	if err != nil {
		t.Fatalf("unexpected error initializing config: %v", err)
	}

	cfg1 := cm.Get()
	if cfg1 == nil {
		t.Fatal("expected non-nil config")
	}

	if cfg1.Server.Port != 8080 {
		t.Fatalf("expected default port 8080, got %d", cfg1.Server.Port)
	}

	// Mutating returned clone must not mutate internal state
	cfg1.Server.Port = 9999
	cfg2 := cm.Get()
	if cfg2.Server.Port != 8080 {
		t.Fatalf("expected internal port to remain 8080, got %d", cfg2.Server.Port)
	}
}

func TestConfigManager_UpdateAndListeners(t *testing.T) {
	cm, err := NewConfigManager("")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var notified atomic.Bool
	cm.OnChange(func(c *Config) {
		if c.Server.Port == 9090 {
			notified.Store(true)
		}
	})

	cfg := cm.Get()
	cfg.Server.Port = 9090
	cm.Update(cfg)

	// Check updated value
	updated := cm.Get()
	if updated.Server.Port != 9090 {
		t.Fatalf("expected port 9090, got %d", updated.Server.Port)
	}

	// Wait for async listener notification
	for i := 0; i < 20; i++ {
		if notified.Load() {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if !notified.Load() {
		t.Fatal("expected OnChange listener to be notified")
	}
}

func TestConfigManager_GetTier(t *testing.T) {
	cm, err := NewConfigManager("")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Test case-insensitivity
	tier, ok := cm.GetTier("FREE")
	if !ok {
		t.Fatal("expected to find FREE tier")
	}
	if tier.SoftLimit != 20 || tier.HardLimit != 25 {
		t.Fatalf("unexpected limits: soft=%d, hard=%d", tier.SoftLimit, tier.HardLimit)
	}

	tierPro, ok := cm.GetTier("pro")
	if !ok {
		t.Fatal("expected to find pro tier")
	}
	if tierPro.SoftLimit != 100 || tierPro.HardLimit != 140 {
		t.Fatalf("unexpected pro limits: soft=%d, hard=%d", tierPro.SoftLimit, tierPro.HardLimit)
	}

	_, ok = cm.GetTier("nonexistent")
	if ok {
		t.Fatal("expected nonexistent tier to return false")
	}
}

func TestConfigManager_SetJitter(t *testing.T) {
	cm, err := NewConfigManager("")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	cm.SetJitter(true, 50, 200, 500)
	cfg := cm.Get()
	if !cfg.Upstream.Jitter.Enabled {
		t.Fatal("expected jitter to be enabled")
	}
	if cfg.Upstream.Jitter.Percentage != 50 {
		t.Fatalf("expected percentage 50, got %d", cfg.Upstream.Jitter.Percentage)
	}
	if cfg.Upstream.Jitter.MinDelayMs != 200 || cfg.Upstream.Jitter.MaxDelayMs != 500 {
		t.Fatalf("unexpected delays: min=%d, max=%d", cfg.Upstream.Jitter.MinDelayMs, cfg.Upstream.Jitter.MaxDelayMs)
	}
}

func TestConfigManager_WatchHotReload(t *testing.T) {
	tempDir := t.TempDir()
	configPath := filepath.Join(tempDir, "config.yaml")

	initialYAML := `
server:
  port: 8080
upstream:
  target_url: "http://upstream:8081"
`
	if err := os.WriteFile(configPath, []byte(initialYAML), 0644); err != nil {
		t.Fatalf("failed to write initial config: %v", err)
	}

	cm, err := NewConfigManager(configPath)
	if err != nil {
		t.Fatalf("NewConfigManager error: %v", err)
	}

	if cm.Get().Server.Port != 8080 {
		t.Fatalf("expected initial port 8080, got %d", cm.Get().Server.Port)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if err := cm.Watch(ctx); err != nil {
		t.Fatalf("cm.Watch error: %v", err)
	}

	reloadCh := make(chan int, 1)
	cm.OnChange(func(c *Config) {
		if c.Server.Port == 9191 {
			select {
			case reloadCh <- c.Server.Port:
			default:
			}
		}
	})

	// Give watcher a moment to initialize
	time.Sleep(50 * time.Millisecond)

	// Update config file
	updatedYAML := `
server:
  port: 9191
upstream:
  target_url: "http://upstream:9999"
`
	if err := os.WriteFile(configPath, []byte(updatedYAML), 0644); err != nil {
		t.Fatalf("failed to update config file: %v", err)
	}

	// Wait up to 2 seconds for hot reload
	select {
	case port := <-reloadCh:
		if port != 9191 {
			t.Fatalf("expected port 9191, got %d", port)
		}
	case <-time.After(2 * time.Second):
		// Check if config manager has updated even if notification race occurred
		if cm.Get().Server.Port != 9191 {
			t.Fatalf("timed out waiting for hot reload, current port is %d", cm.Get().Server.Port)
		}
	}
}
