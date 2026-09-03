# Phase 2 verification round 3 (Codex)

Target: `slice/p0-phase2` at `fbd8dc4f06166e4acf77adea4f3d9718b75de99d`; review scope `a9f8874..fbd8dc4` only. Findings 1–17 correspond to `REVIEW-PHASE2-R2-codex.md`.

1. **[OK] Round-2 #1/#10 artifact identity is folded.** `.gitignore:5` now anchors only the root binary as `/nexus`; `git ls-files --error-unmatch cmd/nexus/main.go` returns `cmd/nexus/main.go`, and the clean `git archive HEAD` build succeeds. The production composition root and fail-closed `journalAudit` are therefore part of the reviewed revision.

2. **[UNFOLDED] Round-2 #2 is improved but the P0.2 transport oracle is still not airtight.** The direct path now compares the grant target with `APIKey.Target()` before consuming it (`internal/llm/provider/provider.go:160-175`), and successful unconsumed provider replies are rejected (`internal/llm/planner/planner.go:109-125`). Two residual counterexamples remain:
   - `http.Client.Do` follows an allowlisted redirect inside the one call at `provider.go:184`, while `CheckRedirect` permits any host in `p.allowed` (`provider.go:131-144`). Thus host A can return 307/308 to allowlisted host B and cause two physical HTTP requests under the single grant consumed at `provider.go:174`; the grant was also bound to A, not B. `TestTransportConsumesGrantExactlyOnce` counts only two explicit `Chat` calls and has no allowed-redirect case (`provider_test.go:141-175`). For P0, reject redirects (or require a fresh target-bound grant before following one).
   - Both provider-error branches still discard `Authority.Report` errors (`internal/llm/planner/planner.go:105-107,118-121`). A conforming-looking adapter that rejects locally before consuming its grant makes `Plan` return an error while S7 remains permanently `AUTHORIZED`. Inspect S7 state: cancel an unconsumed operation; report a consumed one, and propagate any landing failure.

3. **[OK] Round-2 #3 is folded.** `Extract` inspects the authority state after the callback, ignores bytes from an unconsumed callback and cancels it, lands consumed failures terminally, and refuses an accepted value when outcome recording fails (`internal/llm/provider/structured.go:121-172`). `TestReaskStateNeverLeaks` covers both former counterexamples.

4. **[UNFOLDED] Round-2 #4's exact-call digest is folded, but deadline/cancel ownership is not.** `ToolTarget` now length-prefixes every material `ToolCall` field into the target digest (`internal/kernel/effectpath/effectpath.go:58-79`), and the call deadline reaches the executor. However, `EffectPath` itself creates and classifies the deadline context (`effectpath.go:394-410`); S7 supplies neither that context nor the cancellation decision. This still contradicts the binding owner rule “retry/deadline/cancel = S7 only” (`CLAUDE.md:31-34`) and the Annex P0.2 owner/invariant (`HARNESS-SPEC.md:1386-1392`). The accepted ceiling defers `attempt_timeout`/backoff to P2/P3; it does not transfer the existing call-deadline owner to S6.9/effectpath. Put the P0 deadline/cancel derivation behind the `s7min.Authority` seam, or record an explicit owner amendment.

5. **[OK] Round-2 #5 remains folded.** Effectful execution without a proving receipt lands `UNKNOWN`, carries `ErrEffectUnknown`, and stops the loop; the accepted reconciliation-owner ceiling is unchanged.

6. **[OK] Round-2 #6 is folded in code.** Executor output is rebuilt through `contracts.NewToolResult` before middleware or observation (`internal/kernel/effectpath/effectpath.go:412-426`), so the canonical correlation, timestamp, typed-error, block-normalization, and receipt checks now gate the result.

7. **[NEW-ERROR] The new pre-dispatch STARTED seam creates an illegal durable stream when its append fails transiently.** `EffectPath` consumes the grant, then an `onStarted` error changes the in-memory attempt to `UNKNOWN` without dispatch (`internal/kernel/effectpath/effectpath.go:381-392`). The loop subsequently observes `AttemptUnknown` and appends `attempt.lost` (`internal/kernel/loop/loop.go:242-272`). Because the failed STARTED append left the durable state at `AUTHORIZED`, replay sees `AUTHORIZED -> attempt.lost`, which the canonical table rejects; `attempt.lost` is legal only from `RUNNING` (`internal/kernel/machine/machine.go:198-204`). A one-shot commit/projection failure followed by recovery is enough to persist this corrupt sequence. Since dispatch definitely did not start, cancel the consumed in-memory attempt and append `attempt.cancelled` (legal from `AUTHORIZED` durably and `RUNNING` in S7), or carry a typed start-persistence failure that prevents the loop from emitting LOST. Add a causal injected-one-append-failure RED; the current suite has no test for observer ordering or failure.

8. **[OK] Round-2 #8 remains folded.** Multi-call replay identity is untouched by the fold.

9. **[OK] Round-2 #9 is folded.** Tool errors pass the configured known-reference redactor before becoming bounded `UNTRUSTED_EXTERNAL`/`INTERNAL` observations, and daemon error frames use the same redaction seam (`internal/kernel/loop/loop.go:128-169`; `internal/app/daemon/daemon.go:190-195`). The secret-bearing error RED is causal for the model-observation path.

10. **[OK] Round-2 #10 is folded.** The production `journalAudit` is tracked, returns append failure to the PEP, uses an atomic event counter, and bounds the append context (`cmd/nexus/main.go:70-101`).

11. **[OK] Round-2 #11 is folded.** The plaintext exception uses `net.ParseIP(...).IsLoopback()` (plus exact `localhost`), so `127.attacker.example` is refused (`internal/llm/provider/provider.go:77-91`). The explicitly accepted S6.3 resolved-IP pinning ceiling is not reopened.

12. **[OK] Round-2 #12 remains folded.** No changed line weakens causal-error preservation through `OnError`.

13. **[OK] Round-2 #13 is folded within the declared final-answer P0 slice.** `TestCompositionRootServesConversation` calls the production `buildDaemon`, then drives REPL -> UDS -> daemon -> loop -> streaming planner -> governed provider -> journal (`cmd/nexus/main_test.go:27-74`). This is causal: production now passes `prov.Target()` (`cmd/nexus/main.go:156-170`), the exact seam whose former base-URL/host mismatch the test exposed. It still does not execute a tool/PEP action, but the recorded structured-tool-planner ceiling makes that an explicit later trigger rather than a claim hidden by test-only wiring.

14. **[OK] Round-2 #14 remains folded.** The streaming module seam and ordered delta path are unchanged except for the required redactor dependency.

15. **[OK] Round-2 #15 remains folded.** The ledger-literal executor/discriminator tests were not weakened by this diff.

16. **[OK] Round-2 #16 remains folded.** The diff does not touch the converged `procx` termination logic.

17. **[OK] Round-2 #17 spot-check remains valid.** The accepted `PayloadHash` cleanup deferral and S6.3 IP-pinning ceiling were not expanded. No changed line revives the previously resolved Kilo/Agy findings.

Verification:

- `CGO_ENABLED=0 go vet ./... && CGO_ENABLED=0 go test -count=1 ./...` — PASS; 24 tested packages green.
- `git ls-files --error-unmatch cmd/nexus/main.go` — PASS; exactly one tracked path returned.
- Clean `git archive HEAD` at `fbd8dc4`, then `CGO_ENABLED=0 go build ./cmd/nexus` — PASS; produced a statically linked Linux executable.

The weakest link is the transport-attempt definition: the direct-call RED is green, but automatic redirects remain an unmetered second wire request. The accepted terminal-append-failure, P2/P3 `attempt_timeout`, and S6.3 IP-pinning ceilings were respected and not relitigated.

VERDICT: FAIL
