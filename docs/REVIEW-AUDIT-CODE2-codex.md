# Adversarial code review round 2 — complete audit-remediation stack

Reviewed revision: `f33bc9a03cf8280f4e1216063632b4129aed3a34` (`slice/audit-b`), diff `fdb39dc..f33bc9a`

Verdict: **FAIL**

The F/A/C/E round-1 defects are closed, but the combined B/D implementation is not shippable at the reviewed revision. The target-revision internal suite fails because ordinary typed Telegram failures are promoted to fatal substrate failures. Independent owner-boundary review also found missing S7 execution-context propagation, a durable registration crash split-brain, insufficient method/kind binding, and detector omissions that leave several plan guarantees unproved.

## Round-1 disposition

1. **CLOSED — immutable release floor.** `scripts/lib/release-gate.sh:11-13` now owns an unconditional readonly `RELEASE_GO_FLOOR=1.26.6`. The hostile-environment cases and static binding are present at `cmd/nexus/release_gate_test.go:116-127,176-184,214-237`.

2. **CLOSED — canonical egress receipt identity.** Both refusal and permission receipts use `d.endpoint.Host` and `d.endpoint.Port`, and resolution also uses the canonical host (`internal/foundation/egress/egress.go:236-265`). The uppercase-host receipt detector is at `internal/foundation/egress/egress_test.go:317-349`.

3. **CLOSED — ContextBudget detector can observe a pre-refusal operation.** Production enforces the complete wire representation before minting the operation (`internal/llm/planner/planner.go:344-362`); the detector injects a known ID and proves that S7 has no such operation after refusal (`internal/llm/planner/budget_test.go:17-50`). Moving operation creation ahead of the budget check makes that assertion RED.

## Per-slice disposition

- **Slice F — PASS.** The mandatory floor is immutable, both release paths share the helper, and the round-1 bypass detector is now causal.
- **Slice A — PASS.** Provider and Telegram use the shared egress owner, canonical receipts are durable, and the response/stream caps remain at the transport boundary.
- **Slice C — PASS.** The final wire representation is refused before S7 state creation; UDS reads and streaming accumulation remain bounded.
- **Slice E — PASS.** Doctor still consumes resolved configured secret names and has no default-name fallback.
- **Slice B1 — FAIL.** The engine owns grants and retry scheduling, but its attempt deadline/cancel context is not used by provider or Telegram; policy/event validation is also not fail-closed over the full typed contract. See findings 2 and 6.
- **Slice B2 — FAIL.** Delivery retry state is generally paired correctly, but control-effect crash recovery can split S7 from the owner projection, and the transport does not bind the Bot API method/effect kind to the grant. See findings 3 and 4.
- **Slice B3 — FAIL.** Provider retry is driven by `Execute`, but physical requests escape S7's execution context and final-attempt structured transport failure skips the required salvage decision. See findings 2 and 5.
- **Slice D — FAIL.** Health classification breaks ordinary Telegram operation and health persistence errors are discarded; planned D detectors are incomplete. See findings 1, 7, and 8.

## Findings

1. **CRITICAL — Bare typed Telegram failures are classified as fatal substrate failures, stopping the adapter and breaking the acceptance stack.**

   Evidence: every Bot API failure produced by `call` is a bare `*channel.Failure` (`internal/channel/telegram/telegram.go:243-313`), including the error returned by `setMyCommands` (`:809-824`). `channel.ClassOf` recognizes only `*ClassifiedError`; anything else becomes `substrate/unclassified` (`internal/channel/channel.go:87-107`). `record` therefore reports the failure as fatal and returns it (`internal/channel/telegram/telegram.go:675-695`), and `Run` stops before polling or flushing (`:645-665`). This contradicts the plan's required setMyCommands-5xx result—health `transport`, S7 `UNKNOWN`, adapter continuing with no blind re-send (`docs/PLAN-AUDIT-FIXES.md:560-568`). The D5 table omits bare `*channel.Failure` entirely (`internal/channel/telegram/health_test.go:133-153`), while the registration test calls `registerCommands` directly rather than exercising `Run` and health (`internal/channel/telegram/s7_test.go:248-359`). At the target revision, `CGO_ENABLED=0 /home/matej/.local/go/bin/go test -count=1 ./internal/...` failed six Telegram acceptance/chaos tests because no Telegram admission or delivery occurred; all other internal packages passed.

   Concrete fix: classify the existing typed `*channel.Failure` at the health boundary: receipt failure is substrate, 401/403 remote-rejected, and other transport/API failures transport. Make missing/invalid governed `getMe` identity a typed transport failure as well. Add a `Run`-level D2 detector: setMyCommands 5xx must persist `telegram.register` as transport/UNKNOWN while later poll/outbox work continues. Keep the acceptance fake valid and assert the actual startup call sequence.

2. **HIGH — S7 owns an attempt context in name only; provider and Telegram execute physical calls under the caller context.**

   Evidence: SPEC P0.2 requires deadline and cancel propagation into every child task (`docs/HARNESS-SPEC.md:1389-1392`). `AttemptContext` correctly computes the earliest grant/policy/operation/call deadline and registers the live cancel function (`internal/kernel/s7/s7.go:608-636`), but production uses it only for the tool effect path. `Execute` hands the callback its original context (`internal/kernel/s7/execute.go:25-64`). Provider consumes the grant and builds/runs the HTTP request with that original context (`internal/llm/provider/provider.go:237-254`); Telegram does the same (`internal/channel/telegram/telegram.go:243-264`). Consequently `AttemptTimeout`, operation deadline expiry during a call, and `Authority.Cancel(op)` do not stop either physical request. The only cancellation detector manually calls `AttemptContext` (`internal/kernel/s7/s7min_legacy_test.go:202-230`), so it remains GREEN while both real transports bypass the owner.

   Concrete fix: immediately after successful `Consume` and before `client.Do`, derive `auth.AttemptContext` for the operation, rebind the request with that context, and defer its cancel function. Alternatively make the Execute/transport handshake return the S7-owned context centrally, but retain one owner. Add blocking provider and Telegram servers proving an attempt timeout, operation deadline, and concurrent `Cancel` abort the in-flight request and no child continues.

3. **HIGH — A crash after consuming setMyCommands can permanently split S7 UNKNOWN from channel RUNNING, making reconciliation unreachable.**

   Evidence: the pre-wire companion records the durable control effect as `RUNNING` (`internal/channel/telegram/telegram.go:809-813`), and the channel projection stores that state verbatim (`internal/channel/channel.go:577-586`). On restart, S7 converts a durable RUNNING operation to UNKNOWN by appending only S7 events (`internal/kernel/s7/events.go:387-394`); there is no channel companion in that recovery batch. Registration reconciles only when the channel row says `UNKNOWN` (`internal/channel/telegram/telegram.go:765-796`). A surviving `RUNNING` row falls through to `Begin`/`Next`; S7 is already UNKNOWN, so it refuses a new grant and the operation remains stuck forever. The existing restart detector covers returned 5xx and Report, not death after Consume and before Report (`internal/channel/telegram/s7_test.go:248-359`).

   Concrete fix: make the Consume companion for this irreversible control effect park the owner row as `UNKNOWN`, which is honest once the grant is durably started, or define an owner recovery recipe that lands both S7 and channel UNKNOWN in one batch. Add a process/fault detector exactly after Consume and before Report; after restart assert both projections are UNKNOWN, zero blind setMyCommands calls, and getMyCommands reconciliation is reachable.

4. **HIGH — Bot API method/effect kind is caller supplied and is not bound to the grant, enabling a cross-kind grant substitution.**

   Evidence: `call` accepts `method`, `kind`, `op`, and `target` independently, and validates only that the grant equals the supplied op/target (`internal/channel/telegram/telegram.go:243-264`). For example, `uiCall` creates an ephemeral UI operation/target (`:370-376`); a same-package path can present that valid UI grant to `call` with `method=sendMessage` and `kind=delivery`, preserving the UI op/target. It will consume the grant and reach `client.Do` without the durable delivery companion/policy. This directly contradicts the plan statement that `call` recomputes kind identity and refuses cross-kind swaps (`docs/PLAN-AUDIT-FIXES.md:354-359`). The claimed 9d detector changes only delivery operation/target identity (`internal/channel/telegram/s7_test.go:171-203`); it does not hold the grant identity constant while substituting method/kind.

   Concrete fix: bind method and effect kind into a typed operation capability that the transport validates, or split the transport into typed methods whose policy, operation grammar, target grammar, and companion requirement cannot be supplied independently. Add the complete call-site table required by detector 9d, including UI-for-control and delivery-for-poll substitutions with the same otherwise-valid grant; every row must observe zero wire calls.

5. **HIGH — Structured extraction skips the specified salvage ladder when the final generation attempt returns a transport error.**

   Evidence: the plan explicitly requires final-attempt strict-invalid **or transport failure** to salvage re-ask bytes and then original bytes before choosing the S7 outcome (`docs/PLAN-AUDIT-FIXES.md:383-401`). `ExtractVia` instead returns terminal immediately on any `generate` error (`internal/llm/provider/structured.go:133-140`), before the salvage ladder at `:159-170`. Thus a salvageable prose-wrapped original followed by a second-attempt transport error incorrectly returns no value and lands FAILED. Existing 9f tests provide bytes without an error for both attempts (`internal/llm/provider/provider_test.go:237-360`), so the divergence is undetected. Also, no non-test production caller uses `ExtractVia`; the planner paths invoke only plain `chatVia`/`streamVia` (`internal/llm/planner/planner.go:363-405`), despite the plan saying the planner's structured path submits this operation.

   Concrete fix: on the final generation error, preserve the causal error but run salvage over any returned re-ask bytes and `firstRaw` before terminalizing; accepted, validated salvage must return `OutcomeSucceeded`. Add a test with a salvageable original plus attempt-2 transport error that asserts the value and S7 SUCCEEDED together. Wire the intended production structured path to `ExtractVia`, or narrow the plan and finding closure claim if no such path exists.

6. **HIGH — Durable S7 policy and journal inputs are not fully validated or identity-bound, allowing typed contract drift to be silently accepted.**

   Evidence: reopening an operation compares only target, durability, and maximum attempts (`internal/kernel/s7/s7.go:291-298`), ignoring effect class, attempt timeout, deadline, backoff, retryable codes, fallback targets, and idempotency key. `unmarshalPolicy` rejects unknown JSON fields and MaxAttempts below one but does not validate the remaining typed values, negative durations/backoff, unknown retry codes, or trailing JSON (`:692-703`). Event validators use permissive `json.Unmarshal`; reported outcomes/landings are accepted merely when positive, and terminal/reconcile states merely when nonempty (`internal/kernel/s7/events.go:73-147`). Projection then writes the supplied terminal state directly (`:297-311`). This violates E11's unknown-typed-input rejection (`docs/ARCHITECTURE-ESSENTIALS.md:146-154`) and can rehydrate an operation under a materially different or unknown policy/state.

   Concrete fix: validate and compare the complete canonical immutable Policy; reject unknown failure codes/effects/targets, invalid or negative timing/backoff, trailing JSON, and duplicate/extra event data. Strict-decode each S7 event and validate closed outcome/landing/state combinations before projection. Add a table mutating every currently omitted policy field and raw journal events with unknown enum/state values; all must leave journal and projection unchanged.

7. **HIGH — Health persistence is fail-open, and no-op S7 ticks falsely clear degradation.**

   Evidence: health `Report` and `Healthy` surface atomic projection write errors (`internal/channel/health/health.go:86-115,129-141`), but the adapter discards both (`internal/channel/telegram/telegram.go:677-688`), and the daemon supervisor also discards its health writes (`cmd/nexus/main.go:308-317`). Separately, `PollOnce` maps `ErrNotDue` to nil (`internal/channel/telegram/telegram.go:420-423`), registration does the same (`:802-805`), and `Core.Flush` skips not-due rows then returns nil (`internal/channel/channel.go:867-870,938`). `record(nil)` calls `Healthy`, so a degraded component can be marked recovered merely because no physical attempt was due. The D3 test advances ticks during backoff but does not re-read health until after a later successful attempt (`internal/channel/telegram/health_test.go:70-100`), and there is no health-writer failure injection.

   Concrete fix: distinguish a successful physical cycle from a not-due/no-op cycle and leave prior health unchanged for the latter. Treat failure to persist health as substrate-fatal: return/stop from the adapter and make the supervisor preserve/report the failure through its final fallback rather than ignoring it. Add tests that inspect health throughout backoff/UNKNOWN and inject health projection write failure.

8. **HIGH — Multiple mandatory B/D detectors are absent or cannot go RED under their named ablations.**

   Evidence: the plan requires causal fault and ablation coverage at `docs/PLAN-AUDIT-FIXES.md:407-467,560-605`. The exhaustion test checks adjacent event order but injects no former-seam failure and performs no split-append ablation (`internal/kernel/s7/engine_test.go:393-418`), so it does not prove detector 5/9c atomicity. Telegram's 9d test covers only a foreign delivery ID (`internal/channel/telegram/s7_test.go:171-203`), not every Bot API method or cross-kind swap. D6 covers only getUpdates 5xx and 401 (`:205-246`), not the required pre-wire/429/5xx/post-write table or getMe D7. D2b/c/d lacks torn reconcile-batch injection and the required bot-A-UNKNOWN to bot-B provenance scenario (`:248-359`). D5 omits typed `Failure`, and D4 closes the journal globally rather than faulting every outbox mark (`internal/channel/telegram/health_test.go:103-153`). No owner-integration detector covers provider 429/backoff-success, provider 400 terminal, stream mid-way terminal, or authorization/start+park append failures. These omissions would remain GREEN under several exact plan ablations and already allowed finding 1 through.

   Concrete fix: implement the detector matrix literally, retaining current tests: fault the old inter-append seams; cover every Bot API method and cross-kind substitution; table all D6/D7 classifications; inject reconciliation and each outbox-mark batch failure; exercise bot provenance with an old UNKNOWN operation; and test provider retry/terminal/partial-stream plus authorization/started-batch failure at the actual owner boundary. Run each named ablation and preserve the observed RED evidence.

## Verification performed

- Immutable reviewed identity: `f33bc9a03cf8280f4e1216063632b4129aed3a34`; source evidence after the external worktree movement was read with `git show f33bc9a:<path>`.
- Initial target worktree identity was clean and exactly `f33bc9a`. During the review another process advanced `/home/matej/HARNESS/nexus-b` to `63a02bc0e0a150983f28f816b816fa02fcf28117`; no later-revision result is treated as evidence for this verdict.
- `git diff --check fdb39dc..f33bc9a`: PASS.
- `CGO_ENABLED=0 /home/matej/.local/go/bin/go test -count=1 ./internal/...` at target revision: **FAIL** in six Telegram acceptance/chaos cases; all other internal packages passed. The failure is explained by finding 1.
- Full `go test ./...` was not run because cmd release tests execute shell scripts containing recursive cleanup, prohibited by the read-only audit protocol without destructive-command shims.
- No production file in either worktree was modified by this review. No deleted-test weakening was found, but consolidating multiple required detector rows into broad tests created the false-green gaps documented above.

Weakest link: shell release behavior was revalidated statically rather than executed in this round; this does not affect the FAIL verdict, which is independently established by target-revision test failure and reachable B/D owner-boundary defects.

VERDICT: FAIL
