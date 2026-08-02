package apply

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// This machine's wezterm deliberately runs on colors = custom with the
// color_scheme line commented out; the applier must read that as "leave it
// alone", not as a scheme to rewrite.
func TestWeztermSkipsAHandBuiltPalette(t *testing.T) {
	conf := filepath.Join(t.TempDir(), "wezterm.lua")
	body := "   --   color_scheme = \"LvimEverforest_soft\",\n   colors = custom,\n"
	if err := os.WriteFile(conf, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	w := &Wezterm{conf: conf}
	ok, why := w.Detect()
	if ok {
		t.Fatal("a commented-out color_scheme was detected as active")
	}
	if !strings.Contains(why, "hand-built") {
		t.Errorf("the skip reason does not explain itself: %q", why)
	}
}

func TestWeztermRewritesAnActiveScheme(t *testing.T) {
	conf := filepath.Join(t.TempDir(), "wezterm.lua")
	body := "   color_scheme = \"LvimKanagawa_dark\",\n"
	if err := os.WriteFile(conf, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	w := &Wezterm{conf: conf}
	if ok, why := w.Detect(); !ok {
		t.Fatalf("an active color_scheme was not detected: %s", why)
	}
	if _, err := w.Apply(testTheme, testPalette); err != nil {
		t.Fatal(err)
	}
	got, _ := os.ReadFile(conf)
	if !strings.Contains(string(got), `color_scheme = "LvimEverforest_soft",`) {
		t.Errorf("scheme not rewritten in place:\n%s", got)
	}
}

// The include may point anywhere — the colorscheme extras today, clipack's
// copy tomorrow. Only the file name changes; the directory is the user's.
func TestKittyKeepsTheIncludeDirectory(t *testing.T) {
	dir := t.TempDir()
	themes := filepath.Join(dir, "extras", "kitty")
	if err := os.MkdirAll(themes, 0o755); err != nil {
		t.Fatal(err)
	}
	next := filepath.Join(themes, "LvimEverforest_soft.conf")
	if err := os.WriteFile(next, []byte("background #2f383e\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	conf := filepath.Join(dir, "theme.conf")
	old := "include " + filepath.Join(themes, "LvimKanagawa_dark.conf") + "\n"
	if err := os.WriteFile(conf, []byte(old), 0o644); err != nil {
		t.Fatal(err)
	}

	k := &Kitty{themeConf: conf}
	if _, err := k.Apply(testTheme, testPalette); err != nil {
		t.Fatal(err)
	}
	got, _ := os.ReadFile(conf)
	if !strings.Contains(string(got), "include "+next) {
		t.Errorf("include not rewritten:\n%s", got)
	}
}

// Pointing at a theme file that does not exist must fail before the write:
// kitty would otherwise reload into a blank theme.
func TestKittyRefusesAMissingThemeFile(t *testing.T) {
	dir := t.TempDir()
	conf := filepath.Join(dir, "theme.conf")
	if err := os.WriteFile(conf, []byte("include "+filepath.Join(dir, "LvimKanagawa_dark.conf")+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	k := &Kitty{themeConf: conf}
	if _, err := k.Apply(testTheme, testPalette); err == nil {
		t.Fatal("a missing theme file was accepted")
	}
	got, _ := os.ReadFile(conf)
	if !strings.Contains(string(got), "LvimKanagawa_dark.conf") {
		t.Error("the include was rewritten despite the failure")
	}
}
