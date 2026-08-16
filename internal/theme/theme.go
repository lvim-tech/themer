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
	"fmt"
	"os"
	"regexp"
	"sort"
	"strings"
)

// Theme is one switchable theme, known under three spellings:
// canonical LvimEverforest_soft ($THEME, kitty/tmux/wezterm file names),
// palette everforest_soft, and the GTK build name Lvim-EverforestSoft, which
// is the directory it installs under.
type Theme struct {
	Name    string // canonical: LvimEverforest_soft
	Family  string // everforest
	Variant string // soft
	// Swatch is the colours a picker may SHOW beside the name, straight off
	// the published list. Presentation only: nothing is configured from it —
	// every target downloads a file generated in its program's own format —
	// so holding these is holding a picture of a palette, not a palette.
	Swatch []string
}

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

// nameShape is what a theme name may look like, and it is a security boundary
// rather than a matter of taste.
//
// The list is fetched over the network, and every name in it is substituted for
// {theme} and {family} into PATHS: the file a target downloads, the directory a
// tree is copied into — and copy-tree removes that directory before writing it.
// A published name of `../../../.config` therefore aims those operations
// wherever the list wants them, and the user sees nothing but a theme in the
// picker. So a name is held to one path component of ordinary characters: no
// separator, nothing that can be read as `..`, and a first character that rules
// out a name arriving as a command-line flag.
var nameShape = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]*$`)

// ValidName reports whether name may be used as a theme name. Everything
// downstream treats a name as a path component, so this is where that is made
// true.
func ValidName(name string) bool { return nameShape.MatchString(name) }

// Discover reads the theme names from the list `themer --sync` fetched.
//
// One line per canonical name. Nothing is parsed out of a palette any more:
// themer holds no colours, so the only thing left to know is which themes
// exist, and the generator that produces them publishes exactly that.
func Discover(listFile string) ([]Theme, error) {
	b, err := os.ReadFile(listFile)
	if err != nil {
		return nil, fmt.Errorf("theme list: %w", err)
	}
	var themes []Theme
	refused := 0
	for _, line := range strings.Split(string(b), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		// name first, then however many colours the generator chose to
		// publish — a list that carries none still works, it just has
		// nothing to show.
		fields := strings.Fields(line)
		if !ValidName(fields[0]) {
			// Dropped rather than fatal: one unusable name must not take the
			// other forty-seven away with it, and a name this list has no
			// business publishing is not one to hand to the filesystem.
			refused++
			continue
		}
		t := FromName(fields[0])
		t.Swatch = fields[1:]
		themes = append(themes, t)
	}
	sort.Slice(themes, func(i, j int) bool { return themes[i].Name < themes[j].Name })
	if len(themes) == 0 {
		if refused > 0 {
			return nil, fmt.Errorf("%s names %d themes, none of them usable as a name", listFile, refused)
		}
		return nil, fmt.Errorf("%s names no themes", listFile)
	}
	return themes, nil
}

// FromName splits a canonical LvimFamily_variant into its parts. A name that
// does not fit the scheme becomes its own single-theme family rather than being
// dropped: the list belongs to the generator, and themer is in no position to
// rule a name it publishes invalid.
func FromName(name string) Theme {
	t := Theme{Name: name, Family: strings.ToLower(name)}
	if after, ok := strings.CutPrefix(name, "Lvim"); ok {
		if family, variant, ok := strings.Cut(after, "_"); ok && family != "" {
			t.Family, t.Variant = strings.ToLower(family), variant
		}
	}
	return t
}

// Load resolves a theme's palette: inline values when the configuration
// defines them, the palette file otherwise.
// ByName finds a theme by its canonical name.
func ByName(themes []Theme, name string) (Theme, bool) {
	for _, t := range themes {
		if t.Name == name {
			return t, true
		}
	}
	return Theme{}, false
}

// Current reads the active theme name from the state file (~/.theme).
func Current(stateFile string) string {
	b, err := os.ReadFile(stateFile)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(b))
}
