// Package theme discovers the generated Lvim themes and parses their palettes.
//
// The single source of truth is lvim-gtk/palettes/*.scss — those files are
// generated from lvim-colorscheme, carry every colour a theme owns, and their
// basenames (everforest_soft) map 1:1 onto the canonical names everything
// keyed on $THEME uses (LvimEverforest_soft). The mapping is plain
// lowercasing: no family has an internal capital, which was verified against
// the 48 kitty extras before this package relied on it.
package theme

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// Theme is one switchable theme, known under three spellings:
// canonical LvimEverforest_soft ($THEME, kitty/tmux/wezterm file names),
// palette everforest_soft (the lvim-gtk palettes), and the GTK build name
// Lvim-EverforestSoft, which is the directory it installs under.
type Theme struct {
	Name    string // canonical: LvimEverforest_soft
	Family  string // everforest
	Variant string // soft
	// Inline is a palette defined as values in config.toml instead of a
	// file. When set, Load never touches the palettes directory — such a
	// theme exists entirely in the configuration.
	Inline Palette
}

// GTKArg is the palette basename: everforest_soft.
func (t Theme) GTKArg() string { return t.Family + "_" + t.Variant }

// GTKName is the directory the theme installs under in ~/.local/share/themes
// and the value gtk-theme-name takes: Lvim-EverforestSoft. It is derived, not
// stored — the same mapping lvim-gtk's theme_name() applies to a palette
// basename, which drops the underscore and capitalises what followed it.
func (t Theme) GTKName() string {
	name := "Lvim-" + capitalise(t.Family)
	if t.Variant != "" {
		name += capitalise(t.Variant)
	}
	return name
}

func capitalise(s string) string {
	if s == "" {
		return ""
	}
	return strings.ToUpper(s[:1]) + s[1:]
}

// PaletteFile is the theme's palette inside the palettes directory.
func (t Theme) PaletteFile(palettesDir string) string {
	return filepath.Join(palettesDir, t.Family+"_"+t.Variant+".scss")
}

// Palette maps a colour key (bg, green-dark, …) to its #rrggbb value.
type Palette map[string]string

// Discover lists the themes by their palette files.
func Discover(palettesDir string) ([]Theme, error) {
	entries, err := os.ReadDir(palettesDir)
	if err != nil {
		return nil, fmt.Errorf("palettes directory: %w", err)
	}
	var themes []Theme
	for _, e := range entries {
		base, ok := strings.CutSuffix(e.Name(), ".scss")
		if !ok {
			continue
		}
		name, ok := NameFor(base)
		if !ok {
			continue
		}
		family, variant, _ := strings.Cut(base, "_")
		themes = append(themes, Theme{Name: name, Family: family, Variant: variant})
	}
	sort.Slice(themes, func(i, j int) bool { return themes[i].Name < themes[j].Name })
	if len(themes) == 0 {
		return nil, fmt.Errorf("no *.scss palettes in %s", palettesDir)
	}
	return themes, nil
}

// FromConfig builds a theme out of configuration values. The name is parsed
// like a canonical one when it fits the LvimFamily_variant shape; anything
// else becomes its own single-theme family, so a config theme never has to
// follow the naming scheme to appear in the list.
func FromConfig(name string, colors map[string]string) Theme {
	t := Theme{Name: name, Family: strings.ToLower(name)}
	if after, ok := strings.CutPrefix(name, "Lvim"); ok {
		if family, variant, ok := strings.Cut(after, "_"); ok && family != "" {
			t.Family, t.Variant = strings.ToLower(family), variant
		}
	}
	t.Inline = Palette{}
	for key, value := range colors {
		value = strings.ToLower(strings.TrimSpace(value))
		if !strings.HasPrefix(value, "#") {
			value = "#" + value // tolerate bare rrggbb — TOML strings drop nothing
		}
		t.Inline[key] = value
	}
	return t
}

// Merge folds config themes into the discovered list: a matching name
// replaces the file theme (the inline palette wins), a new name is added.
func Merge(themes []Theme, extra []Theme) []Theme {
	for _, e := range extra {
		replaced := false
		for i := range themes {
			if themes[i].Name == e.Name {
				themes[i] = e
				replaced = true
				break
			}
		}
		if !replaced {
			themes = append(themes, e)
		}
	}
	sort.Slice(themes, func(i, j int) bool { return themes[i].Name < themes[j].Name })
	return themes
}

// Load resolves a theme's palette: inline values when the configuration
// defines them, the palette file otherwise.
func Load(t Theme, palettesDir string) (Palette, error) {
	if t.Inline != nil {
		if len(t.Inline) == 0 {
			return nil, fmt.Errorf("theme %s defines no colours in the configuration", t.Name)
		}
		return t.Inline, nil
	}
	return LoadPalette(t.PaletteFile(palettesDir))
}

// ByName finds a theme by its canonical name.
func ByName(themes []Theme, name string) (Theme, bool) {
	for _, t := range themes {
		if t.Name == name {
			return t, true
		}
	}
	return Theme{}, false
}

// LoadPalette parses the `$key: #hex;` lines of a palette file.
func LoadPalette(path string) (Palette, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return ParsePalette(f, path)
}

// ParsePalette reads a palette from any source — the local file, a GitHub
// download. Everything that is not a `$key: #hex;` line — comments, $style —
// is skipped rather than rejected, because the file is generated and its
// prose changes more often than its colour lines. label only names the
// source in errors.
func ParsePalette(r io.Reader, label string) (Palette, error) {
	p := Palette{}
	sc := bufio.NewScanner(r)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if !strings.HasPrefix(line, "$") {
			continue
		}
		key, value, ok := strings.Cut(line[1:], ":")
		if !ok {
			continue
		}
		value = strings.TrimSuffix(strings.TrimSpace(value), ";")
		if !strings.HasPrefix(value, "#") {
			continue
		}
		p[strings.TrimSpace(key)] = strings.ToLower(value)
	}
	if err := sc.Err(); err != nil {
		return nil, err
	}
	if len(p) == 0 {
		return nil, fmt.Errorf("%s holds no colours", label)
	}
	return p, nil
}

// NameFor turns a palette basename (everforest_soft) into the canonical
// theme name, the same lowercase↔title mapping Discover relies on.
func NameFor(base string) (string, bool) {
	family, variant, ok := strings.Cut(base, "_")
	if !ok || family == "" {
		return "", false
	}
	return "Lvim" + strings.ToUpper(family[:1]) + family[1:] + "_" + variant, true
}

// Current reads the active theme name from the state file (~/.theme).
func Current(stateFile string) string {
	b, err := os.ReadFile(stateFile)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(b))
}
