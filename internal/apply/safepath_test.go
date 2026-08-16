package apply

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/lvim-tech/themer/internal/config"
	"github.com/lvim-tech/themer/internal/theme"
)

// The theme name is published by somebody else's list and substituted into
// paths. A name that climbs out of the directory a definition pointed at aims
// the operation somewhere nobody asked for — and copy-tree removes its
// destination before writing it, so the worst case is not a stray file but a
// directory that is gone.
func TestAnOperationRefusesAPathThatClimbsOut(t *testing.T) {
	dir := t.TempDir()
	escaped := filepath.Join(dir, "outside")
	if err := os.MkdirAll(escaped, 0o755); err != nil {
		t.Fatal(err)
	}

	hostile := theme.Theme{Name: "../outside", Family: "../outside"}
	for _, op := range []config.Op{
		{Kind: config.OpWrite, File: filepath.Join(dir, "in", "{theme}"), Content: "x"},
		{Kind: config.OpCopyTree, Source: dir, Target: filepath.Join(dir, "in", "{theme}")},
		{Kind: config.OpJSONSet, File: filepath.Join(dir, "in", "{theme}"), Key: "k", Value: "v"},
	} {
		_, err := opTarget(op).Apply(hostile)
		if err == nil {
			t.Errorf("%s: a path climbing out of its directory was accepted", op.Kind)
			continue
		}
		if !strings.Contains(err.Error(), "climbs out") {
			t.Errorf("%s: error = %v, want it to name the reason", op.Kind, err)
		}
	}
	if _, err := os.Stat(escaped); err != nil {
		t.Errorf("the directory outside was touched: %v", err)
	}
}

// The check is on the expanded path, so a definition that spells one out
// directly is refused too — a `..` is never needed, since the file it means can
// always be named as it is.
func TestSafePath(t *testing.T) {
	if _, err := safePath("/etc/../etc/passwd"); err == nil {
		t.Error("a path with a .. segment was accepted")
	}
	// A name that merely contains dots is not a climb.
	if got, err := safePath("/tmp/a..b/c"); err != nil || got != "/tmp/a..b/c" {
		t.Errorf("safePath = %q, %v — want the path kept", got, err)
	}
}
