# REVIEW-PHASE1B-R2-agy.md — Phase-1B Verification Round 2

**Target Ref:** `slice/p0-phase1` @ commit `2579b77`  
**Reviewer:** `agy` (Antigravity)  
**Input Findings:** Round 1 findings across reviewers: `codex` (19), `kilo` (3), `agy` (5).

---

## 1. Verification of Adjudications & Round-1 Folds

| Finding / Issue | Source Ref & Location | Resolution & Verification | Status |
| :--- | :--- | :--- | :--- |
| **Checker ALL-must-agree Semantics** | `codex #3`, `kilo #2`, `agy #2`<br>[`checker.go:195-227`](file:///home/matej/HARNESS/nexus/internal/kernel/checker/checker.go#L195-L227) | `gradeOne` loops across the entire bundle; for `ExitCodeIs` and `FileHashIs`, any contradicting evidence fails immediately, while all matching records must agree. Order-independent, duplicate-prepend-proof. Verified in [`checker_test.go:138-151`](file:///home/matej/HARNESS/nexus/internal/kernel/checker/checker_test.go#L138-L151). | `[OK]` |
| **Machine Multi-From Cancel & Owner Amendment** | `codex #8`, `kilo #1`, `agy #1`<br>[`machine.go:111-129`](file:///home/matej/HARNESS/nexus/internal/kernel/machine/machine.go#L111-L129), [`HARNESS-SPEC.md:1381`](file:///home/matej/HARNESS/nexus/docs/HARNESS-SPEC.md#L1381) | Formally amended in HARNESS-SPEC Annex P0.1. Suffix aliases (`.admitted`, `.running`) removed. Single canonical `EvRunCancelled`, `EvTurnCancelled`, `EvAttemptCancelled` mapped across all non-terminal states. Cancel from `UNKNOWN` rejected. Verified in [`machine_test.go:133-180`](file:///home/matej/HARNESS/nexus/internal/kernel/machine/machine_test.go#L133-L180). | `[OK]` |
| **Checker Worker != Principal Enforced** | `codex #1`<br>[`checker.go:74, 175-177`](file:///home/matej/HARNESS/nexus/internal/kernel/checker/checker.go#L74) | `AcceptanceContract.Worker` is compared against `Evidence.Producer`; self-produced evidence fails validation at admission. Verified in [`checker_test.go:118-134`](file:///home/matej/HARNESS/nexus/internal/kernel/checker/checker_test.go#L118-L134). | `[OK]` |
| **Checker Delivery Evidence Zero-Value Rejection** | `codex #2`<br>[`checker.go:113-124`](file:///home/matej/HARNESS/nexus/internal/kernel/checker/checker.go#L113-L124) | `validate()` rejects empty `OccurrenceID`, zero timestamps, and missing `Producer`. Verified in [`checker_test.go:155-167`](file:///home/matej/HARNESS/nexus/internal/kernel/checker/checker_test.go#L155-L167). | `[OK]` |
| **Checker SHA256 Shape Validation** | `codex #4`<br>[`checker.go:33-43, 122-124`](file:///home/matej/HARNESS/nexus/internal/kernel/checker/checker.go#L33-L43) | `sha256Hex` enforces exact 64-character lowercase hex format on criteria and evidence. Verified in [`checker_test.go:170-175`](file:///home/matej/HARNESS/nexus/internal/kernel/checker/checker_test.go#L170-L175). | `[OK]` |
| **Journal Projection Init Lease Gate** | `codex #5`<br>[`journal.go:300-306`](file:///home/matej/HARNESS/nexus/internal/kernel/journal/journal.go#L300-L306) | `SyncProjection.Init(db)` runs only after acquiring the writer lease and passing `verifyChainFull()`. A second opener never mutates. Verified in [`projection_test.go:255-267`](file:///home/matej/HARNESS/nexus/internal/kernel/journal/projection_test.go#L255-L267). | `[OK]` |
| **Journal ProjTx Restricted Handle** | `codex #6`<br>[`projection.go:19-55, 78-86`](file:///home/matej/HARNESS/nexus/internal/kernel/journal/projection.go#L19-L55) | `ProjTx` wraps `*sql.Tx` and intercepts statements targeting `events`, `journal_meta`, or `projection_offsets`, returning `NON_CANONICAL_WRITE`. Verified in [`projection_test.go:228-242`](file:///home/matej/HARNESS/nexus/internal/kernel/journal/projection_test.go#L228-L242). | `[OK]` |
| **Machine Fold Replays Journal & Checkpoints** | `codex #9`<br>[`machine.go:64-90`](file:///home/matej/HARNESS/nexus/internal/kernel/machine/machine.go#L64-L90) | `Fold` consumes `[]FoldEvent{Type, Offset}`, returns checkpoint offset, and notifies intermediate observer. Real recorded run fold verified in [`projection_test.go:272-318`](file:///home/matej/HARNESS/nexus/internal/kernel/journal/projection_test.go#L272-L318). | `[OK]` |
| **Machine Step Returns Current State on Error** | `codex #10`<br>[`machine.go:50-62`](file:///home/matej/HARNESS/nexus/internal/kernel/machine/machine.go#L50-L62) | `Step` returns `current` state on rejected transition rather than zero value. Verified in [`machine_test.go:123-129`](file:///home/matej/HARNESS/nexus/internal/kernel/machine/machine_test.go#L123-L129). | `[OK]` |
| **Config Typed Schema Across All Layers** | `codex #11`<br>[`config.go:54-79, 111-165`](file:///home/matej/HARNESS/nexus/internal/foundation/config/config.go#L54-L79) | `keySchema` enforces typed per-key schema without generic string coercion; non-strict booleans rejected. Verified in [`config_test.go:123-148`](file:///home/matej/HARNESS/nexus/internal/foundation/config/config_test.go#L123-L148). | `[OK]` |
| **Config Env Layer Enumerates Environ** | `codex #12`, `agy #4`<br>[`config.go:170-192`](file:///home/matej/HARNESS/nexus/internal/foundation/config/config.go#L170-L192) | `envLayer` scans `environ []string` for `NEXUS_CFG_*` prefix and rejects unknown variables fail-closed. Verified in [`config_test.go:113-119`](file:///home/matej/HARNESS/nexus/internal/foundation/config/config_test.go#L113-L119). | `[OK]` |
| **Config Egress Allow Whitespace & Empty Rejection** | `codex #13`, `kilo #3`, `agy #3`<br>[`config.go:96-109, 203-207`](file:///home/matej/HARNESS/nexus/internal/foundation/config/config.go#L96-L109) | `parseList` trims and rejects empty or whitespace-padded host strings in all layers. Verified in [`config_test.go:152-170`](file:///home/matej/HARNESS/nexus/internal/foundation/config/config_test.go#L152-L170). | `[OK]` |
| **Pathx Profile Slug Containment** | `codex #14`<br>[`pathx.go:21-34, 53-63`](file:///home/matej/HARNESS/nexus/internal/foundation/pathx/pathx.go#L21-L34) | `profileSlugOK` restricts characters to `[a-zA-Z0-9_-]` and length <= 64; `filepath.Rel` validates non-escaping containment. Verified in [`pathx_test.go:35-42`](file:///home/matej/HARNESS/nexus/internal/foundation/pathx/pathx_test.go#L35-L42). | `[OK]` |
| **Pathx EnsureDir Safety Validation** | `codex #15`<br>[`pathx.go:81-102`](file:///home/matej/HARNESS/nexus/internal/foundation/pathx/pathx.go#L81-L102) | `EnsureDir` rejects symlinks, non-directories, open permissions (`mode & 0077 != 0`), and foreign ownership. Verified in [`pathx_test.go:46-67`](file:///home/matej/HARNESS/nexus/internal/foundation/pathx/pathx_test.go#L46-L67). | `[OK]` |
| **Procx Token Guard & Reaping Serialization** | `codex #16`<br>[`procx_linux.go:60-66, 88-138`](file:///home/matej/HARNESS/nexus/internal/foundation/procx/procx_linux.go#L60-L66) | `wait()` is serialized via `sync.Once`; `signalGroup` validates leader `Token.Alive()` before signalling; `Terminate` verifies no surviving group members. Verified in [`procx_linux_test.go:113-143`](file:///home/matej/HARNESS/nexus/internal/foundation/procx/procx_linux_test.go#L113-L143). | `[OK]` |
| **Procx Honest Group-Scope Documentation** | `codex #17`<br>[`procx_linux.go:56-60`](file:///home/matej/HARNESS/nexus/internal/foundation/procx/procx_linux.go#L56-L60) | Package documentation explicitly clarifies that `procx` governs process groups, whereas full tree containment is owned by the bwrap sandbox (T25). | `[OK]` |
| **Closure ProbeResult ConfigHash Binding** | `codex #18`<br>[`closure.go:114-118, 140-174`](file:///home/matej/HARNESS/nexus/internal/kernel/closure/closure.go#L114-L118) | `ProbeResult` carries `ConfigHash`; `Seal` marks capabilities OFF if their probe was measured under a differing config hash. Verified in [`closure_test.go:155-172`](file:///home/matej/HARNESS/nexus/internal/kernel/closure/closure_test.go#L155-L172). | `[OK]` |
| **Closure Duplicate Probe Name Rejection** | `codex #19`, `agy #5`<br>[`closure.go:153-157`](file:///home/matej/HARNESS/nexus/internal/kernel/closure/closure.go#L153-L157) | `Seal` rejects duplicate probe names fail-closed regardless of order. Verified in [`closure_test.go:142-151`](file:///home/matej/HARNESS/nexus/internal/kernel/closure/closure_test.go#L142-L151). | `[OK]` |
| **Comprehensive Spec Matrix REDs** | `codex #20`<br>[`machine_test.go:148-180`](file:///home/matej/HARNESS/nexus/internal/kernel/machine/machine_test.go#L148-L180), [`config_test.go:174-187`](file:///home/matej/HARNESS/nexus/internal/foundation/config/config_test.go#L174-L187) | Exhaustive state transition matrix and all-layer bounds tests implemented. | `[OK]` |

---

## 2. Weakest Points Analysis (Epistemic Honesty)

1. **Lexical Matching in `ProjTx` (`projection.go:23-30`)**:
   `ProjTx.guardStatement` uses a regular expression with word boundaries (`(?i)\b(events|journal_meta|projection_offsets)\b`) to intercept writes to canonical tables. While safe against direct and quoted table references (e.g. `"events"`, `` `events` ``, `[events]`, `main.events`), it is a lexical guard rather than a full SQL parser. As documented, this is the declared topknot ceiling pending the dedicated schema isolation owner in T18.
2. **Unauthenticated Producer Identity in `checker` (`checker.go:79-84`)**:
   `Evidence.Producer` is currently an unauthenticated string attribute compared against `AcceptanceContract.Worker`. While sufficient to prevent accidental self-grading in unit harnesses, full cryptographic provenance and journal-backed receipt verification arrive in T21.
3. **Process Group vs Namespace Boundary in `procx` (`procx_linux.go:56-60`)**:
   `procx` manages process groups via Linux `setpgid`. A grandchild process that explicitly detaches via `setsid` escapes the PGID; strict process tree containment relies on bwrap PID namespaces (`--unshare-pid`) implemented in the sandbox layer (T25).

---

## 3. Verification Evidence

- `go vet ./internal/...`: Clean (exit code 0).
- `go test -count=1 ./internal/...`: All unit, integration, hostile probe, and boundary tests passed across all 15 packages.

---

VERDICT: PASS
