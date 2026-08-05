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
	"github.com/lvim-tech/themer/internal/theme"
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

// ---------------------------------------------------------------------------
// per-instance themes
// ---------------------------------------------------------------------------

// A definition that writes a theme name where {theme} would go has always been
// pinned to it — expandTheme touches only {…}, so a literal passes through.
// Stated here as a test rather than as a claim, because the whole design of
// "one file per instance" rests on it.
func TestALiteralThemeNamePassesThroughUnchanged(t *testing.T) {
	dir := t.TempDir()
	if _, err := opTarget(config.Op{
		Kind:    config.OpWrite,
		File:    filepath.Join(dir, "LvimNord_dark.txt"),
		Content: "LvimNord_dark",
	}).Apply(testTheme); err != nil {
		t.Fatal(err)
	}
	// testTheme is LvimEverforest_soft; nothing about it may reach the file.
	if got := read(t, filepath.Join(dir, "LvimNord_dark.txt")); got != "LvimNord_dark" {
		t.Errorf("content = %q, want the literal name", got)
	}
}

// The pin, said once on the target instead of in every operation: two neovims,
// one dark and one light, are two definitions that disagree about the theme.
func TestAPinnedTargetIgnoresTheSwitchedTheme(t *testing.T) {
	dir := t.TempDir()
	a := NewTarget(config.Target{
		Name:  "second instance",
		Theme: "LvimNord_dark",
		Ops: []config.Op{{
			Kind: config.OpWrite, File: filepath.Join(dir, "{theme}.txt"), Content: "{theme}",
		}},
	})

	if _, err := a.Apply(testTheme); err != nil {
		t.Fatal(err)
	}
	if got := read(t, filepath.Join(dir, "LvimNord_dark.txt")); got != "LvimNord_dark" {
		t.Errorf("content = %q, want the pinned theme", got)
	}
	if _, err := os.Stat(filepath.Join(dir, "LvimEverforest_soft.txt")); err == nil {
		t.Error("the switched theme reached a pinned target")
	}
}

// The prefetch has to ask for the URL the target will actually want, or it
// downloads one file and the target then downloads another.
func TestAPinnedTargetPrefetchesItsOwnTheme(t *testing.T) {
	a := NewTarget(config.Target{
		Name:  "pinned",
		Theme: "LvimNord_dark",
		Ops:   []config.Op{{Kind: config.OpFetch, Source: "https://example.invalid/{theme}.yml", Target: "/tmp/x"}},
	})

	urls := a.URLs(testTheme)
	if len(urls) != 1 || urls[0] != "https://example.invalid/LvimNord_dark.yml" {
		t.Errorf("URLs = %v, want the pinned theme's", urls)
	}
}

// Following the switch WITHOUT following it exactly: the family comes from
// whatever was switched to, the variant is the definition's own choice.
func TestFamilyAndVariantExpandSeparately(t *testing.T) {
	dir := t.TempDir()
	if _, err := opTarget(config.Op{
		Kind:    config.OpWrite,
		File:    filepath.Join(dir, "out.txt"),
		Content: "{theme} {family} {Family} {variant} {Variant}",
	}).Apply(testTheme); err != nil {
		t.Fatal(err)
	}
	want := "LvimEverforest_soft everforest Everforest soft Soft"
	if got := read(t, filepath.Join(dir, "out.txt")); got != want {
		t.Errorf("expanded to %q, want %q", got, want)
	}
}

// The case it exists for: a terminal that has to stay readable while the rest
// of the desktop goes dark.
func TestAVariantShiftedTargetFollowsTheFamily(t *testing.T) {
	dir := t.TempDir()
	if _, err := opTarget(config.Op{
		Kind: config.OpWrite, File: filepath.Join(dir, "Lvim{Family}_light.txt"), Content: "x",
	}).Apply(testTheme); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, "LvimEverforest_light.txt")); err != nil {
		t.Errorf("wanted the light variant of the switched family: %v", err)
	}
}

// An unknown placeholder is still an error: a rule that wrote "{focus}" into a
// config file would look like it had worked.
func TestAnUnknownPlaceholderIsStillRefused(t *testing.T) {
	dir := t.TempDir()
	_, err := opTarget(config.Op{
		Kind: config.OpWrite, File: filepath.Join(dir, "x"), Content: "{focus}",
	}).Apply(testTheme)
	if err == nil || !strings.Contains(err.Error(), "focus") {
		t.Errorf("error = %v, want the unknown placeholder named", err)
	}
}

// A target may name a binary by full path — tmux does, because it is installed
// outside the PATH a compositor keybind carries. Handed to exec with a literal
// tilde, that path finds nothing.
func TestTildeExpandsInCommandArguments(t *testing.T) {
	dir := t.TempDir()
	script := filepath.Join(dir, "touch.sh")
	marker := filepath.Join(dir, "ran")
	if err := os.WriteFile(script, []byte("#!/bin/sh\ntouch \"$1\"\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOME", dir)

	if _, err := opTarget(config.Op{
		Kind: config.OpCommand, Command: []string{"~/touch.sh", "~/ran"},
	}).Apply(testTheme); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(marker); err != nil {
		t.Errorf("the command did not run with expanded paths: %v", err)
	}
}

// ---------------------------------------------------------------------------
// assemble
// ---------------------------------------------------------------------------

func TestAssembleJoinsThePartsInOrder(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "a"), []byte("first\n"), 0o644)
	os.WriteFile(filepath.Join(dir, "LvimEverforest_soft.part"), []byte("second\n"), 0o644)
	dst := filepath.Join(dir, "out", "joined")

	if _, err := opTarget(config.Op{
		Kind:    config.OpAssemble,
		Sources: []string{filepath.Join(dir, "a"), filepath.Join(dir, "{theme}.part")},
		Target:  dst,
	}).Apply(testTheme); err != nil {
		t.Fatal(err)
	}
	if got := read(t, dst); got != "first\nsecond\n" {
		t.Errorf("assembled = %q, want the parts in order", got)
	}
}

// A part that does not end in a newline must not run into the next one: the
// last line of the user's config and the first of the palette would become a
// single, broken line.
func TestAssembleSeparatesPartsThatLackATrailingNewline(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "a"), []byte("theme:"), 0o644)
	os.WriteFile(filepath.Join(dir, "b"), []byte("  fg: red\n"), 0o644)
	dst := filepath.Join(dir, "out")

	if _, err := opTarget(config.Op{
		Kind: config.OpAssemble, Sources: []string{filepath.Join(dir, "a"), filepath.Join(dir, "b")}, Target: dst,
	}).Apply(testTheme); err != nil {
		t.Fatal(err)
	}
	if got := read(t, dst); got != "theme:\n  fg: red\n" {
		t.Errorf("assembled = %q, want the parts on separate lines", got)
	}
}

// The marker is the permission to overwrite. A file someone wrote by hand does
// not carry it, and editing the product instead of the parts is a mistake that
// must not be answered by destroying the edit.
func TestAssembleLeavesAHandWrittenTargetAlone(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "a"), []byte("generated\n"), 0o644)
	dst := filepath.Join(dir, "out")
	os.WriteFile(dst, []byte("mine, edited by hand\n"), 0o644)

	note, err := opTarget(config.Op{
		Kind: config.OpAssemble, Sources: []string{filepath.Join(dir, "a")},
		Target: dst, Marker: "# ASSEMBLED",
	}).Apply(testTheme)
	if err != nil {
		t.Fatal(err)
	}
	if got := read(t, dst); got != "mine, edited by hand\n" {
		t.Errorf("the hand-written file was replaced: %q", got)
	}
	if !strings.Contains(note, "kept") {
		t.Errorf("note = %q, want it to say the file was kept", note)
	}
}

// Its own previous output does carry the marker, so a second switch replaces it.
func TestAssembleReplacesItsOwnPreviousOutput(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "a"), []byte("new\n"), 0o644)
	dst := filepath.Join(dir, "out")
	os.WriteFile(dst, []byte("# ASSEMBLED\nold\n"), 0o644)

	if _, err := opTarget(config.Op{
		Kind: config.OpAssemble, Sources: []string{filepath.Join(dir, "a")},
		Target: dst, Marker: "# ASSEMBLED",
	}).Apply(testTheme); err != nil {
		t.Fatal(err)
	}
	if got := read(t, dst); got != "# ASSEMBLED\nnew\n" {
		t.Errorf("assembled = %q, want it replaced and marked", got)
	}
}

// A missing part is a failure, not a shorter file: half a configuration reads
// as a working one.
func TestAssembleFailsOnAMissingPartAndKeepsThePrevious(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "a"), []byte("first\n"), 0o644)
	dst := filepath.Join(dir, "out")
	os.WriteFile(dst, []byte("# ASSEMBLED\nprevious\n"), 0o644)

	_, err := opTarget(config.Op{
		Kind: config.OpAssemble, Sources: []string{filepath.Join(dir, "a"), filepath.Join(dir, "absent")},
		Target: dst, Marker: "# ASSEMBLED",
	}).Apply(testTheme)
	if err == nil {
		t.Fatal("a missing part was accepted")
	}
	if got := read(t, dst); got != "# ASSEMBLED\nprevious\n" {
		t.Errorf("the previous output was destroyed: %q", got)
	}
}

// zed names its theme at theme.dark, inside an object that also carries mode
// and the light choice. Setting the object wholesale would take both with it.
func TestJSONSetReachesANestedKey(t *testing.T) {
	path := filepath.Join(t.TempDir(), "settings.json")
	os.WriteFile(path, []byte(`{"ui_font_size":16,"theme":{"mode":"system","light":"One Light","dark":"Sandcastle"}}`), 0o644)

	if _, err := opTarget(config.Op{
		Kind: config.OpJSONSet, File: path, Key: "theme.dark", Value: "{theme}",
	}).Apply(testTheme); err != nil {
		t.Fatal(err)
	}

	var doc map[string]any
	if err := json.Unmarshal([]byte(read(t, path)), &doc); err != nil {
		t.Fatal(err)
	}
	theme, _ := doc["theme"].(map[string]any)
	if theme["dark"] != "LvimEverforest_soft" {
		t.Errorf("theme.dark = %v", theme["dark"])
	}
	if theme["mode"] != "system" || theme["light"] != "One Light" {
		t.Errorf("the siblings were lost: %v", theme)
	}
	if doc["ui_font_size"] == nil {
		t.Error("an unrelated top-level setting was lost")
	}
}

// A path may be created where the document has none yet.
func TestJSONSetCreatesMissingObjectsAlongThePath(t *testing.T) {
	path := filepath.Join(t.TempDir(), "settings.json")
	os.WriteFile(path, []byte(`{"ui_font_size":16}`), 0o644)

	if _, err := opTarget(config.Op{
		Kind: config.OpJSONSet, File: path, Key: "theme.dark", Value: "x",
	}).Apply(testTheme); err != nil {
		t.Fatal(err)
	}
	var doc map[string]any
	json.Unmarshal([]byte(read(t, path)), &doc)
	if doc["theme"].(map[string]any)["dark"] != "x" {
		t.Errorf("doc = %v", doc)
	}
}

// A scalar where the path expects an object means the document is not shaped
// the way the definition believes. Overwriting it would destroy a setting.
func TestJSONSetRefusesToWalkThroughAScalar(t *testing.T) {
	path := filepath.Join(t.TempDir(), "settings.json")
	os.WriteFile(path, []byte(`{"theme":"One Light"}`), 0o644)

	_, err := opTarget(config.Op{
		Kind: config.OpJSONSet, File: path, Key: "theme.dark", Value: "x",
	}).Apply(testTheme)
	if err == nil {
		t.Fatal("a scalar in the path was walked through")
	}
	if got := read(t, path); got != `{"theme":"One Light"}` {
		t.Errorf("the document was rewritten: %q", got)
	}
}

// ---------------------------------------------------------------------------
// appearance
// ---------------------------------------------------------------------------

// {appearance} exists because everything that asks the question knows two
// answers while a theme has four variants. The case that matters is `soft`:
// {variant} would write the word "soft" into a setting whose only legal values
// are dark and light, which is how a config comes to be silently invalid.
func TestAppearanceReducesTheFourVariantsToTwo(t *testing.T) {
	for _, c := range []struct {
		variant string
		want    string
	}{
		{"dark", "dark"},
		{"darker", "dark"},
		{"soft", "dark"},
		{"light", "light"},
	} {
		got, err := expandTheme("prefer-{appearance}", theme.Theme{
			Name: "LvimNord_" + c.variant, Family: "nord", Variant: c.variant,
		})
		if err != nil {
			t.Fatalf("%s: %v", c.variant, err)
		}
		if want := "prefer-" + c.want; got != want {
			t.Errorf("%s expanded to %q, want %q", c.variant, got, want)
		}
	}
}

// The capitalised form, for the settings that spell it that way.
func TestAppearanceCapitalisesLikeTheOtherPlaceholders(t *testing.T) {
	got, err := expandTheme("{Appearance}", testTheme) // LvimEverforest_soft
	if err != nil {
		t.Fatal(err)
	}
	if got != "Dark" {
		t.Errorf("{Appearance} = %q, want %q", got, "Dark")
	}
}
