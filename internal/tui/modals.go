package tui

import (
	"fmt"

	"github.com/charmbracelet/lipgloss"
)

type ModalType int

const (
	ModalNone ModalType = iota
	ModalKillSwitch
	ModalBurstFreeze
	ModalEmergencyCutoff
)

type ModalState struct {
	Active    bool
	Type      ModalType
	TargetKey string
	Title     string
	Prompt    string
}

func NewModalState() *ModalState {
	return &ModalState{
		Active:    false,
		Type:      ModalNone,
		TargetKey: "aegis_live_compromised_key_9999",
	}
}

func (m *ModalState) Open(t ModalType, targetKey string) {
	m.Active = true
	m.Type = t
	if targetKey != "" {
		m.TargetKey = targetKey
	}

	switch t {
	case ModalKillSwitch:
		m.Title = "🚨 TWO-STEP PANIC SAFETY PROTOCOL: KEY KILL-SWITCH"
		m.Prompt = fmt.Sprintf("CONFIRM KILL-SWITCH for key %s? [Y/n]\n\nThis will immediately freeze the key across Redis & Postgres.\n\nPress [Y] or [Enter] to Execute | [n] or [Esc] to Cancel", m.TargetKey)
	case ModalBurstFreeze:
		m.Title = "⚠️ TWO-STEP PANIC SAFETY PROTOCOL: GLOBAL BURST FREEZE"
		m.Prompt = "CONFIRM GLOBAL BURST FREEZE PLATFORM-WIDE?\n\nThis will immediately disable burst quotas for all tiers.\nAll excess traffic will be hard-blocked (HTTP 429).\n\nPress [Y] or [Enter] to Execute | [n] or [Esc] to Cancel"
	case ModalEmergencyCutoff:
		m.Title = "🛑 TWO-STEP PANIC SAFETY PROTOCOL: EMERGENCY UPSTREAM CUTOFF"
		m.Prompt = "CONFIRM EMERGENCY UPSTREAM CUTOFF?\n\nThis will sever live upstream routing and force 100% of\ntraffic immediately to the L1 Smart Fallback Cache.\n\nPress [Y] or [Enter] to Execute | [n] or [Esc] to Cancel"
	}
}

func (m *ModalState) Close() {
	m.Active = false
	m.Type = ModalNone
}

func RenderModal(m *ModalState, styles Styles, width int) string {
	if !m.Active {
		return ""
	}

	content := fmt.Sprintf("%s\n\n%s",
		styles.Error.Copy().Bold(true).Render(m.Title),
		m.Prompt,
	)

	return lipgloss.Place(
		width, 14,
		lipgloss.Center, lipgloss.Center,
		styles.Modal.Render(content),
	)
}
