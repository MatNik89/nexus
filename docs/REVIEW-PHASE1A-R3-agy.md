# REVIEW-PHASE1A-R3-agy.md — Phase 1A Verification Round 3 (Codex 8 Fold)

**Target Ref:** `slice/p0-phase1` @ HEAD (`1c5c8a5`)  
**Reviewer:** `agy` (Antigravity)  
**Scope:** Verification of the 8 folds from Round 2 (`REVIEW-PHASE1A-R2-codex.md`) and audit of changed code in `journal.go`, `contracts.go`, `enums.go`, `schema.go`, `redact.go`, and their test suites.

---

## 1. Executive Summary

Commit `1c5c8a5` resolves all 8 issues raised by Codex in Round 2. The event log now authenticates the complete journal record (`offset`, `event_id`, `run_id`, `sequence`, `redaction_policy_version`, `prev_hash`, `sealed_payload_ref`, `envelope`) with full-chain verification at `Open` and stream-verification on `Replay`. Known secrets in structural fields are rejected upfront rather than mutated, ensuring a single canonical envelope across storage, return, hashing, and replay. Profile binding is durably recorded in `journal_meta` with live-writer lease enforcement. `Envelope.MarshalJSON` losslessly preserves admitted wire fields. All nested contracts are strictly normalized and copied.

Verification passes cleanly: `go vet ./internal/...` is clean, and `go test -count=1 ./internal/...` passes all 57 tests.

---

## 2. Verification of the 8 Folds

| Fold ID | Target Area | Description | Implementation in `1c5c8a5` | Status |
|---|---|---|---|---|
| **Fold 1** | Integrity Chain & Verification | Authenticate complete `JournalEvent` record; verify at `Open` and `Replay`; tamper REDs per metadata class. | `journal.go:48-63` defines canonical `chainInput` over all 8 record fields. `Open` (`journal.go:216-220`) runs `verifyChainFull` and refuses tampered DB. `Replay` (`journal.go:386-400`) verifies hash chain and denormalized columns on read. Tested in `journal_test.go:227-277` (subtests for `envelope`, `run_id`, `sequence`, `rpv`, `sealed`, `event_id` tamper). | `[OK]` |
| **Fold 2** | Structural Redaction & Single Envelope | Known secret in structural field must reject append; single canonical envelope. | `journal.go:248-266` collects structural fields; `journal.go:279-287` calls `Toucher.Touches()` and rejects if secret present. `p.Payload` redacted semantically; one `NewEnvelope` instance used for SQLite row, returned struct, hash, and replay. Tested in `journal_test.go:146-162`. | `[OK]` |
| **Fold 3** | DB Profile Binding & Writer Lease | Profile binding in DB metadata; single-writer lease (`pid+starttime`); reopen REDs. | `journal.go:149-176` persists profile in `journal_meta`; mismatched reopen rejected. `journal.go:178-205` manages writer lease via `procStartTime` liveness; active writer blocks 2nd opener, dead writer taken over. Tested in `journal_test.go:93-130`. | `[OK]` |
| **Fold 4** | Lossless Wire Merge | `Envelope.MarshalJSON` must merge preserved `Wire` unknown fields. | `contracts.go:64-101` merges unmarshaled `e.Wire` map with known fields (deterministic key order). Tested in `contracts_test.go:354-370` (parse -> marshal -> parse cycle). | `[OK]` |
| **Fold 5** | Decoded-JSON Redaction & Schema Error Sanitization | Redact decoded JSON string values (`\u` variants covered); sanitize unknown-schema errors. | `redact.go:64-126` (`redactJSONValue`) traverses decoded JSON tree, redacting strings/keys. `schema.go:34-36` and `contracts.go:139` no longer echo schema ID in error strings. Tested in `redact_test.go:39-48`. | `[OK]` |
| **Fold 6** | Nested Contract Normalization & Copying | Rebuild/copy nested contracts via `Normalized()`; cut caller aliasing. | `contracts.go:277-297` implements `ContextBlock.Normalized()` (deep copies content, lineage, applies UTC). `NewMessage` (`contracts.go:327-335`) and `NewToolResult` (`contracts.go:459-467`) build normalized slices. Tested in `contracts_test.go:372-386`. | `[OK]` |
| **Fold 7** | Closed ActorType & Event Type Admission | `ActorType` closed enum; closed event-type set with typed payload validators. | `enums.go:231-247, 532-544` defines `ActorType` enum. `Open` (`journal.go:122, 129-131`) accepts `events map[string]PayloadValidator`; `appendOne` (`journal.go:275-297`) enforces closed event types and payload validators. Tested in `journal_test.go:132-159`. | `[OK]` |
| **Fold 8** | Fault Seam & Injected Commit Failure RED | Injected commit failure burns no allocation; admission-definitive semantics. | `journal.go:92, 332-336` provides `testFailCommit` fault seam. `journal_test.go:161-177` verifies aborted commit burns no offset or sequence. | `[OK]` |

---

## 3. Adversarial Analysis of Changed Code

1. **Transaction Ordering & Metadata Initialization (`journal.go:158-208`)**:
   Profile verification and writer lease acquisition run inside a single immediate transaction prior to starting the actor. If any step fails (mismatched profile, live concurrent writer, unreadable `stat`), the transaction rolls back, the database is closed, and `Open` returns the typed error.
2. **Lossless Wire Serialization (`contracts.go:64-101`)**:
   For locally built envelopes (`len(e.Wire) == 0`), `MarshalJSON` short-circuits to `json.Marshal(envelopeWire(e))` with zero overhead. For wire-admitted envelopes, unknown extension keys are preserved and merged with validated known fields using deterministic lexical key ordering.
3. **Redaction AST Traversal (`redact.go:64-126`)**:
   JSON values are parsed into structural primitives (`map[string]json.RawMessage` and `[]json.RawMessage`). String literals are unmarshaled into Go strings (resolving `\u` escapes) before string matching, ensuring that escape variations cannot bypass known secret detection. Numbers, booleans, and nulls are untouched.
4. **Top 3 Weakest Points (Mandatory Review Discipline)**:
   - `internal/kernel/journal/journal.go:101-116`: `procStartTime` relies on `/proc/[pid]/stat` parsing. Standard on Linux, but dependent on `/proc` availability without strict container `hidepid` restrictions.
   - `internal/security/redact/redact.go:78-86`: Intermediate map allocations during `redactJSONValue` traversal on very large nested JSON payloads.
   - `internal/kernel/journal/journal.go:370-376`: `Replay(from, fn)` scans from offset 0 to incrementally verify the integrity chain before streaming to caller; suitable for P0 log sizes.

---

## 4. Verification Evidence

- `go vet ./internal/...`: Clean (exit code 0).
- `go test -v -count=1 ./internal/...`: All 57 tests passed across 10 packages:
  - `internal/buildcheck`: PASS (3 tests)
  - `internal/foundation/atomicwrite`: PASS (2 tests)
  - `internal/foundation/clockid`: PASS (3 tests)
  - `internal/kernel/assembler`: PASS (3 tests)
  - `internal/kernel/budget`: PASS (2 tests)
  - `internal/kernel/contracts`: PASS (17 tests incl. `TestMessageNormalizesAndCopiesBlocks`, `TestEnumJSONWireIsClosedStrings`, `TestParseEnvelopePreservesUnknownFields`)
  - `internal/kernel/journal`: PASS (15 tests incl. `TestChainTamperDetected`, `TestTamperedJournalRefusesToOpen`, `TestSecondLiveOpenerRefused`, `TestReopenUnderDifferentProfileRefused`, `TestUnknownEventTypeRejected`, `TestPayloadValidatorEnforced`, `TestInjectedCommitFailureBurnsNothing`)
  - `internal/preflight/doctor`: PASS (3 tests)
  - `internal/preflight/probe`: PASS (23 tests)
  - `internal/security/redact`: PASS (5 tests incl. `TestUnicodeEscapeVariantRedacted`, `TestJSONEscapedSecretRedacted`)

---

VERDICT: PASS
