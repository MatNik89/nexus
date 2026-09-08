# Review of convproj implementation round 4 (`d64140e`)

**Scope**: commit `d64140e` on `slice/p0-convproj` (folds impl-r3 detector findings).
**Reviewer**: kilo
**Date**: 2026-09-08

## Verdict

FAIL — the daemon test package does not build. The round-4 hook detector
references `testRecoveryVerifyHook` but it is never declared nor wired into
`recoveredTurnOutcome`, so `go test ./internal/app/daemon/` fails with
`undefined: testRecoveryVerifyHook` and the whole daemon test suite (oracle,
redelivery conformance, rebuild, latency) cannot compile or run. This is an
unbuildable change, a substantive flaw.

## Finding

### F1 (substantive, unbuildable): `testRecoveryVerifyHook` is referenced but never declared or invoked

`TestOrdinaryFailureSkipsRecovery` assigns the hook
(`convproj_diff_test.go:260-261`):

```go
testRecoveryVerifyHook = func() { verifyCount++ }
t.Cleanup(func() { testRecoveryVerifyHook = nil })
```

A whole-repo search finds **no `var testRecoveryVerifyHook` declaration** and
**no call site**: `recoveredTurnOutcome` still calls
`d.deps.Journal.VerifyChain()` (`daemon.go:484`) without invoking any hook.
Verified by running the test:

```
internal/app/daemon/convproj_diff_test.go:260:2: undefined: testRecoveryVerifyHook
internal/app/daemon/convproj_diff_test.go:261:21: undefined: testRecoveryVerifyHook
FAIL  github.com/MatNik89/nexus/internal/app/daemon [build failed]
```

Two independent omissions, both required for the detector to work:

1. Declare the hook (e.g. `var testRecoveryVerifyHook func()` in a `_test.go`
   or `daemon.go`).
2. Invoke it in `recoveredTurnOutcome` after `VerifyChain` succeeds
   (`if testRecoveryVerifyHook != nil { testRecoveryVerifyHook() }`). Without
   (2), even a compiling test would observe `verifyCount == 0` on the
   redelivery branch and fail the `verifyCount == 1` assertion — so the hook
   must be wired, not merely declared.

Consequence: finding (3) is not delivered; it breaks the build of the entire
`daemon` test package, so the "Full suite 0 FAIL" claim cannot be true as
committed.

## Verified correct (structurally)

- **Detector 1 — `TestDuplicateEventSignalIsEventIDOnly`** now forces a real
  non-event-id constraint (a raw shadow row at `journal_offset` causing a
  PRIMARY KEY clash on a FRESH event id) and asserts it is NOT
  `ErrDuplicateEvent`. The `journal` package compiles and the test PASSES.
  RED-capable: the old broad `strings.Contains("UNIQUE constraint")`
  classifier would have matched the PRIMARY KEY violation and the
  `errors.Is` assert would go RED.
- **Detector 2 — malformed-succeed kind** is now actually in the oracle's
  `kinds` list with `{"final":123}`
  (`convproj_diff_test.go:88,111-113`), exercising the decode-error path
  where the reference discards and the fold must too. Structurally complete
  and RED-capable against a regression of the round-2 `conv.go` guard, though
  it cannot be executed until F1's compile error is fixed.

## Notes (not FAIL reasons)

- The malformed-succeed case uses `{"final":123}` (type mismatch, not raw
  invalid JSON). A raw non-JSON payload is the other decode-error shape the
  oracle still does not generate; immaterial since both are rejected by the
  same `json.Unmarshal != nil` guard.

VERDICT: FAIL
