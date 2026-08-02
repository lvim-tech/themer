package tui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/lvim-tech/themer/internal/apply"
	"github.com/lvim-tech/themer/internal/config"
	"github.com/lvim-tech/themer/internal/theme"
)

func testModel(t *testing.T) Model {
	t.Helper()
	dir := t.TempDir()
	palette := "$bg: #2f383e;\n$fg: #b1b7b0;\n$red: #cb4f4f;\n"
	for _, name := range []string{"everforest_soft.scss", "nord_dark.scss"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(palette), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	state := filepath.Join(dir, ".theme")
	if err := os.WriteFile(state, []byte("LvimEverforest_soft\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg := config.Default()
	cfg.PalettesDir = dir
	cfg.StateFile = state
	themes, err := theme.Discover(dir)
	if err != nil {
		t.Fatal(err)
	}
	m := New(cfg, themes)
	next, _ := m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	return next.(Model)
}

func TestListShowsThemesAndMarksTheCurrent(t *testing.T) {
	m := testModel(t)
	view := m.View()
	for _, want := range []string{"LvimEverforest_soft", "LvimNord_dark", "●"} {
		if !strings.Contains(view, want) {
			t.Errorf("list view is missing %q", want)
		}
	}
}

// The apply screen must tell apart every outcome at a glance: the tick, the
// skip with its reason, the failure with its error.
func TestApplyScreenRendersEachStatus(t *testing.T) {
	m := testModel(t)
	m.screen = screenApply
	m.target = theme.Theme{Name: "LvimNord_dark", Family: "nord", Variant: "dark"}
	m.results = []apply.Result{
		{Index: 0, Name: "~/.theme", Status: apply.StatusOK, Note: "LvimNord_dark"},
		{Index: 1, Name: "hyprland", Status: apply.StatusSkipped, Note: "Hyprland is not running"},
		{Index: 2, Name: "kitty", Status: apply.StatusFailed, Note: "theme file missing"},
		{Index: 3, Name: "gtk 3+4", Status: apply.StatusPending},
	}
	view := m.View()
	for _, want := range []string{
		"Switching to LvimNord_dark",
		"✓", "○ ", "✗", "Hyprland is not running", "theme file missing",
	} {
		if !strings.Contains(view, want) {
			t.Errorf("apply view is missing %q:\n%s", want, view)
		}
	}
}

// Coming back from the apply screen re-reads the state file, so the ● sits
// on the theme that was just applied, not the one the program started with.
func TestReturningFromApplyMovesTheCurrentMarker(t *testing.T) {
	m := testModel(t)
	if err := os.WriteFile(m.cfg.StateFile, []byte("LvimNord_dark\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	m.screen = screenApply
	m.applying = false
	next, _ := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = next.(Model)
	if m.screen != screenList {
		t.Fatal("enter did not leave the apply screen")
	}
	if m.current != "LvimNord_dark" {
		t.Errorf("current = %q, the marker did not follow the switch", m.current)
	}
}
