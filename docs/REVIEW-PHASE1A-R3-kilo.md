# REVIEW-PHASE1A-R3 — round-3 verification (8 codex r2 folds)

Scope: verify the 8 folds + NEW defects in the rewritten journal/contracts/redact. `go vet`
clean; `go test -count=1 ./internal/...` GREEN (10 packages).

---

## The 8 folds — all [OK]

**1. [OK] Full-record chain + open/replay enforcement.** `chainInput` (journal.go:51-58) hashes
offset·event_id·run_id·seq·rpv·prevHash·sealedRef·envelope; `Open` runs `verifyChainFull` before
serving (journal.go:216); `Replay` verifies the chain as it streams AND cross-checks denormalized
columns against the envelope (journal.go:386-400). ✓

**2. [OK] Structural-secret REJECT + single canonical envelope.** `structuralFields` + the
`redact.Toucher` interface: a known secret in any non-payload field is REJECTED (journal.go:279-287),
not mutated — so stored and returned identity stay one value. The old full-record byte pass is gone;
payload is redacted semantically BEFORE `NewEnvelope`, so `raw == marshal(env) == returned env`. ✓

**3. [OK] DB-persisted profile + live-writer lease.** `journal_meta` carries `profile` (reopen under
another profile refused, journal.go:163-176) and `writer` = `pid:starttime` (a live foreign writer is
refused; a dead one is taken over, journal.go:185-205). ✓

**4. [OK] Lossless `Envelope.MarshalJSON` Wire merge.** `MarshalJSON` (contracts.go:64-101) marshals
known fields via a `envelopeWire` shadow (no recursion), then overwrites the admitted `Wire` map's
known keys with current values and re-emits sorted — unknown fields survive, mutated known fields win.
✓

**5. [OK] Decoded-JSON redaction incl. `\u` + no-echo schema errors.** `redactJSONValue`
(redact.go:64-126) walks a DECODED tree and redacts string values (escape-spelling-proof); non-JSON
falls back to byte match. `NewEnvelope` no longer echoes `schema_id` (contracts.go:139 — "unknown
schema (version %d)", id dropped). ✓

**6. [OK] Normalized+copied nested contracts.** `ContextBlock.Normalized()` returns an owned,
UTC-canonical copy (contracts.go:277-297); `NewMessage`/`NewToolResult` store the normalized blocks
(contracts.go:327-339, 459-491). ✓

**7. [OK] Closed ActorType + closed event-type set.** `ActorType` is a closed enum with
Marshal/Unmarshal (enums.go:231-244, 532-536); `Open` requires a non-empty event-type set and
`appendOne` rejects unknown types + runs the typed payload validator before persistence
(journal.go:129, 275-297). ✓

**8. [OK] Injected-commit-failure RED + admission-definitive.** `testFailCommit` fault seam after the
INSERT, before Commit (journal.go:332-336); a failed commit leaves `lastOffset`/`lastHash` unadvanced
(actor rollback, journal.go:232-236); `Append` remains admission-definitive (journal.go:358). ✓

## NEW defects (all LOW/MEDIUM, none P0-blocking)

**9. [NEW-ERROR, MEDIUM] Writer-lease check-and-set is not atomic across concurrent openers.** The
lease uses a DEFERRED tx (`db.Begin()`), so the liveness `SELECT writer` is a snapshot read that
predates the other opener's commit. Two simultaneous opens (two processes) can both see "no live
writer" and both UPSERT their lease (`ON CONFLICT DO UPDATE` — no PK conflict), yielding two actors.
The same-process case is also not refused (`oldPid != pid` passes a self-lease). Mitigated: `journal_
offset` PK + `UNIQUE(run_id,sequence)` make the loser's appends fail (no corruption, but the loser
actor is stuck retrying the same offset), and single-daemon P0 never races. Fix before T15: `BEGIN
IMMEDIATE` for the lease tx (and/or refuse a self-lease on a second handle).

**10. [NEW-ERROR, LOW] `chainInput`'s `|`-delimited header is non-injective.** `validID` permits `|`
(0x7C), so `event_id="a|b", run_id="c"` and `event_id="a", run_id="b|c"` produce the same header —
the chain can no longer uniquely bind those fields. IDs are hex in practice and the chain is
hash-only tamper-evidence (not a keyed MAC), so low, but the "authenticates EVERY field" claim is
technically weaker than stated. Fix: length-prefix each field (or binary-encode).

**11. [NEW-ERROR, LOW] `MarshalJSON` is lossless but `UnmarshalJSON` is absent.** A direct
`json.Unmarshal` into `Envelope` (bypassing `ParseEnvelope`) does not populate `Wire`, so the standard
`json.Unmarshal → json.Marshal` round-trip silently drops unknown fields. Losslessness holds only on
the `ParseEnvelope`+`MarshalJSON` path. Low (Replay uses ParseEnvelope), but a future consumer using
raw json will lose fields.

**12. [NEW-ERROR, LOW] Decoded-JSON redaction re-emits objects sorted and drops duplicate keys.**
`redactJSONValue` decodes objects into `map[string]json.RawMessage`, so key order changes and a
duplicate key is collapsed to the last one on re-emission. The hash is recomputed over the re-emitted
bytes, so it stays internally consistent, but the payload's exact byte form is not preserved. Low
(payload is opaque; duplicate keys are pathological).

---

## Verdict

All 8 round-2 folds are correctly implemented and test-verified. The four new observations are
low/medium, are either mitigated by existing DB constraints or bounded to non-P0 paths, and none
blocks the current T04–T06 foundations.

VERDICT: PASS
