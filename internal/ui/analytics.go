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
	allow       analytics.Allowances
	fees        analytics.Fees
	client      *model.ClientStatus       // live snapshot for actual allowance balances; nil in CSV mode
	marketYear  *model.MarketConditions   // year of spread history for the trend strip; nil in CSV mode
	boot        analytics.BootstrapResult // cached on setCycles; too heavy to redo per render
	vp          viewport.Model
	width       int
	height      int
}

func newAnalyticsModel(now time.Time, rates analytics.Rates, allow analytics.Allowances, fees analytics.Fees) analyticsModel {
	return analyticsModel{gran: analytics.Year, now: now, rates: rates, allow: allow, fees: fees, vp: viewport.New(0, 0)}
}

func (m *analyticsModel) setCycles(cs []model.Cycle) {
	m.cycles = cs
	m.boot = analytics.Bootstrap(cs, m.rates, 10_000)
}

func (m *analyticsModel) setSize(w, h int) {
	m.width, m.height = w, h
	m.vp.Width = w
	m.vp.Height = h
}

func (m analyticsModel) update(msg tea.Msg, k keyMap) analyticsModel {
	if key, ok := msg.(tea.KeyMsg); ok {
		switch {
		case keyMatches(key, k.SubTab):
			m.gran = (m.gran + 1) % analytics.GranularityCount
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

	// Lifetime money-weighted (XIRR) rate: like the arb-only annualised but each
	// cycle counts in proportion to its capital, so it tracks what the actual
	// rands earned across deposits. Diverges from time-weighted when capital size
	// varies between cycles.
	if mwr, ok := analytics.MoneyWeighted(m.cycles); ok {
		life := analytics.Lifetime(m.cycles, m.rates)
		b.WriteString("\n" + titleStyle.Render("money-weighted (IRR)") +
			dimStyle.Render(" lifetime, capital-weighted, arb-only: ") + colourReturn(mwr) +
			dimStyle.Render("   vs time-weighted ") + colourReturn(life.Annualised))
	}

	// Bootstrap band on the lifetime figures: how much the headline rate
	// depends on which cycles happened to land (cadence held fixed).
	if m.boot.OK {
		b.WriteString("\n" + titleStyle.Render("bootstrap 90% band") +
			dimStyle.Render(" arb-only ") +
			valueStyle.Render(percent(m.boot.Arb.Lo)+"–"+percent(m.boot.Arb.Hi)) +
			dimStyle.Render("   net ") +
			valueStyle.Render(percent(m.boot.Net.Lo)+"–"+percent(m.boot.Net.Hi)) +
			dimStyle.Render(fmt.Sprintf("   (%d resamples of the cycle returns, cadence fixed)", m.boot.N)))
	}

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

	b.WriteString("\n\n" + m.renderPlanning())
	if trend := m.renderTrend(); trend != "" {
		b.WriteString("\n" + trend)
	}

	if hasPartial {
		b.WriteString("\n" + warnStyle.Render(warnMark+" partial period — annualised figure unreliable; excluded from variance stats"))
	}
	return b.String()
}

// renderPlanning renders the fiscal / capital-planning strip: current-tax-year
// taxable profit, combined SDA+FIA runway for the calendar year, and the
// marginal value of extra capital (see analytics.Plan).
func (m analyticsModel) renderPlanning() string {
	allow := m.allow
	if m.client != nil {
		allow.Live = true
		allow.SDAAvailable = m.client.SDAAvailable
		allow.FIAAvailable = m.client.FIAAvailable
	}
	p := analytics.Plan(m.cycles, m.now, m.rates, allow, m.fees)
	var lines []string

	lines = append(lines, titleStyle.Render(pad(p.TaxYearLabel+" taxable profit", 22))+
		colourMoney(p.TaxYearProfit)+
		dimStyle.Render(fmt.Sprintf("  est. tax @%s ", percent(m.rates.Tax)))+
		valueStyle.Render(money(p.EstimatedTax))+
		dimStyle.Render("  (profit lands in the tax year the cycle ends)"))

	if p.TotalLimit > 0 {
		use := titleStyle.Render(pad(fmt.Sprintf("allowance %d", m.now.Year()), 22)) +
			labelStyle.Render("used ") + valueStyle.Render(money(p.Used)) +
			dimStyle.Render(" of "+money(p.TotalLimit)+" ") +
			valueStyle.Render(percent(p.Used/p.TotalLimit))
		switch {
		case p.Exhausted:
			use += warnStyle.Render("  exhausted — no more cycles this calendar year")
		case p.HasExhaust:
			use += dimStyle.Render("  ≈ exhausts ") + valueStyle.Render(p.ExhaustDate.Format("2006-01-02")) +
				dimStyle.Render(" at the current pace")
		default:
			use += dimStyle.Render("  lasts the year at the current pace")
		}
		lines = append(lines, use)

		if allow.Live {
			lines = append(lines, titleStyle.Render(pad("", 22))+
				labelStyle.Render("SDA left ")+valueStyle.Render(money(allow.SDAAvailable))+
				dimStyle.Render(" · ")+
				labelStyle.Render("FIA left ")+valueStyle.Render(money(allow.FIAAvailable))+
				dimStyle.Render("  (live · FIA via FF's AIT filings)"))
		} else {
			lines = append(lines, titleStyle.Render(pad("", 22))+
				dimStyle.Render("usage inferred from cycle history — live SDA/FIA split needs the live source"))
		}

		if p.SweetSpot > 0 {
			lines = append(lines, titleStyle.Render(pad("capital sweet spot", 22))+
				valueStyle.Render(money(p.SweetSpot))+
				dimStyle.Render(fmt.Sprintf("/cycle at %d cycles/yr — the combined allowance binds above this", p.CyclesPerYear)))
			verdict := titleStyle.Render(pad("current capital", 22)) + valueStyle.Render(money(p.CurrentCapital))
			if p.CurrentCapital > p.SweetSpot {
				verdict += warnStyle.Render("  above the spot") +
					dimStyle.Render(fmt.Sprintf(" — +R100k ≈ +%s/yr gross, %s net (fee-tier effect only)",
						money(p.Extra100kGross), money(p.Extra100kNet)))
			} else {
				verdict += dimStyle.Render(fmt.Sprintf("  below the spot — +R100k ≈ +%s/yr gross, %s net",
					money(p.Extra100kGross), money(p.Extra100kNet)))
			}
			lines = append(lines, verdict)
		}

		// Fee ladder: bigger cycles dilute the fixed fees and pay a lower FF
		// tier, so the modelled net return per cycle rises with capital.
		if p.TopTierMin > 0 && p.CurrentCapital > 0 {
			lines = append(lines, titleStyle.Render(pad("fee ladder", 22))+
				labelStyle.Render("FF fee ")+valueStyle.Render(percent(p.CurrentTier))+
				dimStyle.Render(" of gross now → ")+valueStyle.Render(percent(p.TopTier))+
				dimStyle.Render(" at "+money(p.TopTierMin)+"+ · net/cycle ")+
				colourReturn(p.ReturnNow)+dimStyle.Render(" → ")+colourReturn(p.ReturnAtTop)+
				dimStyle.Render(" at the top tier"))
		}
	}

	return boxStyle.Render(strings.Join(lines, "\n"))
}

// renderTrend renders the decay-detection strip: the OLS slope on per-cycle
// returns over the trailing year, recent-vs-prior 90-day rate and cadence, and
// (live only) recent vs year-average market spread. Empty when there is too
// little data to say anything.
func (m analyticsModel) renderTrend() string {
	t := analytics.TrendOf(m.cycles, m.now)
	if t.N == 0 {
		return ""
	}
	var lines []string

	slope := fmt.Sprintf("%+.3fpp", t.Slope90*100)
	verdict := "noise (not significant)"
	if t.Significant {
		if t.Slope90 < 0 {
			verdict = "significant decay"
		} else {
			verdict = "significant improvement"
		}
	}
	slopeStyled := valueStyle.Render(slope)
	if t.Significant && t.Slope90 < 0 {
		slopeStyled = warnStyle.Render(slope)
	}
	lines = append(lines, titleStyle.Render(pad("return trend", 22))+
		slopeStyled+dimStyle.Render(fmt.Sprintf("/cycle per 90d over %d cycles — %s", t.N, verdict)))

	lines = append(lines, titleStyle.Render(pad("90d vs prior 90d", 22))+
		labelStyle.Render("annualised ")+colourReturn(t.Recent90)+
		dimStyle.Render(" vs ")+colourReturn(t.Prior90)+
		dimStyle.Render(fmt.Sprintf("  ·  cadence %d vs %d cycles", t.RecentCycles, t.PriorCycles)))

	// Live only: is the raw market opportunity itself thinning?
	if m.marketYear != nil {
		if recent, overall, ok := analytics.SpreadTrend(m.marketYear.History, m.marketYear.Period, 30); ok {
			lines = append(lines, titleStyle.Render(pad("market spread", 22))+
				labelStyle.Render("30d avg ")+valueStyle.Render(fmt.Sprintf("%.2f%%", recent))+
				dimStyle.Render(fmt.Sprintf(" vs %dd avg ", m.marketYear.Period))+
				valueStyle.Render(fmt.Sprintf("%.2f%%", overall))+
				dimStyle.Render("  (from the live market-conditions feed)"))
		}
	}

	return boxStyle.Render(strings.Join(lines, "\n"))
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
	names := []analytics.Granularity{analytics.Year, analytics.Quarter, analytics.Month, analytics.TaxYear}
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
