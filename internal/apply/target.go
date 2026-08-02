package apply

import (
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"strings"
	"syscall"

	"github.com/lvim-tech/themer/internal/config"
	"github.com/lvim-tech/themer/internal/theme"
)

// TargetApplier runs one declarative target from the configuration. The
// compositors ship as built-in targets, but nothing about this type knows
// what a compositor is: any tool whose theme lives on rewritable lines and
// reloads on a command or a signal can be added in config.toml without
// touching Go — that is the point of the declarative form.
type TargetApplier struct {
	t     config.Target
	roles map[string]string
}

func NewTarget(t config.Target, roles map[string]string) *TargetApplier {
	return &TargetApplier{t: t, roles: roles}
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
		if _, err := os.Stat(expandHome(d.File)); err != nil {
			return false, "no " + d.File
		}
	}
	if d.Command != "" {
		if _, err := exec.LookPath(d.Command); err != nil {
			return false, d.Command + " not in PATH"
		}
	}
	if d.Running == "" && d.File == "" && d.Command == "" {
		return false, "target declares no detect rule"
	}
	return true, ""
}

func (a *TargetApplier) Apply(t theme.Theme, p theme.Palette) (string, error) {
	var did []string
	for _, e := range a.t.Edits {
		n, err := a.applyEdit(e, t, p)
		if err != nil {
			return "", err
		}
		did = append(did, fmt.Sprintf("%s (%d)", shortPath(e.File), n))
	}
	for _, r := range a.t.Reload {
		note, err := runReload(r)
		if err != nil {
			return "", err
		}
		if note != "" {
			did = append(did, note)
		}
	}
	return strings.Join(did, ", "), nil
}

func (a *TargetApplier) applyEdit(e config.Edit, t theme.Theme, p theme.Palette) (int, error) {
	path := expandHome(e.File)
	b, err := os.ReadFile(path)
	if err != nil {
		return 0, err
	}
	content := string(b)
	total := 0
	for _, rule := range e.Rules {
		value, err := expandColors(rule.Value, t, p, a.roles)
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

// expandColors resolves {placeholder} against the theme name, the role
// mapping, then the palette itself, so a rule can say {focus} (role),
// {green-dark} (raw palette key) or {theme} (canonical name). Colours are
// bare rrggbb: the rule's template owns the surrounding syntax — 0x…ff for
// mango, rgba(…) for hyprland, "#…" for niri.
var placeholder = regexp.MustCompile(`\{([a-zA-Z][a-zA-Z0-9_-]*)\}`)

func expandColors(tpl string, t theme.Theme, p theme.Palette, roles map[string]string) (string, error) {
	var missing string
	out := placeholder.ReplaceAllStringFunc(tpl, func(m string) string {
		key := m[1 : len(m)-1]
		if key == "theme" {
			return t.Name
		}
		if role, ok := roles[key]; ok {
			key = role
		}
		if hex, ok := p[key]; ok {
			return strings.TrimPrefix(hex, "#")
		}
		missing = key
		return m
	})
	if missing != "" {
		return "", fmt.Errorf("placeholder {%s} names no role and no palette colour", missing)
	}
	return out, nil
}

func runReload(r config.Reload) (string, error) {
	if len(r.Command) > 0 {
		if out, err := exec.Command(r.Command[0], r.Command[1:]...).CombinedOutput(); err != nil {
			return "", fmt.Errorf("%s: %s", strings.Join(r.Command, " "), firstLine(string(out)))
		}
		return strings.Join(r.Command, " "), nil
	}
	if r.Signal != "" && r.Process != "" {
		sig, ok := signals[strings.ToUpper(r.Signal)]
		if !ok {
			return "", fmt.Errorf("unknown signal %q", r.Signal)
		}
		if n := signalAll(r.Process, sig); n > 0 {
			return fmt.Sprintf("SIG%s → %d × %s", strings.ToUpper(r.Signal), n, r.Process), nil
		}
		return "", nil // nothing running: the edit still counts
	}
	return "", nil
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
