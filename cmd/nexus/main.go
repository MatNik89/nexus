// Command nexus is the NEXUS personal-assistant kernel binary.
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/MatNik89/nexus/internal/app/daemon"
	"github.com/MatNik89/nexus/internal/app/repl"
	"github.com/MatNik89/nexus/internal/approval"
	"github.com/MatNik89/nexus/internal/channel"
	"github.com/MatNik89/nexus/internal/channel/telegram"
	"github.com/MatNik89/nexus/internal/foundation/atomicwrite"
	"github.com/MatNik89/nexus/internal/foundation/clockid"
	"github.com/MatNik89/nexus/internal/foundation/config"
	"github.com/MatNik89/nexus/internal/foundation/pathx"
	"github.com/MatNik89/nexus/internal/kernel/contracts"
	"github.com/MatNik89/nexus/internal/kernel/effectpath"
	"github.com/MatNik89/nexus/internal/kernel/journal"
	"github.com/MatNik89/nexus/internal/kernel/loop"
	"github.com/MatNik89/nexus/internal/kernel/machine"
	"github.com/MatNik89/nexus/internal/kernel/s7min"
	"github.com/MatNik89/nexus/internal/llm/planner"
	"github.com/MatNik89/nexus/internal/llm/provider"
	"github.com/MatNik89/nexus/internal/memory"
	"github.com/MatNik89/nexus/internal/obligation"
	"github.com/MatNik89/nexus/internal/preflight/doctor"
	"github.com/MatNik89/nexus/internal/schedule"
	"github.com/MatNik89/nexus/internal/security/redact"
)

var version = "0.0.1-p0"

func main() {
	if len(os.Args) > 1 {
		switch os.Args[1] {
		case "doctor":
			os.Exit(runDoctor(os.Args[2:]))
		case "__probe-ptrace":
			os.Exit(runProbePtrace())
		case "daemon":
			os.Exit(runDaemon())
		case "chat":
			yolo := false
			for _, a := range os.Args[2:] {
				if a == "--yolo" {
					yolo = true
				}
			}
			os.Exit(runChat(yolo))
		}
	}
	fmt.Fprintf(os.Stdout, "nexus %s\nusage: nexus <daemon|chat [--yolo]|doctor>\n", version)
}

// resolveEnv loads layout + configuration (global file only in P0;
// project layer joins with the workspace slice).
func resolveEnv() (pathx.Layout, config.Resolved, error) {
	layout, err := pathx.Default()
	if err != nil {
		return pathx.Layout{}, config.Resolved{}, err
	}
	resolved, err := config.Resolve(filepath.Join(layout.Base, "config.json"), "", os.Environ(), nil)
	if err != nil {
		return pathx.Layout{}, config.Resolved{}, err
	}
	return layout, resolved, nil
}

func socketPath(l pathx.Layout) string { return filepath.Join(l.SystemDir(), "daemon.sock") }

// journalAudit journals policy override records (ALLOWED_BY_YOLO). The
// append is FAIL-CLOSED: an error here refuses the confirmation bypass
// (Phase-2 codex #9 / kilo #1 — a yolo action must never execute without
// its durable record), the counter is concurrency-safe, and the append is
// time-bounded so a wedged journal cannot hang a session forever.
type journalAudit struct {
	j       *journal.Journal
	profile contracts.ProfileID
	n       atomic.Uint64
}

const evPolicyYolo = "policy.allowed_by_yolo"

func (a *journalAudit) Record(event string, call contracts.ToolCall) error {
	n := a.n.Add(1)
	payload, err := json.Marshal(map[string]string{"event": event, "tool_id": string(call.ToolID)})
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_, err = a.j.Append(ctx, contracts.EnvelopeParams{
		SchemaID: "nexus.event", SchemaVersion: 1,
		EventID:   contracts.EventID(fmt.Sprintf("ev-policy-%d-%d", os.Getpid(), n)),
		EventType: evPolicyYolo, RunID: "run-policy", EmittedAt: time.Now().UTC(),
		ActorType: contracts.ActorSystem, ActorID: "pep", PrincipalID: "nexus",
		WorkspaceID: "local", ProfileID: a.profile, AttemptNo: 1,
		Payload: payload, PayloadHash: "recomputed",
	})
	return err
}

func runDaemon() int {
	layout, resolved, err := resolveEnv()
	if err != nil {
		fmt.Fprintf(os.Stderr, "nexus daemon: %v\n", err)
		return 2
	}
	b, err := buildDaemon(layout, resolved)
	if err != nil {
		fmt.Fprintf(os.Stderr, "nexus daemon: %v\n", err)
		return 2
	}
	defer b.j.Close()
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	hb := daemon.NewHeartbeat(filepath.Join(layout.SystemDir(), "heartbeat"), 5*time.Second)
	go hb.Run(ctx)
	// Durable scheduler: SYNCHRONOUS startup sweep first — its outcome is
	// mirrored to the health file BEFORE the daemon serves anyone, so a
	// startup failure can never be lost in the async window (Phase-4-r5
	// codex #4). Then the periodic catch-up loop. The FireDecorator moved
	// each fired obligation to DELIVERY_PENDING inside the fire batch.
	healthPath := filepath.Join(layout.SystemDir(), "scheduler_health")
	writeHealth := func(err error) {
		msg := ""
		if err != nil {
			msg = err.Error()
			fmt.Fprintf(os.Stderr, "nexus daemon: scheduler: %v\n", err)
		}
		if werr := atomicwrite.Write(healthPath, []byte(msg), 0o600); werr != nil {
			fmt.Fprintf(os.Stderr, "nexus daemon: health mirror: %v\n", werr)
		}
	}
	_, startupErr := b.sched.Sweep(ctx)
	writeHealth(startupErr)
	go b.sched.Run(ctx, 30*time.Second, nil)
	// Scheduler health mirror (Phase-4-r3 codex #4): sweep failures land
	// in system/scheduler_health where doctor reads them; an empty file
	// means healthy.
	go func() {
		t := time.NewTicker(5 * time.Second)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
				writeHealth(b.sched.Health())
			}
		}
	}()
	// Telegram (channel:builtin): starts ONLY when the token env var is
	// set AND at least one chat is bound — deny-default (B3).
	if tok := os.Getenv(resolved.Config.TelegramTokenEnv); tok != "" {
		bindings := telegramBindings()
		if len(bindings) == 0 {
			fmt.Fprintln(os.Stderr, "nexus daemon: telegram token set but no chat bindings (NEXUS_TELEGRAM_BINDINGS=\"chatid=profile,...\") — adapter stays OFF (deny-default)")
		} else {
			adapter, aerr := telegram.New(telegram.Config{
				APIBase:  "https://api.telegram.org",
				TokenEnv: resolved.Config.TelegramTokenEnv,
				Bindings: bindings,
				Profile:  b.profile,
			}, b.chanCore, telegramHandler(b))
			if aerr != nil {
				fmt.Fprintf(os.Stderr, "nexus daemon: telegram: %v\n", aerr)
			} else {
				go adapter.Run(ctx, 2*time.Second)
				fmt.Println("nexus daemon: telegram adapter running")
			}
		}
	}
	sock := socketPath(layout)
	fmt.Printf("nexus daemon %s — profile %s, socket %s\n", version, resolved.Config.DefaultProfile, sock)
	if err := b.d.Serve(ctx, sock); err != nil {
		fmt.Fprintf(os.Stderr, "nexus daemon: %v\n", err)
		return 1
	}
	return 0
}

// buildDaemon is the COMPOSITION ROOT: every production seam (journal +
// redactor, S7 authority, governed provider, streaming planner factory,
// fail-closed audit) wired exactly as the daemon runs it — and testable
// against a custom layout (Phase-2-r2 codex #13).
// daemonBundle is everything the composition root wires together.
type daemonBundle struct {
	d         *daemon.Daemon
	j         *journal.Journal
	sched     *schedule.Scheduler
	obl       *obligation.Manager
	chanCore  *channel.Core
	approvals *approval.Store
	profile   contracts.ProfileID
	cfg       config.Config
}

func buildDaemon(layout pathx.Layout, resolved config.Resolved) (*daemonBundle, error) {
	profile := resolved.Config.DefaultProfile
	profileDir, err := layout.ProfileDir(profile)
	if err != nil {
		return nil, err
	}
	for _, dir := range []string{profileDir, layout.SystemDir()} {
		if err := pathx.EnsureDir(layout.Base, dir); err != nil {
			// The base itself may not exist yet: create it 0700 first.
			if os.MkdirAll(layout.Base, 0o700) != nil || pathx.EnsureDir(layout.Base, dir) != nil {
				return nil, err
			}
		}
	}
	registry, err := obligation.NewRegistry(map[string]obligation.Kind{
		"file_note": {Handler: obligation.FileNoteHandler(profileDir), ValidateParams: obligation.ValidateFileNoteParams},
	})
	if err != nil {
		return nil, err
	}
	events := map[string]journal.PayloadValidator{evPolicyYolo: nil}
	for _, n := range machine.EventTypes() {
		events[n] = nil
	}
	for n, v := range memory.Events() {
		events[n] = v
	}
	for n, v := range schedule.Events() {
		events[n] = v
	}
	doneGate := obligation.NewDoneGate()
	for n, v := range obligation.Events(registry, doneGate) {
		events[n] = v
	}
	for n, v := range channel.Events() {
		events[n] = v
	}
	for n, v := range approval.Events() {
		events[n] = v
	}

	journalPath, _ := layout.ProfileJournal(profile)
	redactor := redact.NewKnownRefs(knownSecretRefs(resolved.Config))
	// The ONE profile database: journal + memory projection together
	// (Annex P0.3 — facts are journal events folded in the same
	// transaction; no second SQLite file exists).
	j, err := journal.Open(journalPath, profile, redactor, events, memory.NewProjection(), schedule.NewProjection(), obligation.NewProjection(), channel.NewProjection(), approval.NewProjection())
	if err != nil {
		return nil, fmt.Errorf("journal: %w", err)
	}
	authority := s7min.NewAuthority(nil, 5*time.Minute)
	prov, err := provider.NewAPIKey(resolved.Config, authority)
	if err != nil {
		j.Close()
		return nil, fmt.Errorf("provider: %w (conversation is a P0 core capability — fix the config and restart)", err)
	}
	memStore, err := memory.NewStore(j)
	if err != nil {
		j.Close()
		return nil, fmt.Errorf("memory: %w", err)
	}
	sched, err := schedule.New(j, clockid.System{})
	if err != nil {
		j.Close()
		return nil, fmt.Errorf("schedule: %w", err)
	}
	sched.SetCounterFile(filepath.Join(layout.SystemDir(), "last_occurrence_fired"))
	chanCore, err := channel.New(j)
	if err != nil {
		j.Close()
		return nil, err
	}
	approvals, err := approval.NewStore(j, clockid.System{})
	if err != nil {
		j.Close()
		return nil, err
	}
	lazyRunner := &obligation.LazyRunner{}
	oblManager, err := obligation.NewManager(j, sched, registry, clockid.System{}, lazyRunner, authority, profileDir, doneGate)
	if err != nil {
		j.Close()
		return nil, err
	}
	// Reminder firing joins the scheduler batch (no crash window between
	// occurrence-fire and obligation admission).
	sched.SetFireDecorator(oblManager.FireParams)
	// System EffectPath: the manager's programmatic dispatch (scheduled
	// task runs) goes through the SAME sealed path as sessions —
	// ModeDefault, merged sealed tools/rules.
	allTools := mergedTools(memStore, redactor, oblManager)
	sysPep, err := effectpath.NewPEP(mergedRules(), effectpath.NewApprovals(nil, 5*time.Minute),
		&journalAudit{j: j, profile: profile}, effectpath.ModeDefault)
	if err != nil {
		j.Close()
		return nil, err
	}
	sysPath, err := effectpath.NewEffectPath(sysPep, systemMW{},
		effectpath.NewInProcessExecutor(allTools),
		effectpath.NewSandboxedProcessExecutor(nil), authority)
	if err != nil {
		j.Close()
		return nil, err
	}
	lazyRunner.R = sysPath
	target := prov.Target() // the provider's OWN grant target — anything else never reaches the wire
	d, err := daemon.New(daemon.Deps{
		Journal: j,
		PlannerFactory: func(deliver func(string) error) (loop.Planner, error) {
			// Streaming print: provider deltas flow to the client as they
			// are produced (truncation-honest — the provider errors on an
			// incomplete stream). With tools enabled the planner buffers
			// (a tool-call JSON never streams raw) and delivers finals in
			// one piece.
			pl, err := planner.NewStreaming(prov, prov, authority, target, deliver)
			if err != nil {
				return nil, err
			}
			specs := memory.Specs()
			for k, v := range obligation.Specs() {
				specs[k] = v
			}
			return pl.WithTools(specs, profile)
		},
		Authority: authority, Profile: profile,
		Rules:    mergedRules(),
		Tools:    allTools,
		Audit:    &journalAudit{j: j, profile: profile},
		Redactor: redactor,
	})
	if err != nil {
		j.Close()
		return nil, err
	}
	return &daemonBundle{d: d, j: j, sched: sched, obl: oblManager,
		chanCore: chanCore, approvals: approvals, profile: profile, cfg: resolved.Config}, nil
}

// systemMW is the order-only S6.9 seam for the system EffectPath.
type systemMW struct{}

func (systemMW) BeforeTool(context.Context, contracts.ToolCall) error { return nil }
func (systemMW) AfterTool(context.Context, contracts.ToolCall, contracts.ToolResult) error {
	return nil
}
func (systemMW) OnError(ctx context.Context, e error) error { return e }

// mergedRules combines every tool family's PEP decisions.
func mergedRules() map[contracts.ToolID]effectpath.Decision {
	rules := memory.Rules()
	for k, v := range obligation.Rules() {
		rules[k] = v
	}
	return rules
}

// mergedTools combines every tool family's handlers.
func mergedTools(memStore *memory.Store, r redact.Redactor, m *obligation.Manager) map[contracts.ToolID]effectpath.InProcFunc {
	tools := memory.Tools(memStore, r)
	for k, v := range obligation.Tools(m) {
		tools[k] = v
	}
	return tools
}

// knownSecretRefs feeds the C1 known-ref redactor: every configured
// secret env var's VALUE is redacted before the journal.
func knownSecretRefs(cfg config.Config) map[string]string {
	refs := map[string]string{}
	for _, env := range []string{cfg.ProviderKeyEnv, cfg.TelegramTokenEnv} {
		if env == "" {
			continue
		}
		if v := os.Getenv(env); v != "" {
			refs[env] = v
		}
	}
	return refs
}

func runChat(yolo bool) int {
	layout, _, err := resolveEnv()
	if err != nil {
		fmt.Fprintf(os.Stderr, "nexus chat: %v\n", err)
		return 2
	}
	if yolo {
		fmt.Println("YOLO mode: confirmations are OFF for this session (DENY, sandbox and egress stay active).")
	}
	if err := repl.Run(os.Stdin, os.Stdout, socketPath(layout), yolo); err != nil {
		fmt.Fprintf(os.Stderr, "nexus chat: %v\n", err)
		return 1
	}
	return 0
}

func runDoctor(args []string) int {
	strict := len(args) > 0 && args[0] == "--strict"
	env, err := doctor.DefaultEnv()
	if err != nil {
		fmt.Fprintf(os.Stderr, "doctor: %v\n", err)
		return 2
	}
	checks := doctor.Run(env)
	for _, c := range checks {
		if c.Status == doctor.StatusOK {
			fmt.Printf("OK   %-16s %s\n", c.Name, c.Detail)
			continue
		}
		fmt.Printf("OFF  %-16s capability %q disabled — %s\n", c.Name, c.Capability, c.Detail)
		fmt.Printf("     fix: %s\n", c.Fix)
	}
	if doctor.Ready(checks) {
		fmt.Println("prerequisites-ready (P0-capable is granted by the acceptance run, not here)")
		return 0
	}
	fmt.Println("NOT prerequisites-ready — capabilities above stay OFF (fail-closed)")
	if strict {
		return 1
	}
	return 0
}

// telegramBindings parses NEXUS_TELEGRAM_BINDINGS ("chatid=profile,...").
func telegramBindings() map[int64]string {
	out := map[int64]string{}
	raw := os.Getenv("NEXUS_TELEGRAM_BINDINGS")
	for _, pair := range strings.Split(raw, ",") {
		pair = strings.TrimSpace(pair)
		if pair == "" {
			continue
		}
		i := strings.IndexByte(pair, '=')
		if i <= 0 {
			continue
		}
		chat, err := strconv.ParseInt(pair[:i], 10, 64)
		if err != nil {
			continue
		}
		out[chat] = pair[i+1:]
	}
	return out
}

// telegramHandler routes an admitted Telegram message: "approve <id>" /
// "deny <id>" hit the durable HITL store; anything else is a normal
// conversation turn through the same production planner spine
// (ModeDefault ALWAYS — a channel message can never enable yolo, F2).
func telegramHandler(b *daemonBundle) telegram.Handler {
	return func(ctx context.Context, in channel.Inbound) (string, error) {
		text := strings.TrimSpace(in.Text)
		lower := strings.ToLower(text)
		source := "tg:" + in.ChannelIdentity
		switch {
		case strings.HasPrefix(lower, "approve "):
			id := strings.TrimSpace(text[len("approve "):])
			if err := b.approvals.Approve(ctx, id, source); err != nil {
				return "Approval failed: " + err.Error(), nil
			}
			return "Approved " + id + ". The suspended action will resume.", nil
		case strings.HasPrefix(lower, "deny "):
			id := strings.TrimSpace(text[len("deny "):])
			if err := b.approvals.Deny(ctx, id, source); err != nil {
				return "Denial failed: " + err.Error(), nil
			}
			return "Denied " + id + ".", nil
		case lower == "pending":
			pending, err := b.approvals.Pending(ctx)
			if err != nil {
				return "", err
			}
			if len(pending) == 0 {
				return "No pending approvals.", nil
			}
			out := ""
			for _, ch := range pending {
				out += ch.Summary + "\n"
			}
			return out, nil
		}
		// Ordinary conversation: one turn through the daemon's session
		// spine (ModeDefault — channel input can NEVER carry yolo).
		reply, err := b.d.RunChannelTurn(ctx, in.ChannelIdentity, text)
		if err != nil {
			return "", err
		}
		return reply, nil
	}
}
