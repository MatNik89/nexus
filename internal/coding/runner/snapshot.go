//go:build linux

// Package runner is the coding-runner substrate (PLAN-CODING-TRIO.md Slice
// 0): the internal package that owns launching `go list`/`go build`/
// `go test`/(later) `gopls` inside the existing `internal/sandbox` bwrap
// boundary, on behalf of TIA (Slice 2), evidence capture (Slice 1), and
// symedit (Slice 3). No other package in this module launches a coding
// subprocess.
//
// Slice 0's own operations are read-only/idempotent from NEXUS's durable
// state (they run against a disposable snapshot, never the live
// workspace) — plan-review round 1 (my own research + agy + kilo,
// independently converged): no S7 durable lifecycle is needed here, only
// the ordinary non-durable per-attempt grant invariant 2 requires. That
// keeps this package's dependency surface small: `internal/sandbox` +
// `internal/kernel/s7` directly, never `internal/exectool` or
// `internal/kernel/effectpath` (both are shaped for MODEL-VISIBLE,
// human-approved tool calls — a coding-run is an internal initiative no
// human approves per attempt).
package runner

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"syscall"
)

// Snapshot bounds (plan-review round 1 agy note: an explicit depth cap
// closes a resource-exhaustion angle the file-count/byte caps alone
// don't bound — a pathologically deep-but-narrow tree could exhaust
// path-length/stack limits before either cap triggers).
const (
	MaxSnapshotFiles = 20000
	MaxSnapshotBytes = 500 << 20 // 500MB
	MaxSnapshotDepth = 64
)

// Snapshot is a private, disposable RW copy of a source tree made
// available to a coding-run — the live workspace is NEVER exposed
// read-write to the sandboxed process. Digest is a Merkle-style,
// order-independent content digest over every regular file's relative
// path and bytes, usable to detect undeclared mutation by comparing a
// before/after snapshot.
type Snapshot struct {
	Dir    string
	Digest string
}

// CreateSnapshot copies sourceDir into a fresh, private (0700), disposable
// temp directory and returns its content digest. The caller MUST call the
// returned cleanup func once the snapshot is no longer needed — a crash
// before cleanup runs leaves only a disposable temp directory, never a
// durable state to reconcile (this package intentionally has no S7
// durable lifecycle; see the package doc comment).
//
// Fail-closed refusals, never silently skipped, anywhere in the tree:
//   - a symlink (never resolved-and-followed)
//   - a device, socket, or FIFO node
//   - a hard-linked regular file (Nlink > 1) — closes a self-exfiltration
//     angle a plain regular-file check misses: a workspace owner could
//     hard-link a host secret into the tree and have it faithfully copied
//     into the disposable snapshot (plan-review round 1, kilo's
//     independent finding)
//   - more than MaxSnapshotFiles regular files
//   - more than MaxSnapshotBytes total content bytes
//   - a path deeper than MaxSnapshotDepth directory levels
func CreateSnapshot(sourceDir string) (snap Snapshot, cleanup func(), err error) {
	dstDir, err := os.MkdirTemp("", "nexus-coding-snap-*")
	if err != nil {
		return Snapshot{}, nil, fmt.Errorf("runner: create snapshot dir: %w", err)
	}
	cleanup = func() { os.RemoveAll(dstDir) }

	var entries []string // "relpath\x00hexdigest", sorted before hashing
	var fileCount int
	var totalBytes int64

	walkErr := filepath.WalkDir(sourceDir, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		rel, relErr := filepath.Rel(sourceDir, path)
		if relErr != nil {
			return relErr
		}
		if rel == "." {
			return nil
		}
		if depth := strings.Count(rel, string(filepath.Separator)) + 1; depth > MaxSnapshotDepth {
			return fmt.Errorf("runner: snapshot tree exceeds max depth %d at %s (fail closed)", MaxSnapshotDepth, rel)
		}
		if d.Type()&os.ModeSymlink != 0 {
			return fmt.Errorf("runner: snapshot input contains a symlink at %s (fail closed)", rel)
		}
		if d.IsDir() {
			return os.Mkdir(filepath.Join(dstDir, rel), 0o700)
		}
		if !d.Type().IsRegular() {
			return fmt.Errorf("runner: snapshot input contains a non-regular file at %s (fail closed)", rel)
		}

		// Open with O_NOFOLLOW and validate/read against that SAME
		// descriptor (mirrors probe.go's loadBounded pattern, plus
		// plan-review round 1 kilo's TOCTOU note): WalkDir's own
		// symlink check above happens at STAT time, before this call —
		// a plain os.Open (which FOLLOWS symlinks) between that stat and
		// this open would silently read through a symlink swapped into
		// place in that window, and a post-open Stat().Mode().IsRegular()
		// check would NOT catch it (it reports the FOLLOWED target's
		// mode, which looks like an ordinary regular file). O_NOFOLLOW
		// makes the open itself fail closed if the final path component
		// is a symlink, closing the window entirely.
		f, openErr := openRegularNoFollow(path)
		if openErr != nil {
			return fmt.Errorf("runner: %s: %w (fail closed)", rel, openErr)
		}
		defer f.Close()
		st, statErr := f.Stat()
		if statErr != nil {
			return statErr
		}
		if !st.Mode().IsRegular() {
			return fmt.Errorf("runner: %s changed to a non-regular file during snapshot copy (fail closed)", rel)
		}
		sys, ok := st.Sys().(*syscall.Stat_t)
		if !ok {
			return fmt.Errorf("runner: cannot determine link count for %s (fail closed)", rel)
		}
		if sys.Nlink > 1 {
			return fmt.Errorf("runner: snapshot input contains a hard-linked file at %s (fail closed)", rel)
		}
		fileCount++
		if fileCount > MaxSnapshotFiles {
			return fmt.Errorf("runner: snapshot exceeds max file count %d (fail closed)", MaxSnapshotFiles)
		}
		remaining := MaxSnapshotBytes - totalBytes
		if remaining <= 0 {
			return fmt.Errorf("runner: snapshot exceeds max byte size %d (fail closed)", MaxSnapshotBytes)
		}
		data, readErr := io.ReadAll(io.LimitReader(f, remaining+1))
		if readErr != nil {
			return readErr
		}
		if int64(len(data)) > remaining {
			return fmt.Errorf("runner: snapshot exceeds max byte size %d (fail closed)", MaxSnapshotBytes)
		}
		totalBytes += int64(len(data))
		if writeErr := os.WriteFile(filepath.Join(dstDir, rel), data, 0o644); writeErr != nil {
			return writeErr
		}
		sum := sha256.Sum256(data)
		entries = append(entries, rel+"\x00"+hex.EncodeToString(sum[:]))
		return nil
	})
	if walkErr != nil {
		cleanup()
		return Snapshot{}, nil, walkErr
	}

	sort.Strings(entries)
	h := sha256.New()
	for _, e := range entries {
		io.WriteString(h, e)
		io.WriteString(h, "\n")
	}
	return Snapshot{Dir: dstDir, Digest: hex.EncodeToString(h.Sum(nil))}, cleanup, nil
}

// openRegularNoFollow opens path for reading, refusing outright (never
// silently following) if the final path component is a symlink.
func openRegularNoFollow(path string) (*os.File, error) {
	return os.OpenFile(path, os.O_RDONLY|syscall.O_NOFOLLOW, 0)
}
