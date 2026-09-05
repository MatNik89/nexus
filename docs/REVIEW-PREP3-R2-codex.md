# PREP3 Verification Round 2 — Codex

Scope: branch `slice/p0-prep3`, exact commit `d8604b29b36facc1c41f5ffe85eb22c0c4d0971f`. Review mutations were confined to exact-commit `git archive` exports under `/tmp`. I did not read or modify other reviewers' reports.

## Findings

### [CONCRETE][SEV: HIGH] I4 does not verify that the durable receipt carries the occurrence-derived delivery ID

`internal/acceptance/chaos_linux_test.go:280-300` independently proves that the obligation is `DELIVERED` and that the expected outbox row is destination-bound and `SENT`. It never inspects the `obligation.delivered` event's `receipt_id`. Therefore those two facts can be unrelated even though the test description says "its receipt is the exact occurrence-derived delivery id."

PROBE: In an exact clean export I changed only `cmd/nexus/main.go:1083`, making `MarkDelivered` persist `dlv-forged-review-probe` while leaving the correct occurrence-derived outbox row untouched. `CGO_ENABLED=0 go test -count=1 -run '^TestChaosKillSurvival$' -v ./internal/acceptance` passed: 10 cycles, 10 updates, reminder `DELIVERED` via the expected outbox ID.

FIX: During production replay, locate the single `obligation.delivered` event for `occ-rem-chaos#1` and require `producer == telegram` and `receipt_id == wantDlv`, in addition to the exact outbox assertions.

### [CONCRETE][SEV: HIGH] The required post-remote-accept SIGKILL boundary is still not deterministically covered

HARDQ B2 and T22 require SIGKILL after remote accept (`docs/HARDQ-CONSOLIDATED.md:53-61`; `docs/tasks-P0.md:268-280`). Phase B waits only until a reply is enqueued, then kills (`internal/acceptance/chaos_linux_test.go:174-180`), which is before `Flush` parks the row `UNKNOWN`, calls the remote API, and writes `SENT` (`internal/channel/channel.go:488-525`). Random phase C might hit that interval but records no proof that it did. The cited unit test `TestSentButUnrecordedParksUnknown` injects a returned sent-mark error (`internal/channel/channel_test.go:221-240`); it does not SIGKILL a process after fake-transport acceptance. The unit SIGKILL seams cover journal transaction atomicity, not this cross-boundary wire window.

PROBE: Static end-to-end trace of the phase-B predicate, `Core.Flush`, every `NEXUS_TEST_KILL_MID_BATCH` use, and `TestSentButUnrecordedParksUnknown`.

FIX: Add a fake Bot API barrier that signals after recording `sendMessage` acceptance but blocks the HTTP response or the subsequent sent mark; SIGKILL the real daemon at that barrier, reopen, and require the stable row to remain `UNKNOWN` and absent from automatic retry.

### [CONCRETE][SEV: MED] I3 still permits a terminal message with no matching reply outcome

The tool-less division is acceptable: it avoids the round-1 ASK suspension and honestly limits the conversational workload to ingress/reply durability, while I4 exercises a real scheduler/outbox effect. However, the actual I3 oracle only rejects more than one success row and requires three aggregate `SENT` successes (`internal/acceptance/chaos_linux_test.go:244-270`). It never rejects `okAll == 0`, never counts the documented error reply (`errish` is unused), and therefore does not enforce its comment that every terminal message has exactly one reply row.

PROBE: In an exact clean export, after the calm stop I changed one real `reply-for chaos-msg-*` outbox text to an unrelated value and asserted exactly one row was changed. `NEXUS_CHAOS_CYCLES=10 CGO_ENABLED=0 go test -count=1 -run '^TestChaosKillSurvival$' -v ./internal/acceptance` still passed with 16 terminal updates and only 7 matching `SENT` success replies.

FIX: For every pushed update, derive its durable message outcome and require exactly one outbox row: either the exact success reply or the exact typed handler-error reply, in any state allowed by the delivery-honesty contract. Keep the aggregate phase-B success floor as a separate non-vacuity assertion.

### [CONCRETE][SEV: MED] Socket-path existence is not incarnation readiness

After SIGKILL, the Unix-socket pathname remains. `killableDaemon` starts the next process and accepts `os.Stat(sock) == nil` (`internal/acceptance/chaos_linux_test.go:54-77`), so a stale pathname can satisfy readiness before the new daemon opens its journal, seals capabilities, or listens. Phase A/B later observe database progress, but phase C can kill during startup while claiming ready live traffic.

PROBE: A clean-export test set `providerDelay = 1.5s`, killed one ready daemon, then started a second. `killableDaemon` declared the second ready in 4.8ms from the stale socket, and the probe passed.

FIX: Prove readiness by connecting to the socket and completing a same-incarnation health exchange, or remove the stale path before start and additionally verify the child is live after the successful connection.

### [CONCRETE][SEV: MED] Polling treats transient SQLite contention as an immediate fatal error

`waitStore` is deadline-based, but its predicates call helpers that invoke `t.Fatal` on any query error (`internal/acceptance/chaos_linux_test.go:149-157,317-336`). Thus a transient busy/lock response bypasses the deadline and fails immediately instead of being retried. A clean-export chaos run failed at the phase-B predicate with `database is locked (261)`; the review-only state mutations were located after `stopF()` and had not executed.

PROBE: `NEXUS_CHAOS_CYCLES=10 CGO_ENABLED=0 go test -count=1 -run '^TestChaosKillSurvival$' -v ./internal/acceptance` — FAIL at the phase-B polling closure: `database is locked (261)`.

FIX: Make polling queries return `(value, error)`, retry the explicitly transient SQLite busy/locked class within the deadline, and report the last error on timeout. Non-transient query/schema errors must still fail immediately.

### [NIT][SEV: LOW] The header and dead variable still describe the removed memory-fact oracle

`internal/acceptance/chaos_linux_test.go:13` says every marker's fact exists exactly once, while the implementation is tool-less and checks replies. `errish` at line 252 is assigned and discarded.

FIX: Update I3's header to the reply invariant and delete `errish` plus `_ = errish`.

## Verified folds

- Offset-faithful fake Bot API: verified. `getUpdates` retains IDs at or above the supplied offset, so an unacknowledged fetch is redelivered (`internal/acceptance/acceptance_linux_test.go:209-230`). The full acceptance package passed under this fake.
- I1 production verification: verified for this revision. `verifyJournalProduction` opens with the closed event set and calls production `Replay(0)`; all four committed corruption branches passed by being rejected (`internal/acceptance/chaos_linux_test.go:394-488`).
- I2 exact set: verified. The drain and final oracle compare every pushed ID with the inbox, reject invented/missing IDs, require count one, and require `TERMINAL` (`internal/acceptance/chaos_linux_test.go:193-243,339-370`).
- I4 outbox half: verified. The expected occurrence-derived outbox ID, Telegram adapter, `chat-42`, `SENT`, no alternative final obligation state, and no extra reminder row are enforced. The missing receipt-event correlation is the blocking defect above.
- Drain deadline: logically fail-closed after expiration because final I2/I4 assertions reject incomplete state. Its transient-lock behavior remains flaky as reported above.

## Commands and results

- `git rev-parse --abbrev-ref HEAD` — PASS: `slice/p0-prep3`.
- `git rev-parse HEAD` — PASS: `d8604b29b36facc1c41f5ffe85eb22c0c4d0971f`.
- `git show --stat --oneline --decorate --no-renames HEAD` and source-only `HEAD^..HEAD` diff — PASS: reviewed replacement bound to HEAD.
- `git archive d8604b29b36facc1c41f5ffe85eb22c0c4d0971f | tar -x -C <fresh-temp-dir>` — PASS; repo/export SHA-256 matched for both acceptance source files.
- `CGO_ENABLED=0 go test -count=1 -run 'TestChaosKillSurvival|TestChaosCheckerDetectsCorruption' -v ./internal/acceptance` — PASS: 10 cycles, 10 exact-set updates, 3 reported in-flight kills, 7/10 `SENT` success replies, reminder delivered; corruption checker passed.
- Clean-export stale-socket readiness probe — FALSE GREEN: readiness returned in 4.8ms despite a 1.5s startup probe.
- Clean-export missing-reply probe — FALSE GREEN: one matching reply was removed from the oracle; chaos still passed.
- Clean-export forged durable-receipt production ablation — FALSE GREEN: chaos still passed.
- Clean-export 10-random-cycle run — FLAKE: `database is locked (261)` during phase-B polling.
- `CGO_ENABLED=0 go test -count=1 ./... 2>&1 | tee /tmp/nexus-prep3-r2-codex-suite.log` — PASS, exit 0.
- `rg -n 'FAIL' /tmp/nexus-prep3-r2-codex-suite.log` — PASS: no matches.

## Topknot simplification

`internal/acceptance/chaos_linux_test.go:252-263`: delete the unused `errish` variable and assignment; directly enforce the missing per-message outcome instead.

net: -2 lines possible before adding the required assertion.

VERDICT: FAIL
