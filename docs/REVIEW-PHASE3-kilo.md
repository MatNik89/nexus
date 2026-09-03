# REVIEW-PHASE3 — T18 profiles + T19 explicit memory (mandatory phase gate)

Scope: internal/memory/{store,tools}.go + the buildDaemon wiring. `CGO_ENABLED=0 go vet ./...`
clean; `CGO_ENABLED=0 go test -count=1 ./...` GREEN.

---

## HIGH — none

## MED

**1. [MED] Supersession admits a DIAMOND — multiple accepted successors of one fact — and `Recall`
then returns MULTIPLE "latest" versions.** `Supersede` (store.go:176-190) checks only that the old
fact is `accepted`; it does not reject superseding a fact that already has an accepted successor. So
`Supersede(A,B)` then `Supersede(A,C)` succeeds, and `Recall`'s `NOT EXISTS (n.supersedes=f.id AND
n.status='accepted')` hides A while returning BOTH B and C — two contradictory "latest" facts. The
B8 "recall follows the chain to the LATEST accepted version" is singular; a branch breaks it.
`TestSupersededFactReturnsOnlyLatest` covers only the linear chain (A→B→A3) and the unknown/unaccepted
refusals, never the multiple-successor case. Fix: refuse `Supersede` when the old fact already has an
accepted successor (or make the "latest" deterministic), and add a diamond RED.

**2. [MED] The B8 "correction = supersession" is unreachable through the only P0 entry point — the
`memory_remember` tool always creates an INDEPENDENT fact.** `Tools()` (tools.go:26-40) unmarshals
only `content` and calls `SaveFact` (supersedes=nil); there is no "supersede X with Y" argument. So a
user who says "remember X is A" and later "actually X is B" produces two independent accepted facts,
and `Recall("X")` returns both — the exact contradictory-state the supersession chain exists to
prevent. The store's `Supersede` is tested but never wired to the tool, and no topknot comment
declares the correction flow as a later slice. For P0 this means the correction mechanism is
store-level-only, not user-visible. (The diamond in finding 1 is today latent only because the tool
never calls `Supersede`; wiring it would activate the diamond immediately.)

## LOW

**3. [LOW] Rejected-successor semantics are correct but untested.** `Supersede(A,B)` then `Reject(B)`
un-hides A (B is not accepted, so the `NOT EXISTS` predicate is false) — the old fact correctly
stands when its correction is rejected. No RED covers this leg, so a regression would go unnoticed.

**4. [LOW] Fact content is stored and recalled WITHOUT known-reference redaction.** The journal
redactor (C1) does not apply to the memory store, and `memory_recall` feeds `h.Content` straight into
a `TOOL_TRUSTED` observation that reaches the provider (tools.go:52-58). A fact such as "my API key
is sk-123" is persisted plaintext and later sent to the model unredacted. This is defensible for B8
("explicit facts" are the user's data, meant to be recalled), but the memory sink and the
recall→provider boundary are un-redacted by design and that is not recorded in a comment.

**5. [LOW] `SetMaxOpenConns(1)` serializes all daemon sessions on one SQLite connection**
(store.go:66). No deadlock (no nested tx, `defer tx.Rollback()` on `insert`, the `Supersede` SELECT
releases the connection before the write tx), but every concurrent `memory_recall`/`remember` across
UDS sessions blocks on the single connection up to `busy_timeout(5000)`. Acceptable for a single-user
P0; the serialization is a real (if minor) latency ceiling.

**6. [LOW] The memory store is opened in `buildDaemon` but never closed** (main.go:162; only the
journal is `defer j.Close()` at :114). Data is durable (WAL `synchronous=FULL`), so nothing is lost,
but the final checkpoint/close is skipped on shutdown.

## Verified sound

- **B3 physical isolation** is airtight: one file per profile subtree, open-time stamp refusal, every
  row stamped from the store's BOUND profile (not the caller), FTS inside the same file. REDs
  `TestCrossProfileQueriesReturnNothing`, `TestPhysicalIsolationLayout`,
  `TestProfileStampImmutableAcrossRestart`, `TestCallerCannotForgeProfileOnRow` are causal. ✓
- **FTS injection** is closed: the query is always wrapped as a single quoted phrase with `"`
  doubled (`""`), so `OR`/`AND`/`-`/`*`/`(` are literal (store.go:198, 220). ✓
- **Approval gating vs C4** is correct: `memory_remember=ASK`, `memory_recall=ALLOW`; the PEP's
  exact-intent hash covers the raw `Arguments` (hence the exact content); the interactive-approval UX
  deferral to T24 is declared (tools.go:18-22). ✓
- **Trust/lineage** of memory outputs is `TOOL_TRUSTED` + `Lineage=[ToolCallID]` — no laundering. ✓
- **B8 no-decay** holds structurally: `Recall` has no age filter. ✓
- All T18/T19 ledger RED names exist and are causal (verified against the naive implementation).

---

## Verdict

B3 physical isolation, FTS injection defense, exact-intent approval gating, and the RED table are all
solid. The phase gate fails on the B8 supersession semantics: the correction path is unreachable
through the only entry point (finding 2) and the store's own `Supersede` permits a diamond that makes
"latest accepted version" ambiguous (finding 1) — both untested. These are the locked
"append-only supersession" decision, exercised nowhere but the linear happy-path test.

VERDICT: FAIL
