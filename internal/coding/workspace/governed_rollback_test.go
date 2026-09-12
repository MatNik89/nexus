//go:build linux

package workspace

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/MatNik89/nexus/internal/foundation/sealedstore"
	"github.com/MatNik89/nexus/internal/kernel/contracts"
	"github.com/MatNik89/nexus/internal/kernel/journal"
	"github.com/MatNik89/nexus/internal/kernel/s7"
	"golang.org/x/sys/unix"
)

// midCrashOriginalOp produces a REAL MID_CRASH crash-orphan via
// scan_test.go's own crashApply helper (real Begin/Next/Consume, real
// write, no Report — a genuine simulated crash), using its OWN throwaway
// Authority for the crash itself (an in-memory Authority mid-attempt is
// NOT yet UNKNOWN — only a FRESH Authority rehydrated from the SAME
// journal reads it back that way, the same "restart" pattern scan_test.go
// itself always uses), then confirms via RestartScan on that freshly
// rehydrated Authority that it classifies MID_CRASH before returning it
// for the caller's own use — exactly the shape RollbackGoverned's own
// authentication (RestartScan reuse) requires to even consider
// originalOp eligible.
func midCrashOriginalOp(t *testing.T, rootFd int, store *sealedstore.Store, j *journal.Journal, planDigest string) (contracts.OperationID, string, *s7.Authority) {
	t.Helper()
	crashGrants := testDurableGrants(t, j)
	op, digest := crashApply(t, context.Background(), rootFd, store, j, crashGrants, []FileMutation{
		{BaseName: "a.go", After: []byte("package new\n"), Mode: 0o644},
	}, planDigest)
	grants := testDurableGrants(t, j) // "restart": rehydrated from the SAME journal
	findings, err := RestartScan(rootFd, j, grants, store)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, f := range findings {
		if f.Op == op {
			if f.State != TransactionMidCrash {
				t.Fatalf("precondition: op classified %v, want MID_CRASH", f.State)
			}
			found = true
		}
	}
	if !found {
		t.Fatal("precondition: op not found among RestartScan's own findings")
	}
	return op, digest, grants
}

// Detector: a real MID_CRASH crash-orphan is FULLY rolled back by a
// single RollbackGoverned call — both durable events land, S7 reaches
// SUCCEEDED, and the caller supplies ONLY originalOp (no bundle digest).
func TestRollbackGovernedSucceedsAndCommitsBothDurableEvents(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "a.go"), []byte("package old\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	rootFd, err := OpenRoot(root)
	if err != nil {
		t.Fatal(err)
	}
	defer unix.Close(rootFd)
	store := newTestStore(t)
	j := testDurableJournal(t)

	originalOp, digest, grants := midCrashOriginalOp(t, rootFd, store, j, "plan-rollback-1")

	result, err := RollbackGoverned(context.Background(), rootFd, store, originalOp, grants, j, "run-1")
	if err != nil {
		t.Fatalf("RollbackGoverned: %v", err)
	}
	if !result.Succeeded {
		t.Fatal("expected Succeeded = true")
	}
	if result.State != TransactionNeverStarted {
		t.Fatalf("state = %v, want NEVER_STARTED", result.State)
	}
	got, err := os.ReadFile(filepath.Join(root, "a.go"))
	if err != nil || string(got) != "package old\n" {
		t.Fatalf("expected the file restored: got=%q err=%v", got, err)
	}

	op := RollbackOperationID(originalOp, digest)
	state, ok := grants.State(op)
	if !ok || state != contracts.AttemptSucceeded {
		t.Fatalf("operation state = %v (ok=%v), want SUCCEEDED", state, ok)
	}
	types := journalEventTypes(t, j)
	if types[EvRollbackStarted] != 1 {
		t.Fatalf("workspace.rollback_started count = %d, want 1", types[EvRollbackStarted])
	}
	if types[EvRollbackCommitted] != 1 {
		t.Fatalf("workspace.rollback_committed count = %d, want 1", types[EvRollbackCommitted])
	}
}

// Detector: originalOp/runID are required — fail closed rather than
// deriving a meaningless identity.
func TestRollbackGovernedRequiresOriginalOpAndRunID(t *testing.T) {
	root := t.TempDir()
	rootFd, err := OpenRoot(root)
	if err != nil {
		t.Fatal(err)
	}
	defer unix.Close(rootFd)
	store := newTestStore(t)
	j := testDurableJournal(t)
	grants := testDurableGrants(t, j)

	if _, err := RollbackGoverned(context.Background(), rootFd, store, "", grants, j, "run-1"); err == nil {
		t.Fatal("expected an error: empty original op")
	}
	if _, err := RollbackGoverned(context.Background(), rootFd, store, "workspace.apply:work:1:2:plan", grants, j, ""); err == nil {
		t.Fatal("expected an error: empty run id")
	}
}

// Detector (round 1 HIGH #1, reproduced live): a forged/never-Begin'd
// original operation string — never discoverable via RestartScan — is
// refused outright, never accepted as a bare caller assertion.
func TestRollbackGovernedRefusesUnauthenticatedOriginalOp(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "a.go"), []byte("package old\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	rootFd, err := OpenRoot(root)
	if err != nil {
		t.Fatal(err)
	}
	defer unix.Close(rootFd)
	store := newTestStore(t)
	j := testDurableJournal(t)
	grants := testDurableGrants(t, j)

	forged := testApplyOp(t, rootFd, "plan-never-begun") // a plausible-looking string; never Begin'd, never Consumed
	if _, err := RollbackGoverned(context.Background(), rootFd, store, forged, grants, j, "run-1"); err == nil {
		t.Fatal("expected an error: forged original op never authenticated via RestartScan")
	}
}

// Detector: an original operation whose bundle classifies NEVER_STARTED
// (nothing was ever written) has nothing for a PHYSICAL rollback to do —
// refused, distinct from MID_CRASH.
func TestRollbackGovernedRefusesWhenOriginalNeverStarted(t *testing.T) {
	root := t.TempDir()
	rootFd, err := OpenRoot(root)
	if err != nil {
		t.Fatal(err)
	}
	defer unix.Close(rootFd)
	store := newTestStore(t)
	j := testDurableJournal(t)
	grants := testDurableGrants(t, j)

	cctx, cancel := context.WithCancel(context.Background())
	cancel()
	originalOp, _ := crashApply(t, cctx, rootFd, store, j, grants, []FileMutation{
		{BaseName: "a.go", After: []byte("package new\n"), Mode: 0o644},
	}, "plan-never-started")

	_, err = RollbackGoverned(context.Background(), rootFd, store, originalOp, grants, j, "run-1")
	if err == nil {
		t.Fatal("expected an error: original op classifies NEVER_STARTED, not MID_CRASH")
	}
}

// Detector: a MID_CRASH classification with NO attempt error reports
// FailedRetryable with CodeRollbackIncomplete.
func TestRollbackGovernedReportsFailedRetryableWhenIncomplete(t *testing.T) {
	orig := rollbackFn
	defer func() { rollbackFn = orig }()
	rollbackFn = func(ctx context.Context, rootFd int, b Bundle) ([]FileClassification, TransactionState, error) {
		return []FileClassification{{BaseName: "a.go", State: FileAfter}}, TransactionMidCrash, nil
	}

	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "a.go"), []byte("package old\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	rootFd, err := OpenRoot(root)
	if err != nil {
		t.Fatal(err)
	}
	defer unix.Close(rootFd)
	store := newTestStore(t)
	j := testDurableJournal(t)
	originalOp, digest, grants := midCrashOriginalOp(t, rootFd, store, j, "plan-x")

	result, err := RollbackGoverned(context.Background(), rootFd, store, originalOp, grants, j, "run-1")
	if err == nil {
		t.Fatal("expected an error: rollback incomplete")
	}
	if result.Succeeded {
		t.Fatal("expected Succeeded = false")
	}
	op := RollbackOperationID(originalOp, digest)
	state, ok := grants.State(op)
	if !ok || state != contracts.AttemptFailedRetryable {
		t.Fatalf("operation state = %v (ok=%v), want FAILED_RETRYABLE", state, ok)
	}
}

// Detector (a distinct reviewer's round 1 HIGH finding, reproduced
// live): MID_CRASH paired with a REAL (non-cancellation) per-file error
// — rollbackBundle's own NORMAL "partial progress" shape, per its own
// doc comment — must STILL report FailedRetryable, not Unknown. An
// earlier version checked rollbackErr!=nil BEFORE checking
// state==TransactionMidCrash, making the entire MaxAttempts:5 retry
// budget unreachable for this exact, realistic case.
func TestRollbackGovernedReportsFailedRetryableEvenWithRealPerFileError(t *testing.T) {
	orig := rollbackFn
	defer func() { rollbackFn = orig }()
	injected := errors.New("injected per-file restore failure")
	rollbackFn = func(ctx context.Context, rootFd int, b Bundle) ([]FileClassification, TransactionState, error) {
		return []FileClassification{{BaseName: "a.go", State: FileAfter}}, TransactionMidCrash, injected
	}

	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "a.go"), []byte("package old\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	rootFd, err := OpenRoot(root)
	if err != nil {
		t.Fatal(err)
	}
	defer unix.Close(rootFd)
	store := newTestStore(t)
	j := testDurableJournal(t)
	originalOp, digest, grants := midCrashOriginalOp(t, rootFd, store, j, "plan-x")

	result, err := RollbackGoverned(context.Background(), rootFd, store, originalOp, grants, j, "run-1")
	if err == nil {
		t.Fatal("expected an error: rollback incomplete")
	}
	if result.Succeeded {
		t.Fatal("expected Succeeded = false")
	}
	op := RollbackOperationID(originalOp, digest)
	state, ok := grants.State(op)
	if !ok || state != contracts.AttemptFailedRetryable {
		t.Fatalf("operation state = %v (ok=%v), want FAILED_RETRYABLE (MID_CRASH must win over a per-file error)", state, ok)
	}
}

// Detector: a FOREIGN classification reports Unknown — manual
// reconciliation only, matching invariant 4's own table.
func TestRollbackGovernedReportsUnknownWhenForeign(t *testing.T) {
	orig := rollbackFn
	defer func() { rollbackFn = orig }()
	rollbackFn = func(ctx context.Context, rootFd int, b Bundle) ([]FileClassification, TransactionState, error) {
		return []FileClassification{{BaseName: "a.go", State: FileForeign}}, TransactionForeign, nil
	}

	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "a.go"), []byte("package old\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	rootFd, err := OpenRoot(root)
	if err != nil {
		t.Fatal(err)
	}
	defer unix.Close(rootFd)
	store := newTestStore(t)
	j := testDurableJournal(t)
	originalOp, digest, grants := midCrashOriginalOp(t, rootFd, store, j, "plan-x")

	result, err := RollbackGoverned(context.Background(), rootFd, store, originalOp, grants, j, "run-1")
	if err == nil {
		t.Fatal("expected an error: bundle is FOREIGN")
	}
	if result.Succeeded {
		t.Fatal("expected Succeeded = false")
	}
	op := RollbackOperationID(originalOp, digest)
	state, ok := grants.State(op)
	if !ok || state != contracts.AttemptUnknown {
		t.Fatalf("operation state = %v (ok=%v), want UNKNOWN", state, ok)
	}
}

// Detector (round 1 HIGH #4, reproduced live): FOREIGN must never be
// masked by a concurrent cancellation joined into the same error.
func TestRollbackGovernedDoesNotMaskForeignWithCancellation(t *testing.T) {
	orig := rollbackFn
	defer func() { rollbackFn = orig }()
	rollbackFn = func(ctx context.Context, rootFd int, b Bundle) ([]FileClassification, TransactionState, error) {
		return []FileClassification{{BaseName: "a.go", State: FileForeign}}, TransactionForeign, context.Canceled
	}

	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "a.go"), []byte("package old\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	rootFd, err := OpenRoot(root)
	if err != nil {
		t.Fatal(err)
	}
	defer unix.Close(rootFd)
	store := newTestStore(t)
	j := testDurableJournal(t)
	originalOp, digest, grants := midCrashOriginalOp(t, rootFd, store, j, "plan-x")

	_, err = RollbackGoverned(context.Background(), rootFd, store, originalOp, grants, j, "run-1")
	if err == nil {
		t.Fatal("expected an error")
	}
	op := RollbackOperationID(originalOp, digest)
	state, ok := grants.State(op)
	if !ok || state != contracts.AttemptUnknown {
		t.Fatalf("operation state = %v (ok=%v), want UNKNOWN (FOREIGN must win over a joined cancellation, never CANCELLED)", state, ok)
	}
}

// Detector (the central safety property): a NEVER_STARTED classification
// carrying a REAL (non-cancellation) attempt error must NEVER be
// reported Succeeded — it lands Unknown.
func TestRollbackGovernedReportsUnknownWhenNeverStartedHasRealError(t *testing.T) {
	orig := rollbackFn
	defer func() { rollbackFn = orig }()
	injected := errors.New("injected durability failure")
	rollbackFn = func(ctx context.Context, rootFd int, b Bundle) ([]FileClassification, TransactionState, error) {
		return []FileClassification{{BaseName: "a.go", State: FileBefore}}, TransactionNeverStarted, injected
	}

	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "a.go"), []byte("package old\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	rootFd, err := OpenRoot(root)
	if err != nil {
		t.Fatal(err)
	}
	defer unix.Close(rootFd)
	store := newTestStore(t)
	j := testDurableJournal(t)
	originalOp, digest, grants := midCrashOriginalOp(t, rootFd, store, j, "plan-x")

	result, err := RollbackGoverned(context.Background(), rootFd, store, originalOp, grants, j, "run-1")
	if !errors.Is(err, injected) {
		t.Fatalf("expected errors.Is(err, injected): %v", err)
	}
	if result.Succeeded {
		t.Fatal("expected Succeeded = false")
	}
	op := RollbackOperationID(originalOp, digest)
	state, ok := grants.State(op)
	if !ok || state != contracts.AttemptUnknown {
		t.Fatalf("operation state = %v (ok=%v), want UNKNOWN", state, ok)
	}
}

// Detector: a NEVER_STARTED classification carrying a PURE cancellation
// (nothing else wrong) lands CANCELLED — matches ApplyGoverned's own
// "verified clean, but told to stop" precedent.
func TestRollbackGovernedCancelsWhenNeverStartedHasPureCancellationError(t *testing.T) {
	orig := rollbackFn
	defer func() { rollbackFn = orig }()
	rollbackFn = func(ctx context.Context, rootFd int, b Bundle) ([]FileClassification, TransactionState, error) {
		return []FileClassification{{BaseName: "a.go", State: FileBefore}}, TransactionNeverStarted, context.Canceled
	}

	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "a.go"), []byte("package old\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	rootFd, err := OpenRoot(root)
	if err != nil {
		t.Fatal(err)
	}
	defer unix.Close(rootFd)
	store := newTestStore(t)
	j := testDurableJournal(t)
	originalOp, digest, grants := midCrashOriginalOp(t, rootFd, store, j, "plan-x")

	_, err = RollbackGoverned(context.Background(), rootFd, store, originalOp, grants, j, "run-1")
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected errors.Is(err, context.Canceled): %v", err)
	}
	op := RollbackOperationID(originalOp, digest)
	state, ok := grants.State(op)
	if !ok || state != contracts.AttemptCancelled {
		t.Fatalf("operation state = %v (ok=%v), want CANCELLED", state, ok)
	}
}

// Detector (round 1 HIGH #5, reproduced live): an unrecognized
// TransactionState value must fail closed to Unknown, never default to
// Succeeded.
func TestRollbackGovernedRefusesUnrecognizedTransactionState(t *testing.T) {
	orig := rollbackFn
	defer func() { rollbackFn = orig }()
	rollbackFn = func(ctx context.Context, rootFd int, b Bundle) ([]FileClassification, TransactionState, error) {
		return nil, TransactionState(99), nil
	}

	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "a.go"), []byte("package old\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	rootFd, err := OpenRoot(root)
	if err != nil {
		t.Fatal(err)
	}
	defer unix.Close(rootFd)
	store := newTestStore(t)
	j := testDurableJournal(t)
	originalOp, digest, grants := midCrashOriginalOp(t, rootFd, store, j, "plan-x")

	result, err := RollbackGoverned(context.Background(), rootFd, store, originalOp, grants, j, "run-1")
	if err == nil {
		t.Fatal("expected an error: unrecognized transaction state")
	}
	if result.Succeeded {
		t.Fatal("expected Succeeded = false")
	}
	op := RollbackOperationID(originalOp, digest)
	state, ok := grants.State(op)
	if !ok || state != contracts.AttemptUnknown {
		t.Fatalf("operation state = %v (ok=%v), want UNKNOWN", state, ok)
	}
}

// Detector: Consume succeeding but AttemptContext itself failing is the
// SAME "never got to run" shape as a cancellation — Cancelled.
func TestRollbackGovernedCancelsWhenAttemptContextFailsAfterConsume(t *testing.T) {
	origDAC := deriveAttemptContext
	defer func() { deriveAttemptContext = origDAC }()
	deriveAttemptContext = func(grants *s7.Authority, ctx context.Context, op contracts.OperationID) (context.Context, context.CancelFunc, error) {
		return nil, nil, fmt.Errorf("injected attempt-context failure")
	}

	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "a.go"), []byte("package old\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	rootFd, err := OpenRoot(root)
	if err != nil {
		t.Fatal(err)
	}
	defer unix.Close(rootFd)
	store := newTestStore(t)
	j := testDurableJournal(t)
	originalOp, digest, grants := midCrashOriginalOp(t, rootFd, store, j, "plan-x")

	_, err = RollbackGoverned(context.Background(), rootFd, store, originalOp, grants, j, "run-1")
	if err == nil {
		t.Fatal("expected an error: injected attempt-context failure")
	}
	op := RollbackOperationID(originalOp, digest)
	state, ok := grants.State(op)
	if !ok || state != contracts.AttemptCancelled {
		t.Fatalf("operation state = %v (ok=%v), want CANCELLED", state, ok)
	}
	got, readErr := os.ReadFile(filepath.Join(root, "a.go"))
	if readErr != nil || string(got) != "package new\n" {
		t.Fatalf("expected zero writes (rollbackFn never called): got=%q err=%v", got, readErr)
	}
}

// Detector: a REAL retry after a genuinely incomplete first attempt
// reaches SUCCEEDED on a second call to the SAME operation.
func TestRollbackGovernedRetriesAfterIncompleteAndSucceeds(t *testing.T) {
	origPolicy := PolicyWorkspaceRollback
	PolicyWorkspaceRollback = s7.Policy{EffectClass: contracts.EffectIrreversible, MaxAttempts: 5, Durable: true,
		RetryableCodes: []string{s7.CodeRollbackIncomplete}} // zero-value Backoff: immediately due, no sleep needed
	defer func() { PolicyWorkspaceRollback = origPolicy }()

	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "a.go"), []byte("package old\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	rootFd, err := OpenRoot(root)
	if err != nil {
		t.Fatal(err)
	}
	defer unix.Close(rootFd)
	store := newTestStore(t)
	j := testDurableJournal(t)
	originalOp, digest, grants := midCrashOriginalOp(t, rootFd, store, j, "plan-x")

	origFn := rollbackFn
	rollbackFn = func(ctx context.Context, rootFd int, b Bundle) ([]FileClassification, TransactionState, error) {
		return []FileClassification{{BaseName: "a.go", State: FileAfter}}, TransactionMidCrash, nil
	}
	result1, err1 := RollbackGoverned(context.Background(), rootFd, store, originalOp, grants, j, "run-1")
	if err1 == nil || result1.Succeeded {
		t.Fatalf("expected the FIRST attempt to report incomplete: result=%+v err=%v", result1, err1)
	}
	rollbackFn = origFn // second attempt uses the REAL primitive

	result2, err2 := RollbackGoverned(context.Background(), rootFd, store, originalOp, grants, j, "run-1")
	if err2 != nil {
		t.Fatalf("expected the SECOND attempt to succeed: %v", err2)
	}
	if !result2.Succeeded {
		t.Fatal("expected Succeeded = true on the second attempt")
	}
	got, readErr := os.ReadFile(filepath.Join(root, "a.go"))
	if readErr != nil || string(got) != "package old\n" {
		t.Fatalf("expected the file restored after the second attempt: got=%q err=%v", got, readErr)
	}
	op := RollbackOperationID(originalOp, digest)
	state, ok := grants.State(op)
	if !ok || state != contracts.AttemptSucceeded {
		t.Fatalf("operation state = %v (ok=%v), want SUCCEEDED", state, ok)
	}
}

// Detector (round 2 HIGH #2, independently reproduced live by codex): a
// rollback op that already reported FAILED_RETRYABLE (a normal, durable
// Report — NOT a crash, so its own S7 state is FAILED_RETRYABLE, never
// UNKNOWN) must not be permanently stranded once the physical write set
// happens to already read NEVER_STARTED by the time the next call comes
// in. An earlier version refused ANY call whose finding.State wasn't
// MID_CRASH unless the op was specifically UNKNOWN, silently excluding
// every OTHER pre-existing state (most concretely FAILED_RETRYABLE).
func TestRollbackGovernedSelfHealsFailedRetryableWhenBundleAlreadyReadsNeverStarted(t *testing.T) {
	origPolicy := PolicyWorkspaceRollback
	PolicyWorkspaceRollback = s7.Policy{EffectClass: contracts.EffectIrreversible, MaxAttempts: 5, Durable: true,
		RetryableCodes: []string{s7.CodeRollbackIncomplete}} // zero-value Backoff: immediately due, no sleep needed
	defer func() { PolicyWorkspaceRollback = origPolicy }()

	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "a.go"), []byte("package old\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	rootFd, err := OpenRoot(root)
	if err != nil {
		t.Fatal(err)
	}
	defer unix.Close(rootFd)
	store := newTestStore(t)
	j := testDurableJournal(t)
	originalOp, digest, grants := midCrashOriginalOp(t, rootFd, store, j, "plan-x")

	origFn := rollbackFn
	rollbackFn = func(ctx context.Context, rootFd int, b Bundle) ([]FileClassification, TransactionState, error) {
		return []FileClassification{{BaseName: "a.go", State: FileAfter}}, TransactionMidCrash, nil
	}
	if _, err := RollbackGoverned(context.Background(), rootFd, store, originalOp, grants, j, "run-1"); err == nil {
		t.Fatal("expected the FIRST attempt to report incomplete")
	}
	rollbackFn = origFn

	op := RollbackOperationID(originalOp, digest)
	state, ok := grants.State(op)
	if !ok || state != contracts.AttemptFailedRetryable {
		t.Fatalf("precondition: operation state = %v (ok=%v), want FAILED_RETRYABLE", state, ok)
	}

	// Something OTHER than this SAME call now physically completes the
	// restore (here: a direct call to the package-private primitive,
	// standing in for whatever legitimate path could reach this —
	// notably a LATER retry attempt within this same op's own budget
	// that finished every write but still reported a non-cancellation
	// error, landing FAILED_RETRYABLE despite the bundle already reading
	// NEVER_STARTED).
	raw, err := store.Get(digest)
	if err != nil {
		t.Fatal(err)
	}
	b, err := DecodeBundle(raw)
	if err != nil {
		t.Fatal(err)
	}
	if _, cstate, cerr := rollbackBundle(context.Background(), rootFd, b); cerr != nil || cstate != TransactionNeverStarted {
		t.Fatalf("precondition: physical completion must succeed: state=%v err=%v", cstate, cerr)
	}

	result, err := RollbackGoverned(context.Background(), rootFd, store, originalOp, grants, j, "run-2")
	if err != nil {
		t.Fatalf("expected the stranded FAILED_RETRYABLE op to self-heal: %v", err)
	}
	if !result.Succeeded {
		t.Fatal("expected Succeeded = true")
	}
	state, ok = grants.State(op)
	if !ok || state != contracts.AttemptSucceeded {
		t.Fatalf("operation state = %v (ok=%v), want SUCCEEDED", state, ok)
	}
}

// Detector (round 2 HIGH #3, independently reproduced live by codex): an
// UNKNOWN operation whose id merely COLLIDES with this deterministic
// RollbackOperationID string, but was actually Begin'd against a
// DIFFERENT target, must never be trusted as THIS package's own
// legitimate rollback attempt — self-recovery must verify the SAME
// target/policy binding RestartScan already verifies for the ORIGINAL
// operation's own identity.
func TestRollbackGovernedSelfRecoveryRefusesForeignlyBoundOperationID(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "a.go"), []byte("package old\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	rootFd, err := OpenRoot(root)
	if err != nil {
		t.Fatal(err)
	}
	defer unix.Close(rootFd)
	store := newTestStore(t)
	j := testDurableJournal(t)
	originalOp, digest, grants := midCrashOriginalOp(t, rootFd, store, j, "plan-r2-foreign")

	rop := RollbackOperationID(originalOp, digest)
	foreignTarget := contracts.TargetID("workspace:foreign")
	if err := grants.Begin(rop, foreignTarget, PolicyWorkspaceRollback); err != nil {
		t.Fatal(err)
	}
	attemptNo := grants.Attempts(rop)
	build := func(l s7.Landing) s7.Companion {
		return s7.Companion{Key: rop, Params: envelope(j, rop, "run-x", attemptNo, EvRollbackFailed,
			rollbackFailedPayload{Op: string(rop), OriginalOp: string(originalOp), BundleDigest: digest,
				AttemptNo: attemptNo, Landing: int(l.Kind), Code: l.Code})}
	}
	grant, err := grants.Next(rop, build)
	if err != nil {
		t.Fatal(err)
	}
	attemptNo = grant.AttemptNo
	companion := s7.Companion{Key: rop, Params: envelope(j, rop, "run-x", grant.AttemptNo, EvRollbackStarted,
		rollbackStartedPayload{Op: string(rop), OriginalOp: string(originalOp), BundleDigest: digest})}
	if err := grants.Consume(grant, companion); err != nil {
		t.Fatal(err)
	}
	if err := grants.Report(rop, s7.OutcomeUnknown, "", build); err != nil {
		t.Fatal(err)
	}
	state, ok := grants.State(rop)
	if !ok || state != contracts.AttemptUnknown {
		t.Fatalf("precondition: foreign-bound op must be UNKNOWN, got %v (ok=%v)", state, ok)
	}

	// Physically complete the restore too, so the ONLY thing standing
	// between this op and a wrongly-trusted SUCCEEDED report is the
	// target/policy check itself.
	raw, err := store.Get(digest)
	if err != nil {
		t.Fatal(err)
	}
	b, err := DecodeBundle(raw)
	if err != nil {
		t.Fatal(err)
	}
	if _, cstate, cerr := rollbackBundle(context.Background(), rootFd, b); cerr != nil || cstate != TransactionNeverStarted {
		t.Fatalf("precondition: physical completion must succeed: state=%v err=%v", cstate, cerr)
	}

	if _, err := RollbackGoverned(context.Background(), rootFd, store, originalOp, grants, j, "run-y"); err == nil {
		t.Fatal("expected an error: the UNKNOWN op is bound to a foreign target, never this call's own attempt")
	}
	state, ok = grants.State(rop)
	if !ok || state != contracts.AttemptUnknown {
		t.Fatalf("operation state = %v (ok=%v), want UNKNOWN (untouched — never reconciled without a verified target/policy match)", state, ok)
	}
}

// Detector (round 2 finding, codex): rollbackErr is preserved (wrapped),
// not discarded, when MID_CRASH reports FailedRetryable — a caller must
// still be able to errors.Is/errors.As against the real underlying
// cause even though the durable S7 outcome itself is unaffected.
func TestRollbackGovernedPreservesUnderlyingErrorWhenMidCrash(t *testing.T) {
	orig := rollbackFn
	defer func() { rollbackFn = orig }()
	sentinel := errors.New("injected per-file durability failure")
	rollbackFn = func(ctx context.Context, rootFd int, b Bundle) ([]FileClassification, TransactionState, error) {
		return []FileClassification{{BaseName: "a.go", State: FileAfter}}, TransactionMidCrash, sentinel
	}

	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "a.go"), []byte("package old\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	rootFd, err := OpenRoot(root)
	if err != nil {
		t.Fatal(err)
	}
	defer unix.Close(rootFd)
	store := newTestStore(t)
	j := testDurableJournal(t)
	originalOp, digest, grants := midCrashOriginalOp(t, rootFd, store, j, "plan-x")

	_, err = RollbackGoverned(context.Background(), rootFd, store, originalOp, grants, j, "run-1")
	if !errors.Is(err, sentinel) {
		t.Fatalf("expected errors.Is(err, sentinel) = true: %v", err)
	}
	op := RollbackOperationID(originalOp, digest)
	state, ok := grants.State(op)
	if !ok || state != contracts.AttemptFailedRetryable {
		t.Fatalf("operation state = %v (ok=%v), want FAILED_RETRYABLE (the durable outcome is unaffected by preserving the error)", state, ok)
	}
}

// Detector (round 4/5 finding, independently reproduced live by codex,
// kilo and agy — the finding that disproved round 4's own first
// correction attempt): a REAL per-file error whose file never reached
// BEFORE at all (content untouched — e.g. a transient EACCES/ENOSPC on
// the temp-file create, before any rename) must NOT poison a later,
// fully clean retry. There is nothing to "re-verify" for a file that
// was never touched; the whole operation must be free to succeed once
// retried.
func TestRollbackGovernedTransientErrorOnUntouchedFileDoesNotBlockLaterSuccess(t *testing.T) {
	origPolicy := PolicyWorkspaceRollback
	PolicyWorkspaceRollback = s7.Policy{EffectClass: contracts.EffectIrreversible, MaxAttempts: 5, Durable: true,
		RetryableCodes: []string{s7.CodeRollbackIncomplete}}
	defer func() { PolicyWorkspaceRollback = origPolicy }()

	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "a.go"), []byte("old"), 0o644); err != nil {
		t.Fatal(err)
	}
	rootFd, err := OpenRoot(root)
	if err != nil {
		t.Fatal(err)
	}
	defer unix.Close(rootFd)
	store := newTestStore(t)
	j := testDurableJournal(t)
	crashGrants := testDurableGrants(t, j)
	originalOp, digest := crashApply(t, context.Background(), rootFd, store, j, crashGrants, []FileMutation{
		{RelDir: "", BaseName: "a.go", After: []byte("new"), Mode: 0o644},
	}, "plan-r5-transient")
	grants := testDurableGrants(t, j)

	origFn := rollbackFn
	transient := errors.New("transient: temporary write lock")
	rollbackFn = func(ctx context.Context, rootFd int, b Bundle) ([]FileClassification, TransactionState, error) {
		// a.go's write itself failed outright: content stays AFTER,
		// nothing was ever touched.
		return []FileClassification{{BaseName: "a.go", State: FileAfter}}, TransactionMidCrash, transient
	}
	_, err1 := RollbackGoverned(context.Background(), rootFd, store, originalOp, grants, j, "run-1")
	if err1 == nil {
		t.Fatal("attempt 1 should report incomplete")
	}
	rollbackFn = origFn

	rop := RollbackOperationID(originalOp, digest)
	if state, _ := grants.State(rop); state != contracts.AttemptFailedRetryable {
		t.Fatalf("precondition: state = %v, want FAILED_RETRYABLE", state)
	}

	result, err2 := RollbackGoverned(context.Background(), rootFd, store, originalOp, grants, j, "run-2")
	state, _ := grants.State(rop)
	if err2 != nil || !result.Succeeded || state != contracts.AttemptSucceeded {
		t.Fatalf("a transient error on a file that was never touched must not block a later clean success: err=%v result=%+v state=%v", err2, result, state)
	}
}

// Detector (round 4/5 finding, agy/codex, live-reproduced): a real error
// on one file must not blame an UNRELATED file that restored cleanly in
// the SAME attempt.
func TestRollbackGovernedUnrelatedCleanFileNotBlamedForAnotherFilesFailure(t *testing.T) {
	origPolicy := PolicyWorkspaceRollback
	PolicyWorkspaceRollback = s7.Policy{EffectClass: contracts.EffectIrreversible, MaxAttempts: 5, Durable: true,
		RetryableCodes: []string{s7.CodeRollbackIncomplete}}
	defer func() { PolicyWorkspaceRollback = origPolicy }()

	root := t.TempDir()
	for _, d := range []string{"d1", "d2"} {
		if err := os.Mkdir(filepath.Join(root, d), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(root, "d1", "a.go"), []byte("old"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "d2", "b.go"), []byte("old"), 0o644); err != nil {
		t.Fatal(err)
	}
	rootFd, err := OpenRoot(root)
	if err != nil {
		t.Fatal(err)
	}
	defer unix.Close(rootFd)
	store := newTestStore(t)
	j := testDurableJournal(t)
	crashGrants := testDurableGrants(t, j)
	originalOp, digest := crashApply(t, context.Background(), rootFd, store, j, crashGrants, []FileMutation{
		{RelDir: "d1", BaseName: "a.go", After: []byte("new"), Mode: 0o644},
		{RelDir: "d2", BaseName: "b.go", After: []byte("new"), Mode: 0o644},
	}, "plan-r5-unrelated")
	grants := testDurableGrants(t, j)

	origFn := rollbackFn
	unrelated := errors.New("unrelated: b.go write failed outright, never touched")
	rollbackFn = func(ctx context.Context, rootFd int, b Bundle) ([]FileClassification, TransactionState, error) {
		// a.go restores fully and cleanly THIS attempt (durable, real
		// write); b.go's write fails outright and stays After.
		if err := os.WriteFile(filepath.Join(root, "d1", "a.go"), []byte("old"), 0o644); err != nil {
			t.Fatal(err)
		}
		return []FileClassification{
			{RelDir: "d1", BaseName: "a.go", State: FileBefore},
			{RelDir: "d2", BaseName: "b.go", State: FileAfter},
		}, TransactionMidCrash, unrelated
	}
	_, err1 := RollbackGoverned(context.Background(), rootFd, store, originalOp, grants, j, "run-1")
	if err1 == nil {
		t.Fatal("attempt 1 should report incomplete")
	}
	rollbackFn = origFn

	rop := RollbackOperationID(originalOp, digest)
	if state, _ := grants.State(rop); state != contracts.AttemptFailedRetryable {
		t.Fatalf("precondition: state = %v, want FAILED_RETRYABLE", state)
	}

	result, err2 := RollbackGoverned(context.Background(), rootFd, store, originalOp, grants, j, "run-2")
	state, _ := grants.State(rop)
	if err2 != nil || !result.Succeeded || state != contracts.AttemptSucceeded {
		t.Fatalf("a.go's own clean, unrelated restore must not be blamed for b.go's separate write failure: err=%v result=%+v state=%v", err2, result, state)
	}
}

// Detector (round 4/5 HIGH finding, codex, live-reproduced): a SECOND
// restart-only rehydration (two crashes with no real attempt in
// between) inherits S7's own CodeCrashRecovered as rec.lastCode
// (events.go), not "" — the earlier "only normalize an empty code"
// check let this non-empty-but-still-wrong code through verbatim,
// which validApplyFailedNarrative then rejects, durably failing the
// reconcile append itself and stranding the operation.
func TestRollbackGovernedNormalizesCrashRecoveredCodeOnSecondRehydration(t *testing.T) {
	origPolicy := PolicyWorkspaceRollback
	PolicyWorkspaceRollback = s7.Policy{EffectClass: contracts.EffectIrreversible, MaxAttempts: 5, Durable: true,
		RetryableCodes: []string{s7.CodeRollbackIncomplete}} // zero-value Backoff: immediately due, no sleep needed
	defer func() { PolicyWorkspaceRollback = origPolicy }()

	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "a.go"), []byte("package old\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	rootFd, err := OpenRoot(root)
	if err != nil {
		t.Fatal(err)
	}
	defer unix.Close(rootFd)
	store := newTestStore(t)
	j := testDurableJournal(t)
	originalOp, digest, grants := midCrashOriginalOp(t, rootFd, store, j, "plan-crash-recovered")

	// Same hand-rolled Begin/Next/Consume-then-crash shape as
	// TestRollbackGovernedRecoversFromCrashDuringPriorRollbackAttemptThatDidNotFinish
	// — a rollback attempt that crashed before ever Reporting.
	rop := RollbackOperationID(originalOp, digest)
	rootDev, rootIno := mustRootDevIno(t, rootFd)
	rtarget := ApplyTargetID(testProfile, rootDev, rootIno)
	if err := grants.Begin(rop, rtarget, PolicyWorkspaceRollback); err != nil {
		t.Fatal(err)
	}
	rbuild := func(l s7.Landing) s7.Companion {
		return s7.Companion{Key: rop, Params: envelope(j, rop, "run-crash", 0, EvRollbackFailed,
			rollbackFailedPayload{Op: string(rop), AttemptNo: 0, Landing: int(l.Kind)})}
	}
	rgrant, err := grants.Next(rop, rbuild)
	if err != nil {
		t.Fatal(err)
	}
	rcompanion := s7.Companion{Key: rop, Params: envelope(j, rop, "run-crash", rgrant.AttemptNo, EvRollbackStarted,
		rollbackStartedPayload{Op: string(rop), OriginalOp: string(originalOp), BundleDigest: digest})}
	if err := grants.Consume(rgrant, rcompanion); err != nil {
		t.Fatal(err)
	}
	// No rollbackBundle call, no Report — this IS the simulated crash.

	// First restart: RUNNING -> UNKNOWN, durably appended with
	// lastCode=CodeCrashRecovered (S7's own events.go, not this
	// package's doing).
	grants2 := testDurableGrants(t, j)
	if state, ok := grants2.State(rop); !ok || state != contracts.AttemptUnknown {
		t.Fatalf("precondition: state = %v (ok=%v), want UNKNOWN after first restart", state, ok)
	}

	// Second restart: no real attempt ran in between, so the durably
	// persisted lastCode column is STILL CodeCrashRecovered when this
	// SAME op is loaded fresh again.
	grants3 := testDurableGrants(t, j)
	if state, ok := grants3.State(rop); !ok || state != contracts.AttemptUnknown {
		t.Fatalf("precondition: state = %v (ok=%v), want UNKNOWN after second restart", state, ok)
	}

	result, err := RollbackGoverned(context.Background(), rootFd, store, originalOp, grants3, j, "run-crash-recovered")
	if err != nil {
		t.Fatalf("crash_recovered code was not normalized, self-recovery failed: %v", err)
	}
	if !result.Succeeded {
		t.Fatal("expected self-recovery to reconcile and succeed (a.go was never touched by the crashed attempt)")
	}
	state, ok := grants3.State(rop)
	if !ok || state != contracts.AttemptSucceeded {
		t.Fatalf("operation state = %v (ok=%v), want SUCCEEDED", state, ok)
	}
}

// Detector (round 3 HIGH finding, codex, live-reproduced — this is the
// CENTRAL safety property this whole file's own "DURABILITY ACROSS
// RETRIES" doc-comment section exists for; test rewritten round 5 after
// codex/kilo/agy independently disproved the round-3/4 signal, see that
// doc comment): a REAL two-file, two-attempt sequence where attempt 1
// restores file A but A's own directory fsync fails (content correct,
// durability unproven) while cancellation stops file B before its own
// restore even starts (so the whole bundle correctly, safely lands
// FAILED_RETRYABLE/MID_CRASH per round 1's own requirement) — then
// attempt 2 restores B cleanly (a HEALTHY directory) but SKIPS A
// entirely (rollback.go's own established, converged "already matches
// BEFORE" skip); A's own directory is STILL genuinely, persistently
// broken (unlike a merely-transient failure). Reporting Succeeded here
// would assert A's own restore durability was proven when
// verifyDurabilityBeforeSuccess's own fresh re-check of A's directory
// still fails — exactly what invariant 2 exists to prevent. A's
// directory is identified by (dev,ino), not by call order, so the
// re-verification pass (which fsyncs every BEFORE file's directory,
// including A's) is exercised deterministically regardless of how many
// times or in what order fsyncDirHook is invoked.
func TestRollbackGovernedNeverClaimsSuccessOverAnUnverifiedDurabilityBarrier(t *testing.T) {
	origPolicy := PolicyWorkspaceRollback
	PolicyWorkspaceRollback = s7.Policy{EffectClass: contracts.EffectIrreversible, MaxAttempts: 5, Durable: true,
		RetryableCodes: []string{s7.CodeRollbackIncomplete}} // zero-value Backoff: immediately due, no sleep needed
	defer func() { PolicyWorkspaceRollback = origPolicy }()

	root := t.TempDir()
	for _, d := range []string{"d1", "d2"} {
		if err := os.Mkdir(filepath.Join(root, d), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	for _, p := range []string{"d1/a.go", "d2/b.go"} {
		if err := os.WriteFile(filepath.Join(root, p), []byte("old"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	rootFd, err := OpenRoot(root)
	if err != nil {
		t.Fatal(err)
	}
	defer unix.Close(rootFd)
	store := newTestStore(t)
	j := testDurableJournal(t)
	crashGrants := testDurableGrants(t, j)
	original, digest := crashApply(t, context.Background(), rootFd, store, j, crashGrants, []FileMutation{
		{RelDir: "d1", BaseName: "a.go", After: []byte("new"), Mode: 0o644},
		{RelDir: "d2", BaseName: "b.go", After: []byte("new"), Mode: 0o644},
	}, "plan-r3-durability")
	grants := testDurableGrants(t, j)

	d1Fd, err := WalkDirBeneath(rootFd, "d1")
	if err != nil {
		t.Fatal(err)
	}
	var d1St unix.Stat_t
	if err := unix.Fstat(d1Fd, &d1St); err != nil {
		t.Fatal(err)
	}
	unix.Close(d1Fd)
	isD1 := func(dirfd int) bool {
		var st unix.Stat_t
		if err := unix.Fstat(dirfd, &st); err != nil {
			return false
		}
		return uint64(st.Dev) == uint64(d1St.Dev) && st.Ino == d1St.Ino
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	sentinel := errors.New("injected d1 directory fsync failure")
	origHook := fsyncDirHook
	defer func() { fsyncDirHook = origHook }()
	cancelled := false
	fsyncDirHook = func(dirfd int) error {
		if isD1(dirfd) {
			// d1 is PERSISTENTLY broken for this whole test — every call
			// against it fails, never healed.
			if !cancelled {
				cancelled = true
				cancel() // simulates the crash landing during d1's own trailing fsync
			}
			return sentinel
		}
		return unix.Fsync(dirfd)
	}

	_, firstErr := RollbackGoverned(ctx, rootFd, store, original, grants, j, "run-r3-1")
	if !errors.Is(firstErr, sentinel) {
		t.Fatalf("first attempt lost the sentinel error: %v", firstErr)
	}
	rop := RollbackOperationID(original, digest)
	if state, _ := grants.State(rop); state != contracts.AttemptFailedRetryable {
		t.Fatalf("precondition: state = %v, want FAILED_RETRYABLE", state)
	}

	// second attempt: d2 (b.go) is healthy and restores cleanly; d1
	// (a.go) is skipped (already BEFORE) but is STILL persistently
	// broken — the final re-verification pass must still catch it.
	result, secondErr := RollbackGoverned(context.Background(), rootFd, store, original, grants, j, "run-r3-2")
	state, _ := grants.State(rop)
	if secondErr == nil && result.Succeeded && state == contracts.AttemptSucceeded {
		t.Fatalf("unproven d1 directory-fsync durability became a durable SUCCEEDED report: result=%+v state=%v", result, state)
	}
	if !errors.Is(secondErr, sentinel) {
		t.Fatalf("second attempt's refusal lost the sentinel error: %v", secondErr)
	}
}

// Detector (round 5, the positive counterpart to the test above): once
// the SAME genuinely-broken directory heals, a subsequent attempt's
// fresh re-verification must be ABLE to prove durability and succeed —
// the whole point of re-proving now rather than distrusting history
// forever (verifyDurabilityBeforeSuccess's own doc comment).
func TestRollbackGovernedSucceedsOnceAPersistentDurabilityFailureHeals(t *testing.T) {
	origPolicy := PolicyWorkspaceRollback
	PolicyWorkspaceRollback = s7.Policy{EffectClass: contracts.EffectIrreversible, MaxAttempts: 5, Durable: true,
		RetryableCodes: []string{s7.CodeRollbackIncomplete}}
	defer func() { PolicyWorkspaceRollback = origPolicy }()

	root := t.TempDir()
	for _, d := range []string{"d1", "d2"} {
		if err := os.Mkdir(filepath.Join(root, d), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	for _, p := range []string{"d1/a.go", "d2/b.go"} {
		if err := os.WriteFile(filepath.Join(root, p), []byte("old"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	rootFd, err := OpenRoot(root)
	if err != nil {
		t.Fatal(err)
	}
	defer unix.Close(rootFd)
	store := newTestStore(t)
	j := testDurableJournal(t)
	crashGrants := testDurableGrants(t, j)
	original, digest := crashApply(t, context.Background(), rootFd, store, j, crashGrants, []FileMutation{
		{RelDir: "d1", BaseName: "a.go", After: []byte("new"), Mode: 0o644},
		{RelDir: "d2", BaseName: "b.go", After: []byte("new"), Mode: 0o644},
	}, "plan-r5-heals")
	grants := testDurableGrants(t, j)

	d1Fd, err := WalkDirBeneath(rootFd, "d1")
	if err != nil {
		t.Fatal(err)
	}
	var d1St unix.Stat_t
	if err := unix.Fstat(d1Fd, &d1St); err != nil {
		t.Fatal(err)
	}
	unix.Close(d1Fd)
	isD1 := func(dirfd int) bool {
		var st unix.Stat_t
		if err := unix.Fstat(dirfd, &st); err != nil {
			return false
		}
		return uint64(st.Dev) == uint64(d1St.Dev) && st.Ino == d1St.Ino
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	sentinel := errors.New("injected d1 directory fsync failure")
	origHook := fsyncDirHook
	defer func() { fsyncDirHook = origHook }()
	broken := true
	cancelled := false
	fsyncDirHook = func(dirfd int) error {
		if isD1(dirfd) && broken {
			if !cancelled {
				cancelled = true
				cancel()
			}
			return sentinel
		}
		return unix.Fsync(dirfd)
	}

	_, firstErr := RollbackGoverned(ctx, rootFd, store, original, grants, j, "run-r5-1")
	if !errors.Is(firstErr, sentinel) {
		t.Fatalf("first attempt lost the sentinel error: %v", firstErr)
	}
	rop := RollbackOperationID(original, digest)
	if state, _ := grants.State(rop); state != contracts.AttemptFailedRetryable {
		t.Fatalf("precondition: state = %v, want FAILED_RETRYABLE", state)
	}

	broken = false // the underlying storage problem is now genuinely resolved
	result, secondErr := RollbackGoverned(context.Background(), rootFd, store, original, grants, j, "run-r5-2")
	state, _ := grants.State(rop)
	if secondErr != nil || !result.Succeeded || state != contracts.AttemptSucceeded {
		t.Fatalf("a genuinely healed durability barrier should be provable and succeed: err=%v result=%+v state=%v", secondErr, result, state)
	}
}

// Detector (round 4 finding, reproduced live): round 3's own
// classification-only UnverifiedDurability signal over-flagged a
// genuinely clean MID_CRASH pass. Attempt 1 restores d1/a.go fully
// (write+fsync both proven — simulated here by writing the file's own
// BEFORE content directly, since rollbackFn is stubbed to skip the real
// write path) and is cancelled with NO error before ever touching
// d2/b.go: this is rollbackBundle's own "verified, just not finished"
// MID_CRASH shape (round 1's own kilo finding: this exact case is why
// MID_CRASH's retry budget must stay reachable at all), not an unproven
// durability barrier. Attempt 2 then restores d2/b.go cleanly and must
// be allowed to report Succeeded — flagging it Unknown here would be a
// FALSE positive, permanently stranding an operation that never had a
// real per-file failure.
func TestRollbackGovernedDoesNotFlagAProvenDurableMidCrashPass(t *testing.T) {
	origPolicy := PolicyWorkspaceRollback
	PolicyWorkspaceRollback = s7.Policy{EffectClass: contracts.EffectIrreversible, MaxAttempts: 5, Durable: true,
		RetryableCodes: []string{s7.CodeRollbackIncomplete}}
	defer func() { PolicyWorkspaceRollback = origPolicy }()

	root := t.TempDir()
	for _, d := range []string{"d1", "d2"} {
		if err := os.Mkdir(filepath.Join(root, d), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(root, "d1", "a.go"), []byte("old"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "d2", "b.go"), []byte("old"), 0o644); err != nil {
		t.Fatal(err)
	}
	rootFd, err := OpenRoot(root)
	if err != nil {
		t.Fatal(err)
	}
	defer unix.Close(rootFd)
	store := newTestStore(t)
	j := testDurableJournal(t)

	crashGrants := testDurableGrants(t, j)
	originalOp, digest := crashApply(t, context.Background(), rootFd, store, j, crashGrants, []FileMutation{
		{RelDir: "d1", BaseName: "a.go", After: []byte("new"), Mode: 0o644},
		{RelDir: "d2", BaseName: "b.go", After: []byte("new"), Mode: 0o644},
	}, "plan-r4-false-positive")
	grants := testDurableGrants(t, j)

	// Simulate "attempt 1 already restored a.go durably" (no failed
	// fsync anywhere): a.go's content already matches BEFORE.
	if err := os.WriteFile(filepath.Join(root, "d1", "a.go"), []byte("old"), 0o644); err != nil {
		t.Fatal(err)
	}

	origFn := rollbackFn
	rollbackFn = func(ctx context.Context, rootFd int, b Bundle) ([]FileClassification, TransactionState, error) {
		// What a real, fully-clean attempt returns when it restored a.go
		// (durably) and was then cancelled — no error at all — before
		// ever touching b.go.
		return []FileClassification{
			{RelDir: "d1", BaseName: "a.go", State: FileBefore},
			{RelDir: "d2", BaseName: "b.go", State: FileAfter},
		}, TransactionMidCrash, nil
	}
	_, err1 := RollbackGoverned(context.Background(), rootFd, store, originalOp, grants, j, "run-r4-1")
	if err1 == nil {
		t.Fatal("first attempt should report incomplete")
	}
	rollbackFn = origFn

	rop := RollbackOperationID(originalOp, digest)
	if state, _ := grants.State(rop); state != contracts.AttemptFailedRetryable {
		t.Fatalf("precondition: state = %v, want FAILED_RETRYABLE", state)
	}

	result, err2 := RollbackGoverned(context.Background(), rootFd, store, originalOp, grants, j, "run-r4-2")
	state, _ := grants.State(rop)
	if err2 != nil || !result.Succeeded || state != contracts.AttemptSucceeded {
		t.Fatalf("FALSE POSITIVE: a durably-completed rollback (a.go restored durably in attempt 1, b.go restored cleanly in attempt 2, zero real per-file errors ever) was refused: err=%v result=%+v state=%v", err2, result, state)
	}
}

// Detector (round 6 HIGH finding, codex, live-reproduced via a Go build
// overlay): verifyDurabilityBeforeSuccess's own durability barriers run
// against a STALE classification — rollbackFn's own return value —
// without ever re-checking it is still accurate. A concurrent writer
// landing in the window between that classification and this function's
// own fsync calls could invalidate it entirely (here: turn the file
// FOREIGN), and the un-fixed code would still report SUCCEEDED because
// it never re-classifies. The fix re-classifies the WHOLE bundle fresh,
// AFTER every durability barrier, and refuses unless it still reads
// TransactionNeverStarted.
func TestRollbackGovernedRefusesStaleClassificationInvalidatedByConcurrentWriter(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "a.go"), []byte("package old\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	rootFd, err := OpenRoot(root)
	if err != nil {
		t.Fatal(err)
	}
	defer unix.Close(rootFd)
	store := newTestStore(t)
	j := testDurableJournal(t)
	originalOp, _, grants := midCrashOriginalOp(t, rootFd, store, j, "plan-toctou")

	origFn := rollbackFn
	rollbackFn = func(ctx context.Context, rootFd int, b Bundle) ([]FileClassification, TransactionState, error) {
		// Simulate a concurrent writer landing in the window between
		// rollbackFn's own classification and verifyDurabilityBeforeSuccess's
		// re-check: the file gets overwritten to neither BEFORE nor AFTER
		// content right as rollbackFn itself returns "all clean".
		if err := os.WriteFile(filepath.Join(root, "a.go"), []byte("concurrent writer content"), 0o644); err != nil {
			t.Fatal(err)
		}
		return []FileClassification{{BaseName: "a.go", State: FileBefore}}, TransactionNeverStarted, nil
	}
	defer func() { rollbackFn = origFn }()

	result, gotErr := RollbackGoverned(context.Background(), rootFd, store, originalOp, grants, j, "run-toctou")
	if gotErr == nil && result.Succeeded {
		t.Fatalf("reported SUCCEEDED despite a fresh re-classification showing the file no longer matches BEFORE: result=%+v", result)
	}
}

// Detector (round 6 HIGH finding, codex, live-reproduced): a file can
// classify FileBefore without its OWN content ever having been fsynced
// by rollback.go itself (round 3/4/5's premise only covered files
// rollback.go actually wrote this attempt — not ones already reading
// BEFORE for any other reason). verifyDurabilityBeforeSuccess must
// re-fsync each existing file's own content, not just its directory;
// this proves the mechanism is real and load-bearing via the SAME
// fault-injection seam pattern fsyncDirHook already establishes.
func TestRollbackGovernedRefusesWhenFileContentFsyncFails(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "a.go"), []byte("package old\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	rootFd, err := OpenRoot(root)
	if err != nil {
		t.Fatal(err)
	}
	defer unix.Close(rootFd)
	store := newTestStore(t)
	j := testDurableJournal(t)
	originalOp, _, grants := midCrashOriginalOp(t, rootFd, store, j, "plan-file-fsync-fail")

	origFileHook := fsyncFileHook
	defer func() { fsyncFileHook = origFileHook }()
	fsyncFileHook = func(fd int) error {
		return errors.New("injected file content fsync failure")
	}

	result, gotErr := RollbackGoverned(context.Background(), rootFd, store, originalOp, grants, j, "run-file-fsync-fail")
	if gotErr == nil && result.Succeeded {
		t.Fatalf("reported SUCCEEDED despite the file's own content fsync failing during re-verification: result=%+v", result)
	}
}

// Detector (round 7 HIGH finding, codex, live-reproduced 3/3 via a Go
// build overlay): the round-6 fix's own fresh reclassification compares
// content/mode only, never inode identity — so a concurrent writer can
// fsync-race a DIFFERENT inode carrying byte-identical BEFORE content
// into place after this function's own fsync call touched the ORIGINAL
// inode. The reclassification alone cannot detect this (content matches
// either way); verifyDurabilityBeforeSuccess must ALSO re-verify that
// the identity it fsynced is still the one present after reclassifying.
func TestRollbackGovernedRefusesWhenFsyncedInodeIsReplacedBeforeFinalClassification(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(root, "a.go")
	if err := os.WriteFile(target, []byte("package old\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	rootFd, err := OpenRoot(root)
	if err != nil {
		t.Fatal(err)
	}
	defer unix.Close(rootFd)
	store := newTestStore(t)
	j := testDurableJournal(t)
	originalOp, _, grants := midCrashOriginalOp(t, rootFd, store, j, "plan-inode-substitution")

	origFileHook := fsyncFileHook
	defer func() { fsyncFileHook = origFileHook }()
	var synced, replacement unix.Stat_t
	fsyncFileHook = func(fd int) error {
		if err := unix.Fstat(fd, &synced); err != nil {
			return err
		}
		if err := unix.Fsync(fd); err != nil {
			return err
		}
		// A concurrent writer swaps in a NEW inode with byte-identical
		// BEFORE content, right after this fd's own fsync landed —
		// deliberately never fsyncing the new inode itself.
		tmp := filepath.Join(root, "replacement.tmp")
		rf, err := os.OpenFile(tmp, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
		if err != nil {
			return err
		}
		if _, err := rf.Write([]byte("package old\n")); err != nil {
			rf.Close()
			return err
		}
		if err := rf.Close(); err != nil { // deliberately no content fsync
			return err
		}
		if err := os.Rename(tmp, target); err != nil {
			return err
		}
		return unix.Stat(target, &replacement)
	}

	result, gotErr := RollbackGoverned(context.Background(), rootFd, store, originalOp, grants, j, "run-inode-substitution")
	if synced.Ino == replacement.Ino {
		t.Fatalf("test setup failed to substitute the inode: synced=%d replacement=%d", synced.Ino, replacement.Ino)
	}
	if gotErr == nil && result.Succeeded {
		t.Fatalf("reported SUCCEEDED after fsyncing inode %d while the final check observed a different, never-fsynced inode %d", synced.Ino, replacement.Ino)
	}
}

// Detector (round 6 HIGH finding, codex, live-reproduced): the round 4/5
// crash_recovered normalization only covered LandingRetry — an EXHAUSTED
// retry budget produces LandingTerminal carrying the SAME inherited
// CodeCrashRecovered, which the Retry-only guard let through unnormalized,
// durably rejecting the exhaustion companion and stranding the operation
// UNKNOWN instead of landing it terminal.
func TestRollbackGovernedNormalizesCrashRecoveredCodeOnExhaustedTerminalLanding(t *testing.T) {
	origPolicy := PolicyWorkspaceRollback
	PolicyWorkspaceRollback = s7.Policy{EffectClass: contracts.EffectIrreversible, MaxAttempts: 1, Durable: true,
		RetryableCodes: []string{s7.CodeRollbackIncomplete}}
	defer func() { PolicyWorkspaceRollback = origPolicy }()

	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "a.go"), []byte("package old\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	rootFd, err := OpenRoot(root)
	if err != nil {
		t.Fatal(err)
	}
	defer unix.Close(rootFd)
	store := newTestStore(t)
	j := testDurableJournal(t)
	originalOp, digest, grants := midCrashOriginalOp(t, rootFd, store, j, "plan-crash-recovered-terminal")

	rop := RollbackOperationID(originalOp, digest)
	rootDev, rootIno := mustRootDevIno(t, rootFd)
	rtarget := ApplyTargetID(testProfile, rootDev, rootIno)
	if err := grants.Begin(rop, rtarget, PolicyWorkspaceRollback); err != nil {
		t.Fatal(err)
	}
	rbuild := func(l s7.Landing) s7.Companion {
		return s7.Companion{Key: rop, Params: envelope(j, rop, "run-crash", 0, EvRollbackFailed,
			rollbackFailedPayload{Op: string(rop), AttemptNo: 0, Landing: int(l.Kind)})}
	}
	rgrant, err := grants.Next(rop, rbuild)
	if err != nil {
		t.Fatal(err)
	}
	rcompanion := s7.Companion{Key: rop, Params: envelope(j, rop, "run-crash", rgrant.AttemptNo, EvRollbackStarted,
		rollbackStartedPayload{Op: string(rop), OriginalOp: string(originalOp), BundleDigest: digest})}
	if err := grants.Consume(rgrant, rcompanion); err != nil {
		t.Fatal(err)
	}
	// Crash before Report — this consumes the ONLY attempt MaxAttempts:1
	// allows, so a later reconcile finds the budget exhausted.

	// First restart: RUNNING -> UNKNOWN, lastCode=CodeCrashRecovered.
	grants2 := testDurableGrants(t, j)
	if state, ok := grants2.State(rop); !ok || state != contracts.AttemptUnknown {
		t.Fatalf("precondition: state = %v (ok=%v), want UNKNOWN after first restart", state, ok)
	}
	// Second restart: no real attempt ran in between, lastCode is STILL
	// CodeCrashRecovered.
	grants3 := testDurableGrants(t, j)
	if state, ok := grants3.State(rop); !ok || state != contracts.AttemptUnknown {
		t.Fatalf("precondition: state = %v (ok=%v), want UNKNOWN after second restart", state, ok)
	}

	_, gotErr := RollbackGoverned(context.Background(), rootFd, store, originalOp, grants3, j, "run-crash-recovered-terminal")
	if gotErr != nil && strings.Contains(gotErr.Error(), "impossible narrative") {
		t.Fatalf("crash_recovered code was not normalized for the exhausted LandingTerminal companion: %v", gotErr)
	}
}

// Detector (round 3 MEDIUM finding, codex, reproduced live): a forged
// workspace.rollback_failed payload naming an operation identity never
// bound to any real original_op+bundle_digest pair must be refused —
// the SAME Op-consistency check started/committed already enforce.
func TestRollbackFailedValidatorRejectsUnboundOperationIdentity(t *testing.T) {
	forged := []byte(`{"op":"workspace.rollback:forged","attempt_no":1,"landing":2,"code":"rollback_incomplete"}`)
	if err := Events()[EvRollbackFailed](forged); err == nil {
		t.Fatal("expected an error: rollback_failed accepted an operation identity with no original_op+bundle_digest binding")
	}
}

// Detector (round 3 MEDIUM finding, codex): the FOREIGN branch must
// preserve its own accompanying causal error too — the SAME diagnostic
// fix already applied to the MID_CRASH branch.
func TestRollbackGovernedPreservesUnderlyingErrorWhenForeign(t *testing.T) {
	orig := rollbackFn
	defer func() { rollbackFn = orig }()
	sentinel := errors.New("injected foreign-path read failure")
	rollbackFn = func(ctx context.Context, rootFd int, b Bundle) ([]FileClassification, TransactionState, error) {
		return []FileClassification{{BaseName: "a.go", State: FileForeign}}, TransactionForeign, sentinel
	}

	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "a.go"), []byte("old"), 0o644); err != nil {
		t.Fatal(err)
	}
	rootFd, err := OpenRoot(root)
	if err != nil {
		t.Fatal(err)
	}
	defer unix.Close(rootFd)
	store := newTestStore(t)
	j := testDurableJournal(t)
	original, _, grants := midCrashOriginalOp(t, rootFd, store, j, "plan-r3-foreign-error")

	_, gotErr := RollbackGoverned(context.Background(), rootFd, store, original, grants, j, "run-1")
	if !errors.Is(gotErr, sentinel) {
		t.Fatalf("FOREIGN discarded its own causal error: %v", gotErr)
	}
}

// Detector (round 1 HIGH #3, reproduced live): a crash DURING a prior
// rollback attempt (Consume landed, real rollback ran to completion,
// but Report never landed — a simulated crash) rehydrates THIS
// operation to UNKNOWN too. A fresh RollbackGoverned call must
// self-recover via Reconcile, not fail forever with
// ATTEMPT_NOT_AUTHORIZED.
func TestRollbackGovernedRecoversFromCrashDuringPriorRollbackAttemptThatFullySucceeded(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "a.go"), []byte("package old\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	rootFd, err := OpenRoot(root)
	if err != nil {
		t.Fatal(err)
	}
	defer unix.Close(rootFd)
	store := newTestStore(t)
	j := testDurableJournal(t)
	originalOp, digest, grants := midCrashOriginalOp(t, rootFd, store, j, "plan-x")

	// Simulate a crash mid-rollback: Begin/Next/Consume the ROLLBACK
	// operation by hand (bypassing RollbackGoverned), run the REAL
	// rollbackBundle to full completion, then deliberately never Report.
	rop := RollbackOperationID(originalOp, digest)
	rootDev, rootIno := mustRootDevIno(t, rootFd)
	rtarget := ApplyTargetID(testProfile, rootDev, rootIno)
	if err := grants.Begin(rop, rtarget, PolicyWorkspaceRollback); err != nil {
		t.Fatal(err)
	}
	rbuild := func(l s7.Landing) s7.Companion {
		return s7.Companion{Key: rop, Params: envelope(j, rop, "run-crash", 0, EvRollbackFailed,
			rollbackFailedPayload{Op: string(rop), AttemptNo: 0, Landing: int(l.Kind)})}
	}
	rgrant, err := grants.Next(rop, rbuild)
	if err != nil {
		t.Fatal(err)
	}
	rcompanion := s7.Companion{Key: rop, Params: envelope(j, rop, "run-crash", rgrant.AttemptNo, EvRollbackStarted,
		rollbackStartedPayload{Op: string(rop), OriginalOp: string(originalOp), BundleDigest: digest})}
	if err := grants.Consume(rgrant, rcompanion); err != nil {
		t.Fatal(err)
	}
	raw, err := store.Get(digest)
	if err != nil {
		t.Fatal(err)
	}
	b, err := DecodeBundle(raw)
	if err != nil {
		t.Fatal(err)
	}
	if _, state, rerr := rollbackBundle(context.Background(), rootFd, b); rerr != nil || state != TransactionNeverStarted {
		t.Fatalf("precondition: real rollback must fully succeed: state=%v err=%v", state, rerr)
	}
	// No Report — this IS the simulated crash.

	grants2 := testDurableGrants(t, j) // "restart"
	if state, ok := grants2.State(rop); !ok || state != contracts.AttemptUnknown {
		t.Fatalf("precondition: rollback op must rehydrate UNKNOWN, got %v (ok=%v)", state, ok)
	}

	result, err := RollbackGoverned(context.Background(), rootFd, store, originalOp, grants2, j, "run-restart")
	if err != nil {
		t.Fatalf("expected self-recovery to succeed: %v", err)
	}
	if !result.Succeeded {
		t.Fatal("expected Succeeded = true (self-recovered via Reconcile, no new write needed)")
	}
	state, ok := grants2.State(rop)
	if !ok || state != contracts.AttemptSucceeded {
		t.Fatalf("operation state = %v (ok=%v), want SUCCEEDED", state, ok)
	}
}

// Detector (round 2 finding, independently reproduced live by both kilo
// and agy): a crash DURING a prior rollback attempt that has NOT yet
// finished restoring (the bundle still classifies MID_CRASH, not
// NEVER_STARTED — the natural shape of "crashed mid-rollback", as
// opposed to the sibling test above which crashed AFTER every file was
// already restored) rehydrates the rollback op to UNKNOWN exactly like
// its sibling. reconcileRehydratedRollback's own Reconcile(op, false,
// ...) call lands S7's LandingRetry, whose Code field is populated from
// S7's own rec.lastCode — which is "" for an operation that was
// Consumed but never Reported even once (a genuine first crash, no
// prior FailedRetryable to inherit a code from). The build closure
// copied that empty code verbatim into the companion payload, which
// EvRollbackFailed's own validator then rejects (LandingRetry requires
// code == CodeRollbackIncomplete exactly) — so the Reconcile append
// itself failed "not durable", permanently stranding the operation
// UNKNOWN. Fixed by substituting s7.CodeRollbackIncomplete whenever a
// LandingRetry companion's own l.Code comes back empty.
func TestRollbackGovernedRecoversFromCrashDuringPriorRollbackAttemptThatDidNotFinish(t *testing.T) {
	origPolicy := PolicyWorkspaceRollback
	PolicyWorkspaceRollback = s7.Policy{EffectClass: contracts.EffectIrreversible, MaxAttempts: 5, Durable: true,
		RetryableCodes: []string{s7.CodeRollbackIncomplete}} // zero-value Backoff: immediately due, no sleep needed
	defer func() { PolicyWorkspaceRollback = origPolicy }()

	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "a.go"), []byte("package old\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	rootFd, err := OpenRoot(root)
	if err != nil {
		t.Fatal(err)
	}
	defer unix.Close(rootFd)
	store := newTestStore(t)
	j := testDurableJournal(t)
	originalOp, digest, grants := midCrashOriginalOp(t, rootFd, store, j, "plan-x")

	// Simulate a crash mid-rollback BEFORE any restore landed: Begin/
	// Next/Consume the ROLLBACK operation by hand, then crash without
	// ever calling rollbackBundle at all (the most minimal "did not
	// finish" shape — a.go still reads AFTER, so the bundle classifies
	// MID_CRASH, not NEVER_STARTED) and without ever Reporting.
	rop := RollbackOperationID(originalOp, digest)
	rootDev, rootIno := mustRootDevIno(t, rootFd)
	rtarget := ApplyTargetID(testProfile, rootDev, rootIno)
	if err := grants.Begin(rop, rtarget, PolicyWorkspaceRollback); err != nil {
		t.Fatal(err)
	}
	rbuild := func(l s7.Landing) s7.Companion {
		return s7.Companion{Key: rop, Params: envelope(j, rop, "run-crash", 0, EvRollbackFailed,
			rollbackFailedPayload{Op: string(rop), AttemptNo: 0, Landing: int(l.Kind)})}
	}
	rgrant, err := grants.Next(rop, rbuild)
	if err != nil {
		t.Fatal(err)
	}
	rcompanion := s7.Companion{Key: rop, Params: envelope(j, rop, "run-crash", rgrant.AttemptNo, EvRollbackStarted,
		rollbackStartedPayload{Op: string(rop), OriginalOp: string(originalOp), BundleDigest: digest})}
	if err := grants.Consume(rgrant, rcompanion); err != nil {
		t.Fatal(err)
	}
	// No rollbackBundle call, no Report — this IS the simulated crash.

	grants2 := testDurableGrants(t, j) // "restart"
	if state, ok := grants2.State(rop); !ok || state != contracts.AttemptUnknown {
		t.Fatalf("precondition: rollback op must rehydrate UNKNOWN, got %v (ok=%v)", state, ok)
	}

	result, err := RollbackGoverned(context.Background(), rootFd, store, originalOp, grants2, j, "run-restart")
	if err != nil {
		t.Fatalf("expected self-recovery to reconcile retry-eligible and then succeed in the same call: %v", err)
	}
	if !result.Succeeded {
		t.Fatal("expected Succeeded = true")
	}
	got, readErr := os.ReadFile(filepath.Join(root, "a.go"))
	if readErr != nil || string(got) != "package old\n" {
		t.Fatalf("expected the file restored: got=%q err=%v", got, readErr)
	}
	state, ok := grants2.State(rop)
	if !ok || state != contracts.AttemptSucceeded {
		t.Fatalf("operation state = %v (ok=%v), want SUCCEEDED", state, ok)
	}
}

// mustRootDevIno is a small test helper returning rootFd's own
// device+inode, for constructing the SAME ApplyTargetID RollbackGoverned
// itself derives.
func mustRootDevIno(t *testing.T, rootFd int) (uint64, uint64) {
	t.Helper()
	var st unix.Stat_t
	if err := unix.Fstat(rootFd, &st); err != nil {
		t.Fatal(err)
	}
	return uint64(st.Dev), st.Ino
}

// Detector (invariant 2's own required detector, mirrors ApplyGoverned's
// identical proof): a fault injected into EITHER half of the paired
// durable Consume batch must result in ZERO filesystem writes.
func TestRollbackGovernedFaultInEitherHalfOfConsumeBatchWritesZeroFiles(t *testing.T) {
	for _, faultOn := range []string{EvRollbackStarted, s7.EvAttemptStarted} {
		t.Run(faultOn, func(t *testing.T) {
			root := t.TempDir()
			if err := os.WriteFile(filepath.Join(root, "a.go"), []byte("package old\n"), 0o644); err != nil {
				t.Fatal(err)
			}
			rootFd, err := OpenRoot(root)
			if err != nil {
				t.Fatal(err)
			}
			defer unix.Close(rootFd)
			store := newTestStore(t)
			j := testDurableJournal(t)
			originalOp, _, grants := midCrashOriginalOp(t, rootFd, store, j, "plan-x")

			grants.SetAppendFault(func(types []string) error {
				for _, ty := range types {
					if ty == faultOn {
						return fmt.Errorf("injected fault on %s", faultOn)
					}
				}
				return nil
			})

			_, err = RollbackGoverned(context.Background(), rootFd, store, originalOp, grants, j, "run-1")
			if err == nil {
				t.Fatal("expected an error: the durable Consume batch was faulted")
			}
			got, readErr := os.ReadFile(filepath.Join(root, "a.go"))
			if readErr != nil {
				t.Fatal(readErr)
			}
			if string(got) != "package new\n" {
				t.Fatalf("a.go content = %q, want untouched (crash-shaped AFTER) %q (zero writes)", got, "package new\n")
			}
		})
	}
}

// Detector (round 2 finding, agy): isPureCancellation must not mistake a
// context.Canceled buried inside a SINGLE %w-wrapped errors.Join tree
// for a pure cancellation when a real, non-cancellation error is ALSO
// present in that same tree — a single fmt.Errorf("...: %w", ...) layer
// on top of the join must not bypass the multi-error recursion.
func TestIsPureCancellationDoesNotMaskARealErrorBehindASingleWrap(t *testing.T) {
	joined := errors.Join(context.Canceled, errors.New("real durability failure"))
	wrapped := fmt.Errorf("workspace: rollback: %w", joined)
	if isPureCancellation(wrapped) {
		t.Fatal("expected isPureCancellation = false: a real error is joined alongside the cancellation, one layer of %w down")
	}
	// A genuinely pure cancellation, similarly wrapped, must still read
	// as pure.
	pureWrapped := fmt.Errorf("workspace: rollback: %w", errors.Join(context.Canceled))
	if !isPureCancellation(pureWrapped) {
		t.Fatal("expected isPureCancellation = true: only a cancellation is present, one layer of %w down")
	}
}

// Detector: two calls with identical (originalOp, bundleDigest) derive
// the SAME operation id; any difference in either input derives a
// DIFFERENT one.
func TestRollbackOperationIDIsDeterministicAndExactIntent(t *testing.T) {
	base := RollbackOperationID("workspace.apply:work:1:2:plan-A", "digest-A")
	if got := RollbackOperationID("workspace.apply:work:1:2:plan-A", "digest-A"); got != base {
		t.Fatalf("identical inputs produced different ids: %q vs %q", base, got)
	}
	if got := RollbackOperationID("workspace.apply:work:1:2:plan-B", "digest-A"); got == base {
		t.Fatal("a different original op must produce a different rollback operation id")
	}
	if got := RollbackOperationID("workspace.apply:work:1:2:plan-A", "digest-B"); got == base {
		t.Fatal("a different bundle digest must produce a different rollback operation id")
	}
}

// Detector (round 1 MEDIUM #6, reproduced live): the durable event
// validators reject a payload whose Op disagrees with its own
// OriginalOp+BundleDigest.
func TestRollbackEventValidatorsRejectInconsistentOp(t *testing.T) {
	digest := fmt.Sprintf("%064x", 1)
	raw, err := json.Marshal(rollbackStartedPayload{Op: "workspace.rollback:other:" + digest, OriginalOp: "workspace.apply:w:1:2:p", BundleDigest: digest})
	if err != nil {
		t.Fatal(err)
	}
	if err := Events()[EvRollbackStarted](raw); err == nil {
		t.Fatal("expected an error: op inconsistent with original_op+bundle_digest")
	}

	raw2, err := json.Marshal(rollbackCommittedPayload{Op: "workspace.rollback:other:" + digest, OriginalOp: "workspace.apply:w:1:2:p", BundleDigest: digest})
	if err != nil {
		t.Fatal(err)
	}
	if err := Events()[EvRollbackCommitted](raw2); err == nil {
		t.Fatal("expected an error: op inconsistent with original_op+bundle_digest")
	}

	// A CONSISTENT payload must still be accepted.
	consistentOp := RollbackOperationID("workspace.apply:w:1:2:p", digest)
	raw3, err := json.Marshal(rollbackStartedPayload{Op: string(consistentOp), OriginalOp: "workspace.apply:w:1:2:p", BundleDigest: digest})
	if err != nil {
		t.Fatal(err)
	}
	if err := Events()[EvRollbackStarted](raw3); err != nil {
		t.Fatalf("expected a consistent payload to be accepted: %v", err)
	}
}
