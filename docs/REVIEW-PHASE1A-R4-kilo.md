# REVIEW-PHASE1A-R4 — round-4 final verification (7 codex r3 folds)

Scope: verify the 7 folds + NEW defects in changed lines only. `go vet ./internal/...` clean;
`go test -count=1 ./internal/...` GREEN.

---

## The 7 folds — all [OK]

**1. [OK] Length-prefixed + nullability `chainInput` + open-time envelope/column consistency.**
`chainInput` (journal.go:55-78) uses `binary.AppendUvarint` length prefixes for every field and an
explicit `0`/`1` nullability byte for `sealedRef`, so delimiter-char and NULL-vs-"" collisions are
impossible. `verifyChainFull` now parses each envelope and cross-checks `event_id`/`run_id`/`sequence`
against the denormalized columns (journal.go:500-506), the same checks Replay performs. ✓

**2. [OK] Mandatory `Touches` on Redactor + `ParentEventID` + field table.**
`Redactor` now REQUIRES `Redact([]byte) []byte` + `Touches(string) bool` (redact.go:20-23); `None`
reports no matches (redact.go:181-182), so the structural preflight always runs. `structuralFields`
now enumerates `ParentEventID` plus all 10 string-bearing non-payload fields (journal.go:306-327),
and `appendOne` calls `j.redact.Touches(f)` directly (no optional type assertion, journal.go:343).
✓

**3. [OK] Nonce lease + verify-before-lease + release-on-Close.**
Lease value is `pid:starttime:nonce` (journal.go:235-240); the liveness check no longer exempts
`oldPid == pid`, so a second handle in the SAME process (different nonce, same pid+starttime) is
refused. The chain is verified BEFORE ownership is published (journal.go:221-225 precede 227-274),
and `Close` deletes exactly its own token (`DELETE … WHERE value = leaseToken`, journal.go:528-530).
The commit adds the real two-handle and no-stale-lease REDs. ✓

**4. [OK] Defensive event-registry copy.**
`Open` copies the caller's map into the private `j.events` (journal.go:213-220) and rejects empty
names, so post-open caller mutation cannot widen or race the closed set. ✓

**5. [OK] `MarshalJSON` known-key deletion + cleared-optional RED.**
`envelopeKnownKeys` (contracts.go:63-68) lists all 20 owned keys including the omitempty optionals;
`MarshalJSON` deletes every known key from the preserved `Wire` copy before overlaying the current
encoding (contracts.go:93-98), so a cleared `turn_id`/`tool_call_id`/etc. cannot be resurrected. ✓

**6. [OK] Linear budgeted redact walk.**
`Redact` parses ONCE via `json.NewDecoder`+`UseNumber` (redact.go:144-154) and walks the decoded
tree in O(n) with `maxJSONBytes=1MiB` + `maxJSONDepth=64`; over-budget/invalid input falls back to
byte replacement (fail safe). ✓

**7. [OK] Barrier-based admission-definitive RED.**
`testPauseBeforeReply` seam (journal.go:113, 295-297) pauses the actor between commit and reply, so
the RED cancels ctx in the post-admission/pre-result window and asserts the committed result. ✓

## NEW defects (all LOW/MEDIUM, none P0-blocking)

**8. [NEW-ERROR, MEDIUM] The cross-process CONCURRENT-open lease race persists — the lease tx is
still DEFERRED.** The new `ltx` uses `db.Begin()` (journal.go:241), so the liveness `SELECT writer`
is a snapshot read; two processes opening simultaneously both see "no live writer" and both upsert
their lease (`ON CONFLICT DO UPDATE` — no PK conflict), yielding two actors. Fold 3 fixed the
SAME-process two-handle case (codex r3 #3) but not the cross-process concurrent one; this is the
residual from kilo r3 #9. Mitigated: `journal_offset` PK + `UNIQUE(run_id,sequence)` make the loser
fail, and single-daemon P0 never races. Fix before T15: `BEGIN IMMEDIATE` for the lease tx.

**9. [NEW-ERROR, LOW] `envelopeKnownKeys` is a hand-maintained list.** A future Envelope field added
without a matching entry resurrects that field's stale Wire value when it is a cleared optional. The
comment relies on the wire-merge RED to catch a miss; acceptable, but a struct-tag-derived key set
would remove the footgun.

**10. [NEW-ERROR, LOW] Over-budget redaction falls back to byte replacement, which misses
escape-spelled secrets.** A valid JSON payload >1 MiB (or >64 deep) skips the decoded walk; byte
replacement matches only the raw spelling, so a `\u`-encoded known secret is missed. Non-material for
P0 (known secrets are alphanumeric keys/tokens, payloads are small), but "fail safe" is only
"fail-safe for raw spellings". The decoded-walk re-emission also still sorts keys / collapses
duplicate keys (inherited from the r3 walk) — internally consistent because the hash is recomputed.

---

## Verdict

All 7 round-3 folds are correctly implemented and test-verified; the three new observations are
low/medium, mitigated by existing DB constraints or bounded to edge payloads, and none blocks the
T04–T06 foundations.

VERDICT: PASS
