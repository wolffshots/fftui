package analytics

import (
	"time"

	"github.com/wolffshots/fftui/internal/model"
)

// Allowances configures the annual exchange-control caps the arb capital
// cycles through. Future Forex trades against the COMBINED capacity — they
// file the FIA/AIT applications and prioritise operating on FIA, falling back
// to the clearance-free SDA — so planning treats SDA+FIA as one annual pool.
// When Live is true the API's actual remaining balances are used instead of
// inferring usage from the cycle history (the API also sees in-flight cycles
// and non-arb transfers).
type Allowances struct {
	SDALimit float64
	FIALimit float64

	Live         bool
	SDAAvailable float64
	FIAAvailable float64
}

// Total is the combined annual allowance pool.
func (a Allowances) Total() float64 { return a.SDALimit + a.FIALimit }

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

	// Combined SDA+FIA runway for the current CALENDAR year: every cycle sends
	// its ZarIn offshore afresh, so each consumes that much allowance again.
	// Used/Remaining come from the live balances when available, otherwise
	// from summing the year's cycle ZarIns against the configured limits.
	TotalLimit  float64
	Used        float64
	Remaining   float64
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
	var retSum, spreadSum float64
	var latest time.Time
	for _, c := range cs {
		if c.StartDate.After(cutoff) && !c.StartDate.After(now) {
			p.CyclesPerYear++
			retSum += c.Return()
			spreadSum += fees.Spread(c.NetProfit, c.ZarIn)
		}
		if !c.StartDate.Before(latest) {
			latest = c.StartDate
			p.CurrentCapital = c.ZarIn
		}
	}
	if p.CyclesPerYear > 0 {
		p.AvgReturn = retSum / float64(p.CyclesPerYear)
		p.AvgSpread = spreadSum / float64(p.CyclesPerYear)
	}

	if p.TotalLimit > 0 {
		year := now.Year()
		if a.Live {
			p.Remaining = a.SDAAvailable + a.FIAAvailable
			p.Used = p.TotalLimit - p.Remaining
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
