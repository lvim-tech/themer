package theme

import (
	"os"
	"path/filepath"
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
// because GTKName and NvimName are built out of those two parts.
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

func TestDiscoverReportsAMissingList(t *testing.T) {
	if _, err := Discover(filepath.Join(t.TempDir(), "absent.txt")); err == nil {
		t.Error("a missing list was accepted")
	}
}

func TestGTKAndNvimNamesAreDerived(t *testing.T) {
	tests := []struct{ name, gtk, nvim string }{
		{"LvimEverforest_soft", "Lvim-EverforestSoft", "lvim-everforest-soft"},
		// The house palette is lvim_dark and its colorscheme is lvim-dark:
		// the family is dropped when it IS lvim.
		{"LvimLvim_dark", "Lvim-LvimDark", "lvim-dark"},
	}
	for _, tt := range tests {
		got := FromName(tt.name)
		if got.GTKName() != tt.gtk {
			t.Errorf("%s: GTKName = %q, want %q", tt.name, got.GTKName(), tt.gtk)
		}
		if got.NvimName() != tt.nvim {
			t.Errorf("%s: NvimName = %q, want %q", tt.name, got.NvimName(), tt.nvim)
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
