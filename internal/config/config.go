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
	"sort"
	"strings"

	toml "github.com/pelletier/go-toml/v2"
)

type Config struct {
	// StateFile is the file this setup reads $THEME from — themer marks the
	// current theme by reading it. WRITING it is a target like any other, so
	// there is no default here: another setup may keep it elsewhere, or have
	// none, and themer would then simply not know which theme is active.
	StateFile string `toml:"state_file"`
	// PathDir is prepended to PATH for everything themer runs.
	//
	// Started from a shell the tools are already there; started from a
	// COMPOSITOR keybind — which is how themer is normally reached — the
	// environment carries only the login PATH, and tools installed outside it
	// were simply not found. The appliers then failed on a machine where the
	// same switch worked perfectly from a terminal.
	PathDir string `toml:"path_dir"`
	// ThemesURL is the plain list of theme names, one per line, that
	// `themer --sync` fetches. themer holds no colours of its own: every
	// target now downloads a file generated for it, so all that is left to
	// know is WHICH themes exist. There is no default — the list belongs to
	// whoever generates the themes, not to this program.
	ThemesURL string `toml:"themes_url"`
	// Targets are the declarative appliers: what to detect, which lines to
	// rewrite, what to reload. The three compositors ship as defaults; a
	// config target with the same name replaces its default, a new name is
	// appended. Anything themeable by a line rewrite plus a reload command
	// belongs here, not in Go.
	Targets []Target `toml:"targets"`
}

// Target is one declarative applier: what to detect, then an ordered list of
// operations to carry out.
//
// Edits and Reload are the older, narrower spelling. They are desugared into
// Ops at load, so there is ONE execution path — a second one would drift, and
// the two would disagree about ordering the first time a target needed a
// command between two rewrites.
type Target struct {
	Name   string   `toml:"name"`
	Detect Detect   `toml:"detect"`
	Ops    []Op     `toml:"op,omitempty"`
	Edits  []Edit   `toml:"edit,omitempty"`
	Reload []Reload `toml:"reload,omitempty"`
}

// Operation kinds. Everything a target can do is one of these; nothing about
// a particular program belongs in Go.
const (
	OpRewrite  = "rewrite"   // regex replacement over the lines of a file
	OpFetch    = "fetch"     // download source into target, both templated
	OpLink     = "link"      // symlink target → source, both templated
	OpWrite    = "write"     // write a file from a templated string
	OpCommand  = "command"   // run a command, optionally with extra environment
	OpJSONSet  = "json-set"  // set one key in a JSON document, leaving the rest
	OpCopyTree = "copy-tree" // copy a directory tree
	OpSignal   = "signal"    // send a signal to every process of a name
)

// Op is one step of a target. Which fields matter depends on Kind; Validate
// rejects the combinations that do not.
//
// Every path and value is a template: {theme} is the canonical theme name,
// {focus} resolves through the role mapping, {green-dark} straight from the
// palette.
type Op struct {
	Kind string `toml:"kind"`

	File    string   `toml:"file,omitempty"`    // rewrite, write, json-set
	Rules   []Rule   `toml:"rules,omitempty"`   // rewrite
	Content string   `toml:"content,omitempty"` // write
	Key     string   `toml:"key,omitempty"`     // json-set
	Value   string   `toml:"value,omitempty"`   // json-set
	Source  string   `toml:"source,omitempty"`  // link, copy-tree
	Target  string   `toml:"target,omitempty"`  // link, copy-tree
	Command []string `toml:"command,omitempty"` // command
	Env     []string `toml:"env,omitempty"`     // command, as KEY=value
	Signal  string   `toml:"signal,omitempty"`  // signal
	Process string   `toml:"process,omitempty"` // signal
}

// Validate reports an operation that cannot run, naming the target — a
// missing field is a configuration bug, and finding it at load beats finding
// it halfway through a theme switch with the desktop already half changed.
func (o Op) Validate(target string) error {
	need := func(ok bool, what string) error {
		if ok {
			return nil
		}
		return fmt.Errorf("target %q: %s operation needs %s", target, o.Kind, what)
	}
	switch o.Kind {
	case OpRewrite:
		if err := need(o.File != "", "file"); err != nil {
			return err
		}
		return need(len(o.Rules) > 0, "rules")
	case OpWrite:
		return need(o.File != "", "file")
	case OpJSONSet:
		if err := need(o.File != "", "file"); err != nil {
			return err
		}
		return need(o.Key != "", "key")
	case OpFetch, OpLink, OpCopyTree:
		if err := need(o.Source != "", "source"); err != nil {
			return err
		}
		return need(o.Target != "", "target")
	case OpCommand:
		return need(len(o.Command) > 0, "command")
	case OpSignal:
		if err := need(o.Signal != "", "signal"); err != nil {
			return err
		}
		return need(o.Process != "", "process")
	case "":
		return fmt.Errorf("target %q: operation declares no kind", target)
	default:
		return fmt.Errorf("target %q: unknown operation kind %q", target, o.Kind)
	}
}

// Desugar folds the Edits and Reload spellings into Ops, in the order they
// have always run: every edit, then every reload.
func (t Target) Desugar() Target {
	if len(t.Edits) == 0 && len(t.Reload) == 0 {
		return t
	}
	ops := append([]Op(nil), t.Ops...)
	for _, e := range t.Edits {
		ops = append(ops, Op{Kind: OpRewrite, File: e.File, Rules: e.Rules})
	}
	for _, r := range t.Reload {
		switch {
		case len(r.Command) > 0:
			ops = append(ops, Op{Kind: OpCommand, Command: r.Command})
		case r.Signal != "" && r.Process != "":
			ops = append(ops, Op{Kind: OpSignal, Signal: r.Signal, Process: r.Process})
		}
	}
	t.Ops = ops
	t.Edits, t.Reload = nil, nil
	return t
}

// Detect gates a target: every non-empty field must pass.
type Detect struct {
	Running string `toml:"running,omitempty"` // a process with exactly this name exists
	File    string `toml:"file,omitempty"`    // this file exists (~ expands)
	Command string `toml:"command,omitempty"` // this command is in PATH
	// Match is a regex File must contain. It exists because a configuration
	// can be legitimately out of scope rather than absent — wezterm.lua
	// running on `colors = custom` with color_scheme commented out is a
	// hand-built palette, and overriding it from a switcher would be exactly
	// the wrong kind of help. Without this the rewrite would fail instead,
	// turning a deliberate choice into an error.
	Match string `toml:"match,omitempty"`
	// Reason replaces the generic skip message when Match does not hit.
	Reason string `toml:"reason,omitempty"`
	// Always runs the target unconditionally. A few have nothing to detect:
	// writing the state file every other tool reads is the point, and its
	// absence is a reason to create it rather than to skip.
	Always bool `toml:"always,omitempty"`
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

// DefaultTargets is deliberately empty.
//
// themer knows no programs. Which file a tool keeps its theme in, which line
// names it and what to reload are facts about somebody's software, and a
// built-in list made one person's desktop look like part of the program. The
// definitions live in ~/.config/themer/targets/, one file per tool, and are
// carried between machines by whatever manages that directory.
func DefaultTargets() []Target { return nil }

// Default carries only what is true of any machine: where the state file and
// clipack live. No themes URL and no targets — those name somebody's generator
// and somebody's programs, and a built-in would make one person's setup look
// like part of themer.
func Default() Config {
	home, _ := os.UserHomeDir()
	_ = home
	return Config{Targets: DefaultTargets()}
}

// Dir is where themer's own files live.
func Dir() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".config", "themer")
}

// ThemesFile is what `themer --sync` writes: the theme names, one per line.
//
// Under the data directory rather than beside config.toml, because it is
// downloaded rather than written — the same reason configer tracks a program's
// config and never its generated output.
func ThemesFile() string {
	if dir := os.Getenv("XDG_DATA_HOME"); dir != "" {
		return filepath.Join(dir, "themer", "themes.txt")
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".local", "share", "themer", "themes.txt")
}

// TargetsDir holds the tool definitions, one file per program: where it keeps
// its theme, which line names it, what to reload.
//
// A directory rather than one file, for the same reason clipack's registry is
// packages/<category>/<name>.yaml and configer's source is src/<app>/: adding a
// program is a new file, removing one is a deletion, and a diff names the tool
// it touched. Forty-five tools in a single document would answer none of that.
//
// Kept apart from config.toml because the two answer different questions — a
// definition says how a program works and is the same on every machine that
// runs it; config.toml says what this machine does differently.
func TargetsDir() string { return filepath.Join(Dir(), "targets") }

// Load reads the synced themes.toml, then ~/.config/themer/config.toml over
// the defaults. Missing files are the normal case, not errors. The order
// matters: config.toml's themes land after the synced ones, so the merge
// downstream lets a hand-written definition override a synced palette.
func Load() (Config, error) {
	cfg := Default()

	// The tool definitions come before config.toml, so a machine-specific
	// override still wins by name.
	defs, err := loadTargetsDir()
	if err != nil {
		return cfg, err
	}
	cfg.Targets = mergeTargets(cfg.Targets, defs)

	b, err := os.ReadFile(filepath.Join(Dir(), "config.toml"))
	if err != nil {
		return finish(cfg)
	}
	// Unmarshal over the defaults: an absent key keeps its default.
	var overlay Config
	if err := toml.Unmarshal(b, &overlay); err != nil {
		return cfg, err
	}
	merge(&cfg, overlay)
	cfg.Targets = mergeTargets(cfg.Targets, overlay.Targets)
	return finish(cfg)
}

// loadTargetsDir reads every *.toml in the targets directory, in filename
// order so two files defining the same name resolve the same way on every
// machine. An absent directory is the normal case on a fresh install, not an
// error; a file that will not parse is named, because a definition silently
// skipped is a program that silently stops being themed.
func loadTargetsDir() ([]Target, error) {
	entries, err := os.ReadDir(TargetsDir())
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".toml") {
			names = append(names, e.Name())
		}
	}
	sort.Strings(names)

	var out []Target
	for _, name := range names {
		b, err := os.ReadFile(filepath.Join(TargetsDir(), name))
		if err != nil {
			return nil, err
		}
		var doc Config
		if err := toml.Unmarshal(b, &doc); err != nil {
			return nil, fmt.Errorf("targets/%s: %w", name, err)
		}
		if len(doc.Targets) == 0 {
			return nil, fmt.Errorf("targets/%s: declares no [[targets]]", name)
		}
		out = mergeTargets(out, doc.Targets)
	}
	return out, nil
}

// mergeTargets folds later definitions over earlier ones by name: redefining
// "mango" replaces it, a new name extends the list. A target is never deleted
// this way — its own Detect already skips it on a machine that does not run it.
func mergeTargets(base, over []Target) []Target {
	for _, t := range over {
		replaced := false
		for i := range base {
			if base[i].Name == t.Name {
				base[i] = t
				replaced = true
				break
			}
		}
		if !replaced {
			base = append(base, t)
		}
	}
	return base
}

// finish desugars and validates every target, wherever it came from, so a
// broken operation is reported before a single file is touched rather than
// halfway through a switch.
func finish(cfg Config) (Config, error) {
	for i := range cfg.Targets {
		cfg.Targets[i] = cfg.Targets[i].Desugar()
		for _, op := range cfg.Targets[i].Ops {
			if err := op.Validate(cfg.Targets[i].Name); err != nil {
				return cfg, err
			}
		}
	}
	return cfg, nil
}

func merge(dst *Config, src Config) {
	if src.StateFile != "" {
		dst.StateFile = src.StateFile
	}
	if src.PathDir != "" {
		dst.PathDir = src.PathDir
	}
	if src.ThemesURL != "" {
		dst.ThemesURL = src.ThemesURL
	}
}
