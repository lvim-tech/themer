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

func TestFromConfigParsesTheNameAndNormalisesColours(t *testing.T) {
	th := FromConfig("LvimCustom_dark", map[string]string{"bg": " 1E1E2E", "red": "#CB4F4F"})
	if th.Family != "custom" || th.Variant != "dark" {
		t.Errorf("canonical name parsed as %q/%q", th.Family, th.Variant)
	}
	// Bare rrggbb gains its #, case folds — the same shape LoadPalette makes.
	if th.Inline["bg"] != "#1e1e2e" || th.Inline["red"] != "#cb4f4f" {
		t.Errorf("colours not normalised: %v", th.Inline)
	}
	// A free-form name still works: its own family, so it gets its own tab.
	if free := FromConfig("MyThing", nil); free.Family != "mything" {
		t.Errorf("free-form family = %q", free.Family)
	}
}

// A config theme with a known name replaces the file theme, and Load then
// prefers the inline palette — the file is never opened for it again.
func TestMergeReplacesByNameAndLoadPrefersInline(t *testing.T) {
	dir := t.TempDir()
	writePalette(t, dir, "everforest_soft.scss", sample)
	themes, err := Discover(dir)
	if err != nil {
		t.Fatal(err)
	}

	override := FromConfig("LvimEverforest_soft", map[string]string{"bg": "#000000"})
	themes = Merge(themes, []Theme{override, FromConfig("LvimCustom_dark", map[string]string{"bg": "#111111"})})
	if len(themes) != 2 {
		t.Fatalf("merge produced %d themes, want 2", len(themes))
	}

	got, ok := ByName(themes, "LvimEverforest_soft")
	if !ok {
		t.Fatal("the overridden theme vanished")
	}
	p, err := Load(got, dir)
	if err != nil {
		t.Fatal(err)
	}
	if p["bg"] != "#000000" {
		t.Errorf("Load read the file (%q), not the inline palette", p["bg"])
	}

	// The file theme still loads from disk when no inline palette exists.
	plain, _ := ByName([]Theme{{Name: "LvimEverforest_soft", Family: "everforest", Variant: "soft"}}, "LvimEverforest_soft")
	p, err = Load(plain, dir)
	if err != nil {
		t.Fatal(err)
	}
	if p["bg"] != "#2f383e" {
		t.Errorf("file palette read wrong: %q", p["bg"])
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
