package sync

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	toml "github.com/pelletier/go-toml/v2"
)

// The whole round trip against a fake GitHub: list the directory, download
// each palette, and land a themes.toml that parses back into the same
// colours.
func TestRunWritesAParsableThemesFile(t *testing.T) {
	mux := http.NewServeMux()
	srv := httptest.NewServer(mux)
	defer srv.Close()

	mux.HandleFunc("/repos/lvim-tech/lvim-gtk/contents/palettes", func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprintf(w, `[
			{"name": "everforest_soft.scss", "download_url": %q},
			{"name": "README.md", "download_url": %q}
		]`, srv.URL+"/raw/everforest_soft.scss", srv.URL+"/raw/README.md")
	})
	mux.HandleFunc("/raw/everforest_soft.scss", func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprint(w, "$style: 'everforest_soft';\n$bg: #2F383E;\n$red: #cb4f4f;\n")
	})

	dest := filepath.Join(t.TempDir(), "themes.toml")
	n, err := Run(srv.URL, "lvim-tech/lvim-gtk", dest)
	if err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("synced %d themes, want 1 — the README must not become a theme", n)
	}

	b, err := os.ReadFile(dest)
	if err != nil {
		t.Fatal(err)
	}
	var got struct {
		Themes []struct {
			Name    string            `toml:"name"`
			Palette map[string]string `toml:"palette"`
		} `toml:"themes"`
	}
	if err := toml.Unmarshal(b, &got); err != nil {
		t.Fatalf("the generated file does not parse: %v\n%s", err, b)
	}
	if len(got.Themes) != 1 || got.Themes[0].Name != "LvimEverforest_soft" {
		t.Fatalf("wrong themes: %+v", got.Themes)
	}
	if got.Themes[0].Palette["bg"] != "#2f383e" {
		t.Errorf("bg = %q, want the lowercased scss value", got.Themes[0].Palette["bg"])
	}
	if !strings.Contains(string(b), "GENERATED") {
		t.Error("the file does not say it is generated")
	}
}

// A dead download must leave no half-written themes.toml behind.
func TestRunLeavesNoFileOnFailure(t *testing.T) {
	mux := http.NewServeMux()
	srv := httptest.NewServer(mux)
	defer srv.Close()
	mux.HandleFunc("/repos/x/y/contents/palettes", func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprintf(w, `[{"name": "a_b.scss", "download_url": %q}]`, srv.URL+"/gone")
	})

	dest := filepath.Join(t.TempDir(), "themes.toml")
	if _, err := Run(srv.URL, "x/y", dest); err == nil {
		t.Fatal("a 404 download passed silently")
	}
	if _, err := os.Stat(dest); !os.IsNotExist(err) {
		t.Error("a failed sync left a themes.toml behind")
	}
}
