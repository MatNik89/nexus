//go:build linux

// Package workspace is Slice 3's multi-file transaction coordinator
// (PLAN-CODING-TRIO.md invariant 4): a rename touches MANY files, and
// `internal/foundation/atomicwrite` only makes ONE file old-or-new. This
// file is the FIRST, foundational piece: the descriptor-relative
// filesystem primitives every write in a transaction goes through —
// `openat2(RESOLVE_BENEATH|RESOLVE_NO_SYMLINKS)` to walk into a
// directory one component at a time, and an atomic create-temp+fsync+
// renameat+fsync-directory sequence to replace one file, both entirely
// through file descriptors pinned once at the start of a transaction,
// never re-resolved by pathname mid-transaction (closing the TOCTOU
// window a path-string API — `filepath.EvalSymlinks` then `rename` by
// name — leaves open: a parent-directory symlink swapped between the
// last check and the actual write). The durable S7-governed crash-
// recovery machinery (sealed bundles, the mutation_committed marker,
// restart-time PolicyWorkspaceRollback) is a LATER piece built on top of
// this one.
//
// Requires kernel >= 5.6 for openat2 (plan precondition; this host is
// confirmed Linux aarch64 6.12.34, and NEXUS is already Linux-first).
package workspace

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"io/fs"
	"os"
	"strings"

	"golang.org/x/sys/unix"
)

// resolveFlags is the openat2 RESOLVE mask every descriptor-relative
// open in this package uses: RESOLVE_BENEATH refuses any resolution
// that would escape the starting directory fd (via ".." or an absolute
// symlink target); RESOLVE_NO_SYMLINKS refuses ANY symlink component,
// not just the leaf; RESOLVE_NO_MAGICLINKS refuses Linux's special
// /proc-style magic-link resolution, which RESOLVE_NO_SYMLINKS alone
// does not cover.
const resolveFlags = unix.RESOLVE_BENEATH | unix.RESOLVE_NO_SYMLINKS | unix.RESOLVE_NO_MAGICLINKS

// beforeRestoreExchange is a test seam: a no-op in production, it lets
// descriptor_test.go deterministically inject a concurrent write to
// baseName at the exact instant between mismatch detection and the
// restoring exchange, without a real (flaky) goroutine race.
var beforeRestoreExchange = func() {}

// OpenRoot opens rootPath as a directory file descriptor — the pinned
// identity boundary for one whole Prepare->Apply transaction (invariant
// 4's "descriptor-pinned parent-directory... held open across
// Prepare->Apply"). Refuses if ANY component of rootPath — not just the
// final one — is a symlink or magic link.
//
// Uses openat2, not plain open+O_NOFOLLOW (code-review finding, codex,
// round 1, confirmed live on this kernel): O_NOFOLLOW on a plain open()
// call only applies to the FINAL path component, per POSIX/Linux
// semantics — an INTERMEDIATE symlink (e.g. rootPath = "/tmp/x/link/ws"
// where "link" is a symlink) is silently traversed. Since every
// downstream WalkDirBeneath/WriteFileBeneath call trusts rootFd as the
// correct containment boundary, a wrong root fd silently defeats the
// ENTIRE rest of this package's symlink defense. openat2's
// RESOLVE_NO_SYMLINKS applies to EVERY resolved component, not just the
// last — the exact property a plain open() cannot provide. RESOLVE_BENEATH
// is deliberately NOT used here: rootPath is an absolute path with no
// "beneath" ancestor descriptor to confine it to (RESOLVE_BENEATH is
// relative to the STARTING fd, which for an absolute path resolved from
// AT_FDCWD would incorrectly confine it to the current working
// directory).
func OpenRoot(rootPath string) (int, error) {
	how := unix.OpenHow{
		Flags:   unix.O_DIRECTORY | unix.O_NOFOLLOW | unix.O_CLOEXEC,
		Resolve: unix.RESOLVE_NO_SYMLINKS | unix.RESOLVE_NO_MAGICLINKS,
	}
	fd, err := unix.Openat2(unix.AT_FDCWD, rootPath, &how)
	if err != nil {
		return -1, fmt.Errorf("workspace: open root %q: %w", rootPath, err)
	}
	return fd, nil
}

// openDirBeneath opens the SINGLE path component name as a directory,
// relative to parentFd, via openat2 with resolveFlags — refusing
// outright (never resolving-and-following) if name is a symlink, a
// magic link, or resolution would escape parentFd's own subtree.
func openDirBeneath(parentFd int, name string) (int, error) {
	how := unix.OpenHow{
		Flags:   unix.O_DIRECTORY | unix.O_NOFOLLOW | unix.O_CLOEXEC,
		Resolve: resolveFlags,
	}
	fd, err := unix.Openat2(parentFd, name, &how)
	if err != nil {
		return -1, fmt.Errorf("workspace: open directory component %q: %w", name, err)
	}
	return fd, nil
}

// WalkDirBeneath opens the directory at relDir — a CLEAN, slash-
// separated relative path (validated by validateRelPath: no "..", no
// leading "/", no empty components) — beneath rootFd, walking ONE
// descriptor-relative openat2 call per path component in turn (plan
// invariant 4: "walk and open each intermediate directory component
// descriptor-relatively via openat2 in turn... never fall back to a
// path-string API partway through a deep path just because the leading
// components were already validated"). relDir=="" (the file lives
// directly in the root) returns a dup of rootFd. The caller owns closing
// the returned fd.
func WalkDirBeneath(rootFd int, relDir string) (fd int, err error) {
	if relDir == "" {
		dup, dupErr := unix.FcntlInt(uintptr(rootFd), unix.F_DUPFD_CLOEXEC, 0)
		if dupErr != nil {
			return -1, fmt.Errorf("workspace: dup root fd: %w", dupErr)
		}
		return dup, nil
	}
	if err := validateRelPath(relDir); err != nil {
		return -1, err
	}
	cur := rootFd
	owned := false
	for _, comp := range strings.Split(relDir, "/") {
		next, openErr := openDirBeneath(cur, comp)
		if owned {
			unix.Close(cur)
		}
		if openErr != nil {
			return -1, openErr
		}
		cur = next
		owned = true
	}
	return cur, nil
}

// TargetExpectation states what WriteFileBeneath must find (or not
// find) at baseName immediately before replacing it (code-review
// finding, codex, round 1: an earlier version performed an
// unconditional rename-over, silently overwriting whatever was there —
// including a symlink an attacker planted at that exact name between
// Prepare and Apply. The plan's own identity model requires that drift
// be REFUSED, not silently accepted: "an inode change to the... tracked
// NAME resolving somewhere else, is refused").
type TargetExpectation struct {
	// MustNotExist requires baseName to not already exist — a genuinely
	// new file. Enforced ATOMICALLY via RENAME_NOREPLACE on the final
	// rename itself (renameat2), so this case has NO separate
	// check-then-act race window at all.
	MustNotExist bool
	// Dev/Ino, when MustNotExist is false, are the device+inode
	// WriteFileBeneath must find at baseName at the moment it is
	// displaced — captured by the caller at an earlier point (e.g.
	// Prepare time, via StatBeneath). A mismatch, INCLUDING baseName
	// having become a symlink, a directory, or any other non-regular
	// type since then, is refused and the original entry is restored
	// (see WriteFileBeneath's exchange-verify-restore sequence, which
	// closes the check-then-act window a separate fstatat-then-renameat
	// would leave open — code-review finding, codex, round 2).
	Dev, Ino uint64
}

// WriteFileBeneath atomically replaces (or creates) baseName within the
// directory identified by dirFd, per expect: create a private, randomly-
// named temp file (O_CREAT|O_EXCL, so a name collision fails rather than
// silently reusing an existing file), write+fsync its content, then
// either RENAME_NOREPLACE (MustNotExist — zero check-then-act window,
// the kernel refuses the rename atomically if baseName already exists)
// or, for an existing target, an exchange-verify-restore sequence:
// renameat2(RENAME_EXCHANGE) atomically swaps baseName and tmpName (so
// whatever was AT baseName the instant of the swap — not the instant of
// an earlier check — ends up under tmpName), fstatat the now-displaced
// entry under tmpName and require it to be a regular file with
// expect.Dev/Ino, and on any mismatch (wrong identity, or a symlink/
// directory/other non-regular node) exchange back to restore the
// original untouched and refuse. This has NO separate check-then-act
// race window: the swap happens unconditionally and atomically, and
// identity is verified on the result, not on a stale pre-check (code-
// review finding, codex, round 2: a plain fstatat-then-renameat leaves
// exactly the window this closes). fsync the containing directory so
// the rename is itself durable. Every step after dirFd is obtained is
// descriptor-relative — no path-string API is used at any point.
func WriteFileBeneath(dirFd int, baseName string, content []byte, mode fs.FileMode, expect TargetExpectation) error {
	if err := validateBaseName(baseName); err != nil {
		return err
	}
	tmpName, err := tempName()
	if err != nil {
		return err
	}
	how := unix.OpenHow{
		Flags:   unix.O_CREAT | unix.O_EXCL | unix.O_WRONLY | unix.O_NOFOLLOW | unix.O_CLOEXEC,
		Mode:    uint64(mode.Perm()),
		Resolve: resolveFlags,
	}
	fd, err := unix.Openat2(dirFd, tmpName, &how)
	if err != nil {
		return fmt.Errorf("workspace: create temp file %q: %w", tmpName, err)
	}
	f := os.NewFile(uintptr(fd), tmpName)
	if _, err := f.Write(content); err != nil {
		f.Close()
		unix.Unlinkat(dirFd, tmpName, 0)
		return fmt.Errorf("workspace: write temp file %q: %w", tmpName, err)
	}
	if err := f.Sync(); err != nil {
		f.Close()
		unix.Unlinkat(dirFd, tmpName, 0)
		return fmt.Errorf("workspace: fsync temp file %q: %w", tmpName, err)
	}
	if err := f.Close(); err != nil {
		unix.Unlinkat(dirFd, tmpName, 0)
		return fmt.Errorf("workspace: close temp file %q: %w", tmpName, err)
	}

	if expect.MustNotExist {
		if err := unix.Renameat2(dirFd, tmpName, dirFd, baseName, unix.RENAME_NOREPLACE); err != nil {
			unix.Unlinkat(dirFd, tmpName, 0)
			return fmt.Errorf("workspace: %q already exists, expected it to be new (fail closed): %w", baseName, err)
		}
	} else {
		// Capture OUR OWN temp file's identity before exchanging it into
		// place. If a mismatch later forces a restore, this is the only
		// safe way to know whether tmpName still holds our discarded
		// content before unlinking it (code-review finding, codex, round
		// 3: an unconditional unlink after the restoring exchange could
		// delete a DIFFERENT writer's file, if that writer replaced
		// baseName during the window between mismatch detection and the
		// restoring exchange — that writer's content would land under
		// tmpName by the restore swap, indistinguishable from our own
		// discarded content unless identity is checked).
		var ownSt unix.Stat_t
		if err := unix.Fstatat(dirFd, tmpName, &ownSt, unix.AT_SYMLINK_NOFOLLOW); err != nil {
			unix.Unlinkat(dirFd, tmpName, 0)
			return fmt.Errorf("workspace: %q: could not capture own temp file identity: %w", baseName, err)
		}
		if err := unix.Renameat2(dirFd, tmpName, dirFd, baseName, unix.RENAME_EXCHANGE); err != nil {
			unix.Unlinkat(dirFd, tmpName, 0)
			return fmt.Errorf("workspace: %q: could not exchange for identity verification (fail closed): %w", baseName, err)
		}
		// tmpName now holds whatever was displaced from baseName by the
		// swap above — verify it is the exact regular file expect names
		// before treating the swap as final.
		var st unix.Stat_t
		statErr := unix.Fstatat(dirFd, tmpName, &st, unix.AT_SYMLINK_NOFOLLOW)
		match := statErr == nil && st.Mode&unix.S_IFMT == unix.S_IFREG &&
			uint64(st.Dev) == expect.Dev && st.Ino == expect.Ino
		if !match {
			beforeRestoreExchange() // test seam: inject a write to baseName here

			// Restore: swap back so baseName holds the original entry
			// again, untouched.
			if restoreErr := unix.Renameat2(dirFd, tmpName, dirFd, baseName, unix.RENAME_EXCHANGE); restoreErr != nil {
				return fmt.Errorf("workspace: %q identity drifted and restore-swap failed — directory may be inconsistent: %w", baseName, restoreErr)
			}
			// Discard tmpName ONLY if it still identifies our own temp
			// file. If a concurrent writer replaced baseName between the
			// mismatch detection above and the restoring exchange just
			// performed, tmpName now holds THEIR content, not ours —
			// never blind-unlink; preserve it and fail closed instead.
			var afterSt unix.Stat_t
			afterErr := unix.Fstatat(dirFd, tmpName, &afterSt, unix.AT_SYMLINK_NOFOLLOW)
			ownStillOurs := afterErr == nil && afterSt.Mode&unix.S_IFMT == unix.S_IFREG &&
				uint64(afterSt.Dev) == uint64(ownSt.Dev) && afterSt.Ino == ownSt.Ino
			if !ownStillOurs {
				return fmt.Errorf("workspace: %q identity drifted and was refused and restored, but %q no longer identifies our own discarded content after a concurrent write during restore — leaving it in place rather than risk deleting someone else's data (needs manual recovery)",
					baseName, tmpName)
			}
			unix.Unlinkat(dirFd, tmpName, 0)
			if statErr != nil {
				return fmt.Errorf("workspace: %q: could not verify displaced identity after swap (fail closed, restored): %w", baseName, statErr)
			}
			return fmt.Errorf("workspace: %q identity drifted (mode=%o,dev=%d,ino=%d, expected regular file dev=%d,ino=%d) — refusing to replace (fail closed, restored)",
				baseName, st.Mode, st.Dev, st.Ino, expect.Dev, expect.Ino)
		}
		if err := unix.Unlinkat(dirFd, tmpName, 0); err != nil {
			return fmt.Errorf("workspace: %q: replaced but could not remove displaced original %q: %w", baseName, tmpName, err)
		}
	}

	if err := unix.Fsync(dirFd); err != nil {
		return fmt.Errorf("workspace: fsync directory after replacing %q: %w", baseName, err)
	}
	return nil
}

// StatBeneath descriptor-relatively stats baseName within dirFd — the
// caller's way to capture a TargetExpectation's Dev/Ino at Prepare time
// (or at any earlier point) without ever resolving baseName by pathname.
// AT_SYMLINK_NOFOLLOW: a symlink at baseName is reported AS a symlink,
// never followed — and, since only a regular file is ever a valid
// existing-replacement target, refused here rather than silently handed
// back as a capturable identity (code-review finding, codex, round 2: a
// caller could otherwise capture a planted symlink's own Dev/Ino and
// have WriteFileBeneath's identity check "match" it, authorizing an
// overwrite of that symlink instead of refusing it; WriteFileBeneath
// itself also re-checks S_IFREG on the displaced entry as defense in
// depth against drift between capture and replace).
func StatBeneath(dirFd int, baseName string) (dev, ino uint64, err error) {
	if err := validateBaseName(baseName); err != nil {
		return 0, 0, err
	}
	var st unix.Stat_t
	if err := unix.Fstatat(dirFd, baseName, &st, unix.AT_SYMLINK_NOFOLLOW); err != nil {
		return 0, 0, fmt.Errorf("workspace: stat %q: %w", baseName, err)
	}
	if st.Mode&unix.S_IFMT != unix.S_IFREG {
		return 0, 0, fmt.Errorf("workspace: %q is not a regular file (mode=%o) — refusing to expose identity for a non-regular node (fail closed)", baseName, st.Mode)
	}
	return uint64(st.Dev), st.Ino, nil
}

// validateRelPath fails closed on any relative directory path that
// isn't a clean, non-escaping, non-empty-component sequence — this is a
// defense-in-depth check (the caller is expected to have already
// validated paths at Prepare time, per invariant 3), never the ONLY
// enforcement (openat2's own RESOLVE_BENEATH is the mutation-time
// guard).
func validateRelPath(relDir string) error {
	if strings.HasPrefix(relDir, "/") {
		return fmt.Errorf("workspace: relative directory path %q must not be absolute (fail closed)", relDir)
	}
	for _, comp := range strings.Split(relDir, "/") {
		if err := validateComponent(comp); err != nil {
			return fmt.Errorf("workspace: relative directory path %q: %w", relDir, err)
		}
	}
	return nil
}

// validateBaseName fails closed on a base filename that isn't a single,
// clean path component.
func validateBaseName(baseName string) error {
	if err := validateComponent(baseName); err != nil {
		return fmt.Errorf("workspace: base name: %w", err)
	}
	return nil
}

func validateComponent(comp string) error {
	if comp == "" {
		return fmt.Errorf("empty path component (fail closed)")
	}
	if comp == "." || comp == ".." {
		return fmt.Errorf("path component %q is not allowed (fail closed)", comp)
	}
	if strings.Contains(comp, "/") {
		return fmt.Errorf("path component %q contains a separator (fail closed)", comp)
	}
	return nil
}

// tempName generates a private, unpredictable temp filename — prefixed
// so it sorts away from real source files and is trivially identifiable
// as this package's own scratch artifact if ever observed mid-write.
func tempName() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("workspace: generating temp file name: %w", err)
	}
	return ".nexus-workspace-tmp-" + hex.EncodeToString(b), nil
}
