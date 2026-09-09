# Review of audit-hardening slice — CODE4 (`slice/audit-b`)

- **Reviewed revision**: `18392b2` on `slice/audit-b` (worktree `/home/matej/HARNESS/nexus-b`)
- **Delta**: `9ce4dd0..HEAD` (the CODE3 folds)
- **Verdict**: PASS

Verified `CGO_ENABLED=0 go build ./...`, `go vet ./...`, and
`go test -count=1 ./internal/...` all PASS locally.

## CODE3 fold verification (all correct)

1. **AttemptContext** (`s7.go:674-715`) — authority-clock bounds (grant expiry,
   operation deadline, attempt timeout) are projected onto the wall clock as
   `wallBound = time.Now().Add(remaining)` with `remaining = authBound.Sub(a.now())`;
   the caller's `callDeadline` is compared directly, so an executor sees exactly the
   wall deadline it was handed. `remaining <= 0` refuses (fail-closed), the
   cancel func is registered under the same lock as the RUNNING check (no
   Cancel/AttemptContext race). Edge cases: negative remaining → refuse; both bounds
   set → earliest of callDeadline vs wallBound; the double-`time.Now()` skew is now
   confined to the authority-bound projection, not the call deadline.
2. **telegram.record** (`telegram.go:726-749`) — `fatal := cls.Fatal() || errors.Is(err,
   ErrPollTerminal)`: an exhausted poll budget (transport-class health) still ends
   polling. `ErrNothingDue` returns before any health transition, so a no-op tick
   never "recovers" degradation; a health write failure is fatal (substrate).
3. **channel.Flush** (`channel.go:884-978`) — `attempted` is incremented only after a
   grant issues and before `send`; `attempted == 0` returns `ErrNothingDue`. A local
   refusal (grant issued, never consumed) counts as attempted and is surfaced as a
   classified transport failure, not a no-op. `registerCommands` SUCCEEDED now returns
   `ErrNothingDue` (no attempt). The `ErrNotDurable` sentinel replaces the
   `strings.Contains("not durable")` substring match and is also checked on the
   started-park path (`channel.go:918-923`), so a failed STARTED+park is substrate.
4. **s7 events validators** (`events.go:133-208`) — `validReport` is the closed
   (outcome, landing, code, next_at) table; the reconcile validator admits only
   SUCCEEDED / FAILED (no due) / FAILED_RETRYABLE (with due). `appendLocked` wraps
   every failed append in `ErrNotDurable` and honours the `appendFault` seam.
5. **Detectors** — `events_test.go` (10 incompatible + 5 compatible narratives, 4
   non-exit reconcile states, unknown-field/trailing-data refusal), `matrix_test.go`
   `TestGrantKindMethodSubstitutionTable` (cross-kind and wrong-method grants refused
   before Consume), and the planner `retry_test.go` are non-vacuous and anchored to
   the named ablations.
6. **fakeBot counter** — `chatActionCalls` added; see Note 1 for the remaining
   uncounted chrome handlers.
7. **ErrNothingDue call sites** — each is a genuine no-op (not-due backoff, idle
   flush, already-registered menu, registered-and-still-fresh), asserted with
   `errors.Is` plus a zero-counter, not a blanket suppression.

## Notes (not FAIL reasons)

1. **Two fakeBot wire handlers are still uncounted.** `sendRichMessage` and
   `editMessageText`/`editMessageReplyMarkup` set state (`lastMethod`/`lastRich`,
   or nothing) but have no call counter, unlike the counted `sendMessage`/`getUpdates`/
   `setMyCommands`/`getMyCommands`/`getMe`/`sendChatAction`/`answerCallbackQuery`.
   No current test asserts a total wire-call count that would miss them, but a future
   "exactly N wire calls" detector would be false-green against these two chrome paths.
   A `richCalls`/`editCalls` counter would close it.
2. `MarkParamsForTest` (channel) and `SetAppendFault` (s7) are inert-in-production
   test seams; both are minimal and acceptable, worth a comment that they must never
   be reached by production callers.

## Fresh adversarial pass (no new defect)

The S7 `Authority` is mutex-guarded end-to-end; `appendLocked` runs under the
authority lock and only ever calls the journal's independent serialized actor (no
projection re-enters S7, so no lock-cycle). Every durable-append failure path is
`ErrNotDurable`-typed and handled as substrate at the channel boundary; UNKNOWN is
never re-granted (only `Reconcile` exits it); the planner/loop hold no retry loop;
the only two `client.Do` sites are the egress-governed provider and telegram clients;
receipt and health payloads carry no secret (health detail is `sanitize`+`clip`ed).

VERDICT: PASS
