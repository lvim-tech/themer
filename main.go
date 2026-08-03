// themer switches the Lvim theme everywhere at once: the ~/.theme state
// file, every clipack-installed tool, kitty, tmux, wezterm, GTK 3+4 through
// lvim-gtk-select, and whichever compositor is running — plus anything else
// declared as a target in ~/.config/themer/config.toml.
//
//	themer                  # pick from the list
//	themer LvimNord_dark    # switch without the TUI
//	themer --list           # print the theme names
//	themer --sync           # pull the palettes from GitHub into themes.toml
package main

import (
	"fmt"
	"os"
	"path/filepath"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/lvim-tech/themer/internal/apply"
	"github.com/lvim-tech/themer/internal/config"
	"github.com/lvim-tech/themer/internal/sync"
	"github.com/lvim-tech/themer/internal/theme"
	"github.com/lvim-tech/themer/tui"
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		fail(err)
	}
	ensureClipackOnPath(cfg)
	themes, err := theme.Discover(cfg.PalettesDir)
	if err != nil && len(cfg.Themes) == 0 {
		// A missing palettes directory is only fatal while it is the sole
		// source: a config full of inline themes stands on its own.
		fail(err)
	}
	var inline []theme.Theme
	for _, def := range cfg.Themes {
		inline = append(inline, theme.FromConfig(def.Name, def.Palette))
	}
	themes = theme.Merge(themes, inline)

	if len(os.Args) > 1 {
		switch os.Args[1] {
		case "--sync", "-s":
			n, err := sync.Run("https://api.github.com", cfg.PalettesRepo, config.ThemesFile())
			if err != nil {
				fail(err)
			}
			fmt.Printf("synced %d themes from github.com/%s into %s\n", n, cfg.PalettesRepo, config.ThemesFile())
			return
		case "--list", "-l":
			current := theme.Current(cfg.StateFile)
			for _, t := range themes {
				mark := "  "
				if t.Name == current {
					mark = "● "
				}
				fmt.Println(mark + t.Name)
			}
			return
		default:
			runDirect(cfg, themes, os.Args[1])
			return
		}
	}

	p := tea.NewProgram(tui.New(cfg, themes), tea.WithAltScreen())
	if _, err := p.Run(); err != nil {
		fail(err)
	}
}

// runDirect is the scriptable path: no TUI, one line per applier, exit 1 if
// anything failed — so a keybinding or a cron job can switch themes too.
func runDirect(cfg config.Config, themes []theme.Theme, name string) {
	t, ok := theme.ByName(themes, name)
	if !ok {
		fail(fmt.Errorf("no theme %q — see themer --list", name))
	}
	p, err := theme.Load(t, cfg.PalettesDir)
	if err != nil {
		fail(err)
	}
	results := make(chan apply.Result)
	go apply.Run(apply.All(cfg), t, p, results)
	failed := false
	for r := range results {
		switch r.Status {
		case apply.StatusOK:
			fmt.Printf("✓ %-16s %s\n", r.Name, r.Note)
		case apply.StatusSkipped:
			fmt.Printf("○ %-16s %s\n", r.Name, r.Note)
		case apply.StatusFailed:
			failed = true
			fmt.Printf("✗ %-16s %s\n", r.Name, r.Note)
		}
	}
	if failed {
		os.Exit(1)
	}
}

// ensureClipackOnPath puts clipack's bin directory on PATH for everything
// themer runs. Started from a shell it is already there; started from a
// COMPOSITOR keybind — which is how themer is normally reached, through ql
// or a hotkey — the environment carries only the login PATH, and tmux and
// kitty (installed by clipack, with the distribution's copies removed) were
// simply not found. The appliers then failed on a machine where the same
// switch worked perfectly from a terminal.
func ensureClipackOnPath(cfg config.Config) {
	bin := filepath.Join(cfg.ClipackBase, "bin")
	if _, err := os.Stat(bin); err != nil {
		return
	}
	current := os.Getenv("PATH")
	for _, dir := range filepath.SplitList(current) {
		if dir == bin {
			return
		}
	}
	os.Setenv("PATH", bin+string(os.PathListSeparator)+current)
}

func fail(err error) {
	fmt.Fprintln(os.Stderr, "themer:", err)
	os.Exit(1)
}
