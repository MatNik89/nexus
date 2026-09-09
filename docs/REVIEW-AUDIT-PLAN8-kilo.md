# Review of audit-remediation plan v8 (`docs/PLAN-AUDIT-FIXES.md`)

- **Reviewed revision**: `7941979` on `slice/p1-audit`
- **Verdict**: PASS

## Round-7 disposition

- **Note 1 (`PolicyControl` one name, two code lists)** — CLOSED. The v8 call-site
  table binds two closed constants per kind: `PolicyControlRead` for `getMe`
  (`:283`, five read-safe codes `{transport_prewire, http_429, http_5xx,
  transport_postwrite, malformed_reply}`) and `PolicyControlEffect` for
  `setMyCommands` (`:284`, `{transport_prewire, http_429}` only, ambiguous →
  UNKNOWN/E9). `:302-303` lists the two exact sets; Slice D binds them
  (`:492-493`). The lease paragraph now uses `Next(op, build)` (`:189`).

## New findings

None substantive. Both codex round-7 findings are folded and RED-capable:

- **One `PolicyControl` for two contracts (codex r7 #1) — closed.** The
  read/effect split into `PolicyControlRead`/`PolicyControlEffect` removes the
  impossible single-policy ambiguity, and detector D7 (`:531-533`) exercises the
  contrast: `getMe` 5xx/post-write retried exactly once when due, the SAME
  failures on `setMyCommands` land S7 `UNKNOWN` with zero retries (D2).
- **Durable-`Next` nil-builder rule undetected (codex r7 #2) — closed.** Detector
  9g (`:397-400`) begins a durable operation, calls `Next(op, nil)` both while
  grant-eligible and after cap/deadline exhaustion, and requires a fail-closed
  error with no grant, no transition, no journal event; ablating only the
  nil-builder check turns it RED.

## Note (not a FAIL reason)

- Detector D7's table names 5xx and post-write reset for `getMe` but does not
  list `malformed_reply`, which `PolicyControlRead` also admits as retryable
  (`:283`). The malformed-reply behavior is the same read-safe retry path and is
  covered by the closed code list; a one-row addition to D7 would complete the
  coverage.

VERDICT: PASS
