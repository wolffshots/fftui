package ui

import (
	"fmt"
	"sort"
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
	scenario   spreadScenario
	width      int
	height     int
}

// spreadScenario selects which spread the ladder is projected at.
type spreadScenario int

const (
	scenarioNow      spreadScenario = iota // live market feed (CSV mode: the trailing average)
	scenarioLower                          // lowest spread the market actually printed recently
	scenarioHigher                         // highest one
	scenarioRealised                       // what the account actually caught, after execution timing
)

var scenarioOrder = []spreadScenario{scenarioNow, scenarioLower, scenarioHigher, scenarioRealised}

func (s spreadScenario) String() string {
	switch s {
	case scenarioLower:
		return "lower"
	case scenarioHigher:
		return "higher"
	case scenarioRealised:
		return "realised"
	}
	return "now"
}

// scenarioWindow is the history window (days) the lower/higher cases are the
// bounds of: long enough to have seen a bad and a good market, short enough to
// still describe the current one.
const scenarioWindow = 30

// ladder is the capital ladder: every FF fee-tier boundary plus round steps
// either side, so the tier jumps are visible.
var ladder = []float64{
	50_000, 100_000, 150_000, 200_000, 250_000,
	300_000, 400_000, 500_000, 750_000, 1_000_000,
}

func newReturnsModel(now time.Time, fees analytics.Fees) returnsModel {
	return returnsModel{vp: viewport.New(0, 0), now: now, fees: fees}
}

func (m *returnsModel) setCycles(cs []model.Cycle) { m.cycles = cs }

func (m *returnsModel) setData(c *model.ClientStatus, mk, year *model.MarketConditions) {
	m.client, m.market, m.marketYear = c, mk, year
}

func (m *returnsModel) setSize(w, h int) {
	m.width, m.height = w, h
	m.vp.Width, m.vp.Height = w, h
}

func (m returnsModel) update(msg tea.Msg, k keyMap) (returnsModel, tea.Cmd) {
	if key, ok := msg.(tea.KeyMsg); ok && keyMatches(key, k.SubTab) {
		m.scenario = m.nextScenario()
		m.vp.GotoTop()
		return m, nil
	}
	var cmd tea.Cmd
	m.vp, cmd = m.vp.Update(msg)
	return m, cmd
}

// nextScenario is the next scenario that has an input behind it, wrapping.
// Scenarios that cannot be derived from the current data are skipped rather
// than selected and left projecting nothing.
func (m returnsModel) nextScenario() spreadScenario {
	avail := m.available()
	for i, s := range avail {
		if s == m.scenario {
			return avail[(i+1)%len(avail)]
		}
	}
	return m.scenario
}

// available lists the scenarios this data supports, in strip order.
func (m returnsModel) available() []spreadScenario {
	var out []spreadScenario
	for _, s := range scenarioOrder {
		if _, _, ok := m.spreadFor(s); ok {
			out = append(out, s)
		}
	}
	return out
}

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
	if _, _, ok := m.spreadFor(scenarioLower); !ok {
		strip += dimStyle.Render("  (lower/higher need the live market history)")
	}
	return strip
}

func (m returnsModel) view() string {
	vp := m.vp
	vp.SetContent(m.render())
	return vp.View()
}

// spread resolves the spread to project at, for the active scenario.
func (m returnsModel) spread() (frac float64, source string, ok bool) {
	return m.spreadFor(m.scenario)
}

// spreadFor resolves one scenario to a spread and the label saying how it was
// derived. This is a money view, so a scenario with no input reports ok=false
// instead of a zero: the caller drops it rather than project off it.
func (m returnsModel) spreadFor(s spreadScenario) (frac float64, source string, ok bool) {
	realised := func() (float64, int) { return analytics.AvgSpread(m.cycles, m.now, m.fees) }

	switch s {
	case scenarioLower, scenarioHigher:
		if m.marketYear == nil {
			return 0, "", false
		}
		low, high, ok := analytics.SpreadRange(m.marketYear.History, m.marketYear.Period, scenarioWindow)
		if !ok || low <= 0 {
			return 0, "", false
		}
		if s == scenarioLower {
			return low / 100, fmt.Sprintf("lowest spread in the last %d days of market history", scenarioWindow), true
		}
		return high / 100, fmt.Sprintf("highest spread in the last %d days of market history", scenarioWindow), true

	case scenarioRealised:
		avg, n := realised()
		if n == 0 || avg <= 0 {
			return 0, "", false
		}
		return avg, fmt.Sprintf("mean of the %d cycles you traded in the last year, backed out through the fee model", n), true
	}

	if m.market != nil && m.market.Current.Spread > 0 {
		return m.market.Current.Spread / 100, "live market feed", true
	}
	avg, n := realised()
	if n == 0 || avg <= 0 {
		return 0, "", false
	}
	return avg, fmt.Sprintf("mean of your last %d cycles — no live feed in CSV mode, so this is the realised figure", n), true
}

// currentCapital is the in-flight cycle's capital when live, else the latest
// cycle's ZAR in — the row marked "◀ now" on the ladder.
func (m returnsModel) currentCapital() float64 {
	if m.client != nil && m.client.Status.AmountInvested > 0 {
		return m.client.Status.AmountInvested
	}
	var latest time.Time
	var capital float64
	for _, c := range m.cycles {
		if !c.StartDate.Before(latest) {
			latest, capital = c.StartDate, c.ZarIn
		}
	}
	return capital
}

// capitals is the ladder with the current capital slotted in (deduped to the
// nearest rand so it doesn't sit next to an identical rung).
func (m returnsModel) capitals() (list []float64, now float64) {
	now = m.currentCapital()
	list = append(list, ladder...)
	if now > 0 {
		list = append(list, now)
		sort.Float64s(list)
		out := list[:1]
		for _, v := range list[1:] {
			if v-out[len(out)-1] >= 1 {
				out = append(out, v)
			} else if v == now {
				out[len(out)-1] = now // keep the exact figure, drop the rung
			}
		}
		list = out
	}
	return list, now
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
			rightPad("You keep", wKeep))
	b.WriteString(header + "\n")

	capitals, now := m.capitals()
	for _, capital := range capitals {
		p := m.fees.Project(spread, capital)
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
			rightPad(tierPct(p.TierRate), wTierPct) +
			rightPad(charged(p.SuccessFee), wFFFee) +
			rightPad(colourMoney(p.NetProfit), wNetP) +
			rightPad(colourReturn(p.NetReturn), wNetRet) +
			rightPad(keep, wKeep)
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

// renderFeeModel spells out every constituent part of the fee figures above, in
// statement order, so the net column can be checked by hand.
func (m returnsModel) renderFeeModel(spread float64) string {
	f := m.fees
	row := func(label, val, note string) string {
		return labelStyle.Render(pad(label, 24)) + valueStyle.Render(pad(val, 28)) + dimStyle.Render(note)
	}
	var lines []string

	lines = append(lines, titleStyle.Render("fee model")+
		dimStyle.Render("  per cycle, in statement order"))
	lines = append(lines, row("gross earnings", "capital × "+spreadFmt(spread*100),
		"the market spread FF trades into"))

	fixedNote := "bank admin + instant EFT"
	if f.Fixed == analytics.DefaultFees().Fixed {
		fixedNote = "Capitec admin R500.00 + instant EFT R30.00"
	}
	lines = append(lines, row("− third-party fixed", money(f.Fixed), fixedNote))
	lines = append(lines, row("− third-party variable", percent(f.Variable)+" of capital",
		"bank exchange + offshore receipt + offshore trading"))
	lines = append(lines, row("= gross profit", "earnings − those fees", "the statement's Gross Profit line"))
	lines = append(lines, row("− FF success fee", "tier % of GROSS PROFIT",
		"FF's share is taken after the third-party fees, never on a loss"))
	lines = append(lines, labelStyle.Render(pad("", 24))+dimStyle.Render(tierLadder(f)))
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

// tierLadder renders the success-fee schedule, e.g.
// "under R150k 35% · R150k+ 33% · R200k+ 30%".
func tierLadder(f analytics.Fees) string {
	if len(f.Tiers) == 0 {
		return "no success-fee tiers configured"
	}
	parts := make([]string, 0, len(f.Tiers))
	for i, t := range f.Tiers {
		label := randK(t.Min) + "+"
		if i == 0 {
			label = "up to " + randK(f.Tiers[1].Min)
			if len(f.Tiers) == 1 {
				label = "any capital"
			}
		}
		parts = append(parts, label+" "+tierPct(t.Rate))
	}
	return strings.Join(parts, " · ")
}

// tierPct renders a success-fee rate without trailing zeros: 30%, 32.5%.
func tierPct(rate float64) string { return fmt.Sprintf("%.4g%%", rate*100) }

// charged renders a fee as a deduction, and a fee that is not levied (a losing
// cycle pays FF nothing) as a plain zero rather than "-R0.00".
func charged(v float64) string {
	if v <= 0 {
		return money(0)
	}
	return "-" + money(v)
}

// randK is a compact rand amount for tier labels: R150k, R1.0m.
func randK(v float64) string {
	if v >= 1_000_000 {
		return fmt.Sprintf("R%.1fm", v/1_000_000)
	}
	return fmt.Sprintf("R%.0fk", v/1000)
}
