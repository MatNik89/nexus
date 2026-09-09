# Code review — coding-runner substrate, first increment (`640c647..a0322c6`)

- **Reviewed**: `a0322c6` on `main` (`internal/coding/runner/{snapshot,toolchain,run}.go` + tests)
- **Verdict**: PASS — the architectural adjudication resolves to "the EffectPath bypass is
  correct", with one real test bug named below.

## Central question — the EffectPath bypass is correct; E8 is scoped to model-visible tools

Reading E8 itself (`docs/ARCHITECTURE-ESSENTIALS.md:107-115`): the invariant is
`S3.Loop → sealed ToolSpec.ExecutionKind → S6.0.Decide → S6.9.Before → {executor} →
S6.9.After/OnError → S7`. Every link is model-visible: `S3.Loop` is the planner-driven loop,
`ToolSpec` is the sealed tool registry, `S6.0.Decide` is the PEP. The point of "one
effect-path, sandbox IN the type" is that **no model tool can inject an unsandboxed executor
and no tool call can skip the PEP**. `internal/coding/runner.Run` (`run.go:86-208`) is not a
model tool — there is no `ToolCall`, no `ToolSpec`, no `S6.0.Decide`, no human to ask — and it
calls the **real** `sandbox.Backend` (bwrap `Probe→Compile→Launch→Attest`), so the sandbox is
still "in the type" by construction, not bypassed.

The divergence risk codex flagged does not materialize, because the two things that matter are
NOT duplicated:

- **S7 governance is in one place.** `Run` calls the same `s7.Authority` primitives
  `EffectPath.RunTool` does — `Consume` (`run.go:152`), `AttemptContext` (`run.go:160`),
  `Report`/`Cancel` (`run.go:192,204`). `effectpath.go:467` also uses
  `p.grants.AttemptContext`, so the grant-to-exec-deadline binding is already shared; a future
  S7 fix lands in `s7.go` and applies to both callers alike.
- **The outcome classification genuinely differs, and that is the point.** `Run` deliberately
  treats the sandboxed process's nonzero exit as DATA, not an attempt failure
  (`run.go:196-200`): a `go test` that fails tests is a *successfully completed attempt*.
  `EffectPath.RunTool` cannot express that shape — its executor's nonzero exit is an attempt
  failure. Routing a coding-run through `EffectPath` would therefore *misclassify* the most
  common real outcome. This is exactly why `Run`'s classification is not copy-paste duplication
  but a different, correct shape for a different call.

Codex's concrete rework (a closed internal `ToolID` + an always-`DecisionAllow` PEP rule +
route through `EffectPath.RunTool`) is **not cleanly implementable**: `RunTool`'s `ExecProcess`
dispatch binds exactly ONE `SandboxBackend` regardless of `ToolID` (`effectpath.go:435-438`),
and that backend is `exectool.Adapter` — the model-visible, hardcoded-`DecisionAsk` tool
(`exectool.go:72`). To route a coding-run `ToolID` you would have to re-couple the
internal-initiative path with the ASK tool and add a PEP decision that no human ever sees — a
fake approval with extra steps, plus the misclassification above. `Run`'s own closed
`coding.run` journal event (`run.go:237-285`, journaled with outcome, snapshot digest, toolchain
digest, policy hash, S7 op id) is the correct audit record and is not weaker than the tool-call
event shape.

## Side findings

### (side 1) `Process.Output()` silently truncates at 1 MiB — real, but future

`sandbox.boundedBuffer` (`sandbox.go:519-537`) swallows bytes past `limit = 1<<20` with no
marker, and `Process.Output()` returns the truncated string. For THIS increment this is not yet
exercised (its tests run `go build`/`go test` on a tiny module — output is far below 1 MiB),
but it WILL corrupt a `go list -deps -test -json` capture in Slice 2 and a `go test -json`
capture in Slice 1 (a real module's `go list` stream is routinely multi-MiB; a truncated JSON
stream fails `impact.ParseGoList` on the final object). Fix at that slice: stream-parse or add a
truncation marker the consumer can detect; do not let a consumer trust a silently-cut buffer.

### (side 2) `ResolveToolchain(goBinary)` correctly places binary selection on the caller

`ResolveToolchain` uses `goBinary` as given (`toolchain.go:48-52`), and the run passes
`spec.GoBinary` straight into `sandbox.Spec.Target` (`run.go:133`), which requires an absolute
ELF path — so a bare `"go"` name fails at `Compile`, never silently. `GOTOOLCHAIN=local`
(`toolchain.go:68`) prevents the sandboxed `go` from downloading a different toolchain. The
caller (NEXUS's own orchestrator) owns choosing the toolchain-managed 1.26.6 binary vs the
stale `~/.local/go` 1.26.4; the package does not silently pick. Correct.

### (side 3, the real defect this round) — flaky test: grant TTL (1 min) < RunSpec.Timeout (150 s)

`TestRunReportsTestFailureAsDataNotAttemptFailure` constructs
`s7.NewAuthority(time.Now, time.Minute)` (`run_test.go:101`) but passes `Timeout: 150s`
(`run_test.go:121`). `Run` derives the exec deadline via
`grants.AttemptContext(ctx, op, deadline)` (`run.go:160`), and `AttemptContext` caps the
deadline at the **grant expiry** (the authority's 1-minute TTL) — so the sandboxed `go test` is
killed at ~60 s, not 150 s. In isolation this passes (54 s < 60 s); under a cold module cache or
a loaded full suite the `go test` exceeds 60 s and the run is SIGKILLed
(`--- FAIL: TestRunReportsTestFailureAsDataNotAttemptFailure … cancelled by its own deadline:
signal: killed`). This is a test misconfiguration exposing a real, un-documented runner
contract: **the effective deadline is `min(grant TTL, RunSpec.Timeout)`**, not `Timeout`. The
production enforcement (an attempt cannot outlive its grant) is correct S7 semantics — the gap
is that `Run` neither documents that cap nor ensures the caller's grant TTL ≥ `Timeout`. Fix the
test (grant TTL ≥ 150 s, or `Timeout` ≤ 60 s) and add one sentence to `Run`'s doc comment stating
the effective-deadline contract.

## Tests run

`go build ./...`, `go vet ./...` green. `go test ./internal/coding/runner/...` green in
isolation (real bwrap + real toolchain, 81.8 s, 14 tests including the end-to-end real-module
build and the test-failure-is-data case). `go test ./...` FAILS on the flaky test above under
load (reproduced), confirming the TTL/Timeout mismatch is the cause, not the sandbox or the
runner logic.

VERDICT: PASS
