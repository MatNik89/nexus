//go:build linux

// T27 acceptance harness (tasks-P0): ALL SIX PRD §6 done-criteria proven
// END TO END against the REAL `nexus` binary as a black box — the
// harness lives OUTSIDE the implementation it grades (checker ≠ worker).
// Per-criterion sensitivity: a controlled feature-off switch turns
// exactly that criterion RED (never by reverting the check itself).
// The yolo F2 integration RED (hostile containment unchanged under
// --yolo) lives here with the real backend.
package acceptance

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	_ "modernc.org/sqlite"
	"time"
)

// --- harness plumbing (outside the graded implementation) ---

var (
	harnessDir string
	binPath    string
	binErr     error
)

// TestMain owns one PRIVATE package-lifetime build dir (T27 codex #5: a
// fixed basename under the shared TMPDIR let a concurrent export replace
// the graded artifact after sync.Once bound it). NEXUS_ACCEPT_BIN
// overrides with a pre-built binary (the attestation script pins the
// exact bytes it hashes).
func TestMain(m *testing.M) {
	var err error
	harnessDir, err = os.MkdirTemp("", "nexus-acceptance-*")
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}
	if pre := os.Getenv("NEXUS_ACCEPT_BIN"); pre != "" {
		binPath = pre
	} else {
		binPath = filepath.Join(harnessDir, "nexus")
		cmd := exec.Command("go", "build", "-buildvcs=false", "-o", binPath, "github.com/MatNik89/nexus/cmd/nexus")
		cmd.Env = append(os.Environ(), "CGO_ENABLED=0")
		if out, berr := cmd.CombinedOutput(); berr != nil {
			binErr = fmt.Errorf("%v\n%s", berr, out)
		}
	}
	code := m.Run()
	os.RemoveAll(harnessDir)
	os.Exit(code)
}

func nexusBin(t *testing.T) string {
	t.Helper()
	if binErr != nil {
		t.Fatalf("building nexus: %v", binErr)
	}
	return binPath
}

// world is one black-box NEXUS installation: its own XDG base, scripted
// provider, and (optionally) a fake Bot API.
type world struct {
	t         *testing.T
	base      string // XDG_CONFIG_HOME
	provider  *httptest.Server
	script    func(last string) string
	stepMu    sync.Mutex
	step      int
	bot       *fakeBot
	extraEnv  []string
	pinnedBin string
	genN      int
	// providerDelay stretches every provider turn (chaos harness: widens
	// the admitted-but-not-terminal window so random kills provably land
	// in flight).
	providerDelay time.Duration
}

func newWorld(t *testing.T, execAllow []string) *world {
	t.Helper()
	w := &world{t: t, base: t.TempDir()}
	w.provider = httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
		var req struct {
			Messages []struct{ Role, Content string } `json:"messages"`
		}
		json.NewDecoder(r.Body).Decode(&req)
		last := ""
		if len(req.Messages) > 0 {
			last = req.Messages[len(req.Messages)-1].Content
		}
		if w.providerDelay > 0 {
			time.Sleep(w.providerDelay)
		}
		reply := "echo: " + last
		if strings.Contains(last, "ping") && len(last) < 40 {
			reply = "pong" // startup provider probe (fires on EVERY daemon start)
		} else if w.script != nil {
			reply = w.script(last)
		}
		b, _ := json.Marshal(map[string]any{"choices": []any{map[string]any{
			"message": map[string]string{"role": "assistant", "content": reply}}}})
		rw.Write(b)
	}))
	t.Cleanup(w.provider.Close)
	host := strings.TrimPrefix(w.provider.URL, "http://")
	allowJSON, _ := json.Marshal(execAllow)
	cfg := fmt.Sprintf(`{"provider_base_url":%q,"provider_key_env":"NEXUS_ACCEPT_KEY",
		"provider_model":"m","egress_allow":[%q],"default_profile":"private",
		"telegram_token_env":"NEXUS_ACCEPT_TG","exec_allow":%s}`,
		w.provider.URL, host, allowJSON)
	nexusDir := filepath.Join(w.base, "nexus")
	if err := os.MkdirAll(nexusDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(nexusDir, "config.json"), []byte(cfg), 0o600); err != nil {
		t.Fatal(err)
	}
	return w
}

func (w *world) env() []string {
	base := append(os.Environ(),
		"XDG_CONFIG_HOME="+w.base,
		"NEXUS_ACCEPT_KEY=sk-accept")
	return append(base, w.extraEnv...)
}

// daemon starts `nexus daemon` and waits for the socket; returns stop().
// Output is captured to a sink but never read back (F4: reading out.String()
// while the subprocess still writes was a data race / nil panic; all callers
// discarded it). os/exec serializes writes when Stdout==Stderr, so the sink
// itself is race-free; we simply never read it during the run.
func (w *world) daemon() func() {
	w.t.Helper()
	cmd := exec.Command(nexusBin(w.t), "daemon")
	cmd.Env = w.env()
	var out strings.Builder
	cmd.Stdout, cmd.Stderr = &out, &out
	if err := cmd.Start(); err != nil {
		w.t.Fatal(err)
	}
	sock := filepath.Join(w.base, "nexus", "system", "daemon.sock")
	for i := 0; i < 200; i++ {
		if _, err := os.Stat(sock); err == nil {
			break
		}
		time.Sleep(25 * time.Millisecond)
	}
	stopped := false
	stop := func() {
		if stopped {
			return
		}
		stopped = true
		cmd.Process.Signal(os.Interrupt)
		done := make(chan struct{})
		go func() { cmd.Wait(); close(done) }()
		select {
		case <-done:
		case <-time.After(5 * time.Second):
			cmd.Process.Kill()
			<-done
		}
		// Read ONLY after Wait (F4): the builder is quiescent here.
		if w.t.Failed() {
			w.t.Logf("daemon output:\n%s", out.String())
		}
	}
	w.t.Cleanup(stop)
	return stop
}

// chat runs `nexus chat` (optionally --yolo) feeding input lines.
func (w *world) chat(input string, yolo bool) (string, error) {
	w.t.Helper()
	args := []string{"chat"}
	if yolo {
		args = append(args, "--yolo")
	}
	cmd := exec.Command(nexusBin(w.t), args...)
	cmd.Env = w.env()
	cmd.Stdin = strings.NewReader(input + "\n/quit\n")
	out, err := cmd.CombinedOutput()
	return string(out), err
}

// fakeBot is a minimal Bot API for criterion 4.
type fakeBot struct {
	srv     *httptest.Server
	mu      sync.Mutex
	updates []map[string]any
	sent    []string
	nextID  int64
	// Post-accept kill barrier (prep3-r2 codex #2): when armed, the next
	// sendMessage RECORDS acceptance, signals the harness, then blocks
	// the HTTP response — the daemon is killed inside the exact
	// remote-accepted-but-unacknowledged window.
	blockArmed  bool
	blockSignal chan string
	blockHold   chan struct{}
}

func newFakeBot(t *testing.T) *fakeBot {
	b := &fakeBot{nextID: 1}
	b.srv = httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/getMe"):
			rw.Write([]byte(`{"ok":true,"result":{"id":1,"is_bot":true,"username":"acceptance_bot"}}`))
		case strings.HasSuffix(r.URL.Path, "/getUpdates"):
			// OFFSET-FAITHFUL like the real Bot API (prep3 reviews): an
			// update stays pending until the client's offset passes it —
			// a crash after fetch REDELIVERS instead of losing the batch.
			var req struct {
				Offset int64 `json:"offset"`
			}
			json.NewDecoder(r.Body).Decode(&req)
			b.mu.Lock()
			kept := b.updates[:0]
			var batch []map[string]any
			for _, u := range b.updates {
				id, _ := u["update_id"].(int64)
				if id >= req.Offset {
					kept = append(kept, u)
					batch = append(batch, u)
				}
			}
			b.updates = kept
			b.mu.Unlock()
			out, _ := json.Marshal(map[string]any{"ok": true, "result": batch})
			rw.Write(out)
		case strings.HasSuffix(r.URL.Path, "/sendMessage"):
			var req struct {
				Text string `json:"text"`
			}
			json.NewDecoder(r.Body).Decode(&req)
			b.mu.Lock()
			b.sent = append(b.sent, req.Text)
			armed := b.blockArmed
			if armed {
				b.blockArmed = false
			}
			sig, hold := b.blockSignal, b.blockHold
			b.mu.Unlock()
			if armed {
				// Accepted — but the response never reaches the daemon
				// in time: the harness kills it at this barrier.
				if sig != nil {
					sig <- req.Text
				}
				if hold != nil {
					select {
					case <-hold:
					case <-time.After(30 * time.Second):
					}
				}
			}
			rw.Write([]byte(`{"ok":true,"result":{}}`))
		}
	}))
	t.Cleanup(b.srv.Close)
	return b
}

// armPostAcceptBarrier arms the next sendMessage to signal-and-block.
func (b *fakeBot) armPostAcceptBarrier() (signal chan string, release chan struct{}) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.blockSignal = make(chan string, 1)
	b.blockHold = make(chan struct{})
	b.blockArmed = true
	return b.blockSignal, b.blockHold
}

func (b *fakeBot) push(text string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.updates = append(b.updates, map[string]any{
		"update_id": b.nextID, "message": map[string]any{
			"message_id": b.nextID, "chat": map[string]any{"id": int64(42), "type": "private"},
			"from": map[string]any{"id": int64(42)}, "text": text}})
	b.nextID++
}

func (b *fakeBot) waitSent(t *testing.T, substr string, timeout time.Duration) string {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		b.mu.Lock()
		for _, s := range b.sent {
			if strings.Contains(s, substr) {
				b.mu.Unlock()
				return s
			}
		}
		b.mu.Unlock()
		time.Sleep(100 * time.Millisecond)
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	t.Fatalf("no outbound message containing %q; sent=%v", substr, b.sent)
	return ""
}

// rewriteConfig mutates the world's config.json fields (profile switch,
// telegram wiring) between daemon incarnations.
func (w *world) rewriteConfig(t *testing.T, mut func(map[string]any)) {
	t.Helper()
	path := filepath.Join(w.base, "nexus", "config.json")
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var cfg map[string]any
	if err := json.Unmarshal(raw, &cfg); err != nil {
		t.Fatal(err)
	}
	mut(cfg)
	out, _ := json.Marshal(cfg)
	if err := os.WriteFile(path, out, 0o600); err != nil {
		t.Fatal(err)
	}
}

// withTelegram wires a fake Bot API + owner binding into the world.
func (w *world) withTelegram(t *testing.T) *fakeBot {
	t.Helper()
	w.bot = newFakeBot(t)
	w.rewriteConfig(t, func(cfg map[string]any) {
		cfg["telegram_api_base"] = w.bot.srv.URL
	})
	w.extraEnv = append(w.extraEnv,
		"NEXUS_ACCEPT_TG=123:accept-token",
		"NEXUS_TELEGRAM_BINDINGS=42=private")
	return w.bot
}

// --- Criterion 3: reminder fires on time, survives shutdown, delivers ---

func TestCriterion3ReminderDeliversAfterRestart(t *testing.T) {
	w := newWorld(t, nil)
	bot := w.withTelegram(t)
	w.script = func(last string) string {
		if strings.Contains(last, "obs-") {
			return "reminder placed"
		}
		if strings.Contains(last, "remind me to water") {
			return `{"action":"tool","tool_id":"reminder_set","arguments":{"id":"rem-accept","body":"water the plants","year":2026,"month":1,"day":2,"hour":9,"minute":0,"tz":"Europe/Zagreb"}}`
		}
		return "reminder placed"
	}
	stop := w.daemon()
	out, err := w.chat("remind me to water the plants", true)
	if err != nil || !strings.Contains(out, "reminder placed") {
		t.Fatalf("reminder_set failed: %v %q", err, out)
	}
	stop() // full shutdown BEFORE the due time is honored on restart
	stop2 := w.daemon()
	defer stop2()
	// The restarted daemon sweeps the overdue occurrence, the delivery
	// loop pushes it to the owner chat, and the ack closes it.
	delivered := bot.waitSent(t, "water the plants", 20*time.Second)
	if !strings.Contains(delivered, "occ-rem-accept#1") {
		t.Fatalf("delivery does not carry the exact occurrence id (B5): %q", delivered)
	}
	bot.push("ack occ-rem-accept#1")
	bot.waitSent(t, "Acknowledged occ-rem-accept#1", 20*time.Second)
}

// SENSITIVITY: no bound channel (feature-off) → nothing delivers.
func TestCriterion3SensitivityNoChannel(t *testing.T) {
	w := newWorld(t, nil)
	w.script = func(last string) string {
		if strings.Contains(last, "obs-") {
			return "reminder placed"
		}
		if strings.Contains(last, "remind me") {
			return `{"action":"tool","tool_id":"reminder_set","arguments":{"id":"rem-silent","body":"silent reminder","year":2026,"month":1,"day":2,"hour":9,"minute":0,"tz":"Europe/Zagreb"}}`
		}
		return "reminder placed"
	}
	stop := w.daemon()
	if out, err := w.chat("remind me", true); err != nil || !strings.Contains(out, "reminder placed") {
		t.Fatalf("set failed: %v %q", err, out)
	}
	stop()
	// Incarnation 2: NO channel. The occurrence fires and must stay
	// DELIVERY_PENDING — no fake receipt without a wire (fail closed).
	stop2 := w.daemon()
	time.Sleep(3 * time.Second)
	stop2()
	// Incarnation 3: NOW wire a channel. If the channel-less incarnation
	// had minted a false receipt, nothing would deliver here; the
	// occurrence arriving proves it stayed durably PENDING (black-box
	// proof of the no-channel fail-closed hold — codex #6).
	bot := w.withTelegram(t)
	stop3 := w.daemon()
	defer stop3()
	late := bot.waitSent(t, "silent reminder", 25*time.Second)
	if !strings.Contains(late, "occ-rem-silent#1") {
		t.Fatalf("late delivery lacks the exact occurrence id: %q", late)
	}
}

// --- Criterion 4: Telegram round trip + HITL approval before an
// irreversible action ---

func TestCriterion4TelegramHITL(t *testing.T) {
	w := newWorld(t, nil)
	bot := w.withTelegram(t)
	w.script = func(last string) string {
		if strings.Contains(last, "remembered") || strings.Contains(last, "obs-") {
			return "done from phone"
		}
		if strings.Contains(last, "remember the telegram fact") {
			return `{"action":"tool","tool_id":"memory_remember","arguments":{"content":"telegram fact"}}`
		}
		return "done from phone"
	}
	w.daemon()
	bot.push("remember the telegram fact")
	challenge := bot.waitSent(t, "APPROVAL NEEDED", 20*time.Second)
	id := challenge[strings.Index(challenge, "[ch-")+1:]
	id = id[:strings.IndexByte(id, ']')]
	bot.push("approve " + id)
	bot.waitSent(t, "done from phone", 20*time.Second)
}

// SENSITIVITY: unbound chat (feature-off) → refused, nothing admitted.
func TestCriterion4SensitivityUnboundChat(t *testing.T) {
	w := newWorld(t, nil)
	bot := w.withTelegram(t)
	// THE SWITCH: bindings name a different chat.
	for i, e := range w.extraEnv {
		if strings.HasPrefix(e, "NEXUS_TELEGRAM_BINDINGS=") {
			w.extraEnv[i] = "NEXUS_TELEGRAM_BINDINGS=999=private"
		}
	}
	w.daemon()
	bot.push("hello from an unbound chat")
	refusal := bot.waitSent(t, "not bound", 20*time.Second)
	if strings.Contains(refusal, "echo:") {
		t.Fatalf("unbound chat got a real reply: %q", refusal)
	}
}

// --- Criterion 5: work/private isolation (zero leakage) ---

func TestCriterion5ProfileIsolation(t *testing.T) {
	w := newWorld(t, nil)
	w.script = func(last string) string {
		switch {
		case strings.Contains(last, "remembered"):
			return "saved in work"
		case strings.Contains(last, "obs-"):
			return "recalled: " + last
		case strings.Contains(last, "remember the WORKSECRET"):
			return `{"action":"tool","tool_id":"memory_remember","arguments":{"content":"the WORKSECRET is X9"}}`
		case strings.Contains(last, "what is the WORKSECRET"):
			return `{"action":"tool","tool_id":"memory_recall","arguments":{"query":"WORKSECRET"}}`
		default:
			return "recalled: " + last
		}
	}
	w.rewriteConfig(t, func(cfg map[string]any) { cfg["default_profile"] = "work" })
	stop := w.daemon()
	if out, err := w.chat("remember the WORKSECRET", true); err != nil || !strings.Contains(out, "saved in work") {
		t.Fatalf("work remember failed: %v %q", err, out)
	}
	stop()
	// Switch the daemon to the PRIVATE profile over the SAME install.
	w.rewriteConfig(t, func(cfg map[string]any) { cfg["default_profile"] = "private" })
	stop2 := w.daemon()
	defer stop2()
	out2, err := w.chat("what is the WORKSECRET?", false)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out2, "X9") {
		t.Fatalf("PROFILE LEAK: work fact visible in private: %q", out2)
	}
}

// SENSITIVITY: same profile on both sides → the fact IS visible (the
// check detects real recall, so isolation-off turns criterion 5 RED).
func TestCriterion5SensitivitySameProfile(t *testing.T) {
	w := newWorld(t, nil)
	w.script = func(last string) string {
		switch {
		case strings.Contains(last, "remembered"):
			return "saved"
		case strings.Contains(last, "obs-"):
			return "recalled: " + last
		case strings.Contains(last, "remember it"):
			return `{"action":"tool","tool_id":"memory_remember","arguments":{"content":"the SHAREDSECRET is Z1"}}`
		case strings.Contains(last, "what is the SHAREDSECRET"):
			return `{"action":"tool","tool_id":"memory_recall","arguments":{"query":"SHAREDSECRET"}}`
		default:
			return "recalled: " + last
		}
	}
	stop := w.daemon()
	if out, err := w.chat("remember it", true); err != nil || !strings.Contains(out, "saved") {
		t.Fatalf("remember failed: %v %q", err, out)
	}
	stop()
	stop2 := w.daemon() // SAME profile — the switch
	defer stop2()
	out2, _ := w.chat("what is the SHAREDSECRET?", false)
	if !strings.Contains(out2, "Z1") {
		t.Fatalf("criterion-5 detector cannot see same-profile recall (insensitive): %q", out2)
	}
}

// --- Criterion 1: useful conversation on Linux ---

func TestCriterion1Conversation(t *testing.T) {
	w := newWorld(t, nil)
	stop := w.daemon()
	defer stop()
	out, err := w.chat("hello nexus", false)
	if err != nil {
		t.Fatalf("chat: %v\n%s", err, out)
	}
	if !strings.Contains(out, "echo:") || !strings.Contains(out, "hello nexus") {
		t.Fatalf("no model reply through the real binary: %q", out)
	}
}

// SENSITIVITY: kill the provider (feature-off) → criterion 1 RED.
func TestCriterion1SensitivityProviderDown(t *testing.T) {
	w := newWorld(t, nil)
	w.provider.Close() // the switch: no provider
	stop := w.daemon()
	defer stop()
	out, _ := w.chat("hello", false)
	if strings.Contains(out, "echo:") && strings.Contains(out, "hello") {
		t.Fatalf("criterion-1 check insensitive: reply without a provider: %q", out)
	}
}

// --- Criterion 2: memory survives a full restart ---

func TestCriterion2MemoryAcrossRestart(t *testing.T) {
	w := newWorld(t, nil)
	w.script = func(last string) string {
		switch {
		case strings.Contains(last, "remembered"):
			return "saved"
		case strings.Contains(last, "obs-"):
			return "recalled: " + last
		case strings.Contains(last, "remember the ACCEPTFACT"):
			return `{"action":"tool","tool_id":"memory_remember","arguments":{"content":"the ACCEPTFACT is 41"}}`
		case strings.Contains(last, "what is the ACCEPTFACT"):
			return `{"action":"tool","tool_id":"memory_recall","arguments":{"query":"ACCEPTFACT"}}`
		default:
			return "recalled: " + last
		}
	}
	stop := w.daemon()
	out, err := w.chat("remember the ACCEPTFACT", true) // yolo: ASK auto-allows
	if err != nil || !strings.Contains(out, "saved") {
		t.Fatalf("remember failed: %v %q", err, out)
	}
	stop() // FULL restart
	stop2 := w.daemon()
	defer stop2()
	out2, err := w.chat("what is the ACCEPTFACT?", false)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out2, "ACCEPTFACT is 41") {
		t.Fatalf("memory did not survive the restart: %q", out2)
	}
}

// SENSITIVITY: wipe the data dir between sessions → criterion 2 RED.
func TestCriterion2SensitivityFreshStore(t *testing.T) {
	w := newWorld(t, nil)
	w.script = func(last string) string {
		switch {
		case strings.Contains(last, "remembered"):
			return "saved"
		case strings.Contains(last, "obs-"):
			return "recalled: " + last
		case strings.Contains(last, "remember the LOSTFACT"):
			return `{"action":"tool","tool_id":"memory_remember","arguments":{"content":"the LOSTFACT is 7"}}`
		case strings.Contains(last, "what is the LOSTFACT"):
			return `{"action":"tool","tool_id":"memory_recall","arguments":{"query":"LOSTFACT"}}`
		default:
			return "recalled: " + last
		}
	}
	stop := w.daemon()
	if out, err := w.chat("remember the LOSTFACT", true); err != nil || !strings.Contains(out, "saved") {
		t.Fatalf("remember failed: %v %q", err, out)
	}
	stop()
	// THE SWITCH: destroy the profile store (simulates no durable memory).
	os.RemoveAll(filepath.Join(w.base, "nexus", "profiles"))
	stop2 := w.daemon()
	defer stop2()
	out2, _ := w.chat("what is the LOSTFACT?", false)
	if strings.Contains(out2, "LOSTFACT is 7") {
		t.Fatalf("criterion-2 check insensitive: fact recalled from a wiped store: %q", out2)
	}
}

// --- Criterion 6: sandboxed execution (with the yolo F2 RED) ---

func TestCriterion6SandboxedExec(t *testing.T) {
	requireBwrap(t)
	w := newWorld(t, []string{"/bin/ls"})
	w.script = func(last string) string {
		if strings.Contains(last, "exit status") || strings.Contains(last, "obs-") {
			return "ran: " + last
		}
		if strings.Contains(last, "list root") {
			return `{"action":"tool","tool_id":"exec","arguments":{"command":"/bin/ls","args":["/"]}}`
		}
		return "ran: " + last
	}
	stop := w.daemon()
	defer stop()
	out, err := w.chat("list root", true) // yolo confirms the ASK
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "exit status: 0") {
		t.Fatalf("sandboxed exec did not run through the binary: %q", out)
	}
}

// HOSTILE + F2: under --yolo the containment is UNCHANGED — a canary
// outside the closure stays unreadable and an un-promoted interpreter
// stays refused (yolo bypasses confirmations, never the sandbox).
func TestCriterion6HostileUnchangedUnderYolo(t *testing.T) {
	requireBwrap(t)
	helper := buildHelper(t)
	canary := filepath.Join(t.TempDir(), "canary-secret")
	if err := os.WriteFile(canary, []byte("YOLO-CANARY"), 0o644); err != nil {
		t.Fatal(err)
	}
	w := newWorld(t, []string{helper})
	w.script = func(last string) string {
		if strings.Contains(last, "exit status") || strings.Contains(last, "obs-") {
			return "result: " + last
		}
		if strings.Contains(last, "read the canary") {
			args, _ := json.Marshal(map[string]any{"command": helper, "args": []string{"readfile", canary}})
			return `{"action":"tool","tool_id":"exec","arguments":` + string(args) + `}`
		}
		return "result: " + last
	}
	stop := w.daemon()
	defer stop()
	out, err := w.chat("read the canary", true) // YOLO — containment must hold
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out, "YOLO-CANARY") {
		t.Fatalf("yolo weakened containment — canary bytes escaped: %q", out)
	}
	// Interpreter still refused under yolo (deny-default allowlist).
	w2 := newWorld(t, []string{helper})
	w2.script = func(last string) string {
		if strings.Contains(last, "exit status") || strings.Contains(last, "obs-") || strings.Contains(last, "failed") {
			return "result: " + last
		}
		if strings.Contains(last, "run a shell") {
			return `{"action":"tool","tool_id":"exec","arguments":{"command":"/bin/sh","args":["-c","echo PWNED"]}}`
		}
		return "result: " + last
	}
	stop2 := w2.daemon()
	defer stop2()
	out2, _ := w2.chat("run a shell", true)
	if strings.Contains(out2, "PWNED") {
		t.Fatalf("yolo executed an un-promoted interpreter: %q", out2)
	}
}

// SENSITIVITY: no usable bwrap (feature-off via PATH) → exec refused.
func TestCriterion6SensitivityNoSandbox(t *testing.T) {
	w := newWorld(t, []string{"/bin/ls"})
	w.script = func(last string) string {
		if strings.Contains(last, "obs-") || strings.Contains(last, "failed") {
			return "ran: " + last
		}
		if strings.Contains(last, "list root") {
			return `{"action":"tool","tool_id":"exec","arguments":{"command":"/bin/ls","args":["/"]}}`
		}
		return "ran: " + last
	}
	w.extraEnv = []string{"PATH=/nonexistent-path-for-acceptance"}
	// The daemon needs core utils on PATH? It execs bwrap by probed path;
	// with PATH empty Detect fails -> exec capability OFF.
	stop := w.daemon()
	defer stop()
	out, _ := w.chat("list root", true)
	if strings.Contains(out, "exit status: 0") {
		t.Fatalf("criterion-6 check insensitive: exec ran without a sandbox: %q", out)
	}
}

func requireBwrap(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("bwrap"); err != nil {
		if os.Getenv("NEXUS_ACCEPT_REQUIRE_SANDBOX") != "" {
			// The graded acceptance run (p0-accept.sh) must never
			// skip the sandbox criteria — absence is a hard failure
			// (fresh-audit codex #2).
			t.Fatalf("bwrap REQUIRED for the graded acceptance run: %v", err)
		}
		t.Skipf("bwrap unavailable: %v", err)
	}
}

var helperOnce sync.Once
var helperBin string
var helperErr error

func buildHelper(t *testing.T) string {
	t.Helper()
	helperOnce.Do(func() {
		helperBin = filepath.Join(harnessDir, "probehelper")
		cmd := exec.Command("go", "build", "-buildvcs=false", "-o", helperBin, "github.com/MatNik89/nexus/cmd/probehelper")
		cmd.Env = append(os.Environ(), "CGO_ENABLED=0")
		if out, err := cmd.CombinedOutput(); err != nil {
			helperErr = fmt.Errorf("%v\n%s", err, out)
		}
	})
	if helperErr != nil {
		t.Fatalf("building helper: %v", helperErr)
	}
	return helperBin
}

// writeAttestation records an acceptance pass for the EXACT binary.
func writeAttestation(t *testing.T, w *world, bin string) {
	t.Helper()
	f, err := os.Open(bin)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		t.Fatal(err)
	}
	writeAttestationDigest(t, w, hex.EncodeToString(h.Sum(nil)))
}

// writeAttestationHost writes a signed attestation claiming a given host.
// binDigest hashes one file.
func binDigest(t *testing.T, bin string) string {
	t.Helper()
	f, err := os.Open(bin)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		t.Fatal(err)
	}
	return hex.EncodeToString(h.Sum(nil))
}

func writeAttestationHost(t *testing.T, w *world, bin, host string) {
	t.Helper()
	writeAttestationMachine(t, w, binDigest(t, bin), host, localMachineSHA(t))
}

func writeAttestationDigest(t *testing.T, w *world, digest string) {
	t.Helper()
	host, err := exec.Command("uname", "-srm").Output()
	if err != nil {
		t.Fatal(err)
	}
	writeAttestationFull(t, w, digest, strings.TrimSpace(string(host)))
}

func writeAttestationFull(t *testing.T, w *world, digest, host string) {
	t.Helper()
	writeAttestationMachine(t, w, digest, host, localMachineSHA(t))
}

// localMachineSHA mirrors the production machine binding.
func localMachineSHA(t *testing.T) string {
	t.Helper()
	raw, err := os.ReadFile("/etc/machine-id")
	if err != nil {
		t.Skipf("no /etc/machine-id: %v", err)
	}
	sum := sha256.Sum256([]byte(strings.TrimSpace(string(raw))))
	return hex.EncodeToString(sum[:])
}

func writeAttestationMachine(t *testing.T, w *world, digest, host, machine string) {
	t.Helper()
	// The trust set is ONE generation dir behind the atomically-switched
	// trust/current pointer (fresh-audit codex #4 r3) — the fixture
	// publishes exactly like scripts/p0-accept.sh.
	trust := filepath.Join(w.base, "nexus", "trust")
	w.genN++
	gen := fmt.Sprintf("gen-test-%d", w.genN)
	dir := filepath.Join(trust, gen)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	key := w.ownerKey(t)
	signers, err := os.ReadFile(filepath.Join(w.base, "nexus", "allowed_signers"))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "allowed_signers"), signers, 0o600); err != nil {
		t.Fatal(err)
	}
	attPath := filepath.Join(dir, "acceptance.json")
	att := fmt.Sprintf(`{"binary_sha256":%q,"suite":"internal/acceptance+probe+sandbox","passed":true,"host":%q,"machine_id_sha256":%q,"time":"2026-09-05T00:00:00Z"}`,
		digest, host, machine)
	if err := os.WriteFile(attPath, []byte(att), 0o600); err != nil {
		t.Fatal(err)
	}
	// SIGN it as the owner (T27-r2 codex #2): unsigned JSON is not
	// evidence; the world carries its own owner key + allowed_signers.
	if out, err := exec.Command("ssh-keygen", "-Y", "sign", "-f", key, "-n", "nexus-acceptance", attPath).CombinedOutput(); err != nil {
		t.Fatalf("attestation sign: %v\n%s", err, out)
	}
	tmp := filepath.Join(trust, fmt.Sprintf("current.new.%d", w.genN))
	if err := os.Symlink(gen, tmp); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(tmp, filepath.Join(trust, "current")); err != nil {
		t.Fatal(err)
	}
}

// trustCurrent resolves the world's installed trust generation dir.
func (w *world) trustCurrent(t *testing.T) string {
	t.Helper()
	link := filepath.Join(w.base, "nexus", "trust", "current")
	gen, err := os.Readlink(link)
	if err != nil {
		t.Fatalf("no trust generation: %v", err)
	}
	return filepath.Join(w.base, "nexus", "trust", gen)
}

// pinnedDoctorBin builds (once per world) a nexus binary whose
// acceptance-signer fingerprint pins THIS world's allowed_signers.
func (w *world) pinnedDoctorBin(t *testing.T) string {
	t.Helper()
	if w.pinnedBin != "" {
		return w.pinnedBin
	}
	w.ownerKey(t) // ensures allowed_signers exists
	signers, err := os.ReadFile(filepath.Join(w.base, "nexus", "allowed_signers"))
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(signers)
	bin := filepath.Join(w.base, "nexus-pinned")
	cmd := exec.Command("go", "build", "-buildvcs=false",
		"-ldflags", "-X main.acceptanceSignerFingerprint="+hex.EncodeToString(sum[:]),
		"-o", bin, "github.com/MatNik89/nexus/cmd/nexus")
	cmd.Env = append(os.Environ(), "CGO_ENABLED=0")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("pinned build: %v\n%s", err, out)
	}
	w.pinnedBin = bin
	return bin
}

// ownerKey lazily creates the world's owner ssh key + allowed_signers.
func (w *world) ownerKey(t *testing.T) string {
	t.Helper()
	key := filepath.Join(w.base, "owner_key")
	if _, err := os.Stat(key); err == nil {
		return key
	}
	if out, err := exec.Command("ssh-keygen", "-t", "ed25519", "-N", "", "-C", "owner", "-f", key).CombinedOutput(); err != nil {
		t.Fatalf("owner keygen: %v\n%s", err, out)
	}
	pub, err := os.ReadFile(key + ".pub")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(w.base, "nexus", "allowed_signers"), []byte("owner "+string(pub)), 0o600); err != nil {
		t.Fatal(err)
	}
	return key
}

// obligationStatus reads the durable projection DIRECTLY from the
// profile SQLite file (read-only, external-tool equivalent — the harness
// never links the implementation's stores).
func (w *world) obligationStatus(t *testing.T, profile, id string) string {
	t.Helper()
	db, err := sql.Open("sqlite", "file:"+filepath.Join(w.base, "nexus", "profiles", profile, "journal.db")+"?mode=ro")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var st string
	if err := db.QueryRow(`SELECT status FROM obl_obligations WHERE id=?`, id).Scan(&st); err != nil {
		t.Fatalf("obligation %s: %v", id, err)
	}
	return st
}

func ctxT() context.Context { return context.Background() }

// Release attestation (T27, HARDQ A1 second half): the signed binary —
// which COMPILES IN the telegram adapter — verifies; a tampered binary
// fails verification.
func TestReleaseSignatureTamperDetected(t *testing.T) {
	if _, err := exec.LookPath("ssh-keygen"); err != nil {
		t.Skipf("ssh-keygen unavailable: %v", err)
	}
	dir := t.TempDir()
	key := filepath.Join(dir, "release_key")
	if out, err := exec.Command("ssh-keygen", "-t", "ed25519", "-N", "", "-C", "nexus-release", "-f", key).CombinedOutput(); err != nil {
		t.Fatalf("keygen: %v\n%s", err, out)
	}
	// Sign a private copy of the real binary.
	bin := filepath.Join(dir, "nexus")
	src, err := os.ReadFile(nexusBin(t))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(bin, src, 0o755); err != nil {
		t.Fatal(err)
	}
	repoRoot, _ := filepath.Abs("../..")
	if out, err := exec.Command(filepath.Join(repoRoot, "scripts", "release-sign.sh"), bin, key).CombinedOutput(); err != nil {
		t.Fatalf("sign: %v\n%s", err, out)
	}
	pub, err := os.ReadFile(key + ".pub")
	if err != nil {
		t.Fatal(err)
	}
	allowed := filepath.Join(dir, "allowed_signers")
	if err := os.WriteFile(allowed, []byte("owner "+string(pub)), 0o600); err != nil {
		t.Fatal(err)
	}
	verify := func() error {
		out, err := exec.Command(filepath.Join(repoRoot, "scripts", "release-verify.sh"),
			bin, bin+".sig", allowed, "owner").CombinedOutput()
		if err != nil {
			return fmt.Errorf("%v: %s", err, out)
		}
		return nil
	}
	if err := verify(); err != nil {
		t.Fatalf("clean binary failed verification: %v", err)
	}
	// TAMPER one byte → verification must FAIL.
	tampered := append([]byte{}, src...)
	tampered[len(tampered)/2] ^= 0xFF
	if err := os.WriteFile(bin, tampered, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := verify(); err == nil {
		t.Fatal("tampered binary passed signature verification")
	}
}

// doctor --p0 grants P0-capable ONLY from live criteria (T27): with a
// live provider, fake Bot API and bwrap, all six go LIVE; removing the
// bot token turns exactly the telegram criterion OFF and the grant away.
func TestDoctorP0GrantLive(t *testing.T) {
	requireBwrap(t)
	w := newWorld(t, nil)
	w.withTelegram(t)
	// The grant path needs a binary whose acceptance-signer fingerprint
	// PINS this world's allowed_signers (r3 codex #1: the anchor is the
	// binary, not a replaceable file).
	pinned := w.pinnedDoctorBin(t)
	run := func(env []string) (string, int) {
		cmd := exec.Command(pinned, "doctor", "--p0")
		cmd.Env = env
		out, _ := cmd.CombinedOutput()
		return string(out), cmd.ProcessState.ExitCode()
	}
	// WITHOUT the acceptance attestation the grant is refused even with
	// every capability live (T27 codex #3: no self-certification).
	outNoAtt, codeNoAtt := run(w.env())
	if codeNoAtt == 0 || strings.Contains(outNoAtt, "P0-capable") {
		t.Fatalf("grant minted without an acceptance attestation (exit %d):\n%s", codeNoAtt, outNoAtt)
	}
	writeAttestation(t, w, w.pinnedDoctorBin(t))
	out, code := run(w.env())
	if code != 0 || !strings.Contains(out, "P0-capable") {
		t.Fatalf("live world not granted P0-capable (exit %d):\n%s", code, out)
	}
	// A FORGED (unsigned) attestation withdraws the grant even with a
	// correct digest (codex-r2 #2).
	os.Remove(filepath.Join(w.trustCurrent(t), "acceptance.json.sig"))
	outForged, codeForged := run(w.env())
	if codeForged == 0 || strings.Contains(outForged, "P0-capable") {
		t.Fatalf("unsigned attestation granted (exit %d):\n%s", codeForged, outForged)
	}
	// REPLACING THE TRUST ANCHOR with a rogue key + rogue signature is a
	// key-less forgery — the binary-pinned fingerprint refuses it
	// (r3 codex #1 probe 1).
	rogueAnchor := filepath.Join(w.base, "rogue_anchor_key")
	if out, err := exec.Command("ssh-keygen", "-t", "ed25519", "-N", "", "-C", "ra", "-f", rogueAnchor).CombinedOutput(); err != nil {
		t.Fatalf("rogue anchor keygen: %v\n%s", err, out)
	}
	signersPath := filepath.Join(w.trustCurrent(t), "allowed_signers")
	origSigners, err := os.ReadFile(signersPath)
	if err != nil {
		t.Fatal(err)
	}
	roguePub, _ := os.ReadFile(rogueAnchor + ".pub")
	if err := os.WriteFile(signersPath, []byte("owner "+string(roguePub)), 0o600); err != nil {
		t.Fatal(err)
	}
	attP := filepath.Join(w.trustCurrent(t), "acceptance.json")
	os.Remove(attP + ".sig")
	if out, err := exec.Command("ssh-keygen", "-Y", "sign", "-f", rogueAnchor, "-n", "nexus-acceptance", attP).CombinedOutput(); err != nil {
		t.Fatalf("rogue anchor sign: %v\n%s", err, out)
	}
	outAnchor, codeAnchor := run(w.env())
	if codeAnchor == 0 || strings.Contains(outAnchor, "P0-capable") {
		t.Fatalf("replaced trust anchor granted (exit %d):\n%s", codeAnchor, outAnchor)
	}
	if err := os.WriteFile(signersPath, origSigners, 0o600); err != nil {
		t.Fatal(err)
	}
	writeAttestation(t, w, w.pinnedDoctorBin(t))
	// A PATH ssh-keygen SHIM cannot spoof the verifier — the binary uses
	// the fixed root-owned /usr/bin/ssh-keygen (r3 codex #1 probe 2).
	shimDir := filepath.Join(w.base, "shim")
	os.MkdirAll(shimDir, 0o700)
	os.WriteFile(filepath.Join(shimDir, "ssh-keygen"), []byte("#!/bin/sh\nexit 0\n"), 0o755)
	// unsigned + zero-exit shim: must STILL refuse (resolve the CURRENT
	// generation — the pointer moved since attP was computed)
	os.Remove(filepath.Join(w.trustCurrent(t), "acceptance.json.sig"))
	envShim := append([]string{}, w.env()...)
	for i, e := range envShim {
		if strings.HasPrefix(e, "PATH=") {
			envShim[i] = "PATH=" + shimDir + ":" + strings.TrimPrefix(e, "PATH=")
		}
	}
	outShim, codeShim := run(envShim)
	if codeShim == 0 || strings.Contains(outShim, "P0-capable") {
		t.Fatalf("PATH-shimmed verifier granted (exit %d):\n%s", codeShim, outShim)
	}
	writeAttestation(t, w, w.pinnedDoctorBin(t))
	// A WRONG-KEY signature (not in allowed_signers) withdraws the grant
	// — this is the branch the ssh-keygen verify itself carries.
	rogue := filepath.Join(w.base, "rogue_key")
	if out, err := exec.Command("ssh-keygen", "-t", "ed25519", "-N", "", "-C", "rogue", "-f", rogue).CombinedOutput(); err != nil {
		t.Fatalf("rogue keygen: %v\n%s", err, out)
	}
	attPath := filepath.Join(w.trustCurrent(t), "acceptance.json")
	os.Remove(attPath + ".sig")
	if out, err := exec.Command("ssh-keygen", "-Y", "sign", "-f", rogue, "-n", "nexus-acceptance", attPath).CombinedOutput(); err != nil {
		t.Fatalf("rogue sign: %v\n%s", err, out)
	}
	outRogue, codeRogue := run(w.env())
	if codeRogue == 0 || strings.Contains(outRogue, "P0-capable") {
		t.Fatalf("rogue-signed attestation granted (exit %d):\n%s", codeRogue, outRogue)
	}
	writeAttestation(t, w, w.pinnedDoctorBin(t))
	// A DIFFERENT-MACHINE attestation (same kernel tuple, other
	// machine-id) withdraws the grant (P0-prep-r1 codex #1: the kernel
	// tuple alone is not unique).
	hostOut, herr := exec.Command("uname", "-srm").Output()
	if herr != nil {
		t.Fatal(herr)
	}
	digestSelf := binDigest(t, w.pinnedDoctorBin(t))
	writeAttestationMachine(t, w, digestSelf, strings.TrimSpace(string(hostOut)),
		"1111111111111111111111111111111111111111111111111111111111111111")
	outMach, codeMach := run(w.env())
	if codeMach == 0 || strings.Contains(outMach, "P0-capable") {
		t.Fatalf("same-kernel other-machine attestation granted (exit %d):\n%s", codeMach, outMach)
	}
	writeAttestation(t, w, w.pinnedDoctorBin(t))
	// A DIFFERENT-HOST attestation withdraws the grant (P0-prep #2:
	// copying binary+attestation to another machine transfers nothing).
	writeAttestationHost(t, w, w.pinnedDoctorBin(t), "Linux 0.0.0-other x86_64")
	outHost, codeHost := run(w.env())
	if codeHost == 0 || strings.Contains(outHost, "P0-capable") {
		t.Fatalf("foreign-host attestation granted (exit %d):\n%s", codeHost, outHost)
	}
	writeAttestation(t, w, w.pinnedDoctorBin(t))
	// A TAMPERED digest withdraws the grant.
	writeAttestationDigest(t, w, "deadbeef")
	outBad, codeBad := run(w.env())
	if codeBad == 0 || strings.Contains(outBad, "P0-capable") {
		t.Fatalf("grant survived a digest mismatch (exit %d):\n%s", codeBad, outBad)
	}
	writeAttestation(t, w, w.pinnedDoctorBin(t))
	// SENSITIVITY: drop the bot token → telegram OFF, grant withdrawn.
	var envNoTok []string
	for _, e := range w.env() {
		if !strings.HasPrefix(e, "NEXUS_ACCEPT_TG=") {
			envNoTok = append(envNoTok, e)
		}
	}
	out2, code2 := run(envNoTok)
	if code2 == 0 || strings.Contains(out2, "P0-capable") {
		t.Fatalf("grant survived a dead channel (exit %d):\n%s", code2, out2)
	}
	if !strings.Contains(out2, "OFF   telegram") {
		t.Fatalf("wrong criterion went off:\n%s", out2)
	}
}

// SEALED-snapshot ENFORCEMENT (T27 codex #1): when the startup provider
// probe fails, the daemon REFUSES to serve even though the provider
// recovers immediately after — no consumer runs against a capability the
// sealed snapshot marked OFF.
func TestSealedOffCapabilityNeverServes(t *testing.T) {
	w := newWorld(t, nil)
	// The provider fails EXACTLY the first request (the startup probe)
	// and works forever after — codex's recovering-provider scenario.
	var failedOnce sync.Mutex
	failed := false
	orig := w.script
	w.script = func(last string) string {
		if orig != nil {
			return orig(last)
		}
		return "echo: " + last
	}
	handler := w.provider.Config.Handler
	w.provider.Config.Handler = http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
		failedOnce.Lock()
		first := !failed
		failed = true
		failedOnce.Unlock()
		if first {
			http.Error(rw, "provider warming up", http.StatusServiceUnavailable)
			return
		}
		handler.ServeHTTP(rw, r)
	})
	w.daemon()
	// The daemon must have refused to serve: the chat gets no reply.
	out, _ := w.chat("hello after recovery", false)
	if strings.Contains(out, "echo:") && strings.Contains(out, "hello after recovery") {
		t.Fatalf("daemon served a conversation although the sealed snapshot marked it OFF: %q", out)
	}
}

// SEAL-BEFORE-CONSUMERS (T27-r3 codex #1): a startup whose provider
// probe fails must not run ANY effectful consumer — with an overdue
// reminder waiting and a channel wired, the refused incarnation delivers
// NOTHING; a later healthy incarnation still can (nothing was lost or
// falsely advanced past delivery).
func TestSealedOffStartupRunsNoConsumers(t *testing.T) {
	w := newWorld(t, nil)
	bot := w.withTelegram(t)
	w.script = func(last string) string {
		if strings.Contains(last, "obs-") {
			return "reminder placed"
		}
		if strings.Contains(last, "remind me") {
			return `{"action":"tool","tool_id":"reminder_set","arguments":{"id":"rem-seal","body":"sealed reminder","year":2026,"month":1,"day":2,"hour":9,"minute":0,"tz":"Europe/Zagreb"}}`
		}
		return "echo: " + last
	}
	stop := w.daemon()
	if out, err := w.chat("remind me", true); err != nil || !strings.Contains(out, "reminder placed") {
		t.Fatalf("set failed: %v %q", err, out)
	}
	stop()
	// Incarnation 2: the provider fails EVERYTHING → sealed OFF → the
	// daemon must refuse and run NO consumer (no delivery may appear).
	deadProv := w.provider
	w.provider = nil
	broken := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
		http.Error(rw, "down", http.StatusServiceUnavailable)
	}))
	t.Cleanup(broken.Close)
	w.rewriteConfig(t, func(cfg map[string]any) { cfg["provider_base_url"] = broken.URL })
	// egress allow must include the broken host for the probe to even try
	w.rewriteConfig(t, func(cfg map[string]any) {
		cfg["egress_allow"] = []any{strings.TrimPrefix(broken.URL, "http://"), strings.TrimPrefix(deadProv.URL, "http://")}
	})
	stop2 := w.daemon()
	time.Sleep(4 * time.Second)
	stop2()
	bot.mu.Lock()
	for _, sent := range bot.sent {
		if strings.Contains(sent, "sealed reminder") {
			bot.mu.Unlock()
			t.Fatalf("sealed-OFF startup ran the delivery consumer: %q", sent)
		}
	}
	bot.mu.Unlock()
	// DURABLE state must be UNCHANGED by the refused incarnation
	// (T27-r3 codex #2: a pre-seal sweep would have advanced this to
	// DELIVERY_PENDING without any message ever appearing).
	if st := w.obligationStatus(t, "private", "rem-seal"); st != "SCHEDULED" {
		t.Fatalf("sealed-OFF startup advanced durable state: obligation %q (want SCHEDULED)", st)
	}
	// Incarnation 3: healthy again — the reminder arrives now.
	w.rewriteConfig(t, func(cfg map[string]any) { cfg["provider_base_url"] = deadProv.URL })
	w.rewriteConfig(t, func(cfg map[string]any) {
		cfg["egress_allow"] = []any{strings.TrimPrefix(deadProv.URL, "http://")}
	})
	w.provider = deadProv
	stop3 := w.daemon()
	defer stop3()
	bot.waitSent(t, "sealed reminder", 25*time.Second)
}

// countSent counts arrivals of one exact text at the fake API.
func (b *fakeBot) countSent(text string) int {
	b.mu.Lock()
	defer b.mu.Unlock()
	n := 0
	for _, s := range b.sent {
		if s == text {
			n++
		}
	}
	return n
}
