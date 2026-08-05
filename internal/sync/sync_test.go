package sync

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRunWritesTheNamesSorted(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, "# generated\nLvimNord_soft\n\nLvimEverforest_dark\n")
	}))
	defer srv.Close()

	dest := filepath.Join(t.TempDir(), "sub", "themes.txt")
	n, err := Run(srv.URL, dest)
	if err != nil {
		t.Fatal(err)
	}
	if n != 2 {
		t.Errorf("n = %d, want 2", n)
	}
	// Sorted here rather than trusted from the wire, so --list has the same
	// order whatever the generator happened to emit.
	if got := read(t, dest); got != "LvimEverforest_dark\nLvimNord_soft\n" {
		t.Errorf("list = %q", got)
	}
}

// A sync that fails must leave the previous list intact: without it themer has
// no themes at all, which is worse than an out-of-date list.
func TestRunLeavesTheOldListOnFailure(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "nope", http.StatusNotFound)
	}))
	defer srv.Close()

	dest := filepath.Join(t.TempDir(), "themes.txt")
	if err := os.WriteFile(dest, []byte("LvimNord_dark\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	if _, err := Run(srv.URL, dest); err == nil {
		t.Fatal("a 404 was accepted as a theme list")
	}
	if got := read(t, dest); got != "LvimNord_dark\n" {
		t.Errorf("the previous list was destroyed: %q", got)
	}
	if _, err := os.Stat(dest + ".tmp"); err == nil {
		t.Error("the temporary file was left behind")
	}
}

// An empty body is far more likely a redirect or an error page than a
// generator with no themes, and writing it out would empty a working install.
func TestRunRefusesAnEmptyAnswer(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, "\n\n# nothing\n")
	}))
	defer srv.Close()

	dest := filepath.Join(t.TempDir(), "themes.txt")
	if _, err := Run(srv.URL, dest); err == nil {
		t.Fatal("an empty list was accepted")
	}
	if _, err := os.Stat(dest); err == nil {
		t.Error("an empty list was written anyway")
	}
}

// themer ships no themes, so an unconfigured URL is the normal first-run state
// and has to say so rather than fetching nothing from nowhere.
func TestRunWithoutAURLSaysSo(t *testing.T) {
	_, err := Run("", filepath.Join(t.TempDir(), "themes.txt"))
	if err == nil || !strings.Contains(err.Error(), "themes_url") {
		t.Errorf("error = %v, want it to name themes_url", err)
	}
}

func read(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}
