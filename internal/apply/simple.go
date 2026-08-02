package apply

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"syscall"

	"github.com/lvim-tech/themer/internal/theme"
)

// StateFile writes ~/.theme — the one file every new shell reads.
type StateFile struct{ path string }

func NewStateFile(path string) *StateFile { return &StateFile{path: path} }
func (s *StateFile) Name() string         { return "~/.theme" }
func (s *StateFile) Detect() (bool, string) {
	return true, "" // creating it is the point; absence never skips
}
func (s *StateFile) Apply(t theme.Theme, _ theme.Palette) (string, error) {
	if err := os.WriteFile(s.path, []byte(t.Name+"\n"), 0o644); err != nil {
		return "", err
	}
	return t.Name, nil
}

// Clipack re-sources the aggregate with the new $THEME. That is exactly what
// a fresh shell does at startup, so every side effect the config.sh scripts
// carry — yazi's theme.toml sed, bat's config sed plus cache rebuild, the
// lazygit/lazydocker links — happens now instead of at the next login. The
// env exports the scripts also do die with the subprocess, and that is fine:
// live shells cannot be reached from outside anyway.
type Clipack struct{ base string }

func NewClipack(base string) *Clipack { return &Clipack{base: base} }
func (c *Clipack) Name() string       { return "clipack tools" }
func (c *Clipack) Detect() (bool, string) {
	agg := filepath.Join(c.base, "configs", "clipack.sh")
	if _, err := os.Stat(agg); err != nil {
		return false, "no aggregate at " + agg
	}
	return true, ""
}
func (c *Clipack) Apply(t theme.Theme, _ theme.Palette) (string, error) {
	agg := filepath.Join(c.base, "configs", "clipack.sh")
	// zsh, not sh: the aggregate is POSIX but the scripts it sources are a
	// mix of zsh and bash idiom, and zsh is what sources them at every real
	// shell start. Matching that keeps this path identical to a login.
	cmd := exec.Command("zsh", "-c", ". "+shellQuote(agg))
	cmd.Env = append(environWithout("THEME"), "THEME="+t.Name)
	if out, err := cmd.CombinedOutput(); err != nil {
		return "", fmt.Errorf("aggregate: %v: %s", err, firstLine(string(out)))
	}
	return "re-sourced " + agg, nil
}

// Kitty swaps the theme file kitty.conf includes and signals every running
// kitty: SIGUSR1 makes kitty reload its configuration, so open windows
// recolour without a restart.
type Kitty struct{ themeConf string }

func NewKitty() *Kitty {
	home, _ := os.UserHomeDir()
	return &Kitty{themeConf: filepath.Join(home, ".config", "kitty", "theme.conf")}
}
func (k *Kitty) Name() string { return "kitty" }
func (k *Kitty) Detect() (bool, string) {
	if _, err := os.Stat(k.themeConf); err != nil {
		return false, "no " + k.themeConf
	}
	return true, ""
}

var kittyInclude = regexp.MustCompile(`(?m)^(include\s+)(.*)$`)

func (k *Kitty) Apply(t theme.Theme, _ theme.Palette) (string, error) {
	b, err := os.ReadFile(k.themeConf)
	if err != nil {
		return "", err
	}
	m := kittyInclude.FindStringSubmatch(string(b))
	if m == nil {
		return "", fmt.Errorf("%s holds no include line", k.themeConf)
	}
	// Swap only the file name: whichever directory the include already
	// points at — the colorscheme extras or clipack's copy — is the user's
	// choice, and this is not the place to overrule it.
	next := filepath.Join(filepath.Dir(m[2]), t.Name+".conf")
	if _, err := os.Stat(next); err != nil {
		return "", fmt.Errorf("theme file missing: %s", next)
	}
	out := kittyInclude.ReplaceAllString(string(b), "${1}"+next)
	if err := os.WriteFile(k.themeConf, []byte(out), 0o644); err != nil {
		return "", err
	}
	note := "include → " + next
	if n := signalAll("kitty", syscall.SIGUSR1); n > 0 {
		note += fmt.Sprintf(", reloaded %d running", n)
	}
	return note, nil
}

// Tmux points the live server at the new theme, the same two source-file
// calls the tmux config.sh does inside a session.
type Tmux struct{ base string }

func NewTmux(base string) *Tmux { return &Tmux{base: base} }
func (t *Tmux) Name() string    { return "tmux" }
func (t *Tmux) Detect() (bool, string) {
	if _, err := exec.LookPath("tmux"); err != nil {
		return false, "tmux not in PATH"
	}
	if err := exec.Command("tmux", "has-session").Run(); err != nil {
		return false, "no running server"
	}
	return true, ""
}
func (tm *Tmux) Apply(t theme.Theme, _ theme.Palette) (string, error) {
	dir := filepath.Join(tm.base, "configs", "tmux")
	themeConf := filepath.Join(dir, t.Name+".conf")
	if _, err := os.Stat(themeConf); err != nil {
		return "", fmt.Errorf("theme file missing: %s", themeConf)
	}
	for _, f := range []string{themeConf, filepath.Join(dir, "tmux.conf")} {
		if _, err := os.Stat(f); err != nil {
			continue
		}
		if out, err := exec.Command("tmux", "source-file", f).CombinedOutput(); err != nil {
			return "", fmt.Errorf("source-file %s: %s", f, firstLine(string(out)))
		}
	}
	return "sourced " + t.Name + ".conf", nil
}

// signalAll sends sig to every process with exactly this name and reports
// how many it reached.
func signalAll(comm string, sig syscall.Signal) int {
	out, err := exec.Command("pgrep", "-x", comm).Output()
	if err != nil {
		return 0
	}
	n := 0
	for _, line := range strings.Fields(string(out)) {
		var pid int
		if _, err := fmt.Sscanf(line, "%d", &pid); err == nil {
			if syscall.Kill(pid, sig) == nil {
				n++
			}
		}
	}
	return n
}

func environWithout(key string) []string {
	env := os.Environ()
	kept := env[:0]
	for _, kv := range env {
		if !strings.HasPrefix(kv, key+"=") {
			kept = append(kept, kv)
		}
	}
	return kept
}

func shellQuote(s string) string { return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'" }

func firstLine(s string) string {
	s = strings.TrimSpace(s)
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return s[:i]
	}
	return s
}
