package analytics

import "testing"

func TestTierRateBoundaries(t *testing.T) {
	f := DefaultFees()
	cases := []struct {
		capital float64
		want    float64
	}{
		{50_000, 0.35}, // below the first tier clamps to it
		{100_000, 0.35},
		{149_999, 0.35},
		{150_000, 0.33},
		{199_999, 0.33},
		{200_000, 0.30},
		{299_999, 0.30},
		{300_000, 0.28},
		{399_999, 0.28},
		{400_000, 0.25},
		{1_000_000, 0.25},
	}
	for _, c := range cases {
		if got := f.TierRate(c.capital); got != c.want {
			t.Errorf("TierRate(%.0f): got %.2f, want %.2f", c.capital, got, c.want)
		}
	}
	if got := (Fees{}).TierRate(100_000); got != 0 {
		t.Errorf("empty tiers should give rate 0, got %.2f", got)
	}
}

// TestFeesReproduceStatement pins the waterfall to the real cycle statement
// JW229109 (30-12-2025): Amount In R240,254, Gross Earnings R3,609.37,
// third-party fees R550.39 variable + R530.00 fixed, Gross Profit R2,528.98,
// FF fee 30% = R758.69, Net Profit R1,770.29.
func TestFeesReproduceStatement(t *testing.T) {
	const (
		capital  = 240_254.0
		earnings = 3_609.37
		variable = 550.39 // Capitec exchange + offshore receipt + offshore trading
		net      = 1_770.29
	)
	f := DefaultFees()
	f.Variable = variable / capital
	spread := earnings / capital

	assertClose(t, "statement net profit", f.Net(spread, capital), net, 0.01)
	// The statement rounds net to the cent, which perturbs the inverted spread
	// by up to ~(0.005/(1-0.30))/capital ≈ 3e-8.
	assertClose(t, "statement spread back-out", f.Spread(net, capital), spread, 1e-7)
}

func TestSpreadNetRoundtrip(t *testing.T) {
	f := DefaultFees()
	for _, capital := range []float64{120_000, 180_000, 250_000, 350_000, 500_000} {
		net := f.Net(0.012, capital)
		assertClose(t, "roundtrip spread", f.Spread(net, capital), 0.012, 1e-12)
	}
}

// Bigger cycles must never model a worse net return: fixed fees dilute and the
// FF tier only improves with capital.
func TestNetReturnImprovesWithCapital(t *testing.T) {
	f := DefaultFees()
	prev := -1.0
	for _, capital := range []float64{100_000, 150_000, 200_000, 300_000, 400_000, 800_000} {
		r := f.Net(0.012, capital) / capital
		if r < prev {
			t.Errorf("net return fell from %.5f to %.5f at capital %.0f", prev, r, capital)
		}
		prev = r
	}
}
