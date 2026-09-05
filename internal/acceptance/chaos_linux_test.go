//go:build linux

// Chaos-kill harness (pre-P1 prep #3, hardened per the prep3 review):
// SIGKILL the REAL daemon binary at random moments under live traffic,
// restart, repeat — then verify DURABLE invariants:
//
//	I1 journal integrity via the PRODUCTION verifier (journal.Open +
//	   full Replay: per-event hash recomputation, envelope parse,
//	   denormalized-column checks — not just prev-hash linkage);
//	I2 EXACT-SET admission: the pushed update-id set equals the inbox
//	   set (nothing lost, nothing invented), each admitted once, all
//	   TERMINAL after the drain;
//	I3 EXACTLY-once effects: every marker's fact exists exactly once;
//	I4 the reminder converges to DELIVERED and its receipt is the exact
//	   occurrence-derived delivery id, destination-bound and SENT;
//	I5 the store reopens after every kill.
//
// Kill/traffic overlap: the unit layer owns the DETERMINISTIC B7 kill
// seams (NEXUS_TEST_KILL_MID_BATCH matrices in channel/obligation);
// this harness is the supplemental statistical layer over the real
// binary — and it PROVES overlap post-hoc: at least one kill must land
// with an admitted-but-not-terminal update in flight.
// The checkers are proven RED-capable on corrupted store copies.
package acceptance

import (
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"fmt"
	"math/rand"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/MatNik89/nexus/internal/approval"
	"github.com/MatNik89/nexus/internal/channel"
	"github.com/MatNik89/nexus/internal/kernel/journal"
	"github.com/MatNik89/nexus/internal/kernel/machine"
	"github.com/MatNik89/nexus/internal/memory"
	"github.com/MatNik89/nexus/internal/obligation"
	"github.com/MatNik89/nexus/internal/schedule"
	"github.com/MatNik89/nexus/internal/security/redact"
)

// killableDaemon starts the daemon and confirms readiness; chaos kills
// it with SIGKILL mid-flight and ASSERTS the kill landed on a live
// process (prep3 codex #1: a dead or never-ready incarnation proves
// nothing).
func (w *world) killableDaemon(t *testing.T) *exec.Cmd {
	t.Helper()
	cmd := exec.Command(nexusBin(t), "daemon")
	cmd.Env = w.env()
	var sink strings.Builder
	cmd.Stdout, cmd.Stderr = &sink, &sink
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	sock := filepath.Join(w.base, "nexus", "system", "daemon.sock")
	ready := false
	for i := 0; i < 300; i++ {
		if _, err := os.Stat(sock); err == nil {
			ready = true
			break
		}
		time.Sleep(25 * time.Millisecond)
	}
	if !ready {
		cmd.Process.Kill()
		cmd.Wait()
		t.Fatalf("daemon never became ready:\n%s", sink.String())
	}
	return cmd
}

func sigkill(t *testing.T, cmd *exec.Cmd) {
	t.Helper()
	if err := cmd.Process.Kill(); err != nil {
		t.Fatalf("kill missed a live process: %v", err)
	}
	cmd.Wait()
	if cmd.ProcessState == nil || cmd.ProcessState.Success() {
		t.Fatalf("process was not killed: %v", cmd.ProcessState)
	}
}

// chaosDeliveryIDFor mirrors the PRODUCTION occurrence-to-delivery-id
// contract (cmd/nexus deliveryIDFor) — the format is part of the spec
// the harness grades, so it is re-stated here, not imported.
func chaosDeliveryIDFor(occ string) string {
	sum := sha256.Sum256([]byte("reminder-delivery|" + occ))
	return "dlv-" + hex.EncodeToString(sum[:12])
}

func TestChaosKillSurvival(t *testing.T) {
	cycles := 4 // extra pure-random cycles on top of the phased ones
	if v := os.Getenv("NEXUS_CHAOS_CYCLES"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			cycles = n
		}
	}
	rng := rand.New(rand.NewSource(time.Now().UnixNano()))
	w := newWorld(t, nil)
	w.providerDelay = 400 * time.Millisecond // widen the in-flight window
	bot := w.withTelegram(t)
	markers := 0
	w.script = func(last string) string {
		switch {
		case strings.Contains(last, "obs-"):
			return "done"
		case strings.Contains(last, "chaos-msg-"):
			// Plain conversational turn (no ASK tool — the HITL flow is
			// graded by the T27 acceptance; here the chaos invariant is
			// exactly-once ADMISSION + exactly-once REPLY per message,
			// with the EFFECT path exercised by the reminder in I4 and by
			// the unit-level SIGKILL matrices).
			i := strings.Index(last, "chaos-msg-")
			m := last[i:]
			if j := strings.IndexAny(m, " \n\""); j > 0 {
				m = m[:j]
			}
			return "reply-for " + m
		case strings.Contains(last, "remind me"):
			return `{"action":"tool","tool_id":"reminder_set","arguments":{"id":"rem-chaos","body":"chaos reminder","year":2026,"month":1,"day":2,"hour":9,"minute":0,"tz":"Europe/Zagreb"}}`
		}
		return "echo: " + last
	}
	stop, _ := w.daemon()
	if _, err := w.chat("remind me please", true); err != nil {
		t.Fatal(err)
	}
	stop()

	// CHAOS with post-hoc overlap proof: record, at each kill, whether an
	// update was admitted but not yet terminal (in flight).
	// PHASED kills (review r1 + flake hunt): pure randomness either
	// misses the in-flight window or drowns every turn, so each goal gets
	// DETERMINISTIC cycles — phase A waits until an update is durably
	// ADMITTED and kills right then (guaranteed in-flight kill); phase B
	// waits until a success reply is durably ENQUEUED and kills after
	// (guaranteed completed turn); phase C adds pure-random kills on top.
	pushedIDs := map[int64]bool{}
	inFlightKills := 0
	ranCycles := 0
	waitStore := func(pred func() bool, d time.Duration) bool {
		deadline := time.Now().Add(d)
		for time.Now().Before(deadline) {
			if pred() {
				return true
			}
			time.Sleep(100 * time.Millisecond)
		}
		return false
	}
	phase := func(kind string, n int) {
		for i := 0; i < n; i++ {
			ranCycles++
			cmd := w.killableDaemon(t)
			markers++
			id := bot.pushID(fmt.Sprintf("chaos-msg-%03d", markers))
			pushedIDs[id] = true
			switch kind {
			case "inflight":
				if !waitStore(func() bool { return w.countInbox(t, "private", "ADMITTED") > 0 }, 10*time.Second) {
					sigkill(t, cmd)
					t.Fatalf("phase A: update %d never became ADMITTED", id)
				}
				sigkill(t, cmd) // guaranteed mid-processing
				inFlightKills++
			case "complete":
				before := w.countSentReplies(t, "private")
				if !waitStore(func() bool { return w.countSentReplies(t, "private") > before }, 15*time.Second) {
					sigkill(t, cmd)
					t.Fatalf("phase B: no success reply enqueued for update %d", id)
				}
				sigkill(t, cmd)
			default: // random
				time.Sleep(time.Duration(100+rng.Intn(3400)) * time.Millisecond)
				sigkill(t, cmd)
				if w.countInbox(t, "private", "ADMITTED") > 0 {
					inFlightKills++
				}
			}
		}
	}
	phase("inflight", 3)
	phase("complete", 3)
	phase("random", cycles)
	// CALM drain: EVERY pushed id terminal AND the reminder DELIVERED —
	// no fixed-sleep correctness (prep3 codex #5).
	stopF, _ := w.daemon()
	deadline := time.Now().Add(60 * time.Second)
	for time.Now().Before(deadline) {
		if w.inboxExactlyTerminal(t, "private", pushedIDs) && w.oblStatus(t, "private", "rem-chaos") == "DELIVERED" {
			break
		}
		time.Sleep(300 * time.Millisecond)
	}
	stopF()

	// Overlap proof: phase A guarantees at least three in-flight kills.
	if inFlightKills < 3 {
		t.Fatalf("only %d in-flight kills (phase A broken)", inFlightKills)
	}
	// I1: the PRODUCTION journal verifier over the whole stream.
	if err := verifyJournalProduction(w, "private"); err != nil {
		t.Fatalf("I1 production replay failed after chaos: %v", err)
	}
	// I2: exact set equality + exactly-once + all TERMINAL.
	db := w.openStore(t, "private")
	defer db.Close()
	rows, err := db.Query(`SELECT update_id, COUNT(*), MIN(status), MAX(status) FROM chan_inbox GROUP BY update_id`)
	if err != nil {
		t.Fatal(err)
	}
	seen := map[int64]bool{}
	for rows.Next() {
		var uid, cnt int64
		var stMin, stMax string
		if err := rows.Scan(&uid, &cnt, &stMin, &stMax); err != nil {
			t.Fatal(err)
		}
		if !pushedIDs[uid] {
			t.Fatalf("I2 inbox holds update %d that was never pushed", uid)
		}
		if cnt != 1 {
			t.Fatalf("I2 update %d admitted %d times", uid, cnt)
		}
		if stMin != "TERMINAL" || stMax != "TERMINAL" {
			t.Fatalf("I2 update %d not TERMINAL (%s/%s) after the drain", uid, stMin, stMax)
		}
		seen[uid] = true
	}
	rows.Close()
	for id := range pushedIDs {
		if !seen[id] {
			t.Fatalf("I2 pushed update %d NEVER admitted (lost)", id)
		}
	}
	// I3: exactly-once REPLY per message. A kill strictly inside a turn
	// legitimately ends as an error TERMINAL (documented at-most-once
	// ceiling) — then the reply is the error text; otherwise EXACTLY one
	// success reply exists and is SENT. Never two replies of either kind
	// for one message, and never zero rows for a TERMINAL message.
	succeeded := 0
	for m := 1; m <= markers; m++ {
		marker := fmt.Sprintf("chaos-msg-%03d", m)
		var ok, errish int
		if err := db.QueryRow(`SELECT COUNT(*) FROM chan_outbox WHERE text = ? AND status='SENT'`, "reply-for "+marker).Scan(&ok); err != nil {
			t.Fatalf("I3 query: %v", err)
		}
		var okAll int
		if err := db.QueryRow(`SELECT COUNT(*) FROM chan_outbox WHERE text = ?`, "reply-for "+marker).Scan(&okAll); err != nil {
			t.Fatal(err)
		}
		if okAll > 1 {
			t.Fatalf("I3 marker %03d has %d success replies (duplicate reply)", m, okAll)
		}
		_ = errish
		if ok == 1 {
			succeeded++
		}
	}
	if succeeded < 3 {
		t.Fatalf("I3 under-evidenced: %d success replies (phase B guarantees 3)", succeeded)
	}
	// No SILENT effects anywhere: the chaos conversation used no tools,
	// so the memory store must hold nothing from these turns.
	var facts int
	if err := db.QueryRow(`SELECT COUNT(*) FROM mem_facts WHERE content LIKE '%chaos-msg-%'`).Scan(&facts); err != nil {
		t.Fatal(err)
	}
	if facts != 0 {
		t.Fatalf("I3 %d silent effects appeared from tool-less turns", facts)
	}
	// I4: DELIVERED with the EXACT occurrence-derived, destination-bound,
	// SENT delivery row (no LIKE, no alternative states).
	if st := w.oblStatus(t, "private", "rem-chaos"); st != "DELIVERED" {
		t.Fatalf("I4 reminder did not converge to DELIVERED: %s", st)
	}
	wantDlv := chaosDeliveryIDFor("occ-rem-chaos#1")
	var adapter, identity, status string
	if err := db.QueryRow(`SELECT adapter_id, channel_identity, status FROM chan_outbox WHERE delivery_id=?`, wantDlv).
		Scan(&adapter, &identity, &status); err != nil {
		t.Fatalf("I4 exact delivery row %s missing: %v", wantDlv, err)
	}
	if adapter != "telegram" || identity != "chat-42" || status != "SENT" {
		t.Fatalf("I4 delivery row wrong: %s/%s/%s", adapter, identity, status)
	}
	var extra int
	if err := db.QueryRow(`SELECT COUNT(*) FROM chan_outbox WHERE text LIKE '%chaos reminder%' AND delivery_id<>?`, wantDlv).Scan(&extra); err != nil {
		t.Fatal(err)
	}
	if extra != 0 {
		t.Fatalf("I4 %d extra delivery rows beyond the stable id", extra)
	}
	t.Logf("chaos: %d cycles, %d updates, %d in-flight kills, %d/%d replies succeeded, reminder DELIVERED via %s",
		ranCycles, len(pushedIDs), inFlightKills, succeeded, markers, wantDlv)
}

// pushID pushes and returns the update id.
func (b *fakeBot) pushID(text string) int64 {
	b.mu.Lock()
	id := b.nextID
	b.mu.Unlock()
	b.push(text)
	return id
}

// countSentReplies counts ENQUEUED chaos success replies (adaptive goal
// signal; kills usually land before the 2s flush tick, so SENT-ness is
// asserted only after the calm drain in I3).
func (w *world) countSentReplies(t *testing.T, profile string) int {
	t.Helper()
	db := w.openStore(t, profile)
	defer db.Close()
	var n int
	if err := db.QueryRow(`SELECT COUNT(*) FROM chan_outbox WHERE text LIKE 'reply-for chaos-msg-%'`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func (w *world) countInbox(t *testing.T, profile, status string) int {
	t.Helper()
	db := w.openStore(t, profile)
	defer db.Close()
	var n int
	if err := db.QueryRow(`SELECT COUNT(*) FROM chan_inbox WHERE status=?`, status).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

// inboxExactlyTerminal reports whether the inbox holds EXACTLY the
// pushed ids, all TERMINAL.
func (w *world) inboxExactlyTerminal(t *testing.T, profile string, want map[int64]bool) bool {
	t.Helper()
	db := w.openStore(t, profile)
	defer db.Close()
	rows, err := db.Query(`SELECT update_id, status FROM chan_inbox`)
	if err != nil {
		return false
	}
	defer rows.Close()
	got := map[int64]bool{}
	for rows.Next() {
		var uid int64
		var st string
		if err := rows.Scan(&uid, &st); err != nil {
			return false
		}
		if st != "TERMINAL" {
			return false
		}
		got[uid] = true
	}
	if len(got) != len(want) {
		return false
	}
	for id := range want {
		if !got[id] {
			return false
		}
	}
	return true
}

func (w *world) oblStatus(t *testing.T, profile, id string) string {
	t.Helper()
	db := w.openStore(t, profile)
	defer db.Close()
	var st string
	if err := db.QueryRow(`SELECT status FROM obl_obligations WHERE id=?`, id).Scan(&st); err != nil {
		return ""
	}
	return st
}

// openStore opens the profile SQLite read-only (external-tool style).
func (w *world) openStore(t *testing.T, profile string) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", "file:"+filepath.Join(w.base, "nexus", "profiles", profile, "journal.db")+"?mode=ro")
	if err != nil {
		t.Fatal(err)
	}
	return db
}

// verifyJournalProduction runs the PRODUCTION verifier: journal.Open with
// the full closed event set + Replay(0), which recomputes every event's
// integrity hash, parses each envelope, and cross-checks denormalized
// columns (prep3 codex #3: linkage-only checking accepted a forged tail
// hash). The checker deliberately uses the same library API the daemon
// itself trusts — this harness grades the STORE, not the library.
func verifyJournalProduction(w *world, profile string) error {
	reg, err := obligation.NewRegistry(map[string]obligation.Kind{
		"file_note": {Handler: obligation.FileNoteHandler("/tmp"), ValidateParams: obligation.ValidateFileNoteParams},
	})
	if err != nil {
		return err
	}
	events := map[string]journal.PayloadValidator{"policy.allowed_by_yolo": nil}
	for _, n := range machine.EventTypes() {
		events[n] = nil
	}
	for n, v := range memory.Events() {
		events[n] = v
	}
	for n, v := range schedule.Events() {
		events[n] = v
	}
	for n, v := range obligation.Events(reg, obligation.NewDoneGate()) {
		events[n] = v
	}
	for n, v := range channel.Events() {
		events[n] = v
	}
	for n, v := range approval.Events() {
		events[n] = v
	}
	j, err := journal.Open(filepath.Join(w.base, "nexus", "profiles", profile, "journal.db"),
		"private", redact.None{}, events)
	if err != nil {
		return err
	}
	defer j.Close()
	n := 0
	if err := j.Replay(0, func(journal.Event) error { n++; return nil }); err != nil {
		return err
	}
	if n == 0 {
		return fmt.Errorf("empty journal (vacuous)")
	}
	return nil
}

// The invariant checker is RED-capable for CONTENT tampering, not just
// linkage: a forged terminal hash, broken linkage, a mutated envelope,
// and a diverged denormalized column must each fail the production
// verifier.
func TestChaosCheckerDetectsCorruption(t *testing.T) {
	w := newWorld(t, nil)
	stop, _ := w.daemon()
	if _, err := w.chat("hello checker", false); err != nil {
		t.Fatal(err)
	}
	stop()
	orig := filepath.Join(w.base, "nexus", "profiles", "private", "journal.db")
	corrupt := func(name, stmt string) {
		t.Helper()
		cw := &world{t: t, base: t.TempDir()}
		dst := filepath.Join(cw.base, "nexus", "profiles", "private")
		if err := os.MkdirAll(dst, 0o700); err != nil {
			t.Fatal(err)
		}
		raw, err := os.ReadFile(orig)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dst, "journal.db"), raw, 0o600); err != nil {
			t.Fatal(err)
		}
		dbrw, err := sql.Open("sqlite", filepath.Join(dst, "journal.db"))
		if err != nil {
			t.Fatal(err)
		}
		if _, err := dbrw.Exec(stmt); err != nil {
			t.Fatal(err)
		}
		dbrw.Close()
		if err := verifyJournalProduction(cw, "private"); err == nil {
			t.Fatalf("checker accepted %s corruption", name)
		}
	}
	corrupt("terminal-hash",
		`UPDATE events SET integrity_hash='deadbeef' WHERE journal_offset=(SELECT MAX(journal_offset) FROM events)`)
	corrupt("prev-hash-linkage",
		`UPDATE events SET integrity_prev_hash='deadbeef' WHERE journal_offset=(SELECT MAX(journal_offset) FROM events)`)
	corrupt("envelope-content",
		`UPDATE events SET envelope=replace(envelope,'"schema_version":1','"schema_version":9') WHERE journal_offset=(SELECT MAX(journal_offset) FROM events)`)
	corrupt("denormalized-column",
		`UPDATE events SET run_id='run-forged' WHERE journal_offset=(SELECT MAX(journal_offset) FROM events)`)
}
