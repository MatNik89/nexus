//go:build linux

// governed_rollback.go is the S7/journal wiring for rollbackBundle —
// invariant 4 step 7's governed `PolicyWorkspaceRollback` operation. It
// is the ROLLBACK direction's exact counterpart to governed.go's
// ApplyGoverned: one governed attempt per call, exact-intent identity,
// durable STARTED-before-any-write pairing, and Report/Cancel branching
// on the primitive's own outcome — the SAME shape, deliberately, so this
// file reads as "governed.go, but for rollback" rather than a new
// design.
//
// IDENTITY — bound to the ORIGINAL Apply operation's own identity AND
// the sealed-bundle digest being rolled back (invariant 4 §2's own
// spec: "Its identity binds the ORIGINAL operation's identity, profile,
// workspace identity, and the sealed-bundle digest"). originalOp's own
// string already encodes profile + workspace root identity + plan
// digest (ApplyOperationID's own format), so embedding it verbatim
// transitively carries all of that — RollbackOperationID does not
// re-derive those components separately. The rollback's TARGET is the
// SAME resource the original Apply operation is bound to (ApplyTargetID,
// reused directly, not a separate wrapper).
//
// AUTHENTICATION — the caller supplies ONLY originalOp; the sealed-
// bundle digest is NEVER accepted as caller input (code-review finding,
// round 1 HIGH #1, reproduced live: an earlier version trusted a bare
// caller-supplied bundleDigest with zero cross-check, so a forged or
// stale digest could be rolled back "on behalf of" any operation ID
// string, real or not). This function instead calls scan.go's own
// RestartScan and requires originalOp to appear among its findings —
// which ALREADY proves, via already-5-times-reviewed machinery: S7
// authoritatively reports it UNKNOWN (a genuine crash-orphan, never a
// forged string); it targets THIS root; it matches PolicyWorkspaceApply;
// and its BundleDigest was confirmed via the SAME atomic
// s7.attempt_started/workspace.apply_started pairing RestartScan's own
// discovery already verifies. Reusing RestartScan here also closes
// round 1 HIGH #2 for free: RestartScan itself already refuses when
// grants is not bound to j.
//
// RECONCILIATION PREDICATE — "the complete write set now equals the
// sealed BEFORE state" (invariant 4 §2), reconciled against TWO rounds
// of adversarial findings on the outcome-branching priority order
// (round 1 HIGH #4: cancellation must never mask a FOREIGN result or a
// real non-cancellation error joined alongside it; round 1 HIGH #5: an
// unrecognized TransactionState must fail closed, never default to
// success; a SEPARATE reviewer's round 1 finding: TransactionMidCrash
// paired with a real per-file error — rollbackBundle's own NORMAL,
// EXPECTED shape for "partial progress, safe to retry" per its own doc
// comment — was being swallowed by a generic "any error -> Unknown"
// check that ran BEFORE the MidCrash check, making the entire
// MaxAttempts:5 retry budget UNREACHABLE in production for the one
// realistic partial-failure case it exists for). The final, reconciled
// priority order (state checked BEFORE error, mirroring ApplyGoverned's
// OWN established precedent of checking transaction.ErrRollbackIncomplete
// before its cancellation check):
//  1. TransactionForeign -> Unknown, ALWAYS, regardless of rollbackErr or
//     cancellation (invariant 4's own "manual reconciliation only, under
//     any circumstance").
//  2. TransactionMidCrash -> FailedRetryable with CodeRollbackIncomplete,
//     ALWAYS, regardless of rollbackErr — MidCrash IS the safe-to-retry
//     narrative by rollbackBundle's own contract; retrying costs nothing
//     (idempotent, fresh-identity-per-call) whether the incompleteness
//     came from a real write failure or a cancellation landing mid-pass.
//  3. TransactionNeverStarted with a non-nil, non-cancellation error ->
//     Unknown — content LOOKS correct but the attempt had a real problem
//     (durability unproven), never a guessed success.
//  4. TransactionNeverStarted with a PURELY cancellation-shaped error ->
//     Cancelled — matches ApplyGoverned's own identical "verified clean,
//     but told to stop" precedent.
//  5. TransactionNeverStarted with no error at all -> Succeeded.
//  6. Any other TransactionState value (should never happen given
//     ClassifyBundle's own closed 3-value contract, but never assumed)
//     -> Unknown, fail closed.
//
// SELF-RECOVERY — a crash DURING a PRIOR call to THIS SAME rollback
// operation (originalOp, bundleDigest) rehydrates IT to UNKNOWN too, the
// identical fate the original Apply operation suffers (round 1 HIGH #3,
// reproduced live: without this, RollbackGoverned's own Next call
// refuses forever once its own operation is UNKNOWN — "operation ... is
// UNKNOWN: ATTEMPT_NOT_AUTHORIZED" — permanently stranding it). Before
// ever calling Begin, this function checks whether ITS OWN operation
// identity is already UNKNOWN and, if so, re-classifies the bundle and
// calls Reconcile on itself first — the SAME machinery invariant 4 §2
// specifies ("a mixed BEFORE/AFTER state remaining -> rollback
// Reconcile(false) then Next on that SAME rollback operation") — falling
// through to a genuine new attempt only when Reconcile leaves it
// retry-eligible. This check runs BEFORE the "originalOp is currently
// MID_CRASH" eligibility gate below, not after (round 1-bis finding,
// reproduced live: a PRIOR rollback attempt that fully restored every
// file but crashed before Report leaves originalOp's OWN current scan
// reading NEVER_STARTED, not MID_CRASH, since the physical work is
// already done — gating self-recovery behind MID_CRASH made it
// unreachable in precisely that case).
//
// SCOPE OF THIS PIECE (deliberately, matching this whole program's own
// build order): THIS operation's own governed lifecycle, including its
// OWN restart-recovery. Explicitly NOT built here, as its own later,
// separate increment: the LINKAGE step — calling Reconcile(false) on the
// ORIGINAL Apply operation once THIS rollback operation's own success is
// durable (invariant 4 §2: "ONLY after the rollback operation itself
// succeeds may the ORIGINAL Apply operation call Reconcile(false) and
// become eligible for Next again"). That linkage is its own
// orchestration concern with its own error-handling questions (what
// happens if the rollback succeeds but the original op's own Reconcile
// then fails?) deserving its own reviewable increment, exactly how
// scan.go's own "report MID_CRASH, but do not act on it" boundary was
// drawn one piece before this one.
//
// DURABILITY ACROSS RETRIES — three rounds of independently-wrong fixes
// before landing on the actual answer, kept here as the record of why:
//
// round 2 (codex) deferred the whole question: "rollbackBundle's per-file
// skip (already matches BEFORE, leave untouched — rollback.go's own doc
// comment) has no memory of whether an EARLIER attempt's write to reach
// that content genuinely completed its own durability barrier (fsync),
// but retrying is always safe by construction, so this is out of scope."
//
// round 3 (codex) proved that deferral WRONG with a live two-call
// reproduction: attempt 1 restores file A but its directory fsync fails
// (file B stays AFTER, bundle correctly MID_CRASH, safely retried);
// attempt 2 restores B cleanly but SKIPS A (already BEFORE content-wise)
// — the bundle now reads NEVER_STARTED with NO error at all, and the
// un-augmented code reported SUCCEEDED despite A's own restore
// durability never having been proven. round 3's fix: flag
// UnverifiedDurability whenever ANY file classified BEFORE while the
// bundle overall was still MID_CRASH, and refuse a LATER Succeeded claim
// if this operation's own durable history ever recorded that flag.
//
// round 4/5 (codex, kilo, agy, independently and unanimously) proved
// round 3's classification-only signal ALSO wrong: it flags every
// ordinary partial-progress MID_CRASH landing, including ones where NO
// file's durability is actually in question, permanently stranding the
// operation on the very retries MaxAttempts:5 exists to allow. Narrowing
// the signal to "MID_CRASH with a real, non-cancellation rollbackErr"
// (round 4's own first correction attempt) was ALSO independently
// disproven live by all three agents: a real per-file error on a file
// that never even reached BEFORE (e.g. a transient EACCES/ENOSPC on its
// temp-file create, content untouched) still poisons the WHOLE
// operation's future success, and a real error on one file can equally
// blame an entirely unrelated file that restored cleanly in the SAME
// attempt — rollback.go's own joined attemptErrs carries no per-file
// attribution, so no formula built from "rollbackErr present" alone (nor
// from classification alone) can be both sound and complete at this API
// boundary.
//
// The actual fix (round 5), WITHOUT reopening rollback.go's own
// converged write/skip logic and WITHOUT any cross-attempt history or
// per-file error attribution at all: stop trying to INFER past
// durability and instead re-PROVE it, fresh, at the one moment it
// matters — immediately before ever reporting Succeeded (both in the
// normal attempt path and in reconcileRehydratedRollback's own
// crash-recovery path). See verifyDurabilityBeforeSuccess's own doc
// comment for why this is possible without rollback.go's help: a file
// can only ever classify BEFORE after its OWN content fsync already
// succeeded (writeFileBeneath returns early, before any rename, on a
// content-fsync failure), so the ONLY thing that can still be unproven
// for a BEFORE file is its containing directory's own rename-fsync — and
// fsyncing that directory again, right now, either durably proves it or
// honestly refuses (manual reconciliation), with no guessing either way.
// This also removes the self-recovery path's own former asymmetry
// (documented in earlier rounds as an "accepted ceiling" for lack of any
// rollbackErr to consult there): it gets the exact same fresh, real proof
// as the normal path, so it is no longer a ceiling at all.
package workspace

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/MatNik89/nexus/internal/foundation/sealedstore"
	"github.com/MatNik89/nexus/internal/kernel/contracts"
	"github.com/MatNik89/nexus/internal/kernel/journal"
	"github.com/MatNik89/nexus/internal/kernel/s7"
	"golang.org/x/sys/unix"
)

// PolicyWorkspaceRollback governs one restart-time rollback operation:
// irreversible (a filesystem mutation, the SAME EffectClass
// PolicyWorkspaceApply itself uses), durable. UNLIKE PolicyWorkspaceApply's
// own deliberate MaxAttempts:1 (which exists because a real Apply retry
// needs its post-rollback preconditions rebuilt non-forgeably — see
// ApplyGoverned's own doc comment), rollbackBundle's own retries are
// safe-by-construction: every call independently re-classifies the
// bundle and re-captures each file's identity fresh, so no stale
// precondition can ever accumulate across attempts. A real retry budget
// is therefore the correct DEFAULT here, not an opt-in override.
var PolicyWorkspaceRollback = s7.Policy{
	EffectClass:    contracts.EffectIrreversible,
	MaxAttempts:    5,
	AttemptTimeout: 30 * time.Second,
	Deadline:       5 * time.Minute,
	Backoff:        s7.BackoffPolicy{Base: 2 * time.Second, Max: 30 * time.Second},
	RetryableCodes: []string{s7.CodeRollbackIncomplete},
	Durable:        true,
}

// The workspace.rollback_* durable event vocabulary — the rollback
// direction's own counterparts to governed.go's EvApplyStarted/
// EvMutationCommitted/EvApplyFailed.
const (
	EvRollbackStarted   = "workspace.rollback_started"
	EvRollbackCommitted = "workspace.rollback_committed"
	EvRollbackFailed    = "workspace.rollback_failed"
)

type rollbackStartedPayload struct {
	Op           string `json:"op"`
	OriginalOp   string `json:"original_op"`
	BundleDigest string `json:"bundle_digest"`
}
type rollbackCommittedPayload struct {
	Op           string `json:"op"`
	OriginalOp   string `json:"original_op"`
	BundleDigest string `json:"bundle_digest"`
}
type rollbackFailedPayload struct {
	Op           string `json:"op"`
	OriginalOp   string `json:"original_op"`
	BundleDigest string `json:"bundle_digest"`
	AttemptNo    int    `json:"attempt_no"`
	Landing      int    `json:"landing"`
	Code         string `json:"code,omitempty"`
}

// rollbackEventValidators is merged into governed.go's own Events() —
// kept in this file so governed.go never needs to know rollback's own
// payload shapes, while still producing ONE unified map from ONE
// exported entry point (no caller can forget to register half of it).
// The started/committed validators require p.Op to be EXACTLY
// RollbackOperationID(p.OriginalOp, p.BundleDigest) — not merely
// non-empty (code-review finding, round 1 MEDIUM #6, reproduced live: S7
// itself only checks the structural Companion.Key, never the payload
// JSON's own internal consistency, so a crafted/buggy payload could
// otherwise name an Op that disagrees with its own OriginalOp+
// BundleDigest fields).
func rollbackEventValidators() map[string]journal.PayloadValidator {
	return map[string]journal.PayloadValidator{
		EvRollbackStarted: func(raw json.RawMessage) error {
			var p rollbackStartedPayload
			if err := strictDecodeEvent(raw, &p); err != nil {
				return err
			}
			if p.Op == "" || p.OriginalOp == "" {
				return fmt.Errorf("workspace: rollback_started requires op and original_op")
			}
			if !validSHA256Hex(p.BundleDigest) {
				return fmt.Errorf("workspace: rollback_started requires a valid sha256-hex bundle_digest")
			}
			if p.Op != string(RollbackOperationID(contracts.OperationID(p.OriginalOp), p.BundleDigest)) {
				return fmt.Errorf("workspace: rollback_started names an op inconsistent with its own original_op+bundle_digest (fail closed)")
			}
			return nil
		},
		EvRollbackCommitted: func(raw json.RawMessage) error {
			var p rollbackCommittedPayload
			if err := strictDecodeEvent(raw, &p); err != nil {
				return err
			}
			if p.Op == "" || p.OriginalOp == "" {
				return fmt.Errorf("workspace: rollback_committed requires op and original_op")
			}
			if !validSHA256Hex(p.BundleDigest) {
				return fmt.Errorf("workspace: rollback_committed requires a valid sha256-hex bundle_digest")
			}
			if p.Op != string(RollbackOperationID(contracts.OperationID(p.OriginalOp), p.BundleDigest)) {
				return fmt.Errorf("workspace: rollback_committed names an op inconsistent with its own original_op+bundle_digest (fail closed)")
			}
			return nil
		},
		EvRollbackFailed: func(raw json.RawMessage) error {
			var p rollbackFailedPayload
			if err := strictDecodeEvent(raw, &p); err != nil {
				return err
			}
			if p.Op == "" || p.OriginalOp == "" || p.AttemptNo < 0 {
				return fmt.Errorf("workspace: rollback_failed requires op, original_op, and attempt_no>=0 (0 means no attempt was ever consumed)")
			}
			if !validSHA256Hex(p.BundleDigest) {
				return fmt.Errorf("workspace: rollback_failed requires a valid sha256-hex bundle_digest")
			}
			// round 2 MEDIUM #6's own Op-consistency check applied to
			// started/committed but not failed (round 3 MEDIUM finding,
			// codex, reproduced live: a forged payload naming an
			// unbound op string — never authenticated against any real
			// original_op+bundle_digest pair — was accepted).
			if p.Op != string(RollbackOperationID(contracts.OperationID(p.OriginalOp), p.BundleDigest)) {
				return fmt.Errorf("workspace: rollback_failed names an op inconsistent with its own original_op+bundle_digest (fail closed)")
			}
			if !validApplyFailedNarrative(p.AttemptNo, p.Landing, p.Code, s7.CodeRollbackIncomplete) {
				return fmt.Errorf("workspace: rollback_failed names an impossible narrative (attempt_no=%d, landing=%d, code=%q)", p.AttemptNo, p.Landing, p.Code)
			}
			return nil
		},
	}
}

// rollbackFn is a test seam: rollbackBundle in production. Tests
// substitute it to deterministically control RollbackGoverned's own
// branching (a specific classification/error outcome) without needing a
// precisely-timed real filesystem race — the SAME pattern governed.go's
// own applyFn/deriveAttemptContext seams establish.
var rollbackFn = rollbackBundle

// RollbackOperationID derives the EXACT-INTENT S7 operation identity for
// one rollback attempt: the ORIGINAL Apply operation's own identity
// (verbatim — already carries profile + workspace root identity + plan
// digest) plus the sealed-bundle digest being rolled back. Two calls
// with the identical (originalOp, bundleDigest) pair always name the
// SAME operation; any difference in either names a DIFFERENT one.
func RollbackOperationID(originalOp contracts.OperationID, bundleDigest string) contracts.OperationID {
	return contracts.OperationID(fmt.Sprintf("workspace.rollback:%s:%s", originalOp, bundleDigest))
}

// RollbackResult reports what RollbackGoverned actually did.
type RollbackResult struct {
	// Classes is rollbackBundle's own final, post-attempt classification
	// — the truthful per-file state, regardless of outcome.
	Classes []FileClassification
	State   TransactionState
	// Succeeded is true ONLY once this rollback operation's own S7
	// Report/Reconcile(Succeeded) landed durably — State==
	// TransactionNeverStarted with no attempt error at all. False for
	// every other outcome, including a State that LOOKS like success but
	// carried an unresolved attempt error (see this file's own doc
	// comment).
	Succeeded bool
}

// isPureCancellation reports whether err is ENTIRELY explained by
// context cancellation/deadline — recursively, through an errors.Join
// tree — with no OTHER cause mixed in. nil is not a pure cancellation
// (there is nothing to attribute at all).
func isPureCancellation(err error) bool {
	if err == nil {
		return false
	}
	if joined, ok := err.(interface{ Unwrap() []error }); ok {
		for _, e := range joined.Unwrap() {
			if !isPureCancellation(e) {
				return false
			}
		}
		return true
	}
	// A single %w-wrapped error (fmt.Errorf, unlike errors.Join, wraps
	// exactly one) must be unwrapped and recursively inspected too —
	// otherwise a cancellation wrapped one level deeper than an
	// errors.Join tree (e.g. fmt.Errorf("...: %w", errors.Join(ctx.Canceled,
	// realErr))) skips the multi-error branch above entirely and falls
	// straight to errors.Is below, which would find context.Canceled
	// reachable via errors.Is's own Unwrap-chain walk without ever
	// checking whether realErr is ALSO present — silently masking it
	// (round 2 finding, agy).
	if wrapped, ok := err.(interface{ Unwrap() error }); ok {
		return isPureCancellation(wrapped.Unwrap())
	}
	return errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded)
}

// verifyDurabilityBeforeSuccess re-proves, RIGHT NOW, that every file in
// b is durably at BEFORE — the required check immediately before EVER
// reporting a rollback operation Succeeded (round 3 HIGH finding, codex,
// further corrected round 4/5/6 after codex/kilo/agy independently
// live-reproduced that neither a classification-only signal, NOR an
// attempt-error-based signal, NOR a directory-only re-fsync of a STALE
// classification can be trusted: see this file's own top-of-file doc
// comment's "DURABILITY ACROSS RETRIES" section for the full history).
// Callers only ever reach this once the bundle's own classification
// already reads TransactionNeverStarted (every file matches BEFORE), so
// b.Files itself — not a separately-threaded classification slice — is
// the complete, correct set to re-verify.
//
// For each file that Existed (a real BEFORE image, not "should not
// exist"): re-fsync the file's OWN content, THEN re-fsync its containing
// directory (deduplicated). round 4/5's fix only re-fsynced directories,
// reasoning that writeFileBeneath's own contract guarantees a file can
// only classify BEFORE after its OWN content fsync already succeeded —
// TRUE for a file rollback.go itself just wrote, but round 6 (codex,
// live-reproduced via a controlled overlay) proved it false in general:
// rollback.go's own converged skip logic never touches a file already
// reading BEFORE, so THAT file's content durability might never have
// gone through writeFileBeneath at all (a legitimate no-op mutation, or
// content that merely happens to match BEFORE for any other reason) —
// directory-only re-fsync proves the RENAME's durability but says
// nothing about whether the file's OWN bytes are flushed. Re-fsyncing
// the file's own fd, right now, closes that gap the identical way the
// directory fsync closes its own: ANY subsequent successful fsync call
// flushes whatever is CURRENTLY dirty for that inode to stable storage,
// regardless of who wrote it or when.
//
// Finally, AFTER every durability barrier above, this re-classifies the
// WHOLE bundle one more time and requires it to STILL read
// TransactionNeverStarted — closing the round-6 TOCTOU window (a
// concurrent writer landing between rollbackFn's own classification and
// this function's fsync calls could otherwise make a file FOREIGN while
// a STALE classification still drove a Succeeded report). That
// reclassification alone is STILL not sufficient (round 7 finding,
// codex, live-reproduced 3/3 via a Go build overlay): ClassifyBundle
// compares content/mode only, never inode identity, so a concurrent
// writer can swap in a DIFFERENT inode carrying byte-identical BEFORE
// content — one this function never fsynced at all — and the
// content-only reclassification cannot tell the difference. Closing
// THAT requires the SAME capture-identity-then-verify-identity
// discipline this package already uses within a single write
// (writeFileBeneath's own RENAME_EXCHANGE identity check;
// restoreOneFile's TargetExpectation capture) applied across this
// function's own two passes: identity is captured from the EXACT fd
// each fsync call touched, then re-checked by name AFTER the fresh
// reclassification confirms content/mode — refusing unless the SAME
// inode (file or directory) is still there. This is the LAST check
// before the caller ever calls Report(Succeeded)/Reconcile(true) — the
// smallest residual window this package can close without OS-level
// exclusive-access locking, which is out of scope here.
func verifyDurabilityBeforeSuccess(rootFd int, b Bundle) error {
	type identity struct{ dev, ino uint64 }
	fileID := make(map[string]identity, len(b.Files))
	dirID := make(map[string]identity, len(b.Files))
	seenDir := make(map[string]bool, len(b.Files))
	for _, fr := range b.Files {
		if fr.Existed {
			dev, ino, err := fsyncFileBeneath(rootFd, fr.RelDir, fr.BaseName)
			if err != nil {
				return fmt.Errorf("workspace: rollback governed: re-verify durability: fsync %q/%q: %w", fr.RelDir, fr.BaseName, err)
			}
			fileID[fr.RelDir+"/"+fr.BaseName] = identity{dev, ino}
		}
		if seenDir[fr.RelDir] {
			continue
		}
		seenDir[fr.RelDir] = true
		dirFd, err := WalkDirBeneath(rootFd, fr.RelDir)
		if err != nil {
			return fmt.Errorf("workspace: rollback governed: re-verify durability: open %q: %w", fr.RelDir, err)
		}
		var dst unix.Stat_t
		serr := unix.Fstat(dirFd, &dst)
		ferr := fsyncDirHook(dirFd)
		unix.Close(dirFd)
		if serr != nil {
			return fmt.Errorf("workspace: rollback governed: re-verify durability: stat %q: %w", fr.RelDir, serr)
		}
		if ferr != nil {
			return fmt.Errorf("workspace: rollback governed: re-verify durability: fsync %q: %w", fr.RelDir, ferr)
		}
		dirID[fr.RelDir] = identity{uint64(dst.Dev), dst.Ino}
	}
	_, state, err := ClassifyBundle(rootFd, b)
	if err != nil {
		return fmt.Errorf("workspace: rollback governed: re-verify durability: reclassify: %w", err)
	}
	if state != TransactionNeverStarted {
		return fmt.Errorf("workspace: rollback governed: re-verify durability: bundle no longer reads fully BEFORE after durability barriers (classifies %s) — a concurrent change is suspected, never a guessed success", state)
	}
	// Content/mode alone is not identity: re-check that the SAME inode
	// this pass actually fsynced is still the one the fresh
	// reclassification just observed (round 7 finding, codex).
	seenDir = make(map[string]bool, len(b.Files))
	for _, fr := range b.Files {
		if fr.Existed {
			dirFd, derr := WalkDirBeneath(rootFd, fr.RelDir)
			if derr != nil {
				return fmt.Errorf("workspace: rollback governed: re-verify durability: identity check open %q: %w", fr.RelDir, derr)
			}
			dev, ino, serr := StatBeneath(dirFd, fr.BaseName)
			unix.Close(dirFd)
			if serr != nil {
				return fmt.Errorf("workspace: rollback governed: re-verify durability: identity check stat %q/%q: %w", fr.RelDir, fr.BaseName, serr)
			}
			if want := fileID[fr.RelDir+"/"+fr.BaseName]; dev != want.dev || ino != want.ino {
				return fmt.Errorf("workspace: rollback governed: re-verify durability: %q/%q's identity changed since its own content was fsynced — a concurrent substitution is suspected, never a guessed success", fr.RelDir, fr.BaseName)
			}
		}
		if seenDir[fr.RelDir] {
			continue
		}
		seenDir[fr.RelDir] = true
		dirFd, derr := WalkDirBeneath(rootFd, fr.RelDir)
		if derr != nil {
			return fmt.Errorf("workspace: rollback governed: re-verify durability: identity check open %q: %w", fr.RelDir, derr)
		}
		var dst unix.Stat_t
		serr := unix.Fstat(dirFd, &dst)
		unix.Close(dirFd)
		if serr != nil {
			return fmt.Errorf("workspace: rollback governed: re-verify durability: identity check stat %q: %w", fr.RelDir, serr)
		}
		if want := dirID[fr.RelDir]; uint64(dst.Dev) != want.dev || dst.Ino != want.ino {
			return fmt.Errorf("workspace: rollback governed: re-verify durability: directory %q's identity changed since its own fsync — a concurrent substitution is suspected, never a guessed success", fr.RelDir)
		}
	}
	return nil
}

// fsyncFileBeneath descriptor-relatively opens baseName within dirRel
// (read-only — fsync needs no write access), fsyncs it, and returns the
// EXACT identity of the fd it fsynced — closing regardless of outcome.
// The SAME resolveFlags/O_NOFOLLOW discipline every other open in this
// package uses (readFileBeneathWithStat's own pattern) — never resolved
// by pathname, never follows a symlink. Returning the fsynced fd's own
// identity (rather than a separate later stat-by-name) lets
// verifyDurabilityBeforeSuccess's own later identity check detect ANY
// substitution, including one landing between this call and its own
// return.
func fsyncFileBeneath(rootFd int, dirRel, baseName string) (dev, ino uint64, err error) {
	dirFd, err := WalkDirBeneath(rootFd, dirRel)
	if err != nil {
		return 0, 0, err
	}
	defer unix.Close(dirFd)
	if err := validateBaseName(baseName); err != nil {
		return 0, 0, err
	}
	how := unix.OpenHow{Flags: unix.O_RDONLY | unix.O_NOFOLLOW | unix.O_CLOEXEC, Resolve: resolveFlags}
	fd, err := unix.Openat2(dirFd, baseName, &how)
	if err != nil {
		return 0, 0, err
	}
	defer unix.Close(fd)
	var st unix.Stat_t
	if err := unix.Fstat(fd, &st); err != nil {
		return 0, 0, err
	}
	if err := fsyncFileHook(fd); err != nil {
		return 0, 0, err
	}
	return uint64(st.Dev), st.Ino, nil
}

// fsyncFileHook is fsyncFileBeneath's own trailing fsync, indirected the
// SAME way fsyncDirHook (descriptor.go) indirects the directory fsync —
// a test seam to deterministically inject a content-durability failure
// (round 6 finding, kilo NOTE: without this, only the directory-fsync
// half of verifyDurabilityBeforeSuccess was fault-injectable).
var fsyncFileHook = unix.Fsync

// RollbackGoverned makes ONE governed rollback attempt against
// originalOp's own currently-authoritative sealed bundle (discovered and
// authenticated via RestartScan — see this file's own doc comment for
// why bundleDigest is never accepted as caller input). Matches
// ApplyGoverned's own single-attempt-per-call shape exactly: an external
// caller decides whether and when to call again.
func RollbackGoverned(ctx context.Context, rootFd int, store *sealedstore.Store, originalOp contracts.OperationID, grants *s7.Authority, j *journal.Journal, runID contracts.RunID) (RollbackResult, error) {
	if originalOp == "" || !runID.Valid() {
		return RollbackResult{}, fmt.Errorf("workspace: rollback governed: original op and run id are both required (fail closed)")
	}
	profile := j.Profile()
	var st unix.Stat_t
	if err := unix.Fstat(rootFd, &st); err != nil {
		return RollbackResult{}, fmt.Errorf("workspace: rollback governed: stat root: %w", err)
	}
	rootDev, rootIno := uint64(st.Dev), st.Ino

	findings, err := RestartScan(rootFd, j, grants, store)
	if err != nil {
		return RollbackResult{}, fmt.Errorf("workspace: rollback governed: restart scan: %w", err)
	}
	var finding *ScanFinding
	for i := range findings {
		if findings[i].Op == originalOp {
			finding = &findings[i]
			break
		}
	}
	if finding == nil {
		return RollbackResult{}, fmt.Errorf("workspace: rollback governed: %q is not a currently eligible (UNKNOWN, target/policy-matched, bundle-confirmed) workspace.apply operation for this root (fail closed)", originalOp)
	}
	if finding.State == TransactionForeign || finding.BundleDigest == "" {
		return RollbackResult{Classes: finding.Classes, State: finding.State},
			fmt.Errorf("workspace: rollback governed: original operation's bundle is FOREIGN or has no confirmed digest, manual reconciliation required")
	}
	bundleDigest := finding.BundleDigest

	raw, err := store.Get(bundleDigest)
	if err != nil {
		return RollbackResult{}, fmt.Errorf("workspace: rollback governed: fetch sealed bundle: %w", err)
	}
	b, err := DecodeBundle(raw)
	if err != nil {
		return RollbackResult{}, fmt.Errorf("workspace: rollback governed: decode sealed bundle: %w", err)
	}

	op := RollbackOperationID(originalOp, bundleDigest)
	target := ApplyTargetID(profile, rootDev, rootIno)

	// finding.State reflects the ORIGINAL operation's CURRENT on-disk
	// shape — which, if a PRIOR call to THIS SAME rollback op already
	// physically finished the restore before crashing (Report never
	// landed), now reads back as NEVER_STARTED, not MID_CRASH, even
	// though the rollback OPERATION's own durable record still needs to
	// catch up (round 1-bis finding, reproduced live: gating self-
	// recovery behind finding.State==MID_CRASH made it unreachable in
	// exactly the case it exists for — a rollback that fully completed
	// its writes but crashed before Report). So self-recovery is checked
	// FIRST, independent of finding.State; only once there is no prior
	// rollback-op record to recover does finding.State decide whether
	// there is anything left to physically do.
	if state, ok := grants.State(op); ok {
		if state == contracts.AttemptUnknown {
			// Before trusting an UNKNOWN op string as THIS SAME legitimate
			// rollback attempt, confirm S7 itself bound it to the ORIGINAL
			// operation's own target and policy — mirroring RestartScan's
			// identical defense-in-depth for the ORIGINAL Apply operation's
			// own identity (round 2 HIGH #3, reproduced live: a differently-
			// bound op that merely COLLIDES with this deterministic string
			// was reconciled to SUCCEEDED with zero verification it was ever
			// this package's own attempt in the first place).
			if boundTarget, tok := grants.Target(op); !tok || boundTarget != target {
				return RollbackResult{}, fmt.Errorf("workspace: rollback governed: %q is UNKNOWN but bound to a different target — refusing to self-recover an operation this call never began (fail closed)", op)
			}
			if matches, pok := grants.MatchesPolicy(op, PolicyWorkspaceRollback); !pok || !matches {
				return RollbackResult{}, fmt.Errorf("workspace: rollback governed: %q is UNKNOWN but does not match PolicyWorkspaceRollback — refusing to self-recover an operation this call never began (fail closed)", op)
			}
			result, retryEligible, rerr := reconcileRehydratedRollback(op, originalOp, bundleDigest, rootFd, b, grants, j, runID)
			if rerr != nil || !retryEligible {
				return result, rerr
			}
			// Reconcile left this SAME operation FAILED_RETRYABLE within
			// budget — fall through to Begin (a documented no-op on an
			// existing record) + Next to actually attempt it in THIS
			// process.
		}
		// Any OTHER existing state (most notably FAILED_RETRYABLE from a
		// prior call's own Report) falls through to Begin+Next below
		// regardless of finding.State — round 2 HIGH #2, reproduced live:
		// refusing here whenever finding.State != MID_CRASH stranded a
		// legitimately FAILED_RETRYABLE rollback op forever once the
		// physical write set happened to already read NEVER_STARTED
		// (e.g. an earlier attempt in THIS SAME retry sequence finished
		// the writes but reported a non-cancellation error alongside a
		// clean classification). Falling through is safe unconditionally:
		// rollbackFn's own upfront classification is a pure, zero-write
		// no-op whenever the bundle already reads NEVER_STARTED, so this
		// self-heals to Report(Succeeded) with no physical action taken.
	} else if finding.State != TransactionMidCrash {
		// No existing rollback-op record at all, AND the original
		// operation isn't currently MID_CRASH — genuinely nothing has
		// ever been attempted and there is nothing to do.
		return RollbackResult{Classes: finding.Classes, State: finding.State},
			fmt.Errorf("workspace: rollback governed: original operation's bundle classifies %s, not MID_CRASH — nothing for a physical rollback to do", finding.State)
	}

	if err := grants.Begin(op, target, PolicyWorkspaceRollback); err != nil {
		return RollbackResult{}, fmt.Errorf("workspace: rollback governed: %w", err)
	}

	// attemptNo captured ONCE, outside any lock S7 might be holding when
	// it later invokes a builder — the SAME same-goroutine-relock lesson
	// ApplyGoverned's own doc comment documents.
	attemptNo := grants.Attempts(op)
	failedBuild := func(l s7.Landing) s7.Companion {
		return s7.Companion{Key: op, Params: envelope(j, op, runID, attemptNo, EvRollbackFailed,
			rollbackFailedPayload{Op: string(op), OriginalOp: string(originalOp), BundleDigest: bundleDigest,
				AttemptNo: attemptNo, Landing: int(l.Kind), Code: l.Code})}
	}

	grant, err := grants.Next(op, failedBuild)
	if errors.Is(err, s7.ErrNotDue) {
		return RollbackResult{}, fmt.Errorf("workspace: rollback governed: %w", ErrRetryNotDue)
	}
	if err != nil {
		return RollbackResult{}, fmt.Errorf("workspace: rollback governed: %w", err)
	}

	// Unlike ApplyGoverned (which threads bindDurable INTO apply() as a
	// callback, because Apply must seal a BRAND NEW bundle and Consume
	// atomically right after, still before its first write),
	// rollbackBundle operates on a bundle that ALREADY EXISTS — there is
	// no "seal-then-immediately-Consume" ordering to preserve. Consuming
	// EAGERLY, here, before ever calling rollbackFn, already satisfies
	// "durable STARTED before any write."
	companion := s7.Companion{Key: op, Params: envelope(j, op, runID, grant.AttemptNo, EvRollbackStarted,
		rollbackStartedPayload{Op: string(op), OriginalOp: string(originalOp), BundleDigest: bundleDigest})}
	if err := grants.Consume(grant, companion); err != nil {
		if cerr := grants.Cancel(op, failedBuild); cerr != nil {
			return RollbackResult{}, fmt.Errorf("workspace: rollback governed: %w (cancel also failed: %v)", err, cerr)
		}
		return RollbackResult{}, fmt.Errorf("workspace: rollback governed: %w", err)
	}
	attemptNo = grant.AttemptNo
	actx, cancel, aerr := deriveAttemptContext(grants, ctx, op)
	if aerr != nil {
		// Consumed, but the attempt's own context could not be derived
		// (e.g. the deadline already expired in the gap between Next and
		// Consume) — rollbackFn is never even called; nothing was ever
		// attempted, so this is the SAME "never got to run" shape as a
		// cancellation, not a code-carrying failure.
		if cerr := grants.Cancel(op, failedBuild); cerr != nil {
			return RollbackResult{}, fmt.Errorf("workspace: rollback governed: %w (cancel also failed: %v)", aerr, cerr)
		}
		return RollbackResult{}, fmt.Errorf("workspace: rollback governed: %w", aerr)
	}
	attemptCtx, attemptCancel := actx, cancel

	classes, state, rollbackErr := rollbackFn(attemptCtx, rootFd, b)
	if attemptCancel != nil {
		attemptCancel()
	}
	result := RollbackResult{Classes: classes, State: state}

	switch {
	case state == TransactionForeign:
		// Invariant 4's own table: any FOREIGN file, manual
		// reconciliation only, ALWAYS — never masked by a concurrent
		// cancellation or attempt error (round 1 HIGH #4). rollbackErr
		// itself is still wrapped when present (round 3 finding, codex:
		// an earlier version discarded it here too, the SAME diagnostic
		// loss the MID_CRASH branch below was already fixed for).
		var foreignErr error
		if rollbackErr != nil {
			foreignErr = fmt.Errorf("workspace: rollback governed: sealed bundle is FOREIGN, manual reconciliation required: %w", rollbackErr)
		} else {
			foreignErr = fmt.Errorf("workspace: rollback governed: sealed bundle is FOREIGN, manual reconciliation required")
		}
		if rerr := grants.Report(op, s7.OutcomeUnknown, "", failedBuild); rerr != nil {
			return result, fmt.Errorf("%w (report unknown also failed: %v)", foreignErr, rerr)
		}
		return result, foreignErr
	case state == TransactionMidCrash:
		// Still a mix of BEFORE/AFTER this pass — safe to retry
		// REGARDLESS of rollbackErr, since MidCrash IS the expected
		// "partial progress" shape rollbackBundle's own contract
		// describes, whether caused by a real write failure or a
		// cancellation landing mid-pass (a SEPARATE reviewer's round 1
		// finding: checking rollbackErr!=nil BEFORE this case made the
		// entire retry budget unreachable for the one realistic
		// partial-failure scenario it exists for). rollbackErr itself is
		// still WRAPPED into the returned error (round 2 finding, codex:
		// an earlier version discarded it entirely, breaking
		// errors.Is/errors.As against the real underlying cause — e.g. a
		// per-file fsync failure — even though the durable S7 outcome
		// (FailedRetryable/CodeRollbackIncomplete) is unaffected either
		// way).
		var incompleteErr error
		if rollbackErr != nil {
			incompleteErr = fmt.Errorf("workspace: rollback governed: rollback incomplete this pass, safe to retry: %w", rollbackErr)
		} else {
			incompleteErr = fmt.Errorf("workspace: rollback governed: rollback incomplete this pass, safe to retry")
		}
		if rerr := grants.Report(op, s7.OutcomeFailedRetryable, s7.CodeRollbackIncomplete, failedBuild); rerr != nil {
			return result, fmt.Errorf("%w (report also failed: %v)", incompleteErr, rerr)
		}
		return result, incompleteErr
	case state == TransactionNeverStarted:
		switch {
		case rollbackErr != nil && !isPureCancellation(rollbackErr):
			// Content LOOKS all-BEFORE, but the attempt had a REAL
			// (non-cancellation) problem — durability unproven, per
			// rollbackBundle's own contract. Never a guessed success.
			if rerr := grants.Report(op, s7.OutcomeUnknown, "", failedBuild); rerr != nil {
				return result, fmt.Errorf("workspace: rollback governed: %w (report unknown also failed: %v)", rollbackErr, rerr)
			}
			return result, fmt.Errorf("workspace: rollback governed: %w", rollbackErr)
		case isPureCancellation(rollbackErr):
			// Verified clean, but the attempt stopped because it was
			// told to — matches ApplyGoverned's own identical
			// distinction (cancellation, not a code-carrying failure).
			if cerr := grants.Cancel(op, failedBuild); cerr != nil {
				return result, fmt.Errorf("workspace: rollback governed: %w (cancel also failed: %v)", rollbackErr, cerr)
			}
			return result, fmt.Errorf("workspace: rollback governed: %w", rollbackErr)
		default:
			// No error at all: the complete write set now equals the
			// sealed BEFORE state — invariant 4 §2's own reconciliation
			// predicate, fully satisfied on THIS pass. But a clean pass
			// alone is not sufficient proof: every file's own content AND
			// its directory's rename durability must be freshly
			// re-verified, against a FRESH re-classification, before ever
			// claiming Succeeded (round 3 HIGH finding, codex; corrected
			// round 4/5/6 — see verifyDurabilityBeforeSuccess's own doc
			// comment and this file's top-of-file "DURABILITY ACROSS
			// RETRIES" section for why this replaces the earlier
			// classification/error-based signals).
			if verr := verifyDurabilityBeforeSuccess(rootFd, b); verr != nil {
				unverifiedErr := fmt.Errorf("workspace: rollback governed: %w — manual reconciliation required, never a guessed success", verr)
				if rerr := grants.Report(op, s7.OutcomeUnknown, "", failedBuild); rerr != nil {
					return result, fmt.Errorf("%w (report unknown also failed: %v)", unverifiedErr, rerr)
				}
				return result, unverifiedErr
			}
			succeededBuild := func(s7.Landing) s7.Companion {
				return s7.Companion{Key: op, Params: envelope(j, op, runID, grant.AttemptNo, EvRollbackCommitted,
					rollbackCommittedPayload{Op: string(op), OriginalOp: string(originalOp), BundleDigest: bundleDigest})}
			}
			if err := grants.Report(op, s7.OutcomeSucceeded, "", succeededBuild); err != nil {
				return result, fmt.Errorf("workspace: rollback governed: report succeeded: %w", err)
			}
			result.Succeeded = true
			return result, nil
		}
	default:
		// An unrecognized TransactionState value — should never happen
		// given ClassifyBundle's own closed 3-value contract, but never
		// assumed (round 1 HIGH #5): fail closed to Unknown, not success.
		unknownStateErr := fmt.Errorf("workspace: rollback governed: unrecognized transaction state %v (fail closed)", state)
		if rerr := grants.Report(op, s7.OutcomeUnknown, "", failedBuild); rerr != nil {
			return result, fmt.Errorf("%w (report unknown also failed: %v)", unknownStateErr, rerr)
		}
		return result, unknownStateErr
	}
}

// reconcileRehydratedRollback handles a PRIOR call to THIS SAME rollback
// operation (op) having crashed and rehydrated to UNKNOWN — invariant 4
// §2's own required self-recovery, without which RollbackGoverned could
// never resume an operation that crashed mid-rollback (round 1 HIGH #3).
// Re-classifies the bundle (read-only) and Reconciles op against the
// SAME predicate the normal attempt path uses: all-BEFORE -> Reconcile
// (true), Succeeded; FOREIGN -> refuse, leave UNKNOWN, never touch;
// otherwise (still mixed) -> Reconcile(false), and report whether the
// resulting state is retry-eligible (FAILED_RETRYABLE within budget) for
// the caller to fall through to a fresh Next call, or exhausted
// (terminal), in which case the caller must NOT attempt Next again.
func reconcileRehydratedRollback(op, originalOp contracts.OperationID, bundleDigest string, rootFd int, b Bundle, grants *s7.Authority, j *journal.Journal, runID contracts.RunID) (result RollbackResult, retryEligible bool, err error) {
	classes, cstate, cerr := ClassifyBundle(rootFd, b)
	if cerr != nil {
		return RollbackResult{}, false, fmt.Errorf("workspace: rollback governed: reconcile rehydrated rollback: classify: %w", cerr)
	}
	result = RollbackResult{Classes: classes, State: cstate}

	if cstate == TransactionForeign {
		return result, false, fmt.Errorf("workspace: rollback governed: rehydrated rollback's own bundle is FOREIGN, manual reconciliation required")
	}

	attemptNo := grants.Attempts(op)
	var landingKind s7.LandingKind
	build := func(l s7.Landing) s7.Companion {
		landingKind = l.Kind
		if l.Kind == s7.LandingSucceeded {
			return s7.Companion{Key: op, Params: envelope(j, op, runID, attemptNo, EvRollbackCommitted,
				rollbackCommittedPayload{Op: string(op), OriginalOp: string(originalOp), BundleDigest: bundleDigest})}
		}
		// S7's own Reconcile populates a LandingRetry OR LandingTerminal's
		// Code from rec.lastCode (events.go) — Reconcile(false) here
		// never produces any OTHER landing kind (LandingSucceeded is
		// already handled above; LandingCancelled/LandingUnknown come
		// from different S7 entry points, never from Reconcile).
		// validApplyFailedNarrative requires code to be EXACTLY "" or the
		// retryable code for EVERY non-Succeeded landing (its own
		// universal check before the landing-kind switch), so any
		// inherited code other than CodeRollbackIncomplete itself durably
		// rejects this very companion, permanently stranding the
		// operation UNKNOWN. rec.lastCode is NOT always merely "" (first
		// crash) or CodeRollbackIncomplete (a later crash after a prior
		// FailedRetryable report) as an earlier version of this comment
		// assumed (round 2 finding, independently reproduced live by both
		// kilo and agy, for the "" case) — S7's OWN restart rehydration
		// (events.go) can ALSO durably set lastCode to CodeCrashRecovered
		// on a restart-only rehydration with no real attempt in between,
		// which a SECOND such rehydration (two crashes, no attempt
		// between them) then inherits verbatim here (round 4 finding,
		// codex, reproduced live). round 4/5's fix normalized this only
		// for LandingRetry — round 6 (codex, reproduced live) proved that
		// incomplete: an EXHAUSTED retry budget produces LandingTerminal
		// carrying the SAME inherited bad code, which the Retry-only
		// guard let straight through, durably rejecting the exhaustion
		// companion too. Normalizing for EITHER landing kind (the only
		// two Reconcile(false) can ever produce here) closes both.
		code := l.Code
		if code != s7.CodeRollbackIncomplete && (l.Kind == s7.LandingRetry || l.Kind == s7.LandingTerminal) {
			code = s7.CodeRollbackIncomplete
		}
		return s7.Companion{Key: op, Params: envelope(j, op, runID, attemptNo, EvRollbackFailed,
			rollbackFailedPayload{Op: string(op), OriginalOp: string(originalOp), BundleDigest: bundleDigest,
				AttemptNo: attemptNo, Landing: int(l.Kind), Code: code})}
	}

	if cstate == TransactionNeverStarted {
		// The SAME fresh re-verification the normal attempt path performs
		// before EVER reporting Succeeded (verifyDurabilityBeforeSuccess's
		// own doc comment) — a crash during a PRIOR attempt that left
		// this bundle looking fully clean at reconcile time does not, by
		// itself, prove every file's own restore durability; re-fsyncing
		// each file's content and directory, then re-classifying fresh,
		// RIGHT NOW does (round 3 HIGH finding, codex; corrected round
		// 4/5/6).
		if verr := verifyDurabilityBeforeSuccess(rootFd, b); verr != nil {
			return result, false, fmt.Errorf("workspace: rollback governed: reconcile rehydrated rollback: %w — manual reconciliation required, never a guessed success", verr)
		}
		if rerr := grants.Reconcile(op, true, build); rerr != nil {
			return result, false, fmt.Errorf("workspace: rollback governed: reconcile succeeded rollback: %w", rerr)
		}
		result.Succeeded = true
		return result, false, nil
	}

	// cstate == TransactionMidCrash: prove the effect (a fully-clean
	// rollback) is NOT yet achieved; S7 itself decides retry-vs-terminal
	// from the operation's own budget/deadline.
	if rerr := grants.Reconcile(op, false, build); rerr != nil {
		return result, false, fmt.Errorf("workspace: rollback governed: reconcile incomplete rollback: %w", rerr)
	}
	if landingKind != s7.LandingRetry {
		return result, false, fmt.Errorf("workspace: rollback governed: rollback operation's retry budget is exhausted, manual reconciliation required")
	}
	return result, true, nil
}
