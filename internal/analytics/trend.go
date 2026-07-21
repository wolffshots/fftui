package analytics

import (
	"math"
	"time"

	"github.com/wolffshots/fftui/internal/model"
)

// Trend summarises whether the arb is decaying, from the trailing year of
// cycles: an OLS slope on per-cycle returns over time, plus recent-vs-prior
// 90-day comparisons of rate and cadence.
type Trend struct {
	N           int     // cycles in the trailing 365 days
	Slope90     float64 // OLS change in per-cycle return per 90 days (fraction)
	Significant bool    // |t| >= 2 on the slope (≈95% level)

	Recent90     float64 // arb-only annualised over the trailing 90 days
	Prior90      float64 // same for the 90 days before that
	RecentCycles int     // cycles started in the trailing 90 days
	PriorCycles  int
}

// TrendOf computes the return/cadence trend as of `now`.
func TrendOf(cs []model.Cycle, now time.Time) Trend {
	var t Trend
	yearAgo := now.AddDate(0, 0, -365)
	d90 := now.AddDate(0, 0, -90)
	d180 := now.AddDate(0, 0, -180)

	var xs, ys []float64
	gRecent, gPrior := 1.0, 1.0
	for _, c := range cs {
		if !c.StartDate.After(yearAgo) || c.StartDate.After(now) {
			continue
		}
		t.N++
		xs = append(xs, c.StartDate.Sub(yearAgo).Hours()/24)
		ys = append(ys, c.Return())
		switch {
		case c.StartDate.After(d90):
			t.RecentCycles++
			gRecent *= 1 + c.Return()
		case c.StartDate.After(d180):
			t.PriorCycles++
			gPrior *= 1 + c.Return()
		}
	}
	t.Recent90 = annualise(gRecent-1, 90)
	t.Prior90 = annualise(gPrior-1, 90)

	// OLS slope of return vs start day, with a t-test on the slope. Needs a
	// few points to say anything; below that the slope stays zero.
	if t.N >= 5 {
		n := float64(t.N)
		var sx, sy float64
		for i := range xs {
			sx += xs[i]
			sy += ys[i]
		}
		mx, my := sx/n, sy/n
		var sxx, sxy float64
		for i := range xs {
			sxx += (xs[i] - mx) * (xs[i] - mx)
			sxy += (xs[i] - mx) * (ys[i] - my)
		}
		if sxx > 0 {
			slope := sxy / sxx
			var sse float64
			for i := range xs {
				resid := ys[i] - my - slope*(xs[i]-mx)
				sse += resid * resid
			}
			t.Slope90 = slope * 90
			if t.N > 2 && sse > 0 {
				se := math.Sqrt(sse / (n - 2) / sxx)
				t.Significant = math.Abs(slope/se) >= 2
			}
		}
	}
	return t
}

// SpreadTrend compares the mean market spread over roughly the last
// `recentDays` of a history series against the whole series' mean. The API
// returns evenly spaced samples over the requested period, so the recent
// window is a proportional slice off the end rather than parsed timestamps.
// Spread values are percentages as the API reports them (0.82 = 0.82%).
// ok is false with fewer than 10 samples.
func SpreadTrend(points []model.MarketPoint, periodDays, recentDays int) (recent, overall float64, ok bool) {
	n := len(points)
	if n < 10 || periodDays <= 0 || recentDays <= 0 || recentDays > periodDays {
		return 0, 0, false
	}
	var sum float64
	for _, p := range points {
		sum += p.Spread
	}
	overall = sum / float64(n)

	k := n * recentDays / periodDays
	if k < 1 {
		k = 1
	}
	var recentSum float64
	for _, p := range points[n-k:] {
		recentSum += p.Spread
	}
	return recentSum / float64(k), overall, true
}
