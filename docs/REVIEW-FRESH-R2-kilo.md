# REVIEW-FRESH-R2-kilo — QA verification round 2

- **Repo**: `/home/matej/HARNESS/nexus`
- **Reviewed commit**: `564909d` (`fix(fresh-audit): fold the fourth-eyes audit findings (codex 3H+1M+1L, kilo 1L)`), branch `slice/p0-fresh-audit`
- **Method**: read-only. Work in a clean `git archive` export at `/tmp/kilo/r2` (verified: no `.git` present). The repo was never mutated; only this report is written into it.
- **Tooling**: Go 1.26.4 (linux/arm64). `bwrap` 0.11.2 present. `-race` still unusable on this host (TSAN `unsupported VMA range`); no finding below depends on race-detector output.

## Verdict summary

| Item | Disposition |
|------|-------------|
| kilo F1 rejection (shared `boundedBuffer`) | **ACCEPTED** — verified correct against Go source |
| kilo F2 rejection (journal send/close) | **ACCEPTED** — verified correct by 500k-iteration empirical probe |
| codex #1 (post-effect durability → E9) | **CLOSED** — RED-capable (ablation-proven) |
| codex #2 (sandbox requirement in graded run) | **CLOSED** — skip→fail flip + floor detector in scope |
| codex #3 (doctor readiness/liveness honesty) | **PARTIALLY CLOSED** — one new fail-closed detector is not RED-capable (Finding A) |
| codex #4 (p0-accept staging/publish) | **CLOSED** |
| codex #5 (buildcheck from clean export) | **CLOSED** |
| kilo F3 (telegram refusal idempotency) | **CLOSED** — RED-capable (ablation-proven) |

Two new LOW findings (A, B) → verdict FAIL.

---

## Verification of the two REJECTED findings (contest with counter-probe where applicable)

### kilo F1 — ACCEPTED (my original finding was wrong)

The rejection's claim — "os/exec documents that one ==-comparable writer as both Stdout and Stderr is called by at most one goroutine at a time" — is correct, and it is not just documentation but a code path. `os/exec` `childStderr` (Go 1.26 source, `exec.go:569-573`):

```go
func (c *Cmd) childStderr(childStdout *os.File) (*os.File, error) {
	if c.Stderr != nil && interfaceEqual(c.Stderr, c.Stdout) {
		return childStdout, nil
	}
	return c.writerDescriptor(c.Stderr)
}
```

`SetOutput(out, out)` sets both to the same `*boundedBuffer` (a comparable pointer type), so `interfaceEqual` is true and stderr reuses stdout's single pipe → a **single** copy goroutine writes the shared buffer. No race. My F1 was based on the wrong assumption of two copy goroutines; the fix's comment (`sandbox.go:325-330`) is accurate. Accepted.

### kilo F2 — ACCEPTED (my original finding was wrong)

The rejection's rationale is correct. I ran a faithful standalone replication of the exact journal structure (unbuffered `reqs`, `select { send / <-done }` sender, `select { <-done / recv }` actor, buffered reply, concurrent `close(done)`), 500,000 iterations with a rendezvous barrier:

```
iterations=500000 sendCommitted=499423 lost=0
no lost value detected
```

Result: in the 499,423 iterations where the send **committed**, the actor *always* sent the reply (zero lost values); in the 577 where it did not commit, the sender's `<-done` case fired (no hang). This matches the runtime semantics: an unbuffered send in a `select` commits only on rendezvous, and once the value is handed to the receiver's sudog the receiver processes that case — there is no "committed-but-unreceived" state, and no window where the reply is dropped. The `journal.go:581-586` comment added by the fold is correct. Accepted.

---

## Verification of each fold (closed / RED-capable)

### codex #1 — CLOSED (RED-capable, ablation-proven)

`cmd/nexus/main.go:362-370`: `MarkResumeCompleted` failure after a committed effect now returns an explicit E9 UNKNOWN error ("…EXECUTED… Do NOT retry…"), never plain success.

- Regression `TestPostEffectDurabilityFailureIsNotSuccess` passes on the fixed code.
- **RED ablation**: I reverted `resumeApproved` to the old log-and-fall-through in the export. Result:

```
nexus: resume-completed mark ch-…: journal is closed
--- FAIL: TestPostEffectDurabilityFailureIsNotSuccess
    main_test.go:1586: post-effect durability failure reported as success: "remembered (fact-tc-dl): delayed fact"
```

The detector genuinely goes RED when the fix is absent.

### codex #2 — CLOSED (skip→fail flip and floor scope verified)

- `NEXUS_ACCEPT_REQUIRE_SANDBOX=1` converts the three bwrap-absence `t.Skipf` sites (`acceptance_linux_test.go:715`, `probe_linux_test.go:38`, `sandbox_linux_test.go:51`) into `t.Fatalf`.
- **Empirical**: with a PATH lacking `bwrap` (symlinks to `go/cc/sh/…` only):
  - without the env var → `ok` (SKIP);
  - with `NEXUS_ACCEPT_REQUIRE_SANDBOX=1` → `--- FAIL … bwrap REQUIRED for the graded acceptance run`.
- `p0-accept.sh:36-37` now grades `./internal/acceptance ./internal/preflight/probe ./internal/sandbox` with the env set. The syscall-floor detector `TestSyscallFloorDeniesPtrace` (`probe_linux_test.go:204-214`) is in the graded scope and has a genuine negative control (`TestNegativeControlPtraceAllowedWhenSeccompLoosened`, `:216-225`), so it is not vacuous. Doctor's suite pin (`main.go:889`) matches the new string.

### codex #3 — PARTIALLY CLOSED (Finding A)

The false-LIVE claim is genuinely fixed: the doctor now folds the real projections (`main.go:791-792`), and the reminders criterion distinguishes fresh / stale / absent heartbeat (`main.go:807-820`), with honest "READY" wording. The core HIGH finding is resolved.

But the fail-closed **stale-heartbeat** half has no RED test. The codex #3 required-repair explicitly listed "negative tests for absent/stale heartbeat, absent health, and corrupt/unfoldable projections" — none were added (the only new test in `main_test.go` is the codex #1 regression). See Finding A below.

### codex #4 — CLOSED

`scripts/p0-accept.sh` now: validates `$KEY`/`$KEY.pub` before touching any installed state (`:18-22`); stages `allowed_signers`, the binary, the attestation and signature all in the private `$WORK` dir; and publishes via same-directory renames only after a full pass (`:54-59`). A failed run no longer corrupts the previously-installed trust anchor (the original `cat "$KEY.pub"` failure path is gone). The three separate `mv`s are not one atomic op, but any partial publish leaves the anchor/attestation/signature mutually inconsistent, which the doctor's verification fails **closed** (no false grant), so this is acceptable.

### codex #5 — CLOSED (verified from a clean export)

`staticcheck_test.go:16-27` derives the repo root via `runtime.Caller(0)` + three `filepath.Dir` hops, gated on `scripts/static-check.sh` existing. Ran in the clean export (no `.git`):

```
go test -count=1 ./internal/buildcheck  →  ok  (12.429s)
```

### kilo F3 — CLOSED (RED-capable, ablation-proven)

`telegram.go:206-216` derives refusal delivery ids from `(identity, updateID)`; the three refusal sites (`:229`, `:238`, `:247`) now use `EnqueueReplyID`, making redelivered refusals idempotent.

- Regression `TestRedeliveredRefusalIsIdempotent` passes.
- **RED ablation**: reverted the refusal sites to `EnqueueReply` (random ids). Result:

```
--- FAIL: TestRedeliveredRefusalIsIdempotent
    telegram_test.go:405: redelivered refusal produced 2 sends, want exactly 1: […]
```

The detector goes RED when the fix is absent.

---

## Findings (unresolved)

### Finding A — LOW — codex #3's stale-heartbeat detector cannot go RED

**Evidence**: `cmd/nexus/main.go:814-820` adds a three-branch reminders liveness check (health-error → fail; stale heartbeat → fail; fresh → ok; absent → READY). Only the **absent** branch is exercised by any test (`TestDoctorP0GrantLive` runs `doctor --p0` in a world with no daemon, hence no heartbeat file). The stale (fail-closed) and fresh branches have no coverage, and there is no test for the "corrupt/unfoldable projection" case that the new projection fold (`main.go:791-792`) is meant to surface.

**Ablation (run)**: I removed the stale-heartbeat branch in the export (so `os.Stat(heartbeat)` success always reports "fresh"), then ran:

```
NEXUS_ACCEPT_REQUIRE_SANDBOX=1 go test -count=1 -run 'TestDoctorP0GrantLive' ./internal/acceptance  →  ok (5.411s)
```

The detector's removal leaves every doctor test green — i.e. the fail-closed half of the codex #3 fold is not RED-protected. This is precisely the "detectors that cannot go RED" class the audit was asked to hunt, and it contradicts the codex #3 required-repair list that the fold claims to satisfy.

**Suggested fix**: add `runDoctorP0`-level negative tests: (a) a stale heartbeat file (old mtime) → reminders OFF and P0-capable withdrawn; (b) a fresh heartbeat → reminders live; (c) a journal whose projection fails to fold (e.g. a legacy/corrupt event) → memory/profiles OFF.

### Finding B — LOW — post-effect fault seam is reachable in production (not gated to test builds)

**Evidence**: `cmd/nexus/main.go:357-361` keys the codex #1 fault injection on a bare `os.Getenv("NEXUS_TEST_CLOSE_JOURNAL_POST_EFFECT")`. The journal's own fault seams gate on `testing.Testing()` (`journal.go:545-546`), so they cannot fire in a shipped binary. This new seam breaks that discipline: a production `nexus daemon` started with `NEXUS_TEST_CLOSE_JOURNAL_POST_EFFECT=1` in its environment will call `b.j.Close()` at the post-effect boundary, closing the single-writer journal mid-resume and taking down every subsequent append (self-DoS). The failure is honest (E9, fail-closed), so this is not a security hole, but it is an environment-reachable test seam the codebase elsewhere deliberately excludes from production.

**Suggested fix**: gate the seam with `testing.Testing()` (like `NEXUS_TEST_KILL_MID_BATCH`), or move the fault injection behind a compile-time/test-only hook.

---

## Confidence

- **Ran and verified directly**: codex #5 (clean-export buildcheck), kilo F3 regression + RED ablation, codex #1 regression + RED ablation, codex #2 skip→fail flip, syscall-floor detector + negative control, F2 500k-iteration probe.
- **Verified by Go 1.26 source**: F1 (single copy goroutine for a ==-comparable shared writer).
- **Verified by code review**: codex #4 staging/publish discipline.
- **Ablation-proven gap**: Finding A (stale-heartbeat detector removal → all tests still green).

Everything was re-derived from the repository at `564909d`; no prior-session context was relied upon.

VERDICT: FAIL
