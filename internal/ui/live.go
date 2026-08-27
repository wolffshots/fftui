package ui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/wolffshots/fftui/internal/model"
)

// liveModel is view 5: the current in-progress cycle's status, the live market
// spread (with a history sparkline), and the client's funds and allowances.
// All of this is live-only; in CSV mode it shows a hint instead.
type liveModel struct {
	vp     viewport.Model
	client *model.ClientStatus
	market *model.MarketConditions
	width  int
	height int
}

func newLiveModel() liveModel {
	return liveModel{vp: viewport.New(0, 0)}
}

func (m *liveModel) setData(c *model.ClientStatus, mk *model.MarketConditions) {
	m.client, m.market = c, mk
	// No GotoTop: this runs on every (auto-)refresh, and yanking the scroll
	// away mid-read is worse than a stale offset — SetContent already clamps
	// it when the content shrinks.
	m.vp.SetContent(m.render())
}

func (m *liveModel) setSize(w, h int) {
	m.width, m.height = w, h
	m.vp.Width, m.vp.Height = w, h
	m.vp.SetContent(m.render())
}

func (m liveModel) update(msg tea.Msg) (liveModel, tea.Cmd) {
	var cmd tea.Cmd
	m.vp, cmd = m.vp.Update(msg)
	return m, cmd
}

func (m liveModel) view() string {
	if m.client == nil && m.market == nil {
		return dimStyle.Render("live data is only available from the live API — run without --csv")
	}
	return m.vp.View()
}

func (m liveModel) render() string {
	row := func(label, val string) string {
		return labelStyle.Render(pad(label, 22)) + val
	}

	var b strings.Builder

	// ---- Current cycle -----------------------------------------------------
	if m.client != nil {
		st := m.client.Status
		label := st.SecondaryText
		if label == "" {
			label = st.Slug
		}
		b.WriteString(titleStyle.Render("Current cycle") + "\n")
		b.WriteString(statusDot(st.Slug) + " " + valueStyle.Render(label) + "\n")
		if st.Description != "" {
			// Separate the glyph with a tab: it renders one or two cells wide
			// depending on the terminal and font, and only the terminal's own
			// tab stop can start the text at the same column either way.
			if g := statusIcon(st.Icon); g != "" {
				b.WriteString(dimStyle.Render(g) + tabSentinel)
			}
			b.WriteString(dimStyle.Render(wrap(st.Description, m.textWidth())) + "\n")
		}
		b.WriteString(row("Amount invested", valueStyle.Render(money(st.AmountInvested))) + "\n")
		if st.NetProfit != nil {
			b.WriteString(row("Net profit so far", colourMoney(*st.NetProfit)) + "\n")
		} else {
			b.WriteString(row("Net profit so far", dimStyle.Render("— (still queued)")) + "\n")
		}
		b.WriteString("\n")
	}

	// ---- Market / spread ---------------------------------------------------
	if m.market != nil {
		cur := m.market.Current
		b.WriteString(titleStyle.Render("Market conditions") + "\n")
		b.WriteString(row("Spread", positiveStyle.Render(spreadFmt(cur.Spread))) + "\n")
		b.WriteString(row("Local price", valueStyle.Render(fmt.Sprintf("%.4f", cur.LocalPrice))) + "\n")
		b.WriteString(row("Offshore price", valueStyle.Render(fmt.Sprintf("%.4f", cur.OffshorePrice))) + "\n")
		b.WriteString(row("Exchange rate", valueStyle.Render(fmt.Sprintf("%.4f", cur.ExchangeRate))) + "\n")

		spark := func(title string, series []float64, st lipgloss.Style, f func(float64) string) {
			if len(series) < 2 {
				return
			}
			min, max := series[0], series[0]
			for _, v := range series {
				if v < min {
					min = v
				}
				if v > max {
					max = v
				}
			}
			b.WriteString("\n" + dimStyle.Render(fmt.Sprintf("%s, last %dd", title, m.market.Period)) + "\n")
			b.WriteString(st.Render(sparkline(series, m.textWidth())) + "\n")
			b.WriteString(dimStyle.Render("  min ") + valueStyle.Render(f(min)) +
				dimStyle.Render("  max ") + valueStyle.Render(f(max)) +
				dimStyle.Render("  latest ") + valueStyle.Render(f(series[len(series)-1])) + "\n")
		}
		spark("Spread", m.spreadSeries(), positiveStyle, spreadFmt)
		spark("Exchange rate", m.rateSeries(), valueStyle, rateFmt)
		b.WriteString("\n")
	}

	// ---- Funds & allowances ------------------------------------------------
	if m.client != nil {
		c := m.client
		b.WriteString(titleStyle.Render("Funds & allowances") + "\n")
		b.WriteString(row("Funds available", valueStyle.Render(money(c.FundsAvailable))) + "\n")
		if d := c.DepositBank; d.Account != "" {
			line := valueStyle.Render(d.Account) + dimStyle.Render("  "+d.Bank)
			if d.Branch != "" {
				line += dimStyle.Render("  branch " + d.Branch)
			}
			if d.Type != "" {
				line += dimStyle.Render("  " + d.Type)
			}
			b.WriteString(row("Deposit account", line) + "\n")
		}
		b.WriteString(row("Total profit to date", colourMoney(c.TotalProfit)) + "\n")
		b.WriteString(row("Minimum return", valueStyle.Render(percent(c.MinimumReturn))) + "\n")
		b.WriteString(row("SDA available", valueStyle.Render(money(c.SDAAvailable))) + "\n")
		if d := c.SDADetail; d != (model.SDADetail{}) {
			b.WriteString(row("", dimStyle.Render("unused ")+valueStyle.Render(money(d.Unused))+
				dimStyle.Render("  reserved ")+valueStyle.Render(money(d.Reserved))+
				dimStyle.Render("  used ")+valueStyle.Render(money(d.Used))) + "\n")
		}
		b.WriteString(row("AIT available", valueStyle.Render(money(c.AITAvailable))) + "\n")
		if d := c.AITDetail; d != (model.AITDetail{}) {
			line := dimStyle.Render("available ") + valueStyle.Render(money(d.Available)) +
				dimStyle.Render("  pending ") + valueStyle.Render(money(d.Pending))
			if d.PendingDays > 0 {
				line += dimStyle.Render(fmt.Sprintf(" (%d working days)", d.PendingDays))
			}
			line += dimStyle.Render("  still to apply for ") + valueStyle.Render(money(d.ToApply))
			b.WriteString(row("", line) + "\n")
		}
		if c.FundsUpdated != "" {
			b.WriteString(dimStyle.Render(c.FundsUpdated) + "\n")
		}
		if c.FundsWarning != "" {
			b.WriteString(warnStyle.Render(wrap(c.FundsWarning, m.textWidth())) + "\n")
		}
	}

	return lipgloss.NewStyle().Padding(0, 1).TabWidth(lipgloss.NoTabConversion).Render(b.String())
}

// spreadSeries extracts the spread history for the sparkline.
func (m liveModel) spreadSeries() []float64 {
	if m.market == nil {
		return nil
	}
	out := make([]float64, len(m.market.History))
	for i, p := range m.market.History {
		out[i] = p.Spread
	}
	return out
}

// rateFmt renders an exchange rate at the precision the API reports it.
func rateFmt(v float64) string { return fmt.Sprintf("%.4f", v) }

// rateSeries extracts the ZAR/USD exchange-rate history for the sparkline.
func (m liveModel) rateSeries() []float64 {
	if m.market == nil {
		return nil
	}
	out := make([]float64, len(m.market.History))
	for i, p := range m.market.History {
		out[i] = p.ExchangeRate
	}
	return out
}

// textWidth is the usable width inside the 1-col padding, clamped for sparklines.
func (m liveModel) textWidth() int {
	w := m.width - 4
	if w < 10 {
		w = 10
	}
	if w > 120 {
		w = 120
	}
	return w
}

// wrap soft-wraps s to width using lipgloss so styled width is respected.
func wrap(s string, width int) string {
	return lipgloss.NewStyle().Width(width).Render(s)
}

// tabSentinel stands in for a tab inside rendered content. lipgloss replaces
// tabs with spaces on every Render, including the one the viewport does
// internally, so RootModel.View swaps this back for a real tab at the end.
const tabSentinel = "\x00"
