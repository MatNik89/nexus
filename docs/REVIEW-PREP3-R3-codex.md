# PREP3 Verification Round 3 — Codex

Scope: `slice/p0-prep3` at `6fd51c477c34e90e49e46d3c3ecae99f3f5ff396`. I reviewed the committed round-2 fold, ran the untouched harness and suite in the real repository, and ran mutations only in clean `git archive` exports. I did not inspect other reviewers' reports.

## Findings

### [CONCRETE][SEV: MED] Phase D still false-greens on duplicate owner-redelivery sends

The fake API correctly records a `sendMessage` before blocking its response (`internal/acceptance/acceptance_linux_test.go:238-264`), and phase D correctly proves that the killed daemon leaves the row `UNKNOWN` (`internal/acceptance/chaos_linux_test.go:252-257`). The subsequent oracle does not prove the stronger claims in its own comment, however. It starts the calm daemon and immediately injects owner `redeliver` commands for every `UNKNOWN` row (`internal/acceptance/chaos_linux_test.go:258-289`), so there is no observation interval in which an automatic resend would be detected independently. It then accepts any count satisfying `acceptedAfter >= acceptedBefore + 1` (`internal/acceptance/chaos_linux_test.go:301-307`), although "only ... the second arrival" requires equality.

In a clean export, I mutated the production Telegram flush path to call `sendMessage` twice only for the exact phase-D reply (`reply-for chaos-msg-011`). `TestChaosKillSurvival` still passed. Thus one explicit owner action can produce two additional remote acceptances and the checker remains green. This is a demonstrated false-green at the principal B2 boundary, not merely missing test polish.

Required repair: after restart, give the real daemon a bounded, observable flush opportunity before submitting an owner command and require the phase-D acceptance count to remain exactly unchanged while the row remains `UNKNOWN`. Then submit exactly one owner command and require `acceptedAfter == acceptedBefore + 1` (and retain the final `SENT` assertion).

### [CONCRETE][SEV: LOW] The busy/locked tolerance was not applied to the phase-B baseline query

`pollQuery` now tolerates transient SQLite busy/locked errors within its caller's deadline (`internal/acceptance/chaos_linux_test.go:452-469`). Phase B first calls `countSentReplies` while the daemon is live (`internal/acceptance/chaos_linux_test.go:188`), and that helper still turns any query error directly into `t.Fatal` (`internal/acceptance/chaos_linux_test.go:474-482`). The three requested repeats did not reproduce a lock, but this sibling access retains the same timing-dependent failure class. Use the deadline-owned query path for the baseline as well, or otherwise apply a bounded busy policy consistently.

## Fold verification

- I4 now obtains `obligation.delivered` through production `journal.Open` plus `Replay`, requires exactly one matching occurrence, and verifies both `producer="telegram"` and the occurrence-derived receipt ID (`internal/acceptance/chaos_linux_test.go:408-438,647-708`). A clean-export mutation that forged the emitted receipt ID made the test fail with `I4 receipt event diverges`; this fold is red-capable.
- Phase D reaches the remote-accepted/unrecorded window and checks the durable `UNKNOWN` state. Its no-automatic-resend/exact-second-arrival oracle remains insufficient as described above.
- The exact-set terminal check, per-message success/error accounting, success-row `SENT` check, and redeliver-command accounting are present. Tool-less conversation effects are an acceptable explicit scope division here because the scheduler/outbox effect boundary is exercised by I4 and the production-specific SIGKILL matrices remain separate; the report must not imply that this harness alone covers arbitrary tool execution.
- Readiness now removes the stale path and requires a live Unix-socket dial. Polling queries retry transient busy/locked results, subject to the residual direct-query finding above.
- The drain waits for all outbox rows to become `SENT`, exact-set terminal inbox state, and the reminder to become `DELIVERED`.

## Commands and evidence

- `git branch --show-current && git rev-parse HEAD && git show --stat --oneline --decorate HEAD` — PASS; bound to `slice/p0-prep3` / `6fd51c477c34e90e49e46d3c3ecae99f3f5ff396`.
- `CGO_ENABLED=0 go test -count=3 -run '^TestChaosKillSurvival$' -v ./internal/acceptance 2>&1 | tee /tmp/nexus-prep3-r3-codex-chaos3.log` plus a literal `FAIL` scan — PASS; all three executions passed (31.80 s, 33.43 s, 32.64 s), with 3-4 asserted in-flight kills per execution and no `FAIL` token.
- `CGO_ENABLED=0 go test -count=1 ./... 2>&1 | tee /tmp/nexus-prep3-r3-codex-suite.log` plus `rg -n 'FAIL' /tmp/nexus-prep3-r3-codex-suite.log` — PASS; exit 0 and no `FAIL` token anywhere in the captured suite output.
- Clean archive, forged I4 receipt: changed the production delivery event to persist `ReceiptID: "dlv-forged-review-probe"`, then ran `CGO_ENABLED=0 go test -count=1 -run '^TestChaosKillSurvival$' -v ./internal/acceptance` — EXPECTED FAIL; `I4 receipt event diverges: producer="telegram" receipt="dlv-forged-review-probe" ...`.
- Clean archive, targeted duplicate-send mutation: changed the production Telegram flush callback to invoke `sendMessage` twice only when the text was the deterministic phase-D marker `reply-for chaos-msg-011`, then ran `CGO_ENABLED=0 go test -count=1 -run '^TestChaosKillSurvival$' -v ./internal/acceptance` — UNEXPECTED PASS; the test completed green after 11 cycles, proving the phase-D oracle false-green.

## Simplification note

`verifyJournalProduction` and `deliveredReceipt` duplicate construction of the full production event registry. A single test helper that opens the production-configured journal would remove duplicated proof machinery without weakening any invariant.

## Weakest link and proof ceiling

The weakest link is phase D's inference from a lower-bound final count to “no automatic resend and exactly one owner-driven resend.” Current green runs prove survival and eventual drain on this host, but they do not exclude extra remote effects at the accepted/unrecorded recovery boundary. Three repeats also do not eliminate the direct-query lock race.

VERDICT: FAIL
