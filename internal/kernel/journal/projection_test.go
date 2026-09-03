//go:build linux

// T07 RED table (tasks-P0): generic projection harness with FAKE domain
// rows. Spec anchors: HARDQ B7 (same-transaction core projections, durable
// async offsets), Annex P0.3 (test_projection_cannot_bypass_journal),
// ledger T07 REDs (bypass / crash-offset / read-your-own-writes).
package journal

import (
	"context"
	"database/sql"
	"fmt"
	"path/filepath"
	"testing"

	"github.com/MatNik89/nexus/internal/security/redact"
)

// fakeCounter is a contract-valid FAKE core-state projection: one row per
// appended event, in the append's own transaction.
type fakeCounter struct {
	failOn   uint64
	failOnce bool // fail the FIRST Apply only (offsets are reused after an
	// aborted append — correctly, since aborts burn nothing)
	failed bool
}

func (f *fakeCounter) Name() string { return "fake_counter" }
func (f *fakeCounter) Init(db *sql.DB) error {
	_, err := db.Exec(`CREATE TABLE IF NOT EXISTS fake_state (
		journal_offset INTEGER PRIMARY KEY, event_type TEXT NOT NULL)`)
	return err
}
func (f *fakeCounter) Apply(tx *sql.Tx, ev Event) error {
	if f.failOnce && !f.failed {
		f.failed = true
		return fmt.Errorf("injected projection failure")
	}
	if f.failOn != 0 && ev.JournalOffset == f.failOn {
		return fmt.Errorf("injected projection failure")
	}
	_, err := tx.Exec(`INSERT INTO fake_state(journal_offset, event_type) VALUES(?,?)`,
		int64(ev.JournalOffset), ev.Envelope.EventType)
	return err
}

func openWithProjection(t *testing.T, dir string, sp SyncProjection) *Journal {
	t.Helper()
	j, err := Open(filepath.Join(dir, "journal.db"), "work", redact.None{}, manyEvents(), sp)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { j.Close() })
	return j
}

func countFake(t *testing.T, j *Journal) int {
	t.Helper()
	var n int
	if err := j.db.QueryRow(`SELECT COUNT(*) FROM fake_state`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

// Read-your-own-writes: state is visible IMMEDIATELY after Append returns —
// same transaction, no async gap (the agy round-1 TOCTOU case).
func TestSyncProjectionVisibleAfterAppend(t *testing.T) {
	j := openWithProjection(t, t.TempDir(), &fakeCounter{})
	ctx := context.Background()
	for i := 1; i <= 3; i++ {
		if _, err := j.Append(ctx, params("run-a", fmt.Sprintf("e%d", i))); err != nil {
			t.Fatal(err)
		}
		if got := countFake(t, j); got != i {
			t.Fatalf("after append %d the projection shows %d rows (must be same-tx visible)", i, got)
		}
	}
}

// Atomicity: a failing sync projection aborts the WHOLE append — no event
// without state, no state without event, no burned allocation.
func TestSyncProjectionFailureAbortsAppend(t *testing.T) {
	j := openWithProjection(t, t.TempDir(), &fakeCounter{failOnce: true})
	ctx := context.Background()
	if _, err := j.Append(ctx, params("run-a", "e1")); err == nil {
		t.Fatal("append with failing sync projection accepted")
	}
	events := 0
	j.Replay(0, func(Event) error { events++; return nil })
	if events != 0 || countFake(t, j) != 0 {
		t.Fatalf("partial commit: events=%d state=%d (must both be 0)", events, countFake(t, j))
	}
	// The failure burned nothing.
	ev, err := j.Append(ctx, params("run-a", "e2"))
	if err != nil {
		t.Fatal(err)
	}
	if ev.JournalOffset != 1 {
		t.Fatalf("aborted append burned offset: %d", ev.JournalOffset)
	}
}

// test_projection_cannot_bypass_journal (P0.3): state rows exist ONLY for
// journaled events — a row planted around the journal is detectable against
// the journal's own history.
func TestProjectionCannotBypassJournal(t *testing.T) {
	j := openWithProjection(t, t.TempDir(), &fakeCounter{})
	ctx := context.Background()
	for i := 1; i <= 2; i++ {
		if _, err := j.Append(ctx, params("run-a", fmt.Sprintf("e%d", i))); err != nil {
			t.Fatal(err)
		}
	}
	// Adversary writes state DIRECTLY, skipping the journal.
	if _, err := j.db.Exec(`INSERT INTO fake_state(journal_offset, event_type) VALUES(99, 'forged')`); err != nil {
		t.Fatal(err)
	}
	// Reconciliation: every state row must correspond to a journaled offset.
	journaled := map[uint64]bool{}
	if err := j.Replay(0, func(ev Event) error {
		journaled[ev.JournalOffset] = true
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	rows, err := j.db.Query(`SELECT journal_offset FROM fake_state`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	orphans := 0
	for rows.Next() {
		var off uint64
		rows.Scan(&off)
		if !journaled[off] {
			orphans++
		}
	}
	if orphans != 1 {
		t.Fatalf("bypass detection broken: want exactly 1 orphan state row, found %d", orphans)
	}
}

// Async projector: the durable offset never advances past durable work —
// an apply failure leaves the offset behind, and a re-run REPLAYS the event.
func TestProjectorOffsetNeverAdvancesPastDurableWork(t *testing.T) {
	j := open(t, t.TempDir(), redact.None{})
	ctx := context.Background()
	for i := 1; i <= 3; i++ {
		if _, err := j.Append(ctx, params("run-a", fmt.Sprintf("e%d", i))); err != nil {
			t.Fatal(err)
		}
	}
	pr, err := j.NewProjector("obs")
	if err != nil {
		t.Fatal(err)
	}
	seen := 0
	err = pr.Run(func(tx *sql.Tx, ev Event) error {
		seen++
		if ev.JournalOffset == 2 {
			return fmt.Errorf("injected apply failure")
		}
		return nil
	})
	if err == nil {
		t.Fatal("injected failure not surfaced")
	}
	off, _ := pr.Offset()
	if off != 1 {
		t.Fatalf("offset advanced past durable work: %d (must be 1)", off)
	}
	// Re-run REPLAYS offset 2 (at-least-once, monotone).
	replayed := []uint64{}
	if err := pr.Run(func(tx *sql.Tx, ev Event) error {
		replayed = append(replayed, ev.JournalOffset)
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if len(replayed) != 2 || replayed[0] != 2 || replayed[1] != 3 {
		t.Fatalf("re-run must process exactly offsets 2,3; got %v", replayed)
	}
	if off, _ := pr.Offset(); off != 3 {
		t.Fatalf("final offset %d, want 3", off)
	}
}

// Two projectors are independent (per-name durable offsets).
func TestProjectorsIndependent(t *testing.T) {
	j := open(t, t.TempDir(), redact.None{})
	ctx := context.Background()
	for i := 1; i <= 2; i++ {
		if _, err := j.Append(ctx, params("run-a", fmt.Sprintf("e%d", i))); err != nil {
			t.Fatal(err)
		}
	}
	a, _ := j.NewProjector("a")
	b, _ := j.NewProjector("b")
	if err := a.Run(func(*sql.Tx, Event) error { return nil }); err != nil {
		t.Fatal(err)
	}
	offA, _ := a.Offset()
	offB, _ := b.Offset()
	if offA != 2 || offB != 0 {
		t.Fatalf("projector offsets not independent: a=%d b=%d", offA, offB)
	}
}
