# Review of the full audit-remediation stack r3 (`slice/audit-b`)

- **Reviewed revision**: `457c56d` on `slice/audit-b` (worktree `/home/matej/HARNESS/nexus-b`)
- **Diff**: `fdb39dc..457c56d`
- **Verdict**: FAIL

`CGO_ENABLED=0 go test -count=1 ./internal/...` FAILS in two `internal/kernel/effectpath`
tests; every other internal package passes.

## Round-2 disposition

My round-2 was PASS with two notes. The codex round-2 was FAIL with eight findings;
seven are closed, one (finding 2) was fixed by a change that then regressed the
effectpath suite. Disposition by finding:

- **#1 (bare `*Failure` → substrate)** — CLOSED. `channel.ClassOf` now classifies a
  typed `*Failure` by its own fields: `receipt_not_durable` → substrate, 401/403 →
  remote_rejected, else transport (`channel.go:94-117`).
- **#4 (method/kind not grant-bound)** — CLOSED. `kindMethods` is the closed method
  set per `callKind`, and a method presented under another kind is refused before
  Consume (`telegram.go:217-232`).
- **#5 (salvage on final transport error)** — CLOSED (`structured.go` salvage runs
  over re-ask/original bytes before terminalizing on the final generation error).
- **#6 (policy/event validation)** — CLOSED. `Policy.Validate` + `canonical()` full
  identity compare on `Begin`; `strictDecode` (no unknown fields, no trailing JSON)
  for `policy_json` (`s7.go:120-162,332-360,759-783`).
- **#7 (health fail-open + no-op ticks)** — CLOSED. `health.Healthy`/`health.Report`
  write errors are surfaced fatal; no-op `ErrNotDue` cycles leave prior health
  unchanged (`telegram.go:733-745`).
- **#8 (detector matrix)** — CLOSED. `s7/fault_test.go`, `telegram/matrix_test.go`,
  and the planner `retry_test.go` add the fault seams and cross-kind rows.
- **#3 (setMyCommands split-brain)** — CLOSED (the control-effect Consume parks the
  owner row UNKNOWN, and the restart batch lands both S7 and channel UNKNOWN).
- **#2 (S7 AttemptContext unused)** — the transport integration is fixed, but the
  fix also changed `AttemptContext` internals and REGRESSED the effectpath suite; see
  F1.

## New finding

### F1 (substantive — regression): `AttemptContext` now derives the child deadline from `WithTimeout(remaining)` instead of `WithDeadline(deadline)`, breaking SPEC P0.2 deadline propagation and failing the effectpath suite

`internal/kernel/s7/s7.go:685-700` changed the returned context from
`context.WithDeadline(ctx, deadline)` to:

```go
now := a.now()
...
remaining := deadline.Sub(now)
if remaining <= 0 { return nil, nil, ErrAttemptNotAuthorized }
dctx, cancel = context.WithTimeout(ctx, remaining)
```

`deadline` is the authority's computed earliest (grant expiry / operation deadline /
attempt timeout / call deadline), an absolute instant. `WithTimeout` re-derives a
wall-clock deadline from a FRESH `time.Now()` that differs from the `a.now()` used to
compute `remaining`, so:

- **Wall clock** (`a.now()==time.Now`): the child deadline is
  `deadline + (time.Now()₂ − time.Now()₁)` — skewed later by the double read. This is
  exactly what fails `TestCallDeadlinePropagatedIntoExecution`
  (`effectpath_test.go:571-573`): `executor deadline … != call deadline …` (~259 ns).
- **Injected clock** (`a.now()==time.Unix(1000,0)`): the authority's absolute grant
  expiry is discarded and replaced by `wall_now + remaining`, so the injected lease no
  longer caps the deadline. `TestAttemptContextIsS7Owned`
  (`effectpath_test.go:616-622`) expects the pinned grant expiry
  (`1970-01-01 01:17:40`) and instead observes `…m=+60.07s`.

The effectpath package and its tests are unchanged from the B1 slice; the regression
is introduced solely by the `s7.go` `AttemptContext` edit. The `remaining <= 0`
expiry refusal is correct and should be kept; only the deadline construction is
wrong.

Concrete fix: keep the `remaining <= 0` fail-closed check, then set the child context
with `context.WithDeadline(ctx, deadline)` (the absolute authority instant), not
`context.WithTimeout(ctx, remaining)`. Both named effectpath tests must return GREEN,
and the injected-clock case must again observe the pinned grant expiry as the deadline.

VERDICT: FAIL
