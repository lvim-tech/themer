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
		// The footer hints wrap on narrow terminals rather than truncate.
		// Measure the chrome instead of assuming one line per part, or the
		// list draws past the bottom edge and unsticks the footer.
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
		body := m.list.View()
		if m.err != nil {
			body = lipgloss.JoinVertical(lipgloss.Left, body,
				m.styles.Err.Render(m.styles.Icons.Error+" "+m.err.Error()))
		}
		return m.frame(m.header(), body, m.listFooter())
	}
}

// frame composes one screen: the header pinned to the top, the footer pinned to
// the bottom edge, and the body filling — never overflowing — the space between.
// Padding the body is what makes the footer sticky: without it the hints ride
// directly under the last row and drift up and down as the list shrinks.
func (m Model) frame(header, body, footer string) string {
	if m.height <= 0 {
		// No size yet (the first WindowSizeMsg has not arrived): compose
		// plainly rather than guess an edge to stick the footer to.
		return lipgloss.JoinVertical(lipgloss.Left, header, body, footer)
	}
	bodyH := max(m.height-lipgloss.Height(header)-lipgloss.Height(footer), 0)
	body = clampLines(body, bodyH)
	if pad := bodyH - lipgloss.Height(body); pad > 0 {
		body += strings.Repeat("\n", pad)
	}
	// The backstop: every part is already sized to fit, but a frame taller than
	// the terminal scrolls, and under the alternate screen the row lost off the
	// top is the header.
	return clampLines(header+"\n"+body+"\n"+footer, m.height)
}

// clampLines cuts s to at most n lines, from the bottom.
func clampLines(s string, n int) string {
	if n <= 0 {
		return ""
	}
	lines := strings.Split(s, "\n")
	if len(lines) <= n {
		return s
	}
	return strings.Join(lines[:n], "\n")
}

// header renders the top bar — the application chip on the left, the family tab
// strip beside it — and a muted meta line under it.
func (m Model) header() string {
	s := m.styles

	chip := s.Title.Render("themer")
	bar := chip + " " + m.tabStrip(m.width-lipgloss.Width(chip)-1)

	meta := fmt.Sprintf("%d themes", len(m.themes))
	if m.current != "" {
		meta += fmt.Sprintf("  %s  current: %s", s.Icons.Separator, m.current)
	}

	return lipgloss.JoinVertical(lipgloss.Left, bar, s.HeaderMeta.Render(meta))
}

// tabStrip renders All plus one tab per family, narrowing until it fits rather
// than wrapping or running off the edge.
//
// Three widths, tried in order: padded tabs with the active one a button; bare
// labels one space apart; then only the active tab, a count of the rest and the
// key that moves. A strip that overflows pushes the last families out of sight,
// and a family the user cannot see is a family they do not know exists.
func (m Model) tabStrip(avail int) string {
	s := m.styles
	labels := make([]string, 0, len(m.families)+1)
	for i, name := range append([]string{"All"}, m.families...) {
		if i > 0 {
			name = strings.ToUpper(name[:1]) + name[1:]
		}
		labels = append(labels, name)
	}

	full := make([]string, len(labels))
	tight := make([]string, len(labels))
	for i, name := range labels {
		if i == m.tab {
			// The button keeps its shape at both widths: the active tab is the
			// one place the strip must never go quiet.
			full[i] = s.TabActive.Render(name)
			tight[i] = s.TabActive.Render(name)
		} else {
			full[i] = s.Tab.Render(name)
			tight[i] = s.TabTight.Render(name)
		}
	}

	if bar := strings.Join(full, ""); avail <= 0 || lipgloss.Width(bar) <= avail {
		return bar
	}
	if bar := strings.Join(tight, " "); lipgloss.Width(bar) <= avail {
		return bar
	}
	// Nothing else fits. Naming where you are beats a row cut off at an
	// arbitrary point, which reads as though the missing families do not exist.
	return s.TabActive.Render(labels[m.tab]) +
		s.Muted.Render(fmt.Sprintf("  %d/%d  tab moves", m.tab+1, len(labels)))
}

// hintButton renders one footer hint the family way: the key in brackets,
// painted accent, with its muted description beside it.
func (m Model) hintButton(key, label string) string {
	s := m.styles
	return s.Key.Render("["+key+"]") + " " + s.Muted.Render(label)
}

// wrapHints lays the hint buttons into lines that fit the width, whole hints
// only — a key clipped mid-bracket is worse than a second line, and a hint that
// falls off the edge is the one nobody discovers.
//
// Measured with lipgloss.Width, not len: every hint carries colour, and the
// escape codes would otherwise be counted as characters.
func (m Model) wrapHints(parts []string) string {
	width := m.width
	if width < 20 {
		width = 20
	}
	var lines []string
	cur, curW := "", 0
	for _, p := range parts {
		w := lipgloss.Width(p)
		switch {
		case cur == "":
			cur, curW = p, w
		case curW+2+w <= width:
			cur, curW = cur+"  "+p, curW+2+w
		default:
			lines = append(lines, cur)
			cur, curW = p, w
		}
	}
	if cur != "" {
		lines = append(lines, cur)
	}
	return strings.Join(lines, "\n")
}

// listFooter renders the contextual key hints that sit on the bottom edge. They
// go quiet while the filter is typing, where the keys belong to the input.
func (m Model) listFooter() string {
	var parts []string
	if m.list.FilterState() == list.Filtering {
		parts = []string{
			m.hintButton("enter", "accept"),
			m.hintButton("esc", "cancel"),
		}
	} else {
		parts = []string{
			m.hintButton("↑/↓", "move"),
			m.hintButton("enter", "apply"),
			m.hintButton("tab", "family"),
			m.hintButton("/", "filter"),
			m.hintButton("q", "quit"),
		}
	}
	return m.wrapHints(parts)
}

// viewApply renders the per-target status list: the chip up top, one marker per
// applier coloured by its outcome, and a bottom-edge footer that says whether
// the switch is still running.
func (m Model) viewApply() string {
	s := m.styles

	header := s.Title.Render("themer") + " " +
		s.HeaderMeta.Render("Switching to "+m.target.Name)

	var b strings.Builder
	b.WriteString("\n")
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
	body := strings.TrimRight(b.String(), "\n")

	var footer string
	if m.applying {
		footer = s.Muted.Render("applying…")
	} else {
		footer = m.wrapHints([]string{
			m.hintButton("enter/esc", "back"),
			m.hintButton("q", "quit"),
		})
	}
	return m.frame(header, body, footer)
}
