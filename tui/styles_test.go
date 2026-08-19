package tui

import (
	"testing"

	"github.com/charmbracelet/lipgloss"

	"github.com/lvim-tech/themer/internal/uitheme"
)

func TestNewStylesCarriesThemeAndIcons(t *testing.T) {
	theme, err := uitheme.ResolveTheme(uitheme.Theme{Name: "mono"})
	if err != nil {
		t.Fatal(err)
	}
	s := NewStyles(theme)
	if s.Theme.Name != "mono" {
		t.Errorf("Theme.Name = %q, want mono", s.Theme.Name)
	}
	// The glyph set travels with the styles so every renderer uses the same one.
	if s.Icons.Done != "+" {
		t.Errorf("Icons.Done = %q, want the ascii glyph the mono preset asks for", s.Icons.Done)
	}
}

func TestNewStylesAppliesColors(t *testing.T) {
	theme := uitheme.Theme{
		Border: "rounded",
		Icons:  uitheme.IconsUnicode,
		Colors: uitheme.ColorSet{
			Accent:  uitheme.Color{Light: "#111111", Dark: "#111111"},
			Success: uitheme.Color{Light: "#00ff00", Dark: "#00ff00"},
			Error:   uitheme.Color{Light: "#ff0000", Dark: "#ff0000"},
		},
	}
	s := NewStyles(theme)

	if s.Cursor.GetForeground() == nil {
		t.Error("the accent colour was not applied to the cursor style")
	}
	if s.Marker.GetForeground() == nil {
		t.Error("the success colour was not applied to the current-theme marker")
	}
	if s.Err.GetForeground() == nil {
		t.Error("the error colour was not applied")
	}
	// With an accent set the selection paints a background rather than reversing.
	if s.Selection.GetBackground() == nil {
		t.Error("the selection did not take the accent background")
	}
	if s.Selection.GetReverse() {
		t.Error("the selection should paint a background, not reverse, when accent is set")
	}
}

func TestNewStylesLeavesUnsetColorsAlone(t *testing.T) {
	// lipgloss has no "no colour" value, so an unset entry must simply not be
	// applied — otherwise "mono" would paint everything black.
	s := NewStyles(uitheme.Theme{Border: "normal"})

	if fg := s.Text.GetForeground(); fg != nil {
		if _, isNoColor := fg.(lipgloss.NoColor); !isNoColor {
			t.Errorf("Text foreground = %#v, want it left unset", fg)
		}
	}
}

func TestNewStylesFallsBackToWeightWithoutColor(t *testing.T) {
	// The mono preset has no colours at all, so emphasis has to come from weight
	// and dimming or the chrome collapses into one flat tone.
	s := NewStyles(uitheme.Theme{Icons: uitheme.IconsASCII})

	if !s.Marker.GetBold() {
		t.Error("Marker is not bold; the current theme would be indistinguishable without colour")
	}
	if !s.Muted.GetFaint() {
		t.Error("Muted is not faint; secondary text would read as body text")
	}
	if !s.Title.GetReverse() {
		t.Error("Title is not reversed; the badge would be invisible without a background")
	}
	if !s.Selection.GetReverse() {
		t.Error("Selection is not reversed; nothing would mark the cursor row without colour")
	}
}

func TestDefaultStylesUsesTheFamilyDefault(t *testing.T) {
	s := DefaultStyles()
	if s.Theme.Name != uitheme.DefaultThemeName {
		t.Errorf("DefaultStyles theme = %q, want the default preset", s.Theme.Name)
	}
	if s.Title.GetBackground() == nil {
		t.Error("the default title badge has no accent background")
	}
}
