// Package analytics turns a set of cycles into savings-comparable annualised
// returns. The headline annualised figures are always computed over a bucket's
// elapsed CALENDAR days (not holding days), so idle time between cycles honestly
// drags the rate down. Holding-days annualisation lives only on model.Cycle and
// is used only by the detail view.
//
// Reporting convention: every rate is a NOMINAL annual rate compounded monthly
// (as a bank quotes "6% p.a. compounded monthly"), so the idle account reads
// back as exactly its input rate and all figures are directly comparable to it.
// Internally the maths works in effective growth factors and converts to the
// nominal-monthly quote only at the end (see toNominalMonthly).
package analytics

import (
	"math"
	"sort"
	"time"

	"github.com/wolffshots/fftui/internal/model"
)

// PartialDayThreshold: buckets covering fewer elapsed days than this have an
// exploding ^(365/days) term and an unreliable annualised figure (§4.4).
const PartialDayThreshold = 20

// Rates configures the two overlays on the raw arb figures: Idle is the annual
// rate earned on non-trading days; Tax is the marginal rate on all returns
// (both arb profit and idle interest). Both are fractional (0.06, 0.41).
type Rates struct {
	Idle float64
	Tax  float64
}

// Granularity selects the calendar bucket size for Buckets.
type Granularity int

const (
	Year Granularity = iota
	Quarter
	Month
)

func (g Granularity) String() string {
	switch g {
	case Year:
		return "Year"
	case Quarter:
		return "Quarter"
	default:
		return "Month"
	}
}

// Bucket is one aggregated period (a month, quarter, year, or the whole life).
type Bucket struct {
	Label        string
	Start        time.Time // calendar start of the period
	End          time.Time // calendar end of the period
	Count        int
	TotalProfit  float64
	Compound     float64 // G = Π(1+rᵢ) - 1
	CalendarDays int
	Annualised   float64 // idle days treated as 0% (savings-comparable arb-only rate)
	// AnnualisedWithIdle also credits the configured idle rate on days the
	// capital is not in a trade — "what if idle cash earned the reserve rate".
	AnnualisedWithIdle float64
	// After-tax variants: returns (arb + idle) scaled by (1-Tax) — the effective
	// take-home. AnnualisedAfterTax taxes arb-only; AnnualisedWithIdleAfterTax
	// taxes the with-idle blend (the true net).
	AnnualisedAfterTax         float64
	AnnualisedWithIdleAfterTax float64
	// Floor figures: the with-idle (and after-tax) annualised computed over the
	// bucket's FULL calendar span instead of only the elapsed days — i.e. the
	// actual trades so far plus idle for the entire remainder of the period, with
	// NO extrapolation of the trading pace. This is the pessimistic bound the
	// real period should beat (more trades will replace idle days). For a
	// completed bucket it equals the with-idle / net figures.
	AnnualisedFloor         float64
	AnnualisedFloorAfterTax float64
	TradingDays             int  // distinct calendar days with a cycle open (within the bucket)
	Partial                 bool // CalendarDays < PartialDayThreshold
	InProgress              bool // now falls within [Start, End]; floor differs from annualised
}

// Summary is the lifetime roll-up shown in table footers and the "All" bucket.
type Summary struct {
	Count                      int
	TotalProfit                float64
	AvgReturn                  float64 // mean per-cycle fractional return
	Compound                   float64 // lifetime compound growth
	CalendarDays               int     // lastEndDate - firstStartDate
	Annualised                 float64 // arb-only, idle days at 0%
	AnnualisedWithIdle         float64 // arb + idle rate on non-trading days
	AnnualisedAfterTax         float64 // arb-only, after tax
	AnnualisedWithIdleAfterTax float64 // arb + idle, after tax (effective take-home)
	TradingDays                int     // distinct calendar days with a cycle open
}

// compoundGrowth returns G = Π(1+rᵢ) - 1.
func compoundGrowth(cs []model.Cycle) float64 {
	g := 1.0
	for _, c := range cs {
		g *= 1 + c.Return()
	}
	return g - 1
}

// compoundGrowthTaxed compounds each cycle's return net of tax: Π(1+rᵢ(1-tax))-1.
// (Not derivable from compoundGrowth, so it re-walks the cycles.)
func compoundGrowthTaxed(cs []model.Cycle, tax float64) float64 {
	g := 1.0
	for _, c := range cs {
		g *= 1 + c.Return()*(1-tax)
	}
	return g - 1
}

// monthsPerYear / daysPerYear underpin the reporting convention: every rate is
// quoted as a NOMINAL annual rate compounded monthly (like a bank's "6% p.a.
// compounded monthly"), so a full year of the idle account reads back as exactly
// its input rate.
const (
	monthsPerYear = 12.0
	daysPerYear   = 365.0
)

// toNominalMonthly converts an annual growth factor (1+EAR) into a nominal
// annual rate compounded monthly: 12·((1+EAR)^(1/12) − 1).
func toNominalMonthly(annualFactor float64) float64 {
	return monthsPerYear * (math.Pow(annualFactor, 1.0/monthsPerYear) - 1)
}

// annualise takes period growth g over `days` and reports it as a nominal
// annual rate compounded monthly. Guards days<=0.
func annualise(g float64, days int) float64 {
	if days <= 0 {
		return 0
	}
	annualFactor := math.Pow(1+g, daysPerYear/float64(days))
	return toNominalMonthly(annualFactor)
}

// idleDailyFactor is the per-day growth factor of an account paying nominalIdle
// per year compounded monthly (monthly rate nominalIdle/12), expressed daily so
// it can accrue over an arbitrary number of idle days.
func idleDailyFactor(nominalIdle float64) float64 {
	monthly := 1 + nominalIdle/monthsPerYear
	return math.Pow(monthly, monthsPerYear/daysPerYear)
}

// annualiseWithIdle blends arb growth with idle interest: capital compounds by
// the cycle returns (1+g) and additionally earns the idle account's daily factor
// on every calendar day it is NOT in a trade, then the whole thing is reported
// as a nominal monthly-compounded annual rate. An empty bucket (g=0, no trading
// days) collapses to exactly nominalIdle.
func annualiseWithIdle(g float64, tradingDays, calendarDays int, nominalIdle float64) float64 {
	if calendarDays <= 0 {
		return 0
	}
	idleDays := calendarDays - tradingDays
	if idleDays < 0 {
		idleDays = 0 // overlapping cycles in a short bucket; don't go negative
	}
	periodFactor := (1 + g) * math.Pow(idleDailyFactor(nominalIdle), float64(idleDays))
	annualFactor := math.Pow(periodFactor, daysPerYear/float64(calendarDays))
	return toNominalMonthly(annualFactor)
}

// dayCount returns the inclusive number of calendar days from a to b.
func dayCount(a, b time.Time) int {
	return int(b.Sub(a).Hours()/24+0.5) + 1
}

// overlapDays returns the inclusive day count of the intersection of [as, ae]
// and [bs, be], or 0 when they don't overlap.
func overlapDays(as, ae, bs, be time.Time) int {
	s := as
	if bs.After(s) {
		s = bs
	}
	e := ae
	if be.Before(e) {
		e = be
	}
	if e.Before(s) {
		return 0
	}
	return dayCount(s, e)
}

// distinctTradingDays counts the calendar days within [from, to] covered by at
// least one cycle. Overlapping cycles and same-day rollovers (one cycle ending
// the day the next starts) count each day once, so idle days aren't undercounted.
func distinctTradingDays(cs []model.Cycle, from, to time.Time) int {
	days := map[int64]struct{}{}
	for _, c := range cs {
		s, e := c.StartDate, c.EndDate
		if s.Before(from) {
			s = from
		}
		if e.After(to) {
			e = to
		}
		for d := s; !d.After(e); d = d.AddDate(0, 0, 1) {
			days[d.Unix()/86400] = struct{}{}
		}
	}
	return len(days)
}

// Lifetime computes the whole-history roll-up. calendarDays is measured as
// lastEndDate - firstStartDate (a plain difference, not inclusive), per §4.3.
// r carries the idle and tax overlays (pass a zero Rates for pure arb figures).
func Lifetime(cs []model.Cycle, r Rates) Summary {
	if len(cs) == 0 {
		return Summary{}
	}
	sorted := sortedByStart(cs)
	first := sorted[0].StartDate
	last := sorted[0].EndDate
	var profit, retSum float64
	for _, c := range sorted {
		profit += c.NetProfit
		retSum += c.Return()
		if c.EndDate.After(last) {
			last = c.EndDate
		}
	}
	tradingDays := distinctTradingDays(sorted, first, last)
	days := int(last.Sub(first).Hours()/24 + 0.5)
	g := compoundGrowth(sorted)
	gTax := compoundGrowthTaxed(sorted, r.Tax)
	return Summary{
		Count:                      len(sorted),
		TotalProfit:                profit,
		AvgReturn:                  retSum / float64(len(sorted)),
		Compound:                   g,
		CalendarDays:               days,
		Annualised:                 annualise(g, days),
		AnnualisedWithIdle:         annualiseWithIdle(g, tradingDays, days, r.Idle),
		AnnualisedAfterTax:         annualise(gTax, days),
		AnnualisedWithIdleAfterTax: annualiseWithIdle(gTax, tradingDays, days, r.Idle*(1-r.Tax)),
		TradingDays:                tradingDays,
	}
}

// Buckets groups cycles into calendar buckets at the given granularity. A cycle
// spanning a bucket boundary contributes to each bucket in proportion to the
// days it spends there — growth splits geometrically ((1+r)^(d/holdDays)),
// profit linearly — so the buckets' compound factors multiply back to the
// cycle's full return. Count remains "cycles started in this bucket". When
// includeDead is true, empty calendar buckets between the first cycle and `now`
// are emitted as zero-return buckets (materially lowers the median); otherwise
// only buckets with trading activity are returned. Results are chronological.
func Buckets(cs []model.Cycle, gran Granularity, now time.Time, includeDead bool, r Rates) []Bucket {
	if len(cs) == 0 {
		return nil
	}
	sorted := sortedByStart(cs)

	from := periodStart(gran, sorted[0].StartDate)
	to := periodStart(gran, now)
	for _, c := range sorted { // data may run past `now`; don't truncate it
		if pe := periodStart(gran, c.EndDate); pe.After(to) {
			to = pe
		}
	}

	var out []Bucket
	for p := from; !p.After(to); p = nextPeriod(gran, p) {
		start, end := periodBounds(gran, p)
		inProgress := !now.Before(start) && !now.After(end)
		windowEnd := end
		if inProgress {
			windowEnd = now // only the elapsed part of the current period (§4.3)
		}

		gF, gTaxF := 1.0, 1.0
		var profit float64
		count, active := 0, false
		for _, c := range sorted {
			if periodStart(gran, c.StartDate).Equal(p) {
				count++
			}
			d := overlapDays(c.StartDate, c.EndDate, start, windowEnd)
			if d == 0 {
				continue
			}
			active = true
			frac := float64(d) / float64(c.HoldDays())
			gF *= math.Pow(1+c.Return(), frac)
			gTaxF *= math.Pow(1+c.Return()*(1-r.Tax), frac)
			profit += c.NetProfit * frac
		}
		if !active && count == 0 && !includeDead {
			continue
		}
		g, gTax := gF-1, gTaxF-1

		days := dayCount(start, windowEnd)
		fullDays := dayCount(start, end) // whole calendar span, ignoring `now`
		tradingDays := distinctTradingDays(sorted, start, windowEnd)
		out = append(out, Bucket{
			Label:                      periodLabel(gran, p),
			Start:                      start,
			End:                        end,
			Count:                      count,
			TotalProfit:                profit,
			Compound:                   g,
			CalendarDays:               days,
			Annualised:                 annualise(g, days),
			AnnualisedWithIdle:         annualiseWithIdle(g, tradingDays, days, r.Idle),
			AnnualisedAfterTax:         annualise(gTax, days),
			AnnualisedWithIdleAfterTax: annualiseWithIdle(gTax, tradingDays, days, r.Idle*(1-r.Tax)),
			// Floor: same blend but over the full period (remainder = idle).
			AnnualisedFloor:         annualiseWithIdle(g, tradingDays, fullDays, r.Idle),
			AnnualisedFloorAfterTax: annualiseWithIdle(gTax, tradingDays, fullDays, r.Idle*(1-r.Tax)),
			TradingDays:             tradingDays,
			Partial:                 days < PartialDayThreshold,
			InProgress:              inProgress,
		})
	}
	return out
}

// VarianceStats summarises the spread of annualised rates across buckets (§4.5).
// All figures are fractional; the UI renders std in percentage points.
type VarianceStats struct {
	Mean, Median, Std, Min, Max float64
	N                           int
}

// Variance computes mean/median/population-std/min/max of the buckets'
// arb-only annualised rates. Partial buckets are excluded — §4.4 flags their
// annualised figure as unreliable, so it must not distort the stats either.
func Variance(buckets []Bucket) VarianceStats {
	return varianceOf(fullBucketRates(buckets, func(b Bucket) float64 { return b.Annualised }))
}

// VarianceWithIdle is Variance over the with-idle annualised rates.
func VarianceWithIdle(buckets []Bucket) VarianceStats {
	return varianceOf(fullBucketRates(buckets, func(b Bucket) float64 { return b.AnnualisedWithIdle }))
}

// VarianceWithIdleAfterTax is Variance over the after-tax with-idle (take-home)
// annualised rates.
func VarianceWithIdleAfterTax(buckets []Bucket) VarianceStats {
	return varianceOf(fullBucketRates(buckets, func(b Bucket) float64 { return b.AnnualisedWithIdleAfterTax }))
}

// fullBucketRates extracts one rate per non-partial bucket.
func fullBucketRates(buckets []Bucket, rate func(Bucket) float64) []float64 {
	rates := make([]float64, 0, len(buckets))
	for _, b := range buckets {
		if b.Partial {
			continue
		}
		rates = append(rates, rate(b))
	}
	return rates
}

func varianceOf(rates []float64) VarianceStats {
	if len(rates) == 0 {
		return VarianceStats{}
	}
	sort.Float64s(rates)

	var sum float64
	min, max := rates[0], rates[len(rates)-1]
	for _, r := range rates {
		sum += r
	}
	mean := sum / float64(len(rates))

	var sq float64
	for _, r := range rates {
		sq += (r - mean) * (r - mean)
	}
	std := math.Sqrt(sq / float64(len(rates))) // population

	var median float64
	n := len(rates)
	if n%2 == 1 {
		median = rates[n/2]
	} else {
		median = (rates[n/2-1] + rates[n/2]) / 2
	}
	return VarianceStats{Mean: mean, Median: median, Std: std, Min: min, Max: max, N: n}
}

// ---- calendar helpers ------------------------------------------------------

func sortedByStart(cs []model.Cycle) []model.Cycle {
	out := make([]model.Cycle, len(cs))
	copy(out, cs)
	sort.SliceStable(out, func(i, j int) bool { return out[i].StartDate.Before(out[j].StartDate) })
	return out
}

// periodStart returns the canonical first-day-of-period date for t.
func periodStart(g Granularity, t time.Time) time.Time {
	y, m, _ := t.Date()
	switch g {
	case Year:
		return time.Date(y, 1, 1, 0, 0, 0, 0, time.UTC)
	case Quarter:
		qm := (int(m)-1)/3*3 + 1
		return time.Date(y, time.Month(qm), 1, 0, 0, 0, 0, time.UTC)
	default: // Month
		return time.Date(y, m, 1, 0, 0, 0, 0, time.UTC)
	}
}

func nextPeriod(g Granularity, p time.Time) time.Time {
	switch g {
	case Year:
		return p.AddDate(1, 0, 0)
	case Quarter:
		return p.AddDate(0, 3, 0)
	default:
		return p.AddDate(0, 1, 0)
	}
}

// periodBounds returns the inclusive [start, end] calendar dates for the period
// whose start is p.
func periodBounds(g Granularity, p time.Time) (time.Time, time.Time) {
	return p, nextPeriod(g, p).AddDate(0, 0, -1)
}

func periodLabel(g Granularity, p time.Time) string {
	_, m, _ := p.Date()
	switch g {
	case Year:
		return p.Format("2006")
	case Quarter:
		q := (int(m)-1)/3 + 1
		return p.Format("2006") + "-Q" + string(rune('0'+q))
	default:
		return p.Format("2006-01")
	}
}
