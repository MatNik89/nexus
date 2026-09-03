# REVIEW-PHASE2-R4 — round-4 verification (3 codex r3 folds)

Scope: the diff fbd8dc4..f1b9d3e only + NEW defects it introduced. `CGO_ENABLED=0 go vet ./...`
clean; `CGO_ENABLED=0 go test -count=1 ./...` GREEN.

---

## The folds — all [OK]

**1. [OK] Every redirect refused (one grant = one physical request).** `CheckRedirect` now returns an
error unconditionally ("provider: redirects refused — one grant, one physical request", provider.go),
so `http.Client.Do` aborts instead of following an allowlisted 307/308 as a second, differently-
targeted request under the already-consumed grant. The allowlisted-bouncer RED asserts zero backend
hits (provider_test.go +34). ✓

**2. [OK] Planner failure landing from authority state.** `ChatPlanner.landFailure` inspects
`auth.State(op)`: an AUTHORIZED (unconsumed) operation is CANCELLED (nothing physically ran); a
consumed one lands `OutcomeFailedTerminal`; the landing error is joined into the returned error
(`errorsJoin`), so a discarded Report can no longer leave AUTHORIZED forever (planner.go). ✓

**3. [OK] `s7min.Authority.AttemptContext` owns the execution deadline.** It returns a context with
the deadline = the EARLIER of the caller's call deadline and the grant expiry, and REFUSES any
operation not in RUNNING (s7min.go:212-231). `EffectPath` now runs the executor under
`p.grants.AttemptContext(ctx, op, call.Deadline)` (effectpath.go) instead of deriving the deadline
itself. **Judgment:** this satisfies "retry/deadline/cancel = S7 only" for P0 — the deadline and
cancel derivation now sits behind the authority seam; the P2/P3 `attempt_timeout`/backoff vector
remains the declared ceiling. ✓

**4. [OK] STARTED-append failure cancels, never parks UNKNOWN/LOST.** On `onStarted` failure the
effect path calls `p.grants.Cancel(op)` (RUNNING→CANCELLED in S7) and returns (effectpath.go); the
loop then observes `AttemptCancelled` and appends `attempt.cancelled`, which is LEGAL from the
durable AUTHORIZED state (the failed STARTED append left nothing but planned/authorized). The prior
illegal AUTHORIZED→`attempt.lost` replay is gone. ✓

## NEW defects (all LOW)

**5. [NEW-ERROR, LOW] `errorsJoin` uses `%v` for the landing error** (planner.go), so the S7 landing
failure is not unwrappable via `errors.Is`/`errors.As` — a caller cannot programmatically detect
"the attempt landing failed". The cause is still `%w` (unwrappable); the landing message is
informational only. Cosmetic, but `errors.Join` would preserve both.

**6. [NEW-ERROR, LOW] The redirect-refusal error surfaces the redirect URL via `url.Error`.** A
redirect to a secret-bearing URL (e.g. a signed URL with credentials) would have its URL echoed in
the transport error. For P0 the provider is a trusted endpoint and the error passes the daemon's
redaction seam, so low.

**7. [NEW-ERROR, LOW] `AttemptContext`'s `callDeadline.IsZero()` branch is dead code.** A zero
`call.Deadline` is already rejected at `RunTool`'s deadline check (`ErrCallExpired`), so the
zero-deadline fallback to `expires` is unreachable. Defensive, not a bug.

## Declared ceilings — judged acceptable (unchanged)

- terminal-append-failure → durable RUNNING for the crash-recovery owner;
- attempt_timeout/backoff → P2/P3 S7 engine;
- E11 IP-pinning → S6.3 dialer.
None was widened by this diff, and fold 3 moves the P0 deadline derivation INTO S7 rather than
relitigating the ceiling.

---

## Verdict

All three round-3 findings are correctly folded: the transport oracle is now airtight (no redirects),
planner failures land from the authority state with propagated landing errors, S7 owns the P0
execution deadline/cancel, and the STARTED-append failure produces a durable-legal CANCELLED stream
instead of an illegal LOST. The three new observations are low-severity. Full suite green.

VERDICT: PASS
