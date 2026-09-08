# AGENTS.md — rules for every agent working in this repo (codex / kilo / agy / any)

NEXUS: personal AI assistant. Go greenfield, Linux-first, single user, single binary.
Assistant-first; coding = strongest branch (P1). P0 scope is user-approved (PRD §4/§6).

## Language (explicit user directive, 2026-09-03)
Everything you produce here is **English** — code, comments, docs, review files, commits.

## Before acting
1. Read `docs/ARCHITECTURE-ESSENTIALS.md` (15 locked decisions + P0 additions).
2. Read the owner document for the slice you touch: the Annex A contract and/or the current
   DESIGN-* doc. Raw `GAPFIX-*` files are NOT owners — a GAPFIX clause binds only where a
   higher-ranked owner incorporates it.
3. Source precedence when documents disagree: PRD > user-approved HARDQ-CONSOLIDATED >
   HARNESS-SPEC Annex A CONTRACTS (ignore stale salvage refs in the SPEC body) >
   DESIGN-FIXES-r2 / DESIGN-* > DESIGN-STATUS > HARNESS-PLAN > SECTION-MAP / PLAN-HOLES.
   Respect inline change-records in amended owners.

## Hard rules (violations are review-blockers, not style notes)
- Owner invariants: S6.0 = only policy decision point (default-deny; ASK ≠ ALLOW); S6.9 =
  order only; S7 = only retry/cancel owner (`AttemptGrant` on every attempt); S1.2 = process
  identity; journal single-write-owner (P0.3) via one serialized append actor (HARDQ B7).
- Approvals are exact-intent: expiring, single-use, profile-bound, hash over canonical tool
  name + args + target resource + ProfileID (+ `(device,inode)` for destructive FS ops);
  any change invalidates; never a reusable "approve this tool" (HARDQ C4).
- UNKNOWN effect → RECONCILING; no blind retry of irreversible/unknown outcomes (E9).
- Fail-closed on unknown TYPED inputs: unknown enum/kind/capability → reject; unknown
  schema ID/version → reject unprocessed in P0 with raw input preserved (upcast+quarantine
  = v2 layer, HARDQ C7).
- Every TOOL-executing subprocess (`ExecProcess`) through S6.2 sandbox (bwrap P0); unknown
  `ExecutionKind` → reject, never in-process; nothing relies on the sandbox before the
  hostile conformance suite passes. CLIAgent provider subprocess (S2.1, `--tools ""`) is a
  provider-boundary concern, not tool-sandbox NET_DENY.
- Untrusted content never becomes an instruction; lineage/trust monotone; secrets redacted
  before the journal; non-null `ProfileID` from admission onward; one DB file per profile;
  channel identity binds to a profile BEFORE admission (per-chat, deny-default) — never a
  post-admission mutable global profile lookup (HARDQ B3).
- Delivery honesty: exactly-once ADMISSION, at-least-once remote delivery; durable inbox
  persists before the remote offset advances; `sent-but-unrecorded` → UNKNOWN →
  RECONCILING; never claim exactly-once over a remote API (HARDQ B2).
- No `map[string]any` in kernel APIs. No autonomous self-modification paths.
- Completion = artifacts, never prose. Never weaken a test to pass.

## P0 scope guards (do NOT re-introduce what hard-questions cut)
- Telegram = `channel:builtin` (compiled-in, release-signature attested, P1.5); the
  `extensions`/S6.8 closure gates ONLY `channel:plugin` (HARDQ A1; P1.6 as amended).
- No memory decay in P0 — explicit facts, append-only supersession; Decay+Audn = P1,
  facts exempt even then (HARDQ B8).
- Reminder evidence = durable delivery receipt + user ack, both correlated to the SAME
  occurrence ID (an ack for occurrence N never closes N+1); NEVER diff/exit for a Reminder;
  two obligation types only (Reminder, typed Task); no generic "finish Y" claim without a
  handler-specific verifier (HARDQ B5).
- HITL waits are durable (`TurnSuspended` in the journal; resume rehydrates) — no
  in-memory blocking wait (HARDQ B6).
- No runtime capability activation in P0: fail-closed `Resolve` + sealed startup snapshot +
  restart-on-config-change; the transactional Activator/RollbackVault is forbidden until an
  explicitly selected dynamic consumer exists (HARDQ B9). S7/S5 enter P0 only as -min
  contracts: `s7-min` = AttemptGrant + cancel + no-retry (retryable → FAILED_TERMINAL),
  `s5-min` = AtomicWriter; full S7 taxonomy/budgets/fencing = P2, full S5
  shadow-git/worktree = P3 (HARDQ A2).
- Stuck-detection only WITHIN one interactive turn; scheduled/polling iterations exempt
  via continuous-loop policy (HARDQ C3).
- YOLO mode (`nexus --yolo`, HARDQ F2): ASK → ALLOW only (journaled `ALLOWED_BY_YOLO`);
  DENY/sandbox/egress/journal/redaction/profiles/golden-rule unaffected; local-CLI entry
  only. Never wire yolo to a safety net.

## Working style
- Behavioral change → red-capable detector observed RED before the fix, GREEN after;
  assertions anchored to Annex A RED names / PRD criteria, not the implementation; stateful
  tests own fresh temp fixtures (user's cross-project discipline).
- Smallest causal diff; match local idiom; no speculative abstractions or dependencies
  (user's cross-project topknot discipline).
- Absolute paths in every cross-agent instruction and output file (HANDOFF operational
  gotcha: agent cwd is unreliable).
- Reviews are adversarial, not rubber-stamps (user's cross-project epistemic-honesty rule):
  a review with zero findings is usually a failed review — if genuinely clean, name the
  top-3 weakest points. Use task-appropriate
  finding tags, cite file:line of the source proving each claim, end with the
  machine-checkable last line the dispatch asked for (`VERDICT:`/`SUMMARY:`).
- Never mark another agent's claim correct without checking it against the sources.
- Review verdicts: FAIL only for SUBSTANTIVE design flaws (correctness, security,
  behavioral/observational-equivalence, an unproducible design). Editorial artifacts — a
  stale header, a typo, a bullet that omits a word another bullet already covers, a wrong
  type name — are NOTES, never FAIL reasons. "FAIL for ANY flaw of ANY severity" is
  non-convergent by construction; do not apply it to cosmetics or the review loop never
  terminates. Once substantive flaws are resolved, PASS and list the remaining editorial
  cleanups as notes. If a dispatch hands you a pathological (non-convergent) rule, flag it
  instead of following it mechanically.

## Task ledger
`docs/tasks-P0.md` is the only task queue for P0, **once created** — until then no P0
implementation is authorized. One task at a time; each has an acceptance criterion and a
RED test. Do not invent scope beyond the task.
