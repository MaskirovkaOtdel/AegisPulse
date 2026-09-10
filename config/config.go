package config

import (
	"context"
	"fmt"
	"log"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/fsnotify/fsnotify"
	"gopkg.in/yaml.v3"
)

type ServerConfig struct {
	Port           int `yaml:"port"`
	AdminPort      int `yaml:"admin_port"`
	ReadTimeoutMs  int `yaml:"read_timeout_ms"`
	WriteTimeoutMs int `yaml:"write_timeout_ms"`
}

type JitterConfig struct {
	Enabled    bool `yaml:"enabled"`
	Percentage int  `yaml:"percentage"`
	MinDelayMs int  `yaml:"min_delay_ms"`
	MaxDelayMs int  `yaml:"max_delay_ms"`
}

type UpstreamConfig struct {
	TargetURL string       `yaml:"target_url"`
	TimeoutMs int          `yaml:"timeout_ms"`
	Jitter    JitterConfig `yaml:"jitter"`
}

type TierConfig struct {
	SoftLimit           int64 `yaml:"soft_limit"`
	HardLimit           int64 `yaml:"hard_limit"`
	WindowSeconds       int64 `yaml:"window_seconds"`
	ReplayWindowSeconds int64 `yaml:"replay_window_seconds"`
}

type MonetizationConfig struct {
	CostPerCredit float64 `yaml:"cost_per_credit"`
	Currency      string  `yaml:"currency"`
}

type CircuitBreakerConfig struct {
	MaxConsecutiveFailures int `yaml:"max_consecutive_failures"`
	FailureLatencyMs       int `yaml:"failure_latency_ms"`
	CooldownSeconds        int `yaml:"cooldown_seconds"`
}

type CacheConfig struct {
	TTLSeconds             int                  `yaml:"ttl_seconds"`
	CleanupIntervalSeconds int                  `yaml:"cleanup_interval_seconds"`
	CircuitBreaker         CircuitBreakerConfig `yaml:"circuit_breaker"`
}

type RedisConfig struct {
	Addr         string `yaml:"addr"`
	Password     string `yaml:"password"`
	DB           int    `yaml:"db"`
	StreamEvents string `yaml:"stream_events"`
	StreamDLQ    string `yaml:"stream_dlq"`
}

type PostgresConfig struct {
	Host     string `yaml:"host"`
	Port     int    `yaml:"port"`
	User     string `yaml:"user"`
	Password string `yaml:"password"`
	DBName   string `yaml:"dbname"`
	SSLMode  string `yaml:"sslmode"`
}

func (p PostgresConfig) DSN() string {
	if dbURL := os.Getenv("DATABASE_URL"); dbURL != "" {
		return dbURL
	}
	return fmt.Sprintf("postgres://%s:%s@%s:%d/%s?sslmode=%s",
		p.User, p.Password, p.Host, p.Port, p.DBName, p.SSLMode)
}

type SecurityConfig struct {
	MasterKey       string `yaml:"master_key"`
	HeaderAPIKey    string `yaml:"header_api_key"`
	HeaderSignature string `yaml:"header_signature"`
}

type SinkDetail struct {
	Enabled  bool   `yaml:"enabled"`
	Endpoint string `yaml:"endpoint"`
}

type WebhooksConfig struct {
	WorkerConcurrency int                   `yaml:"worker_concurrency"`
	MaxRetries        int                   `yaml:"max_retries"`
	RetryBaseDelayMs  int                   `yaml:"retry_base_delay_ms"`
	Sinks             map[string]SinkDetail `yaml:"sinks"`
}

type Config struct {
	Server       ServerConfig          `yaml:"server"`
	Upstream     UpstreamConfig        `yaml:"upstream"`
	Tiers        map[string]TierConfig `yaml:"tiers"`
	Monetization MonetizationConfig    `yaml:"monetization"`
	Cache        CacheConfig           `yaml:"cache"`
	Redis        RedisConfig           `yaml:"redis"`
	Postgres     PostgresConfig        `yaml:"postgres"`
	Security     SecurityConfig        `yaml:"security"`
	Webhooks     WebhooksConfig        `yaml:"webhooks"`
}

type ConfigManager struct {
	mu         sync.RWMutex
	filePath   string
	cfg        *Config
	listeners  []func(*Config)
	listenersM sync.Mutex
}

var (
	defaultConfig = &Config{
		Server: ServerConfig{
			Port:           8080,
			AdminPort:      8082,
			ReadTimeoutMs:  5000,
			WriteTimeoutMs: 10000,
		},
		Upstream: UpstreamConfig{
			TargetURL: "http://upstream:8081",
			TimeoutMs: 1500,
			Jitter: JitterConfig{
				Enabled:    false,
				Percentage: 30,
				MinDelayMs: 200,
				MaxDelayMs: 500,
			},
		},
		Tiers: map[string]TierConfig{
			"free": {
				SoftLimit:           20,
				HardLimit:           25,
				WindowSeconds:       60,
				ReplayWindowSeconds: 60,
			},
			"pro": {
				SoftLimit:           100,
				HardLimit:           140,
				WindowSeconds:       60,
				ReplayWindowSeconds: 300,
			},
			"enterprise": {
				SoftLimit:           1000,
				HardLimit:           1300,
				WindowSeconds:       60,
				ReplayWindowSeconds: 600,
			},
		},
		Monetization: MonetizationConfig{
			CostPerCredit: 0.005,
			Currency:      "USD",
		},
		Cache: CacheConfig{
			TTLSeconds:             300,
			CleanupIntervalSeconds: 60,
			CircuitBreaker: CircuitBreakerConfig{
				MaxConsecutiveFailures: 3,
				FailureLatencyMs:       1000,
				CooldownSeconds:        10,
			},
		},
		Redis: RedisConfig{
			Addr:         "redis:6379",
			Password:     "",
			DB:           0,
			StreamEvents: "stream:gateway:events",
			StreamDLQ:    "stream:gateway:dlq",
		},
		Postgres: PostgresConfig{
			Host:     "postgres",
			Port:     5432,
			User:     "aegis",
			Password: "aegis_secure_password",
			DBName:   "aegispulse",
			SSLMode:  "disable",
		},
		Security: SecurityConfig{
			MasterKey:       "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef",
			HeaderAPIKey:    "X-API-Key",
			HeaderSignature: "X-Aegis-Signature",
		},
		Webhooks: WebhooksConfig{
			WorkerConcurrency: 4,
			MaxRetries:        3,
			RetryBaseDelayMs:  1000,
			Sinks: map[string]SinkDetail{
				"generic": {Enabled: true, Endpoint: "http://webhook-receiver:8083/webhook/generic"},
				"discord": {Enabled: true, Endpoint: "http://webhook-receiver:8083/webhook/discord"},
				"slack":   {Enabled: true, Endpoint: "http://webhook-receiver:8083/webhook/slack"},
			},
		},
	}
)

func NewConfigManager(filePath string) (*ConfigManager, error) {
	cm := &ConfigManager{
		filePath: filePath,
		cfg:      cloneConfig(defaultConfig),
	}

	if filePath != "" {
		if err := cm.loadFromFile(); err != nil {
			log.Printf("[CONFIG] Warning: Could not load initial config from %s: %v. Using defaults with env overrides.", filePath, err)
		}
	}
	cm.applyEnvOverrides()
	return cm, nil
}

func (cm *ConfigManager) loadFromFile() error {
	data, err := os.ReadFile(cm.filePath)
	if err != nil {
		return err
	}

	var parsed Config
	if err := yaml.Unmarshal(data, &parsed); err != nil {
		return fmt.Errorf("failed to parse yaml: %w", err)
	}

	cm.mu.Lock()
	cm.cfg = &parsed
	cm.applyEnvOverridesLocked()
	current := cloneConfig(cm.cfg)
	cm.mu.Unlock()

	cm.notifyListeners(current)
	return nil
}

func (cm *ConfigManager) applyEnvOverrides() {
	cm.mu.Lock()
	defer cm.mu.Unlock()
	cm.applyEnvOverridesLocked()
}

func (cm *ConfigManager) applyEnvOverridesLocked() {
	if val := os.Getenv("PORT"); val != "" {
		if p, err := strconv.Atoi(val); err == nil {
			cm.cfg.Server.Port = p
		}
	}
	if val := os.Getenv("UPSTREAM_URL"); val != "" {
		cm.cfg.Upstream.TargetURL = val
	}
	if val := os.Getenv("REDIS_ADDR"); val != "" {
		cm.cfg.Redis.Addr = val
	}
	if val := os.Getenv("AEGIS_MASTER_KEY"); val != "" {
		cm.cfg.Security.MasterKey = val
	}
	if val := os.Getenv("POSTGRES_HOST"); val != "" {
		cm.cfg.Postgres.Host = val
	}
	if val := os.Getenv("POSTGRES_USER"); val != "" {
		cm.cfg.Postgres.User = val
	}
	if val := os.Getenv("POSTGRES_PASSWORD"); val != "" {
		cm.cfg.Postgres.Password = val
	}
	if val := os.Getenv("POSTGRES_DB"); val != "" {
		cm.cfg.Postgres.DBName = val
	}
}

func (cm *ConfigManager) Get() *Config {
	cm.mu.RLock()
	defer cm.mu.RUnlock()
	return cloneConfig(cm.cfg)
}

func (cm *ConfigManager) Update(cfg *Config) {
	cm.mu.Lock()
	cm.cfg = cloneConfig(cfg)
	current := cloneConfig(cm.cfg)
	cm.mu.Unlock()
	cm.notifyListeners(current)
}

func (cm *ConfigManager) GetTier(tierName string) (TierConfig, bool) {
	cm.mu.RLock()
	defer cm.mu.RUnlock()
	tier, ok := cm.cfg.Tiers[strings.ToLower(tierName)]
	return tier, ok
}

func (cm *ConfigManager) SetJitter(enabled bool, percentage int, minMs, maxMs int) {
	cm.mu.Lock()
	cm.cfg.Upstream.Jitter.Enabled = enabled
	if percentage >= 0 && percentage <= 100 {
		cm.cfg.Upstream.Jitter.Percentage = percentage
	}
	if minMs > 0 {
		cm.cfg.Upstream.Jitter.MinDelayMs = minMs
	}
	if maxMs >= minMs {
		cm.cfg.Upstream.Jitter.MaxDelayMs = maxMs
	}
	current := cloneConfig(cm.cfg)
	cm.mu.Unlock()
	cm.notifyListeners(current)
}

func (cm *ConfigManager) OnChange(fn func(*Config)) {
	cm.listenersM.Lock()
	defer cm.listenersM.Unlock()
	cm.listeners = append(cm.listeners, fn)
}

func (cm *ConfigManager) notifyListeners(cfg *Config) {
	cm.listenersM.Lock()
	defer cm.listenersM.Unlock()
	for _, fn := range cm.listeners {
		go fn(cfg)
	}
}

func (cm *ConfigManager) Watch(ctx context.Context) error {
	if cm.filePath == "" {
		return nil
	}

	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		return fmt.Errorf("failed to create fsnotify watcher: %w", err)
	}

	if err := watcher.Add(cm.filePath); err != nil {
		_ = watcher.Close()
		return fmt.Errorf("failed to watch %s: %w", cm.filePath, err)
	}

	go func() {
		defer watcher.Close()
		var lastReload time.Time
		for {
			select {
			case <-ctx.Done():
				return
			case event, ok := <-watcher.Events:
				if !ok {
					return
				}
				if event.Has(fsnotify.Write) || event.Has(fsnotify.Create) || event.Has(fsnotify.Rename) {
					// Re-add watch in case of atomic file replacement/rename
					_ = watcher.Add(cm.filePath)
					// Debounce multiple rapid file events
					if time.Since(lastReload) > 300*time.Millisecond {
						lastReload = time.Now()
						log.Printf("[CONFIG] Detected change in %s. Hot-reloading configuration...", cm.filePath)
						if err := cm.loadFromFile(); err != nil {
							log.Printf("[CONFIG] Error reloading config: %v", err)
						} else {
							log.Printf("[CONFIG] Hot-reload successful. New configuration active.")
						}
					}
				}
			case err, ok := <-watcher.Errors:
				if !ok {
					return
				}
				log.Printf("[CONFIG] Watcher error: %v", err)
			}
		}
	}()

	return nil
}

func cloneConfig(src *Config) *Config {
	if src == nil {
		return nil
	}
	c := *src
	c.Tiers = make(map[string]TierConfig, len(src.Tiers))
	for k, v := range src.Tiers {
		c.Tiers[k] = v
	}
	c.Webhooks.Sinks = make(map[string]SinkDetail, len(src.Webhooks.Sinks))
	for k, v := range src.Webhooks.Sinks {
		c.Webhooks.Sinks[k] = v
	}
	return &c
}
