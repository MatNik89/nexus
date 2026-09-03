# tasks-P0.md — P0 walking-skeleton task ledger (the ONLY P0 task queue)

Rules: one task at a time, in order (deviations need a stated reason). Every task carries
**Trace** (PRD / Annex A / HARDQ source), **Acceptance**, and **RED** (a red-capable
detector observed RED before the change, GREEN after; anchored to the spec, never the
implementation; stateful tests own fresh temp fixtures). A task is DONE only when its RED
list is GREEN and the slice passed agent review (batch reviews per phase are fine —
constitution: CLAUDE.md/AGENTS.md). Status legend: `[ ]` open · `[~]` in progress · `[x]`
done (append commit hash).

Build order = HARDQ-CONSOLIDATED A2 (unanimous): vertical slices, SECTION-MAP DAG edges
satisfied via -min contracts. Nothing relies on the sandbox boundary before T02 passes.

---

## Phase 0 — scaffold + go/no-go preflight

**[ ] T01 — Repo scaffold + build gates.**
Go module `github.com/MatNik89/nexus`; package cut per DESIGN-S0 (`internal/kernel/
{contracts,journal,machine,negotiation,closure}`, `internal/foundation/{config,pathx,procx,
telemetry}`, later dirs empty). `CGO_ENABLED=0` enforced in Makefile/CI script; static-link
check (`file`/`ldd` script) per E1.
Trace: E1; HARNESS-PLAN S0/S1 package layout.
Acceptance: `CGO_ENABLED=0 go build ./...` + `go vet ./...` clean; static-check script GREEN.
RED: static-check against a deliberately cgo-tainted build fails.

**[ ] T02 — Sandbox feasibility probe + hostile conformance suite v0 (bwrap).**
Standalone probe (no kernel deps): drives bwrap on the REAL deployment host through the
five capability columns (FS_RO/FS_RW/NET_DENY/SYSCALL-floor/PROC_TREE). Hostile cases:
read `/etc/shadow` → denied; any egress → denied; workspace RW → allowed; dynamic ELF
(`/bin/ls`) runs (loader closure per B4); shebang script → rejected; undeclared child-exec
→ rejected; kill → whole tree dead. Archive the report + publish kernel/ABI floor (C5).
Trace: PRD §6.6/§7; HARDQ D1/B4/C5; DESIGN-S0 (suite shape).
Acceptance: suite GREEN on deployment host; report committed. **Go/no-go gate for T22-T23.**
RED: run with bwrap absent → probe reports UNAVAILABLE (no crash, no weaker fallback).

**[ ] T03 — `nexus doctor` preflight (minimal).**
Checks: kernel/ABI floor · bwrap present · data-dir exists/0700/writable · provider key ·
Telegram token. Missing item → exact instruction or consented one-command install offer.
Capability-scoped result (no key → conversation off; no token → Telegram off; no bwrap →
exec off; bad data-dir → stateful startup blocked); prints `conversation-only` vs
`P0-capable`.
Trace: HARDQ F1/C5 (user requirement 2026-09-03).
Acceptance: each missing prerequisite produces its scoped result; all green → `P0-capable`.
RED: bwrap removed from PATH → doctor reports exec-off, exit code nonzero for `--strict`.

## Phase 1 — K0 primitives

**[ ] T04 — Typed contracts (S0.1).**
`Envelope/Message/ContextBlock/ToolCall/ToolResult/TypedError` per Annex P0.1 field lists +
`ProfileID` (HARDQ B3) + `ExecutionKind`/`EffectPhase`/`CommitReceipt` (DESIGN-FIXES).
Enums `iota`+`Valid()` default-reject; `Content` XOR `ContentRef` in constructors; no
`map[string]any`; unknown schema ID/version → reject unprocessed, raw preserved (C7).
Trace: HARNESS-SPEC P0.1; E3; DESIGN-FIXES-r2.
Acceptance: constructors reject every malformed case in the P0.1 MUST lists.
RED: unknown discriminator accepted → test fails; XOR violation accepted → test fails.

**[ ] T05 — EventJournal (P0.3) + single append actor.**
SQLite-WAL (`modernc.org/sqlite`), bounded `busy_timeout`; ONE serialized append actor owns
Append + sequence; core-state projections fold in the SAME `BEGIN IMMEDIATE` transaction;
known-ref secret redaction BEFORE append (C1); the three transaction recipes reserved as
API (B7). Daemon owns DB; CLI later via UDS (T15).
Trace: HARNESS-SPEC P0.3; E4; HARDQ B7/C1.
Acceptance: replay fold reproduces state byte-identically; projections cannot bypass.
RED: `test_projection_cannot_bypass_journal`; concurrent turn+scheduler appends →
contiguous sequences, nothing lost; known secret in payload → never reaches any sink;
SIGKILL at each commit boundary → old-or-new, never half.

**[ ] T06 — State machine as fold (S0.2).**
Transition table `map[State][]Transition` default-reject; state reconstructed by folding
journal events; checkpoint = offset; UNKNOWN only via reconciliation event.
Trace: HARNESS-SPEC P0.1 states; E4.
Acceptance: fold of a recorded run reproduces every intermediate state.
RED: illegal transition (RUNNING→ADMITTED, SUCCEEDED→RUNNING) → reject.

**[ ] T07 — Config (S1.1-min).**
One typed `Config`; precedence Default<Global<Project<Env<CLI; schema validation before
merge; `ValidateBounds` — config can only NARROW the kernel floor; restart-on-change (no
hot reload in P0 — B9).
Trace: HARNESS-PLAN 1.1; E11; HARDQ B9.
Acceptance: precedence proven by table-driven test; invalid config → startup refuses.
RED: config attempts egress `*` / sandbox below floor → reject.

**[ ] T08 — Paths + process identity (S1.2-min).**
`os.UserConfigDir`-based layout; `proc_linux.go`: `Setpgid`, killpg TERM→grace→KILL→Wait;
PID + start-token identity.
Trace: HARNESS-PLAN 1.2; E5 (S1.2 owner).
Acceptance: spawned tree fully terminates; identity survives PID reuse check.
RED: group terminate leaves an orphan grandchild alive → test fails.

**[ ] T09 — Sealed capability snapshot (S0.5-min).**
`Resolver.Resolve` = fail-closed validation only (unknown/cycle/conflict → REJECTED);
sealed startup snapshot from config + live probes (sandbox/provider/channel). NO
Activator/RollbackVault (B9 guard).
Trace: HARNESS-SPEC P0.4/P0.5 (validation half); E7 P0 note; HARDQ B9.
Acceptance: snapshot lists exactly the compiled P0 capabilities with probe results.
RED: one ablated gate (e.g. bwrap missing) → that capability OFF in snapshot, start
continues conversation-only; unknown capability name in config → reject.

**[ ] T10 — Minimal deterministic checker (S16.6-det).**
Generic `AcceptanceContract` + `EvidenceBundle`; graders: exit-code, file-diff presence,
delivery-receipt+ack (occurrence-correlated — B5). Checker is a separate principal from the
worker; prose is never evidence.
Trace: E13; HARDQ B5; SECTION-MAP K0.
Acceptance: coding-style and assistant-style contracts both gradeable.
RED: worker output "done" with empty EvidenceBundle → Grade=FAIL.

## Phase 2 — safe conversation spine

**[ ] T11 — PEP (S6.0) + lifecycle chain (S6.9) + EffectPath.**
`Decide` total switch default-deny, ASK≠ALLOW; `MiddlewareChain` before/after/on_error;
`EffectPath` exactly per DESIGN-FIXES-r2 K1/K2 (concrete executor types; unknown
`ExecutionKind` → reject; `classifyEffectPhase` fail-closed).
Trace: DESIGN-FIXES-r2 (canonical code + its 8 named REDs); E5/E8/E9.
Acceptance: all 8 DESIGN-FIXES REDs GREEN.
RED: `TestUnknownDecisionDenies`, `TestUnknownExecKindRejectsNotInproc`,
`TestAskRequiresExactApproval`, `TestInProcessToolNeverSpawns`, `TestNoAttemptWithoutGrant`,
`TestVetoThroughOnErrorReconciles`, `TestExecErrorSkipsAfterToolAndOutput`,
`TestReadOnlyProcessStillSandboxed` (this one lands with T23; stub-skip until then, marked).

**[ ] T12 — s7-min.**
`AttemptGrant{attempt_no,target,expires_at,nonce}` + cancel token + no-retry policy (every
retryable class → FAILED_TERMINAL in P0); grant single-use.
Trace: HARDQ A2; HARNESS-SPEC P0.2 (single-owner half); E5.
Acceptance: every physical attempt in the loop carries a grant.
RED: `test_adapter_cannot_self_retry` — second use of one grant → `ATTEMPT_NOT_AUTHORIZED`.

**[ ] T13 — Provider (APIKey) + structured output (S2.1/S2.3-min).**
One `Provider{Chat/Stream/Capabilities/DataDescriptor}`; OpenAI-compatible HTTP, Bearer;
`StructuredOutput.Validate → re-ask(with AttemptGrant) → salvage`; tolerant parser; never
silent-accept.
Trace: HARNESS-SPEC P0.7; E12 (provider block).
Acceptance: live chat call succeeds with configured provider; malformed JSON path exercised.
RED: `test_structured_output_never_silent_accept`.

**[ ] T14 — One-turn loop (S3.1-min) + trust fencing.**
plan→act→observe single turn; tool errors packed into observation (loop never crashes);
prompt assembler enforces P0.1 lineage/trust monotonicity (untrusted block never becomes
instruction). No effectful process tool yet.
Trace: E5 path; HARNESS-SPEC P0.1 provenance RED; E10/E12 (trust).
Acceptance: e2e conversation turn through journal; replay reproduces the turn.
RED: `test_s0_rejects_provenance_laundering` (untrusted → SYSTEM via compaction stub →
`PROVENANCE_DOWNGRADE`, model sink 0 calls).

**[ ] T15 — Terminal REPL (14.1-min) + daemon/UDS split.**
`nexus daemon` owns the DB; `nexus chat` REPL connects over UDS (B7); minimal message
render + input + streaming print (REF-brainless-tui P0 subset: message/thinking/tool-line).
Trace: PRD §6.1; HARDQ B7 (UDS); REF-brainless-tui.
Acceptance: PRD §6.1 — a useful conversation from the terminal, daemon running.
RED: two concurrent CLI clients + daemon → zero `SQLITE_BUSY` surfaced to user.

## Phase 3 — memory + profiles

**[ ] T16 — Profiles: physical isolation + admission stamping.**
`~/.nexus/profiles/<p>/nexus.db` per profile + `system.db` (zero profile payloads);
non-null `ProfileID` stamped at admission, immutable through the causal chain; no mutable
global current-profile lookup post-admission.
Trace: PRD §6.5; HARDQ B3; E14.
Acceptance: identical content seeded in `work` and `private`; every query path stays inside
its profile.
RED: cross-profile memory query → 0 hits; FTS index of profile A never returns B tokens;
restart + replay preserves ProfileID on every row.

**[ ] T17 — Explicit memory (S9.2-min) + FORGET/PURGE.**
"remember X" → exact preview → profile-bound fact (write-approval ON); inferred candidates
→ review queue (not recallable until accepted); append-only supersession; retrieval =
recency + exact/tag. NO decay (B8). `MEMORY_FORGET` = reversible tombstone;
`DATA_PURGE` = bytes gone including tombstone (per-profile file + VACUUM handles WAL/
freelist residuals).
Trace: PRD §6.2; HARNESS-SPEC P2.1; HARDQ B8; E14.
Acceptance: PRD §6.2 — fact told in one session recalled in a fresh session after restart.
RED: rejected inference not recallable; forget → recall blocked, restore works;
`test_purge_cannot_complete_with_residual_copy` — after purge, `grep` over DB+WAL files
finds no plaintext.

## Phase 4 — obligations + scheduler

**[ ] T18 — 3.6-min durable scheduler.**
Persisted occurrence: UTC instant + IANA zone + `dst=ONCE_FIRST` + `missed_run=COALESCE`;
stable occurrence ID; injected wall/monotonic clocks; single-process overlap rule; startup
+ wake (D-Bus PrepareForSleep where available) catch-up sweep — `EvaluateDue` is a range
query, overdue fires with explicit OVERDUE notice.
Trace: PRD §6.3; HARDQ B1; HARNESS-SPEC P2.6 (minimal policy subset).
Acceptance: reminder fires on time across daemon restart.
RED: restart-before-due → fires once; restart-after-due → fires once with OVERDUE;
`Europe/Zagreb` autumn fold → once; spring gap → policy applied; suspend-over-due → catch-up
fires on wake.

**[ ] T19 — ObligationStore (9.6-min): Reminder + typed Task.**
Writes THROUGH journal; `Reminder`: `SCHEDULED→DUE→DELIVERY_PENDING→DELIVERED→ACKED|
EXPIRED`; typed `Task` handlers with handler-specific postconditions; `MarkDone` only with
Evidence correlated to the SAME occurrence ID (T10 grader). Scheduled evaluations carry the
continuous-loop policy (C3) — exempt from interactive stuck-breaker.
Trace: PRD §4.3/§6.3; HARDQ B5/C3; E13.
Acceptance: reminder lifecycle end-to-end; un-evidenced MarkDone impossible.
RED: ACK for occurrence N does not close N+1; MarkDone without evidence → rejected;
polling task repeated 10× identical → no stuck-breaker trip.

## Phase 5 — Telegram (channel:builtin)

**[ ] T20 — Gateway core + Telegram built-in adapter.**
Long-poll; durable inbox keyed `(adapter_id, chat, update_id)` `RECEIVED→ADMITTED→
TERMINAL` — normalized message persisted BEFORE offset advance, replay returns existing
outcome; transactional outbox + stable delivery ID; at-least-once remote delivery,
`sent-but-unrecorded` → UNKNOWN→RECONCILING (never blind retry); per-chat profile binding,
deny-default (unbound chat → typed "which profile?" refusal); non-text → typed fail-closed
reply. Registers as `channel:builtin` (P1.6 as amended — NO extensions closure).
Trace: PRD §6.4 (half); HARDQ A1/B2/C2; E15; HARNESS-SPEC P1.6.
Acceptance: phone→NEXUS→phone round trip; crash tests leave no lost/duplicated admission.
RED: SIGKILL matrix (before insert / after insert / after admission / after effect /
before offset advance / after remote accept) → each converges to exactly-once admission +
reconciled-or-UNKNOWN delivery; unbound chat refused; BlockImage → typed reply, no crash.

**[ ] T21 — Remote HITL (durable).**
`ApprovalChallenge` exact-intent (canonical tool name + args + target + ProfileID;
`(device,inode)` for destructive FS — C4), expiring, single-use; on `DecisionAsk` loop
commits `TurnSuspended` and exits; approval appends `ApprovalReceived`, resume rehydrates
from journal.
Trace: PRD §6.4; HARDQ B6/C4; HARNESS-SPEC P1.6 HITL states + P1.4.
Acceptance: PRD §6.4 — irreversible action from Telegram requires approval before execution.
RED: approve-after-daemon-restart → action completes exactly once; replayed approval →
`APPROVAL_REPLAY`; modified args under old approval → rejected
(`test_approval_cannot_authorize_modified_effect`).

## Phase 6 — sandboxed exec (gated by T02)

**[ ] T22 — SandboxBackend + bwrap backend (S6.2-P0).**
`Boundary/SandboxBackend{Probe,Compile,Launch,Attest}`; bwrap: RO closure (`/usr`,`/lib*`,
`/bin`,`/sbin` dirs; `/etc/ld.so.cache`+CA certs as FILE grants; NEVER recursive `/etc`),
disposable RW workdir, net unshared, S1.2 process identity inside Launch; promoted absolute
ELF only; scripts/shell-strings/undeclared children rejected. Attestation of applied
profile.
Trace: HARDQ D1/B4; E10; DESIGN-S0 (interface); PRD §6.6.
Acceptance: T02 hostile suite re-run through the REAL backend → GREEN.
RED: `/etc/shadow` read → EACCES; egress → denied; `/bin/ls` runs; shebang → rejected;
timeout → tree dead (P1.2); attestation mismatch → refuse to trust result.

**[ ] T23 — exec tool on the effect-path.**
`ExecutionKind=ExecProcess` sealed on the tool spec; one-shot exec through
T11 EffectPath → T22 backend; results pruned (exit status + error lines preserved).
Trace: E5/E8; DESIGN-FIXES; PRD §6.6.
Acceptance: model-requested command runs sandboxed end-to-end with approval where policy
says ASK.
RED: `TestReadOnlyProcessStillSandboxed` (un-stub from T11); unknown kind → reject.

## Phase 7 — P0 closure

**[ ] T24 — P0 acceptance run.**
Scripted end-to-end checks for ALL SIX PRD §6 criteria + full hostile suite + doctor
`P0-capable` + demo script. Every Annex RED touched by P0 listed and GREEN. Agent review of
the whole P0 (3-agent, adversarial) before declaring P0 done.
Trace: PRD §6 (the contract); constitution review gate.
Acceptance: six-for-six criteria demonstrably GREEN on the deployment host.
RED: any single criterion's check can be made to fail by reverting its feature commit
(spot-check ablation on at least two criteria).

---

Deferred ledger (do NOT build in P0): full S7 (P2) · full S5 (P3) · Decay+Audn (P1) ·
transactional Activator (P4+) · entropy secret detection (P4) · SemanticHealth (P2+) ·
upcast chain/quarantine (v2) · native sandbox helper (P1 hardening) · plugin channels (P) ·
coding trio TIA/symedit/deep evidence (P1).
