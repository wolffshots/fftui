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
	m.vp.SetContent(m.render())
	m.vp.GotoTop()
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

		if series := m.spreadSeries(); len(series) > 1 {
			w := m.textWidth()
			min, max := series[0], series[0]
			for _, v := range series {
				if v < min {
					min = v
				}
				if v > max {
					max = v
				}
			}
			b.WriteString("\n" + dimStyle.Render(fmt.Sprintf("Spread, last %dd", m.market.Period)) + "\n")
			b.WriteString(positiveStyle.Render(sparkline(series, w)) + "\n")
			b.WriteString(dimStyle.Render("  min ") + valueStyle.Render(spreadFmt(min)) +
				dimStyle.Render("  max ") + valueStyle.Render(spreadFmt(max)) +
				dimStyle.Render("  latest ") + valueStyle.Render(spreadFmt(series[len(series)-1])) + "\n")
		}
		b.WriteString("\n")
	}

	// ---- Funds & allowances ------------------------------------------------
	if m.client != nil {
		c := m.client
		b.WriteString(titleStyle.Render("Funds & allowances") + "\n")
		b.WriteString(row("Funds available", valueStyle.Render(money(c.FundsAvailable))) + "\n")
		b.WriteString(row("Total profit to date", colourMoney(c.TotalProfit)) + "\n")
		b.WriteString(row("Minimum return", valueStyle.Render(percent(c.MinimumReturn))) + "\n")
		b.WriteString(row("SDA available", valueStyle.Render(money(c.SDAAvailable))) + "\n")
		b.WriteString(row("FIA available", valueStyle.Render(money(c.FIAAvailable))) + "\n")
		if c.FundsUpdated != "" {
			b.WriteString(dimStyle.Render(c.FundsUpdated) + "\n")
		}
	}

	return lipgloss.NewStyle().Padding(0, 1).Render(b.String())
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
