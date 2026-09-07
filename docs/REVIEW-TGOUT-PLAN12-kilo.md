# REVIEW-TGOUT-PLAN12-kilo — design review of PLAN-TGOUT.md v12 (commit aafd411)

**Scope:** `docs/PLAN-TGOUT.md` at `aafd411` on `slice/p0-tgout` (HEAD verified; file
contains `failedTurnOutcome`). Read-only DESIGN review — no code. Cross-checked against
the round-11 findings and the current `daemon.go` recovery path.

## What v12 correctly resolves

- **r11 F1(b) (SafeMessage imprecision) — closed.** `turn.failed` durably carries TWO
  fields — `error_code` AND `tool` (the precedence winner); the promise is narrowed to
  "same validated error code + tool -> same edge response", with no field-for-field
  `TypedError` equality claimed and `Category`/`Origin`/`Retryability` stated as
  reconstructed constants for this code (`:98-104`). The persisted-tool ablation is
  specified (`:111-112`).
- **r11 F1(a) (return-contract conflation) — closed.** A NEW helper
  `failedTurnOutcome(turn) (code, tool, ok)` — deliberately NOT the string-typed
  `completedTurnFinal` — is consulted on turn-collision after succeeded/suspended, and
  `RunChannelTurn` returns the reconstructed typed error so the edge maps it exactly like
  the live path (`:104-108`).

The reconstruction is sound: `failedTurnOutcome` returns `(code, tool, ok)`, and
`RunChannelTurn` rebuilds the `TypedError` through the validating constructor
(`NewTypedError(code, ErrCatValidation, RetryNever, safeMessage(code, tool), "planner", …)`);
"zero planner calls" holds structurally because the deterministic turn id collides on the
already-appended `turn.created`/`turn.started` events before `iterate` reaches
`planner.Plan`.

## Note (not a finding)

The persisted-`tool` ablation is worded "drop the persisted tool field -> the tool name
vanishes from the message", but the tool name never appears in the Croatian edge message
(it is "Preformuliraj zahtjev.", static, `:116-117`) — the tool name lives in the journaled
`code+tool` payload and the reconstructed fallback `SafeMessage`. The ablation is observed
by the detector's "journaled code+tool" assertion (dropping the field fails it → RED), and
the `SafeMessage` reconstruction still yields a non-empty (generic) message when `tool` is
absent, so the mechanism is correct; only the ablation's wording is slightly loose.

## Confidence

Both round-11 findings are functionally closed and the mechanism is now explicit and
sound. The single remaining item is a wording nit in the ablation description, not a
design flaw.

VERDICT: PASS
