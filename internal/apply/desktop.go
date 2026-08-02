package apply

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/lvim-tech/themer/internal/theme"
)

// Wezterm renames the active color_scheme — and only that. This machine's
// wezterm.lua deliberately runs on `colors = custom` with the color_scheme
// line commented out; overriding a hand-built palette from a switcher would
// be exactly the wrong kind of help, so that configuration is a Skip with
// its reason spelled out. Wezterm watches its config file, so a rewrite is
// also the reload.
type Wezterm struct{ conf string }

func NewWezterm() *Wezterm {
	home, _ := os.UserHomeDir()
	return &Wezterm{conf: filepath.Join(home, ".config", "wezterm", "wezterm.lua")}
}
func (w *Wezterm) Name() string { return "wezterm" }

// An active (uncommented) assignment; the commented form starts with --.
var weztermScheme = regexp.MustCompile(`(?m)^(\s*)color_scheme(\s*)=(\s*)"[^"]*"`)

func (w *Wezterm) Detect() (bool, string) {
	b, err := os.ReadFile(w.conf)
	if err != nil {
		return false, "no " + w.conf
	}
	if !weztermScheme.Match(b) {
		return false, "runs on a hand-built palette (colors = custom), left alone"
	}
	return true, ""
}
func (w *Wezterm) Apply(t theme.Theme, _ theme.Palette) (string, error) {
	b, err := os.ReadFile(w.conf)
	if err != nil {
		return "", err
	}
	out := weztermScheme.ReplaceAllString(string(b), `${1}color_scheme${2}=${3}"`+t.Name+`"`)
	if err := os.WriteFile(w.conf, []byte(out), 0o644); err != nil {
		return "", err
	}
	return "color_scheme → " + t.Name, nil
}

// GTK delegates to lvim-gtk-select, which is the only switcher that does
// this correctly: libadwaita ignores gtk-theme-name and reads only
// ~/.config/gtk-4.0/gtk.css, so the GTK4 half must be copied on every
// switch — see activate() in lvim-gtk's _common.sh. Reimplementing that
// here would drift the first time the script learned something new.
type GTK struct{}

func NewGTK() *GTK          { return &GTK{} }
func (g *GTK) Name() string { return "gtk 3+4" }
func (g *GTK) Detect() (bool, string) {
	if _, err := exec.LookPath("lvim-gtk-select"); err != nil {
		return false, "lvim-gtk-select not in PATH"
	}
	return true, ""
}
func (g *GTK) Apply(t theme.Theme, _ theme.Palette) (string, error) {
	// No tty on stdin: the script's ask() answers its own defaults then,
	// which is what a non-interactive switch wants.
	cmd := exec.Command("lvim-gtk-select", t.GTKArg())
	cmd.Stdin = nil
	out, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("lvim-gtk-select %s: %s", t.GTKArg(), lastLine(string(out)))
	}
	return "lvim-gtk-select " + t.GTKArg(), nil
}

func lastLine(s string) string {
	lines := strings.Split(strings.TrimSpace(s), "\n")
	return lines[len(lines)-1]
}
