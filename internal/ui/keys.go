package ui

import "github.com/charmbracelet/bubbles/key"

// keyMap holds every binding. Help text is generated from these so the help bar
// and the bindings never drift apart.
type keyMap struct {
	Table     key.Binding
	Analytics key.Binding
	Detail    key.Binding
	Charts    key.Binding
	Live      key.Binding

	Up   key.Binding
	Down key.Binding

	SubTab     key.Binding
	ToggleDead key.Binding
	SortCol    key.Binding
	SortDir    key.Binding
	Filter     key.Binding
	Enter      key.Binding

	Refresh key.Binding
	Help    key.Binding
	Back    key.Binding
	Quit    key.Binding
}

func newKeyMap() keyMap {
	return keyMap{
		Table:      key.NewBinding(key.WithKeys("1"), key.WithHelp("1", "table")),
		Analytics:  key.NewBinding(key.WithKeys("2"), key.WithHelp("2", "analytics")),
		Detail:     key.NewBinding(key.WithKeys("3"), key.WithHelp("3", "detail")),
		Charts:     key.NewBinding(key.WithKeys("4"), key.WithHelp("4", "charts")),
		Live:       key.NewBinding(key.WithKeys("5"), key.WithHelp("5", "live")),
		Up:         key.NewBinding(key.WithKeys("up", "k"), key.WithHelp("↑/k", "up")),
		Down:       key.NewBinding(key.WithKeys("down", "j"), key.WithHelp("↓/j", "down")),
		SubTab:     key.NewBinding(key.WithKeys("tab"), key.WithHelp("tab", "year/qtr/month")),
		ToggleDead: key.NewBinding(key.WithKeys("a"), key.WithHelp("a", "active/incl-dead")),
		SortCol:    key.NewBinding(key.WithKeys("s"), key.WithHelp("s", "sort col")),
		SortDir:    key.NewBinding(key.WithKeys("S"), key.WithHelp("S", "sort dir")),
		Filter:     key.NewBinding(key.WithKeys("/"), key.WithHelp("/", "filter")),
		Enter:      key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "detail")),
		Refresh:    key.NewBinding(key.WithKeys("r"), key.WithHelp("r", "refresh")),
		Help:       key.NewBinding(key.WithKeys("?"), key.WithHelp("?", "help")),
		Back:       key.NewBinding(key.WithKeys("esc"), key.WithHelp("esc", "back")),
		Quit:       key.NewBinding(key.WithKeys("q", "ctrl+c"), key.WithHelp("q", "quit")),
	}
}

// ShortHelp / FullHelp implement bubbles/help.KeyMap.
func (k keyMap) ShortHelp() []key.Binding {
	return []key.Binding{k.Table, k.Analytics, k.Charts, k.Live, k.Filter, k.Refresh, k.Help, k.Quit}
}

func (k keyMap) FullHelp() [][]key.Binding {
	return [][]key.Binding{
		{k.Table, k.Analytics, k.Detail, k.Charts, k.Live},
		{k.Up, k.Down, k.Enter, k.Filter},
		{k.SubTab, k.ToggleDead, k.SortCol, k.SortDir},
		{k.Refresh, k.Back, k.Help, k.Quit},
	}
}
