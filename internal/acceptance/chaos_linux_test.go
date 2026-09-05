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
//	I3 one OUTCOME per message: exactly one reply row (success or typed
//	   error), never duplicated, all SENT after the drain;
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
	"encoding/json"
	"fmt"
	"math/rand"
	"net"
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
	// Remove the stale socket so the dial below can only reach the NEW
	// incarnation.
	os.Remove(filepath.Join(w.base, "nexus", "system", "daemon.sock"))
	cmd := exec.Command(nexusBin(t), "daemon")
	cmd.Env = w.env()
	var sink strings.Builder
	cmd.Stdout, cmd.Stderr = &sink, &sink
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	sock := filepath.Join(w.base, "nexus", "system", "daemon.sock")
	ready := false
	for i := 0; i < 400; i++ {
		// CONNECT-readiness (prep3-r2 codex #4): a stale pathname from
		// the killed incarnation is not readiness — only a live listener
		// accepts a dial. The daemon re-creates the socket on start.
		if conn, err := net.Dial("unix", sock); err == nil {
			conn.Close()
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
				if !waitStore(func() bool {
					n, err := w.pollQuery("private", `SELECT COUNT(*) FROM chan_inbox WHERE status='ADMITTED'`)
					return err == nil && n > 0
				}, 10*time.Second) {
					sigkill(t, cmd)
					t.Fatalf("phase A: update %d never became ADMITTED", id)
				}
				sigkill(t, cmd) // guaranteed mid-processing
				inFlightKills++
			case "complete":
				// Baseline accepts ONLY a successful sample (bounded loop).
				before, berr := -1, error(nil)
				for tries := 0; tries < 50; tries++ {
					before, berr = w.pollQuery("private", `SELECT COUNT(*) FROM chan_outbox WHERE text LIKE 'reply-for chaos-msg-%'`)
					if berr == nil {
						break
					}
					if berr != errTransientBusy {
						break
					}
					time.Sleep(100 * time.Millisecond)
				}
				if berr != nil || before < 0 {
					sigkill(t, cmd)
					t.Fatalf("phase B baseline never sampled cleanly: %v", berr)
				}
				if !waitStore(func() bool {
					n, err := w.pollQuery("private", `SELECT COUNT(*) FROM chan_outbox WHERE text LIKE 'reply-for chaos-msg-%'`)
					return err == nil && n > before
				}, 15*time.Second) {
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
	// Phase D (prep3-r2 codex #2): DETERMINISTIC kill inside the
	// remote-accepted-but-unrecorded window — the fake API records the
	// send, signals, and blocks the response while the daemon dies. The
	// row must persist as UNKNOWN (B2: sent-but-unrecorded, no automatic
	// retry); the drain later resolves it through the OWNER's redeliver.
	ranCycles++
	cmdD := w.killableDaemon(t)
	// Quiesce HARD: no pending outbox rows AND no admitted-in-flight
	// turns — under full-suite CPU load late replies from the random
	// phase otherwise keep hitting the armed barrier.
	if !waitStore(func() bool {
		p, err1 := w.pollQuery("private", `SELECT COUNT(*) FROM chan_outbox WHERE status='PENDING'`)
		a, err2 := w.pollQuery("private", `SELECT COUNT(*) FROM chan_inbox WHERE status='ADMITTED'`)
		return err1 == nil && err2 == nil && p == 0 && a == 0
	}, 40*time.Second) {
		sigkill(t, cmdD)
		t.Fatal("phase D: outbox/turns never quiesced")
	}
	markers++
	wantReply := fmt.Sprintf("reply-for chaos-msg-%03d", markers)
	sig, release := bot.armPostAcceptBarrier()
	idD := bot.pushID(fmt.Sprintf("chaos-msg-%03d", markers))
	pushedIDs[idD] = true
	var acceptedText string
	for tries := 0; tries < 10; tries++ {
		select {
		case acceptedText = <-sig:
		case <-time.After(20 * time.Second):
			sigkill(t, cmdD)
			t.Fatal("phase D: flush never hit the armed barrier")
		}
		if acceptedText == wantReply {
			break
		}
		// A stray row (reminder retry etc.) hit the barrier first: let it
		// through and re-arm for ours.
		t.Logf("phase D: stray barrier hit: %q", acceptedText)
		close(release)
		sig, release = bot.armPostAcceptBarrier()
		acceptedText = ""
	}
	if acceptedText != wantReply {
		sigkill(t, cmdD)
		t.Fatalf("phase D: barrier kept catching stray rows, never %q", wantReply)
	}
	sigkill(t, cmdD) // dies with the send accepted but unacknowledged
	close(release)
	unknown, perr := w.pollQuery("private", `SELECT COUNT(*) FROM chan_outbox WHERE status='UNKNOWN' AND text = ?`, wantReply)
	if perr != nil || unknown != 1 {
		t.Fatalf("phase D: accepted-but-unrecorded row not parked UNKNOWN (n=%d err=%v)", unknown, perr)
	}
	acceptedBefore := bot.countSent(wantReply)
	// CALM drain: EVERY pushed id terminal AND the reminder DELIVERED —
	// no fixed-sleep correctness (prep3 codex #5).
	stopF, _ := w.daemon()
	// OBSERVATION INTERVAL first (prep3-r3 codex #1): give the restarted
	// daemon two full flush ticks with NO owner command — the phase-D
	// row must stay UNKNOWN and its acceptance count unchanged (an
	// automatic resend of a possibly-delivered send would show up here).
	time.Sleep(5 * time.Second)
	if got := bot.countSent(wantReply); got != acceptedBefore {
		t.Fatalf("phase D: automatic resend during the observation interval (%d -> %d)", acceptedBefore, got)
	}
	if n, err := w.pollQuery("private", `SELECT COUNT(*) FROM chan_outbox WHERE status='UNKNOWN' AND text = ?`, wantReply); err != nil || n != 1 {
		t.Fatalf("phase D: row left UNKNOWN during observation? n=%d err=%v", n, err)
	}
	// Exactly ONE owner command for the phase-D row, sent HERE (excluded
	// from the generic drain loop below).
	redelivered := map[string]bool{}
	{
		db := w.openStore(t, "private")
		var dID string
		if err := db.QueryRow(`SELECT delivery_id FROM chan_outbox WHERE status='UNKNOWN' AND text = ?`, wantReply).Scan(&dID); err != nil {
			db.Close()
			t.Fatalf("phase D: id lookup: %v", err)
		}
		db.Close()
		redelivered[dID] = true
		cmdID := bot.pushID("redeliver " + dID)
		pushedIDs[cmdID] = true
	}
	deadline := time.Now().Add(90 * time.Second)
	for time.Now().Before(deadline) {
		// E9 UNKNOWN rows are a LEGITIMATE chaos outcome (a kill between
		// remote accept and the sent-mark): the harness plays the OWNER
		// and drives the human `redeliver` command for each (prep3-r2
		// kilo #1) — never expecting the daemon to auto-retry them.
		if n, err := w.pollQuery("private", `SELECT COUNT(*) FROM chan_outbox WHERE status='UNKNOWN'`); err == nil && n > 0 {
			db := w.openStore(t, "private")
			ids := []string{}
			if rows, qerr := db.Query(`SELECT delivery_id FROM chan_outbox WHERE status='UNKNOWN'`); qerr == nil {
				for rows.Next() {
					var id string
					if rows.Scan(&id) == nil {
						ids = append(ids, id)
					}
				}
				rows.Close()
			}
			db.Close()
			for _, id := range ids {
				if redelivered[id] {
					continue // one owner command per row (I3 accounting)
				}
				redelivered[id] = true
				cmdID := bot.pushID("redeliver " + id)
				pushedIDs[cmdID] = true // command updates are real messages
			}
		}
		allSent, serr := w.pollQuery("private", `SELECT COUNT(*) FROM chan_outbox WHERE status NOT IN ('SENT')`)
		if serr == nil && allSent == 0 &&
			w.inboxExactlyTerminal(t, "private", pushedIDs) &&
			w.oblStatus(t, "private", "rem-chaos") == "DELIVERED" {
			break
		}
		time.Sleep(400 * time.Millisecond)
	}
	stopF()

	// Phase D honesty: the daemon must NOT have auto-resent the UNKNOWN
	// row — only the drain's explicit redeliver may produce the second
	// arrival (B2: no blind retry of a possibly-delivered send).
	acceptedAfter := bot.countSent(wantReply)
	if acceptedAfter != acceptedBefore+1 {
		t.Fatalf("phase D: EXACTLY one extra arrival required after ONE owner redeliver (%d -> %d)", acceptedBefore, acceptedAfter)
	}
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
		var okAll int
		if err := db.QueryRow(`SELECT COUNT(*) FROM chan_outbox WHERE text = ?`, "reply-for "+marker).Scan(&okAll); err != nil {
			t.Fatal(err)
		}
		if okAll > 1 {
			t.Fatalf("I3 marker %03d has %d success replies (duplicate reply)", m, okAll)
		}
		var ok int
		if err := db.QueryRow(`SELECT COUNT(*) FROM chan_outbox WHERE text = ? AND status='SENT'`, "reply-for "+marker).Scan(&ok); err != nil {
			t.Fatalf("I3 query: %v", err)
		}
		if okAll == 1 && ok != 1 {
			t.Fatalf("I3 marker %03d reply exists but is not SENT after the drain", m)
		}
		if ok == 1 {
			succeeded++
		}
	}
	// EVERY terminal chaos message owns exactly ONE outcome row: a
	// success reply or the typed handler-error reply (prep3-r2 codex #3).
	var errReplies int
	if err := db.QueryRow(`SELECT COUNT(*) FROM chan_outbox WHERE text LIKE 'I could not process%'`).Scan(&errReplies); err != nil {
		t.Fatal(err)
	}
	chaosMsgs := 0
	for id := range pushedIDs {
		_ = id
		chaosMsgs++
	}
	redeliverCmds := chaosMsgs - markers // drain-driven command updates
	// command updates answer with "Re-queued..." rows; account for them:
	var requeued int
	if err := db.QueryRow(`SELECT COUNT(*) FROM chan_outbox WHERE text LIKE 'Re-queued%'`).Scan(&requeued); err != nil {
		t.Fatal(err)
	}
	if succeeded+errReplies != markers {
		t.Fatalf("I3 outcome accounting broken: %d success + %d error != %d chaos messages", succeeded, errReplies, markers)
	}
	if requeued != redeliverCmds {
		t.Fatalf("I3 %d Re-queued replies for %d redeliver commands", requeued, redeliverCmds)
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
	// The DURABLE receipt EVENT must itself carry the occurrence-derived
	// id and the telegram producer (prep3-r2 codex #1: the outbox row and
	// the obligation state must not merely coexist).
	producer, receipt, derr := deliveredReceipt(w, "private", "occ-rem-chaos#1")
	if derr != nil {
		t.Fatalf("I4 delivered event: %v", derr)
	}
	if producer != "telegram" || receipt != wantDlv {
		t.Fatalf("I4 receipt event diverges: producer=%q receipt=%q want telegram/%s", producer, receipt, wantDlv)
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

// pollQuery runs a single-int query tolerating TRANSIENT SQLite
// busy/locked errors (prep3-r2 codex #5): the poll deadline owns the
// retry budget; non-transient errors surface.
func (w *world) pollQuery(profile, q string, args ...interface{}) (int, error) {
	db, err := sql.Open("sqlite", "file:"+filepath.Join(w.base, "nexus", "profiles", profile, "journal.db")+"?mode=ro")
	if err != nil {
		return 0, err
	}
	defer db.Close()
	var n int
	if err := db.QueryRow(q, args...).Scan(&n); err != nil {
		if strings.Contains(err.Error(), "locked") || strings.Contains(err.Error(), "busy") {
			// SENTINEL, never a fake zero (prep3-r4 codex): a transient
			// lock must not masquerade as a real sample.
			return 0, errTransientBusy
		}
		return 0, err
	}
	return n, nil
}

// errTransientBusy marks a retryable SQLite contention sample.
var errTransientBusy = fmt.Errorf("transient sqlite busy/locked")

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

// deliveredReceipt extracts the durable obligation.delivered event for
// one occurrence via the production replay (payload authenticity rides
// the verified chain).
func deliveredReceipt(w *world, profile, occ string) (producer, receipt string, err error) {
	reg, rerr := obligation.NewRegistry(map[string]obligation.Kind{
		"file_note": {Handler: obligation.FileNoteHandler("/tmp"), ValidateParams: obligation.ValidateFileNoteParams},
	})
	if rerr != nil {
		return "", "", rerr
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
	j, jerr := journal.Open(filepath.Join(w.base, "nexus", "profiles", profile, "journal.db"),
		"private", redact.None{}, events)
	if jerr != nil {
		return "", "", jerr
	}
	defer j.Close()
	found := 0
	err = j.Replay(0, func(ev journal.Event) error {
		if ev.Envelope.EventType != "obligation.delivered" {
			return nil
		}
		var p struct {
			OccurrenceID string `json:"occurrence_id"`
			Producer     string `json:"producer"`
			ReceiptID    string `json:"receipt_id"`
		}
		if e := json.Unmarshal(ev.Envelope.Payload, &p); e != nil {
			return e
		}
		if p.OccurrenceID == occ {
			found++
			producer, receipt = p.Producer, p.ReceiptID
		}
		return nil
	})
	if err != nil {
		return "", "", err
	}
	if found != 1 {
		return "", "", fmt.Errorf("%d delivered events for %s (want 1)", found, occ)
	}
	return producer, receipt, nil
}
