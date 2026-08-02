// themer switches the Lvim theme everywhere at once: the ~/.theme state
// file, every clipack-installed tool, kitty, tmux, wezterm, GTK 3+4 through
// lvim-gtk-select, and whichever compositor is running — plus anything else
// declared as a target in ~/.config/themer/config.toml.
//
//	themer                  # pick from the list
//	themer LvimNord_dark    # switch without the TUI
//	themer --list           # print the theme names
package main

import (
	"fmt"
	"os"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/lvim-tech/themer/internal/apply"
	"github.com/lvim-tech/themer/internal/config"
	"github.com/lvim-tech/themer/internal/theme"
	"github.com/lvim-tech/themer/tui"
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		fail(err)
	}
	themes, err := theme.Discover(cfg.PalettesDir)
	if err != nil {
		fail(err)
	}

	if len(os.Args) > 1 {
		switch os.Args[1] {
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
	p, err := theme.LoadPalette(t.PaletteFile(cfg.PalettesDir))
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

func fail(err error) {
	fmt.Fprintln(os.Stderr, "themer:", err)
	os.Exit(1)
}
