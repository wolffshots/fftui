package ui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/wolffshots/fftui/internal/model"
)

type detailModel struct {
	vp     viewport.Model
	cycle  model.Cycle
	hasSel bool
	width  int
	height int
}

func newDetailModel() detailModel {
	return detailModel{vp: viewport.New(0, 0)}
}

func (m *detailModel) setSize(w, h int) {
	m.width, m.height = w, h
	m.vp.Width = w
	m.vp.Height = h
	if m.hasSel {
		m.vp.SetContent(m.render())
	}
}

func (m *detailModel) setCycle(c model.Cycle) {
	m.cycle = c
	m.hasSel = true
	m.vp.SetContent(m.render())
	m.vp.GotoTop()
}

func (m detailModel) update(msg tea.Msg) (detailModel, tea.Cmd) {
	var cmd tea.Cmd
	m.vp, cmd = m.vp.Update(msg)
	return m, cmd
}

func (m detailModel) view() string {
	if !m.hasSel {
		return dimStyle.Render("no cycle selected — pick one in the table (view 1) and press enter")
	}
	return m.vp.View()
}

func (m detailModel) render() string {
	c := m.cycle
	row := func(label, val string) string {
		return labelStyle.Render(pad(label, 25)) + valueStyle.Render(val)
	}

	var b strings.Builder
	b.WriteString(titleStyle.Render(c.Code) + dimStyle.Render("  "+c.TradeType) + "\n\n")
	b.WriteString(row("Date range", fmt.Sprintf("%s → %s",
		c.StartDate.Format("2006-01-02"), c.EndDate.Format("2006-01-02"))) + "\n")
	b.WriteString(row("Hold days", fmt.Sprintf("%d", c.HoldDays())) + "\n\n")

	b.WriteString(row("ZAR in", money(c.ZarIn)) + "\n")
	b.WriteString(row("ZAR out", money(c.ZarOut)) + "\n")
	b.WriteString(row("Net profit", colourMoney(c.NetProfit)) + "\n")
	b.WriteString(row("Return", colourReturn(c.Return())) + "\n\n")

	// Holding-days annualisation — best-case, no-idle. Explicitly labelled so it
	// isn't mistaken for the savings-comparable headline rate.
	annLabel := row("Annualised (hold-days)", colourReturn(c.AnnualisedHold()))
	b.WriteString(annLabel + "\n")
	note := "best-case, no-idle basis: this single cycle's return compounded over its\n" +
		"hold days only. NOT comparable to a savings rate — the analytics view's\n" +
		"annualised figures (which include idle time) are the honest comparison."
	b.WriteString(dimStyle.Render(note))

	return lipgloss.NewStyle().Padding(0, 1).Render(b.String())
}
