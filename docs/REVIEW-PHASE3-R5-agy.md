# REVIEW-PHASE3-R5-agy.md — Phase-3 Verification Round 5 (Final Narrow)

**Target Ref:** `slice/p0-phase3` @ commit `061b4cf`  
**Reviewer:** `agy` (Antigravity)  
**Scope:** Diff `fcb0d6a..061b4cf` ONLY (folding Round-4 findings: Codex 2 unfolded).

---

## 1. Verification of Round 4 Folds

| # | Item & Source | Verification & Evidence in Diff | Status |
| :- | :--- | :--- | :--- |
| 1 | **Whole-Payload Secret Gate in `Store.append`**<br>`codex #4` | `Store.append` marshals the entire event payload and passes it to `s.j.RedactorRewrites(raw)` before appending (`store.go:355-368`), covering `content`, `tags`, `ids`, and `lineage`. Secret-smuggling through tags is refused fail-closed. Causal RED in [`facts_test.go:381-388`](file:///home/matej/HARNESS/nexus/internal/memory/facts_test.go#L381-L388) (`TestSecretContentRefusedNotRewritten`) asserts that placing a known secret in `tags` is refused and leaves zero rows behind. | `[OK]` |
| 2 | **Semantic Value Comparison in `RedactorRewrites`**<br>`codex #4` | `RedactorRewrites` (`journal.go:540-554`) unmarshals `raw` and `red` and evaluates `!reflect.DeepEqual(a, b)`. If any string value (key, content, tag, lineage) is mutated by redaction, `DeepEqual` detects the modification while ignoring non-semantic JSON serialization formatting differences. | `[OK]` |
| 3 | **True Previous-Revision Checkpoint Schema in Migration RED**<br>`codex #2` | `TestV1SchemaDatabaseRebuildsAtOpen` (`facts_test.go:458-479`) drops `proj_sync_offsets` and recreates the true previous-revision 2-column table `(name, applied_offset)` without a `version` column. Reopening the journal executes `ALTER TABLE proj_sync_offsets ADD COLUMN version ...` (`journal.go:326`), detects `version=0 != 2`, resets `mem_facts`, and rebuilds the full projection from canonical events. | `[OK]` |

---

## 2. Seam & Invariant Judgments

1. **`RedactorRewrites` Semantic Comparison & Receipt Honesty**:
   - `json.Unmarshal` into `any` parses arbitrary JSON into native Go data structures (`map[string]any`, `[]any`, `string`, `float64`).
   - When redaction occurs, string values containing secrets are replaced (e.g. `"sk-TAG-SECRET"` → `"[REDACTED:KEY]"`). `reflect.DeepEqual` performs a deep recursive equality check, guaranteeing that any changed string value triggers a rewrite verdict (`!reflect.DeepEqual(a, b) == true`).
   - No secret value in any payload field can slip past the gate, ensuring that any returned `AFTER_COMMIT` commit receipt corresponds to byte-exact, unredacted database state.
2. **Causality of Migration RED**:
   - The fixture builds the actual two-column table `proj_sync_offsets` and a `mem_facts` table lacking `lineage`.
   - Removing the `ALTER TABLE ... ADD COLUMN version` migration from `journal.go:326` causes `SELECT applied_offset, version FROM proj_sync_offsets` to fail immediately with `no such column: version`, proving the RED is strictly causal and sensitive to the migration logic.

---

## 3. Weakest Points Analysis (Epistemic Honesty)

1. **Double JSON Serialization on Fact Writes ([`internal/memory/store.go:355, 369`](file:///home/matej/HARNESS/nexus/internal/memory/store.go#L355))**:
   `Store.append` serializes `payload` into `raw` JSON to evaluate `RedactorRewrites`, and `journal.Append` later unmarshals/validates the envelope payload. Safe and minimal for P0 memory write volume, but introduces redundant serialization.
2. **Fallback on Malformed JSON in `RedactorRewrites` ([`internal/kernel/journal/journal.go:550-552`](file:///home/matej/HARNESS/nexus/internal/kernel/journal/journal.go#L550-L552))**:
   If either `raw` or `red` fails `json.Unmarshal`, `RedactorRewrites` treats any byte difference as a rewrite (`return true, nil`). Fail-closed and safe for typed JSON events in P0.
3. **Ignored Error on Idempotent Schema Migration ([`internal/kernel/journal/journal.go:326`](file:///home/matej/HARNESS/nexus/internal/kernel/journal/journal.go#L326))**:
   `j.db.Exec("ALTER TABLE proj_sync_offsets ADD COLUMN version ...")` runs unconditionally on Open and discards the SQLite duplicate-column error on subsequent opens.

---

## 4. Verification Evidence

- `CGO_ENABLED=0 go vet ./...`: Clean (exit code 0).
- `CGO_ENABLED=0 go test -count=1 ./...`: All 25 packages passed uncached (exit code 0).

---

VERDICT: PASS
