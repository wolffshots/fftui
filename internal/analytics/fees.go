package analytics

// Fees models the per-cycle fee waterfall shown on the Future Forex cycle
// statements:
//
//	gross earnings  = capital × market spread
//	− third-party   = Fixed rand (Capitec admin + instant EFT)
//	                  + Variable × capital (Capitec exchange + offshore fees)
//	= gross profit
//	− FF success fee = gross profit × tier rate (tier keyed on capital)
//	= net profit
//
// The ordering and the fixed amounts were verified to the cent against real
// cycle statements; the tier table is Future Forex's published schedule (only
// the 35% and 30% tiers are corroborated by statements, the rest are as
// published). Everything is configurable via flags because FF can revise it.
type Fees struct {
	Fixed    float64   // rand per cycle (default R530: admin R500 + EFT R30)
	Variable float64   // fraction of capital per cycle (default 0.0023)
	Tiers    []FeeTier // ascending Min; FF's share of gross profit
}

// FeeTier applies Rate to gross profit for cycles with capital >= Min (until
// the next tier's Min).
type FeeTier struct {
	Min  float64
	Rate float64
}

// DefaultFees returns the fee schedule as of mid-2026.
func DefaultFees() Fees {
	return Fees{
		Fixed:    530,
		Variable: 0.0023,
		Tiers: []FeeTier{
			{100_000, 0.35},
			{150_000, 0.33},
			{200_000, 0.30},
			{300_000, 0.28},
			{400_000, 0.25},
		},
	}
}

// TierRate returns FF's share of gross profit at the given cycle capital.
// Capital below the first tier clamps to the first tier's rate.
func (f Fees) TierRate(capital float64) float64 {
	if len(f.Tiers) == 0 {
		return 0
	}
	rate := f.Tiers[0].Rate
	for _, t := range f.Tiers {
		if capital >= t.Min {
			rate = t.Rate
		}
	}
	return rate
}

// Net projects the per-cycle net profit at `capital` given a gross-earnings
// market spread (a fraction of capital).
func (f Fees) Net(spread, capital float64) float64 {
	grossProfit := (spread-f.Variable)*capital - f.Fixed
	return grossProfit * (1 - f.TierRate(capital))
}

// Spread inverts Net: the gross-earnings spread implied by an observed net
// profit at a known capital. Used to back the market spread out of the cycle
// history so returns can be projected at other capital sizes.
func (f Fees) Spread(net, capital float64) float64 {
	if capital <= 0 {
		return 0
	}
	grossProfit := net / (1 - f.TierRate(capital))
	return (grossProfit+f.Fixed)/capital + f.Variable
}
