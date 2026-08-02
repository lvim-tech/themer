package theme

import (
	"os"
	"path/filepath"
	"testing"
)

func writePalette(t *testing.T, dir, name, body string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

const sample = `// generated
$style:         'everforest_soft';

$bg:             #2F383E;
$green-dark:     #656831;
$red:            #cb4f4f;
not a colour line
$terminal-bg:    #262f34;
`

func TestDiscoverBuildsCanonicalNames(t *testing.T) {
	dir := t.TempDir()
	writePalette(t, dir, "everforest_soft.scss", sample)
	writePalette(t, dir, "rosepine_dark.scss", sample)
	writePalette(t, dir, "README.md", "not a palette")

	themes, err := Discover(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(themes) != 2 {
		t.Fatalf("expected 2 themes, got %d", len(themes))
	}
	// Rosepine, not RosePine: the families were checked against the 48
	// kitty extras — plain title-casing is the real mapping.
	if themes[0].Name != "LvimEverforest_soft" || themes[1].Name != "LvimRosepine_dark" {
		t.Fatalf("wrong names: %s, %s", themes[0].Name, themes[1].Name)
	}
	if themes[0].GTKArg() != "everforest_soft" {
		t.Fatalf("wrong gtk arg: %s", themes[0].GTKArg())
	}
}

func TestLoadPaletteReadsColoursAndNormalises(t *testing.T) {
	dir := t.TempDir()
	writePalette(t, dir, "everforest_soft.scss", sample)

	p, err := LoadPalette(filepath.Join(dir, "everforest_soft.scss"))
	if err != nil {
		t.Fatal(err)
	}
	// $style is quoted, not a colour; case folds so templates can rely on it.
	if _, ok := p["style"]; ok {
		t.Error("$style leaked into the palette")
	}
	if p["bg"] != "#2f383e" {
		t.Errorf("bg = %q, want lowercased #2f383e", p["bg"])
	}
	if p["green-dark"] != "#656831" || p["terminal-bg"] != "#262f34" {
		t.Errorf("hyphenated keys parsed wrong: %v", p)
	}
}
