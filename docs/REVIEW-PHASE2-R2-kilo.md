# REVIEW-PHASE2-R2 — round-2 verification (codex 12H/4M + kilo 3M/4L fold)

Scope: the fold diff 17f5603..a9f8874 + NEW defects it introduced. `CGO_ENABLED=0 go vet ./...`
clean; `CGO_ENABLED=0 go test -count=1 ./...` GREEN (the previously-intermittent procx Terminate
tests now pass; procx_linux.go was touched in the fold to fix that race).

---

## My round-1 findings — folded / declared

**1. [OK] `journalAudit` fire-and-forget (kilo MED #1 / codex #9).** `AuditSink.Record` now returns
an error (effectpath.go:69-71); the yolo branch fails closed when the audit is not durable
(effectpath.go:324-327); `journalAudit` propagates `Append` errors. ✓

**2. [OK] Attempt-lifecycle divergence (kilo MED #2 / codex #6).** The loop now journals the attempt
lifecycle FROM the authority's actual post-`RunTool` state (loop.go:219-247): pre-execution refusal
(grants still AUTHORIZED) folds as CANCELLED via `grants.Cancel`; executed attempts fold STARTED +
the true terminal; a post-exec journal-append failure returns "treat as UNKNOWN pending
reconciliation". ✓

**3. [OK] Provider egress host-string-only (kilo MED #3 / codex #10).** `NewAPIKey` rejects
non-loopback plaintext `http`; `CheckRedirect` re-applies the allowlist decision and refuses
plaintext redirects; the ceiling ("S6.3 owns the real IP-pinning dialer") is now recorded in a
topknot comment. ✓

**4. [OK] `PayloadHash:"recomputed"` (kilo LOW #4).** KEPT — declared skip (journal recomputes; API
cleanup deferred), matching the task's adjudication. ✓

**5. [OK] `observeGuard` producer channel (kilo LOW #5).** Reserved producer names (`user`/`system`/
`repl`/`loop`) are now rejected (loop.go:118-123). ✓

**6. [OK] Extract salvage divergence (kilo LOW #6) + `decodeStrict` (LOW #7).** Folded via the
transport-consumed grant and result-validation rewrites; no separate divergence remains. ✓

## Codex HIGH findings — spot-checked folded

#1/#2 transport-consumed grant (`provider.transport` consumes `p.auth.Consume(g)` immediately before
the wire; `Chat`/`Stream`/`Probe` all take a grant) ✓ · #3 `call.Validate` + deadline + `ToolTarget`
call-binding at the effect boundary ✓ · #4 `failureOutcome` → UNKNOWN for effectful post-dispatch
failure ✓ · #5 result-correlation check + status-driven outcome + effectful-success-requires-receipt ✓
· #7 `ToolCallID` carried on attempt envelopes (multi-call replay) ✓ · #8 error text → UNTRUSTED
fence + 1024 bound ✓ · #11 `onErr` preserves the causal error (middleware can never erase a refusal) ✓
· #12 full-spine e2e through real planner+provider seam (daemon_test +107, planner +91) ✓ · #13
streaming wired (planner streams; daemon emits deltas) ✓ · #14 literal RED symbols restored ✓ ·
#15 procx determinism (procx_linux.go +29) ✓.

## The five judgments — all sound

**(a) P0.2 oracle is now airtight.** Grant consumption moved INTO `transport`, so one physical wire
call == one consumed grant; a second call on the same grant is refused by `Consume` (single-use).
The decorative re-ask grant (codex #2) is gone — `Extract`'s `ReAsk` now runs through the governed
transport.

**(b) Pre-exec refusal → CANCELLED is honest.** The amended P0.1 cancel edge is legal from
AUTHORIZED, so a DENY/ASK-block/unknown-kind (grant issued, never consumed) folding as CANCELLED is
the honest terminal — nothing physically ran. The -min "cancel on needs-approval" is declared (B6
durable `TurnSuspended` is the T21+ owner).

**(c) Effectful-without-receipt → UNKNOWN + loop STOP is E9-conformant.** `failureOutcome` returns
UNKNOWN for any effectful call without a `BeforeCommit` receipt; `report` tags the error with
`ErrEffectUnknown`; the loop's `errors.Is(…ErrEffectUnknown)` stops the turn — no blind continuation
past a possibly-committed effect.

**(d) The full-spine e2e now crosses every production seam.** The daemon test drives REPL→UDS→daemon→
loop→real planner→real provider transport→journal with a deterministic transport, not an echoPlanner.

**(e) `PayloadHash` literal kept** — journal recomputes; API cleanup deferred. Declared, not silent.

## NEW defects (all LOW)

**7. [NEW-ERROR, LOW] The attempt STARTED + terminal events are appended retroactively** — all after
`RunTool` returns (loop.go:232-240), so `attempt.started`'s `EmittedAt` is after the actual execution.
State sequence is correct; timing is approximate. Low (audit nuance).

**8. [NEW-ERROR, LOW] "Journal append failed after a physical attempt" fails the turn but does not
journal the UNKNOWN/lost state** (loop.go:233, 241). The durable fold then shows the attempt stuck
AUTHORIZED, not UNKNOWN, because the terminal append is the one that failed (journal down). The
"pending reconciliation" is a caller instruction, not a journal-visible marker. Low (inherent to a
down journal; reconciliation is the E9 owner's).

---

## Verdict

All three of my MED findings and the codex HIGH findings are correctly folded; the five judgments
(a)–(e) hold; the only new observations are two low-severity audit-nuance items. The full suite is
green including the previously-flaky procx gate.

VERDICT: PASS
