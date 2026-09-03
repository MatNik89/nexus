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
		RunID: contracts.RunID(run), EmittedAt: time.Unix(1000, 0), ActorType: "user",
		ActorID: "a", PrincipalID: "p", WorkspaceID: "w", ProfileID: "work",
		AttemptNo: 1, Payload: json.RawMessage(`{"e":"` + event + `"}`), PayloadHash: "recomputed-by-journal",
	}
}

func open(t *testing.T, dir string, r redact.Redactor) *Journal {
	t.Helper()
	j, err := Open(filepath.Join(dir, "journal.db"), "work", r)
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

// Secrets in NON-payload fields must also never reach the sink (codex #10).
func TestSecretInNonPayloadFieldNeverReachesSink(t *testing.T) {
	dir := t.TempDir()
	secret := "tok-9988776655"
	j := open(t, dir, redact.NewKnownRefs(map[string]string{"tg": secret}))
	p := params("run-a", "x")
	p.EventType = "notify-" + secret
	if _, err := j.Append(context.Background(), p); err != nil {
		t.Fatal(err)
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
	if _, err := Open(filepath.Join(t.TempDir(), "j.db"), "work", nil); err == nil {
		t.Fatal("nil redactor accepted")
	}
	if _, err := Open(filepath.Join(t.TempDir(), "j.db"), "", redact.None{}); err == nil {
		t.Fatal("empty profile binding accepted")
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
	// Tamper with a stored envelope directly.
	if _, err := j.db.Exec(`UPDATE events SET envelope = replace(envelope, 'e1', 'eX') WHERE journal_offset = 2`); err != nil {
		t.Fatal(err)
	}
	if err := j.VerifyChain(); err == nil {
		t.Fatal("tampered journal passed chain verification")
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
		j, err := Open(filepath.Join(dir, "journal.db"), "work", redact.None{})
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
