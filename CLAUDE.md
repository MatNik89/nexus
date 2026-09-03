# CLAUDE.md — NEXUS repo constitution

NEXUS: personal AI assistant. Go greenfield, Linux-first, single user, single binary.
Assistant-first identity; coding is the strongest branch (P1), not the purpose.
User-approved PRD 2026-09-03: P0 = conversation · cross-session memory ·
obligations/reminders · Telegram · work/private profiles · Linux sandbox (PRD §6 criteria).

## Language
EVERYTHING in this repo is **English**: code, comments, docs, commit messages, and every
prompt sent to any agent. Croatian only in direct conversation with the user.

## Read order for any task
1. `docs/ARCHITECTURE-ESSENTIALS.md` — 15 locked decisions + P0 additions + open items.
2. The owner document for your slice (Annex A contract, DESIGN-*, GAPFIX-*).
3. `docs/tasks-P0.md` — the task ledger (one task at a time).

## Source precedence (when documents disagree)
PRD.md (product, user adjudicates) > user-approved `HARDQ-CONSOLIDATED.md` resolutions >
HARNESS-SPEC **Annex A contracts** (body carries stale salvage refs — never revive them) >
DESIGN-FIXES-r2 / DESIGN-*.md > DESIGN-STATUS > HARNESS-PLAN.md > SECTION-MAP / PLAN-HOLES.
Amended owners carry inline change-records; respect them.

## Hard rules (never violate; a PR that touches these needs explicit user sign-off)
- **Owner invariants:** policy = S6.0 PEP (default-deny, ASK ≠ ALLOW) · lifecycle order =
  S6.9 (never decides policy) · retry/deadline/cancel = S7 only (every attempt carries an
  `AttemptGrant`) · process identity = S1.2 · one serialized journal append actor (P0.3).
- **Fail-closed on unknown:** unknown enum/kind/schema/capability → reject, never guess,
  never a weaker fallback.
- **Sandbox:** every subprocess goes through S6.2 (`SandboxBackend`; bwrap in P0, native
  helper P1). NOTHING relies on the sandbox boundary before the hostile conformance suite
  passes on the deployment host. Unknown `ExecutionKind` → reject, never in-process.
- **Trust:** untrusted content never becomes an instruction; lineage/trust_class monotone
  (MAX/union); secrets redacted BEFORE the journal.
- **Profiles:** non-null `ProfileID` stamped at admission, carried through the whole causal
  chain; one SQLite file per profile; channel identity binds to a profile before admission.
- **No `map[string]any` in kernel APIs**; opaque payload = `json.RawMessage` + closed
  discriminator validation.
- **No autonomous self-modification:** anomaly → scrubbed RepairBundle → user opt-in →
  fix via git → `nexus upgrade`.
- **Evidence over prose:** completion is graded on artifacts (checker ≠ worker);
  `ObligationStore.MarkDone` only with Evidence; never weaken a test/criterion to go GREEN.

## Method — SDD per-slice
Big plan = vision/reference; build one slice at a time from `docs/tasks-P0.md`. Every
non-trivial task: acceptance criterion traceable to PRD/Annex A + a RED test observed RED
before the change and GREEN after, anchored to the spec (Annex RED names), never derived
from the implementation. Stateful tests own fresh temp fixtures.

## Multi-agent review (mandatory)
Every non-trivial deliverable (design, doc, code slice) goes through adversarial review by
the three herdr agents (codex, kilo, agy) BEFORE it is called done: independent passes →
moderator merge (verify findings against sources, no rubber-stamp fold) → fold → targeted
re-verification until convergence. Dispatch by pane_id; prompts in English; absolute paths.

## Go build rules
`CGO_ENABLED=0`; per-OS code only via build tags; SQLite = `modernc.org/sqlite` (WAL,
bounded busy_timeout); bwrap is a DECLARED install prerequisite checked by `nexus doctor` —
never a silent dependency. Non-Go tools never in-process (sidecar / pure-Go / descope).

## Git
Docs iterate on `main`. Code slices: branch per slice, self-check + agent review before
merge. Never commit secrets; `.gitignore` owns runtime artifacts.
