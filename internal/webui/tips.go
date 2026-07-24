package webui

import (
	"fmt"
	"html/template"
	"sync/atomic"
)

// tips is the single source of truth for the jargon tooltips rendered by the
// template `tip` func. Copy is grounded in internal/analytics (see each file's
// doc comments) — descriptive only, never advice.
var tips = map[string]string{
	"cyc": "The number of cycles that started in this period. A cycle " +
		"spanning a period boundary still counts once, in the period it " +
		"started; its return and profit are split across the periods it " +
		"touches.",
	"compound": "Total growth over the period from chaining every cycle's " +
		"return together, as if each payout were re-deployed into the next " +
		"cycle. Raw growth, not annualised.",
	"annualised": "The period's compound growth restated as a nominal annual " +
		"rate compounded monthly (the way a bank quotes a savings rate), " +
		"measured over elapsed calendar days. Days with no cycle open count " +
		"at 0%, so idle time between cycles drags this figure down.",
	"idle": "The same annualised figure, but days with no cycle open are " +
		"credited at the configured idle rate instead of 0% — modelling the " +
		"cash earning interest in a savings account between trades.",
	"net": "The with-idle rate after applying the configured marginal tax " +
		"rate to all returns, arb profit and idle interest alike — the " +
		"modelled take-home rate.",
	"variance": "Spread statistics of the arb-only annualised rate across " +
		"the full buckets above. Periods shorter than 20 days are excluded: " +
		"annualising a handful of days gives an unreliable, exploding figure.",
	"variance-idle": "The same spread statistics over each bucket's " +
		"with-idle rate (idle days credited at the configured rate instead " +
		"of 0%).",
	"variance-net": "The same spread statistics over each bucket's after-tax " +
		"take-home rate (arb plus idle interest, both taxed at the " +
		"configured marginal rate).",
	"irr": "The internal rate of return of the account's actual external " +
		"cash flows: the initial deposit, each later top-up or withdrawal " +
		"inferred from changing cycle sizes, and the final value, at their " +
		"real dates. Unlike the time-weighted annualised figure it weights " +
		"every stretch by the capital actually in it, so the two diverge " +
		"when cycle sizes jump.",
	"bootstrap": "The 5th–95th percentile of the lifetime annualised rate " +
		"when the observed per-cycle returns are resampled with replacement " +
		"10,000 times, keeping the real calendar span and trading cadence " +
		"fixed. Shows how much the headline figure depends on which cycles " +
		"happened to land.",
	"floor": "The with-idle rate for the current period assuming no further " +
		"trades: the cycles recorded so far plus idle interest for the " +
		"entire remainder of the period, with no extrapolation of the " +
		"trading pace. The realised figure can only end up at or above this.",
	"taxable": "Arb profit realised in this South African tax year (1 March " +
		"to end February): a cycle's whole profit lands in the tax year its " +
		"end date falls in. The estimate applies the configured marginal " +
		"rate to that profit.",
	"allowance": "The combined SDA+FIA exchange-control pool for the " +
		"calendar year. Each cycle sends its capital offshore afresh, so " +
		"every cycle consumes that much allowance again; without the live " +
		"API, usage is inferred by summing the year's cycle capital.",
	"sda": "Single Discretionary Allowance — the slice of the annual " +
		"exchange-control allowance usable without a tax-clearance " +
		"application. Every cycle sends its capital offshore afresh, so " +
		"each cycle consumes allowance again.",
	"fia": "Foreign Investment Allowance — the larger annual " +
		"exchange-control allowance, requiring a SARS tax-clearance (AIT) " +
		"application that Future Forex files. Planning treats SDA and FIA " +
		"as one combined annual pool.",
	"sweet-spot": "The combined annual SDA+FIA allowance divided by the " +
		"trailing year's cycle count — the per-cycle capital above which " +
		"the allowance pool runs out before the year does, so additional " +
		"capital can no longer be fully re-deployed each cycle.",
	"capital": "The latest cycle's capital, with the projected change in " +
		"annual profit from adding R100k. Below the sweet spot the extra " +
		"rand re-deploys through every cycle; above it, deployment is " +
		"allowance-capped, so only the improved fee position remains.",
	"fee-ladder": "Future Forex's success fee is a percentage of each " +
		"cycle's gross profit, stepping down through capital tiers as cycle " +
		"size grows. The arrows compare the modelled net return per cycle " +
		"at the current capital with the same figure at the top (cheapest) " +
		"tier's minimum capital.",
	"type": "The trade structure label as reported by Future Forex's cycle " +
		"export (its Trade Type column).",
	"zar-in": "The rand amount committed at the start of the cycle — the " +
		"capital sent offshore.",
	"zar-out": "The rand amount paid back when the cycle closed. Net profit " +
		"is recomputed as ZAR out minus ZAR in.",
	"return": "The simple per-cycle return: net profit divided by the ZAR " +
		"put in. Not annualised, so it is not comparable to the annualised " +
		"figures.",
	"days": "Calendar days the cycle was open, counting both the start and " +
		"end date — a same-day cycle shows 1.",
	"monthly-annualised": "Each month's arb-only annualised rate. Months " +
		"covering fewer than 20 elapsed days — including a just-started " +
		"current month — are excluded, since annualising a handful of days " +
		"gives an unreliable figure.",
	"min-return": "The minimum return threshold the Future Forex API " +
		"reports for the account, as a fraction of cycle capital.",
	"return-trend": "A least-squares slope fitted to per-cycle returns over " +
		"the trailing year, quoted as the change per 90 days, with a t-test " +
		"(roughly the 95% level) separating real decay or improvement from " +
		"noise.",
	"spread": "The percentage gap between the local and offshore price of " +
		"the same dual-listed asset — the raw market margin a cycle " +
		"captures before fees.",
	"spread-trend": "The mean market spread over roughly the last 30 days " +
		"versus the mean over the whole period, from the live " +
		"market-conditions feed — whether the raw arb opportunity is " +
		"thinning or holding.",
}

// tipSeq makes tooltip ids unique even when the same key renders twice on a
// page (e.g. "idle"/"net" appear in both the analytics header and the
// variance strip), keeping every aria-describedby target unambiguous.
var tipSeq atomic.Uint64

// tipHTML renders label wrapped in a hover/focus/tap tooltip carrying the tip
// text for key (see static/style.css .tip/.tipbox and static/app.js). An
// unknown key degrades to the plain escaped label.
func tipHTML(key, label string) template.HTML {
	text, ok := tips[key]
	if !ok {
		return template.HTML(template.HTMLEscapeString(label))
	}
	id := fmt.Sprintf("tip-%s-%d", key, tipSeq.Add(1))
	return template.HTML(fmt.Sprintf(
		`<span class="tip" tabindex="0" aria-describedby="%s">%s<span class="tipbox" role="tooltip" id="%s">%s</span></span>`,
		id, template.HTMLEscapeString(label), id, template.HTMLEscapeString(text)))
}
