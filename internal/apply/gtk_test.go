package apply

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/lvim-tech/themer/internal/theme"
)

// newTestGTK builds a GTK target over throwaway directories with one theme
// installed, and records the gsettings write instead of performing it — the
// real one would repaint the machine running the suite.
func newTestGTK(t *testing.T) (*GTK, *string) {
	t.Helper()
	dir := t.TempDir()
	src := filepath.Join(dir, "themes", "Lvim-EverforestSoft", "gtk-4.0")
	if err := os.MkdirAll(filepath.Join(src, "lvim-assets"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(src, "gtk.css"), []byte("/* lvim-gtk everforest_soft */\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(src, "lvim-assets", "check.svg"), []byte("<svg/>"), 0o644); err != nil {
		t.Fatal(err)
	}
	var set string
	g := &GTK{
		themes:  filepath.Join(dir, "themes"),
		cfg3:    filepath.Join(dir, "config", "gtk-3.0"),
		cfg4:    filepath.Join(dir, "config", "gtk-4.0"),
		setName: func(name string) error { set = name; return nil },
	}
	return g, &set
}

func read(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("%s: %v", path, err)
	}
	return string(b)
}

// The GTK4 half is the only one that has to be copied, and the assets have to
// follow the stylesheet: url() resolves relative to the stylesheet, which now
// lives in ~/.config rather than in the theme directory.
func TestGTKCopiesTheStylesheetAndItsAssets(t *testing.T) {
	g, set := newTestGTK(t)
	if _, err := g.Apply(testTheme); err != nil {
		t.Fatal(err)
	}
	if got := read(t, filepath.Join(g.cfg4, "gtk.css")); !strings.Contains(got, "everforest_soft") {
		t.Errorf("gtk-4.0/gtk.css not installed: %q", got)
	}
	if got := read(t, filepath.Join(g.cfg4, "lvim-assets", "check.svg")); got != "<svg/>" {
		t.Errorf("assets not copied: %q", got)
	}
	if *set != "Lvim-EverforestSoft" {
		t.Errorf("gsettings got %q, want Lvim-EverforestSoft", *set)
	}
}

// Stale glyphs from the previous theme must not survive the switch — the
// directory is replaced, not merged.
func TestGTKReplacesTheAssetsRatherThanMerging(t *testing.T) {
	g, _ := newTestGTK(t)
	stale := filepath.Join(g.cfg4, "lvim-assets", "old.svg")
	if err := os.MkdirAll(filepath.Dir(stale), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(stale, []byte("<svg/>"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := g.Apply(testTheme); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(stale); err == nil {
		t.Error("a glyph from the previous theme survived the switch")
	}
}

// Someone else's GTK4 stylesheet is preserved once, and the copy is never
// written again — the point is to hold what was there BEFORE lvim-gtk ever
// ran. The case that tests it is a second generator putting its own stylesheet
// back between two switches: preserving that one would overwrite the only copy
// of the original, which is the same as not preserving anything at all.
func TestGTKPreservesTheStylesheetThatWasThereFirst(t *testing.T) {
	g, _ := newTestGTK(t)
	if err := os.MkdirAll(g.cfg4, 0o755); err != nil {
		t.Fatal(err)
	}
	css := filepath.Join(g.cfg4, "gtk.css")
	if err := os.WriteFile(css, []byte("/* the original */\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := g.Apply(testTheme); err != nil {
		t.Fatal(err)
	}
	// nwg-look, a settings dialog, another generator: something writes its own
	// stylesheet back over ours before the next switch.
	if err := os.WriteFile(css, []byte("/* someone else */\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := g.Apply(testTheme); err != nil {
		t.Fatal(err)
	}
	if got := read(t, css+".before-lvim"); got != "/* the original */\n" {
		t.Errorf("the preserved copy no longer holds the original: %q", got)
	}
}

// A user stylesheet loads at PRIORITY_USER (800) and outranks the theme's 200.
// Left in place it silently overrides everything this target just installed.
func TestGTKMovesAsideAForeignGTK3Stylesheet(t *testing.T) {
	g, _ := newTestGTK(t)
	if err := os.MkdirAll(g.cfg3, 0o755); err != nil {
		t.Fatal(err)
	}
	css := filepath.Join(g.cfg3, "gtk.css")
	if err := os.WriteFile(css, []byte("window { background: red; }\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := g.Apply(testTheme); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(css); err == nil {
		t.Error("the overriding stylesheet was left in place")
	}
	if got := read(t, filepath.Join(g.cfg3, "before-lvim", "gtk.css")); !strings.Contains(got, "red") {
		t.Errorf("it was deleted rather than moved aside: %q", got)
	}
}

// GTK3 with no settings daemon — a plain wayland session — reads this file and
// nothing else, so all three shapes it can arrive in have to end up naming the
// theme. The last case is the one the shell version got wrong: its sed matched
// [Settings], so a file without that header was left untouched and the switch
// reported success having changed nothing.
func TestGTKNamesTheThemeInEverySettingsINIShape(t *testing.T) {
	for _, tc := range []struct{ name, before string }{
		{"missing file", ""},
		{"key present", "[Settings]\ngtk-theme-name=Lvim-KanagawaDark\ngtk-font-name=Sans 10\n"},
		{"section without the key", "[Settings]\ngtk-font-name=Sans 10\n"},
		{"no section at all", "gtk-font-name=Sans 10\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			g, _ := newTestGTK(t)
			ini := filepath.Join(g.cfg3, "settings.ini")
			if tc.before != "" {
				if err := os.MkdirAll(g.cfg3, 0o755); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(ini, []byte(tc.before), 0o644); err != nil {
					t.Fatal(err)
				}
			}
			if _, err := g.Apply(testTheme); err != nil {
				t.Fatal(err)
			}
			got := read(t, ini)
			if !strings.Contains(got, "gtk-theme-name=Lvim-EverforestSoft") {
				t.Errorf("theme not named:\n%s", got)
			}
			if strings.Contains(got, "Lvim-KanagawaDark") {
				t.Errorf("the previous theme is still named:\n%s", got)
			}
			if strings.Count(got, "gtk-theme-name=") != 1 {
				t.Errorf("gtk-theme-name appears %d times:\n%s", strings.Count(got, "gtk-theme-name="), got)
			}
			if !strings.Contains(got, "[Settings]") {
				t.Errorf("the section header is gone:\n%s", got)
			}
		})
	}
}

// Applying a theme that was never built must fail loudly. Half-applying it —
// gtk.css copied from nowhere, gtk-theme-name pointing at a missing directory
// — would leave the desktop unthemed with every target reporting success.
func TestGTKRefusesAThemeThatIsNotInstalled(t *testing.T) {
	g, set := newTestGTK(t)
	missing := theme.Theme{Name: "LvimKanagawa_dark", Family: "kanagawa", Variant: "dark"}
	if _, err := g.Apply(missing); err == nil {
		t.Fatal("a theme that is not installed was accepted")
	}
	if *set != "" {
		t.Errorf("gtk-theme-name was set to %q despite the failure", *set)
	}
}

// Nothing to switch is a skip with a reason, never a failure.
func TestGTKSkipsWhenNoThemeIsInstalled(t *testing.T) {
	g := &GTK{themes: filepath.Join(t.TempDir(), "themes")}
	ok, why := g.Detect()
	if ok {
		t.Fatal("an empty themes directory was detected as usable")
	}
	if !strings.Contains(why, "themes") {
		t.Errorf("the skip reason does not say what is missing: %q", why)
	}
}

func TestGTKName(t *testing.T) {
	for _, tc := range []struct{ family, variant, want string }{
		{"everforest", "soft", "Lvim-EverforestSoft"},
		{"catppuccin", "darker", "Lvim-CatppuccinDarker"},
		{"lvim", "light", "Lvim-LvimLight"},
		{"custom", "", "Lvim-Custom"},
	} {
		got := theme.Theme{Family: tc.family, Variant: tc.variant}.GTKName()
		if got != tc.want {
			t.Errorf("%s_%s → %s, want %s", tc.family, tc.variant, got, tc.want)
		}
	}
}

// The layout of an installed theme belongs to lvim-gtk, not here. This target
// copies two paths out of it, so a change on that side has to fail at `go
// test` rather than at the next switch, when the symptom is GTK4 keeping the
// previous colours and nothing saying why.
func TestInstalledThemesStillHaveTheLayoutWeCopy(t *testing.T) {
	g := NewGTK()
	entries, err := os.ReadDir(g.themes)
	if err != nil {
		t.Skip("no themes directory on this machine")
	}
	checked := 0
	for _, e := range entries {
		if !e.IsDir() || !strings.HasPrefix(e.Name(), "Lvim-") {
			continue
		}
		checked++
		for _, rel := range []string{"gtk-4.0/gtk.css", "gtk-4.0/lvim-assets"} {
			if _, err := os.Stat(filepath.Join(g.themes, e.Name(), rel)); err != nil {
				t.Errorf("%s: %v — lvim-gtk's layout moved under us", e.Name(), err)
			}
		}
	}
	if checked == 0 {
		t.Skip("no Lvim-* theme installed on this machine")
	}
}
