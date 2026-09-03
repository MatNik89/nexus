// Package atomicwrite is the s5-min cut (tasks-P0 T05, HARDQ A2; Annex
// P0.11 atomic-write clause): file mutations land as tmp → fsync → rename —
// never in place, so a crash leaves the OLD or the NEW content, never half.
// Scope: file/workspace mutations only; persistence engines (SQLite-WAL)
// own their own durability (Essentials E4).
package atomicwrite

import (
	"fmt"
	"os"
	"path/filepath"
)

// pauseHook, when non-nil, runs after the tmp file is durable but BEFORE
// the rename — the crash window the RED test kills the process inside.
// Test-only seam; production never sets it.
var pauseHook func()

// Write atomically replaces path with data.
func Write(path string, data []byte, perm os.FileMode) error {
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, "."+filepath.Base(path)+".tmp-*")
	if err != nil {
		return fmt.Errorf("atomicwrite: %w", err)
	}
	tmpName := tmp.Name()
	cleanup := func() { tmp.Close(); os.Remove(tmpName) }
	if _, err := tmp.Write(data); err != nil {
		cleanup()
		return fmt.Errorf("atomicwrite %s: %w", path, err)
	}
	if err := tmp.Chmod(perm); err != nil {
		cleanup()
		return fmt.Errorf("atomicwrite %s: %w", path, err)
	}
	if err := tmp.Sync(); err != nil { // data durable BEFORE the swap (P0.11)
		cleanup()
		return fmt.Errorf("atomicwrite %s: fsync: %w", path, err)
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpName)
		return fmt.Errorf("atomicwrite %s: %w", path, err)
	}
	if pauseHook != nil {
		pauseHook()
	}
	if err := os.Rename(tmpName, path); err != nil {
		os.Remove(tmpName)
		return fmt.Errorf("atomicwrite %s: rename: %w", path, err)
	}
	// Make the rename itself durable: fsync the directory.
	if d, err := os.Open(dir); err == nil {
		d.Sync()
		d.Close()
	}
	return nil
}
