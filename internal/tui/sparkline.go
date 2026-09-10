package tui

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
)

var sparkChars = []rune{' ', '▂', '▃', '▄', '▅', '▆', '▇', '█'}

// RenderSparkline converts a slice of floating point numbers into an ASCII sparkline string
func RenderSparkline(values []float64, maxBars int, style lipgloss.Style) string {
	if len(values) == 0 {
		return style.Render("──────────────────── [No Data]")
	}

	data := values
	if len(data) > maxBars {
		data = data[len(data)-maxBars:]
	}

	// Find min and max
	minVal := data[0]
	maxVal := data[0]
	for _, v := range data {
		if v < minVal {
			minVal = v
		}
		if v > maxVal {
			maxVal = v
		}
	}

	var sb strings.Builder
	numTicks := len(sparkChars) - 1

	for _, v := range data {
		if maxVal == minVal {
			sb.WriteRune(sparkChars[0])
			continue
		}
		ratio := (v - minVal) / (maxVal - minVal)
		idx := int(ratio * float64(numTicks))
		if idx < 0 {
			idx = 0
		}
		if idx > numTicks {
			idx = numTicks
		}
		sb.WriteRune(sparkChars[idx])
	}

	return style.Render(sb.String())
}

// RenderHorizontalBar generates a colored bar meter like [████████░░░░░░░░]
func RenderHorizontalBar(current, max int64, width int, filledStyle, emptyStyle lipgloss.Style) string {
	if max <= 0 {
		max = 1
	}
	if current < 0 {
		current = 0
	}
	if current > max {
		current = max
	}

	filledLen := int((float64(current) / float64(max)) * float64(width))
	if filledLen > width {
		filledLen = width
	}
	emptyLen := width - filledLen

	return filledStyle.Render(strings.Repeat("█", filledLen)) +
		emptyStyle.Render(strings.Repeat("░", emptyLen))
}
