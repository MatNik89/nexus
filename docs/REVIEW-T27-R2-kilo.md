# T27 verification round 2 — Kilo (final-gate re-verification)

Target: `slice/p0-phase7` at HEAD `5fedd4e` (folds `7c68cda` codex #1-6 + `5fedd4e` causal detectors). Verifies each of my round-1 findings is fixed. Method: code-read, run the suite in the repo (machine-read), run the causal detectors in a clean `git archive HEAD` export. Working tree never modified.

## Suite

`CGO_ENABLED=0 go vet ./...` exit 0. `CGO_ENABLED=0 go test -count=1 ./...` — note: the **first** run transiently failed (`cmd/nexus [build failed]` + `internal/acceptance` all-fail + `internal/buildcheck` `TestStaticCheckPassesStaticNexusBinary`), all three being the packages that `go build` the nexus binary in a subprocess during a parallel `go test ./...`; a clean re-run is fully green (33 packages `ok`, acceptance 55s). This is a cold-cache parallel-build contention, see Finding N1.

## Round-1 findings — all fixed

| # | Round-1 finding | Fix | Evidence |
|---|---|---|---|
| 1 | [MED] `doctor --p0` "reminders" substrate check let P0-capable be earned with a dead delivery loop | codex #3: the grant now additionally requires the out-of-implementation acceptance attestation, sha256-bound to the running binary (`verifyAcceptanceAttestation`, `main.go:835-868`, reading `system/acceptance.json` written by `scripts/p0-accept.sh`). | `TestDoctorP0GrantLive` asserts no-attestation → no grant, digest-mismatch → no grant, token-removal → no grant. |
| 2 | [LOW] `TestCriterion3SensitivityNoChannel` vacuous (`w.bot==nil` only) | codex #6: replaced with a causal black-box — a channel-less incarnation runs, then a LATE-wired channel delivers the still-PENDING occurrence (`acceptance_linux_test.go:307-345`); a false receipt in the channel-less incarnation would have left nothing to deliver. | The late delivery asserts `occ-rem-silent#1`, proving the occurrence stayed durably PENDING. |
| 3 | [LOW] reminder "delivery receipt" was the outbox ENQUEUE id | codex #2: `deliverPendingReminders` derives a stable id (`deliveryIDFor(occ)`, `main.go:871-874`), enqueues idempotently (`channel.EnqueueReplyID` PK-stable, `channel.go:389-420`), and mints the receipt **only** when `DeliveryStatus == "SENT"` (`main.go:851-868`). | `TestReminderReceiptOnlyFromSent`: dead wire → stays DELIVERY_PENDING with exactly one row; SENT mints. |
| 4 | [LOW] `ownerChat` map-iteration non-determinism | kilo #4: picks the LOWEST bound chat id (`main.go:185-197`). | Deterministic by construction. |
| 5 | [LOW] `telegram_api_base` unvalidated (token rides the URL path) | codex #4: `ValidateBounds` rejects anything but the production endpoint or an explicit http(s) loopback (`config.go:315-325`). | `TestTelegramAPIBaseLoopbackOnly`. |

The seal is now also **enforced** before consumers start (`main.go:240-273`): conversation OFF → `return 1` (no serve); the telegram adapter's `Run` is gated on `snap.On("telegram")` (`TestSealedOffCapabilityNeverServes`, provider fails only the startup probe then recovers — daemon still refuses).

## NEW finding

### N1. [LOW] `go test ./...` is transiently flaky on a cold build cache

The acceptance harness's `TestMain` (`acceptance_linux_test.go:43-69`) and `internal/buildcheck` both `go build` the `nexus` binary in subprocesses while `go test ./...` concurrently builds `cmd/nexus` for its own test binary. On a cold cache this contended: the first full run reported `FAIL cmd/nexus [build failed]`, all acceptance tests failed at 0.00s (TestMain build failure), and `internal/buildcheck`'s static check failed; every affected package passes in isolation and a re-run of the full suite is green. No deterministic failing probe (transient, environment/cache-dependent), and not a production-code defect — but it will intermittently redden `go test ./...` in CI on a fresh checkout.

## Re-run of negative probes

- off-telegram dispatch → covered by `TestReminderReceiptOnlyFromSent` (no SENT → no receipt, one stable row) and the causal no-channel sensitivity (late delivery proves no false receipt).
- 500-wire / fabricated receipt → the receipt is gated on the durable `SENT` transition only (`channel.DeliveryStatus`), never on enqueue or UNKNOWN.
- disabled-consumer grant → gated by the sha256-bound acceptance attestation (`TestDoctorP0GrantLive`).
- token-to-arbitrary-host → rejected by `telegram_api_base` loopback-only validation.
- shared-TMPDIR replacement → `TestMain` now owns a private `os.MkdirTemp("", "nexus-acceptance-*")` dir (codex #5), not a fixed basename under the shared TMPDIR.

## Verdict

All five round-1 findings are genuinely fixed (each backed by a causal detector), the seal is enforced rather than logged, and the only new observation is a transient cold-cache test flake (LOW, not a production defect).

VERDICT: PASS
