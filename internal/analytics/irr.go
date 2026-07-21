package analytics

import (
	"math"
	"time"

	"github.com/wolffshots/fftui/internal/model"
)

// MoneyWeighted computes the money-weighted return (XIRR) of the account
// implied by the cycles, quoted in the usual nominal-monthly convention.
//
// The cash flows are the account's EXTERNAL flows, not the per-cycle ones:
// each cycle's ZarOut is assumed to sit in the account at 0% and be redeployed
// into the next cycle, so only the difference at each cycle start is an
// inferred deposit (ZarIn above the cash on hand) or withdrawal (below), plus
// the initial ZarIn and the final value coming back. Idle days therefore drag
// the rate down exactly as in the arb-only Annualised — the two coincide when
// capital compounds cleanly (ZarIn = previous ZarOut), and diverge when cycle
// sizes jump (deposits), because XIRR weights each stretch by the capital that
// was actually in it. (Naive XIRR over the raw ±cycle flows would instead
// measure the rate while deployed — a huge, non-comparable number.)
//
// ok is false when there is no data or no meaningful root (e.g. all flows on
// one day, or the rate falls outside a sane bracket).
func MoneyWeighted(cs []model.Cycle) (rate float64, ok bool) {
	if len(cs) == 0 {
		return 0, false
	}
	sorted := sortedByStart(cs)
	first := sorted[0].StartDate

	type flow struct{ days, amt float64 }
	flows := make([]flow, 0, len(sorted)+1)
	addFlow := func(when time.Time, amt float64) {
		if amt != 0 {
			flows = append(flows, flow{when.Sub(first).Hours() / 24, amt})
		}
	}

	// pending holds earlier cycles whose ZarOut hasn't been redeployed yet. At
	// each cycle start, payouts that have landed by then (end ≤ start, so a
	// same-day rollover counts; a cycle can't fund its own start) become cash;
	// the gap between cash and ZarIn is the inferred external flow.
	var pending []model.Cycle
	for _, c := range sorted {
		cash := 0.0
		kept := pending[:0]
		for _, p := range pending {
			if !p.EndDate.After(c.StartDate) {
				cash += p.ZarOut
			} else {
				kept = append(kept, p)
			}
		}
		pending = append(kept, c)
		addFlow(c.StartDate, cash-c.ZarIn)
	}
	// Whatever is still deployed (or parked) comes back at its own end date.
	for _, p := range pending {
		addFlow(p.EndDate, p.ZarOut)
	}

	// NPV as a function of the effective annual rate.
	npv := func(ear float64) float64 {
		var v float64
		for _, f := range flows {
			v += f.amt * math.Pow(1+ear, -f.days/daysPerYear)
		}
		return v
	}

	// Bisection over a wide bracket. As ear→-1 the latest (positive, final
	// value) flow dominates so npv→+∞; as ear→∞ only the undiscounted day-0
	// flow (negative, the first ZarIn) survives so npv<0. If the signs don't
	// actually bracket a root (degenerate flows), report no figure rather than
	// a wild one.
	lo, hi := -0.9999, 1000.0
	fLo, fHi := npv(lo), npv(hi)
	if math.IsNaN(fLo) || math.IsNaN(fHi) || fLo*fHi >= 0 {
		return 0, false
	}
	for i := 0; i < 200; i++ {
		mid := (lo + hi) / 2
		if npv(mid)*fLo > 0 {
			lo = mid
		} else {
			hi = mid
		}
	}
	ear := (lo + hi) / 2
	return toNominalMonthly(1 + ear), true
}
