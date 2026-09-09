# Review of audit-remediation plan v10 (`docs/PLAN-AUDIT-FIXES.md`)

- **Reviewed revision**: `8c3120b` on `slice/p1-audit`
- **Verdict**: PASS

## Round-9 disposition

- **Round-9 note ("the same identity reconciled FAILED and re-begun" contradiction)**
  — CLOSED. v10 adds `Reconcile(op, ok, build)` (`:210-219`) and rewrites the
  registration flow (`:323-325`): equal → SUCCEEDED; different → the SAME operation
  lands `FAILED_RETRYABLE` within budget and `Next` issues the replacement grant —
  explicitly "No `Begin` re-begin of a terminal identity exists" (`:219`).

## New findings

None substantive. The codex round-9 finding and note are folded and verified:

- **Reconciliation not realizable through the S7 API (codex r9 #1) — closed.**
  `Reconcile(op, ok bool, build)` is the ONLY exit from UNKNOWN (`:210-218`): present
  → `attempt.reconciled_ok` → SUCCEEDED; absent → the new canonical edge
  `attempt.reconciled_retry` (UNKNOWN → FAILED_RETRYABLE) within the existing
  attempt/deadline budget, else `attempt.reconciled_failed` → FAILED; only `Next`
  may then issue the next grant. Durable `s7.operation_reconciled{op, state,
  next_attempt_unix}` + exactly ONE companion commit in ONE batch; nil builder
  refused; UNKNOWN rehydrated. The channel validator enforces
  `operation_id == "control:tg:setMyCommands:" + payload_hash` with closed
  method/state sets (`:328-331`), so a foreign hash is refused at the journal
  boundary.
- **Stale "three policy CONSTANTS" note — closed.** `:139` now enumerates Tool,
  Provider, Structured, Delivery, Poll, ControlRead, ControlEffect, UI.

## Notes (not FAIL reasons)

- Detector D2b (`:560-563`) asserts S7 state, mismatched-hash refusal, and torn-batch
  injection with ablations. The `Reconcile` `ok bool` proof is the adapter's typed
  conclusion of the `getMyCommands`-vs-desired comparison; both adapter and S7 are
  trusted owners, so a boolean is sufficient (the raw remote state is not
  re-transmitted). This is a design note, not a defect.

VERDICT: PASS
