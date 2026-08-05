package apply

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/lvim-tech/themer/internal/theme"
)

// Neovim rewrites the theme lvim-colorscheme restores at the next startup.
//
// Without it the desktop and the editor drift: themer had no nvim target at
// all, so switching here left neovim — and nvcat, which is neovim — on the
// previous theme until it was switched a second time from inside the editor.
// The two records agreed only because both had been set to the same thing by
// hand.
//
// BOTH files are written, because that is what the plugin's own save_theme
// does: settings.json under the shared `colorscheme` key, and the plain
// one-line mirror beside it. load_theme reads the mirror first, so the mirror
// alone would work today — and would leave the document answering with a stale
// name the day the mirror is removed.
//
// Running instances keep their colours. neovim has no SIGUSR1 the way kitty
// does, and reaching a live one means --server plus a socket path themer has no
// business guessing at.
type Neovim struct{ dir string }

// NewNeovim points at lvim-colorscheme's own data directory, under
// stdpath("data") — which is XDG_DATA_HOME, not $HOME.
func NewNeovim() *Neovim {
	return &Neovim{dir: filepath.Join(dataHome(), "nvim", "lvim-colorscheme")}
}

func (n *Neovim) Name() string { return "neovim" }

// Detect requires the plugin's data directory, not merely neovim's: the
// directory appears the first time lvim-colorscheme persists anything, so its
// presence means the plugin is installed and has run. Keying on neovim alone
// would create a stray directory on a machine that uses a different theme.
func (n *Neovim) Detect() (bool, string) {
	if fi, err := os.Stat(n.dir); err != nil || !fi.IsDir() {
		return false, "no " + shortPath(n.dir)
	}
	return true, ""
}

func (n *Neovim) Apply(t theme.Theme) (string, error) {
	name := t.NvimName()
	// Document first, mirror second — the plugin's order. Reversed, a failure
	// here would leave the mirror already switched and load_theme answering
	// with a name the document contradicts.
	if err := n.writeSettings(name); err != nil {
		return "", err
	}
	if err := os.WriteFile(filepath.Join(n.dir, "theme"), []byte(name+"\n"), 0o644); err != nil {
		return "", err
	}
	return name + " → theme, settings.json", nil
}

// writeSettings sets the colorscheme key and leaves every other setting alone.
func (n *Neovim) writeSettings(name string) error {
	path := filepath.Join(n.dir, "settings.json")

	settings := map[string]any{}
	switch b, err := os.ReadFile(path); {
	case err == nil:
		// A document we cannot parse is not one to overwrite: it holds the
		// user's sidebar, dim and transparency choices, and guessing at them
		// costs more than the stale colorscheme key does.
		if err := json.Unmarshal(b, &settings); err != nil {
			return fmt.Errorf("%s: %w", shortPath(path), err)
		}
	case !os.IsNotExist(err):
		return err
	}
	settings["colorscheme"] = name

	encoded, err := json.Marshal(settings)
	if err != nil {
		return err
	}
	// Written through a temporary file, like the store the plugin uses: a
	// truncated settings.json costs every setting in it, not just this key.
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, append(encoded, '\n'), 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}
