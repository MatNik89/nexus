# PREP3 Review Round 1 — Codex

Scope: branch `slice/p0-prep3`, exact commit `6863248702b11bd35055b2890fc730910645b28b`. The reviewed commit adds only `internal/acceptance/chaos_linux_test.go`. All mutations and added probe code were confined to the exact-commit `git archive` export at `/tmp/nexus-prep3-codex.bUFB3b`; no other reviewer report was read.

## Findings

### [HIGH] Kills are not synchronized with any in-flight durability boundary

`internal/acceptance/chaos_linux_test.go:99-108` queues messages, sleeps for a random 0.5–3.499 seconds, and sends SIGKILL. Queuing into `fakeBot.updates` is not evidence that the daemon fetched or began processing an update. Production polling runs only every two seconds (`internal/channel/telegram/telegram.go:285-299`), so a kill can occur before the first poll or long after an entire batch commits. No barrier proves that any kill lands between fetch/admission/effect/terminal/outbox boundaries. `killableDaemon` also accepts the mere existence of the socket and ignores both kill and wait errors (`internal/acceptance/chaos_linux_test.go:44-54,107-108`), so it does not prove that each target incarnation reached readiness and was alive when killed.

Required repair: expose test-only phase barriers from the fake transport/daemon boundary, wait until a selected update reaches a named pre-commit or post-commit seam, then SIGKILL and assert `ProcessState` reports `SIGKILL`. Cover every B2/B7 boundary deterministically; random scheduling may remain supplemental.

### [HIGH] Missing pushed updates and missing effects can pass

The fake Bot API removes the entire batch as soon as `getUpdates` is called (`internal/acceptance/acceptance_linux_test.go:202-208`), without retaining it until the client's offset advances. That contradicts the recovery premise in `internal/channel/telegram/telegram.go:180-199`: a crash after fetch can permanently lose an update in this harness. I2 checks only rows that happen to exist and merely requires the resulting set to be nonempty (`internal/acceptance/chaos_linux_test.go:129-150`); it never compares inbox update IDs with the complete set pushed. I3 then accepts zero facts for every expected marker because it rejects only `cnt > 1` (`internal/acceptance/chaos_linux_test.go:152-161`).

Clean-export negative control: I added one expected-but-absent marker after the calm run. `NEXUS_CHAOS_CYCLES=1 CGO_ENABLED=0 go test -count=1 -run '^TestChaosKillSurvival$' -v ./internal/acceptance` still passed and logged `3 updates admitted, 4 markers`. Therefore the claimed exactly-once effect coverage is false-green.

Required repair: make the fake API offset-faithful, retain unacknowledged updates, record every pushed update ID, require exact set equality with inbox rows, and require exactly one accepted memory fact for every terminal memory command. Use exact content/ID equality rather than substring `LIKE` matching.

### [HIGH] I1 does not verify the production journal hash contract

The production chain authenticates every journal field and recomputes SHA-256 (`internal/kernel/journal/journal.go:54-86,613-647`). The acceptance checker selects only `(journal_offset, integrity_prev_hash, integrity_hash)` and checks stored-link equality (`internal/acceptance/chaos_linux_test.go:214-243`). It never recomputes a hash, parses the envelope, checks denormalized columns, or validates the terminal hash. Its comment that it re-derives the chain exactly like production is incorrect.

Clean-export negative control: `TestReviewProbeCheckerAcceptsTamperedTailHash` changed the final stored `integrity_hash` to `deadbeef`; `verifyJournalChain` returned success and the probe passed. The committed corruption test mutates the final row's previous hash (`internal/acceptance/chaos_linux_test.go:246-279`), which exercises only the one comparison the weak checker already performs.

Required repair: invoke the production canonical full-record verifier, or share its canonical hash routine with a read-only acceptance verifier. The RED-capability test must separately mutate envelope content, a denormalized field, and the terminal hash.

### [MEDIUM] I4 does not bind the reminder to its stable delivery ID

I4 finds rows by `text LIKE '%chaos reminder%'` and status (`internal/acceptance/chaos_linux_test.go:163-183`). It never asserts the expected occurrence-derived `delivery_id`, adapter, destination identity, or receipt correlation. It also accepts `SCHEDULED` and `DELIVERY_PENDING` after the calm incarnation (`internal/acceptance/chaos_linux_test.go:184-185`), so a reminder that never converges to delivery can pass.

Clean-export negative control: after the calm daemon stopped, I rewrote the sole matching outbox row's delivery ID to `dlv-forged`. In the same run as the absent-marker mutation, the harness still passed with `reminder=DELIVERED`.

Required repair: derive the expected ID from `occ-rem-chaos#1`, query that exact primary key, and assert one row with the exact adapter, destination, text, and `SENT` status plus matching obligation receipt evidence. After the explicit calm drain, require `DELIVERED` (or `ACKED` only if this scenario actually sends an acknowledgement).

### [MEDIUM] The calm-drain condition has a check-before-admission race

The calm loop exits as soon as the current count of `ADMITTED` rows is zero (`internal/acceptance/chaos_linux_test.go:110-119`). Immediately after daemon startup, zero can mean that queued updates have not yet reached the two-second poll, not that all pushed updates are terminal. The fixed two-second sleep is another scheduler race.

A clean-export run whose only substantive invariant mutations occurred after `stopF()` failed before those mutations could affect state: `I2 update 14 stuck ADMITTED after the calm incarnation`. This is direct evidence that the drain predicate can exit too early.

Required repair: wait until every recorded pushed update ID exists exactly once in `TERMINAL`, and until the expected reminder reaches its required final state; fail on the deadline. Remove fixed sleeps from correctness decisions.

## Checks and probes

- `git rev-parse --abbrev-ref HEAD` — PASS: `slice/p0-prep3`.
- `git rev-parse HEAD` — PASS: `6863248702b11bd35055b2890fc730910645b28b`.
- `git show --stat --oneline --decorate --no-renames HEAD` plus the full `HEAD^..HEAD` diff — PASS: one 280-line added file, as scoped.
- `git archive 6863248702b11bd35055b2890fc730910645b28b | tar -x -C /tmp/nexus-prep3-codex.bUFB3b` — PASS; source and export hashes of `chaos_linux_test.go` both `5cc22db76b598dd8b4c1f50624e3a9298c767cbf1e556b5b164d306574552919`.
- `CGO_ENABLED=0 go test -count=1 -run 'TestChaosKillSurvival|TestChaosCheckerDetectsCorruption' -v ./internal/acceptance` — PASS: 8 cycles, 17 admitted updates, 17 markers, reminder `DELIVERED`; both tests passed.
- Clean-export tail-hash mutation probe — FALSE GREEN: the weak I1 checker accepted a forged terminal hash.
- Clean-export missing-marker plus forged-delivery-ID probe — FALSE GREEN: test passed with 3 admitted updates, 4 expected markers, forged delivery ID, and reminder `DELIVERED`.
- Clean-export calm-drain timing probe — RED: `I2 update 14 stuck ADMITTED`; the post-stop invariant mutations had not yet participated.
- `CGO_ENABLED=0 go test -count=1 ./... 2>&1 | tee /tmp/nexus-prep3-codex-suite.log` — PASS, exit 0.
- `rg -n 'FAIL' /tmp/nexus-prep3-codex-suite.log` — PASS: no matches.

## Assessment notes

- The suggested `MIN(status)` concern is not independently exploitable in the current query: when `COUNT(*) == 1`, `MIN(status)` is that row's status; when duplicates exist, the count check fails first. The larger I2 defect is omission of the expected update set.
- The final direct SQL queries exercise the SQLite file, so `sql.Open` laziness does not make the final reopen check vacuous. The harness still does not affirmatively prove that every killed incarnation reached a successful reopen/readiness boundary.
- Topknot simplification: the custom linkage-only checker should be deleted in favor of one canonical full-record verification path; maintaining a second, weaker definition caused the I1 false green.

VERDICT: FAIL
