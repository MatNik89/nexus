# REVIEW-PHASE2-R3 — round-3 verification (10 codex r2 folds)

Scope: the fold diff a9f8874..fbd8dc4 + NEW defects. `CGO_ENABLED=0 go vet ./...` clean;
`CGO_ENABLED=0 go test -count=1 ./...` GREEN. Artifact identity proven: `git ls-files cmd/nexus/`
lists main.go/main_test.go/probe_linux.go, `.gitignore` is anchored `/nexus`, and a clean
`git archive HEAD` builds `./cmd/nexus` successfully.

---

## The 10 codex r2 findings — all folded

**1. [OK] Artifact identity.** `cmd/nexus/` is now tracked; the unanchored `nexus` ignore pattern is
`/nexus`; `git archive HEAD` + `go build ./cmd/nexus` succeeds. The production entry point and
composition root are in the commit. ✓

**2. [OK] Provider grant target binding + Report errors.** `APIKey.Target()` = `provider:<host>`;
`transport` rejects `g.TargetID != p.Target()` (provider.go:163-171) before consuming; `Report`
errors are propagated, not discarded. The P0.2 duplicate-use oracle is now target-bound and
airtight. ✓

**3. [OK] Re-ask state leak closed.** `Extract` tracks `reaskLive` and reports exactly one outcome on
every exit: a consumed-then-error callback lands `OutcomeFailedTerminal`; an unconsumed callback is
CANCELLED and its bytes IGNORED (structured.go:126-151). No RUNNING leak, no ungoverned bytes. ✓

**4. [OK] Exact-call binding + S7-owned deadline.** `ToolTarget` is now a sha256 digest over
ids+arguments+schema-hash+effect+kind+deadline+idempotency+profile (effectpath.go:58-71), so a
modified call with the same IDs can no longer ride an old grant. `RunTool` runs under
`context.WithDeadline(ctx, call.Deadline)` and classifies a deadline-killed READ_ONLY call as
CANCELLED (effectpath.go:394-409); the attempt_timeout/backoff vector stays the P2/P3 S7 engine's
(declared ceiling). ✓

**5. [OK] Round-1 #4 (effectful → UNKNOWN).** Unchanged from r2; still correct. ✓

**6. [OK] Full `ToolResult` validation via the canonical constructor.** `RunTool` now re-runs
`contracts.NewToolResult(call, …)` and rejects any result that fails correlation, closed status,
ordered timestamps, typed-error shape, output-block normalization, or receipt binding
(effectpath.go:417-426) — no more field-picking. ✓

**7. [OK] Crash/post-effect window.** `SetStartObserver` runs AFTER a successful `Consume` and BEFORE
dispatch (effectpath.go:386-393); a failed observer parks UNKNOWN, so consumption without a durable
record never runs. The loop wires `attempt.started` there (loop.go:246-248). The
terminal-append-failure case leaves durable RUNNING for the crash-recovery owner (loop.go:268-273) —
the declared ceiling with a named owner. ✓

**8. [OK] Round-1 #7 (multi-call replay).** Unchanged from r2; still correct. ✓

**9. [OK] Secret redaction before the model/UI boundary.** `errorObservation` takes a
`redact.Redactor` and runs the error text through `RedactText` (known-ref redaction via JSON round
trip, loop.go:138-170); sensitivity is `INTERNAL`, not the lowest literal. ✓

**10. [OK] Production audit sink in HEAD.** The real `journalAudit` (atomic identity, bounded
context, returned `Append` error) now lives in the tracked `cmd/nexus/main.go`. ✓

**11. [OK] Loopback by IP literal.** `loopbackHost` uses `net.SplitHostPort` + `net.ParseIP` +
`ip.IsLoopback()` (plus a `localhost` convenience), so `127.attacker.example` no longer passes the
cleartext exception (provider.go:82-92). ✓

**12. [OK] Round-1 #11 (OnError preserves causal error).** Unchanged from r2. ✓

**13. [OK] Composition-root test + PEP path.** `TestCompositionRootServesConversation`
(main_test.go) exercises the real production composition root; the task notes it caught and fixed a
REAL target-mismatch bug (the daemon's empty rule/tool maps could not drive the effect path). The
judgment that this is a genuine production-wiring fix (not a vacuous test) is confirmed by the
composition root now passing the target-bound provider/effectpath seams. ✓

**14-16. [OK]** Round-1 #13/#14/#15 remain folded as in r2. ✓

## NEW defects (all LOW)

**17. [NEW-ERROR, LOW] `loopbackHost` special-cases `localhost`.** A poisoned `/etc/hosts` mapping
`localhost` to a remote address would pass the cleartext exception and send the bearer key without
TLS. Low (single-user controls `/etc/hosts`; the special-case exists for local-provider convenience).

**18. [NEW-ERROR, LOW] A redirect to `http://localhost` is permitted** (provider.go:140) — a local
cleartext listener could receive the conversation. Low (same-user local listener).

**19. [NEW-ERROR, LOW] READ_ONLY "cancelled by deadline" misclassifies a parent-context cancel.**
`context.Cause(execCtx)` is non-nil for BOTH deadline expiry and parent cancellation, so a daemon
shutdown mid-tool is labelled "cancelled by deadline" (effectpath.go:406-409). The terminal state
(CANCELLED) is the same either way; cosmetic.

## Declared ceilings — judged acceptable

- terminal-append-failure leaves durable RUNNING → crash-recovery owner reconciles (named owner). ✓
- attempt_timeout/backoff vector → P2/P3 S7 engine (named trigger). ✓
- E11 IP-pinning → S6.3 dialer (named owner + upgrade trigger in the provider comment). ✓

---

## Verdict

All 10 round-2 findings are folded and verified, including the blocking artifact-identity fix (clean
archive builds the tracked entry point) and the composition-root test that caught a real production
bug. The three new observations are low-severity, and the three declared ceilings each carry a named
owner/trigger. Full suite green.

VERDICT: PASS
