package ui

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/wolffshots/fftui/internal/analytics"
	"github.com/wolffshots/fftui/internal/model"
)

// keyMatches reports whether a key message matches a binding.
func keyMatches(msg tea.KeyMsg, b key.Binding) bool {
	return key.Matches(msg, b)
}

type analyticsModel struct {
	cycles      []model.Cycle
	gran        analytics.Granularity
	includeDead bool
	now         time.Time
	rates       analytics.Rates
	vp          viewport.Model
	width       int
	height      int
}

func newAnalyticsModel(now time.Time, rates analytics.Rates) analyticsModel {
	return analyticsModel{gran: analytics.Year, now: now, rates: rates, vp: viewport.New(0, 0)}
}

func (m *analyticsModel) setCycles(cs []model.Cycle) { m.cycles = cs }

func (m *analyticsModel) setSize(w, h int) {
	m.width, m.height = w, h
	m.vp.Width = w
	m.vp.Height = h
}

func (m analyticsModel) update(msg tea.Msg, k keyMap) analyticsModel {
	if key, ok := msg.(tea.KeyMsg); ok {
		switch {
		case keyMatches(key, k.SubTab):
			m.gran = (m.gran + 1) % 3
			m.vp.GotoTop()
			return m
		case keyMatches(key, k.ToggleDead):
			m.includeDead = !m.includeDead
			m.vp.GotoTop()
			return m
		}
	}
	// Everything else (arrows/j/k, mouse wheel) scrolls the viewport, so the
	// view stays usable when the bucket list outgrows the terminal.
	m.vp, _ = m.vp.Update(msg)
	return m
}

// view renders the content into the scrolling viewport. The viewport copy keeps
// m's scroll offset; SetContent clamps it if the content shrank.
func (m analyticsModel) view() string {
	vp := m.vp
	vp.SetContent(m.renderContent())
	return vp.View()
}

func (m analyticsModel) renderContent() string {
	buckets := analytics.Buckets(m.cycles, m.gran, m.now, m.includeDead, m.rates)
	if len(buckets) == 0 {
		return dimStyle.Render("no data")
	}

	var b strings.Builder

	// Sub-tab indicator.
	b.WriteString(m.granTabs() + "\n\n")

	// Column layout. Annualised% is arb-only (idle at 0%); +Idle% credits idle
	// cash on non-trading days; Net% is the +Idle figure after tax (take-home).
	const (
		wPeriod = 12
		wCyc    = 5
		wProfit = 14
		wComp   = 11
		wAnn    = 12
		wIdle   = 11
		wNet    = 11
	)
	idleHdr := fmt.Sprintf("+Idle@%.0f%%", m.rates.Idle*100)
	netHdr := fmt.Sprintf("Net@%.0f%%", m.rates.Tax*100)
	header := lipgloss.NewStyle().Foreground(accent).Bold(true).Render(
		pad("Period", wPeriod) + rightPad("Cyc", wCyc) + rightPad("Profit R", wProfit) +
			rightPad("Compound%", wComp) + rightPad("Annualised%", wAnn) +
			rightPad(idleHdr, wIdle) + rightPad(netHdr, wNet))
	b.WriteString(header + "\n")
	hasPartial := false

	for _, bk := range buckets {
		label := bk.Label
		if bk.Partial {
			label += " " + warnMark
			hasPartial = true
		}
		line := pad(label, wPeriod) +
			rightPad(strconv.Itoa(bk.Count), wCyc) +
			rightPad(money(bk.TotalProfit), wProfit) +
			rightPad(percent(bk.Compound), wComp) +
			rightPad(percent(bk.Annualised), wAnn) +
			rightPad(percent(bk.AnnualisedWithIdle), wIdle) +
			rightPad(percent(bk.AnnualisedWithIdleAfterTax), wNet)
		if bk.Partial {
			b.WriteString(dimStyle.Render(line) + dimStyle.Render("  (partial)") + "\n")
		} else {
			b.WriteString(line + "\n")
		}
	}

	// Variance strips: arb-only, with-idle, and after-tax take-home.
	v := analytics.Variance(buckets)
	scope := "active only"
	if m.includeDead {
		scope = "incl. dead buckets"
	}
	b.WriteString("\n")
	strip := varianceLine(m.gran.String()+" variance", v) + "\n" +
		varianceLine(fmt.Sprintf("+idle@%.0f%%", m.rates.Idle*100), analytics.VarianceWithIdle(buckets)) + "\n" +
		varianceLine(fmt.Sprintf("net@%.0f%% (after tax)", m.rates.Tax*100), analytics.VarianceWithIdleAfterTax(buckets))
	b.WriteString(boxStyle.Render(strip) + "\n")
	b.WriteString(dimStyle.Render(fmt.Sprintf("scope: %s (a to toggle) · stats over %d full buckets · idle %s/yr on non-trading days · tax %s on returns",
		scope, v.N, percent(m.rates.Idle), percent(m.rates.Tax))))

	// Floor callout for the in-progress period: what the full period yields if no
	// further trades happen (remainder earns idle) — the pessimistic bound the
	// real period should beat.
	if cur, ok := inProgressBucket(buckets); ok {
		b.WriteString("\n" + titleStyle.Render(cur.Label+" floor") +
			dimStyle.Render(" (no more trades, remainder idle → definitely beat this): ") +
			labelStyle.Render("+idle ") + colourReturn(cur.AnnualisedFloor) +
			labelStyle.Render("  net ") + colourReturn(cur.AnnualisedFloorAfterTax) +
			dimStyle.Render(fmt.Sprintf("   vs extrapolated %s / %s",
				percent(cur.AnnualisedWithIdle), percent(cur.AnnualisedWithIdleAfterTax))))
	}

	if hasPartial {
		b.WriteString("\n" + warnStyle.Render(warnMark+" partial period — annualised figure unreliable; excluded from variance stats"))
	}
	return b.String()
}

// inProgressBucket returns the bucket that contains `now` (the current period),
// whose floor differs from its extrapolated annualised figure.
func inProgressBucket(buckets []analytics.Bucket) (analytics.Bucket, bool) {
	for _, b := range buckets {
		if b.InProgress {
			return b, true
		}
	}
	return analytics.Bucket{}, false
}

func varianceLine(title string, v analytics.VarianceStats) string {
	return fmt.Sprintf("%s  mean %s  median %s  std %s  min %s  max %s",
		titleStyle.Render(pad(title, 22)),
		valueStyle.Render(percent(v.Mean)),
		valueStyle.Render(percent(v.Median)),
		valueStyle.Render(points(v.Std)),
		valueStyle.Render(percent(v.Min)),
		valueStyle.Render(percent(v.Max)),
	)
}

func (m analyticsModel) granTabs() string {
	names := []analytics.Granularity{analytics.Year, analytics.Quarter, analytics.Month}
	var parts []string
	for _, g := range names {
		if g == m.gran {
			parts = append(parts, tabActiveStyle.Render(g.String()))
		} else {
			parts = append(parts, tabInactiveStyle.Render(g.String()))
		}
	}
	return dimStyle.Render("tab ▸ ") + lipgloss.JoinHorizontal(lipgloss.Top, parts...)
}

// pad left-aligns s within width.
func pad(s string, width int) string {
	w := lipgloss.Width(s)
	if w >= width {
		return s
	}
	return s + strings.Repeat(" ", width-w)
}
