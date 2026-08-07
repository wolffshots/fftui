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

// returnsModel is view 6: what a cycle earns at each capital size for the
// CURRENT spread, run through the same fee waterfall as the cycle statements.
// The spread comes from the live market feed, or (CSV mode) from the trailing
// year of cycles backed out through the fee model.
type returnsModel struct {
	vp     viewport.Model
	fees   analytics.Fees
	cycles []model.Cycle
	now    time.Time
	market *model.MarketConditions
	client *model.ClientStatus
	width  int
	height int
}

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

func (m *returnsModel) setData(c *model.ClientStatus, mk *model.MarketConditions) {
	m.client, m.market = c, mk
}

func (m *returnsModel) setSize(w, h int) {
	m.width, m.height = w, h
	m.vp.Width, m.vp.Height = w, h
}

func (m returnsModel) update(msg tea.Msg) (returnsModel, tea.Cmd) {
	var cmd tea.Cmd
	m.vp, cmd = m.vp.Update(msg)
	return m, cmd
}

func (m returnsModel) view() string {
	vp := m.vp
	vp.SetContent(m.render())
	return vp.View()
}

// spread resolves the spread to project at: the live market feed first, else
// the trailing-year average backed out of the cycles.
func (m returnsModel) spread() (frac float64, source string, ok bool) {
	if m.market != nil && m.market.Current.Spread > 0 {
		return m.market.Current.Spread / 100, "live market feed", true
	}
	avg, n := analytics.AvgSpread(m.cycles, m.now, m.fees)
	if n == 0 || avg <= 0 {
		return 0, "", false
	}
	return avg, fmt.Sprintf("mean of your last %d cycles — no live feed in CSV mode", n), true
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
	b.WriteString(titleStyle.Render("Expected return per cycle") +
		dimStyle.Render("  at a gross-earnings spread of ") +
		valueStyle.Render(spreadFmt(spread*100)) +
		dimStyle.Render("  ("+source+")") + "\n\n")

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
