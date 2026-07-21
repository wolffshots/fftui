package analytics

import "testing"

func TestBootstrapReferenceData(t *testing.T) {
	cs := loadCycles(t)
	b := Bootstrap(cs, refRates, 10_000)
	if !b.OK {
		t.Fatal("expected a bootstrap result for 43 cycles")
	}
	if b.N != 10_000 {
		t.Errorf("resamples: got %d, want 10000", b.N)
	}
	if b.Arb.Lo >= b.Arb.Hi || b.Net.Lo >= b.Net.Hi {
		t.Errorf("degenerate bands: arb %+v net %+v", b.Arb, b.Net)
	}
	// The band must bracket the point estimates (9.78% arb-only, 8.91% net on
	// the demo data) — resampling is centred on the observed returns.
	s := Lifetime(cs, refRates)
	if !(b.Arb.Lo < s.Annualised && s.Annualised < b.Arb.Hi) {
		t.Errorf("arb band [%.4f, %.4f] misses the point estimate %.4f", b.Arb.Lo, b.Arb.Hi, s.Annualised)
	}
	if !(b.Net.Lo < s.AnnualisedWithIdleAfterTax && s.AnnualisedWithIdleAfterTax < b.Net.Hi) {
		t.Errorf("net band [%.4f, %.4f] misses the point estimate %.4f", b.Net.Lo, b.Net.Hi, s.AnnualisedWithIdleAfterTax)
	}
}

// The seed is fixed, so the band must be identical across calls — the UI
// re-renders constantly and the figures must not jitter.
func TestBootstrapDeterministic(t *testing.T) {
	cs := loadCycles(t)
	a := Bootstrap(cs, refRates, 2_000)
	b := Bootstrap(cs, refRates, 2_000)
	if a != b {
		t.Errorf("bootstrap not deterministic: %+v vs %+v", a, b)
	}
}

func TestBootstrapTooFewCycles(t *testing.T) {
	cs := loadCycles(t)
	if b := Bootstrap(cs[:7], refRates, 10_000); b.OK {
		t.Error("expected no result for 7 cycles")
	}
	if b := Bootstrap(cs, refRates, 50); b.OK {
		t.Error("expected no result for 50 resamples")
	}
}
