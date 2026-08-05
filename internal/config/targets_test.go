package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// withConfigDir points Dir() at a throwaway directory. XDG is redirected too:
// on this machine XDG_CONFIG_HOME is set, and a test that moved only $HOME
// would read — or write — the developer's own themer configuration.
func withConfigDir(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	dir := filepath.Join(home, ".config", "themer", "targets")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	return dir
}

func writeTarget(t *testing.T, dir, file, body string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, file), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

// One file per program is the point of the directory: each is read, and each
// contributes its own target.
func TestEveryFileInTheDirectoryIsRead(t *testing.T) {
	dir := withConfigDir(t)
	writeTarget(t, dir, "eza.toml", "[[targets]]\nname = 'eza'\n[targets.detect]\ncommand = 'eza'\n")
	writeTarget(t, dir, "k9s.toml", "[[targets]]\nname = 'k9s'\n[targets.detect]\ncommand = 'k9s'\n")

	got, err := loadTargetsDir()
	if err != nil {
		t.Fatal(err)
	}
	names := map[string]bool{}
	for _, tgt := range got {
		names[tgt.Name] = true
	}
	if !names["eza"] || !names["k9s"] {
		t.Errorf("loaded %v, want both files", names)
	}
}

// Filename order, so two files naming the same target resolve identically on
// every machine rather than following whatever order the directory returns.
func TestTargetsAreMergedInFilenameOrder(t *testing.T) {
	dir := withConfigDir(t)
	writeTarget(t, dir, "01-base.toml", "[[targets]]\nname = 'eza'\n[targets.detect]\ncommand = 'first'\n")
	writeTarget(t, dir, "02-over.toml", "[[targets]]\nname = 'eza'\n[targets.detect]\ncommand = 'second'\n")

	got, err := loadTargetsDir()
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 {
		t.Fatalf("targets = %d, want the two definitions merged into one", len(got))
	}
	if got[0].Detect.Command != "second" {
		t.Errorf("command = %q, want the later filename to win", got[0].Detect.Command)
	}
}

// A definition that will not parse is named. Skipped silently, it would show
// up as a program that quietly stopped being themed.
func TestABrokenDefinitionIsNamed(t *testing.T) {
	dir := withConfigDir(t)
	writeTarget(t, dir, "eza.toml", "[[targets]\nname = ")

	_, err := loadTargetsDir()
	if err == nil {
		t.Fatal("a malformed definition was ignored")
	}
	if !strings.Contains(err.Error(), "eza.toml") {
		t.Errorf("error = %v, want it to name the file", err)
	}
}

// An empty file is a mistake, not a definition: it usually means the target
// was renamed or the [[targets]] header was lost in an edit.
func TestAFileWithNoTargetsIsReported(t *testing.T) {
	dir := withConfigDir(t)
	writeTarget(t, dir, "eza.toml", "# nothing here yet\n")

	if _, err := loadTargetsDir(); err == nil {
		t.Fatal("a file declaring no targets was accepted")
	}
}

// Only .toml is read, so an editor's backup or a stray note does not become a
// definition — or an error.
func TestNonTOMLFilesAreIgnored(t *testing.T) {
	dir := withConfigDir(t)
	writeTarget(t, dir, "eza.toml", "[[targets]]\nname = 'eza'\n[targets.detect]\ncommand = 'eza'\n")
	writeTarget(t, dir, "eza.toml.bak", "this is not toml at all")
	writeTarget(t, dir, "NOTES.md", "# reminders")

	got, err := loadTargetsDir()
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 {
		t.Errorf("targets = %d, want only the .toml file read", len(got))
	}
}

// A fresh install has no directory yet. That is the normal case, not an error.
func TestAnAbsentDirectoryIsNotAnError(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))

	got, err := loadTargetsDir()
	if err != nil {
		t.Fatalf("an absent targets directory reported %v", err)
	}
	if len(got) != 0 {
		t.Errorf("targets = %d, want none", len(got))
	}
}

// ---------------------------------------------------------------------------
// priority
// ---------------------------------------------------------------------------

// Filename order is arbitrary with respect to a decision: it put mango — a
// compositor, which must repaint last — ahead of state-file, which must be
// written first. Priority is what states the decision.
func TestPriorityOverridesFilenameOrder(t *testing.T) {
	dir := withConfigDir(t)
	writeTarget(t, dir, "mango.toml", "[[targets]]\nname = 'mango'\npriority = 90\n[targets.detect]\nalways = true\n")
	writeTarget(t, dir, "state-file.toml", "[[targets]]\nname = 'state'\npriority = 10\n[targets.detect]\nalways = true\n")

	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.Targets) != 2 {
		t.Fatalf("targets = %d", len(cfg.Targets))
	}
	// mango.toml sorts first by filename; priority has to beat that.
	if cfg.Targets[0].Name != "state" {
		t.Errorf("order = %q, %q — want the low priority first", cfg.Targets[0].Name, cfg.Targets[1].Name)
	}
}

// Equal priorities keep the order their filenames gave them, so a set of
// targets that never asks for a place stays predictable between runs.
func TestEqualPrioritiesKeepFilenameOrder(t *testing.T) {
	dir := withConfigDir(t)
	for _, n := range []string{"a", "b", "c"} {
		writeTarget(t, dir, n+".toml", "[[targets]]\nname = '"+n+"'\n[targets.detect]\nalways = true\n")
	}

	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	for i, want := range []string{"a", "b", "c"} {
		if cfg.Targets[i].Name != want {
			t.Errorf("targets[%d] = %q, want %q", i, cfg.Targets[i].Name, want)
		}
	}
}

// An unset priority is the middle, so a definition can be ordered before or
// after the crowd without anyone having to write a negative number.
func TestUnsetPriorityLandsInTheMiddle(t *testing.T) {
	dir := withConfigDir(t)
	writeTarget(t, dir, "late.toml", "[[targets]]\nname = 'late'\npriority = 90\n[targets.detect]\nalways = true\n")
	writeTarget(t, dir, "plain.toml", "[[targets]]\nname = 'plain'\n[targets.detect]\nalways = true\n")
	writeTarget(t, dir, "early.toml", "[[targets]]\nname = 'early'\npriority = 10\n[targets.detect]\nalways = true\n")

	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	for i, want := range []string{"early", "plain", "late"} {
		if cfg.Targets[i].Name != want {
			t.Errorf("targets[%d] = %q, want %q", i, cfg.Targets[i].Name, want)
		}
	}
}
