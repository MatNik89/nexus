//go:build linux

// Soak harness (pre-P1 prep #4): run the REAL daemon binary for a long
// wall-clock stretch under steady traffic and watch the properties no
// short test can see — memory growth, FD leaks, WAL discipline, journal
// growth, reply-latency degradation, and cold-start time on a grown
// journal. One planned mid-soak restart exercises full replay at size.
//
// Gated: SKIPPED unless NEXUS_SOAK_MINUTES is set (reviewers run short
// values, e.g. 2-5; the real 24-48h run uses 1440-2880 via nohup).
//
// Signals (each with a hard threshold, machine-checked):
//
//	S1 RSS: median of the last third <= 1.5x median of the first third
//	   (after warmup) — a steady leak fails;
//	S2 FDs: final count <= baseline + 10;
//	S3 WAL: final size <= 8 MiB (auto-checkpoint must keep it bounded);
//	S4 latency: p95 of the last third <= 3x p95 of the first third;
//	S5 mid-soak restart on the grown journal completes readiness within
//	   60s and the SECOND cold start is recorded for the report;
//	S6 end-state: production journal replay clean + exact-set inbox
//	   TERMINAL + every reply row SENT (reusing the chaos checkers).
//
// The metrics collector is proven RED-capable by a synthetic series test.
package acceptance

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"
)

type soakSample struct {
	at      time.Time
	rssKB   int
	fds     int
	walB    int64
	dbB     int64
	replyMS int64
}

func readRSSKB(pid int) int {
	raw, err := os.ReadFile(fmt.Sprintf("/proc/%d/status", pid))
	if err != nil {
		return -1
	}
	for _, ln := range strings.Split(string(raw), "\n") {
		if strings.HasPrefix(ln, "VmRSS:") {
			f := strings.Fields(ln)
			if len(f) >= 2 {
				n, _ := strconv.Atoi(f[1])
				return n
			}
		}
	}
	return -1
}

func countFDs(pid int) int {
	ents, err := os.ReadDir(fmt.Sprintf("/proc/%d/fd", pid))
	if err != nil {
		return -1
	}
	return len(ents)
}

func fileSize(p string) int64 {
	st, err := os.Stat(p)
	if err != nil {
		return 0
	}
	return st.Size()
}

func medianInt(xs []int) int {
	if len(xs) == 0 {
		return 0
	}
	s := append([]int{}, xs...)
	sort.Ints(s)
	return s[len(s)/2]
}

func p95Int64(xs []int64) int64 {
	if len(xs) == 0 {
		return 0
	}
	s := append([]int64{}, xs...)
	sort.Slice(s, func(i, j int) bool { return s[i] < s[j] })
	return s[(len(s)*95)/100]
}

// soakVerdict applies S1-S4 to a sample series (separated so its own
// sensitivity is testable without hours of wall clock).
func soakVerdict(samples []soakSample, warmup int) error {
	if len(samples) < warmup+9 {
		return fmt.Errorf("too few samples (%d) for a verdict (vacuous)", len(samples))
	}
	work := samples[warmup:]
	third := len(work) / 3
	first, last := work[:third], work[len(work)-third:]
	var rssF, rssL []int
	var latF, latL []int64
	for _, s := range first {
		rssF = append(rssF, s.rssKB)
		if s.replyMS > 0 {
			latF = append(latF, s.replyMS)
		}
	}
	for _, s := range last {
		rssL = append(rssL, s.rssKB)
		if s.replyMS > 0 {
			latL = append(latL, s.replyMS)
		}
	}
	mF, mL := medianInt(rssF), medianInt(rssL)
	if mF <= 0 || mL <= 0 {
		return fmt.Errorf("RSS sampling broken (medians %d/%d)", mF, mL)
	}
	if mL*2 > mF*3 { // mL > 1.5*mF without floats
		return fmt.Errorf("S1 RSS grew %dKB -> %dKB (leak signal, >1.5x)", mF, mL)
	}
	fd0, fdN := first[0].fds, last[len(last)-1].fds
	if fd0 <= 0 || fdN <= 0 {
		return fmt.Errorf("FD sampling broken (%d/%d)", fd0, fdN)
	}
	if fdN > fd0+10 {
		return fmt.Errorf("S2 FDs grew %d -> %d (leak)", fd0, fdN)
	}
	walN := last[len(last)-1].walB
	if walN > 8<<20 {
		return fmt.Errorf("S3 WAL unbounded: %d bytes at the end (checkpoint broken)", walN)
	}
	if len(latF) >= 3 && len(latL) >= 3 {
		pF, pL := p95Int64(latF), p95Int64(latL)
		if pF > 0 && pL > 3*pF {
			return fmt.Errorf("S4 latency degraded: p95 %dms -> %dms (>3x)", pF, pL)
		}
	}
	return nil
}

func TestSoakSurvival(t *testing.T) {
	minStr := os.Getenv("NEXUS_SOAK_MINUTES")
	if minStr == "" {
		t.Skip("soak is explicit: set NEXUS_SOAK_MINUTES (e.g. 3 for a smoke, 1440 for the real run)")
	}
	minutes, err := strconv.Atoi(minStr)
	if err != nil || minutes < 1 {
		t.Fatalf("NEXUS_SOAK_MINUTES: %v", err)
	}
	total := time.Duration(minutes) * time.Minute
	w := newWorld(t, nil)
	bot := w.withTelegram(t)
	seq := 0
	w.script = func(last string) string {
		switch {
		case strings.Contains(last, "obs-"):
			return "placed"
		case strings.Contains(last, "soak-msg-"):
			i := strings.Index(last, "soak-msg-")
			m := last[i:]
			if j := strings.IndexAny(m, " \n\""); j > 0 {
				m = m[:j]
			}
			return "reply-for " + m
		case strings.Contains(last, "soak remind"):
			seq++
			return fmt.Sprintf(`{"action":"tool","tool_id":"reminder_set","arguments":{"id":"rem-soak-%d","body":"soak reminder %d","year":2026,"month":1,"day":2,"hour":9,"minute":0,"tz":"Europe/Zagreb"}}`, seq, seq)
		}
		return "echo: " + last
	}
	journalPath := filepath.Join(w.base, "nexus", "profiles", "private", "journal.db")
	walPath := journalPath + "-wal"

	start := func() (*exec.Cmd, time.Duration) {
		t0 := time.Now()
		cmd := w.killableDaemon(t) // dial-verified readiness
		return cmd, time.Since(t0)
	}
	cmd, cold1 := start()
	pid := cmd.Process.Pid

	pushedIDs := map[int64]bool{}
	var samples []soakSample
	msg := 0
	reminderEvery := 10 // every 10th tick sets a reminder through chat
	deadline := time.Now().Add(total)
	half := time.Now().Add(total / 2)
	restarted := false
	var cold2 time.Duration
	tick := 0
	for time.Now().Before(deadline) {
		tick++
		msg++
		marker := fmt.Sprintf("soak-msg-%06d", msg)
		sentAt := time.Now()
		id := bot.pushID(marker)
		pushedIDs[id] = true
		// latency = push -> reply arrives at the bot
		lat := int64(-1)
		latDeadline := time.Now().Add(30 * time.Second)
		for time.Now().Before(latDeadline) {
			if bot.countSent("reply-for "+marker) >= 1 {
				lat = time.Since(sentAt).Milliseconds()
				break
			}
			time.Sleep(150 * time.Millisecond)
		}
		if lat < 0 {
			t.Fatalf("soak: reply for %s never arrived within 30s (tick %d)", marker, tick)
		}
		if tick%reminderEvery == 0 {
			if out, cerr := w.chat("soak remind", true); cerr != nil || !strings.Contains(out, "placed") {
				t.Fatalf("soak: reminder chat failed at tick %d: %v %q", tick, cerr, out)
			}
		}
		samples = append(samples, soakSample{
			at: time.Now(), rssKB: readRSSKB(pid), fds: countFDs(pid),
			walB: fileSize(walPath), dbB: fileSize(journalPath), replyMS: lat,
		})
		// S5: one planned restart at half-time — full replay on the grown
		// journal, cold-start bounded.
		if !restarted && time.Now().After(half) {
			restarted = true
			cmd.Process.Signal(os.Interrupt)
			done := make(chan struct{})
			go func() { cmd.Wait(); close(done) }()
			select {
			case <-done:
			case <-time.After(15 * time.Second):
				cmd.Process.Kill()
				<-done
			}
			var d time.Duration
			cmd, d = start()
			pid = cmd.Process.Pid
			cold2 = d
			if cold2 > 60*time.Second {
				t.Fatalf("S5 cold start on the grown journal took %v (>60s)", cold2)
			}
		}
		time.Sleep(2 * time.Second)
	}
	// graceful stop, then S6 end-state checks (reuse the chaos checkers).
	cmd.Process.Signal(os.Interrupt)
	done := make(chan struct{})
	go func() { cmd.Wait(); close(done) }()
	select {
	case <-done:
	case <-time.After(15 * time.Second):
		cmd.Process.Kill()
		<-done
	}
	// drain leftovers with one calm incarnation
	stopF, _ := w.daemon()
	drainDeadline := time.Now().Add(60 * time.Second)
	for time.Now().Before(drainDeadline) {
		notSent, e1 := w.pollQuery("private", `SELECT COUNT(*) FROM chan_outbox WHERE status NOT IN ('SENT')`)
		if e1 == nil && notSent == 0 && w.inboxExactlyTerminal(t, "private", pushedIDs) {
			break
		}
		time.Sleep(500 * time.Millisecond)
	}
	stopF()
	if err := verifyJournalProduction(w, "private"); err != nil {
		t.Fatalf("S6 journal replay after soak: %v", err)
	}
	if !w.inboxExactlyTerminal(t, "private", pushedIDs) {
		t.Fatal("S6 inbox not exact-set TERMINAL after soak")
	}
	if verr := soakVerdict(samples, 3); verr != nil {
		t.Fatalf("soak verdict: %v", verr)
	}
	lastS := samples[len(samples)-1]
	t.Logf("soak: %v, %d msgs, %d reminders, cold1=%v cold2=%v, RSS %dKB, FDs %d, WAL %dB, journal %dB",
		total, msg, seq, cold1, cold2, lastS.rssKB, lastS.fds, lastS.walB, lastS.dbB)
}

// The soak verdict itself must be RED-capable on synthetic series.
func TestSoakVerdictSensitivity(t *testing.T) {
	base := func() []soakSample {
		var s []soakSample
		for i := 0; i < 30; i++ {
			s = append(s, soakSample{rssKB: 50000, fds: 20, walB: 1 << 20, replyMS: 100})
		}
		return s
	}
	if err := soakVerdict(base(), 3); err != nil {
		t.Fatalf("healthy series rejected: %v", err)
	}
	leak := base()
	for i := 20; i < 30; i++ {
		leak[i].rssKB = 90000 // >1.5x
	}
	if err := soakVerdict(leak, 3); err == nil || !strings.Contains(err.Error(), "S1") {
		t.Fatalf("RSS leak not caught: %v", err)
	}
	fds := base()
	fds[29].fds = 60
	if err := soakVerdict(fds, 3); err == nil || !strings.Contains(err.Error(), "S2") {
		t.Fatalf("FD leak not caught: %v", err)
	}
	wal := base()
	wal[29].walB = 20 << 20
	if err := soakVerdict(wal, 3); err == nil || !strings.Contains(err.Error(), "S3") {
		t.Fatalf("WAL growth not caught: %v", err)
	}
	slow := base()
	for i := 20; i < 30; i++ {
		slow[i].replyMS = 900 // >3x p95
	}
	if err := soakVerdict(slow, 3); err == nil || !strings.Contains(err.Error(), "S4") {
		t.Fatalf("latency degradation not caught: %v", err)
	}
	if err := soakVerdict(base()[:5], 3); err == nil {
		t.Fatal("vacuous short series accepted")
	}
}
