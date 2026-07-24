package webui

import (
	"net/url"
	"sort"
	"strings"

	"github.com/wolffshots/fftui/internal/data"
	"github.com/wolffshots/fftui/internal/model"
)

// signClass maps a value to the CSS class used for colouring: "pos" for
// positive, "neg" for negative, "" for zero.
func signClass(v float64) string {
	switch {
	case v > 0:
		return "pos"
	case v < 0:
		return "neg"
	default:
		return ""
	}
}

// ---------------------------------------------------------------------------
// Shared page chrome
// ---------------------------------------------------------------------------

// baseVM carries the chrome every page shares: status strip, tab bar, error
// banner, no-data state, and the footer.
type baseVM struct {
	Title     string
	Active    string // tab slug: cycles | analytics | detail | charts | live
	Version   string
	CSVMode   bool
	NoData    bool   // no snapshot yet — body shows a refresh prompt instead
	Err       string // last refresh error ("" when the last refresh succeeded)
	FetchedAt string
	Status    *statusVM // nil hides the strip (CSV mode / failed live pull)
}

// statusVM is the live header strip, mirroring ui/statusbar.go.
type statusVM struct {
	HasClient bool
	DotClass  string // pos | warn | dim (statusDot's slug switch)
	Label     string
	Invested  float64 // 0 hides the segment
	HasSpread bool
	Spread    float64
	Updated   string
}

// base assembles the shared chrome from the latest snapshot + stored error.
func (s *Server) base(title, active string, snap *data.Snapshot, err error) baseVM {
	vm := baseVM{Title: title, Active: active, Version: s.opts.Version, CSVMode: s.opts.CSVMode}
	if err != nil {
		vm.Err = err.Error()
	}
	if snap == nil {
		vm.NoData = true
		return vm
	}
	vm.FetchedAt = snap.FetchedAt.Format("2006-01-02 15:04:05")
	vm.Status = statusFrom(snap.Client, snap.Market)
	return vm
}

// statusFrom mirrors ui.renderStatusBar: nil (hidden) when there is no live
// data at all, otherwise the client segment and/or the spread segment.
func statusFrom(client *model.ClientStatus, market *model.MarketConditions) *statusVM {
	if client == nil && market == nil {
		return nil
	}
	st := &statusVM{}
	if client != nil {
		st.HasClient = true
		label := client.Status.SecondaryText
		if label == "" {
			label = client.Status.Slug
		}
		st.Label = label
		st.DotClass = dotClass(client.Status.Slug)
		st.Invested = client.Status.AmountInvested
		st.Updated = client.FundsUpdated
	}
	if market != nil {
		st.HasSpread = true
		st.Spread = market.Current.Spread
	}
	return st
}

// dotClass replicates ui.statusDot's slug switch as a CSS class: trading
// (green), loaded/queued (amber), otherwise dim.
func dotClass(slug string) string {
	switch slug {
	case "trade_processing":
		return "pos"
	case "trade_loaded":
		return "warn"
	default:
		return "dim"
	}
}

// ---------------------------------------------------------------------------
// Cycles table: filter + sort (a local mirror of ui/table.go semantics)
// ---------------------------------------------------------------------------

type sortKey int

const (
	sortStart sortKey = iota
	sortCode
	sortType
	sortZarIn
	sortZarOut
	sortProfit
	sortReturn
	sortDays
)

var sortKeys = map[string]sortKey{
	"start": sortStart, "code": sortCode, "type": sortType,
	"zarin": sortZarIn, "zarout": sortZarOut,
	"profit": sortProfit, "return": sortReturn, "days": sortDays,
}

var sortParams = map[sortKey]string{
	sortStart: "start", sortCode: "code", sortType: "type",
	sortZarIn: "zarin", sortZarOut: "zarout",
	sortProfit: "profit", sortReturn: "return", sortDays: "days",
}

// sortCycles is a deliberate local copy of ui.sortCycles' 8-case switch (the
// ui package stays TUI-only; nothing from it is imported here).
func sortCycles(cs []model.Cycle, col sortKey, asc bool) {
	less := func(i, j int) bool {
		a, b := cs[i], cs[j]
		switch col {
		case sortCode:
			return a.Code < b.Code
		case sortType:
			return a.TradeType < b.TradeType
		case sortZarIn:
			return a.ZarIn < b.ZarIn
		case sortZarOut:
			return a.ZarOut < b.ZarOut
		case sortProfit:
			return a.NetProfit < b.NetProfit
		case sortReturn:
			return a.Return() < b.Return()
		case sortDays:
			return a.HoldDays() < b.HoldDays()
		default: // sortStart
			return a.StartDate.Before(b.StartDate)
		}
	}
	sort.SliceStable(cs, func(i, j int) bool {
		if asc {
			return less(i, j)
		}
		return less(j, i)
	})
}

// filterCycles mirrors the TUI filter: case-insensitive substring match on the
// cycle code or trade type.
func filterCycles(cs []model.Cycle, q string) []model.Cycle {
	q = strings.ToLower(strings.TrimSpace(q))
	if q == "" {
		return cs
	}
	out := cs[:0]
	for _, c := range cs {
		if strings.Contains(strings.ToLower(c.Code), q) ||
			strings.Contains(strings.ToLower(c.TradeType), q) {
			out = append(out, c)
		}
	}
	return out
}

func sumProfit(cs []model.Cycle) float64 {
	var t float64
	for _, c := range cs {
		t += c.NetProfit
	}
	return t
}

// cyclesURL builds a /cycles URL carrying the full table state.
func cyclesURL(sortName, dir, q string) string {
	v := url.Values{}
	v.Set("sort", sortName)
	v.Set("dir", dir)
	if q != "" {
		v.Set("q", q)
	}
	return "/cycles?" + v.Encode()
}

type cyclesVM struct {
	baseVM
	Q, Sort, Dir string
	Filtered     bool
	Cols         []colVM
	Rows         []cycleRowVM
	Count        int
	TotalProfit  float64
	// Summary is nil while a filter is active — an annualised rate over a
	// filtered subset is a misleading hybrid, so the footer omits it (same
	// rule as the TUI).
	Summary *summaryVM
}

type colVM struct {
	Key    string // tips key for the header's tooltip ("" = no tooltip)
	Title  string
	URL    string // "" for a non-sortable column (End)
	Active bool
	Arrow  string // ▲ / ▼ on the active column
}

type cycleRowVM struct {
	Code, Type, Start, End string
	ZarIn, ZarOut, Profit  float64
	Return                 float64
	Days                   int
}

type summaryVM struct {
	Annualised, WithIdle, Net float64
	IdleLabel, TaxLabel       string // e.g. "6.00%", "41.00%"
}

// cycleColumns builds the header links. Clicking a new column keeps the
// current direction; clicking the active column flips it (the web analogue of
// the TUI's independent s / S bindings).
func cycleColumns(cur sortKey, asc bool, q string) []colVM {
	dir := func(a bool) string {
		if a {
			return "asc"
		}
		return "desc"
	}
	defs := []struct {
		title string
		tip   string // tips key ("" = no tooltip)
		key   sortKey
		sort  bool
	}{
		{"Code", "", sortCode, true},
		{"Type", "type", sortType, true},
		{"Start", "", sortStart, true},
		{"End", "", 0, false},
		{"ZAR In", "zar-in", sortZarIn, true},
		{"ZAR Out", "zar-out", sortZarOut, true},
		{"Profit", "", sortProfit, true},
		{"Return%", "return", sortReturn, true},
		{"Days", "days", sortDays, true},
	}
	cols := make([]colVM, 0, len(defs))
	for _, d := range defs {
		col := colVM{Key: d.tip, Title: d.title}
		if d.sort {
			linkDir := dir(asc)
			if d.key == cur {
				col.Active = true
				linkDir = dir(!asc) // clicking the active column flips
				if asc {
					col.Arrow = "▲"
				} else {
					col.Arrow = "▼"
				}
			}
			col.URL = cyclesURL(sortParams[d.key], linkDir, q)
		}
		cols = append(cols, col)
	}
	return cols
}

// ---------------------------------------------------------------------------
// Detail
// ---------------------------------------------------------------------------

type detailVM struct {
	baseVM
	Cycle detailCycleVM
}

type detailCycleVM struct {
	Code, Type, Start, End string
	Days                   int
	ZarIn, ZarOut, Profit  float64
	Return, AnnualisedHold float64
}

// ---------------------------------------------------------------------------
// Analytics
// ---------------------------------------------------------------------------

type analyticsVM struct {
	baseVM
	Grans      []granVM
	Scope      string // "active only" / "incl. dead buckets"
	DeadURL    string // toggle link
	IdleHdr    string // "+Idle@6%"
	NetHdr     string // "Net@41%"
	IdlePct    string // "6.00%"
	TaxPct     string // "41.00%"
	Buckets    []bucketVM
	HasPartial bool
	Variances  []varianceVM
	VarN       int
	MW         *mwVM
	Boot       *bootVM
	Floor      *floorVM
	Plan       *planVM
	Trend      *trendVM
}

type granVM struct {
	Label  string
	URL    string
	Active bool
}

type bucketVM struct {
	Label      string
	Partial    bool
	Count      int
	Profit     float64
	Compound   float64
	Annualised float64
	WithIdle   float64
	Net        float64
}

type varianceVM struct {
	Key                         string // tips key for the title's tooltip
	Title                       string
	Mean, Median, Std, Min, Max float64
}

type mwVM struct {
	Rate, TimeWeighted float64
}

type bootVM struct {
	ArbLo, ArbHi, NetLo, NetHi float64
	N                          int
}

type floorVM struct {
	Label             string
	Floor, FloorTax   float64
	Extrap, ExtrapTax float64
}

// planVM mirrors ui/analytics.go renderPlanning line by line.
type planVM struct {
	TaxYearLabel  string
	TaxYearProfit float64
	EstimatedTax  float64
	TaxPct        string

	HasAllowance bool
	Year         int
	Used         float64
	TotalLimit   float64
	UsedFrac     float64
	Exhausted    bool
	HasExhaust   bool
	ExhaustDate  string
	Live         bool
	SDAAvailable float64
	FIAAvailable float64

	HasSweetSpot   bool
	SweetSpot      float64
	CyclesPerYear  int
	CurrentCapital float64
	AboveSpot      bool
	Extra100kGross float64
	Extra100kNet   float64

	HasFeeLadder bool
	CurrentTier  float64
	TopTier      float64
	TopTierMin   float64
	ReturnNow    float64
	ReturnAtTop  float64
}

type trendVM struct {
	Slope        string // "+0.123pp"
	SlopeWarn    bool   // significant decay → warn styling
	N            int
	Verdict      string
	Recent90     float64
	Prior90      float64
	RecentCycles int
	PriorCycles  int
	Spread       *spreadTrendVM // live only
}

type spreadTrendVM struct {
	Recent, Overall float64 // already in percent units (spreadFmt)
	PeriodDays      int
}

// ---------------------------------------------------------------------------
// Charts
// ---------------------------------------------------------------------------

type chartsVM struct {
	baseVM
	Charts []chartVM
}

type chartVM struct {
	Key              string // tips key for the title's tooltip ("" = none)
	Title            string
	Series           []float64
	Width            int
	Neg              bool // latest < 0 → red sparkline
	Min, Max, Latest string
	N                int
}

// ---------------------------------------------------------------------------
// Live
// ---------------------------------------------------------------------------

type liveVM struct {
	baseVM
	CSVHint bool // no live data at all — show the CSV-mode hint
	Client  *liveClientVM
	Market  *liveMarketVM
}

type liveClientVM struct {
	DotClass       string
	Label          string
	Description    string
	AmountInvested float64
	HasNetProfit   bool
	NetProfit      float64
	FundsAvailable float64
	TotalProfit    float64
	MinimumReturn  float64
	SDAAvailable   float64
	FIAAvailable   float64
	FundsUpdated   string
}

type liveMarketVM struct {
	Spread        float64
	LocalPrice    float64
	OffshorePrice float64
	ExchangeRate  float64
	HasHistory    bool // more than one point, mirroring the TUI
	Series        []float64
	Width         int
	Min, Max      float64
	Latest        float64
	Period        int
}
