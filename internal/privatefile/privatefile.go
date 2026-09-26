// Package privatefile writes files that may hold credentials: owner-only
// (0600) and atomic, so a reader sees the old contents or the new ones.
package privatefile

import (
	"fmt"
	"os"
	"path/filepath"
)

// Write replaces path with data through a 0600 temp file in the same
// directory and a rename. Because the rename swaps in a new inode, the
// result is 0600 even when an older file at path was world-readable.
func Write(path string, data []byte) (err error) {
	tmp, err := os.CreateTemp(filepath.Dir(path), "."+filepath.Base(path)+".tmp-*")
	if err != nil {
		return fmt.Errorf("create temp file: %w", err)
	}
	tmpName := tmp.Name()
	defer func() {
		if err != nil {
			_ = tmp.Close()
			_ = os.Remove(tmpName)
		}
	}()
	if err = tmp.Chmod(0o600); err != nil {
		return fmt.Errorf("chmod temp file: %w", err)
	}
	if _, err = tmp.Write(data); err != nil {
		return fmt.Errorf("write temp file: %w", err)
	}
	if err = tmp.Close(); err != nil {
		return fmt.Errorf("close temp file: %w", err)
	}
	if err = os.Rename(tmpName, path); err != nil {
		return fmt.Errorf("rename temp file: %w", err)
	}
	return nil
}
