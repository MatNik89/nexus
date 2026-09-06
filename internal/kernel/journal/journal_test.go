// T06 RED table v2 (post Phase-1A review) at the pre-agreed seam
// (Open/Append/Replay/Close + VerifyChain). Spec anchors: Annex P0.3
// (journal offset ≠ per-run sequence, integrity chain, durable flush,
// secret-never-reaches-sink), HARDQ B3 (profile binding), B7 (single
// append actor), C1 (known-ref redaction), review findings #2/#9/#12/#13.
package journal

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/MatNik89/nexus/internal/kernel/contracts"
	"github.com/MatNik89/nexus/internal/security/redact"
)

func params(run, event string) contracts.EnvelopeParams {
	return contracts.EnvelopeParams{
		SchemaID: "nexus.event", SchemaVersion: 1,
		EventID: contracts.EventID("ev-" + run + "-" + event), EventType: event,
		RunID: contracts.RunID(run), EmittedAt: time.Unix(1000, 0), ActorType: contracts.ActorUser,
		ActorID: "a", PrincipalID: "p", WorkspaceID: "w", ProfileID: "work",
		AttemptNo: 1, Payload: json.RawMessage(`{"e":"` + event + `"}`), PayloadHash: "recomputed-by-journal",
	}
}

// testEvents is the closed event-type set used by the suite; the "e*"/"k*"
// generated names funnel through a wildcard-free explicit list built per test.
func testEvents(names ...string) map[string]PayloadValidator {
	m := map[string]PayloadValidator{}
	for _, n := range names {
		m[n] = nil
	}
	return m
}

func manyEvents() map[string]PayloadValidator {
	m := map[string]PayloadValidator{}
	for i := 0; i < 200; i++ {
		m[fmt.Sprintf("e%d", i)] = nil
		m[fmt.Sprintf("k%d", i)] = nil
	}
	for _, n := range []string{"e1", "e2", "good", "bad", "same", "leak", "x", "post-crash"} {
		m[n] = nil
	}
	return m
}

func open(t *testing.T, dir string, r redact.Redactor) *Journal {
	t.Helper()
	j, err := Open(filepath.Join(dir, "journal.db"), "work", r, manyEvents())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { j.Close() })
	return j
}

// Offsets are global; sequences are PER RUN (P0.3 separates them).
func TestOffsetsGlobalSequencesPerRun(t *testing.T) {
	j := open(t, t.TempDir(), redact.None{})
	ctx := context.Background()
	a1, _ := j.Append(ctx, params("run-a", "e1"))
	b1, _ := j.Append(ctx, params("run-b", "e1"))
	a2, err := j.Append(ctx, params("run-a", "e2"))
	if err != nil {
		t.Fatal(err)
	}
	if a1.JournalOffset != 1 || b1.JournalOffset != 2 || a2.JournalOffset != 3 {
		t.Fatalf("global offsets wrong: %d %d %d", a1.JournalOffset, b1.JournalOffset, a2.JournalOffset)
	}
	if a1.Envelope.Sequence != 1 || a2.Envelope.Sequence != 2 || b1.Envelope.Sequence != 1 {
		t.Fatalf("per-run sequences wrong: a1=%d a2=%d b1=%d",
			a1.Envelope.Sequence, a2.Envelope.Sequence, b1.Envelope.Sequence)
	}
}

// A REJECTED append burns neither offset nor sequence (review #2).
func TestFailedAppendBurnsNothing(t *testing.T) {
	j := open(t, t.TempDir(), redact.None{})
	ctx := context.Background()
	bad := params("run-a", "bad")
	bad.EventType = "" // invalid → rejected
	if _, err := j.Append(ctx, bad); err == nil {
		t.Fatal("invalid envelope accepted")
	}
	good, err := j.Append(ctx, params("run-a", "good"))
	if err != nil {
		t.Fatal(err)
	}
	if good.JournalOffset != 1 || good.Envelope.Sequence != 1 {
		t.Fatalf("rejected append burned an offset/sequence: offset=%d seq=%d",
			good.JournalOffset, good.Envelope.Sequence)
	}
}

// Profile binding (B3): the work journal physically refuses private events.
func TestProfileMismatchRejected(t *testing.T) {
	j := open(t, t.TempDir(), redact.None{})
	p := params("run-a", "leak")
	p.ProfileID = "private"
	if _, err := j.Append(context.Background(), p); err == nil {
		t.Fatal("event with a foreign profile accepted by a bound journal")
	}
}

func TestConcurrentAppendsContiguousOffsets(t *testing.T) {
	j := open(t, t.TempDir(), redact.None{})
	const workers, per = 4, 25
	var wg sync.WaitGroup
	for w := 0; w < workers; w++ {
		wg.Add(1)
		go func(w int) {
			defer wg.Done()
			for i := 0; i < per; i++ {
				if _, err := j.Append(context.Background(), params(fmt.Sprintf("run-%d", w), fmt.Sprintf("e%d", i))); err != nil {
					t.Errorf("append: %v", err)
					return
				}
			}
		}(w)
	}
	wg.Wait()
	offsets := map[uint64]bool{}
	if err := j.Replay(0, func(ev Event) error {
		offsets[ev.JournalOffset] = true
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	for i := uint64(1); i <= uint64(workers*per); i++ {
		if !offsets[i] {
			t.Fatalf("offset gap at %d (have %d events)", i, len(offsets))
		}
	}
	if err := j.VerifyChain(); err != nil {
		t.Fatalf("integrity chain invalid after concurrent appends: %v", err)
	}
}

// PayloadHash must describe the STORED (redacted) payload (kilo #2).
func TestPayloadHashMatchesStoredPayload(t *testing.T) {
	secret := "sk-SECRET-XYZ"
	j := open(t, t.TempDir(), redact.NewKnownRefs(map[string]string{"key": secret}))
	p := params("run-a", "leak")
	p.Payload = json.RawMessage(`{"text":"` + secret + `"}`)
	ev, err := j.Append(context.Background(), p)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(ev.Envelope.Payload), secret) {
		t.Fatal("secret in returned payload")
	}
	sum := shaHex(ev.Envelope.Payload)
	if ev.Envelope.PayloadHash != sum {
		t.Fatalf("payload_hash %q does not match stored payload hash %q", ev.Envelope.PayloadHash, sum)
	}
}

// A known secret in a STRUCTURAL field REJECTS the append — never a mutated
// stored copy diverging from the returned one (r2 codex #2), and nothing
// reaches the sink.
func TestSecretInStructuralFieldRejected(t *testing.T) {
	secret := "tok-9988776655"
	fields := map[string]func(*contracts.EnvelopeParams){
		"actor_id": func(p *contracts.EnvelopeParams) { p.ActorID = contracts.ActorID("a-" + secret) },
		"event_id": func(p *contracts.EnvelopeParams) { p.EventID = contracts.EventID("e-" + secret) },
		"run_id":   func(p *contracts.EnvelopeParams) { p.RunID = contracts.RunID("r-" + secret) },
		// event_type is registered below so admission reaches the
		// structural-secret check (r4 codex #2: an unregistered name was
		// rejected earlier, making this subtest non-causal).
		"event_type":      func(p *contracts.EnvelopeParams) { p.EventType = "x-" + secret },
		"principal_id":    func(p *contracts.EnvelopeParams) { p.PrincipalID = contracts.PrincipalID("p-" + secret) },
		"workspace_id":    func(p *contracts.EnvelopeParams) { p.WorkspaceID = contracts.WorkspaceID("w-" + secret) },
		"parent_event_id": func(p *contracts.EnvelopeParams) { id := contracts.EventID("pe-" + secret); p.ParentEventID = &id },
		"turn_id":         func(p *contracts.EnvelopeParams) { id := contracts.TurnID("t-" + secret); p.TurnID = &id },
		"tool_call_id":    func(p *contracts.EnvelopeParams) { id := contracts.ToolCallID("tc-" + secret); p.ToolCallID = &id },
		"idempotency_key": func(p *contracts.EnvelopeParams) { k := "k-" + secret; p.IdempotencyKey = &k },
	}
	for name, mutate := range fields {
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			ev := manyEvents()
			ev["x-"+secret] = nil // registered: rejection must come from Touches
			j, err := Open(filepath.Join(dir, "journal.db"), "work",
				redact.NewKnownRefs(map[string]string{"tg": secret}), ev)
			if err != nil {
				t.Fatal(err)
			}
			defer j.Close()
			p := params("run-a", "x")
			mutate(&p)
			if _, err := j.Append(context.Background(), p); err == nil {
				t.Fatalf("append with a known secret in %s accepted", name)
			}
			j.Close()
			for _, f := range []string{"journal.db", "journal.db-wal"} {
				if b, err := os.ReadFile(filepath.Join(dir, f)); err == nil && strings.Contains(string(b), secret) {
					t.Fatalf("secret present in sink file %s", f)
				}
			}
		})
	}
}

// Cancellation gates admission only: once admitted, the outcome is
// definitive (codex #12).
func TestCancelledContextAfterAdmissionStillDefinitive(t *testing.T) {
	j := open(t, t.TempDir(), redact.None{})
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // already cancelled: admission may fail — that is fine
	ev, err := j.Append(ctx, params("run-a", "e1"))
	if err == nil {
		// If it was admitted despite the cancel race, it must be REAL.
		found := false
		j.Replay(0, func(e Event) error {
			if e.Envelope.EventID == ev.Envelope.EventID {
				found = true
			}
			return nil
		})
		if !found {
			t.Fatal("append reported success but event is not durable")
		}
		return
	}
	// If it errored, the journal must NOT contain the event.
	count := 0
	j.Replay(0, func(Event) error { count++; return nil })
	if count != 0 && err == context.Canceled {
		t.Fatal("caller saw 'cancelled' for an event that committed")
	}
}

// Duplicate event IDs are refused (idempotent-admission seed, codex #12).
func TestDuplicateEventIDRejected(t *testing.T) {
	j := open(t, t.TempDir(), redact.None{})
	ctx := context.Background()
	p := params("run-a", "same")
	if _, err := j.Append(ctx, p); err != nil {
		t.Fatal(err)
	}
	if _, err := j.Append(ctx, p); err == nil {
		t.Fatal("duplicate event_id accepted")
	}
}

func TestConcurrentCloseSafe(t *testing.T) {
	j := open(t, t.TempDir(), redact.None{})
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); j.Close() }()
	}
	wg.Wait() // must not panic (codex #13)
}

func TestOpenFailsClosedOnMissingDeps(t *testing.T) {
	if _, err := Open(filepath.Join(t.TempDir(), "j.db"), "work", nil, manyEvents()); err == nil {
		t.Fatal("nil redactor accepted")
	}
	if _, err := Open(filepath.Join(t.TempDir(), "j.db"), "", redact.None{}, manyEvents()); err == nil {
		t.Fatal("empty profile binding accepted")
	}
	if _, err := Open(filepath.Join(t.TempDir(), "j.db"), "work", redact.None{}, nil); err == nil {
		t.Fatal("empty event-type set accepted")
	}
}

// Profile binding is IN the database (r2 codex #3): reopening under a
// different profile is refused even by a fresh process handle.
func TestReopenUnderDifferentProfileRefused(t *testing.T) {
	dir := t.TempDir()
	j := open(t, dir, redact.None{})
	j.Close()
	if _, err := Open(filepath.Join(dir, "journal.db"), "private", redact.None{}, manyEvents()); err == nil {
		t.Fatal("work-bound database reopened as private")
	}
}

// The REAL two-handle case (r3 codex #3): while one handle is open, a
// second Open on the same database is refused — nonce distinguishes
// handles within one process. After Close (lease released) reopen works.
func TestSecondLiveHandleRefused(t *testing.T) {
	dir := t.TempDir()
	j1 := open(t, dir, redact.None{})
	if _, err := Open(filepath.Join(dir, "journal.db"), "work", redact.None{}, manyEvents()); err == nil {
		t.Fatal("second live handle on the same journal accepted")
	}
	j1.Close()
	j2, err := Open(filepath.Join(dir, "journal.db"), "work", redact.None{}, manyEvents())
	if err != nil {
		t.Fatalf("reopen after Close (lease released) must succeed: %v", err)
	}
	j2.Close()
}

// A FAILED open must not leave a live lease behind (r3 codex #3).
func TestFailedOpenLeavesNoLease(t *testing.T) {
	dir := t.TempDir()
	j := open(t, dir, redact.None{})
	ctx := context.Background()
	for i := 0; i < 2; i++ {
		if _, err := j.Append(ctx, params("run-a", fmt.Sprintf("e%d", i))); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := j.db.Exec(`UPDATE events SET envelope = replace(envelope, 'e1', 'eX') WHERE journal_offset = 2`); err != nil {
		t.Fatal(err)
	}
	j.Close()
	if _, err := Open(filepath.Join(dir, "journal.db"), "work", redact.None{}, manyEvents()); err == nil {
		t.Fatal("tampered journal opened")
	}
	// The failed open must not have written a writer lease.
	db, err := sql.Open("sqlite", "file:"+filepath.Join(dir, "journal.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var v string
	err = db.QueryRow(`SELECT value FROM journal_meta WHERE key='writer'`).Scan(&v)
	if err == nil {
		t.Fatalf("failed open left a writer lease behind: %s", v)
	}
}

// The closed event set stays closed after Open (r3 codex #4).
func TestEventSetImmutableAfterOpen(t *testing.T) {
	dir := t.TempDir()
	callerMap := testEvents("only.event")
	j, err := Open(filepath.Join(dir, "journal.db"), "work", redact.None{}, callerMap)
	if err != nil {
		t.Fatal(err)
	}
	defer j.Close()
	callerMap["smuggled.event"] = nil // post-open mutation of the CALLER map
	p := params("run-a", "smuggled.event")
	if _, err := j.Append(context.Background(), p); err == nil {
		t.Fatal("post-open mutation of the caller map widened the closed set")
	}
}

// Admission-definitive with a REAL barrier (r3 codex #10): cancel the ctx
// while the actor is BETWEEN commit and reply — Append must still return
// the committed result, never ctx.Err().
func TestCancelBetweenCommitAndReplyStillDefinitive(t *testing.T) {
	j := open(t, t.TempDir(), redact.None{})
	inWindow := make(chan struct{})
	release := make(chan struct{})
	testPauseBeforeReply = func() {
		close(inWindow)
		<-release
	}
	defer func() { testPauseBeforeReply = nil }()
	ctx, cancel := context.WithCancel(context.Background())
	type result struct {
		ev  Event
		err error
	}
	res := make(chan result, 1)
	go func() {
		ev, err := j.Append(ctx, params("run-a", "e1"))
		res <- result{ev, err}
	}()
	<-inWindow // actor has committed, reply not yet delivered
	cancel()   // cancellation lands exactly in the forbidden window
	// Revert-proof (r4 codex #7): while the actor is STILL blocked, a
	// wrong implementation selecting ctx.Done() would return NOW — prove
	// nothing returns before release.
	select {
	case r := <-res:
		t.Fatalf("Append returned during the blocked window: %+v (cancel must not preempt a committed result)", r)
	case <-time.After(300 * time.Millisecond):
	}
	close(release)
	r := <-res
	if r.err != nil {
		t.Fatalf("caller saw %v for an event that committed (admission-definitive violated)", r.err)
	}
	if r.ev.JournalOffset != 1 {
		t.Fatalf("unexpected offset %d", r.ev.JournalOffset)
	}
}

// Unknown event types are rejected before persistence (r2 codex #7).
func TestUnknownEventTypeRejected(t *testing.T) {
	dir := t.TempDir()
	j, err := Open(filepath.Join(dir, "journal.db"), "work", redact.None{}, testEvents("known.event"))
	if err != nil {
		t.Fatal(err)
	}
	defer j.Close()
	p := params("run-a", "unknown.event")
	if _, err := j.Append(context.Background(), p); err == nil {
		t.Fatal("unregistered event type accepted")
	}
	ok := params("run-a", "known.event")
	if _, err := j.Append(context.Background(), ok); err != nil {
		t.Fatalf("registered event type rejected: %v", err)
	}
}

// A registered payload validator gates admission.
func TestPayloadValidatorEnforced(t *testing.T) {
	dir := t.TempDir()
	j, err := Open(filepath.Join(dir, "journal.db"), "work", redact.None{},
		map[string]PayloadValidator{"typed.event": func(p json.RawMessage) error {
			var v struct {
				Must string `json:"must"`
			}
			if err := json.Unmarshal(p, &v); err != nil || v.Must == "" {
				return fmt.Errorf("payload requires non-empty 'must'")
			}
			return nil
		}})
	if err != nil {
		t.Fatal(err)
	}
	defer j.Close()
	p := params("run-a", "typed.event")
	p.Payload = json.RawMessage(`{"other":1}`)
	if _, err := j.Append(context.Background(), p); err == nil {
		t.Fatal("payload failing its typed validator accepted")
	}
	p.Payload = json.RawMessage(`{"must":"yes"}`)
	if _, err := j.Append(context.Background(), p); err != nil {
		t.Fatalf("valid typed payload rejected: %v", err)
	}
}

// Injected COMMIT failure burns nothing (r2 codex #8: validation-only
// rollback was too weak).
func TestInjectedCommitFailureBurnsNothing(t *testing.T) {
	j := open(t, t.TempDir(), redact.None{})
	testFailCommit = func() error { return fmt.Errorf("injected commit failure") }
	_, err := j.Append(context.Background(), params("run-a", "e1"))
	testFailCommit = nil
	if err == nil {
		t.Fatal("injected commit failure not surfaced")
	}
	ev, err := j.Append(context.Background(), params("run-a", "e2"))
	if err != nil {
		t.Fatal(err)
	}
	if ev.JournalOffset != 1 || ev.Envelope.Sequence != 1 {
		t.Fatalf("failed commit burned allocation: offset=%d seq=%d", ev.JournalOffset, ev.Envelope.Sequence)
	}
}

func TestChainTamperDetected(t *testing.T) {
	dir := t.TempDir()
	j := open(t, dir, redact.None{})
	ctx := context.Background()
	for i := 0; i < 3; i++ {
		if _, err := j.Append(ctx, params("run-a", fmt.Sprintf("e%d", i))); err != nil {
			t.Fatal(err)
		}
	}
	if err := j.VerifyChain(); err != nil {
		t.Fatalf("fresh chain must verify: %v", err)
	}
	// Tamper with each authenticated field class in turn (r2 codex #1:
	// one-field hashing was implementation-derived).
	mutations := map[string]string{
		"envelope": `UPDATE events SET envelope = replace(envelope, 'e1', 'eX') WHERE journal_offset = 2`,
		"run_id":   `UPDATE events SET run_id = 'run-forged' WHERE journal_offset = 2`,
		"sequence": `UPDATE events SET sequence = 99 WHERE journal_offset = 2`,
		"rpv":      `UPDATE events SET redaction_policy_version = 42 WHERE journal_offset = 2`,
		"sealed":   `UPDATE events SET sealed_payload_ref = 'forged' WHERE journal_offset = 2`,
		"event_id": `UPDATE events SET event_id = 'ev-forged' WHERE journal_offset = 2`,
	}
	for name, stmt := range mutations {
		t.Run(name, func(t *testing.T) {
			d2 := t.TempDir()
			jj := open(t, d2, redact.None{})
			for i := 0; i < 3; i++ {
				if _, err := jj.Append(ctx, params("run-a", fmt.Sprintf("e%d", i))); err != nil {
					t.Fatal(err)
				}
			}
			if _, err := jj.db.Exec(stmt); err != nil {
				t.Fatal(err)
			}
			if err := jj.VerifyChain(); err == nil {
				t.Fatalf("tampered %s passed chain verification", name)
			}
			// Replay must ALSO refuse tampered history (enforced-on-read).
			if err := jj.Replay(0, func(Event) error { return nil }); err == nil {
				t.Fatalf("tampered %s served by Replay", name)
			}
		})
	}
	_ = dir
}

// A tampered journal must not even OPEN (enforced at startup).
func TestTamperedJournalRefusesToOpen(t *testing.T) {
	dir := t.TempDir()
	j := open(t, dir, redact.None{})
	ctx := context.Background()
	for i := 0; i < 3; i++ {
		if _, err := j.Append(ctx, params("run-a", fmt.Sprintf("e%d", i))); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := j.db.Exec(`UPDATE events SET envelope = replace(envelope, 'e1', 'eX') WHERE journal_offset = 2`); err != nil {
		t.Fatal(err)
	}
	j.Close()
	if _, err := Open(filepath.Join(dir, "journal.db"), "work", redact.None{}, manyEvents()); err == nil {
		t.Fatal("tampered journal opened successfully")
	}
}

// --- SIGKILL at the commit boundary, barrier-synced (codex #16) ---

func TestMain(m *testing.M) {
	if os.Getenv("NEXUS_J_CHILD") == "1" {
		dir := os.Getenv("NEXUS_J_DIR")
		barrier := os.Getenv("NEXUS_J_BARRIER")
		n := 0
		testPauseAfterCommit = func() {
			n++
			if n == 3 { // exactly after the 3rd commit, INSIDE the window
				os.WriteFile(barrier, []byte("post-commit-3"), 0o600)
				time.Sleep(time.Hour) // parent SIGKILLs here
			}
		}
		j, err := Open(filepath.Join(dir, "journal.db"), "work", redact.None{}, manyEvents())
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		for i := 1; ; i++ {
			if _, err := j.Append(context.Background(), params("run-k", fmt.Sprintf("k%d", i))); err != nil {
				os.Exit(1)
			}
		}
	}
	os.Exit(m.Run())
}

func TestSigkillAfterCommitBoundaryKeepsExactPrefix(t *testing.T) {
	dir := t.TempDir()
	barrier := filepath.Join(dir, "barrier")
	cmd := exec.Command(os.Args[0], "-test.run", "TestMain")
	cmd.Env = append(os.Environ(), "NEXUS_J_CHILD=1", "NEXUS_J_DIR="+dir, "NEXUS_J_BARRIER="+barrier)
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(15 * time.Second)
	for {
		if _, err := os.Stat(barrier); err == nil {
			break
		}
		if time.Now().After(deadline) {
			cmd.Process.Kill()
			t.Fatal("child never reached the post-commit barrier")
		}
		time.Sleep(10 * time.Millisecond)
	}
	cmd.Process.Kill()
	cmd.Wait()
	j := open(t, dir, redact.None{})
	var last uint64
	if err := j.Replay(0, func(ev Event) error {
		if ev.JournalOffset != last+1 {
			return fmt.Errorf("gap after %d", last)
		}
		last = ev.JournalOffset
		return nil
	}); err != nil {
		t.Fatalf("journal corrupted: %v", err)
	}
	// Barrier semantics: the 3rd commit was durable when we killed →
	// EXACTLY 3 events survive (durable flush proof, not "some prefix").
	if last != 3 {
		t.Fatalf("want exactly 3 durable events at the barrier, got %d", last)
	}
	if err := j.VerifyChain(); err != nil {
		t.Fatalf("chain invalid after crash: %v", err)
	}
	if _, err := j.Append(context.Background(), params("run-k", "post-crash")); err != nil {
		t.Fatalf("append after crash recovery failed: %v", err)
	}
}

func shaHex(b []byte) string {
	// tiny local helper mirroring the spec definition (sha256 hex)
	return fmt.Sprintf("%x", sha256Sum(b))
}

func sha256Sum(b []byte) []byte {
	h := sha256.New()
	h.Write(b)
	return h.Sum(nil)
}

// A failed lease release surfaces as a Close error (r4 codex #3).
func TestLeaseDeleteFailureSurfacesOnClose(t *testing.T) {
	j := open(t, t.TempDir(), redact.None{})
	testFailLeaseDelete = func() error { return fmt.Errorf("injected lease-delete failure") }
	defer func() { testFailLeaseDelete = nil }()
	if err := j.Close(); err == nil {
		t.Fatal("Close swallowed the lease-release failure")
	}
}

// Over-budget payload REJECTS the append (redaction cannot fail open).
func TestOverBudgetPayloadRejected(t *testing.T) {
	j := open(t, t.TempDir(), redact.NewKnownRefs(map[string]string{"k": "secret"}))
	p := params("run-a", "e1")
	p.Payload = json.RawMessage(`{"pad":"` + strings.Repeat("a", 1<<20) + `"}`)
	if _, err := j.Append(context.Background(), p); err == nil {
		t.Fatal("over-budget payload accepted")
	}
}

// A failed Open with a FAILING lease release surfaces both errors
// (r5 codex #2 — the sibling of the Close path).
func TestFailedOpenSurfacesLeaseReleaseFailure(t *testing.T) {
	dir := t.TempDir()
	j := open(t, dir, redact.None{})
	ctx := context.Background()
	for i := 0; i < 2; i++ {
		if _, err := j.Append(ctx, params("run-a", fmt.Sprintf("e%d", i))); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := j.db.Exec(`UPDATE events SET envelope = replace(envelope, 'e1', 'eX') WHERE journal_offset = 2`); err != nil {
		t.Fatal(err)
	}
	j.Close()
	testFailLeaseDelete = func() error { return fmt.Errorf("injected release failure") }
	defer func() { testFailLeaseDelete = nil }()
	_, err := Open(filepath.Join(dir, "journal.db"), "work", redact.None{}, manyEvents())
	if err == nil {
		t.Fatal("tampered journal opened")
	}
	if !strings.Contains(err.Error(), "injected release failure") {
		t.Fatalf("lease-release failure swallowed on the failed-open path: %v", err)
	}
}

// The B7 recipe primitive: a batch is ONE transaction — an injected
// commit failure leaves NOTHING durable (never a fired occurrence without
// its admitted run), and a successful batch chains + replays cleanly.
func TestAppendBatchAllOrNothing(t *testing.T) {
	j := open(t, t.TempDir(), redact.None{})
	ctx := context.Background()
	if _, err := j.Append(ctx, params("run-a", "e1")); err != nil {
		t.Fatal(err)
	}
	testFailCommit = func() error { return fmt.Errorf("injected crash before commit") }
	_, err := j.AppendBatch(ctx, []contracts.EnvelopeParams{
		params("run-b", "e2"), params("run-b", "e3"),
	})
	testFailCommit = nil
	if err == nil {
		t.Fatal("batch survived an injected commit failure")
	}
	count := 0
	if err := j.Replay(0, func(Event) error { count++; return nil }); err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("partial batch durable: %d events (want only the pre-batch 1)", count)
	}
	// A successful batch lands atomically and the chain verifies.
	evs, err := j.AppendBatch(ctx, []contracts.EnvelopeParams{
		params("run-b", "e2"), params("run-b", "e3"), params("run-b", "e4"),
	})
	if err != nil || len(evs) != 3 {
		t.Fatalf("batch failed: %v %v", evs, err)
	}
	if evs[1].IntegrityPrevHash != evs[0].IntegrityHash || evs[2].IntegrityPrevHash != evs[1].IntegrityHash {
		t.Fatal("batch events not chained in order")
	}
	if err := j.VerifyChain(); err != nil {
		t.Fatalf("chain broken after batch: %v", err)
	}
	// An invalid member ANYWHERE poisons the whole batch.
	bad := params("run-c", "e5")
	bad.EventType = "never.registered"
	if _, err := j.AppendBatch(ctx, []contracts.EnvelopeParams{params("run-c", "e5"), bad}); err == nil {
		t.Fatal("batch with an invalid member accepted")
	}
	count = 0
	j.Replay(0, func(ev Event) error {
		if ev.Envelope.RunID == "run-c" {
			count++
		}
		return nil
	})
	if count != 0 {
		t.Fatalf("poisoned batch left %d run-c events", count)
	}
}

// SOAK S1 (2026-09-06): the journal's SQLite memory is BOUNDED BY
// DESIGN — a capped connection pool (each pooled connection owns a page
// cache, so unbounded pool = RSS growing with database size) and a
// declared per-connection cache_size.
func TestJournalPoolAndCacheBounded(t *testing.T) {
	j := open(t, t.TempDir(), redact.None{})
	if got := j.db.Stats().MaxOpenConnections; got != 4 {
		t.Fatalf("connection pool unbounded or wrong cap: MaxOpenConnections=%d, want 4", got)
	}
	var cs int
	if err := j.db.QueryRow("PRAGMA cache_size").Scan(&cs); err != nil {
		t.Fatal(err)
	}
	if cs != -2000 {
		t.Fatalf("cache_size not the declared -2000 (2MB/conn): %d", cs)
	}
}
