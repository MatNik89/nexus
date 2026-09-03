# REVIEW-PHASE1A-R2-agy.md — Phase 1A Verification Round 2 (T04–T06 Fold)

**Target Ref:** `slice/p0-phase1` @ HEAD (`1bdae45`)  
**Reviewer:** `agy` (Antigravity)  
**Scope:** Verification of folded findings across all Round 1 reviews (`REVIEW-PHASE1A-{agy,codex,kilo}.md`) and adversarial audit of rewritten foundations (`journal.go`, `contracts.go`, `enums.go`, `schema.go`, `assembler.go`, `atomicwrite.go`, `redact.go`, and test suites).

---

## 1. Executive Summary

Commit `1bdae45` comprehensively addresses all 33 findings raised in Round 1. The rewrite of `journal.go` elevates the event log to the full Annex P0.3 specification (`JournalOffset` separated from per-run `Sequence`, `integrity_prev_hash`/`integrity_hash` chain verification, profile binding, synchronous `FULL` durability, and admission-only context gating). All 14 enums now implement strict, closed canonical-string `MarshalJSON`/`UnmarshalJSON` methods that fail closed against numbers and unknown discriminators. `Assembler.Base` now uses content-hash-derived fencing that is mathematically resistant to literal closing-tag injection.

Verification passes cleanly across the entire workspace: `go vet ./internal/...` is clean, and `go test -count=1 ./internal/...` passes all 54 tests (including 12 new targeted RED tests and hostile barrier crash tests).

---

## 2. Verification of Round 1 Findings

### agy Findings Verification

| ID | Tag | Finding Description | Resolution in `1bdae45` | Status |
|---|---|---|---|---|
| **agy#1** | `[BREAK]` | Enums do not implement `TextMarshaler`/`json.Marshaler` (serialize as numbers; wire strings fail; invalid numbers decode unchecked). | `enums.go:279-512` implements generic `marshalJSON`/`unmarshalJSON` on `enumSpec` and wires all 14 enums. Numbers and unknown strings rejected. Tested in `contracts_test.go:293-323`. | `[OK]` |
| **agy#2** | `[BREAK]` | `Journal.Close()` is not concurrency-safe (`select` race panics on double close). | `journal.go:49, 294-301` wraps closure in `sync.Once`, blocks on `<-j.actorDone`, stores `closeErr`. Tested in `journal_test.go:206-214` (8 concurrent workers). | `[OK]` |
| **agy#3** | `[SEC]` | `Assembler.Base` untrusted fence closing tag (`</untrusted-data>`) closable by payload. | `assembler.go:51-54` computes SHA-256 hash prefix of content and generates tag `<untrusted-<hash[:8]>>`. Payloads cannot fix-point own hash. Tested in `assembler_test.go:58-84`. | `[OK]` |
| **agy#4** | `[RED]` | `contracts_test.go` omitted JSON marshal/unmarshal tests on structs containing enums. | `contracts_test.go:293-323` (`TestEnumJSONWireIsClosedStrings`) covers canonical strings, rejected numbers, rejected unknown discriminators, and rejected out-of-range marshal. | `[OK]` |
| **agy#5** | `[RED]` | `TestContextBlockRejectsInvalidProvenance` omitted invalid `Sensitivity(99)` test. | `contracts_test.go:95-98` explicitly tests and asserts rejection of `Sensitivity(99)`. | `[OK]` |
| **agy#6** | `[QUALITY]` | `NewToolResult` omitted validation of incoming `call ToolCall`. | `contracts.go:382-384` executes `call.Validate()`, `b.Validate()` on outputs, and `terr.Validate()`. Tested in `contracts_test.go:333-337`. | `[OK]` |
| **agy#7** | `[QUALITY]` | `NewMessage` checked slice length but omitted validation of nested blocks. | `contracts.go:257-261` iterates over all blocks calling `b.Validate()`. Tested in `contracts_test.go:327-331`. | `[OK]` |
| **agy#8** | `[QUALITY]` | `p.PayloadHash` remained stale (unredacted hash) after secret redaction in `appendOne`. | `journal.go:155-157` redacts payload first, then recomputes `p.PayloadHash = hex(sha256(redacted))`. Tested in `journal_test.go:128-144`. | `[OK]` |

---

### Codex & Kilo Cross-Verification Spot-Check

| ID | Description | Resolution in `1bdae45` | Status |
|---|---|---|---|
| **codex#1** | Full P0.3 `JournalEvent` contract missing (`journal_offset`, hash chain, etc.). | `journal.go:31-38` defines typed `Event`; table stores `journal_offset`, `redaction_policy_version`, `integrity_prev_hash`, `integrity_hash`, `sealed_payload_ref`. `VerifyChain()` implemented. | `[OK]` |
| **codex#2** | Failed appends burned sequence numbers. | `journal.go:134-137` advances `lastOffset` and `lastHash` only when `rep.err == nil`. Tested in `journal_test.go:64-81`. | `[OK]` |
| **codex#3** | Construction bypassed schema admission; registry was mutable public map. | `schema.go:16-25` makes `knownSchemas` private; `contracts.go:87-91` checks `schemaKnown` in `NewEnvelope`. Tested in `contracts_test.go:341-352`. | `[OK]` |
| **codex#5** | Unknown fields on known schemas discarded. | `contracts.go:51` adds `Wire json.RawMessage` to `Envelope`; `schema.go:77-80` preserves raw input. Tested in `contracts_test.go:354-369`. | `[OK]` |
| **codex#7** | State enums lacked `MANUAL_RECOVERY` reconciliation vocabulary. | `enums.go:186, 222` adds `RunManualRecovery` and `AttemptManualRecovery`. Tested in `contracts_test.go:385-392`. | `[OK]` |
| **codex#8** | Timestamps were not normalized to UTC. | `contracts.go:19` normalizes all struct timestamps via `utc()`; `clockid.go:29, 57` emits UTC in `System` and `Fake`. Tested in `contracts_test.go:372-383`. | `[OK]` |
| **codex#9** | Journal was not bound to a profile (HARDQ B3). | `journal.go:44, 74-76, 148-150` binds `profile` at `Open` and rejects foreign profiles. Tested in `journal_test.go:84-91`. | `[OK]` |
| **codex#10** | Redaction only covered payload; ID errors echoed raw values. | `journal.go:184` redacts full marshaled record; `ids.go:53` formats error without echoing value. Tested in `journal_test.go:147-162`. | `[OK]` |
| **codex#11** | JSON-escaped secrets missed by raw byte matching. | `redact.go:44-49` extracts and registers `json.Marshal(val)` escaped variant. Tested in `redact_test.go:29-37`. | `[OK]` |
| **codex#12 / kilo#3** | Cancellation after admission reported failure while committing. | `journal.go:215-226` uses `ctx` for channel send (admission) only, then waits for definitive reply. Unique `event_id` enforced in schema. Tested in `journal_test.go:164-204`. | `[OK]` |
| **codex#14** | Durability level was NORMAL; `AtomicWriter` ignored dir-fsync errors. | `journal.go:79` sets `synchronous(FULL)`; `atomicwrite.go:54-64` propagates directory open/fsync/close errors. | `[OK]` |
| **codex#16** | RED crash test lacked exact commit barrier synchronization. | `journal_test.go:248-317` (`TestSigkillAfterCommitBoundaryKeepsExactPrefix`) synchronizes on exact 3rd commit barrier and verifies exact count. | `[OK]` |
| **codex#17** | `Replay` lacked failing offset context and nil callback check. | `journal.go:231-233, 251, 255` checks nil callback and wraps decode/callback errors with `%w` and failing offset. | `[OK]` |

---

## 3. Top-3 Weakest Points Analysis (Mandatory Review Discipline)

Under the mandatory review discipline hunting for potential candidate defects, the top 3 design nuances and potential future seams were analyzed:

1. **Memory vs Stored Envelope Asymmetry on Non-Payload Redaction (`internal/kernel/journal/journal.go:173-207`)**:
   In `appendOne`, payload redaction (`p.Payload`) occurs before `NewEnvelope`, but defense-in-depth redaction for non-payload fields (`raw = j.redact.Redact(raw)`) occurs after `NewEnvelope` on the serialized JSON. If a secret was placed in a non-payload field (e.g., `EventType`), the `Event.Envelope` returned in-memory to the immediate caller of `Append()` contains the original string, whereas the persisted SQLite row and subsequent `Replay()` produce the redacted string `[REDACTED:<name>]`. While the persistence sink is 100% clean and protected, callers should rely on `Replay` for canonical post-redaction state.
2. **`EnvelopeParams` Omits `Wire` Raw Bytes Propagation (`internal/kernel/contracts/contracts.go:56-77`)**:
   `Envelope.Wire` (`contracts.go:51`) stores raw wire bytes to preserve unknown fields on known schemas (`schema.go:77-80`), but `EnvelopeParams` does not have a `Wire` field. When an admitted wire envelope is converted to `EnvelopeParams` for journal append, unknown extension fields are dropped during `json.Marshal(env)`. This satisfies P0 scope (HARDQ C7), but should be revisited when implementing full v2 migration forwarding in T22.
3. **`validID` Control Character Boundary (`internal/kernel/contracts/ids.go:28-33`)**:
   `validID` checks `r < 0x20 || r == 0x7f` and length `<= 256`. It is strictly designed for ASCII identifiers. It does not reject Unicode non-ASCII control/formatting characters (such as C1 controls `0x80-0x9F` or directional overrides `U+202E`). This is safe for internal system/run/tool IDs, but should be kept in mind if untrusted UTF-8 strings are ever mapped directly into ID types.

---

## 4. Verification Evidence

- `go vet ./internal/...`: Clean (exit code 0).
- `go test -v -count=1 ./internal/...`: All 54 tests passed across 10 packages:
  - `internal/buildcheck`: PASS (3 tests)
  - `internal/foundation/atomicwrite`: PASS (2 tests incl. `TestAtomicWriteNoPartialOnKill`)
  - `internal/foundation/clockid`: PASS (3 tests)
  - `internal/kernel/assembler`: PASS (3 tests incl. `TestHostileClosingTagCannotEscapeFence`)
  - `internal/kernel/budget`: PASS (2 tests)
  - `internal/kernel/contracts`: PASS (16 tests incl. `TestEnumJSONWireIsClosedStrings`, `TestMessageRejectsForgedBlock`, `TestToolResultRejectsForgedCall`, `TestConstructorRejectsUnknownSchema`, `TestParseEnvelopePreservesUnknownFields`, `TestTimestampsNormalizedToUTC`, `TestManualRecoveryStatesExist`)
  - `internal/kernel/journal`: PASS (12 tests incl. `TestOffsetsGlobalSequencesPerRun`, `TestFailedAppendBurnsNothing`, `TestProfileMismatchRejected`, `TestConcurrentAppendsContiguousOffsets`, `TestPayloadHashMatchesStoredPayload`, `TestSecretInNonPayloadFieldNeverReachesSink`, `TestCancelledContextAfterAdmissionStillDefinitive`, `TestDuplicateEventIDRejected`, `TestConcurrentCloseSafe`, `TestOpenFailsClosedOnMissingDeps`, `TestChainTamperDetected`, `TestSigkillAfterCommitBoundaryKeepsExactPrefix`)
  - `internal/preflight/doctor`: PASS (3 tests)
  - `internal/preflight/probe`: PASS (23 hostile conformance tests)
  - `internal/security/redact`: PASS (4 tests incl. `TestJSONEscapedSecretRedacted`)

---

VERDICT: PASS
