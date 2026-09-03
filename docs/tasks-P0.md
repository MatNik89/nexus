# tasks-P0.md — P0 walking-skeleton task ledger (the ONLY P0 task queue) — v2

Rules: one task at a time, in order (deviations need a stated reason). Every task carries
**Trace** (PRD / Annex A / HARDQ source), **Acceptance**, and **RED** (a red-capable
detector observed RED before the change, GREEN after; anchored to the spec, never the
implementation; stateful tests own fresh temp fixtures). A task is DONE only when its RED
list is GREEN and the slice passed agent review (batch reviews per phase are fine).
Status: `[ ]` open · `[~]` in progress · `[x]` done (append commit hash).
PRD citations use "PRD §6 item N" (PRD §6 has numbered items, not subsections).

Build order = HARDQ-CONSOLIDATED A2 (unanimous): vertical slices, SECTION-MAP DAG edges
satisfied via -min contracts. Nothing relies on the sandbox boundary before T02 passes.

**Review gates (MANDATORY — user directive 2026-09-03):**
1. EVERY phase ends with a deep 3-agent adversarial review of THAT phase's batch, folded
   to convergence, before the next phase starts.
2. Three INTEGRATION deep-reviews cover everything-so-far at the seams where pieces first
   join: (a) after Phase 2 — the whole first end-to-end path (loop→PEP→provider→journal→
   REPL) as one chain; (b) after Phase 6 — the whole security surface together
   (sandbox+exec+profiles+HITL+yolo); (c) T27 — full-P0 adversarial review (already in
   the task).
3. Retroactive trigger: a task that changes the behavior of earlier code pulls that
   earlier code into its review scope. Unchanged, already-converged code is NOT re-reviewed
   without a new proof surface (constitution rule).
v2 folds REVIEW-TASKS round-1 (codex 15 + kilo 8 findings).

---

## Phase 0 — scaffold + go/no-go preflight

**[x] T01 (018a9d7) — Repo scaffold + build gates.**
Go module `github.com/MatNik89/nexus`; package cut per DESIGN-S0; `CGO_ENABLED=0` enforced
in Makefile/CI script; static-link check script.
Trace: E1; HARNESS-PLAN S0/S1 layout.
Acceptance: `CGO_ENABLED=0 go build ./...` + `go vet ./...` clean; static-check GREEN.
RED: static-check against a deliberately cgo-tainted build fails.

**[x] T02 (6fc4b1e..17888b3) — Sandbox feasibility probe + hostile conformance suite v0 (bwrap).**
Standalone probe on the REAL deployment host: five capability columns
(FS_RO/FS_RW/NET_DENY/SYSCALL-floor/PROC_TREE). Hostile cases: `/etc/shadow` read denied;
any egress denied; workspace RW allowed; dynamic ELF (`/bin/ls`) runs (B4 closure); shebang
rejected; undeclared child-exec rejected; kill → tree dead. Archive report; publish
kernel/ABI floor (C5).
Trace: PRD §6 item 6, §7; HARDQ D1/B4/C5; DESIGN-S0 suite shape.
Acceptance: suite GREEN on deployment host; report committed. **Go/no-go gate for T25-T26.**
RED: (a) bwrap absent → probe reports UNAVAILABLE, no weaker fallback; (b) negative
controls per boundary, HAZARD-SAFE (they prove the suite detects loosening WITHOUT
exposing real trust boundaries): FS control reads a non-secret CANARY file planted in a
test-owned dir (never recursive real `/etc`); net control reaches only a test-owned local
sink (never uncontrolled egress); process control escapes into a test-owned process tree.
Each loosened profile MUST turn its boundary check RED.

**[x] T03 (6fc4b1e..17888b3) — `nexus doctor` preflight (minimal).**
Checks: kernel/ABI floor · bwrap · data-dir exists/0700/writable · provider key · Telegram
token. Missing → exact instruction or consented one-command install. Capability-scoped
results (no key → conversation off; no token → Telegram off; no bwrap → exec off; bad
data-dir → stateful startup blocked). Final label: **`prerequisites-ready`** — the
`P0-capable` label is granted ONLY by T27 after the six live criteria pass.
Trace: HARDQ F1/C5 (user requirement 2026-09-03); Essentials P0 additions.
Acceptance: all five prerequisites verified; each produces its scoped result.
RED: table-driven — EACH of the five prerequisites individually broken (key removed, token
removed, data-dir chmod 000, bwrap off PATH, floor-probe forced-fail) → its scoped OFF
outcome + nonzero `--strict` exit; all five green → `prerequisites-ready`.

## Phase 1 — K0 primitives

**DONE 2026-09-03** — merged to main (723b227); Phase-1A 5 review rounds + Phase-1B 4 rounds (codex/kilo/agy), final 3× PASS zero findings.

**[x] T04 (1bdae45..6cbc2c1) — Typed contracts (S0.1).**
`Envelope/Message/ContextBlock/ToolCall/ToolResult/TypedError` per Annex P0.1 + `ProfileID`
(B3) + `ExecutionKind`/`EffectPhase`/`CommitReceipt` (DESIGN-FIXES). Enums default-reject;
`Content` XOR `ContentRef`; no `map[string]any`; unknown schema ID/version → reject
unprocessed, raw preserved (C7).
Trace: HARNESS-SPEC P0.1; E3; DESIGN-FIXES-r2.
Acceptance: constructors reject every malformed case in the P0.1 MUST lists.
RED: unknown discriminator accepted → fail; XOR violation accepted → fail.

**[x] T05 (d6dd454+r-folds) — K0 seams: clock/ID injection · AtomicWriter (s5-min) · ContextBudget-min · Assembler-min.**
Injectable wall+monotonic clock and ID-generator seams (deterministic tests everywhere
downstream); `AtomicWriter` tmp→fsync→rename, never in-place (the s5-min cut);
`ContextBudget.Measure+HardLimit` (S8.1-min); `Assembler.Base` (S11.1-min) skeleton.
Trace: HARDQ A2 (K0 step 2 + s5-min); SECTION-MAP K0 (S8.1-min/S11.1-min); E4/P0.11
AtomicWriter clause.
Acceptance: task-local conformance tests for ALL FOUR seams pass (downstream import/use
checks are additionally enforced by later tasks, not by this one).
RED: `test_atomic_write_no_partial` (SIGKILL mid-write → old or new, never half);
budget hard-limit breach → refuse, not truncate-silently; injected clock/ID → two runs of
the same scenario produce identical event timelines (determinism conformance);
`Assembler.Base` composes a fixed block set deterministically (golden output).

**[x] T06 (df64a23..6cbc2c1) — EventJournal core (P0.3): durability + serialization.**
SQLite-WAL (`modernc.org/sqlite`), bounded `busy_timeout`; ONE serialized append actor owns
Append + sequence; known-ref secret redaction BEFORE append (C1).
Trace: HARNESS-SPEC P0.3; E4; HARDQ B7/C1.
Acceptance: replay fold reproduces state; append is the only write path.
RED: concurrent appends (two sources) → contiguous sequences, nothing lost; known secret in
payload → never reaches any sink; SIGKILL at commit boundary → old-or-new.

**[x] T07 (this commit) — Synchronous projection harness + generic transaction recipe (B7 core).**
Generic harness: a core-state projection folds in the SAME `BEGIN IMMEDIATE` transaction
as the append (read-your-own-writes), proven with contract-valid FAKE domain rows;
observability projections lag async via durable offsets. The three CONCRETE recipes land
with their first real consumers and carry their crash/visibility REDs there:
inbox-admission+journal → T22 · occurrence+run-admission → T20 · terminal-result+outbox
→ T22.
Trace: HARDQ B7; E4; REVIEW-TASKS2 fix (no consumer-before-producer).
Acceptance: harness passes with fake rows; recipe API sealed for consumers.
RED: `test_projection_cannot_bypass_journal`; projector crash → offset not advanced past
durable row; append-then-read of a core projection in the same process → new state visible.

**[x] T08 (this commit) — State machine as fold (S0.2).**
Transition table default-reject; state reconstructed by folding journal events;
checkpoint = offset; UNKNOWN only via reconciliation event.
Trace: HARNESS-SPEC P0.1 states; E4.
Acceptance: fold of a recorded run reproduces every intermediate state.
RED: illegal transition (RUNNING→ADMITTED, SUCCEEDED→RUNNING) → reject.

**[x] T09 (this commit) — Config (S1.1-min).**
Typed `Config`; precedence Default<Global<Project<Env<CLI; schema validation before merge;
`ValidateBounds` (config only NARROWS); restart-on-change, no hot reload (B9).
Trace: HARNESS-PLAN 1.1; E11; HARDQ B9.
Acceptance: precedence table-driven test; invalid config → startup refuses.
RED: config attempts egress `*` / sandbox below floor → reject.

**[x] T10 (this commit) — Paths + process identity (S1.2-min).**
`os.UserConfigDir` layout; `proc_linux.go` Setpgid + killpg TERM→grace→KILL→Wait;
PID + start-token identity.
Trace: HARNESS-PLAN 1.2; E5.
Acceptance: spawned tree fully terminates; identity survives PID-reuse check.
RED: group terminate leaves orphan grandchild alive → fail.

**[x] T11 (this commit) — Sealed capability snapshot (S0.5-min, sandbox-probe only at this phase).**
`Resolver.Resolve` fail-closed validation (unknown/cycle/conflict → REJECTED); sealed
startup snapshot from config + the T02 sandbox probe + **contract-valid probe fakes** for
provider/channel (live probes register in T15/T23; T27 verifies the final snapshot). NO
Activator/RollbackVault (B9).
Trace: HARNESS-SPEC P0.4/P0.5 validation half; E7 P0 note; HARDQ B9; REVIEW-TASKS fix.
Acceptance: snapshot lists compiled P0 capabilities; extensible probe registry.
RED: ablated gate (bwrap missing) → capability OFF, conversation-only start continues;
unknown capability name in config → reject.

**[x] T12 (this commit) — Minimal deterministic checker (S16.6-det).**
Generic `AcceptanceContract` + `EvidenceBundle`; graders: exit-code · file-diff/hash ·
delivery-receipt+ack correlated to occurrence ID (B5). Checker ≠ worker principal; prose is
never evidence.
Trace: E13; HARDQ B5; SECTION-MAP K0.
Acceptance: coding-style and assistant-style contracts both gradeable.
RED: "done" with empty EvidenceBundle → Grade=FAIL; ack for occurrence N grading N+1 → FAIL.

## Phase 2 — safe conversation spine

**[x] T13 — s7-min (BEFORE the effect-path that consumes it).**
`AttemptGrant{attempt_no,target,expires_at,nonce}` single-use + cancel token + no-retry
policy (retryable → FAILED_TERMINAL in P0).
Trace: HARDQ A2; HARNESS-SPEC P0.2 single-owner half; E5; REVIEW-TASKS order fix.
Acceptance: every physical attempt carries a grant.
RED: `test_adapter_cannot_self_retry` — second use of one grant → `ATTEMPT_NOT_AUTHORIZED`.

**[x] T14 — PEP (S6.0) + lifecycle chain (S6.9) + EffectPath.**
`Decide` total switch default-deny, ASK≠ALLOW; `MiddlewareChain` before/after/on_error;
`EffectPath` per DESIGN-FIXES-r2 K1/K2 (concrete executor types; unknown kind → reject;
`classifyEffectPhase` fail-closed). Sandbox executor = contract FAKE here; real backend
binds in T26 (its integration RED lives there — no stub-skip in this task's own list).
**Policy mode input (HARDQ F2):** `Decide` takes a session `PolicyMode{Default|Yolo}`;
Yolo maps ASK → ALLOW and journals `ALLOWED_BY_YOLO`; DENY is unaffected by mode.
Trace: DESIGN-FIXES-r2; E5/E8/E9; HARDQ F2; REVIEW-TASKS fix (no unsatisfiable RED).
Acceptance: 7 contract REDs GREEN with fakes; yolo mode REDs GREEN.
RED: `TestUnknownDecisionDenies` · `TestUnknownExecKindRejectsNotInproc` ·
`TestAskRequiresExactApproval` · `TestInProcessToolNeverSpawns` ·
`TestNoAttemptWithoutGrant` · `TestVetoThroughOnErrorReconciles` ·
`TestExecErrorSkipsAfterToolAndOutput` · `TestYoloAllowsAskButNeverDeny` (yolo: ASK
executes without approval + journal carries ALLOWED_BY_YOLO; DENY still denied) ·
`TestYoloCannotBeSetByChannelInput` (mode is a session construct, not reachable from a
message payload).

**[x] T15 — Provider (APIKey) + structured output (S2.1/S2.3-min).**
`Provider{Chat/Stream/Capabilities/DataDescriptor}`; OpenAI-compatible HTTP;
`Validate → re-ask(with AttemptGrant) → salvage`. Tolerance is LIMITED to
non-security/non-effect payloads (P0.7); security/effect/policy discriminators are strict.
Registers the live provider probe into the T11 snapshot.
Trace: HARNESS-SPEC P0.7 (incl. its no-tolerant-security clause); E12 provider block.
Acceptance: live chat call OK; malformed-JSON paths exercised; probe registered.
RED: `test_structured_output_never_silent_accept`; malformed/unknown EFFECT or POLICY
discriminator → reaches no sink (strict reject, no repair).

**[ ] T16 — One-turn loop (S3.1-min) + trust fencing + interactive stuck-breaker.**
plan→act→observe single turn; tool errors packed into observation; assembler enforces
lineage/trust monotonicity. Identical-call stuck-breaker WITHIN one interactive turn
(same tool+args N× → break); scheduled/polling iterations exempt via continuous-loop
policy (C3).
Trace: E5; HARNESS-SPEC P0.1 provenance RED; HARDQ C3.
Acceptance: e2e turn through journal; replay reproduces it.
RED: `test_s0_rejects_provenance_laundering`; POSITIVE breaker test — same-turn identical
call repeated N× → breaker trips; polling-policy call repeated N× → no trip.

**[ ] T17 — Terminal REPL (14.1-min) + daemon/UDS split + liveness heartbeat.**
`nexus daemon` owns the DB, emits liveness heartbeat (C8 positive half); `nexus chat`
REPL over UDS; message render + input + streaming print (REF-brainless-tui P0 subset).
`--yolo` flag on the local CLI sets the session PolicyMode (HARDQ F2); the flag is
surfaced in the prompt/status line so the user always sees the mode.
Trace: PRD §6 item 1; HARDQ B7 (UDS) / C8 / F2; REF-brainless-tui.
Acceptance: PRD §6 item 1 — useful conversation from the terminal against the daemon.
RED: scripted e2e conversation oracle with a DETERMINISTIC fake provider (input → expected
rendered output through journal) + separately-labeled live-provider smoke; two concurrent
CLI clients → zero surfaced `SQLITE_BUSY`; heartbeat stops → detectable within interval.

## Phase 3 — memory + profiles

**[ ] T18 — Profiles: physical isolation + admission stamping.**
Per-profile SQLite file + `system.db` (zero profile payloads); non-null `ProfileID` at
admission, immutable through the chain; no post-admission mutable global lookup.
Trace: PRD §6 item 5; HARDQ B3; E14.
Acceptance: identical content seeded in `work`+`private`; every query path stays inside
its profile. (Cross-profile delivery/approval integration REDs land in T24.)
RED: cross-profile memory query → 0 hits; FTS of profile A never returns B tokens;
restart+replay preserves ProfileID on every row.

**[ ] T19 — Explicit memory (S9.2-min).**
"remember X" → exact preview → profile-bound fact (approval ON); inferred → review queue
(not recallable until accepted); append-only supersession ("actually it's Y" corrects a
fact in P0); retrieval recency+exact/tag; NO decay (B8). **MEMORY_FORGET and DATA_PURGE
both deferred to their Annex P2.1 contract** — neither is a PRD §6 criterion and P2.1 is
their normative owner (REVIEW-TASKS2: no P0 authority exists for pulling FORGET forward);
supersession covers P0 correction needs.
Trace: PRD §6 item 2; HARDQ B8; HARNESS-SPEC P2.1 (deferral authority).
Acceptance: PRD §6 item 2 — fact survives restart into a fresh session.
RED: rejected inference not recallable; superseded fact returns only its latest version;
accepted fact recallable after restart, only in its profile.

## Phase 4 — scheduler + obligations

**[ ] T20 — 3.6-min durable scheduler + occurrence counter.**
Persisted occurrence (UTC + IANA + `dst=ONCE_FIRST` + `missed_run=COALESCE`), stable
occurrence ID, T05 clock seams, single-process overlap rule; startup + wake (D-Bus
PrepareForSleep where available) catch-up sweep — `EvaluateDue` range query, overdue fires
with OVERDUE notice. Maintains `last_occurrence_fired` counter (C8 positive half).
Owns the concrete B7 recipe **`occurrence+run-admission`**: firing an occurrence and
admitting its run commit in ONE `BEGIN IMMEDIATE` transaction.
Trace: PRD §6 item 3; HARDQ B1/B7/C8; HARNESS-SPEC P2.6 minimal subset.
Acceptance: reminder fires on time across daemon restart; counter observable via doctor.
RED: restart-before-due → once; restart-after-due → once with OVERDUE; Zagreb autumn
fold → once; spring gap → policy applied; suspend-over-due → catch-up on wake;
recipe atomicity — SIGKILL between occurrence-fire and run-admission → on restart either
BOTH are durable or NEITHER (never a fired occurrence without its admitted run).

**[ ] T21 — ObligationStore (9.6-min): Reminder + ONE concrete typed Task.**
Writes THROUGH journal; `Reminder`
`SCHEDULED→DUE→DELIVERY_PENDING→DELIVERED→ACKED|EXPIRED`; concrete P0 Task handler
**`file_note`** (append a line to a profile-scoped notes file via T05 AtomicWriter;
postcondition = checker verifies file content hash) — proves the typed-handler path
end-to-end without exec. `MarkDone` only with Evidence correlated to the SAME occurrence
ID (T12).
Trace: PRD §4.3, §6 item 3; HARDQ B5; E13.
Acceptance: reminder lifecycle e2e; `file_note` task completes only via verified
postcondition; empty handler registry impossible (registry must contain ≥1 handler).
RED: ACK for occurrence N does not close N+1; MarkDone without evidence → rejected;
`file_note` "done" claim with unchanged file → Grade=FAIL.

## Phase 5 — Telegram (channel:builtin)

**[ ] T22 — Durable channel ingress/egress (transport-neutral B2 core).**
Durable inbox keyed `(adapter_id, channel_identity, update_id)`
`RECEIVED→ADMITTED→TERMINAL` — normalized message persisted BEFORE offset advance, replay
returns existing outcome; transactional outbox + stable delivery ID; at-least-once remote
delivery; `sent-but-unrecorded` → UNKNOWN→RECONCILING (never blind retry). Owns the two
concrete B7 channel recipes: **`inbox-admission+journal`** (normalized inbound row + its
journal event in ONE `BEGIN IMMEDIATE` transaction) and **`terminal-result+outbox`**
(terminal run result + outbox enqueue in ONE transaction).
Trace: HARDQ B2/B7; E15.
Acceptance: crash anywhere leaves exactly-once admission + reconciled-or-UNKNOWN delivery.
RED: SIGKILL matrix — before insert / after insert / after admission / after effect /
before offset advance / after remote accept (fake transport) → each converges correctly;
recipe atomicity — SIGKILL inside each of the two recipes → both halves durable or
neither (no inbound row without its journal event; no terminal result without its outbox
row, and vice versa); visibility — committed recipe rows readable in the next
same-process read.

**[ ] T23 — Telegram built-in adapter.**
Long-poll on T22 core; per-chat profile binding deny-default (unbound chat → typed "which
profile?" refusal); non-text → typed fail-closed reply (C2); registers as `channel:builtin`
(P1.6 as amended — NO extensions closure); registers live channel probe into T11 snapshot.
Trace: PRD §6 item 4 (half); HARDQ A1/C2/B3; HARNESS-SPEC P1.6.
Acceptance: phone→NEXUS→phone round trip on the real bot.
RED: unbound chat refused; BlockImage/BlockAudio → typed reply, loop alive; replayed
update_id returns existing outcome (no double effect).

**[ ] T24 — Remote HITL (durable) + cross-profile integration REDs.**
`ApprovalChallenge` exact-intent (canonical tool+args+target+ProfileID; `(device,inode)`
for destructive FS — C4), expiring, single-use; `DecisionAsk` → commit `TurnSuspended`,
loop exits; approval appends `ApprovalReceived`, resume rehydrates from journal.
Trace: PRD §6 item 4; HARDQ B6/C4/B3; HARNESS-SPEC P1.6 HITL + P1.4.
Acceptance: irreversible action from Telegram requires approval before execution.
RED: approve-after-daemon-restart → completes exactly once; replay → `APPROVAL_REPLAY`;
modified args under old approval → rejected (`test_approval_cannot_authorize_modified_
effect`); **cross-profile (B3 chain):** occurrence/delivery/approval created in `work` can
NEVER be seen, delivered, or approved via a `private`-bound chat (and vice versa),
including across restart/replay; **yolo (F2, policy half):** in yolo the HITL path does
not park (ASK auto-allows, journaled) and a Telegram message can neither enable yolo nor
piggyback on it to authorize a DENY-classified effect. (The yolo-with-REAL-sandbox
integration RED — hostile suite unchanged under yolo — lives in T27, where the real
backend exists; r4 codex #5.)

## Phase 6 — sandboxed exec (gated by T02)

**[ ] T25 — SandboxBackend + bwrap backend (S6.2-P0).**
`SandboxBackend{Probe,Compile,Launch,Attest}`; bwrap: B4 closure (`/usr`,`/lib*`,`/bin`,
`/sbin` dirs; `/etc/ld.so.cache`+CA certs FILE grants; never recursive `/etc`), disposable
RW workdir, net unshared, S1.2 identity inside Launch; promoted absolute ELF only;
scripts/shell-strings/undeclared children rejected; profile attestation.
Trace: HARDQ D1/B4; E10; DESIGN-S0 interface; PRD §6 item 6.
Acceptance: T02 hostile suite re-run through the REAL backend → GREEN.
RED: `/etc/shadow` → EACCES; egress denied; `/bin/ls` runs; shebang rejected; timeout →
tree dead; attestation mismatch → result untrusted.

**[ ] T26 — Exec tool on the effect-path.**
`ExecutionKind=ExecProcess` sealed; one-shot exec through T14 EffectPath → T25 backend;
observation pruning preserves exit status + error lines.
Trace: E5/E8; DESIGN-FIXES; PRD §6 item 6.
Acceptance: model-requested command runs sandboxed e2e, ASK path exercised.
RED: `TestReadOnlyProcessStillSandboxed` (real-backend integration — owned HERE);
unknown kind → reject.

## Phase 7 — P0 closure

**[ ] T27 — P0 acceptance run + release attestation + `P0-capable` grant.**
Scripted end-to-end checks for ALL SIX PRD §6 criteria + full hostile suite + minimal
release signing (binary signed; built-in Telegram adapter integrity attested by that
signature — HARDQ A1's second half) + doctor upgraded to grant `P0-capable` from live
criteria. Acceptance harness lives OUTSIDE the ablated implementation.
Trace: PRD §6; HARDQ A1 (attestation half); T11 snapshot promise; constitution review gate.
Acceptance: six-for-six criteria GREEN on the deployment host; final sealed capability
snapshot contains LIVE sandbox+provider+channel probe attestations (fakes gone); 3-agent
adversarial review of the whole P0 before declaring done.
RED: per-criterion sensitivity — for EACH of the six criteria, a controlled feature-off
switch or targeted mutation (never reverting the check itself) turns exactly that
criterion's check RED; tampered binary → signature verification fails; each live probe
removed or stale-hashed → its capability OFF in the snapshot and no dispatch to it;
**yolo integration (F2):** with the REAL backend, the hostile sandbox/egress suite passes
unchanged under `--yolo` (yolo never weakens containment).

---

Deferred ledger (do NOT build in P0): full S7 (P2) · full S5 shadow-git/worktree (P3) ·
**MEMORY_FORGET + DATA_PURGE (both P2.1 — their normative owner; P0 correction =
supersession; USP #4 completes at P2)** ·
Decay+Audn (P1) · transactional Activator (P4+) · entropy secret detection (P4) ·
SemanticHealth evaluator (P2+; C8's two positive signals ARE in T17/T20) · upcast
chain/quarantine (v2) · native sandbox helper (P1 hardening) · plugin channels (P) ·
coding trio TIA/symedit/deep evidence (P1).
