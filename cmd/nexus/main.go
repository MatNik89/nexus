// Command nexus is the NEXUS personal-assistant kernel binary.
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/MatNik89/nexus/internal/app/daemon"
	"github.com/MatNik89/nexus/internal/app/repl"
	"github.com/MatNik89/nexus/internal/foundation/config"
	"github.com/MatNik89/nexus/internal/foundation/pathx"
	"github.com/MatNik89/nexus/internal/kernel/contracts"
	"github.com/MatNik89/nexus/internal/kernel/journal"
	"github.com/MatNik89/nexus/internal/kernel/loop"
	"github.com/MatNik89/nexus/internal/kernel/machine"
	"github.com/MatNik89/nexus/internal/kernel/s7min"
	"github.com/MatNik89/nexus/internal/llm/planner"
	"github.com/MatNik89/nexus/internal/llm/provider"
	"github.com/MatNik89/nexus/internal/memory"
	"github.com/MatNik89/nexus/internal/preflight/doctor"
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
	d, j, err := buildDaemon(layout, resolved)
	if err != nil {
		fmt.Fprintf(os.Stderr, "nexus daemon: %v\n", err)
		return 2
	}
	defer j.Close()
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	hb := daemon.NewHeartbeat(filepath.Join(layout.SystemDir(), "heartbeat"), 5*time.Second)
	go hb.Run(ctx)
	sock := socketPath(layout)
	fmt.Printf("nexus daemon %s — profile %s, socket %s\n", version, resolved.Config.DefaultProfile, sock)
	if err := d.Serve(ctx, sock); err != nil {
		fmt.Fprintf(os.Stderr, "nexus daemon: %v\n", err)
		return 1
	}
	return 0
}

// buildDaemon is the COMPOSITION ROOT: every production seam (journal +
// redactor, S7 authority, governed provider, streaming planner factory,
// fail-closed audit) wired exactly as the daemon runs it — and testable
// against a custom layout (Phase-2-r2 codex #13).
func buildDaemon(layout pathx.Layout, resolved config.Resolved) (*daemon.Daemon, *journal.Journal, error) {
	profile := resolved.Config.DefaultProfile
	profileDir, err := layout.ProfileDir(profile)
	if err != nil {
		return nil, nil, err
	}
	for _, dir := range []string{profileDir, layout.SystemDir()} {
		if err := pathx.EnsureDir(layout.Base, dir); err != nil {
			// The base itself may not exist yet: create it 0700 first.
			if os.MkdirAll(layout.Base, 0o700) != nil || pathx.EnsureDir(layout.Base, dir) != nil {
				return nil, nil, err
			}
		}
	}
	events := map[string]journal.PayloadValidator{evPolicyYolo: nil}
	for _, n := range machine.EventTypes() {
		events[n] = nil
	}
	for n, v := range memory.Events() {
		events[n] = v
	}
	journalPath, _ := layout.ProfileJournal(profile)
	redactor := redact.NewKnownRefs(knownSecretRefs(resolved.Config))
	// The ONE profile database: journal + memory projection together
	// (Annex P0.3 — facts are journal events folded in the same
	// transaction; no second SQLite file exists).
	j, err := journal.Open(journalPath, profile, redactor, events, memory.NewProjection())
	if err != nil {
		return nil, nil, fmt.Errorf("journal: %w", err)
	}
	authority := s7min.NewAuthority(nil, 5*time.Minute)
	prov, err := provider.NewAPIKey(resolved.Config, authority)
	if err != nil {
		j.Close()
		return nil, nil, fmt.Errorf("provider: %w (conversation is a P0 core capability — fix the config and restart)", err)
	}
	memStore, err := memory.NewStore(j)
	if err != nil {
		j.Close()
		return nil, nil, fmt.Errorf("memory: %w", err)
	}
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
			return pl.WithTools(memory.Specs(), profile)
		},
		Authority: authority, Profile: profile,
		Rules:    memory.Rules(), // memory_remember=ASK, memory_recall=ALLOW
		Tools:    memory.Tools(memStore, redactor),
		Audit:    &journalAudit{j: j, profile: profile},
		Redactor: redactor,
	})
	if err != nil {
		j.Close()
		return nil, nil, err
	}
	return d, j, nil
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
