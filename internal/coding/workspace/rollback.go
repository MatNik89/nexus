//go:build linux

// rollback.go is the restart-time ROLLBACK WRITE primitive — the other
// half of invariant 4 steps 6/7's MID_CRASH recovery (recovery.go's
// ClassifyBundle is the read-only "what happened" half; this is the
// write side). Given a sealed Bundle, restores every file currently
// matching AFTER back to its captured BEFORE image (or removes it, for
// a file that did not exist before), then re-classifies the WHOLE
// bundle to report the state actually achieved — never assumed from
// write success/failure alone.
//
// SCOPE OF THIS PIECE (deliberately, matching this whole program's own
// build order — a pure primitive before the governed operation that
// wraps it, the SAME sequencing transaction.go's own apply() had before
// governed.go's ApplyGoverned): this is the WRITE primitive only. NOT
// built here, as its own later, materially larger increment: the
// governed `PolicyWorkspaceRollback` S7 operation (its own durable
// grant, Begin/Next/Consume/Report/Reconcile lifecycle, exact-intent
// bound to the original operation's identity + the sealed-bundle digest
// — invariant 4 §2's own spec) that WRAPS this primitive, decides
// whether to retry it, and — only once it fully succeeds — lets the
// ORIGINAL Apply operation itself Reconcile and become eligible for
// Next again.
//
// rollbackBundle is package-private (code-review finding, round 1 HIGH
// #1: exporting a physical recovery-write primitive with no S7 grant
// governing it directly contradicts invariant 2's "every physical
// attempt carries an AttemptGrant" — the EXACT reason transaction.go's
// own apply was unexported in the original S7-wiring review). It stays
// private until the governed PolicyWorkspaceRollback operation exists to
// be its sole caller, mirroring ApplyGoverned/apply's own relationship
// exactly.
package workspace

import (
	"context"
	"errors"
	"fmt"
	"io/fs"

	"golang.org/x/sys/unix"
)

// rollbackBundle restores every file in a sealed Bundle's write set
// toward its captured BEFORE image, using the SAME descriptor-relative
// primitives apply()'s own same-process rollback (rollbackAndReport)
// uses — writeFileBeneath/removeFileWithExpectedIdentity — but driven
// purely from the bundle's own recorded before/after images, since no
// in-memory prepared/written bookkeeping survives a crash. Each file's
// CURRENT device+inode is captured FRESH, immediately before its own
// restore attempt (the SAME "precondition checked at the operation's
// own capture point, never trusted from an earlier pass" discipline
// apply() itself already uses) — the expected CONTENT/MODE checked
// atomically against that fresh identity is the bundle's own recorded
// After image, so a file that drifted to something else in the exact
// instant between this function's upfront classification and its own
// restore attempt is safely refused, not silently overwritten.
//
// b is validated with the SAME shape rules DecodeBundle itself enforces
// (validateBundleShape, factored out of recovery.go) before ANY write is
// attempted — code-review finding, round 1 HIGH #3: a directly-
// constructed Bundle (never reachable through the real apply()→
// DecodeBundle path, but reachable by any caller holding a Bundle value)
// naming a duplicate (rel_dir, base_name) target caused a real write
// before the ambiguity was ever detected.
//
// A file already matching BEFORE is left untouched — NOT merely an
// efficiency skip (an earlier version of this doc comment wrongly
// claimed that): for a NO-OP mutation whose Before and After images are
// byte-identical, the CURRENT on-disk state is indistinguishable from
// AFTER by content/mode alone, so restoreOneFile's own atomic check
// would happily "restore" it anyway — an unnecessary physical rewrite
// that changes the file's inode for no reason (code-review finding,
// round 1 MEDIUM: reproduced live via a real no-op mutation, ablating
// this skip changed the file's inode with no content difference to
// justify it). This skip is what makes that case a true no-op.
//
// If ANY file is already FOREIGN before this attempt starts, NOTHING is
// written at all — never a guess, matching ClassifyBundle's own
// contract. A per-file restore failure during the attempt does not
// abort the pass: every OTHER file is still attempted, and the returned
// classification (a fresh ClassifyBundle pass AFTER attempting every
// restore) reports the state ACTUALLY achieved, which may still be
// MID_CRASH (some files restored, some not — safe to call again) rather
// than fully rolled back. Every per-file failure AND a cancellation that
// stopped the pass early are joined into the returned error ALONGSIDE
// the classification — never silently discarded (code-review finding,
// round 1 HIGH #2, reproduced live: a write that lands its content
// successfully but then fails ITS OWN durability fsync leaves the
// content looking like a clean BEFORE state to a later re-classify,
// while the write's own durability was never actually proven — a caller
// that only looked at the returned TransactionState, with no visibility
// into that failure, could wrongly treat the rollback as fully,
// durably complete). A non-nil error alongside a non-nil classification
// means exactly this: "here is the truthful current state, but the
// ATTEMPT itself had problems — do not treat TransactionNeverStarted as
// a clean, fully-durable success without also checking this error."
//
// ctx is checked once per file, before that file's own restore attempt
// (mirroring apply()'s own per-file cancellation boundary), AND once
// more immediately after the loop ends — a per-file pre-check alone
// cannot catch cancellation landing DURING the LAST file's own restore
// (e.g. inside its trailing fsync), after which the loop simply runs
// out of files with no further pre-check ever firing (round 2 HIGH #1,
// reproduced live). Either way, a cancelled context is folded into the
// returned error, and the final re-classify still runs regardless,
// reporting the truth of whatever was actually achieved — never
// assuming success or failure.
//
// The returned TransactionState carries the SAME three meanings
// ClassifyBundle's own return does, reused rather than duplicated
// (topknot: minimize owned concepts) — read in THIS caller's context:
//   - TransactionNeverStarted: every file now matches BEFORE — the
//     rollback is COMPLETE (invariant 4's own "the complete write set
//     now equals the sealed BEFORE state") IF the returned error is
//     also nil — see above.
//   - TransactionMidCrash: still a mix of BEFORE/AFTER — some file(s)
//     could not be restored this pass; safe to call again (retryable).
//   - TransactionForeign: at least one file matches neither image —
//     refuse, manual reconciliation only, exactly as ClassifyBundle's
//     own contract already establishes.
func rollbackBundle(ctx context.Context, rootFd int, b Bundle) ([]FileClassification, TransactionState, error) {
	if err := validateBundleShape(b); err != nil {
		return nil, 0, err
	}
	before, beforeState, err := ClassifyBundle(rootFd, b)
	if err != nil {
		return nil, 0, err
	}
	if beforeState != TransactionMidCrash {
		// Already all-BEFORE (nothing to do) or already FOREIGN (refuse
		// outright) — both reported truthfully via the SAME upfront
		// classification; no write attempted either way. A per-file
		// Foreign classification is structurally impossible here: within
		// a MID_CRASH bundle ClassifyBundle's own summary logic already
		// guarantees anyForeign==false (a single Foreign file forces the
		// WHOLE bundle Foreign, never Mixed) — see recovery.go.
		return before, beforeState, nil
	}
	var attemptErrs []error
	cancelledDuringLoop := false
	for i, fr := range b.Files {
		if before[i].State != FileAfter {
			continue // already BEFORE — a true no-op, see this file's own doc comment above
		}
		if ctx.Err() != nil {
			attemptErrs = append(attemptErrs, fmt.Errorf("workspace: rollback: cancelled before restoring %q: %w", fr.BaseName, ctx.Err()))
			cancelledDuringLoop = true
			break // stop attempting further restores; the final re-classify below still reports the truth
		}
		if err := restoreOneFile(rootFd, fr); err != nil {
			attemptErrs = append(attemptErrs, err) // best-effort: keep trying the rest, report the truth via the final re-classify
		}
	}
	// A per-file pre-check only catches cancellation BETWEEN restores —
	// not cancellation landing DURING the LAST file's own restore itself
	// (e.g. inside its trailing fsync), after which the loop simply runs
	// out of files and falls through here with no further pre-check ever
	// firing. The SAME final-boundary race apply()'s own ctx check
	// (transaction.go) already guards against (code-review finding,
	// round 2 HIGH #1, reproduced live: cancelling exactly during the
	// one-and-only restore's own fsync produced a clean NEVER_STARTED,
	// nil-error result with no trace the attempt was ever cancelled).
	if ctx.Err() != nil && !cancelledDuringLoop {
		attemptErrs = append(attemptErrs, fmt.Errorf("workspace: rollback: cancelled during or immediately after the last restore attempt: %w", ctx.Err()))
	}
	classes, state, err := ClassifyBundle(rootFd, b)
	if err != nil {
		attemptErrs = append(attemptErrs, err)
		return classes, state, errors.Join(attemptErrs...)
	}
	if len(attemptErrs) == 0 {
		return classes, state, nil
	}
	return classes, state, errors.Join(attemptErrs...)
}

func restoreOneFile(rootFd int, fr FileRecord) error {
	dirFd, err := WalkDirBeneath(rootFd, fr.RelDir)
	if err != nil {
		return fmt.Errorf("workspace: rollback %q: %w", fr.BaseName, err)
	}
	defer unix.Close(dirFd)

	dev, ino, err := StatBeneath(dirFd, fr.BaseName)
	if err != nil {
		return fmt.Errorf("workspace: rollback %q: capture current identity: %w", fr.BaseName, err)
	}
	// Dev/Ino is a FRESH capture (nothing else survives a crash to know
	// it by); ContentDigest/ExpectMode is the bundle's OWN recorded
	// After image — what must STILL be found there, atomically
	// re-verified by writeFileBeneath/removeFileWithExpectedIdentity
	// themselves at the instant of the swap, never trusted from this
	// capture alone.
	expect := TargetExpectation{
		Dev: dev, Ino: ino,
		ContentDigest: sha256Hex(fr.After), ExpectMode: fs.FileMode(fr.AfterMode).Perm(), CheckMode: true,
	}
	if fr.Existed {
		_, err := writeFileBeneath(dirFd, fr.BaseName, fr.Before, fs.FileMode(fr.BeforeMode).Perm(), expect)
		return err
	}
	return removeFileWithExpectedIdentity(dirFd, fr.BaseName, expect)
}
