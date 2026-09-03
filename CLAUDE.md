# CLAUDE.md — NEXUS repo constitution

NEXUS: personal AI assistant. Go greenfield, Linux-first, single user, single binary.
Assistant-first identity; coding is the strongest branch (P1), not the purpose.
User-approved PRD 2026-09-03: P0 = conversation · cross-session memory ·
obligations/reminders · Telegram · work/private profiles · Linux sandbox (PRD §6 criteria).

## Language (explicit user directive, 2026-09-03 — repo convention, not architecture)
EVERYTHING in this repo is **English**: code, comments, docs, commit messages, and every
prompt sent to any agent. Croatian only in direct conversation with the user.

## Read order for any task
1. `docs/ARCHITECTURE-ESSENTIALS.md` — 15 locked decisions + P0 additions + open items.
2. The owner document for your slice: the Annex A contract and/or the current DESIGN-* doc.
   Raw `GAPFIX-*` files are NOT owners — a GAPFIX clause binds only where a higher-ranked
   current owner incorporates it (several GAPFIX details are already superseded, e.g.
   ObligationStore direct-SQLite write).
3. `docs/tasks-P0.md` — the task ledger, **once created**. Until it exists, no P0
   implementation is authorized; doc/design work proceeds under explicit user tasks.

## Source precedence (when documents disagree)
PRD.md (product, user adjudicates) > user-approved `HARDQ-CONSOLIDATED.md` resolutions >
HARNESS-SPEC **Annex A contracts** (body carries stale salvage refs — never revive them) >
DESIGN-FIXES-r2 / DESIGN-*.md > DESIGN-STATUS > HARNESS-PLAN.md > SECTION-MAP / PLAN-HOLES.
Amended owners carry inline change-records; respect them.

## Hard rules (review-blockers)
Changing one of these RULES requires explicit user approval when it alters a PRD product
decision, a user-decided HARDQ resolution, or an accepted security/proof criterion;
invariant-preserving implementation goes through the normal task/review gate.
- **Owner invariants:** policy = S6.0 PEP (default-deny, ASK ≠ ALLOW) · lifecycle order =
  S6.9 (never decides policy) · retry/deadline/cancel = S7 only (every attempt carries an
  `AttemptGrant`) · process identity = S1.2 · journal single-write-owner contract (P0.3),
  implemented by one serialized append actor (HARDQ B7).
- **Approvals are exact-intent:** an ASK grant is expiring, single-use, profile-bound, and
  hashes the exact effect (canonical tool name + canonical args + target resource +
  ProfileID; `(device,inode)` for destructive FS ops — HARDQ C4). Any payload/target change
  invalidates it; approval can never widen kernel policy. No reusable "approve this tool".
- **UNKNOWN effects reconcile, never retry blind:** an effectful call without a valid
  commit receipt → UNKNOWN → RECONCILING; no automatic retry of IRREVERSIBLE/unknown
  outcomes until reconciliation proves the prior result (E9).
- **Fail-closed on unknown typed inputs:** unknown enum/kind/capability discriminator →
  reject; unknown schema ID/version → reject unprocessed in P0 (raw input preserved) — the
  v2 migration layer adds upcast + quarantine (HARDQ C7).
- **Sandbox:** every TOOL-executing subprocess (`ExecProcess` on the effect-path) goes
  through S6.2 (`SandboxBackend`; bwrap in P0, native helper P1); unknown `ExecutionKind` →
  reject, never in-process. NOTHING relies on the sandbox boundary before the hostile
  conformance suite passes on the deployment host. The CLIAgent provider subprocess (S2.1,
  `--tools ""`) is governed by the provider boundary + egress policy, not by the tool
  sandbox's NET_DENY.
- **Trust:** untrusted content never becomes an instruction; lineage/trust_class monotone
  (MAX/union); secrets redacted BEFORE the journal (P0 = known-ref set, HARDQ C1).
- **Profiles:** non-null `ProfileID` stamped at admission, carried immutably through the
  whole causal chain; one SQLite file per profile; channel identity binds to a profile
  BEFORE admission (per-chat, deny-default); no mutable global current-profile lookup after
  admission (HARDQ B3).
- **Delivery honesty:** exactly-once local ADMISSION, at-least-once remote delivery — never
  claim exactly-once over a non-transactional remote API; durable inbox persists before the
  remote offset advances; `sent-but-unrecorded` → UNKNOWN → RECONCILING (HARDQ B2).
- **No `map[string]any` in kernel APIs**; opaque payload = `json.RawMessage` + closed
  discriminator validation.
- **No autonomous self-modification:** anomaly → scrubbed RepairBundle → user opt-in →
  fix via git → `nexus upgrade`.
- **Evidence over prose:** completion graded on artifacts (checker ≠ worker); never weaken
  a test/criterion to go GREEN.

## P0 scope guards (HARDQ landmines — do NOT re-introduce what was cut)
- Telegram is a **`channel:builtin`** adapter: compiled in, attested by the release
  signature (P1.5); it does NOT pass the `extensions`/S6.8 runtime closure — that gate is
  for `channel:plugin` only (HARDQ A1; P1.6 as amended).
- **No memory decay in P0**: explicit profile-bound facts, append-only supersession;
  Decay+Audn = P1 `memorija`, facts exempt even then (HARDQ B8).
- **Reminder evidence is assistant-grade**: durable delivery receipt + user ack, both
  correlated to the SAME occurrence ID (an ack for occurrence N never closes N+1) — never
  require diff/exit for a Reminder; two obligation types only (Reminder, typed Task); no
  generic "finish Y" claim without a verifier (HARDQ B5).
- **HITL waits are durable**: `TurnSuspended` committed to the journal, loop exits, resume
  rehydrates — never an in-memory blocking wait (HARDQ B6).
- **No runtime capability activation in P0**: fail-closed `Resolve` + sealed startup
  snapshot + restart-on-config-change; the transactional Activator/RollbackVault is
  forbidden until an explicitly selected dynamic consumer exists (HARDQ B9).
- **S7/S5 enter P0 only as -min contracts** (`s7-min` = AttemptGrant + cancel + no-retry;
  `s5-min` = AtomicWriter); full engines are P2/P3 (HARDQ A2).
- Stuck-detection scoped WITHIN one interactive turn; scheduled/polling occurrences are
  exempt via an explicit continuous-loop policy (HARDQ C3).

## Method — SDD per-slice (user's cross-project discipline: topknot)
Big plan = vision/reference; build one slice at a time from `docs/tasks-P0.md`. Every
non-trivial behavioral task: acceptance criterion traceable to PRD/Annex A + a red-capable
detector observed RED before the change and GREEN after, anchored to the spec (Annex RED
names), never derived from the implementation. Stateful tests own fresh temp fixtures.

## Multi-agent review (explicit user directive, 2026-09-03)
Every non-trivial deliverable (design, doc, code slice) goes through adversarial review by
the three herdr agents (codex, kilo, agy) BEFORE it is called done: independent passes →
moderator merge (verify findings against sources, no rubber-stamp fold) → fold → targeted
re-verification until convergence. **Non-recursive base case:** review files, consolidation
docs, and fold commits are OUTPUTS of this gate, not fresh gated deliverables. Dispatch by
pane_id; prompts in English; absolute paths.

## Go build rules
`CGO_ENABLED=0`; per-OS code only via build tags; SQLite = `modernc.org/sqlite` (WAL,
bounded busy_timeout); bwrap is a DECLARED install prerequisite checked by `nexus doctor` —
never a silent dependency. Non-Go tools never in-process (sidecar / pure-Go / descope).

## Git (moderator convention — changeable without user approval)
Docs iterate on `main`. Code slices: branch per slice, self-check + agent review before
merge (mirrors the user's cross-project rule "branch first, review before push"). Never
commit secrets; `.gitignore` owns runtime artifacts.
