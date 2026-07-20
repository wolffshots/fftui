package ui

import (
	"strings"

	"github.com/charmbracelet/lipgloss"

	"github.com/wolffshots/fftui/internal/model"
)

// renderStatusBar renders the one-line live header shown above the tab bar on
// every view. It returns "" when there is no live data (CSV mode, or a live
// pull that failed), so the caller simply omits the line.
//
// Segments are ordered most- to least-important; the whole line is clipped to
// one row at the terminal width so it never wraps and steals a body row on
// narrow terminals (the trailing "updated" segment is the first to fall off).
func renderStatusBar(client *model.ClientStatus, market *model.MarketConditions, width int) string {
	if client == nil && market == nil {
		return ""
	}

	var parts []string
	if client != nil {
		st := client.Status
		label := st.SecondaryText
		if label == "" {
			label = st.Slug
		}
		seg := statusDot(st.Slug) + " " + valueStyle.Render(label)
		if st.AmountInvested > 0 {
			seg += dimStyle.Render(" · invested ") + valueStyle.Render(money(st.AmountInvested))
		}
		parts = append(parts, seg)
	}
	if market != nil {
		parts = append(parts, dimStyle.Render("spread ")+positiveStyle.Render(spreadFmt(market.Current.Spread)))
	}
	if client != nil && client.FundsUpdated != "" {
		parts = append(parts, dimStyle.Render(client.FundsUpdated))
	}

	line := strings.Join(parts, dimStyle.Render("   "))
	// MaxWidth clips (ANSI-aware) to a single row; the 1-col padding on each side
	// means the content budget is width-2.
	return statusBarStyle.MaxWidth(width).Render(lipgloss.NewStyle().MaxWidth(width - 2).Render(line))
}

// statusDot colours a ● by the current trade-stage slug: trading (green),
// loaded/queued (amber), otherwise dim.
func statusDot(slug string) string {
	switch slug {
	case "trade_processing":
		return positiveStyle.Render("●")
	case "trade_loaded":
		return warnStyle.Render("●")
	default:
		return dimStyle.Render("●")
	}
}
