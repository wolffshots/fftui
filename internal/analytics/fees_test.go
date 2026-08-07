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

// cycles are SYNTHETIC fixtures: round capitals, with spreads and variable-fee
// rates inside the bands four real cycle statements (Dec 2025 - Aug 2026)
// showed. The expected lines were worked out by hand, not by this package, so
// the test still catches an arithmetic regression. No real balances are
// recorded here — this repo is public. The real statements were checked against
// this model when it was written: the waterfall matched them to the cent.
var cycles = []struct {
	name                                   string
	capital, spread, varRate               float64
	earnings, variable                     float64
	grossProfit, tierRate, successFee, net float64
}{
	{"R150k/33%", 150_000, 0.015023, 0.002291, 2_253.45, 343.65, 1_379.80, 0.33, 455.334, 924.466},
	{"R250k/30%", 250_000, 0.009540, 0.002346, 2_385.00, 586.50, 1_268.50, 0.30, 380.550, 887.950},
	{"R260k/30%", 260_000, 0.007663, 0.002282, 1_992.38, 593.32, 869.06, 0.30, 260.718, 608.342},
	{"R300k/28%", 300_000, 0.007771, 0.002321, 2_331.30, 696.30, 1_105.00, 0.28, 309.400, 795.600},
}

// TestProjectionWaterfall checks every line of the modelled waterfall against
// hand-computed figures. This is the structural check: the third-party fees
// come off gross earnings first, the tier is keyed on capital, and FF's share
// is a cut of GROSS PROFIT — not of earnings, and not of capital.
func TestProjectionWaterfall(t *testing.T) {
	for _, c := range cycles {
		f := DefaultFees()
		f.Variable = c.varRate
		p := f.Project(c.spread, c.capital)

		if p.TierRate != c.tierRate {
			t.Errorf("%s: tier rate %.2f, want %.2f", c.name, p.TierRate, c.tierRate)
		}
		assertClose(t, c.name+" gross earnings", p.GrossEarnings, c.earnings, 0.01)
		assertClose(t, c.name+" third-party", p.VariableFee+p.FixedFee, c.variable+530, 0.01)
		assertClose(t, c.name+" gross profit", p.GrossProfit, c.grossProfit, 0.01)
		assertClose(t, c.name+" success fee", p.SuccessFee, c.successFee, 0.01)
		assertClose(t, c.name+" net profit", p.NetProfit, c.net, 0.01)
		assertClose(t, c.name+" net return", p.NetReturn, c.net/c.capital, 1e-9)

		// The inversions the cycles table and detail view use, from net profit
		// back to the gross lines and the spread that produced them.
		assertClose(t, c.name+" spread back-out", f.Spread(c.net, c.capital), c.spread, 1e-9)
		assertClose(t, c.name+" gross profit back-out", f.GrossProfit(c.net, c.capital), c.grossProfit, 0.01)
		assertClose(t, c.name+" gross return", f.GrossReturn(c.net, c.capital), c.grossProfit/c.capital, 1e-9)
	}
}

// observedVarRates are the third-party variable fees charged on four real cycle
// statements, as a fraction of capital. Only the RATES are recorded (FF's
// pricing, not personal amounts) — they are what the default is calibrated to.
var observedVarRates = []float64{0.002291, 0.002346, 0.002282, 0.002321}

// TestDefaultVariableFeeTracksObserved checks the shipped default (0.23% of
// capital) against those rates. The returns view projects with this default, so
// its error is the error on every modelled net profit: the assertion is that a
// projection stays within 1.5% of what the fee actually costs.
func TestDefaultVariableFeeTracksObserved(t *testing.T) {
	f := DefaultFees()
	const capital, spread = 250_000.0, 0.0095
	for _, rate := range observedVarRates {
		if diff := rate - f.Variable; diff < -0.00005 || diff > 0.00005 {
			t.Errorf("observed variable fee %.4f%% of capital, default models %.4f%%",
				rate*100, f.Variable*100)
		}
		actual := f
		actual.Variable = rate
		want := actual.Net(spread, capital)
		got := f.Net(spread, capital)
		if err := (got - want) / want; err < -0.015 || err > 0.015 {
			t.Errorf("at a %.4f%% variable fee: default models net %.2f, actual %.2f (%.1f%% off)",
				rate*100, got, want, err*100)
		}
	}
}

// A losing cycle pays no success fee — FF's share is a cut of profit, so a
// negative gross profit must pass through to net untouched.
func TestNoSuccessFeeOnALoss(t *testing.T) {
	f := DefaultFees()
	p := f.Project(0.004, 50_000) // 0.4% spread on R50k: fees exceed earnings
	if p.GrossProfit >= 0 {
		t.Fatalf("expected a loss, got gross profit %.2f", p.GrossProfit)
	}
	if p.SuccessFee != 0 {
		t.Errorf("success fee %.2f charged on a loss", p.SuccessFee)
	}
	assertClose(t, "loss net equals gross", p.NetProfit, p.GrossProfit, 1e-9)
	assertClose(t, "loss spread round trip", f.Spread(p.NetProfit, 50_000), 0.004, 1e-12)
}

// BreakEven is the capital where gross profit crosses zero.
func TestBreakEven(t *testing.T) {
	f := DefaultFees()
	be, ok := f.BreakEven(0.008)
	if !ok {
		t.Fatal("0.8% spread should break even somewhere")
	}
	assertClose(t, "break-even gross profit", f.Project(0.008, be).GrossProfit, 0, 1e-9)
	if _, ok := f.BreakEven(f.Variable); ok {
		t.Error("a spread at the variable fee rate can never break even")
	}
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
