package apply

import (
	"os"
	"strings"
	"testing"
)

// waybar's style path is frozen at launch, so the target rewrites the theme
// @import inside the permanent current.css the compositors now start it
// with. The relative styles/ prefix must survive: the file lives in
// ~/.config/waybar and GTK resolves the import against it.
//
// current.css carries a SECOND import — structure.css, the colour-free
// layout — which must come out of a switch untouched. The rule is anchored
// on styles/ for exactly that: a bare `@import ".*"` matched this line too
// and turned it into a duplicate theme import, leaving the bar unstyled.
func TestWaybarRewritesThemeImportAndSparesStructure(t *testing.T) {
	a, path := targetFor(t, "waybar", "current.css",
		"@import \"styles/LvimKanagawa_dark.css\";\n@import \"structure.css\";\n")

	if _, err := a.Apply(testTheme, testPalette); err != nil {
		t.Fatal(err)
	}
	got, _ := os.ReadFile(path)
	if !strings.Contains(string(got), `@import "styles/LvimEverforest_soft.css";`) {
		t.Errorf("theme import not rewritten:\n%s", got)
	}
	if !strings.Contains(string(got), `@import "structure.css";`) {
		t.Errorf("structure import did not survive the switch:\n%s", got)
	}
	if n := strings.Count(string(got), "styles/LvimEverforest_soft.css"); n != 1 {
		t.Errorf("theme import written %d times, want exactly 1:\n%s", n, got)
	}
}
