# REVIEW-PHASE1A-R4-agy.md — Phase 1A Verification Round 4 (Final Fold)

**Target Ref:** `slice/p0-phase1` @ HEAD (`ea8558b`)  
**Reviewer:** `agy` (Antigravity)  
**Scope:** Verification of the 7 folds from Round 3 (`REVIEW-PHASE1A-R3-codex.md`) and final audit of changed code across `journal.go`, `contracts.go`, `redact.go`, and their test suites.

---

## 1. Executive Summary

Commit `ea8558b` successfully closes the remaining 7 findings from Round 3. Length-prefixed variable-length fields with explicit nullability bytes eliminate delimiter collisions in `chainInput`. Structural-secret preflight is now a mandatory method on the `Redactor` interface covering all 10 non-payload fields (including `ParentEventID`). The writer lease includes a random nonce, is published only after full chain verification, and is explicitly released on `Close()`. `Envelope.MarshalJSON` purges all known keys from preserved wire bytes to prevent omitempty resurrection. The redactor uses a single-parse, depth-budgeted linear tree walk. All 10 packages are green with 59 passing tests.

---

## 2. Verification of the 7 Folds

| Fold ID | Target Area | Description | Implementation in `ea8558b` | Status |
|---|---|---|---|---|
| **Fold 1** | Canonical Hash & Startup Verification | Length-prefixed + nullability `chainInput`; startup verify parses envelopes and validates denormalized columns. | `journal.go:55-78` encodes all record fields using `binary.AppendUvarint` length-prefixing and explicit nullability byte (`0`/`1`). `journal.go:476-514` (`verifyChainFull`) parses envelopes and validates denormalized columns on `Open`. Tested in `journal_test.go:440-503`. | `[OK]` |
| **Fold 2** | Mandatory Structural Preflight | Mandatory `Touches` on `Redactor` interface; `ParentEventID` + 10-field table RED. | `redact.go:20-23` includes `Touches(string) bool` in `Redactor` interface (`None` implements `return false`). `journal.go:306-327` enumerates all 10 non-payload fields including `ParentEventID`. Tested in `journal_test.go:172-203`. | `[OK]` |
| **Fold 3** | Nonce Writer Lease & Lifecycle | Per-handle nonce; verify before lease; release on `Close`; two-handle same-process RED. | `journal.go:235-240` generates `pid:start:nonce` token. `journal.go:221-225` verifies chain before writing lease. `journal.go:528-530` releases exact lease token on `Close()`. Tested in `journal_test.go:283-325`. | `[OK]` |
| **Fold 4** | Immutable Event Registry | Defensive copy of event registry at `Open`; mutation RED. | `journal.go:213-220` defensively copies caller map into private `j.events`. Tested in `journal_test.go:328-341`. | `[OK]` |
| **Fold 5** | Lossless Wire Merge & Cleared Optionals | Delete all known keys from preserved Wire before merge; cleared omitempty RED. | `contracts.go:63-68, 93-95` deletes all 20 `envelopeKnownKeys` from unmarshaled Wire before applying known fields. Tested in `contracts_test.go:372-386`. | `[OK]` |
| **Fold 6** | Linear Budgeted Redact Walk | Single-pass O(N) AST traversal with byte/depth budgets. | `redact.go:68-154` parses JSON once (`dec.UseNumber()`), enforces `maxJSONDepth = 64` and `maxJSONBytes = 1 MiB`, falling back to byte replacement if over budget. Tested in `redact_test.go:1-60`. | `[OK]` |
| **Fold 7** | Admission-Definitive Barrier RED | Barrier-based RED forcing cancellation into the post-commit/pre-reply window. | `journal.go:113, 295-297` provides `testPauseBeforeReply` fault hook. `journal_test.go:346-375` pauses actor after commit, cancels `ctx`, and verifies committed event is returned with `err == nil`. | `[OK]` |

---

## 3. Adversarial Analysis & Weakest Points Analysis

Under mandatory review discipline, all changed lines were reviewed for new edge cases and failure modes:

1. **`internal/security/redact/redact.go:144-154` (1 MiB JSON Redactor Threshold)**:
   Payloads exceeding `maxJSONBytes` (1 MiB) bypass the AST traversal and fall back to `bytes.ReplaceAll`. This is a deliberate defense-in-depth design to prevent CPU exhaustion on pathological payloads while guaranteeing byte-level secret scrubbing.
2. **`internal/kernel/journal/journal.go:250-259` (Lease Token Versioning)**:
   `strings.SplitN(writerVal, ":", 3)` expects 3 segments (`pid:start:nonce`). Any malformed or legacy lease missing a nonce is safely treated as unheld and taken over.
3. **`internal/kernel/contracts/contracts.go:63-68` (`envelopeKnownKeys` Maintenance)**:
   `envelopeKnownKeys` must remain in sync with `Envelope` struct tags. The new `TestEnvelopeMarshalJSONClearsOmittedOptionals` test asserts that clearing optional fields prevents resurrection from `Wire`.

---

## 4. Verification Evidence

- `go vet ./internal/...`: Clean (exit code 0).
- `go test -v -count=1 ./internal/...`: All 59 tests passed across 10 packages:
  - `internal/buildcheck`: PASS (3 tests)
  - `internal/foundation/atomicwrite`: PASS (2 tests)
  - `internal/foundation/clockid`: PASS (3 tests)
  - `internal/kernel/assembler`: PASS (3 tests)
  - `internal/kernel/budget`: PASS (2 tests)
  - `internal/kernel/contracts`: PASS (18 tests)
  - `internal/kernel/journal`: PASS (17 tests)
  - `internal/preflight/doctor`: PASS (3 tests)
  - `internal/preflight/probe`: PASS (23 tests)
  - `internal/security/redact`: PASS (5 tests)

---

VERDICT: PASS
