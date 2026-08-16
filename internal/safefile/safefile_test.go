package safefile

import (
	"os"
	"path/filepath"
	"testing"
)

func TestWriteCreatesTheDirectoryAndTheFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sub", "themes.txt")

	if err := Write(path, []byte("body\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(b) != "body\n" {
		t.Errorf("content = %q", b)
	}
	fi, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if fi.Mode().Perm() != 0o644 {
		t.Errorf("mode = %v, want 0644 — CreateTemp opens at 0600", fi.Mode().Perm())
	}
}

// The whole point of the temporary file is that the destination is either the
// old content or the new one, never half of either.
func TestWriteReplacesInOneStep(t *testing.T) {
	path := filepath.Join(t.TempDir(), "themes.txt")
	if err := os.WriteFile(path, []byte("old\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := Write(path, []byte("new\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(path)
	if string(b) != "new\n" {
		t.Errorf("content = %q", b)
	}
}

// A failed write leaves the directory as it found it: a leftover temporary file
// is one the next reader has to know to ignore.
func TestWriteLeavesNoTemporaryFileBehind(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "themes.txt")
	if err := Write(path, []byte("body\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Name() != "themes.txt" {
		t.Errorf("directory holds %d entries, want only the file itself", len(entries))
	}
}

func TestModeOfPrefersWhatTheFileAlreadyHas(t *testing.T) {
	path := filepath.Join(t.TempDir(), "settings.json")
	if got := ModeOf(path, 0o644); got != 0o644 {
		t.Errorf("mode of a file that is not there = %v, want the fallback", got)
	}
	if err := os.WriteFile(path, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if got := ModeOf(path, 0o644); got != 0o600 {
		t.Errorf("mode = %v, want the 0600 the file carries", got)
	}
}
