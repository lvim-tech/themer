package theme

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeList(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "themes.txt")
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

// The list carries canonical names; family and variant are derived from them,
// because GTKName is built out of those two parts.
func TestDiscoverSplitsTheCanonicalNames(t *testing.T) {
	path := writeList(t, "LvimEverforest_soft\nLvimNord_dark\nLvimLvim_darker\n")

	got, err := Discover(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 3 {
		t.Fatalf("themes = %d, want 3", len(got))
	}
	// Sorted, so the picker and --list do not reorder between runs.
	if got[0].Name != "LvimEverforest_soft" || got[2].Name != "LvimNord_dark" {
		t.Errorf("order = %q … %q, want them sorted", got[0].Name, got[2].Name)
	}
	if got[0].Family != "everforest" || got[0].Variant != "soft" {
		t.Errorf("split = %q/%q, want everforest/soft", got[0].Family, got[0].Variant)
	}
}

// Blank lines and comments are skipped: the list comes from a generator that
// may well start annotating it, and a stray "#" must not become a theme.
func TestDiscoverSkipsBlanksAndComments(t *testing.T) {
	path := writeList(t, "# generated\n\nLvimNord_dark\n\n  \nLvimNord_soft\n")

	got, err := Discover(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Errorf("themes = %d, want only the two names", len(got))
	}
}

// A name outside the scheme becomes its own family rather than being dropped:
// the list belongs to the generator, and themer is in no position to rule a
// name it publishes invalid.
func TestDiscoverKeepsANameOutsideTheScheme(t *testing.T) {
	got, err := Discover(writeList(t, "Solarized\n"))
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Name != "Solarized" {
		t.Fatalf("themes = %v, want the name kept", got)
	}
	if got[0].Family != "solarized" || got[0].Variant != "" {
		t.Errorf("split = %q/%q, want the whole name as the family", got[0].Family, got[0].Variant)
	}
}

// An empty list is a failure, not an empty picker: it means the fetch answered
// with something that was not the list.
func TestDiscoverRejectsAnEmptyList(t *testing.T) {
	if _, err := Discover(writeList(t, "# only a comment\n")); err == nil {
		t.Error("a list naming no themes was accepted")
	}
}

// Every name is substituted into a filesystem path, so a published name that
// is not a single path component is dropped where it enters — before anything
// downstream builds a path out of it.
func TestValidNameHoldsANameToOnePathComponent(t *testing.T) {
	for _, name := range []string{"LvimNord_dark", "Solarized", "base16.default-dark", "a"} {
		if !ValidName(name) {
			t.Errorf("%q was refused, and it is an ordinary theme name", name)
		}
	}
	for _, name := range []string{
		"../../.config", "a/b", "..", ".hidden", "-flag", "", "with space",
		"nul\x00byte", "~/elsewhere", "/etc/passwd",
	} {
		if ValidName(name) {
			t.Errorf("%q was accepted as a theme name", name)
		}
	}
}

// One unusable name must not take the rest of the list with it, and must not
// reach the picker either.
func TestDiscoverDropsANameItCannotUse(t *testing.T) {
	got, err := Discover(writeList(t, "LvimNord_dark\n../../../etc\nLvimNord_soft\n"))
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("themes = %v, want only the two usable names", got)
	}
	for _, th := range got {
		if !ValidName(th.Name) {
			t.Errorf("%q survived Discover", th.Name)
		}
	}
}

// A list of nothing but unusable names is a failure that has to say what it
// was, or it reads as the generator having published nothing.
func TestDiscoverSaysWhenEveryNameWasRefused(t *testing.T) {
	_, err := Discover(writeList(t, "../../../etc\n/etc/passwd\n"))
	if err == nil {
		t.Fatal("a list of unusable names was accepted")
	}
	if !strings.Contains(err.Error(), "usable") {
		t.Errorf("error = %v, want it to say the names were refused", err)
	}
}

func TestDiscoverReportsAMissingList(t *testing.T) {
	if _, err := Discover(filepath.Join(t.TempDir(), "absent.txt")); err == nil {
		t.Error("a missing list was accepted")
	}
}

func TestGTKNameIsDerived(t *testing.T) {
	tests := []struct{ name, gtk string }{
		{"LvimEverforest_soft", "Lvim-EverforestSoft"},
		// The base family is called base, not lvim, so this is no longer the
		// odd one out: Lvim-BaseDark, by the same rule as every other.
		{"LvimBase_dark", "Lvim-BaseDark"},
	}
	for _, tt := range tests {
		if got := FromName(tt.name).GTKName(); got != tt.gtk {
			t.Errorf("%s: GTKName = %q, want %q", tt.name, got, tt.gtk)
		}
	}
}

func TestByNameFindsAndMisses(t *testing.T) {
	themes := []Theme{FromName("LvimNord_dark"), FromName("LvimNord_soft")}
	if _, ok := ByName(themes, "LvimNord_soft"); !ok {
		t.Error("a theme that is in the list was not found")
	}
	if _, ok := ByName(themes, "LvimNord_light"); ok {
		t.Error("a theme that is not in the list was found")
	}
}
