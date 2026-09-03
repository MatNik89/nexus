// T06 RED table (tasks-P0), written FIRST at the pre-agreed seam
// (Open/Append/Replay/Close — the ledger's own contract): replay fold
// reproduces state; concurrent appends yield contiguous sequences; a known
// secret never reaches the sink (the DB file IS the sink, so grepping it is
// a spec assertion, not implementation coupling); SIGKILL at a commit
// boundary leaves a contiguous prefix (old-or-new, never a gap or half).
package journal

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/MatNik89/nexus/internal/kernel/contracts"
	"github.com/MatNik89/nexus/internal/security/redact"
)

func params(event string) contracts.EnvelopeParams {
	return contracts.EnvelopeParams{
		SchemaID: "nexus.event", SchemaVersion: 1,
		EventID: contracts.EventID("ev-" + event), EventType: event,
		RunID: "run-1", EmittedAt: time.Unix(1000, 0), ActorType: "user",
		ActorID: "a", PrincipalID: "p", WorkspaceID: "w", ProfileID: "work",
		AttemptNo: 1, Payload: json.RawMessage(`{"e":"` + event + `"}`), PayloadHash: "h",
	}
}

func open(t *testing.T, dir string, r redact.Redactor) *Journal {
	t.Helper()
	j, err := Open(filepath.Join(dir, "journal.db"), r)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { j.Close() })
	return j
}

func TestAppendReplayRoundTrip(t *testing.T) {
	j := open(t, t.TempDir(), redact.None{})
	for i := 1; i <= 3; i++ {
		env, err := j.Append(context.Background(), params(fmt.Sprintf("e%d", i)))
		if err != nil {
			t.Fatal(err)
		}
		if env.Sequence != uint64(i) { // spec: sequence strictly increases per run
			t.Fatalf("want sequence %d, got %d", i, env.Sequence)
		}
	}
	var got []contracts.Envelope
	if err := j.Replay(0, func(e contracts.Envelope) error {
		got = append(got, e)
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if len(got) != 3 || got[0].EventType != "e1" || got[2].EventType != "e3" {
		t.Fatalf("replay does not reproduce the appended events: %+v", got)
	}
}

func TestConcurrentAppendsContiguous(t *testing.T) {
	j := open(t, t.TempDir(), redact.None{})
	const workers, per = 4, 25
	var wg sync.WaitGroup
	for w := 0; w < workers; w++ {
		wg.Add(1)
		go func(w int) {
			defer wg.Done()
			for i := 0; i < per; i++ {
				if _, err := j.Append(context.Background(), params(fmt.Sprintf("w%d-%d", w, i))); err != nil {
					t.Errorf("append: %v", err)
					return
				}
			}
		}(w)
	}
	wg.Wait()
	seqs := map[uint64]bool{}
	if err := j.Replay(0, func(e contracts.Envelope) error {
		seqs[e.Sequence] = true
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if len(seqs) != workers*per {
		t.Fatalf("lost events: want %d, got %d", workers*per, len(seqs))
	}
	for i := uint64(1); i <= uint64(workers*per); i++ {
		if !seqs[i] {
			t.Fatalf("sequence gap at %d", i)
		}
	}
}

func TestKnownSecretNeverReachesSink(t *testing.T) {
	dir := t.TempDir()
	secret := "sk-SUPER-SECRET-VALUE-12345"
	j := open(t, dir, redact.NewKnownRefs(map[string]string{"provider_key": secret}))
	p := params("leak")
	p.Payload = json.RawMessage(`{"text":"my key is ` + secret + `"}`)
	if _, err := j.Append(context.Background(), p); err != nil {
		t.Fatal(err)
	}
	// Seam check: replay must show the redacted marker, not the secret.
	if err := j.Replay(0, func(e contracts.Envelope) error {
		if strings.Contains(string(e.Payload), secret) {
			t.Fatal("secret visible through Replay")
		}
		if !strings.Contains(string(e.Payload), "[REDACTED:provider_key]") {
			t.Fatal("redaction marker missing")
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	// Sink check (spec P0.3: secret-never-reaches-sink; the files ARE the sink).
	j.Close()
	for _, f := range []string{"journal.db", "journal.db-wal"} {
		b, err := os.ReadFile(filepath.Join(dir, f))
		if err != nil {
			continue
		}
		if strings.Contains(string(b), secret) {
			t.Fatalf("secret present in sink file %s", f)
		}
	}
}

func TestAppendAfterCloseFails(t *testing.T) {
	j := open(t, t.TempDir(), redact.None{})
	j.Close()
	if _, err := j.Append(context.Background(), params("late")); err == nil {
		t.Fatal("append after Close accepted")
	}
}

func TestAppendRejectsInvalidEnvelope(t *testing.T) {
	j := open(t, t.TempDir(), redact.None{})
	p := params("bad")
	p.ProfileID = "" // B3 violation must be rejected by the same P0.1 validation
	if _, err := j.Append(context.Background(), p); err == nil {
		t.Fatal("invalid envelope accepted by Append")
	}
}

// --- SIGKILL at the commit boundary (child-process pattern) ---

func TestMain(m *testing.M) {
	if os.Getenv("NEXUS_J_CHILD") == "1" {
		dir := os.Getenv("NEXUS_J_DIR")
		j, err := Open(filepath.Join(dir, "journal.db"), redact.None{})
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		for i := 1; ; i++ {
			if _, err := j.Append(context.Background(), params(fmt.Sprintf("k%d", i))); err != nil {
				os.Exit(1)
			}
		}
	}
	os.Exit(m.Run())
}

func TestSigkillAtCommitBoundaryLeavesContiguousPrefix(t *testing.T) {
	dir := t.TempDir()
	cmd := exec.Command(os.Args[0], "-test.run", "TestMain")
	cmd.Env = append(os.Environ(), "NEXUS_J_CHILD=1", "NEXUS_J_DIR="+dir)
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	time.Sleep(300 * time.Millisecond) // let it commit an arbitrary number
	cmd.Process.Kill()
	cmd.Wait()
	j := open(t, dir, redact.None{})
	var last uint64
	if err := j.Replay(0, func(e contracts.Envelope) error {
		if e.Sequence != last+1 {
			return fmt.Errorf("gap: after %d comes %d", last, e.Sequence)
		}
		if want := "k" + strconv.FormatUint(e.Sequence, 10); e.EventType != want {
			return fmt.Errorf("content mismatch at %d: %s", e.Sequence, e.EventType)
		}
		last = e.Sequence
		return nil
	}); err != nil {
		t.Fatalf("journal corrupted by SIGKILL: %v", err)
	}
	if last == 0 {
		t.Fatal("oracle vacuous: no events committed before the kill")
	}
	// The journal must remain APPENDABLE after crash recovery.
	if _, err := j.Append(context.Background(), params("post-crash")); err != nil {
		t.Fatalf("append after crash recovery failed: %v", err)
	}
}
