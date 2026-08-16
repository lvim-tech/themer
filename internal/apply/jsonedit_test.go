package apply

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/lvim-tech/themer/internal/config"
)

// zed's settings.json is JSONC: its own default file ships with // comments,
// and encoding/json rejects those. Parsing the document instead of editing it
// made json-set fail every time on the one program it was written for.
func TestJSONSetEditsADocumentWithComments(t *testing.T) {
	path := filepath.Join(t.TempDir(), "settings.json")
	doc := `{
  // the theme is set by themer
  "theme": {
    "mode": "system",
    "dark": "Sandcastle", // replaced
    "light": "One Light"
  },
  /* the rest is mine */
  "ui_font_size": 16,
}
`
	if err := os.WriteFile(path, []byte(doc), 0o644); err != nil {
		t.Fatal(err)
	}

	if _, err := opTarget(config.Op{
		Kind: config.OpJSONSet, File: path, Key: "theme.dark", Value: "{theme}",
	}).Apply(testTheme); err != nil {
		t.Fatal(err)
	}

	got := read(t, path)
	if !strings.Contains(got, `"dark": "LvimEverforest_soft",`) {
		t.Errorf("the key was not set:\n%s", got)
	}
	// Everything the operation did not come for is still there, byte for byte:
	// the comments, the trailing comma, the indentation and the key order.
	want := strings.Replace(doc, `"Sandcastle"`, `"LvimEverforest_soft"`, 1)
	if got != want {
		t.Errorf("the document was rewritten:\n%s\nwant:\n%s", got, want)
	}
}

// A pretty-printed document stays pretty-printed. Marshalling it back put every
// setting on one line, in Go's map order, on the first switch.
func TestJSONSetKeepsFormattingAndOrder(t *testing.T) {
	path := filepath.Join(t.TempDir(), "settings.json")
	doc := "{\n  \"z_last\": 1,\n  \"colorscheme\": \"old\",\n  \"a_first\": 2\n}\n"
	if err := os.WriteFile(path, []byte(doc), 0o644); err != nil {
		t.Fatal(err)
	}

	if _, err := opTarget(config.Op{
		Kind: config.OpJSONSet, File: path, Key: "colorscheme", Value: "new",
	}).Apply(testTheme); err != nil {
		t.Fatal(err)
	}
	want := strings.Replace(doc, `"old"`, `"new"`, 1)
	if got := read(t, path); got != want {
		t.Errorf("document = %q, want %q", got, want)
	}
}

// Numbers are not ours to re-spell. Through a map they all became float64, so
// an integer past 2^53 came back changed and 1e3 came back as 1000.
func TestJSONSetLeavesNumbersAlone(t *testing.T) {
	path := filepath.Join(t.TempDir(), "settings.json")
	doc := `{"id":123456789012345678901,"scale":1e3,"colorscheme":"old"}`
	if err := os.WriteFile(path, []byte(doc), 0o644); err != nil {
		t.Fatal(err)
	}

	if _, err := opTarget(config.Op{
		Kind: config.OpJSONSet, File: path, Key: "colorscheme", Value: "new",
	}).Apply(testTheme); err != nil {
		t.Fatal(err)
	}
	if got := read(t, path); got != `{"id":123456789012345678901,"scale":1e3,"colorscheme":"new"}` {
		t.Errorf("document = %q", got)
	}
}

// A key the document has never had is inserted where the object's own members
// are, so the file stays readable rather than gaining a line at column one.
func TestJSONSetInsertsAMissingKeyIndented(t *testing.T) {
	path := filepath.Join(t.TempDir(), "settings.json")
	if err := os.WriteFile(path, []byte("{\n  \"ui_font_size\": 16\n}\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	if _, err := opTarget(config.Op{
		Kind: config.OpJSONSet, File: path, Key: "theme.dark", Value: "x",
	}).Apply(testTheme); err != nil {
		t.Fatal(err)
	}
	want := "{\n  \"theme\": {\"dark\": \"x\"},\n  \"ui_font_size\": 16\n}\n"
	if got := read(t, path); got != want {
		t.Errorf("document = %q, want %q", got, want)
	}
}

// A file that is not there yet is a document to write, not a failure: a program
// configured entirely by themer has no settings.json until the first switch.
func TestJSONSetWritesADocumentThatDoesNotExistYet(t *testing.T) {
	path := filepath.Join(t.TempDir(), "new", "settings.json")

	if _, err := opTarget(config.Op{
		Kind: config.OpJSONSet, File: path, Key: "theme.dark", Value: "x",
	}).Apply(testTheme); err != nil {
		t.Fatal(err)
	}
	var doc map[string]any
	if err := json.Unmarshal([]byte(read(t, path)), &doc); err != nil {
		t.Fatal(err)
	}
	if doc["theme"].(map[string]any)["dark"] != "x" {
		t.Errorf("doc = %v", doc)
	}
}

// A document rewritten for one key must not come back readable to everyone: the
// permissions it had are the ones its owner chose.
func TestJSONSetKeepsThePermissionsTheFileHad(t *testing.T) {
	path := filepath.Join(t.TempDir(), "settings.json")
	if err := os.WriteFile(path, []byte(`{"colorscheme":"old"}`), 0o600); err != nil {
		t.Fatal(err)
	}

	if _, err := opTarget(config.Op{
		Kind: config.OpJSONSet, File: path, Key: "colorscheme", Value: "new",
	}).Apply(testTheme); err != nil {
		t.Fatal(err)
	}
	fi, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if fi.Mode().Perm() != 0o600 {
		t.Errorf("mode = %v, want 0600", fi.Mode().Perm())
	}
}

// The scanner has to reach the key past everything it does not care about, and
// has to say so rather than guess when the document is not a document.
func TestFindJSONKeyWalksAndRefuses(t *testing.T) {
	tests := []struct {
		name, src string
		path      []string
		value     string // what the located span holds, when there is one
		found     bool
		wantErr   bool
	}{
		{name: "past an array", src: `{"a":[1,{"b":2}],"k":"v"}`, path: []string{"k"}, value: `"v"`, found: true},
		{name: "past a nested object", src: `{"a":{"k":"no"},"k":"yes"}`, path: []string{"k"}, value: `"yes"`, found: true},
		{name: "past an escaped string", src: `{"a":"}\"","k":1}`, path: []string{"k"}, value: `1`, found: true},
		{name: "a key spelled with an escape", src: `{"key":true}`, path: []string{"key"}, value: `true`, found: true},
		{name: "missing key", src: `{"a":1}`, path: []string{"k"}},
		{name: "not an object", src: `[1,2]`, path: []string{"k"}, wantErr: true},
		{name: "truncated", src: `{ not json`, path: []string{"k"}, wantErr: true},
		{name: "unterminated string", src: `{"k":"v`, path: []string{"k"}, wantErr: true},
		{name: "scalar in the path", src: `{"k":1}`, path: []string{"k", "deeper"}, wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			spot, err := findJSONKey([]byte(tt.src), tt.path)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("spot = %+v, want an error", spot)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if spot.found != tt.found {
				t.Fatalf("found = %v, want %v", spot.found, tt.found)
			}
			if spot.found {
				if got := tt.src[spot.start:spot.end]; got != tt.value {
					t.Errorf("value = %q, want %q", got, tt.value)
				}
			}
		})
	}
}
