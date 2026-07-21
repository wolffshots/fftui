package analytics

import (
	"math/rand"
	"sort"

	"github.com/wolffshots/fftui/internal/model"
)

// CI is a confidence band on an annualised rate (fractions).
type CI struct{ Lo, Hi float64 }

// BootstrapResult carries the resampled 90% bands for the lifetime figures.
type BootstrapResult struct {
	Arb CI  // arb-only annualised
	Net CI  // with-idle, after-tax (take-home)
	N   int // resamples
	OK  bool
}

// Bootstrap resamples the per-cycle returns with replacement — keeping the
// observed timeline (calendar span, trading days, cycle count) fixed — and
// reports the 5th–95th percentile band of the recomputed lifetime annualised
// figures. It answers "how much does the headline rate depend on which cycles
// happened to land", at the observed cadence; cadence variability itself is
// deliberately not resampled. The RNG seed is fixed so the band is stable
// across refreshes. OK is false with fewer than 8 cycles (the band would be
// meaninglessly wide) or fewer than 100 resamples.
func Bootstrap(cs []model.Cycle, r Rates, resamples int) BootstrapResult {
	if len(cs) < 8 || resamples < 100 {
		return BootstrapResult{}
	}
	sorted := sortedByStart(cs)
	first := sorted[0].StartDate
	last := sorted[0].EndDate
	for _, c := range sorted {
		if c.EndDate.After(last) {
			last = c.EndDate
		}
	}
	days := int(last.Sub(first).Hours()/24 + 0.5)
	tradingDays := distinctTradingDays(sorted, first, last)
	returns := make([]float64, len(sorted))
	for i, c := range sorted {
		returns[i] = c.Return()
	}

	rng := rand.New(rand.NewSource(1)) // fixed seed: stable UI, still i.i.d. draws
	arb := make([]float64, resamples)
	net := make([]float64, resamples)
	for i := range arb {
		g, gTax := 1.0, 1.0
		for range returns {
			ret := returns[rng.Intn(len(returns))]
			g *= 1 + ret
			gTax *= 1 + ret*(1-r.Tax)
		}
		arb[i] = annualise(g-1, days)
		net[i] = annualiseWithIdle(gTax-1, tradingDays, days, r.Idle*(1-r.Tax))
	}
	sort.Float64s(arb)
	sort.Float64s(net)
	lo, hi := resamples*5/100, resamples*95/100
	if hi >= resamples {
		hi = resamples - 1
	}
	return BootstrapResult{
		Arb: CI{arb[lo], arb[hi]},
		Net: CI{net[lo], net[hi]},
		N:   resamples,
		OK:  true,
	}
}
