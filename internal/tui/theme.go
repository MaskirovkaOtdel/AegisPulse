package tui

import (
	"github.com/charmbracelet/lipgloss"
)

type Theme struct {
	Name       string
	Primary    lipgloss.Color
	Secondary  lipgloss.Color
	Accent     lipgloss.Color
	Success    lipgloss.Color
	Warning    lipgloss.Color
	Error      lipgloss.Color
	Background lipgloss.Color
	Foreground lipgloss.Color
}

var (
	ThemeDracula = Theme{
		Name:       "Dracula",
		Primary:    lipgloss.Color("#BD93F9"), // Purple
		Secondary:  lipgloss.Color("#FF79C6"), // Pink
		Accent:     lipgloss.Color("#8BE9FD"), // Cyan
		Success:    lipgloss.Color("#50FA7B"), // Green
		Warning:    lipgloss.Color("#FFB86C"), // Orange
		Error:      lipgloss.Color("#FF5555"), // Red
		Background: lipgloss.Color("#282A36"),
		Foreground: lipgloss.Color("#F8F8F2"),
	}

	ThemeCatppuccinMocha = Theme{
		Name:       "Catppuccin Mocha",
		Primary:    lipgloss.Color("#CBA6F7"), // Mauve
		Secondary:  lipgloss.Color("#B4BEFE"), // Lavender
		Accent:     lipgloss.Color("#89DCEB"), // Sky
		Success:    lipgloss.Color("#A6E3A1"), // Green
		Warning:    lipgloss.Color("#FAB387"), // Peach
		Error:      lipgloss.Color("#F38BA8"), // Maroon
		Background: lipgloss.Color("#1E1E2E"),
		Foreground: lipgloss.Color("#CDD6F4"),
	}

	ThemeMatrixCyberpunk = Theme{
		Name:       "Matrix Cyberpunk",
		Primary:    lipgloss.Color("#00FF66"), // Electric Green
		Secondary:  lipgloss.Color("#00E5FF"), // Neon Cyan
		Accent:     lipgloss.Color("#39FF14"), // Neon Lime
		Success:    lipgloss.Color("#00FF00"), // Terminal Green
		Warning:    lipgloss.Color("#FFFF00"), // Bright Yellow
		Error:      lipgloss.Color("#FF0055"), // Cyber Red
		Background: lipgloss.Color("#0D1117"),
		Foreground: lipgloss.Color("#E6EDF3"),
	}

	ThemeCatppuccinLatte = Theme{
		Name:       "Catppuccin Latte (Light)",
		Primary:    lipgloss.Color("#8839EF"), // Mauve
		Secondary:  lipgloss.Color("#7287FD"), // Lavender
		Accent:     lipgloss.Color("#1E66F5"), // Blue
		Success:    lipgloss.Color("#40A02B"), // Green
		Warning:    lipgloss.Color("#DF8E1D"), // Yellow
		Error:      lipgloss.Color("#D20F39"), // Red
		Background: lipgloss.Color("#EFF1F5"),
		Foreground: lipgloss.Color("#4C4F69"),
	}

	Themes = []Theme{ThemeDracula, ThemeCatppuccinMocha, ThemeMatrixCyberpunk, ThemeCatppuccinLatte}
)

// DetectInitialTheme checks system/terminal Light vs Dark mode and selects appropriate default
func DetectInitialTheme() int {
	if !lipgloss.HasDarkBackground() {
		return 3 // Catppuccin Latte (Light preset)
	}
	return 0 // Dracula (Dark preset)
}

type Styles struct {
	Header    lipgloss.Style
	Box       lipgloss.Style
	AlertBox  lipgloss.Style
	Success   lipgloss.Style
	Warning   lipgloss.Style
	Error     lipgloss.Style
	Primary   lipgloss.Style
	Secondary lipgloss.Style
	Dim       lipgloss.Style
	Sparkline lipgloss.Style
	Modal     lipgloss.Style
}

func MakeStyles(t Theme) Styles {
	return Styles{
		Header: lipgloss.NewStyle().
			Bold(true).
			Foreground(t.Primary).
			Background(t.Background).
			Padding(0, 1),
		Box: lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(t.Primary).
			Padding(1, 2),
		AlertBox: lipgloss.NewStyle().
			Border(lipgloss.ThickBorder()).
			BorderForeground(t.Error).
			Padding(1, 2),
		Success:   lipgloss.NewStyle().Foreground(t.Success).Bold(true),
		Warning:   lipgloss.NewStyle().Foreground(t.Warning).Bold(true),
		Error:     lipgloss.NewStyle().Foreground(t.Error).Bold(true),
		Primary:   lipgloss.NewStyle().Foreground(t.Primary),
		Secondary: lipgloss.NewStyle().Foreground(t.Secondary),
		Dim:       lipgloss.NewStyle().Foreground(lipgloss.Color("#6272A4")),
		Sparkline: lipgloss.NewStyle().Foreground(t.Accent),
		Modal: lipgloss.NewStyle().
			Border(lipgloss.DoubleBorder()).
			BorderForeground(t.Error).
			Background(t.Background).
			Foreground(t.Foreground).
			Padding(1, 3).
			Bold(true),
	}
}
