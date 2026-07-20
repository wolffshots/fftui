package model

import (
	"math"
	"time"
)

// Cycle is one arbitrage trade. NetProfit and Return are always recomputed from
// ZarIn/ZarOut; any stored values in a source are treated as display hints only.
type Cycle struct {
	Code      string
	TradeType string
	StartDate time.Time
	EndDate   time.Time
	ZarIn     float64
	ZarOut    float64
	NetProfit float64
}

// Return is the fractional return for this single cycle.
func (c Cycle) Return() float64 { return c.NetProfit / c.ZarIn }

// HoldDays is the inclusive number of calendar days the cycle was open.
func (c Cycle) HoldDays() int {
	return int(c.EndDate.Sub(c.StartDate).Hours()/24) + 1
}

// AnnualisedHold annualises this cycle's return over its own holding days, as a
// nominal annual rate compounded monthly (the same convention as the analytics
// figures). This is the "best-case, no-idle" figure and must only ever be shown
// in the detail view — never in the savings-comparable headline figures.
func (c Cycle) AnnualisedHold() float64 {
	days := c.HoldDays()
	if days <= 0 {
		return 0
	}
	annualFactor := math.Pow(1+c.Return(), 365.0/float64(days))
	return 12 * (math.Pow(annualFactor, 1.0/12.0) - 1)
}
