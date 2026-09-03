# REVIEW-PHASE1B-agy.md — Phase 1 Batch Code Review (T07–T12)

**Target Ref:** `slice/p0-phase1` @ HEAD (`e94e97e`)  
**Reviewer:** `agy` (Antigravity)  
**Scope:** Adversarial batch review of tasks T07 through T12:
- T07: `internal/kernel/journal/projection.go`, `projection_test.go`
- T08: `internal/kernel/machine/machine.go`, `machine_test.go`
- T09: `internal/foundation/config/config.go`, `config_test.go`
- T10: `internal/foundation/pathx/pathx.go`, `internal/foundation/procx/procx_linux.go`, `procx_linux_test.go`
- T11: `internal/kernel/closure/closure.go`, `closure_test.go`
- T12: `internal/kernel/checker/checker.go`, `checker_test.go`

---

## 1. Executive Summary

Phase 1B implements the synchronous projection harness (T07), state machine transition tables (T08), typed configuration resolver (T09), path/process identity (T10), sealed capability snapshot (T11), and minimal deterministic checker (T12).

While the foundational mechanics (such as same-transaction sync projections, process group termination, and topological capability resolution) are cleanly structured and pass basic test suites, adversarial analysis identified **2 `[BREAK]`**, **1 `[SEC]`**, and **2 `[QUALITY]`** defects:
1. State machines in T08 invent artificial event names (`run.cancelled.admitted`, `run.cancelled.running`, etc.) instead of mapping canonical cancellation events (`run.cancelled`) across all eligible source states.
2. The deterministic checker in T12 uses an early-return first-match loop that fails bundles containing multiple exit or file-hash evidences if a non-matching evidence item appears before a matching one.
3. The config resolver in T09 does not trim whitespace on comma-split `egress_allow` entries and fails open on unknown `NEXUS_CFG_*` environment variables.

---

## 2. Findings Ledger

### 1. [BREAK] Invented cancellation event types break canonical state transitions (`machine.go:104-106, 135-136, 167-169`)
- **Location:** [`internal/kernel/machine/machine.go:104-106`](file:///home/matej/HARNESS/nexus/internal/kernel/machine/machine.go#L104-L106), [`135-136`](file:///home/matej/HARNESS/nexus/internal/kernel/machine/machine.go#L135-L136), [`167-169`](file:///home/matej/HARNESS/nexus/internal/kernel/machine/machine.go#L167-L169)
- **Description:** `RunTable`, `TurnTable`, and `AttemptTable` define non-canonical invented event names (`"run.cancelled.admitted"`, `"run.cancelled.running"`, `"turn.cancelled.running"`, `"attempt.cancelled.authorized"`, `"attempt.cancelled.running"`) to represent cancellation from non-initial states. In the architecture (Annex P0.1, line 1380), the domain event is `run.cancelled` (`EvRunCancelled`). Because `NewTable` structures edges as `map[Event]map[From]To`, `NewTable` already supports multiple source states for the same event type.
- **Impact:** When downstream components (such as the turn loop in T16 or HITL cancel in T24) emit the canonical event `run.cancelled` against a run in state `ADMITTED` or `RUNNING`, `RunTable().Step(RunRunning, EvRunCancelled)` fails closed with an illegal transition error.
- **Fix:** Map `EvRunCancelled` from `RunCreated`, `RunAdmitted`, and `RunRunning` to `RunCancelled`; map `EvTurnCancelled` from `TurnCreated` and `TurnRunning` to `TurnCancelled`; and map `EvAttemptCancelled` from `AttemptPlanned`, `AttemptAuthorized`, and `AttemptRunning` to `AttemptCancelled`. Remove the `.admitted`, `.authorized`, and `.running` event suffix aliases.

---

### 2. [BREAK] First-match early return in `gradeOne` rejects valid evidence bundles (`checker.go:151-170`)
- **Location:** [`internal/kernel/checker/checker.go:151-170`](file:///home/matej/HARNESS/nexus/internal/kernel/checker/checker.go#L151-L170)
- **Description:** In `gradeOne`, when evaluating `ExitCodeIs` or `FileHashIs`, the loop over `bundle` immediately terminates and returns `fail(...)` upon encountering the *first* evidence item of that type whose value does not match:
  ```go
  case c.ExitCodeIs != nil:
      for _, e := range bundle {
          if e.Exit != nil {
              if e.Exit.Code == *c.ExitCodeIs {
                  return CriterionResult{Index: idx, Pass: true}
              }
              return fail(fmt.Sprintf("exit code %d, want %d", e.Exit.Code, *c.ExitCodeIs))
          }
      }
  ```
- **Impact:** If an evidence bundle contains multiple exit evidences (for instance, an intermediate command failure followed by a successful retry) or multiple file-hash evidences for the same path, the grading outcome depends strictly on bundle order. If a non-matching evidence entry appears first, `Grade` returns `FAIL` even if a valid matching evidence item is present later in the bundle.
- **Fix:** Only return `fail(...)` after scanning the entire bundle if no matching evidence was found, or require explicit correlation IDs on `ExitEvidence`.

---

### 3. [SEC] Untrimmed comma splitting on `egress_allow` creates malformed host entries (`config.go:157-162, 172-178`)
- **Location:** [`internal/foundation/config/config.go:157-162`](file:///home/matej/HARNESS/nexus/internal/foundation/config/config.go#L157-L162), [`172-178`](file:///home/matej/HARNESS/nexus/internal/foundation/config/config.go#L172-L178)
- **Description:** In `applyKey`, `egress_allow` is parsed via `strings.Split(val, ",")` without trimming whitespace from individual tokens.
- **Impact:** A CLI override such as `-c egress_allow="api.openai.com, api.telegram.org"` populates `Config.EgressAllow` with `[]string{"api.openai.com", " api.telegram.org"}` (retaining the leading space). This un-trimmed host bypasses `ValidateBounds` checks and subsequently breaks exact hostname matching in sandbox egress enforcement. Empty tokens (e.g. from trailing or double commas) also create empty string entries.
- **Fix:** Trim whitespace from each token when splitting `egress_allow` and discard empty tokens.

---

### 4. [QUALITY] `envLayer` fails open on unknown `NEXUS_CFG_*` variables (`config.go:119-127`)
- **Location:** [`internal/foundation/config/config.go:119-127`](file:///home/matej/HARNESS/nexus/internal/foundation/config/config.go#L119-L127)
- **Description:** `parseFileLayer` and `cliLayer` strictly enforce `knownKeys` and fail closed on unknown keys. In contrast, `envLayer` only probes `NEXUS_CFG_` + `strings.ToUpper(k)` for `k in knownKeys`.
- **Impact:** If an operator makes a typo in an environment variable name (e.g., `NEXUS_CFG_PROVIDER_MOEL="gpt-4o"`), it is silently ignored rather than rejected, violating the fail-closed configuration principle (E11).
- **Fix:** Allow `envLayer` to inspect all `NEXUS_CFG_*` environment variables and reject unknown keys.

---

### 5. [QUALITY] `Seal` silently overwrites duplicate probe results (`closure.go:142-145`)
- **Location:** [`internal/kernel/closure/closure.go:142-145`](file:///home/matej/HARNESS/nexus/internal/kernel/closure/closure.go#L142-L145)
- **Description:** `probeOK := map[string]ProbeResult{}` in `Seal` iterates through `probes []ProbeResult` and assigns `probeOK[p.Name] = p` without checking for duplicates.
- **Impact:** If the caller supplies duplicate results for the same probe name with conflicting outcomes (e.g. `Passed: false` followed by `Passed: true`), the later result silently overwrites the failure.
- **Fix:** Check for duplicate probe names in `probes` and fail closed if a duplicate or conflicting probe result is passed.

---

## 3. Verified Sound Implementations

1. **T07 Synchronous Projections (`projection.go:20-38`, `journal.go:424-430`) `[OK]`**:
   `applySyncProjections` runs inside the open SQLite transaction of `appendOne` after the event insert and before commit. If any sync projection fails, `tx.Rollback()` rolls back both the event and derived state atomically.
2. **T07 Async Projector (`projection.go:44-107`) `[OK]`**:
   `Projector.Run` utilizes an atomic CAS (`UPDATE projection_offsets SET current_offset=? WHERE name=? AND current_offset=?`) inside each event's transaction, ensuring monotonic at-least-once replay and detecting concurrent worker collisions.
3. **T10 Path Layout & Process Identity (`pathx.go:16-55`, `procx_linux.go:19-91`) `[OK]`**:
   `pathx` enforces `0700` private directories and separate per-profile database paths under `~/.config/nexus/profiles/<profile>/journal.db`. `procx` launches child processes in their own process group (`Setpgid: true`) and escalates termination from `SIGTERM` to `SIGKILL` with `StartTime` instance token validation.
4. **T11 Capability Dependency Resolution (`closure.go:37-105`) `[OK]`**:
   `Resolve` performs three-color cycle detection, transitive closure calculation, and conflict verification, producing a deterministic topological order for capability activation.

---

## 4. Verification Evidence

- `go vet ./internal/...`: Clean (exit code 0).
- `go test -v -count=1 ./internal/...`: All 62 tests passed across 10 packages (including new T07–T12 unit tests).

---

VERDICT: FAIL
