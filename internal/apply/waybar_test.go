package apply

import (
	"os"
	"strings"
	"testing"
)

// waybar's style path is frozen at launch, so the target rewrites the one
// @import inside the permanent current.css the compositors now start it
// with. The relative styles/ prefix must survive: the file lives in
// ~/.config/waybar and GTK resolves the import against it.
func TestWaybarRewritesTheImportAndKeepsItRelative(t *testing.T) {
	a, path := targetFor(t, "waybar", "current.css", "@import \"styles/LvimKanagawa_dark.css\";\n")

	if _, err := a.Apply(testTheme, testPalette); err != nil {
		t.Fatal(err)
	}
	got, _ := os.ReadFile(path)
	if !strings.Contains(string(got), `@import "styles/LvimEverforest_soft.css";`) {
		t.Errorf("import not rewritten:\n%s", got)
	}
}
