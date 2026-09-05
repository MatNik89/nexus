//go:build linux

// Soak harness (pre-P1 prep #4, hardened per the prep4 review): run the
// REAL daemon binary for a long wall-clock stretch under OPEN-LOOP
// traffic (fixed arrival rate, replies correlated asynchronously — a
// slowdown builds observable backlog instead of silently lowering the
// offered load) and machine-check the long-run signals:
//
//	S1 RSS budget PER INCARNATION: within each incarnation, the median
//	   of its last third <= 1.5x the median of its first third AND its
//	   peak <= 1.6x its baseline median. This is a window-scoped growth
//	   budget, not a universal leak proof (a sub-budget slow leak needs
//	   a longer run — stated, not hidden).
//	S2 FD budget PER INCARNATION: peak <= incarnation baseline + 10.
//	S3 WAL: MAX VALID sample over the whole run <= 8 MiB; a failed WAL
//	   stat is an INVALID sample (never a healthy zero) and more than
//	   10% invalid samples fails the run.
//	S4 latency: p95 of the last third <= 3x p95 of the first third;
//	   too few valid samples is a FAILURE, not a skip (note: for small
//	   N the p95 index degenerates to the maximum — acceptable here).
//	S5 planned mid-soak restart: cold start on the grown journal <= 60s.
//	S6 end-state HARD: drain timeout fails; zero non-SENT outbox rows
//	   asserted independently; production journal replay clean;
//	   exact-set TERMINAL inbox. Backlog: any message unanswered for
//	   >60s fails immediately (no coordinated omission).
//
// SCOPE (explicit): traffic = telegram conversation replies + periodic
// reminder creation via yolo chat. Memory write/recall and durable
// ASK/HITL suspension are NOT part of this soak — they are covered by
// the T27 acceptance and the chaos harness; this harness measures
// long-run resource and delivery discipline.
//
// Gated: SKIPPED unless NEXUS_SOAK_MINUTES is set.
// The verdict is proven RED-capable on synthetic series.
package acceptance

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

type soakSample struct {
	at          time.Time
	incarnation int
	rssKB       int
	fds         int
	walB        int64
	walValid    bool
	dbB         int64
	backlog     int
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

func fileSizeChecked(p string) (int64, bool) {
	st, err := os.Stat(p)
	if err != nil {
		return 0, false
	}
	return st.Size(), true
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

// soakVerdict applies S1-S4 (latencies passed separately: they belong
// to messages, not process samples).
func soakVerdict(samples []soakSample, latFirst, latLast []int64, warmup, expectedIncarnations int) error {
	if len(samples) < warmup+9 {
		return fmt.Errorf("too few samples (%d) for a verdict (vacuous)", len(samples))
	}
	work := samples[warmup:]
	// S1+S2 PER INCARNATION (prep4 codex/agy: a restart must not launder
	// a leak; the pre-restart peak counts).
	byInc := map[int][]soakSample{}
	for _, s := range work {
		byInc[s.incarnation] = append(byInc[s.incarnation], s)
	}
	// EVERY planned incarnation must be judgeable (prep4-r2 codex #1: a
	// short second incarnation must never exempt its samples).
	for inc := 1; inc <= expectedIncarnations; inc++ {
		if len(byInc[inc]) < 6 {
			return fmt.Errorf("S1/S2 incarnation %d has only %d samples — cannot be judged (fail closed)", inc, len(byInc[inc]))
		}
	}
	for inc, ss := range byInc {
		third := len(ss) / 3
		var rssF, rssAll []int
		for _, s := range ss[:third] {
			rssF = append(rssF, s.rssKB)
		}
		var rssL []int
		for _, s := range ss[len(ss)-third:] {
			rssL = append(rssL, s.rssKB)
		}
		peakRSS, baseFD, peakFD := 0, ss[0].fds, 0
		for _, s := range ss {
			rssAll = append(rssAll, s.rssKB)
			if s.rssKB > peakRSS {
				peakRSS = s.rssKB
			}
			if s.fds > peakFD {
				peakFD = s.fds
			}
		}
		mF, mL := medianInt(rssF), medianInt(rssL)
		if mF <= 0 || mL <= 0 {
			return fmt.Errorf("S1 inc%d RSS sampling broken (medians %d/%d)", inc, mF, mL)
		}
		if mL*2 > mF*3 {
			return fmt.Errorf("S1 inc%d RSS median grew %dKB -> %dKB (>1.5x budget)", inc, mF, mL)
		}
		if peakRSS*5 > mF*8 { // peak > 1.6x baseline median
			return fmt.Errorf("S1 inc%d RSS peak %dKB vs baseline %dKB (>1.6x budget)", inc, peakRSS, mF)
		}
		if baseFD <= 0 || peakFD <= 0 {
			return fmt.Errorf("S2 inc%d FD sampling broken (%d/%d)", inc, baseFD, peakFD)
		}
		if peakFD > baseFD+10 {
			return fmt.Errorf("S2 inc%d FD peak %d vs baseline %d (leak)", inc, peakFD, baseFD)
		}
	}
	// S3: MAX valid WAL over the run; invalid samples bounded.
	invalid, maxWal := 0, int64(0)
	for _, s := range work {
		if !s.walValid {
			invalid++
			continue
		}
		if s.walB > maxWal {
			maxWal = s.walB
		}
	}
	if invalid*10 > len(work) {
		return fmt.Errorf("S3 %d/%d WAL samples invalid (measurement broken)", invalid, len(work))
	}
	if maxWal > 8<<20 {
		return fmt.Errorf("S3 WAL peaked at %d bytes (checkpoint broken)", maxWal)
	}
	// S4: sparse latencies are a FAILURE (prep4 agy).
	if len(latFirst) < 3 || len(latLast) < 3 {
		return fmt.Errorf("S4 too few latency samples (%d/%d) — collection broken", len(latFirst), len(latLast))
	}
	pF, pL := p95Int64(latFirst), p95Int64(latLast)
	if pF <= 0 {
		return fmt.Errorf("S4 latency sampling broken (p95 first=%d)", pF)
	}
	if pL > 3*pF {
		return fmt.Errorf("S4 latency degraded: p95 %dms -> %dms (>3x)", pF, pL)
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
		cmd := w.killableDaemon(t)
		return cmd, time.Since(t0)
	}
	cmd, cold1 := start()
	pid, incarnation := cmd.Process.Pid, 1

	// OPEN-LOOP producer: a DEDICATED goroutine pushes at a fixed 2s
	// rate, independent of correlation, sampling and reminder work
	// (prep4-r2 codex #2 — the offered rate must not sag under load).
	// Latencies carry the message sequence and are sorted by it before
	// the temporal thirds are cut (map-iteration order is not time).
	type sentRec struct {
		seq int
		t0  time.Time
	}
	type latRec struct {
		seq int
		ms  int64
	}
	pushedIDs := map[int64]bool{}
	sentAt := map[string]sentRec{}
	var mu sync.Mutex
	var latencies []latRec
	msg := 0
	prodStop := make(chan struct{})
	prodDone := make(chan struct{})
	go func() {
		defer close(prodDone)
		ticker := time.NewTicker(2 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-prodStop:
				return
			case <-ticker.C:
				mu.Lock()
				msg++
				marker := fmt.Sprintf("soak-msg-%06d", msg)
				id := bot.pushID(marker)
				pushedIDs[id] = true
				sentAt[marker] = sentRec{seq: msg, t0: time.Now()}
				mu.Unlock()
			}
		}
	}()
	collect := func() int {
		mu.Lock()
		defer mu.Unlock()
		for m, rec := range sentAt {
			if bot.countSent("reply-for "+m) >= 1 {
				latencies = append(latencies, latRec{seq: rec.seq, ms: time.Since(rec.t0).Milliseconds()})
				delete(sentAt, m)
				continue
			}
			if time.Since(rec.t0) > 60*time.Second {
				t.Fatalf("soak: %s unanswered for >60s (backlog explosion)", m)
			}
		}
		return len(sentAt)
	}
	var samples []soakSample
	tick := 0
	deadline := time.Now().Add(total)
	half := time.Now().Add(total / 2)
	restarted := false
	var cold2 time.Duration
	for time.Now().Before(deadline) {
		tick++
		backlog := collect()
		if tick%10 == 0 {
			if out, cerr := w.chat("soak remind", true); cerr != nil || !strings.Contains(out, "placed") {
				t.Fatalf("soak: reminder chat failed at tick %d: %v %q", tick, cerr, out)
			}
		}
		walB, walOK := fileSizeChecked(walPath)
		dbB, _ := fileSizeChecked(journalPath)
		samples = append(samples, soakSample{
			at: time.Now(), incarnation: incarnation, rssKB: readRSSKB(pid), fds: countFDs(pid),
			walB: walB, walValid: walOK, dbB: dbB, backlog: backlog,
		})
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
			pid, incarnation = cmd.Process.Pid, 2
			cold2 = d
			if cold2 > 60*time.Second {
				t.Fatalf("S5 cold start on the grown journal took %v (>60s)", cold2)
			}
		}
		time.Sleep(2 * time.Second)
	}
	close(prodStop)
	<-prodDone
	// Let the tail answer, then graceful stop.
	tailDeadline := time.Now().Add(45 * time.Second)
	for time.Now().Before(tailDeadline) {
		if collect() == 0 {
			break
		}
		time.Sleep(300 * time.Millisecond)
	}
	mu.Lock()
	left := len(sentAt)
	mu.Unlock()
	if left > 0 {
		t.Fatalf("soak: %d messages never answered at shutdown", left)
	}
	cmd.Process.Signal(os.Interrupt)
	done := make(chan struct{})
	go func() { cmd.Wait(); close(done) }()
	select {
	case <-done:
	case <-time.After(15 * time.Second):
		cmd.Process.Kill()
		<-done
	}
	// S6 HARD: drain with an explicit timeout FAILURE + independent
	// non-SENT assertion (prep4 codex #1).
	stopF, _ := w.daemon()
	drained := false
	drainDeadline := time.Now().Add(90 * time.Second)
	for time.Now().Before(drainDeadline) {
		notSent, e1 := w.pollQuery("private", `SELECT COUNT(*) FROM chan_outbox WHERE status NOT IN ('SENT')`)
		if e1 == nil && notSent == 0 && w.inboxExactlyTerminal(t, "private", pushedIDs) {
			drained = true
			break
		}
		time.Sleep(500 * time.Millisecond)
	}
	stopF()
	if !drained {
		t.Fatal("S6 drain deadline expired (outbox or inbox never converged)")
	}
	notSent, e1 := w.pollQuery("private", `SELECT COUNT(*) FROM chan_outbox WHERE status NOT IN ('SENT')`)
	if e1 != nil || notSent != 0 {
		t.Fatalf("S6 %d non-SENT outbox rows after the drain (err=%v)", notSent, e1)
	}
	if err := verifyJournalProduction(w, "private"); err != nil {
		t.Fatalf("S6 journal replay after soak: %v", err)
	}
	if !w.inboxExactlyTerminal(t, "private", pushedIDs) {
		t.Fatal("S6 inbox not exact-set TERMINAL after soak")
	}
	sort.Slice(latencies, func(i, j int) bool { return latencies[i].seq < latencies[j].seq })
	third := len(latencies) / 3
	if third == 0 {
		t.Fatalf("soak vacuous: %d latencies", len(latencies))
	}
	msFrom := func(rs []latRec) []int64 {
		out := make([]int64, len(rs))
		for i, r := range rs {
			out[i] = r.ms
		}
		return out
	}
	if verr := soakVerdict(samples, msFrom(latencies[:third]), msFrom(latencies[len(latencies)-third:]), 3, 2); verr != nil {
		t.Fatalf("soak verdict: %v", verr)
	}
	lastS := samples[len(samples)-1]
	maxBacklog := 0
	for _, s := range samples {
		if s.backlog > maxBacklog {
			maxBacklog = s.backlog
		}
	}
	t.Logf("soak: %v, %d msgs, %d reminders, cold1=%v cold2=%v, RSS %dKB, FDs %d, WAL %dB, journal %dB, maxBacklog=%d",
		total, msg, seq, cold1, cold2, lastS.rssKB, lastS.fds, lastS.walB, lastS.dbB, maxBacklog)
}

// The soak verdict must be RED-capable — including the reviewer-proven
// blind spots: pre-restart peaks, mid-run WAL spikes, invalid WAL
// samples, and sparse latencies.
func TestSoakVerdictSensitivity(t *testing.T) {
	mk := func(inc, n int) []soakSample {
		var s []soakSample
		for i := 0; i < n; i++ {
			s = append(s, soakSample{incarnation: inc, rssKB: 50000, fds: 20, walB: 1 << 20, walValid: true})
		}
		return s
	}
	lat := func(v int64, n int) []int64 {
		out := make([]int64, n)
		for i := range out {
			out[i] = v
		}
		return out
	}
	healthy := append(mk(1, 15), mk(2, 15)...)
	if err := soakVerdict(healthy, lat(100, 10), lat(120, 10), 3, 2); err != nil {
		t.Fatalf("healthy series rejected: %v", err)
	}
	// S1 endpoint growth within one incarnation.
	leak := append(mk(1, 15), mk(2, 15)...)
	for i := 10; i < 15; i++ {
		leak[i].rssKB = 90000
	}
	if err := soakVerdict(leak, lat(100, 10), lat(120, 10), 3, 2); err == nil || !strings.Contains(err.Error(), "S1") {
		t.Fatalf("in-incarnation RSS growth not caught: %v", err)
	}
	// S1 PRE-RESTART PEAK laundered by the restart (reviewer probe).
	peak := append(mk(1, 15), mk(2, 15)...)
	peak[12].rssKB = 95000 // spike inside incarnation 1 only
	if err := soakVerdict(peak, lat(100, 10), lat(120, 10), 3, 2); err == nil || !strings.Contains(err.Error(), "S1") {
		t.Fatalf("pre-restart RSS peak not caught: %v", err)
	}
	// S2 FD spike inside the FIRST incarnation (reviewer probe).
	fds := append(mk(1, 15), mk(2, 15)...)
	fds[12].fds = 60
	if err := soakVerdict(fds, lat(100, 10), lat(120, 10), 3, 2); err == nil || !strings.Contains(err.Error(), "S2") {
		t.Fatalf("pre-restart FD spike not caught: %v", err)
	}
	// S3 MID-RUN WAL spike (reviewer probe: end-sample-only was blind).
	wal := append(mk(1, 15), mk(2, 15)...)
	wal[8].walB = 20 << 20
	if err := soakVerdict(wal, lat(100, 10), lat(120, 10), 3, 2); err == nil || !strings.Contains(err.Error(), "S3") {
		t.Fatalf("mid-run WAL spike not caught: %v", err)
	}
	// S3 invalid WAL samples are NEVER healthy zeros (reviewer probe).
	walBad := append(mk(1, 15), mk(2, 15)...)
	for i := range walBad {
		walBad[i].walValid = false
	}
	if err := soakVerdict(walBad, lat(100, 10), lat(120, 10), 3, 2); err == nil || !strings.Contains(err.Error(), "S3") {
		t.Fatalf("invalid WAL samples not caught: %v", err)
	}
	// S4 sparse latencies FAIL (reviewer probe: silent skip before).
	if err := soakVerdict(healthy, lat(100, 2), lat(120, 2), 3, 2); err == nil || !strings.Contains(err.Error(), "S4") {
		t.Fatalf("sparse latencies not caught: %v", err)
	}
	// S4 degradation.
	if err := soakVerdict(healthy, lat(100, 10), lat(900, 10), 3, 2); err == nil || !strings.Contains(err.Error(), "S4") {
		t.Fatalf("latency degradation not caught: %v", err)
	}
	// SHORT second incarnation with insane values must FAIL, not be
	// exempted (prep4-r2 codex #1 probe).
	short := append(mk(1, 24), soakSample{incarnation: 2, rssKB: 200000, fds: 100, walB: 1 << 20, walValid: true},
		soakSample{incarnation: 2, rssKB: 200000, fds: 100, walB: 1 << 20, walValid: true},
		soakSample{incarnation: 2, rssKB: 200000, fds: 100, walB: 1 << 20, walValid: true})
	if err := soakVerdict(short, lat(100, 10), lat(120, 10), 3, 2); err == nil || !strings.Contains(err.Error(), "cannot be judged") {
		t.Fatalf("short insane incarnation not caught: %v", err)
	}
	// Vacuous short series.
	if err := soakVerdict(mk(1, 5), lat(100, 10), lat(100, 10), 3, 1); err == nil {
		t.Fatal("vacuous short series accepted")
	}
}
