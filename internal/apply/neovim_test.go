package apply

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/lvim-tech/themer/internal/config"
	"github.com/lvim-tech/themer/internal/theme"
)

// newTestNeovim builds the target over a throwaway data directory holding the
// settings document a real install carries.
func newTestNeovim(t *testing.T, settings string) *Neovim {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "nvim", "lvim-colorscheme")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if settings != "" {
		if err := os.WriteFile(filepath.Join(dir, "settings.json"), []byte(settings), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return &Neovim{dir: dir}
}

const liveSettings = `{"dim_inactive":true,"sidebars":"dark","colorscheme":"lvim-gruvbox-light"}` + "\n"

// Both records, because load_theme prefers the mirror but falls back to the
// document: writing one and not the other leaves the fallback answering with
// the theme that was just switched away from.
func TestNeovimWritesTheMirrorAndTheDocument(t *testing.T) {
	n := newTestNeovim(t, liveSettings)
	if _, err := n.Apply(testTheme); err != nil {
		t.Fatal(err)
	}

	if got := read(t, filepath.Join(n.dir, "theme")); got != "lvim-everforest-soft\n" {
		t.Errorf("mirror = %q, want %q", got, "lvim-everforest-soft\n")
	}
	var settings map[string]any
	if err := json.Unmarshal([]byte(read(t, filepath.Join(n.dir, "settings.json"))), &settings); err != nil {
		t.Fatal(err)
	}
	if settings["colorscheme"] != "lvim-everforest-soft" {
		t.Errorf("document colorscheme = %v, want lvim-everforest-soft", settings["colorscheme"])
	}
}

// The document is shared: it holds the settings panel's own values, and a
// rewrite that keeps only the key we came for silently resets the rest.
func TestNeovimLeavesTheOtherSettingsAlone(t *testing.T) {
	n := newTestNeovim(t, liveSettings)
	if _, err := n.Apply(testTheme); err != nil {
		t.Fatal(err)
	}

	var settings map[string]any
	if err := json.Unmarshal([]byte(read(t, filepath.Join(n.dir, "settings.json"))), &settings); err != nil {
		t.Fatal(err)
	}
	if settings["dim_inactive"] != true || settings["sidebars"] != "dark" {
		t.Errorf("settings = %v, want dim_inactive and sidebars preserved", settings)
	}
}

// A document that will not parse is the user's, not ours to replace — and the
// mirror must not move either, or the two disagree about what is active.
func TestNeovimRefusesAnUnparsableDocumentAndLeavesTheMirror(t *testing.T) {
	n := newTestNeovim(t, "{ this is not json")
	if err := os.WriteFile(filepath.Join(n.dir, "theme"), []byte("lvim-gruvbox-light\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	if _, err := n.Apply(testTheme); err == nil {
		t.Fatal("Apply = nil, want an error for an unparsable settings.json")
	}
	if got := read(t, filepath.Join(n.dir, "settings.json")); got != "{ this is not json" {
		t.Errorf("settings.json = %q, want it untouched", got)
	}
	if got := read(t, filepath.Join(n.dir, "theme")); got != "lvim-gruvbox-light\n" {
		t.Errorf("mirror = %q, want it untouched", got)
	}
}

// A fresh install has persisted nothing yet; the switch still has to land.
func TestNeovimWritesADocumentThatDoesNotExistYet(t *testing.T) {
	n := newTestNeovim(t, "")
	if _, err := n.Apply(testTheme); err != nil {
		t.Fatal(err)
	}

	var settings map[string]any
	if err := json.Unmarshal([]byte(read(t, filepath.Join(n.dir, "settings.json"))), &settings); err != nil {
		t.Fatal(err)
	}
	if settings["colorscheme"] != "lvim-everforest-soft" {
		t.Errorf("document colorscheme = %v, want lvim-everforest-soft", settings["colorscheme"])
	}
}

// Keyed on the plugin's directory rather than neovim's: a machine with neovim
// but without lvim-colorscheme has nothing here to switch, and Apply would
// create a directory no one reads.
func TestNeovimSkipsWithoutThePluginsDataDirectory(t *testing.T) {
	n := &Neovim{dir: filepath.Join(t.TempDir(), "nvim", "lvim-colorscheme")}
	ok, why := n.Detect()
	if ok {
		t.Fatal("Detect = true, want false when the directory is absent")
	}
	if !strings.Contains(why, "lvim-colorscheme") {
		t.Errorf("reason = %q, want it to name the missing directory", why)
	}
}

// NewNeovim resolves through XDG_DATA_HOME, not $HOME: this machine sets it,
// and a target that ignored it would write beside the real one and be read by
// nothing.
func TestNeovimResolvesTheDataHome(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_DATA_HOME", dir)
	if got, want := NewNeovim().dir, filepath.Join(dir, "nvim", "lvim-colorscheme"); got != want {
		t.Errorf("dir = %q, want %q", got, want)
	}
}

// The switch has to reach neovim at all — the gap this target was added to
// close. Without it every other target reported success and the editor kept
// the previous theme.
func TestNeovimIsAmongTheAppliers(t *testing.T) {
	for _, a := range All(config.Config{}) {
		if a.Name() == "neovim" {
			return
		}
	}
	t.Error("All() has no neovim applier")
}

// The mapping itself, over every theme the generator publishes: the names in
// extras/themes.txt are the only input, and lvim-colorscheme's own colors/*.lua
// is the answer. Skipped rather than failed where the checkout is absent — this
// guards against drift, it is not a reason CI cannot run.
func TestNvimNamesMatchTheInstalledColorschemes(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil {
		t.Skip(err)
	}
	repo := filepath.Join(home, "lvim-tech", "lvim-colorscheme")
	themes, err := theme.Discover(filepath.Join(repo, "extras", "themes.txt"))
	if err != nil {
		t.Skipf("no published theme list to check against: %v", err)
	}

	for _, th := range themes {
		file := filepath.Join(repo, "colors", th.NvimName()+".lua")
		if _, err := os.Stat(file); err != nil {
			t.Errorf("%s → %s, but %s does not exist", th.Name, th.NvimName(), shortPath(file))
		}
	}
}
