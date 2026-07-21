package analytics

import (
	"math"
	"testing"
	"time"

	"github.com/wolffshots/fftui/internal/model"
)

func day(y int, m time.Month, d int) time.Time {
	return time.Date(y, m, d, 0, 0, 0, 0, time.UTC)
}

// A single cycle of exactly one year turning 100 into 110 has a 10% EAR by
// construction, so the quote must be exactly toNominalMonthly(1.10).
func TestMoneyWeightedSingleCycle(t *testing.T) {
	cs := []model.Cycle{{
		StartDate: day(2025, 1, 1), EndDate: day(2026, 1, 1),
		ZarIn: 100, ZarOut: 110, NetProfit: 10,
	}}
	got, ok := MoneyWeighted(cs)
	if !ok {
		t.Fatal("expected a figure for a single-year cycle")
	}
	assertClose(t, "single-cycle IRR %", pct(got), pct(toNominalMonthly(1.10)), 1e-6)
}

// Two back-to-back one-year cycles: 100→110 (10%), then 300→315 (5%). The
// flows collapse to a quadratic in x=(1+EAR)⁻¹: 315x² − 190x − 100 = 0, so the
// expected EAR has a closed form. The money-weighted figure must sit below the
// time-weighted one because three times the capital earned the worse rate.
func TestMoneyWeightedWeightsCapital(t *testing.T) {
	cs := []model.Cycle{
		{StartDate: day(2024, 1, 1), EndDate: day(2024, 12, 31), ZarIn: 100, ZarOut: 110, NetProfit: 10},
		{StartDate: day(2024, 12, 31), EndDate: day(2025, 12, 31), ZarIn: 300, ZarOut: 315, NetProfit: 15},
	}
	x := (190 + math.Sqrt(190*190+4*315*100)) / (2 * 315)
	want := toNominalMonthly(1 / x)

	got, ok := MoneyWeighted(cs)
	if !ok {
		t.Fatal("expected a figure")
	}
	assertClose(t, "two-cycle IRR %", pct(got), pct(want), 1e-6)

	tw := Lifetime(cs, Rates{}).Annualised
	if got >= tw {
		t.Errorf("money-weighted %.4f should be below time-weighted %.4f (big capital earned the lower rate)", got, tw)
	}
}

// The reference dataset reinvests every payout in full (each ZarIn equals the
// previous ZarOut), so all intermediate flows vanish and the money-weighted
// rate must equal the time-weighted arb-only rate to solver precision.
func TestMoneyWeightedReferenceData(t *testing.T) {
	cs := loadCycles(t)
	got, ok := MoneyWeighted(cs)
	if !ok {
		t.Fatal("expected a figure for the reference dataset")
	}
	tw := Lifetime(cs, Rates{}).Annualised
	assertClose(t, "reference IRR vs time-weighted %", pct(got), pct(tw), 1e-6)
}

func TestMoneyWeightedDegenerate(t *testing.T) {
	if _, ok := MoneyWeighted(nil); ok {
		t.Error("no cycles should yield no figure")
	}
	// All flows on one day: NPV is a constant (the profit), never zero.
	sameDay := []model.Cycle{{
		StartDate: day(2025, 1, 1), EndDate: day(2025, 1, 1),
		ZarIn: 100, ZarOut: 101, NetProfit: 1,
	}}
	if _, ok := MoneyWeighted(sameDay); ok {
		t.Error("same-day flows should yield no figure")
	}
}
