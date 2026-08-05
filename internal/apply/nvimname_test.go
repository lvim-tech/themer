package apply

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/lvim-tech/themer/internal/theme"
)

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

	// lvim-{family}-{variant}, with no special case: the base family is called
	// base, so nothing has to strip a redundant lvim from itself. Written out
	// here rather than in themer, which now expresses it as a plain template.
	nvimName := func(th theme.Theme) string { return "lvim-" + th.Family + "-" + th.Variant }

	for _, th := range themes {
		file := filepath.Join(repo, "colors", nvimName(th)+".lua")
		if _, err := os.Stat(file); err != nil {
			t.Errorf("%s → %s, but %s does not exist", th.Name, nvimName(th), shortPath(file))
		}
	}
}
