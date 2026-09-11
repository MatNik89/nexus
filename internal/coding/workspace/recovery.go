//go:build linux

// recovery.go is the FIRST piece of invariant 4 steps 6/7's restart-time
// crash-recovery machinery: given a sealed Bundle (read back via
// sealedstore, referenced by a durable workspace.apply_started event —
// see governed.go), classify what state the transaction's write set is
// ACTUALLY in on disk right now. This is the read-only "what happened"
// half of recovery.
//
// SCOPE OF THIS PIECE (deliberately, matching this whole program's
// build order — primitives before the governed operation that uses
// them): classification only. NOT built here, as later increments of
// their own: the restart-time SCAN that finds every rehydrated-UNKNOWN
// workspace.apply operation and calls this classification (needs a
// caller that owns "when does this run" — daemon startup, most likely,
// outside this package); the governed `PolicyWorkspaceRollback`
// operation invariant 4 §2 specifies for the AllAfter/Mixed cases (its
// own new S7 durable operation type, exact-intent-bound to the original
// operation's identity + the bundle digest — a materially larger piece,
// its own reviewable increment); and the `Reconcile(false)`/`Next`
// calls invariant 4's own classification table specifies happen AFTER
// classification decides what to do. This piece answers "what state is
// file X actually in," nothing more.
package workspace

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"

	"golang.org/x/sys/unix"
)

// DecodeBundle parses a Bundle from the raw bytes a sealedstore.Store
// returned (already digest-verified by Store.Get itself — this function
// does no additional integrity check beyond shape/consistency
// validation). Decodes into the SAME FileRecord shape apply's own
// json.Marshal(Bundle{...}) produces (transaction.go's own wire tags) —
// deliberately NOT a separate presence-tracking DTO: a round 1 attempt
// at that (pointer fields to distinguish "absent" from "zero value")
// broke round-tripping apply's own legitimate bundles, because Go's
// `,omitempty` on an empty []byte and a real `after:null` for a
// genuinely empty file are BOTH valid zero-value encodings that a
// presence-pointer cannot tell apart from a missing key (codex review
// round 2, HIGH #1 — reproduced live: apply(existing empty file) →
// sealedstore.Get → DecodeBundle rejected its own producer's artifact).
// Instead, validate VALUES after decoding: fails closed on an empty
// base_name, a duplicate (rel_dir, base_name) target, existed=false
// paired with a nonzero before/before_mode (an internally inconsistent
// record), or a mode outside the permission-bit vocabulary
// (foundation/descriptor.go never captures anything else) — every one
// of these is a real shape defect, none of them is something apply's
// own encoder can ever legitimately produce.
func DecodeBundle(data []byte) (Bundle, error) {
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	var b Bundle
	if err := dec.Decode(&b); err != nil {
		return Bundle{}, fmt.Errorf("workspace: decode sealed bundle (fail closed): %w", err)
	}
	if _, err := dec.Token(); err != io.EOF {
		return Bundle{}, fmt.Errorf("workspace: sealed bundle has trailing data after its JSON value (fail closed)")
	}
	if len(b.Files) == 0 {
		return Bundle{}, fmt.Errorf("workspace: sealed bundle names no files (fail closed)")
	}

	const permBits = 0o777
	seen := make(map[string]bool, len(b.Files))
	for _, fr := range b.Files {
		if fr.BaseName == "" {
			return Bundle{}, fmt.Errorf("workspace: sealed bundle names a file with an empty base_name (fail closed)")
		}
		key := fr.RelDir + "\x00" + fr.BaseName
		if seen[key] {
			return Bundle{}, fmt.Errorf("workspace: sealed bundle names %q twice (fail closed)", key)
		}
		seen[key] = true
		if !fr.Existed && (len(fr.Before) != 0 || fr.BeforeMode != 0) {
			return Bundle{}, fmt.Errorf("workspace: sealed bundle entry %q has existed=false but sets before/before_mode (fail closed)", fr.BaseName)
		}
		if fr.AfterMode&^permBits != 0 {
			return Bundle{}, fmt.Errorf("workspace: sealed bundle entry %q has after_mode %#o outside the permission-bit vocabulary (fail closed)", fr.BaseName, fr.AfterMode)
		}
		if fr.Existed && fr.BeforeMode&^permBits != 0 {
			return Bundle{}, fmt.Errorf("workspace: sealed bundle entry %q has before_mode %#o outside the permission-bit vocabulary (fail closed)", fr.BaseName, fr.BeforeMode)
		}
		// apply itself now refuses to commit an after-mode lacking
		// owner-read (transaction.go, codex review round 3, HIGH #1):
		// every file apply WRITES is owned by the NEXUS process itself,
		// so classification reopening it O_RDONLY later can rely on the
		// owner-read bit. An after_mode without it is therefore an
		// impossible producer narrative. before_mode carries NO such
		// guarantee — it is the PRE-EXISTING file's captured mode, which
		// may belong to a different UID and be readable only through
		// group/other bits (apply's own CaptureFileBeneath already
		// proved it was readable AT capture time; permission bits alone
		// don't encode who owns it). Do NOT apply the same owner-read
		// requirement to before_mode (codex review round 4, HIGH #1 —
		// reproduced live: a real cross-UID file at mode 0044, readable
		// via other-bits, captured and replaced successfully by apply,
		// then rejected by an earlier version of this exact check).
		if fr.AfterMode&0o400 == 0 {
			return Bundle{}, fmt.Errorf("workspace: sealed bundle entry %q has after_mode %#o without the owner-read bit — impossible producer narrative (fail closed)", fr.BaseName, fr.AfterMode)
		}
	}
	return b, nil
}

// FileState is one file's classification against a sealed Bundle's two
// captured images — invariant 4 step 6's own exhaustive per-file table.
type FileState int

const (
	// FileBefore: the file's CURRENT on-disk content+mode matches
	// exactly what the bundle captured BEFORE the transaction (or, if
	// the bundle recorded the file did not exist before, it still does
	// not exist).
	FileBefore FileState = iota + 1
	// FileAfter: matches exactly what the bundle captured AFTER.
	FileAfter
	// FileForeign: matches NEITHER captured image — something else
	// changed this file since the bundle was sealed. Per invariant 4:
	// refuse, manual reconciliation only, never a guess.
	FileForeign
)

func (s FileState) String() string {
	switch s {
	case FileBefore:
		return "BEFORE"
	case FileAfter:
		return "AFTER"
	case FileForeign:
		return "FOREIGN"
	default:
		return fmt.Sprintf("FileState(%d)", int(s))
	}
}

// FileClassification is one Bundle file's restart-time state.
type FileClassification struct {
	RelDir, BaseName string
	State            FileState
}

// TransactionState summarizes a whole Bundle's classification —
// invariant 4 step 6's own exhaustive table, the part that does not
// depend on whether a `workspace.mutation_committed` event exists (that
// check belongs to the AS-YET-UNBUILT restart scan this piece's own doc
// comment describes: a rehydrated-UNKNOWN operation can never have a
// durable mutation_committed, since ApplyGoverned always commits that
// event in the SAME atomic batch as S7's own SUCCEEDED transition — see
// governed.go's succeededBuild).
type TransactionState int

const (
	// TransactionNeverStarted: every file is still BEFORE — the crash
	// happened before Apply's write loop touched anything (or Apply
	// never got that far). Safe to retry fresh.
	TransactionNeverStarted TransactionState = iota + 1
	// TransactionMidCrash: every file is AFTER, or a mix of BEFORE and
	// AFTER — Apply's write loop was running (or finished) when the
	// crash happened, but `mutation_committed` never landed. Must be
	// rolled back through a governed PolicyWorkspaceRollback operation
	// (not built in this piece) before the original operation may retry.
	TransactionMidCrash
	// TransactionForeign: at least one file matches neither captured
	// image. Refuse; manual reconciliation only, never a guess and
	// never a blind retry or rollback.
	TransactionForeign
)

func (s TransactionState) String() string {
	switch s {
	case TransactionNeverStarted:
		return "NEVER_STARTED"
	case TransactionMidCrash:
		return "MID_CRASH"
	case TransactionForeign:
		return "FOREIGN"
	default:
		return fmt.Sprintf("TransactionState(%d)", int(s))
	}
}

// ClassifyBundle re-reads the CURRENT on-disk state of every file a
// sealed Bundle recorded — via the SAME descriptor-pinned rootFd
// discipline every other capture in this package uses, never a
// path-string API — and classifies each file, then summarizes the whole
// transaction. Read-only: performs no write, no rollback, no S7 call.
func ClassifyBundle(rootFd int, b Bundle) ([]FileClassification, TransactionState, error) {
	if len(b.Files) == 0 {
		return nil, 0, fmt.Errorf("workspace: classify: bundle names no files (fail closed)")
	}
	classes := make([]FileClassification, 0, len(b.Files))
	for _, fr := range b.Files {
		state, err := classifyOneFile(rootFd, fr)
		if err != nil {
			return nil, 0, err
		}
		classes = append(classes, FileClassification{RelDir: fr.RelDir, BaseName: fr.BaseName, State: state})
	}

	anyForeign, anyAfter, allBefore := false, false, true
	for _, c := range classes {
		switch c.State {
		case FileForeign:
			anyForeign = true
		case FileAfter:
			anyAfter = true
			allBefore = false
		case FileBefore:
			// no-op
		}
	}
	switch {
	case anyForeign:
		return classes, TransactionForeign, nil
	case allBefore:
		return classes, TransactionNeverStarted, nil
	case anyAfter:
		return classes, TransactionMidCrash, nil
	default:
		// Every file was BEFORE-or-AFTER and allBefore is false, so at
		// least one AFTER exists — anyAfter must be true; this branch is
		// unreachable, kept only so the switch is visibly exhaustive.
		return classes, TransactionMidCrash, nil
	}
}

func classifyOneFile(rootFd int, fr FileRecord) (FileState, error) {
	dirFd, err := WalkDirBeneath(rootFd, fr.RelDir)
	if err != nil {
		return 0, fmt.Errorf("workspace: classify %q: %w", fr.BaseName, err)
	}
	defer unix.Close(dirFd)

	content, _, _, mode, capErr := CaptureFileBeneath(dirFd, fr.BaseName)
	switch {
	case capErr == nil:
		// BEFORE is checked first, deliberately: a no-op mutation (After
		// byte-identical to Before, e.g. a mutation whose write never ran
		// because Apply was cancelled first) makes both images match the
		// current content. Resolve that overlap conservatively as
		// BEFORE — "nothing happened here yet" — never as a completed
		// write that didn't actually occur (codex review round 1, HIGH
		// #2: a pre-write-cancelled no-op mutation was misclassified
		// AFTER, which would have driven a spurious rollback).
		if fr.Existed && sha256Hex(content) == sha256Hex(fr.Before) && mode.Perm() == fs.FileMode(fr.BeforeMode).Perm() {
			return FileBefore, nil
		}
		if sha256Hex(content) == sha256Hex(fr.After) && mode.Perm() == fs.FileMode(fr.AfterMode).Perm() {
			return FileAfter, nil
		}
		return FileForeign, nil
	case errors.Is(capErr, fs.ErrNotExist):
		if !fr.Existed {
			return FileBefore, nil // matches the bundle's own "did not exist before" state
		}
		return FileForeign, nil // existed before, missing now — unexplained, never a guess
	default:
		return 0, fmt.Errorf("workspace: classify %q: %w", fr.BaseName, capErr)
	}
}
