package apply

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/lvim-tech/themer/internal/config"
)

// opTarget builds an applier over a bare operation list, with no detect rule
// and nothing built in — the engine under test, not any particular program.
func opTarget(ops ...config.Op) *TargetApplier {
	return NewTarget(config.Target{Name: "test", Ops: ops})
}

// ---------------------------------------------------------------------------
// write
// ---------------------------------------------------------------------------

func TestWriteExpandsThePlaceholdersInBothPathAndContent(t *testing.T) {
	dir := t.TempDir()
	a := opTarget(config.Op{
		Kind:    config.OpWrite,
		File:    filepath.Join(dir, "state", "{theme}.txt"),
		Content: "{theme}\n",
	})

	if _, err := a.Apply(testTheme); err != nil {
		t.Fatal(err)
	}
	// The directory is created too: a target should not have to declare a
	// mkdir operation before every write.
	got, err := os.ReadFile(filepath.Join(dir, "state", "LvimEverforest_soft.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "LvimEverforest_soft\n" {
		t.Errorf("content = %q, want the theme name", got)
	}
}

// ---------------------------------------------------------------------------
// link
// ---------------------------------------------------------------------------

func TestLinkPointsAtTheThemeAndFollowsASwitch(t *testing.T) {
	dir := t.TempDir()
	for _, name := range []string{"LvimEverforest_soft.yml", "LvimNord_dark.yml"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(name), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	dst := filepath.Join(dir, "out", "theme.yml")
	op := config.Op{Kind: config.OpLink, Source: filepath.Join(dir, "{theme}.yml"), Target: dst}

	if _, err := opTarget(op).Apply(testTheme); err != nil {
		t.Fatal(err)
	}
	if got := read(t, dst); got != "LvimEverforest_soft.yml" {
		t.Fatalf("link resolves to %q", got)
	}

	// Switching again has to REPLACE the link: os.Symlink refuses an existing
	// name, so a switch that only tried to create one would silently keep the
	// previous theme.
	other := testTheme
	other.Name, other.Family, other.Variant = "LvimNord_dark", "nord", "dark"
	if _, err := opTarget(op).Apply(other); err != nil {
		t.Fatal(err)
	}
	if got := read(t, dst); got != "LvimNord_dark.yml" {
		t.Errorf("after the second switch the link resolves to %q", got)
	}
}

// A file the user wrote is theirs. The old shell setup scripts got this right
// once per package; the engine has to get it right once.
func TestLinkLeavesARealFileAlone(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "LvimEverforest_soft.yml"), []byte("theme"), 0o644); err != nil {
		t.Fatal(err)
	}
	dst := filepath.Join(dir, "theme.yml")
	if err := os.WriteFile(dst, []byte("mine"), 0o644); err != nil {
		t.Fatal(err)
	}

	note, err := opTarget(config.Op{
		Kind: config.OpLink, Source: filepath.Join(dir, "{theme}.yml"), Target: dst,
	}).Apply(testTheme)
	if err != nil {
		t.Fatal(err)
	}
	if got := read(t, dst); got != "mine" {
		t.Errorf("the user's file was replaced: %q", got)
	}
	if !strings.Contains(note, "kept") {
		t.Errorf("note = %q, want it to say the file was kept", note)
	}
}

func TestLinkFailsWhenTheThemeFileIsMissing(t *testing.T) {
	dir := t.TempDir()
	_, err := opTarget(config.Op{
		Kind: config.OpLink, Source: filepath.Join(dir, "{theme}.yml"), Target: filepath.Join(dir, "out.yml"),
	}).Apply(testTheme)
	if err == nil {
		t.Fatal("linking a theme that does not exist passed silently")
	}
}

// ---------------------------------------------------------------------------
// json-set
// ---------------------------------------------------------------------------

func TestJSONSetKeepsEverySettingItDidNotComeFor(t *testing.T) {
	path := filepath.Join(t.TempDir(), "settings.json")
	if err := os.WriteFile(path, []byte(`{"sidebars":"dark","dim_inactive":true,"colorscheme":"old"}`), 0o644); err != nil {
		t.Fatal(err)
	}

	if _, err := opTarget(config.Op{
		Kind: config.OpJSONSet, File: path, Key: "colorscheme", Value: "{theme}",
	}).Apply(testTheme); err != nil {
		t.Fatal(err)
	}

	var doc map[string]any
	if err := json.Unmarshal([]byte(read(t, path)), &doc); err != nil {
		t.Fatal(err)
	}
	if doc["colorscheme"] != "LvimEverforest_soft" {
		t.Errorf("colorscheme = %v", doc["colorscheme"])
	}
	if doc["sidebars"] != "dark" || doc["dim_inactive"] != true {
		t.Errorf("settings = %v, want the untouched keys preserved", doc)
	}
}

// A document that will not parse holds settings we cannot reconstruct; it is
// reported, never replaced.
func TestJSONSetRefusesADocumentItCannotParse(t *testing.T) {
	path := filepath.Join(t.TempDir(), "settings.json")
	if err := os.WriteFile(path, []byte("{ not json"), 0o644); err != nil {
		t.Fatal(err)
	}

	if _, err := opTarget(config.Op{
		Kind: config.OpJSONSet, File: path, Key: "colorscheme", Value: "{theme}",
	}).Apply(testTheme); err == nil {
		t.Fatal("an unparsable document was accepted")
	}
	if got := read(t, path); got != "{ not json" {
		t.Errorf("the document was rewritten: %q", got)
	}
}

// ---------------------------------------------------------------------------
// copy-tree
// ---------------------------------------------------------------------------

func TestCopyTreeReplacesWhatWasThereBefore(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "src", "assets")
	if err := os.MkdirAll(src, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(src, "check.svg"), []byte("<svg/>"), 0o644); err != nil {
		t.Fatal(err)
	}
	dst := filepath.Join(dir, "out", "assets")
	if err := os.MkdirAll(dst, 0o755); err != nil {
		t.Fatal(err)
	}
	// An asset only the PREVIOUS theme carried: it belongs to the theme, so a
	// switch must take it away rather than leave a mixed directory.
	if err := os.WriteFile(filepath.Join(dst, "stale.svg"), []byte("old"), 0o644); err != nil {
		t.Fatal(err)
	}

	if _, err := opTarget(config.Op{Kind: config.OpCopyTree, Source: src, Target: dst}).
		Apply(testTheme); err != nil {
		t.Fatal(err)
	}
	if got := read(t, filepath.Join(dst, "check.svg")); got != "<svg/>" {
		t.Errorf("asset not copied: %q", got)
	}
	if _, err := os.Stat(filepath.Join(dst, "stale.svg")); err == nil {
		t.Error("the previous theme's asset survived the switch")
	}
}

// ---------------------------------------------------------------------------
// ordering and failure
// ---------------------------------------------------------------------------

// The whole reason ops are one ordered list: a target's steps are one switch,
// and a failure halfway must not let the rest run.
func TestOperationsStopAtTheFirstFailure(t *testing.T) {
	dir := t.TempDir()
	after := filepath.Join(dir, "after.txt")

	_, err := opTarget(
		config.Op{Kind: config.OpWrite, File: filepath.Join(dir, "before.txt"), Content: "1"},
		config.Op{Kind: config.OpLink, Source: filepath.Join(dir, "missing-{theme}"), Target: filepath.Join(dir, "l")},
		config.Op{Kind: config.OpWrite, File: after, Content: "2"},
	).Apply(testTheme)

	if err == nil {
		t.Fatal("the failing operation was not reported")
	}
	if _, err := os.Stat(filepath.Join(dir, "before.txt")); err != nil {
		t.Error("the operation before the failure did not run")
	}
	if _, err := os.Stat(after); err == nil {
		t.Error("an operation after the failure ran anyway")
	}
}

// Every operation contributes to the note, in order, so the TUI line says what
// actually happened rather than only what happened last.
func TestApplyReportsEveryOperationInOrder(t *testing.T) {
	dir := t.TempDir()
	note, err := opTarget(
		config.Op{Kind: config.OpWrite, File: filepath.Join(dir, "a"), Content: "1"},
		config.Op{Kind: config.OpWrite, File: filepath.Join(dir, "b"), Content: "2"},
	).Apply(testTheme)
	if err != nil {
		t.Fatal(err)
	}
	if i, j := strings.Index(note, "/a"), strings.Index(note, "/b"); i < 0 || j < 0 || i > j {
		t.Errorf("note = %q, want both writes named in order", note)
	}
}

// ---------------------------------------------------------------------------
// desugaring
// ---------------------------------------------------------------------------

// The old spelling has to reach the same execution path, in the same order it
// always ran: every edit, then every reload.
func TestDesugarKeepsEditsBeforeReloads(t *testing.T) {
	got := config.Target{
		Name:   "x",
		Edits:  []config.Edit{{File: "f", Rules: []config.Rule{{Regex: "a", Value: "b"}}}},
		Reload: []config.Reload{{Signal: "USR1", Process: "p"}},
	}.Desugar()

	if len(got.Ops) != 2 {
		t.Fatalf("ops = %d, want the edit and the reload", len(got.Ops))
	}
	if got.Ops[0].Kind != config.OpRewrite || got.Ops[1].Kind != config.OpSignal {
		t.Errorf("kinds = %q, %q", got.Ops[0].Kind, got.Ops[1].Kind)
	}
	// Left in place, the old fields would run a second time through whatever
	// still reads them.
	if got.Edits != nil || got.Reload != nil {
		t.Error("the desugared spelling was left behind as well")
	}
}

// A target built anywhere — a built-in list, a test, a future caller — must
// carry its operations. One that skipped desugaring would apply nothing and
// report success.
func TestNewTargetDesugarsWhateverItIsGiven(t *testing.T) {
	a := NewTarget(config.Target{
		Name:  "x",
		Edits: []config.Edit{{File: "f", Rules: []config.Rule{{Regex: "a", Value: "b"}}}},
	})
	if len(a.t.Ops) != 1 {
		t.Errorf("ops = %d, want the edit desugared", len(a.t.Ops))
	}
}

// ---------------------------------------------------------------------------
// validation
// ---------------------------------------------------------------------------

// A missing field is a configuration bug. Finding it at load beats finding it
// halfway through a switch with the desktop already half changed.
func TestValidateRejectsIncompleteOperations(t *testing.T) {
	tests := []struct {
		name string
		op   config.Op
		want string
	}{
		{"rewrite without rules", config.Op{Kind: config.OpRewrite, File: "f"}, "rules"},
		{"rewrite without file", config.Op{Kind: config.OpRewrite}, "file"},
		{"write without file", config.Op{Kind: config.OpWrite}, "file"},
		{"json-set without key", config.Op{Kind: config.OpJSONSet, File: "f"}, "key"},
		{"link without target", config.Op{Kind: config.OpLink, Source: "s"}, "target"},
		{"copy-tree without source", config.Op{Kind: config.OpCopyTree, Target: "t"}, "source"},
		{"command without argv", config.Op{Kind: config.OpCommand}, "command"},
		{"signal without process", config.Op{Kind: config.OpSignal, Signal: "USR1"}, "process"},
		{"no kind at all", config.Op{}, "no kind"},
		{"unknown kind", config.Op{Kind: "teleport"}, "unknown operation kind"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.op.Validate("mytarget")
			if err == nil {
				t.Fatal("Validate() = nil, want an error")
			}
			if !strings.Contains(err.Error(), tt.want) {
				t.Errorf("error = %v, want it to mention %q", err, tt.want)
			}
			if !strings.Contains(err.Error(), "mytarget") {
				t.Errorf("error = %v, want it to name the target", err)
			}
		})
	}
}

func TestValidateAcceptsTheCompleteForms(t *testing.T) {
	ok := []config.Op{
		{Kind: config.OpRewrite, File: "f", Rules: []config.Rule{{Regex: "a", Value: "b"}}},
		{Kind: config.OpWrite, File: "f"},
		{Kind: config.OpJSONSet, File: "f", Key: "k"},
		{Kind: config.OpLink, Source: "s", Target: "t"},
		{Kind: config.OpCopyTree, Source: "s", Target: "t"},
		{Kind: config.OpCommand, Command: []string{"true"}},
		{Kind: config.OpSignal, Signal: "USR1", Process: "p"},
	}
	for _, op := range ok {
		if err := op.Validate("x"); err != nil {
			t.Errorf("%s: %v", op.Kind, err)
		}
	}
}

// ---------------------------------------------------------------------------
// fetch
// ---------------------------------------------------------------------------

// The theme file is named after the theme in the repository, so {theme} has to
// expand in the URL as well as in the destination.
func TestFetchExpandsTheThemeIntoTheURL(t *testing.T) {
	var asked string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		asked = r.URL.Path
		fmt.Fprint(w, "theme body")
	}))
	defer srv.Close()

	dst := filepath.Join(t.TempDir(), "out", "theme.yml")
	if _, err := opTarget(config.Op{
		Kind: config.OpFetch, Source: srv.URL + "/extras/eza/{theme}.yml", Target: dst,
	}).Apply(testTheme); err != nil {
		t.Fatal(err)
	}

	if asked != "/extras/eza/LvimEverforest_soft.yml" {
		t.Errorf("requested %q, want the theme name expanded", asked)
	}
	if got := read(t, dst); got != "theme body" {
		t.Errorf("body = %q", got)
	}
}

// A switch that cannot reach the theme must leave the previous one in place:
// half a theme is worse than the one that was working.
func TestFetchLeavesThePreviousThemeOnFailure(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "gone", http.StatusNotFound)
	}))
	defer srv.Close()

	dst := filepath.Join(t.TempDir(), "theme.yml")
	if err := os.WriteFile(dst, []byte("previous"), 0o644); err != nil {
		t.Fatal(err)
	}

	_, err := opTarget(config.Op{Kind: config.OpFetch, Source: srv.URL + "/x", Target: dst}).
		Apply(testTheme)
	if err == nil {
		t.Fatal("a 404 was accepted as a theme")
	}
	if got := read(t, dst); got != "previous" {
		t.Errorf("the previous theme was destroyed: %q", got)
	}
	// The temporary file must not survive either — a stray .tmp beside every
	// theme is how a directory turns into litter.
	if _, err := os.Stat(dst + ".tmp"); err == nil {
		t.Error("the temporary file was left behind")
	}
}

func TestFetchCreatesTheDestinationDirectory(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, "x")
	}))
	defer srv.Close()

	dst := filepath.Join(t.TempDir(), "skins", "deep", "{theme}.yaml")
	if _, err := opTarget(config.Op{Kind: config.OpFetch, Source: srv.URL + "/x", Target: dst}).
		Apply(testTheme); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(filepath.Dir(dst), "LvimEverforest_soft.yaml")); err != nil {
		t.Errorf("the destination directory was not created: %v", err)
	}
}

// ---------------------------------------------------------------------------
// detect: match
// ---------------------------------------------------------------------------

// A configuration can be out of scope rather than absent. wezterm.lua running
// on a hand-built palette with color_scheme commented out is a deliberate
// choice: it is a Skip with its reason, not a failure — and without the match
// the rewrite would run, fail on "matched nothing", and report the choice as a
// broken target.
func TestDetectMatchSkipsWithTheReasonItWasGiven(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "wezterm.lua")
	if err := os.WriteFile(path, []byte("  colors = custom\n  -- color_scheme = \"x\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	a := NewTarget(config.Target{
		Name: "wezterm",
		Detect: config.Detect{
			File:   path,
			Match:  `^(\s*)color_scheme(\s*)=(\s*)"[^"]*"`,
			Reason: "runs on a hand-built palette (colors = custom), left alone",
		},
	})

	ok, why := a.Detect()
	if ok {
		t.Fatal("Detect() = true for a config with no active color_scheme line")
	}
	if !strings.Contains(why, "hand-built palette") {
		t.Errorf("reason = %q, want the one the target supplied", why)
	}
}

func TestDetectMatchPassesWhenTheLineIsThere(t *testing.T) {
	path := filepath.Join(t.TempDir(), "wezterm.lua")
	if err := os.WriteFile(path, []byte("  color_scheme = \"LvimNord_dark\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	a := NewTarget(config.Target{
		Name:   "wezterm",
		Detect: config.Detect{File: path, Match: `^(\s*)color_scheme(\s*)=(\s*)"[^"]*"`},
	})

	if ok, why := a.Detect(); !ok {
		t.Errorf("Detect() = false (%s), want the active line to pass", why)
	}
}
