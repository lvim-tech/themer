package uitheme

import (
	"os"
	"path/filepath"
	"testing"
)

func TestResolveDefaultIsFullyPopulated(t *testing.T) {
	resolved, err := ResolveTheme(Theme{})
	if err != nil {
		t.Fatalf("ResolveTheme(zero) error = %v", err)
	}
	if resolved.Name != DefaultThemeName {
		t.Errorf("Name = %q, want the default preset", resolved.Name)
	}
	if resolved.Colors.Accent.IsZero() {
		t.Error("the default palette resolved without an accent colour")
	}
	if resolved.Colors.TitleFg.IsZero() {
		t.Error("the default palette resolved without a title foreground")
	}
}

func TestResolveMonoHasNoColours(t *testing.T) {
	resolved, err := ResolveTheme(Theme{Name: "mono"})
	if err != nil {
		t.Fatalf("ResolveTheme(mono) error = %v", err)
	}
	// mono paints with the terminal's own colours: emphasis comes from weight,
	// so every role must stay unset for NewStyles to degrade correctly.
	for _, entry := range resolved.Colors.Named() {
		if !entry.Color.IsZero() {
			t.Errorf("mono set %q = %s, want it left unset", entry.Key, entry.Color)
		}
	}
	if resolved.Icons != IconsASCII {
		t.Errorf("mono Icons = %q, want ascii", resolved.Icons)
	}
}

func TestResolveOverridesStructureNotPalette(t *testing.T) {
	// Selecting a border in config.toml must not wipe the base palette: only the
	// knob that was named changes.
	resolved, err := ResolveTheme(Theme{Name: "default", Border: "rounded"})
	if err != nil {
		t.Fatal(err)
	}
	if resolved.Border != "rounded" {
		t.Errorf("Border = %q, the override did not land", resolved.Border)
	}
	if resolved.Colors.Accent.IsZero() {
		t.Error("overriding the border dropped the palette")
	}
}

func TestColorOverrideMergesOntoBase(t *testing.T) {
	over := Theme{Name: "default", Colors: ColorSet{
		Accent: Color{Light: "#123456", Dark: "#123456"},
	}}
	resolved, err := ResolveTheme(over)
	if err != nil {
		t.Fatal(err)
	}
	if resolved.Colors.Accent.Dark != "#123456" {
		t.Errorf("Accent.Dark = %q, the override did not win", resolved.Colors.Accent.Dark)
	}
	// A single-colour override leaves the other eight roles on the base.
	if resolved.Colors.Success.IsZero() {
		t.Error("overriding one colour dropped the rest of the palette")
	}
}

func TestLoadThemeFileFromDir(t *testing.T) {
	dir := t.TempDir()
	// ThemesDir reads ~/.config/themer/themes; point HOME at a temp tree so the
	// test never touches a real config.
	t.Setenv("HOME", dir)
	themesDir := filepath.Join(dir, ".config", "themer", "themes")
	if err := os.MkdirAll(themesDir, 0o755); err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(themesDir, "midnight.yaml")
	body := "colors:\n  accent: \"#001f3f\"\n  accent_alt: {light: \"#ff851b\", dark: \"#ff851b\"}\n"
	if err := os.WriteFile(file, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}

	loaded, err := LoadThemeFile("midnight")
	if err != nil {
		t.Fatalf("LoadThemeFile error = %v", err)
	}
	if loaded.Name != "midnight" {
		t.Errorf("Name = %q, want the file's basename", loaded.Name)
	}
	// Scalar form applies to both backgrounds.
	if loaded.Colors.Accent.Light != "#001f3f" || loaded.Colors.Accent.Dark != "#001f3f" {
		t.Errorf("scalar accent did not fill both backgrounds: %+v", loaded.Colors.Accent)
	}
	// Mapping form keeps its two values.
	if loaded.Colors.AccentAlt.Dark != "#ff851b" {
		t.Errorf("mapping accent_alt.dark = %q", loaded.Colors.AccentAlt.Dark)
	}

	// Resolving a file name layers it on the default base, so unnamed roles fall
	// back rather than vanish.
	resolved, err := ResolveTheme(Theme{Name: "midnight"})
	if err != nil {
		t.Fatalf("ResolveTheme(file) error = %v", err)
	}
	if resolved.Colors.Accent.Dark != "#001f3f" {
		t.Errorf("resolved accent = %q, the file did not win", resolved.Colors.Accent.Dark)
	}
	if resolved.Colors.Text.IsZero() {
		t.Error("a partial file dropped the base's text colour")
	}
}

func TestResolveMissingThemeFallsBackToDefault(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HOME", dir) // no themes directory at all
	resolved, err := ResolveTheme(Theme{Name: "does-not-exist"})
	if err == nil {
		t.Error("a missing theme should report an error the caller can log")
	}
	// Even on error the caller gets a usable theme, not a blank one.
	if resolved.Colors.Accent.IsZero() {
		t.Error("the fallback theme has no accent colour")
	}
}

func TestLoadThemeFileRejectsPathEscape(t *testing.T) {
	for _, name := range []string{"../evil", "a/b", ".", ".."} {
		if _, err := LoadThemeFile(name); err == nil {
			t.Errorf("LoadThemeFile(%q) was accepted; a name is a file, not a path", name)
		}
	}
}

func TestValidateRejectsUnknownKnobs(t *testing.T) {
	if err := (Theme{Border: "triangle"}).Validate(); err == nil {
		t.Error("an unknown border was accepted")
	}
	if err := (Theme{Icons: "emoji"}).Validate(); err == nil {
		t.Error("an unknown icon set was accepted")
	}
	bad := Theme{Colors: ColorSet{Accent: Color{Light: "notacolour", Dark: "notacolour"}}}
	if err := bad.Validate(); err == nil {
		t.Error("a non-colour value was accepted")
	}
	good := Theme{Border: "rounded", Icons: IconsUnicode,
		Colors: ColorSet{Accent: Color{Light: "#fff", Dark: "5"}}}
	if err := good.Validate(); err != nil {
		t.Errorf("a hex value and an ANSI index were rejected: %v", err)
	}
}
