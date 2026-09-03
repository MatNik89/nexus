# REVIEW-PHASE1B-R4-agy.md — Phase-1B Verification Round 4 (Narrow)

**Target Ref:** `slice/p0-phase1` @ commit `e746fd8`  
**Reviewer:** `agy` (Antigravity)  
**Scope:** Narrow verification of diff `cf09f36..e746fd8` folding the 2 residual round-3 findings (`codex #3` and `#10`).

---

## 1. Verification of Round-3 Folds

| # | Finding & Source | Resolution & Code Location | Status |
| :- | :--- | :--- | :--- |
| 1 | **`FoldFrom` with Explicit Prior Checkpoint**<br>`codex #3` (R3) | `machine.Table[S]` adds `FoldFrom(initial S, priorCheckpoint uint64, events []FoldEvent, observe ...)` (`machine.go:87-104`). Offsets must strictly exceed `priorCheckpoint`; replayed offsets `<= priorCheckpoint` are rejected with `offset %d is not strictly increasing after checkpoint %d (fail closed)` without regressing the checkpoint. An empty resumed batch preserves `priorCheckpoint` unchanged. `Fold` delegates to `FoldFrom(initial, 0, events, observe)` (`machine.go:80-82`). Verified in [`machine_test.go:320-344`](file:///home/matej/HARNESS/nexus/internal/kernel/machine/machine_test.go#L320-L344). | `[OK]` |
| 2 | **Resolved Config Hash & Typed `ConfigBinding` Seam**<br>`codex #10` (R3) | `config.Resolved` implements `ConfigHash() string` (`config.go:60-67`), computing the SHA-256 digest over the canonical JSON-marshaled `Config`. `closure.Seal` now consumes the typed `ConfigBinding` interface (`closure.go:144-147, 186-200`), validates non-nil binding, verifies 64-char lowercase SHA-256 hex format, binds the snapshot, and checks probe hashes against the computed digest. Verified in [`closure_test.go:16-25, 216-245`](file:///home/matej/HARNESS/nexus/internal/kernel/closure/closure_test.go#L216-L245). | `[OK]` |

---

## 2. Assessment of Declared Topknot Ceiling

The commit documentation declares the topknot minimal-machinery ceiling for the `ConfigBinding` seam:
> *local interface forgery out of P0 scope, composing root passes real Resolved, T27 verifies live*

**Adjudication:** This ceiling is sound and complies with the ponytail contract. Within a Go single-binary process space, defining `ConfigBinding` as an interface permits tests to exercise negative shape and mismatch paths, while the production composing root passes `config.Resolved` directly. Full end-to-end attestation across all live probes is verified in T27.

---

## 3. Weakest Points Analysis (Epistemic Honesty)

1. **`Resolved.ConfigHash` Determinism ([`internal/foundation/config/config.go:60-67`](file:///home/matej/HARNESS/nexus/internal/foundation/config/config.go#L60-L67))**:
   `ConfigHash` marshals `Config` via `json.Marshal`. Because `Config` is a fixed struct of scalar strings, booleans, and string slices, field serialization order is strictly deterministic. If map-valued fields are added in later phases, deterministic sorting would be required (topknot ceiling for scalar P0 struct).
2. **`FoldFrom` In-Order Assumption ([`internal/kernel/machine/machine.go:89-98`](file:///home/matej/HARNESS/nexus/internal/kernel/machine/machine.go#L89-L98))**:
   `FoldFrom` enforces strictly increasing offsets (`ev.Offset > checkpoint`). While entity-filtered streams can have gaps (e.g. offsets 5, 8, 12), any out-of-order event halts folding and returns the last legal state before the failing offset.
3. **Seam Interface vs Concrete Struct ([`internal/kernel/closure/closure.go:144-147`](file:///home/matej/HARNESS/nexus/internal/kernel/closure/closure.go#L144-L147))**:
   Using `ConfigBinding` avoids a direct package dependency from `kernel/closure` to `foundation/config`, maintaining package layering while tying capability snapshotting directly to the resolved configuration.

---

## 4. Verification Evidence

- `go vet ./internal/...`: Clean (exit code 0).
- `go test -count=1 ./internal/...`: All 15 internal packages passed uncached.

---

VERDICT: PASS
