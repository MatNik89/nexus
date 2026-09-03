# REVIEW-PHASE2 — T13–T17 deep + e2e integration (both gates)

Scope: s7min, effectpath (PEP/S6.9/EffectPath), loop, provider+structured, planner, daemon, repl,
main.go — plus the whole REPL→UDS→daemon→loop→PEP→s7→provider→journal chain. `CGO_ENABLED=0 go vet
./...` clean; `CGO_ENABLED=0 go test -count=1 ./...` GREEN (24 packages). Owner invariants, F2, P0.1/
P0.3, E11, and HARDQ A2/B6/C3/C4/F2 checked against code.

---

## HIGH — none

## MED

**1. [MED] `journalAudit.Record` is fire-and-forget — the F2 "journaled `ALLOWED_BY_YOLO`" is not
fail-closed.** `AuditSink.Record` returns void (effectpath.go:55-57); the real sink
(main.go:78-89) discards `Append`'s error AND uses `context.Background()` (no timeout). A yolo
ASK→ALLOW therefore proceeds even if the journal append fails (journal down / disk full), and a
wedged append actor blocks the session goroutine forever. The DENY half of F2 is unaffected (yolo
only bypasses confirmations), but the "journaled" half is best-effort, not a guarantee. Fix: make
`Record` return an error and have `RunTool`'s yolo branch fail closed (deny) when the audit record
cannot be durably written; give the sink a bounded context.

**2. [MED] The loop journals the attempt as STARTED→FAILED for pre-execution refusals, diverging from
the s7min authority — the durable fold reconstructs a lie.** loop.go:199 appends `attempt.started`
BEFORE `RunTool` (whose Decide/Consume happen inside), and the `ErrNeedsApproval`/DENY/unknown-kind
paths append `attempt.failed` (loop.go:203-211, 213-217). For a DENY or ASK-block the s7min
authority never leaves AUTHORIZED (grant issued, never consumed), yet the journal records
AUTHORIZED→STARTED→FAILED. Since P0.3 makes the journal the single source of truth ("state
reconstructed by fold"), a replayed turn shows a never-run attempt as RUN-then-FAILED. Fix: emit the
attempt lifecycle from the actual s7min state (journal `started` only after `Consume`, and record a
distinct "pending-approval"/"denied" outcome rather than `attempt.failed` for the pre-consume
refusals), or move attempt-event emission into the effect path.

**3. [MED] Provider egress is a construction-time host-string match, not the E11 dial-time
IP-pinning boundary, and that deferral is not declared.** provider.go:71-78 checks
`h == u.Host` once at `NewAPIKey`; the dial itself is a plain `http.Client.Do` with no resolved-IP
pinning, no metadata-IP (169.254.169.254) block, no DNS-rebinding defense. For a single-user
trusted-provider P0 the exploitability is low, but the E11 "pin every IP before connect" decision is
silently deferred rather than recorded as a ceiling. Fix: a topknot comment naming S6.3 as the owner
of the real dialer (and, until then, the allowlist is host-string-only), or pin the allowlist host's
resolved IPs at construction.

## LOW

**4. [LOW] `PayloadHash: "recomputed"` is a magic literal** (loop.go:88, main.go:87) that works only
because the journal overwrites it after redaction. Fragile coupling: if the journal ever stops
recomputing, the literal is stored verbatim. Callers should pass `""` (or the journal API should own
hashing without a placeholder).

**5. [LOW] `observeGuard` validates Trust + lineage but not `Producer`** (loop.go:100-117). Trust is
the security boundary (correct), so a tool cannot mint SYSTEM/USER trust; but a block claiming
`Producer:"user"`/`"system"` with `Trust:TOOL_TRUSTED` passes unobjected. Not a laundering path (the
assembler fences on Trust), but the producer field is a lying-metadata channel worth rejecting.

**6. [LOW] `Extract` returns a salvaged value after the re-ask was already `Report`ed
`OutcomeFailedTerminal`** (structured.go:128-142). The attempt outcome and the returned value then
diverge (salvage-from-reask succeeds without re-reporting). Cosmetic for P0 (salvage is a GENERAL
repair), but the s7 record understates the result.

**7. [LOW] `decodeStrict`'s trailing-data check via `dec.More()` misses a lone `]`/`}` after the
value** (structured.go:48) — `More()` returns false on a `]`/`}`, so `{"a":1}]` is accepted.
Negligible (all real trailing data is caught), but the "no trailing data" claim is slightly wider
than the check.

---

## Verified sound (integration seams)

- **S6.0 default-deny / ASK≠ALLOW:** `PEP.Decide` default-DENY on unknown tool or invalid rule
  (effectpath.go:152-163); ASK requires a consumed exact-intent approval (or yolo). ✓
- **S7 sole retry owner:** `Authority` serializes under one mutex; `Issue` refuses a second grant;
  `Consume` is single-use with a constant-time nonce check and a step through the canonical attempt
  table; `Report` refuses everything from UNKNOWN; cancel-before-issue blocks a later grant. No grant
  race (mutex-serialized). ✓
- **P0.3 journal single-writer:** one daemon process owns the journal behind a 0600 UDS; `sameUID`
  via SO_PEERCRED; clients never touch SQLite. ✓
- **S6.9 order-only:** `Middleware` has no policy method — order hooks cannot set ALLOW/ASK/DENY
  (structural). `orderOnlyMW` is honest. ✓
- **Yolo containment (F2):** mode bound at hello (session construct), never reachable from a `chat`
  payload (frame carries no mode); prompt always shows `[YOLO]`; DENY unchanged; `noSandbox` refuses
  every process exec until T26 (fail-closed). ✓
- **Trust fencing:** `assembler.Base` runs on every iteration; `observeGuard` rejects trust laundering
  and missing lineage; tool errors become TOOL_TRUSTED observations, never turn failures. ✓
- **HITL gate honesty:** `ErrNeedsApproval` is surfaced as a turn failure with the declared -min
  ceiling (durable `TurnSuspended` = B6 owner task); never packed as an observation the model could
  self-approve. ✓
- **Secret handling:** key lives only in a private field; errors echo host/env-var NAME, never the
  value; `knownSecretRefs` feeds the redactor before the journal. ✓

## RED causality

The ledger REDs I traced are causal (fail on the naive implementation), e.g. s7min no-retry +
cancel-blocks-issue, PEP default-deny + yolo-never-deny, loop provenance-laundering + identical-call
breaker, provider structured-output-never-silent. I found no vacuous RED in the reviewed files.

---

## Verdict

The owner-invariant seams (S6.0, S7, S6.9, P0.3, yolo containment, trust fencing, secret handling)
are correctly enforced and the first e2e chain is wired end-to-end. Three MED integration-seam
defects remain: the yolo audit is fire-and-forget (not fail-closed journaling), the attempt lifecycle
is journaled optimistically and diverges from the s7min authority for pre-execution refusals, and the
provider egress deferral of E11's IP-pinning is undeclared. These are exactly the seam class the
integration gate exists to catch.

VERDICT: FAIL
