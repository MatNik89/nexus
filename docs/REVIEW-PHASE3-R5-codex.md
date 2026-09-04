# Phase 3 verification round 5 — Codex

Target: `slice/p0-phase3` at `061b4cf7e027ac3acb380dd5d6cb6e8f06f77f88`; reviewed only `fcb0d6a..061b4cf`.

Verification evidence:

- `CGO_ENABLED=0 go vet ./... && CGO_ENABLED=0 go test -count=1 ./...` — PASS.
- `CGO_ENABLED=0 go test -count=20 -run '^(TestSecretContentRefusedNotRewritten|TestV1SchemaDatabaseRebuildsAtOpen)$' ./internal/memory` — PASS.
- Clean-archive controlled ablations independently removed the whole-payload gate, the checkpoint `ALTER`, and the version comparison; the corresponding RED failed for the intended reason in every case.

1. **[OK] The exact-bytes gate now covers the complete memory event payload at its single append owner.** `Store.append` first marshals the typed saved/proposed/decision/supersession payload, asks the journal-owned redactor whether that exact value would change, and refuses before `Journal.Append` on either a rewrite or redactor error (`internal/memory/store.go:350-381`). This covers content, tags, fact IDs, supersession IDs, and lineage. The production tool returns an AFTER_COMMIT receipt only after this shared append returns nil (`internal/memory/tools.go:71-93`), so the former secret-tag path cannot earn a receipt.

2. **[OK] `RedactorRewrites` semantic comparison does not miss a real rewrite in the supported P0 path.** Byte comparison alone would falsely classify JSON key-order/format normalization as redaction, so the method compares decoded values when both inputs are JSON and treats changed non-JSON bytes or decoding failure as rewritten (`internal/kernel/journal/journal.go:535-554`). The only production implementations are `KnownRefs`, which changes decoded string values, and `None`; memory supplies uniquely keyed, typed `json.Marshal` output. Any actual known-ref replacement therefore changes a string and fails `reflect.DeepEqual`. Float-number lexical equivalence and duplicate-key collapse cannot arise from these typed memory payloads. A future redactor that intentionally rewrites numeric lexemes would require a stronger typed comparison contract, but that is not a reachable P0 behavior or a defect introduced here.

3. **[OK] The secret-tag RED is causal for the reported regression.** The test sends a safe content value with the known secret only in a tag and asserts rejection plus zero durable rows (`internal/memory/facts_test.go:351-397`). In a controlled ablation restoring the prior content-only gate, the existing content checks passed and the test failed specifically with `known-secret tag accepted (stored tag would be rewritten under the receipt)`.

4. **[OK] The migration RED now constructs the true previous checkpoint schema and is sensitive to both required production branches.** It replaces the current checkpoint table with the old two-column `(name, applied_offset)` table, inserts a live checkpoint, and removes the projection's `lineage` column before Open (`internal/memory/facts_test.go:456-477`). Removing only `ALTER TABLE ... ADD COLUMN version` made Open fail with `no such column: version`; retaining ALTER but removing only the version-mismatch reset made the RED fail because the old projection was not rebuilt and `f.lineage` remained absent. Thus the fixture exercises both the actual schema migration and version-triggered rebuild rather than merely editing a current-format version value.

5. **[OK] No material new defect was introduced in the changed lines.** The whole-payload check remains journal-owned and fail-closed, the migration fixture uses a fresh temporary SQLite database, and neither fix adds a new dependency or production state. The unchanged limitation that `RedactorRewrites` is tailored to the current string-redaction contract is explicitly bounded above and does not weaken the locked P0 behavior.

VERDICT: PASS
