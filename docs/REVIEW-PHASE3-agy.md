# REVIEW-PHASE3 — Phase 3 Deep Review (T18 profiles + T19 explicit memory)

**Target Ref:** `slice/p0-phase3` @ HEAD (`4bab530` over `4cc55d2`)  
**Commits:** `a009034` (T18: per-profile physical isolation) + `4bab530` (T19: explicit memory + daemon tool wiring)  
**Reviewer:** `agy` (Antigravity)  
**Scope:** `internal/memory/{store,tools}.go`, `internal/memory/{store_test,facts_test}.go`, `cmd/nexus/main.go`

---

## Findings

### 1. [HIGH] `memory_remember` produces mutating side effects without a `CommitReceipt`, causing `EffectPath.RunTool` to park `UNKNOWN` (`ErrEffectUnknown`) and abort the turn on non-read-only calls.
- **File:Line:** [`internal/memory/tools.go:33-40, 72-88`](file:///home/matej/HARNESS/nexus/internal/memory/tools.go#L33-L40), [`internal/kernel/effectpath/effectpath.go:244-260, 429-440`](file:///home/matej/HARNESS/nexus/internal/kernel/effectpath/effectpath.go#L244-L260)
- **Concrete Failure Scenario:** `memory_remember` is a state-mutating tool that commits facts to the profile SQLite database via `store.SaveFact`. Under Annex P0.2 / K2 / HARDQ E9, an effectful tool call is dispatched with `c.Effect = contracts.EffectReversible` (or `EffectIrreversible`). When `RunTool` executes `memory_remember`, `textResult` returns a `contracts.ToolResult` with `Commit: nil`. In `effectpath.ClassifyEffectPhase`, any non-`EffectReadOnly` call lacking a valid `CommitReceipt` evaluates to `PhaseUnknown`. Consequently, `RunTool` classifies the execution as `OutcomeUnknown` with cause `"effectpath: effectful result carries no valid commit receipt"` and returns `ErrEffectUnknown`. `loop.RunTurn` halts immediately on `ErrEffectUnknown`. The fact is persisted in SQLite, but the conversation turn crashes in `UNKNOWN` state pending reconciliation.
- **Fix:** Attach a valid `contracts.CommitReceipt` (`Phase: contracts.PhaseAfterCommit`, `ToolCallID: c.ToolCallID`, `AttemptNo: c.AttemptNo`, `ContentHash: ...`) to `contracts.ToolResult` upon successful SQLite commit in `memory_remember`, or provide a helper for in-process mutating tools.

---

### 2. [MED] Unconstrained multi-successor branching in `Store.Supersede` allows superseding already-superseded facts, corrupting linear recall history.
- **File:Line:** [`internal/memory/store.go:176-190`](file:///home/matej/HARNESS/nexus/internal/memory/store.go#L176-L190)
- **Concrete Failure Scenario:** When fact `f1` ("my keys are on the desk") is corrected by `f2` ("my keys are in the drawer"), `f1` remains in state `StatusAccepted` with `f2.supersedes = 'f1'`. If a subsequent call invokes `Supersede("f1", "f3", "my keys are in the car")`, `Supersede` only validates `SELECT status FROM facts WHERE id='f1'` and does not check whether `f1` already has a successor (`NOT EXISTS (SELECT 1 FROM facts WHERE supersedes = 'f1')`). Because both `f2` and `f3` now have no successors, `Recall("keys")` executes `WHERE ... NOT EXISTS (SELECT 1 FROM facts n WHERE n.supersedes = f.id AND n.status = 'accepted')` and returns **both** `f2` and `f3`. This produces an unconstrained diamond/forked history rather than linear append-only supersession.
- **Fix:** In `Supersede`, enforce that `oldID` is the current leaf of the supersession chain (i.e. `SELECT 1 FROM facts WHERE supersedes = ?` returns no rows) before permitting supersession.

---

### 3. [MED] SQLite connection resource leak in `buildDaemon` / `cmd/nexus/main.go` — `memStore` is opened but never closed on shutdown or initialization failure.
- **File:Line:** [`cmd/nexus/main.go:109-125, 162-187`](file:///home/matej/HARNESS/nexus/cmd/nexus/main.go#L109-L125)
- **Concrete Failure Scenario:** `buildDaemon` opens `memStore, err := memory.Open(filepath.Join(profileDir, "memory.db"), profile)` and supplies `memory.Tools(memStore)` to `daemon.Deps`. However, `buildDaemon` returns only `(*daemon.Daemon, *journal.Journal, error)`. `memStore` is not returned, and `daemon.Daemon` owns no reference or teardown hook for it. In `runDaemon()`, `defer j.Close()` is registered, but `memStore.Close()` is never invoked when the daemon process receives SIGINT/SIGTERM. Furthermore, if `daemon.New` fails at line 182, `j.Close()` is called while `memStore.Close()` is omitted.
- **Fix:** Manage `memStore` lifecycle in the daemon composition root (e.g. return a cleanup func or track closers in `buildDaemon` / `Daemon`).

---

### 4. [LOW] Zero test coverage for `internal/memory/tools.go` (`Tools` and `Rules`).
- **File:Line:** [`internal/memory/tools.go:24-88`](file:///home/matej/HARNESS/nexus/internal/memory/tools.go#L24-L88)
- **Concrete Failure Scenario:** While `internal/memory/store_test.go` and `facts_test.go` test `Store` methods directly, `Tools(store)` and `Rules()` have no tests in the entire repository. Argument parsing (unmarshaling, empty `content` / empty `query`), result formatting (`- fact1\n- fact2\n` vs `"no facts found"`), `ContextBlock` metadata construction (`TrustToolTrusted`, `SensitivityInternal`), and PEP rule binding (`memory_remember: DecisionAsk`, `memory_recall: DecisionAllow`) are completely unexercised by the test suite.
- **Fix:** Add `internal/memory/tools_test.go` testing both tools across valid/invalid inputs, output formatting, and integration with `EffectPath.RunTool`.

---

### 5. [LOW] `Search` returns stale superseded facts, diverging from `Recall`.
- **File:Line:** [`internal/memory/store.go:216-231`](file:///home/matej/HARNESS/nexus/internal/memory/store.go#L216-L231)
- **Concrete Failure Scenario:** `Recall` filters out superseded facts via `AND NOT EXISTS (SELECT 1 FROM facts n WHERE n.supersedes = f.id AND n.status = 'accepted')`. In contrast, `Search` only filters `AND f.status = 'accepted'`. When `Search` is called on a query matching superseded content, it returns both historical/stale versions and current versions.
- **Fix:** If `Search` is intended strictly as a historical audit search, document the semantic difference in the docstring; otherwise, align `Search` with `Recall`'s latest-version filter.

---

## 2. Standards & Spec Verification

- **B3 Physical Isolation:**
  - One SQLite file per profile under its own subtree (`profiles/<profile>/memory.db`) verified.
  - Profile binding is persisted in `store_meta` and verified on `Open`. A file stamped for another profile is rejected fail-closed (`TestProfileStampImmutableAcrossRestart`).
  - Rows are stamped with `s.profile` and callers cannot forge the stamp (`TestCallerCannotForgeProfileOnRow`).
  - FTS virtual table resides inside the profile-specific database file; cross-profile leakage is physically impossible.
- **B8 Explicit Memory & Supersession Semantics:**
  - `Propose` -> preview is exact content. Unaccepted facts are not recallable (`TestRememberPreviewAndApprovalGate`).
  - `Reject` moves status to `rejected`. Rejected inferences remain in the DB (append-only) but are excluded from `Recall` and `Search` (`TestRejectedInferenceNotRecallable`).
  - No decay: retrieval order is recency-based (`rowid DESC`) with no age/half-life filtering (`TestRecallRecencyOrder`).
- **FTS Query Quoting:**
  - `phrase := `"` + strings.ReplaceAll(q, `"`, `""`) + `"` properly escapes double quotes for SQLite FTS5 phrase literals, preventing FTS syntax injection.
- **RED Causality:**
  - All T18/T19 RED names in `docs/tasks-P0.md` exist and are causal.

---

VERDICT: FAIL
