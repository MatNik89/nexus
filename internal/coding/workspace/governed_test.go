//go:build linux

package workspace

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/MatNik89/nexus/internal/foundation/sealedstore"
	"github.com/MatNik89/nexus/internal/kernel/contracts"
	"github.com/MatNik89/nexus/internal/kernel/journal"
	"github.com/MatNik89/nexus/internal/kernel/s7"
	"github.com/MatNik89/nexus/internal/security/redact"
	"golang.org/x/sys/unix"
)

// testProfile is the fixed profile every testDurableJournal in this file
// is bound to — ApplyGoverned derives its own operation identity from
// j.Profile(), never a caller-supplied profile (round-2 fix), so tests
// use this ONE constant everywhere rather than repeating the literal.
const testProfile contracts.ProfileID = "work"

func testDurableJournal(t *testing.T) *journal.Journal {
	t.Helper()
	events := s7.Events()
	for n, v := range Events() {
		events[n] = v
	}
	j, err := journal.Open(filepath.Join(t.TempDir(), "j.db"), testProfile, redact.None{}, events, s7.NewProjection())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { j.Close() })
	return j
}

func testDurableGrants(t *testing.T, j *journal.Journal) *s7.Authority {
	t.Helper()
	a, err := s7.New(j, time.Now, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	return a
}

func journalEventTypes(t *testing.T, j *journal.Journal) map[string]int {
	t.Helper()
	got := map[string]int{}
	if err := j.Replay(0, func(ev journal.Event) error { got[ev.Envelope.EventType]++; return nil }); err != nil {
		t.Fatal(err)
	}
	return got
}

// applyStartedAttemptNumbers returns, in journal order, the AttemptNo
// every workspace.apply_started event carried.
func applyStartedAttemptNumbers(t *testing.T, j *journal.Journal) []int {
	t.Helper()
	var got []int
	if err := j.Replay(0, func(ev journal.Event) error {
		if ev.Envelope.EventType == EvApplyStarted {
			got = append(got, ev.Envelope.AttemptNo)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	return got
}

// testApplyOp recomputes the SAME exact-intent operation id ApplyGoverned
// itself derives, so tests can inspect S7's authoritative state without
// duplicating the derivation logic by hand (and silently drifting from
// it).
func testApplyOp(t *testing.T, rootFd int, planDigest string) contracts.OperationID {
	t.Helper()
	var st unix.Stat_t
	if err := unix.Fstat(rootFd, &st); err != nil {
		t.Fatal(err)
	}
	return ApplyOperationID(testProfile, uint64(st.Dev), st.Ino, planDigest)
}

// Detector: two calls with identical inputs derive the SAME operation id
// (retry/idempotency depends on this); any single differing input —
// profile, root identity, or plan digest — derives a DIFFERENT one
// (AGENTS.md's exact-intent rule: any payload/target change invalidates
// the grant, it never widens to cover something else).
func TestApplyOperationIDIsDeterministicAndExactIntent(t *testing.T) {
	base := ApplyOperationID("work", 1, 2, "digest-A")
	if got := ApplyOperationID("work", 1, 2, "digest-A"); got != base {
		t.Fatalf("identical inputs produced different ids: %q vs %q", base, got)
	}
	if got := ApplyOperationID("work", 1, 2, "digest-B"); got == base {
		t.Fatal("a different plan digest must produce a different operation id")
	}
	if got := ApplyOperationID("other-profile", 1, 2, "digest-A"); got == base {
		t.Fatal("a different profile must produce a different operation id")
	}
	if got := ApplyOperationID("work", 9, 2, "digest-A"); got == base {
		t.Fatal("a different root device must produce a different operation id")
	}
	if got := ApplyOperationID("work", 1, 9, "digest-A"); got == base {
		t.Fatal("a different root inode must produce a different operation id")
	}
}

// Detector (round-2 fix, finding 4): ApplyGoverned derives its profile
// from the JOURNAL's own bound profile, never a caller-supplied one — so
// two journals bound to different profiles, given the identical root and
// plan digest, must produce DIFFERENT S7 operations (no caller can lie
// about which profile an operation belongs to).
func TestApplyGovernedDerivesProfileFromJournalNotACallerParameter(t *testing.T) {
	root := t.TempDir()
	rootFd, err := OpenRoot(root)
	if err != nil {
		t.Fatal(err)
	}
	defer unix.Close(rootFd)
	store := newTestStore(t)

	jWork := testDurableJournal(t) // bound to "work"
	gWork := testDurableGrants(t, jWork)
	if _, err := ApplyGoverned(context.Background(), rootFd, store, []FileMutation{
		{BaseName: "a.go", After: []byte("package a\n"), Mode: 0o644},
	}, gWork, jWork, "plan-profile-1", "run-1"); err != nil {
		t.Fatalf("ApplyGoverned: %v", err)
	}
	opWork := testApplyOp(t, rootFd, "plan-profile-1")
	if _, ok := gWork.State(opWork); !ok {
		t.Fatal("expected the operation derived from the journal's own profile to exist")
	}
}

// Detector: a successful ApplyGoverned call actually consumes a REAL S7
// grant (workspace.apply_started paired atomically with S7's own durable
// STARTED, before any write) and lands the operation SUCCEEDED with a
// workspace.mutation_committed companion in the same batch as S7's own
// terminal transition — proving the bindDurable seam is wired to a real
// grant lifecycle, not a stub.
func TestApplyGovernedCommitsBothDurableEventsAndSucceedsOnSuccess(t *testing.T) {
	root := t.TempDir()
	rootFd, err := OpenRoot(root)
	if err != nil {
		t.Fatal(err)
	}
	defer unix.Close(rootFd)
	store := newTestStore(t)
	j := testDurableJournal(t)
	grants := testDurableGrants(t, j)

	res, err := ApplyGoverned(context.Background(), rootFd, store, []FileMutation{
		{BaseName: "a.go", After: []byte("package a\n"), Mode: 0o644},
	}, grants, j, "plan-digest-1", "run-1")
	if err != nil {
		t.Fatalf("ApplyGoverned: %v", err)
	}
	if !res.Committed {
		t.Fatal("expected Committed = true")
	}

	op := testApplyOp(t, rootFd, "plan-digest-1")
	state, ok := grants.State(op)
	if !ok || state != contracts.AttemptSucceeded {
		t.Fatalf("operation state = %v (ok=%v), want SUCCEEDED", state, ok)
	}

	types := journalEventTypes(t, j)
	if types[EvApplyStarted] != 1 {
		t.Fatalf("workspace.apply_started count = %d, want 1", types[EvApplyStarted])
	}
	if types[EvMutationCommitted] != 1 {
		t.Fatalf("workspace.mutation_committed count = %d, want 1", types[EvMutationCommitted])
	}
	if types[s7.EvAttemptStarted] != 1 {
		t.Fatalf("s7.attempt_started count = %d, want 1 (Consume must fire durably)", types[s7.EvAttemptStarted])
	}
}

// Detector: a precondition Apply refuses BEFORE ever calling bindDurable
// (a stale ExpectedBeforeDigest) must never reach Consume — the grant is
// never durably STARTED, no file is written, and the operation lands
// CANCELLED (never started), never FAILED (which would wrongly imply an
// attempt was actually made).
func TestApplyGovernedCancelsWhenPreconditionRefusesBeforeConsume(t *testing.T) {
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

	_, err = ApplyGoverned(context.Background(), rootFd, store, []FileMutation{
		{BaseName: "a.go", After: []byte("package new\n"), Mode: 0o644,
			ExpectedBeforeDigest: sha256Hex([]byte("package DIFFERENT\n"))},
	}, grants, j, "plan-digest-2", "run-1")
	if err == nil {
		t.Fatal("expected an error: the precondition does not match the on-disk content")
	}

	op := testApplyOp(t, rootFd, "plan-digest-2")
	state, ok := grants.State(op)
	if !ok || state != contracts.AttemptCancelled {
		t.Fatalf("operation state = %v (ok=%v), want CANCELLED (never started)", state, ok)
	}
	types := journalEventTypes(t, j)
	if types[EvApplyStarted] != 0 {
		t.Fatalf("workspace.apply_started count = %d, want 0 (Consume must never fire)", types[EvApplyStarted])
	}
	got, readErr := os.ReadFile(filepath.Join(root, "a.go"))
	if readErr != nil {
		t.Fatal(readErr)
	}
	if string(got) != "package old\n" {
		t.Fatalf("a.go content = %q, want untouched %q", got, "package old\n")
	}
}

// Detector: a mid-transaction write failure AFTER Consume already
// succeeded, whose rollback is fully VERIFIED, but whose retry budget is
// exhausted (this test pins PolicyWorkspaceApply to MaxAttempts=1) must
// land the operation FAILED — not CANCELLED, which would wrongly imply
// nothing physical ever happened — while EvMutationCommitted must never
// appear. Reuses transaction_test.go's own symlink-drift technique via
// the beforeApplyWritesFile seam, on the SECOND of two files so the
// first file's write (and therefore Consume, which fires before any
// write) has already happened.
func TestApplyGovernedReportsFailedTerminalWhenRetriesExhaustAfterVerifiedRollback(t *testing.T) {
	origPolicy := PolicyWorkspaceApply
	PolicyWorkspaceApply = s7.Policy{EffectClass: contracts.EffectIrreversible, MaxAttempts: 1, Durable: true}
	defer func() { PolicyWorkspaceApply = origPolicy }()

	root := t.TempDir()
	sub := filepath.Join(root, "sub")
	if err := os.Mkdir(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	defer os.Chmod(sub, 0o755) // TempDir cleanup needs this restored
	if err := os.WriteFile(filepath.Join(root, "a.go"), []byte("package old_a\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(sub, "b.go"), []byte("package old_b\n"), 0o644); err != nil {
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

	// A genuine infra-style failure (EACCES creating b.go's temp file,
	// its OWN content/identity/mode never touched) — NOT a precondition
	// mismatch — so both a.go's rollback AND b.go's re-verification (its
	// directory only lost WRITE, not READ/EXECUTE) succeed cleanly,
	// proving the "fully verified, safe to retry" branch is reachable via
	// something OTHER than the far more common drift-triggered failure
	// (which the sibling test below proves correctly classifies
	// Incomplete instead).
	orig := beforeApplyWritesFile
	defer func() { beforeApplyWritesFile = orig }()
	fired := false
	beforeApplyWritesFile = func(baseName string) {
		if baseName == "b.go" && !fired {
			fired = true
			if err := os.Chmod(sub, 0o555); err != nil {
				t.Fatal(err)
			}
		}
	}

	_, err = ApplyGoverned(context.Background(), rootFd, store, []FileMutation{
		{BaseName: "a.go", After: []byte("package new_a\n"), Mode: 0o644},
		{RelDir: "sub", BaseName: "b.go", After: []byte("package new_b\n"), Mode: 0o644},
	}, grants, j, "plan-digest-3", "run-1")
	if err == nil {
		t.Fatal("expected an error: b.go's directory lost write permission mid-transaction")
	}
	if errors.Is(err, ErrRollbackIncomplete) {
		t.Fatal("a.go's own rollback was clean and b.go was never touched — this must NOT be classified incomplete")
	}
	if !fired {
		t.Fatal("test setup bug: the injection hook never fired")
	}

	op := testApplyOp(t, rootFd, "plan-digest-3")
	state, ok := grants.State(op)
	if !ok || state != contracts.AttemptFailed {
		t.Fatalf("operation state = %v (ok=%v), want FAILED (a real, verified-clean attempt started, then its 1-attempt budget was exhausted)", state, ok)
	}
	types := journalEventTypes(t, j)
	if types[EvApplyStarted] != 1 {
		t.Fatalf("workspace.apply_started count = %d, want 1 (Consume DID fire before the write phase)", types[EvApplyStarted])
	}
	if types[EvMutationCommitted] != 0 {
		t.Fatalf("workspace.mutation_committed count = %d, want 0 (not every file was written)", types[EvMutationCommitted])
	}
	got, readErr := os.ReadFile(filepath.Join(root, "a.go"))
	if readErr != nil {
		t.Fatal(readErr)
	}
	if string(got) != "package old_a\n" {
		t.Fatalf("a.go content = %q, want rolled back to %q", got, "package old_a\n")
	}
}

// Detector (code-review finding, codex round 3 HIGH #2, the actual bug
// this piece's ORIGINAL "real rollback" test failed to catch): the FAR
// more common failure mode — a mutation's own precondition no longer
// matches, e.g. content drift — leaves THAT file itself not-all-before,
// even after a.go's own OWN successful write is cleanly rolled back. The
// whole operation must be Unknown, never proposed retryable, regardless
// of how clean any INDIVIDUAL file's own rollback was.
func TestApplyGovernedReportsUnknownWhenAnUnreachedFileHasDrifted(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "a.go"), []byte("package old_a\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "b.go"), []byte("package old_b\n"), 0o644); err != nil {
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

	orig := beforeApplyWritesFile
	defer func() { beforeApplyWritesFile = orig }()
	fired := false
	beforeApplyWritesFile = func(baseName string) {
		if baseName == "b.go" && !fired {
			fired = true
			// In-place content drift — same inode, different bytes —
			// b.go's own write refuses on its content-digest precondition.
			if err := os.WriteFile(filepath.Join(root, "b.go"), []byte("package DRIFTED\n"), 0o644); err != nil {
				t.Fatal(err)
			}
		}
	}

	_, err = ApplyGoverned(context.Background(), rootFd, store, []FileMutation{
		{BaseName: "a.go", After: []byte("package new_a\n"), Mode: 0o644},
		{BaseName: "b.go", After: []byte("package new_b\n"), Mode: 0o644},
	}, grants, j, "plan-digest-drift-1", "run-1")
	if err == nil {
		t.Fatal("expected an error: b.go's content drifted mid-transaction")
	}
	if !fired {
		t.Fatal("test setup bug: the injection hook never fired")
	}
	if !errors.Is(err, ErrRollbackIncomplete) {
		t.Fatalf("expected errors.Is(err, ErrRollbackIncomplete) — b.go no longer matches its captured before-state — got: %v", err)
	}

	op := testApplyOp(t, rootFd, "plan-digest-drift-1")
	state, ok := grants.State(op)
	if !ok || state != contracts.AttemptUnknown {
		t.Fatalf("operation state = %v (ok=%v), want UNKNOWN (an unreached file drifted — never safe to propose retryable)", state, ok)
	}
	got, readErr := os.ReadFile(filepath.Join(root, "a.go"))
	if readErr != nil {
		t.Fatal(readErr)
	}
	if string(got) != "package old_a\n" {
		t.Fatalf("a.go content = %q, want rolled back to %q", got, "package old_a\n")
	}
}

// Detector: an operation that just landed FAILED_RETRYABLE, called again
// before its own backoff has elapsed, must return ErrRetryNotDue — never
// silently spin or block (ApplyGoverned makes exactly ONE attempt per
// call, see its own doc comment).
func TestApplyGovernedReturnsErrRetryNotDueBeforeBackoffElapses(t *testing.T) {
	// The package default is MaxAttempts=1 (this piece does not claim
	// retry support by default — see PolicyWorkspaceApply's own doc
	// comment); ErrNotDue can only occur for an operation with retry
	// budget left, so this test explicitly opts into that, exactly as a
	// real caller building its own retry driver would.
	origPolicy := PolicyWorkspaceApply
	PolicyWorkspaceApply = s7.Policy{EffectClass: contracts.EffectIrreversible, MaxAttempts: 3, Durable: true,
		Backoff: s7.BackoffPolicy{Base: time.Hour}, RetryableCodes: []string{s7.CodeMutationRolledBack}}
	defer func() { PolicyWorkspaceApply = origPolicy }()

	root := t.TempDir()
	rootFd, err := OpenRoot(root)
	if err != nil {
		t.Fatal(err)
	}
	defer unix.Close(rootFd)
	store := newTestStore(t)
	j := testDurableJournal(t)
	grants := testDurableGrants(t, j)
	mutations := []FileMutation{{BaseName: "a.go", After: []byte("package a\n"), Mode: 0o644}}

	origApply := applyFn
	defer func() { applyFn = origApply }()
	applyFn = func(ctx context.Context, rootFd int, store *sealedstore.Store, mutations []FileMutation, bindDurable func(string) error) (Result, error) {
		if err := bindDurable(validFakeDigest); err != nil {
			return Result{}, err
		}
		return Result{}, fmt.Errorf("simulated transient failure, nothing physically written yet")
	}

	if _, err := ApplyGoverned(context.Background(), rootFd, store, mutations, grants, j, "plan-notdue-1", "run-1"); err == nil {
		t.Fatal("expected the first attempt to fail")
	}
	_, err = ApplyGoverned(context.Background(), rootFd, store, mutations, grants, j, "plan-notdue-1", "run-1")
	if !errors.Is(err, ErrRetryNotDue) {
		t.Fatalf("expected errors.Is(err, ErrRetryNotDue) on the immediate retry, got: %v", err)
	}
}

// Detector (invariant 2's state-transition binding, Unknown half): a
// failure whose rollback Apply's own error chain marks INCOMPLETE
// (transaction.ErrRollbackIncomplete present — the workspace may be in a
// mixed before/after state) must land the operation UNKNOWN, never
// retried blind, and never re-grantable by an ordinary Next — only
// reconciliation may exit it (not built in this piece, see the package
// doc). Uses the applyFn seam to force this specific, hard-to-reach
// filesystem state deterministically.
func TestApplyGovernedReportsUnknownWhenRollbackIsIncomplete(t *testing.T) {
	root := t.TempDir()
	rootFd, err := OpenRoot(root)
	if err != nil {
		t.Fatal(err)
	}
	defer unix.Close(rootFd)
	store := newTestStore(t)
	j := testDurableJournal(t)
	grants := testDurableGrants(t, j)
	mutations := []FileMutation{{BaseName: "a.go", After: []byte("package a\n"), Mode: 0o644}}

	origApply := applyFn
	defer func() { applyFn = origApply }()
	applyFn = func(ctx context.Context, rootFd int, store *sealedstore.Store, mutations []FileMutation, bindDurable func(string) error) (Result, error) {
		if err := bindDurable(validFakeDigest); err != nil {
			return Result{}, err
		}
		return Result{}, fmt.Errorf("simulated write failure: %w", errors.Join(errors.New("original failure"), ErrRollbackIncomplete))
	}

	_, err = ApplyGoverned(context.Background(), rootFd, store, mutations, grants, j, "plan-incomplete-1", "run-1")
	if err == nil {
		t.Fatal("expected an error")
	}
	if !errors.Is(err, ErrRollbackIncomplete) {
		t.Fatalf("expected the returned error to still carry ErrRollbackIncomplete, got: %v", err)
	}

	op := testApplyOp(t, rootFd, "plan-incomplete-1")
	state, ok := grants.State(op)
	if !ok || state != contracts.AttemptUnknown {
		t.Fatalf("operation state = %v (ok=%v), want UNKNOWN (incomplete rollback must never be retried blind)", state, ok)
	}

	if _, err2 := ApplyGoverned(context.Background(), rootFd, store, mutations, grants, j, "plan-incomplete-1", "run-1"); err2 == nil {
		t.Fatal("expected a second call for the SAME (now UNKNOWN) operation to be refused, never silently re-granted")
	}
}

// validFakeDigest is a syntactically valid (64 lowercase hex chars)
// stand-in bundle digest for tests that don't exercise a real
// sealedstore.Put — Events()'s validators now require this shape (round-2
// fix, finding 5): a test literal like "fake-digest-1" would itself be
// rejected at journal admission.
const validFakeDigest = "0000000000000000000000000000000000000000000000000000000000000000"

// Detector (round-2 fix, finding 5): the closed workspace.* validators
// reject a bundle digest that is not a well-formed sha256 hex string —
// an artifact reference this codebase's future GC/recovery sweep could
// never resolve must never be admitted as if it were real.
func TestEventsRejectsMalformedBundleDigest(t *testing.T) {
	v := Events()[EvApplyStarted]
	bad, err := json.Marshal(applyStartedPayload{Op: "op-x", BundleDigest: "not-a-real-digest"})
	if err != nil {
		t.Fatal(err)
	}
	if err := v(bad); err == nil {
		t.Fatal("expected the validator to reject a non-sha256-hex bundle_digest")
	}
	good, err := json.Marshal(applyStartedPayload{Op: "op-x", BundleDigest: validFakeDigest})
	if err != nil {
		t.Fatal(err)
	}
	if err := v(good); err != nil {
		t.Fatalf("expected a valid sha256-hex digest to be accepted: %v", err)
	}
}

// Detector (the plan's own explicitly required RED-capable detector,
// PLAN-CODING-TRIO.md invariant 2: "injecting a fault into EITHER half of
// the paired companion batch... must result in ZERO filesystem writes"):
// Consume commits S7's own s7.attempt_started TOGETHER WITH
// workspace.apply_started in ONE journal batch. A fault on EITHER event
// type name aborts the WHOLE batch — Consume fails, bindDurable returns
// that error to Apply BEFORE Apply has written anything (bindDurable
// fires before the write loop), and ApplyGoverned's own
// never-consumed branch lands the operation Cancelled, not Failed.
func TestApplyGovernedFaultInEitherHalfOfConsumeBatchWritesZeroFiles(t *testing.T) {
	for _, faultOn := range []string{EvApplyStarted, s7.EvAttemptStarted} {
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
			grants := testDurableGrants(t, j)
			grants.SetAppendFault(func(types []string) error {
				for _, ty := range types {
					if ty == faultOn {
						return fmt.Errorf("injected fault on %s", faultOn)
					}
				}
				return nil
			})

			_, err = ApplyGoverned(context.Background(), rootFd, store, []FileMutation{
				{BaseName: "a.go", After: []byte("package new\n"), Mode: 0o644},
			}, grants, j, "plan-fault-1", "run-1")
			if err == nil {
				t.Fatal("expected an error: the durable Consume batch was faulted")
			}

			op := testApplyOp(t, rootFd, "plan-fault-1")
			state, ok := grants.State(op)
			if !ok || state != contracts.AttemptCancelled {
				t.Fatalf("operation state = %v (ok=%v), want CANCELLED (Consume never durably completed)", state, ok)
			}
			got, readErr := os.ReadFile(filepath.Join(root, "a.go"))
			if readErr != nil {
				t.Fatal(readErr)
			}
			if string(got) != "package old\n" {
				t.Fatalf("a.go content = %q, want untouched %q (zero writes)", got, "package old\n")
			}
		})
	}
}

// Detector (the CENTRAL fix for codex round-2 HIGH #1): a retry after a
// REAL, physical rollback — not a synthetic seam — must actually succeed.
// b.go's DIRECTORY loses write permission mid-transaction (a genuine
// infra-style failure — NOT a precondition mismatch, see the sibling
// verified-terminal test's own comment for why that distinction matters
// after codex round 3's finding 2 fix), so the whole operation classifies
// verified-clean, not Incomplete. a.go gets ACTUALLY written then
// ACTUALLY rolled back (minting a NEW inode — the exact mechanism that
// made the ORIGINAL retry design impossible: reusing the stale pre-Apply
// ExpectedBeforeDev/Ino would refuse the retry's own precondition check).
// The transient condition is then resolved (permission restored),
// mutations are rebuilt via UpdateMutationsAfterRollback using
// res1.RolledBack, and the SAME operation is retried — proving it
// actually reaches SUCCEEDED, not just that ApplyGoverned's internal
// branching compiles.
func TestApplyGovernedRetryAfterRealVerifiedRollbackSucceedsWithUpdatedMutations(t *testing.T) {
	origPolicy := PolicyWorkspaceApply
	PolicyWorkspaceApply = s7.Policy{EffectClass: contracts.EffectIrreversible, MaxAttempts: 2, Durable: true,
		RetryableCodes: []string{s7.CodeMutationRolledBack}} // zero-value Backoff: immediately due, no sleep needed
	defer func() { PolicyWorkspaceApply = origPolicy }()

	root := t.TempDir()
	subB := filepath.Join(root, "subB")
	if err := os.Mkdir(subB, 0o755); err != nil {
		t.Fatal(err)
	}
	defer os.Chmod(subB, 0o755) // TempDir cleanup needs this restored
	if err := os.WriteFile(filepath.Join(root, "a.go"), []byte("package old_a\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(subB, "b.go"), []byte("package old_b\n"), 0o644); err != nil {
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

	// b.go's own file is NEVER touched (only its directory's permission
	// changes) — its identity stays stable throughout this test: only
	// a.go's identity is expected to move (by its own real rollback),
	// which is exactly what this test isolates and proves
	// UpdateMutationsAfterRollback correctly repairs.
	orig := beforeApplyWritesFile
	defer func() { beforeApplyWritesFile = orig }()
	fired := false
	beforeApplyWritesFile = func(baseName string) {
		if baseName == "b.go" && !fired {
			fired = true
			if err := os.Chmod(subB, 0o555); err != nil {
				t.Fatal(err)
			}
		}
	}

	// Preconditions are captured from the REAL on-disk state, exactly as
	// symedit.planMutations does for a real RenameSymbol Plan (CheckExpectedIdentity:
	// true is load-bearing here — the whole point of this test is proving a
	// retry survives an identity check against a REAL post-rollback inode).
	aDev, aIno, err := StatBeneath(rootFd, "a.go")
	if err != nil {
		t.Fatal(err)
	}
	bDirFd, err := WalkDirBeneath(rootFd, "subB")
	if err != nil {
		t.Fatal(err)
	}
	bDev, bIno, err := StatBeneath(bDirFd, "b.go")
	unix.Close(bDirFd)
	if err != nil {
		t.Fatal(err)
	}
	mutations := []FileMutation{
		{BaseName: "a.go", After: []byte("package new_a\n"), Mode: 0o644,
			ExpectedBeforeDigest: sha256Hex([]byte("package old_a\n")), ExpectedBeforeDev: aDev, ExpectedBeforeIno: aIno,
			CheckExpectedIdentity: true, ExpectedBeforeMode: 0o644, CheckExpectedMode: true},
		{RelDir: "subB", BaseName: "b.go", After: []byte("package new_b\n"), Mode: 0o644,
			ExpectedBeforeDigest: sha256Hex([]byte("package old_b\n")), ExpectedBeforeDev: bDev, ExpectedBeforeIno: bIno,
			CheckExpectedIdentity: true, ExpectedBeforeMode: 0o644, CheckExpectedMode: true},
	}

	res1, err1 := ApplyGoverned(context.Background(), rootFd, store, mutations, grants, j, "plan-real-retry-1", "run-1")
	if err1 == nil {
		t.Fatal("expected the first attempt to fail: b.go's directory lost write permission mid-transaction")
	}
	if errors.Is(err1, ErrRollbackIncomplete) {
		t.Fatal("a.go's own rollback was clean and b.go was never touched — this must NOT be classified incomplete")
	}
	if !fired {
		t.Fatal("test setup bug: the injection hook never fired")
	}
	if len(res1.RolledBack) != 1 || res1.RolledBack[0].BaseName != "a.go" {
		t.Fatalf("expected exactly 1 rolled-back file (a.go), got %+v", res1.RolledBack)
	}

	// The transient condition is gone: restore write permission.
	if err := os.Chmod(subB, 0o755); err != nil {
		t.Fatal(err)
	}

	retryMutations := UpdateMutationsAfterRollback(mutations, res1.RolledBack)
	if retryMutations[0].ExpectedBeforeDev != res1.RolledBack[0].Dev || retryMutations[0].ExpectedBeforeIno != res1.RolledBack[0].Ino {
		t.Fatal("UpdateMutationsAfterRollback did not update a.go's precondition to its post-rollback identity")
	}

	res2, err2 := ApplyGoverned(context.Background(), rootFd, store, retryMutations, grants, j, "plan-real-retry-1", "run-1")
	if err2 != nil {
		t.Fatalf("retry with updated mutations should succeed: %v", err2)
	}
	if !res2.Committed {
		t.Fatal("expected Committed = true on the retry")
	}

	op := testApplyOp(t, rootFd, "plan-real-retry-1")
	state, ok := grants.State(op)
	if !ok || state != contracts.AttemptSucceeded {
		t.Fatalf("operation state = %v (ok=%v), want SUCCEEDED (the SAME operation, retried)", state, ok)
	}
	if got := grants.Attempts(op); got != 2 {
		t.Fatalf("consumed attempts = %d, want 2 (the SAME operation, never a fresh one)", got)
	}
	if got := applyStartedAttemptNumbers(t, j); len(got) != 2 || got[0] != 1 || got[1] != 2 {
		t.Fatalf("apply_started attempt numbers = %v, want [1 2]", got)
	}

	gotA, readErr := os.ReadFile(filepath.Join(root, "a.go"))
	if readErr != nil {
		t.Fatal(readErr)
	}
	if string(gotA) != "package new_a\n" {
		t.Fatalf("a.go = %q, want %q", gotA, "package new_a\n")
	}
	gotB, readErr := os.ReadFile(filepath.Join(subB, "b.go"))
	if readErr != nil {
		t.Fatal(readErr)
	}
	if string(gotB) != "package new_b\n" {
		t.Fatalf("b.go = %q, want %q", gotB, "package new_b\n")
	}
}

// Detector (code-review finding, codex round 3 HIGH #4): a consumed
// attempt that stops because ITS OWN ctx was cancelled — even though the
// resulting rollback is fully verified clean — must land the operation
// Cancelled, never a code-carrying FAILED/FAILED_RETRYABLE landing. This
// is the SAME distinction runner.Run's own selfDeadlineCancelled makes
// for an identical reason: the attempt stopped because it was told to,
// not because it failed on its own terms.
func TestApplyGovernedCancelsOperationWhenCtxCancelledMidTransaction(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "a.go"), []byte("package old_a\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "b.go"), []byte("package old_b\n"), 0o644); err != nil {
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

	ctx, cancel := context.WithCancel(context.Background())
	orig := beforeApplyWritesFile
	defer func() { beforeApplyWritesFile = orig }()
	beforeApplyWritesFile = func(baseName string) {
		if baseName == "a.go" {
			cancel() // takes effect before b.go's OWN ctx check, AFTER a.go's
		}
	}

	_, err = ApplyGoverned(ctx, rootFd, store, []FileMutation{
		{BaseName: "a.go", After: []byte("package new_a\n"), Mode: 0o644},
		{BaseName: "b.go", After: []byte("package new_b\n"), Mode: 0o644},
	}, grants, j, "plan-cancel-1", "run-1")
	if err == nil {
		t.Fatal("expected an error: ctx was cancelled mid-transaction")
	}
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected errors.Is(err, context.Canceled), got: %v", err)
	}

	op := testApplyOp(t, rootFd, "plan-cancel-1")
	state, ok := grants.State(op)
	if !ok || state != contracts.AttemptCancelled {
		t.Fatalf("operation state = %v (ok=%v), want CANCELLED (stopped because ctx said to, not because it failed)", state, ok)
	}
	got, readErr := os.ReadFile(filepath.Join(root, "a.go"))
	if readErr != nil {
		t.Fatal(readErr)
	}
	if string(got) != "package old_a\n" {
		t.Fatalf("a.go content = %q, want rolled back to %q", got, "package old_a\n")
	}
}

// Detector (code-review finding, codex round 4 MEDIUM #4): Consume can
// succeed (a durable STARTED record commits) and THEN
// grants.AttemptContext itself can fail — e.g. the operation's own
// deadline expired in the gap between Next issuing the grant and
// Consume finishing — before Apply's write loop is ever entered.
// Reporting this as CodeMutationRolledBack ("verified rollback, safe to
// retry") would be a false narrative: nothing was ever written, so
// nothing was ever rolled back. It must land CANCELLED instead, the same
// as a ctx cancellation. Uses the deriveAttemptContext seam to force
// this deterministically (a real clock race here would need a
// microsecond-precision window with no test-visible hook).
func TestApplyGovernedCancelsWhenAttemptContextFailsAfterConsume(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "a.go"), []byte("package old_a\n"), 0o644); err != nil {
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

	origDerive := deriveAttemptContext
	defer func() { deriveAttemptContext = origDerive }()
	deriveAttemptContext = func(g *s7.Authority, ctx context.Context, op contracts.OperationID) (context.Context, context.CancelFunc, error) {
		return nil, nil, fmt.Errorf("simulated: attempt deadline already passed")
	}

	_, err = ApplyGoverned(context.Background(), rootFd, store, []FileMutation{
		{BaseName: "a.go", After: []byte("package new_a\n"), Mode: 0o644},
	}, grants, j, "plan-attemptctx-1", "run-1")
	if err == nil {
		t.Fatal("expected an error: AttemptContext failed after Consume succeeded")
	}

	op := testApplyOp(t, rootFd, "plan-attemptctx-1")
	state, ok := grants.State(op)
	if !ok || state != contracts.AttemptCancelled {
		t.Fatalf("operation state = %v (ok=%v), want CANCELLED (nothing was ever written, this is not a rollback)", state, ok)
	}
	types := journalEventTypes(t, j)
	if types[EvApplyStarted] != 1 {
		t.Fatalf("workspace.apply_started count = %d, want 1 (Consume DID durably complete)", types[EvApplyStarted])
	}
	got, readErr := os.ReadFile(filepath.Join(root, "a.go"))
	if readErr != nil {
		t.Fatal(readErr)
	}
	if string(got) != "package old_a\n" {
		t.Fatalf("a.go content = %q, want untouched %q (Apply's write loop was never entered)", got, "package old_a\n")
	}
}

// Detector (code-review finding, codex round 5 HIGH #1): S7's own Cancel
// preserves the PRIOR report's code as rec.lastCode when building its
// companion — so cancelling an operation that already landed
// FAILED_RETRYABLE/CodeMutationRolledBack once (a legitimate step in a
// caller-driven retry, e.g. the retry's OWN precondition turns out stale
// too) produces (attempt_no>=1, Cancelled, CodeMutationRolledBack).
// Reproduces exactly that sequence: attempt 1 fails with a genuinely
// verified rollback (the chmod technique, real infra failure) and lands
// FAILED_RETRYABLE; attempt 2 is issued a grant but DELIBERATELY reuses
// the STALE (pre-rollback) mutations — the same real bug the sibling
// "real retry" test proves UpdateMutationsAfterRollback fixes — so it
// refuses on its own precondition before ever reaching Consume, landing
// the operation Cancelled via the exact real S7-generated narrative
// above. Before the round-5 fix this durable Cancel append itself failed
// validation, stranding the operation AUTHORIZED forever.
func TestApplyGovernedCancelsRetryEligibleOperationCarryingPriorRetryCode(t *testing.T) {
	origPolicy := PolicyWorkspaceApply
	PolicyWorkspaceApply = s7.Policy{EffectClass: contracts.EffectIrreversible, MaxAttempts: 2, Durable: true,
		RetryableCodes: []string{s7.CodeMutationRolledBack}} // zero-value Backoff: immediately due, no sleep needed
	defer func() { PolicyWorkspaceApply = origPolicy }()

	root := t.TempDir()
	subB := filepath.Join(root, "subB")
	if err := os.Mkdir(subB, 0o755); err != nil {
		t.Fatal(err)
	}
	defer os.Chmod(subB, 0o755) // TempDir cleanup needs this restored
	if err := os.WriteFile(filepath.Join(root, "a.go"), []byte("package old_a\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(subB, "b.go"), []byte("package old_b\n"), 0o644); err != nil {
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

	orig := beforeApplyWritesFile
	defer func() { beforeApplyWritesFile = orig }()
	fired := false
	beforeApplyWritesFile = func(baseName string) {
		if baseName == "b.go" && !fired {
			fired = true
			if err := os.Chmod(subB, 0o555); err != nil {
				t.Fatal(err)
			}
		}
	}

	aDev, aIno, err := StatBeneath(rootFd, "a.go")
	if err != nil {
		t.Fatal(err)
	}
	bDirFd, err := WalkDirBeneath(rootFd, "subB")
	if err != nil {
		t.Fatal(err)
	}
	bDev, bIno, err := StatBeneath(bDirFd, "b.go")
	unix.Close(bDirFd)
	if err != nil {
		t.Fatal(err)
	}
	mutations := []FileMutation{
		{BaseName: "a.go", After: []byte("package new_a\n"), Mode: 0o644,
			ExpectedBeforeDigest: sha256Hex([]byte("package old_a\n")), ExpectedBeforeDev: aDev, ExpectedBeforeIno: aIno,
			CheckExpectedIdentity: true, ExpectedBeforeMode: 0o644, CheckExpectedMode: true},
		{RelDir: "subB", BaseName: "b.go", After: []byte("package new_b\n"), Mode: 0o644,
			ExpectedBeforeDigest: sha256Hex([]byte("package old_b\n")), ExpectedBeforeDev: bDev, ExpectedBeforeIno: bIno,
			CheckExpectedIdentity: true, ExpectedBeforeMode: 0o644, CheckExpectedMode: true},
	}

	_, err1 := ApplyGoverned(context.Background(), rootFd, store, mutations, grants, j, "plan-cancel-retry-1", "run-1")
	if err1 == nil {
		t.Fatal("expected the first attempt to fail: b.go's directory lost write permission mid-transaction")
	}
	if errors.Is(err1, ErrRollbackIncomplete) {
		t.Fatal("a.go's own rollback was clean and b.go was never touched — this must NOT be classified incomplete")
	}
	if !fired {
		t.Fatal("test setup bug: the injection hook never fired")
	}

	op := testApplyOp(t, rootFd, "plan-cancel-retry-1")
	stateAfter1, ok := grants.State(op)
	if !ok || stateAfter1 != contracts.AttemptFailedRetryable {
		t.Fatalf("operation state after attempt 1 = %v (ok=%v), want FAILED_RETRYABLE", stateAfter1, ok)
	}
	if err := os.Chmod(subB, 0o755); err != nil {
		t.Fatal(err) // permission restored, but mutations below are deliberately left STALE
	}

	// Deliberately reuse the SAME stale mutations (no UpdateMutationsAfterRollback)
	// — attempt 2 must refuse on a.go's now-stale precondition BEFORE Consume.
	_, err2 := ApplyGoverned(context.Background(), rootFd, store, mutations, grants, j, "plan-cancel-retry-1", "run-1")
	if err2 == nil {
		t.Fatal("expected attempt 2 to refuse: a.go's precondition is stale after attempt 1's real rollback")
	}

	stateAfter2, ok := grants.State(op)
	if !ok || stateAfter2 != contracts.AttemptCancelled {
		t.Fatalf("operation state after attempt 2 = %v (ok=%v), want CANCELLED (not stuck AUTHORIZED)", stateAfter2, ok)
	}
}
