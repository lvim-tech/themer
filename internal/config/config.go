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
	// Roles maps a desktop role (focus, border, urgent, …) to a palette key.
	Roles map[string]string `toml:"roles"`
	// Targets are the declarative appliers: what to detect, which lines to
	// rewrite, what to reload. The three compositors ship as defaults; a
	// config target with the same name replaces its default, a new name is
	// appended. Anything themeable by a line rewrite plus a reload command
	// belongs here, not in Go.
	Targets []Target `toml:"targets"`
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
			// waybar freezes its --style path at launch, so all three
			// compositors start it with a permanent current.css whose
			// single @import this rule rewrites; SIGUSR2 then makes the
			// running bar re-read it. Detection is by file, not process:
			// with no bar running the edit still counts — the next launch
			// picks it up — and the signal quietly reaches nobody.
			Name:   "waybar",
			Detect: Detect{File: "~/.config/waybar/current.css"},
			Edits: []Edit{{
				File: "~/.config/waybar/current.css",
				Rules: []Rule{
					{Regex: `^@import ".*";`, Value: `@import "styles/{theme}.css";`},
				},
			}},
			Reload: []Reload{{Signal: "USR2", Process: "waybar"}},
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
		PalettesDir: filepath.Join(home, "lvim-tech", "lvim-gtk", "palettes"),
		StateFile:   filepath.Join(home, ".theme"),
		ClipackBase: filepath.Join(home, "clipack"),
		Roles:       DefaultRoles(),
		Targets:     DefaultTargets(),
	}
}

// Load reads ~/.config/themer/config.toml over the defaults. A missing file
// is the normal case, not an error.
func Load() (Config, error) {
	cfg := Default()
	home, err := os.UserHomeDir()
	if err != nil {
		return cfg, nil
	}
	b, err := os.ReadFile(filepath.Join(home, ".config", "themer", "config.toml"))
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
}
