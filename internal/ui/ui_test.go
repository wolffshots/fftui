package ui

import (
	"context"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/wolffshots/fftui/internal/analytics"
	"github.com/wolffshots/fftui/internal/model"
)

func testModel(t *testing.T) RootModel {
	t.Helper()
	src := model.NewCSVSource("../../testdata/cycles.csv")
	cs, err := src.Fetch(context.Background())
	if err != nil {
		t.Fatalf("fetch: %v", err)
	}
	now := time.Date(2026, 7, 10, 0, 0, 0, 0, time.UTC)
	m := New(src, now, analytics.Rates{Idle: 0.06, Tax: 0.41})
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
	for _, k := range []rune{'1', '2', '3', '4', '5'} {
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
		for _, k := range []rune{'1', '2', '3', '4', '5'} {
			m = send(m, rune1(k))
			_ = m.View()
		}
	}
}
