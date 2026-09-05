# PREP4 verification round 5 — Codex

Reviewed branch `slice/p0-prep4` at immutable HEAD `d66a2b0c4bed93520ace723b7119c4628d8de9bb`. The real working tree was not modified during review or test execution. Each probe and the baseline ran in a separate fresh `git archive` export; every exported `internal/acceptance/soak_linux_test.go` initially matched HEAD at SHA-256 `a34b28a1e74f26d67d1d01c6323ab326af1994dbb84711170e5bb2e291bf809f`.

## Findings

No concrete defect remains in the round-4 cadence detector.

### [NIT][SEV: LOW] The fold description says missed-deadline catch-up was removed, but the loop can still catch up

When `next` is already in the past, the producer pushes immediately and advances `next` from its previous scheduled value on the next iteration (`internal/acceptance/soak_linux_test.go:273-298`). A long lock stall therefore still produces immediate catch-up arrivals. This is not a false-green now: the minimum-gap oracle rejects that burst, as the 12-second probe demonstrated. The implementation comment accurately says the spacing oracle will judge the late push, but the commit/fold wording should say catch-up is detected rather than removed.

## Fold verification

- **Two-sided count — PASS.** The oracle rejects both `offered < expect-1` and `offered > expect+1` (`internal/acceptance/soak_linux_test.go:368-379`). The one-second producer mutation failed with `offered 60 arrivals, schedule implies 30±1 (rate broken)`.
- **Producer-owned timestamps and bounded spacing — PASS.** Each accepted arrival records a timestamp under producer ownership (`internal/acceptance/soak_linux_test.go:289-297`); the oracle rejects gaps above three seconds and below 1.5 seconds (`internal/acceptance/soak_linux_test.go:380-390`). The exact round-4 lock-held 12-second mutation failed with `producer bursting — inter-arrival gap 15.501µs ... (<1.5s)`.
- **Normal workload — PASS.** An unmodified three-minute clean-export soak completed with 90 messages, nine reminders, and maximum backlog two.

## Commands and machine-read results

- `git show HEAD` — PASS; reviewed `d66a2b0` and its `internal/acceptance/soak_linux_test.go` diff.
- Three times, one per isolated copy: `git archive d66a2b0c4bed93520ace723b7119c4628d8de9bb | tar -x -C <new-temp-dir>` — PASS; all source hashes matched HEAD before mutation.
- Lock-held 12-second snapshot probe: `NEXUS_SOAK_MINUTES=1 CGO_ENABLED=0 /home/matej/.local/go/bin/go test -count=1 -run '^TestSoakSurvival$' -v ./internal/acceptance` — expected FAIL in 60.26s; literal `FAIL` scan found the intended cadence failure.
- One-second over-fast producer probe: the same one-minute command — expected FAIL in 60.29s; literal `FAIL` scan found the intended two-sided count failure.
- Unmodified clean export: `NEXUS_SOAK_MINUTES=3 CGO_ENABLED=0 /home/matej/.local/go/bin/go test -count=1 -run '^TestSoakSurvival$' -v ./internal/acceptance` — PASS in 180.62s; literal `FAIL` scan found none.

Top three weakest points despite PASS:

1. The late-deadline loop still bursts; correctness relies on the post-run spacing oracle rejecting the run rather than preventing catch-up traffic.
2. The 1.5–3.0 second tolerance has one clean three-minute observation on this host, not cross-host scheduler calibration.
3. The cadence proof is environment-gated and expensive, so these clean-export mutations—not a fast committed unit detector—provide its direct RED-capability evidence.

Lean already; the fold adds no dependency or speculative abstraction.

VERDICT: PASS
