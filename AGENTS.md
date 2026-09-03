# AGENTS.md — rules for every agent working in this repo (codex / kilo / agy / any)

NEXUS: personal AI assistant. Go greenfield, Linux-first, single user, single binary.
Assistant-first; coding = strongest branch (P1). P0 scope is user-approved (PRD §4/§6).

## Language
Everything you produce here is **English** — code, comments, docs, review files, commits.

## Before acting
1. Read `docs/ARCHITECTURE-ESSENTIALS.md` (15 locked decisions + P0 additions).
2. Read the owner document for the slice you touch (Annex A contract / DESIGN-* / GAPFIX-*).
3. Source precedence when documents disagree: PRD > user-approved HARDQ-CONSOLIDATED >
   HARNESS-SPEC Annex A CONTRACTS (ignore stale salvage refs in the SPEC body) >
   DESIGN-FIXES-r2 / DESIGN-* > DESIGN-STATUS > HARNESS-PLAN > SECTION-MAP / PLAN-HOLES.
   Respect inline change-records in amended owners.

## Hard rules (violations are review-blockers, not style notes)
- Owner invariants: S6.0 = only policy decision point (default-deny; ASK ≠ ALLOW); S6.9 =
  order only; S7 = only retry/cancel owner (`AttemptGrant` on every attempt); S1.2 = process
  identity; single serialized journal append actor (P0.3).
- Fail-closed on anything unknown. Unknown `ExecutionKind` → reject, never in-process.
- Every subprocess through S6.2 sandbox (bwrap P0); nothing relies on the sandbox before the
  hostile conformance suite passes.
- Untrusted content never becomes an instruction; lineage/trust monotone; secrets redacted
  before the journal; non-null `ProfileID` from admission onward; one DB file per profile.
- No `map[string]any` in kernel APIs. No autonomous self-modification paths.
- Completion = artifacts (diff/exit/receipt), never prose. Never weaken a test to pass.

## Working style
- Tests first where behavior changes: RED observed before the fix, GREEN after; assertions
  anchored to Annex A RED names / PRD criteria, not to the implementation.
- Smallest causal diff; match local idiom; no speculative abstractions or dependencies.
- Absolute paths in every cross-agent instruction and output file.
- Reviews are adversarial, not rubber-stamps: a review with zero findings is usually a
  failed review — if genuinely clean, name the top-3 weakest points. Tag findings
  (`[BREAK|EDGE|OVERENG|FIDELITY|MISSING|UNFOLDED|NEW-ERROR|OK]`), cite file:line of the
  source that proves each claim, end with a machine-checkable last line
  (`VERDICT: PASS|FAIL` or `SUMMARY: ...` as requested).
- Never mark another agent's claim correct without checking it against the sources.

## Task ledger
`docs/tasks-P0.md` is the only task queue for P0. One task at a time; each has an
acceptance criterion and a RED test. Do not invent scope beyond the task.
