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

// Projection is the modelled waterfall for one cycle at a given capital and
// gross-earnings spread — the same lines, in the same order, as the statement.
type Projection struct {
	Capital       float64
	Spread        float64 // gross-earnings spread, as a fraction of capital
	GrossEarnings float64 // capital × spread
	VariableFee   float64 // Variable × capital (bank exchange + offshore fees)
	FixedFee      float64 // Fixed (bank admin + instant EFT)
	GrossProfit   float64 // gross earnings − third-party fees
	TierRate      float64 // FF's share of gross profit at this capital
	SuccessFee    float64 // TierRate × gross profit
	NetProfit     float64 // gross profit − success fee
	NetReturn     float64 // NetProfit / Capital
}

// Project models the waterfall at `capital` for a gross-earnings `spread` (a
// fraction of capital). FF's success fee is a share of GROSS PROFIT — what is
// left after the third-party fees — and a losing cycle pays no success fee.
func (f Fees) Project(spread, capital float64) Projection {
	p := Projection{
		Capital:       capital,
		Spread:        spread,
		GrossEarnings: spread * capital,
		VariableFee:   f.Variable * capital,
		FixedFee:      f.Fixed,
		TierRate:      f.TierRate(capital),
	}
	p.GrossProfit = p.GrossEarnings - p.VariableFee - p.FixedFee
	if p.GrossProfit > 0 {
		p.SuccessFee = p.GrossProfit * p.TierRate
	}
	p.NetProfit = p.GrossProfit - p.SuccessFee
	if capital > 0 {
		p.NetReturn = p.NetProfit / capital
	}
	return p
}

// Net projects the per-cycle net profit at `capital` given a gross-earnings
// market spread (a fraction of capital).
func (f Fees) Net(spread, capital float64) float64 {
	return f.Project(spread, capital).NetProfit
}

// BreakEven is the cycle capital at which gross profit reaches zero for a
// spread: below it the fees are more than the spread earns. The second result
// is false when no capital breaks even (the variable fee alone eats the spread).
func (f Fees) BreakEven(spread float64) (float64, bool) {
	if spread <= f.Variable {
		return 0, false
	}
	return f.Fixed / (spread - f.Variable), true
}

// Spread inverts Net: the gross-earnings spread implied by an observed net
// profit at a known capital. Used to back the market spread out of the cycle
// history so returns can be projected at other capital sizes.
func (f Fees) Spread(net, capital float64) float64 {
	if capital <= 0 {
		return 0
	}
	return (f.GrossProfit(net, capital)+f.Fixed)/capital + f.Variable
}

// GrossProfit inverts the success fee only: the gross profit (the statement
// line after third-party fees, before FF's share) implied by an observed net
// profit at a known capital. A loss pays no success fee, so gross equals net.
func (f Fees) GrossProfit(net, capital float64) float64 {
	if net <= 0 {
		return net
	}
	return net / (1 - f.TierRate(capital))
}

// GrossReturn is GrossProfit as a fraction of capital — the percentage next to
// the statement's Gross Profit line.
func (f Fees) GrossReturn(net, capital float64) float64 {
	if capital <= 0 {
		return 0
	}
	return f.GrossProfit(net, capital) / capital
}
