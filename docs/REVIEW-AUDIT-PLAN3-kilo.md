# Review of audit-remediation plan v3 (`docs/PLAN-AUDIT-FIXES.md`)

- **Reviewed revision**: `a5088e4` on `slice/p1-audit`
- **Verdict**: PASS

## Round-2 disposition

- **R2-F1 (Slice B Option 1 parked transient failures terminally -> HARDQ B2
  at-least-once violation)** — CLOSED. v3 builds the FULL S7 retry engine (owner
  decision, `PLAN-AUDIT-FIXES.md:103-107`). Transient failures now land
  `FAILED_RETRYABLE` and are retried via S7 backoff; permanent 4xx lands
  `FAILED` terminal (`:193-196`). This restores at-least-once for the
  retryable class without any adapter/loop retry loop, conforming to SPEC P0.2
  (`HARNESS-SPEC.md:1386-1393`) and HARDQ A2.

## New findings

None substantive. The engine design, delivery wiring, provider retry,
S7-governed polling, and toolchain gate are all buildable and close their
findings at the owner boundary. Verified against the current code:

- **State machine append is feasible.** `contracts.AttemptState` (`enums.go:218-227`)
  ends at `AttemptManualRecovery`; appending `AttemptFailedRetryable` keeps
  existing numeric values unchanged, and `machine.AttemptTable()`
  (`machine.go:200-213`) gains the four new transitions
  (`attempt.failed_retryable` RUNNING→FAILED_RETRYABLE,
  `attempt.retry_authorized` FAILED_RETRYABLE→AUTHORIZED, `attempt.exhausted`
  →FAILED, `attempt.cancelled` →CANCELLED) without disturbing the existing
  rows. `s7min` promotion to `s7` is one package; `Issue(op,target)` is
  preserved as `Begin(PolicyTool)+Next` for the loop.
- **Durability model is correct.** Durable `s7.operation_begun/attempt_authorized/
  attempt_reported/operation_terminal` + projection `s7_operations`; rehydration
  enforces `attempts < max_attempts` and `next_attempt_at` across restart
  (detector #6), and `attempt_authorized` append-failure refuses the grant
  (fail closed, no wire without a durable authorization).
- **Delivery wiring is correct.** Grant bound to `delivery:<id>` /
  `channel:<adapter>:delivery:<id>`; the adapter recomputes the expected op and
  compares before `Consume` (identity binding, detector #0b); the ordered
  lifecycle (grant → UNKNOWN lease → send → Report → SENT/UNKNOWN/PENDING/FAILED)
  leaves no operation AUTHORIZED/RUNNING after any outcome (detector #0); 5xx is
  `ErrAmbiguousSend` (never a definite failure); `redeliver` starts a NEW
  operation, never re-grants an exhausted one.
- **Provider retry (B3)** uses the same engine; stream failure after the first
  byte is terminal (correct — partial output already reached the user); tools
  keep `PolicyTool` MaxAttempts 1.
- **S7-governed polling (Slice D)** is a scheduled-iteration model that conforms
  to HARDQ C3: success completes `poll:<adapter>:<n>` and the next tick begins
  `<n+1>`; failure is retried only via S7 backoff; terminal 401/403 → `Run`
  returns and health records `remote_rejected`. `PolicyPoll` is `Durable:false`
  (in-memory), so no journal churn per 2s tick.
- **Slice F toolchain-directive + deploy gate** is correct: `toolchain go1.26.6`
  with `GOTOOLCHAIN=auto` removes the host-upgrade blocker, and the `go version
  "$BIN"` floor check fails closed independently even if `GOTOOLCHAIN=local`.
  `scripts/deploy.sh` running the same gate makes no slice installable unguarded.

## Notes (not FAIL reasons)

1. **`Policy` omits SPEC P0.2 `operation_id` / `cancel_token_id` MUST-fields.**
   `Policy` has `EffectClass/MaxAttempts/AttemptTimeout/Deadline/Backoff/
   RetryableCodes/FallbackTargets/IdempotencyKey/Durable` but binds
   `operation_id` via `Begin(op, …)` and `cancel_token_id` = the operation id
   (`Cancel(op)`). Semantics preserved; structurally the ExecutionPolicy shape is
   split (op as key, policy as value) rather than one struct. Worth a one-line
   comment mapping to the SPEC fields.
2. **"rehydrated RUNNING record" wording is imprecise.** The durability event set
   omits the Consume/`attempt.started` transition, so a rehydrated record is
   **AUTHORIZED**, not RUNNING. The correct, detector-#7-forced behavior is
   "rehydrated AUTHORIZED (possibly-consumed) → UNKNOWN"; the plan should say
   that literally, or the implementer could re-grant a touched-wire attempt
   (E9 double-send). Detector #7 asserting `operation UNKNOWN` resolves it.
3. **`FAILED` vs the SPEC's `FAILED_TERMINAL`.** The terminal state keeps the
   existing name `FAILED` while the new state is `FAILED_RETRYABLE`; semantically
   identical to SPEC P0.2, only the terminal name differs.
4. **Slice A's "S7 target string (`<host>:<port>`)"** is the *provider* operation
   target; B2's delivery target is `channel:<adapter>:delivery:<id>`. Distinct
   concerns, loosely worded — not an inconsistency.

VERDICT: PASS
