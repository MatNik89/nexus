# REVIEW-PHASE3-R3 — round-3 verification (7 codex r2 folds)

Scope: the diff c05bb8b..91179ff only + NEW defects it introduced. `CGO_ENABLED=0 go vet ./...`
clean; `CGO_ENABLED=0 go test -count=1 ./...` GREEN; `go test -count=20 -run
'^TestMemoryToolSpineSurvivesRestart$' ./cmd/nexus` PASS (the former flake is gone).

---

## The 7 folds — all [OK]

**1. [OK] Projection catch-up/rebuild — crash window is correct.** `catchupProjections(head)`
replays every event past the durable `proj_sync_offsets` checkpoint through `Apply` before the
journal serves (journal.go). The checkpoint advances IN the append transaction via
`INSERT … ON CONFLICT DO UPDATE` (journal.go), so event + projection state + checkpoint commit
TOGETHER — a crash can only leave the checkpoint at or behind the last committed event, never ahead.
The rebuild RED drops the projection tables + checkpoint row and reopens, refolding the full state
including rejected and superseded rows from the canonical stream alone. `Apply` is safe to refold
because a fresh table replay inserts from empty and the terminal-decision UPDATE is guarded by
`AND status='proposed'` (idempotent per event). ✓

**2. [OK] `QueryProjection` is single-statement.** `strings.ContainsRune(query, ';')` rejects any
semicolon, and `proj_sync_offsets` was added to the canonical-table guard. The earlier
`SELECT; DELETE; SELECT` probe is now refused. Legitimate memory reads use bound parameters, so the
over-strict semicolon ban costs nothing. ✓

**3. [OK] Secret-content refusal preserves exact bytes + receipt honesty.** `NewStore` binds the
journal's own redactor; `refuseSecretContent` marshals the content, runs `Redact`, and refuses any
content whose bytes the redactor would rewrite (store.go) — a known-secret fact is rejected BEFORE
the append, so the stored bytes, the `CommitReceipt.ContentHash`, and the "remembered" text all
describe the same exact content. ✓

**4. [OK] Lineage persistence + recall union.** `factPayload.Lineage` and `Row.Lineage` persist the
source chain; `SaveFactLineage` carries it; recall unions each fact's stored lineage with the recall
call ID, deduped (tools.go). ✓

**5. [OK] Tag LIKE escaping.** `strings.NewReplacer("|","||","%","|%","_","|_")` + `ESCAPE '|'` makes
a `%`/`_`/`|` tag match only its literal self, never a wildcard (store.go). ✓

**6. [OK] Sorted tool prompt.** Tool IDs are `sort.Strings`'d before rendering the available-tools
list, so identical inputs produce byte-identical prompts (planner.go). ✓

**7. [OK] e2e teardown ordering.** The composition test now joins the prior daemon (cancel → wait for
`Serve` to return) before starting the next incarnation on the same UDS path; the `-count=20` run is
stable. ✓

## NEW defects (all LOW)

**8. [NEW-ERROR, LOW] `memory_remember` stamps only its own call ID as lineage**
(tools.go: `SaveFactLineage(..., []string{string(c.ToolCallID)})`), not the upstream content lineage
(the user message / the untrusted block the fact was quoted from). The mechanism exists, but the tool
has no access to the source context blocks, so the fact's provenance chain terminates at the
remember call rather than the original source. Trust monotonicity is unaffected (the min-trust rule
still holds), so this is a provenance-truncation note, not a laundering path.

**9. [NEW-ERROR, LOW] `refuseSecretContent` relies on the redactor's byte round-trip being idempotent**
in the absence of a secret (Marshal→Redact→Marshal). This holds for the current JSON-string walk, but
a future redactor that re-normalizes encoding (e.g. key ordering) could produce a false "secret
present" refusal. A dedicated "touches" predicate would be more robust than byte-comparison.

---

## Verdict

All seven round-2 findings are correctly folded: projection state is now genuinely reconstructible
from the canonical stream with a correct crash window, the read seam is single-statement, secret
content is refused rather than silently rewritten, lineage and tag retrieval are sound, the tool
prompt is deterministic, and the e2e test is no longer flaky. The two new observations are
low-severity. Full suite green including the 20x e2e repetition.

VERDICT: PASS
