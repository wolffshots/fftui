package ui

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/wolffshots/fftui/internal/analytics"
	"github.com/wolffshots/fftui/internal/data"
	"github.com/wolffshots/fftui/internal/model"
)

func testModel(t *testing.T) RootModel { return testModelAuto(t, 0) }

// testModelAuto is testModel with an auto-refresh interval configured.
func testModelAuto(t *testing.T, every time.Duration) RootModel {
	t.Helper()
	src := model.NewCSVSource("../../testdata/cycles.csv")
	cs, err := src.Fetch(context.Background())
	if err != nil {
		t.Fatalf("fetch: %v", err)
	}
	now := time.Date(2026, 7, 10, 0, 0, 0, 0, time.UTC)
	m := New(data.NewService(src), now, analytics.Rates{Idle: 0.06, Tax: 0.41},
		analytics.Allowances{SDALimit: 2_000_000, FIALimit: 10_000_000}, analytics.DefaultFees(), every)
	// Simulate the async load + a terminal size.
	mm, _ := m.Update(cyclesLoadedMsg{cycles: cs})
	m = mm.(RootModel)
	mm, _ = m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	return mm.(RootModel)
}

func send(m RootModel, msg tea.Msg) RootModel {
	mm, _ := m.Update(msg)
	return mm.(RootModel)
}

func rune1(r rune) tea.KeyMsg { return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}} }

// TestAllViewsRender drives tab switching and asserts each view produces output
// without panicking.
func TestAllViewsRender(t *testing.T) {
	m := testModel(t)
	for _, k := range []rune{'1', '2', '3', '4', '5', '6'} {
		m = send(m, rune1(k))
		out := m.View()
		if strings.TrimSpace(out) == "" {
			t.Fatalf("view %c rendered empty", k)
		}
	}
}

// TestTableSortAndFilter exercises sort cycling, direction toggle, and the
// filter text input without panicking, and checks filtering narrows the set.
func TestTableSortAndFilter(t *testing.T) {
	m := testModel(t)
	m = send(m, rune1('1'))
	m = send(m, rune1('s')) // change sort column
	m = send(m, rune1('S')) // toggle direction

	m = send(m, rune1('/')) // open filter
	if !m.table.filtering {
		t.Fatal("expected filtering mode after /")
	}
	for _, r := range "FX001" {
		m = send(m, rune1(r))
	}
	m = send(m, tea.KeyMsg{Type: tea.KeyEnter}) // apply
	if m.table.filtering {
		t.Fatal("filter should be applied after enter")
	}
	if len(m.table.visible) == 0 || len(m.table.visible) == 43 {
		t.Fatalf("filter did not narrow: %d rows", len(m.table.visible))
	}
}

// TestEscClearsAppliedFilter applies a filter with enter, then checks esc from
// normal table mode clears it (§6: "/ filter; esc clears").
func TestEscClearsAppliedFilter(t *testing.T) {
	m := testModel(t)
	m = send(m, rune1('/'))
	for _, r := range "FX001" {
		m = send(m, rune1(r))
	}
	m = send(m, tea.KeyMsg{Type: tea.KeyEnter}) // apply
	if len(m.table.visible) == 43 {
		t.Fatal("filter did not narrow")
	}
	m = send(m, tea.KeyMsg{Type: tea.KeyEsc})
	if m.table.filter.Value() != "" {
		t.Fatalf("esc should clear the applied filter, still %q", m.table.filter.Value())
	}
	if len(m.table.visible) != 43 {
		t.Fatalf("all rows should be visible after esc, got %d", len(m.table.visible))
	}
}

// TestDateWindowIndicator: an active --from/--to window is flagged on the tab
// bar (every view is filtered, so the marker is global); no window, no marker.
func TestDateWindowIndicator(t *testing.T) {
	m := testModel(t)
	if strings.Contains(m.renderTabs(), "window") {
		t.Fatal("tab bar shows a window marker with no date range set")
	}
	m.svc.SetDateRange(time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC), time.Time{})
	if out := m.renderTabs(); !strings.Contains(out, "window 2026-03-01 → …") {
		t.Fatalf("tab bar missing window marker:\n%s", out)
	}
}

// TestTabBarFitsWidth: with every marker lit on an 80-column terminal the bar
// stays inside the width, and the failure marker is the one that survives the
// cut — the window is standing state the user set and can re-read, while the
// failure is the only sign a silent refresh stopped working.
func TestTabBarFitsWidth(t *testing.T) {
	m := testModelAuto(t, 5*time.Minute)
	m = send(m, tea.WindowSizeMsg{Width: 80, Height: 40}) // renderTabs caps on m.width
	m.svc.SetDateRange(
		time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
		time.Date(2026, 6, 30, 0, 0, 0, 0, time.UTC))
	m.refreshErr = errors.New("boom")

	out := m.renderTabs()
	if !strings.Contains(out, "refresh failed") {
		t.Fatalf("failure marker truncated away at 80 columns:\n%s", out)
	}
	for _, line := range strings.Split(out, "\n") {
		if w := lipgloss.Width(line); w > 80 {
			t.Fatalf("tab bar line is %d columns, want <= 80:\n%s", w, line)
		}
	}
}

// TestDateWindowInteractive: `w` opens the footer editor; a valid window
// narrows the data from the cached fetch (no refetch), a bad value keeps the
// editor open with an error, and wiping the value clears the window.
func TestDateWindowInteractive(t *testing.T) {
	svc := data.NewService(model.NewCSVSource("../../testdata/cycles.csv"))
	snap, err := svc.Refresh(context.Background())
	if err != nil {
		t.Fatalf("refresh: %v", err)
	}
	now := time.Date(2026, 7, 10, 0, 0, 0, 0, time.UTC)
	m := New(svc, now, analytics.Rates{Idle: 0.06, Tax: 0.41},
		analytics.Allowances{SDALimit: 2_000_000, FIALimit: 10_000_000}, analytics.DefaultFees(), 0)
	m = send(m, cyclesLoadedMsg{cycles: snap.Cycles, now: snap.Now})
	m = send(m, tea.WindowSizeMsg{Width: 120, Height: 40})

	// Open the editor and apply an open-ended window.
	m = send(m, rune1('w'))
	if !m.editingWindow {
		t.Fatal("w did not open the window editor")
	}
	m = send(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("2026-01-01..")})
	mm, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = mm.(RootModel)
	if m.editingWindow {
		t.Fatal("enter did not close the editor")
	}
	if cmd == nil {
		t.Fatal("apply issued no reload command")
	}
	m = send(m, cmd())
	if got := len(m.table.all); got != 14 {
		t.Fatalf("table has %d cycles after windowing, want 14", got)
	}
	if !strings.Contains(m.View(), "window 2026-01-01 → …") {
		t.Fatal("tab bar missing the applied window")
	}

	// A bad value keeps the editor open and shows the error.
	m = send(m, rune1('w'))
	m = send(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("junk")})
	m = send(m, tea.KeyMsg{Type: tea.KeyEnter})
	if !m.editingWindow || m.windowErr == "" {
		t.Fatalf("bad input: editing=%v err=%q; want still editing with an error", m.editingWindow, m.windowErr)
	}
	m = send(m, tea.KeyMsg{Type: tea.KeyEsc})

	// Reopen (prefilled with the active window), wipe it, apply: full set back.
	m = send(m, rune1('w'))
	if got := m.windowInput.Value(); got != "2026-01-01.." {
		t.Fatalf("editor prefill = %q, want 2026-01-01..", got)
	}
	m = send(m, tea.KeyMsg{Type: tea.KeyCtrlU})
	mm, cmd = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = mm.(RootModel)
	m = send(m, cmd())
	if got := len(m.table.all); got != 43 {
		t.Fatalf("table has %d cycles after clearing, want 43", got)
	}
}

// TestParseWindow pins the editor grammar: from..to, either side optional.
func TestParseWindow(t *testing.T) {
	mar := time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC)
	jun := time.Date(2026, 6, 30, 0, 0, 0, 0, time.UTC)
	zero := time.Time{}
	cases := []struct {
		in       string
		from, to time.Time
		wantErr  bool
	}{
		{"", zero, zero, false},
		{"2026-03-01..2026-06-30", mar, jun, false},
		{"2026-03-01..", mar, zero, false},
		{"..2026-06-30", zero, jun, false},
		{" 2026-03-01 .. ", mar, zero, false},
		{"2026-03-01", zero, zero, true},             // no separator
		{"junk..", zero, zero, true},                 // bad date
		{"2026-06-30..2026-03-01", zero, zero, true}, // inverted
	}
	for _, tc := range cases {
		from, to, err := parseWindow(tc.in)
		if (err != nil) != tc.wantErr {
			t.Errorf("parseWindow(%q) err = %v, wantErr %v", tc.in, err, tc.wantErr)
			continue
		}
		if err == nil && (!from.Equal(tc.from) || !to.Equal(tc.to)) {
			t.Errorf("parseWindow(%q) = %v..%v, want %v..%v", tc.in, from, to, tc.from, tc.to)
		}
	}
}

// TestEnterOpensDetail selects a row and opens the detail view.
func TestEnterOpensDetail(t *testing.T) {
	m := testModel(t)
	m = send(m, rune1('1'))
	m = send(m, tea.KeyMsg{Type: tea.KeyEnter})
	if m.active != viewDetail {
		t.Fatalf("expected detail view, got %v", m.active)
	}
	if !m.detail.hasSel {
		t.Fatal("detail has no selected cycle")
	}
	// q from detail returns to the table rather than quitting.
	m = send(m, rune1('q'))
	if m.active != viewTable {
		t.Fatalf("q in detail should return to table, got %v", m.active)
	}
}

// TestAnalyticsToggles cycles granularity and the active/dead toggle.
func TestAnalyticsToggles(t *testing.T) {
	m := testModel(t)
	m = send(m, rune1('2'))
	start := m.analytics.gran
	m = send(m, tea.KeyMsg{Type: tea.KeyTab})
	if m.analytics.gran == start {
		t.Fatal("tab did not change granularity")
	}
	m = send(m, rune1('a'))
	if !m.analytics.includeDead {
		t.Fatal("a did not toggle include-dead")
	}
	if strings.TrimSpace(m.View()) == "" {
		t.Fatal("analytics view empty")
	}
}

// TestAnalyticsShowsMoneyWeighted asserts the lifetime IRR line renders. The
// reference data reinvests every payout in full, so the money-weighted figure
// must read the same as the arb-only annualised (9.78%).
func TestAnalyticsShowsMoneyWeighted(t *testing.T) {
	m := testModel(t)
	out := m.analytics.renderContent()
	if !strings.Contains(out, "money-weighted (IRR)") {
		t.Fatal("analytics view missing money-weighted line")
	}
	if !strings.Contains(out, "9.78%") {
		t.Fatal("money-weighted line missing expected 9.78% figure")
	}
}

// TestAnalyticsPlanningStrip asserts the planning box renders the tax-year,
// SDA and sweet-spot lines, and that the granularity cycle includes Tax year.
func TestAnalyticsPlanningStrip(t *testing.T) {
	m := testModel(t)
	m = send(m, rune1('2'))
	out := m.analytics.renderContent()
	for _, want := range []string{"TY2027 taxable profit", "allowance 2026", "capital sweet spot", "usage inferred from cycle history", "fee ladder"} {
		if !strings.Contains(out, want) {
			t.Errorf("planning strip missing %q", want)
		}
	}
	// With a live snapshot the strip shows the actual SDA/FIA balances instead.
	m.analytics.client = &model.ClientStatus{SDAAvailable: 1_644_450, FIAAvailable: 7_145_861.97}
	out = m.analytics.renderContent()
	for _, want := range []string{"SDA left", "FIA left"} {
		if !strings.Contains(out, want) {
			t.Errorf("live planning strip missing %q", want)
		}
	}
	for i := 0; i < 3; i++ {
		m = send(m, tea.KeyMsg{Type: tea.KeyTab})
	}
	if m.analytics.gran != analytics.TaxYear {
		t.Fatalf("three tabs from Year should land on Tax year, got %v", m.analytics.gran)
	}
	if !strings.Contains(m.analytics.renderContent(), "TY2025") {
		t.Error("tax-year granularity should show TY2025 bucket")
	}
}

// TestAnalyticsStatsStrips asserts the bootstrap band and trend strip render,
// including the live market-spread line when a year of history is present.
func TestAnalyticsStatsStrips(t *testing.T) {
	m := testModel(t)
	out := m.analytics.renderContent()
	for _, want := range []string{"bootstrap 90% band", "return trend", "90d vs prior 90d"} {
		if !strings.Contains(out, want) {
			t.Errorf("stats strips missing %q", want)
		}
	}
	if strings.Contains(out, "market spread") {
		t.Error("market spread line should need live history")
	}

	points := make([]model.MarketPoint, 60)
	for i := range points {
		points[i].Spread = 0.9
	}
	m.analytics.marketYear = &model.MarketConditions{History: points, Period: 365}
	if !strings.Contains(m.analytics.renderContent(), "market spread") {
		t.Error("market spread line missing with live history")
	}
}

// TestLiveViewRendersData feeds a live snapshot (client status + market) and
// checks the Live view and the status-bar strip show it.
func TestLiveViewRendersData(t *testing.T) {
	m := testModel(t)
	net := 123.45
	client := &model.ClientStatus{
		Status: model.TradeStatus{
			Slug:           "trade_loaded",
			SecondaryText:  "Awaiting market conditions",
			Description:    "Your funds are currently queued to trade.",
			AmountInvested: 119422.50,
			NetProfit:      &net,
		},
		FundsAvailable: 119422.50,
		FundsUpdated:   "Last updated 12:00 on 07 Jul 2026",
		TotalProfit:    19422.50,
		MinimumReturn:  0.1,
		SDAAvailable:   500000,
		FIAAvailable:   4200000,
	}
	market := &model.MarketConditions{
		Current: model.MarketPoint{Spread: 0.82, LocalPrice: 16.59, OffshorePrice: 0.999, ExchangeRate: 16.47},
		History: []model.MarketPoint{{Spread: 0.68}, {Spread: 0.71}, {Spread: 0.82}},
		Period:  7,
	}
	mm, _ := m.Update(cyclesLoadedMsg{cycles: m.table.all, client: client, market: market})
	m = mm.(RootModel)
	m = send(m, tea.WindowSizeMsg{Width: 120, Height: 40})

	// Status bar (visible on every view) shows the status text.
	if !strings.Contains(m.View(), "Awaiting market conditions") {
		t.Fatal("status bar missing current-cycle status")
	}
	// Live view shows the spread and funds sections.
	m = send(m, rune1('5'))
	out := m.View()
	for _, want := range []string{"Market conditions", "0.82%", "Funds & allowances", "Minimum return"} {
		if !strings.Contains(out, want) {
			t.Errorf("live view missing %q", want)
		}
	}
}

// TestReturnsView projects the ladder at a known spread and checks the row for
// R200,000 against the fee waterfall computed by hand: gross earnings R1,600.00,
// third-party R460.00 + R530.00, gross profit R610.00, FF 30% = R183.00, net
// R427.00 (0.21%/cycle).
func TestReturnsView(t *testing.T) {
	m := testModel(t)
	market := &model.MarketConditions{Current: model.MarketPoint{Spread: 0.80}, Period: 7}
	mm, _ := m.Update(cyclesLoadedMsg{cycles: m.table.all, market: market})
	m = send(mm.(RootModel), tea.WindowSizeMsg{Width: 130, Height: 60})
	m = send(m, rune1('6'))

	out := m.returns.render()
	for _, want := range []string{
		"0.80%",           // the projected spread, from the live feed
		"R1,600.00",       // gross earnings at R200k
		"-R990.00",        // third-party: 0.23% × R200k + R530
		"R610.00",         // gross profit
		"-R183.00",        // FF success fee, 30% of gross profit
		"R427.00",         // net profit
		"R92,982.46",      // break-even capital at this spread
		"instant EFT",     // the fixed-fee constituent parts
		"GROSS PROFIT",    // what FF's share is a cut of
		"up to R150k 35%", // the success-fee ladder
	} {
		if !strings.Contains(out, want) {
			t.Errorf("returns view missing %q", want)
		}
	}
}

// TestReturnsViewCSVFallback: with no live feed the view projects the trailing
// average spread backed out of the cycles instead of going blank.
func TestReturnsViewCSVFallback(t *testing.T) {
	m := testModel(t) // no client/market — CSV mode
	out := m.returns.render()
	if !strings.Contains(out, "no live feed in CSV mode") {
		t.Errorf("expected the CSV-mode spread source, got:\n%s", out)
	}
}

// TestStatusBarSingleLine checks the live strip clips to one row on a narrow
// terminal instead of wrapping and stealing a body line.
func TestStatusBarSingleLine(t *testing.T) {
	client := &model.ClientStatus{
		Status:       model.TradeStatus{Slug: "trade_loaded", SecondaryText: "Awaiting market conditions", AmountInvested: 119422.50},
		FundsUpdated: "Last updated 12:00 on 07 Jul 2026",
	}
	market := &model.MarketConditions{Current: model.MarketPoint{Spread: 0.83}}
	for _, w := range []int{40, 60, 80, 120} {
		bar := renderStatusBar(client, market, w)
		if h := lipgloss.Height(bar); h != 1 {
			t.Errorf("width %d: status bar is %d rows, want 1:\n%s", w, h, bar)
		}
		if lipgloss.Width(bar) > w {
			t.Errorf("width %d: status bar width %d exceeds terminal", w, lipgloss.Width(bar))
		}
	}
}

// TestLiveViewCSVHint shows a hint when there's no live data (CSV mode).
func TestLiveViewCSVHint(t *testing.T) {
	m := testModel(t) // testModel loads via cyclesLoadedMsg with nil client/market
	m = send(m, rune1('5'))
	if !strings.Contains(m.View(), "only available from the live API") {
		t.Fatal("expected CSV-mode hint in live view")
	}
}

// TestRefreshUpdatesNow: a fetch carries its own "today", so a long-running
// session's elapsed-day counts don't stay frozen at the launch date.
func TestRefreshUpdatesNow(t *testing.T) {
	m := testModel(t)
	later := time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC)
	m = send(m, cyclesLoadedMsg{cycles: m.table.all, now: later})
	if !m.now.Equal(later) || !m.analytics.now.Equal(later) || !m.charts.now.Equal(later) {
		t.Errorf("now not propagated: root=%v analytics=%v charts=%v",
			m.now, m.analytics.now, m.charts.now)
	}
	// A zero now (pre-fetched embed, LoadedMsg) must not clobber the clock.
	m = send(m, cyclesLoadedMsg{cycles: m.table.all})
	if !m.now.Equal(later) {
		t.Errorf("zero-now load reset the clock to %v", m.now)
	}
}

// TestFilteredFooterHidesAnnualised: annualised rates over a filtered subset
// are a misleading hybrid, so the footer drops them while a filter is active.
func TestFilteredFooterHidesAnnualised(t *testing.T) {
	m := testModel(t)
	if !strings.Contains(m.table.view(), "annualised ") {
		t.Fatal("unfiltered footer should show annualised rates")
	}
	m = send(m, rune1('/'))
	for _, r := range "FX001" {
		m = send(m, rune1(r))
	}
	m = send(m, tea.KeyMsg{Type: tea.KeyEnter})
	out := m.table.view()
	if !strings.Contains(out, "annualised n/a") || strings.Contains(out, "+idle@") {
		t.Errorf("filtered footer should hide annualised rates, got:\n%s", out)
	}
}

// TestResizeNoPanic reflows across a range of sizes including tiny terminals.
func TestResizeNoPanic(t *testing.T) {
	m := testModel(t)
	for _, sz := range [][2]int{{40, 10}, {200, 60}, {10, 5}, {80, 24}} {
		m = send(m, tea.WindowSizeMsg{Width: sz[0], Height: sz[1]})
		for _, k := range []rune{'1', '2', '3', '4', '5', '6'} {
			m = send(m, rune1(k))
			_ = m.View()
		}
	}
}

// TestAutoRefreshTick: a tick starts a quiet background fetch (no loading
// screen) and re-arms the next tick; a tick landing while a fetch is already
// in flight only re-arms.
func TestAutoRefreshTick(t *testing.T) {
	m := testModelAuto(t, 30*time.Second)
	mm, cmd := m.Update(autoRefreshMsg{seq: m.refreshSeq})
	m = mm.(RootModel)
	if !m.refreshing || m.loading {
		t.Fatalf("refreshing=%v loading=%v; want a quiet background fetch", m.refreshing, m.loading)
	}
	if cmd == nil {
		t.Fatal("tick did not re-arm the loop")
	}
	mm, cmd = m.Update(autoRefreshMsg{seq: m.refreshSeq})
	m = mm.(RootModel)
	if cmd == nil {
		t.Fatal("tick during an in-flight fetch should still re-arm")
	}
}

// TestAutoRefreshToggle: R pauses the loop (a pending tick dies) and resumes
// it with a fresh chain — the pre-pause tick must die after the resume too, or
// two chains would tick; without --refresh-interval configured R is a no-op.
func TestAutoRefreshToggle(t *testing.T) {
	m := testModelAuto(t, 30*time.Second)
	if !m.autoRefresh {
		t.Fatal("auto-refresh should start enabled when an interval is set")
	}
	prePause := m.refreshSeq
	m = send(m, rune1('R'))
	if m.autoRefresh {
		t.Fatal("R did not pause auto-refresh")
	}
	if _, cmd := m.Update(autoRefreshMsg{seq: m.refreshSeq}); cmd != nil {
		t.Fatal("tick while paused should let the loop die")
	}
	mm, cmd := m.Update(rune1('R'))
	m = mm.(RootModel)
	if !m.autoRefresh || cmd == nil {
		t.Fatalf("resume: autoRefresh=%v cmd=%v; want enabled with a re-armed tick", m.autoRefresh, cmd)
	}
	if _, cmd := m.Update(autoRefreshMsg{seq: prePause}); cmd != nil {
		t.Fatal("pre-pause tick re-armed after the resume; two chains would tick")
	}
	if _, cmd := m.Update(autoRefreshMsg{seq: m.refreshSeq}); cmd == nil {
		t.Fatal("the fresh chain's tick should still re-arm")
	}

	off := testModel(t)
	mm, _ = off.Update(rune1('R'))
	off = mm.(RootModel)
	if off.autoRefresh || off.refreshing {
		t.Fatal("R should be a no-op with no interval configured")
	}
}

// TestAutoRefreshQuietFailure: a failed background refresh keeps the last good
// data on screen (the service kept the snapshot) and flags the tab bar instead
// of switching to the error screen; a manual r clears the marker (its outcome
// supersedes it), and a fetched load clears it too.
func TestAutoRefreshQuietFailure(t *testing.T) {
	m := testModelAuto(t, 30*time.Second)
	m = send(m, autoRefreshMsg{seq: m.refreshSeq})
	m = send(m, fetchErrMsg{err: errors.New("boom"), background: true})
	if m.err != nil {
		t.Fatalf("background failure set err=%v; the error screen is for manual fetches", m.err)
	}
	if m.refreshErr == nil {
		t.Fatal("background failure not recorded")
	}
	if !strings.Contains(m.renderTabs(), "refresh failed") {
		t.Fatal("tab bar missing the failure marker")
	}
	if strings.Contains(m.View(), "fetch failed:") {
		t.Fatal("error screen shown for a background failure")
	}

	// r retries: the stale marker must not linger on the loading screen or
	// double up with the error screen if the retry also fails.
	m = send(m, rune1('r'))
	if m.refreshErr != nil {
		t.Fatal("manual refresh did not clear the failure marker")
	}
	m = send(m, fetchErrMsg{err: errors.New("still down")})
	if m.err == nil || strings.Contains(m.renderTabs(), "refresh failed") {
		t.Fatalf("manual failure: err=%v; want the error screen alone, no tab-bar marker", m.err)
	}
	m = send(m, cyclesLoadedMsg{cycles: m.table.all, fetched: true})
	if m.err != nil || m.refreshErr != nil {
		t.Fatal("fetched load did not clear the failure state")
	}
}

// TestWindowChangeDuringAutoRefresh: applying a date window re-publishes the
// cached snapshot while a background fetch is in flight; that cache re-publish
// must not resolve the refresh flags, or the in-flight failure would take the
// loud path (and the next tick would race a second fetch).
func TestWindowChangeDuringAutoRefresh(t *testing.T) {
	m := testModelAuto(t, 30*time.Second)
	m = send(m, autoRefreshMsg{seq: m.refreshSeq})
	m = send(m, cyclesLoadedMsg{cycles: m.table.all}) // reloadCmd shape: no fetched flag
	if !m.refreshing {
		t.Fatal("cache re-publish resolved the in-flight background refresh")
	}
	m = send(m, fetchErrMsg{err: errors.New("boom"), background: true})
	if m.err != nil || m.refreshErr == nil {
		t.Fatalf("err=%v refreshErr=%v; want the quiet path after a window change", m.err, m.refreshErr)
	}
}

// TestAutoRefreshIndicator: the armed interval is flagged on the tab bar (the
// re-fetch is otherwise silent) and reads as paused after R — a paused timer
// must stay distinguishable from no timer at all. Pausing also clears a failure
// marker, since no tick remains that could ever clear it.
func TestAutoRefreshIndicator(t *testing.T) {
	m := testModel(t)
	if strings.Contains(m.renderTabs(), "auto") {
		t.Fatal("tab bar shows an auto marker with no interval configured")
	}
	m = testModelAuto(t, 5*time.Minute)
	if out := m.renderTabs(); !strings.Contains(out, "auto 5m") || strings.Contains(out, "paused") {
		t.Fatalf("tab bar missing armed auto marker:\n%s", out)
	}
	m.refreshErr = errors.New("boom")
	m = send(m, rune1('R'))
	if out := m.renderTabs(); !strings.Contains(out, "auto 5m paused") {
		t.Fatalf("tab bar missing paused marker:\n%s", out)
	}
	if m.refreshErr != nil || strings.Contains(m.renderTabs(), "refresh failed") {
		t.Fatal("pause stranded the failure marker: no tick is left to clear it")
	}
}

// TestManualRefreshDuringAutoRefresh: r while a background fetch is in flight
// surfaces it as the loading screen instead of racing a second fetch — and its
// failure then takes the loud path, because the user is watching for it.
func TestManualRefreshDuringAutoRefresh(t *testing.T) {
	m := testModelAuto(t, 30*time.Second)
	m = send(m, autoRefreshMsg{seq: m.refreshSeq})
	m = send(m, rune1('r'))
	if !m.loading || m.refreshing {
		t.Fatalf("loading=%v refreshing=%v; want promoted to the loading screen", m.loading, m.refreshing)
	}
	m = send(m, fetchErrMsg{err: errors.New("boom"), background: true})
	if m.loading || m.err == nil {
		t.Fatalf("loading=%v err=%v; a promoted fetch's failure must reach the error screen", m.loading, m.err)
	}
}

// TestIntervalLabel pins the tab-bar interval format: Duration.String minus
// its trailing zero units.
func TestIntervalLabel(t *testing.T) {
	cases := []struct {
		in   time.Duration
		want string
	}{
		{30 * time.Second, "30s"},
		{90 * time.Second, "1m30s"},
		{5 * time.Minute, "5m"},
		{time.Hour, "1h"},
		{90 * time.Minute, "1h30m"},
	}
	for _, tc := range cases {
		if got := intervalLabel(tc.in); got != tc.want {
			t.Errorf("intervalLabel(%v) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

// TestDetailFollowsRefresh: a refresh re-resolves the open detail view against
// the new cycle set — updated figures re-render in place, and a cycle that
// disappeared (window change, refresh) drops the selection instead of showing
// stale figures. Auto-refresh made this easy to hit, since the set can now
// change with no keypress.
func TestDetailFollowsRefresh(t *testing.T) {
	m := testModel(t)
	m = send(m, tea.KeyMsg{Type: tea.KeyEnter}) // open detail for the selected cycle
	if m.active != viewDetail || !m.detail.hasSel {
		t.Fatal("detail did not open")
	}
	updated := m.detail.cycle
	updated.NetProfit += 1000
	m = send(m, cyclesLoadedMsg{cycles: []model.Cycle{updated}})
	if !m.detail.hasSel || m.detail.cycle.NetProfit != updated.NetProfit {
		t.Fatalf("detail not refreshed: hasSel=%v profit=%v, want %v",
			m.detail.hasSel, m.detail.cycle.NetProfit, updated.NetProfit)
	}
	m = send(m, cyclesLoadedMsg{cycles: nil})
	if m.detail.hasSel {
		t.Fatal("detail kept a cycle the refresh removed")
	}
}

// TestLiveViewKeepsScroll: a background refresh re-renders the live view in
// place and the scroll position survives, same as the detail view — GotoTop on
// every load made the funds block at the bottom unreadable under auto-refresh.
func TestLiveViewKeepsScroll(t *testing.T) {
	lm := newLiveModel()
	lm.setSize(60, 4)
	client := &model.ClientStatus{
		Status:         model.TradeStatus{Slug: "trade_loaded", SecondaryText: "Awaiting market conditions"},
		FundsAvailable: 119422.50,
		TotalProfit:    19422.50,
	}
	market := &model.MarketConditions{
		Current: model.MarketPoint{Spread: 0.82},
		History: []model.MarketPoint{{Spread: 0.68}, {Spread: 0.82}},
		Period:  7,
	}
	lm.setData(client, market)
	lm.vp.SetYOffset(3)
	lm.setData(client, market)
	if lm.vp.YOffset != 3 {
		t.Fatalf("YOffset = %d after a refresh, want 3", lm.vp.YOffset)
	}
}

// TestTableKeepsSelectionOnRefresh: a refreshed set can insert rows above the
// cursor (default sort is newest-first), so the highlight follows the cycle's
// code — enter must never open a row the user didn't pick.
func TestTableKeepsSelectionOnRefresh(t *testing.T) {
	m := testModel(t)
	m = send(m, rune1('1'))
	m = send(m, rune1('j'))
	m = send(m, rune1('j'))
	sel, ok := m.table.selectedCycle()
	if !ok {
		t.Fatal("no selection to preserve")
	}
	newest := model.Cycle{Code: "FXNEW", TradeType: "ARB",
		StartDate: time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC),
		EndDate:   time.Date(2026, 8, 5, 0, 0, 0, 0, time.UTC),
		ZarIn:     1000, ZarOut: 1100, NetProfit: 100}
	m = send(m, cyclesLoadedMsg{cycles: append([]model.Cycle{newest}, m.table.all...), fetched: true})
	got, ok := m.table.selectedCycle()
	if !ok || got.Code != sel.Code {
		t.Fatalf("selection moved to %q after a refresh, want %q", got.Code, sel.Code)
	}
}

// TestNoMarkerOverErrorScreen: while the error screen from a failed manual
// refresh is up, a failing background tick must not stack the tab-bar marker
// on top of it — one failure, one report.
func TestNoMarkerOverErrorScreen(t *testing.T) {
	m := testModelAuto(t, 30*time.Second)
	m = send(m, rune1('r'))
	m = send(m, fetchErrMsg{err: errors.New("manual boom")})
	if m.err == nil {
		t.Fatal("manual failure should reach the error screen")
	}
	m = send(m, autoRefreshMsg{seq: m.refreshSeq})
	m = send(m, fetchErrMsg{err: errors.New("background boom"), background: true})
	if m.refreshErr != nil || strings.Contains(m.renderTabs(), "refresh failed") {
		t.Fatal("background failure stacked the marker on the error screen")
	}
}
