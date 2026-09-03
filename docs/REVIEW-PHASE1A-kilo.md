# REVIEW-PHASE1A — T04–T06 foundations (adversarial)

Scope: contracts (T04), seams (T05), journal (T06) + redactor. These sit under every later task.
`go vet ./internal/...` clean; `go test -count=1 ./internal/...` GREEN. Two claims verified by probe.

---

## Findings

**1. [SEC] Enums marshal as NUMBERS and there is NO closed-discriminator JSON decode — an
out-of-range enum round-trips into a `Valid()==false` field UNDETECTED.**
`enums.go:5-8` states "JSON decoding goes through closed string tables — an unknown discriminator
is REJECTED". That is not implemented: none of the enum types (`Role`, `EffectClass`, `TrustClass`,
`Sensitivity`, `ErrorCategory`, `Retryability`, `ExecutionKind`, `EffectPhase`, `Decision`,
`PolicyMode`, the state enums, `ResultStatus`) declares `MarshalJSON`/`UnmarshalJSON`. The `Parse*`
functions (`ParseEffectClass`, `ParseTrustClass`, …) exist and are tested
(`contracts_test.go:180-196`) but are dead w.r.t. JSON — nothing wires them into encoding.
Empirical proof: `json.Marshal(NewToolCall(...Effect: EffectReversible...))` →
`"effect_class":2`; `json.Unmarshal({"effect_class":99,...})` → `EffectClass(99)` with `Valid()==false`,
no error. Same for `ContextBlock.Trust`, `Sensitivity`, `ToolResult.Status`, `TypedError.Category/
Retryability`, `CommitReceipt.Phase`. `ParseEnvelope` (schema.go:35-66) only validates envelope-level
fields and leaves the payload opaque, so a corrupt/malicious payload with `effect_class:99` or
`trust_class:99` flows to the first consumer that `json.Unmarshal`s it — silently. This violates
P0.1 "unknown discriminator se odbija" (HARNESS-SPEC:1382) and the E8/E11 fail-closed enum rule, and
a numeric wire format is also non-self-describing (reordering the const block silently changes the
format). Fix: add `MarshalJSON`/`UnmarshalJSON` per enum (marshal the name; unmarshal via `byName`,
reject unknown — the closed string table the comment already promises).

**2. [SEC] The journal persists a REDACTED payload with a STALE payload_hash — the integrity chain
is broken for every redacted event.**
`journal.go:86-103`: `appendOne` redacts `p.Payload` (line 89) but never recomputes `p.PayloadHash`,
so the stored envelope has a payload hash computed over the UNREDACTED bytes while the payload is
redacted. Any consumer verifying `sha256(payload)==payload_hash` (the E11/D2 audit hash-chain, the
P0.1 `payload_hash` MUST) fails or silently sees a tamper signal on exactly the events that carried
secrets. `TestKnownSecretNeverReachesSink` checks only that the secret is absent from the sink — it
never asserts the hash matches the persisted payload, so the defect is invisible. Fix: recompute
`payload_hash` over the redacted bytes inside `appendOne`, or make the caller redact before hashing
and treat the journal redaction as an idempotent second layer.

**3. [QUALITY] `Append` can return `ctx.Err()` after the event has actually committed.**
`journal.go:117-122`: the second `select` can observe `ctx.Done()` between the durable append and the
reply, so the caller sees "cancelled" for an event that WAS admitted. This is the classic
sent-but-unrecorded ambiguity; B2 says the caller must treat it as UNKNOWN→RECONCILING, but the
journal offers no way to resolve it except a full `Replay` scan. Low (the seq is contiguous and the
caller can re-find the event by idempotency_key), but worth a note that "exactly-once ADMISSION"
(B2) is only recoverable, not observable, at this seam.

## Verified sound (not rubber-stamp)

**4. [OK] WAL/busy_timeout/synchronous pragmas ARE applied via the modernc DSN.**
`journal.go:47` uses `_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)&_pragma=synchronous(NORMAL)`;
modernc.org/sqlite v1.58.0 supports `_pragma`. Empirical: `PRAGMA journal_mode` → `wal`,
`PRAGMA synchronous` → `1` (NORMAL, durable in WAL). The SIGKILL test confirms a contiguous,
appendable prefix. ✓

**5. [OK] P0.1 field completeness (field-by-field vs HARNESS-SPEC:1374-1379).**
Envelope (18 MUST fields) + `profile_id` (HARDQ B3, correct augmentation); Message (4/4); ToolCall
(8/8 + `execution_kind` per DESIGN-FIXES-r2 + `profile_id` per B3); ToolResult (7/7 + `commit`);
TypedError (7/7); ContextBlock (11/11). No MUST missing; the additions are all sourced. ✓

**6. [OK] Redaction ordering.** Redaction is the FIRST op in `appendOne`, before `NewEnvelope`
validation, so a known-ref secret in the payload cannot surface in a validation error path. ✓

**7. [OK] Sequence recovery + actor/Close races.** `MAX(sequence)+1` recovery is correct; the single
actor goroutine serializes allocation+INSERT (concurrent appends contiguous — tested); `Close` vs
in-flight `Append` cannot deadlock or drop an already-received append (the actor processes a received
req before re-checking `done`; a blocked send observes `done` and returns "closed"). ✓

**8. [OK] yolo PolicyMode present per F2** (enums.go:156-168; `ParsePolicyMode("SUPER_YOLO")` and
`PolicyMode(0).Valid()` both covered). ✓

---

## Verdict

The seam design (actor serialization, WAL durability, SIGKILL recovery, field completeness,
redaction-before-validation) is sound. But two material fail-closed gaps sit in the foundations:
the enum JSON boundary has no closed-discriminator rejection (the comment claims it; the code does
not — confirmed `effect_class:99` round-trips silently), and redaction desynchronizes `payload_hash`
from the persisted payload. Both propagate into every later consumer and are currently untested.

VERDICT: FAIL
