# REVIEW-PHASE1A-agy.md — Phase 1 Partial Code Review (Batch T04–T06)

**Target Ref:** `slice/p0-phase1` @ HEAD (`062d156`)  
**Reviewer:** `agy` (Antigravity)  
**Scope:**
- `internal/kernel/contracts/{enums,ids,contracts,schema}.go` + `contracts_test.go` (T04)
- `internal/foundation/clockid/*`, `internal/foundation/atomicwrite/*` (T05)
- `internal/kernel/{budget,assembler}/*` (T05)
- `internal/kernel/journal/*` (T06)
- `internal/security/redact/*` (T06)
**Contracts & Specs:** `docs/tasks-P0.md` T04–T06, `HARNESS-SPEC.md` Annex P0.1/P0.3, `HARDQ-CONSOLIDATED.md` B3/B7/C1/C7/F2, `ARCHITECTURE-ESSENTIALS.md`, `DESIGN-FIXES-r2.md`.

---

## 1. Executive Summary

Phase 1 establishes the foundational data contracts, time/ID/write seams, and the single-write-owner durability journal. The overall architecture is clean and closely matches the -min boundaries. The crash-consistency tests for `AtomicWriter` and `Journal` (`SIGKILL` child process patterns) are robust and non-vacuous.

However, an **adversarial review reveals 2 blocking defects (`BREAK`), 1 security finding (`SEC`), 2 test gaps (`RED`), and 3 quality/robustness issues (`QUALITY`)**. Most critically:
1. **Enum JSON serialization is broken across the board**: None of the 14 closed enums implement `TextMarshaler`/`TextUnmarshaler` or `json.Marshaler`/`json.Unmarshaler`. In Go, `type X uint8` serializes as raw integers (`"role": 1`), wire JSON using spec string discriminators (`"role": "USER"`) fails unmarshaling with type errors, and invalid integers (`99`) bypass discriminator validation undetected.
2. **`Journal.Close()` is not safe against concurrent invocation**: Calling `Close()` concurrently triggers `panic: close of closed channel` despite documented idempotency.
3. **`Assembler.Base` lacks escape defense against fence closing tags**: Untrusted content containing `</untrusted-data>` is emitted raw without escaping, permitting fence breakout in prompt assembly.

---

## 2. Two-Axis Assessment

### Standards Axis
- Conforms to Go project structure and `CGO_ENABLED=0` constraint.
- No `map[string]any` in kernel contracts (opaque payloads use `json.RawMessage`).
- Minimal-machinery / ponytail contract respected (no speculative dependencies or premature abstractions).
- **Smell Baseline finding**: In `internal/kernel/journal/journal.go:151-160`, `Close()` relies on a non-atomic `select` check on `j.done` rather than `sync.Once`, creating a concurrent race condition.

### Spec Axis
- **Annex P0.1**: All canonical fields for `Envelope`, `Message`, `ContextBlock`, `ToolCall`, `ToolResult`, and `TypedError` are present.
- **HARDQ B3**: Mandatory non-null `ProfileID` is stamped and enforced across `Envelope`, `ToolCall`, and IDs.
- **HARDQ B7 / P0.3**: Single append actor owns sequence allocation and serialization; WAL + bounded timeout set in DSN.
- **HARDQ C1**: Known-ref exact-match redaction runs prior to envelope construction and journal persistence.
- **HARDQ C7**: Closed schema registry rejects unknown schema ID/version unprocessed with byte-for-byte raw preserved.
- **HARDQ F2**: `PolicyMode` enum (`DEFAULT`, `YOLO`) defined with zero-value invalid and fail-closed parser.

---

## 3. Deep-Dive Attack Axes

### Axis 1: P0.1 MUST-List Completeness & Field Matrix

| Spec Contract | Go Struct | Required Fields Checked | Notes | Status |
|---|---|---|---|---|
| `Envelope` (P0.1) | `contracts.Envelope` | `schema_id`, `schema_version`, `event_id`, `event_type`, `run_id`, `turn_id`, `tool_call_id`, `parent_event_id`, `sequence`, `emitted_at`, `actor_type`, `actor_id`, `principal_id`, `tenant_id`, `workspace_id`, `attempt_no`, `idempotency_key`, `payload`, `payload_hash` + `ProfileID` (B3) | All 19 fields validated in `NewEnvelope`. | `[OK]` |
| `Message` (P0.1) | `contracts.Message` | `message_id`, `role`, `content_blocks[]`, `created_at` | Validated in `NewMessage`. Inner blocks slice validated non-empty. | `[OK]` |
| `ToolCall` (P0.1) | `contracts.ToolCall` | `tool_call_id`, `tool_id`, `arguments`, `arguments_schema_hash`, `effect_class`, `deadline`, `attempt_no`, `idempotency_key` (mandatory for non-READ_ONLY) + `ExecutionKind` + `ProfileID` | Validated in `NewToolCall`. Idempotency key mandatory for `EffectReversible` / `EffectIrreversible`. | `[OK]` |
| `ToolResult` (P0.1) | `contracts.ToolResult` | `tool_call_id`, `attempt_no`, `status`, `output_blocks[]`, `error`, `started_at`, `finished_at`, `commit` | Validated in `NewToolResult`. Correlates with originating `ToolCall`. `FAILED` requires typed error. | `[OK]` |
| `TypedError` (P0.1) | `contracts.TypedError` | `code`, `category`, `retryability`, `safe_message`, `retry_after`, `origin`, `cause_event_id` | Validated in `NewTypedError`. `RetryAfter` requires `retry_after`. | `[OK]` |
| `ContextBlock` (P0.1) | `contracts.ContextBlock` | `block_id`, `kind`, `content` XOR `content_ref`, `content_hash`, `source_uri`, `producer`, `trust_class`, `sensitivity`, `lineage[]`, `observed_at`, `expires_at` | XOR strictly enforced: `(p.Content == nil) == (p.ContentRef == nil)`. Lineage required non-nil. | `[OK]` |

---

### Axis 2: Journal Correctness & Concurrency

1. **Actor / Channel Lifecycle**:
   - `Append()` communicates with `actor()` over unbuffered channel `j.reqs`. Because sending on an unbuffered channel blocks until `actor` receives, the request is guaranteed to be processed once sent.
   - `appendReq.reply` has buffer 1, preventing the actor from blocking if `ctx.Done()` cancels the caller while waiting for the reply.
   - **Flaw**: `Close()` in `internal/kernel/journal/journal.go:151-160` uses `select { case <-j.done: default: close(j.done) }`. In concurrent execution, two callers can both take `default:` and call `close(j.done)`, triggering `panic: close of closed channel`.

2. **Sequence Recovery After Crash**:
   - `actor()` runs `SELECT COALESCE(MAX(sequence),0) FROM events` on startup. On clean startup or after crash recovery, `next` is correctly initialized to the highest committed sequence number.

3. **WAL Pragmas**:
   - `journal.go:47` opens `file:%s?_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)&_pragma=synchronous(NORMAL)`.
   - Verified by test run: `journal.db-wal` is generated on disk and tested in `TestKnownSecretNeverReachesSink`.

4. **Redaction Ordering**:
   - In `journal.go:89`, `p.Payload = json.RawMessage(j.redact.Redact([]byte(p.Payload)))` runs before `contracts.NewEnvelope(p)` validation and SQLite insert.
   - Error messages from `NewEnvelope` do not echo raw payload content.
   - **Quality Note**: If a caller precomputed `p.PayloadHash` on unredacted data, `p.PayloadHash` remains the unredacted hash because `appendOne` does not recompute or check hash consistency.

5. **Replay Semantics**:
   - `Replay(from, fn)` orders by `sequence ASC`, checks `ParseEnvelope`, short-circuits on callback error, and checks `rows.Err()`.

---

### Axis 3: Fail-Closed Gaps & Enum JSON Round-Trip

1. **Enum JSON Serialization Defect (`[BREAK]`/`[SEC]`)**:
   - In `internal/kernel/contracts/enums.go`, enums are defined as `type <Name> uint8`.
   - The docstring at `enums.go:6-8` states: *"JSON decoding goes through closed string tables — an unknown discriminator is REJECTED, never defaulted (Annex P0.1: 'unknown discriminator se odbija')*".
   - **Reality**: No enum implements `encoding/json` (`Marshaler`/`Unmarshaler`) or `encoding` (`TextMarshaler`/`TextUnmarshaler`).
   - Standard `encoding/json` serializes them as integers (`"trust_class": 1`).
   - If wire JSON contains canonical strings per Annex P0.1 (`{"role": "USER"}`), `json.Unmarshal` fails with:
     `json: cannot unmarshal string into Go struct field ... of type contracts.Role`
   - If wire JSON contains an invalid out-of-range integer (`{"role": 99}`), `json.Unmarshal` unmarshals `99` without error, bypassing the closed discriminator requirement.

---

### Axis 4: RED Quality & Test Robustness

1. **Positive RED Qualities**:
   - `TestAtomicWriteNoPartialOnKill` (`atomicwrite_test.go:44-78`) exercises actual `SIGKILL` mid-write and verifies full preservation of old content.
   - `TestSigkillAtCommitBoundaryLeavesContiguousPrefix` (`journal_test.go:173-204`) executes continuous background appends under `SIGKILL`, verifying contiguous sequences and post-crash appendability.
   - `TestKnownSecretNeverReachesSink` (`journal_test.go:103-135`) checks physical `.db` and `.db-wal` disk bytes for secret leaks.
   - `TestParseEnvelopeUnknownSchemaPreservesRaw` (`contracts_test.go:209-228`) verifies raw payload preservation on unknown schema version/ID.

2. **RED Gaps**:
   - `contracts_test.go` contains no JSON marshal/unmarshal tests for domain structs containing enums (`Message`, `ContextBlock`, `ToolCall`, `ToolResult`, `TypedError`). Only standalone `Parse<Enum>` functions were tested.
   - `assembler_test.go` does not test payload containing fence-breakout sequences (`</untrusted-data>`).

---

## 5. Findings Ledger

### `[BREAK]` Critical / Functional Defects
- **`[BREAK]` `internal/kernel/contracts/enums.go:11-240`**: Enums do not implement `TextMarshaler` / `TextUnmarshaler` (or `json.Marshaler` / `json.Unmarshaler`). Canonical string values from Annex P0.1 cannot be unmarshaled from JSON, and marshaling emits integers instead of closed string discriminators. Out-of-range integers decode into invalid enum states without failing closed.
- **`[BREAK]` `internal/kernel/journal/journal.go:151-160`**: `Journal.Close()` is not race-free. Concurrent calls to `Close()` trigger `panic: close of closed channel` because `select { case <-j.done: default: close(j.done) }` is not atomic. Must use `sync.Once`.

### `[SEC]` Security / Boundary Hazards
- **`[SEC]` `internal/kernel/assembler/assembler.go:45`**: `Assembler.Base` embeds untrusted context block content directly inside `<untrusted-data>...</untrusted-data>` without escaping closing tags. An untrusted prompt payload containing `</untrusted-data>\n[SYSTEM ...]` escapes the structural fence.

### `[RED]` Test Discipline & Coverage Gaps
- **`[RED]` `internal/kernel/contracts/contracts_test.go:230-246`**: JSON round-trip tests only test `Envelope` (which has no direct enum fields). Tests for `Message`, `ContextBlock`, `ToolCall`, `ToolResult`, and `TypedError` JSON round-tripping with spec string discriminators are absent, allowing the enum JSON bug to pass undetected.
- **`[RED]` `internal/kernel/contracts/contracts_test.go:83-97`**: `TestContextBlockRejectsInvalidProvenance` does not verify rejection of invalid `Sensitivity` values (e.g. `Sensitivity(99)`).

### `[QUALITY]` Code Quality & Resilience
- **`[QUALITY]` `internal/kernel/contracts/contracts.go:323-344`**: `NewToolResult` does not validate that the incoming `call ToolCall` is valid (e.g. non-empty `ToolCallID`, positive `AttemptNo`), allowing uninitialized `ToolCall{}` to create inconsistent `ToolResult`.
- **`[QUALITY]` `internal/kernel/contracts/contracts.go:202-220`**: `NewMessage` checks `len(blocks) == 0` but does not validate individual blocks in `blocks`, allowing uninitialized `ContextBlock{}` structs to be wrapped in a `Message`.
- **`[QUALITY]` `internal/kernel/journal/journal.go:89-91`**: `p.PayloadHash` is not updated or checked after secret redaction. If the caller provided a hash of unredacted bytes, the stored `PayloadHash` will not match the hash of the stored redacted payload.

---

## 6. Verification Command Output

- `go vet ./internal/...`: Clean (exit code 0).
- `go test -v -count=1 ./internal/...`: All 42 tests passed.
  - `internal/buildcheck`: PASS
  - `internal/foundation/atomicwrite`: PASS (incl. `TestAtomicWriteNoPartialOnKill`)
  - `internal/foundation/clockid`: PASS
  - `internal/kernel/assembler`: PASS
  - `internal/kernel/budget`: PASS
  - `internal/kernel/contracts`: PASS
  - `internal/kernel/journal`: PASS (incl. `TestSigkillAtCommitBoundaryLeavesContiguousPrefix`)
  - `internal/preflight/doctor`: PASS
  - `internal/preflight/probe`: PASS
  - `internal/security/redact`: PASS

---

VERDICT: FAIL
