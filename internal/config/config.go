// Package config holds themer's few knobs: where things live and which
// palette colour plays which desktop role.
//
// The role mapping exists because the compositor colours cannot be derived:
// the values found in mango's appearance.conf were hand-tuned — three of the
// eight matched the palette exactly (green, purple, red), the other five
// matched nothing in any of the 48 palettes. So the mapping ships as an
// editable default rather than pretending those five were ever computable.
package config

import (
	"fmt"
	"os"
	"path/filepath"

	toml "github.com/pelletier/go-toml/v2"
)

type Config struct {
	// PalettesDir is the generated lvim-gtk palette directory — the single
	// source both the theme list and every colour come from.
	PalettesDir string `toml:"palettes"`
	// StateFile is the file the whole ecosystem reads $THEME from.
	StateFile string `toml:"state_file"`
	// ClipackBase is clipack's base directory (bin/, configs/).
	ClipackBase string `toml:"clipack_base"`
	// PalettesRepo is the GitHub repository (owner/name) `themer --sync`
	// pulls the palettes from into themes.toml.
	PalettesRepo string `toml:"palettes_repo"`
	// Roles maps a desktop role (focus, border, urgent, …) to a palette key.
	Roles map[string]string `toml:"roles"`
	// Targets are the declarative appliers: what to detect, which lines to
	// rewrite, what to reload. The three compositors ship as defaults; a
	// config target with the same name replaces its default, a new name is
	// appended. Anything themeable by a line rewrite plus a reload command
	// belongs here, not in Go.
	Targets []Target `toml:"targets"`
	// Themes are palettes defined as values right here instead of files: a
	// new name adds a theme, a known one replaces the file palette. Only
	// what reads the palette follows automatically — appliers that need a
	// per-theme file (kitty, tmux, waybar) still fail loudly until that
	// file exists too.
	Themes []ThemeDef `toml:"themes"`
}

// ThemeDef is one inline theme: a name and its colours.
type ThemeDef struct {
	Name    string            `toml:"name"`
	Palette map[string]string `toml:"palette"`
}

// Target is one declarative applier.
type Target struct {
	Name   string   `toml:"name"`
	Detect Detect   `toml:"detect"`
	Edits  []Edit   `toml:"edit"`
	Reload []Reload `toml:"reload"`
}

// Detect gates a target: every non-empty field must pass.
type Detect struct {
	Running string `toml:"running"` // a process with exactly this name exists
	File    string `toml:"file"`    // this file exists (~ expands)
	Command string `toml:"command"` // this command is in PATH
}

// Edit rewrites lines of one file. Rule values may hold regex group
// references (${1}) and colour placeholders: {focus} resolves through the
// role mapping, {green-dark} straight from the palette, {theme} to the
// canonical theme name. Colours expand to bare rrggbb — the template owns
// the surrounding syntax (0x…ff, rgba(…), "#…").
type Edit struct {
	File  string `toml:"file"`
	Rules []Rule `toml:"rules"`
}

type Rule struct {
	Regex string `toml:"regex"`
	Value string `toml:"value"`
}

// Reload is either a command or a signal to every process of a name.
type Reload struct {
	Command []string `toml:"command"`
	Signal  string   `toml:"signal"`
	Process string   `toml:"process"`
}

// DefaultRoles is the starting point for the compositor colours. Chosen by
// hue against the hand-tuned mango values of 2026-08: focus was a yellow,
// border a green, urgent matched $red exactly, scratchpad $green exactly,
// global $purple exactly. Users tune the rest in config.toml.
func DefaultRoles() map[string]string {
	return map[string]string{
		"focus":          "yellow-dark",
		"border":         "green-dark",
		"urgent":         "red",
		"scratchpad":     "green",
		"global":         "purple",
		"maximizescreen": "green",
		"overlay":        "teal",
		"root":           "bg-dark",
	}
}

// DefaultTargets covers the three compositors this machine alternates
// between, plus waybar. Every regex anchors at line start so the
// commented-out variants the configs carry (# rootcolor=…,
// # col.active_border=…) stay untouched.
func DefaultTargets() []Target {
	return []Target{
		{
			// waybar needs no theme files at all: its theme is thirteen
			// @define-color lines, so they are written straight from the
			// palette into a permanent colors.css. current.css (the --style
			// the compositors launch waybar with, since it freezes that path
			// at startup) just imports colors.css and the colour-free
			// structure.css, and never changes again. SIGUSR2 makes a running
			// bar re-read the chain; with no bar running the edit still
			// counts and the next launch picks it up.
			Name:   "waybar",
			Detect: Detect{File: "~/.config/waybar/colors.css"},
			Edits: []Edit{{
				File: "~/.config/waybar/colors.css",
				Rules: []Rule{
					{Regex: `^@define-color bg .*;`, Value: "@define-color bg #{bg};"},
					{Regex: `^@define-color bg_dark .*;`, Value: "@define-color bg_dark #{bg-dark};"},
					{Regex: `^@define-color fg .*;`, Value: "@define-color fg #{fg};"},
					{Regex: `^@define-color fg_light .*;`, Value: "@define-color fg_light #{fg-light};"},
					{Regex: `^@define-color fg_soft_dark .*;`, Value: "@define-color fg_soft_dark #{fg-soft-dark};"},
					{Regex: `^@define-color red .*;`, Value: "@define-color red #{red};"},
					{Regex: `^@define-color orange .*;`, Value: "@define-color orange #{orange};"},
					{Regex: `^@define-color yellow .*;`, Value: "@define-color yellow #{yellow};"},
					{Regex: `^@define-color green .*;`, Value: "@define-color green #{green};"},
					{Regex: `^@define-color teal .*;`, Value: "@define-color teal #{teal};"},
					{Regex: `^@define-color cyan .*;`, Value: "@define-color cyan #{cyan};"},
					{Regex: `^@define-color cyan_dark .*;`, Value: "@define-color cyan_dark #{cyan-dark};"},
					{Regex: `^@define-color blue .*;`, Value: "@define-color blue #{blue};"},
				},
			}},
			Reload: []Reload{{Signal: "USR2", Process: "waybar"}},
		},
		{
			// alacritty live-reloads its config, so there is no reload step —
			// rewriting the import is the whole job. The rule keeps whatever
			// directory the import already points at (the colorscheme repo's
			// extras here, same idea as kitty's include) and swaps only the
			// file name, so moving the themes means touching the config once,
			// not this rule.
			Name:   "alacritty",
			Detect: Detect{File: "~/.config/alacritty/alacritty.toml"},
			Edits: []Edit{{
				File: "~/.config/alacritty/alacritty.toml",
				Rules: []Rule{
					{Regex: `^import = \["(.*/)?[^/"]*"\]`, Value: `import = ["${1}{theme}.toml"]`},
				},
			}},
		},
		{
			// ghostty picks its theme by NAME from ~/.config/ghostty/themes
			// and reloads the whole config on SIGUSR2. Detection is by file:
			// with no window open the edit still lands and the next launch
			// reads it, same reasoning as waybar. The ^-anchor spares the
			// commented guidance lines the config carries.
			Name:   "ghostty",
			Detect: Detect{File: "~/.config/ghostty/config"},
			Edits: []Edit{{
				File: "~/.config/ghostty/config",
				Rules: []Rule{
					{Regex: `^theme\s*=.*$`, Value: "theme = {theme}"},
				},
			}},
			Reload: []Reload{{Signal: "USR2", Process: "ghostty"}},
		},
		{
			// k9s picks a skin BY NAME from ~/.config/k9s/skins, so the
			// switch is one word in its config. The rule rewrites every
			// `skin:` line, which is deliberate: k9s allows a per-context
			// skin, and leaving those pinned to the old palette is exactly
			// the half-themed result this is meant to prevent. No reload —
			// k9s re-reads the skin itself while running.
			Name:   "k9s",
			Detect: Detect{File: "~/.config/k9s/config.yaml"},
			Edits: []Edit{{
				File: "~/.config/k9s/config.yaml",
				Rules: []Rule{
					{Regex: `^(\s*)skin:\s*.*$`, Value: "${1}skin: {theme}"},
				},
			}},
		},
		{
			// btop names its theme in btop.conf and reads that file only at
			// startup, so there is nothing to signal: the next launch comes
			// up themed.
			Name:   "btop",
			Detect: Detect{File: "~/.config/btop/btop.conf"},
			Edits: []Edit{{
				File: "~/.config/btop/btop.conf",
				Rules: []Rule{
					{Regex: `^color_theme\s*=.*$`, Value: `color_theme = "{theme}"`},
				},
			}},
		},
		{
			Name:   "mango",
			Detect: Detect{Running: "mango", File: "~/.config/mango/appearance.conf"},
			Edits: []Edit{{
				File: "~/.config/mango/appearance.conf",
				Rules: []Rule{
					{Regex: `^rootcolor=\S+`, Value: "rootcolor=0x{root}ff"},
					{Regex: `^bordercolor=\S+`, Value: "bordercolor=0x{border}ff"},
					{Regex: `^focuscolor=\S+`, Value: "focuscolor=0x{focus}ff"},
					{Regex: `^maximizescreencolor=\S+`, Value: "maximizescreencolor=0x{maximizescreen}ff"},
					{Regex: `^urgentcolor=\S+`, Value: "urgentcolor=0x{urgent}ff"},
					{Regex: `^scratchpadcolor=\S+`, Value: "scratchpadcolor=0x{scratchpad}ff"},
					{Regex: `^globalcolor=\S+`, Value: "globalcolor=0x{global}ff"},
					{Regex: `^overlaycolor=\S+`, Value: "overlaycolor=0x{overlay}ff"},
				},
			}},
			Reload: []Reload{{Command: []string{"mmsg", "-d", "reload_config"}}},
		},
		{
			Name:   "hyprland",
			Detect: Detect{Running: "Hyprland", File: "~/.config/hypr/appearance.conf"},
			Edits: []Edit{{
				File: "~/.config/hypr/appearance.conf",
				Rules: []Rule{
					{Regex: `^(\s*)col\.active_border\s*=.*$`, Value: "${1}col.active_border = rgba({focus}ff)"},
					{Regex: `^(\s*)col\.inactive_border\s*=.*$`, Value: "${1}col.inactive_border = rgba({border}ff)"},
				},
			}},
			Reload: []Reload{{Command: []string{"hyprctl", "reload"}}},
		},
		{
			// inactive-color begins its own line, so the ^-anchored
			// active-color rule cannot swallow it despite the substring.
			// inactive runs FIRST: were active's anchor ever relaxed, it
			// would then corrupt the already-written inactive value and the
			// test catches it — in the other order the damage is repaired
			// by the later rule and stays invisible.
			// The urgent template keeps the a6 alpha the config always had.
			Name:   "niri",
			Detect: Detect{Running: "niri", File: "~/.config/niri/incl/04-layout.kdl"},
			Edits: []Edit{{
				File: "~/.config/niri/incl/04-layout.kdl",
				Rules: []Rule{
					{Regex: `^(\s*)inactive-color\s+"[^"]*"`, Value: `${1}inactive-color "#{border}"`},
					{Regex: `^(\s*)active-color\s+"[^"]*"`, Value: `${1}active-color "#{focus}"`},
					{Regex: `^(\s*)urgent-color\s+"[^"]*"`, Value: `${1}urgent-color "#{urgent}a6"`},
				},
			}},
			// niri watches only the top-level config.kdl, and the colours
			// live in an included file — ask for the reload explicitly.
			Reload: []Reload{{Command: []string{"niri", "msg", "action", "load-config-file"}}},
		},
	}
}

func Default() Config {
	home, _ := os.UserHomeDir()
	return Config{
		PalettesDir:  filepath.Join(home, "lvim-tech", "lvim-gtk", "palettes"),
		StateFile:    filepath.Join(home, ".theme"),
		ClipackBase:  filepath.Join(home, "clipack"),
		PalettesRepo: "lvim-tech/lvim-gtk",
		Roles:        DefaultRoles(),
		Targets:      DefaultTargets(),
	}
}

// Dir is where themer's own files live.
func Dir() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".config", "themer")
}

// ThemesFile is the sync target: the generated snapshot of the palettes,
// kept apart from the hand-written config.toml so a sync can rewrite it
// wholesale without ever touching a manual edit.
func ThemesFile() string { return filepath.Join(Dir(), "themes.toml") }

// Load reads the synced themes.toml, then ~/.config/themer/config.toml over
// the defaults. Missing files are the normal case, not errors. The order
// matters: config.toml's themes land after the synced ones, so the merge
// downstream lets a hand-written definition override a synced palette.
func Load() (Config, error) {
	cfg := Default()

	var synced Config
	if b, err := os.ReadFile(ThemesFile()); err == nil {
		if err := toml.Unmarshal(b, &synced); err != nil {
			return cfg, fmt.Errorf("themes.toml: %w", err)
		}
		cfg.Themes = synced.Themes
	}

	b, err := os.ReadFile(filepath.Join(Dir(), "config.toml"))
	if err != nil {
		return cfg, nil
	}
	// Unmarshal over the defaults: an absent key keeps its default, and a
	// partial roles: block replaces only the keys it names.
	fileRoles := map[string]string{}
	var overlay Config
	if err := toml.Unmarshal(b, &overlay); err != nil {
		return cfg, err
	}
	fileRoles = overlay.Roles
	overlay.Roles = nil
	merge(&cfg, overlay)
	for role, key := range fileRoles {
		cfg.Roles[role] = key
	}
	// Targets merge by name: redefining "mango" replaces the built-in,
	// a new name extends the list. There is no way to delete a built-in
	// short of redefining it with a detect rule that never passes — its
	// Detect already skips it on any machine that does not run it.
	for _, t := range overlay.Targets {
		replaced := false
		for i := range cfg.Targets {
			if cfg.Targets[i].Name == t.Name {
				cfg.Targets[i] = t
				replaced = true
				break
			}
		}
		if !replaced {
			cfg.Targets = append(cfg.Targets, t)
		}
	}
	cfg.Themes = append(cfg.Themes, overlay.Themes...) // after the synced ones: manual wins the merge
	return cfg, nil
}

func merge(dst *Config, src Config) {
	if src.PalettesDir != "" {
		dst.PalettesDir = src.PalettesDir
	}
	if src.StateFile != "" {
		dst.StateFile = src.StateFile
	}
	if src.ClipackBase != "" {
		dst.ClipackBase = src.ClipackBase
	}
	if src.PalettesRepo != "" {
		dst.PalettesRepo = src.PalettesRepo
	}
}
