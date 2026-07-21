package analytics

import (
	"testing"
	"time"
)

func TestTaxYearPeriods(t *testing.T) {
	// 28 Feb belongs to the tax year that started the previous March; 1 March
	// opens the next one. Labels carry the year the period ENDS in.
	feb := periodStart(TaxYear, time.Date(2026, 2, 28, 0, 0, 0, 0, time.UTC))
	mar := periodStart(TaxYear, time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC))
	if !feb.Equal(time.Date(2025, 3, 1, 0, 0, 0, 0, time.UTC)) {
		t.Errorf("28 Feb 2026 tax year starts %s, want 2025-03-01", feb.Format("2006-01-02"))
	}
	if !mar.Equal(time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC)) {
		t.Errorf("1 Mar 2026 tax year starts %s, want 2026-03-01", mar.Format("2006-01-02"))
	}
	if got := periodLabel(TaxYear, mar); got != "TY2027" {
		t.Errorf("label: got %s, want TY2027", got)
	}
}

func TestTaxYearBucketsReferenceData(t *testing.T) {
	cs := loadCycles(t)
	buckets := Buckets(cs, TaxYear, refNow, false, refRates)
	if len(buckets) != 3 { // TY2025 (from Sep 2024), TY2026, TY2027 (in progress)
		t.Fatalf("bucket count: got %d, want 3", len(buckets))
	}
	for i, want := range []string{"TY2025", "TY2026", "TY2027"} {
		if buckets[i].Label != want {
			t.Errorf("bucket %d label: got %s, want %s", i, buckets[i].Label, want)
		}
	}
	if !buckets[2].InProgress {
		t.Error("TY2027 should be in progress at refNow (2026-07-10)")
	}
}

// refAllow is the standard combined pool: R2m SDA + R10m FIA.
var refAllow = Allowances{SDALimit: 2_000_000, FIALimit: 10_000_000}

func TestPlanReferenceData(t *testing.T) {
	cs := loadCycles(t)
	p := Plan(cs, refNow, refRates, refAllow, DefaultFees())

	// Realisation accounting: cycles ending on/after 2026-03-01 (FX0034–FX0043).
	if p.TaxYearLabel != "TY2027" {
		t.Errorf("tax year label: got %s, want TY2027", p.TaxYearLabel)
	}
	assertClose(t, "tax-year profit", p.TaxYearProfit, 4878.33, 0.01)
	assertClose(t, "estimated tax", p.EstimatedTax, 4878.33*0.41, 0.01)

	// Inferred usage: the 14 cycles started in calendar 2026 (FX0030–FX0043),
	// against the combined R12m pool.
	assertClose(t, "allowance used", p.Used, 1_621_808.33, 0.01)
	assertClose(t, "allowance remaining", p.Remaining, 10_378_191.67, 0.01)
	if p.Exhausted {
		t.Error("combined pool should not be exhausted")
	}
	// At the year-to-date pace the R12m outlasts the calendar year — no date.
	if p.HasExhaust {
		t.Error("no exhaustion projection expected within the year")
	}

	// Trailing 365 days: FX0020–FX0043.
	if p.CyclesPerYear != 24 {
		t.Errorf("cycles/yr: got %d, want 24", p.CyclesPerYear)
	}
	assertClose(t, "sweet spot", p.SweetSpot, 12_000_000.0/24, 0.01)
	assertClose(t, "current capital", p.CurrentCapital, 118_934.87, 0.01)
	if p.CurrentCapital >= p.SweetSpot {
		t.Error("demo data should be below the combined-pool sweet spot")
	}
	// Fee-aware projections. The demo cycles sit in the 35% tier; the top
	// tier is 25% at R400k, so the modelled net return must improve with size
	// and extra capital must be worth a positive amount per year.
	if p.CurrentTier != 0.35 {
		t.Errorf("current tier: got %.2f, want 0.35", p.CurrentTier)
	}
	if p.TopTier != 0.25 || p.TopTierMin != 400_000 {
		t.Errorf("top tier: got %.2f at %.0f, want 0.25 at 400000", p.TopTier, p.TopTierMin)
	}
	if p.ReturnAtTop <= p.ReturnNow {
		t.Errorf("net return should improve at the top tier: now %.5f, top %.5f", p.ReturnNow, p.ReturnAtTop)
	}
	// The modelled return at current capital should land near the observed
	// trailing average (same capital range, so the model is interpolating).
	if p.ReturnNow < p.AvgReturn*0.8 || p.ReturnNow > p.AvgReturn*1.2 {
		t.Errorf("modelled ReturnNow %.5f implausibly far from observed AvgReturn %.5f", p.ReturnNow, p.AvgReturn)
	}
	if p.Extra100kGross <= 0 {
		t.Errorf("extra capital below the sweet spot should add profit, got %.2f", p.Extra100kGross)
	}
	assertClose(t, "extra-100k net", p.Extra100kNet, p.Extra100kGross*(1-0.41), 1e-9)
}

// TestPlanLiveBalances: with live SDA/FIA balances the usage comes from the
// API figures (which also see in-flight cycles), not the cycle-history sum.
func TestPlanLiveBalances(t *testing.T) {
	cs := loadCycles(t)
	a := refAllow
	a.Live = true
	a.SDAAvailable = 1_644_450.00
	a.FIAAvailable = 7_145_861.97
	p := Plan(cs, refNow, refRates, a, DefaultFees())
	assertClose(t, "live used", p.Used, 3_209_688.03, 0.01)
	assertClose(t, "live remaining", p.Remaining, 8_790_311.97, 0.01)
	if p.Exhausted {
		t.Error("should not be exhausted")
	}
	if p.HasExhaust { // ~523 days of runway at this pace → the year resets first
		t.Error("no in-year exhaustion expected")
	}
}

func TestPlanZeroLimitDisablesAllowance(t *testing.T) {
	cs := loadCycles(t)
	p := Plan(cs, refNow, refRates, Allowances{}, DefaultFees())
	if p.Used != 0 || p.Remaining != 0 || p.Exhausted || p.HasExhaust || p.SweetSpot != 0 {
		t.Errorf("zero limits should zero the allowance figures: %+v", p)
	}
	// Tax and cadence figures are independent of the allowance.
	if p.TaxYearProfit == 0 || p.CyclesPerYear == 0 {
		t.Error("tax/cadence figures should still be computed")
	}
}

// TestPlanSDAOnlyPool: an SDA-only pool small enough to exhaust in-year gets a
// projected date (R2m − R1,621,808.33 at the YTD pace ≈ 44 days out).
func TestPlanSDAOnlyPool(t *testing.T) {
	cs := loadCycles(t)
	p := Plan(cs, refNow, refRates, Allowances{SDALimit: 2_000_000}, DefaultFees())
	if !p.HasExhaust {
		t.Fatal("expected an exhaustion projection")
	}
	if got := p.ExhaustDate.Format("2006-01-02"); got != "2026-08-23" {
		t.Errorf("exhaust date: got %s, want 2026-08-23", got)
	}
	if p.Exhausted {
		t.Error("not exhausted yet")
	}
}

func TestPlanExhaustedPool(t *testing.T) {
	cs := loadCycles(t)
	p := Plan(cs, refNow, refRates, Allowances{SDALimit: 1_000_000}, DefaultFees()) // pre-Apr-2026 SDA alone
	if !p.Exhausted {
		t.Error("R1m pool should read exhausted on the demo data")
	}
	if p.HasExhaust {
		t.Error("no projection once exhausted")
	}
	if p.Remaining != 0 {
		t.Errorf("remaining should clamp to 0, got %f", p.Remaining)
	}
}
