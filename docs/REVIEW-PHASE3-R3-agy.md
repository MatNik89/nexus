# REVIEW-PHASE3-R3-agy.md — Phase-3 Verification Round 3 (Narrow)

**Target Ref:** `slice/p0-phase3` @ commit `91179fff`  
**Reviewer:** `agy` (Antigravity)  
**Scope:** Diff `c05bb8b..HEAD` ONLY (folding Round-2 findings: Codex 3 unfolded + 4 new-error).

---

## 1. Verification of Round 2 Folds

| # | Item & Source | Verification & Evidence in Diff | Status |
| :- | :--- | :--- | :--- |
| 1 | **Projection Catch-up & Full State Rebuild**<br>`codex #1, #11` | In `journal.applySyncProjections`, `proj_sync_offsets` checkpoint is updated in the SAME transaction as event append and projection apply (`projection.go:108-111`). On `journal.Open`, `catchupProjections` checks `cp < head` and replays all missing canonical events (`journal.go:326-366`). Tested in [`store_test.go:155-175`](file:///home/matej/HARNESS/nexus/internal/memory/store_test.go#L155-L175) and [`facts_test.go:313-349`](file:///home/matej/HARNESS/nexus/internal/memory/facts_test.go#L313-L349) by dropping projection tables + checkpoint and verifying complete reconstruction of active, rejected, and superseded states. | `[OK]` |
| 2 | **`QueryProjection` Single-Statement Enforcement**<br>`codex #14` | `QueryProjection` rejects any query containing `;` (`strings.ContainsRune(query, ';')`) fail-closed (`projection.go:198-200`). Multi-statement queries attempting to inject mutating SQL (e.g. `SELECT; DELETE; SELECT`) are blocked. | `[OK]` |
| 3 | **Secret-Content Refusal (Byte-Exact & Receipt Honesty)**<br>`codex #15` | `refuseSecretContent` checks `s.redactor.Redact(raw)` before appending (`store.go:309-321`). Any content containing known secrets is refused upfront with a clear error, preventing silent redaction mutations that would cause stored bytes to diverge from PEP approvals or commit receipts. Verified in [`facts_test.go:354-389`](file:///home/matej/HARNESS/nexus/internal/memory/facts_test.go#L354-L389). | `[OK]` |
| 4 | **Lineage Persistence & Recall Union**<br>`codex #7` | `Lineage` is stored as a JSON column in `mem_facts` (`store.go:159, 186, 350`). `memory_recall` unions all source lineages from retrieved facts with the current recall tool call ID (`tools.go:122-135`). Verified in [`facts_test.go:412-445`](file:///home/matej/HARNESS/nexus/internal/memory/facts_test.go#L412-L445). | `[OK]` |
| 5 | **Tag LIKE Metacharacter Escaping (`ESCAPE '|'`)**<br>`codex #8` | `RecallTag` uses `strings.NewReplacer("|", "||", "%", "|%", "_", "|_")` and appends `ESCAPE '|'` to the SQL query (`store.go:470-471`). Searching for tag `"%"` returns only literal `"%"` tagged facts, and `"_" ` does not match arbitrary tags. Verified in [`facts_test.go:392-408`](file:///home/matej/HARNESS/nexus/internal/memory/facts_test.go#L392-L408). | `[OK]` |
| 6 | **Deterministic Sorted Tool Prompt**<br>`codex #17` | `ChatPlanner.Plan` extracts tool IDs into a slice and applies `sort.Strings(ids)` before rendering the system prompt (`planner.go:198-205`), eliminating map-iteration nondeterminism. | `[OK]` |
| 7 | **E2E Teardown Ordering & Socket Cleanliness**<br>`codex #16` | `runSession` in `TestMemoryToolSpineSurvivesRestart` cleanly calls `cancel()`, waits on `<-serveDone`, and closes the journal before reusing the socket path in the subsequent session (`main_test.go:154-156`), eliminating test flakiness and socket-reuse races. | `[OK]` |

---

## 2. Seam Judgments & Crash-Window Analysis

1. **Projection Checkpoint Crash-Window Atomicity**:
   - `events` row insertion, `mem_facts` / `mem_facts_fts` projection apply, and `proj_sync_offsets` checkpoint update all execute within the single open `tx *sql.Tx` in `appendOne` (`journal.go:452-501`).
   - If a crash occurs before `tx.Commit()`, all mutations roll back atomically.
   - If a crash occurs after `tx.Commit()`, event, projection tables, and checkpoint are durably persisted together.
   - If the database file suffers partial projection loss or is manually reset for rebuild, `catchupProjections` automatically synchronizes the projection tables to `head` on `journal.Open`.
2. **Lineage Monotonicity**:
   - Source lineage chains are preserved across proposal, acceptance, and supersession (`supersedes` links the predecessor's ID), and are returned in recall observation context blocks.
3. **Secret Boundary Integrity**:
   - Explicit memory writes containing known secret values are rejected before reaching the journal, ensuring stored facts are byte-exact to their preview and commit receipts.

---

## 3. Weakest Points Analysis (Epistemic Honesty)

1. **Semicolon Rejection in `QueryProjection` ([`internal/kernel/journal/projection.go:198-200`](file:///home/matej/HARNESS/nexus/internal/kernel/journal/projection.go#L198-L200))**:
   `strings.ContainsRune(query, ';')` is a simple literal check that unconditionally forbids semicolons in projection queries. While fully preventing multi-statement injection in P0, any future query containing a string literal with a semicolon would fail closed.
2. **Space-Delimited LIKE Tag Search ([`internal/memory/store.go:470-471`](file:///home/matej/HARNESS/nexus/internal/memory/store.go#L470-L471))**:
   `RecallTag` operates via `f.tags LIKE ? ESCAPE '|'` over a space-padded string rather than a relational `fact_tags` junction table. Tag single-token validation (`validate()`) and metacharacter escaping make this sound for P0 limits (`maxQueryLen = 256`, `LIMIT 50`).
3. **Per-Event Transaction Loop in `catchupProjections` ([`internal/kernel/journal/journal.go:347-360`](file:///home/matej/HARNESS/nexus/internal/kernel/journal/journal.go#L347-L360))**:
   Replay catch-up runs one transaction per missing event (`tx.Begin()`, `Apply()`, `UPDATE proj_sync_offsets`, `tx.Commit()`). This guarantees strict incremental crash safety during catch-up, though batching would be needed if replay volume grew to millions of events.

---

## 4. Verification Evidence

- `CGO_ENABLED=0 go vet ./...`: Clean (exit code 0).
- `CGO_ENABLED=0 go test -count=1 ./...`: All 25 packages passed uncached (exit code 0).

---

VERDICT: PASS
