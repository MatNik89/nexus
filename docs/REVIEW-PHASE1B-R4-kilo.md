# REVIEW-PHASE1B-R4 — round-4 verification (2 codex r3 folds)

Scope: the diff cf09f36..e746fd8 only (FoldFrom + ConfigBinding). `go vet ./internal/...` clean;
`go test -count=1 ./internal/...` GREEN.

---

**1. [OK] `machine.FoldFrom` with explicit `priorCheckpoint` — both codex r3 #3 defects closed.**
`Fold` delegates to `FoldFrom(initial, 0, …)` (machine.go:80-82); `FoldFrom` seeds
`checkpoint := priorCheckpoint` and rejects `ev.Offset <= checkpoint` (machine.go:83-92). A replayed
already-applied offset is rejected even when the transition itself is legal, and an empty resumed
batch returns `(initial, priorCheckpoint, nil)` — never a regressed zero.
`TestFoldFromEnforcesPriorCheckpoint` exercises the stale-first-event and empty-resume literals. ✓

**2. [OK] `config.Resolved.ConfigHash()` + `closure.ConfigBinding` typed seam — codex r3 #10 folded
under an acceptable ceiling.**
- `config.Resolved.ConfigHash()` (config.go:56-66) computes `sha256(json.Marshal(r.Config))` over a
  typed struct (strings/bool/`[]string`/`ProfileID`), so the digest is deterministic and the
  `b, _` is genuinely infallible (no error-producing field type).
- `closure.ConfigBinding` is a typed interface; `Seal` takes it, rejects `nil`, and shape-validates
  the returned digest (`sha256Hex`) before comparing to each probe's `ConfigHash`
  (closure.go:140-154, 186-207).
- `TestInventedOrForeignDigestCannotAttest` seals against the REAL `config.Resolved.ConfigHash()` and
  proves a shape-valid invented token (`"111…1"`) or a foreign real config's digest turns the
  capability OFF (mismatch → fail-closed).

**Ceiling judgment — acceptable.** The remaining gap is exactly the one the code declares: a HOSTILE
local caller can implement `ConfigBinding` with an invented digest AND tag its probes with the same
invented value. That is the Go-interface-forgery boundary — the composing root passes the real
`config.Resolved`, and T27 verifies the all-live snapshot end-to-end, so a well-meaning caller can no
longer hand `Seal` a free-floating string (they must supply a typed binding whose canonical
implementation is the true digest). For a single-process P0 daemon whose composing root is its own
trusted code, this is a sound and honestly-scoped ceiling.

---

## NEW defects — none

`FoldFrom` is a clean delegation + comparison; `ConfigHash()` marshals a fixed-order typed struct
(the nil-vs-empty `EgressAllow` slice yields distinct digests, which is a resolved-value identity
nuance, not a defect); the `ConfigBinding` seam adds no new failure path.

---

## Verdict

Both round-3 findings are correctly folded with causal REDs, and the declared topknot ceiling
(local-interface forgery out of P0 scope) is acceptable. No new defect in the changed lines.

VERDICT: PASS
