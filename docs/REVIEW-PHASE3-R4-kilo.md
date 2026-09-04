# REVIEW-PHASE3-R4 — round-4 verification (5 codex r3 folds)

Scope: the diff 91179ff..fcb0d6a only + NEW defects it introduced. `CGO_ENABLED=0 go vet ./...`
clean; `CGO_ENABLED=0 go test -count=1 ./...` GREEN.

---

## The folds — all [OK]

**1. [OK] SyncProjection `Version`/`Reset` + catch-up version handling — crash-consistent.**
The interface now carries `Version() int` and `Reset(db *ProjDB) error` (journal.go). `proj_sync_offsets`
gains a `version` column (with a one-shot `ALTER TABLE … ADD COLUMN` for the older table).
`catchupProjections` reads `applied_offset`+`version`; a missing checkpoint OR a version mismatch calls
`sp.Reset` (drops derived tables), reseeds checkpoint 0 + the current version, then `sp.Init`
recreates and the whole stream refolds. `Projection.Version()` is `2` (v1 lacked lineage/checkpoints),
and `TestV1SchemaDatabaseRebuildsAtOpen` proves an older-revision DB is dropped-and-rebuilt rather than
patched blind. The append transaction still commits (event, projection state, checkpoint, version)
atomically, so the crash window holds. ✓

**2. [OK] Journal-owned `RedactorRewrites` seam — no false receipt is possible.**
`Journal.RedactorRewrites(raw)` consults the journal's OWN redactor (journal.go); `NewStore(j)` no
longer takes a redactor, and `refuseSecretContent` now calls `s.j.RedactorRewrites(raw)`
(store.go:296-320). A caller can no longer inject a divergent redactor copy and obtain an
AFTER_COMMIT receipt whose hash describes unrewritten bytes while the projection stored redacted
bytes — the exact-bytes check and the append now share one, journal-owned redactor. ✓

**3. [OK] Supersession lineage union through the production tool.**
`SupersedeLineage(…, lineage)` carries the correction call's chain; `Apply`'s supersede branch reads
the predecessor's stored lineage and merges `oldLineage ∪ predecessor-id ∪ new lineage`, deduped
(store.go). `memory_remember` passes `[]string{c.ToolCallID}` through
`store.SupersedeLineage` (tools.go). `TestSupersessionUnionsLineageThroughTool` proves the leaf
retains source + predecessor + correction provenance. ✓

**4. [OK] The two causal REDs are committed.**
`TestQueryProjectionCannotMutate` (projection_test.go) asserts the former `SELECT;DELETE;SELECT`
probe is rejected with the row count unchanged; `TestToolPromptDeterministic` (planner_test.go)
asserts a repeated byte-identical sorted tool section. Both would fail on the naive implementation. ✓

## NEW defects (all LOW)

**5. [NEW-ERROR, LOW] The `ALTER TABLE … ADD COLUMN version` runs on EVERY Open with its error
discarded** (journal.go). For a fresh or v2 database the column already exists and the ALTER fails
silently every open; for a v1 database it is the migration. Correct today, but an ALTER failure for a
real reason (locked file, permission) is indistinguishable from "already migrated" because the error
is not inspected. A `column-absent` guard (query `PRAGMA table_info`) would make the migration
idempotent-by-check instead of idempotent-by-ignored-error.

**6. [NEW-ERROR, LOW] `RedactorRewrites` is byte-comparison over the redactor's round trip**, so its
"exact-bytes" verdict inherits the redactor's encoding idempotency (Marshal→Redact→Marshal). Holds for
the current `KnownRefs` JSON-string walk, but a future redactor that normalizes encoding could refuse
clean content as if it held a secret. A typed "touches" predicate would be more robust.

---

## Verdict

All five round-3 findings are folded with causal REDs: the projection schema is versioned and
rebuilds from the canonical stream on mismatch, the exact-bytes seam is journal-owned (no injected
redactor copy), supersession lineage is unioned through the production tool, and both requested
regression REDs are committed. The two new observations are low-severity. Full suite green.

VERDICT: PASS
