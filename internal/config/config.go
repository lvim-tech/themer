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

	"github.com/lvim-tech/themer/internal/uitheme"
)

type Config struct {
	// Theme selects how themer's OWN interface looks — its chrome, not the
	// palettes it switches between. It names a built-in preset or a file in
	// ~/.config/themer/themes, and may override the border or icon set. Left
	// unset it resolves to the family default, so themer is themeable like the
	// rest of the tools it drives.
	Theme uitheme.Theme `toml:"theme,omitempty"`
	// StateFile is the file this setup reads $THEME from — themer marks the
	// current theme by reading it. WRITING it is a target like any other, so
	// there is no default here: another setup may keep it elsewhere, or have
	// none, and themer would then simply not know which theme is active.
	StateFile string `toml:"state_file"`
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
	Name string `toml:"name"`
	// Priority orders the switch: lower runs earlier. Unset means
	// DefaultPriority, so a target only needs a number when it has a reason
	// to be early or late.
	//
	// Two reasons exist, both learned the hard way. The state file has to be
	// written before anything reads it. The compositors have to repaint last,
	// because a repaint before a failure looks like a switch that finished.
	// Filename order used to decide this, which put mango — a compositor —
	// ahead of state-file, exactly backwards.
	Priority int `toml:"priority,omitempty"`
	// Theme pins this target to one theme, whatever the switch was.
	//
	// Two neovims, one dark and one light; a terminal that has to stay
	// readable on a projector; a second instance of anything. A target is a
	// file, so a second instance is a second file — and this is the field that
	// lets the second one disagree about which theme it wants.
	//
	// A definition can also just write the name into its paths instead of
	// {theme}, and that has always worked. This says it once instead of in
	// every operation.
	Theme  string   `toml:"theme,omitempty"`
	Detect Detect   `toml:"detect"`
	Ops    []Op     `toml:"op,omitempty"`
	Edits  []Edit   `toml:"edit,omitempty"`
	Reload []Reload `toml:"reload,omitempty"`
}

// Operation kinds. Everything a target can do is one of these; nothing about
// a particular program belongs in Go.
// DefaultPriority is where a target lands when it does not ask for a place.
// Halfway, so a definition can be ordered before or after without negatives.
const DefaultPriority = 50

const (
	OpRewrite   = "rewrite"    // regex replacement over the lines of a file
	OpFetch     = "fetch"      // download source into target, both templated
	OpLink      = "link"       // symlink target → source, both templated
	OpWrite     = "write"      // write a file from a templated string
	OpCommand   = "command"    // run a command, optionally with extra environment
	OpJSONSet   = "json-set"   // set one key in a JSON document, leaving the rest
	OpCopyTree  = "copy-tree"  // copy a directory tree
	OpCopy      = "copy"       // copy one file, optionally keeping what was there
	OpSetLine   = "set-line"   // set a key=value line in an ini-shaped file
	OpMoveAside = "move-aside" // get somebody else's file out of the way
	OpAssemble  = "assemble"   // join several files into one
	OpSignal    = "signal"     // send a signal to every process of a name
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

	// Keep is where copy puts what was at Target before overwriting it —
	// ONCE. A first switch must not destroy a stylesheet somebody else wrote,
	// and a later one must not overwrite that rescue with our own file.
	Keep string `toml:"keep,omitempty"` // copy, move-aside
	// Unless marks a file as already ours: a target containing it is not
	// somebody else's and needs no rescuing. Without this the second switch
	// would "preserve" the theme the first one installed.
	Unless string `toml:"unless,omitempty"` // copy, move-aside
	// Section is the ini header set-line works within: the anchor a new line
	// is inserted after, and the header written when the file has none.
	Section string `toml:"section,omitempty"` // set-line
	// Regex is the line set-line replaces when it is already there.
	Regex string `toml:"regex,omitempty"` // set-line
	// Sources are the files assemble joins, in order.
	//
	// Some programs read exactly one file and offer no include: starship and
	// lazydocker both do. Their theme is therefore not a file they can be
	// pointed at but a fragment that has to be glued to the user's own — which
	// a shell script did on every shell start, and which is why neither
	// configuration could belong to the user.
	Sources []string `toml:"sources,omitempty"` // assemble
	// Marker is written as the first line of an assembled file, and is the
	// PERMISSION to overwrite it: a target that exists without the marker was
	// written by hand and is left alone. Without it an assemble would destroy
	// a configuration the moment someone decided to edit the product instead
	// of the parts.
	Marker string `toml:"marker,omitempty"` // assemble

	// Optional lets an operation fail without failing the target.
	//
	// gsettings is the case it exists for: a bare wayland session may carry no
	// dconf at all, and settings.ini alone already dresses GTK3 there. Failing
	// the whole switch over it would be the wrong trade — but so would running
	// it and never saying whether it worked, which is why the note records
	// what actually happened.
	Optional bool `toml:"optional,omitempty"`
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
	case OpFetch, OpLink, OpCopyTree, OpCopy:
		if err := need(o.Source != "", "source"); err != nil {
			return err
		}
		return need(o.Target != "", "target")
	case OpSetLine:
		if err := need(o.File != "", "file"); err != nil {
			return err
		}
		if err := need(o.Regex != "", "regex"); err != nil {
			return err
		}
		return need(o.Value != "", "value")
	case OpMoveAside:
		if err := need(o.File != "", "file"); err != nil {
			return err
		}
		return need(o.Target != "", "target")
	case OpAssemble:
		if err := need(len(o.Sources) > 0, "sources"); err != nil {
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
	// Dir is a directory that must exist. File cannot test one: it reads the
	// contents, which fails on a directory.
	Dir string `toml:"dir,omitempty"`
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
	// Stable, so targets sharing a priority keep the order their filenames
	// gave them — the only thing that decided it before.
	sort.SliceStable(cfg.Targets, func(i, j int) bool {
		return priorityOf(cfg.Targets[i]) < priorityOf(cfg.Targets[j])
	})
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
	if src.ThemesURL != "" {
		dst.ThemesURL = src.ThemesURL
	}
	// The interface theme is a small table, so it is taken whole: naming it in
	// config.toml at all means choosing it, and a partial [theme] still resolves
	// against the default base downstream.
	if src.Theme != (uitheme.Theme{}) {
		dst.Theme = src.Theme
	}
}

func priorityOf(t Target) int {
	if t.Priority == 0 {
		return DefaultPriority
	}
	return t.Priority
}
