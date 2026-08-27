package analytics

import (
	"time"

	"github.com/wolffshots/fftui/internal/model"
)

// Allowances configures the annual exchange-control caps the arb capital
// cycles through. Future Forex trades against the COMBINED capacity — they
// file the AIT applications and prioritise operating on the AIT allowance,
// falling back to the clearance-free SDA — so planning treats SDA+AIT as one
// annual pool.
// When Live is true the API's actual remaining balances are used instead of
// inferring usage from the cycle history (the API also sees in-flight cycles
// and non-arb transfers).
type Allowances struct {
	SDALimit float64
	AITLimit float64

	Live         bool
	SDAAvailable float64
	AITAvailable float64
	// Usage straight from the API, which knows about the reserved hold and
	// any non-arb transfers the cycle history cannot see.
	SDAUsed     float64
	SDAReserved float64
	AITUsed     float64
}

// WithLive folds a live client snapshot into the configured allowances: FF's
// own caps, usage and reserved hold replace the configured figures. A nil
// snapshot leaves them untouched, so CSV mode keeps inferring from cycles.
func (a Allowances) WithLive(c *model.ClientStatus) Allowances {
	if c == nil {
		return a
	}
	a.Live = true
	a.SDAAvailable, a.AITAvailable = c.SDAAvailable, c.AITAvailable
	a.SDAUsed, a.SDAReserved = c.SDADetail.Used, c.SDADetail.Reserved
	if l := c.SDADetail.Limit; l > 0 {
		a.SDALimit = l
	}
	if l := c.AITDetail.Limit; l > 0 {
		a.AITLimit = l
		a.AITUsed = l - c.AITAvailable
	}
	return a
}

// Total is the combined annual allowance pool.
func (a Allowances) Total() float64 { return a.SDALimit + a.AITLimit }

// Planning bundles the fiscal / capital-planning figures shown at the bottom
// of the analytics view: taxable profit in the current SA tax year, combined
// allowance usage and runway for the calendar year, and the marginal value of
// extra capital.
type Planning struct {
	// Tax uses realisation accounting: a cycle's whole profit lands in the tax
	// year its EndDate falls in (unlike the rate buckets, which prorate across
	// boundaries for rate fairness) — that is what a provisional return needs.
	TaxYearLabel  string
	TaxYearProfit float64
	EstimatedTax  float64

	// Combined SDA+AIT runway for the current CALENDAR year: every cycle sends
	// its ZarIn offshore afresh, so each consumes that much allowance again.
	// Used/Remaining come from the live balances when available, otherwise
	// from summing the year's cycle ZarIns against the configured limits.
	TotalLimit  float64
	Used        float64
	Remaining   float64
	Reserved    float64 // live only: SDA held against cycles in flight, neither used nor available
	Live        bool
	Exhausted   bool
	ExhaustDate time.Time // projected exhaustion at the year-to-date pace
	HasExhaust  bool      // false when already exhausted, no pace, or the year resets first

	// Capital productivity, measured over the trailing 365 days.
	CyclesPerYear  int
	AvgReturn      float64 // mean per-cycle fractional return (trailing)
	CurrentCapital float64 // latest cycle's ZarIn
	// SweetSpot is the per-cycle capital above which the combined allowance
	// runs out before the year does (total / cycles-per-year).
	SweetSpot float64

	// Fee-aware projections: the mean gross-earnings market spread is backed
	// out of the trailing cycles through the fee waterfall, then net returns
	// are projected at other capital sizes — bigger cycles dilute the fixed
	// fees and climb to a lower FF tier, so the net return improves with size.
	AvgSpread   float64 // mean gross-earnings spread per cycle (trailing)
	CurrentTier float64 // FF share of gross profit at CurrentCapital
	TopTier     float64 // FF share at the top (cheapest) tier
	TopTierMin  float64 // capital where the top tier starts
	ReturnNow   float64 // modelled net return/cycle at CurrentCapital
	ReturnAtTop float64 // modelled net return/cycle at TopTierMin
	// Extra100k is the projected change in ANNUAL profit from adding R100k of
	// cycle capital: below the sweet spot the extra rand compounds through
	// every cycle (and improves the fee position); above it, deployment is
	// allowance-capped, so only the fee-position improvement remains.
	Extra100kGross float64 // pre-tax rand per year
	Extra100kNet   float64 // after tax
}

// AvgSpread backs the gross-earnings market spread out of every cycle that
// started in the trailing 365 days and averages it, with the cycle count. It is
// the CSV-mode stand-in for the live market spread.
func AvgSpread(cs []model.Cycle, now time.Time, fees Fees) (float64, int) {
	cutoff := now.AddDate(0, 0, -365)
	var sum float64
	var n int
	for _, c := range cs {
		if c.StartDate.After(cutoff) && !c.StartDate.After(now) {
			sum += fees.Spread(c.NetProfit, c.ZarIn)
			n++
		}
	}
	if n == 0 {
		return 0, 0
	}
	return sum / float64(n), n
}

// Plan computes the planning figures as of `now`. A zero-total Allowances
// disables the runway and sweet-spot figures (they stay zero).
func Plan(cs []model.Cycle, now time.Time, r Rates, a Allowances, fees Fees) Planning {
	p := Planning{TotalLimit: a.Total(), Live: a.Live}

	tyStart := periodStart(TaxYear, now)
	p.TaxYearLabel = periodLabel(TaxYear, tyStart)
	for _, c := range cs {
		if periodStart(TaxYear, c.EndDate).Equal(tyStart) {
			p.TaxYearProfit += c.NetProfit
		}
	}
	p.EstimatedTax = p.TaxYearProfit * r.Tax

	// Trailing-365-day cadence and return, and the latest cycle's capital.
	cutoff := now.AddDate(0, 0, -365)
	var retSum float64
	var latest time.Time
	for _, c := range cs {
		if c.StartDate.After(cutoff) && !c.StartDate.After(now) {
			p.CyclesPerYear++
			retSum += c.Return()
		}
		if !c.StartDate.Before(latest) {
			latest = c.StartDate
			p.CurrentCapital = c.ZarIn
		}
	}
	if p.CyclesPerYear > 0 {
		p.AvgReturn = retSum / float64(p.CyclesPerYear)
	}
	p.AvgSpread, _ = AvgSpread(cs, now, fees)

	if p.TotalLimit > 0 {
		year := now.Year()
		if a.Live {
			p.Remaining = a.SDAAvailable + a.AITAvailable
			p.Reserved = a.SDAReserved
			if used := a.SDAUsed + a.AITUsed; used > 0 {
				p.Used = used // FF's own figure, which excludes the reserved hold
			} else {
				p.Used = p.TotalLimit - p.Remaining
			}
		} else {
			for _, c := range cs {
				if c.StartDate.Year() == year && !c.StartDate.After(now) {
					p.Used += c.ZarIn
				}
			}
			p.Remaining = p.TotalLimit - p.Used
		}
		if p.Remaining <= 0 {
			p.Remaining = 0
			p.Exhausted = true
		}
		// Project exhaustion at the year-to-date burn rate; if the projection
		// lands after 31 December the allowances reset first.
		jan1 := time.Date(year, 1, 1, 0, 0, 0, 0, time.UTC)
		elapsed := now.Sub(jan1).Hours()/24 + 1
		if !p.Exhausted && p.Used > 0 && elapsed > 0 {
			daysLeft := p.Remaining / (p.Used / elapsed)
			d := now.AddDate(0, 0, int(daysLeft))
			if d.Year() == year {
				p.ExhaustDate = d
				p.HasExhaust = true
			}
		}
		if p.CyclesPerYear > 0 {
			p.SweetSpot = p.TotalLimit / float64(p.CyclesPerYear)
		}
	}

	// Fee-aware capital projections.
	if p.CyclesPerYear > 0 && p.CurrentCapital > 0 {
		p.CurrentTier = fees.TierRate(p.CurrentCapital)
		if n := len(fees.Tiers); n > 0 {
			p.TopTierMin = fees.Tiers[n-1].Min
			p.TopTier = fees.Tiers[n-1].Rate
			p.ReturnAtTop = fees.Net(p.AvgSpread, p.TopTierMin) / p.TopTierMin
		}
		p.ReturnNow = fees.Net(p.AvgSpread, p.CurrentCapital) / p.CurrentCapital

		annual := func(capital float64) float64 {
			if p.SweetSpot > 0 && capital > p.SweetSpot {
				// Allowance-bound: deployed rand per year is capped at the pool,
				// but bigger cycles still improve the net return rate on it.
				return fees.Net(p.AvgSpread, capital) / capital * p.TotalLimit
			}
			return fees.Net(p.AvgSpread, capital) * float64(p.CyclesPerYear)
		}
		p.Extra100kGross = annual(p.CurrentCapital+100_000) - annual(p.CurrentCapital)
		p.Extra100kNet = p.Extra100kGross * (1 - r.Tax)
	}
	return p
}
