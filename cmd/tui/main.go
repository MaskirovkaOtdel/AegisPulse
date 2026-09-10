package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"aegispulse/internal/tui"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

type standaloneModel struct {
	gatewayURL string
	httpClient *http.Client
	themeIdx   int
	styles     tui.Styles
	width      int
	height     int
	modal      *tui.ModalState
	logs       []string
	logsMu     *sync.Mutex
	startTime  time.Time

	// Remote metrics state
	normalCount    int64
	burstCount     int64
	blockedCount   int64
	replayBlocked  int64
	cacheFallbacks int64
	dlqPending     int64
	p95Latency     float64
	p99Latency     float64
	revenue        float64
	cbState        string
	jitterActive   bool
	burstFrozen    bool
	recentSamples  []float64
}

type remoteTickMsg time.Time

func main() {
	gatewayURL := flag.String("url", "http://localhost:8080", "AegisPulse Gateway URL")
	flag.Parse()

	initTheme := tui.DetectInitialTheme()
	m := standaloneModel{
		gatewayURL: *gatewayURL,
		httpClient: &http.Client{Timeout: 3 * time.Second},
		themeIdx:   initTheme,
		styles:     tui.MakeStyles(tui.Themes[initTheme]),
		modal:      tui.NewModalState(),
		startTime:  time.Now(),
		cbState:    "CLOSED",
		logsMu:     &sync.Mutex{},
		logs: []string{
			"🛰️ AegisPulse Standalone TUI Cockpit connected to " + *gatewayURL,
			"⚡ Real-time telemetry poller active.",
		},
	}

	p := tea.NewProgram(m, tea.WithAltScreen())
	if _, err := p.Run(); err != nil {
		log.Fatalf("Error running TUI: %v", err)
	}
}

func (m standaloneModel) Init() tea.Cmd {
	return tea.Batch(
		tea.EnterAltScreen,
		tickRemoteCmd(),
	)
}

func tickRemoteCmd() tea.Cmd {
	return tea.Tick(400*time.Millisecond, func(t time.Time) tea.Msg {
		return remoteTickMsg(t)
	})
}

func (m standaloneModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		return m, nil

	case remoteTickMsg:
		m.pollGatewayMetrics()
		return m, tickRemoteCmd()

	case tea.KeyMsg:
		if m.modal.Active {
			switch msg.String() {
			case "y", "Y", "enter":
				m.executeRemoteModal()
				m.modal.Close()
				return m, nil
			case "n", "N", "esc":
				m.addLog("🚫 Panic modal canceled by operator.")
				m.modal.Close()
				return m, nil
			}
			return m, nil
		}

		switch msg.String() {
		case "q", "ctrl+c":
			return m, tea.Quit
		case "t", "T":
			m.themeIdx = (m.themeIdx + 1) % len(tui.Themes)
			m.styles = tui.MakeStyles(tui.Themes[m.themeIdx])
			m.addLog(fmt.Sprintf("🎨 Theme switched to [%s]", tui.Themes[m.themeIdx].Name))
		case " ":
			m.triggerRemoteBurst()
		case "l", "L":
			m.toggleRemoteJitter()
		case "a", "A":
			m.triggerRemoteReplayAttack()
		case "ctrl+k":
			m.modal.Open(tui.ModalKillSwitch, "aegis_live_compromised_key_9999")
		case "ctrl+b":
			m.modal.Open(tui.ModalBurstFreeze, "")
		case "ctrl+u":
			m.modal.Open(tui.ModalEmergencyCutoff, "")
		}
	}
	return m, nil
}

func (m *standaloneModel) pollGatewayMetrics() {
	start := time.Now()
	resp, err := m.httpClient.Get(m.gatewayURL + "/metrics")
	if err != nil {
		return
	}
	defer resp.Body.Close()

	rttMs := float64(time.Since(start).Microseconds()) / 1000.0
	if rttMs > 0 {
		m.recentSamples = append(m.recentSamples, rttMs)
		if len(m.recentSamples) > 30 {
			m.recentSamples = m.recentSamples[1:]
		}
		m.p95Latency, m.p99Latency = calcPercentiles(m.recentSamples)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return
	}

	var newNormal, newBurst, newBlocked, newReplay int64
	lines := strings.Split(string(body), "\n")
	for _, l := range lines {
		if strings.HasPrefix(l, "aegispulse_requests_total{") {
			if strings.Contains(l, `status="NORMAL"`) {
				newNormal += parseLastInt(l)
			} else if strings.Contains(l, `status="BURST"`) {
				newBurst += parseLastInt(l)
			} else if strings.Contains(l, `status="BLOCKED"`) || strings.Contains(l, `status="BURST_FROZEN"`) {
				newBlocked += parseLastInt(l)
			}
		} else if strings.HasPrefix(l, "aegispulse_replay_attacks_blocked_total{") {
			newReplay += parseLastInt(l)
		} else if strings.HasPrefix(l, "aegispulse_cache_fallback_total ") {
			m.cacheFallbacks = parseLastInt(l)
		} else if strings.HasPrefix(l, "aegispulse_dlq_pending_total ") {
			m.dlqPending = parseLastInt(l)
		} else if strings.HasPrefix(l, "aegispulse_monetized_burst_revenue_dollars ") {
			m.revenue = parseLastFloat(l)
		}
	}
	m.normalCount = newNormal
	m.burstCount = newBurst
	m.blockedCount = newBlocked
	m.replayBlocked = newReplay

	// Query panic status for dynamic resilience state
	statusResp, err := m.httpClient.Get(m.gatewayURL + "/api/v1/panic/status")
	if err == nil {
		var st struct {
			CircuitBreaker  string `json:"circuit_breaker"`
			EmergencyCutoff bool   `json:"emergency_cutoff"`
			BurstFrozen     bool   `json:"burst_frozen"`
			JitterActive    bool   `json:"jitter_active"`
		}
		if json.NewDecoder(statusResp.Body).Decode(&st) == nil {
			m.cbState = st.CircuitBreaker
			if st.EmergencyCutoff {
				m.cbState = "EMERGENCY CUTOFF"
			}
			m.burstFrozen = st.BurstFrozen
			m.jitterActive = st.JitterActive
		}
		_ = statusResp.Body.Close()
	}
}

func calcPercentiles(samples []float64) (float64, float64) {
	n := len(samples)
	if n == 0 {
		return 0, 0
	}
	sorted := append([]float64(nil), samples...)
	sort.Float64s(sorted)
	p95Idx := int(float64(n) * 0.95)
	p99Idx := int(float64(n) * 0.99)
	if p95Idx >= n {
		p95Idx = n - 1
	}
	if p99Idx >= n {
		p99Idx = n - 1
	}
	return sorted[p95Idx], sorted[p99Idx]
}

func (m *standaloneModel) triggerRemoteBurst() {
	m.addLog("🚀 Unleashing remote traffic burst (100 requests)...")
	go func() {
		keys := []string{"aegis_live_free_key_1001", "aegis_live_pro_key_2002"}
		for i := 0; i < 100; i++ {
			go func(idx int) {
				req, _ := http.NewRequest(http.MethodGet, m.gatewayURL+"/api/resource", nil)
				req.Header.Set("X-API-Key", keys[idx%len(keys)])
				resp, err := m.httpClient.Do(req)
				if err == nil {
					_ = resp.Body.Close()
				}
			}(i)
		}
	}()
}

func (m *standaloneModel) toggleRemoteJitter() {
	resp, err := m.httpClient.Post(m.gatewayURL+"/api/v1/panic/jitter", "application/json", strings.NewReader(`{}`))
	if err == nil {
		var res struct {
			JitterActive bool `json:"jitter_active"`
		}
		_ = json.NewDecoder(resp.Body).Decode(&res)
		_ = resp.Body.Close()
		m.jitterActive = res.JitterActive
	} else {
		m.jitterActive = !m.jitterActive
	}
	state := "DISABLED"
	if m.jitterActive {
		state = "ENABLED (200-500ms)"
	}
	m.addLog(fmt.Sprintf("⚡ Upstream jitter toggled on gateway: %s", state))
}

func (m *standaloneModel) triggerRemoteReplayAttack() {
	m.addLog("⚔️ Injecting simulated replay attack with expired timestamp...")
	go func() {
		req, _ := http.NewRequest(http.MethodPost, m.gatewayURL+"/api/transfer", strings.NewReader(`{"exploit":true}`))
		req.Header.Set("X-API-Key", "aegis_live_free_key_1001")
		req.Header.Set("X-Aegis-Signature", "t=100000,v1=deadbeef1234")
		req.Header.Set("Content-Type", "application/json")
		resp, err := m.httpClient.Do(req)
		if err == nil {
			if resp.StatusCode == http.StatusUnauthorized {
				m.addLog("🛡️ Replay attack intercepted & blocked (HTTP 401)!")
			}
			_ = resp.Body.Close()
		}
	}()
}

func (m *standaloneModel) executeRemoteModal() {
	switch m.modal.Type {
	case tui.ModalKillSwitch:
		target := m.modal.TargetKey
		body, _ := json.Marshal(map[string]string{"api_key": target})
		resp, err := m.httpClient.Post(m.gatewayURL+"/api/v1/panic/kill", "application/json", bytes.NewReader(body))
		if err == nil {
			m.addLog(fmt.Sprintf("🚨 KILL-SWITCH CONFIRMED: Key [%s] frozen!", target))
			_ = resp.Body.Close()
		}
	case tui.ModalBurstFreeze:
		m.burstFrozen = !m.burstFrozen
		body, _ := json.Marshal(map[string]bool{"frozen": m.burstFrozen})
		resp, err := m.httpClient.Post(m.gatewayURL+"/api/v1/panic/burst-freeze", "application/json", bytes.NewReader(body))
		if err == nil {
			m.addLog(fmt.Sprintf("⚠️ GLOBAL BURST FREEZE toggled: %v", m.burstFrozen))
			_ = resp.Body.Close()
		}
	case tui.ModalEmergencyCutoff:
		cutoff := m.cbState != "OPEN"
		body, _ := json.Marshal(map[string]bool{"cutoff": cutoff})
		resp, err := m.httpClient.Post(m.gatewayURL+"/api/v1/panic/emergency-cutoff", "application/json", bytes.NewReader(body))
		if err == nil {
			m.addLog(fmt.Sprintf("🛑 EMERGENCY UPSTREAM CUTOFF toggled: %v", cutoff))
			if cutoff {
				m.cbState = "OPEN"
			} else {
				m.cbState = "CLOSED"
			}
			_ = resp.Body.Close()
		}
	}
}

func (m *standaloneModel) addLog(msg string) {
	m.logsMu.Lock()
	defer m.logsMu.Unlock()
	timestamp := time.Now().Format("15:04:05")
	m.logs = append(m.logs, fmt.Sprintf("[%s] %s", timestamp, msg))
	if len(m.logs) > 8 {
		m.logs = m.logs[1:]
	}
}

func (m standaloneModel) View() string {
	w := m.width
	if w <= 0 {
		w = 100
	}

	if m.modal.Active {
		return tui.RenderModal(m.modal, m.styles, w)
	}

	header := m.styles.Header.Render(fmt.Sprintf(
		" AEGISPULSE CYBER-DECK COCKPIT (STANDALONE) | TARGET: %s | THEME: %s ",
		m.gatewayURL, tui.Themes[m.themeIdx].Name,
	))

	totalReqs := m.normalCount + m.burstCount + m.blockedCount
	if totalReqs == 0 {
		totalReqs = 1
	}

	normalBar := tui.RenderHorizontalBar(m.normalCount, totalReqs, 20, m.styles.Success, m.styles.Dim)
	burstBar := tui.RenderHorizontalBar(m.burstCount, totalReqs, 20, m.styles.Warning, m.styles.Dim)
	blockedBar := tui.RenderHorizontalBar(m.blockedCount, totalReqs, 20, m.styles.Error, m.styles.Dim)

	telemetryContent := fmt.Sprintf(
		"TRAFFIC BREAKDOWN:\n"+
			"  NORMAL : %5d  %s\n"+
			"  BURST  : %5d  %s\n"+
			"  BLOCKED: %5d  %s\n\n"+
			"CIRCUIT BREAKER  : %s\n"+
			"UPSTREAM JITTER  : %v\n"+
			"BURST FREEZE     : %v\n"+
			"REPLAY BLOCKS    : %d\n"+
			"FALLBACK CACHED  : %d requests\n"+
			"DLQ PENDING      : %d webhooks\n"+
			"BURST REVENUE    : %s",
		m.normalCount, normalBar,
		m.burstCount, burstBar,
		m.blockedCount, blockedBar,
		m.styles.Success.Render("["+m.cbState+"]"),
		m.jitterActive,
		m.burstFrozen,
		m.replayBlocked,
		m.cacheFallbacks,
		m.dlqPending,
		m.styles.Success.Render(fmt.Sprintf("$%.4f USD", m.revenue)),
	)
	telemetryBox := m.styles.Box.Copy().Width(w/2 - 3).Render(telemetryContent)

	sparkline := tui.RenderSparkline(m.recentSamples, 30, m.styles.Sparkline)
	sparklineContent := fmt.Sprintf(
		"REAL-TIME LATENCY SPECTRUM:\n\n"+
			"  p95 LATENCY: %.2f ms\n"+
			"  p99 LATENCY: %.2f ms\n\n"+
			"  LATENCY TREND (Recent %d probes):\n"+
			"  %s\n\n"+
			"CHAOS & DRILL CONTROLS:\n"+
			"  [Space]  Unleash sudden traffic burst (100 reqs)\n"+
			"  [L]      Toggle upstream latency jitter\n"+
			"  [A]      Inject expired timestamp replay attack\n"+
			"  [T]      Cycle Cyberpunk / Dracula / Mocha theme\n"+
			"  [q]      Quit cockpit",
		m.p95Latency,
		m.p99Latency,
		len(m.recentSamples),
		sparkline,
	)
	sparklineBox := m.styles.Box.Copy().Width(w/2 - 3).Render(sparklineContent)

	mainRow := lipgloss.JoinHorizontal(lipgloss.Top, telemetryBox, sparklineBox)

	m.logsMu.Lock()
	logLines := strings.Join(m.logs, "\n")
	m.logsMu.Unlock()
	logBox := m.styles.Box.Copy().Width(w - 4).Render("SECURITY & GATEWAY ACTIVITY STREAM:\n" + logLines)

	panicBar := m.styles.AlertBox.Copy().Width(w - 4).Render(
		"🚨 TWO-STEP PANIC HOTKEYS: [Ctrl+K] Key Kill-Switch | [Ctrl+B] Global Burst Freeze | [Ctrl+U] Emergency Upstream Cutoff",
	)

	return lipgloss.JoinVertical(lipgloss.Left, header, mainRow, logBox, panicBar)
}

func parseLastInt(line string) int64 {
	parts := strings.Fields(line)
	if len(parts) >= 2 {
		val, err := strconv.ParseFloat(parts[len(parts)-1], 64)
		if err == nil {
			return int64(val)
		}
	}
	return 0
}

func parseLastFloat(line string) float64 {
	parts := strings.Fields(line)
	if len(parts) >= 2 {
		val, err := strconv.ParseFloat(parts[len(parts)-1], 64)
		if err == nil {
			return val
		}
	}
	return 0
}
