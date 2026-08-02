// Package tui is themer's whole interface: pick a theme from the list,
// watch each applier land. Two screens, nothing modal.
package tui

import (
	"fmt"
	"io"
	"strings"

	"github.com/charmbracelet/bubbles/list"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/lvim-tech/themer/internal/apply"
	"github.com/lvim-tech/themer/internal/config"
	"github.com/lvim-tech/themer/internal/theme"
)

type screen int

const (
	screenList screen = iota
	screenApply
)

// swatchKeys are the palette colours previewed beside each theme name, in
// terminal order. bg leads so the darkness of the variant reads first.
var swatchKeys = []string{"bg", "fg", "red", "orange", "yellow", "green", "teal", "cyan", "blue", "purple", "magenta"}

type item struct {
	t       theme.Theme
	current bool
	swatch  string
}

func (i item) FilterValue() string { return i.t.Name }

// itemDelegate renders one theme per line: marker, name, swatches. The
// swatch row is precomputed — 48 themes × a redraw per keystroke is not
// where lipgloss styles should be built.
type itemDelegate struct{}

func (d itemDelegate) Height() int                             { return 1 }
func (d itemDelegate) Spacing() int                            { return 0 }
func (d itemDelegate) Update(_ tea.Msg, _ *list.Model) tea.Cmd { return nil }
func (d itemDelegate) Render(w io.Writer, m list.Model, index int, li list.Item) {
	it, ok := li.(item)
	if !ok {
		return
	}
	name := fmt.Sprintf("%-28s", it.t.Name)
	marker := "  "
	if it.current {
		marker = markerStyle.Render("● ")
	}
	line := marker + name + " " + it.swatch
	if index == m.Index() {
		line = selectedStyle.Render("│ ") + marker + selectedStyle.Render(name) + " " + it.swatch
	} else {
		line = "  " + line
	}
	fmt.Fprint(w, line)
}

var (
	markerStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("2"))
	selectedStyle  = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("6"))
	activeTabStyle = lipgloss.NewStyle().Bold(true).Reverse(true)
	tabStyle       = lipgloss.NewStyle().Faint(true)
	dimStyle       = lipgloss.NewStyle().Faint(true)
	okStyle        = lipgloss.NewStyle().Foreground(lipgloss.Color("2"))
	failStyle      = lipgloss.NewStyle().Foreground(lipgloss.Color("1"))
	titleStyle     = lipgloss.NewStyle().Bold(true)
)

type Model struct {
	cfg      config.Config
	themes   []theme.Theme
	list     list.Model
	screen   screen
	width    int
	height   int
	current  string
	items    []item   // every theme with its precomputed swatch, in list order
	families []string // unique family names, the tabs after "All"
	tab      int      // 0 is All, i>0 is families[i-1]

	// apply screen
	target   theme.Theme
	appliers []apply.Applier
	results  []apply.Result
	applying bool
	events   chan apply.Result
	err      error
}

func New(cfg config.Config, themes []theme.Theme) Model {
	current := theme.Current(cfg.StateFile)
	var families []string
	for _, t := range themes {
		if len(families) == 0 || families[len(families)-1] != t.Family {
			families = append(families, t.Family) // themes arrive sorted, so families group
		}
	}
	l := list.New(nil, itemDelegate{}, 0, 0)
	l.Title = "themer — one switch for the whole desktop"
	l.SetShowStatusBar(false)
	l.SetShowTitle(false) // the tab bar carries the context instead
	l.Styles.Title = titleStyle
	// The paginator's default • dots read as specks under a 48-item list;
	// full circles at the same cell width keep the layout and become legible.
	l.Paginator.ActiveDot = selectedStyle.Render("●")
	l.Paginator.InactiveDot = dimStyle.Render("○")
	items := make([]item, len(themes))
	for i, t := range themes {
		items[i] = item{t: t, current: t.Name == current, swatch: swatchFor(cfg, t)}
	}
	m := Model{cfg: cfg, themes: themes, list: l, current: current, items: items, families: families}
	m.setTab(0)
	m.selectCurrent() // open on the active theme, not on whatever sorts first
	return m
}

// selectCurrent moves the cursor to the ● theme when it is on screen.
func (m *Model) selectCurrent() {
	for i, li := range m.list.Items() {
		if it, ok := li.(item); ok && it.current {
			m.list.Select(i)
			return
		}
	}
}

// setTab fills the list with the tab's themes: tab 0 is every family, tab
// i>0 is families[i-1]. The cursor goes back to the top — carrying an index
// between differently sized lists lands on an arbitrary theme.
func (m *Model) setTab(tab int) {
	m.tab = tab
	var visible []list.Item
	for _, it := range m.items {
		if tab > 0 && it.t.Family != m.families[tab-1] {
			continue
		}
		visible = append(visible, it)
	}
	m.list.SetItems(visible)
	m.list.ResetSelected()
	m.selectCurrent() // a tab that holds the active theme opens on it
}

// swatchFor renders the preview blocks. A palette that fails to load shows
// as an empty swatch rather than a startup error: one broken generated file
// must not take the whole list down.
func swatchFor(cfg config.Config, t theme.Theme) string {
	p, err := theme.Load(t, cfg.PalettesDir)
	if err != nil {
		return dimStyle.Render("palette unreadable")
	}
	var b strings.Builder
	for _, key := range swatchKeys {
		if hex, ok := p[key]; ok {
			b.WriteString(lipgloss.NewStyle().Foreground(lipgloss.Color(hex)).Render("● "))
		}
	}
	return strings.TrimRight(b.String(), " ")
}

func (m Model) Init() tea.Cmd { return nil }

type resultMsg apply.Result
type applyDoneMsg struct{}

func waitForResult(ch <-chan apply.Result) tea.Cmd {
	return func() tea.Msg {
		r, ok := <-ch
		if !ok {
			return applyDoneMsg{}
		}
		return resultMsg(r)
	}
}

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		// The tab bar wraps on narrow terminals; measure it instead of
		// assuming one line, or the list draws past the bottom edge.
		m.list.SetSize(msg.Width, msg.Height-lipgloss.Height(m.tabBar())-1)
		return m, nil

	case tea.KeyMsg:
		switch m.screen {
		case screenList:
			// While the built-in filter is typing, every key belongs to it.
			if m.list.FilterState() == list.Filtering {
				break
			}
			switch msg.String() {
			case "q", "ctrl+c":
				return m, tea.Quit
			case "enter":
				if it, ok := m.list.SelectedItem().(item); ok {
					return m.startApply(it.t)
				}
			case "tab", "c":
				m.setTab((m.tab + 1) % (len(m.families) + 1))
				return m, nil
			case "shift+tab", "C":
				m.setTab((m.tab + len(m.families)) % (len(m.families) + 1))
				return m, nil
			}
		case screenApply:
			if m.applying {
				break // no keys mid-switch: half a theme is worse than waiting
			}
			switch msg.String() {
			case "q", "ctrl+c":
				return m, tea.Quit
			case "enter", "esc":
				m.screen = screenList
				m = m.refreshCurrent()
				return m, nil
			}
		}

	case resultMsg:
		r := apply.Result(msg)
		if r.Index >= 0 && r.Index < len(m.results) {
			m.results[r.Index] = r
		}
		return m, waitForResult(m.events)

	case applyDoneMsg:
		m.applying = false
		return m, nil
	}

	var cmd tea.Cmd
	m.list, cmd = m.list.Update(msg)
	return m, cmd
}

func (m Model) startApply(t theme.Theme) (tea.Model, tea.Cmd) {
	p, err := theme.Load(t, m.cfg.PalettesDir)
	if err != nil {
		m.err = err
		return m, nil
	}
	m.target = t
	m.appliers = apply.All(m.cfg)
	m.results = make([]apply.Result, len(m.appliers))
	for i, a := range m.appliers {
		m.results[i] = apply.Result{Index: i, Name: a.Name(), Status: apply.StatusPending}
	}
	m.screen = screenApply
	m.applying = true
	m.err = nil
	m.events = make(chan apply.Result)
	go apply.Run(m.appliers, t, p, m.events)
	return m, waitForResult(m.events)
}

// refreshCurrent re-marks the list after a switch without re-reading the
// palettes — only the ● moves. It touches the master items, not the list's
// visible slice, so the marker survives a later tab change.
func (m Model) refreshCurrent() Model {
	m.current = theme.Current(m.cfg.StateFile)
	for i := range m.items {
		m.items[i].current = m.items[i].t.Name == m.current
	}
	keep := m.list.Index()
	m.setTab(m.tab)
	m.list.Select(keep)
	return m
}

func (m Model) View() string {
	switch m.screen {
	case screenApply:
		return m.viewApply()
	default:
		v := m.tabBar() + "\n" + m.list.View()
		if m.err != nil {
			v += "\n" + failStyle.Render("✗ "+m.err.Error())
		}
		return v
	}
}

// tabBar renders All plus one tab per family. Tabs wrap as plain text on
// narrow terminals rather than truncating: losing sight of a family is
// worse than a second line.
func (m Model) tabBar() string {
	labels := make([]string, 0, len(m.families)+1)
	for i, name := range append([]string{"All"}, m.families...) {
		if i > 0 {
			name = strings.ToUpper(name[:1]) + name[1:]
		}
		if i == m.tab {
			labels = append(labels, activeTabStyle.Render(" "+name+" "))
		} else {
			labels = append(labels, tabStyle.Render(" "+name+" "))
		}
	}
	bar := strings.Join(labels, "")
	if m.width > 0 {
		bar = lipgloss.NewStyle().Width(m.width).Render(bar)
	}
	return bar
}

func (m Model) viewApply() string {
	var b strings.Builder
	b.WriteString(titleStyle.Render("Switching to "+m.target.Name) + "\n\n")
	for _, r := range m.results {
		var mark, note string
		switch r.Status {
		case apply.StatusPending:
			mark, note = dimStyle.Render("·"), ""
		case apply.StatusRunning:
			mark, note = "…", ""
		case apply.StatusOK:
			mark, note = okStyle.Render("✓"), dimStyle.Render(r.Note)
		case apply.StatusSkipped:
			mark, note = dimStyle.Render("○"), dimStyle.Render(r.Note)
		case apply.StatusFailed:
			mark, note = failStyle.Render("✗"), failStyle.Render(r.Note)
		}
		fmt.Fprintf(&b, "  %s %-16s %s\n", mark, r.Name, note)
	}
	b.WriteString("\n")
	if m.applying {
		b.WriteString(dimStyle.Render("applying…"))
	} else {
		b.WriteString(dimStyle.Render("done — enter/esc back, q quit"))
	}
	return b.String()
}
