# Review of audit-remediation plan v7 (`docs/PLAN-AUDIT-FIXES.md`)

- **Reviewed revision**: `82de4b9` on `slice/p1-audit`
- **Verdict**: PASS

## Round-6 disposition

- **Note 1 (B2 stale `Next(op)` + "mark FAILED")** — CLOSED. `:250-253` now reads
  `g, err := s7.Next(op, build)`; `ErrNotDue -> skip (nothing committed)`;
  `ErrExhausted -> ALREADY landed (S7 FAILED + the builder's FAILED companion in one
  batch inside Next) — nothing further to do`.
- **Note 2 (S7 dual narrative)** — CLOSED. `:204-207` states the in-memory
  `machine.AttemptTable` and the durable `s7.*` projection are ONE story and adds a
  package doc comment tying them, plus `Next` refuses a nil builder on durable ops.

## New findings

None substantive. Both codex round-6 findings and the three stale-wording notes
are folded and verified:

- **Split-brain structured salvage (codex r6 #1) — closed.** `:342-349` runs the
  ordered salvage ladder (re-ask bytes, then original) INSIDE the final attempt's
  outcome decision: accepted salvage → `OutcomeSucceeded` with the value; nothing →
  `OutcomeFailedTerminal` with no value; "a value is never returned after `Execute`
  has committed a failed terminal state: the caller's result and the S7 state are
  one decision (returned value <=> `SUCCEEDED`)". Detector 9f (`:401-409`) now
  asserts state+result together in every case (a/b/b2/c/d).
- **`setMyCommands` E9 (codex r6 #2) — closed.** `:282` marks it `control-effect`
  with `RetryableCodes {transport_prewire, http_429}` ONLY, and `:292-297` splits
  control by effect: `control-read` (getMe, 5xx/post-write retryable) vs
  `control-effect` (setMyCommands, ambiguous → `OutcomeUnknown`, no new grant, menu
  re-registered as a NEW operation on next daemon start).
- **Three stale-wordings — removed.** B2 `Next(op, build)` (`:250`); one
  `PolicyPoll`/`PolicyControl` code list (`:298-301`); the delivery-kind
  classification line isolated as `:308-311`.

## Notes (not FAIL reasons)

1. **`PolicyControl` is one name for two code lists.** `:281` gives `getMe`
   (`control-read`) a `PolicyControl` with "5xx/post-write retryable", while
   `:282`/`:299-301` give `setMyCommands` a `PolicyControl` with
   `{transport_prewire, http_429}` only. `:299-301`'s summary "PolicyControl lists
   {transport_prewire, http_429}" is the EFFECT list, so a literal single
   `PolicyControl` would make `getMe`'s 5xx terminal — contradicting `:281`. The
   read/effect split itself is correct; the plan should name two constants
   (`PolicyControlRead` with the full retryable set, `PolicyControlEffect` with
   pre-wire/429) so the implementer cannot conflate them.

VERDICT: PASS
