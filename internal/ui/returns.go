package ui

import (
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/wolffshots/fftui/internal/analytics"
	"github.com/wolffshots/fftui/internal/model"
)

// returnsModel is view 6: what a cycle earns at each capital size at one
// spread, run through the same fee waterfall as the cycle statements. tab
// cycles that spread through the scenarios below, so the ladder can be read at
// a worse and a better market than today's.
type returnsModel struct {
	vp         viewport.Model
	fees       analytics.Fees
	cycles     []model.Cycle
	now        time.Time
	market     *model.MarketConditions
	marketYear *model.MarketConditions // 365d history; the 7d Market series is too short for a 30d window
	client     *model.ClientStatus
	scenario   analytics.Scenario
	width      int
	height     int
}

func newReturnsModel(now time.Time, fees analytics.Fees) returnsModel {
	return returnsModel{vp: viewport.New(0, 0), now: now, fees: fees}
}

// The setters rebuild the content, so view never recomputes it per key.
func (m *returnsModel) setCycles(cs []model.Cycle) {
	m.cycles = cs
	m.vp.SetContent(m.render())
}

func (m *returnsModel) setData(c *model.ClientStatus, mk, year *model.MarketConditions) {
	m.client, m.market, m.marketYear = c, mk, year
	m.vp.SetContent(m.render())
}

func (m *returnsModel) setSize(w, h int) {
	m.width, m.height = w, h
	m.vp.Width, m.vp.Height = w, h
	m.vp.SetContent(m.render())
}

func (m returnsModel) update(msg tea.Msg, k keyMap) (returnsModel, tea.Cmd) {
	if key, ok := msg.(tea.KeyMsg); ok && keyMatches(key, k.SubTab) {
		m.scenario = m.nextScenario()
		m.vp.SetContent(m.render())
		m.vp.GotoTop()
		return m, nil
	}
	var cmd tea.Cmd
	m.vp, cmd = m.vp.Update(msg)
	return m, cmd
}

// input gathers what the shared projection needs; the web front end builds the
// same struct, so both quote identical figures.
func (m returnsModel) input() analytics.ScenarioInput {
	in := analytics.ScenarioInput{Cycles: m.cycles, Now: m.now, Fees: m.fees}
	if m.market != nil {
		in.LiveSpread = m.market.Current.Spread
	}
	if m.marketYear != nil {
		in.History, in.HistoryDays = m.marketYear.History, m.marketYear.Period
	}
	if m.client != nil {
		in.Invested = m.client.Status.AmountInvested
	}
	return in
}

// nextScenario is the next scenario that has an input behind it, wrapping.
// Scenarios that cannot be derived from the current data are skipped rather
// than selected and left projecting nothing.
func (m returnsModel) nextScenario() analytics.Scenario {
	avail := m.available()
	for i, s := range avail {
		if s == m.scenario {
			return avail[(i+1)%len(avail)]
		}
	}
	return m.scenario
}

// available lists the scenarios this data supports, in strip order.
func (m returnsModel) available() []analytics.Scenario { return m.input().Available() }

// scenarioTabs mirrors the Analytics granularity strip. CSV mode has no market
// history, so the observed bounds are left off the strip entirely — with the
// reason — rather than offered against a number they cannot be derived from.
func (m returnsModel) scenarioTabs() string {
	var parts []string
	for _, s := range m.available() {
		if s == m.scenario {
			parts = append(parts, tabActiveStyle.Render(s.String()))
		} else {
			parts = append(parts, tabInactiveStyle.Render(s.String()))
		}
	}
	strip := dimStyle.Render("tab ▸ ") + lipgloss.JoinHorizontal(lipgloss.Top, parts...)
	// Both bounds come from the one history series, so one check covers both.
	if _, _, ok := m.input().Spread(analytics.ScenarioLower); !ok {
		strip += dimStyle.Render("  (lower/higher need the live market history)")
	}
	return strip
}

func (m returnsModel) view() string { return m.vp.View() }

// spread resolves the spread to project at, for the active scenario.
func (m returnsModel) spread() (frac float64, source string, ok bool) {
	return m.input().Spread(m.scenario)
}

const (
	wCapital = 15
	wEarn    = 12
	wThird   = 12
	wGrossP  = 14
	wTierPct = 7
	wFFFee   = 12
	wNetP    = 13
	wNetRet  = 11
	wKeep    = 10
	wMinSprd = 11
)

func (m returnsModel) render() string {
	spread, source, ok := m.spread()
	if !ok {
		return dimStyle.Render("no spread to project — needs the live market feed, or at least one cycle in the last year")
	}

	var b strings.Builder
	b.WriteString(m.scenarioTabs() + "\n\n")
	b.WriteString(titleStyle.Render("Expected return per cycle") +
		dimStyle.Render("  at a gross-earnings spread of ") +
		valueStyle.Render(spreadFmt(spread*100)) +
		dimStyle.Render("  ("+m.scenario.String()+": "+source+")") + "\n\n")

	header := lipgloss.NewStyle().Foreground(accent).Bold(true).Render(
		rightPad("Capital", wCapital) + rightPad("Gross earn", wEarn) +
			rightPad("3rd-party", wThird) + rightPad("Gross profit", wGrossP) +
			rightPad("FF %", wTierPct) + rightPad("FF fee", wFFFee) +
			rightPad("Net profit", wNetP) + rightPad("Net/cycle", wNetRet) +
			rightPad("You keep", wKeep) + rightPad("Min spread", wMinSprd))
	b.WriteString(header + "\n")

	fees := m.fees.At(m.now)
	capitals, now := m.input().Capitals()
	for _, capital := range capitals {
		p := fees.Project(spread, capital)
		// "You keep" is the share of the gross EARNINGS that survives both the
		// third-party fees and FF's cut. A losing cycle keeps nothing to split.
		keep := "—"
		if p.NetProfit > 0 && p.GrossEarnings > 0 {
			keep = percent(p.NetProfit / p.GrossEarnings)
		}
		line := rightPad(money(p.Capital), wCapital) +
			rightPad(money(p.GrossEarnings), wEarn) +
			rightPad(charged(p.VariableFee+p.FixedFee), wThird) +
			rightPad(colourMoney(p.GrossProfit), wGrossP) +
			rightPad(analytics.TierPct(p.TierRate), wTierPct) +
			rightPad(charged(p.SuccessFee), wFFFee) +
			rightPad(colourMoney(p.NetProfit), wNetP) +
			rightPad(colourReturn(p.NetReturn), wNetRet) +
			rightPad(keep, wKeep) +
			rightPad(minSpread(fees, capital), wMinSprd)
		if capital == now {
			line += titleStyle.Render("  ◀ now")
		}
		b.WriteString(line + "\n")
	}
	b.WriteString(dimStyle.Render("you keep = net profit ÷ gross earnings — your share of the spread after the "+
		"third-party fees and FF's cut") + "\n")

	b.WriteString("\n" + m.renderFeeModel(spread))
	return lipgloss.NewStyle().Padding(0, 1).Render(b.String())
}

// minSpread renders the break-even spread for one capital: below it the cycle
// loses money whatever the market does. Shown per row because the fixed fee
// amortises, so a bigger cycle clears on a thinner market.
func minSpread(f analytics.Fees, capital float64) string {
	be, ok := f.BreakEvenSpread(capital)
	if !ok {
		return "—"
	}
	return percent(be)
}

// renderFeeModel spells out every constituent part of the fee figures above, in
// statement order, so the net column can be checked by hand.
func (m returnsModel) renderFeeModel(spread float64) string {
	f := m.fees.At(m.now)
	row := func(label, val, note string) string {
		return labelStyle.Render(pad(label, 24)) + valueStyle.Render(pad(val, 28)) + dimStyle.Render(note)
	}
	var lines []string

	lines = append(lines, titleStyle.Render("fee model")+
		dimStyle.Render("  per cycle, in statement order"))
	lines = append(lines, row("gross earnings", "capital × "+spreadFmt(spread*100),
		"the market spread FF trades into"))

	fixedNote := "bank admin + instant EFT"
	if f.Fixed == analytics.DefaultFees().At(m.now).Fixed {
		fixedNote = "Capitec admin " + money(f.Fixed-analytics.EFTFee) +
			" + instant EFT " + money(analytics.EFTFee)
	}
	// Flag a dated cut before it lands, so the ladder is not silently stale.
	if !f.FixedFrom.IsZero() && f.FixedFrom.After(m.now) {
		fixedNote += " — falls to " + money(f.FixedAfter) + " on " + f.FixedFrom.Format("2 Jan 2006")
	}
	lines = append(lines, row("− third-party fixed", money(f.Fixed), fixedNote))
	lines = append(lines, row("− third-party variable", percent(f.Variable)+" of capital",
		"bank exchange + offshore receipt + offshore trading"))
	lines = append(lines, row("= gross profit", "earnings − those fees", "the statement's Gross Profit line"))
	lines = append(lines, row("− FF success fee", "tier % of GROSS PROFIT",
		"FF's share is taken after the third-party fees, never on a loss"))
	lines = append(lines, labelStyle.Render(pad("", 24))+dimStyle.Render(f.TierLadder()))
	lines = append(lines, row("= net profit", "what lands in your account", "before income tax"))

	if be, ok := f.BreakEven(spread); ok {
		lines = append(lines, row("break-even capital", money(be),
			"below this the fees are bigger than the spread earns"))
	} else {
		lines = append(lines, row("break-even capital", "none",
			"the variable fee alone eats this spread — no cycle size profits"))
	}

	return boxStyle.Render(strings.Join(lines, "\n")) + "\n" +
		dimStyle.Render("modelled, not quoted: the variable fee is assumed proportional at every size. "+
			"Real statements charged\n0.228%–0.235% of capital, so a projected net profit is good to about ±1%. "+
			"Override with --fee-fixed / --fee-variable.")
}

// charged renders a fee as a deduction, and a fee that is not levied (a losing
// cycle pays FF nothing) as a plain zero rather than "-R0.00".
func charged(v float64) string {
	if v <= 0 {
		return money(0)
	}
	return "-" + money(v)
}
