# PREP3 Verification Round 5 — Codex

Scope: `slice/p0-prep3` at `4b42ff37f74d4d6d5dcf9d7657bdf04cd3e9e2de`. I reviewed `git show HEAD`, swept every `pollQuery` caller, ran the untouched chaos harness in the real repository, and ran the transient-contention probe only in a clean `git archive` export. I did not inspect another reviewer's report.

## Result

No concrete defect was found in the tiny fold.

- `pollQuery` returns the distinct `errTransientBusy` value for SQLite busy/locked errors and never returns a successful fake zero (`internal/acceptance/chaos_linux_test.go:497-516`).
- Phase-B baseline acquisition is bounded to 50 attempts, retries only `errTransientBusy`, stops on any non-transient error, and accepts a sample only when `berr == nil` (`internal/acceptance/chaos_linux_test.go:188-203`). The later wait predicate also requires `err == nil`, so the sentinel cannot satisfy progress (`internal/acceptance/chaos_linux_test.go:204-210`).
- The same-class caller sweep found no caller that interprets the sentinel's accompanying zero as data. Predicate callers require `err == nil`; direct invariant checks reject non-nil errors.

## Commands and evidence

- `git branch --show-current; git rev-parse HEAD; git status --short --branch; git show HEAD` — PASS; bound to `slice/p0-prep3` / `4b42ff37f74d4d6d5dcf9d7657bdf04cd3e9e2de`, with no pre-existing worktree change affecting the reviewed code.
- `CGO_ENABLED=0 go test -count=3 -run '^TestChaosKillSurvival$' -v ./internal/acceptance 2>&1 | tee /tmp/nexus-prep3-r5-codex-chaos3.log` followed by a literal `FAIL` scan — PASS; all three executions passed in 33.69 s, 35.65 s, and 37.06 s, with no `FAIL` token.
- `git archive 4b42ff37f74d4d6d5dcf9d7657bdf04cd3e9e2de | tar -x -C <fresh-temp-dir>` plus source-hash comparison — PASS; exported `chaos_linux_test.go` matched HEAD byte-for-byte before mutation.
- Clean-export sentinel probe: forced the first phase-B baseline read in each of the three cycles to return `(0, errTransientBusy)`, asserted exactly three injections, then ran `CGO_ENABLED=0 go test -count=1 -run '^TestChaosKillSurvival$' -v ./internal/acceptance` — PASS; exit 0, no `FAIL`, and the bounded loop retried all three sentinels rather than accepting zero.

## Weakest points and proof ceiling

1. Busy classification still relies on substrings in driver error text; that is pre-existing and no counterexample was observed for the deployed SQLite driver.
2. The deterministic clean-export probe injects the exact sentinel at the helper boundary; it proves caller semantics, not the driver's ability to produce every platform-specific busy spelling.
3. Three chaos executions are a bounded sample of randomized phase C and do not exhaust all schedules.

The fold is minimal, introduces no dependency or new abstraction beyond one sentinel, and closes the reported false-baseline class by construction.

VERDICT: PASS
