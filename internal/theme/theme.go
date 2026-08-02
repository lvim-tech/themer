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
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// Theme is one switchable theme, known under three spellings:
// canonical LvimEverforest_soft ($THEME, kitty/tmux/wezterm file names),
// palette everforest_soft (lvim-gtk palettes and the lvim-gtk-select
// argument), and the GTK build name Lvim-EverforestSoft, which only
// lvim-gtk-select needs and derives itself.
type Theme struct {
	Name    string // canonical: LvimEverforest_soft
	Family  string // everforest
	Variant string // soft
}

// GTKArg is what lvim-gtk-select takes on its command line.
func (t Theme) GTKArg() string { return t.Family + "_" + t.Variant }

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
		family, variant, ok := strings.Cut(base, "_")
		if !ok {
			continue
		}
		themes = append(themes, Theme{
			Name:    "Lvim" + strings.ToUpper(family[:1]) + family[1:] + "_" + variant,
			Family:  family,
			Variant: variant,
		})
	}
	sort.Slice(themes, func(i, j int) bool { return themes[i].Name < themes[j].Name })
	if len(themes) == 0 {
		return nil, fmt.Errorf("no *.scss palettes in %s", palettesDir)
	}
	return themes, nil
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

// LoadPalette parses the `$key: #hex;` lines of a palette file. Everything
// else in the file — comments, $style — is skipped rather than rejected,
// because the file is generated and its prose changes more often than its
// colour lines.
func LoadPalette(path string) (Palette, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	p := Palette{}
	sc := bufio.NewScanner(f)
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
		return nil, fmt.Errorf("%s holds no colours", path)
	}
	return p, nil
}

// Current reads the active theme name from the state file (~/.theme).
func Current(stateFile string) string {
	b, err := os.ReadFile(stateFile)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(b))
}
