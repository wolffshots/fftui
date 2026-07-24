package webui

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"github.com/wolffshots/fftui/internal/analytics"
	"github.com/wolffshots/fftui/internal/data"
	"github.com/wolffshots/fftui/internal/format"
	"github.com/wolffshots/fftui/internal/model"
)

const dateLayout = "2006-01-02"

// handleCycles renders the sortable/filterable cycles table. All state lives
// in query params: sort, dir, q.
func (s *Server) handleCycles(w http.ResponseWriter, r *http.Request) {
	snap, lastErr := s.svc.Latest()
	vm := cyclesVM{baseVM: s.base("Cycles", "cycles", snap, lastErr)}
	if snap == nil {
		s.render(w, "cycles", vm)
		return
	}

	q := r.URL.Query()
	vm.Q = q.Get("q")
	col, ok := sortKeys[q.Get("sort")]
	if !ok {
		col = sortStart
	}
	asc := q.Get("dir") == "asc" // default matches the TUI: newest first
	vm.Sort = sortParams[col]
	vm.Dir = "desc"
	if asc {
		vm.Dir = "asc"
	}
	vm.Filtered = vm.Q != ""
	vm.Cols = cycleColumns(col, asc, vm.Q)

	// Always work on a copy — the snapshot's slice is shared across requests
	// and with the TUI.
	cs := append([]model.Cycle(nil), snap.Cycles...)
	cs = filterCycles(cs, vm.Q)
	sortCycles(cs, col, asc)

	vm.Rows = make([]cycleRowVM, len(cs))
	for i, c := range cs {
		vm.Rows[i] = cycleRowVM{
			Code:   c.Code,
			Type:   c.TradeType,
			Start:  c.StartDate.Format(dateLayout),
			End:    c.EndDate.Format(dateLayout),
			ZarIn:  c.ZarIn,
			ZarOut: c.ZarOut,
			Profit: c.NetProfit,
			Return: c.Return(),
			Days:   c.HoldDays(),
		}
	}
	vm.Count = len(cs)
	vm.TotalProfit = sumProfit(cs)
	if !vm.Filtered {
		sum := analytics.Lifetime(cs, s.opts.Rates)
		vm.Summary = &summaryVM{
			Annualised: sum.Annualised,
			WithIdle:   sum.AnnualisedWithIdle,
			Net:        sum.AnnualisedWithIdleAfterTax,
			IdleLabel:  format.Percent(s.opts.Rates.Idle),
			TaxLabel:   format.Percent(s.opts.Rates.Tax),
		}
	}
	s.render(w, "cycles", vm)
}

// handleDetail renders one cycle, mirroring ui/detail.go.
func (s *Server) handleDetail(w http.ResponseWriter, r *http.Request) {
	code := r.PathValue("code")
	snap, lastErr := s.svc.Latest()
	vm := detailVM{baseVM: s.base(code, "detail", snap, lastErr)}
	if snap == nil {
		s.render(w, "detail", vm)
		return
	}
	for _, c := range snap.Cycles {
		if c.Code == code {
			vm.Cycle = detailCycleVM{
				Code:           c.Code,
				Type:           c.TradeType,
				Start:          c.StartDate.Format(dateLayout),
				End:            c.EndDate.Format(dateLayout),
				Days:           c.HoldDays(),
				ZarIn:          c.ZarIn,
				ZarOut:         c.ZarOut,
				Profit:         c.NetProfit,
				Return:         c.Return(),
				AnnualisedHold: c.AnnualisedHold(),
			}
			s.render(w, "detail", vm)
			return
		}
	}
	errorPage(w, http.StatusNotFound, "no cycle with code "+code)
}

// granularities maps the gran query param; keep URL slugs lowercase.
var granSlugs = []struct {
	slug string
	gran analytics.Granularity
}{
	{"year", analytics.Year},
	{"quarter", analytics.Quarter},
	{"month", analytics.Month},
	{"taxyear", analytics.TaxYear},
}

func parseGran(s string) analytics.Granularity {
	for _, g := range granSlugs {
		if g.slug == s {
			return g.gran
		}
	}
	return analytics.Year
}

func analyticsURL(gran string, dead bool) string {
	v := url.Values{}
	v.Set("gran", gran)
	if dead {
		v.Set("dead", "1")
	}
	return "/analytics?" + v.Encode()
}

// handleAnalytics mirrors ui/analytics.go renderContent's call sequence:
// Buckets → variance strips → money-weighted → bootstrap band → in-progress
// floor → planning strip → trend strips → partial warning.
func (s *Server) handleAnalytics(w http.ResponseWriter, r *http.Request) {
	snap, lastErr := s.svc.Latest()
	vm := analyticsVM{baseVM: s.base("Analytics", "analytics", snap, lastErr)}
	if snap == nil {
		s.render(w, "analytics", vm)
		return
	}

	q := r.URL.Query()
	gran := parseGran(q.Get("gran"))
	includeDead := q.Get("dead") == "1"
	granSlug := "year"
	for _, g := range granSlugs {
		if g.gran == gran {
			granSlug = g.slug
		}
		vm.Grans = append(vm.Grans, granVM{
			Label:  g.gran.String(),
			URL:    analyticsURL(g.slug, includeDead),
			Active: g.gran == gran,
		})
	}
	vm.DeadURL = analyticsURL(granSlug, !includeDead)
	vm.Scope = "active only"
	if includeDead {
		vm.Scope = "incl. dead buckets"
	}

	rates := s.opts.Rates
	buckets := analytics.Buckets(snap.Cycles, gran, snap.Now, includeDead, rates)
	if len(buckets) == 0 {
		s.render(w, "analytics", vm)
		return
	}

	vm.IdleHdr = fmt.Sprintf("+Idle@%.0f%%", rates.Idle*100)
	vm.NetHdr = fmt.Sprintf("Net@%.0f%%", rates.Tax*100)
	vm.IdlePct = format.Percent(rates.Idle)
	vm.TaxPct = format.Percent(rates.Tax)

	for _, bk := range buckets {
		vm.Buckets = append(vm.Buckets, bucketVM{
			Label:      bk.Label,
			Partial:    bk.Partial,
			Count:      bk.Count,
			Profit:     bk.TotalProfit,
			Compound:   bk.Compound,
			Annualised: bk.Annualised,
			WithIdle:   bk.AnnualisedWithIdle,
			Net:        bk.AnnualisedWithIdleAfterTax,
		})
		if bk.Partial {
			vm.HasPartial = true
		}
	}

	// Variance strips: arb-only, with-idle, and after-tax take-home.
	v := analytics.Variance(buckets)
	vm.VarN = v.N
	varLine := func(key, title string, vs analytics.VarianceStats) varianceVM {
		return varianceVM{Key: key, Title: title, Mean: vs.Mean, Median: vs.Median, Std: vs.Std, Min: vs.Min, Max: vs.Max}
	}
	vm.Variances = []varianceVM{
		varLine("variance", gran.String()+" variance", v),
		varLine("variance-idle", fmt.Sprintf("+idle@%.0f%%", rates.Idle*100), analytics.VarianceWithIdle(buckets)),
		varLine("variance-net", fmt.Sprintf("net@%.0f%% (after tax)", rates.Tax*100), analytics.VarianceWithIdleAfterTax(buckets)),
	}

	if mwr, ok := analytics.MoneyWeighted(snap.Cycles); ok {
		life := analytics.Lifetime(snap.Cycles, rates)
		vm.MW = &mwVM{Rate: mwr, TimeWeighted: life.Annualised}
	}

	if boot := s.bootstrap(snap); boot.OK {
		vm.Boot = &bootVM{
			ArbLo: boot.Arb.Lo, ArbHi: boot.Arb.Hi,
			NetLo: boot.Net.Lo, NetHi: boot.Net.Hi,
			N: boot.N,
		}
	}

	for _, bk := range buckets {
		if bk.InProgress {
			vm.Floor = &floorVM{
				Label:     bk.Label,
				Floor:     bk.AnnualisedFloor,
				FloorTax:  bk.AnnualisedFloorAfterTax,
				Extrap:    bk.AnnualisedWithIdle,
				ExtrapTax: bk.AnnualisedWithIdleAfterTax,
			}
			break
		}
	}

	vm.Plan = s.planVM(snap)
	vm.Trend = trendVMOf(snap)
	s.render(w, "analytics", vm)
}

// planVM mirrors ui/analytics.go renderPlanning, including the live allowance
// override when a client snapshot is present.
func (s *Server) planVM(snap *data.Snapshot) *planVM {
	allow := s.opts.Allow
	if snap.Client != nil {
		allow.Live = true
		allow.SDAAvailable = snap.Client.SDAAvailable
		allow.FIAAvailable = snap.Client.FIAAvailable
	}
	p := analytics.Plan(snap.Cycles, snap.Now, s.opts.Rates, allow, s.opts.Fees)

	vm := &planVM{
		TaxYearLabel:  p.TaxYearLabel,
		TaxYearProfit: p.TaxYearProfit,
		EstimatedTax:  p.EstimatedTax,
		TaxPct:        format.Percent(s.opts.Rates.Tax),
	}
	if p.TotalLimit > 0 {
		vm.HasAllowance = true
		vm.Year = snap.Now.Year()
		vm.Used = p.Used
		vm.TotalLimit = p.TotalLimit
		vm.UsedFrac = p.Used / p.TotalLimit
		vm.Exhausted = p.Exhausted
		vm.HasExhaust = p.HasExhaust
		if p.HasExhaust {
			vm.ExhaustDate = p.ExhaustDate.Format(dateLayout)
		}
		vm.Live = allow.Live
		vm.SDAAvailable = allow.SDAAvailable
		vm.FIAAvailable = allow.FIAAvailable

		if p.SweetSpot > 0 {
			vm.HasSweetSpot = true
			vm.SweetSpot = p.SweetSpot
			vm.CyclesPerYear = p.CyclesPerYear
			vm.CurrentCapital = p.CurrentCapital
			vm.AboveSpot = p.CurrentCapital > p.SweetSpot
			vm.Extra100kGross = p.Extra100kGross
			vm.Extra100kNet = p.Extra100kNet
		}
		if p.TopTierMin > 0 && p.CurrentCapital > 0 {
			vm.HasFeeLadder = true
			vm.CurrentTier = p.CurrentTier
			vm.TopTier = p.TopTier
			vm.TopTierMin = p.TopTierMin
			vm.ReturnNow = p.ReturnNow
			vm.ReturnAtTop = p.ReturnAtTop
		}
	}
	return vm
}

// trendVMOf mirrors ui/analytics.go renderTrend; nil when there is too little
// data to say anything.
func trendVMOf(snap *data.Snapshot) *trendVM {
	t := analytics.TrendOf(snap.Cycles, snap.Now)
	if t.N == 0 {
		return nil
	}
	vm := &trendVM{
		Slope:        fmt.Sprintf("%+.3fpp", t.Slope90*100),
		N:            t.N,
		Verdict:      "noise (not significant)",
		Recent90:     t.Recent90,
		Prior90:      t.Prior90,
		RecentCycles: t.RecentCycles,
		PriorCycles:  t.PriorCycles,
	}
	if t.Significant {
		if t.Slope90 < 0 {
			vm.Verdict = "significant decay"
			vm.SlopeWarn = true
		} else {
			vm.Verdict = "significant improvement"
		}
	}
	// Live only: is the raw market opportunity itself thinning?
	if snap.MarketYear != nil {
		if recent, overall, ok := analytics.SpreadTrend(snap.MarketYear.History, snap.MarketYear.Period, 30); ok {
			vm.Spread = &spreadTrendVM{Recent: recent, Overall: overall, PeriodDays: snap.MarketYear.Period}
		}
	}
	return vm
}

// handleCharts renders the three sparkline series exactly as ui/charts.go:
// per-cycle return, monthly annualised (partial months excluded), and
// cumulative profit.
func (s *Server) handleCharts(w http.ResponseWriter, r *http.Request) {
	snap, lastErr := s.svc.Latest()
	vm := chartsVM{baseVM: s.base("Charts", "charts", snap, lastErr)}
	if snap == nil {
		s.render(w, "charts", vm)
		return
	}
	cs := snap.Cycles
	if len(cs) == 0 {
		s.render(w, "charts", vm)
		return
	}

	returns := make([]float64, len(cs))
	for i, c := range cs {
		returns[i] = c.Return()
	}

	// Partial months are excluded — their annualised figure is flagged
	// unreliable and would distort the sparkline's min/max scaling.
	monthly := analytics.Buckets(cs, analytics.Month, snap.Now, false, s.opts.Rates)
	monthlyAnn := make([]float64, 0, len(monthly))
	for _, bk := range monthly {
		if bk.Partial {
			continue
		}
		monthlyAnn = append(monthlyAnn, bk.Annualised)
	}

	cum := make([]float64, len(cs))
	var run float64
	for i, c := range cs {
		run += c.NetProfit
		cum[i] = run
	}

	vm.Charts = []chartVM{
		chart("Return % per cycle (chronological)", returns, format.Percent),
		chart("Monthly annualised rate", monthlyAnn, format.Percent),
		chart("Cumulative profit", cum, format.Money),
	}
	vm.Charts[1].Key = "monthly-annualised"
	s.render(w, "charts", vm)
}

func chart(title string, series []float64, fmtFn func(float64) string) chartVM {
	c := chartVM{Title: title, Series: series, Width: chartWidth, N: len(series)}
	if len(series) == 0 {
		return c
	}
	min, max := series[0], series[0]
	for _, v := range series {
		if v < min {
			min = v
		}
		if v > max {
			max = v
		}
	}
	latest := series[len(series)-1]
	c.Neg = latest < 0
	c.Min, c.Max, c.Latest = fmtFn(min), fmtFn(max), fmtFn(latest)
	return c
}

// handleLive mirrors ui/live.go: current cycle, market spread with history
// sparkline, and funds/allowances. In CSV mode (no live data) it shows the
// same hint the TUI does.
func (s *Server) handleLive(w http.ResponseWriter, r *http.Request) {
	snap, lastErr := s.svc.Latest()
	vm := liveVM{baseVM: s.base("Live", "live", snap, lastErr)}
	if snap == nil {
		s.render(w, "live", vm)
		return
	}
	if snap.Client == nil && snap.Market == nil {
		vm.CSVHint = true
		s.render(w, "live", vm)
		return
	}
	if c := snap.Client; c != nil {
		label := c.Status.SecondaryText
		if label == "" {
			label = c.Status.Slug
		}
		cvm := &liveClientVM{
			DotClass:       dotClass(c.Status.Slug),
			Label:          label,
			Description:    c.Status.Description,
			AmountInvested: c.Status.AmountInvested,
			FundsAvailable: c.FundsAvailable,
			TotalProfit:    c.TotalProfit,
			MinimumReturn:  c.MinimumReturn,
			SDAAvailable:   c.SDAAvailable,
			FIAAvailable:   c.FIAAvailable,
			FundsUpdated:   c.FundsUpdated,
		}
		if c.Status.NetProfit != nil {
			cvm.HasNetProfit = true
			cvm.NetProfit = *c.Status.NetProfit
		}
		vm.Client = cvm
	}
	if mk := snap.Market; mk != nil {
		mvm := &liveMarketVM{
			Spread:        mk.Current.Spread,
			LocalPrice:    mk.Current.LocalPrice,
			OffshorePrice: mk.Current.OffshorePrice,
			ExchangeRate:  mk.Current.ExchangeRate,
			Period:        mk.Period,
			Width:         chartWidth,
		}
		if len(mk.History) > 1 {
			mvm.HasHistory = true
			series := make([]float64, len(mk.History))
			min, max := mk.History[0].Spread, mk.History[0].Spread
			for i, p := range mk.History {
				series[i] = p.Spread
				if p.Spread < min {
					min = p.Spread
				}
				if p.Spread > max {
					max = p.Spread
				}
			}
			mvm.Series = series
			mvm.Min, mvm.Max = min, max
			mvm.Latest = series[len(series)-1]
		}
		vm.Market = mvm
	}
	s.render(w, "live", vm)
}

// handleRefresh re-fetches from the source, then bounces back to the page the
// user came from. Errors are stored on the service and surfaced as the error
// banner on the next render, so the redirect is unconditional.
func (s *Server) handleRefresh(w http.ResponseWriter, r *http.Request) {
	// Background context to match the TUI's fetchCmd — the service applies
	// its own 90s timeout, and a client disconnect shouldn't abort a fetch
	// another viewer may be waiting on.
	_, _ = s.svc.Refresh(context.Background())
	http.Redirect(w, r, refererPath(r), http.StatusSeeOther)
}

// refererPath returns the Referer reduced to its path+query (never an
// absolute URL, so it cannot be an open redirect), or /cycles. Paths starting
// with // (or /\) are rejected too — browsers treat those as scheme-relative
// URLs, which would leave the site.
func refererPath(r *http.Request) string {
	if ref := r.Referer(); ref != "" {
		if u, err := url.Parse(ref); err == nil && strings.HasPrefix(u.Path, "/") &&
			!strings.HasPrefix(u.Path, "//") && !strings.HasPrefix(u.Path, `/\`) {
			return u.RequestURI()
		}
	}
	return "/cycles"
}
