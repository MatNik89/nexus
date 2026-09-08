# Review of audit-remediation plan v9 (`docs/PLAN-AUDIT-FIXES.md`)

- **Reviewed revision**: `b8e1ab7` on `slice/p1-audit`
- **Verdict**: PASS

## Round-8 disposition

- **Round-8 note (D7 omits `malformed_reply` for `getMe`)** — STILL OPEN as a minor
  coverage note; the v9 changes do not touch D7's enumerated cases. Not a verdict
  driver.

## New findings

None substantive. The codex round-8 finding and note are folded and verified:

- **Ambiguous `setMyCommands` blind re-register on restart (codex r8 #1) — closed.**
  Command registration is now a DURABLE S7 operation with a stable identity
  `control:tg:setMyCommands:<sha256(sorted command list)>` under
  `PolicyControlEffect{Durable:true}`, with the owner companion
  `channel.control_effect{operation_id, method, payload_hash, state}` (`:301-304`).
  On daemon start a rehydrated `UNKNOWN` registration performs NO `setMyCommands`;
  instead an S7-governed read-only `getMyCommands` reconciliation (`:286`, added to
  the call-site table) compares remote vs desired: equal → reconciled SUCCEEDED
  (`attempt.reconciled_ok`); different → a new effect operation. A rehydrated
  SUCCEEDED for the same hash makes no call; a changed desired set is a new identity;
  health is `degraded` until resolved (`:305-314`). Detector D2b (`:535-540`) proves
  the restart path with its own ablation (blind re-register → RED).
- **`PolicyUI` E9 wording (codex r8 note) — closed.** `:288` now states UI methods
  are `MaxAttempts 1, EffectReversible, in-memory; DEFINITE failures terminal,
  5xx/post-write/malformed -> UNKNOWN per E9`.

## Note (not a FAIL reason)

1. **"the same identity reconciled FAILED and re-begun" is contradictory wording.**
   `:311-312` says a different remote menu with an unchanged desired set is
   "the same identity reconciled FAILED and re-begun". But `attempt.reconciled_failed`
   is UNKNOWN → FAILED (terminal), and `Begin` is idempotent on an existing record,
   so re-beginning the *same* hash-derived identity is a no-op. The D2b detector's
   "exactly ONE new setMyCommands under a fresh grant" forces the correct behavior
   (a fresh identity, like the `delivery:<id>:r<attempts>` redelivery pattern), so the
   design is correct; the wording should say the re-apply is a NEW operation with a
   fresh identity (e.g. `control:tg:setMyCommands:<H>:<counter>`), not "the same
   identity".

VERDICT: PASS
