// Command nexus is the NEXUS personal-assistant kernel binary.
package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
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
	"github.com/MatNik89/nexus/internal/exectool"
	"github.com/MatNik89/nexus/internal/foundation/atomicwrite"
	"github.com/MatNik89/nexus/internal/foundation/clockid"
	"github.com/MatNik89/nexus/internal/foundation/config"
	"github.com/MatNik89/nexus/internal/foundation/pathx"
	"github.com/MatNik89/nexus/internal/kernel/closure"
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
	"github.com/MatNik89/nexus/internal/sandbox"
	"github.com/MatNik89/nexus/internal/schedule"
	"github.com/MatNik89/nexus/internal/security/redact"
)

var version = "0.0.1-p0"

// acceptanceSignerFingerprint pins the sha256 of the allowed_signers
// file at BUILD time (-ldflags -X). The trust anchor is therefore the
// binary itself (installed via the signed release), not a user-writable
// file an attacker without the owner key could replace (T27-r3 codex
// #1). Empty = this build cannot grant P0-capable.
var acceptanceSignerFingerprint string

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
	var chanProbe *closure.ProbeResult
	var tgAdapter *telegram.Adapter
	// Telegram (channel:builtin): starts ONLY when the token env var is
	// set AND at least one chat is bound — deny-default (B3).
	if tok := os.Getenv(resolved.Config.TelegramTokenEnv); tok != "" {
		bindings, berr := telegramBindings(os.Getenv("NEXUS_TELEGRAM_BINDINGS"))
		switch {
		case berr != nil:
			fmt.Fprintf(os.Stderr, "nexus daemon: %v — adapter stays OFF (fail closed)\n", berr)
		case len(bindings) == 0:
			fmt.Fprintln(os.Stderr, "nexus daemon: telegram token set but no chat bindings (NEXUS_TELEGRAM_BINDINGS=\"chatid=profile,...\") — adapter stays OFF (deny-default)")
		default:
			adapter, aerr := telegram.New(telegram.Config{
				APIBase:  resolved.Config.TelegramAPIBase,
				TokenEnv: resolved.Config.TelegramTokenEnv,
				Bindings: bindings,
				Profile:  b.profile,
			}, b.chanCore, telegramHandler(b))
			if aerr != nil {
				fmt.Fprintf(os.Stderr, "nexus daemon: telegram: %v\n", aerr)
			} else {
				pr := adapter.Probe(ctx, resolved)
				chanProbe = &pr
				tgAdapter = adapter // start decision belongs to the SEALED snapshot
				if !pr.Passed {
					fmt.Fprintf(os.Stderr, "nexus daemon: telegram channel probe failed: %s\n", pr.Detail)
				}
			}
		}
	}
	// FINAL sealed capability snapshot (T11/T27 codex #1): sealed BEFORE
	// any consumer starts, and ENFORCED — conversation OFF refuses to
	// serve at all (fail closed), telegram starts only when its sealed
	// capability is ON; nothing dispatches to an OFF capability.
	probes, requested := b.liveProbes(ctx, resolved, b.prov, b.sandboxOK, chanProbe)
	snap, serr := sealStartupSnapshot(resolved, probes, requested)
	if serr != nil {
		fmt.Fprintf(os.Stderr, "nexus daemon: capability seal: %v\n", serr)
		return 1
	}
	for _, st := range snap.List() {
		state := "ON"
		if !st.On {
			state = "OFF (" + st.Reason + ")"
		}
		fmt.Printf("nexus daemon: capability %-12s %s\n", st.Name, state)
	}
	if !snap.On("conversation") {
		fmt.Fprintf(os.Stderr, "nexus daemon: conversation capability OFF (%s) — refusing to serve (fail closed)\n",
			snap.Status("conversation").Reason)
		return 1
	}
	// EVERYTHING effectful below this line runs ONLY under a sealed,
	// conversation-ON snapshot (T27-r2 codex #1: the sweep, scheduler,
	// approved-resume scan and delivery loop must never advance state a
	// sealed-OFF startup would refuse).
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
	// Startup resume scan (Phase-5-r2 codex #2): an approval whose
	// receipt committed but whose resume was lost to a crash completes
	// NOW — the action is never stranded. The reply rides the outbox.
	if serr := b.resumeApprovedPending(ctx); serr != nil {
		fmt.Fprintf(os.Stderr, "nexus daemon: approved-resume scan: %v\n", serr)
	}
	// Reminder DELIVERY loop (T27, PRD criterion 3): DELIVERY_PENDING
	// occurrences push to the owner chat over the telegram outbox and are
	// marked delivered with the durable receipt. Without a bound channel
	// they stay DELIVERY_PENDING (fail closed — no fake receipt).
	ownerChat := func() string {
		bindings, berr := telegramBindings(os.Getenv("NEXUS_TELEGRAM_BINDINGS"))
		if berr != nil {
			return ""
		}
		// Deterministic pick (T27 kilo #4): the LOWEST chat id bound to
		// this profile — map iteration order must never route reminders.
		best := int64(0)
		for chat, prof := range bindings {
			if contracts.ProfileID(prof) == b.profile && (best == 0 || chat < best) {
				best = chat
			}
		}
		if best == 0 {
			return ""
		}
		return "chat-" + strconv.FormatInt(best, 10)
	}()
	if ownerChat != "" {
		go func() {
			t := time.NewTicker(2 * time.Second)
			defer t.Stop()
			for {
				select {
				case <-ctx.Done():
					return
				case <-t.C:
					b.deliverPendingReminders(ctx, ownerChat)
				}
			}
		}()
	}
	if tgAdapter != nil {
		if snap.On("telegram") {
			go tgAdapter.Run(ctx, 2*time.Second)
			fmt.Println("nexus daemon: telegram adapter running (sealed capability ON)")
		} else {
			fmt.Fprintf(os.Stderr, "nexus daemon: telegram capability OFF (%s) — adapter not started (fail closed)\n",
				snap.Status("telegram").Reason)
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
	sysPath   *effectpath.EffectPath
	authority *s7min.Authority
	prov      *provider.APIKey
	sandboxOK bool
}

// resumeApproved is the T24 resume: it executes the EXACT approved call
// through the sealed system EffectPath (the durable approval is consumed
// inside the PEP), then REHYDRATES the original suspended turn (B6,
// Phase-5-r2 codex #2/#4) — the loop re-enters it with the tool's
// observation and continues to a real final. The transport deadline is
// refreshed before execution (Phase-5-r2 codex #3 / kilo #1: the frozen
// 2-minute planner deadline would refuse every delayed remote approval;
// the deadline is deliberately NOT part of the C4 EffectHash, so the
// refresh cannot alter the approved intent).
func (b *daemonBundle) resumeApproved(ctx context.Context, identity, challengeID string) (string, error) {
	turn, run, call, blocks, err := b.approvals.SuspendedCall(ctx, challengeID, "tg:"+identity)
	if err != nil {
		return "", err
	}
	call.Deadline = time.Now().Add(2 * time.Minute)
	op := contracts.OperationID("resume-" + challengeID)
	grant, err := b.authority.Issue(op, effectpath.ToolTarget(call))
	if err != nil {
		return "", err
	}
	out, err := b.sysPath.RunTool(ctx, call, grant)
	if err != nil {
		// The approval is CONSUMED but the effect did not complete — an E9
		// UNKNOWN. The challenge stays CONSUMED-and-open; the user gets an
		// honest reconcile path (retry re-issues a FRESH challenge), never
		// a blind auto-retry (Phase-5-r3 kilo #1).
		return "", fmt.Errorf("the approved action did not complete: %w. Reply retry %s to issue a fresh approval", err, challengeID)
	}
	result := "done"
	if len(out.Output) > 0 && out.Output[0].Content != nil {
		result = *out.Output[0].Content
	}
	// REHYDRATION (Phase-5-r3 codex #1/#2): the ORIGINAL context plus the
	// approved tool's OWN observation blocks re-enter the original turn —
	// their trust labels and lineage are PRESERVED (Phase-6 codex #1:
	// flattening and re-minting them TOOL_TRUSTED laundered untrusted
	// exec output past the assembler's injection fence).
	continuation, err := resumeBlocks(blocks, out, call, result)
	if err != nil {
		return "", err
	}
	final, ferr := b.d.ResumeChannelTurn(ctx, identity, turn, run, challengeID, continuation)
	if merr := b.approvals.MarkResumeCompleted(ctx, challengeID); merr != nil {
		fmt.Fprintf(os.Stderr, "nexus: resume-completed mark %s: %v\n", challengeID, merr)
	}
	if ferr != nil {
		// The effect DID run and the approval is consumed — report the
		// tool result honestly even when the continuation fails.
		return result, nil
	}
	return final, nil
}

// resumeApprovedPending completes every approval whose receipt committed
// but whose resume was lost to a crash (Phase-5-r2 codex #2) — run at
// startup; each outcome rides the outbox back to its originating chat.
func (b *daemonBundle) resumeApprovedPending(ctx context.Context) error {
	approvedList, err := b.approvals.Approved(ctx)
	if err != nil {
		return err
	}
	var errs []error
	for _, ac := range approvedList {
		identity := strings.TrimPrefix(ac.ExpectedSource, "tg:")
		result, rerr := b.resumeApproved(ctx, identity, ac.ChallengeID)
		text := "Approved " + ac.ChallengeID + ". " + result
		if rerr != nil {
			errs = append(errs, fmt.Errorf("resume %s: %w", ac.ChallengeID, rerr))
			text = "Your approval " + ac.ChallengeID + " could not be resumed: " + rerr.Error()
		}
		if _, qerr := b.chanCore.EnqueueReply(ctx, "telegram", identity, b.profile, text); qerr != nil {
			errs = append(errs, fmt.Errorf("resume reply %s: %w", ac.ChallengeID, qerr))
		}
	}
	// E9 reconcile notices (Phase-5-r3 kilo #1): a CONSUMED challenge
	// whose resume never completed is an UNKNOWN — surface it to the USER
	// with the retry path; never auto-retry.
	open, oerr := b.approvals.ConsumedUnfinished(ctx)
	if oerr != nil {
		errs = append(errs, oerr)
	}
	for _, oc := range open {
		identity := strings.TrimPrefix(oc.ExpectedSource, "tg:")
		msg := "Approval " + oc.ChallengeID + " was consumed but its action may not have completed. Reply retry " + oc.ChallengeID + " to issue a fresh approval, or ignore this if it is done."
		if _, qerr := b.chanCore.EnqueueReply(ctx, "telegram", identity, b.profile, msg); qerr != nil {
			errs = append(errs, fmt.Errorf("reconcile notice %s: %w", oc.ChallengeID, qerr))
		}
	}
	return errors.Join(errs...)
}

// resumeBlocks assembles the continuation context: the ORIGINAL blocks
// plus the tool's OWN observation blocks with trust and lineage
// PRESERVED (Phase-6 codex #1: flattening exec output and re-minting it
// TOOL_TRUSTED laundered attacker-controlled text past the assembler's
// untrusted fence). Only a tool that returned NO blocks gets a synthetic
// trusted stub.
func resumeBlocks(original []contracts.ContextBlock, out contracts.ToolResult,
	call contracts.ToolCall, result string) ([]contracts.ContextBlock, error) {
	obsBlocks := out.Output
	if len(obsBlocks) == 0 {
		obs, err := resumeObservation(call, result)
		if err != nil {
			return nil, err
		}
		obsBlocks = []contracts.ContextBlock{obs}
	}
	return append(append([]contracts.ContextBlock{}, original...), obsBlocks...), nil
}

// resumeObservation packs the approved tool's result for the resumed turn.
func resumeObservation(call contracts.ToolCall, result string) (contracts.ContextBlock, error) {
	content := fmt.Sprintf("The user approved the suspended action. Tool %s executed with result: %s. Reply to the user.",
		call.ToolID, result)
	sum := sha256.Sum256([]byte(content))
	return contracts.NewContextBlock(contracts.ContextBlockParams{
		BlockID: contracts.BlockID("obs-resume-" + string(call.ToolCallID)),
		Kind:    "tool_result", Content: &content, ContentHash: hex.EncodeToString(sum[:]),
		SourceURI: "nexus://approval/resume", Producer: "tool",
		Trust: contracts.TrustToolTrusted, Sensitivity: contracts.SensitivityInternal,
		Lineage: []string{string(call.ToolCallID)}, ObservedAt: time.Now().UTC(),
	})
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
	// REAL sandbox (T25/T26): probe bwrap; a passing probe binds the exec
	// tool, a failing one leaves every ExecProcess call fail-closed and
	// the exec capability OFF (deny-default).
	var execAdapter *exectool.Adapter
	sbBackend := sandbox.NewBwrap()
	if rep, perr := sbBackend.Probe(context.Background()); perr == nil {
		if ad, aerr := exectool.New(sbBackend, rep, redactor, resolved.Config.ExecAllow); aerr == nil {
			execAdapter = ad
		}
	}
	var execDoor effectpath.SandboxBackend
	if execAdapter != nil {
		execDoor = execAdapter
	}
	sysPep, err := effectpath.NewPEP(mergedRules(), effectpath.NewApprovals(nil, 5*time.Minute),
		&journalAudit{j: j, profile: profile}, effectpath.ModeDefault)
	if err != nil {
		j.Close()
		return nil, err
	}
	sysPep.SetDurableApprovals(approvals)
	sysPath, err := effectpath.NewEffectPath(sysPep, systemMW{},
		effectpath.NewInProcessExecutor(allTools),
		effectpath.NewSandboxedProcessExecutor(execDoor), authority)
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
			if execAdapter != nil {
				// The planner offers exec ONLY behind a passing sandbox
				// probe (deny-default).
				for k, v := range exectool.Spec() {
					specs[k] = v
				}
			}
			return pl.WithTools(specs, profile)
		},
		Authority: authority, Profile: profile,
		Rules:            mergedRules(),
		Tools:            allTools,
		Audit:            &journalAudit{j: j, profile: profile},
		Redactor:         redactor,
		DurableApprovals: approvals,
		Sandbox:          execDoor,
		SuspenderFor: func(identity string) loop.Suspender {
			source := "tg:" + identity
			return func(ctx context.Context, turn contracts.TurnID, run contracts.RunID,
				call contracts.ToolCall, blocks []contracts.ContextBlock) (string, error) {
				ch, err := approvals.Suspend(ctx, turn, run, call, source, blocks)
				if err != nil {
					return "", err
				}
				return ch.Summary, nil
			}
		},
	})
	if err != nil {
		j.Close()
		return nil, err
	}
	return &daemonBundle{d: d, j: j, sched: sched, obl: oblManager,
		chanCore: chanCore, approvals: approvals, profile: profile, cfg: resolved.Config,
		sysPath: sysPath, authority: authority,
		prov: prov, sandboxOK: execAdapter != nil}, nil
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
	// exec is ALWAYS ASK — the rule exists even when no sandbox is bound
	// (the process door then refuses fail-closed regardless of policy).
	for k, v := range exectool.Rules() {
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
	if len(args) > 0 && args[0] == "--p0" {
		return runDoctorP0()
	}
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

// runDoctorP0 grants the P0-capable label from LIVE criteria (T27): each
// of the six PRD §6 capabilities is measured NOW — provider round trip,
// journal open for BOTH profiles, scheduler health, telegram getMe,
// trust-rooted sandbox canary. Anything not live keeps the grant at
// prerequisites-ready.
func runDoctorP0() int {
	layout, resolved, err := resolveEnv()
	if err != nil {
		fmt.Fprintf(os.Stderr, "doctor --p0: %v\n", err)
		return 2
	}
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	type crit struct {
		name string
		ok   bool
		why  string
	}
	var criteria []crit
	add := func(name string, ok bool, why string) { criteria = append(criteria, crit{name, ok, why}) }

	// 1. conversation — LIVE provider round trip.
	authority := s7min.NewAuthority(nil, 5*time.Minute)
	if prov, perr := provider.NewAPIKey(resolved.Config, authority); perr != nil {
		add("conversation", false, perr.Error())
	} else if pr := prov.Probe(ctx, resolved); !pr.Passed {
		add("conversation", false, pr.Detail)
	} else {
		add("conversation", true, "live provider round trip")
	}
	// 2+3+5. memory, obligations, profiles — BOTH profile journals open
	// with projections folded; scheduler health mirror clean.
	profilesOK := true
	for _, prof := range []contracts.ProfileID{"work", "private"} {
		jp, jerr := layout.ProfileJournal(prof)
		if jerr != nil {
			profilesOK = false
			add("profiles", false, jerr.Error())
			break
		}
		profDir, _ := layout.ProfileDir(prof)
		if merr := os.MkdirAll(profDir, 0o700); merr != nil {
			profilesOK = false
			add("profiles", false, merr.Error())
			break
		}
		reg, rerr := obligation.NewRegistry(map[string]obligation.Kind{
			"file_note": {Handler: obligation.FileNoteHandler(profDir), ValidateParams: obligation.ValidateFileNoteParams},
		})
		if rerr != nil {
			profilesOK = false
			add("profiles", false, rerr.Error())
			break
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
		for n, v := range obligation.Events(reg, obligation.NewDoneGate()) {
			events[n] = v
		}
		for n, v := range channel.Events() {
			events[n] = v
		}
		for n, v := range approval.Events() {
			events[n] = v
		}
		j, jerr := journal.Open(jp, prof, redact.None{}, events)
		if jerr != nil {
			profilesOK = false
			add("profiles", false, string(prof)+": "+jerr.Error())
			break
		}
		j.Close()
	}
	if profilesOK {
		add("memory", true, "profile journals open, projections folded")
		add("profiles", true, "work and private journals independently open")
	} else {
		add("memory", false, "profile journal not openable")
	}
	healthPath := filepath.Join(layout.SystemDir(), "scheduler_health")
	if hb, herr := os.ReadFile(healthPath); herr == nil && len(strings.TrimSpace(string(hb))) > 0 {
		add("reminders", false, "scheduler health: "+strings.TrimSpace(string(hb)))
	} else {
		add("reminders", profilesOK, "durable scheduler substrate ready")
	}
	// 4. telegram — token + strict bindings + LIVE getMe.
	probeCore, probeCleanup := mustProbeCore(layout, resolved)
	defer probeCleanup()
	tgOK, tgWhy := false, ""
	if tok := os.Getenv(resolved.Config.TelegramTokenEnv); tok == "" {
		tgWhy = "no bot token in " + resolved.Config.TelegramTokenEnv
	} else if bindings, berr := telegramBindings(os.Getenv("NEXUS_TELEGRAM_BINDINGS")); berr != nil {
		tgWhy = berr.Error()
	} else if len(bindings) == 0 {
		tgWhy = "no chat bindings (NEXUS_TELEGRAM_BINDINGS)"
	} else if adapter, aerr := telegram.New(telegram.Config{
		APIBase: resolved.Config.TelegramAPIBase, TokenEnv: resolved.Config.TelegramTokenEnv,
		Bindings: bindings, Profile: resolved.Config.DefaultProfile,
	}, probeCore, func(context.Context, channel.Inbound) (string, error) { return "", nil }); aerr != nil {
		tgWhy = aerr.Error()
	} else if pr := adapter.Probe(ctx, resolved); !pr.Passed {
		tgWhy = pr.Detail
	} else {
		tgOK, tgWhy = true, "live getMe passed"
	}
	add("telegram", tgOK, tgWhy)
	// 6. sandbox — trust-rooted enforcement canary.
	if _, serr := sandbox.NewBwrap().Probe(ctx); serr != nil {
		add("sandbox", false, serr.Error())
	} else {
		add("sandbox", true, "trust-rooted bwrap, enforcement canary passed")
	}

	// ACCEPTANCE ATTESTATION (T27 codex #3): the doctor never
	// self-certifies the six criteria — the grant additionally requires
	// the out-of-implementation acceptance suite to have PASSED against
	// EXACTLY this binary (sha256-bound attestation from
	// scripts/p0-accept.sh).
	attOK, attWhy := verifyAcceptanceAttestation(layout)
	add("acceptance", attOK, attWhy)

	all := true
	for _, c := range criteria {
		state := "LIVE"
		if !c.ok {
			state, all = "OFF ", false
		}
		fmt.Printf("%s %-14s %s\n", state, c.name, c.why)
	}
	if all {
		fmt.Println("P0-capable — all six PRD §6 capabilities measured live AND the acceptance suite attested this exact binary")
		return 0
	}
	fmt.Println("prerequisites-ready at most — criteria above are not all live (fail closed)")
	return 1
}

// verifyAcceptanceAttestation binds the grant to the graded binary.
func verifyAcceptanceAttestation(layout pathx.Layout) (bool, string) {
	raw, err := os.ReadFile(filepath.Join(layout.SystemDir(), "acceptance.json"))
	if err != nil {
		return false, "no acceptance attestation — run scripts/p0-accept.sh on this host"
	}
	var att struct {
		BinarySHA256 string `json:"binary_sha256"`
		Suite        string `json:"suite"`
		Passed       bool   `json:"passed"`
		Time         string `json:"time"`
	}
	if json.Unmarshal(raw, &att) != nil || !att.Passed || att.Suite != "internal/acceptance" || att.BinarySHA256 == "" {
		return false, "acceptance attestation malformed or not a pass (fail closed)"
	}
	self, err := os.Executable()
	if err != nil {
		return false, err.Error()
	}
	f, err := os.Open(self)
	if err != nil {
		return false, err.Error()
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return false, err.Error()
	}
	if hex.EncodeToString(h.Sum(nil)) != att.BinarySHA256 {
		return false, "this binary is NOT the one the acceptance suite graded (digest mismatch, fail closed)"
	}
	// PROVENANCE (T27-r2 codex #2, hardened per r3 codex #1): the
	// attestation must carry the owner's signature, the trust anchor
	// (allowed_signers) must hash to the fingerprint COMPILED INTO this
	// binary, and the verifier is a FIXED root-owned executable — a
	// key-less attacker can neither replace the anchor nor shim the
	// verifier. A rebuilt binary with a different pin is outside the
	// boundary: the owner installs only signed releases.
	if acceptanceSignerFingerprint == "" {
		return false, "this build carries no pinned acceptance signer (build via scripts/p0-accept.sh)"
	}
	attPath := filepath.Join(layout.SystemDir(), "acceptance.json")
	sigPath := attPath + ".sig"
	signers := filepath.Join(layout.Base, "allowed_signers")
	if _, err := os.Stat(sigPath); err != nil {
		return false, "acceptance attestation is UNSIGNED — run scripts/p0-accept.sh with NEXUS_RELEASE_KEY"
	}
	signersBytes, err := os.ReadFile(signers)
	if err != nil {
		return false, "no allowed_signers file at " + signers + " (install the owner public key)"
	}
	sum := sha256.Sum256(signersBytes)
	if hex.EncodeToString(sum[:]) != acceptanceSignerFingerprint {
		return false, "allowed_signers does NOT match the fingerprint pinned in this binary (trust anchor replaced, fail closed)"
	}
	const verifier = "/usr/bin/ssh-keygen"
	vst, err := os.Stat(verifier)
	if err != nil {
		return false, verifier + " missing (openssh-client is a declared prerequisite)"
	}
	if sys, ok := vst.Sys().(*syscall.Stat_t); !ok || sys.Uid != 0 || vst.Mode().Perm()&0o022 != 0 {
		return false, verifier + " is not a root-owned, non-writable executable (fail closed)"
	}
	attFile, err := os.Open(attPath)
	if err != nil {
		return false, err.Error()
	}
	defer attFile.Close()
	// The verifier must see EXACTLY the bytes the fingerprint check
	// hashed — never re-read the mutable path (a swap between the hash
	// and the verify call would otherwise slip through; T27-r4 codex).
	pinnedDir, err := os.MkdirTemp("", "nexus-signers-")
	if err != nil {
		return false, err.Error()
	}
	defer os.RemoveAll(pinnedDir)
	pinnedSigners := filepath.Join(pinnedDir, "allowed_signers")
	if err := os.WriteFile(pinnedSigners, signersBytes, 0o600); err != nil {
		return false, err.Error()
	}
	cmd := exec.Command(verifier, "-Y", "verify", "-f", pinnedSigners, "-I", "owner",
		"-n", "nexus-acceptance", "-s", sigPath)
	cmd.Stdin = attFile
	if out, err := cmd.CombinedOutput(); err != nil {
		return false, "attestation signature verification FAILED: " + strings.TrimSpace(string(out))
	}
	return true, "signed acceptance pass for this exact binary (" + att.Time + ")"
}

// mustProbeCore opens a throwaway channel core for the doctor's live
// telegram probe (never the production journal).
func mustProbeCore(layout pathx.Layout, resolved config.Resolved) (*channel.Core, func()) {
	dir, err := os.MkdirTemp("", "nexus-doctor-")
	if err != nil {
		return nil, func() {}
	}
	cleanup := func() { os.RemoveAll(dir) }
	j, err := journal.Open(filepath.Join(dir, "probe.db"), resolved.Config.DefaultProfile,
		redact.None{}, channel.Events(), channel.NewProjection())
	if err != nil {
		cleanup()
		return nil, func() {}
	}
	c, err := channel.New(j)
	if err != nil {
		j.Close()
		cleanup()
		return nil, func() {}
	}
	return c, func() { j.Close(); cleanup() }
}

// deliverPendingReminders is ONE pass of the reminder delivery loop:
// enqueue under the occurrence-stable id (idempotent), then mint the
// delivery receipt ONLY from the channel owner's proven SENT transition
// (T27 codex #2 / kilo #3 — an enqueue or an UNKNOWN in-flight row is
// never delivery evidence).
func (b *daemonBundle) deliverPendingReminders(ctx context.Context, ownerChat string) {
	pending, perr := b.obl.PendingDeliveries(ctx)
	if perr != nil {
		return
	}
	for _, d := range pending {
		dlvID := deliveryIDFor(d.OccurrenceID)
		if _, qerr := b.chanCore.EnqueueReplyID(ctx, dlvID, "telegram", ownerChat, b.profile,
			"Reminder: "+d.Body+" (reply: ack "+d.OccurrenceID+")"); qerr != nil {
			continue
		}
		st, serr := b.chanCore.DeliveryStatus(ctx, dlvID)
		if serr != nil || st != "SENT" {
			continue // retry next tick; UNKNOWN reconciles
		}
		if merr := b.obl.MarkDelivered(ctx, d.OccurrenceID,
			obligation.DeliveryReceipt{Producer: "telegram", ReceiptID: dlvID}); merr != nil {
			fmt.Fprintf(os.Stderr, "nexus daemon: reminder delivery mark %s: %v\n", d.OccurrenceID, merr)
		}
	}
}

// deliveryIDFor derives the STABLE outbox delivery id of an occurrence.
func deliveryIDFor(occ string) string {
	sum := sha256.Sum256([]byte("reminder-delivery|" + occ))
	return "dlv-" + hex.EncodeToString(sum[:12])
}

// telegramBindings parses NEXUS_TELEGRAM_BINDINGS ("chatid=profile,...")
// STRICTLY (Phase-5 codex #13): a malformed pair, unparsable chat id,
// empty profile, or duplicate chat id is a loud error — never a silent
// skip or last-write-wins remap. Both sides of '=' are trimmed.
func telegramBindings(raw string) (map[int64]string, error) {
	out := map[int64]string{}
	for _, pair := range strings.Split(raw, ",") {
		pair = strings.TrimSpace(pair)
		if pair == "" {
			continue
		}
		i := strings.IndexByte(pair, '=')
		if i <= 0 {
			return nil, fmt.Errorf("telegram bindings: malformed pair %q (want chatid=profile)", pair)
		}
		chat, err := strconv.ParseInt(strings.TrimSpace(pair[:i]), 10, 64)
		if err != nil {
			return nil, fmt.Errorf("telegram bindings: chat id in %q is not a number", pair)
		}
		if chat <= 0 {
			// Telegram PRIVATE chats have positive ids; negative ids are
			// groups/supergroups — a reminder routed there would leak to
			// every member (T27-r2 codex #3, single-user boundary).
			return nil, fmt.Errorf("telegram bindings: chat id %d is not a private chat (groups are refused, fail closed)", chat)
		}
		profile := strings.TrimSpace(pair[i+1:])
		if profile == "" {
			return nil, fmt.Errorf("telegram bindings: empty profile in %q", pair)
		}
		if prev, dup := out[chat]; dup {
			return nil, fmt.Errorf("telegram bindings: chat %d bound twice (%q and %q) — ambiguous", chat, prev, profile)
		}
		out[chat] = profile
	}
	return out, nil
}

// sealStartupSnapshot builds the T11 sealed capability snapshot (T27:
// the static provider/store fakes are GONE — every entry is a live
// measurement supplied by the caller; a missing probe leaves its
// capability OFF, fail closed).
func sealStartupSnapshot(resolved config.Resolved, live []closure.ProbeResult, requested []string) (*closure.Snapshot, error) {
	return closure.Seal(closure.P0Capabilities(), requested, live, resolved)
}

// liveProbes measures every capability the daemon can prove RIGHT NOW:
// journal open (store), a real provider round trip, the trust-rooted
// sandbox enforcement canary, and — when configured — the channel getMe.
func (b *daemonBundle) liveProbes(ctx context.Context, resolved config.Resolved,
	prov *provider.APIKey, sandboxOK bool, channelProbe *closure.ProbeResult) ([]closure.ProbeResult, []string) {
	hash := resolved.ConfigHash()
	probes := []closure.ProbeResult{
		// The journal IS open with all projections folded — buildDaemon
		// cannot construct this bundle otherwise (a real, live fact).
		{Name: "store", Passed: true, Detail: "journal open, projections folded", ConfigHash: hash},
		prov.Probe(ctx, resolved),
	}
	if sandboxOK {
		probes = append(probes, closure.ProbeResult{Name: "sandbox", Passed: true,
			Detail: "trust-rooted bwrap, enforcement canary passed", ConfigHash: hash})
	}
	requested := []string{"conversation", "memory", "obligations", "profiles"}
	if sandboxOK {
		requested = append(requested, "exec")
	}
	if channelProbe != nil {
		probes = append(probes, *channelProbe)
		requested = append(requested, "telegram")
	}
	return probes, requested
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
				// Crash-window recovery (Phase-5-r2 codex #2): if the
				// receipt already committed for THIS source but the resume
				// never ran, a replayed approve completes it instead of
				// stranding the action. A foreign source still fails here
				// because SuspendedCall is source-bound.
				if _, _, _, _, aerr := b.approvals.SuspendedCall(ctx, id, source); aerr != nil {
					return "Approval failed: " + err.Error(), nil
				}
			}
			// RESUME (T24): execute the exact approved effect now.
			result, rerr := b.resumeApproved(ctx, in.ChannelIdentity, id)
			if rerr != nil {
				return "Approved " + id + " but the resume failed: " + rerr.Error(), nil
			}
			return "Approved " + id + ". " + result, nil
		case strings.HasPrefix(lower, "retry "):
			// E9 reconcile for a CONSUMED-but-unfinished resume: the user
			// confirms by re-approving a FRESH challenge over the same
			// exact intent — never a blind auto-retry (Phase-5-r3 kilo #1).
			id := strings.TrimSpace(text[len("retry "):])
			// ONE atomic batch closes the old reconcile item and issues
			// the fresh challenge (Phase-5-r4 codex #1) — a crash can
			// never leave two consumable authorizations.
			ch, rerr := b.approvals.RetryChallenge(ctx, id, source)
			if rerr != nil {
				return "Nothing to retry for " + id + ": " + rerr.Error(), nil
			}
			return ch.Summary, nil
		case strings.HasPrefix(lower, "deny "):
			id := strings.TrimSpace(text[len("deny "):])
			if err := b.approvals.Deny(ctx, id, source); err != nil {
				return "Denial failed: " + err.Error(), nil
			}
			return "Denied " + id + ".", nil
		case strings.HasPrefix(lower, "ack "):
			// Direct occurrence ack (B5: the ack correlates to the EXACT
			// occurrence id shown in the delivery message). A user acking
			// within the delivery loop's tick window races the receipt
			// mint (T27-r2 agy #1) — the ack itself carries the proof the
			// message arrived, so settle the SENT-proven receipt inline
			// first (same SENT gate; never a fabricated receipt).
			occ := strings.TrimSpace(text[len("ack "):])
			if st, serr := b.chanCore.DeliveryStatus(ctx, deliveryIDFor(occ)); serr == nil && st == "SENT" {
				b.obl.MarkDelivered(ctx, occ, obligation.DeliveryReceipt{
					Producer: "telegram", ReceiptID: deliveryIDFor(occ)}) // idempotent; already-delivered is fine
			}
			if err := b.obl.MarkAcked(ctx, occ, obligation.AckGesture{Source: source}); err != nil {
				return "Ack failed: " + err.Error(), nil
			}
			return "Acknowledged " + occ + ".", nil
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
		// spine (ModeDefault — channel input can NEVER carry yolo). The
		// suspender binds any approval challenge to THIS chat as the only
		// legal decision source.
		reply, err := b.d.RunChannelTurn(ctx, in.ChannelIdentity, in.UpdateID, text)
		if err != nil {
			return "", err
		}
		return reply, nil
	}
}
