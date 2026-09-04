# Phase 5 verification round 2 — Codex

Reviewed immutable target: `91ed2c701e53fd92642d10e790a845da0a15bacc` on `slice/p0-phase5`.

Result: **FAIL**. Several round-1 fixes are real and causal, but durable HITL is still not a durable turn resume, non-terminal inbound replay can now duplicate an already-executed effect, approval expiry remains racy, and the outbox has a newly confirmed poison-head starvation path.

## Findings

1. **[HIGH] Non-terminal replay can execute a completed effect twice.** Telegram reruns the entire handler whenever the inbox row is not terminal (`internal/channel/telegram/telegram.go:220-245`), while the terminal transition is committed only after the handler returns through `CompleteInbound` (`internal/channel/channel.go:479-506`). No durable handler/tool outcome is associated with the inbound before that point. **Concrete failure:** the first incarnation admits an update, executes its effect, and dies before `CompleteInbound`; redelivery sees `ADMITTED`, reruns the planner with a new tool-call/idempotency identity, and repeats the effect. **Probe:** clean-export `TestR2CrashAfterEffectBeforeTerminalDoesNotRepeatEffect` failed with `effects=2`. The fold fixes crash-before-handler loss, but introduces an unsafe crash-after-effect replay branch and does not satisfy T22's after-effect crash matrix.

2. **[HIGH] Approval receipt is durable, but resume is neither restart-driven nor turn-rehydrating.** `resumeApproved` loads only `call_json` and directly runs that one call on the system effect path (`cmd/nexus/main.go:216-236`). It does not use `SuspendedTurn`, restore context/journal offset, or re-enter the loop; `SuspendedTurn` remains unused production code (`internal/approval/approval.go:432-447`). Startup performs no scan for already-approved challenges. The command handler first appends `ApprovalReceived`, then calls resume (`cmd/nexus/main.go:528-538`). **Concrete failure:** crash after `Approve` commits but before `resumeApproved`; after restart Telegram redelivers the approval command, the second `Approve` returns `APPROVAL_REPLAY`, and the action remains stranded. **Probes:** `TestR2ApprovalSurvivesCrashAfterApprovalReceived` failed with `challenge already APPROVED: APPROVAL_REPLAY`; `TestR2ResumeRehydratesAndContinuesTurn` failed with `provider_calls=1`, proving resume executed the tool but never continued the suspended turn.

3. **[HIGH] A valid 15-minute challenge becomes unusable after the original two-minute tool-call deadline.** Planner-created calls expire after two minutes (`internal/llm/planner/planner.go:169-179`), but approval challenges remain live for fifteen minutes (`internal/approval/approval.go:32-33`, `internal/approval/approval.go:275-281`). Resume reuses the original call unchanged (`internal/approval/approval.go:409-429`), and `RunTool` rejects it before consulting the durable approval when its deadline has elapsed (`internal/kernel/effectpath/effectpath.go:348-383`). **Concrete failure:** the user approves from a phone after two minutes but before challenge expiry; the handler says the challenge was approved, then resume fails `CALL_DEADLINE_EXCEEDED`, and replay cannot retry because approval is already `APPROVED`. **Probe:** clean-export `TestR2DelayedApprovalWithinChallengeTTLStillRuns` failed with that exact error.

4. **[MED] A suspended turn is simultaneously recorded as succeeded.** After the suspender commits `approval.turn_suspended`, the loop appends `turn.succeeded` before returning the challenge (`internal/kernel/loop/loop.go:289-304`). The canonical turn state table maps that event to terminal `SUCCEEDED` and has no resume transition (`internal/kernel/machine/machine.go:153-171`). **Concrete failure:** recovery/state inspection reports a completed turn while its requested effect is still awaiting approval; the later direct tool execution has no turn continuation to attach to. **Probe:** clean-export `TestR2SuspendedTurnIsNotRecordedSucceeded` failed with `suspended turn was falsely recorded succeeded: count=1`.

5. **[MED] Approval expiry is still a check-then-append race.** Both decision and consumption read/check expiry before appending their transition (`internal/approval/approval.go:339-358`, `internal/approval/approval.go:375-406`); the projection CAS checks status and hash but not `expires_unix` against an event-owned decision time (`internal/approval/approval.go:172-212`). The corrected `>=` boundary is real, but expiry can pass after the precheck and before the consume event commits. **Concrete failure:** the clock is live at the precheck and expired when the consumption event is minted; the projection still commits `CONSUMED`. **Probe:** deterministic clean-export `TestR2ExpiryCannotRaceBetweenCheckAndConsumeEvent` failed with `approval expired after precheck but was consumed`.

6. **[MED] One permanent delivery failure starves all later outbox rows.** `Flush` orders all pending rows by creation time and returns immediately after the first definite failure is re-pended (`internal/channel/channel.go:389-404`, `internal/channel/channel.go:438-469`). Every later flush selects the same poison head first. **Concrete failure:** a permanently rejected/blocked Telegram chat remains first in the profile outbox; every later valid delivery stays pending forever. **Probe:** clean-export `TestR2PermanentFailureDoesNotStarveLaterDeliveries` failed after two flushes with `good sends=0`. This is a newly confirmed defect in the reviewed delivery class.

7. **[MED] The live Telegram probe gates adapter startup but is still not part of the sealed T11 snapshot.** Startup calls `telegramProbeGate` and starts the adapter only on success (`cmd/nexus/main.go:163-186`), and the helper evaluates `Adapter.Probe` (`cmd/nexus/main.go:507-515`). However, no production code passes that `closure.ProbeResult` to `closure.Seal`; the repository-wide production call-site sweep contains no closure snapshot construction. **Concrete failure:** the Telegram adapter is locally gated, but the sealed startup capability snapshot cannot attest that the channel probe with the matching config hash passed, contrary to T23. The committed `TestTelegramProbeGate` proves the helper only, not snapshot registration (`cmd/nexus/main_test.go:572-601`).

8. **[LOW] Token sanitization misses request-construction errors.** Transport and response-decode errors are sanitized (`internal/channel/telegram/telegram.go:107-149`), but `http.NewRequestWithContext` errors are returned raw (`internal/channel/telegram/telegram.go:121-125`). **Concrete failure:** an invalid URL escape in configured URL material causes Go's parser error to include the complete `bot<token>` URL. **Probe:** clean-export `TestR2TokenIsSanitizedWhenRequestConstructionFails` failed with a URL containing the probe token. Production hard-codes Telegram's valid API base and real bot tokens do not contain `%`, so this is a configuration/error-path leak rather than a demonstrated remote exploit.

9. **[MED] Two claimed regression detectors remain non-causal, and two fixes have no detector.** `TestConcurrentAdmissionRace` starts goroutines without a barrier before the pre-read (`internal/channel/channel_test.go:410-442`); after deleting the database unique constraint, it stayed green for ten runs. Thus it still does not prove the race backstop. Replacing random challenge IDs with the old 64-bit effect-hash prefix left the entire approval suite green, and restoring REPL provenance for channel blocks left the channel HITL test green. The F2 detector is now causal: changing `RunChannelTurn` to `ModeYolo` made `TestChannelAskSuspendsThenApproveResumes` fail with `channel ASK did not suspend ... "saved it"`. **Concrete failure:** the unique-index, random-ID, and channel-provenance regressions can be reintroduced without their advertised suites turning RED.

## Round-1 finding-by-finding verification

1. **HITL integration — FAIL.** `Loop.Suspender`, `PEP.DurableApprovals`, and `SuspenderFor` are genuinely wired, and the ModeDefault detector is causal. Findings 2-4 prove that the implementation does not survive the approval-to-resume crash window or rehydrate/continue the turn.
2. **Inbox terminal lifecycle — PARTIAL/FAIL.** `EvInboundTerminal` and the terminal-plus-outbox `AppendBatch` are real. Crash-before-handler recovery now works. Finding 1 proves the new rerun rule duplicates effects after the next crash seam.
3. **Ambiguous accepted send — PASS.** UNKNOWN is committed before invoking the wire, `ErrAmbiguousSend` remains UNKNOWN, and the clean-export round-1 ambiguity probe passed. Removing the UNKNOWN-first mark made `TestAmbiguousSendParksUnknownNeverResends` fail with `accepts=2`.
4. **Sent-mark plus UNKNOWN-mark failure — PASS.** UNKNOWN already exists before the callback; closing the journal after simulated acceptance and reopening did not resend. The clean-export round-1 probe passed.
5. **Approval source/group authorization — PASS.** `expected_source` is enforced in the projection CAS, group chats are refused, and the round-1 foreign-source probe passed. Removing either source binding or the private-chat guard made its committed detector fail.
6. **Exact-intent target binding — PARTIAL/NOT CURRENTLY REACHABLE.** Durable and in-memory approvals now share `effectpath.EffectHash`; modified arguments remain causal. The hash still has no first-class target-resource or device/inode input (`internal/kernel/effectpath/effectpath.go:121-139`). No direct destructive filesystem tool is currently registered, so the prior symlink-swap exploit is not reachable in Phase 5; the C4 device/inode requirement remains a mandatory trigger when that tool surface lands.
7. **Expiry — PARTIAL/FAIL.** Exact-boundary and post-approval expiry probes pass, but finding 5 proves the transactional race remains.
8. **Secret-bearing summary — PASS.** `Suspend` asks the journal redactor whether the full payload would be rewritten and refuses before append (`internal/approval/approval.go:275-300`). Removing that refusal made `TestChallengeRefusesSecretExposure` fail.
9. **Bot-token errors — PARTIAL.** Normal transport/probe errors are sanitized and the original round-1 failing-transport probe passes. Finding 8 identifies the remaining raw error branch.
10. **Live probe registration — PARTIAL/FAIL.** Adapter startup is now fail-closed and its gate detector is causal, but finding 7 shows the T11 sealed-snapshot requirement remains unwired.
11. **Ledger RED causality — PARTIAL/FAIL.** F2, source, expiry-boundary, secret, group, token-transport, terminal, and UNKNOWN-first guards have red-capable tests. The after-effect crash matrix still fails and the concurrent-admission detector remains non-causal (findings 1 and 9).
12. **Challenge ID — IMPLEMENTATION PASS, DETECTOR FAIL.** IDs are now 96-bit `crypto/rand` values (`internal/approval/approval.go:265-270`), but restoring the old deterministic prefix left all approval tests green.
13. **Strict bindings parser — PASS.** Whitespace, malformed pairs, invalid IDs, empty profiles, and duplicates are handled explicitly (`cmd/nexus/main.go:476-504`). Disabling duplicate rejection made `TestTelegramBindingsStrict` fail.
14. **Channel provenance — IMPLEMENTATION PASS, DETECTOR FAIL.** Channel blocks now carry `nexus://telegram/<identity>` and producer `telegram` (`internal/app/daemon/daemon.go:240-264`), but restoring REPL provenance left the relevant integration test green.

## Verification evidence

Required repository suite:

```text
CGO_ENABLED=0 go test ./...
exit 0 — all packages passed
```

Original round-1 negative probes rerun in a clean `git archive HEAD` export:

```text
ambiguous remote acceptance remains UNKNOWN across retry: PASS
journal loss after remote acceptance remains UNKNOWN across restart: PASS
admission-before-handler crash reruns handler: PASS
approved challenge at/after expiry is rejected: PASS
foreign decision source is rejected: PASS
known-secret challenge is refused before append: PASS (committed detector)
normal Telegram transport/probe errors redact the token: PASS
```

Causal ablations in separate clean exports:

```text
ModeDefault -> ModeYolo: TestChannelAskSuspendsThenApproveResumes RED
remove UNKNOWN-first mark: TestAmbiguousSendParksUnknownNeverResends RED
remove expected_source CAS: TestForeignSourceCannotDecide RED
change expiry >= to >: TestApprovalExpiresAtConsume RED
remove secret refusal: TestChallengeRefusesSecretExposure RED
remove private-chat guard: TestGroupChatRefused RED
remove transport sanitizer: TestTokenNeverInErrors RED
disable duplicate binding rejection: TestTelegramBindingsStrict RED
disable probe gate: TestTelegramProbeGate RED
remove inbox UNIQUE constraint: TestConcurrentAdmissionRace GREEN for count=10 (non-causal)
restore hash-prefix challenge ID: approval suite GREEN (no detector)
restore REPL channel provenance: channel HITL test GREEN (no detector)
```

Proof ceiling: no live Telegram bot or external canary was used. The real Bot API's availability and deployed closure snapshot were not tested. All adversarial probes and mutations ran only in temporary clean exports; the repository source was not modified.

VERDICT: FAIL
