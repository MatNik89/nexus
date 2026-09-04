# Phase 5 verification round 2 — Kilo (defensive fold review)

Target: `slice/p0-phase5` at HEAD `91ed2c7` (fold of review round 1). Method: code-read each fold point, run the suite in the working tree, and re-run negative probes against a clean `git archive HEAD` export (`/tmp/kilo/r2-export`) — working tree never modified. Suite: `CGO_ENABLED=0 go vet ./...` exit 0; `CGO_ENABLED=0 go test -count=1 ./...` all 32 packages `ok`.

## Round-1 finding status (verified)

| # | Round-1 finding | Status at HEAD |
|---|---|---|
| 1 | HITL not wired end-to-end | **FIXED** — `loop.Suspender` (`loop.go:57,74,296-304`), PEP `DurableApprovals` (`effectpath.go:165-181,375-383`), `daemon.Deps.SuspenderFor` (`daemon.go:52-53`), `RunChannelTurn` wires both (`daemon.go:220-238`), `telegramHandler` approve→`resumeApproved` (`main.go:528-538`). |
| 2 | Bot token leaks into errors | **FIXED** — `sanitize` strips the token from every transport/malformed-body error (`telegram.go:109-114,131,148`); `TestTokenNeverInErrors` covers a dead endpoint. |
| 3 | Terminal lifecycle incomplete / silent drop | **FIXED** — `EvInboundTerminal` + `CompleteInbound` one-batch recipe (`channel.go:479-507`), `MarkInboundTerminal` for empty replies (`:509-517`), non-terminal replay re-runs the handler (`telegram.go:220-232`). |
| 4 | Two divergent `effectHash` | **FIXED** — single exported `effectpath.EffectHash` (`effectpath.go:124-139`); `approval.effectHash` delegates (`approval.go:58`). |
| 5 | Projection doesn't enforce expiry | **PARTIAL** — consume-expiry added (`approval.go:398`), but the approve/deny projection still has no expiry predicate (`approval.go:177-197`); remains a LOW defense-in-depth gap (journal is not channel-reachable). |
| 6 | 64-bit hash-prefix challenge id | **FIXED** — random 12-byte (96-bit) id (`approval.go:266-270`). |
| 7 | Naive `approve `/`deny ` prefix + unbound cross-profile reply | **STILL PRESENT** (LOW/taste) — prefix routing unchanged (`main.go:528-544`); foreign-chat approve is now blocked by `expected_source` (codex #6), which removes the security half. |

The headline round-1 gap (#1) is genuinely closed: the full suspend→approve→resume→consume chain is wired through the production composition root, and `TestChannelAskSuspendsThenApproveResumes` (`main_test.go:448-524`) proves ModeDefault causality (an ASK tool from the channel suspends, a foreign chat is refused, the originating chat resumes exactly once).

## NEW defects introduced by the fold (confirmed)

### 1. [HIGH] Resume re-executes the suspended call with its FROZEN 2-minute deadline — delayed approval can never resume

`resumeApproved` (`cmd/nexus/main.go:219-237`) executes the canonical call returned by `ApprovedCall` (`approval.go:411-430`), which is the JSON stored at `Suspend` time — including the planner's `Deadline: time.Now().Add(2 * time.Minute)` (`planner.go:175`). `EffectPath.RunTool` checks the deadline *before* the durable consume (`effectpath.go:355`), so a call whose 2-minute window has passed is rejected with `CALL_DEADLINE_EXCEEDED` and the approval is **never consumed** — it stays `APPROVED` and is un-resumable (re-`approve` → `APPROVAL_REPLAY`).

The challenge TTL is 15 minutes (`DefaultChallengeTTL`), but the call deadline is 2 minutes, so any approval arriving in the 2–15-minute window — the entire point of *remote, delayed* HITL (C4: "delayed mobile approval must not self-invalidate") — fails.

Probe (`/tmp/kilo/r2-export/cmd/nexus/zz_probe_resume_test.go`), against a clean export:

```
Suspend(call deadline = now+1s) → sleep 1.5s → Approve (within TTL, OK) → resumeApproved
RESUME FAILED: effectpath: tool "memory_remember": CALL_DEADLINE_EXCEEDED
```

Fix: `resumeApproved` (or `ApprovedCall`) must refresh the transport deadline before execution, since the deadline is not part of the C4 exact-intent hash (correctly excluded from `EffectHash`) and carries no security meaning for an already-approved, single-use challenge.

### 2. [MED] Telegram transport classification is too coarse — connection-refused parks the row UNKNOWN (never auto-retried)

`telegram.call` maps **every** `http.Client.Do` error to `ErrAmbiguousSend` (`telegram.go:127-131`, "the wire WAS touched"). But `Do` errors include definite pre-wire failures — `dial tcp … connection refused`, DNS resolution failure — where nothing left the process and a retry is provably safe. Those now park the outbox row `UNKNOWN`, and `Flush` reads only `PENDING` (`channel.go:408-410`), with no automatic reconciliation path in the daemon. A single transient network blip permanently stalls the reply until manual `Reconcile`.

Probe (`/tmp/kilo/r2-export/internal/channel/telegram/zz_probe_classify_test.go`), clean export:

```
EnqueueReply → FlushOutbox against http://127.0.0.1:1 (connection refused)
flush error: … dial tcp 127.0.0.1:1: connect: connection refused
pending=0 unknown=1   // row stuck UNKNOWN, not retry-safe PENDING
```

This regresses the T22/B2 contract "transport ERROR keeps the row PENDING (retry safe)" that `TestDeliveryHonesty` still asserts for the *core* (`channel_test.go:195-203`) — the adapter breaks it in practice. Fix: classify connection/DNS-level `Do` errors (e.g. `*net.OpError`/`*net.DNSError` with nothing sent) as a definite pre-wire failure (plain error), reserving `ErrAmbiguousSend` for post-send failures (read reset/EOF/timeout-after-write) and 5xx/malformed-2xx.

### 3. [MED] Non-terminal replay re-runs a NON-idempotent turn (at-least-once tool effects)

The fix for round-1 #3 re-runs the handler when an admitted message is non-terminal (`telegram.go:220-232`), but the handler (`RunChannelTurn`) mints **fresh** turn/run ids and the planner mints **fresh** `ToolCallID`/idempotency keys each run (`planner.go:165-180`). A crash after the turn's tool effects commit but before `CompleteInbound` therefore re-executes those tools with fresh operation ids — no dedup, because the turn's durable effects are not correlated to the channel admission. This is the honest at-least-once boundary (explicitly labeled in the code) and is bounded for P0 by id-fail-closed tools (`reminder_set`/`task_create` reject duplicate ids; `memory_remember` supersedes), but it is a real at-least-once-effect semantics change with no correlation seam to close it later. Recorded as a ceiling, not a blocker.

## Minor (LOW, non-blocking)

4. [LOW] Duplicate `sysPep.SetDurableApprovals(approvals)` (`main.go:335-336`) — harmless copy-paste, idempotent setter.
5. [LOW] Projection approve/deny still lacks an expiry predicate (`approval.go:177-197`); expiry is enforced only at the API layer (`approval.go:350`, `:398`). Round-1 #5 remains as defense-in-depth.
6. [LOW] `approve `/`deny ` prefix routing still intercepts any owner message beginning with those words (`main.go:528-544`); now security-safe via `expected_source` but still a UX false-positive.

## What holds

Token sanitization is complete (probe output itself shows `[REDACTED-TOKEN]`). `expected_source` binding is enforced at the projection (`approval.go:178,191`), so a foreign chat can no longer approve. `ConsumeApproval` expiry uses the `>=` boundary and looks up by the **full** effect hash (`approval.go:375-406`). Random challenge ids remove the hash-prefix collision surface. Strict bindings parsing (`main.go:480-505`) and the live probe gate (`main.go:510-516`) are wired and covered by `TestTelegramBindingsStrict`/`TestTelegramProbeGate`. `CompleteInbound` is a true one-batch recipe (`AppendBatch`, `channel.go:503`).

## Verdict

All round-1 findings are fixed or acceptably bounded, and the end-to-end HITL wiring is real. But the fold introduced two confirmed, probe-backed defects: the frozen-deadline resume (HIGH — defeats delayed approval, the feature's core use case) and the over-broad ambiguous transport classification (MED — transient network errors permanently stall outbound delivery). Neither is taste; both are concrete broken behavior.

VERDICT: FAIL
