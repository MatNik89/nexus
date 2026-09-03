// T06 RED table v2 (post Phase-1A review) at the pre-agreed seam
// (Open/Append/Replay/Close + VerifyChain). Spec anchors: Annex P0.3
// (journal offset ≠ per-run sequence, integrity chain, durable flush,
// secret-never-reaches-sink), HARDQ B3 (profile binding), B7 (single
// append actor), C1 (known-ref redaction), review findings #2/#9/#12/#13.
package journal

import (
	"context"
	"crypto/sha256"
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
	dir := t.TempDir()
	secret := "tok-9988776655"
	j := open(t, dir, redact.NewKnownRefs(map[string]string{"tg": secret}))
	p := params("run-a", "x")
	p.ActorID = contracts.ActorID("actor-" + secret)
	if _, err := j.Append(context.Background(), p); err == nil {
		t.Fatal("append with a known secret in a structural field accepted")
	}
	j.Close()
	for _, f := range []string{"journal.db", "journal.db-wal"} {
		if b, err := os.ReadFile(filepath.Join(dir, f)); err == nil && strings.Contains(string(b), secret) {
			t.Fatalf("secret present in sink file %s", f)
		}
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

// A second LIVE opener is refused (single writer, B7); after Close the
// same process may reopen (its lease is its own).
func TestSecondLiveOpenerRefused(t *testing.T) {
	dir := t.TempDir()
	j := open(t, dir, redact.None{})
	_ = j
	// Same process holds the lease; simulate a FOREIGN live writer by
	// planting another live pid (our parent) in the lease.
	j.Close()
	j2, err := Open(filepath.Join(dir, "journal.db"), "work", redact.None{}, manyEvents())
	if err != nil {
		t.Fatalf("reopen after close must succeed: %v", err)
	}
	ppid := os.Getppid()
	st, err := procStartTime(ppid)
	if err != nil {
		t.Skip("no parent stat")
	}
	j2.db.Exec(`UPDATE journal_meta SET value=? WHERE key='writer'`, fmt.Sprintf("%d:%s", ppid, st))
	j2.Close()
	if _, err := Open(filepath.Join(dir, "journal.db"), "work", redact.None{}, manyEvents()); err == nil {
		t.Fatal("journal with a LIVE foreign writer lease reopened")
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
			var v struct{ Must string `json:"must"` }
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
		"envelope":  `UPDATE events SET envelope = replace(envelope, 'e1', 'eX') WHERE journal_offset = 2`,
		"run_id":    `UPDATE events SET run_id = 'run-forged' WHERE journal_offset = 2`,
		"sequence":  `UPDATE events SET sequence = 99 WHERE journal_offset = 2`,
		"rpv":       `UPDATE events SET redaction_policy_version = 42 WHERE journal_offset = 2`,
		"sealed":    `UPDATE events SET sealed_payload_ref = 'forged' WHERE journal_offset = 2`,
		"event_id":  `UPDATE events SET event_id = 'ev-forged' WHERE journal_offset = 2`,
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
