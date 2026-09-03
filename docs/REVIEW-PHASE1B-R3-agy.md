# REVIEW-PHASE1B-R3-agy.md — Phase-1B Verification Round 3

**Target Ref:** `slice/p0-phase1` @ commit `cf09f36`  
**Reviewer:** `agy` (Antigravity)  
**Input Findings:** Verification of the 12 Codex Round-2 findings (5 new-error + 7 unfolded).

---

## 1. Verification of Round-2 Folds

| # | Finding & Source | Code Location & Resolution | Status |
| :- | :--- | :--- | :--- |
| 1 | **Projection Init Restricted Handle**<br>`codex #2` (R2) | `SyncProjection.Init` now takes `*ProjDB` (`projection.go:52-65, 91-93`, `journal.go:302`), intercepting direct DDL/DML and trigger creations that mention canonical tables (`events`, `journal_meta`, `projection_offsets`) with `NON_CANONICAL_WRITE`. Verified in [`projection_test.go:271-305`](file:///home/matej/HARNESS/nexus/internal/kernel/journal/projection_test.go#L271-L305). | `[OK]` |
| 2 | **`ProjTx.QueryRow` Removed**<br>`codex #4` (R2) | `QueryRow` deleted from `ProjTx` (`projection.go:48-50`) to eliminate error suppression where `NON_CANONICAL_WRITE` masqueraded as `ErrNoRows`. Callers must use `Query`. | `[OK]` |
| 3 | **`Fold` Enforces Strictly Increasing Offsets**<br>`codex #6` (R2) | `Fold` rejects zero, duplicate, or decreasing offsets with `offset %d is not strictly increasing after checkpoint %d (fail closed)` (`machine.go:83-87`), preventing checkpoint regression or duplicate replays. Verified in [`machine_test.go:224-250`](file:///home/matej/HARNESS/nexus/internal/kernel/machine/machine_test.go#L224-L250). | `[OK]` |
| 4 | **Exhaustive Turn & Attempt State Matrices**<br>`codex #7` (R2) | Added comprehensive owner-derived state transition matrices for `TurnTable` and `AttemptTable` (`machine_test.go:252-315`) covering all states, events, canonical cancellations, and invalid/UNKNOWN rejections. | `[OK]` |
| 5 | **Canonical Uppercase & Unique Env Names**<br>`codex #9` (R2) | `envLayer` requires `suffix == strings.ToUpper(suffix)` and rejects duplicate normalized keys fail-closed (`config.go:187-198`), eliminating order-dependent case folding channels. Verified in [`config_test.go:191-205`](file:///home/matej/HARNESS/nexus/internal/foundation/config/config_test.go#L191-L205). | `[OK]` |
| 6 | **Diagnostics Withhold Rejected Values**<br>`codex #10` (R2) | `parseBoolStrict` and `parseList` omit raw input values from error strings (`config.go:103, 122`), reporting position/origin only to prevent secret leakage into logs. Verified with secret canaries in [`config_test.go:209-240`](file:///home/matej/HARNESS/nexus/internal/foundation/config/config_test.go#L209-L240). | `[OK]` |
| 7 | **`EnsureDir` No-Follow Component Walk**<br>`codex #11` (R2) | `EnsureDir(trustedRoot, path)` walks every component below `trustedRoot` using `verifyPrivateDir` (`Lstat`, `mode & 0077 == 0`, UID match) (`pathx.go:83-107`), refusing symlinked parent components and path escapes. Verified in [`pathx_test.go:71-91`](file:///home/matej/HARNESS/nexus/internal/foundation/pathx/pathx_test.go#L71-L91). | `[OK]` |
| 8 | **`Terminate` Straggler Reaping via `pgidRecycled`**<br>`codex #12` (R2) | `pgidRecycled` checks if the leader PID was reallocated with a different `StartTime`, allowing group `SIGKILL` to reach TERM-resistant children even after prompt leader exit (`procx_linux.go:113-125, 144-149`). Verified in [`procx_linux_test.go:147-169`](file:///home/matej/HARNESS/nexus/internal/foundation/procx/procx_linux_test.go#L147-L169). | `[OK]` |
| 9 | **P0-Min Attestation Vector & Spec Amendment**<br>`codex #13` (R2) | Formally recorded in HARNESS-SPEC Annex P0.5 amendment (`HARNESS-SPEC.md:1546-1553`). `Seal` requires `configHash` to be a 64-char lowercase SHA256 hex digest (`closure.go:151-171`), and `Snapshot.ConfigHash()` retains it. Verified in [`closure_test.go:169-176`](file:///home/matej/HARNESS/nexus/internal/kernel/closure/closure_test.go#L169-L176). | `[OK]` |
| 10 | **Dangling Conflict Targets Rejected at Resolve**<br>`codex #14` (R2) | `Resolve` iterates all manifest `Conflicts` and validates target existence in `byName` (`closure.go:55-61`). Verified in [`closure_test.go:179-191`](file:///home/matej/HARNESS/nexus/internal/kernel/closure/closure_test.go#L179-L191). | `[OK]` |
| 11 | **Worker Required & Contract-Bound Evidence**<br>`codex #15` (R2) | `Grade` requires non-empty `contract.Worker` and enforces `e.Contract == contract.ID` on every evidence item (`checker.go:171-173, 190-192`), preventing cross-contract replay and worker self-grading. Verified in [`checker_test.go:180-195`](file:///home/matej/HARNESS/nexus/internal/kernel/checker/checker_test.go#L180-L195). | `[OK]` |
| 12 | **Acknowledgement Timestamp Ordering**<br>`codex #17` (R2) | `gradeOne` computes `firstDelivered` and rejects acknowledgements where `e.Ack.AckAt.Before(firstDelivered)` (`checker.go:245-271`), while permitting redeliveries after ack. Verified in [`checker_test.go:198-223`](file:///home/matej/HARNESS/nexus/internal/kernel/checker/checker_test.go#L198-L223). | `[OK]` |

---

## 2. Weakest Points Analysis (Epistemic Honesty)

1. **Lexical Guard in `ProjDB` and `ProjTx` ([`internal/kernel/journal/projection.go:23-30`](file:///home/matej/HARNESS/nexus/internal/kernel/journal/projection.go#L23-L30))**:
   `guardStatement` matches canonical table names via regex word boundaries (`(?i)\b(events|journal_meta|projection_offsets)\b`). While this catches direct and quoted references in DDL, DML, and triggers, it relies on lexical filtering rather than SQL AST parsing. As documented in the code comment, this is the declared minimal-machinery ceiling pending the dedicated schema isolation owner in T18.
2. **`EnsureDir` Trust Root Boundary ([`internal/foundation/pathx/pathx.go:83-88`](file:///home/matej/HARNESS/nexus/internal/foundation/pathx/pathx.go#L83-L88))**:
   `EnsureDir(trustedRoot, path)` verifies directories strictly below `trustedRoot`. Any symlink at or above `trustedRoot` (such as a symlinked `$HOME` in `os.UserConfigDir`) is treated as the caller's trust decision.
3. **Linux Kernel PGID Pinning Assumption ([`internal/foundation/procx/procx_linux.go:107-116`](file:///home/matej/HARNESS/nexus/internal/foundation/procx/procx_linux.go#L107-L116))**:
   `procx` relies on the Linux kernel guarantee that a PID used as a PGID is not reallocated to an unrelated process until the entire process group has exited. If `StartTime(pgid)` returns an error (meaning the group leader has exited but members remain), `pgidRecycled()` returns `false` so the signal group continues to receive `SIGKILL`.

---

## 3. Verification Evidence

- `go vet ./internal/...`: Clean (exit code 0).
- `go test -count=1 ./internal/...`: All 15 internal packages passed uncached.

---

VERDICT: PASS
