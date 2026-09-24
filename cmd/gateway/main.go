package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"aegispulse/config"
	"aegispulse/internal/cache"
	"aegispulse/internal/crypto"
	"aegispulse/internal/metrics"
	"aegispulse/internal/proxy"
	"aegispulse/internal/ratelimit"
	"aegispulse/internal/storage"
	"aegispulse/internal/tui"
	"aegispulse/internal/webhook"
	tea "github.com/charmbracelet/bubbletea"
)

func main() {
	configPath := flag.String("config", "config/config.yaml", "Path to config.yaml file")
	headlessFlag := flag.Bool("headless", false, "Force headless mode without interactive TUI")
	flag.Parse()

	log.Println("===================================================================")
	log.Println("     🛡️  AEGISPULSE API GATEWAY & ZERO-TRUST SECURITY SYSTEM  🛡️    ")
	log.Println("===================================================================")

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// 1. Load Configuration
	cfgManager, err := config.NewConfigManager(*configPath)
	if err != nil {
		log.Fatalf("[FATAL] Failed to initialize config manager: %v", err)
	}
	if err := cfgManager.Watch(ctx); err != nil {
		log.Printf("[CONFIG] Note: Config file watch disabled: %v", err)
	}
	cfg := cfgManager.Get()
	log.Printf("[CONFIG] Gateway Port: %d | Upstream Target: %s", cfg.Server.Port, cfg.Upstream.TargetURL)

	// 2. Initialize Master Envelope Cipher (AES-256-GCM)
	envelopeCipher, err := crypto.NewEnvelopeCipher(cfg.Security.MasterKey)
	if err != nil {
		log.Fatalf("[FATAL] Failed to initialize envelope cipher: %v", err)
	}
	log.Println("[SECURITY] AES-256-GCM Envelope Encryption initialized.")

	// 3. Initialize Storage: Redis
	redisClient, err := storage.NewRedisClient(cfg.Redis.Addr, cfg.Redis.Password, cfg.Redis.DB)
	if err != nil {
		log.Printf("[STORAGE] Redis connection warning: %v", err)
	}
	defer redisClient.Close()

	// 4. Initialize Storage: PostgreSQL
	repo, err := storage.NewPostgresRepo(ctx, cfg.Postgres.DSN(), envelopeCipher, redisClient)
	if err != nil {
		log.Fatalf("[FATAL] Failed to initialize database repository: %v", err)
	}
	defer repo.Close()

	// 5. Initialize Prometheus Metrics
	promMetrics := metrics.NewMetrics(nil)

	// Background reporter for Redis memory consumption
	go func() {
		ticker := time.NewTicker(3 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				if redisClient != nil && redisClient.Client != nil {
					if mem, err := redisClient.GetMemoryUsage(ctx); err == nil && mem > 0 {
						promMetrics.SetRedisMemory(float64(mem))
					}
				}
			}
		}
	}()

	// 6. Initialize L1 Smart Cache and Circuit Breaker
	cleanupInterval := time.Duration(cfg.Cache.CleanupIntervalSeconds) * time.Second
	smartCache := cache.NewSmartCache(cleanupInterval)
	defer smartCache.Close()

	cbCooldown := time.Duration(cfg.Cache.CircuitBreaker.CooldownSeconds) * time.Second
	cbLatency := time.Duration(cfg.Cache.CircuitBreaker.FailureLatencyMs) * time.Millisecond
	circuitBreaker := cache.NewCircuitBreaker(
		cfg.Cache.CircuitBreaker.MaxConsecutiveFailures,
		cbLatency,
		cbCooldown,
	)

	// 7. Initialize Rate Limiter (Redis Lua + In-Memory Fallback)
	var rClient = redisClient.Client
	limiter := ratelimit.NewLimiter(rClient, cfg.Redis.StreamEvents)

	// 8. Initialize Universal Webhook Engine & DLQ
	dlqManager := webhook.NewDLQManager(rClient, cfg.Redis.StreamDLQ)
	_ = dlqManager.LoadFromRedis(ctx)
	webhookEngine := webhook.NewEngine(cfgManager, rClient, dlqManager, promMetrics)
	webhookEngine.Start(ctx)
	defer webhookEngine.Stop()

	// 9. Initialize Reverse Proxy Core
	gatewayProxy, err := proxy.NewGatewayProxy(
		cfgManager,
		repo,
		limiter,
		smartCache,
		circuitBreaker,
		webhookEngine,
		promMetrics,
		redisClient,
	)
	if err != nil {
		log.Fatalf("[FATAL] Failed to create gateway reverse proxy: %v", err)
	}

	// 10. Start Gateway HTTP Server
	serverAddr := fmt.Sprintf(":%d", cfg.Server.Port)
	server := &http.Server{
		Addr:           serverAddr,
		Handler:        gatewayProxy,
		ReadTimeout:    time.Duration(cfg.Server.ReadTimeoutMs) * time.Millisecond,
		WriteTimeout:   time.Duration(cfg.Server.WriteTimeoutMs) * time.Millisecond,
		MaxHeaderBytes: 1 << 20,
	}

	go func() {
		log.Printf("[HTTP] Gateway listening on http://0.0.0.0:%d", cfg.Server.Port)
		log.Printf("[HTTP] Interactive Demo Playground: http://0.0.0.0:%d/demo", cfg.Server.Port)
		log.Printf("[HTTP] OpenAPI 3.0 Documentation: http://0.0.0.0:%d/docs", cfg.Server.Port)
		log.Printf("[HTTP] Prometheus metrics available at http://0.0.0.0:%d/metrics", cfg.Server.Port)
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("[FATAL] Gateway HTTP server failed: %v", err)
		}
	}()

	// 11. Smart TTY Detection & TUI Launch
	chaosEngine := tui.NewChaosEngine(fmt.Sprintf("http://localhost:%d", cfg.Server.Port))

	if !*headlessFlag && tui.IsTTY() {
		log.Println("[TUI] Interactive TTY detected. Launching AegisPulse Cyber-Deck Cockpit...")
		// Allow 200ms for HTTP server to settle
		time.Sleep(200 * time.Millisecond)

		model := tui.NewModel(cfgManager, gatewayProxy, promMetrics, circuitBreaker, limiter, repo, webhookEngine, chaosEngine, redisClient)
		p := tea.NewProgram(model, tea.WithAltScreen())
		if _, err := p.Run(); err != nil {
			log.Printf("[TUI] Error running TUI: %v", err)
		}
		// Graceful exit after TUI quit
		cancel()
	} else {
		// Non-TTY / Headless mode (Docker, CI, piped output)
		go tui.RunHeadless(ctx, promMetrics, circuitBreaker, cfgManager)

		sigCh := make(chan os.Signal, 1)
		signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)
		<-sigCh
		log.Println("[SHUTDOWN] Signal received. Initiating graceful shutdown...")
		cancel()
	}

	// Graceful shutdown with 5s timeout
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer shutdownCancel()
	_ = server.Shutdown(shutdownCtx)
	log.Println("[SHUTDOWN] AegisPulse Gateway stopped cleanly.")
}
