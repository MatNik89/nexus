# Review of audit-remediation plan v6 (`docs/PLAN-AUDIT-FIXES.md`)

- **Reviewed revision**: `64084cd` on `slice/p1-audit`
- **Verdict**: PASS

## Round-5 disposition

- **Note 1 (opaque-key binding residual)** — CLOSED. `:176-178` now states, by
  construction in the single `Flush` loop, that `Key == payload.operation_id ==
  op`; the split between S7's opaque-key check and the channel's `operation_id ==
  delivery:<delivery_id>` check is documented as one invariant.
- **Note 2 (S7 dual narrative)** — STILL OPEN as a note; a tying comment remains
  advisable.

## New findings

None substantive. All three codex round-5 findings are folded and verified:

- **Exhaustion/deadline in two appends (codex r5 #1) — closed.** `Next(op, build
  func(Landing) Companion)` (`:147-153`) now lands exhaustion/deadline through the
  SAME paired recipe: S7 computes `Landing{Terminal}`, the owner's builder returns
  the `FAILED` companion, and both commit in one `journal.AppendBatch`; for a
  non-durable operation `build` is nil. Detector 5 adds the seam ablation
  (`:347-350`).
- **`PolicyStructured` over-counting (codex r5 #2) — closed.** `ExtractVia`
  (`:317-327`) makes the initial generation and the re-ask ONE S7 operation:
  attempt 1 = `generate(g, false)` + validate, attempt 2 = `generate(g, true)`
  (re-ask); `PolicyStructured{MaxAttempts 2}` = initial + exactly one re-ask; the
  salvage ladder runs after the operation is terminal, outside the transport
  count; SECURITY/EFFECT propose terminal on the first invalid result. Detector
  9f a-d (`:378-382`) is RED-capable (c: force MaxAttempts 1 → exactly one total
  transport call).
- **Poll retry code mismatch (codex r5 #3) — closed.** Classification is
  kind/effect-aware with one closed vocabulary (`:276-285`): `transport_prewire`,
  `http_429`, `http_4xx`, `http_5xx`, `transport_postwrite`, `malformed_reply`,
  `local_refused`. Effectful delivery keeps 5xx/post-write/malformed →
  `ErrAmbiguousSend` → UNKNOWN (E9); read-only/idempotent kinds (getUpdates,
  getMe, setMyCommands) propose the retryable codes; `PolicyPoll` lists exactly
  `{transport_prewire, http_429, http_5xx, transport_postwrite, malformed_reply}`
  (`:272`).

## Notes (not FAIL reasons)

1. **B2 delivery wiring is stale against the v6 `Next` signature.** `:243-245`
   still reads `g, err := s7.Next(op)` … `ErrExhausted`/terminal → mark `FAILED`,
   which is the v5 two-append behavior that v6 B1 (`:147-153`) just removed.
   Under `Next(op, build)` the exhaustion is already committed with its `FAILED`
   companion, so `ErrExhausted` means "already landed" and no separate mark
   exists. Detector 5's seam ablation forces the correct behavior, but the B2
   prose should be updated to `Next(op, build)` + "ErrExhausted → nothing further
   to do" so an implementer cannot re-read the old two-append description.
2. **S7 dual narrative** (in-memory `machine.AttemptTable` `attempt.*` + durable
   `s7.*` projection) still lacks a tying comment; rehydration rebuilds the former
   from the latter, so it is consistent, just undocumented.

VERDICT: PASS
