package ui

import (
	"context"
	"time"

	"github.com/charmbracelet/bubbles/help"
	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/wolffshots/fftui/internal/analytics"
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

// liveSpreadPeriod is the history window (days) for the Live view spread
// sparkline; must be one of the API's allowed periods (1/7/30/90/365).
const liveSpreadPeriod = 7

// Messages emitted by the async fetch command. cyclesLoadedMsg carries the cycle
// history plus the live-only extras (nil in CSV mode or if their pull failed).
type cyclesLoadedMsg struct {
	cycles []model.Cycle
	client *model.ClientStatus
	market *model.MarketConditions
	now    time.Time // fetch-time "today"; zero when embedding pre-fetched data
}
type fetchErrMsg struct{ err error }

// Today returns the current local calendar date as a UTC-midnight time — the
// same convention cycle dates are parsed with. Deriving it from the local date
// (rather than truncating UTC time) avoids the early-morning off-by-one for
// timezones ahead of UTC (e.g. SAST).
func Today() time.Time {
	y, m, d := time.Now().Date()
	return time.Date(y, m, d, 0, 0, 0, 0, time.UTC)
}

// LoadedMsg wraps a pre-fetched cycle set as an Update message, for embedding
// the model with data already in hand (previews, tests).
func LoadedMsg(cs []model.Cycle) tea.Msg { return cyclesLoadedMsg{cycles: cs} }

// RootModel is the top-level Bubble Tea model. It owns one sub-model per view
// and routes updates to the active one.
type RootModel struct {
	source model.CycleSource
	now    time.Time
	rates  analytics.Rates

	keys         keyMap
	help         help.Model
	showFullHelp bool
	spin         spinner.Model
	loading      bool
	err          error

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
// for the with-idle and after-tax annualised figures.
func New(source model.CycleSource, now time.Time, rates analytics.Rates) RootModel {
	sp := spinner.New()
	sp.Spinner = spinner.Dot
	sp.Style = lipgloss.NewStyle().Foreground(accent)

	return RootModel{
		source:    source,
		now:       now,
		rates:     rates,
		keys:      newKeyMap(),
		help:      help.New(),
		spin:      sp,
		loading:   true,
		active:    viewTable,
		table:     newTableModel(rates),
		analytics: newAnalyticsModel(now, rates),
		detail:    newDetailModel(),
		charts:    newChartsModel(now, rates),
		live:      newLiveModel(),
	}
}

func (m RootModel) Init() tea.Cmd {
	return tea.Batch(m.spin.Tick, fetchCmd(m.source))
}

// fetchCmd runs the source fetch off the UI goroutine. For the live source it
// also pulls the current-cycle status and market spread; those extras are
// best-effort — if they fail, the cycles still load and the live view/status bar
// just stay empty rather than failing the whole refresh.
func fetchCmd(src model.CycleSource) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
		defer cancel()
		cs, err := src.Fetch(ctx)
		if err != nil {
			return fetchErrMsg{err}
		}
		msg := cyclesLoadedMsg{cycles: cs, now: Today()}
		if ff, ok := src.(*model.LiveSource); ok {
			if st, err := ff.FetchClient(ctx); err == nil {
				msg.client = st
			}
			if mc, err := ff.FetchMarketConditions(ctx, liveSpreadPeriod); err == nil {
				msg.market = mc
			}
		}
		return msg
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
		m.analytics.setCycles(cs)
		m.charts.setCycles(cs)
		m.live.setData(msg.client, msg.market)
		m.applySizes()
		return m, nil

	case fetchErrMsg:
		m.loading = false
		m.err = msg.err
		return m, nil

	case tea.KeyMsg:
		return m.handleKey(msg)
	}

	// Forward other messages (e.g. viewport/table internal) to the active view.
	return m.forward(msg)
}

func (m RootModel) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
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
		m.loading = true
		m.err = nil
		return m, tea.Batch(m.spin.Tick, fetchCmd(m.source))

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
	return tabBarStyle.Render(bar)
}

func (m RootModel) renderHelp() string {
	return footerStyle.Width(m.width).Render(m.help.View(m.keys))
}
