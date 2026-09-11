//go:build linux

// transaction.go is the multi-file atomic-replace orchestration built on
// descriptor.go's primitives (PLAN-CODING-TRIO.md invariant 4, steps
// 1/3/4/5 of 7).
//
// SCOPE OF THIS PIECE (deliberately): a same-process transaction — one
// sealed before/after bundle persisted BEFORE any write (step 1), each
// file replaced atomically in order through writeFileBeneath (step 3),
// and ordinary (non-crash) mid-transaction failure rolled back to the
// sealed before-images (step 5). The bindDurable hook's real S7 Consume
// pairing (step 2) is built in governed.go; restart-time classification
// (step 6, "what state is file X actually in") is built in recovery.go.
// Still NOT built anywhere in this package, as a LATER increment: the
// restart-time SCAN that finds every rehydrated-UNKNOWN operation and
// calls recovery.go's classification (needs a caller outside this
// package — daemon startup, most likely) and the governed
// `PolicyWorkspaceRollback` operation restart recovery uses once
// classification says AllAfter/Mixed (its own new S7 durable operation
// type — a materially larger piece, its own reviewable increment,
// matching this whole program's "smallest reviewable increment" build
// order).
package workspace

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"

	"github.com/MatNik89/nexus/internal/foundation/sealedstore"
	"golang.org/x/sys/unix"
)

// FileMutation is one file's target content within a transaction. The
// caller (e.g. symedit's future Apply step) supplies the exact bytes to
// write; this package never generates content itself.
type FileMutation struct {
	// RelDir is a clean, slash-separated directory path beneath the
	// transaction root (see WalkDirBeneath) — "" for the root itself.
	RelDir string
	// BaseName is the single file name within RelDir.
	BaseName string
	// After is the complete new content for this file.
	After []byte
	// Mode is the file's permission bits the result ALWAYS ends up with
	// — both for a new file and for a replace. writeFileBeneath's atomic
	// replace works by creating a new temp file at this mode and
	// exchanging it into place; the pre-existing file's own mode is
	// never consulted or preserved. A caller replacing an existing file
	// that must keep its original permissions is responsible for
	// capturing them (e.g. at Prepare time) and passing them through
	// here explicitly.
	Mode fs.FileMode
	// ExpectedBeforeDigest, when non-empty, is the sha256 hex digest the
	// EXISTING file's content must match at the exact moment Apply
	// captures its own before-state — checked as part of THAT capture,
	// not as a separate pass (code-review finding, codex: a caller that
	// validates a preimage itself and only THEN calls Apply leaves a
	// window between its own check and Apply's own later capture; a
	// concurrent writer landing in that window would have Apply silently
	// treat their content as the legitimate "before" and overwrite it —
	// carrying the caller's own expectation through to be checked AT
	// Apply's own capture point, not separately beforehand, closes that
	// window rather than merely narrowing it). Empty skips the check —
	// existing callers, and MustNotExist-style new-file mutations
	// (nothing exists yet to digest), are unaffected.
	ExpectedBeforeDigest string
	// ExpectedBeforeDev/ExpectedBeforeIno, checked only when
	// CheckExpectedIdentity is true, are the device+inode the EXISTING
	// file's identity must match at Apply's own capture point — the
	// same closes-the-window rationale as ExpectedBeforeDigest, applied
	// to identity (PLAN-CODING-TRIO.md invariant 4: "the PRE-EDIT inode
	// is a PRECONDITION check at Apply time" — code-review finding,
	// codex: content alone does not catch an atomic replace-with-
	// identical-bytes that changes only identity).
	ExpectedBeforeDev, ExpectedBeforeIno uint64
	CheckExpectedIdentity                bool
	// ExpectedBeforeMode, checked only when CheckExpectedMode is true,
	// is the permission bits the EXISTING file's mode must match at
	// Apply's own capture point — independent of content (invariant 4:
	// "content, mode, and metadata expectations are checked explicitly
	// ...independent of inode" — code-review finding, codex: a
	// content-preserving chmod between an outer caller's own capture and
	// Apply's capture would otherwise be silently overwritten with the
	// caller's stale mode). A bool gate, not a zero-value sentinel —
	// mode 0000 is a legitimate value (same reasoning as
	// TargetExpectation.CheckMode elsewhere in this package).
	ExpectedBeforeMode fs.FileMode
	CheckExpectedMode  bool
}

// FileRecord is one mutation's captured before/after state, as
// persisted in a sealed Bundle — invariant 4 requires durably
// RECONSTRUCTABLE bytes, not just a digest, so restart-time recovery
// never needs to re-derive content a crash could otherwise lose.
// Exported (round-6-of-S7-wiring precedent: widen visibility for a
// genuine cross-cutting need, never speculatively) so recovery.go's own
// restart-time classification — and any future caller outside this
// package that reads a sealed bundle back — can decode it without
// knowing the JSON shape by hand.
type FileRecord struct {
	RelDir     string `json:"rel_dir"`
	BaseName   string `json:"base_name"`
	Existed    bool   `json:"existed"`
	Before     []byte `json:"before,omitempty"`
	BeforeMode uint32 `json:"before_mode,omitempty"`
	After      []byte `json:"after"`
	AfterMode  uint32 `json:"after_mode"`
}

// Bundle is the complete sealed record of one Apply transaction's
// write set — see FileRecord.
type Bundle struct {
	Files []FileRecord `json:"files"`
}

// Result reports what Apply actually did.
type Result struct {
	// BundleDigest identifies the sealed before/after bundle persisted
	// before any write — the bindDurable hook (and a future S7/journal-
	// integrated caller) records this as the durable recovery reference.
	BundleDigest string
	// Committed is true only once every mutation has been written
	// successfully — the same-process analogue of invariant 4's
	// `workspace.mutation_committed` marker (not yet a journal event in
	// this increment).
	Committed bool
	// RolledBack lists, for a FAILED Apply whose rollback was fully
	// verified (ErrRollbackIncomplete absent from the returned error),
	// each already-written file's identity IMMEDIATELY AFTER rollback
	// restored it — see RolledBackFile's own doc comment for why a
	// caller retrying the SAME logical mutation MUST use these, not the
	// original pre-Apply identity. Empty on success (Committed==true)
	// and meaningless (do not use) when ErrRollbackIncomplete is present.
	RolledBack []RolledBackFile
}

// RolledBackFile is one file's identity immediately after Apply's own
// rollback restored it. Restoring a file via writeFileBeneath's atomic
// exchange always produces a NEW inode, even when the restored bytes and
// mode are byte-identical to the original (this package's own atomic-
// replace discipline never preserves a target's pre-existing identity —
// see writeFileBeneath's own doc comment). A caller that wants to retry
// the SAME logical mutation after a verified rollback MUST update its
// ExpectedBeforeDev/ExpectedBeforeIno to these values first (see
// UpdateMutationsAfterRollback) — the original Prepare-time identity is
// gone the instant rollback restores the file (code-review finding,
// codex round 2 HIGH #1: reusing the stale original precondition makes a
// "verified rollback, safe to retry" proposal impossible to ever honor).
type RolledBackFile struct {
	RelDir, BaseName string
	Dev, Ino         uint64
}

// UpdateMutationsAfterRollback returns a COPY of mutations with each
// entry named in rolledBack having its ExpectedBeforeDev/ExpectedBeforeIno
// updated to that file's actual post-rollback identity (content and mode
// preconditions are unchanged — rollback restores the EXACT original
// bytes and mode, only the inode changes). A mutation not named in
// rolledBack is returned unchanged: nothing about its precondition needs
// to change (this only happens for a mutation whose target did not exist
// before Apply and was removed again by rollback — its original
// "must not exist" precondition, carrying no identity, is still valid).
func UpdateMutationsAfterRollback(mutations []FileMutation, rolledBack []RolledBackFile) []FileMutation {
	byKey := make(map[string]RolledBackFile, len(rolledBack))
	for _, rb := range rolledBack {
		byKey[rb.RelDir+"\x00"+rb.BaseName] = rb
	}
	out := make([]FileMutation, len(mutations))
	for i, m := range mutations {
		out[i] = m
		if rb, ok := byKey[m.RelDir+"\x00"+m.BaseName]; ok {
			out[i].ExpectedBeforeDev = rb.Dev
			out[i].ExpectedBeforeIno = rb.Ino
		}
	}
	return out
}

// writtenFile tracks one already-applied mutation for possible rollback.
// It is added to the rollback set as soon as writeFileBeneath reports
// committed==true — REGARDLESS of whether that same call also returned
// an error (code-review finding, codex, round 1: writeFileBeneath can
// complete its replacement and only THEN fail, e.g. removing the
// displaced original or a trailing directory fsync — a caller that only
// tracks success/failure, not "did the entry change", can silently omit
// an actually-mutated file from its own rollback bookkeeping).
type writtenFile struct {
	dirFd         int
	relDir        string
	baseName      string
	existedBefore bool
	beforeContent []byte
	beforeMode    fs.FileMode
	beforeDigest  string
	afterMode     fs.FileMode
	afterDigest   string
	// identityKnown is false when the post-write StatBeneath capture
	// itself failed — rollback cannot safely present a TargetExpectation
	// it never actually captured, so this file is reported as needing
	// MANUAL reconciliation instead of an automated (and potentially
	// wrong-identity) rollback attempt.
	identityKnown      bool
	afterDev, afterIno uint64
}

// prepared is one mutation's captured before-state, from Apply's own
// prepare pass — kept at package level (not local to Apply) so
// rollbackAndReport/verifyStillBefore can re-verify a mutation that was
// never reached at all, not just ones Apply actually wrote.
type prepared struct {
	dirFd        int
	mutation     FileMutation
	existed      bool
	beforeDev    uint64
	beforeIno    uint64
	before       []byte
	beforeMode   fs.FileMode
	beforeDigest string
}

// beforeApplyWritesFile is a test seam: a no-op in production, it lets
// transaction_test.go deterministically inject a change to a file's
// on-disk state at the exact instant between Apply's prepare pass
// (identity capture) and its write pass — mirroring descriptor_test.go's
// beforeRestoreExchange seam for the same class of problem.
var beforeApplyWritesFile = func(baseName string) {}

// Apply performs one multi-file transaction beneath rootFd: for each
// mutation, capture its current (before) state — content, identity, AND
// mode from ONE descriptor-relative open per file (code-review finding,
// codex, round 1: a separate stat-then-reopen pair leaves a window for
// in-place content mutation, same inode, to go undetected) — persist ONE
// sealed bundle containing every file's before AND after bytes (step 1),
// invoke bindDurable with that bundle's digest while its pin is still
// held and BEFORE any write (the durable-reference seam step 2's future
// S7/journal pairing will use — aborting here if it fails means the
// required "bundle, durable reference, THEN first write" ordering holds
// even though no real durable reference exists yet in this increment),
// then replace each file atomically in order (steps 3/4), binding each
// write's precondition to the captured before-identity, before-content
// digest, and before-mode (not just Dev/Ino — code-review finding,
// codex, round 1: identity alone does not catch in-place content
// mutation). On any per-file write failure — or a write that DID commit
// but then failed a later step — every already-committed file is rolled
// back to its sealed before-image (step 5) before Apply returns the
// original error.
//
// ctx is checked once per mutation, immediately before that file's write
// (code-review finding, codex round 2 HIGH #2): a consumed S7 attempt's
// AttemptContext bounds this call, and a concurrent Cancel or an expired
// deadline must stop Apply from writing FURTHER files rather than
// silently continuing while S7 has already moved to a terminal state.
// This is a per-file boundary check, not mid-syscall interruption — a
// single writeFileBeneath call, once started, always runs to completion
// (local descriptor I/O has no cancellable wait point); ctx.Err() being
// non-nil at the top of an iteration is treated exactly like any other
// write failure at that same point: every already-committed file is
// rolled back before Apply returns.
//
// apply is package-private (code-review finding, codex round 3 HIGH #1,
// round 4 HIGH #1): invariant 2 requires every dispatched effect here to
// be S7-governed — ApplyGoverned, in this same package, is the ONLY
// exported entry point into a real mutation. A previous version left
// this exported and reachable cross-package (via symedit's own
// now-deleted ungoverned wrapper); nothing outside this package needs it
// directly, and same-package tests reach it exactly as before.
func apply(ctx context.Context, rootFd int, store *sealedstore.Store, mutations []FileMutation, bindDurable func(digest string) error) (Result, error) {
	if len(mutations) == 0 {
		return Result{}, fmt.Errorf("workspace: apply: no mutations given")
	}
	if bindDurable == nil {
		return Result{}, fmt.Errorf("workspace: apply: bindDurable must not be nil (pass a no-op func explicitly if no durable reference is needed yet)")
	}
	seen := make(map[string]bool, len(mutations))
	for _, m := range mutations {
		key := m.RelDir + "\x00" + m.BaseName
		if seen[key] {
			return Result{}, fmt.Errorf("workspace: apply: duplicate target %q/%q in one mutation set (fail closed)", m.RelDir, m.BaseName)
		}
		seen[key] = true
		// Owner-read is required, not merely permitted: restart-time
		// recovery classification must reopen this file O_RDONLY to hash
		// it (descriptor.go's CaptureFileBeneath), and no pre-chmod
		// descriptor survives a crash. A committed after-mode without
		// the owner-read bit makes a legitimate transaction permanently
		// unclassifiable (codex review round 3, HIGH #1 — reproduced
		// live: apply(mode=0000) commits successfully, then
		// ClassifyBundle fails "permission denied" trying to read it
		// back).
		if m.Mode.Perm()&0o400 == 0 {
			return Result{}, fmt.Errorf("workspace: apply: %q's target mode %o lacks the owner-read bit — restart-time recovery could never reopen it to classify (fail closed, no write performed)", m.BaseName, m.Mode.Perm())
		}
	}

	prep := make([]prepared, 0, len(mutations))
	defer func() {
		for _, p := range prep {
			unix.Close(p.dirFd)
		}
	}()

	records := make([]FileRecord, 0, len(mutations))
	for _, m := range mutations {
		if err := validateBaseName(m.BaseName); err != nil {
			return Result{}, err
		}
		dirFd, err := WalkDirBeneath(rootFd, m.RelDir)
		if err != nil {
			return Result{}, fmt.Errorf("workspace: apply: %w", err)
		}

		var before []byte
		var existed bool
		var beforeMode fs.FileMode
		var dev, ino uint64
		content, capDev, capIno, capMode, capErr := CaptureFileBeneath(dirFd, m.BaseName)
		switch {
		case capErr == nil:
			existed, before, dev, ino, beforeMode = true, content, capDev, capIno, capMode
		case errors.Is(capErr, fs.ErrNotExist):
			existed = false
		default:
			unix.Close(dirFd)
			return Result{}, fmt.Errorf("workspace: apply: capturing before-state of %q: %w", m.BaseName, capErr)
		}

		var beforeDigest string
		if existed {
			beforeDigest = sha256Hex(before)
		}
		if m.ExpectedBeforeDigest != "" && beforeDigest != m.ExpectedBeforeDigest {
			unix.Close(dirFd)
			return Result{}, fmt.Errorf("workspace: apply: %q's current content (digest=%s) does not match the caller's expected before-digest (%s) — fail closed, no write performed", m.BaseName, beforeDigest, m.ExpectedBeforeDigest)
		}
		if m.CheckExpectedIdentity && (dev != m.ExpectedBeforeDev || ino != m.ExpectedBeforeIno) {
			unix.Close(dirFd)
			return Result{}, fmt.Errorf("workspace: apply: %q's current identity (dev=%d,ino=%d) does not match the caller's expected before-identity (dev=%d,ino=%d) — fail closed, no write performed", m.BaseName, dev, ino, m.ExpectedBeforeDev, m.ExpectedBeforeIno)
		}
		if m.CheckExpectedMode && beforeMode.Perm() != m.ExpectedBeforeMode.Perm() {
			unix.Close(dirFd)
			return Result{}, fmt.Errorf("workspace: apply: %q's current mode (%o) does not match the caller's expected before-mode (%o) — fail closed, no write performed", m.BaseName, beforeMode.Perm(), m.ExpectedBeforeMode.Perm())
		}
		prep = append(prep, prepared{
			dirFd: dirFd, mutation: m, existed: existed,
			beforeDev: dev, beforeIno: ino, before: before,
			beforeMode: beforeMode, beforeDigest: beforeDigest,
		})
		records = append(records, FileRecord{
			RelDir: m.RelDir, BaseName: m.BaseName, Existed: existed,
			Before: before, BeforeMode: uint32(beforeMode.Perm()),
			After: m.After, AfterMode: uint32(m.Mode.Perm()),
		})
	}

	raw, err := json.Marshal(Bundle{Files: records})
	if err != nil {
		return Result{}, fmt.Errorf("workspace: apply: marshal sealed bundle: %w", err)
	}
	digest, pin, err := store.Put(raw)
	if err != nil {
		return Result{}, fmt.Errorf("workspace: apply: persist sealed bundle: %w", err)
	}
	if err := bindDurable(digest); err != nil {
		pin.Release()
		return Result{BundleDigest: digest}, fmt.Errorf("workspace: apply: binding sealed bundle durably failed, aborting before any write: %w", err)
	}
	// The pin is released once Apply itself returns — this increment's
	// bindDurable stands in for the future S7/journal reference; once
	// that reference is real, holding the pin exactly through
	// bindDurable's own success (not longer) is correct per sealedstore's
	// contract (the caller who durably references the digest owns the
	// pin from that point on, not Apply).
	defer pin.Release()

	written := make([]writtenFile, 0, len(prep))
	for i, p := range prep {
		if err := ctx.Err(); err != nil {
			rolledBack, rerr := rollbackAndReport(written, prep[i:], fmt.Errorf("workspace: apply: cancelled before writing %q: %w", p.mutation.BaseName, err))
			return Result{BundleDigest: digest, RolledBack: rolledBack}, rerr
		}
		beforeApplyWritesFile(p.mutation.BaseName) // test seam: inject drift between prepare and write

		expect := TargetExpectation{MustNotExist: !p.existed, Dev: p.beforeDev, Ino: p.beforeIno}
		if p.existed {
			expect.ContentDigest = p.beforeDigest
			expect.ExpectMode = p.beforeMode
			expect.CheckMode = true
		}

		committed, werr := writeFileBeneath(p.dirFd, p.mutation.BaseName, p.mutation.After, p.mutation.Mode, expect)
		if !committed {
			if werr == nil {
				werr = fmt.Errorf("internal inconsistency: writeFileBeneath reported neither committed nor an error")
			}
			rolledBack, rerr := rollbackAndReport(written, prep[i:], fmt.Errorf("workspace: apply: writing %q failed: %w", p.mutation.BaseName, werr))
			return Result{BundleDigest: digest, RolledBack: rolledBack}, rerr
		}

		wf := writtenFile{
			dirFd: p.dirFd, relDir: p.mutation.RelDir, baseName: p.mutation.BaseName,
			existedBefore: p.existed, beforeContent: p.before, beforeMode: p.beforeMode, beforeDigest: p.beforeDigest,
			afterMode: p.mutation.Mode, afterDigest: sha256Hex(p.mutation.After),
		}
		afterDev, afterIno, statErr := StatBeneath(p.dirFd, p.mutation.BaseName)
		if statErr == nil {
			wf.afterDev, wf.afterIno, wf.identityKnown = afterDev, afterIno, true
		}
		written = append(written, wf)

		if werr != nil {
			rolledBack, rerr := rollbackAndReport(written, prep[i+1:], fmt.Errorf("workspace: apply: writing %q reported an error after taking effect: %w", p.mutation.BaseName, werr))
			return Result{BundleDigest: digest, RolledBack: rolledBack}, rerr
		}
		if statErr != nil {
			rolledBack, rerr := rollbackAndReport(written, prep[i+1:], fmt.Errorf("workspace: apply: wrote %q but could not capture its new identity: %w", p.mutation.BaseName, statErr))
			return Result{BundleDigest: digest, RolledBack: rolledBack}, rerr
		}
	}

	// Final check (code-review finding, codex round 3 HIGH #4): every
	// write succeeded, but ctx may have been cancelled DURING the last
	// one — declaring Committed success at that point would let S7 land
	// SUCCEEDED (via the caller's own Report) after it may already be
	// CANCELLED. Every file just written is rolled back exactly like any
	// other post-write failure; nothing remains unreached (the whole set
	// was just written), so this checks only the already-written set.
	if err := ctx.Err(); err != nil {
		rolledBack, rerr := rollbackAndReport(written, nil, fmt.Errorf("workspace: apply: cancelled after the last write, before commit: %w", err))
		return Result{BundleDigest: digest, RolledBack: rolledBack}, rerr
	}

	return Result{BundleDigest: digest, Committed: true}, nil
}

// ErrRollbackIncomplete marks an Apply failure where NOT every
// already-written file could be restored to its sealed before-image and
// verified — the workspace may hold a MIXED before/after state; manual
// reconciliation is required (PLAN-CODING-TRIO.md invariant 2's
// state-transition binding: "a FAILED or INCOMPLETE rollback reports
// Unknown — never retryable, never silently retried"). Its ABSENCE from
// a failed Apply's error chain (errors.Is returns false) means every
// already-written file WAS restored and verified — including the
// trivial case where nothing had been written yet — even though Apply
// still returns a non-nil error for the original failure; a durable
// caller (e.g. workspace.ApplyGoverned) uses this distinction to decide
// between a retryable landing and an UNKNOWN one.
var ErrRollbackIncomplete = errors.New("workspace: rollback of already-written files was incomplete")

// rollbackAndReport restores every already-committed file to its
// captured before-state, in reverse order, and folds any rollback
// failure into the returned error alongside the original failure. A
// file whose post-write identity could not be captured is never
// guessed at — it is reported as needing manual reconciliation instead.
// On a fully verified rollback it also returns each restored file's NEW
// post-rollback identity (see RolledBackFile) — nil whenever
// ErrRollbackIncomplete is returned instead (nothing here can be trusted
// in that case).
func rollbackAndReport(written []writtenFile, unreached []prepared, original error) ([]RolledBackFile, error) {
	var rollbackErrs []error
	rolledBack := make([]RolledBackFile, 0, len(written))
	for i := len(written) - 1; i >= 0; i-- {
		w := written[i]
		if !w.identityKnown {
			rollbackErrs = append(rollbackErrs, fmt.Errorf("%q: post-write identity was never captured — cannot safely automate rollback, manual reconciliation required", w.baseName))
			continue
		}
		var err error
		if w.existedBefore {
			restore := TargetExpectation{Dev: w.afterDev, Ino: w.afterIno, ContentDigest: w.afterDigest, ExpectMode: w.afterMode, CheckMode: true}
			_, err = writeFileBeneath(w.dirFd, w.baseName, w.beforeContent, w.beforeMode, restore)
			if err == nil {
				if newDev, newIno, statErr := StatBeneath(w.dirFd, w.baseName); statErr == nil {
					rolledBack = append(rolledBack, RolledBackFile{RelDir: w.relDir, BaseName: w.baseName, Dev: newDev, Ino: newIno})
				} else {
					err = fmt.Errorf("restored but could not re-capture its new identity: %w", statErr)
				}
			}
		} else {
			remove := TargetExpectation{Dev: w.afterDev, Ino: w.afterIno, ContentDigest: w.afterDigest, ExpectMode: w.afterMode, CheckMode: true}
			err = removeFileWithExpectedIdentity(w.dirFd, w.baseName, remove)
			// Removed back to non-existent: no identity to report — the
			// mutation's original "must not exist" precondition, which
			// carries no identity, is still valid for a retry.
		}
		if err != nil {
			rollbackErrs = append(rollbackErrs, fmt.Errorf("restoring %q: %w", w.baseName, err))
		}
	}
	// A "verified all-BEFORE" claim (code-review finding, codex round 3
	// HIGH #2) covers the WHOLE transaction, not just the files this
	// process happened to reach and write: every mutation that was NEVER
	// written — including the one whose own write attempt just failed,
	// most commonly because IT no longer matches what was captured — must
	// be re-verified against its ORIGINAL captured before-state too. A
	// caller (e.g. workspace.ApplyGoverned) that skips this and reports
	// "verified, safe to retry" whenever the WRITTEN subset rolled back
	// cleanly would be wrong the moment anything else in the set had
	// independently drifted.
	for _, p := range unreached {
		if err := verifyStillBefore(p); err != nil {
			rollbackErrs = append(rollbackErrs, err)
		}
	}
	if len(rollbackErrs) > 0 {
		return nil, errors.Join(
			fmt.Errorf("%w (rollback/verification of %d file(s) failed, manual reconciliation required: %w)",
				original, len(written)+len(unreached), errors.Join(rollbackErrs...)),
			ErrRollbackIncomplete,
		)
	}
	if len(written) > 0 {
		return rolledBack, fmt.Errorf("%w (rolled back %d already-written file(s))", original, len(written))
	}
	return nil, original
}

// verifyStillBefore re-reads one prepared mutation's CURRENT on-disk
// state and confirms it still matches exactly what Apply originally
// captured — used for a mutation whose own write was never reached, to
// prove the whole transaction is genuinely all-BEFORE, not just the
// subset that happened to get written.
func verifyStillBefore(p prepared) error {
	if !p.existed {
		if _, _, err := StatBeneath(p.dirFd, p.mutation.BaseName); err == nil {
			return fmt.Errorf("%q now exists but was expected to still not exist", p.mutation.BaseName)
		} else if !errors.Is(err, fs.ErrNotExist) {
			return fmt.Errorf("%q: could not verify it still does not exist: %w", p.mutation.BaseName, err)
		}
		return nil
	}
	content, dev, ino, mode, err := CaptureFileBeneath(p.dirFd, p.mutation.BaseName)
	if err != nil {
		return fmt.Errorf("%q: could not re-verify its before-state: %w", p.mutation.BaseName, err)
	}
	if sha256Hex(content) != p.beforeDigest || dev != p.beforeDev || ino != p.beforeIno || mode.Perm() != p.beforeMode.Perm() {
		return fmt.Errorf("%q no longer matches its originally captured before-state", p.mutation.BaseName)
	}
	return nil
}

func sha256Hex(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}
