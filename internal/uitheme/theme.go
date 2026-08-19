// Package uitheme describes how themer's OWN interface looks — its chrome, not
// the palettes of the themes it switches between. It is plain data: this package
// never imports a rendering library, so the description stays independent of the
// terminal front-end that consumes it (see tui.NewStyles).
//
// It mirrors clipack's cnfg/theme.go so the whole lvim-tech family reads the
// same nine-role palette from the same shape of file. themer's own config.toml
// selects the theme by name (and may override border/icons); the palette itself
// lives in a YAML file under ~/.config/themer/themes/{name}.yaml, or in one of
// the built-in presets.
//
//	[theme]
//	name = "default"   # a built-in, or themes/<name>.yaml
//	border = "rounded"
//	icons = "unicode"
//
// Anything left out falls back to the base, so selecting a theme never means
// restating a palette.
package uitheme

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

// Theme describes the interface's look. In config.toml it is the [theme] table,
// which selects a base by name and may override the structural knobs; the
// palette is carried by the YAML theme file the name points at, so Colors is not
// read from TOML.
type Theme struct {
	Name   string   `toml:"name,omitempty" yaml:"name,omitempty"`
	Border string   `toml:"border,omitempty" yaml:"border,omitempty"`
	Icons  string   `toml:"icons,omitempty" yaml:"icons,omitempty"`
	Colors ColorSet `toml:"-" yaml:"colors,omitempty"`
}

// Color is one palette entry. It adapts to the terminal background: Light on a
// light one, Dark on a dark one.
//
// In YAML it accepts either form:
//
//	accent: "#81a1c1"                           # both backgrounds
//	accent: {light: "#2f5d9e", dark: "#81a1c1"} # per background
type Color struct {
	Light string `yaml:"light,omitempty"`
	Dark  string `yaml:"dark,omitempty"`
}

// IsZero reports whether the colour was left unset.
func (c Color) IsZero() bool {
	return c.Light == "" && c.Dark == ""
}

// String renders the colour the way it is written in YAML.
func (c Color) String() string {
	if c.Light == c.Dark {
		return c.Light
	}
	return fmt.Sprintf("light:%s dark:%s", c.Light, c.Dark)
}

// UnmarshalYAML accepts both the scalar and the mapping form.
func (c *Color) UnmarshalYAML(node *yaml.Node) error {
	if node.Kind == yaml.ScalarNode {
		var value string
		if err := node.Decode(&value); err != nil {
			return err
		}
		c.Light, c.Dark = value, value
		return nil
	}

	// A distinct type avoids recursing back into this method.
	type rawColor Color
	var raw rawColor
	if err := node.Decode(&raw); err != nil {
		return err
	}

	*c = Color(raw)
	// A half-specified colour applies to both backgrounds.
	if c.Light == "" {
		c.Light = c.Dark
	}
	if c.Dark == "" {
		c.Dark = c.Light
	}
	return nil
}

// MarshalYAML writes the short form when both backgrounds share a value.
func (c Color) MarshalYAML() (any, error) {
	if c.Light == c.Dark {
		return c.Light, nil
	}
	return map[string]string{"light": c.Light, "dark": c.Dark}, nil
}

// ColorSet is the nine-role palette a theme defines. The roles are the family's
// unified vocabulary: the same nine names clipack, and every generated theme,
// key on.
type ColorSet struct {
	// Accent drives the selection cursor, the title badge and focused borders.
	Accent Color `yaml:"accent,omitempty"`
	// AccentAlt marks section headings, the active tab and step markers.
	AccentAlt Color `yaml:"accent_alt,omitempty"`
	// Text is the default foreground.
	Text Color `yaml:"text,omitempty"`
	// Muted is for secondary information: hints, field labels, skipped rows.
	Muted Color `yaml:"muted,omitempty"`
	// Subtle is the colour of unfocused pane borders.
	Subtle Color `yaml:"subtle,omitempty"`
	// Success marks the current theme and completed operations.
	Success Color `yaml:"success,omitempty"`
	// Warning marks non-fatal problems.
	Warning Color `yaml:"warning,omitempty"`
	// Error marks failures.
	Error Color `yaml:"error,omitempty"`
	// TitleFg is the foreground of the title badge, drawn on Accent.
	TitleFg Color `yaml:"title_fg,omitempty"`
}

// Icons is the glyph set used for status markers.
type Icons struct {
	Cursor    string
	Current   string
	Step      string
	Output    string
	Warn      string
	Error     string
	Done      string
	Skipped   string
	Pending   string
	Running   string
	Bullet    string
	Separator string
	// PageActive and PageInactive are the pagination dots under the list. They
	// carry their own trailing space: the paginator concatenates them with no
	// separator, and adjacent circles are hard to count without one.
	PageActive   string
	PageInactive string
}

// Theme defaults and the accepted values for each knob.
const (
	DefaultThemeName = "default"
	DefaultBorder    = "normal"
	DefaultIcons     = "unicode"

	IconsUnicode = "unicode"
	IconsASCII   = "ascii"

	// ThemeFileExt is the extension of a theme file in the themes directory.
	ThemeFileExt = ".yaml"
)

// unicodeIcons is the default glyph set.
var unicodeIcons = Icons{
	Cursor:       "│ ",
	Current:      "● ",
	Step:         "▶",
	Output:       "│",
	Warn:         "!",
	Error:        "✗",
	Done:         "✓",
	Skipped:      "○",
	Pending:      "·",
	Running:      "…",
	Bullet:       "•",
	Separator:    "·",
	PageActive:   "● ",
	PageInactive: "○ ",
}

// asciiIcons keeps themer readable on terminals or fonts that cannot render the
// box-drawing and check glyphs.
var asciiIcons = Icons{
	Cursor:       "> ",
	Current:      "* ",
	Step:         ">",
	Output:       "|",
	Warn:         "!",
	Error:        "x",
	Done:         "+",
	Skipped:      "-",
	Pending:      ".",
	Running:      "~",
	Bullet:       "-",
	Separator:    "-",
	PageActive:   "# ",
	PageInactive: ". ",
}

// Borders lists the accepted border styles.
var Borders = []string{"rounded", "normal", "thick", "double", "hidden", "none"}

// IconSets lists the accepted icon sets.
var IconSets = []string{IconsUnicode, IconsASCII}

// builtinThemes holds the presets, keyed by name. Every preset is complete: an
// override always merges on top of a fully populated base.
//
// The default palette is clipack's, so themer's own chrome matches the rest of
// the family out of the box: no magenta and no yellow — Nord's blue and orange
// in their place — the same rule the generated themes follow.
var builtinThemes = map[string]Theme{
	"default": {
		Name:   "default",
		Border: DefaultBorder,
		Icons:  DefaultIcons,
		Colors: ColorSet{
			Accent:    Color{Light: "#2f5d9e", Dark: "#81a1c1"},
			AccentAlt: Color{Light: "#a2542b", Dark: "#d08770"},
			Text:      Color{Light: "#1c1c1c", Dark: "#e5e9f0"},
			Muted:     Color{Light: "#6b6b6b", Dark: "#7f8896"},
			Subtle:    Color{Light: "#c8c8c8", Dark: "#3b4252"},
			Success:   Color{Light: "#217a3d", Dark: "#a3be8c"},
			// The SAME red as Error, on purpose: ruling out magenta and yellow
			// leaves four hues for five roles, so warning and error share one and
			// are told apart by weight — error is bold, warning is not (NewStyles).
			Warning: Color{Light: "#b3261e", Dark: "#bf616a"},
			Error:   Color{Light: "#b3261e", Dark: "#bf616a"},
			TitleFg: Color{Light: "#ffffff", Dark: "#1c1c1c"},
		},
	},
	"mono": {
		// No colour at all: everything is drawn with the terminal's own
		// foreground, and emphasis comes from weight and dimming.
		Name:   "mono",
		Border: "normal",
		Icons:  IconsASCII,
		Colors: ColorSet{},
	},
}

// BuiltinThemeNames lists the built-in presets in alphabetical order.
func BuiltinThemeNames() []string {
	names := make([]string, 0, len(builtinThemes))
	for name := range builtinThemes {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// BuiltinTheme returns a preset by name.
func BuiltinTheme(name string) (Theme, bool) {
	theme, ok := builtinThemes[name]
	return theme, ok
}

// ThemesDir returns the directory holding themer's own theme files:
// ~/.config/themer/themes. It is computed here rather than imported from the
// config package to keep that package free to hold a Theme without a cycle.
func ThemesDir() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".config", "themer", "themes")
}

// CustomThemeNames lists the theme files installed in the themes directory,
// without their extension. A missing directory simply means none are installed.
func CustomThemeNames() ([]string, error) {
	entries, err := os.ReadDir(ThemesDir())
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}

	var names []string
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		if ext := filepath.Ext(name); ext == ThemeFileExt || ext == ".yml" {
			names = append(names, strings.TrimSuffix(name, ext))
		}
	}
	sort.Strings(names)
	return names, nil
}

// LoadThemeFile reads a theme from the themes directory.
func LoadThemeFile(name string) (Theme, error) {
	dir := ThemesDir()

	// A theme name is a file name, never a path: refuse anything that would
	// escape the themes directory.
	if name != filepath.Base(name) || name == "." || name == ".." {
		return Theme{}, fmt.Errorf("invalid theme name %q", name)
	}

	for _, ext := range []string{ThemeFileExt, ".yml"} {
		data, err := os.ReadFile(filepath.Join(dir, name+ext))
		if err != nil {
			continue
		}

		var theme Theme
		if err := yaml.Unmarshal(data, &theme); err != nil {
			return Theme{}, fmt.Errorf("parsing theme %s%s: %w", name, ext, err)
		}
		theme.Name = name
		return theme, nil
	}

	return Theme{}, fmt.Errorf("theme %q not found: no built-in of that name and no %s in %s",
		name, name+ThemeFileExt, dir)
}

// ResolveTheme produces the theme the interface should actually render with.
//
// The base is the built-in preset named by t.Name, or the matching file in the
// themes directory. Whatever t itself sets is then layered on top, so a border
// or icon set can be overridden in config.toml without touching the palette. A
// base that is neither built-in nor installed is an error; the caller decides
// whether that is fatal.
func ResolveTheme(t Theme) (Theme, error) {
	name := t.Name
	if name == "" {
		name = DefaultThemeName
	}

	base, ok := builtinThemes[name]
	if !ok {
		loaded, err := LoadThemeFile(name)
		if err != nil {
			return DefaultTheme(), err
		}
		// A file may itself omit the structural knobs.
		base = builtinThemes[DefaultThemeName].merge(loaded)
	}

	resolved := base.merge(Theme{Border: t.Border, Icons: t.Icons, Colors: t.Colors})
	resolved.Name = name
	return resolved, nil
}

// DefaultTheme is the fully populated fallback.
func DefaultTheme() Theme {
	return builtinThemes[DefaultThemeName]
}

// merge layers the non-empty values of over on top of t.
func (t Theme) merge(over Theme) Theme {
	out := t
	if over.Name != "" {
		out.Name = over.Name
	}
	if over.Border != "" {
		out.Border = over.Border
	}
	if over.Icons != "" {
		out.Icons = over.Icons
	}
	out.Colors = t.Colors.merge(over.Colors)
	return out
}

// IconSet returns the glyphs for the theme.
func (t Theme) IconSet() Icons {
	if t.Icons == IconsASCII {
		return asciiIcons
	}
	return unicodeIcons
}

// merge layers non-empty overrides on top of a base palette.
func (c ColorSet) merge(over ColorSet) ColorSet {
	pick := func(override, base Color) Color {
		if override.IsZero() {
			return base
		}
		return override
	}

	return ColorSet{
		Accent:    pick(over.Accent, c.Accent),
		AccentAlt: pick(over.AccentAlt, c.AccentAlt),
		Text:      pick(over.Text, c.Text),
		Muted:     pick(over.Muted, c.Muted),
		Subtle:    pick(over.Subtle, c.Subtle),
		Success:   pick(over.Success, c.Success),
		Warning:   pick(over.Warning, c.Warning),
		Error:     pick(over.Error, c.Error),
		TitleFg:   pick(over.TitleFg, c.TitleFg),
	}
}

// NamedColor pairs a palette entry with the key it has in a theme file.
type NamedColor struct {
	Key   string
	Color Color
}

// Named returns the palette keyed by the names used in a theme file.
func (c ColorSet) Named() []NamedColor {
	return []NamedColor{
		{"accent", c.Accent},
		{"accent_alt", c.AccentAlt},
		{"text", c.Text},
		{"muted", c.Muted},
		{"subtle", c.Subtle},
		{"success", c.Success},
		{"warning", c.Warning},
		{"error", c.Error},
		{"title_fg", c.TitleFg},
	}
}

// Validate rejects unusable values, naming the alternatives. The theme name is
// deliberately not checked here: resolving it needs the filesystem, and loading
// the config has to keep working when a theme file is temporarily missing.
func (t Theme) Validate() error {
	if t.Border != "" && !contains(Borders, t.Border) {
		return fmt.Errorf("unknown border %q (available: %s)",
			t.Border, strings.Join(Borders, ", "))
	}
	if t.Icons != "" && !contains(IconSets, t.Icons) {
		return fmt.Errorf("unknown icon set %q (available: %s)",
			t.Icons, strings.Join(IconSets, ", "))
	}

	for _, entry := range t.Colors.Named() {
		if err := validateColor(entry.Key, entry.Color); err != nil {
			return err
		}
	}

	return nil
}

// validateColor accepts a hex value or an ANSI palette index, which is what the
// rendering layer understands.
func validateColor(key string, c Color) error {
	for _, value := range []string{c.Light, c.Dark} {
		if value == "" {
			continue
		}
		if !isHexColor(value) && !isANSIIndex(value) {
			return fmt.Errorf("colour %q: %q is not a hex value like \"#b48ead\" or an ANSI index like \"5\"", key, value)
		}
	}
	return nil
}

// isHexColor reports whether value is #rgb or #rrggbb.
func isHexColor(value string) bool {
	if !strings.HasPrefix(value, "#") {
		return false
	}
	digits := value[1:]
	if len(digits) != 3 && len(digits) != 6 {
		return false
	}
	for _, r := range digits {
		isDigit := r >= '0' && r <= '9'
		isLower := r >= 'a' && r <= 'f'
		isUpper := r >= 'A' && r <= 'F'
		if !isDigit && !isLower && !isUpper {
			return false
		}
	}
	return true
}

// isANSIIndex reports whether value is a 0-255 palette index.
func isANSIIndex(value string) bool {
	if value == "" || len(value) > 3 {
		return false
	}
	n := 0
	for _, r := range value {
		if r < '0' || r > '9' {
			return false
		}
		n = n*10 + int(r-'0')
	}
	return n <= 255
}

func contains(list []string, value string) bool {
	for _, item := range list {
		if item == value {
			return true
		}
	}
	return false
}
