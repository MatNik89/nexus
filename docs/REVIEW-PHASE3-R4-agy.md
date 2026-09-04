# REVIEW-PHASE3-R4-agy.md — Phase-3 Verification Round 4 (Narrow)

**Target Ref:** `slice/p0-phase3` @ commit `fcb0d6a`  
**Reviewer:** `agy` (Antigravity)  
**Scope:** Diff `91179ff..fcb0d6a` ONLY (folding Round-3 findings: Codex 2 new-error + 3 unfolded).

---

## 1. Verification of Round 3 Folds

| # | Item & Source | Verification & Evidence in Diff | Status |
| :- | :--- | :--- | :--- |
| 1 | **Projection Schema Owner (`Version`/`Reset`) & Upgrade/Rebuild**<br>`codex #2` | `SyncProjection` interface now includes `Version() int` and `Reset(db *ProjDB) error` (`projection.go:95-102`). `catchupProjections` stores `(name, applied_offset, version)` in `proj_sync_offsets` (`journal.go:318-365`). If a projection version changes or its checkpoint is missing, it resets derived tables and rebuilds from offset 0 from the canonical stream. Tested in [`facts_test.go:449-495`](file:///home/matej/HARNESS/nexus/internal/memory/facts_test.go#L449-L495) (`TestV1SchemaDatabaseRebuildsAtOpen`) covering both previous-revision schema upgrades (missing column / old version) and checkpoint-only corruption rebuilds. | `[OK]` |
| 2 | **Journal-Owned Exact-Bytes Seam (`RedactorRewrites`)**<br>`codex #6` | `NewStore(j)` no longer accepts an independently injectable redactor (`store.go:343-348`). `refuseSecretContent` calls `s.j.RedactorRewrites(raw)` on the journal itself (`store.go:351-363`, `journal.go:538-544`). No caller can supply a mismatched redactor or get a commit receipt for bytes that journal redaction rewrote. | `[OK]` |
| 3 | **Supersession Lineage Union through Production Tool**<br>`codex #7` | `Projection.Apply(EvFactSuperseded)` merges `oldLineage ∪ [oldID] ∪ new.Lineage` inside the append transaction (`store.go:287-315`). `memory_remember` supplies its `ToolCallID` via `store.SupersedeLineage` (`tools.go:77`). Verified in [`facts_test.go:498-531`](file:///home/matej/HARNESS/nexus/internal/memory/facts_test.go#L498-L531) (`TestSupersessionUnionsLineageThroughTool`) confirming `source-block ∪ f-orig ∪ correction-call` all survive into recall. | `[OK]` |
| 4 | **`QueryProjection` Mutation Probe Causal RED**<br>`codex #4` | Added [`TestQueryProjectionCannotMutate`](file:///home/matej/HARNESS/nexus/internal/kernel/journal/projection_test.go#L369-L399) testing multi-statement (`SELECT; DELETE; SELECT`), plain `DELETE`, `INSERT`, and canonical `SELECT`. All probes return `NON_CANONICAL_WRITE`, row count remains unchanged at 2, and single-statement read succeeds. | `[OK]` |
| 5 | **Deterministic Tool Prompt Causal RED**<br>`codex #10` | Added [`TestToolPromptDeterministic`](file:///home/matej/HARNESS/nexus/internal/llm/planner/planner_test.go#L299-L332) executing 8 iterations across fresh spec maps. Verifies exact prompt byte equality (`len(prompts) == 1`) and asserts alphabetical sorting of tool descriptions. | `[OK]` |

---

## 2. Seam Judgments & Crash-Window Analysis

1. **Projection Versioning & Crash-Safety**:
   - `(event, projection, checkpoint)` mutations commit atomically within the single `tx *sql.Tx` in `appendOne` (`journal.go:452-502`, `projection.go:108-113`).
   - Projection version bumps or schema resets trigger a full deterministic rebuild from the canonical event stream on `journal.Open`, avoiding partial/blind table migrations.
2. **Provenance & Secret Boundaries**:
   - Lineage preservation is transitive across arbitrary supersession depths (`oldLineage ∪ oldID ∪ newCallID`).
   - Journal-owned redaction check guarantees that only byte-exact, unredacted facts receive `AFTER_COMMIT` commit receipts.

---

## 3. Weakest Points Analysis (Epistemic Honesty)

1. **Unchecked Legacy Column Upgrade Query ([`internal/kernel/journal/journal.go:326`](file:///home/matej/HARNESS/nexus/internal/kernel/journal/journal.go#L326))**:
   `j.db.Exec("ALTER TABLE proj_sync_offsets ADD COLUMN version ...")` runs unconditionally on Open to migrate pre-versioned tables. In SQLite, the resulting duplicate column error on subsequent opens is discarded; safe because the table schema is verified by subsequent queries, but relies on silent error discarding.
2. **Silent Unmarshal Fallback in Supersession Lineage ([`internal/memory/store.go:304-307`](file:///home/matej/HARNESS/nexus/internal/memory/store.go#L304-L307))**:
   `json.Unmarshal([]byte(oldLineageRaw), &oldLineage)` in `Apply(EvFactSuperseded)` ignores JSON unmarshal errors, defaulting to an empty slice if the old lineage string was corrupted.
3. **Literal Semicolon Ban ([`internal/kernel/journal/projection.go:198-200`](file:///home/matej/HARNESS/nexus/internal/kernel/journal/projection.go#L198-L200))**:
   `strings.ContainsRune(query, ';')` unconditionally rejects semicolons anywhere in projection queries, permanently blocking multi-statement attacks in P0 at the cost of disallowing semicolons in string constants.

---

## 4. Verification Evidence

- `CGO_ENABLED=0 go vet ./...`: Clean (exit code 0).
- `CGO_ENABLED=0 go test -count=1 ./...`: All 25 packages passed uncached (exit code 0).

---

VERDICT: PASS
