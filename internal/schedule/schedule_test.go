//go:build linux

// T20 RED table (tasks-P0): durable one-shot scheduler — persisted
// occurrence (wall time + IANA + dst=ONCE_FIRST + missed_run=COALESCE),
// stable occurrence id, fake-clock seams, catch-up sweep with OVERDUE
// notice, last_occurrence_fired counter, and the B7 recipe
// occurrence+run-admission in ONE transaction. Anchored to PRD §6 item 3
// + HARDQ B1/B7/C8.
package schedule

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/MatNik89/nexus/internal/foundation/clockid"
	"github.com/MatNik89/nexus/internal/kernel/contracts"
	"github.com/MatNik89/nexus/internal/kernel/journal"
	"github.com/MatNik89/nexus/internal/kernel/machine"
	"github.com/MatNik89/nexus/internal/security/redact"
)

func ctxT() context.Context { return context.Background() }

func openSched(t *testing.T, dir string, clock clockid.Clock) (*Scheduler, string) {
	t.Helper()
	path := filepath.Join(dir, "journal.db")
	events := Events()
	for _, n := range machine.EventTypes() {
		events[n] = nil
	}
	j, err := journal.Open(path, "work", redact.None{}, events, NewProjection())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { j.Close() })
	s, err := New(j, clock)
	if err != nil {
		t.Fatal(err)
	}
	return s, path
}

func reopenSched(t *testing.T, path string, clock clockid.Clock) *Scheduler {
	t.Helper()
	events := Events()
	for _, n := range machine.EventTypes() {
		events[n] = nil
	}
	j, err := journal.Open(path, "work", redact.None{}, events, NewProjection())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { j.Close() })
	s, err := New(j, clock)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

// utc builds a fake clock pinned at the given UTC instant.
func utc(y int, mo time.Month, d, h, mi int) time.Time {
	return time.Date(y, mo, d, h, mi, 0, 0, time.UTC)
}

// Restart BEFORE due → fires exactly once, on time, not OVERDUE.
func TestRestartBeforeDueFiresOnce(t *testing.T) {
	clock := clockid.NewFake(utc(2026, 9, 4, 10, 0))
	s, path := openSched(t, t.TempDir(), clock)
	if err := s.CreateReminder(ctxT(), "rem-1", "call mom", WallTime{
		Year: 2026, Month: 9, Day: 4, Hour: 12, Minute: 30, TZ: "Europe/Zagreb",
	}); err != nil {
		t.Fatal(err)
	}
	// Sweep before due: nothing fires.
	fired, err := s.Sweep(ctxT())
	if err != nil || len(fired) != 0 {
		t.Fatalf("premature fire: %v %v", fired, err)
	}
	// "Restart": fresh scheduler over the same journal.
	s.Journal().Close()
	clock.Advance(30*time.Minute + 10*time.Second) // 10s past due: inside one sweep interval
	s2 := reopenSched(t, path, clock)
	fired, err = s2.Sweep(ctxT())
	if err != nil || len(fired) != 1 {
		t.Fatalf("due reminder did not fire after restart: %v %v", fired, err)
	}
	if fired[0].Overdue {
		t.Fatalf("on-time fire marked OVERDUE: %+v", fired[0])
	}
	// Idempotent: further sweeps and restarts fire NOTHING.
	if fired, _ := s2.Sweep(ctxT()); len(fired) != 0 {
		t.Fatalf("double fire: %v", fired)
	}
	s2.Journal().Close()
	s3 := reopenSched(t, path, clock)
	if fired, _ := s3.Sweep(ctxT()); len(fired) != 0 {
		t.Fatalf("refire after second restart: %v", fired)
	}
	if n, _ := s3.LastOccurrenceFired(ctxT()); n != 1 {
		t.Fatalf("counter %d, want 1", n)
	}
}

// Restart AFTER due (missed while down) → fires once WITH the OVERDUE
// notice (missed_run=COALESCE: one catch-up fire, not a burst).
func TestRestartAfterDueFiresOnceOverdue(t *testing.T) {
	clock := clockid.NewFake(utc(2026, 9, 4, 10, 0))
	s, path := openSched(t, t.TempDir(), clock)
	if err := s.CreateReminder(ctxT(), "rem-1", "water plants", WallTime{
		Year: 2026, Month: 9, Day: 4, Hour: 12, Minute: 30, TZ: "Europe/Zagreb",
	}); err != nil {
		t.Fatal(err)
	}
	s.Journal().Close()
	clock.Advance(8 * time.Hour) // daemon was down long past due
	s2 := reopenSched(t, path, clock)
	fired, err := s2.Sweep(ctxT())
	if err != nil || len(fired) != 1 {
		t.Fatalf("missed reminder not caught up: %v %v", fired, err)
	}
	if !fired[0].Overdue {
		t.Fatal("late catch-up fire not marked OVERDUE")
	}
	if fired, _ := s2.Sweep(ctxT()); len(fired) != 0 {
		t.Fatalf("COALESCE violated — second fire: %v", fired)
	}
}

// Zagreb AUTUMN FOLD (2026-10-25: 03:00 CEST → 02:00 CET; 02:30 occurs
// twice): dst=ONCE_FIRST fires exactly once, at the FIRST occurrence.
func TestZagrebAutumnFoldFiresOnce(t *testing.T) {
	// First 02:30 CEST = 00:30 UTC; second 02:30 CET = 01:30 UTC.
	clock := clockid.NewFake(utc(2026, 10, 24, 20, 0))
	s, _ := openSched(t, t.TempDir(), clock)
	if err := s.CreateReminder(ctxT(), "rem-fold", "fold case", WallTime{
		Year: 2026, Month: 10, Day: 25, Hour: 2, Minute: 30, TZ: "Europe/Zagreb",
	}); err != nil {
		t.Fatal(err)
	}
	// At 00:29 UTC: not yet due.
	clock.Advance(4*time.Hour + 29*time.Minute)
	if fired, _ := s.Sweep(ctxT()); len(fired) != 0 {
		t.Fatalf("fired before the FIRST 02:30: %v", fired)
	}
	// At 00:31 UTC (first 02:30 CEST passed): fires once.
	clock.Advance(2 * time.Minute)
	fired, err := s.Sweep(ctxT())
	if err != nil || len(fired) != 1 {
		t.Fatalf("first-occurrence fire missing: %v %v", fired, err)
	}
	// At 01:31 UTC (the SECOND 02:30, now CET): no second fire.
	clock.Advance(time.Hour)
	if fired, _ := s.Sweep(ctxT()); len(fired) != 0 {
		t.Fatalf("fold produced a second fire: %v", fired)
	}
}

// Zagreb SPRING GAP (2026-03-29: 02:00 → 03:00; 02:30 does not exist):
// the policy normalizes FORWARD — fires once at the normalized instant.
func TestZagrebSpringGapPolicyApplied(t *testing.T) {
	clock := clockid.NewFake(utc(2026, 3, 29, 0, 0)) // 01:00 CET
	s, _ := openSched(t, t.TempDir(), clock)
	if err := s.CreateReminder(ctxT(), "rem-gap", "gap case", WallTime{
		Year: 2026, Month: 3, Day: 29, Hour: 2, Minute: 30, TZ: "Europe/Zagreb",
	}); err != nil {
		t.Fatal(err)
	}
	// 02:30 CET does not exist; normalized to 03:30 CEST = 01:30 UTC.
	clock.Advance(time.Hour + 29*time.Minute) // 01:29 UTC
	if fired, _ := s.Sweep(ctxT()); len(fired) != 0 {
		t.Fatalf("fired before the normalized instant: %v", fired)
	}
	clock.Advance(2 * time.Minute) // 01:31 UTC
	fired, err := s.Sweep(ctxT())
	if err != nil || len(fired) != 1 {
		t.Fatalf("gap policy fire missing: %v %v", fired, err)
	}
}

// Suspend-over-due: the machine sleeps across the due instant; the next
// sweep (wake catch-up) fires once with OVERDUE.
func TestSuspendOverDueCatchUpOnWake(t *testing.T) {
	clock := clockid.NewFake(utc(2026, 9, 4, 10, 0))
	s, _ := openSched(t, t.TempDir(), clock)
	if err := s.CreateReminder(ctxT(), "rem-s", "suspended", WallTime{
		Year: 2026, Month: 9, Day: 4, Hour: 12, Minute: 0, TZ: "Europe/Zagreb",
	}); err != nil {
		t.Fatal(err)
	}
	clock.Advance(6 * time.Hour) // slept over the due instant
	fired, err := s.Sweep(ctxT())
	if err != nil || len(fired) != 1 || !fired[0].Overdue {
		t.Fatalf("wake catch-up broken: %v %v", fired, err)
	}
}

// The B7 recipe: occurrence-fire and run-admission are ONE atomic batch —
// replay NEVER shows a fired occurrence without its admitted run, and the
// admitted run folds legally through the canonical run table.
func TestOccurrenceAndRunAdmissionAtomic(t *testing.T) {
	clock := clockid.NewFake(utc(2026, 9, 4, 10, 0))
	s, _ := openSched(t, t.TempDir(), clock)
	if err := s.CreateReminder(ctxT(), "rem-1", "atomic", WallTime{
		Year: 2026, Month: 9, Day: 4, Hour: 11, Minute: 0, TZ: "Europe/Zagreb",
	}); err != nil {
		t.Fatal(err)
	}
	clock.Advance(2 * time.Hour)
	fired, err := s.Sweep(ctxT())
	if err != nil || len(fired) != 1 {
		t.Fatal(err)
	}
	// Replay: every occurrence_fired is immediately followed (same batch)
	// by its run.created + run.admitted, and the run folds legally.
	var runEvents []machine.FoldEvent
	occRuns := map[string]string{}
	if err := s.Journal().Replay(0, func(ev journal.Event) error {
		if ev.Envelope.EventType == EvOccurrenceFired {
			occRuns[string(ev.Envelope.RunID)] = "fired"
		}
		if ev.Envelope.EventType == machine.EvRunCreated || ev.Envelope.EventType == machine.EvRunAdmitted {
			runEvents = append(runEvents, machine.FoldEvent{Type: ev.Envelope.EventType, Offset: ev.JournalOffset})
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if len(occRuns) != 1 || len(runEvents) != 2 {
		t.Fatalf("recipe events missing: occ=%v runs=%v", occRuns, runEvents)
	}
	st, _, err := machine.RunTable().Fold(contracts.RunInvalid, runEvents, nil)
	if err != nil || st != contracts.RunAdmitted {
		t.Fatalf("admitted run folds to %v (%v)", st, err)
	}
	// Stable occurrence id.
	if !strings.HasPrefix(fired[0].OccurrenceID, "occ-rem-1#") {
		t.Fatalf("occurrence id not stable/derived: %q", fired[0].OccurrenceID)
	}
}

// Invalid inputs fail closed.
func TestScheduleValidation(t *testing.T) {
	clock := clockid.NewFake(utc(2026, 9, 4, 10, 0))
	s, _ := openSched(t, t.TempDir(), clock)
	if err := s.CreateReminder(ctxT(), "", "x", WallTime{Year: 2026, Month: 9, Day: 4, Hour: 1, TZ: "Europe/Zagreb"}); err == nil {
		t.Fatal("empty schedule id accepted")
	}
	if err := s.CreateReminder(ctxT(), "r", "", WallTime{Year: 2026, Month: 9, Day: 4, Hour: 1, TZ: "Europe/Zagreb"}); err == nil {
		t.Fatal("empty body accepted")
	}
	if err := s.CreateReminder(ctxT(), "r", "x", WallTime{Year: 2026, Month: 9, Day: 4, Hour: 1, TZ: "Mars/Olympus"}); err == nil {
		t.Fatal("unknown IANA zone accepted")
	}
	if err := s.CreateReminder(ctxT(), "r", "x", WallTime{Year: 2026, Month: 13, Day: 40, Hour: 99, TZ: "Europe/Zagreb"}); err == nil {
		t.Fatal("impossible wall time accepted")
	}
	// Duplicate schedule id refused.
	good := WallTime{Year: 2026, Month: 9, Day: 5, Hour: 9, TZ: "Europe/Zagreb"}
	if err := s.CreateReminder(ctxT(), "r-dup", "x", good); err != nil {
		t.Fatal(err)
	}
	if err := s.CreateReminder(ctxT(), "r-dup", "y", good); err == nil {
		t.Fatal("duplicate schedule id accepted")
	}
}

// The overdue policy IS the sweep interval — exact boundary REDs
// (Phase-4 codex #10).
func TestOverdueBoundaryIsSweepInterval(t *testing.T) {
	mk := func(advance time.Duration) Fired {
		clock := clockid.NewFake(utc(2026, 9, 4, 10, 0))
		s, _ := openSched(t, t.TempDir(), clock)
		if err := s.CreateReminder(ctxT(), "rem-b", "boundary", WallTime{
			Year: 2026, Month: 9, Day: 4, Hour: 12, Minute: 30, TZ: "Europe/Zagreb",
		}); err != nil {
			t.Fatal(err)
		}
		clock.Advance(30*time.Minute + advance) // due at +30m
		fired, err := s.Sweep(ctxT())
		if err != nil || len(fired) != 1 {
			t.Fatalf("sweep: %v %v", fired, err)
		}
		return fired[0]
	}
	if f := mk(DefaultSweepInterval - time.Second); f.Overdue {
		t.Fatal("fire within one sweep interval marked OVERDUE")
	}
	if f := mk(DefaultSweepInterval); !f.Overdue {
		t.Fatal("fire one full interval late not marked OVERDUE")
	}
}

// Lord Howe 30-minute fold (Phase-4 codex #8): dst=ONCE_FIRST holds for
// non-one-hour zones — the EARLIEST instant carrying the wall time wins.
// 2026-04-05: 02:00 LHDT (+11) → 01:30 LHST (+10:30); 01:45 occurs twice
// (14:45 UTC and 15:15 UTC on 2026-04-04).
func TestLordHoweHalfHourFoldFiresFirst(t *testing.T) {
	clock := clockid.NewFake(utc(2026, 4, 4, 10, 0))
	s, _ := openSched(t, t.TempDir(), clock)
	if err := s.CreateReminder(ctxT(), "rem-lh", "half-hour fold", WallTime{
		Year: 2026, Month: 4, Day: 5, Hour: 1, Minute: 45, TZ: "Australia/Lord_Howe",
	}); err != nil {
		t.Fatal(err)
	}
	// 14:44 UTC: before the FIRST 01:45 → nothing.
	clock.Advance(4*time.Hour + 44*time.Minute)
	if fired, _ := s.Sweep(ctxT()); len(fired) != 0 {
		t.Fatalf("fired before the first Lord Howe occurrence: %v", fired)
	}
	// 14:46 UTC: the FIRST 01:45 has passed → exactly one fire.
	clock.Advance(2 * time.Minute)
	fired, err := s.Sweep(ctxT())
	if err != nil || len(fired) != 1 {
		t.Fatalf("ONCE_FIRST failed for a 30-minute fold: %v %v", fired, err)
	}
	// 15:16 UTC (the second 01:45): no refire.
	clock.Advance(30 * time.Minute)
	if fired, _ := s.Sweep(ctxT()); len(fired) != 0 {
		t.Fatalf("half-hour fold double fire: %v", fired)
	}
}

// Impossible civil dates are refused, never normalized (Phase-4 codex #9).
func TestImpossibleCivilDateRefused(t *testing.T) {
	clock := clockid.NewFake(utc(2026, 1, 1, 0, 0))
	s, _ := openSched(t, t.TempDir(), clock)
	if err := s.CreateReminder(ctxT(), "r-feb30", "x", WallTime{
		Year: 2026, Month: 2, Day: 30, Hour: 10, TZ: "Europe/Zagreb",
	}); err == nil {
		t.Fatal("2026-02-30 accepted (would silently normalize into March)")
	}
	// The spring-gap normalization (clock only, same civil date) stays legal.
	if err := s.CreateReminder(ctxT(), "r-gap", "x", WallTime{
		Year: 2026, Month: 3, Day: 29, Hour: 2, Minute: 30, TZ: "Europe/Zagreb",
	}); err != nil {
		t.Fatalf("DST-gap wall time refused: %v", err)
	}
}

// The persisted admission-time instant is the promise (Phase-4 codex #7):
// replayed/rebuilt projections carry the SAME due_utc from the canonical
// payload, and the autumn-fold decision survives a restart between the
// two occurrences (codex #14).
func TestPersistedDueSurvivesRestartBetweenFoldOccurrences(t *testing.T) {
	clock := clockid.NewFake(utc(2026, 10, 24, 20, 0))
	s, path := openSched(t, t.TempDir(), clock)
	if err := s.CreateReminder(ctxT(), "rem-fold", "fold", WallTime{
		Year: 2026, Month: 10, Day: 25, Hour: 2, Minute: 30, TZ: "Europe/Zagreb",
	}); err != nil {
		t.Fatal(err)
	}
	// Fire at the FIRST occurrence (00:31 UTC).
	clock.Advance(4*time.Hour + 31*time.Minute)
	fired, err := s.Sweep(ctxT())
	if err != nil || len(fired) != 1 {
		t.Fatalf("first-occurrence fire: %v %v", fired, err)
	}
	// RESTART between the two occurrences.
	s.Journal().Close()
	clock.Advance(time.Hour) // now past the SECOND 02:30
	s2 := reopenSched(t, path, clock)
	if fired, _ := s2.Sweep(ctxT()); len(fired) != 0 {
		t.Fatalf("restart between fold occurrences refired: %v", fired)
	}
}

// Two CONCURRENT sweeps produce exactly ONE durable occurrence (the
// projection's fired flag aborts the loser's batch; the loser skips it as
// benign — Phase-4 codex #14).
func TestConcurrentSweepsFireOnce(t *testing.T) {
	clock := clockid.NewFake(utc(2026, 9, 4, 10, 0))
	s, _ := openSched(t, t.TempDir(), clock)
	if err := s.CreateReminder(ctxT(), "rem-c", "concurrent", WallTime{
		Year: 2026, Month: 9, Day: 4, Hour: 11, Minute: 0, TZ: "Europe/Zagreb",
	}); err != nil {
		t.Fatal(err)
	}
	clock.Advance(2 * time.Hour)
	type res struct {
		fired []Fired
		err   error
	}
	results := make(chan res, 2)
	for i := 0; i < 2; i++ {
		go func() {
			f, err := s.Sweep(ctxT())
			results <- res{f, err}
		}()
	}
	total := 0
	for i := 0; i < 2; i++ {
		r := <-results
		if r.err != nil {
			t.Fatalf("concurrent sweep errored: %v", r.err)
		}
		total += len(r.fired)
	}
	if total != 1 {
		t.Fatalf("concurrent sweeps produced %d fires (want exactly 1)", total)
	}
	occ := 0
	s.Journal().Replay(0, func(ev journal.Event) error {
		if ev.Envelope.EventType == EvOccurrenceFired {
			occ++
		}
		return nil
	})
	if occ != 1 {
		t.Fatalf("%d durable occurrences (want 1)", occ)
	}
}
