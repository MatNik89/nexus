# REVIEW-FRESH-R3-kilo — QA verification round 3

- **Repo**: `/home/matej/HARNESS/nexus`
- **Reviewed commit**: `722ef83` (`fix(fresh-audit-r2): trust-set rollback, honest LIVE/READY states, seam gating, buildvcs`), branch `slice/p0-fresh-audit`
- **Method**: read-only. Work in a clean `git archive` export at `/tmp/kilo/r3` (verified: no `.git`). Repo never mutated; only this report is written into it.
- **Tooling**: Go 1.26.4 (linux/arm64), `bwrap` 0.11.2 present.

## Verdict summary

All four round-2 findings are closed and RED-capable:

| Item | Disposition | RED ablation |
|------|-------------|--------------|
| codex #4 r2 — trust-set rollback | **CLOSED** | disable rollback → `MIXED trust generation` FAIL |
| codex #3 r2 — `reminderReadiness` states | **CLOSED** | remove stale branch → `want (false,OFF,*STALE*)` FAIL |
| kilo B — seam gating | **CLOSED** | `testing.Testing()` proven false in a `go build` binary |
| agy defect 1 — `-buildvcs=false` | **CLOSED** | all 7 test helpers + `p0-accept.sh` verified by grep |

No unresolved findings.

---

## 1. codex #4 r2 — trust-set rollback (CLOSED, RED-capable)

`scripts/p0-accept.sh:17-65` adds `publish_trust_set`, which snapshots the prior trust set (anchor + attestation + signature) into `$WORK/rollback` **before** any rename, then performs three same-directory renames, calling `rollback()` on any failure or seam-triggered interruption. `rollback()` restores all three prior files (or removes files that had no prior generation), so no mixed generation can survive.

- Conformance test `TestAcceptPublishRollbackRestoresWholeSet` (`cmd/nexus/main_test.go`) drives `publish_trust_set` via `NEXUS_ACCEPT_TEST_PUBLISH_ONLY=1` with `NEXUS_ACCEPT_TEST_FAIL_AFTER=1` and `=2` (the two interruption points where a mixed set is possible), asserting the complete prior generation is restored, then asserts an un-faulted publication installs the complete new set. **PASS** (0.55s).
- **RED ablation** (export): I replaced `rollback()` with a no-op. Result:

```
--- FAIL: TestAcceptPublishRollbackRestoresWholeSet
    main_test.go:1665: step 1: …/allowed_signers not restored (got "NEW-ANCHOR\n") — MIXED trust generation
```

The detector genuinely goes RED when the rollback is broken.

## 2. codex #3 r2 — `reminderReadiness` states (CLOSED, RED-capable)

`cmd/nexus/main.go:874-895` factors the reminders decision into the pure `reminderReadiness(health, hbAge, hbExists, profilesOK) (bool, string, string)` with four branches (dirty-health → OFF, stale heartbeat → OFF, fresh heartbeat → LIVE, no heartbeat → READY). The doctor now prints an explicit `LIVE`/`READY`/`OFF` state per criterion (`main.go:857-863`), replacing the round-2 wording-only fix with a real, testable state machine.

- Branch table test `TestReminderReadinessBranches` (`cmd/nexus/main_test.go`) covers all four branches **plus** the `profilesOK=false` edge. **PASS** (0.00s).
- **RED ablation** (export): I removed the stale-heartbeat branch. Result:

```
--- FAIL: TestReminderReadinessBranches
    main_test.go:1618: stale heartbeat: got (true,LIVE,"daemon heartbeat fresh…"), want (false,OFF,*STALE*)
```

My round-2 Finding A (stale-heartbeat detector could not go RED) is now closed.

- Cross-package consistency: the doctor's new `%-5s` state column and the acceptance assertion `"OFF   telegram"` (`acceptance_linux_test.go:1084`) are mutually consistent — `TestDoctorP0GrantLive` passes end-to-end against a freshly pinned binary (104s).

## 3. kilo B — seam gating (CLOSED)

`cmd/nexus/main.go:357` now gates the fault seam with `testing.Testing()`:

```go
if testing.Testing() && os.Getenv("NEXUS_TEST_CLOSE_JOURNAL_POST_EFFECT") != "" {
	b.j.Close()
}
```

This matches the codebase's own discipline (`journal.go:545-546`). I empirically confirmed `testing.Testing()` returns `false` in a `go build` binary (built a tiny probe: prints `testing.Testing() = false`), so the seam cannot fire in a production daemon. The codex #1 regression `TestPostEffectDurabilityFailureIsNotSuccess` still passes under `go test` (where `testing.Testing()` is true). Full `cmd/nexus` suite passes; `go vet ./cmd/nexus` clean.

## 4. agy defect 1 — `-buildvcs=false` (CLOSED)

Grep across the tree confirms every `exec.Command("go", "build", …)` in test helpers now carries `-buildvcs=false` (7 sites: `acceptance`×3, `buildcheck`, `exectool`, `probe`, `sandbox`), and `scripts/p0-accept.sh:78` carries it on the graded-binary build. `go test ./internal/buildcheck` passes from the clean export (8.7s).

**Premise check (not a defect, but worth recording):** I tested the underlying claim that `go build` fails in a `git archive` export. On Go 1.26.4, `CGO_ENABLED=0 go build -o /tmp/… ./cmd/nexus` from the `.git`-less export exits **0** (`-buildvcs=auto` silently skips stamping when no VCS is present). The `-buildvcs=false` additions are therefore harmless and improve digest determinism across checkout-vs-export builds, but no current build actually fails without them. This does not weaken the closure — the stated scope (test helpers + `p0-accept.sh`) is fully addressed.

## Residual observations (NOT findings)

- `Makefile:6` still runs `go build` without `-buildvcs=false`. It is (a) outside the agy repair scope ("test helpers + `p0-accept.sh`"), (b) not broken — it builds successfully from a clean export (exit 0, verified), and (c) not digest-matched to the acceptance attestation (the graded binary is built separately by `p0-accept.sh`). No defect.

## Confidence

- **Ablation-proven RED**: rollback (codex #4 r2), `reminderReadiness` (codex #3 r2).
- **Empirically verified**: `testing.Testing() == false` outside `go test` (kilo B), all `-buildvcs=false` sites by grep (agy 1), full `cmd/nexus` + `internal/buildcheck` + `TestDoctorP0GrantLive` pass at my uid.
- Everything re-derived from `722ef83`; no prior-session context relied upon.

VERDICT: PASS
