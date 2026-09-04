# REVIEW-PHASE3-R5 — round-5 final verification (2 codex r4 folds)

Scope: the last fold diff fcb0d6a..061b4cf only + NEW defects it introduced. `CGO_ENABLED=0 go
vet ./...` clean; `CGO_ENABLED=0 go test -count=1 ./...` GREEN.

---

**1. [OK] Whole-payload secret gate in `Store.append` + tag-smuggle RED.** The gate moved from
`refuseSecretContent` (content-only) to `Store.append`, which marshals the FULL event payload
(content, tags, ids/options) and consults `s.j.RedactorRewrites(raw)` before the append
(store.go). A known secret smuggled into a `tags` value is now refused, so a caller can no longer
earn an AFTER_COMMIT receipt while the stored tag is rewritten. The tag RED asserts the refusal and
zero residual rows.

**Judgment — the value-compare cannot miss a real rewrite.** `RedactorRewrites` short-circuits on
byte equality, then falls back to `json.Unmarshal` into `any` + `reflect.DeepEqual` (journal.go):
a re-serialization that preserves VALUES is correctly treated as not-a-rewrite, while a genuine
redaction replaces the value ("sk-123" → "[REDACTED:KEY]") and `DeepEqual` detects the difference
(including the type change when a numeric field is rewritten to a string). For the memory payloads —
typed structs marshaled deterministically, no duplicate keys, no numeric-secret surface — the
round-trip is stable, so the comparison is sound. ✓

**2. [OK] The previous-revision fixture builds the true old checkpoint schema and is ALTER-sensitive.**
`TestV1SchemaDatabaseRebuildsAtOpen` now `DROP`s the current `proj_sync_offsets`, recreates the OLD
TWO-COLUMN table `(name, applied_offset)`, seeds a checkpoint, and strips the memory `lineage` column
(facts_test.go) — emulating the real `91179ff` on-disk state. Removing the production
`ALTER TABLE … ADD COLUMN version` migration would make `Open`'s `SELECT … version` fail with
"no such column: version", so the RED now fails when the exact migration under test is removed. ✓

## NEW defects — none material

- **LOW:** `RedactorRewrites`'s `reflect.DeepEqual` path depends on the memory payloads remaining
  deterministic typed structs; a future payload carrying duplicate keys or a numeric field that the
  redactor touches would need a re-check, but neither exists today.
- **LOW:** the `ALTER TABLE … ADD COLUMN version` remains an error-discarded one-shot on every Open
  (inherited from the prior round, unchanged by this diff).

---

## Verdict

Both round-4 findings are folded with causal REDs: the exact-bytes gate now covers the whole event
payload (tags included) through the journal-owned redactor with a sound value-level comparison, and
the prior-revision RED constructs the genuine two-column checkpoint schema and is sensitive to
removing the ALTER migration. No material new defect; full suite green.

VERDICT: PASS
