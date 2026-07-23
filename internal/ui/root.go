package ui

import (
	"context"
	"time"

	"github.com/charmbracelet/bubbles/help"
	"github.com/charmbracelet/bubbles/spinner"
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
}
type fetchErrMsg struct{ err error }

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
// the per-cycle fee schedule for the fee-aware capital projections.
func New(svc *data.Service, now time.Time, rates analytics.Rates, allow analytics.Allowances, fees analytics.Fees) RootModel {
	sp := spinner.New()
	sp.Spinner = spinner.Dot
	sp.Style = lipgloss.NewStyle().Foreground(accent)

	return RootModel{
		svc:       svc,
		now:       now,
		rates:     rates,
		keys:      newKeyMap(),
		help:      help.New(),
		spin:      sp,
		loading:   true,
		active:    viewTable,
		table:     newTableModel(rates),
		analytics: newAnalyticsModel(now, rates, allow, fees),
		detail:    newDetailModel(),
		charts:    newChartsModel(now, rates),
		live:      newLiveModel(),
	}
}

func (m RootModel) Init() tea.Cmd {
	return tea.Batch(m.spin.Tick, fetchCmd(m.svc))
}

// fetchCmd runs the service refresh off the UI goroutine and translates the
// snapshot (or error) into the existing Bubble Tea messages.
func fetchCmd(svc *data.Service) tea.Cmd {
	return func() tea.Msg {
		snap, err := svc.Refresh(context.Background())
		if err != nil {
			return fetchErrMsg{err}
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
		return m, tea.Batch(m.spin.Tick, fetchCmd(m.svc))

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
