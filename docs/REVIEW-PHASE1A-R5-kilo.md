# REVIEW-PHASE1A-R5 — round-5 final convergence (5 codex r4 folds)

Scope: the 5 folds + NEW defects in changed lines only. `go vet ./internal/...` clean;
`go test -count=1 ./internal/...` GREEN.

---

## The 5 folds — all [OK]

**1. [OK] `Redact` returns an error; over-budget valid JSON is rejected at admission.**
`Redactor.Redact` is now `([]byte, error)` (redact.go:20-27). `KnownRefs.Redact` returns a typed error
for valid JSON over `maxJSONBytes` or over `maxJSONDepth`, and for a transform that yields invalid
JSON (redact.go:146-167) — never a byte-level fallback for valid JSON. `appendOne` propagates the
error before the tx (journal.go:356-359), so over-budget input is REJECTED, not persisted. ✓

**2. [OK] Lease-before-verify + release-on-failure + Close error join + injected-delete RED.**
The lease tx now precedes `verifyChainFull` (journal.go:234-283), so recovery runs UNDER ownership;
on verify failure `releaseLease` runs before returning (journal.go:276-280). `Close` joins the lease
DELETE error with `db.Close` via `errors.Join` (journal.go:546), and the `testFailLeaseDelete` seam
+ `TestLeaseDeleteFailureSurfacesOnClose` prove a swallowed release can't recur. ✓

**3. [OK] Case-insensitive known-key deletion + TURN_ID alias RED.**
`MarshalJSON` deletes preserved keys with `strings.EqualFold` against `envelopeKnownKeys`
(contracts.go:96-103), so a case-aliased `"TURN_ID"` (which `encoding/json` maps to `TurnID`) can no
longer survive as a fake unknown field and resurrect a cleared optional. ✓

**4. [OK] Causal event_type structural subtest.**
The test registers `"x-"+secret` in the closed event set before the subtest
(`ev["x-"+secret] = nil`), with the explicit comment "registered: rejection must come from
Touches" — so removing the structural-secret check turns the subtest RED, no longer masked by
closed-registry admission. ✓

**5. [OK] Revert-proof admission barrier.**
The RED asserts, while the actor is STILL blocked (`testPauseBeforeReply`), that cancelling ctx does
NOT return from `Append` ("cancel must not preempt a committed result"); only after releasing the
barrier is the committed result required. The old post-admission `select { reply | ctx.Done }` would
now fail this test. ✓

---

## NEW defects — none material

- **LOW:** the verify-failure path's `releaseLease` closure (journal.go:269-271) discards the DELETE
  error, unlike the `Close` path which joins it. Best-effort cleanup on an already-failing Open (a
  corrupt journal), and a dead-pid lease is taken over on the next open anyway — so non-material, but
  for symmetry it could also join the release error.
- **Residual (not new):** `envelopeKnownKeys` remains hand-maintained; the case-insensitive fix does
  not remove that footgun, but the wire-merge RED is the guard.

No other defect in the changed lines: the case-insensitive deletion is correct for both canonical and
aliased keys while preserving genuinely unknown fields; `Redact`'s error path rejects before any
sequence/tx allocation.

---

## Verdict

All 5 round-4 folds are correctly implemented and test-verified; the sole new observation is a
low-severity best-effort release on the abnormal (corrupt-journal) path.

VERDICT: PASS
