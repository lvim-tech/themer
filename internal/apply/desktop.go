package apply

import (
	"bytes"
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

// GTK switches the desktop half. Only the GTK4 stylesheet is copied;
// GTK2, GTK3 and the window decorations are chosen BY NAME out of
// ~/.local/share/themes/<name>/ and never move. libadwaita is the exception
// that makes this target more than one gsettings call: it ignores
// gtk-theme-name entirely and reads ~/.config/gtk-4.0/gtk.css, so the GTK4
// half has to be copied on every switch or GTK4 applications stay on the
// previous theme while everything else changes.
//
// This was a call out to lvim-gtk-select until the script's absence was found
// to make Detect() return false — and a false Detect is a SILENT skip: every
// other target reported success and the desktop kept its old colours. The
// four operations below are the whole of that script's activate(); porting
// them removes the failure mode rather than reporting it.
type GTK struct {
	themes string
	cfg3   string
	cfg4   string
	// setName is the gsettings write, injected so tests do not repaint the
	// machine they run on.
	setName func(name string) error
}

func NewGTK() *GTK {
	return &GTK{
		themes:  filepath.Join(dataHome(), "themes"),
		cfg3:    filepath.Join(configHome(), "gtk-3.0"),
		cfg4:    filepath.Join(configHome(), "gtk-4.0"),
		setName: gsettingsTheme,
	}
}

func (g *GTK) Name() string { return "gtk 3+4" }

func (g *GTK) Detect() (bool, string) {
	entries, err := os.ReadDir(g.themes)
	if err != nil {
		return false, "no " + shortPath(g.themes)
	}
	for _, e := range entries {
		if e.IsDir() && strings.HasPrefix(e.Name(), "Lvim-") {
			return true, ""
		}
	}
	return false, "no Lvim-* theme in " + shortPath(g.themes)
}

func (g *GTK) Apply(t theme.Theme, _ theme.Palette) (string, error) {
	name := t.GTKName()
	src := filepath.Join(g.themes, name)
	if fi, err := os.Stat(src); err != nil || !fi.IsDir() {
		return "", fmt.Errorf("not installed: %s (build it with lvim-gtk's scripts/setup)", name)
	}

	if err := g.copyGTK4(src); err != nil {
		return "", err
	}
	if err := g.writeSettingsINI(name); err != nil {
		return "", err
	}
	moved, err := g.moveAsideForeignCSS()
	if err != nil {
		return "", err
	}

	did := name + " → gtk.css, settings.ini"
	// gsettings is best-effort on purpose: a bare wayland session may carry no
	// dconf at all, and settings.ini alone already dresses GTK3 there. Failing
	// the whole switch over it would be the wrong trade.
	if err := g.setName(name); err == nil {
		did += ", gsettings"
	}
	if moved != "" {
		did += " (moved aside " + moved + ")"
	}
	return did, nil
}

// copyGTK4 installs the stylesheet libadwaita reads, and the glyphs it points
// at. lvim-assets travels with it because url() resolves relative to the
// STYLESHEET, which now lives in ~/.config rather than in the theme; the name
// is lvim-assets rather than assets because ~/.config/gtk-4.0 is shared and
// another generator has already left an assets/ of its own there.
func (g *GTK) copyGTK4(src string) error {
	if err := os.MkdirAll(g.cfg4, 0o755); err != nil {
		return err
	}
	css := filepath.Join(g.cfg4, "gtk.css")

	// Someone else's GTK4 stylesheet is preserved ONCE, so a first run never
	// destroys it and later runs never overwrite that copy with our own.
	backup := css + ".before-lvim"
	if b, err := os.ReadFile(css); err == nil && !bytes.Contains(b, []byte("lvim-gtk")) {
		if _, err := os.Stat(backup); os.IsNotExist(err) {
			if err := os.WriteFile(backup, b, 0o644); err != nil {
				return err
			}
		}
	}

	b, err := os.ReadFile(filepath.Join(src, "gtk-4.0", "gtk.css"))
	if err != nil {
		return fmt.Errorf("theme has no gtk-4.0/gtk.css: %w", err)
	}
	if err := os.WriteFile(css, b, 0o644); err != nil {
		return err
	}

	assets := filepath.Join(g.cfg4, "lvim-assets")
	if err := os.RemoveAll(assets); err != nil {
		return err
	}
	return os.CopyFS(assets, os.DirFS(filepath.Join(src, "gtk-4.0", "lvim-assets")))
}

var gtkThemeName = regexp.MustCompile(`(?m)^gtk-theme-name=.*$`)

// writeSettingsINI is not a duplicate of the gsettings call above: two
// independent sources decide gtk-theme-name and must not be allowed to
// disagree. GSettings is what a GNOME session and xdg-desktop-portal read;
// settings.ini is what GTK3 reads with no settings daemon running, which is a
// plain wayland session and what nwg-look writes. Set one and the other still
// names the previous theme, so which colours an application gets depends on
// how it happened to be launched.
func (g *GTK) writeSettingsINI(name string) error {
	if err := os.MkdirAll(g.cfg3, 0o755); err != nil {
		return err
	}
	path := filepath.Join(g.cfg3, "settings.ini")
	b, err := os.ReadFile(path)
	if err != nil {
		return os.WriteFile(path, []byte("[Settings]\ngtk-theme-name="+name+"\n"), 0o644)
	}
	line := "gtk-theme-name=" + name
	switch content := string(b); {
	case gtkThemeName.MatchString(content):
		content = gtkThemeName.ReplaceAllString(content, line)
		return os.WriteFile(path, []byte(content), 0o644)
	case strings.Contains(content, "[Settings]"):
		content = strings.Replace(content, "[Settings]", "[Settings]\n"+line, 1)
		return os.WriteFile(path, []byte(content), 0o644)
	default:
		// A settings.ini with no [Settings] header at all: the shell version
		// left it untouched here, which read as success and changed nothing.
		return os.WriteFile(path, []byte("[Settings]\n"+line+"\n"+content), 0o644)
	}
}

// moveAsideForeignCSS gets a previous generator's ~/.config/gtk-3.0/gtk.css
// out of the way. A user stylesheet loads at PRIORITY_USER (800) and outranks
// the theme's 200, so anything left there quietly overrides ours. Moved, not
// deleted.
func (g *GTK) moveAsideForeignCSS() (string, error) {
	path := filepath.Join(g.cfg3, "gtk.css")
	b, err := os.ReadFile(path)
	if err != nil || bytes.Contains(b, []byte("lvim-gtk")) {
		return "", nil
	}
	dir := filepath.Join(g.cfg3, "before-lvim")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	if err := os.Rename(path, filepath.Join(dir, "gtk.css")); err != nil {
		return "", err
	}
	return shortPath(path), nil
}

func gsettingsTheme(name string) error {
	if _, err := exec.LookPath("gsettings"); err != nil {
		return err
	}
	return exec.Command("gsettings", "set", "org.gnome.desktop.interface", "gtk-theme", name).Run()
}

func dataHome() string {
	if dir := os.Getenv("XDG_DATA_HOME"); dir != "" {
		return dir
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".local", "share")
}

func configHome() string {
	if dir := os.Getenv("XDG_CONFIG_HOME"); dir != "" {
		return dir
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".config")
}
