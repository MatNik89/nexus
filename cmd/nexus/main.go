// Command nexus is the NEXUS personal-assistant kernel binary.
package main

import (
	"bytes"
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
	"regexp"
	"strconv"
	"strings"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	"github.com/MatNik89/nexus/internal/app/daemon"
	"github.com/MatNik89/nexus/internal/app/repl"
	"github.com/MatNik89/nexus/internal/approval"
	"github.com/MatNik89/nexus/internal/channel"
	"github.com/MatNik89/nexus/internal/channel/health"
	"github.com/MatNik89/nexus/internal/channel/telegram"
	"github.com/MatNik89/nexus/internal/coding/runner"
	"github.com/MatNik89/nexus/internal/coding/symedit"
	"github.com/MatNik89/nexus/internal/coding/workspace"
	"github.com/MatNik89/nexus/internal/conv"
	"github.com/MatNik89/nexus/internal/exectool"
	"github.com/MatNik89/nexus/internal/foundation/atomicwrite"
	"github.com/MatNik89/nexus/internal/foundation/clockid"
	"github.com/MatNik89/nexus/internal/foundation/config"
	"github.com/MatNik89/nexus/internal/foundation/egress"
	"github.com/MatNik89/nexus/internal/foundation/pathx"
	"github.com/MatNik89/nexus/internal/foundation/sealedstore"
	"github.com/MatNik89/nexus/internal/kernel/closure"
	"github.com/MatNik89/nexus/internal/kernel/contracts"
	"github.com/MatNik89/nexus/internal/kernel/effectpath"
	"github.com/MatNik89/nexus/internal/kernel/journal"
	"github.com/MatNik89/nexus/internal/kernel/loop"
	"github.com/MatNik89/nexus/internal/kernel/machine"
	"github.com/MatNik89/nexus/internal/kernel/s7"
	"github.com/MatNik89/nexus/internal/llm/planner"
	"github.com/MatNik89/nexus/internal/llm/provider"
	"github.com/MatNik89/nexus/internal/memory"
	"github.com/MatNik89/nexus/internal/obligation"
	"github.com/MatNik89/nexus/internal/preflight/doctor"
	"github.com/MatNik89/nexus/internal/sandbox"
	"github.com/MatNik89/nexus/internal/schedule"
	"github.com/MatNik89/nexus/internal/security/redact"
	"golang.org/x/sys/unix"
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

// s7Now is the S7 clock (nil = time.Now); tests pin it to drive backoff
// deterministically through the real composition.
var s7Now func() time.Time

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
	if b.sealed != nil {
		defer b.sealed.Close()
	}
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
			// /cronjob shared calendar store (ephemeral, 15-min TTL) — wired
			// into both the adapter (UI) and the handler (durable commit).
			b.picker = telegram.NewPickerStore(15 * time.Minute)
			pickerLoc, plerr := time.LoadLocation(resolved.Config.Timezone)
			if plerr != nil {
				pickerLoc = time.UTC
			}
			adapter, aerr := telegram.New(telegram.Config{
				APIBase:     resolved.Config.TelegramAPIBase,
				TokenEnv:    resolved.Config.TelegramTokenEnv,
				Bindings:    bindings,
				Profile:     b.profile,
				EgressAllow: resolved.Config.EgressAllow,
				Receipt:     b.egressSink,
				Authority:   b.authority,
				Health:      b.health,
			}, b.chanCore, telegramHandler(b))
			if aerr != nil {
				fmt.Fprintf(os.Stderr, "nexus daemon: telegram: %v\n", aerr)
			} else {
				adapter.SetPicker(b.picker, pickerLoc)
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
			adapterCtx, adapterCancel := context.WithCancel(ctx)
			go func() {
				// Supervisor (Slice D): the adapter's typed terminal class is
				// persisted UNCHANGED (remote_rejected stays remote_rejected);
				// substrate only for an unclassified return or a recovered
				// panic. The adapter context is cancelled for prompt teardown.
				// Sealed-capability rule: health is REPORTED, never a fallback.
				defer adapterCancel()
				defer func() {
					if r := recover(); r != nil {
						_ = b.health.Report("telegram", health.ClassSubstrate, "panic", fmt.Sprint(r), true)
						fmt.Fprintf(os.Stderr, "nexus daemon: telegram adapter PANICKED (capability degraded): %v\n", r)
					}
				}()
				if err := tgAdapter.Run(adapterCtx, 2*time.Second); err != nil {
					cls, code := channel.ClassOf(err)
					// The stderr line is the final fallback when even the health
					// projection cannot be written (never a silent stop).
					if herr := b.health.Report("telegram", cls, code, err.Error(), true); herr != nil {
						fmt.Fprintf(os.Stderr, "nexus daemon: health projection write FAILED: %v\n", herr)
					}
					fmt.Fprintf(os.Stderr, "nexus daemon: telegram adapter STOPPED (%s/%s): %v\n", cls, code, err)
				}
			}()
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
	d          *daemon.Daemon
	j          *journal.Journal
	sched      *schedule.Scheduler
	obl        *obligation.Manager
	chanCore   *channel.Core
	egressSink egress.ReceiptSink
	health     *health.Owner
	approvals  *approval.Store
	profile    contracts.ProfileID
	cfg        config.Config
	sysPath    *effectpath.EffectPath
	authority  *s7.Authority
	prov       *provider.APIKey
	sandboxOK  bool
	// sealed is rename_symbol's before/after-image store; nil when the
	// tool could not be bound (no sandbox probe, no go/gopls on PATH).
	sealed *sealedstore.Store
	// picker is the shared /cronjob calendar store: the adapter drives the
	// ephemeral UI, the handler turns a completed pick into a durable reminder.
	picker *telegram.PickerStore
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
	if testing.Testing() && os.Getenv("NEXUS_TEST_CLOSE_JOURNAL_POST_EFFECT") != "" {
		// Test seam (fresh-audit codex #1): fault injection at the exact
		// post-effect boundary — the effect committed, durability fails.
		b.j.Close()
	}
	final, ferr := b.d.ResumeChannelTurn(ctx, identity, turn, run, challengeID, continuation)
	if merr := b.approvals.MarkResumeCompleted(ctx, challengeID); merr != nil {
		// The effect COMMITTED but its durable completion record did not
		// — an E9 UNKNOWN (fresh-audit codex #1). Never plain success:
		// the challenge replays as consumed-unfinished at the next
		// startup, and a retry invitation here would re-authorize an
		// already-committed effect.
		return "", fmt.Errorf("the approved action EXECUTED (result: %s), but recording its completion failed: %w. Do NOT retry %s — the daemon reconciles it at the next startup", result, merr, challengeID)
	}
	if ferr != nil {
		// The effect DID run, the approval is consumed and durably
		// closed — report the tool result honestly even when the
		// continuation fails.
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
	// Conversational clock zone (fail-closed): the model sees the local
	// time in this zone. Config validation already rejected Local/""/
	// unloadable, but resolve here so a bad value never starts the daemon.
	convLoc, tzErr := time.LoadLocation(resolved.Config.Timezone)
	if tzErr != nil {
		return nil, fmt.Errorf("timezone %q: %w", resolved.Config.Timezone, tzErr)
	}
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
	for n, v := range conv.Events() {
		events[n] = v
	}
	for n, v := range approval.Events() {
		events[n] = v
	}
	for n, v := range s7.Events() {
		events[n] = v
	}
	for n, v := range runner.Events() {
		events[n] = v
	}
	for n, v := range workspace.Events() {
		events[n] = v
	}

	journalPath, _ := layout.ProfileJournal(profile)
	redactor := redact.NewKnownRefs(knownSecretRefs(resolved.Config))
	// The ONE profile database: journal + memory projection together
	// (Annex P0.3 — facts are journal events folded in the same
	// transaction; no second SQLite file exists).
	j, err := journal.Open(journalPath, profile, redactor, events, memory.NewProjection(), schedule.NewProjection(), obligation.NewProjection(), channel.NewProjection(), approval.NewProjection(), conv.NewProjection(), s7.NewProjection())
	if err != nil {
		return nil, fmt.Errorf("journal: %w", err)
	}
	// Full S7 (Slice B1): the durable-capable authority bound to the profile
	// journal — it rehydrates every non-terminal durable operation
	// (deliveries, command registration) before anything can ask for a grant.
	authority, err := s7.New(j, s7Now, 5*time.Minute)
	if err != nil {
		j.Close()
		return nil, fmt.Errorf("s7: %w", err)
	}
	// ONE E11 receipt sink for every outbound component (Slice A): built
	// right after the journal, BEFORE the provider or any adapter exists.
	egressSink := channel.EgressSink(j)
	// ONE channel-health owner (Slice D): a journal-independent projection
	// under the system dir, so a journal failure is still reportable.
	healthOwner, err := health.New(filepath.Join(layout.SystemDir(), "channel_health.json"), os.Stderr)
	if err != nil {
		j.Close()
		return nil, fmt.Errorf("health: %w", err)
	}
	prov, err := provider.NewAPIKey(resolved.Config, authority, egressSink)
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
	// REAL sandbox (T25/T26): probe bwrap; a passing probe binds the exec
	// tool, a failing one leaves every ExecProcess call fail-closed and
	// the exec capability OFF (deny-default).
	var execAdapter *exectool.Adapter
	sbBackend := sandbox.NewBwrap()
	sbReport, sbErr := sbBackend.Probe(context.Background())
	sandboxProbeOK := sbErr == nil
	if sandboxProbeOK {
		if ad, aerr := exectool.New(sbBackend, sbReport, redactor, resolved.Config.ExecAllow); aerr == nil {
			execAdapter = ad
		}
	}
	var execDoor effectpath.SandboxBackend
	if execAdapter != nil {
		execDoor = execAdapter
	}
	// rename_symbol (S6 tool-boundary, piece 2): ONLY bound when the
	// sandbox probe passed (its own governed subprocess launches rely on
	// the SAME probe report exectool uses) AND both the go and gopls
	// binaries are resolvable on the host — same deny-default pattern as
	// execAdapter above, never a hard startup failure.
	var symeditTools map[contracts.ToolID]effectpath.InProcFunc
	var sealed *sealedstore.Store
	if sandboxProbeOK {
		goBinary, goErr := exec.LookPath("go")
		goplsBinary, goplsErr := exec.LookPath("gopls")
		if goErr == nil && goplsErr == nil {
			sealedDir := filepath.Join(profileDir, "sealed")
			if pathx.EnsureDir(layout.Base, sealedDir) == nil {
				if s, serr := sealedstore.Open(sealedDir); serr == nil {
					// Growth bound (codex round-2 review finding: no
					// production caller invokes Store.GC yet — see
					// docs/SECTION-MAP.md's own deferred-GC rationale).
					// One apply = one Put (transaction.go's own single
					// whole-bundle write) — 2000 entries bounds growth to
					// roughly 2000 total renames before further applies
					// fail closed, generous for single-user volume while
					// never risking deleting a possibly-live artifact.
					s.MaxEntries = 2000
					// A count alone does not bound disk USAGE: one entry
					// can be up to ~500MB (runner.MaxSnapshotBytes,
					// base64-encoded into the bundle) — 8GiB total bounds
					// worst-case growth to a bounded fraction of typical
					// disk (codex round-3 review finding).
					s.MaxTotalBytes = 8 << 30
					sealed = s
					workspaceRoot := func(p contracts.ProfileID) (string, bool) {
						return resolved.Config.CodingWorkspaceRoot(p)
					}
					// Restart-time discovery (S6 tool-boundary piece 2,
					// codex round-2 review finding: piece 2 is the FIRST
					// thing that makes ApplyGoverned production-reachable,
					// so a crash mid-apply previously had NO startup-time
					// visibility at all). RestartScan is read-only by its
					// own design (scan.go's own doc comment: "discovery +
					// classification... nothing more") — it takes no S7
					// action and performs no rollback. codex round-3
					// review, HIGH: log-only was not enough on its own —
					// "at minimum unresolved/error states must disable
					// mutation" — so an unresolved finding, OR the scan
					// itself failing to run at all (OpenRoot/RestartScan
					// error), now REFUSES to bind the mutation tools
					// (fail closed) rather than merely logging and
					// continuing. Automatically DRIVING RollbackGoverned
					// from a finding remains a separate, deliberately
					// deferred recovery-driver obligation
					// (docs/SECTION-MAP.md) — an operator must resolve it
					// (or a future driver must exist) before rename_symbol
					// becomes available again.
					scanClean := true
					if root, ok := workspaceRoot(profile); ok {
						rootFd, oerr := workspace.OpenRoot(root)
						if oerr != nil {
							scanClean = false
							fmt.Fprintf(os.Stderr, "nexus daemon: coding-workspace restart scan could not open the workspace root for profile %q (rename_symbol disabled until resolved): %v\n", profile, oerr)
						} else {
							findings, serr := workspace.RestartScan(rootFd, j, authority, sealed)
							unix.Close(rootFd)
							if serr != nil {
								scanClean = false
								fmt.Fprintf(os.Stderr, "nexus daemon: coding-workspace restart scan failed for profile %q (rename_symbol disabled until resolved): %v\n", profile, serr)
							} else if len(findings) > 0 {
								scanClean = false
								fmt.Fprintf(os.Stderr, "nexus daemon: coding-workspace restart scan found %d unresolved operation(s) for profile %q — rename_symbol disabled until manually reconciled:\n", len(findings), profile)
								for _, f := range findings {
									fmt.Fprintf(os.Stderr, "  op=%s state=%v\n", f.Op, f.State)
								}
							}
						}
					}
					if scanClean {
						symeditTools = symedit.Tools(workspaceRoot, goBinary, goplsBinary, sealed, sbBackend, sbReport, authority, j)
					}
				}
			}
		}
	}
	allTools := mergedTools(memStore, redactor, oblManager, symeditTools)
	sysPep, err := effectpath.NewPEP(mergedRules(), effectpath.NewApprovals(nil, 5*time.Minute),
		&journalAudit{j: j, profile: profile}, effectpath.ModeDefault)
	if err != nil {
		if sealed != nil {
			sealed.Close()
		}
		j.Close()
		return nil, err
	}
	sysPep.SetDurableApprovals(approvals)
	sysPath, err := effectpath.NewEffectPath(sysPep, systemMW{},
		effectpath.NewInProcessExecutor(allTools),
		effectpath.NewSandboxedProcessExecutor(execDoor), authority)
	if err != nil {
		if sealed != nil {
			sealed.Close()
		}
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
			pl, err := planner.NewStreaming(prov, prov, authority, target, deliver, resolved.Config.ContextHardLimitTokens)
			if err != nil {
				return nil, err
			}
			pl.SetClock(convLoc, time.Now)
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
			if symeditTools != nil {
				// The planner offers rename_symbol ONLY when it was
				// actually bound above (deny-default — same discipline).
				for k, v := range symedit.Specs() {
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
		if sealed != nil {
			sealed.Close()
		}
		j.Close()
		return nil, err
	}
	return &daemonBundle{d: d, j: j, sched: sched, obl: oblManager,
		chanCore: chanCore, egressSink: egressSink, health: healthOwner, approvals: approvals, profile: profile, cfg: resolved.Config,
		sysPath: sysPath, authority: authority,
		prov: prov, sandboxOK: execAdapter != nil, sealed: sealed}, nil
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
	// rename_symbol's rules exist regardless of whether the tool itself is
	// bound (an unbound tool is simply never offered to the planner —
	// registering its rule here costs nothing and keeps mergedRules the
	// single source of truth PEP consults).
	for k, v := range symedit.Rules() {
		rules[k] = v
	}
	return rules
}

// mergedTools combines every tool family's handlers. symeditTools is nil
// when rename_symbol could not be bound (no sandbox probe, no go/gopls on
// PATH, or the sealed store failed to open) — deny-default, not a startup
// failure.
func mergedTools(memStore *memory.Store, r redact.Redactor, m *obligation.Manager, symeditTools map[contracts.ToolID]effectpath.InProcFunc) map[contracts.ToolID]effectpath.InProcFunc {
	tools := memory.Tools(memStore, r)
	for k, v := range obligation.Tools(m) {
		tools[k] = v
	}
	for k, v := range symeditTools {
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
	checks := doctorChecks()
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

// doctorChecks runs the ordinary doctor against the RESOLVED configuration
// (Slice E, AUDIT-FULL F7): the secret-reference names come from the same
// resolver the daemon uses, so a custom provider_key_env / telegram_token_env
// is checked by ITS name. A configuration that does not resolve is itself a
// finding (the daemon would refuse it too), never a crash and never a silent
// fallback to the default names.
func doctorChecks() []doctor.Check {
	var pre []doctor.Check
	secrets := doctor.Secrets{}
	if _, resolved, err := resolveEnv(); err != nil {
		pre = append(pre, doctor.Check{Name: "config", Capability: "stateful-startup", Status: doctor.StatusOff,
			Detail: "configuration does not resolve: " + err.Error(),
			Fix:    "fix config.json / NEXUS_CFG_* (the daemon refuses the same configuration)"})
	} else {
		secrets = doctor.Secrets{ProviderKeyEnv: resolved.Config.ProviderKeyEnv, TelegramTokenEnv: resolved.Config.TelegramTokenEnv}
	}
	env, err := doctor.DefaultEnv(secrets)
	if err != nil {
		return append(pre, doctor.Check{Name: "host", Capability: "stateful-startup", Status: doctor.StatusOff,
			Detail: err.Error(), Fix: "set HOME or XDG_CONFIG_HOME to a writable directory"})
	}
	return append(pre, doctor.Run(env)...)
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
		name  string
		ok    bool
		state string // LIVE = measured round trip now; READY = durable substrate verified
		why   string
	}
	var criteria []crit
	add := func(name string, ok bool, why string) { criteria = append(criteria, crit{name, ok, "LIVE", why}) }
	addReady := func(name string, ok bool, why string) { criteria = append(criteria, crit{name, ok, "READY", why}) }

	// Probe substrate (throwaway journal) — also the E11 receipt sink for the
	// doctor's live provider/telegram probes.
	probeCore, probeSink, probeHealth, probeCleanup := mustProbeCore(layout, resolved)
	defer probeCleanup()
	// 1. conversation — LIVE provider round trip.
	authority := s7.NewAuthority(nil, 5*time.Minute)
	if prov, perr := provider.NewAPIKey(resolved.Config, authority, probeSink); perr != nil {
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
		j, jerr := journal.Open(jp, prof, redact.None{}, events,
			memory.NewProjection(), schedule.NewProjection(), obligation.NewProjection(),
			channel.NewProjection(), approval.NewProjection())
		if jerr != nil {
			profilesOK = false
			add("profiles", false, string(prof)+": "+jerr.Error())
			break
		}
		j.Close()
	}
	if profilesOK {
		addReady("memory", true, "profile journals open, projections folded — durable substrate, not a live round trip")
		addReady("profiles", true, "work and private journals independently open")
	} else {
		addReady("memory", false, "profile journal not openable")
	}
	// Reminders honesty (fresh-audit codex #3 r2): the pure decision is
	// factored out so every branch has a RED-capable unit test.
	health := ""
	if hb, herr := os.ReadFile(filepath.Join(layout.SystemDir(), "scheduler_health")); herr == nil {
		health = strings.TrimSpace(string(hb))
	}
	hbAge, hbExists := time.Duration(0), false
	if hbStat, herr := os.Stat(filepath.Join(layout.SystemDir(), "heartbeat")); herr == nil {
		hbAge, hbExists = time.Since(hbStat.ModTime()), true
	}
	rOK, rState, rWhy := reminderReadiness(health, hbAge, hbExists, profilesOK)
	criteria = append(criteria, crit{"reminders", rOK, rState, rWhy})
	// 4. telegram — token + strict bindings + LIVE getMe.
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
		EgressAllow: resolved.Config.EgressAllow, Receipt: probeSink, Authority: authority, Health: probeHealth,
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
		state := c.state
		if !c.ok {
			state, all = "OFF", false
		}
		fmt.Printf("%-5s %-14s %s\n", state, c.name, c.why)
	}
	if all {
		fmt.Println("P0-capable — live probes passed (conversation, telegram, sandbox), durable substrates verified (memory, obligations, profiles), and the acceptance suite attested this exact binary")
		return 0
	}
	fmt.Println("prerequisites-ready at most — criteria above are not all live (fail closed)")
	return 1
}

// reminderReadiness is the PURE reminders-criterion decision (fresh-audit
// codex #3 r2 — every branch unit-testable): a dirty scheduler-health
// mirror always fails; a FRESH daemon heartbeat promotes the claim to
// live scheduling; a STALE heartbeat is a hard failure (the daemon
// died); no heartbeat at all is the pre-daemon install flow — substrate
// READY only.
func reminderReadiness(health string, hbAge time.Duration, hbExists, profilesOK bool) (bool, string, string) {
	switch {
	case health != "":
		return false, "OFF", "scheduler health: " + health
	case hbExists && hbAge > 30*time.Second:
		return false, "OFF", "daemon heartbeat is STALE (daemon dead?) — scheduler not running"
	case hbExists:
		return profilesOK, "LIVE", "daemon heartbeat fresh, scheduler health clean"
	default:
		return profilesOK, "READY", "durable scheduler substrate ready — no daemon running yet"
	}
}

// verifyAcceptanceAttestation binds the grant to the graded binary.
func verifyAcceptanceAttestation(layout pathx.Layout) (bool, string) {
	// GENERATION POINTER (fresh-audit codex #4 r3): the trust set
	// (anchor + attestation + signature) lives in one generation
	// directory; <config>/trust/current is a symlink switched by a
	// SINGLE atomic rename in p0-accept.sh — a kill at ANY point of
	// publication leaves the previous complete generation installed.
	// The pointer target is resolved ONCE and must be a bare directory
	// name (no path traversal).
	trustDir := filepath.Join(layout.Base, "trust")
	genName, lerr := os.Readlink(filepath.Join(trustDir, "current"))
	if lerr != nil {
		return false, "no acceptance trust generation — run scripts/p0-accept.sh on this host"
	}
	if strings.ContainsAny(genName, "/\\") || genName == "." || genName == ".." {
		return false, "trust generation pointer is not a bare directory name (fail closed)"
	}
	genDir := filepath.Join(trustDir, genName)
	raw, err := os.ReadFile(filepath.Join(genDir, "acceptance.json"))
	if err != nil {
		return false, "no acceptance attestation — run scripts/p0-accept.sh on this host"
	}
	var att struct {
		BinarySHA256 string `json:"binary_sha256"`
		Suite        string `json:"suite"`
		Passed       bool   `json:"passed"`
		Host         string `json:"host"`
		MachineID    string `json:"machine_id_sha256"`
		Time         string `json:"time"`
	}
	if json.Unmarshal(raw, &att) != nil || !att.Passed || att.Suite != "internal/acceptance+probe+sandbox" || att.BinarySHA256 == "" {
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
	// HOST binding (P0-prep #2): the attestation is valid only on the
	// host that ran the acceptance suite — copying the pair to another
	// machine does not transfer the grant.
	host, herr := runtimeHostID()
	if herr != nil {
		return false, "cannot read the host identity (fail closed): " + herr.Error()
	}
	if att.Host != host {
		return false, "acceptance attestation was produced on a DIFFERENT host (fail closed) — run scripts/p0-accept.sh here"
	}
	// MACHINE binding (P0-prep-r1 codex #1): the kernel tuple is not
	// unique — the attestation also carries sha256(/etc/machine-id),
	// stable and root-owned per machine.
	mid, merr := machineIDSHA()
	if merr != nil {
		return false, "cannot read /etc/machine-id (fail closed): " + merr.Error()
	}
	if att.MachineID != mid {
		return false, "acceptance attestation was produced on a DIFFERENT machine (machine-id mismatch, fail closed)"
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
	sigPath := filepath.Join(genDir, "acceptance.json.sig")
	signers := filepath.Join(genDir, "allowed_signers")
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
	// SAME byte-binding rule for the attestation itself (T27-r4 codex,
	// same class): the bytes whose fields granted are the bytes the
	// signature verifies — `raw` was read ONCE at the top; the mutable
	// path is never reopened.
	pinnedDir, err := os.MkdirTemp("", "nexus-signers-")
	if err != nil {
		return false, err.Error()
	}
	defer os.RemoveAll(pinnedDir)
	pinnedSigners := filepath.Join(pinnedDir, "allowed_signers")
	if err := os.WriteFile(pinnedSigners, signersBytes, 0o600); err != nil {
		return false, err.Error()
	}
	// The signature file too is pinned to one read.
	sigBytes, err := os.ReadFile(sigPath)
	if err != nil {
		return false, err.Error()
	}
	pinnedSig := filepath.Join(pinnedDir, "acceptance.json.sig")
	if err := os.WriteFile(pinnedSig, sigBytes, 0o600); err != nil {
		return false, err.Error()
	}
	cmd := exec.Command(verifier, "-Y", "verify", "-f", pinnedSigners, "-I", "owner",
		"-n", "nexus-acceptance", "-s", pinnedSig)
	cmd.Stdin = bytes.NewReader(raw)
	if out, err := cmd.CombinedOutput(); err != nil {
		return false, "attestation signature verification FAILED: " + strings.TrimSpace(string(out))
	}
	return true, "signed acceptance pass for this exact binary (" + att.Time + ")"
}

// runtimeHostID formats the running kernel identity exactly as
// `uname -srm` does (the attestation's host field).
func runtimeHostID() (string, error) {
	var u syscall.Utsname
	if err := syscall.Uname(&u); err != nil {
		return "", err
	}
	conv := func(f [65]int8) string {
		b := make([]byte, 0, 65)
		for _, c := range f {
			if c == 0 {
				break
			}
			b = append(b, byte(c))
		}
		return string(b)
	}
	return conv(u.Sysname) + " " + conv(u.Release) + " " + conv(u.Machine), nil
}

// machineIDSHA is the sha256 of the machine's stable identity file.
// The file must be TRUSTWORTHY (regular, root-owned, not group/world
// writable) and VALID (exactly one nonzero 32-lowercase-hex id) — a
// template, empty, or user-writable identity would let two machines
// share a binding or a local writer choose one (P0-prep-r2 codex #1).
func machineIDSHA() (string, error) { return machineIDSHAAt("/etc/machine-id") }

func machineIDSHAAt(path string) (string, error) {
	st, err := os.Lstat(path)
	if err != nil {
		return "", err
	}
	if !st.Mode().IsRegular() {
		return "", fmt.Errorf("%s is not a regular file (fail closed)", path)
	}
	sys, ok := st.Sys().(*syscall.Stat_t)
	if !ok || sys.Uid != 0 || st.Mode().Perm()&0o022 != 0 {
		return "", fmt.Errorf("%s is not a root-owned, non-writable identity file (fail closed)", path)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	id := trimMachineID(string(raw))
	if err := validateMachineID(id); err != nil {
		return "", fmt.Errorf("%s: %w", path, err)
	}
	sum := sha256.Sum256([]byte(id))
	return hex.EncodeToString(sum[:]), nil
}

// trimMachineID strips EXACTLY the ASCII set [ \t\r\n] — the shared
// trim contract with scripts/machine-id-check.sh (prep-low codex). Never
// locale/Unicode whitespace: a U+00A0-padded id stays malformed on BOTH
// sides instead of diverging.
func trimMachineID(raw string) string {
	return strings.Trim(raw, " \t\r\n")
}

// validateMachineID enforces the systemd machine-id shape.
func validateMachineID(id string) error {
	if len(id) != 32 {
		return fmt.Errorf("machine id must be exactly 32 hex chars (fail closed)")
	}
	nonzero := false
	for _, c := range id {
		if !(c >= '0' && c <= '9' || c >= 'a' && c <= 'f') {
			return fmt.Errorf("machine id must be lowercase hex (fail closed)")
		}
		if c != '0' {
			nonzero = true
		}
	}
	if !nonzero {
		return fmt.Errorf("machine id is all zeros (uninitialized, fail closed)")
	}
	return nil
}

// mustProbeCore opens a throwaway channel core for the doctor's live
// telegram probe (never the production journal).
func mustProbeCore(layout pathx.Layout, resolved config.Resolved) (*channel.Core, egress.ReceiptSink, *health.Owner, func()) {
	dir, err := os.MkdirTemp("", "nexus-doctor-")
	if err != nil {
		return nil, nil, nil, func() {}
	}
	cleanup := func() { os.RemoveAll(dir) }
	j, err := journal.Open(filepath.Join(dir, "probe.db"), resolved.Config.DefaultProfile,
		redact.None{}, channel.Events(), channel.NewProjection())
	if err != nil {
		cleanup()
		return nil, nil, nil, func() {}
	}
	c, err := channel.New(j)
	if err != nil {
		j.Close()
		cleanup()
		return nil, nil, nil, func() {}
	}
	h, err := health.New(filepath.Join(dir, "channel_health.json"), io.Discard)
	if err != nil {
		j.Close()
		cleanup()
		return nil, nil, nil, func() {}
	}
	return c, channel.EgressSink(j), h, func() { j.Close(); cleanup() }
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

// Command-id shapes (P0-prep #3): commands fire only on exact ids.
var (
	challengeIDRe  = regexp.MustCompile(`^ch-[0-9a-f]{24}$`)
	occurrenceIDRe = regexp.MustCompile(`^occ-[A-Za-z0-9_.-]+#[0-9]+$`)
	deliveryIDRe   = regexp.MustCompile(`^dlv-[0-9a-f]{24}$`)
)

// telegramHandler routes an admitted Telegram message: "approve <id>" /
// "deny <id>" hit the durable HITL store; anything else is a normal
// conversation turn through the same production planner spine
// (ModeDefault ALWAYS — a channel message can never enable yolo, F2).
// reminderIDFor derives the deterministic reminder id for an admitted update,
// so a description replay maps to the SAME reminder (front reconfirm +
// idempotent create in cronjobDescription).
func reminderIDFor(in channel.Inbound) string {
	h := sha256.Sum256([]byte(in.AdapterID + "|" + in.ChannelIdentity + "|" + strconv.FormatInt(in.UpdateID, 10)))
	return "rem-" + hex.EncodeToString(h[:8])
}

// chatIDFromIdentity reverses the "chat-<id>" channel identity.
func chatIDFromIdentity(identity string) (int64, bool) {
	id, err := strconv.ParseInt(strings.TrimPrefix(identity, "chat-"), 10, 64)
	if err != nil {
		return 0, false
	}
	return id, true
}

func cronjobConfirm(w schedule.WallTime) string {
	return fmt.Sprintf("Podsjetnik postavljen za %04d-%02d-%02d %02d:%02d.",
		w.Year, int(w.Month), w.Day, w.Hour, w.Minute)
}

// cronjobDescription is the /cronjob durable commit + replay guard (plan v4).
// handled=false means "not a picker description — route this text normally".
func (b *daemonBundle) cronjobDescription(ctx context.Context, in channel.Inbound) (string, bool, error) {
	remID := reminderIDFor(in)
	st, _, wall, err := b.obl.ReminderIntent(ctx, remID)
	switch st {
	case obligation.ReminderFound:
		return cronjobConfirm(wall), true, nil // replay: reconfirm from persisted intent
	case obligation.ReminderStorageError:
		return "", true, fmt.Errorf("cronjob: reminder lookup failed: %w", err) // fail closed
	}
	// NotFound: only a completed pick awaiting its description is a commit.
	chatID, ok := chatIDFromIdentity(in.ChannelIdentity)
	if !ok {
		return "", false, nil
	}
	y, mo, d, h, mi, ok := b.picker.AwaitingPick(chatID)
	if !ok {
		return "", false, nil // no pick in progress -> normal turn
	}
	descr := strings.TrimSpace(in.Text)
	low := strings.ToLower(strings.TrimPrefix(descr, "/"))
	if descr == "" || low == "cancel" || low == "odustani" {
		b.picker.DropSource(chatID)
		return "Otkazano.", true, nil
	}
	w := schedule.WallTime{Year: y, Month: mo, Day: d, Hour: h, Minute: mi, TZ: b.cfg.Timezone}
	if err := b.obl.CreateReminder(ctx, remID, descr, w); err != nil {
		return "", true, fmt.Errorf("cronjob: create reminder failed: %w", err)
	}
	b.picker.DropSource(chatID)
	return cronjobConfirm(w), true, nil
}

func telegramHandler(b *daemonBundle) telegram.Handler {
	return func(ctx context.Context, in channel.Inbound) (string, error) {
		// /cronjob durable path FIRST (PLAN-CRONJOB.md): the deterministic
		// reminder id for THIS update is looked up durably before any command
		// parsing or the ephemeral session gate, so a replay after a crash
		// that lost the picker session still reconfirms rather than
		// mis-routing the description as a normal turn.
		if b.picker != nil {
			if reply, handled, err := b.cronjobDescription(ctx, in); err != nil {
				return "", err
			} else if handled {
				return reply, nil
			}
		}
		text := strings.TrimSpace(in.Text)
		// A menu-issued command arrives with a leading slash ("/outbox");
		// strip ONE so the command words below match. Ordinary text that
		// merely starts with "/" (rare) loses one slash — harmless.
		text = strings.TrimPrefix(text, "/")
		lower := strings.ToLower(text)
		source := "tg:" + in.ChannelIdentity
		// Command words route ONLY with an exact well-formed id — an
		// ordinary sentence starting with "approve"/"ack"/... is
		// CONVERSATION, never a swallowed command error (P0-prep #3).
		switch {
		case strings.HasPrefix(lower, "approve ") && challengeIDRe.MatchString(strings.TrimSpace(text[len("approve "):])):
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
		case strings.HasPrefix(lower, "retry ") && challengeIDRe.MatchString(strings.TrimSpace(text[len("retry "):])):
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
		case strings.HasPrefix(lower, "deny ") && challengeIDRe.MatchString(strings.TrimSpace(text[len("deny "):])):
			id := strings.TrimSpace(text[len("deny "):])
			if err := b.approvals.Deny(ctx, id, source); err != nil {
				return "Denial failed: " + err.Error(), nil
			}
			return "Denied " + id + ".", nil
		case strings.HasPrefix(lower, "ack ") && occurrenceIDRe.MatchString(strings.TrimSpace(text[len("ack "):])):
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
		case lower == "new" || lower == "reset" || lower == "clear":
			// Conversation boundary (hermes /new): forget prior context.
			payload, _ := json.Marshal(map[string]string{"identity": in.ChannelIdentity})
			_, aerr := b.j.Append(ctx, contracts.EnvelopeParams{
				SchemaID: "nexus.event", SchemaVersion: 1,
				EventID:   contracts.EventID(fmt.Sprintf("ev-convreset-%s-%d", in.ChannelIdentity, in.UpdateID)),
				EventType: "conversation.reset", RunID: contracts.RunID("run-convreset-" + in.ChannelIdentity),
				EmittedAt: time.Now().UTC(), ActorType: contracts.ActorUser, ActorID: "owner",
				PrincipalID: "nexus", WorkspaceID: "local", ProfileID: b.profile, AttemptNo: 1,
				Payload: payload, PayloadHash: "recomputed",
			})
			// A redelivered /new collides on the id — already done,
			// still confirm (idempotent).
			if aerr != nil && !errors.Is(aerr, journal.ErrDuplicateEvent) {
				return "", aerr
			}
			return "Novi razgovor — zaboravio sam prethodni kontekst. 🧹", nil
		case lower == "help" || lower == "start":
			return "**NEXUS** — tvoj osobni asistent.\n\n" +
				"Samo mi piši normalno i razgovaramo — pamtim razgovor.\n\n" +
				"**Razgovor**\n" +
				"/new — novi razgovor (zaboravim prošli kontekst)\n" +
				"/cronjob — zakaži podsjetnik (klikni datum i sat, pa upiši tekst)\n\n" +
				"**Stanje**\n" +
				"/pending — čeka li nešto tvoje odobrenje\n" +
				"/outbox — poruke koje možda nisu stigle\n\n" +
				"**Kad nešto treba tvoju potvrdu**\n" +
				"Javim ti s oznakom (npr. ch-…) i odgovoriš tom oznakom:\n" +
				"• approve <oznaka> — odobri\n" +
				"• deny <oznaka> — odbij\n" +
				"• retry <oznaka> — ponovi odobrenje\n" +
				"• ack <oznaka> — potvrdi da si odradio podsjetnik\n" +
				"• redeliver <oznaka> — ponovno pošalji poruku", nil
		case lower == "outbox":
			// UNKNOWN rows await HUMAN reconciliation (E9/B2): list them
			// so the owner can decide (P0-prep #1 — a wire failure no
			// longer dies silently).
			rows, err := b.chanCore.UnreconciledFor(ctx, "telegram", in.ChannelIdentity)
			if err != nil {
				return "", err
			}
			failed, err := b.chanCore.FailedFor(ctx, "telegram", in.ChannelIdentity)
			if err != nil {
				return "", err
			}
			if len(rows) == 0 && len(failed) == 0 {
				return "Outbox clean: no deliveries awaiting reconciliation.", nil
			}
			clip := func(txt string) string {
				if len(txt) > 80 {
					return txt[:80] + "…"
				}
				return txt
			}
			out := ""
			if len(rows) > 0 {
				out += "Deliveries with UNKNOWN outcome (may or may not have arrived):\n"
				for _, r := range rows {
					out += r.DeliveryID + ": " + clip(r.Text) + "\n"
				}
			}
			if len(failed) > 0 {
				out += "Deliveries FAILED (S7 gave up: budget spent or a definite rejection):\n"
				for _, r := range failed {
					out += r.DeliveryID + " [" + r.LastCode + "]: " + clip(r.Text) + "\n"
				}
			}
			out += "Reply redeliver <dlv-id> to resend one as a NEW delivery attempt (an UNKNOWN one MAY arrive twice)."
			return out, nil
		case strings.HasPrefix(lower, "redeliver ") && deliveryIDRe.MatchString(strings.TrimSpace(text[len("redeliver "):])):
			// HUMAN-confirmed E9 reconciliation: the owner accepts the
			// duplicate risk explicitly — never an automatic retry.
			id := strings.TrimSpace(text[len("redeliver "):])
			// Destination-bound: only THIS chat's deliveries (codex #2);
			// the guard is atomic inside the projection transition.
			if err := b.chanCore.ReconcileFor(ctx, id, false, "telegram", in.ChannelIdentity, source); err != nil {
				return "Redeliver failed: " + err.Error(), nil
			}
			return "Re-queued " + id + " — it will go out on the next flush (and may arrive twice).", nil
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
			// LOCALIZATION AT THE EDGE (tgout plan): the typed drift
			// sentinel is extracted STRUCTURALLY — never by parsing
			// error strings — and mapped to the Croatian user message.
			var de loop.DriftError
			if errors.As(err, &de) && de.Typed.Code == "TOOL_SCHEMA_DRIFT" {
				return "Nisam uspio ispravno pozvati alat (" + string(de.Tool) + "). Preformuliraj zahtjev.", nil
			}
			return "", err
		}
		return reply, nil
	}
}
