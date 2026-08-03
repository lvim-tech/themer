package apply

import (
	"os"
	"strings"
	"testing"
)

// waybar's whole theme is thirteen @define-color lines written straight from
// the palette into the permanent colors.css — no theme files, no import
// rewriting. current.css imports colors.css and structure.css and is never
// touched. Every line must be rewritten: a rule that matches nothing fails
// loudly, so a colors.css missing a key surfaces instead of half-theming.
func TestWaybarRewritesEveryDefineColorFromThePalette(t *testing.T) {
	lines := []string{
		"@define-color bg #000000;",
		"@define-color bg_dark #000000;",
		"@define-color fg #000000;",
		"@define-color fg_light #000000;",
		"@define-color fg_soft_dark #000000;",
		"@define-color red #000000;",
		"@define-color orange #000000;",
		"@define-color yellow #000000;",
		"@define-color green #000000;",
		"@define-color teal #000000;",
		"@define-color cyan #000000;",
		"@define-color cyan_dark #000000;",
		"@define-color blue #000000;",
	}
	a, path := targetFor(t, "waybar", "colors.css", strings.Join(lines, "\n")+"\n")

	if _, err := a.Apply(testTheme, testPalette); err != nil {
		t.Fatal(err)
	}
	got, _ := os.ReadFile(path)
	if strings.Contains(string(got), "#000000") {
		t.Errorf("a colour survived untouched:\n%s", got)
	}
	if !strings.Contains(string(got), "@define-color bg #"+strings.TrimPrefix(testPalette["bg"], "#")+";") {
		t.Errorf("bg not written from the palette:\n%s", got)
	}
}
