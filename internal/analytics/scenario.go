package analytics

import (
	"fmt"
	"sort"
	"time"

	"github.com/wolffshots/fftui/internal/model"
)

// Scenario selects which spread a returns ladder is projected at, so the
// ladder can be read at a worse and a better market than today's.
type Scenario int

const (
	ScenarioNow      Scenario = iota // live market feed (CSV mode: the trailing average)
	ScenarioLower                    // lowest spread the market actually printed recently
	ScenarioHigher                   // highest one
	ScenarioRealised                 // what the account actually caught, after execution timing
)

// Scenarios is the strip order both front ends present them in.
var Scenarios = []Scenario{ScenarioNow, ScenarioLower, ScenarioHigher, ScenarioRealised}

func (s Scenario) String() string {
	switch s {
	case ScenarioLower:
		return "lower"
	case ScenarioHigher:
		return "higher"
	case ScenarioRealised:
		return "realised"
	}
	return "now"
}

// ParseScenario maps a String() slug back to its Scenario. An unknown slug is
// the live-feed default rather than an error: it arrives from a URL.
func ParseScenario(slug string) Scenario {
	for _, s := range Scenarios {
		if s.String() == slug {
			return s
		}
	}
	return ScenarioNow
}

// ScenarioWindow is the history window (days) the lower/higher cases are the
// bounds of: long enough to have seen a bad and a good market, short enough to
// still describe the current one.
const ScenarioWindow = 30

// Ladder is the capital ladder: every FF fee-tier boundary plus round steps
// either side, so the tier jumps are visible.
var Ladder = []float64{
	50_000, 100_000, 150_000, 200_000, 250_000,
	300_000, 400_000, 500_000, 750_000, 1_000_000,
}

// ScenarioInput is everything a returns ladder is projected from. Both front
// ends build one so they cannot drift apart on a money figure.
type ScenarioInput struct {
	Cycles      []model.Cycle
	Now         time.Time
	Fees        Fees
	Live        bool                // a market feed is present; false in CSV mode
	LiveSpread  float64             // market.Current.Spread, percent units; zero or negative is a real reading
	History     []model.MarketPoint // 365d history; the 7d Market series is too short for a 30d window
	HistoryDays int                 // that history's period, in days
	Invested    float64             // the in-flight cycle's capital; 0 falls back to the latest cycle
}

// Spread resolves one scenario to a spread (a fraction of capital) and the
// label saying how it was derived. This is a money view, so a scenario with no
// input reports ok=false instead of a zero: the caller drops it rather than
// project off it.
func (in ScenarioInput) Spread(s Scenario) (frac float64, source string, ok bool) {
	realised := func() (float64, int) { return AvgSpread(in.Cycles, in.Now, in.Fees) }

	switch s {
	case ScenarioLower, ScenarioHigher:
		low, high, ok := SpreadRange(in.History, in.HistoryDays, ScenarioWindow)
		if !ok {
			return 0, "", false
		}
		if s == ScenarioLower {
			return low / 100, fmt.Sprintf("lowest spread in the last %d days of market history", ScenarioWindow), true
		}
		return high / 100, fmt.Sprintf("highest spread in the last %d days of market history", ScenarioWindow), true

	case ScenarioRealised:
		avg, n := realised()
		if n == 0 || avg <= 0 {
			return 0, "", false
		}
		return avg, fmt.Sprintf("mean of the %d cycles you traded in the last year, backed out through the fee model", n), true
	}

	if in.Live {
		return in.LiveSpread / 100, "live market feed", true
	}
	avg, n := realised()
	if n == 0 || avg <= 0 {
		return 0, "", false
	}
	return avg, fmt.Sprintf("mean of your last %d cycles — no live feed in CSV mode, so this is the realised figure", n), true
}

// Available lists the scenarios this data supports, in strip order.
func (in ScenarioInput) Available() []Scenario {
	var out []Scenario
	for _, s := range Scenarios {
		if _, _, ok := in.Spread(s); ok {
			out = append(out, s)
		}
	}
	return out
}

// CurrentCapital is the in-flight cycle's capital when live, else the latest
// cycle's ZAR in — the row marked as "now" on the ladder.
func (in ScenarioInput) CurrentCapital() float64 {
	if in.Invested > 0 {
		return in.Invested
	}
	var latest time.Time
	var capital float64
	for _, c := range in.Cycles {
		if !c.StartDate.Before(latest) {
			latest, capital = c.StartDate, c.ZarIn
		}
	}
	return capital
}

// Capitals is the ladder with the current capital slotted in (deduped to the
// nearest rand so it doesn't sit next to an identical rung).
func (in ScenarioInput) Capitals() (list []float64, now float64) {
	now = in.CurrentCapital()
	list = append(list, Ladder...)
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
