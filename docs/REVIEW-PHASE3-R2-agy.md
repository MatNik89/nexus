# REVIEW-PHASE3-R2-agy.md — Phase-3 Verification Round 2

**Target Ref:** `slice/p0-phase3` @ commit `c05bb8b`  
**Reviewer:** `agy` (Antigravity)  
**Input Ledger:** Fold verification of Round 1 findings across Codex (5 HIGH, 6 MED, 1 LOW), Kilo (2 MED), and Agy (1 HIGH, 2 MED).

---

## 1. Verification of Round 1 Folds

| # | Finding & Source | Resolution & Code Location | Status |
| :- | :--- | :--- | :--- |
| 1 | **Memory as Sync Journal Projection in Single DB**<br>`codex #1, #12`, `agy #3` | The separate `memory.db` file is removed. Memory is now a `SyncProjection` (`internal/memory/store.go:137-265`) registered into `journal.Open` (`cmd/nexus/main.go:150-165`). Fact mutations append canonical journal events (`EvFactSaved/Proposed/Accepted/Rejected/Superseded`) folded into `mem_facts` and `mem_facts_fts` in the SAME transaction. Journal replay reproduces all facts (`store_test.go:155-160`). Store lifecycle is owned by the journal (`j.Close()` cleans up everything). | `[OK]` |
| 2 | **Tool Reachability & Sealed Planner Tool-Decoding**<br>`codex #2, #3`, `kilo #2` | `ChatPlanner.WithTools` enables tool decoding (`planner.go:74-85`). `toolCallFromReply` strictly decodes whole-reply JSON `{action:"tool",...}`, verifies closed `ToolID`, and builds `ToolCall` parameters from the sealed `ToolSpec` registry (`EffectReversible`, `ExecInProcess`, `ArgsSchemaHash`) and stamps with the session `ProfileID` (`planner.go:140-184`). `memory_remember` exposes the `supersedes` argument for user-visible corrections (`tools.go:65, 73-77`). | `[OK]` |
| 3 | **`memory_remember` CommitReceipt**<br>`codex #3`, `agy #1` | `memory_remember` returns a valid `contracts.CommitReceipt` (`PhaseAfterCommit`, `ToolCallID`, `AttemptNo`, `ContentHash`) upon successful commit (`tools.go:88-93`), satisfying `EffectPath.RunTool` receipt validation for effectful calls. Verified in [`facts_test.go:295-308`](file:///home/matej/HARNESS/nexus/internal/memory/facts_test.go#L295-L308). | `[OK]` |
| 4 | **Tool Profile Binding & Cross-Profile Refusal**<br>`codex #4` | Both `memory_remember` and `memory_recall` check `c.ProfileID == store.Profile()` and refuse mismatched profiles fail-closed (`tools.go:52-56`). Verified in [`facts_test.go:278-293`](file:///home/matej/HARNESS/nexus/internal/memory/facts_test.go#L278-L293). | `[OK]` |
| 5 | **Strictly Linear Supersession (Diamond Defense)**<br>`codex #5`, `kilo #1`, `agy #2` | `mem_facts` has a `UNIQUE INDEX ux_mem_supersedes ON mem_facts(supersedes)` (`store.go:157-158`), and `Apply(EvFactSuperseded)` performs an in-transaction leaf check (`status == StatusAccepted` AND `hasSuccessor == false`) (`store.go:236-261`). Verified in [`facts_test.go:90-96`](file:///home/matej/HARNESS/nexus/internal/memory/facts_test.go#L90-L96) (diamond refused). | `[OK]` |
| 6 | **Shared Latest-Accepted Retrieval Predicate**<br>`codex #6` | `latestAccepted` constant is shared across `Recall`, `Search`, `RecallTag`, and `Exact` (`store.go:352-356, 401-430`). Verified in [`facts_test.go:123-155`](file:///home/matej/HARNESS/nexus/internal/memory/facts_test.go#L123-L155) (stale content excluded across all surfaces). | `[OK]` |
| 7 | **Monotone Trust & Known-Ref Secret Redaction**<br>`codex #7` | Fact rows persist `Trust` and `Sensitivity`. Inferred facts default to `TrustUntrustedExternal`. `memory_recall` downgrades observation trust to the least trusted hit and applies `redactText(r, ...)` (`tools.go:117-132`). Verified in [`facts_test.go:244-277`](file:///home/matej/HARNESS/nexus/internal/memory/facts_test.go#L244-L277). | `[OK]` |
| 8 | **Tag & Byte-Exact Retrieval**<br>`codex #8` | `RecallTag` (token-bounded tag search) and `Exact` (byte-identical match) added to `Store` (`store.go:416-430`). Verified in [`facts_test.go:159-183`](file:///home/matej/HARNESS/nexus/internal/memory/facts_test.go#L159-L183). | `[OK]` |
| 9 | **B3 Layout Containment & Negative Enumeration**<br>`codex #9, #11` | Profile databases live exclusively under `profiles/<profile>/journal.db`. `TestPhysicalIsolationLayout` enumerates `os.ReadDir(layout.SystemDir())` to prove no `.db` files exist in the system directory (`store_test.go:102-118`). | `[OK]` |
| 10 | **Context-Aware Queries & Work Bounds**<br>`codex #10` | Store retrieval methods take `ctx context.Context`, enforce a 256-byte query cap via `checkQuery`, and limit results with `LIMIT 50` (`store.go:358-397`). | `[OK]` |
| 11 | **Full Memory Spine E2E with Daemon Restart**<br>`codex #2` | Added [`TestMemoryToolSpineSurvivesRestart`](file:///home/matej/HARNESS/nexus/cmd/nexus/main_test.go#L91) driving `repl.Run` → `UDS` → `daemon` → `loop` → `planner` (decodes `memory_remember`) → `PEP` (`ALLOWED_BY_YOLO`) → `journal.Append` → `Projection.Apply` → `CommitReceipt` → daemon restart → `memory_recall` query → verified output (`main_test.go:91-167`). | `[OK]` |
| 12 | **Memory Tools & Rules Test Coverage**<br>`agy #4` | Memory tools and rules are comprehensively covered across valid/invalid inputs, profile guards, monotone trust, and receipt generation in `facts_test.go:244-308` and `planner_test.go`. | `[OK]` |

---

## 2. Adjudication of Key Seam Judgments

1. **Memory as Sync Journal Projection in the Single Profile DB**:
   - Satisfies P0.3 (journal as single write owner and canonical history) and B3 (single DB file per profile). Replaying journal events reconstructs the projection tables from scratch.
   - The `QueryProjection` read seam on `Journal` enforces SELECT-only queries and lexically blocks access to canonical tables (`\b(events|journal_meta|projection_offsets)\b`).
2. **Planner Tool-Decoding & Model Containment**:
   - Whole-reply JSON decoding via `json.NewDecoder` requires `action == "tool"`, EOF after token (no trailing prose), and a known `tool_id`.
   - `ToolCall` parameters are populated from the sealed `ToolSpec` registry in Go, and stamped with the session `ProfileID`. The model cannot forge effect classes, execution kinds, schema hashes, or profile IDs.
3. **Strictly Linear Supersession**:
   - Leaf validation runs inside the journal append transaction, serialized by the single append actor, and backed by a SQLite unique index on `supersedes`. Diamond branching is structurally impossible.
4. **E2E Memory Spine Integration**:
   - `TestMemoryToolSpineSurvivesRestart` covers the entire production composition root through daemon restart.
5. **Declared Ceilings**:
   - *Buffered `Chat` with tools enabled:* Token-level streaming alongside tool calling belongs to the multi-tool P1 planner.
   - *Interactive approval UX:* Surfaces via `NEEDS_APPROVAL` until the T24 durable HITL task.
   - Both ceilings are sound and explicitly documented with upgrade triggers.

---

## 3. Weakest Points Analysis (Epistemic Honesty)

1. **Lexical Table-Guard in `QueryProjection` ([`internal/kernel/journal/projection.go:26-33, 184-193`](file:///home/matej/HARNESS/nexus/internal/kernel/journal/projection.go#L26-L33))**:
   `guardStatement` matches canonical table names via regex rather than a full SQL AST parser; safe for P0 internal sync projections but remains an explicit topknot ceiling.
2. **Buffered Planner Execution with Tools ([`internal/llm/planner/planner.go:70-74, 211-233`](file:///home/matej/HARNESS/nexus/internal/llm/planner/planner.go#L70-L74))**:
   When tools are enabled, the planner buffers provider output rather than streaming tokens to avoid leaking raw tool-call JSON to the client terminal.
3. **Interactive Approval UX Floor ([`internal/memory/tools.go:33-36`](file:///home/matej/HARNESS/nexus/internal/memory/tools.go#L33-L36))**:
   `memory_remember` approval surfaces via the `NEEDS_APPROVAL` turn termination until T24 implements durable `TurnSuspended` HITL rehydration.

---

## 4. Verification Evidence

- `CGO_ENABLED=0 go vet ./...`: Clean (exit code 0).
- `CGO_ENABLED=0 go test -count=1 ./...`: All 25 packages passed uncached (exit code 0).

---

VERDICT: PASS
