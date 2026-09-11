//go:build linux

package workspace

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/MatNik89/nexus/internal/foundation/sealedstore"
	"github.com/MatNik89/nexus/internal/kernel/contracts"
	"github.com/MatNik89/nexus/internal/kernel/journal"
	"github.com/MatNik89/nexus/internal/kernel/s7"
	"golang.org/x/sys/unix"
)

// crashApply simulates a REAL crash immediately after S7's durable
// Consume lands: it runs Begin/Next/Consume through the SAME primitives
// ApplyGoverned itself uses, invokes the real apply() (a real sealed
// bundle is written, real files may or may not be mutated depending on
// ctx/mutations), then deliberately returns WITHOUT ever calling
// Report or Cancel — exactly what a process death between Consume and
// Report leaves behind on disk and in the journal. A later
// testDurableGrants(t, j) call on the SAME journal rehydrates this
// operation to UNKNOWN via s7's own rehydrate(), matching a real
// restart — this is not a synthetic shortcut, it is the same "reopen a
// fresh Authority on the same journal" pattern s7's own
// TestDurableRestartPreservesCapAndBackoff uses.
func crashApply(t *testing.T, ctx context.Context, rootFd int, store *sealedstore.Store, j *journal.Journal, grants *s7.Authority, mutations []FileMutation, planDigest string) (contracts.OperationID, string) {
	t.Helper()
	var st unix.Stat_t
	if err := unix.Fstat(rootFd, &st); err != nil {
		t.Fatal(err)
	}
	rootDev, rootIno := uint64(st.Dev), st.Ino
	op := ApplyOperationID(testProfile, rootDev, rootIno, planDigest)
	target := ApplyTargetID(testProfile, rootDev, rootIno)
	if err := grants.Begin(op, target, PolicyWorkspaceApply); err != nil {
		t.Fatal(err)
	}
	attemptNo := grants.Attempts(op)
	failedBuild := func(l s7.Landing) s7.Companion {
		return s7.Companion{Key: op, Params: envelope(j, op, "run-crash", attemptNo, EvApplyFailed,
			applyFailedPayload{Op: string(op), AttemptNo: attemptNo, Landing: int(l.Kind), Code: l.Code})}
	}
	grant, err := grants.Next(op, failedBuild)
	if err != nil {
		t.Fatal(err)
	}
	consumed := false
	bundleDigest := ""
	bindDurable := func(digest string) error {
		companion := s7.Companion{Key: op, Params: envelope(j, op, "run-crash", grant.AttemptNo, EvApplyStarted,
			applyStartedPayload{Op: string(op), BundleDigest: digest})}
		if err := grants.Consume(grant, companion); err != nil {
			return err
		}
		consumed, bundleDigest = true, digest
		return nil
	}
	_, _ = apply(ctx, rootFd, store, mutations, bindDurable) // error (if any) discarded: a real crash sees no return value either
	if !consumed {
		t.Fatal("crashApply: bindDurable never fired — Consume did not land durably, this does not simulate a real crash")
	}
	return op, bundleDigest
}

// Detector: a NEVER_STARTED crash-orphan (durable Consume landed, sealed
// bundle written, but the write loop never touched a file because the
// context was already cancelled) is found and classified NEVER_STARTED —
// and, per this piece's own narrowed scope (round 1 HIGH #1: reconciling
// it under the default single-attempt policy would terminalize it
// permanently), left completely untouched in S7.
func TestRestartScanReportsNeverStartedWithoutActing(t *testing.T) {
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
	op, _ := crashApply(t, cctx, rootFd, store, j, grants, []FileMutation{
		{BaseName: "a.go", After: []byte("package a\n"), Mode: 0o644},
	}, "plan-digest-1")

	grants2 := testDurableGrants(t, j) // "restart"
	findings, err := RestartScan(rootFd, j, grants2, store)
	if err != nil {
		t.Fatal(err)
	}
	if len(findings) != 1 {
		t.Fatalf("findings = %d, want 1", len(findings))
	}
	f := findings[0]
	if f.Op != op {
		t.Fatalf("op = %q, want %q", f.Op, op)
	}
	if f.State != TransactionNeverStarted {
		t.Fatalf("state = %v, want NEVER_STARTED", f.State)
	}
	state, ok := grants2.State(op)
	if !ok || state != contracts.AttemptUnknown {
		t.Fatalf("state after scan = %v (ok=%v), want UNKNOWN (untouched — this piece must never act)", state, ok)
	}
}

// Detector: a MID_CRASH crash-orphan (every file actually written, but
// no workspace.mutation_committed ever landed) is found and reported,
// but RestartScan must NEVER act on it — S7 state left exactly UNKNOWN —
// because the governed rollback this case requires
// (PolicyWorkspaceRollback) does not exist yet.
func TestRestartScanReportsMidCrashWithoutActing(t *testing.T) {
	root := t.TempDir()
	rootFd, err := OpenRoot(root)
	if err != nil {
		t.Fatal(err)
	}
	defer unix.Close(rootFd)
	store := newTestStore(t)
	j := testDurableJournal(t)
	grants := testDurableGrants(t, j)

	op, _ := crashApply(t, context.Background(), rootFd, store, j, grants, []FileMutation{
		{BaseName: "a.go", After: []byte("package a\n"), Mode: 0o644},
	}, "plan-digest-1")
	if got, err := os.ReadFile(filepath.Join(root, "a.go")); err != nil || string(got) != "package a\n" {
		t.Fatalf("expected the file to actually be written before the simulated crash: got=%q err=%v", got, err)
	}
	before := journalEventTypes(t, j)

	grants2 := testDurableGrants(t, j) // "restart"
	findings, err := RestartScan(rootFd, j, grants2, store)
	if err != nil {
		t.Fatal(err)
	}
	if len(findings) != 1 {
		t.Fatalf("findings = %d, want 1", len(findings))
	}
	f := findings[0]
	if f.State != TransactionMidCrash {
		t.Fatalf("state = %v, want MID_CRASH", f.State)
	}

	after := journalEventTypes(t, j)
	if after[s7.EvOperationReconcile] != before[s7.EvOperationReconcile] {
		t.Fatal("RestartScan appended a reconcile event for a MID_CRASH finding — must never touch S7 state for this case")
	}
	state, ok := grants2.State(op)
	if !ok || state != contracts.AttemptUnknown {
		t.Fatalf("state after scan = %v (ok=%v), want UNKNOWN (untouched)", state, ok)
	}
}

// Detector: a file mutated by something else between the crash and the
// restart scan classifies FOREIGN and is reported without any S7 action
// — manual reconciliation only, never a guess.
func TestRestartScanReportsForeignWithoutActing(t *testing.T) {
	root := t.TempDir()
	rootFd, err := OpenRoot(root)
	if err != nil {
		t.Fatal(err)
	}
	defer unix.Close(rootFd)
	store := newTestStore(t)
	j := testDurableJournal(t)
	grants := testDurableGrants(t, j)

	op, _ := crashApply(t, context.Background(), rootFd, store, j, grants, []FileMutation{
		{BaseName: "a.go", After: []byte("package a\n"), Mode: 0o644},
	}, "plan-digest-1")
	if err := os.WriteFile(filepath.Join(root, "a.go"), []byte("package foreign\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	grants2 := testDurableGrants(t, j) // "restart"
	findings, err := RestartScan(rootFd, j, grants2, store)
	if err != nil {
		t.Fatal(err)
	}
	if len(findings) != 1 {
		t.Fatalf("findings = %d, want 1", len(findings))
	}
	f := findings[0]
	if f.State != TransactionForeign {
		t.Fatalf("state = %v, want FOREIGN", f.State)
	}
	state, ok := grants2.State(op)
	if !ok || state != contracts.AttemptUnknown {
		t.Fatalf("state after scan = %v (ok=%v), want UNKNOWN (untouched)", state, ok)
	}
}

// Detector: a durable STARTED record whose referenced sealed bundle is
// missing from the store (deleted, corrupted, GC'd out from under a live
// reference — should never legitimately happen, but the scan must never
// crash or silently skip on it) classifies FOREIGN — invariant 4's own
// table names this case explicitly.
func TestRestartScanTreatsMissingSealedBundleAsForeign(t *testing.T) {
	root := t.TempDir()
	rootFd, err := OpenRoot(root)
	if err != nil {
		t.Fatal(err)
	}
	defer unix.Close(rootFd)
	storeDir := t.TempDir()
	if err := os.Chmod(storeDir, 0o700); err != nil {
		t.Fatal(err)
	}
	store, err := sealedstore.Open(storeDir)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	j := testDurableJournal(t)
	grants := testDurableGrants(t, j)

	cctx, cancel := context.WithCancel(context.Background())
	cancel()
	op, digest := crashApply(t, cctx, rootFd, store, j, grants, []FileMutation{
		{BaseName: "a.go", After: []byte("package a\n"), Mode: 0o644},
	}, "plan-digest-1")
	if err := os.Remove(filepath.Join(storeDir, digest)); err != nil {
		t.Fatal(err)
	}

	grants2 := testDurableGrants(t, j) // "restart"
	findings, err := RestartScan(rootFd, j, grants2, store)
	if err != nil {
		t.Fatal(err)
	}
	if len(findings) != 1 {
		t.Fatalf("findings = %d, want 1", len(findings))
	}
	if findings[0].Op != op {
		t.Fatalf("op = %q, want %q", findings[0].Op, op)
	}
	if findings[0].State != TransactionForeign {
		t.Fatalf("state = %v, want FOREIGN", findings[0].State)
	}
}

// Detector: a fully CLOSED transaction (workspace.mutation_committed
// landed, S7 SUCCEEDED) is never even included in the scan's findings —
// invariant 2's own v5 correction: a closed transaction is out of scope
// for recovery, permanently, never rescanned even on a later restart.
func TestRestartScanSkipsClosedOperation(t *testing.T) {
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
	if err != nil || !res.Committed {
		t.Fatalf("ApplyGoverned: res=%+v err=%v", res, err)
	}

	grants2 := testDurableGrants(t, j) // "restart"
	findings, err := RestartScan(rootFd, j, grants2, store)
	if err != nil {
		t.Fatal(err)
	}
	if len(findings) != 0 {
		t.Fatalf("findings = %d, want 0 (a closed transaction is out of scope for recovery)", len(findings))
	}
}

// Detector: running RestartScan a SECOND time (a second restart, no new
// activity in between) reports the SAME finding again — this piece is
// read-only and idempotent BY CONSTRUCTION (it never mutates S7 state,
// so nothing changes between scans of an untouched operation).
func TestRestartScanIsIdempotentAcrossRepeatedRestarts(t *testing.T) {
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
	crashApply(t, cctx, rootFd, store, j, grants, []FileMutation{
		{BaseName: "a.go", After: []byte("package a\n"), Mode: 0o644},
	}, "plan-digest-1")

	grants2 := testDurableGrants(t, j)
	first, err := RestartScan(rootFd, j, grants2, store)
	if err != nil || len(first) != 1 || first[0].State != TransactionNeverStarted {
		t.Fatalf("first scan: findings=%+v err=%v", first, err)
	}

	grants3 := testDurableGrants(t, j) // a further "restart"
	second, err := RestartScan(rootFd, j, grants3, store)
	if err != nil {
		t.Fatal(err)
	}
	if len(second) != 1 || second[0].State != TransactionNeverStarted {
		t.Fatalf("second scan: findings=%+v (want the SAME still-UNKNOWN finding again — this piece never advances state)", second)
	}
}

// Detector: two different workspace roots sharing ONE journal — an
// UNKNOWN operation bound to root B's own identity must never leak into
// a scan of root A. Proves the exact-intent prefix scoping, not just
// "some filter exists."
func TestRestartScanScopesToRootIdentity(t *testing.T) {
	rootA := t.TempDir()
	rootFdA, err := OpenRoot(rootA)
	if err != nil {
		t.Fatal(err)
	}
	defer unix.Close(rootFdA)
	rootB := t.TempDir()
	rootFdB, err := OpenRoot(rootB)
	if err != nil {
		t.Fatal(err)
	}
	defer unix.Close(rootFdB)

	store := newTestStore(t)
	j := testDurableJournal(t)
	grants := testDurableGrants(t, j)

	cctx, cancel := context.WithCancel(context.Background())
	cancel()
	crashApply(t, cctx, rootFdB, store, j, grants, []FileMutation{
		{BaseName: "b.go", After: []byte("package b\n"), Mode: 0o644},
	}, "plan-digest-b")

	grants2 := testDurableGrants(t, j)
	findings, err := RestartScan(rootFdA, j, grants2, store)
	if err != nil {
		t.Fatal(err)
	}
	if len(findings) != 0 {
		t.Fatalf("findings = %d, want 0 (root B's operation must not leak into root A's scan)", len(findings))
	}
}

// Detector (codex review round 1, HIGH #2, reproduced live): a companion
// event whose OWN payload "op" field disagrees with the S7 operation it
// was actually Consumed against (Companion.Key) must NOT hide the real
// crash-orphaned operation. RestartScan must discover it from S7's own
// authoritative s7.attempt_started event, never from the — possibly
// lying — owner payload.
func TestRestartScanFindsRealOperationEvenWhenCompanionPayloadLies(t *testing.T) {
	root := t.TempDir()
	rootFd, err := OpenRoot(root)
	if err != nil {
		t.Fatal(err)
	}
	defer unix.Close(rootFd)
	store := newTestStore(t)
	j := testDurableJournal(t)
	grants := testDurableGrants(t, j)

	var st unix.Stat_t
	if err := unix.Fstat(rootFd, &st); err != nil {
		t.Fatal(err)
	}
	opReal := ApplyOperationID(testProfile, uint64(st.Dev), st.Ino, "plan-real")
	opDecoy := ApplyOperationID(testProfile, uint64(st.Dev), st.Ino, "plan-decoy")
	target := ApplyTargetID(testProfile, uint64(st.Dev), st.Ino)
	if err := grants.Begin(opReal, target, PolicyWorkspaceApply); err != nil {
		t.Fatal(err)
	}
	grant, err := grants.Next(opReal, func(l s7.Landing) s7.Companion {
		return s7.Companion{Key: opReal, Params: envelope(j, opReal, "run-crash", 0, EvApplyFailed,
			applyFailedPayload{Op: string(opReal), AttemptNo: 0, Landing: int(l.Kind)})}
	})
	if err != nil {
		t.Fatal(err)
	}
	cctx, cancel := context.WithCancel(context.Background())
	cancel()
	bindDurable := func(digest string) error {
		// Companion.Key correctly names opReal (S7 itself enforces this at
		// Consume) — but the PAYLOAD's own "op" field lies, naming opDecoy.
		return grants.Consume(grant, s7.Companion{Key: opReal, Params: envelope(j, opReal, "run-crash", grant.AttemptNo, EvApplyStarted,
			applyStartedPayload{Op: string(opDecoy), BundleDigest: digest})})
	}
	if _, err := apply(cctx, rootFd, store, []FileMutation{
		{BaseName: "a.go", After: []byte("package a\n"), Mode: 0o644},
	}, bindDurable); err == nil {
		t.Fatal("expected apply to refuse under a pre-cancelled context")
	}

	grants2 := testDurableGrants(t, j) // "restart"
	findings, err := RestartScan(rootFd, j, grants2, store)
	if err != nil {
		t.Fatal(err)
	}
	if len(findings) != 1 {
		t.Fatalf("findings = %d, want 1 (the real op, opDecoy must never even be considered)", len(findings))
	}
	if findings[0].Op != opReal {
		t.Fatalf("op = %q, want %q (the real S7-bound operation, not the decoy the payload named)", findings[0].Op, opReal)
	}
}

// Detector (codex review round 1, HIGH #2, reproduced live): an
// operation whose STRING happens to share this root's own exact-intent
// prefix, but whose S7-bound TARGET is for a DIFFERENT resource, must be
// excluded from this root's scan entirely — a textual prefix collision
// is not proof of ownership; s7.Authority.Target is the authority.
func TestRestartScanExcludesOperationWithForeignTarget(t *testing.T) {
	root := t.TempDir()
	rootFd, err := OpenRoot(root)
	if err != nil {
		t.Fatal(err)
	}
	defer unix.Close(rootFd)
	store := newTestStore(t)
	j := testDurableJournal(t)
	grants := testDurableGrants(t, j)

	var st unix.Stat_t
	if err := unix.Fstat(rootFd, &st); err != nil {
		t.Fatal(err)
	}
	// A string that collides with THIS root's exact-intent prefix, but is
	// deliberately Begin'd against an unrelated target — something only a
	// bug or a caller other than ApplyGoverned could ever produce (which
	// always derives op+target together from the SAME root identity).
	opCollision := ApplyOperationID(testProfile, uint64(st.Dev), st.Ino, "plan-collision")
	foreignTarget := contracts.TargetID("workspace:other-profile:99:99")
	if err := grants.Begin(opCollision, foreignTarget, PolicyWorkspaceApply); err != nil {
		t.Fatal(err)
	}
	grant, err := grants.Next(opCollision, func(l s7.Landing) s7.Companion {
		return s7.Companion{Key: opCollision, Params: envelope(j, opCollision, "run-crash", 0, EvApplyFailed,
			applyFailedPayload{Op: string(opCollision), AttemptNo: 0, Landing: int(l.Kind)})}
	})
	if err != nil {
		t.Fatal(err)
	}
	cctx, cancel := context.WithCancel(context.Background())
	cancel()
	bindDurable := func(digest string) error {
		return grants.Consume(grant, s7.Companion{Key: opCollision, Params: envelope(j, opCollision, "run-crash", grant.AttemptNo, EvApplyStarted,
			applyStartedPayload{Op: string(opCollision), BundleDigest: digest})})
	}
	if _, err := apply(cctx, rootFd, store, []FileMutation{
		{BaseName: "a.go", After: []byte("package a\n"), Mode: 0o644},
	}, bindDurable); err == nil {
		t.Fatal("expected apply to refuse under a pre-cancelled context")
	}

	grants2 := testDurableGrants(t, j) // "restart"
	if state, ok := grants2.State(opCollision); !ok || state != contracts.AttemptUnknown {
		t.Fatalf("precondition: opCollision must rehydrate UNKNOWN, got %v (ok=%v)", state, ok)
	}
	findings, err := RestartScan(rootFd, j, grants2, store)
	if err != nil {
		t.Fatal(err)
	}
	if len(findings) != 0 {
		t.Fatalf("findings = %d, want 0 (a prefix collision bound to a foreign target must never be scanned)", len(findings))
	}
}

// Detector (codex review round 2, HIGH #1, reproduced live): an
// authoritative UNKNOWN s7.attempt_started whose adjacent companion is
// missing, malformed, or simply the WRONG event type must surface as
// FOREIGN — never silently dropped from the findings entirely. An S7
// record proves an attempt truly started; this piece just can't prove
// which sealed bundle (if any) it belongs to.
func TestRestartScanTreatsBrokenCompanionAsForeignNotOmitted(t *testing.T) {
	root := t.TempDir()
	rootFd, err := OpenRoot(root)
	if err != nil {
		t.Fatal(err)
	}
	defer unix.Close(rootFd)
	store := newTestStore(t)
	j := testDurableJournal(t)
	grants := testDurableGrants(t, j)

	var st unix.Stat_t
	if err := unix.Fstat(rootFd, &st); err != nil {
		t.Fatal(err)
	}
	op := ApplyOperationID(testProfile, uint64(st.Dev), st.Ino, "plan-wrong-companion")
	target := ApplyTargetID(testProfile, uint64(st.Dev), st.Ino)
	if err := grants.Begin(op, target, PolicyWorkspaceApply); err != nil {
		t.Fatal(err)
	}
	grant, err := grants.Next(op, func(l s7.Landing) s7.Companion {
		return s7.Companion{Key: op, Params: envelope(j, op, "run-crash", 0, EvApplyFailed,
			applyFailedPayload{Op: string(op), AttemptNo: 0, Landing: int(l.Kind)})}
	})
	if err != nil {
		t.Fatal(err)
	}
	// A VALID companion, but the WRONG event type — workspace.apply_failed
	// instead of workspace.apply_started, exactly like a real (if
	// unexpected) landing rather than a crash. Its own AttemptNo/Landing
	// shape must pass EvApplyFailed's own admission validator.
	wrong := s7.Companion{Key: op, Params: envelope(j, op, "run-crash", grant.AttemptNo, EvApplyFailed,
		applyFailedPayload{Op: string(op), AttemptNo: grant.AttemptNo, Landing: int(s7.LandingUnknown)})}
	if err := grants.Consume(grant, wrong); err != nil {
		t.Fatal(err)
	}
	// No Report/Cancel follows — this IS the simulated crash, on an
	// attempt whose own companion never confirmed a sealed bundle at all.

	grants2 := testDurableGrants(t, j) // "restart"
	if state, ok := grants2.State(op); !ok || state != contracts.AttemptUnknown {
		t.Fatalf("precondition: op must rehydrate UNKNOWN, got %v (ok=%v)", state, ok)
	}
	findings, err := RestartScan(rootFd, j, grants2, store)
	if err != nil {
		t.Fatal(err)
	}
	if len(findings) != 1 {
		t.Fatalf("findings = %d, want 1 (an UNKNOWN workspace apply with a broken companion must never be silently omitted)", len(findings))
	}
	if findings[0].Op != op || findings[0].State != TransactionForeign {
		t.Fatalf("finding = %+v, want {Op:%q State:FOREIGN}", findings[0], op)
	}
}

// Detector (codex review round 2, HIGH #1's own "worse" elaboration,
// reproduced live): a SECOND attempt whose own companion is broken must
// NEVER fall back to reusing an EARLIER attempt's still-valid digest —
// the earlier bundle belongs to a DIFFERENT, already-superseded attempt.
func TestRestartScanDoesNotReuseStaleDigestFromEarlierAttempt(t *testing.T) {
	origPolicy := PolicyWorkspaceApply
	PolicyWorkspaceApply = s7.Policy{EffectClass: contracts.EffectIrreversible, MaxAttempts: 2, Durable: true,
		RetryableCodes: []string{s7.CodeMutationRolledBack}}
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

	var st unix.Stat_t
	if err := unix.Fstat(rootFd, &st); err != nil {
		t.Fatal(err)
	}
	op := ApplyOperationID(testProfile, uint64(st.Dev), st.Ino, "plan-multi-attempt")
	target := ApplyTargetID(testProfile, uint64(st.Dev), st.Ino)
	if err := grants.Begin(op, target, PolicyWorkspaceApply); err != nil {
		t.Fatal(err)
	}
	// attemptNo is captured ONCE, OUTSIDE each builder closure — never
	// re-queried live via grants.Attempts(op) FROM INSIDE one. Every
	// builder S7 passes control to is invoked WHILE S7 already holds its
	// own internal lock; calling a method that also locks from inside one
	// is a same-goroutine relock — a real deadlock this exact shape hit
	// immediately here (governed.go's ApplyGoverned already documents the
	// identical lesson from the original S7-wiring review).
	buildFor := func(attemptNo int) func(s7.Landing) s7.Companion {
		return func(l s7.Landing) s7.Companion {
			return s7.Companion{Key: op, Params: envelope(j, op, "run-1", attemptNo, EvApplyFailed,
				applyFailedPayload{Op: string(op), AttemptNo: attemptNo, Landing: int(l.Kind), Code: l.Code})}
		}
	}

	// Attempt 1: a real, VALID companion — then a normal (non-crash)
	// verified-clean-rollback report, landing FAILED_RETRYABLE.
	staleBundle, err := json.Marshal(Bundle{Files: []FileRecord{{BaseName: "a.go", Existed: false, After: []byte("x"), AfterMode: 0o644}}})
	if err != nil {
		t.Fatal(err)
	}
	staleDigest, pin, err := store.Put(staleBundle)
	if err != nil {
		t.Fatal(err)
	}
	pin.Release()
	g1, err := grants.Next(op, buildFor(grants.Attempts(op)))
	if err != nil {
		t.Fatal(err)
	}
	if err := grants.Consume(g1, s7.Companion{Key: op, Params: envelope(j, op, "run-1", g1.AttemptNo, EvApplyStarted,
		applyStartedPayload{Op: string(op), BundleDigest: staleDigest})}); err != nil {
		t.Fatal(err)
	}
	if err := grants.Report(op, s7.OutcomeFailedRetryable, s7.CodeMutationRolledBack, buildFor(grants.Attempts(op))); err != nil {
		t.Fatal(err)
	}

	// Attempt 2: consumed, but its OWN companion is broken (wrong event
	// type) — then a simulated crash (no Report).
	g2, err := grants.Next(op, buildFor(grants.Attempts(op)))
	if err != nil {
		t.Fatal(err)
	}
	wrong := s7.Companion{Key: op, Params: envelope(j, op, "run-1", g2.AttemptNo, EvApplyFailed,
		applyFailedPayload{Op: string(op), AttemptNo: g2.AttemptNo, Landing: int(s7.LandingUnknown)})}
	if err := grants.Consume(g2, wrong); err != nil {
		t.Fatal(err)
	}

	grants2 := testDurableGrants(t, j) // "restart"
	if state, ok := grants2.State(op); !ok || state != contracts.AttemptUnknown {
		t.Fatalf("precondition: op must rehydrate UNKNOWN, got %v (ok=%v)", state, ok)
	}
	findings, err := RestartScan(rootFd, j, grants2, store)
	if err != nil {
		t.Fatal(err)
	}
	if len(findings) != 1 {
		t.Fatalf("findings = %d, want 1", len(findings))
	}
	if findings[0].State != TransactionForeign {
		t.Fatalf("state = %v, want FOREIGN (attempt 2's broken companion must not fall back to attempt 1's stale digest %q)", findings[0].State, staleDigest)
	}
}

// Detector (codex review round 2, HIGH #2, reproduced live): an
// operation whose STRING shares this root's exact-intent prefix and
// whose TARGET matches, but whose bound POLICY does not (e.g. a
// non-durable read-only effect, never an irreversible durable workspace
// mutation), is an impossible governance narrative for this owner. It
// must never be reported as a legitimate workspace-apply finding — but
// (round 4 HIGH #1 correction: silently excluding EVERY policy mismatch
// made a genuine policy-drifted crash-orphan invisible too, see
// TestRestartScanReportsPolicyDriftedOperationAsForeign below) it
// surfaces as FOREIGN rather than vanishing.
func TestRestartScanReportsForeignPolicyOperationAsForeign(t *testing.T) {
	root := t.TempDir()
	rootFd, err := OpenRoot(root)
	if err != nil {
		t.Fatal(err)
	}
	defer unix.Close(rootFd)
	store := newTestStore(t)
	j := testDurableJournal(t)
	grants := testDurableGrants(t, j)

	var st unix.Stat_t
	if err := unix.Fstat(rootFd, &st); err != nil {
		t.Fatal(err)
	}
	op := ApplyOperationID(testProfile, uint64(st.Dev), st.Ino, "plan-foreign-policy")
	target := ApplyTargetID(testProfile, uint64(st.Dev), st.Ino)
	foreignPolicy := s7.Policy{EffectClass: contracts.EffectReadOnly, MaxAttempts: 1, Durable: true}
	if err := grants.Begin(op, target, foreignPolicy); err != nil {
		t.Fatal(err)
	}
	grant, err := grants.Next(op, func(l s7.Landing) s7.Companion {
		return s7.Companion{Key: op, Params: envelope(j, op, "run-crash", 0, EvApplyFailed,
			applyFailedPayload{Op: string(op), AttemptNo: 0, Landing: int(l.Kind)})}
	})
	if err != nil {
		t.Fatal(err)
	}
	cctx, cancel := context.WithCancel(context.Background())
	cancel()
	bindDurable := func(digest string) error {
		return grants.Consume(grant, s7.Companion{Key: op, Params: envelope(j, op, "run-crash", grant.AttemptNo, EvApplyStarted,
			applyStartedPayload{Op: string(op), BundleDigest: digest})})
	}
	if _, err := apply(cctx, rootFd, store, []FileMutation{
		{BaseName: "a.go", After: []byte("package a\n"), Mode: 0o644},
	}, bindDurable); err == nil {
		t.Fatal("expected apply to refuse under a pre-cancelled context")
	}

	grants2 := testDurableGrants(t, j) // "restart"
	if state, ok := grants2.State(op); !ok || state != contracts.AttemptUnknown {
		t.Fatalf("precondition: op must rehydrate UNKNOWN, got %v (ok=%v)", state, ok)
	}
	findings, err := RestartScan(rootFd, j, grants2, store)
	if err != nil {
		t.Fatal(err)
	}
	if len(findings) != 1 || findings[0].State != TransactionForeign {
		t.Fatalf("findings = %+v, want exactly one FOREIGN (an operation begun under a foreign policy must never be reported as a legitimate workspace apply, but must not vanish either)", findings)
	}
}

// Detector (codex review round 3, HIGH #1, reproduced live): RestartScan
// must refuse outright when j and grants are NOT the same bound journal —
// state/target/policy answers from an UNRELATED Authority, cross-checked
// against events replayed from a DIFFERENT journal, can misreport a
// CLOSED transaction as MID_CRASH purely because the two journals happen
// to share a profile+root+plan-digest (hence the identical operation ID).
func TestRestartScanRefusesMismatchedJournalAndAuthority(t *testing.T) {
	root := t.TempDir()
	rootFd, err := OpenRoot(root)
	if err != nil {
		t.Fatal(err)
	}
	defer unix.Close(rootFd)
	store := newTestStore(t)

	journalA := testDurableJournal(t)
	grantsA := testDurableGrants(t, journalA)
	res, err := ApplyGoverned(context.Background(), rootFd, store, []FileMutation{
		{BaseName: "a.go", After: []byte("package a\n"), Mode: 0o644},
	}, grantsA, journalA, "same-plan", "run-a")
	if err != nil || !res.Committed {
		t.Fatalf("closed apply on journal A: res=%+v err=%v", res, err)
	}

	// journalB is a COMPLETELY SEPARATE journal that happens to share this
	// test's fixed profile+root+plan-digest, so it derives the IDENTICAL
	// operation ID — a real scenario this narrow (same rootFd across two
	// journals) needs no adversarial construction beyond that coincidence.
	var st unix.Stat_t
	if err := unix.Fstat(rootFd, &st); err != nil {
		t.Fatal(err)
	}
	op := ApplyOperationID(testProfile, uint64(st.Dev), st.Ino, "same-plan")
	target := ApplyTargetID(testProfile, uint64(st.Dev), st.Ino)
	journalB := testDurableJournal(t)
	grantsB := testDurableGrants(t, journalB)
	if err := grantsB.Begin(op, target, PolicyWorkspaceApply); err != nil {
		t.Fatal(err)
	}
	g, err := grantsB.Next(op, func(l s7.Landing) s7.Companion {
		return s7.Companion{Key: op, Params: envelope(journalB, op, "run-b", 0, EvApplyFailed,
			applyFailedPayload{Op: string(op), AttemptNo: 0, Landing: int(l.Kind)})}
	})
	if err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(Bundle{Files: []FileRecord{
		{BaseName: "a.go", Existed: true, Before: []byte("package a\n"), BeforeMode: 0o644, After: []byte("package a\n"), AfterMode: 0o644},
	}})
	if err != nil {
		t.Fatal(err)
	}
	digest, pin, err := store.Put(raw)
	if err != nil {
		t.Fatal(err)
	}
	defer pin.Release()
	if err := grantsB.Consume(g, s7.Companion{Key: op, Params: envelope(journalB, op, "run-b", g.AttemptNo, EvApplyStarted,
		applyStartedPayload{Op: string(op), BundleDigest: digest})}); err != nil {
		t.Fatal(err)
	}
	// No Report — journal B's own attempt is a simulated crash.
	grantsBRestarted := testDurableGrants(t, journalB)

	if _, err := RestartScan(rootFd, journalA, grantsBRestarted, store); err == nil {
		t.Fatal("expected RestartScan to refuse a mismatched (journal, authority) pair")
	}
	// The genuinely matched pairing must still work correctly (journal A's
	// closed transaction stays out of scope, as always).
	grantsARestarted := testDurableGrants(t, journalA)
	findings, err := RestartScan(rootFd, journalA, grantsARestarted, store)
	if err != nil {
		t.Fatal(err)
	}
	if len(findings) != 0 {
		t.Fatalf("matched pairing: findings = %d, want 0 (journal A's transaction is closed)", len(findings))
	}
}

// Detector (codex review round 4, HIGH #1, reproduced live): a LEGITIMATE
// workspace-apply operation begun under an intentionally overridden
// policy (governed.go's own documented caller-driven-retry mechanism —
// PolicyWorkspaceApply is a package var precisely so a caller CAN
// override it), then crashed, must still be found and reported after a
// restart even if PolicyWorkspaceApply has since reverted to its
// default (or simply changed across an upgrade) — it must surface as
// FOREIGN, never silently vanish, since this piece cannot durably tell
// "legitimate policy drift" apart from "a genuine foreign-policy
// collision" (the case TestRestartScanReportsForeignPolicyOperationAsForeign
// covers) without persisting a policy identity of its own.
func TestRestartScanReportsPolicyDriftedOperationAsForeign(t *testing.T) {
	original := PolicyWorkspaceApply
	overridden := s7.Policy{EffectClass: contracts.EffectIrreversible, MaxAttempts: 2, Durable: true,
		RetryableCodes: []string{s7.CodeMutationRolledBack}}
	PolicyWorkspaceApply = overridden
	defer func() { PolicyWorkspaceApply = original }()

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
	op, _ := crashApply(t, cctx, rootFd, store, j, grants, []FileMutation{
		{BaseName: "a.go", After: []byte("package a\n"), Mode: 0o644},
	}, "plan-policy-drift")

	// "Restart" or "upgrade": the package's current policy has reverted
	// to its default by the time the scan runs — the operation's OWN
	// bound policy (the overridden one, durably recorded at Begin time)
	// has not changed, but PolicyWorkspaceApply now has.
	PolicyWorkspaceApply = original
	grants2 := testDurableGrants(t, j)
	if state, ok := grants2.State(op); !ok || state != contracts.AttemptUnknown {
		t.Fatalf("precondition: op must rehydrate UNKNOWN, got %v (ok=%v)", state, ok)
	}
	findings, err := RestartScan(rootFd, j, grants2, store)
	if err != nil {
		t.Fatal(err)
	}
	if len(findings) != 1 {
		t.Fatalf("findings = %+v, want exactly one (a legitimate policy-drifted crash-orphan must never be silently omitted)", findings)
	}
	if findings[0].Op != op || findings[0].State != TransactionForeign {
		t.Fatalf("finding = %+v, want {Op:%q State:FOREIGN}", findings[0], op)
	}
}
