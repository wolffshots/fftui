package ui

import (
	"github.com/charmbracelet/lipgloss"

	"github.com/wolffshots/fftui/internal/format"
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

// The pure formatters live in internal/format; aliased here so ui call sites
// stay unchanged.
var (
	money     = format.Money
	percent   = format.Percent
	points    = format.Points
	spreadFmt = format.SpreadFmt
)

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
