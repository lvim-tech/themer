package apply

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/lvim-tech/themer/internal/config"
)

// ---------------------------------------------------------------------------
// copy
// ---------------------------------------------------------------------------

// keep and unless are the pair the operation exists for: without keep, the
// first switch destroys a stylesheet somebody else wrote; without unless, the
// second switch "rescues" the theme the first one installed and the real
// rescue is lost. The rescue is written once and never overwritten.
func TestCopyRescuesTheUsersFileOnceAndOnlyOnce(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "theme.css")
	dst := filepath.Join(dir, "gtk.css")
	keep := filepath.Join(dir, "kept", "gtk.css.orig")
	if err := os.WriteFile(src, []byte("/* Lvim */ theirs\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(dst, []byte("mine\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	op := config.Op{Kind: config.OpCopy, Source: src, Target: dst, Keep: keep, Unless: "Lvim"}
	if _, err := opTarget(op).Apply(testTheme); err != nil {
		t.Fatal(err)
	}
	if got := read(t, dst); got != "/* Lvim */ theirs\n" {
		t.Errorf("destination = %q, want the copied file", got)
	}
	if got := read(t, keep); got != "mine\n" {
		t.Errorf("kept = %q, want what was there before", got)
	}

	// A second switch finds our own marker in the destination and rescues
	// nothing, so the first rescue survives.
	if err := os.WriteFile(src, []byte("/* Lvim */ a different theme\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := opTarget(op).Apply(testTheme); err != nil {
		t.Fatal(err)
	}
	if got := read(t, keep); got != "mine\n" {
		t.Errorf("kept = %q — the rescue was overwritten by our own file", got)
	}
}

// Even without the marker, a rescue that already exists is not written over:
// the first one is the user's file, and every later one is ours.
func TestCopyDoesNotOverwriteAnExistingRescue(t *testing.T) {
	dir := t.TempDir()
	src, dst, keep := filepath.Join(dir, "src"), filepath.Join(dir, "dst"), filepath.Join(dir, "keep")
	for path, body := range map[string]string{src: "new\n", dst: "second\n", keep: "first\n"} {
		if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	if _, err := opTarget(config.Op{
		Kind: config.OpCopy, Source: src, Target: dst, Keep: keep,
	}).Apply(testTheme); err != nil {
		t.Fatal(err)
	}
	if got := read(t, keep); got != "first\n" {
		t.Errorf("kept = %q, want the first rescue", got)
	}
}

// A source that is not there is a loud failure: the destination it would have
// written is a file some program is about to read.
func TestCopyReportsAMissingSource(t *testing.T) {
	dir := t.TempDir()
	_, err := opTarget(config.Op{
		Kind: config.OpCopy, Source: filepath.Join(dir, "absent"), Target: filepath.Join(dir, "dst"),
	}).Apply(testTheme)
	if err == nil {
		t.Fatal("copying a file that does not exist passed silently")
	}
}

// ---------------------------------------------------------------------------
// set-line
// ---------------------------------------------------------------------------

// All four states the file can be in. The last one is why this is an operation
// rather than a rewrite: a regex over a settings.ini with no [Settings] header
// matches nothing, which read as success and changed nothing.
func TestSetLineHandlesEveryStateTheFileCanBeIn(t *testing.T) {
	const regex = `^gtk-theme-name=.*$`
	const value = "gtk-theme-name=Lvim"

	tests := []struct {
		name, before, want string
		section            string
	}{
		{
			name:    "replaces the line where it already is",
			section: "[Settings]",
			before:  "[Settings]\ngtk-theme-name=Old\ngtk-font-name=Sans\n",
			want:    "[Settings]\ngtk-theme-name=Lvim\ngtk-font-name=Sans\n",
		},
		{
			name:    "inserts under the section that exists",
			section: "[Settings]",
			before:  "[Settings]\ngtk-font-name=Sans\n",
			want:    "[Settings]\ngtk-theme-name=Lvim\ngtk-font-name=Sans\n",
		},
		{
			name:    "writes the section when there is none",
			section: "[Settings]",
			before:  "gtk-font-name=Sans\n",
			want:    "[Settings]\ngtk-theme-name=Lvim\ngtk-font-name=Sans\n",
		},
		{
			name:   "prepends when no section was declared",
			before: "other=1\n",
			want:   "gtk-theme-name=Lvim\nother=1\n",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "settings.ini")
			if err := os.WriteFile(path, []byte(tt.before), 0o644); err != nil {
				t.Fatal(err)
			}
			if _, err := opTarget(config.Op{
				Kind: config.OpSetLine, File: path, Regex: regex, Value: value, Section: tt.section,
			}).Apply(testTheme); err != nil {
				t.Fatal(err)
			}
			if got := read(t, path); got != tt.want {
				t.Errorf("file = %q, want %q", got, tt.want)
			}
		})
	}
}

// A file that is not there yet is created, section and all — the directory too,
// so a target need not declare a mkdir before it.
func TestSetLineCreatesTheFileAndItsDirectory(t *testing.T) {
	path := filepath.Join(t.TempDir(), "gtk-3.0", "settings.ini")

	note, err := opTarget(config.Op{
		Kind:  config.OpSetLine,
		File:  path,
		Regex: `^gtk-theme-name=.*$`,
		Value: "gtk-theme-name={theme}",
		// The placeholder expands in the value too, which is the whole reason
		// the operation is templated.
		Section: "[Settings]",
	}).Apply(testTheme)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(note, "created") {
		t.Errorf("note = %q, want it to say the file was created", note)
	}
	if got := read(t, path); got != "[Settings]\ngtk-theme-name=LvimEverforest_soft\n" {
		t.Errorf("file = %q", got)
	}
}

// A regex the definition got wrong is reported rather than silently matching
// nothing.
func TestSetLineReportsARegexItCannotCompile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "settings.ini")
	if _, err := opTarget(config.Op{
		Kind: config.OpSetLine, File: path, Regex: "([", Value: "x",
	}).Apply(testTheme); err == nil {
		t.Fatal("an uncompilable regex was accepted")
	}
}

// ---------------------------------------------------------------------------
// move-aside
// ---------------------------------------------------------------------------

// A user stylesheet outranks a theme's, so anything left there quietly
// overrides ours: the switch appears to work and the colours do not change. It
// is moved, never deleted.
func TestMoveAsideKeepsWhatItMoves(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "gtk.css")
	target := filepath.Join(dir, "backup", "gtk.css")
	if err := os.WriteFile(path, []byte("theirs\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	if _, err := opTarget(config.Op{
		Kind: config.OpMoveAside, File: path, Target: target, Unless: "Lvim",
	}).Apply(testTheme); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Error("the file was left in the way")
	}
	if got := read(t, target); got != "theirs\n" {
		t.Errorf("moved file = %q, want it kept intact", got)
	}
}

// A file that is already ours stays where it is, and a file that was never
// there is not an error — nothing is in the way, which is the desired state.
func TestMoveAsideLeavesOurOwnFileAndAnAbsentOne(t *testing.T) {
	dir := t.TempDir()
	ours := filepath.Join(dir, "gtk.css")
	if err := os.WriteFile(ours, []byte("/* Lvim */\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	op := config.Op{Kind: config.OpMoveAside, File: ours, Target: filepath.Join(dir, "backup"), Unless: "Lvim"}
	if _, err := opTarget(op).Apply(testTheme); err != nil {
		t.Fatal(err)
	}
	if got := read(t, ours); got != "/* Lvim */\n" {
		t.Errorf("our own file was moved: %q", got)
	}

	op.File = filepath.Join(dir, "absent.css")
	if _, err := opTarget(op).Apply(testTheme); err != nil {
		t.Errorf("a file that was never in the way was reported as a failure: %v", err)
	}
}
