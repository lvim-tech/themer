// themer switches the Lvim theme everywhere at once: the ~/.theme state
// file, every clipack-installed tool, kitty, tmux, wezterm, GTK 3+4, and
// whichever compositor is running — plus anything else declared as a target
// in ~/.config/themer/config.toml.
//
//	themer                  # pick from the list
//	themer LvimNord_dark    # switch without the TUI
//	themer --list           # print the theme names
//	themer --sync           # fetch the list of theme names
package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

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
	ensureOnPath(cfg)
	// --sync is handled before the list is needed: it is what produces the
	// list, so requiring one first would leave a fresh install unable to get
	// started.
	if len(os.Args) > 1 && (os.Args[1] == "--sync" || os.Args[1] == "-s") {
		n, err := sync.Run(cfg.ThemesURL, config.ThemesFile())
		if err != nil {
			fail(err)
		}
		fmt.Printf("synced %d theme names from %s into %s\n", n, cfg.ThemesURL, config.ThemesFile())
		return
	}

	themes, err := theme.Discover(config.ThemesFile())
	if err != nil {
		fail(fmt.Errorf("%w\n\nthemer ships no themes of its own. Name the list in %s:\n\n    themes_url = \"https://raw.githubusercontent.com/<owner>/<repo>/main/extras/themes.txt\"\n\nthen run `themer --sync`", err, config.Dir()+"/config.toml"))
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
	results := make(chan apply.Result)
	go apply.Run(apply.All(cfg), t, results)
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

// ensureOnPath prepends the configured directory to PATH for everything themer
// runs. See PathDir for why this exists at all.
func ensureOnPath(cfg config.Config) {
	bin := cfg.PathDir
	if bin == "" {
		return
	}
	bin = expandHome(bin)
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

// expandHome resolves a leading ~ the way every configured path is resolved.
func expandHome(p string) string {
	if strings.HasPrefix(p, "~/") {
		home, _ := os.UserHomeDir()
		return home + p[1:]
	}
	return p
}
