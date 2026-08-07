package ui

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/help"
	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/wolffshots/fftui/internal/analytics"
	"github.com/wolffshots/fftui/internal/data"
	"github.com/wolffshots/fftui/internal/model"
)

type viewID int

const (
	viewTable viewID = iota
	viewAnalytics
	viewDetail
	viewCharts
	viewLive
)

var tabNames = []string{"Cycles", "Analytics", "Detail", "Charts", "Live"}

// Messages emitted by the async fetch command. cyclesLoadedMsg carries the cycle
// history plus the live-only extras (nil in CSV mode or if their pull failed).
type cyclesLoadedMsg struct {
	cycles     []model.Cycle
	client     *model.ClientStatus
	market     *model.MarketConditions
	marketYear *model.MarketConditions // year-long history for the trend strip
	now        time.Time               // fetch-time "today"; zero when embedding pre-fetched data
	fetched    bool                    // true from fetchCmd; cache re-publishes (reloadCmd, LoadedMsg) leave the refresh flags alone
}
type fetchErrMsg struct {
	err        error
	background bool // an auto-refresh failure: keep the body, flag the tab bar
}

// autoRefreshMsg is the periodic re-fetch tick — its own type so the timer
// path can't be confused with a fetch result. seq guards against doubled tick
// chains: a tick armed before a pause/resume carries a stale sequence number
// and is dropped instead of re-arming alongside the fresh chain.
type autoRefreshMsg struct{ seq int }

// Today lives in internal/data; aliased here so existing callers (main.go)
// stay unchanged.
var Today = data.Today

// LoadedMsg wraps a pre-fetched cycle set as an Update message, for embedding
// the model with data already in hand (previews, tests).
func LoadedMsg(cs []model.Cycle) tea.Msg { return cyclesLoadedMsg{cycles: cs} }

// RootModel is the top-level Bubble Tea model. It owns one sub-model per view
// and routes updates to the active one.
type RootModel struct {
	svc   *data.Service
	now   time.Time
	rates analytics.Rates

	keys         keyMap
	help         help.Model
	showFullHelp bool
	spin         spinner.Model
	loading      bool
	err          error

	// Auto-refresh (--refresh-interval; R pauses/resumes): a background tick
	// re-fetches without the loading screen a manual r shows. refreshing marks
	// that quiet in-flight fetch, and refreshErr keeps its failure on the tab
	// bar while the last good data stays on screen.
	refreshEvery time.Duration // 0 disables the tick loop
	autoRefresh  bool          // session toggle; starts on when an interval is set
	refreshing   bool
	refreshErr   error
	refreshSeq   int // current tick chain; older ticks are stale

	// Date-window editor (w): while open it replaces the help footer and
	// captures all keys, mirroring how the table's / filter captures input.
	windowInput   textinput.Model
	editingWindow bool
	windowErr     string

	active viewID

	table     tableModel
	analytics analyticsModel
	detail    detailModel
	charts    chartsModel
	live      liveModel

	// Live snapshot, also used by the status-bar strip. Nil in CSV mode.
	client *model.ClientStatus
	market *model.MarketConditions

	width  int
	height int
}

// New builds the root model. rates carries the idle-cash rate and tax rate used
// for the with-idle and after-tax annualised figures; allow carries the annual
// SDA/FIA limits for the planning figures (a zero total disables them); fees is
// the per-cycle fee schedule for the fee-aware capital projections;
// refreshEvery re-fetches automatically on that interval (0 disables it, R
// pauses/resumes it for the session).
func New(svc *data.Service, now time.Time, rates analytics.Rates, allow analytics.Allowances, fees analytics.Fees, refreshEvery time.Duration) RootModel {
	sp := spinner.New()
	sp.Spinner = spinner.Dot
	sp.Style = lipgloss.NewStyle().Foreground(accent)

	wi := textinput.New()
	wi.Placeholder = "from..to (YYYY-MM-DD, either side optional; empty shows all)"
	wi.Prompt = "date window: "
	wi.CharLimit = 24

	km := newKeyMap()
	if refreshEvery <= 0 {
		// Nothing to toggle without --refresh-interval; keep R out of the help
		// rather than advertise a no-op.
		km.AutoRefresh.SetEnabled(false)
	}

	return RootModel{
		svc:          svc,
		windowInput:  wi,
		now:          now,
		rates:        rates,
		keys:         km,
		help:         help.New(),
		spin:         sp,
		loading:      true,
		refreshEvery: refreshEvery,
		autoRefresh:  refreshEvery > 0,
		active:       viewTable,
		table:        newTableModel(rates, fees),
		analytics:    newAnalyticsModel(now, rates, allow, fees),
		detail:       newDetailModel(fees),
		charts:       newChartsModel(now, rates),
		live:         newLiveModel(),
	}
}

func (m RootModel) Init() tea.Cmd {
	return tea.Batch(m.spin.Tick, fetchCmd(m.svc, false), m.autoRefreshTick())
}

// autoRefreshTick arms the next periodic re-fetch, or returns nil (a no-op in
// a Batch) when no interval is configured or the toggle is off. Each tick
// re-arms the next one, so dropping the returned command ends the loop — the
// same self-rescheduling shape as the spinner's Tick. tea.Tick rather than
// tea.Every: Every aligns to wall-clock boundaries, which would make several
// running instances hit the API in lockstep.
func (m RootModel) autoRefreshTick() tea.Cmd {
	if !m.autoRefresh || m.refreshEvery <= 0 {
		return nil
	}
	seq := m.refreshSeq
	return tea.Tick(m.refreshEvery, func(time.Time) tea.Msg { return autoRefreshMsg{seq: seq} })
}

// fetchCmd runs the service refresh off the UI goroutine and translates the
// snapshot (or error) into the existing Bubble Tea messages. background marks
// an auto-refresh fetch, whose failure is flagged quietly on the tab bar
// instead of taking over the body.
func fetchCmd(svc *data.Service, background bool) tea.Cmd {
	return func() tea.Msg {
		snap, err := svc.Refresh(context.Background())
		if err != nil {
			return fetchErrMsg{err: err, background: background}
		}
		return cyclesLoadedMsg{
			cycles:     snap.Cycles,
			client:     snap.Client,
			market:     snap.Market,
			marketYear: snap.MarketYear,
			now:        snap.Now,
			fetched:    true,
		}
	}
}

// reloadCmd re-reads the service's published snapshot — used after a window
// change, which re-filters the cached fetch, so no source round trip happens.
func reloadCmd(svc *data.Service) tea.Cmd {
	return func() tea.Msg {
		snap, _ := svc.Latest()
		if snap == nil {
			return nil // nothing fetched yet; the window applies on first load
		}
		return cyclesLoadedMsg{
			cycles:     snap.Cycles,
			client:     snap.Client,
			market:     snap.Market,
			marketYear: snap.MarketYear,
			now:        snap.Now,
		}
	}
}

func (m RootModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		m.help.Width = msg.Width
		m.applySizes()
		return m, nil

	case spinner.TickMsg:
		if m.loading {
			var cmd tea.Cmd
			m.spin, cmd = m.spin.Update(msg)
			return m, cmd
		}
		return m, nil

	case cyclesLoadedMsg:
		m.loading = false
		m.err = nil
		if msg.fetched {
			// Only a real fetch resolves the background-refresh flags — a
			// cache re-publish (window change mid-flight) mustn't desync an
			// in-flight one into the loud error path.
			m.refreshing = false
			m.refreshErr = nil
		}
		if !msg.now.IsZero() {
			// A refresh also refreshes "today", so elapsed-day counts for the
			// in-progress period don't go stale in a long-running session.
			m.now = msg.now
			m.analytics.now = msg.now
			m.charts.now = msg.now
		}
		cs := msg.cycles
		m.client = msg.client
		m.market = msg.market
		m.table.setCycles(cs)
		m.analytics.client = msg.client         // live allowance balances for the planning strip
		m.analytics.marketYear = msg.marketYear // year of spread history for the trend strip
		m.analytics.setCycles(cs)
		m.charts.setCycles(cs)
		m.live.setData(msg.client, msg.market)
		m.detail.refresh(cs)
		m.applySizes()
		return m, nil

	case fetchErrMsg:
		if msg.background && !m.loading {
			// A background auto-refresh failed. The service kept the last good
			// snapshot, so keep showing it and flag the failure on the tab bar
			// instead of replacing the body with the error screen. (When r
			// promoted this fetch to the loading screen, m.loading routes its
			// failure to the loud branch below instead.)
			m.refreshing = false
			m.refreshErr = msg.err
			return m, nil
		}
		m.loading = false
		m.err = msg.err
		return m, nil

	case autoRefreshMsg:
		if !m.autoRefresh || msg.seq != m.refreshSeq {
			return m, nil // paused, or a stale tick from an old chain; let it die
		}
		if m.loading || m.refreshing {
			return m, m.autoRefreshTick() // a fetch is already in flight; just re-arm
		}
		m.refreshing = true
		return m, tea.Batch(fetchCmd(m.svc, true), m.autoRefreshTick())

	case tea.KeyMsg:
		return m.handleKey(msg)
	}

	// Forward other messages (e.g. viewport/table internal) to the active view.
	return m.forward(msg)
}

func (m RootModel) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	// While the date-window editor is open it captures every key, like the
	// table filter below.
	if m.editingWindow {
		switch msg.String() {
		case "enter":
			from, to, err := parseWindow(m.windowInput.Value())
			if err != nil {
				m.windowErr = err.Error()
				return m, nil
			}
			m.editingWindow = false
			m.windowErr = ""
			m.windowInput.Blur()
			m.svc.SetDateRange(from, to)
			m.applySizes()
			// SetDateRange already re-published the re-windowed snapshot from
			// the cached fetch; reload it without touching the source.
			return m, reloadCmd(m.svc)
		case "esc":
			m.editingWindow = false
			m.windowErr = ""
			m.windowInput.Blur()
			m.applySizes()
			return m, nil
		}
		var cmd tea.Cmd
		m.windowInput, cmd = m.windowInput.Update(msg)
		return m, cmd
	}

	// While the table filter is capturing input, route keys straight to it so
	// typing (including q, r, digits) isn't hijacked by global shortcuts.
	if m.active == viewTable && m.table.filtering {
		var cmd tea.Cmd
		m.table, cmd, _ = m.table.update(msg, m.keys)
		return m, cmd
	}

	switch {
	case keyMatches(msg, m.keys.Quit):
		if msg.String() != "ctrl+c" && m.active == viewDetail {
			m.active = viewTable // from detail, first hop back to the table
			return m, nil
		}
		return m, tea.Quit

	case keyMatches(msg, m.keys.Help):
		m.showFullHelp = !m.showFullHelp
		m.help.ShowAll = m.showFullHelp
		m.applySizes()
		return m, nil

	case keyMatches(msg, m.keys.Refresh):
		if m.loading {
			return m, nil // a fetch is already in flight; don't race a second one
		}
		if m.refreshing {
			// A background auto-refresh is already in flight; surface it as
			// the loading screen instead of racing a second fetch.
			m.refreshing = false
			m.loading = true
			m.err = nil
			m.refreshErr = nil
			return m, m.spin.Tick
		}
		m.loading = true
		m.err = nil
		m.refreshErr = nil // the user asked for this fetch; its outcome owns both indicators
		return m, tea.Batch(m.spin.Tick, fetchCmd(m.svc, false))

	case keyMatches(msg, m.keys.AutoRefresh):
		if m.refreshEvery <= 0 {
			return m, nil // no --refresh-interval configured; nothing to toggle
		}
		m.autoRefresh = !m.autoRefresh
		if m.autoRefresh {
			// Start a fresh tick chain; bumping the sequence kills any tick
			// still pending from before the pause.
			m.refreshSeq++
			return m, m.autoRefreshTick()
		}
		return m, nil

	case keyMatches(msg, m.keys.DateWindow):
		m.editingWindow = true
		m.windowErr = ""
		m.windowInput.SetValue(windowValue(m.svc.DateRange()))
		m.windowInput.CursorEnd()
		m.windowInput.Focus()
		m.applySizes()
		return m, textinput.Blink

	case keyMatches(msg, m.keys.Table):
		m.active = viewTable
		return m, nil
	case keyMatches(msg, m.keys.Analytics):
		m.active = viewAnalytics
		return m, nil
	case keyMatches(msg, m.keys.Detail):
		m.active = viewDetail
		return m, nil
	case keyMatches(msg, m.keys.Charts):
		m.active = viewCharts
		return m, nil
	case keyMatches(msg, m.keys.Live):
		m.active = viewLive
		return m, nil

	case keyMatches(msg, m.keys.Back) && m.active == viewDetail:
		m.active = viewTable
		return m, nil
	}

	return m.forward(msg)
}

// forward routes a message to the active sub-view.
func (m RootModel) forward(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch m.active {
	case viewTable:
		var cmd tea.Cmd
		var openDetail bool
		m.table, cmd, openDetail = m.table.update(msg, m.keys)
		if openDetail {
			if c, ok := m.table.selectedCycle(); ok {
				m.detail.setCycle(c)
				m.active = viewDetail
			}
		}
		return m, cmd
	case viewAnalytics:
		m.analytics = m.analytics.update(msg, m.keys)
		return m, nil
	case viewDetail:
		var cmd tea.Cmd
		m.detail, cmd = m.detail.update(msg)
		return m, cmd
	case viewLive:
		var cmd tea.Cmd
		m.live, cmd = m.live.update(msg)
		return m, cmd
	}
	return m, nil
}

// applySizes recomputes the body height from the chrome (tab bar + help) and
// resizes every sub-view. Called on resize, load, and help toggle.
func (m *RootModel) applySizes() {
	if m.width == 0 || m.height == 0 {
		return
	}
	topH := lipgloss.Height(m.renderTabs())
	botH := lipgloss.Height(m.renderHelp())
	if bar := m.renderStatusBar(); bar != "" {
		topH += lipgloss.Height(bar)
	}
	bodyH := m.height - topH - botH - 1
	if bodyH < 3 {
		bodyH = 3
	}
	m.table.setSize(m.width, bodyH)
	m.analytics.setSize(m.width, bodyH)
	m.detail.setSize(m.width, bodyH)
	m.charts.setSize(m.width, bodyH)
	m.live.setSize(m.width, bodyH)
}

// renderStatusBar renders the live header strip (empty in CSV mode).
func (m RootModel) renderStatusBar() string {
	return renderStatusBar(m.client, m.market, m.width)
}

func (m RootModel) View() string {
	if m.width == 0 {
		return "loading…"
	}
	top := m.renderTabs()
	if bar := m.renderStatusBar(); bar != "" {
		top = lipgloss.JoinVertical(lipgloss.Left, bar, top)
	}
	bottom := m.renderHelp()

	var body string
	switch {
	case m.loading:
		body = m.spin.View() + " fetching cycles…"
	case m.err != nil:
		body = errorStyle.Render("fetch failed: ") + m.err.Error() +
			"\n\n" + dimStyle.Render("press r to retry")
	default:
		switch m.active {
		case viewTable:
			body = m.table.view()
		case viewAnalytics:
			body = m.analytics.view()
		case viewDetail:
			body = m.detail.view()
		case viewCharts:
			body = m.charts.view()
		case viewLive:
			body = m.live.view()
		}
	}

	return lipgloss.JoinVertical(lipgloss.Left, top, body, bottom)
}

func (m RootModel) renderTabs() string {
	var tabs []string
	for i, name := range tabNames {
		label := name
		if viewID(i) == m.active {
			tabs = append(tabs, tabActiveStyle.Render(label))
		} else {
			tabs = append(tabs, tabInactiveStyle.Render(label))
		}
	}
	bar := lipgloss.JoinHorizontal(lipgloss.Top, tabs...)
	// Every view shows only cycles inside the --from/--to window, so flag it
	// globally rather than per view.
	if from, to := m.svc.DateRange(); !from.IsZero() || !to.IsZero() {
		bar += dimStyle.Render("  window ") + valueStyle.Render(rangeLabel(from, to))
	}
	// Auto-refresh re-fetches silently (no loading screen), so show that it's
	// armed — or paused, and whether its last pass failed — rather than let
	// the data change (or quietly stop changing) with no visible cause.
	if m.refreshEvery > 0 {
		if m.autoRefresh {
			bar += dimStyle.Render("  auto ") + valueStyle.Render(intervalLabel(m.refreshEvery))
		} else {
			bar += dimStyle.Render("  auto " + intervalLabel(m.refreshEvery) + " paused")
		}
	}
	if m.refreshErr != nil {
		bar += errorStyle.Render("  ⚠ refresh failed")
	}
	return tabBarStyle.Render(bar)
}

// intervalLabel formats the auto-refresh interval without the zero units
// Duration.String appends ("5m", not "5m0s").
func intervalLabel(d time.Duration) string {
	s := d.String()
	if strings.HasSuffix(s, "m0s") {
		s = s[:len(s)-2]
	}
	if strings.HasSuffix(s, "h0m") {
		s = s[:len(s)-2]
	}
	return s
}

// rangeLabel formats the active date window, e.g. "2026-03-01 → …".
func rangeLabel(from, to time.Time) string {
	f, t := "…", "…"
	if !from.IsZero() {
		f = from.Format("2006-01-02")
	}
	if !to.IsZero() {
		t = to.Format("2006-01-02")
	}
	return f + " → " + t
}

func (m RootModel) renderHelp() string {
	if m.editingWindow {
		line := m.windowInput.View()
		if m.windowErr != "" {
			line += "  " + errorStyle.Render(m.windowErr)
		}
		return footerStyle.Width(m.width).Render(line)
	}
	return footerStyle.Width(m.width).Render(m.help.View(m.keys))
}

// parseWindow parses the editor's "from..to" value (YYYY-MM-DD each side,
// either optional). Empty input clears the window.
func parseWindow(s string) (from, to time.Time, err error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return time.Time{}, time.Time{}, nil
	}
	lo, hi, ok := strings.Cut(s, "..")
	if !ok {
		return from, to, fmt.Errorf("use from..to (either side optional)")
	}
	parse := func(v string) (time.Time, error) {
		v = strings.TrimSpace(v)
		if v == "" {
			return time.Time{}, nil
		}
		return time.Parse("2006-01-02", v)
	}
	if from, err = parse(lo); err != nil {
		return from, to, fmt.Errorf("%q is not YYYY-MM-DD", strings.TrimSpace(lo))
	}
	if to, err = parse(hi); err != nil {
		return from, to, fmt.Errorf("%q is not YYYY-MM-DD", strings.TrimSpace(hi))
	}
	if !from.IsZero() && !to.IsZero() && to.Before(from) {
		return from, to, fmt.Errorf("window ends before it starts")
	}
	return from, to, nil
}

// windowValue is the editable text for the current window ("" when unset) —
// the inverse of parseWindow, used to prefill the editor.
func windowValue(from, to time.Time) string {
	if from.IsZero() && to.IsZero() {
		return ""
	}
	f, t := "", ""
	if !from.IsZero() {
		f = from.Format("2006-01-02")
	}
	if !to.IsZero() {
		t = to.Format("2006-01-02")
	}
	return f + ".." + t
}
