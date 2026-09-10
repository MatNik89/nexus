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
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
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

// fsyncDirHook is WriteFileBeneath's trailing directory fsync, indirected
// through a var so descriptor_test.go/transaction_test.go can force it to
// fail deterministically — proving WriteFileBeneath's committed return
// value is correct even when this specific trailing step errors after
// the rename/exchange has already landed (code-review finding, codex,
// transaction.go round 1).
var fsyncDirHook = unix.Fsync

// beforeQuarantineVerify is a test seam: a no-op in production, it lets
// descriptor_test.go deterministically inject a concurrent CREATE at
// baseName immediately after RemoveFileWithExpectedIdentity has
// quarantined (renamed away) whatever was previously there, proving that
// creation survives untouched — without a real (flaky) goroutine race.
var beforeQuarantineVerify = func() {}

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
	// ContentDigest, when non-empty, is the sha256 hex digest the
	// displaced entry's ACTUAL BYTES must match — independent of Dev/Ino
	// (code-review finding, codex, transaction.go round 1: identity
	// alone does not catch a writer that mutates a file's content
	// IN PLACE via truncate+rewrite, same inode, between the caller
	// capturing Dev/Ino and this call — invariant 4 explicitly requires
	// "content, mode, and metadata expectations... checked explicitly,
	// independent of inode"). Empty skips the check (existing callers
	// unaffected).
	ContentDigest string
	// ExpectMode is the permission bits (via Perm()) the displaced
	// entry's mode must match, checked only when CheckMode is true.
	ExpectMode fs.FileMode
	// CheckMode gates the ExpectMode check. A bool, not a zero-value
	// sentinel on ExpectMode (code-review finding, codex, round 2: mode
	// 0000 is a legitimately capturable mode — using ExpectMode==0 to
	// mean "skip" was fail-open for that exact value).
	CheckMode bool
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
// entry under tmpName and require it to be a regular file matching
// expect.Dev/Ino AND (when set) expect.ContentDigest/ExpectMode, and on
// any mismatch exchange back to restore the original untouched and
// refuse. This has NO separate check-then-act race window: the swap
// happens unconditionally and atomically, and identity/content is
// verified on the result, not on a stale pre-check (code-review finding,
// codex, round 2: a plain fstatat-then-renameat leaves exactly the
// window this closes; round-on-transaction.go: identity alone does not
// catch in-place content mutation preserving the same inode). fsync the
// containing directory so the rename is itself durable. Every step after
// dirFd is obtained is descriptor-relative — no path-string API is used
// at any point.
//
// committed reports whether baseName's directory entry now points at
// the NEW content — true as soon as the rename/exchange itself has
// landed, even if a LATER step (removing the displaced original,
// directory fsync) then fails and WriteFileBeneath returns a non-nil
// error (code-review finding, codex, transaction.go round 1: a caller
// that only tracks success/failure, not "did the entry change", can
// silently omit an actually-mutated file from its own rollback
// bookkeeping). A caller MUST treat committed==true as "this file needs
// rollback on transaction failure" regardless of err.
func WriteFileBeneath(dirFd int, baseName string, content []byte, mode fs.FileMode, expect TargetExpectation) (committed bool, err error) {
	if err := validateBaseName(baseName); err != nil {
		return false, err
	}
	tmpName, err := tempName()
	if err != nil {
		return false, err
	}
	how := unix.OpenHow{
		Flags:   unix.O_CREAT | unix.O_EXCL | unix.O_WRONLY | unix.O_NOFOLLOW | unix.O_CLOEXEC,
		Mode:    uint64(mode.Perm()),
		Resolve: resolveFlags,
	}
	fd, err := unix.Openat2(dirFd, tmpName, &how)
	if err != nil {
		return false, fmt.Errorf("workspace: create temp file %q: %w", tmpName, err)
	}
	// Openat2's own Mode is subject to the process umask like any O_CREAT
	// (code-review finding, codex, round 2: requesting 0666 under a
	// typical 0022 umask silently creates 0644, contradicting
	// FileMutation.Mode's "the result ALWAYS ends up with" contract) —
	// Fchmod is NOT umask-masked, so force the exact requested bits.
	if err := unix.Fchmod(fd, uint32(mode.Perm())); err != nil {
		unix.Close(fd)
		unix.Unlinkat(dirFd, tmpName, 0)
		return false, fmt.Errorf("workspace: chmod temp file %q: %w", tmpName, err)
	}
	f := os.NewFile(uintptr(fd), tmpName)
	if _, err := f.Write(content); err != nil {
		f.Close()
		unix.Unlinkat(dirFd, tmpName, 0)
		return false, fmt.Errorf("workspace: write temp file %q: %w", tmpName, err)
	}
	if err := f.Sync(); err != nil {
		f.Close()
		unix.Unlinkat(dirFd, tmpName, 0)
		return false, fmt.Errorf("workspace: fsync temp file %q: %w", tmpName, err)
	}
	if err := f.Close(); err != nil {
		unix.Unlinkat(dirFd, tmpName, 0)
		return false, fmt.Errorf("workspace: close temp file %q: %w", tmpName, err)
	}

	if expect.MustNotExist {
		if err := unix.Renameat2(dirFd, tmpName, dirFd, baseName, unix.RENAME_NOREPLACE); err != nil {
			unix.Unlinkat(dirFd, tmpName, 0)
			return false, fmt.Errorf("workspace: %q already exists, expected it to be new (fail closed): %w", baseName, err)
		}
		committed = true
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
			return false, fmt.Errorf("workspace: %q: could not capture own temp file identity: %w", baseName, err)
		}
		if err := unix.Renameat2(dirFd, tmpName, dirFd, baseName, unix.RENAME_EXCHANGE); err != nil {
			unix.Unlinkat(dirFd, tmpName, 0)
			return false, fmt.Errorf("workspace: %q: could not exchange for identity verification (fail closed): %w", baseName, err)
		}
		// From here on baseName holds the NEW content — the exchange has
		// already landed. A mismatch below means we made the WRONG swap
		// and must restore, not that nothing happened yet.
		var st unix.Stat_t
		statErr := unix.Fstatat(dirFd, tmpName, &st, unix.AT_SYMLINK_NOFOLLOW)
		digestOK, modeOK := true, true
		var digestErr error
		if statErr == nil && expect.ContentDigest != "" {
			displaced, rerr := ReadFileBeneath(dirFd, tmpName)
			if rerr != nil {
				digestErr = rerr
				digestOK = false
			} else {
				sum := sha256.Sum256(displaced)
				digestOK = hex.EncodeToString(sum[:]) == expect.ContentDigest
			}
		}
		if statErr == nil && expect.CheckMode {
			modeOK = fs.FileMode(st.Mode&0o777) == expect.ExpectMode.Perm()
		}
		match := statErr == nil && st.Mode&unix.S_IFMT == unix.S_IFREG &&
			uint64(st.Dev) == expect.Dev && st.Ino == expect.Ino && digestOK && modeOK
		if !match {
			beforeRestoreExchange() // test seam: inject a write to baseName here

			// Restore: swap back so baseName holds the original entry
			// again, untouched.
			if restoreErr := unix.Renameat2(dirFd, tmpName, dirFd, baseName, unix.RENAME_EXCHANGE); restoreErr != nil {
				return true, fmt.Errorf("workspace: %q identity drifted and restore-swap failed — directory may be inconsistent: %w", baseName, restoreErr)
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
				return false, fmt.Errorf("workspace: %q identity drifted and was refused and restored, but %q no longer identifies our own discarded content after a concurrent write during restore — leaving it in place rather than risk deleting someone else's data (needs manual recovery)",
					baseName, tmpName)
			}
			unix.Unlinkat(dirFd, tmpName, 0)
			if statErr != nil {
				return false, fmt.Errorf("workspace: %q: could not verify displaced identity after swap (fail closed, restored): %w", baseName, statErr)
			}
			if digestErr != nil {
				return false, fmt.Errorf("workspace: %q: could not verify displaced content digest after swap (fail closed, restored): %w", baseName, digestErr)
			}
			return false, fmt.Errorf("workspace: %q identity drifted (mode=%o,dev=%d,ino=%d,digestOK=%v,modeOK=%v, expected regular file dev=%d,ino=%d) — refusing to replace (fail closed, restored)",
				baseName, st.Mode, st.Dev, st.Ino, digestOK, modeOK, expect.Dev, expect.Ino)
		}
		if err := unix.Unlinkat(dirFd, tmpName, 0); err != nil {
			return true, fmt.Errorf("workspace: %q: replaced but could not remove displaced original %q: %w", baseName, tmpName, err)
		}
		committed = true
	}

	if err := fsyncDirHook(dirFd); err != nil {
		return committed, fmt.Errorf("workspace: fsync directory after replacing %q: %w", baseName, err)
	}
	return committed, nil
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

// readFileBeneathWithStat is ReadFileBeneath's and captureFileBeneath's
// shared core: one descriptor-relative open, one Fstat and one read off
// the SAME fd — never a separate stat-then-reopen pair, which would
// leave a window for in-place content mutation (same inode, e.g.
// truncate+rewrite) to go undetected between the two calls (code-review
// finding, codex, transaction.go round 1).
func readFileBeneathWithStat(dirFd int, baseName string) ([]byte, unix.Stat_t, error) {
	var st unix.Stat_t
	if err := validateBaseName(baseName); err != nil {
		return nil, st, err
	}
	how := unix.OpenHow{
		Flags:   unix.O_RDONLY | unix.O_NOFOLLOW | unix.O_CLOEXEC,
		Resolve: resolveFlags,
	}
	fd, err := unix.Openat2(dirFd, baseName, &how)
	if err != nil {
		return nil, st, fmt.Errorf("workspace: read %q: %w", baseName, err)
	}
	f := os.NewFile(uintptr(fd), baseName)
	defer f.Close()
	if err := unix.Fstat(fd, &st); err != nil {
		return nil, st, fmt.Errorf("workspace: read %q: stat: %w", baseName, err)
	}
	if st.Mode&unix.S_IFMT != unix.S_IFREG {
		return nil, st, fmt.Errorf("workspace: %q is not a regular file (fail closed)", baseName)
	}
	data, err := io.ReadAll(f)
	if err != nil {
		return nil, st, fmt.Errorf("workspace: read %q: %w", baseName, err)
	}
	return data, st, nil
}

// ReadFileBeneath descriptor-relatively reads the complete content of
// baseName within dirFd. Refuses if baseName is a symlink or any other
// non-regular node: the same openat2 resolveFlags every other operation
// in this package uses.
func ReadFileBeneath(dirFd int, baseName string) ([]byte, error) {
	data, _, err := readFileBeneathWithStat(dirFd, baseName)
	return data, err
}

// CaptureFileBeneath is ReadFileBeneath plus the identity and mode from
// the SAME open — used by transaction.go's prepare pass (and by callers
// outside this package needing a file's original mode, e.g. symedit's
// orchestration preserving it across a content-only edit) so a file's
// before-content, before-identity, and before-mode all come from one
// atomic view of one inode, never two independent opens that could
// straddle an in-place mutation.
func CaptureFileBeneath(dirFd int, baseName string) (content []byte, dev, ino uint64, mode fs.FileMode, err error) {
	data, st, err := readFileBeneathWithStat(dirFd, baseName)
	if err != nil {
		return nil, 0, 0, 0, err
	}
	return data, uint64(st.Dev), st.Ino, fs.FileMode(st.Mode & 0o777), nil
}

// RemoveFileWithExpectedIdentity descriptor-relatively removes baseName
// within dirFd, but ONLY if it is still the exact regular file matching
// expect — used by transaction.go's rollback path to undo a file this
// same process just created. expect's MustNotExist is ignored (removal
// has no "must be new" case); Dev/Ino/ContentDigest/CheckMode+ExpectMode
// are all checked against the displaced entry exactly as WriteFileBeneath's
// replace path checks them.
//
// Design (code-review finding, codex, round 3 — a THIRD iteration on
// this function: round 1 was plain fstatat-then-unlinkat, round 2's
// "fix" was a tombstone-exchange whose own success path still separately
// re-stat'd-then-unlinked the PUBLIC name baseName, narrowing but not
// eliminating the exact same check-then-act race class): rename baseName
// to a private, unpredictable quarantine name FIRST and unconditionally
// — this is the ENTIRE identity-capture step, one syscall, no partner
// file needed (RENAME_EXCHANGE required a tombstone to swap WITH; a
// plain rename needs nothing, since the goal here is simply "vacate
// baseName atomically," not "replace it with something else"). baseName
// now definitively does not exist — there is no public name left for a
// concurrent writer to race a cleanup unlink against, closing the
// class of bug entirely rather than narrowing its window. Verify the
// quarantined entry matches expect; on match, delete it (baseName was
// already vacated, nothing further to touch). On mismatch, rename it
// BACK to baseName via RENAME_NOREPLACE — refusing (and preserving the
// content under the quarantine name for manual recovery) if baseName is
// now occupied by something a concurrent creator placed there while
// this function was vacating and checking.
func RemoveFileWithExpectedIdentity(dirFd int, baseName string, expect TargetExpectation) error {
	if err := validateBaseName(baseName); err != nil {
		return err
	}
	quarName, err := tempName()
	if err != nil {
		return err
	}

	// RENAME_NOREPLACE (code-review finding, codex, round 4): a plain
	// Renameat would silently CLOBBER quarName if it already existed
	// (e.g. a stale .nexus-workspace-tmp-* artifact) — tempName's 128-bit
	// randomness makes collision astronomically unlikely, but this is a
	// destructive, fail-closed boundary; the kernel provides exact
	// no-clobber semantics at zero architectural cost, so use them.
	if err := unix.Renameat2(dirFd, baseName, dirFd, quarName, unix.RENAME_NOREPLACE); err != nil {
		return fmt.Errorf("workspace: remove %q: could not quarantine for identity verification (fail closed): %w", baseName, err)
	}
	beforeQuarantineVerify() // test seam: inject a concurrent create at baseName here

	// baseName no longer exists; quarName holds whatever was displaced
	// from it. Verify before deleting.
	var st unix.Stat_t
	statErr := unix.Fstatat(dirFd, quarName, &st, unix.AT_SYMLINK_NOFOLLOW)
	digestOK, modeOK := true, true
	var digestErr error
	if statErr == nil && expect.ContentDigest != "" {
		displaced, rerr := ReadFileBeneath(dirFd, quarName)
		if rerr != nil {
			digestErr = rerr
			digestOK = false
		} else {
			sum := sha256.Sum256(displaced)
			digestOK = hex.EncodeToString(sum[:]) == expect.ContentDigest
		}
	}
	if statErr == nil && expect.CheckMode {
		modeOK = fs.FileMode(st.Mode&0o777) == expect.ExpectMode.Perm()
	}
	match := statErr == nil && st.Mode&unix.S_IFMT == unix.S_IFREG &&
		uint64(st.Dev) == expect.Dev && st.Ino == expect.Ino && digestOK && modeOK
	if !match {
		if restoreErr := unix.Renameat2(dirFd, quarName, dirFd, baseName, unix.RENAME_NOREPLACE); restoreErr != nil {
			return fmt.Errorf("workspace: remove %q: identity drifted, and restoring it failed (baseName may now be occupied by something else) — content preserved under %q for manual recovery: %w",
				baseName, quarName, restoreErr)
		}
		if statErr != nil {
			return fmt.Errorf("workspace: remove %q: could not verify displaced identity after quarantine (fail closed, restored): %w", baseName, statErr)
		}
		if digestErr != nil {
			return fmt.Errorf("workspace: remove %q: could not verify displaced content digest after quarantine (fail closed, restored): %w", baseName, digestErr)
		}
		return fmt.Errorf("workspace: remove %q: identity drifted (mode=%o,dev=%d,ino=%d,digestOK=%v,modeOK=%v, expected regular file dev=%d,ino=%d) — refusing to remove (fail closed, restored)",
			baseName, st.Mode, st.Dev, st.Ino, digestOK, modeOK, expect.Dev, expect.Ino)
	}
	// Verified: quarName holds the exact expected target — delete it.
	// baseName was already vacated by the initial rename; nothing more
	// to touch there, so there is no further check-then-act window.
	if err := unix.Unlinkat(dirFd, quarName, 0); err != nil {
		return fmt.Errorf("workspace: remove %q: verified but could not remove quarantined target %q: %w", baseName, quarName, err)
	}
	if err := unix.Fsync(dirFd); err != nil {
		return fmt.Errorf("workspace: remove %q: fsync directory after removing: %w", baseName, err)
	}
	return nil
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
// tempName is a var (not a plain func) so descriptor_test.go can force a
// deterministic, predictable, colliding name to test the no-clobber
// guarantees around it — production always uses the real random name.
var tempName = func() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("workspace: generating temp file name: %w", err)
	}
	return ".nexus-workspace-tmp-" + hex.EncodeToString(b), nil
}
