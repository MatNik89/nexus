# PREP3 Verification Round 3 — agy

Target: branch `slice/p0-prep3` at HEAD `4b42ff3` (folds of round 2 through round 4 reviews).
Method: Adversarial verification of all fold claims against sources and contracts, executed in clean `git archive HEAD` isolated export directories (`/tmp/nexus-prep3-r3-probe`) with negative ablation probes, 3x consecutive execution of `TestChaosKillSurvival`, and full repository test suite machine verification (`CGO_ENABLED=0 go test -p 1 -count=1 ./...`). The repository working tree was not modified during testing.

---

## 1. Verified Fold Claims

| Fold Claim | Implementation Details | Verification & Proof |
|---|---|---|
| **(codex#1) I4 Durable Receipt Verification via Production Replay** | `internal/acceptance/chaos_linux_test.go:474-480, 697-755`: `deliveredReceipt` uses `journal.Open` + `Replay(0)` over the verified hash-chain to inspect the `obligation.delivered` event payload, requiring `producer == "telegram"` and `receipt == wantDlv` (`dlv-2d1ba3e07a3e5e5d204efe8c`). | **PASS (RED-Capable)**: In clean-export negative probe mutating `cmd/nexus/main.go:1083` to persist `ReceiptID: "dlv-forged-test"`, `TestChaosKillSurvival` failed immediately at `:479` with `I4 receipt event diverges: producer="telegram" receipt="dlv-forged-test" want telegram/dlv-2d1ba3e07a3e5e5d204efe8c`. |
| **(codex#2) Phase D: Post-Accept Remote Kill Seam & E9 UNKNOWN Honesty** | `internal/acceptance/chaos_linux_test.go:224-349` & `acceptance_linux_test.go:201-279`: Quiesces outbox (`PENDING==0 && ADMITTED==0`), arms `bot.armPostAcceptBarrier()`, sends message, signals upon acceptance and blocks HTTP return, then SIGKILLs daemon. Asserts row is parked `UNKNOWN`. Restarts daemon, verifies 5s observation interval with NO automatic resend (`countSent` unchanged), then drives human `redeliver <dID>` command. | **PASS (RED-Capable)**: Verified that auto-resend without human command is rejected. In clean-export ablation mutating `Core.Pending` to auto-fetch `UNKNOWN` rows, the quiesce and observation assertions turned RED (`phase D: outbox/turns never quiesced`). |
| **(codex#3) Per-Update Outcome Accounting & SENT Assurance** | `internal/acceptance/chaos_linux_test.go:415-440`: Every terminal message is strictly accounted for: `succeeded + errReplies == markers`, `requeued == redeliverCmds`, and every existing success reply is verified to be `SENT` (`okAll == 1 && ok != 1` fails). | **PASS (RED-Capable)**: In clean-export ablation mutating a success reply to an unrelated string, `TestChaosKillSurvival` turned RED at `:438` with `I3 outcome accounting broken: 6 success + 4 error != 11 chaos messages`. |
| **(codex#4) Connect-Readiness on Pre-Removed Socket** | `internal/acceptance/chaos_linux_test.go:57-88`: `killableDaemon` calls `os.Remove(daemon.sock)` before spawning the child process and polls `net.Dial("unix", sock)` up to 400 iterations (10s), ensuring stale pathnames from previous SIGKILLs cannot trigger false readiness. | **PASS**: Eliminates cycle 2+ startup false-positives; verifies live listener before injecting traffic. |
| **(codex#5) `pollQuery` Transient Contention Handling** | `internal/acceptance/chaos_linux_test.go:497-516`: Returns `(0, errTransientBusy)` on SQLite busy/locked errors, allowing `waitStore` loops to retry within the deadline rather than calling `t.Fatal` prematurely. Bounded baseline sampling in Phase B handles transient busy without fake zero samples. | **PASS**: Transient lock contention does not crash test polling; non-transient errors surface cleanly. |
| **(kilo#1) Drain Loop Owner-Driven Redeliver & Flake Resolution** | `internal/acceptance/chaos_linux_test.go:306-340`: The calm drain loop discovers any UNKNOWN outbox rows and drives one explicit `redeliver <id>` owner command per row, then waits until `allSent == 0 && inboxExactlyTerminal && oblStatus == "DELIVERED"`. | **PASS**: Executed `TestChaosKillSurvival` across 3 consecutive runs (35.54s, 34.71s, 32.70s); the previously reproduced `DELIVERY_PENDING` timeout flake is completely eliminated. |
| **(kilo#3/NITs) Header & Outbox SENT Condition** | `internal/acceptance/chaos_linux_test.go:13-14, 333-338`: Test header comments updated to reflect one outcome per message; drain requires `SELECT COUNT(*) FROM chan_outbox WHERE status NOT IN ('SENT') == 0`. | **PASS**: Verified aligned and consistent. |

---

## 2. Top 3 Weakest Points

1. **SQLite Driver Error Matching for Busy Detection (`chaos_linux_test.go:505`)**:
   `pollQuery` matches `"locked"` or `"busy"` substrings in `err.Error()`. While robust for the current modernc.org/sqlite driver, driver changes or wrapped error types could alter string formatting.
2. **Phase D Stray Barrier Filtering (`chaos_linux_test.go:255-264`)**:
   Phase D filters stray late outbox deliveries by releasing and re-arming the barrier up to 10 times. While the pre-quiesce guard (`p==0 && a==0`) minimizes strays, high background load could theoretically burn multiple retries.
3. **Observation Window Fixed Duration (`chaos_linux_test.go:283`)**:
   The 5-second observation interval post-restart is a fixed sleep to ensure the restarted daemon's 2-second ticker runs without auto-sending. While adequate for testing, it relies on timing rather than an explicit event hook.

---

## 3. Test Suite Execution

- `CGO_ENABLED=0 go test -p 1 -count=1 ./...`: **PASS** (all packages green, 0 failures, 0 panics).
- `CGO_ENABLED=0 go test -v -count=3 -run TestChaos ./internal/acceptance`: **PASS** (3/3 iterations passed cleanly, `TestChaosCheckerDetectsCorruption` passed 4/4 branches).
- Negative ablation probes in isolated export: **ALL VERIFIED RED-CAPABLE**.

---

VERDICT: PASS
