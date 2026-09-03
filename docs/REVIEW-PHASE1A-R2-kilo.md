# REVIEW-PHASE1A-R2 — round-2 verification (T04–T06 fold)

Scope: verify my 3 round-1 findings + spot-check codex/agy; hunt NEW defects in the rewritten
journal/contracts/redact. `go vet ./internal/...` clean; `go test -count=1 ./internal/...` GREEN.

---

## My round-1 findings — all folded

**1. [OK] kilo #1 — enums now CLOSED on the JSON wire.** All 14 enums implement
`MarshalJSON`/`UnmarshalJSON` through the canonical string tables (enums.go:373-511); numbers and
unknown strings are rejected (`unmarshalJSON` json.Unmarshal's into a `string`, then `parse`). The
comment's "closed string tables" claim is now true. `MANUAL_RECOVERY` added to `RunState`/`AttemptState`
(enums.go:186,222 — codex #7). ✓

**2. [OK] kilo #2 — payload_hash recomputed after redaction.** `appendOne` redacts the payload, then
`sha256(redacted payload)` → `p.PayloadHash` (journal.go:155-157), so integrity describes the STORED
bytes. ✓

**3. [OK] kilo #3 — ctx gates ADMISSION only; post-admission outcome is definitive.** `Append` sends,
then unconditionally `rep := <-req.reply` (journal.go:217-225) — a caller can no longer see
"cancelled" for an event that actually committed. ✓

## Spot-check codex (17) + agy (13) — folded (verified in code/commit)

JournalEvent typed (`journal_offset` GLOBAL separate from per-run `Envelope.Sequence`,
`redaction_policy_version`, `integrity_prev_hash`/`integrity_hash` chain + `VerifyChain`,
`sealed_payload_ref`, `event_id UNIQUE`) — codex #1 ✓. Allocation advances only on durable commit
(actor rolls back `lastOffset`/`lastHash` on error; per-run sequence allocated inside the tx) — #2 ✓.
Schema admission gates CONSTRUCTION too (`schemaKnown` + private `knownSchemas`) — #3 ✓. Unknown-field
preservation via `Envelope.Wire` (json:"-") — #5 ✓. Recursive `Validate()` on ContextBlock/ToolCall/
TypedError; `NewMessage`/`NewToolResult` validate nested blocks — #6 ✓. UTC normalization in
constructors + `System.Now` — #8 ✓. Profile-bound `Open` + append mismatch refusal — #9 ✓.
Full-record redaction pass + no-value-echo ID errors — #10 ✓. JSON-escaped secret variants matched
(redact.go:44-49) — #11 ✓. `sync.Once` Close + fail-closed `Open` (nil redactor, recovery errors) —
#13 ✓. `synchronous=FULL` + AtomicWriter dir-fsync propagation — #14 ✓. Hash-bound assembler fence
(`untrusted-<sha256[:8]>` tag) — #15 ✓. Replay error wrapping + nil-callback rejection — #17 ✓. Agy's
enum JSON / Close race / fence / recursive-validation / payload_hash all covered by the above.

## NEW defects (all LOW)

**4. [NEW-ERROR, LOW] `Append` returns an `Event.Envelope` that is not byte-identical to the
persisted record for non-payload-secret events.** The full-record redaction (journal.go:184) runs on
the marshaled bytes only; the returned `Event.Envelope` (built at line 173) carries payload-redaction
but NOT the full-record pass. So `Event.IntegrityHash` (computed over the redacted stored bytes) does
not verify against `marshal(Event.Envelope)` when a known secret sits in a non-payload field
(e.g. `ActorID`). Harmless for P0 (the caller supplied the secret; the sink is redacted), but a
future consumer that re-derives the hash from the returned envelope would see a phantom tamper.
Fix: build the returned `Event.Envelope` by re-parsing the stored (redacted) `raw`, or redact the
candidate non-payload fields before `NewEnvelope`.

**5. [NEW-ERROR, LOW] Full-record redaction can corrupt envelope JSON and is not re-validated.** The
pass replaces raw byte sequences; a short numeric secret (e.g. `1234`) occurring inside a JSON
number/boolean (or as a substring of a longer number) would be replaced with `[REDACTED:x]` and
produce invalid JSON, which `Replay`'s `ParseEnvelope` would then reject. Not reachable with the P0
known secrets (long alphanumeric keys/tokens, string context only), but the redactor contract is
"byte replacement over encoded JSON"; worth a re-validation (`json.Valid`) guard or a typed-string
redaction path before T15/T16 feed arbitrary payloads.

## Verified sound (non-material notes)

Assembler fence is now hash-bound: forging a closing tag is a 64-bit fixed point over the content
(infeasible); the 8-byte truncation is a deliberate readability tradeoff, not a defect. AtomicWriter
propagates dir open/sync/close errors; the remaining "rename already committed but durability
unconfirmed" ambiguity on dir-fsync failure is the standard atomic-write semantics and correctly
fail-closed (the caller must treat it as UNKNOWN).

---

## Verdict

All of my findings and the codex/agy findings I spot-checked are correctly folded, tests and vet are
green, and the two new observations are low-severity and correctly bounded to non-payload-secret /
arbitrary-payload edge cases, not the P0 happy path.

VERDICT: PASS
