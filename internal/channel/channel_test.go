//go:build linux

// T22 RED table (tasks-P0): transport-neutral durable channel core
// (HARDQ B2/B7, E15) — durable inbox keyed (adapter_id, channel_identity,
// update_id) RECEIVED→ADMITTED→TERMINAL with exactly-once ADMISSION
// (replay returns the existing outcome); transactional outbox with a
// stable delivery id and at-least-once remote delivery;
// sent-but-unrecorded → UNKNOWN→RECONCILING, never blind retry. The two
// concrete B7 recipes: inbox-admission+journal and terminal-result+outbox
// — each ONE transaction, both halves durable or neither.
package channel

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/MatNik89/nexus/internal/kernel/contracts"
	"github.com/MatNik89/nexus/internal/kernel/journal"
	"github.com/MatNik89/nexus/internal/security/redact"
)

func ctxT() context.Context { return context.Background() }

func open(t *testing.T, dir string) (*Core, *journal.Journal) {
	t.Helper()
	j, err := journal.Open(filepath.Join(dir, "journal.db"), "work", redact.None{}, Events(), NewProjection())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { j.Close() })
	c, err := New(j)
	if err != nil {
		t.Fatal(err)
	}
	return c, j
}

func inbound(update int64, text string) Inbound {
	return Inbound{
		AdapterID:       "telegram",
		ChannelIdentity: "chat-42",
		UpdateID:        update,
		Text:            text,
		Profile:         "work",
	}
}

// Exactly-once ADMISSION: the same (adapter, identity, update_id) admitted
// twice returns the EXISTING outcome — no second admission, no second
// journal event; a different update admits independently.
func TestExactlyOnceAdmission(t *testing.T) {
	c, j := open(t, t.TempDir())
	out1, err := c.Admit(ctxT(), inbound(1, "hello"))
	if err != nil {
		t.Fatal(err)
	}
	if out1.Replayed {
		t.Fatal("first admission marked as replay")
	}
	out2, err := c.Admit(ctxT(), inbound(1, "hello"))
	if err != nil {
		t.Fatal(err)
	}
	if !out2.Replayed || out2.MessageID != out1.MessageID {
		t.Fatalf("replayed update did not return the existing outcome: %+v vs %+v", out2, out1)
	}
	count := 0
	j.Replay(0, func(ev journal.Event) error {
		if ev.Envelope.EventType == EvInboundAdmitted {
			count++
		}
		return nil
	})
	if count != 1 {
		t.Fatalf("replay produced %d admission events (want 1)", count)
	}
	if out3, _ := c.Admit(ctxT(), inbound(2, "second")); out3.Replayed {
		t.Fatal("a NEW update was treated as replay")
	}
}

// The B7 recipe inbox-admission+journal: SIGKILL inside the recipe →
// on reopen NO inbound row without its journal event and vice versa.
func TestInboxRecipeAtomicUnderSigkill(t *testing.T) {
	if os.Getenv("CHAN_CRASH_CHILD") == "1" {
		inboxCrashChild()
		return
	}
	dir := t.TempDir()
	cmd := exec.Command(os.Args[0], "-test.run", "TestInboxRecipeAtomicUnderSigkill")
	cmd.Env = append(os.Environ(), "CHAN_CRASH_CHILD=1", "CHAN_CRASH_DIR="+dir,
		"NEXUS_TEST_KILL_MID_BATCH=single")
	if err := cmd.Run(); err == nil {
		t.Fatal("child survived the mid-batch kill seam")
	}
	c, j := open(t, dir)
	rows := c.testInboxRows(t)
	events := 0
	j.Replay(0, func(ev journal.Event) error {
		if ev.Envelope.EventType == EvInboundAdmitted {
			events++
		}
		return nil
	})
	if rows != events {
		t.Fatalf("recipe torn: %d inbox rows vs %d admission events", rows, events)
	}
	// Recovery: the same update admits cleanly now.
	if _, err := c.Admit(ctxT(), inbound(1, "crashy")); err != nil {
		t.Fatalf("post-crash admission: %v", err)
	}
}

func inboxCrashChild() {
	dir := os.Getenv("CHAN_CRASH_DIR")
	j, err := journal.Open(filepath.Join(dir, "journal.db"), "work", redact.None{}, Events(), NewProjection())
	if err != nil {
		os.Exit(1)
	}
	c, err := New(j)
	if err != nil {
		os.Exit(1)
	}
	c.Admit(context.Background(), inbound(1, "crashy")) // dies mid-batch via the seam
	os.Exit(0)
}

// The B7 recipe terminal-result+outbox: enqueueing a reply commits the
// terminal result AND the outbox row together — SIGKILL inside leaves
// both or neither.
func TestOutboxRecipeAtomicUnderSigkill(t *testing.T) {
	if os.Getenv("CHAN_OUTCRASH_CHILD") == "1" {
		outboxCrashChild()
		return
	}
	dir := t.TempDir()
	cmd := exec.Command(os.Args[0], "-test.run", "TestOutboxRecipeAtomicUnderSigkill")
	cmd.Env = append(os.Environ(), "CHAN_OUTCRASH_CHILD=1", "CHAN_CRASH_DIR="+dir,
		"NEXUS_TEST_KILL_MID_BATCH=single")
	if err := cmd.Run(); err == nil {
		t.Fatal("child survived the mid-batch kill seam")
	}
	c, j := open(t, dir)
	pending := len(mustPending(t, c))
	terminals := 0
	j.Replay(0, func(ev journal.Event) error {
		if ev.Envelope.EventType == EvOutboundEnqueued {
			terminals++
		}
		return nil
	})
	if pending != terminals {
		t.Fatalf("recipe torn: %d outbox rows vs %d enqueue events", pending, terminals)
	}
}

func outboxCrashChild() {
	dir := os.Getenv("CHAN_CRASH_DIR")
	j, err := journal.Open(filepath.Join(dir, "journal.db"), "work", redact.None{}, Events(), NewProjection())
	if err != nil {
		os.Exit(1)
	}
	c, err := New(j)
	if err != nil {
		os.Exit(1)
	}
	c.EnqueueReply(context.Background(), "telegram", "chat-42", "work", "the reply") // dies mid-batch
	os.Exit(0)
}

func mustPending(t *testing.T, c *Core) []Outbound {
	t.Helper()
	p, err := c.Pending(ctxT())
	if err != nil {
		t.Fatal(err)
	}
	return p
}

// At-least-once delivery honesty: a transport ERROR keeps the row pending
// for retry; transport ACCEPT marks it sent exactly once; a
// sent-but-unrecorded crash (accept returned, daemon died before the
// journal write) parks the row UNKNOWN → reconciliation, never blind
// retry.
func TestDeliveryHonesty(t *testing.T) {
	c, _ := open(t, t.TempDir())
	if _, err := c.EnqueueReply(ctxT(), "telegram", "chat-42", "work", "hi"); err != nil {
		t.Fatal(err)
	}
	// Transport error → still pending.
	fails := 0
	err := c.Flush(ctxT(), func(o Outbound) error { fails++; return fmt.Errorf("network down") })
	if err == nil {
		t.Fatal("transport failure swallowed")
	}
	if len(mustPending(t, c)) != 1 {
		t.Fatal("failed delivery lost the pending row")
	}
	// Accept → sent exactly once; second flush sends nothing.
	sent := 0
	if err := c.Flush(ctxT(), func(o Outbound) error { sent++; return nil }); err != nil {
		t.Fatal(err)
	}
	if sent != 1 || len(mustPending(t, c)) != 0 {
		t.Fatalf("delivery not exactly-once-marked: sent=%d pending=%d", sent, len(mustPending(t, c)))
	}
	if err := c.Flush(ctxT(), func(o Outbound) error { sent++; return nil }); err != nil {
		t.Fatal(err)
	}
	if sent != 1 {
		t.Fatalf("already-sent row re-delivered: %d", sent)
	}
}

// sent-but-unrecorded: the transport ACCEPTED but the sent-mark append
// failed → the row parks UNKNOWN (never silently retried, never lost).
func TestSentButUnrecordedParksUnknown(t *testing.T) {
	c, _ := open(t, t.TempDir())
	if _, err := c.EnqueueReply(ctxT(), "telegram", "chat-42", "work", "hi"); err != nil {
		t.Fatal(err)
	}
	c.testFailSentMark = true
	if err := c.Flush(ctxT(), func(o Outbound) error { return nil }); err == nil {
		t.Fatal("sent-mark failure swallowed")
	}
	c.testFailSentMark = false
	// The row is UNKNOWN: not pending (no blind retry)...
	if len(mustPending(t, c)) != 0 {
		t.Fatal("UNKNOWN row still pending (blind retry possible)")
	}
	// ...and visible for reconciliation.
	unknown, err := c.Unreconciled(ctxT())
	if err != nil || len(unknown) != 1 {
		t.Fatalf("UNKNOWN row not visible for reconciliation: %v %v", unknown, err)
	}
	// Reconciliation with proof-of-send closes it; proof-of-loss re-pends.
	if err := c.Reconcile(ctxT(), unknown[0].DeliveryID, true); err != nil {
		t.Fatal(err)
	}
	if u, _ := c.Unreconciled(ctxT()); len(u) != 0 {
		t.Fatalf("reconciled row still unknown: %v", u)
	}
}

// Profile binding at admission (B3): the inbound row carries its bound
// profile; an admission for an unbound identity is refused fail-closed
// upstream (T23 adapter RED) — here the CORE refuses an empty profile.
func TestAdmissionRequiresProfile(t *testing.T) {
	c, _ := open(t, t.TempDir())
	in := inbound(1, "x")
	in.Profile = ""
	if _, err := c.Admit(ctxT(), in); err == nil {
		t.Fatal("profile-less admission accepted")
	}
	in.Profile = "private" // journal is bound to work
	if _, err := c.Admit(ctxT(), in); err == nil {
		t.Fatal("cross-profile admission accepted")
	}
}

// Visibility: committed recipe rows are readable in the next same-process
// read (B7 read-your-own-writes).
func TestReadYourOwnWrites(t *testing.T) {
	c, _ := open(t, t.TempDir())
	out, err := c.Admit(ctxT(), inbound(7, "visible"))
	if err != nil {
		t.Fatal(err)
	}
	st, err := c.InboundStatus(ctxT(), out.MessageID)
	if err != nil || st != StateAdmitted {
		t.Fatalf("admitted row not immediately visible: %v %v", st, err)
	}
	if _, err := c.EnqueueReply(ctxT(), "telegram", "chat-42", "work", "r"); err != nil {
		t.Fatal(err)
	}
	if len(mustPending(t, c)) != 1 {
		t.Fatal("enqueued outbox row not immediately visible")
	}
}

// Restart durability: pending outbox rows and admission dedup survive a
// reopen (the whole point of DURABLE channel state).
func TestChannelStateSurvivesRestart(t *testing.T) {
	dir := t.TempDir()
	c, j := open(t, dir)
	if _, err := c.Admit(ctxT(), inbound(1, "before")); err != nil {
		t.Fatal(err)
	}
	if _, err := c.EnqueueReply(ctxT(), "telegram", "chat-42", "work", "pending reply"); err != nil {
		t.Fatal(err)
	}
	j.Close()
	j2, err := journal.Open(filepath.Join(dir, "journal.db"), "work", redact.None{}, Events(), NewProjection())
	if err != nil {
		t.Fatal(err)
	}
	defer j2.Close()
	c2, err := New(j2)
	if err != nil {
		t.Fatal(err)
	}
	if out, err := c2.Admit(ctxT(), inbound(1, "before")); err != nil || !out.Replayed {
		t.Fatalf("dedup lost across restart: %+v %v", out, err)
	}
	if len(mustPending(t, c2)) != 1 {
		t.Fatal("pending outbox row lost across restart")
	}
	_ = contracts.ProfileID("work")
	_ = time.Now
}
