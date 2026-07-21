package analytics

import (
	"testing"
	"time"

	"github.com/wolffshots/fftui/internal/model"
)

// synthCycles builds n evenly spaced 1-day cycles over the trailing year with
// the given per-cycle returns (capital 100k).
func synthCycles(now time.Time, returns []float64) []model.Cycle {
	n := len(returns)
	cs := make([]model.Cycle, n)
	for i, r := range returns {
		start := now.AddDate(0, 0, -360+i*(360/n))
		cs[i] = model.Cycle{
			StartDate: start, EndDate: start,
			ZarIn: 100_000, ZarOut: 100_000 * (1 + r), NetProfit: 100_000 * r,
		}
	}
	return cs
}

func TestTrendDetectsDecay(t *testing.T) {
	now := time.Date(2026, 7, 10, 0, 0, 0, 0, time.UTC)
	// Returns fall linearly 0.8% → 0.1% over the year: unmistakable decay.
	returns := make([]float64, 20)
	for i := range returns {
		returns[i] = 0.008 - 0.0007*float64(i)/1 // -0.07pp per step
	}
	tr := TrendOf(synthCycles(now, returns), now)
	if tr.N != 20 {
		t.Fatalf("cycles: got %d, want 20", tr.N)
	}
	if tr.Slope90 >= 0 {
		t.Errorf("slope should be negative, got %+.5f", tr.Slope90)
	}
	if !tr.Significant {
		t.Error("a clean linear decline should be significant")
	}
	if tr.Recent90 >= tr.Prior90 {
		t.Errorf("recent 90d annualised %.4f should trail prior %.4f", tr.Recent90, tr.Prior90)
	}
}

func TestTrendFlatIsNotSignificant(t *testing.T) {
	now := time.Date(2026, 7, 10, 0, 0, 0, 0, time.UTC)
	returns := make([]float64, 20)
	for i := range returns {
		returns[i] = 0.004
	}
	tr := TrendOf(synthCycles(now, returns), now)
	if tr.Significant {
		t.Error("constant returns must not read as a significant trend")
	}
	if tr.Slope90 != 0 {
		t.Errorf("flat returns should give zero slope, got %+.6f", tr.Slope90)
	}
}

func TestTrendReferenceData(t *testing.T) {
	cs := loadCycles(t)
	tr := TrendOf(cs, refNow)
	if tr.N != 24 {
		t.Errorf("trailing-year cycles: got %d, want 24", tr.N)
	}
	// Trailing 90d (from 2026-04-11): FX0037–FX0043; prior 90d: FX0031–FX0036.
	if tr.RecentCycles != 7 || tr.PriorCycles != 6 {
		t.Errorf("cadence: got %d vs %d, want 7 vs 6", tr.RecentCycles, tr.PriorCycles)
	}
	// The demo data alternates returns by design — no significant drift.
	if tr.Significant {
		t.Errorf("demo data should not show a significant trend (slope %+.5f)", tr.Slope90)
	}
}

func TestSpreadTrend(t *testing.T) {
	points := make([]model.MarketPoint, 100)
	for i := range points {
		points[i].Spread = 1.0
	}
	// Last ~30 days (8 of 100 samples over 365d) halve.
	for i := 92; i < 100; i++ {
		points[i].Spread = 0.5
	}
	recent, overall, ok := SpreadTrend(points, 365, 30)
	if !ok {
		t.Fatal("expected a result")
	}
	assertClose(t, "recent spread", recent, 0.5, 1e-12)
	assertClose(t, "overall spread", overall, 0.96, 1e-12)

	if _, _, ok := SpreadTrend(points[:5], 365, 30); ok {
		t.Error("too few samples should give no result")
	}
}
