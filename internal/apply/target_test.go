package apply

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/lvim-tech/themer/internal/config"
	"github.com/lvim-tech/themer/internal/theme"
)

var testTheme = theme.Theme{Name: "LvimEverforest_soft", Family: "everforest", Variant: "soft"}

var testPalette = theme.Palette{
	"yellow-dark": "#a6935a",
	"green-dark":  "#656831",
	"red":         "#cb4f4f",
	"green":       "#75783a",
	"purple":      "#635d71",
	"teal":        "#357b6d",
	"bg-dark":     "#202527",
}

func targetFor(t *testing.T, name, file, body string) (*TargetApplier, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), file)
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	var tgt config.Target
	for _, d := range config.DefaultTargets() {
		if d.Name == name {
			tgt = d
		}
	}
	if tgt.Name == "" {
		t.Fatalf("no default target %q", name)
	}
	tgt.Detect = config.Detect{File: path}
	tgt.Edits[0].File = path
	tgt.Reload = nil // reload commands need the compositor; the edit is what is under test
	return NewTarget(tgt, config.DefaultRoles()), path
}

// The mango file carries commented-out duplicates of every colour line —
// the rewrite must leave those alone or the file stops documenting itself.
func TestMangoRewriteSkipsTheCommentedDuplicates(t *testing.T) {
	body := "borderpx=3\nrootcolor=0x201b14ff\nbordercolor=0x3f7445ff\nfocuscolor=0xb99247ff\n" +
		"maximizescreencolor=0x89aa61ff\nurgentcolor=0xcb4f4fff\nscratchpadcolor=0x75783aff\n" +
		"globalcolor=0x635d71ff\noverlaycolor=0x14a57cff\n\n# rootcolor=0x201b14\n# bordercolor=0x3f7445\n"
	a, path := targetFor(t, "mango", "appearance.conf", body)

	if _, err := a.Apply(testTheme, testPalette); err != nil {
		t.Fatal(err)
	}
	got, _ := os.ReadFile(path)
	for _, want := range []string{
		"rootcolor=0x202527ff",   // {root} → bg-dark
		"focuscolor=0xa6935aff",  // {focus} → yellow-dark
		"urgentcolor=0xcb4f4fff", // {urgent} → red
		"# rootcolor=0x201b14",   // the comment survives
		"# bordercolor=0x3f7445", //
	} {
		if !strings.Contains(string(got), want) {
			t.Errorf("rewritten file is missing %q:\n%s", want, got)
		}
	}
	if strings.Contains(string(got), "0x201b14ff") {
		t.Error("the live rootcolor kept its old value")
	}
}

// inactive-color contains "active-color" as a substring; only the line
// anchor keeps the two rules apart, so this is the test that fails first
// if someone relaxes the regex.
func TestNiriRewriteKeepsActiveAndInactiveApart(t *testing.T) {
	body := "    focus-ring {\n        active-color \"#b99247\"\n        inactive-color \"#3f7445\"\n" +
		"        urgent-color \"#cb4f4fa6\"\n    }\n"
	a, path := targetFor(t, "niri", "04-layout.kdl", body)

	if _, err := a.Apply(testTheme, testPalette); err != nil {
		t.Fatal(err)
	}
	got, _ := os.ReadFile(path)
	if !strings.Contains(string(got), `active-color "#a6935a"`) {
		t.Errorf("active-color not rewritten:\n%s", got)
	}
	if !strings.Contains(string(got), `inactive-color "#656831"`) {
		t.Errorf("inactive-color not rewritten to the border role:\n%s", got)
	}
	if !strings.Contains(string(got), `urgent-color "#cb4f4fa6"`) {
		t.Errorf("urgent-color lost its alpha:\n%s", got)
	}
}

func TestHyprlandRewriteKeepsIndentAndComment(t *testing.T) {
	body := "general {\n    # col.active_border = rgba(cb4f4fff) rgba(cb4f4fff) 90deg\n" +
		"    col.active_border = rgba(B99247FF)\n    col.inactive_border = rgba(3F7445FF)\n}\n"
	a, path := targetFor(t, "hyprland", "appearance.conf", body)

	if _, err := a.Apply(testTheme, testPalette); err != nil {
		t.Fatal(err)
	}
	got, _ := os.ReadFile(path)
	if !strings.Contains(string(got), "    col.active_border = rgba(a6935aff)") {
		t.Errorf("active border wrong or indent lost:\n%s", got)
	}
	if !strings.Contains(string(got), "# col.active_border = rgba(cb4f4fff)") {
		t.Errorf("the commented example was rewritten:\n%s", got)
	}
}

// A rule that matches nothing must fail loudly: silently skipping it shows
// up later as "the switch worked but my bar kept the old colours".
func TestRuleMatchingNothingFails(t *testing.T) {
	a, _ := targetFor(t, "mango", "appearance.conf", "borderpx=3\n")
	if _, err := a.Apply(testTheme, testPalette); err == nil {
		t.Fatal("a rule matching nothing passed silently")
	}
}

func TestUnknownPlaceholderNamesTheKey(t *testing.T) {
	a, _ := targetFor(t, "mango", "appearance.conf", "rootcolor=0x0\n")
	a.t.Edits[0].Rules = []config.Rule{{Regex: `^rootcolor=\S+`, Value: "rootcolor=0x{nonsense}ff"}}
	_, err := a.Apply(testTheme, testPalette)
	if err == nil || !strings.Contains(err.Error(), "nonsense") {
		t.Fatalf("expected the missing placeholder to be named, got %v", err)
	}
}

func TestThemePlaceholderExpandsToTheCanonicalName(t *testing.T) {
	a, path := targetFor(t, "mango", "appearance.conf", "rootcolor=0x0\n")
	a.t.Edits[0].Rules = []config.Rule{{Regex: `^rootcolor=\S+`, Value: "rootcolor={theme}"}}
	if _, err := a.Apply(testTheme, testPalette); err != nil {
		t.Fatal(err)
	}
	got, _ := os.ReadFile(path)
	if !strings.Contains(string(got), "rootcolor=LvimEverforest_soft") {
		t.Errorf("{theme} did not expand:\n%s", got)
	}
}

// ghostty's config carries commented guidance lines that mention the theme
// key; only the real assignment may move when the theme switches.
func TestGhosttyRewritesThemeAndSparesComments(t *testing.T) {
	a, path := targetFor(t, "ghostty", "config",
		"# The theme line below is rewritten by themer on every switch.\n"+
			"# theme = LvimNord_dark\n"+
			"theme = LvimKanagawa_dark\n")

	if _, err := a.Apply(testTheme, testPalette); err != nil {
		t.Fatal(err)
	}
	got, _ := os.ReadFile(path)
	if !strings.Contains(string(got), "\ntheme = LvimEverforest_soft\n") {
		t.Errorf("theme not rewritten:\n%s", got)
	}
	if !strings.Contains(string(got), "# theme = LvimNord_dark") {
		t.Errorf("commented example did not survive:\n%s", got)
	}
}

// alacritty's import keeps its directory across a switch — only the file
// name is the theme's. A rule that lost the directory would point alacritty
// at a theme file in ~/.config/alacritty that does not exist.
func TestAlacrittyRewritesImportAndKeepsItsDirectory(t *testing.T) {
	a, path := targetFor(t, "alacritty", "alacritty.toml",
		"live_config_reload = true\n"+
			"import = [\"/somewhere/extras/alacritty/LvimNord_dark.toml\"]\n")

	if _, err := a.Apply(testTheme, testPalette); err != nil {
		t.Fatal(err)
	}
	got, _ := os.ReadFile(path)
	if !strings.Contains(string(got), `import = ["/somewhere/extras/alacritty/LvimEverforest_soft.toml"]`) {
		t.Errorf("import not rewritten in place:\n%s", got)
	}
	if !strings.Contains(string(got), "live_config_reload = true") {
		t.Errorf("unrelated line did not survive:\n%s", got)
	}
}
