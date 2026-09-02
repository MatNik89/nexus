# ARCHITECTURE-ESSENTIALS — 15 critical decisions (cheat-sheet)

Distillation of the current design (HARNESS-PLAN 108 subsections + HARNESS-SPEC Annex A +
DESIGN-* + DESIGN-FIXES-r2 + 3-agent review REVIEW-ESSENTIALS-*). Daily reference while coding;
the full source always wins. Open preconditions are listed at the bottom — this document does
NOT claim go/no-go has been granted.

**Source precedence (when documents disagree):** PRD.md owns product decisions
(identity/scope — the user adjudicates) > **HARNESS-SPEC Annex A contracts** (the contracts
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
→ HARNESS-PLAN header, S1.2, A9 component strategy; PLAN-HOLES C4.

## E2 — One kernel, two profiles; assistant FIRST (PENDING product approval) + no self-modification
`AssistantProfile` = the everyday face (chat/memory/obligations/channels); `CodingProfile` =
strongest branch (P1), not the purpose. Same S0–S9 kernel, same TurnLoop — only gating differs
per profile. Linux first. Proposed and written into PRD v1; **formal product approval is
still PENDING** (PRD:66 "awaits your confirmation"; DESIGN-STATUS #6 OPEN — see Open list).
C7's profile listing is corrected per A8:
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
atomicity contract — do NOT wrap per-record writes in rename.
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
sandbox (S6.2) lands BEFORE tools/exec (S4). Residual ordering conflicts → hard-questions.
→ SECTION-MAP §1; HARNESS-SPEC:1020–1056.

## E7 — Transactional capability activation (atomic activation is impossible)
Gates with OS/external side-effects cannot activate atomically. Immutable
`ActivationPlan` (+PlanHash) → `Prepare → Commit → Activate` + `RollbackToken` + explicit
`PREPARING / FAILED / ROLLING_BACK` states; partially ACTIVE set is illegal; all gate
attestations bind the SAME config_hash; `Effective() = min(declared, measured)` and any
config change invalidates the grant (P0.5). RED: `test_every_flag_fails_when_one_gate_is_ablated`.
→ DESIGN-S0-sandbox-codex; HARNESS-SPEC P0.4/P0.5; DESIGN-STATUS #2.

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

## E10 — Linux sandbox: helper with no unsandboxed window (BIGGEST RISK)
Self-reexec helper: ABI probe → `no_new_privs` → Landlock ruleset → per-arch seccomp-BPF →
irreversible `execveat`; FD/env sanitization; TOCTOU fail-closed. **Win/macOS in v1 =
`UNAVAILABLE` + high-risk exec BLOCKED — no silent weaker fallback ever.** The hostile
conformance suite on a real Linux kernel is a **go/no-go preflight BEFORE production
S6.2/arbitrary-exec implementation** (other P0 work may proceed; nothing may RELY on the
sandbox boundary before the probe passes); Win/mac real-OS probes (W0–W10 / M0–M9)
need the user's hardware and are deferred with those platforms. bwrap = optional external
adapter, not a guarantee.
→ DESIGN-S0-sandbox-codex; DESIGN-STATUS #3; PLAN-HOLES C2; PRD §6 item 6, §7.

## E11 — Fail-closed is the default answer to the unknown; egress is a real dialer boundary
Unknown enum → reject; unknown schema version → QUARANTINE (no downcast, no silent drop);
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

## E14 — Memory: spine + FORGET ≠ PURGE + profile isolation
SQLite-WAL spine; P0 ships only Decay (FSRS-lite — changes RANK, never existence) + Audn
(supersede-on-read, no silent overwrite); Dream/MemGit/MemAssoc = v2 descope.
**MEMORY_FORGET (reversible, recall ban) ≠ DATA_PURGE (irreversible, S6.7 overrides, deletes
the tombstone too)** — USP #4, the only P0-shipped differentiator. `PersonalProfile` =
deny-default isolation of memory/secrets/channels/cache: write in `work` → query in
`private` = 0 hits. Memory write-approval DEFAULT ON.
→ S9.2/9.4/9.5; HARNESS-SPEC P2.1; GAPFIX-kilo.

## E15 — Channels: durable delivery + remote HITL; S7 owns the retries
Gateway core (identity, deny-default allowlist, threading, receipts); adapters thin
(Telegram first). `run_done ≠ result_delivered` — two durable states, transactional outbox:
a gateway crash neither loses the reply nor repeats the side-effect. The gateway provides
idempotency keys and receipts; **S7 alone authorizes and schedules delivery retries** (no
second retry owner). Remote HITL: agent asks on the phone, run waits durably; approval token
= exact-intent + expiring + single-use (replay → `APPROVAL_REPLAY`).
**OPEN (hard-questions):** P1.6 declares `channels requires extensions` closure
(`INCOMPLETE_CAPABILITY_CLOSURE` fail-closed), but `extensions` is cut from P0 while Telegram
is IN P0 — resolve as built-in channel (Phase O stdio-built-in path) vs plugin channels
(Phase P), or amend P1.6. Do not code around it silently.
→ S14.5, S7.3 (N9), S12.4; HARNESS-SPEC P1.6; SECTION-MAP Phase O.

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

## Open before P0 code (verified against DESIGN-STATUS + HARNESS-SPEC)
1. **PRD formal approval** — identity (one kernel/two profiles) + P0 scope await the user's
   sign-off (PRD:66; DESIGN-STATUS #6). Until recorded, E2 is a proposal, not a decision.
2. **Linux hostile-conformance preflight** on a real kernel — go/no-go before production
   S6.2/arbitrary-exec implementation (E10).
3. **P1.6 vs P0-Telegram closure tension** (E15) — resolve in hard-questions round.
4. **Build-order reconciliation** (E6) — SECTION-MAP DAG vs HARNESS-SPEC slices; confirm in
   hard-questions, materializes in tasks-P0.md.
5. Win/mac real-OS probes — user hardware; deferred with those platforms (not a P0 blocker).
(Annex A P0.5–P0.13 contracts EXIST — HARNESS-SPEC:1534+; the older PLAN-HOLES C3 claim is stale.)
