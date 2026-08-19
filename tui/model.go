// Package tui is themer's whole interface: pick a theme from the list, watch
// each applier land. Two screens, nothing modal.
//
// The interface dresses itself the same way the rest of the lvim-tech family
// does: a title badge, a tab strip, a selection and status markers, all coloured
// from a resolved uitheme.Theme compiled once into a Styles (see styles.go).
// themer's own config selects that theme by name — so the theme-switcher is
// itself themeable, instead of the hardcoded ANSI indices it used to carry.
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
	"github.com/lvim-tech/themer/internal/uitheme"
)

type screen int

const (
	screenList screen = iota
	screenApply
)

type item struct {
	t       theme.Theme
	current bool
	swatch  string
}

func (i item) FilterValue() string { return i.t.Name }

// swatchFor renders the preview blocks from the colours the published list
// carried. A theme whose line named none simply shows nothing — the list
// belongs to the generator, and a picker is in no position to insist.
//
// These are the OTHER themes' palettes, shown as a picture beside the name; they
// are read straight from the published list and never resolved through the
// interface's own theme, which dresses only the chrome around them.
func swatchFor(t theme.Theme) string {
	var b strings.Builder
	for _, hex := range t.Swatch {
		b.WriteString(lipgloss.NewStyle().Foreground(lipgloss.Color(hex)).Render("● "))
	}
	return b.String()
}

// itemDelegate renders one theme per line: marker, name, swatches. The swatch
// row is precomputed — 48 themes × a redraw per keystroke is not where lipgloss
// styles should be built. It carries the compiled Styles so every row draws the
// current-theme marker and the selection in the interface's own colours.
type itemDelegate struct {
	styles Styles
}

func (d itemDelegate) Height() int                             { return 1 }
func (d itemDelegate) Spacing() int                            { return 0 }
func (d itemDelegate) Update(_ tea.Msg, _ *list.Model) tea.Cmd { return nil }
func (d itemDelegate) Render(w io.Writer, m list.Model, index int, li list.Item) {
	it, ok := li.(item)
	if !ok {
		return
	}
	s := d.styles
	name := fmt.Sprintf("%-28s", it.t.Name)
	marker := "  "
	if it.current {
		marker = s.Marker.Render(s.Icons.Current)
	}
	var line string
	if index == m.Index() {
		line = s.Cursor.Render(s.Icons.Output+" ") + marker + s.Selection.Render(name) + " " + it.swatch
	} else {
		line = "  " + marker + name + " " + it.swatch
	}
	fmt.Fprint(w, line)
}

type Model struct {
	cfg      config.Config
	styles   Styles
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
	// The interface's own theme: the name selected in config.toml, resolved
	// against the built-in presets and the themes directory. A theme that will
	// not resolve is not fatal here — ResolveTheme hands back the family default
	// so the picker still opens dressed like the rest of the tools.
	resolved, _ := uitheme.ResolveTheme(cfg.Theme)
	s := NewStyles(resolved)

	current := theme.Current(cfg.StateFile)
	var families []string
	for _, t := range themes {
		if len(families) == 0 || families[len(families)-1] != t.Family {
			families = append(families, t.Family) // themes arrive sorted, so families group
		}
	}
	l := list.New(nil, itemDelegate{styles: s}, 0, 0)
	l.Title = "themer — one switch for the whole desktop"
	l.SetShowStatusBar(false)
	l.SetShowTitle(false) // the title badge carries the context instead
	l.SetShowHelp(false)  // the footer draws contextual, wrapping hints
	l.Styles.Title = s.Title
	// The filter's prompt and cursor take the interface's own colours. bubbles
	// reads Styles.Filter* exactly once, inside list.New(), so setting the struct
	// afterwards reaches nothing — the input keeps the library's defaults: a neon
	// yellow prompt (#ECFD65) and a pink cursor (#EE6FF8), neither of which
	// belongs to any palette here.
	l.Styles.FilterPrompt = s.Cursor
	l.Styles.FilterCursor = s.Cursor
	l.FilterInput.PromptStyle = s.Cursor
	l.FilterInput.Cursor.Style = s.Cursor
	l.FilterInput.PlaceholderStyle = s.Muted

	// The paginator's default • dots read as specks under a 48-item list. Full
	// circles, one space apart — the paginator itself concatenates the dots with
	// no gap, so the space rides inside each dot's string.
	l.Paginator.ActiveDot = s.Cursor.Render(s.Icons.PageActive)
	l.Paginator.InactiveDot = s.Muted.Render(s.Icons.PageInactive)
	items := make([]item, len(themes))
	for i, t := range themes {
		items[i] = item{t: t, current: t.Name == current, swatch: swatchFor(t)}
	}
	m := Model{cfg: cfg, styles: s, themes: themes, list: l, current: current, items: items, families: families}
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

// setTab fills the list with the tab's themes: tab 0 is every family, tab i>0 is
// families[i-1]. The cursor goes back to the top — carrying an index between
// differently sized lists lands on an arbitrary theme.
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
		// The chrome wraps on narrow terminals: the tab strip and the footer
		// hints both spill to a second line rather than truncate. Measure them
		// instead of assuming one line each, or the list draws past the bottom
		// edge.
		chromeH := lipgloss.Height(m.header()) + lipgloss.Height(m.listFooter())
		m.list.SetSize(msg.Width, msg.Height-chromeH)
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
	go apply.Run(m.appliers, t, m.events)
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
		parts := []string{m.header(), m.list.View()}
		if m.err != nil {
			parts = append(parts, m.styles.Err.Render(m.styles.Icons.Error+" "+m.err.Error()))
		}
		parts = append(parts, m.listFooter())
		return lipgloss.JoinVertical(lipgloss.Left, parts...)
	}
}

// header renders the title badge, its muted meta line and the family tab strip.
func (m Model) header() string {
	s := m.styles

	meta := fmt.Sprintf("%d themes", len(m.themes))
	if m.current != "" {
		meta += fmt.Sprintf("  %s  current: %s", s.Icons.Separator, m.current)
	}
	title := lipgloss.JoinHorizontal(lipgloss.Center,
		s.Title.Render(" themer "),
		"  ",
		s.HeaderMeta.Render(meta),
	)

	return lipgloss.JoinVertical(lipgloss.Left, title, m.tabBar())
}

// tabBar renders All plus one tab per family. Tabs wrap as plain text on narrow
// terminals rather than truncating: losing sight of a family is worse than a
// second line. The active tab is accent_alt bold, the rest muted.
func (m Model) tabBar() string {
	s := m.styles
	labels := make([]string, 0, len(m.families)+1)
	for i, name := range append([]string{"All"}, m.families...) {
		if i > 0 {
			name = strings.ToUpper(name[:1]) + name[1:]
		}
		if i == m.tab {
			labels = append(labels, s.TabActive.Render(name))
		} else {
			labels = append(labels, s.Tab.Render(name))
		}
	}
	bar := strings.Join(labels, "")
	if m.width > 0 {
		bar = lipgloss.NewStyle().Width(m.width).Render(bar)
	}
	return bar
}

// listFooter renders the contextual key hints under the list. They wrap to the
// terminal width rather than truncate — a key the user cannot see is a key they
// do not have — and go quiet while the filter is typing, where the keys belong
// to the input.
func (m Model) listFooter() string {
	s := m.styles
	var parts []string
	if m.list.FilterState() == list.Filtering {
		parts = []string{"enter accept", "esc cancel"}
	} else {
		parts = []string{"↑/↓ move", "enter apply", "tab family", "/ filter", "q quit"}
	}
	hint := s.Muted.Render(m.hint(parts...))
	if m.width > 0 {
		return lipgloss.NewStyle().Width(m.width).Render(hint)
	}
	return hint
}

// hint joins key hints with the theme's separator glyph.
func (m Model) hint(parts ...string) string {
	return strings.Join(parts, "  "+m.styles.Icons.Bullet+"  ")
}

// viewApply renders the per-target status list: the title badge, one marker per
// applier coloured by its outcome, and a footer that says whether the switch is
// still running.
func (m Model) viewApply() string {
	s := m.styles
	var b strings.Builder

	title := lipgloss.JoinHorizontal(lipgloss.Center,
		s.Title.Render(" themer "),
		"  ",
		s.HeaderMeta.Render("Switching to "+m.target.Name),
	)
	b.WriteString(title + "\n\n")

	for _, r := range m.results {
		var mark, note string
		switch r.Status {
		case apply.StatusPending:
			mark, note = s.Muted.Render(s.Icons.Pending), ""
		case apply.StatusRunning:
			mark, note = s.Step.Render(s.Icons.Running), ""
		case apply.StatusOK:
			mark, note = s.OK.Render(s.Icons.Done), s.Muted.Render(r.Note)
		case apply.StatusSkipped:
			mark, note = s.Muted.Render(s.Icons.Skipped), s.Muted.Render(r.Note)
		case apply.StatusFailed:
			mark, note = s.Err.Render(s.Icons.Error), s.Err.Render(r.Note)
		}
		fmt.Fprintf(&b, "  %s %-16s %s\n", mark, r.Name, note)
	}
	b.WriteString("\n")

	if m.applying {
		b.WriteString(s.Muted.Render("applying…"))
	} else {
		b.WriteString(s.Muted.Render(m.hint("enter/esc back", "q quit")))
	}
	return b.String()
}
