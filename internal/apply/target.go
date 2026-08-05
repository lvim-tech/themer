package apply

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"syscall"
	"time"

	"github.com/lvim-tech/themer/internal/config"
	"github.com/lvim-tech/themer/internal/theme"
)

// TargetApplier runs one declarative target from the configuration. The
// compositors ship as built-in targets, but nothing about this type knows
// what a compositor is: any tool whose theme lives on rewritable lines and
// reloads on a command or a signal can be added in config.toml without
// touching Go — that is the point of the declarative form.
type TargetApplier struct {
	t config.Target
}

// NewTarget desugars here rather than trusting the caller to have done it.
// Load desugars too, but a target also arrives from a built-in list or a test,
// and one that skipped the step would carry no operations and apply NOTHING —
// silently, reporting success.
func NewTarget(t config.Target) *TargetApplier {
	return &TargetApplier{t: t.Desugar()}
}

func (a *TargetApplier) Name() string { return a.t.Name }

func (a *TargetApplier) Detect() (bool, string) {
	d := a.t.Detect
	if d.Running != "" {
		if err := exec.Command("pgrep", "-x", d.Running).Run(); err != nil {
			return false, d.Running + " is not running"
		}
	}
	if d.File != "" {
		b, err := os.ReadFile(expandHome(d.File))
		if err != nil {
			return false, "no " + d.File
		}
		if d.Match != "" {
			re, err := regexp.Compile("(?m)" + d.Match)
			if err != nil {
				return false, fmt.Sprintf("detect match %q: %v", d.Match, err)
			}
			if !re.Match(b) {
				if d.Reason != "" {
					return false, d.Reason
				}
				return false, d.File + " does not match " + d.Match
			}
		}
	}
	if d.Command != "" {
		if _, err := exec.LookPath(d.Command); err != nil {
			return false, d.Command + " not in PATH"
		}
	}
	if !d.Always && d.Running == "" && d.File == "" && d.Command == "" {
		return false, "target declares no detect rule"
	}
	return true, ""
}

// Apply runs the target's operations in order and STOPS at the first failure.
//
// Stopping is the point, not a shortcut: GTK's four steps are one switch —
// copying the stylesheet and then failing to name the theme leaves a desktop
// that is neither the old one nor the new one. An operation list that carried
// on would turn every multi-step target into that.
func (a *TargetApplier) Apply(t theme.Theme) (string, error) {
	var did []string
	for _, op := range a.t.Ops {
		note, err := a.runOp(op, t)
		if err != nil {
			return "", err
		}
		if note != "" {
			did = append(did, note)
		}
	}
	return strings.Join(did, ", "), nil
}

func (a *TargetApplier) runOp(op config.Op, t theme.Theme) (string, error) {
	// Every path is templated too: a theme file is usually named after the
	// theme, so {theme} has to expand in `file` and `target`, not only in the
	// values written into them.
	expand := func(s string) (string, error) { return expandTheme(s, t) }

	switch op.Kind {
	case config.OpRewrite:
		n, err := a.applyRewrite(op, t)
		if err != nil {
			return "", err
		}
		return fmt.Sprintf("%s (%d)", shortPath(op.File), n), nil

	case config.OpWrite:
		path, err := expand(op.File)
		if err != nil {
			return "", err
		}
		content, err := expand(op.Content)
		if err != nil {
			return "", err
		}
		path = expandHome(path)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			return "", err
		}
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			return "", err
		}
		return "wrote " + shortPath(path), nil

	case config.OpFetch:
		src, err := expand(op.Source)
		if err != nil {
			return "", err
		}
		dst, err := expand(op.Target)
		if err != nil {
			return "", err
		}
		return fetch(src, expandHome(dst))

	case config.OpLink:
		src, err := expand(op.Source)
		if err != nil {
			return "", err
		}
		dst, err := expand(op.Target)
		if err != nil {
			return "", err
		}
		return a.link(expandHome(src), expandHome(dst))

	case config.OpCopyTree:
		src, err := expand(op.Source)
		if err != nil {
			return "", err
		}
		dst, err := expand(op.Target)
		if err != nil {
			return "", err
		}
		return copyTree(expandHome(src), expandHome(dst))

	case config.OpJSONSet:
		path, err := expand(op.File)
		if err != nil {
			return "", err
		}
		value, err := expand(op.Value)
		if err != nil {
			return "", err
		}
		return jsonSet(expandHome(path), op.Key, value)

	case config.OpCommand:
		argv := make([]string, len(op.Command))
		for i, a := range op.Command {
			v, err := expand(a)
			if err != nil {
				return "", err
			}
			argv[i] = v
		}
		env := make([]string, len(op.Env))
		for i, e := range op.Env {
			v, err := expand(e)
			if err != nil {
				return "", err
			}
			env[i] = v
		}
		return runCommand(argv, env)

	case config.OpSignal:
		return sendSignal(op.Signal, op.Process)
	}
	return "", fmt.Errorf("%s: unknown operation kind %q", a.t.Name, op.Kind)
}

func (a *TargetApplier) applyRewrite(e config.Op, t theme.Theme) (int, error) {
	file, err := expandTheme(e.File, t)
	if err != nil {
		return 0, err
	}
	path := expandHome(file)
	b, err := os.ReadFile(path)
	if err != nil {
		return 0, err
	}
	content := string(b)
	total := 0
	for _, rule := range e.Rules {
		value, err := expandTheme(rule.Value, t)
		if err != nil {
			return 0, fmt.Errorf("%s: %w", a.t.Name, err)
		}
		re, err := regexp.Compile("(?m)" + rule.Regex)
		if err != nil {
			return 0, fmt.Errorf("%s: rule %q: %w", a.t.Name, rule.Regex, err)
		}
		matches := len(re.FindAllString(content, -1))
		if matches == 0 {
			// A rule that matches nothing is a configuration bug, and a
			// silent one would surface as "the switch worked but my bar
			// kept the old colours" — fail loudly instead.
			return 0, fmt.Errorf("%s: rule %q matched nothing in %s", a.t.Name, rule.Regex, e.File)
		}
		content = re.ReplaceAllString(content, value)
		total += matches
	}
	return total, os.WriteFile(path, []byte(content), 0o644)
}

// fetchClient is shared so a target with several fetch operations reuses the
// connection instead of opening one per file.
var fetchClient = &http.Client{Timeout: 30 * time.Second}

// fetch downloads source into target.
//
// The generated theme file is the theme: it is overwritten, unlike a link,
// which steps aside for a file the user wrote. There is no way to tell those
// apart here — a downloaded file and a hand-written one look identical on
// disk — so the rule the shell setup scripts had ("replace only a link") does
// not survive the move to fetching. A target whose destination the user edits
// wants `link` and a local store, not `fetch`.
//
// Written through a temporary file: a half-downloaded theme is worse than the
// previous one, and a switch that fails should leave the old theme intact.
func fetch(source, target string) (string, error) {
	resp, err := fetchClient.Get(source)
	if err != nil {
		return "", fmt.Errorf("fetching %s: %w", source, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("fetching %s: %s", source, resp.Status)
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("reading %s: %w", source, err)
	}

	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		return "", err
	}
	tmp := target + ".tmp"
	if err := os.WriteFile(tmp, body, 0o644); err != nil {
		return "", err
	}
	if err := os.Rename(tmp, target); err != nil {
		return "", err
	}
	return fmt.Sprintf("fetched %s (%d B)", shortPath(target), len(body)), nil
}

// link points dst at src, replacing only what this tool may replace.
//
// A real file the user wrote is theirs: it is left alone and reported, not
// overwritten. That rule is copied from the shell setup scripts this replaces,
// where it was written once per package and got it right every time.
func (a *TargetApplier) link(src, dst string) (string, error) {
	if _, err := os.Stat(src); err != nil {
		return "", fmt.Errorf("%s: nothing to link at %s", a.t.Name, shortPath(src))
	}
	if fi, err := os.Lstat(dst); err == nil && fi.Mode()&os.ModeSymlink == 0 {
		return "kept " + shortPath(dst) + " (not a link)", nil
	}
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return "", err
	}
	// Removed first: os.Symlink will not replace an existing name, and a stale
	// link pointing at the previous theme is exactly what has to go.
	if err := os.Remove(dst); err != nil && !os.IsNotExist(err) {
		return "", err
	}
	if err := os.Symlink(src, dst); err != nil {
		return "", err
	}
	return shortPath(dst) + " → " + filepath.Base(src), nil
}

// copyTree copies src over dst, leaving anything already there that src does
// not carry. os.CopyFS refuses to overwrite, so the destination is cleared
// first — a theme's asset directory is wholly owned by the theme.
func copyTree(src, dst string) (string, error) {
	if fi, err := os.Stat(src); err != nil || !fi.IsDir() {
		return "", fmt.Errorf("no directory to copy at %s", shortPath(src))
	}
	if err := os.RemoveAll(dst); err != nil {
		return "", err
	}
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return "", err
	}
	if err := os.CopyFS(dst, os.DirFS(src)); err != nil {
		return "", err
	}
	return "copied " + shortPath(dst), nil
}

// jsonSet sets one key and leaves every other key as it was.
//
// Read-modify-write rather than a template: the document holds settings that
// belong to the program, not to the theme, and rewriting it wholesale would
// quietly reset them. A document that will not parse is reported, never
// replaced.
func jsonSet(path, key, value string) (string, error) {
	doc := map[string]any{}
	switch b, err := os.ReadFile(path); {
	case err == nil:
		if err := json.Unmarshal(b, &doc); err != nil {
			return "", fmt.Errorf("%s: %w", shortPath(path), err)
		}
	case !os.IsNotExist(err):
		return "", err
	}
	doc[key] = value

	encoded, err := json.Marshal(doc)
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return "", err
	}
	// Written through a temporary file: a truncated document costs every
	// setting in it, not just the one being set.
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, append(encoded, '\n'), 0o644); err != nil {
		return "", err
	}
	if err := os.Rename(tmp, path); err != nil {
		return "", err
	}
	return key + " → " + value, nil
}

func runCommand(argv, env []string) (string, error) {
	cmd := exec.Command(argv[0], argv[1:]...)
	if len(env) > 0 {
		// Appended to the inherited environment, and last: a target that sets
		// THEME means to override whatever the calling shell had.
		cmd.Env = append(os.Environ(), env...)
	}
	if out, err := cmd.CombinedOutput(); err != nil {
		return "", fmt.Errorf("%s: %s", strings.Join(argv, " "), firstLine(string(out)))
	}
	return strings.Join(argv, " "), nil
}

func sendSignal(name, process string) (string, error) {
	sig, ok := signals[strings.ToUpper(name)]
	if !ok {
		return "", fmt.Errorf("unknown signal %q", name)
	}
	if n := signalAll(process, sig); n > 0 {
		return fmt.Sprintf("SIG%s → %d × %s", strings.ToUpper(name), n, process), nil
	}
	return "", nil // nothing running: the edit still counts
}

// expandTheme resolves the placeholders a target may use.
//
// There is one: {theme}, the canonical name. Colour placeholders are gone with
// the palette — every target now downloads a file the generator produced, so
// nothing here composes a colour value any more. An unknown placeholder is an
// error rather than a literal, because a rule that wrote "{focus}" into a
// config file would look like it had worked.
var placeholder = regexp.MustCompile(`\{([a-zA-Z][a-zA-Z0-9_-]*)\}`)

func expandTheme(tpl string, t theme.Theme) (string, error) {
	var missing string
	out := placeholder.ReplaceAllStringFunc(tpl, func(m string) string {
		if key := m[1 : len(m)-1]; key == "theme" {
			return t.Name
		} else {
			missing = key
		}
		return m
	})
	if missing != "" {
		return "", fmt.Errorf("unknown placeholder {%s}: only {theme} is expanded", missing)
	}
	return out, nil
}

var signals = map[string]syscall.Signal{
	"USR1": syscall.SIGUSR1,
	"USR2": syscall.SIGUSR2,
	"HUP":  syscall.SIGHUP,
}

func expandHome(p string) string {
	if strings.HasPrefix(p, "~/") {
		home, _ := os.UserHomeDir()
		return home + p[1:]
	}
	return p
}

func shortPath(p string) string {
	home, _ := os.UserHomeDir()
	return strings.Replace(p, home, "~", 1)
}
