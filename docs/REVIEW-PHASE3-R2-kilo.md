# REVIEW-PHASE3-R2 — round-2 verification (codex 12 + kilo 2 MED + agy folds)

Scope: the fold diff 4bab530..c05bb8b + NEW defects it introduced. `CGO_ENABLED=0 go vet ./...`
clean; `CGO_ENABLED=0 go test -count=1 ./...` GREEN.

---

## My round-1 findings — both folded

**1. [OK] Supersession diamond (kilo #1 / codex #5).** A `UNIQUE` partial index on `mem_facts
(supersedes) WHERE supersedes IS NOT NULL` (store.go:157-158) plus an in-`Apply` leaf check
(`EXISTS(... WHERE supersedes=?1)`, store.go:236-261) — both inside the append transaction, serialized
by the single append actor. A fact with ANY successor can never be superseded again. ✓

**2. [OK] `memory_remember` never superseding (kilo #2 / codex #2).** The tool now accepts a
`supersedes` argument and routes it through `store.Supersede` (tools.go:63-80); "actually it's Y"
becomes a strictly-linear correction instead of a duplicate. ✓

## Codex spot-check — all folded

- **#1 two-DB topology** → memory is now a SYNC JOURNAL PROJECTION in the ONE profile DB: every
  mutation is a journal event (`memory.fact_*`), folded by `Projection.Apply` in the append
  transaction; reads go through the guarded `Journal.QueryProjection` (SELECT-only + canonical-table
  guard, projection.go:184-193). P0.3/B3 now satisfied — copy/replay reproduces facts; no fact
  without its event. ✓
- **#2 finals-only planner** → `WithTools` + `toolCallFromReply` (strict whole-reply JSON,
  `action="tool"`, trailing content → final, unknown tool_id → error). ✓
- **#3 forged effect/kind + no receipt** → `Specs()` seals `Effect/ExecutionKind/ArgsSchemaHash`
  from the registry (never the model); `memory_remember` returns a `CommitReceipt(AfterCommit)` bound
  to call+attempt. ✓
- **#4 profile identity** → `profileGuard` refuses any call whose `ProfileID` differs from the store
  profile (tools.go:52-57); the store uses the bound journal profile; the effectpath `ToolTarget`
  hashes `ProfileID`. ✓
- **#6 Search leaks superseded** → `Search` is now an alias of `Recall` (same `latestAccepted`
  predicate). ✓
- **#7 trust/sensitivity discarded** → persisted (`factPayload.Trust/Sensitivity`); recall output is
  as trusted as its LEAST trusted fact and passes the known-ref redactor (tools.go:116-132). ✓
- **#8 exact/tag surface** → `RecallTag`/`Exact`/`Tags` added. ✓
- **#9 path boundary** → moot (store is a journal facade; the journal owns the path). ✓
- **#10 context/budgets** → `QueryContext`, `checkQuery` (256B), `resultLimit` 50. ✓
- **#11 REDs cross seams** → e2e memory-spine RED through production `buildDaemon` incl. restart
  (main_test.go +101, facts_test +262). ✓
- **#12 store close** → moot (no separate SQLite file). ✓

## The five judgments

**(a)** Memory-as-journal-projection satisfies P0.3/B3: one profile DB, events are the source of
truth, the `QueryProjection` read seam is SELECT-only and lexically canonical-table-guarded. ✓
**(b)** The model can control ONLY `tool_id` + `arguments`; effect/kind/schema are sealed from the
registry and `ProfileID` is session-stamped. Prose-vs-tool ambiguity is narrow (whole-reply JSON +
`action="tool"` + closed tool set; trailing content downgrades to prose) and the model is a trusted
principal. ✓
**(c)** Supersession is strictly linear via the unique index + in-Apply leaf check. ✓
**(d)** The e2e RED crosses the production spine with restart. ✓
**(e)** Ceilings declared: buffered Chat when tools are enabled (token streaming = multi-tool P1);
approval UX via NEEDS_APPROVAL until T24. ✓

## NEW defects (all LOW)

**3. [NEW-ERROR, LOW] `memory_remember`'s acknowledgment echoes the RAW content unredacted**
(tools.go:93), while `memory_recall` redacts (tools.go:129). A fact containing a known secret is
re-sent verbatim to the model in the "remembered (...)" observation. The content already reached the
model in the user's turn, so it is not a new exfiltration, but the two memory tools are inconsistent
about the redaction boundary.

**4. [NEW-ERROR, LOW] Recall sensitivity is fixed at `SensitivityConfidential`** (tools.go:164)
regardless of the fact's stored sensitivity — the monotone-combine fix was applied to trust but not
sensitivity. Today harmless (`explicitDefaults` hardcodes CONFIDENTIAL for every fact), but it is the
same drift codex #7 flagged for trust.

---

## Verdict

Both of my MED findings and all twelve codex findings are correctly folded; the memory store is now a
genuine same-transaction journal projection satisfying P0.3/B3, tool decoding is sealed and
profile-stamped, supersession is strictly linear, and the e2e RED crosses the production spine. The
two new observations are low-severity and one is self-limiting. Full suite green.

VERDICT: PASS
