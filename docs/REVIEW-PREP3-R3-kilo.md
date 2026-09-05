# PREP3 verification round 3 — Kilo (chaos harness re-verification)

Target: `slice/p0-prep3` at HEAD `6fd51c4` (fold of my round-2 findings). Method: code-read, run the harness 5x + the suite in the repo. Working tree never modified.

## Suite / harness runs

`CGO_ENABLED=0 go vet ./...` exit 0; `CGO_ENABLED=0 go test -count=1 ./...` all 33 packages `ok` (acceptance 354s). `TestChaosKillSurvival` green **5/5** (31-34s each; 3-7 in-flight kills; 7-8/11 replies succeeded; reminder DELIVERED via the occurrence-derived id; the phase-D stray-barrier filter loop fired twice and recovered cleanly).

## r2 findings — fixed

| r2 finding | Status |
|---|---|
| [MED] I4 flake — reminder stranded DELIVERY_PENDING after a kill in the send→SENT window | **FIXED** — the calm drain now plays the OWNER and drives one deduplicated `redeliver <dlv-id>` per UNKNOWN row (`chaos_linux_test.go:269-289`), then waits for all-SENT + exact-set TERMINAL + DELIVERED. 5/5 green confirms the flake is gone. |
| [LOW] drain omitted reply-SENT | **FIXED** — the drain condition now includes `allSent == 0` (no non-SENT outbox row, `:291-294`). |
| [LOW] tool-less scoping boundary | Accepted division of labor; header documents it; the boundary (random kill inside a tool-bearing turn's grant-consume→effect-commit / effect-commit→turn.succeeded) remains covered only by the deterministic B7 seams and the at-least-once ceiling — a real, documented, accepted boundary. |

## codex fold (verified)

- **#1 receipt-event binding**: `deliveredReceipt` (`:647-708`) extracts the durable `obligation.delivered` event via production `journal.Open` + `Replay(0)` and requires `producer == "telegram"` AND `receipt_id == chaosDeliveryIDFor("occ-rem-chaos#1")`, with `found == 1` (no double delivery). The obligation state and the outbox row can no longer merely coexist.
- **#2 phase-D barrier**: `armPostAcceptBarrier` records `sendMessage` acceptance, signals, and blocks the response; the daemon is SIGKILLed inside the accepted-but-unrecorded window; the row is asserted UNKNOWN; the drain's owner-redeliver is the only second arrival (`acceptedAfter >= acceptedBefore+1`, `:304-306`).
- **#3 per-update outcome**: `succeeded + errReplies == markers`, an existing success reply must be SENT (`:362-368`), `requeued == redeliverCmds` (deduplicated commands, `:384-394`).
- **#4 readiness**: `killableDaemon` removes the stale socket and `net.Dial`s a live listener (`:69-81`).
- **#5 poll tolerance**: `pollQuery` maps transient SQLite `locked`/`busy` to no-progress (`:455-469`).

## NEW-defect hunt

None material. Two notes (not defects): phase D does not add an explicit "the daemon did not auto-resend" counter (it asserts the row is UNKNOWN and that the redeliver is the second arrival; the no-auto-retry of UNKNOWN is separately pinned by the channel unit tests `TestDeliveryHonesty`/`TestSentButUnrecordedParksUnknown`); and the `acceptedAfter >= acceptedBefore+1` check is a floor, not an exact count (a hypothetical third send would not trip it, but the per-row `okAll > 1`/`requeued == redeliverCmds` accounting closes the duplicate path).

## Verdict

My round-2 findings are genuinely fixed (the DELIVERY_PENDING flake is gone — 5/5 green, the drain now drives the owner reconcile, and it waits for SENT-ness), the codex fold is real and causal, and no NEW defect was introduced.

VERDICT: PASS
