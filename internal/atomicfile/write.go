// Package atomicfile writes files so that a crash never leaves a partially
// written result. Readers observe either the previous contents or the complete
// new contents, never a torn mixture.
//
// Every durable file this system writes goes through here. Duplicating the
// sequence is how one copy eventually loses its flush and starts corrupting
// state under power loss.
package atomicfile

import (
	"fmt"
	"os"
	"path/filepath"
)

// Write replaces path with data atomically.
//
// The temporary file is created in the destination directory so the rename
// stays within one filesystem, where POSIX guarantees it is atomic. Contents
// are flushed before the rename, otherwise a power loss can leave the
// directory entry pointing at unwritten blocks.
func Write(path string, data []byte, perm os.FileMode) error {
	if path == "" {
		return fmt.Errorf("path cannot be empty")
	}

	dir := filepath.Dir(path)

	tmp, err := os.CreateTemp(dir, ".tmp-"+filepath.Base(path)+"-*")
	if err != nil {
		return fmt.Errorf("failed to create temporary file: %w", err)
	}
	tmpName := tmp.Name()

	// Remove the temporary file on any path that does not reach the rename.
	defer func() {
		if tmpName != "" {
			_ = os.Remove(tmpName)
		}
	}()

	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return fmt.Errorf("failed to write %s: %w", path, err)
	}

	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return fmt.Errorf("failed to flush %s: %w", path, err)
	}

	if err := tmp.Close(); err != nil {
		return fmt.Errorf("failed to close %s: %w", path, err)
	}

	// CreateTemp uses 0600; apply the caller's mode before publishing.
	if err := os.Chmod(tmpName, perm); err != nil {
		return fmt.Errorf("failed to set permissions on %s: %w", path, err)
	}

	if err := os.Rename(tmpName, path); err != nil {
		return fmt.Errorf("failed to commit %s: %w", path, err)
	}
	tmpName = ""

	syncDir(dir)
	return nil
}

// syncDir flushes a directory entry so a rename survives power loss.
//
// Not every platform permits opening a directory for sync, and failing to do
// so does not endanger the file contents, which are already durable. The
// failure is therefore not surfaced.
func syncDir(dir string) {
	d, err := os.Open(dir)
	if err != nil {
		return
	}
	defer d.Close()

	_ = d.Sync()
}
