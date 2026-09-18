package analytics

import (
	"context"
	"math"
	"sort"
	"testing"
	"time"

	"github.com/wolffshots/fftui/internal/model"
)

// refNow is the reference "today" the pinned figures were computed against, to
// match the fictional demo dataset in testdata/cycles.csv (last cycle 2026-07-07).
var refNow = time.Date(2026, 7, 10, 0, 0, 0, 0, time.UTC)

// refRates are the overlays used for with-idle / after-tax assertions.
var refRates = Rates{Idle: 0.06, Tax: 0.41}

func loadCycles(t *testing.T) []model.Cycle {
	t.Helper()
	cs, err := model.NewCSVSource("../../testdata/cycles.csv").Fetch(context.Background())
	if err != nil {
		t.Fatalf("load csv: %v", err)
	}
	return cs
}

// pct converts a fractional value to percentage points for comparison.
func pct(f float64) float64 { return f * 100 }

func assertClose(t *testing.T, name string, got, want, tol float64) {
	t.Helper()
	if math.Abs(got-want) > tol {
		t.Errorf("%s: got %.3f, want %.3f (±%.2f)", name, got, want, tol)
	}
}

func TestLifetimeReferenceValues(t *testing.T) {
	cs := loadCycles(t)
	if len(cs) != 43 {
		t.Fatalf("cycle count: got %d, want 43", len(cs))
	}
	s := Lifetime(cs, refRates)

	assertClose(t, "total net profit", s.TotalProfit, 19422.50, 0.01)
	assertClose(t, "avg return/cycle %", pct(s.AvgReturn), 0.414, 0.01)
	assertClose(t, "lifetime compound %", pct(s.Compound), 19.42, 0.1)
	if s.CalendarDays != 665 {
		t.Errorf("lifetime calendar days: got %d, want 665", s.CalendarDays)
	}
	// All annualised figures are nominal p.a. compounded monthly — lower than the
	// effective-annual equivalent by the EAR→nominal conversion (10.23% EAR →
	// 9.78% nominal). The underlying compound growth (19.42%) is unchanged.
	assertClose(t, "lifetime annualised %", pct(s.Annualised), 9.78, 0.1)
	// With idle cash (6% nominal/monthly) on the non-trading days.
	assertClose(t, "lifetime annualised +idle %", pct(s.AnnualisedWithIdle), 15.13, 0.1)
	// After 41% tax on returns.
	assertClose(t, "lifetime annualised after-tax %", pct(s.AnnualisedAfterTax), 5.77, 0.1)
	assertClose(t, "lifetime take-home (+idle, after-tax) %", pct(s.AnnualisedWithIdleAfterTax), 8.91, 0.1)
	// Distinct calendar days with a cycle open. The demo schedule has no same-day
	// rollovers, so this equals Σ HoldDays (77).
	if s.TradingDays != 77 {
		t.Errorf("lifetime trading days: got %d, want 77", s.TradingDays)
	}
	// Ordering must hold: arb ≤ +idle, and after-tax ≤ pre-tax.
	if !(s.Annualised <= s.AnnualisedWithIdle && s.AnnualisedAfterTax <= s.Annualised &&
		s.AnnualisedWithIdleAfterTax <= s.AnnualisedWithIdle) {
		t.Errorf("figure ordering violated: arb=%.4f +idle=%.4f arbTax=%.4f netTax=%.4f",
			s.Annualised, s.AnnualisedWithIdle, s.AnnualisedAfterTax, s.AnnualisedWithIdleAfterTax)
	}
}

func TestZeroRatesCollapseToArbOnly(t *testing.T) {
	cs := loadCycles(t)
	s := Lifetime(cs, Rates{}) // idle 0, tax 0
	for _, got := range []float64{s.AnnualisedWithIdle, s.AnnualisedAfterTax, s.AnnualisedWithIdleAfterTax} {
		if math.Abs(got-s.Annualised) > 1e-9 {
			t.Errorf("zero rates should equal arb-only %.6f, got %.6f", s.Annualised, got)
		}
	}
}

func TestDeadBucketRates(t *testing.T) {
	cs := loadCycles(t)
	// A zero-cycle month earns the idle rate pre-tax and idle*(1-tax) after tax.
	for _, b := range Buckets(cs, Month, refNow, true, refRates) {
		if b.Count == 0 && !b.Partial {
			assertClose(t, "dead +idle % ("+b.Label+")", pct(b.AnnualisedWithIdle), 6.0, 0.01)
			assertClose(t, "dead net % ("+b.Label+")", pct(b.AnnualisedWithIdleAfterTax), 6.0*(1-0.41), 0.01)
		}
	}
}

func TestYearlyReferenceValues(t *testing.T) {
	cs := loadCycles(t)
	yearly := Buckets(cs, Year, refNow, false, refRates)
	byLabel := map[string]Bucket{}
	for _, b := range yearly {
		byLabel[b.Label] = b
	}

	// Nominal p.a. compounded monthly (see TestLifetimeReferenceValues).
	assertClose(t, "2025 annualised %", pct(byLabel["2025"].Annualised), 9.16, 0.1)
	assertClose(t, "2024 annualised % (partial)", pct(byLabel["2024"].Annualised), 2.79, 0.2)
	assertClose(t, "2026 YTD annualised %", pct(byLabel["2026"].Annualised), 11.19, 0.2)

	// 2025 is a complete year → 365 calendar days, not flagged partial.
	if byLabel["2025"].CalendarDays != 365 {
		t.Errorf("2025 calendar days: got %d, want 365", byLabel["2025"].CalendarDays)
	}
}

func TestFullYearFloor(t *testing.T) {
	cs := loadCycles(t)
	byLabel := map[string]Bucket{}
	for _, b := range Buckets(cs, Year, refNow, false, refRates) {
		byLabel[b.Label] = b
	}

	// 2026 is in progress: the floor treats the remainder of the year as idle,
	// so it sits below the extrapolated with-idle figure. +idle floor 11.47%,
	// net floor 6.76%.
	y26 := byLabel["2026"]
	if !y26.InProgress {
		t.Error("2026 should be InProgress")
	}
	assertClose(t, "2026 floor +idle %", pct(y26.AnnualisedFloor), 11.47, 0.1)
	assertClose(t, "2026 floor net %", pct(y26.AnnualisedFloorAfterTax), 6.76, 0.1)
	if !(y26.AnnualisedFloor < y26.AnnualisedWithIdle) {
		t.Errorf("floor (%.3f) should be below extrapolated with-idle (%.3f)",
			y26.AnnualisedFloor, y26.AnnualisedWithIdle)
	}

	// A completed year has no remainder, so its floor equals its with-idle /
	// net figures exactly, and it is not InProgress.
	y25 := byLabel["2025"]
	if y25.InProgress {
		t.Error("2025 (completed) should not be InProgress")
	}
	if math.Abs(y25.AnnualisedFloor-y25.AnnualisedWithIdle) > 1e-9 ||
		math.Abs(y25.AnnualisedFloorAfterTax-y25.AnnualisedWithIdleAfterTax) > 1e-9 {
		t.Errorf("completed-year floor should equal with-idle/net: %.4f/%.4f vs %.4f/%.4f",
			y25.AnnualisedFloor, y25.AnnualisedFloorAfterTax,
			y25.AnnualisedWithIdle, y25.AnnualisedWithIdleAfterTax)
	}
}

func TestMonthlyVariance(t *testing.T) {
	cs := loadCycles(t)
	buckets := Buckets(cs, Month, refNow, false, refRates) // active only
	v := Variance(buckets)

	// Regression pins, arb-only, nominal p.a. compounded monthly. The partial
	// current month (July 2026, 10 elapsed days) is excluded from the stats.
	assertClose(t, "monthly mean %", pct(v.Mean), 10.14, 0.1)
	assertClose(t, "monthly median %", pct(v.Median), 9.73, 0.1)
	assertClose(t, "monthly std pts", pct(v.Std), 1.57, 0.1)
}

func TestQuarterlyVariance(t *testing.T) {
	cs := loadCycles(t)
	buckets := Buckets(cs, Quarter, refNow, false, refRates) // active only
	v := Variance(buckets)

	// Regression pins, arb-only, nominal p.a. compounded monthly. The partial
	// current quarter (Q3 2026, 10 elapsed days) is excluded from the stats.
	assertClose(t, "quarterly mean %", pct(v.Mean), 8.47, 0.1)
	assertClose(t, "quarterly median %", pct(v.Median), 9.50, 0.1)
	assertClose(t, "quarterly std pts", pct(v.Std), 2.43, 0.1)
}

func TestPartialPeriodsFlagged(t *testing.T) {
	cs := loadCycles(t)
	// The current month and quarter (July / Q3 2026) span only ~10 elapsed days
	// and must be flagged partial.
	for _, gran := range []Granularity{Month, Quarter} {
		buckets := Buckets(cs, gran, refNow, false, refRates)
		last := buckets[len(buckets)-1]
		if !last.Partial {
			t.Errorf("%s: last bucket %q not flagged partial (days=%d)", gran, last.Label, last.CalendarDays)
		}
	}
}

// mkCycle builds a synthetic cycle from date strings for edge-case tests.
func mkCycle(t *testing.T, code, start, end string, in, out float64) model.Cycle {
	t.Helper()
	s, err := time.Parse("2006-01-02", start)
	if err != nil {
		t.Fatal(err)
	}
	e, err := time.Parse("2006-01-02", end)
	if err != nil {
		t.Fatal(err)
	}
	return model.Cycle{Code: code, StartDate: s, EndDate: e, ZarIn: in, ZarOut: out, NetProfit: out - in}
}

// TestSameDayRolloverCountsOnce: when one cycle ends the day the next starts,
// that day is a single trading day, not two.
func TestSameDayRolloverCountsOnce(t *testing.T) {
	cs := []model.Cycle{
		mkCycle(t, "A", "2025-01-13", "2025-01-14", 100000, 100500),
		mkCycle(t, "B", "2025-01-14", "2025-01-15", 100500, 101000),
	}
	s := Lifetime(cs, Rates{})
	if s.TradingDays != 3 { // 13th, 14th, 15th — not 2+2
		t.Errorf("trading days: got %d, want 3", s.TradingDays)
	}
}

// TestBoundarySpanningCycleProrated: a cycle crossing a month boundary splits
// between the months by days spent in each — growth geometrically, profit
// linearly — and the months' compound factors multiply back to the full return.
func TestBoundarySpanningCycleProrated(t *testing.T) {
	cs := []model.Cycle{mkCycle(t, "X", "2025-01-30", "2025-02-02", 100000, 101000)} // 1% over 4 days
	now := time.Date(2025, 3, 1, 0, 0, 0, 0, time.UTC)
	buckets := Buckets(cs, Month, now, false, Rates{})
	if len(buckets) != 2 {
		t.Fatalf("want 2 buckets (Jan active by start, Feb active by overlap), got %d", len(buckets))
	}
	jan, feb := buckets[0], buckets[1]
	if jan.Count != 1 || feb.Count != 0 {
		t.Errorf("count: jan=%d feb=%d, want 1/0 (counted where started)", jan.Count, feb.Count)
	}
	// 2 of 4 hold days in each month.
	assertClose(t, "jan profit", jan.TotalProfit, 500, 1e-9)
	assertClose(t, "feb profit", feb.TotalProfit, 500, 1e-9)
	if jan.TradingDays != 2 || feb.TradingDays != 2 {
		t.Errorf("trading days: jan=%d feb=%d, want 2/2", jan.TradingDays, feb.TradingDays)
	}
	recombined := (1+jan.Compound)*(1+feb.Compound) - 1
	assertClose(t, "recombined growth", recombined, 0.01, 1e-12)
}

// TestVarianceExcludesPartialBuckets: the ⚠-flagged partial bucket must not
// feed the mean/median/std/min/max it is documented as too unreliable for.
func TestVarianceExcludesPartialBuckets(t *testing.T) {
	cs := loadCycles(t)
	buckets := Buckets(cs, Month, refNow, false, refRates)
	var nFull, nPartial int
	for _, b := range buckets {
		if b.Partial {
			nPartial++
		} else {
			nFull++
		}
	}
	if nPartial == 0 {
		t.Fatal("expected at least one partial bucket at refNow (July 2026)")
	}
	if v := Variance(buckets); v.N != nFull {
		t.Errorf("variance N: got %d, want %d (partials excluded)", v.N, nFull)
	}
}

func TestIncludeDeadLowersMedian(t *testing.T) {
	cs := loadCycles(t)
	active := Variance(Buckets(cs, Month, refNow, false, refRates))
	withDead := Variance(Buckets(cs, Month, refNow, true, refRates))
	if !(withDead.Median < active.Median) {
		t.Errorf("incl-dead median (%.3f) should be below active-only (%.3f)", withDead.Median, active.Median)
	}
	if withDead.N <= active.N {
		t.Errorf("incl-dead should have more buckets: %d vs %d", withDead.N, active.N)
	}
}

// TestScenarioInput covers the shared spread resolution both front ends
// project their returns ladder from: the live feed wins for "now", the
// observed 30-day bounds come from the market history, and a scenario with no
// input behind it is dropped rather than projected off a zero.
func TestScenarioInput(t *testing.T) {
	now := time.Date(2026, 7, 10, 0, 0, 0, 0, time.UTC)
	hist := make([]model.MarketPoint, 30)
	for i := range hist {
		hist[i] = model.MarketPoint{Spread: 0.50 + float64(i)*0.05} // 0.50% … 1.95%
	}
	in := ScenarioInput{
		Cycles:      []model.Cycle{{StartDate: now.AddDate(0, 0, -30), ZarIn: 200_000, NetProfit: 1_000}},
		Now:         now,
		Fees:        DefaultFees(),
		LiveSpread:  0.80,
		History:     hist,
		HistoryDays: 30,
		Invested:    123_456,
	}

	for _, tc := range []struct {
		s    Scenario
		want float64
	}{
		{ScenarioNow, 0.0080},
		{ScenarioLower, 0.0050},
		{ScenarioHigher, 0.0195},
	} {
		got, src, ok := in.Spread(tc.s)
		if !ok {
			t.Fatalf("%v: no spread", tc.s)
		}
		if math.Abs(got-tc.want) > 1e-9 {
			t.Errorf("%v spread = %g, want %g", tc.s, got, tc.want)
		}
		if src == "" {
			t.Errorf("%v: empty source label", tc.s)
		}
	}
	if len(in.Available()) != 4 {
		t.Errorf("full data should offer every scenario, got %v", in.Available())
	}

	// No history and no live feed: only the realised figure survives.
	bare := ScenarioInput{Cycles: in.Cycles, Now: now, Fees: in.Fees}
	if got := bare.Available(); len(got) != 2 || got[0] != ScenarioNow || got[1] != ScenarioRealised {
		t.Errorf("CSV mode should offer now + realised only, got %v", got)
	}
	if _, _, ok := bare.Spread(ScenarioLower); ok {
		t.Error("lower must not resolve without market history")
	}
	// No cycles either: nothing to project off at all.
	if got := (ScenarioInput{Now: now, Fees: in.Fees}).Available(); len(got) != 0 {
		t.Errorf("no data should offer no scenarios, got %v", got)
	}

	// The current capital slots into the ladder in place, keeping its exact
	// figure rather than sitting beside a near-identical rung.
	caps, cur := in.Capitals()
	if cur != 123_456 {
		t.Errorf("current capital = %v, want the invested amount", cur)
	}
	if len(caps) != len(Ladder)+1 {
		t.Errorf("capitals = %d rungs, want the ladder plus one", len(caps))
	}
	if !sort.Float64sAreSorted(caps) {
		t.Errorf("capitals not sorted: %v", caps)
	}
	// An invested amount already on the ladder replaces the rung, not adds to it.
	on := in
	on.Invested = 200_000
	if caps, _ := on.Capitals(); len(caps) != len(Ladder) {
		t.Errorf("a capital already on the ladder should not add a rung, got %d", len(caps))
	}

	if got := ParseScenario("higher"); got != ScenarioHigher {
		t.Errorf("ParseScenario(higher) = %v", got)
	}
	if got := ParseScenario("nonsense"); got != ScenarioNow {
		t.Errorf("an unknown slug should default to now, got %v", got)
	}
}
