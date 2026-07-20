package ui

import (
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/charmbracelet/bubbles/table"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/wolffshots/fftui/internal/analytics"
	"github.com/wolffshots/fftui/internal/model"
)

type sortColumn int

const (
	sortStart sortColumn = iota
	sortCode
	sortType
	sortZarIn
	sortZarOut
	sortProfit
	sortReturn
	sortDays
)

var sortColumnNames = []string{"Start", "Code", "Type", "ZAR In", "ZAR Out", "Profit", "Return", "Days"}

type tableModel struct {
	tbl       table.Model
	all       []model.Cycle // canonical (start-ascending) set
	visible   []model.Cycle // filtered+sorted, 1:1 with table rows
	sortCol   sortColumn
	sortAsc   bool
	filter    textinput.Model
	filtering bool
	summary   analytics.Summary
	rates     analytics.Rates
	width     int
	height    int
}

func newTableModel(rates analytics.Rates) tableModel {
	ti := textinput.New()
	ti.Placeholder = "filter by code or type…"
	ti.Prompt = "/"
	ti.CharLimit = 40

	t := table.New(
		table.WithColumns(tableColumns()),
		table.WithFocused(true),
	)
	st := table.DefaultStyles()
	st.Header = st.Header.
		BorderStyle(lipgloss.NormalBorder()).
		BorderForeground(dim).
		BorderBottom(true).
		Bold(true).
		Foreground(accent)
	st.Selected = st.Selected.
		Foreground(lipgloss.Color("#FFFFFF")).
		Background(accent).
		Bold(false)
	t.SetStyles(st)

	return tableModel{
		tbl:     t,
		sortCol: sortStart,
		sortAsc: false, // default: newest first
		filter:  ti,
		rates:   rates,
	}
}

func tableColumns() []table.Column {
	// Fixed sensible widths; the table scrolls horizontally if the terminal is
	// narrower. Numeric columns are wide enough for R100,000.00 style values.
	return []table.Column{
		{Title: "Code", Width: 10},
		{Title: "Type", Width: 8},
		{Title: "Start", Width: 10},
		{Title: "End", Width: 10},
		{Title: "ZAR In", Width: 14},
		{Title: "ZAR Out", Width: 14},
		{Title: "Profit", Width: 12},
		{Title: "Return%", Width: 9},
		{Title: "Days", Width: 5},
	}
}

func (m *tableModel) setCycles(cs []model.Cycle) {
	m.all = cs
	m.rebuild()
}

func (m *tableModel) setSize(w, h int) {
	m.width, m.height = w, h
	m.tbl.SetWidth(w)
	// Reserve two lines below the table for the sort/filter line and the totals
	// footer that view() appends.
	inner := h - 2
	if inner < 3 {
		inner = 3
	}
	m.tbl.SetHeight(inner)
}

// rebuild applies the current filter + sort and refreshes the table rows.
func (m *tableModel) rebuild() {
	view := make([]model.Cycle, 0, len(m.all))
	q := strings.ToLower(strings.TrimSpace(m.filter.Value()))
	for _, c := range m.all {
		if q == "" || strings.Contains(strings.ToLower(c.Code), q) ||
			strings.Contains(strings.ToLower(c.TradeType), q) {
			view = append(view, c)
		}
	}
	sortCycles(view, m.sortCol, m.sortAsc)
	m.visible = view
	// Count and profit in the footer describe the visible set; the annualised
	// rates are only shown unfiltered (view() omits them under a filter).
	m.summary = analytics.Lifetime(view, m.rates)

	rows := make([]table.Row, len(view))
	for i, c := range view {
		rows[i] = table.Row{
			c.Code,
			c.TradeType,
			c.StartDate.Format("2006-01-02"),
			c.EndDate.Format("2006-01-02"),
			rightPad(money(c.ZarIn), 14),
			rightPad(money(c.ZarOut), 14),
			rightPad(money(c.NetProfit), 12),
			// bubbles/table v1 truncates cells with a non-ANSI-aware width, so
			// embedded colour codes corrupt the layout; keep this cell plain and
			// right-aligned. Green returns still appear in the footer and detail.
			rightPad(percent(c.Return()), 7),
			rightPad(strconv.Itoa(c.HoldDays()), 4),
		}
	}
	m.tbl.SetRows(rows)
	if m.tbl.Cursor() >= len(rows) {
		m.tbl.GotoTop()
	}
}

func sortCycles(cs []model.Cycle, col sortColumn, asc bool) {
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

// rightPad right-aligns s within width (for numeric columns).
func rightPad(s string, width int) string {
	if lipgloss.Width(s) >= width {
		return s
	}
	return strings.Repeat(" ", width-lipgloss.Width(s)) + s
}

func (m tableModel) selectedCycle() (model.Cycle, bool) {
	i := m.tbl.Cursor()
	if i < 0 || i >= len(m.visible) {
		return model.Cycle{}, false
	}
	return m.visible[i], true
}

// update returns the updated model, an optional command, and whether the caller
// should switch to the detail view (enter pressed on a row).
func (m tableModel) update(msg tea.Msg, k keyMap) (tableModel, tea.Cmd, bool) {
	if m.filtering {
		if key, ok := msg.(tea.KeyMsg); ok {
			switch key.String() {
			case "enter":
				m.filtering = false
				m.filter.Blur()
				m.rebuild()
				return m, nil, false
			case "esc":
				m.filtering = false
				m.filter.SetValue("")
				m.filter.Blur()
				m.rebuild()
				return m, nil, false
			}
		}
		var cmd tea.Cmd
		m.filter, cmd = m.filter.Update(msg)
		m.rebuild()
		return m, cmd, false
	}

	if key, ok := msg.(tea.KeyMsg); ok {
		switch {
		case keyMatches(key, k.Filter):
			m.filtering = true
			m.filter.Focus()
			return m, textinput.Blink, false
		case keyMatches(key, k.SortCol):
			m.sortCol = (m.sortCol + 1) % sortColumn(len(sortColumnNames))
			m.rebuild()
			return m, nil, false
		case keyMatches(key, k.SortDir):
			m.sortAsc = !m.sortAsc
			m.rebuild()
			return m, nil, false
		case keyMatches(key, k.Enter):
			if _, ok := m.selectedCycle(); ok {
				return m, nil, true
			}
			return m, nil, false
		case keyMatches(key, k.Back):
			// esc clears an applied filter (§6).
			if m.filter.Value() != "" {
				m.filter.SetValue("")
				m.rebuild()
			}
			return m, nil, false
		}
	}

	var cmd tea.Cmd
	m.tbl, cmd = m.tbl.Update(msg)
	return m, cmd, false
}

func (m tableModel) view() string {
	var b strings.Builder
	b.WriteString(m.tbl.View())
	b.WriteString("\n")

	dir := "desc"
	if m.sortAsc {
		dir = "asc"
	}
	sortLine := dimStyle.Render("sort: ") +
		valueStyle.Render(sortColumnNames[m.sortCol]) +
		dimStyle.Render(" "+dir)
	if m.filtering {
		sortLine = m.filter.View()
	} else if m.filter.Value() != "" {
		sortLine += dimStyle.Render("   filter: ") + valueStyle.Render(m.filter.Value())
	}
	b.WriteString(sortLine + "\n")

	footer := dimStyle.Render("cycles ") + valueStyle.Render(strconv.Itoa(len(m.visible))) +
		dimStyle.Render("   profit ") + colourMoney(sumProfit(m.visible))
	if m.filter.Value() == "" {
		s := m.summary
		footer += dimStyle.Render("   annualised ") + colourReturn(s.Annualised) +
			dimStyle.Render(fmt.Sprintf("   +idle@%s ", percent(m.rates.Idle))) +
			colourReturn(s.AnnualisedWithIdle) +
			dimStyle.Render(fmt.Sprintf("   net@%s ", percent(m.rates.Tax))) +
			colourReturn(s.AnnualisedWithIdleAfterTax)
	} else {
		// An annualised rate over a filtered subset is a misleading hybrid (days
		// when excluded cycles were trading would read as idle), so omit it.
		footer += dimStyle.Render("   annualised n/a (filtered)")
	}
	b.WriteString(footer)
	return b.String()
}

func sumProfit(cs []model.Cycle) float64 {
	var t float64
	for _, c := range cs {
		t += c.NetProfit
	}
	return t
}
