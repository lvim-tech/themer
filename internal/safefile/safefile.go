// Package safefile writes a file so that a crash cannot leave a half-written
// one behind.
//
// Four places wanted the same dance — fetch, assemble, json-set and the theme
// list — and each had written it out again, one step short of finishing the
// job: the rename was issued without the data having been flushed, so on a
// crash the file could come back under its real name with nothing in it. That
// is the very outcome the temporary file was there to prevent, and it is worse
// than the truncation it does prevent, because an empty theme list reads as
// "every theme was removed upstream".
package safefile

import (
	"io/fs"
	"os"
	"path/filepath"
)

// Write puts data at path, through a temporary file in the same directory.
//
// Same directory because a rename is only atomic within one filesystem, and a
// temporary file under /tmp crosses one on any machine that mounts it apart.
// The name is random rather than path+".tmp": a predictable name is one that
// can be pointed at something else before we open it, and the destination is
// not always a directory only its owner can write.
func Write(path string, data []byte, perm fs.FileMode) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	f, err := os.CreateTemp(dir, "."+filepath.Base(path)+".*")
	if err != nil {
		return err
	}
	tmp := f.Name()
	// Cleaned up on every way out but the rename, so a failed write leaves the
	// directory exactly as it found it — including no leftover to confuse the
	// next run.
	defer func() {
		f.Close()
		os.Remove(tmp)
	}()

	// CreateTemp opens at 0600, and the file has to end up readable by whatever
	// program the theme is being written for.
	if err := f.Chmod(perm); err != nil {
		return err
	}
	if _, err := f.Write(data); err != nil {
		return err
	}
	// Before the rename, not after: without it the rename can reach the disk
	// first and the file is published empty.
	if err := f.Sync(); err != nil {
		return err
	}
	// Closed explicitly, because a write error is reported at close as often as
	// at write, and the deferred Close cannot say so.
	if err := f.Close(); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// ModeOf is the permission bits path already carries, or fallback when there is
// no file there yet. A settings file somebody deliberately made private must
// not come back world-readable merely because we rewrote one key in it.
func ModeOf(path string, fallback fs.FileMode) fs.FileMode {
	if fi, err := os.Stat(path); err == nil {
		return fi.Mode().Perm()
	}
	return fallback
}
