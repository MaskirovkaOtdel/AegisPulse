package tui

import (
	"context"
	"fmt"
	"log"
	"os"
	"strings"
	"sync"
	"time"

	"aegispulse/config"
	"aegispulse/internal/cache"
	"aegispulse/internal/crypto"
	"aegispulse/internal/metrics"
	"aegispulse/internal/proxy"
	"aegispulse/internal/ratelimit"
	"aegispulse/internal/storage"
	"aegispulse/internal/webhook"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"golang.org/x/term"
)

type TickMsg time.Time
type LogMsg string

type Model struct {
	cfgManager  *config.ConfigManager
	proxy       *proxy.GatewayProxy
	metrics     *metrics.Metrics
	cb          *cache.CircuitBreaker
	limiter     ratelimit.Limiter
	repo        storage.Repository
	whEngine    *webhook.Engine
	chaos       *ChaosEngine
	redisClient *storage.RedisClient

	themeIdx  int
	styles    Styles
	width     int
	height    int
	modal     *ModalState
	logs      []string
	logsMu    *sync.Mutex
	revenue   float64
	startTime time.Time
}

func NewModel(
	cfgManager *config.ConfigManager,
	proxy *proxy.GatewayProxy,
	m *metrics.Metrics,
	cb *cache.CircuitBreaker,
	limiter ratelimit.Limiter,
	repo storage.Repository,
	whEngine *webhook.Engine,
	chaos *ChaosEngine,
	redisClient *storage.RedisClient,
) Model {
	initialTheme := DetectInitialTheme()
	return Model{
		cfgManager:  cfgManager,
		proxy:       proxy,
		metrics:     m,
		cb:          cb,
		limiter:     limiter,
		repo:        repo,
		whEngine:    whEngine,
		chaos:       chaos,
		redisClient: redisClient,
		themeIdx:    initialTheme,
		styles:      MakeStyles(Themes[initialTheme]),
		modal:       NewModalState(),
		logsMu:      &sync.Mutex{},
		logs: []string{
			"🛡️ AegisPulse Cyber-Deck Cockpit initialized.",
			"⚡ Dual-Threshold Sliding Window active.",
			"🔐 AES-256-GCM Envelope Encryption armed.",
		},
		startTime: time.Now(),
	}
}

func (m Model) Init() tea.Cmd {
	return tea.Batch(
		tea.EnterAltScreen,
		tickCmd(),
	)
}

func tickCmd() tea.Cmd {
	return tea.Tick(300*time.Millisecond, func(t time.Time) tea.Msg {
		return TickMsg(t)
	})
}

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		return m, nil

	case TickMsg:
		snap := m.metrics.GetSnapshot()
		cfg := m.cfgManager.Get()
		m.revenue = float64(snap.BurstCount) * cfg.Monetization.CostPerCredit
		m.metrics.SetRevenue(m.revenue)
		return m, tickCmd()

	case LogMsg:
		m.addLog(string(msg))
		return m, nil

	case tea.KeyMsg:
		// Modal interaction takes precedence
		if m.modal.Active {
			switch msg.String() {
			case "y", "Y", "enter":
				m.executeModalAction()
				m.modal.Close()
				return m, nil
			case "n", "N", "esc":
				m.addLog("🚫 Panic protocol modal canceled by operator.")
				m.modal.Close()
				return m, nil
			}
			return m, nil
		}

		switch msg.String() {
		case "q", "ctrl+c":
			return m, tea.Quit

		case "t", "T":
			m.themeIdx = (m.themeIdx + 1) % len(Themes)
			m.styles = MakeStyles(Themes[m.themeIdx])
			m.addLog(fmt.Sprintf("🎨 Theme switched to [%s]", Themes[m.themeIdx].Name))

		case " ":
			// Unleash traffic burst
			m.chaos.UnleashBurst(100, func(s string) {
				m.addLog(s)
			})

		case "l", "L":
			// Toggle upstream simulated jitter
			active := m.proxy.ToggleJitter()
			stateStr := "DISABLED"
			if active {
				stateStr = "ENABLED (200ms - 500ms, 30% traffic)"
			}
			m.addLog(fmt.Sprintf("⚡ Upstream simulated jitter toggled: %s", stateStr))

		case "a", "A":
			// Inject simulated replay attack
			m.chaos.InjectReplayAttack(func(s string) {
				m.addLog(s)
			})

		case "ctrl+k":
			// Key Kill-Switch panic modal
			m.modal.Open(ModalKillSwitch, "aegis_live_compromised_key_9999")

		case "ctrl+b":
			// Global Burst Freeze panic modal
			m.modal.Open(ModalBurstFreeze, "")

		case "ctrl+u":
			// Emergency Upstream Cutoff panic modal
			m.modal.Open(ModalEmergencyCutoff, "")
		}
	}

	return m, nil
}

func (m *Model) executeModalAction() {
	ctx := context.Background()
	nowUnix := time.Now().Unix()

	switch m.modal.Type {
	case ModalKillSwitch:
		target := m.modal.TargetKey
		_ = m.repo.FreezeAPIKey(ctx, target)
		if m.redisClient != nil {
			_ = m.redisClient.FreezeKey(ctx, target)
			_ = m.redisClient.FreezeKey(ctx, crypto.HashKey(target))
		}
		m.addLog(fmt.Sprintf("🚨 KILL-SWITCH EXECUTED: Compromised key [%s] frozen across Redis & Postgres!", target))

		// Create signed HMAC audit event
		auditPayload := fmt.Sprintf(`{"action":"KILL_SWITCH","target_key":"%s","operator":"tui"}`, target)
		sig := crypto.GenerateSignature([]byte(auditPayload), []byte("aegis_master_key"), nowUnix)
		_ = m.repo.CreateAuditLog(ctx, "KILL_SWITCH_ACTIVATED", auditPayload, sig)

		// Dispatch signed HMAC audit event to #security-incidents via Universal Webhook
		m.whEngine.Publish(&webhook.UniversalEvent{
			EventID:     fmt.Sprintf("kill_%d", time.Now().UnixNano()),
			EventType:   "KILL_SWITCH",
			APIKey:      target,
			Tier:        "SYSTEM",
			Timestamp:   nowUnix,
			Title:       "EMERGENCY KILL-SWITCH CONFIRMED",
			Description: fmt.Sprintf("Operator confirmed panic kill-switch for key %s. Key neutralized platform-wide.", target),
			Channel:     "#security-incidents",
			Signature:   sig,
		})

	case ModalBurstFreeze:
		currentlyFrozen := m.limiter.IsGlobalBurstFrozen()
		newFrozen := !currentlyFrozen
		m.limiter.SetGlobalBurstFreeze(newFrozen)
		status := "ACTIVATED (Burst quota locked, 429 enforced)"
		if !newFrozen {
			status = "DEACTIVATED (Normal burst quotas restored)"
		}
		m.addLog(fmt.Sprintf("⚠️ GLOBAL BURST FREEZE %s", status))

		auditPayload := fmt.Sprintf(`{"action":"BURST_FREEZE","frozen":%v,"operator":"tui"}`, newFrozen)
		sig := crypto.GenerateSignature([]byte(auditPayload), []byte("aegis_master_key"), nowUnix)
		_ = m.repo.CreateAuditLog(ctx, "GLOBAL_BURST_FREEZE", auditPayload, sig)

		m.whEngine.Publish(&webhook.UniversalEvent{
			EventID:     fmt.Sprintf("freeze_%d", time.Now().UnixNano()),
			EventType:   "SECURITY_INCIDENT",
			APIKey:      "SYSTEM",
			Tier:        "GLOBAL",
			Timestamp:   nowUnix,
			Title:       "GLOBAL BURST FREEZE TOGGLED",
			Description: fmt.Sprintf("Global burst freeze state set to %v", newFrozen),
			Channel:     "#security-incidents",
			Signature:   sig,
		})

	case ModalEmergencyCutoff:
		currentlyCutoff := m.cb.IsEmergencyCutoff()
		newCutoff := !currentlyCutoff
		m.cb.SetEmergencyCutoff(newCutoff)
		status := "ACTIVATED: 100% traffic forced to L1 Fallback Cache"
		if !newCutoff {
			status = "DEACTIVATED: Live upstream routing restored"
		}
		m.addLog(fmt.Sprintf("🛑 EMERGENCY UPSTREAM CUTOFF %s", status))

		auditPayload := fmt.Sprintf(`{"action":"EMERGENCY_UPSTREAM_CUTOFF","cutoff":%v,"operator":"tui"}`, newCutoff)
		sig := crypto.GenerateSignature([]byte(auditPayload), []byte("aegis_master_key"), nowUnix)
		_ = m.repo.CreateAuditLog(ctx, "EMERGENCY_UPSTREAM_CUTOFF", auditPayload, sig)

		m.whEngine.Publish(&webhook.UniversalEvent{
			EventID:     fmt.Sprintf("cutoff_%d", time.Now().UnixNano()),
			EventType:   "SECURITY_INCIDENT",
			APIKey:      "SYSTEM",
			Tier:        "GATEWAY",
			Timestamp:   nowUnix,
			Title:       "EMERGENCY UPSTREAM CUTOFF TOGGLED",
			Description: fmt.Sprintf("Emergency cutoff state set to %v", newCutoff),
			Channel:     "#security-incidents",
			Signature:   sig,
		})
	}
}

func (m *Model) addLog(msg string) {
	m.logsMu.Lock()
	defer m.logsMu.Unlock()
	timestamp := time.Now().Format("15:04:05")
	m.logs = append(m.logs, fmt.Sprintf("[%s] %s", timestamp, msg))
	if len(m.logs) > 8 {
		m.logs = m.logs[1:]
	}
}

func (m Model) View() string {
	w := m.width
	if w <= 0 {
		w = 100
	}

	snap := m.metrics.GetSnapshot()
	cbState := m.cb.GetState()

	// If a modal is active, overlay it
	if m.modal.Active {
		return RenderModal(m.modal, m.styles, w)
	}

	// 1. Header Banner
	bannerText := fmt.Sprintf(" AEGISPULSE CYBER-DECK COCKPIT v1.0 | UPTIME: %s | THEME: %s ",
		time.Since(m.startTime).Round(time.Second), Themes[m.themeIdx].Name)
	header := m.styles.Header.Render(bannerText)

	// 2. Metrics & Telemetry Panel
	totalReqs := snap.NormalCount + snap.BurstCount + snap.BlockedCount
	if totalReqs == 0 {
		totalReqs = 1
	}

	normalBar := RenderHorizontalBar(snap.NormalCount, totalReqs, 20, m.styles.Success, m.styles.Dim)
	burstBar := RenderHorizontalBar(snap.BurstCount, totalReqs, 20, m.styles.Warning, m.styles.Dim)
	blockedBar := RenderHorizontalBar(snap.BlockedCount, totalReqs, 20, m.styles.Error, m.styles.Dim)

	cbBadge := m.styles.Success.Render("[CLOSED - HEALTHY]")
	if cbState == cache.CircuitHalfOpen {
		cbBadge = m.styles.Warning.Render("[HALF-OPEN - PROBING]")
	} else if cbState == cache.CircuitOpen {
		cbBadge = m.styles.Error.Render("[OPEN - TRAFFIC DIVERTED]")
	}
	if m.cb.IsEmergencyCutoff() {
		cbBadge = m.styles.Error.Render("[EMERGENCY CUTOFF ACTIVE]")
	}

	jitterBadge := m.styles.Dim.Render("[OFF]")
	if m.proxy.IsJitterActive() {
		jitterBadge = m.styles.Warning.Render("[ACTIVE: 200-500ms]")
	}

	burstFreezeBadge := m.styles.Dim.Render("[OFF]")
	if m.limiter.IsGlobalBurstFrozen() {
		burstFreezeBadge = m.styles.Error.Render("[FROZEN]")
	}

	telemetryContent := fmt.Sprintf(
		"TRAFFIC BREAKDOWN:\n"+
			"  NORMAL : %5d  %s\n"+
			"  BURST  : %5d  %s\n"+
			"  BLOCKED: %5d  %s\n\n"+
			"CIRCUIT BREAKER  : %s\n"+
			"UPSTREAM JITTER  : %s\n"+
			"BURST FREEZE     : %s\n"+
			"REPLAY BLOCKS    : %s\n"+
			"FALLBACK CACHED  : %d requests\n"+
			"DLQ PENDING      : %d webhooks\n"+
			"BURST REVENUE    : %s",
		snap.NormalCount, normalBar,
		snap.BurstCount, burstBar,
		snap.BlockedCount, blockedBar,
		cbBadge,
		jitterBadge,
		burstFreezeBadge,
		m.styles.Warning.Render(fmt.Sprintf("%d blocked", snap.ReplayBlocked)),
		snap.CacheFallbacks,
		snap.DLQPending,
		m.styles.Success.Render(fmt.Sprintf("$%.4f USD", m.revenue)),
	)
	telemetryBox := m.styles.Box.Copy().Width(w/2 - 3).Render(telemetryContent)

	// 3. Sparkline & Latency Panel
	sparkline := RenderSparkline(snap.RecentSamples, 30, m.styles.Sparkline)
	sparklineContent := fmt.Sprintf(
		"REAL-TIME LATENCY SPECTRUM:\n\n"+
			"  p95 LATENCY: %.2f ms\n"+
			"  p99 LATENCY: %.2f ms\n\n"+
			"  LATENCY TREND (Recent 30 reqs):\n"+
			"  %s\n\n"+
			"CHAOS & DRILL CONTROLS:\n"+
			"  [Space]  Unleash sudden traffic burst (50-250 reqs)\n"+
			"  [L]      Toggle upstream latency jitter (200-500ms)\n"+
			"  [A]      Inject expired timestamp replay attack\n"+
			"  [T]      Cycle Cyberpunk / Dracula / Mocha theme\n"+
			"  [q]      Quit dashboard",
		snap.P95LatencyMs,
		snap.P99LatencyMs,
		sparkline,
	)
	sparklineBox := m.styles.Box.Copy().Width(w/2 - 3).Render(sparklineContent)

	mainRow := lipgloss.JoinHorizontal(lipgloss.Top, telemetryBox, sparklineBox)

	// 4. Activity & Security Incident Log
	m.logsMu.Lock()
	logLines := strings.Join(m.logs, "\n")
	m.logsMu.Unlock()
	logBox := m.styles.Box.Copy().Width(w - 4).Render("SECURITY & GATEWAY ACTIVITY STREAM:\n" + logLines)

	// 5. Two-step panic hotkey status bar
	panicBar := m.styles.AlertBox.Copy().Width(w - 4).Render(
		"🚨 TWO-STEP PANIC HOTKEYS: [Ctrl+K] Key Kill-Switch | [Ctrl+B] Global Burst Freeze | [Ctrl+U] Emergency Upstream Cutoff",
	)

	return lipgloss.JoinVertical(lipgloss.Left, header, mainRow, logBox, panicBar)
}

// IsTTY determines whether standard output is connected to an interactive terminal
func IsTTY() bool {
	return term.IsTerminal(int(os.Stdout.Fd()))
}

// RunHeadless runs AegisPulse in headless mode for non-interactive / Docker / CI environments
func RunHeadless(ctx context.Context, m *metrics.Metrics, cb *cache.CircuitBreaker, cfgManager *config.ConfigManager) {
	log.Printf("[AEGISPULSE] Running in HEADLESS mode (Non-interactive terminal detected).")
	ticker := time.NewTicker(3 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			snap := m.GetSnapshot()
			cfg := cfgManager.Get()
			rev := float64(snap.BurstCount) * cfg.Monetization.CostPerCredit
			log.Printf("[TELEMETRY] Normal=%d Burst=%d Blocked=%d ReplayBlocked=%d FallbackServed=%d DLQ=%d Breaker=%s Revenue=$%.4f",
				snap.NormalCount, snap.BurstCount, snap.BlockedCount, snap.ReplayBlocked,
				snap.CacheFallbacks, snap.DLQPending, cb.GetState(), rev)
		}
	}
}
