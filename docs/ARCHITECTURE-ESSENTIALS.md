# ARCHITECTURE-ESSENTIALS — 15 critical decisions (cheat-sheet)

Distillation of the current design (HARNESS-PLAN 108 subsections + HARNESS-SPEC Annex A +
DESIGN-* + DESIGN-FIXES-r2 + 3-agent review REVIEW-ESSENTIALS-*). Daily reference while coding;
the full source always wins. Open preconditions are listed at the bottom — this document does
NOT claim go/no-go has been granted.

**Source precedence (when documents disagree):** PRD.md owns product decisions
(identity/scope — the user adjudicates) > **user-approved HARDQ-CONSOLIDATED resolutions**
(A/B/C/D/F items, 2026-09-03 — they supersede the specific clauses they amend in Annex A,
DESIGN-S0, DESIGN-STATUS and SECTION-MAP; the amended owners carry inline change-records) >
**HARNESS-SPEC Annex A contracts** (the contracts
only — the SPEC body still carries stale salvage references that the greenfield decision
C1/HARNESS-PLAN:3 deleted; never revive them via "SPEC wins") > DESIGN-FIXES-r2 /
DESIGN-*.md (they supersede named older parts) > DESIGN-STATUS (dated status ledger) >
HARNESS-PLAN.md > SECTION-MAP / PLAN-HOLES (some claims stale — e.g. PLAN-HOLES C3 predates
Annex A P0.5–P0.13, which EXIST at HARNESS-SPEC:1534+).

Format: **DECISION → why → source**.

---

## E1 — Go, `CGO_ENABLED=0`, single binary
One kernel = one self-contained binary (no cgo; static linking is the goal, verified by a CI
check, not assumed). SQLite spine = `modernc.org/sqlite` (pure Go, WAL). Per-OS code only via
build tags (`proc_*.go`, `sandbox_*.go`). Non-Go world tools NEVER in-process — deliberate
subprocess sidecar, pure-Go replacement, or descope (each choice explicit, no free "adapter").
One accepted purity exception (user-approved 2026-09-03): the P0 sandbox backend is bwrap —
a DECLARED install prerequisite checked by the doctor preflight, never a silent dependency.
→ HARNESS-PLAN header, S1.2, A9 component strategy; PLAN-HOLES C4; HARDQ D1/F1.

## E2 — One kernel, two profiles; assistant FIRST (approved 2026-09-03) + no self-modification
`AssistantProfile` = the everyday face (chat/memory/obligations/channels); `CodingProfile` =
strongest branch (P1), not the purpose. Same S0–S9 kernel, same TurnLoop — only gating differs
per profile. Linux first. Written into PRD v1 and **formally approved by the user
2026-09-03** (recorded in PRD.md; DESIGN-STATUS #6 RESOLVED). C7's profile listing is
corrected per A8:
coding USP trio = TIA + evidence-gate + AST-symedit (shadow-git was disproven as unique).
**Golden rule:** NEXUS never modifies its own source autonomously — anomaly → scrubbed
RepairBundle → user opt-in → Claude fixes via git → `nexus upgrade`.
→ PRD §3–5, A3.1/A3.2, A8; PLAN-HOLES C7.

## E3 — Hand-written typed Go contracts are the source of truth
JSON-Schema/OpenAPI/protobuf are derived projections, never the source. No `map[string]any`
in kernel APIs; opaque payload = `json.RawMessage` + closed discriminator validation
(default-reject enum branches compensate for missing sum types). `Content` XOR `ContentRef`
enforced in constructors.
→ HARNESS-PLAN S0 synthesis.

## E4 — Persistence: EventJournal owns canonical events; everything else atomic behind owners
`EventJournal.Append` is the ONLY writer of canonical domain events and the state derived
from them; state/log/trace/transcript/audit/metrics are PROJECTIONS (fold), never parallel
writers; no projection allocates event_id/sequence. State is RECONSTRUCTED by folding events;
checkpoint = journal offset. Secrets redacted BEFORE the journal. ObligationStore writes
THROUGH the journal. Scope limit (do not over-read): workspace files, memory spine, backups
are separate durable side-effects behind their effect-path owners. File/workspace mutations
(S5.1/5.3 scope, P0.11) use `AtomicWriter` (tmp → fsync → rename, never in-place) with a
snapshot taken BEFORE every FS effect; rollback is byte-identical and never touches
non-staged user changes. Persistence engines (SQLite-WAL spine) provide their own durable
atomicity contract — do NOT wrap per-record writes in rename. **Physical commit boundaries
(HARDQ B7):** WAL + bounded `busy_timeout`; one serialized append actor owns Append +
sequence allocation; core-state projections fold in the SAME transaction; three named
`BEGIN IMMEDIATE` recipes commit together: inbox-admission + journal event · occurrence +
run admission · terminal result + outbox enqueue. Daemon owns the DB; CLI connects via UDS.
Self-DoS exception: debug telemetry may best-effort drop; state/security/audit classes never.
→ HARNESS-SPEC P0.3/P0.11; S1.3, S5.3, S15; DESIGN-FIXES ownership fix.

## E5 — Owner invariants (one owner per concern, no exceptions)
- **Policy ALLOW/ASK/DENY = S6.0 PEP** (`Decide`, total switch, default-deny; ASK ≠ ALLOW).
- **Lifecycle ORDER = S6.9** (before/after/on_error branch) — 6.9 NEVER decides policy.
- **Retry/deadline/cancel/failover = S7 ONLY** — every physical attempt carries an
  `AttemptGrant`; 2.2 / 2.3 re-ask / 3.3 / 3.4 / fleet / AccountFleet only classify or propose.
- **Process identity = S1.2** (PID + start-token; killpg/Job Object) — consumed by S6.2.Launch.
- **Queue lease/fencing = S7.2** (CAS claim, fencing_token++, stale → `STALE_FENCING_TOKEN`).
→ SECTION-MAP owner invariants; S7; DESIGN-FIXES-r2.

## E6 — Build order: two documents, two roles — do not conflate
**SECTION-MAP §1 (K0 → K1 → L → M–P) is the dependency DAG** — topological constraints on
what must exist before what (cycles broken via -min contracts; REVIEW2 K4). **HARNESS-SPEC
"Redoslijed gradnje" (P0–P6) is the vertical delivery-slice plan** — what each shippable
slice contains. They are NOT the same ordering (e.g. SPEC's P1 minimal slice ships
provider/loop/tool/sandbox before full S7/S5; SECTION-MAP places S7/S5 edges in K1): a P0
task plan must satisfy SECTION-MAP dependency edges WITHIN the SPEC's vertical slices.
Invariants that hold either way: S16.6-det minimal checker is a K0/P0-phase primitive;
sandbox (S6.2) lands BEFORE tools/exec (S4). **RESOLVED (hard-questions, unanimous):**
SPEC vertical slices bind delivery; SECTION-MAP DAG edges are satisfied by NAMED -min
contracts pulled forward — `s7-min` (AttemptGrant + cancel + no-retry; **superseded
2026-09-09 by the full S7 engine in P1, owner decision — see AGENTS.md change-record**), `s5-min`
(AtomicWriter), `3.6-min` (persisted scheduler + wake catch-up), `7.3-min` (inbox/outbox +
occurrence idempotency); full engines keep their later phases. Walking-skeleton order:
HARDQ-CONSOLIDATED A2 (7 steps) → tasks-P0.md.
→ SECTION-MAP §1; HARNESS-SPEC:1020–1056; HARDQ A2.

## E7 — Transactional capability activation (atomic activation is impossible)
Gates with OS/external side-effects cannot activate atomically. Immutable
`ActivationPlan` (+PlanHash) → `Prepare → Commit → Activate` + `RollbackToken` + explicit
`PREPARING / FAILED / ROLLING_BACK` states; partially ACTIVE set is illegal; all gate
attestations bind the SAME config_hash; `Effective() = min(declared, measured)` and any
config change invalidates the grant (P0.5). RED: `test_every_flag_fails_when_one_gate_is_ablated`.
**P0 scope note (hard-questions, unanimous OVERENG):** P0 has zero runtime activation —
it ships only fail-closed `Resolver.Resolve` validation + a sealed startup capability
snapshot (config + live sandbox/provider/channel probes; restart on config change). The
Activator/RollbackVault machinery above activates with the first dynamic consumer
(extensions, P4+). This decision stays locked as the design for that phase.
→ DESIGN-S0-sandbox-codex; HARNESS-SPEC P0.4/P0.5; DESIGN-STATUS #2; HARDQ B9.

## E8 — One effect-path, sandbox IN the type
`S3.Loop → sealed ToolSpec.ExecutionKind → S6.0.Decide → S6.9.Before →
{InProcessExecutor | SandboxedProcessExecutor(S6.2.Launch + S1.2)} → S6.9.After/OnError → S7`.
Branch on the sealed `ExecutionKind`; unknown kind → REJECT, never inproc. Read-only shell is
still a process → sandboxed. In-process tools NEVER spawn. Concrete executor types inside the
`EffectPath` struct — an unsandboxed executor cannot be injected.
→ DESIGN-FIXES-r2 K1/K2 (canonical code).

## E9 — Effect taxonomy instead of the impossible cancel
"Cancel mid-tool → no side-effect" DOES NOT EXIST. Instead: `CommitReceipt` + phases
`BEFORE_COMMIT / AFTER_COMMIT / UNKNOWN`; every EFFECTFUL call without a valid receipt →
UNKNOWN → RECONCILING (never blind retry, never straight back to READY). Irreversible effects
go through P1.4 draft → approve → commit → verify → compensate; approval binds the
exact-intent hash — a modified effect cannot ride an old approval.
→ DESIGN-FIXES classifyEffectPhase; S7.1/7.2; HARNESS-SPEC P1.4.

## E10 — Sandbox (BIGGEST RISK): bwrap = P0 ENFORCED backend; native helper = P1 hardening
**User decision 2026-09-03 (amends the earlier bwrap-as-optional stance):** P0 backend =
bubblewrap behind the `SandboxBackend` interface (Probe/Compile/Launch/Attest), fail-closed,
attested, and gated by the SAME hostile conformance suite — **the suite is the invariant,
the backend is swappable.** The hand-written self-reexec helper (ABI probe → `no_new_privs`
→ Landlock → per-arch seccomp-BPF → irreversible `execveat`, no unsandboxed window, TOCTOU
fail-closed) remains the P1 hardening path per DESIGN-S0. **P0 command contract is
deliberately small (HARDQ B4, refined by the Phase-0 code review 2026-09-03):** promoted
absolute ELF executables with a SYNTHETIC content-pinned closure — ONLY the target and its
resolved loader/library dependencies, each copied into a memfd at Prepare time and bound
via `--ro-bind-data` (path swap and inode truncation are inert) — plus a disposable RW
workdir. NO blanket `/usr`/`/lib*` dir grants (the earlier dir-grant wording contradicted
"undeclared child rejected": codex demonstrated `exec /usr/bin/id` under a bound `/usr`);
an undeclared child executable simply does not exist in the mount namespace. NEVER
recursive `/etc`; no `/etc` file grants at all in the P0 profile. Scripts, shell strings,
non-ELF targets REJECTED before any sandbox setup. Syscall floor = real seccomp cBPF
(pure Go assembler, arm64/amd64; other arches fail closed). Hostile suite on the real
deployment host =
go/no-go BEFORE anything RELIES on the sandbox boundary; kernel/ABI floor published (C5);
doctor preflight (F1) checks bwrap + kernel and offers consented install — bwrap absent →
exec capability OFF (conversation-only), no weaker fallback. **Win/macOS in v1 =
`UNAVAILABLE` + high-risk exec BLOCKED**; their probes deferred with those platforms.
→ HARDQ D1/B4/C5/F1; DESIGN-S0-sandbox-codex; PLAN-HOLES C2; PRD §6 item 6, §7.

## E11 — Fail-closed is the default answer to the unknown; egress is a real dialer boundary
Unknown enum → reject; unknown schema ID/version → REJECT unprocessed in P0, raw input
preserved (the upcast chain + QUARANTINE sink are the v2 migration layer's — HARDQ C7 —
and never a lossy downcast or silent drop when they activate);
unknown capability `Resolve` → error; config must not WIDEN the kernel floor
(`ValidateBounds`); route below capability floor → error; canary token in output → block the
ENTIRE delivery. Egress is NOT string filtering: policy-aware resolver + dialer that pins
every resolved IP before connect (DNS-rebinding/multi-A safe), metadata-IP blocked in every
encoding, redirect re-check, proxy-env sanitization, child-socket containment, egress receipt.
Crypto primitives are distinct and never improvised: HMAC (`hmac.New`) = keyed MAC for local
integrity; Ed25519 = digital signature for external anchor; plain pinned hash for skill-lock
verify-on-load; NEVER `hash.Sum(key)` (key leaks into output — forgeable).
→ S0.1/0.3/0.4, S1.1, S2.6, S6.3, S6.5; PLAN-HOLES codex egress finding; DESIGN-symedit-crypto (D2).

## E12 — Trust/lineage monotone; untrusted NEVER becomes instruction
`trust_class(summary) = MAX(sources)`, `sensitivity = MAX`, `lineage = union` — compaction
must not launder untrusted content (P0.1 RED `test_s0_rejects_provenance_laundering`).
Prompt-assembler trust fencing; every search hit datamarked untrusted; remote A2A peer =
UNTRUSTED (card is not authorization).
→ HARNESS-SPEC P0.1 (:1383); S8.2, S11.1, S12.5, S4.4.

## E13 — Evidence-graded completion: checker ≠ worker; minimal checker is P0, trio is P1
`Checker.Grade` grades ARTIFACTS (git diff + real exit code + deterministic signals), NEVER
the worker's prose. Generic `AcceptanceContract` + `EvidenceBundle` (coding = diff+exit;
assistant = delivery receipt / calendar / grounding). **The minimal deterministic checker
(S16.6-det) is a K0 primitive and a P0 dependency** — `ObligationStore.MarkDone` accepts only
Evidence, so P0 obligations cannot ship without it. The full coding USP trio — TIA (USP #2),
AST-symedit (USP #3), deep evidence-gate (USP #1) — is the P1 coding branch. Credit (16.4)
only from verified outcomes, never self-report.
→ SECTION-MAP K0; S3.1, S16.6, S9.6; DESIGN-checker-tia-edit-agy; A8.

## E14 — Memory: explicit facts in P0, physical profile isolation, FORGET ≠ PURGE
**P0 memory = explicit profile-bound facts** ("remember X" = the write intent, stored after
an exact preview; inferred facts stay session-only or in a batch-review queue — not
recallable until accepted; raw transcripts are NOT semantic memory), append-only
supersession, deterministic recency + exact/tag retrieval, bytes lossless. **NO decay
machinery in P0** (hard-questions: FSRS on facts would "forget" the user's server IP and
violate PRD §6.2): Decay(FSRS-lite) + Audn move to P1 `memorija`, activated only on a
measured retrieval problem, and NEVER applied to explicit facts (fact type exempt, S=∞);
Dream/MemGit/MemAssoc = v2. **MEMORY_FORGET (reversible, recall ban) ≠ DATA_PURGE
(irreversible, S6.7 overrides, deletes the tombstone too)** — USP #4; BOTH land with their
normative Annex P2.1 contract (task-review fix — neither is a PRD §6 criterion; P0
correction = append-only supersession). **Profile isolation is
physical (HARDQ B3): one SQLite file per profile** (+ system DB with zero profile payloads);
scope column stays only as a redundant tag; non-null `ProfileID` stamped at admission and
carried immutably through the whole causal chain (envelope→run→tool→memory→obligation→
occurrence→journal→inbox/outbox→approval); channel identity binds to a profile BEFORE
admission (per-chat deny-default binding). Memory write-approval DEFAULT ON.
→ S9.2/9.4/9.5; HARNESS-SPEC P2.1; GAPFIX-kilo; HARDQ B3/B8.

## E15 — Channels: durable delivery (honest at-least-once) + remote HITL; S7 owns retries
Gateway core (identity, deny-default allowlist, threading, receipts); adapters thin
(Telegram first). `run_done ≠ result_delivered` — two durable states, transactional outbox
AND durable inbound inbox keyed `(adapter_id, channel_identity, update_id)` with
`RECEIVED → ADMITTED → TERMINAL` (normalized message persisted BEFORE the remote offset
advances; replay returns the existing outcome). **Honest delivery boundary (HARDQ B2): the
local outbox gives exactly-once ADMISSION and at-least-once remote delivery** — a
non-transactional remote API cannot be exactly-once; `sent-but-unrecorded` →
UNKNOWN → RECONCILING, never blind retry. The gateway provides idempotency keys and
receipts; **S7 alone authorizes and schedules delivery retries.** Remote HITL is DURABLE
(HARDQ B6): on `DecisionAsk` the loop commits `TurnSuspended` and exits; approval appends
`ApprovalReceived` and resumes from the journal — survives restart; token = exact-intent +
expiring + single-use (replay → `APPROVAL_REPLAY`; canonical intent fields per HARDQ C4).
**RESOLVED (hard-questions, unanimous): `channel:builtin` vs `channel:plugin` split** — the
P0 Telegram adapter is compiled in and attested by the release signature (P1.5); the P1.6
extensions closure gates ONLY dynamically loaded adapters (P1.6 amended; its RED keeps
testing the plugin path).
→ S14.5, S7.3 (N9), S12.4; HARNESS-SPEC P1.6 (amended); HARDQ A1/B2/B6/C4.

---

## Provider layer (compressed — not one of the 15, but P0-relevant)
One `Provider` interface (`Chat/Stream/Capabilities/DataDescriptor`); auth is pluggable:
APIKey (default) · OAuth (sanctioned) · CLIAgent (subprocess `codex`/`claude` with
`--tools ""` — NEXUS keeps the loop). SubscriptionOAuth is **cut from P0** (ToS/ban risk;
opt-in later, fail-closed without `acknowledged_tos`). The loop never knows the mode.
Structured output: validate → re-ask (carries AttemptGrant) → salvage; never silently accept
invalid output. → S2.1/2.2/2.3; PLAN-HOLES C6.

## P0 scope (precise labels — "not in P0" ≠ "out of v1")
P0 = 6 capabilities from PRD §4 (conversation · spine memory · obligations · Telegram ·
profiles · Linux sandbox) with the six done-criteria of PRD §6 as the acceptance contract
(memory survives restart; reminder survives shutdown and fires on time; Telegram irreversible
action pre-approved; zero profile leakage; sandbox denies `/etc/shadow` and network under
hostile test). **Out of v1 entirely (PRD §5):** multi-user SaaS, autonomous self-repair,
Win/mac isolation promises, ToS-bypass defaults, desktop GUI. **Cut from P0, lives in P1/P2
or awaits PRD decision (PLAN-HOLES C6):** learned router, subscription-OAuth, council/flows/
boards, GraphRAG/CAG, fleet/pairing, video, computer-use, voice-WebRTC, K8s, callgraph/
archmap layer, PROV-O, guardian-LLM. Coding USP trio = P1.

## P0 additions from the hard-questions round (adopted, unanimous or convergent)
Pulled into P0 as -min cuts: durable local scheduler + wake catch-up (HARDQ B1) · Telegram
inbox/outbox + occurrence idempotency (B2) · two obligation types Reminder/Task with
assistant-grade evidence (B5) · durable HITL suspend/resume (B6) · single append actor +
synchronous core projections + transaction recipes + daemon-owns-DB/CLI-via-UDS (B7, in E4)
· known-ref secret redaction (C1) · non-text fail-closed reply (C2) · **stuck-detection
scoped to WITHIN one interactive turn — scheduled/polling occurrences carry an explicit
continuous-loop policy exempt from the identical-argument breaker (C3; RED in tasks-P0.md)**
· **YOLO mode `nexus --yolo` (user directive 2026-09-03, HARDQ F2): ASK → ALLOW for the
session, no prompts/HITL parking; DENY unchanged; sandbox/egress/journal/redaction/
profile-isolation/golden-rule UNAFFECTED; local-CLI entry only; decisions journaled
`ALLOWED_BY_YOLO` — yolo disables confirmations, never safety nets**
· **P0 health = liveness heartbeat + last-occurrence-fired counter (C8)** · **doctor
preflight (F1): checks kernel/ABI floor, bwrap, data-dir permissions, provider key, Telegram
token; anything missing → consented install or exact instructions; results are
capability-scoped (no key → conversation off; no token → Telegram off; no bwrap → exec off;
unsafe data-dir → stateful startup blocked); `P0-capable` = all six PRD §6 criteria
reachable, not sandbox alone.** Deferred OUT of P0: transactional Activator (B9), Decay+Audn
(B8 — Annex P0.13 relabeled), full S7/S5 engines (A2), entropy secret detection,
SemanticHealth evaluator (C8), upcast chain + quarantine sink (C7 — Annex P0.1 scoped),
P0.8/P0.9/P0.11-checkpoint contracts relabeled to their real triggers (C6).

## Open before P0 code
1. **Hostile-conformance suite against the bwrap backend on the real deployment host** —
   go/no-go before anything relies on the sandbox boundary; kernel/ABI floor published (E10).
2. Win/mac real-OS probes — user hardware; deferred with those platforms (not a P0 blocker).
(CLOSED: PRD approval 2026-09-03 · P1.6/Telegram split — HARDQ A1 · build order — HARDQ A2 ·
sandbox backend D1 = bwrap, user 2026-09-03.)
(Annex A P0.5–P0.13 contracts EXIST — HARNESS-SPEC:1534+; the older PLAN-HOLES C3 claim is stale.)
