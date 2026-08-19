package tui

import (
	"github.com/charmbracelet/lipgloss"

	"github.com/lvim-tech/themer/internal/uitheme"
)

// Styles is every lipgloss style themer's own interface draws with, compiled
// once from a resolved uitheme.Theme. It is built in NewStyles and then only
// read, so a theme change means building a new Styles rather than mutating
// package state — which is also what makes the rendering testable with an
// arbitrary palette.
//
// The style vocabulary is the family's: a title badge, a tab strip, a selection,
// status markers coloured by role. It mirrors clipack/tui/styles.go so themer's
// chrome sits inside the same design as the rest of the tools it switches.
type Styles struct {
	Theme uitheme.Theme
	Icons uitheme.Icons

	// Title is the badge: TitleFg on Accent, bold. HeaderMeta is the muted line
	// of context beside it.
	Title      lipgloss.Style
	HeaderMeta lipgloss.Style

	// Tab strip: active is accent_alt bold, inactive is muted.
	Tab       lipgloss.Style
	TabActive lipgloss.Style

	// Cursor is the accent bar down the left of the selected row, and the colour
	// the filter prompt, cursor and active page dot take.
	Cursor lipgloss.Style
	// Selection paints the selected theme's name: TitleFg on Accent, or reverse
	// video where there is no accent to paint with.
	Selection lipgloss.Style
	// Marker is the ● beside the theme that is currently applied.
	Marker lipgloss.Style

	// Roles.
	Text  lipgloss.Style
	Muted lipgloss.Style
	OK    lipgloss.Style
	Warn  lipgloss.Style
	Err   lipgloss.Style
	Step  lipgloss.Style
}

// NewStyles compiles a resolved theme into styles.
//
// An unset colour deliberately produces a style with no foreground rather than a
// black one: that is how the "mono" theme renders using the terminal's own
// colours, and how a partial custom theme degrades gracefully.
func NewStyles(theme uitheme.Theme) Styles {
	c := theme.Colors

	accent := adaptive(c.Accent)
	accentAlt := adaptive(c.AccentAlt)
	text := adaptive(c.Text)
	muted := adaptive(c.Muted)
	success := adaptive(c.Success)
	warning := adaptive(c.Warning)
	failure := adaptive(c.Error)

	s := Styles{
		Theme: theme,
		Icons: theme.IconSet(),

		// The title is the one place a background is painted, so it needs its own
		// foreground to stay legible on the accent colour.
		Title:      bold(fg(bg(lipgloss.NewStyle(), c.Accent), c.TitleFg)).Padding(0, 1),
		HeaderMeta: setFg(lipgloss.NewStyle(), muted),

		Tab:       setFg(lipgloss.NewStyle().Padding(0, 1), muted),
		TabActive: setFg(lipgloss.NewStyle().Padding(0, 1).Bold(true), accentAlt),

		Cursor: setFg(lipgloss.NewStyle(), accent),
		Marker: setFg(lipgloss.NewStyle(), success),

		// Reverse video is the base: it needs no colour at all, so the selection
		// stays visible on a monochrome theme. An accent colour replaces it below,
		// where one exists.
		Selection: lipgloss.NewStyle().Reverse(true),

		Text:  setFg(lipgloss.NewStyle(), text),
		Muted: setFg(lipgloss.NewStyle(), muted),
		OK:    setFg(lipgloss.NewStyle(), success),
		Warn:  setFg(lipgloss.NewStyle(), warning),
		// Bold is what separates a failure from a warning: the default palette
		// gives the two the same red, so weight tells them apart.
		Err:  setFg(lipgloss.NewStyle().Bold(true), failure),
		Step: setFg(lipgloss.NewStyle().Bold(true), accentAlt),
	}

	if !c.Accent.IsZero() {
		s.Selection = bg(fg(lipgloss.NewStyle(), c.TitleFg), c.Accent)
	}

	// Without colour, emphasis has to come from weight and dimming: the "mono"
	// theme relies on this so the chrome does not collapse into one flat tone.
	if c.Accent.IsZero() {
		s.Title = s.Title.Reverse(true)
	}
	if c.Success.IsZero() {
		s.Marker = s.Marker.Bold(true)
	}
	if c.Muted.IsZero() {
		s.Muted = s.Muted.Faint(true)
		s.HeaderMeta = s.HeaderMeta.Faint(true)
		s.Tab = s.Tab.Faint(true)
	}
	if c.AccentAlt.IsZero() {
		s.Step = s.Step.Bold(true)
		s.TabActive = s.TabActive.Bold(true).Underline(true)
	}

	return s
}

// DefaultStyles is the fallback used before a configuration is available.
func DefaultStyles() Styles {
	return NewStyles(uitheme.DefaultTheme())
}

// adaptive turns a theme colour into a lipgloss colour, or nil when unset.
// lipgloss has no "no colour" value, so an unset entry is represented by a nil
// TerminalColor and simply not applied.
func adaptive(c uitheme.Color) lipgloss.TerminalColor {
	if c.IsZero() {
		return nil
	}
	return lipgloss.AdaptiveColor{Light: c.Light, Dark: c.Dark}
}

// setFg applies a foreground only when the colour is set.
func setFg(s lipgloss.Style, color lipgloss.TerminalColor) lipgloss.Style {
	if color == nil {
		return s
	}
	return s.Foreground(color)
}

func fg(s lipgloss.Style, c uitheme.Color) lipgloss.Style {
	return setFg(s, adaptive(c))
}

func bg(s lipgloss.Style, c uitheme.Color) lipgloss.Style {
	if c.IsZero() {
		return s
	}
	return s.Background(lipgloss.AdaptiveColor{Light: c.Light, Dark: c.Dark})
}

func bold(s lipgloss.Style) lipgloss.Style {
	return s.Bold(true)
}
