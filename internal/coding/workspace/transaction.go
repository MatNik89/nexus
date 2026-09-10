//go:build linux

// transaction.go is the multi-file atomic-replace orchestration built on
// descriptor.go's primitives (PLAN-CODING-TRIO.md invariant 4, steps
// 1/3/4/5 of 7).
//
// SCOPE OF THIS PIECE (deliberately): a same-process transaction — one
// sealed before/after bundle persisted BEFORE any write (step 1), a
// caller-supplied durable-binding hook invoked while the bundle's pin is
// still held and before any write (the seam step 2's S7/journal pairing
// will use once it exists), each file replaced atomically in order
// through WriteFileBeneath (step 3), and ordinary (non-crash) mid-
// transaction failure rolled back to the sealed before-images (step 5).
// NOT YET built here, as a LATER increment: the actual S7 `Consume`
// companion-batch pairing behind the bindDurable hook, the restart-time
// crash-recovery classification table (step 6), and the governed
// `PolicyWorkspaceRollback` operation restart recovery uses (also step
// 6) — those need S7/journal wiring this package does not yet have, and
// are their own reviewable increment once this one has converged
// (matching this whole program's "smallest reviewable increment" build
// order: descriptor.go's primitives were reviewed and merged before this
// file was written).
package workspace

import (
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
	// — both for a new file and for a replace. WriteFileBeneath's atomic
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

// fileRecord is one mutation's captured before/after state, serialized
// into the sealed bundle — invariant 4 requires durably RECONSTRUCTABLE
// bytes, not just a digest, so recovery never needs to re-derive content
// a crash could otherwise lose.
type fileRecord struct {
	RelDir     string `json:"rel_dir"`
	BaseName   string `json:"base_name"`
	Existed    bool   `json:"existed"`
	Before     []byte `json:"before,omitempty"`
	BeforeMode uint32 `json:"before_mode,omitempty"`
	After      []byte `json:"after"`
	AfterMode  uint32 `json:"after_mode"`
}

type bundle struct {
	Files []fileRecord `json:"files"`
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
}

// writtenFile tracks one already-applied mutation for possible rollback.
// It is added to the rollback set as soon as WriteFileBeneath reports
// committed==true — REGARDLESS of whether that same call also returned
// an error (code-review finding, codex, round 1: WriteFileBeneath can
// complete its replacement and only THEN fail, e.g. removing the
// displaced original or a trailing directory fsync — a caller that only
// tracks success/failure, not "did the entry change", can silently omit
// an actually-mutated file from its own rollback bookkeeping).
type writtenFile struct {
	dirFd         int
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
func Apply(rootFd int, store *sealedstore.Store, mutations []FileMutation, bindDurable func(digest string) error) (Result, error) {
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
	}

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
	prep := make([]prepared, 0, len(mutations))
	defer func() {
		for _, p := range prep {
			unix.Close(p.dirFd)
		}
	}()

	records := make([]fileRecord, 0, len(mutations))
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
		records = append(records, fileRecord{
			RelDir: m.RelDir, BaseName: m.BaseName, Existed: existed,
			Before: before, BeforeMode: uint32(beforeMode.Perm()),
			After: m.After, AfterMode: uint32(m.Mode.Perm()),
		})
	}

	raw, err := json.Marshal(bundle{Files: records})
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
	for _, p := range prep {
		beforeApplyWritesFile(p.mutation.BaseName) // test seam: inject drift between prepare and write

		expect := TargetExpectation{MustNotExist: !p.existed, Dev: p.beforeDev, Ino: p.beforeIno}
		if p.existed {
			expect.ContentDigest = p.beforeDigest
			expect.ExpectMode = p.beforeMode
			expect.CheckMode = true
		}

		committed, werr := WriteFileBeneath(p.dirFd, p.mutation.BaseName, p.mutation.After, p.mutation.Mode, expect)
		if !committed {
			if werr == nil {
				werr = fmt.Errorf("internal inconsistency: WriteFileBeneath reported neither committed nor an error")
			}
			return Result{BundleDigest: digest}, rollbackAndReport(written, fmt.Errorf("workspace: apply: writing %q failed: %w", p.mutation.BaseName, werr))
		}

		wf := writtenFile{
			dirFd: p.dirFd, baseName: p.mutation.BaseName,
			existedBefore: p.existed, beforeContent: p.before, beforeMode: p.beforeMode, beforeDigest: p.beforeDigest,
			afterMode: p.mutation.Mode, afterDigest: sha256Hex(p.mutation.After),
		}
		afterDev, afterIno, statErr := StatBeneath(p.dirFd, p.mutation.BaseName)
		if statErr == nil {
			wf.afterDev, wf.afterIno, wf.identityKnown = afterDev, afterIno, true
		}
		written = append(written, wf)

		if werr != nil {
			return Result{BundleDigest: digest}, rollbackAndReport(written, fmt.Errorf("workspace: apply: writing %q reported an error after taking effect: %w", p.mutation.BaseName, werr))
		}
		if statErr != nil {
			return Result{BundleDigest: digest}, rollbackAndReport(written, fmt.Errorf("workspace: apply: wrote %q but could not capture its new identity: %w", p.mutation.BaseName, statErr))
		}
	}

	return Result{BundleDigest: digest, Committed: true}, nil
}

// rollbackAndReport restores every already-committed file to its
// captured before-state, in reverse order, and folds any rollback
// failure into the returned error alongside the original failure. A
// file whose post-write identity could not be captured is never
// guessed at — it is reported as needing manual reconciliation instead.
func rollbackAndReport(written []writtenFile, original error) error {
	var rollbackErrs []error
	for i := len(written) - 1; i >= 0; i-- {
		w := written[i]
		if !w.identityKnown {
			rollbackErrs = append(rollbackErrs, fmt.Errorf("%q: post-write identity was never captured — cannot safely automate rollback, manual reconciliation required", w.baseName))
			continue
		}
		var err error
		if w.existedBefore {
			restore := TargetExpectation{Dev: w.afterDev, Ino: w.afterIno, ContentDigest: w.afterDigest, ExpectMode: w.afterMode, CheckMode: true}
			_, err = WriteFileBeneath(w.dirFd, w.baseName, w.beforeContent, w.beforeMode, restore)
		} else {
			remove := TargetExpectation{Dev: w.afterDev, Ino: w.afterIno, ContentDigest: w.afterDigest, ExpectMode: w.afterMode, CheckMode: true}
			err = RemoveFileWithExpectedIdentity(w.dirFd, w.baseName, remove)
		}
		if err != nil {
			rollbackErrs = append(rollbackErrs, fmt.Errorf("restoring %q: %w", w.baseName, err))
		}
	}
	if len(rollbackErrs) > 0 {
		return fmt.Errorf("%w (rollback of %d already-written file(s) ALSO failed, manual reconciliation required: %w)",
			original, len(written), errors.Join(rollbackErrs...))
	}
	if len(written) > 0 {
		return fmt.Errorf("%w (rolled back %d already-written file(s))", original, len(written))
	}
	return original
}

func sha256Hex(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}
