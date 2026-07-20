package ui

import (
	"fmt"
	"math"
	"strconv"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// Adaptive colours so the UI stays readable on dark and light terminals.
var (
	accent    = lipgloss.AdaptiveColor{Light: "#6C3FC4", Dark: "#B58BFF"}
	positive  = lipgloss.AdaptiveColor{Light: "#1F7A3D", Dark: "#4ADE80"}
	negative  = lipgloss.AdaptiveColor{Light: "#B01919", Dark: "#F87171"}
	dim       = lipgloss.AdaptiveColor{Light: "#6B7280", Dark: "#7A828E"}
	fg        = lipgloss.AdaptiveColor{Light: "#1A1A1A", Dark: "#E5E7EB"}
	warnColor = lipgloss.AdaptiveColor{Light: "#9A6700", Dark: "#FBBF24"}
)

var (
	tabActiveStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#FFFFFF")).
			Background(accent).
			Bold(true).
			Padding(0, 2)

	tabInactiveStyle = lipgloss.NewStyle().
				Foreground(dim).
				Padding(0, 2)

	tabBarStyle = lipgloss.NewStyle().Padding(0, 0, 1, 0)

	titleStyle = lipgloss.NewStyle().Foreground(accent).Bold(true)

	positiveStyle = lipgloss.NewStyle().Foreground(positive)
	negativeStyle = lipgloss.NewStyle().Foreground(negative)
	dimStyle      = lipgloss.NewStyle().Foreground(dim)
	warnStyle     = lipgloss.NewStyle().Foreground(warnColor)
	labelStyle    = lipgloss.NewStyle().Foreground(dim)
	valueStyle    = lipgloss.NewStyle().Foreground(fg).Bold(true)

	footerStyle = lipgloss.NewStyle().
			Foreground(dim).
			BorderTop(true).
			BorderStyle(lipgloss.NormalBorder()).
			BorderForeground(dim)

	errorStyle = lipgloss.NewStyle().Foreground(negative).Bold(true)

	boxStyle = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(dim).
			Padding(0, 1)

	statusBarStyle = lipgloss.NewStyle().Padding(0, 1)
)

// warnMark is the ⚠ shown on partial (unreliable) periods.
const warnMark = "⚠"

// ---- formatters ------------------------------------------------------------

// money formats a ZAR amount as R1,234.56.
func money(v float64) string {
	neg := v < 0
	if neg {
		v = -v
	}
	whole := int64(v)
	frac := int64(math.Round((v - float64(whole)) * 100))
	if frac == 100 { // rounding carry
		whole++
		frac = 0
	}
	s := "R" + groupThousands(whole) + "." + fmt.Sprintf("%02d", frac)
	if neg {
		return "-" + s
	}
	return s
}

// groupThousands inserts comma separators into a non-negative integer.
func groupThousands(n int64) string {
	s := strconv.FormatInt(n, 10)
	if len(s) <= 3 {
		return s
	}
	var b strings.Builder
	pre := len(s) % 3
	if pre > 0 {
		b.WriteString(s[:pre])
		if len(s) > pre {
			b.WriteByte(',')
		}
	}
	for i := pre; i < len(s); i += 3 {
		b.WriteString(s[i : i+3])
		if i+3 < len(s) {
			b.WriteByte(',')
		}
	}
	return b.String()
}

// percent formats a fractional value as a 2dp percentage, e.g. 6.25%.
func percent(frac float64) string {
	return fmt.Sprintf("%.2f%%", frac*100)
}

// points formats a fractional spread as percentage points, e.g. 19.9pts.
func points(frac float64) string {
	return fmt.Sprintf("%.1fpts", frac*100)
}

// spreadFmt formats a market spread that is ALREADY in percent units (the API
// returns 0.82 to mean 0.82%), so unlike percent() it does not scale by 100.
func spreadFmt(v float64) string {
	return fmt.Sprintf("%.2f%%", v)
}

// colourReturn styles a fractional return green/red by sign.
func colourReturn(frac float64) string {
	s := percent(frac)
	if frac < 0 {
		return negativeStyle.Render(s)
	}
	return positiveStyle.Render(s)
}

// colourMoney styles a ZAR amount green/red by sign.
func colourMoney(v float64) string {
	s := money(v)
	if v < 0 {
		return negativeStyle.Render(s)
	}
	return positiveStyle.Render(s)
}
