# Adversarial design review round 2 — audit-remediation plan v2
Reviewed revision: `c89897ba10f02963b14e1d7925578f1909104e34`
Verdict: FAIL

The review is bound to the committed tree above. `HEAD` matched `c89897b`, the
requested artifacts and production sources were tracked, and the worktree was clean at
initial binding. Untracked round-2 reviews that appeared later were excluded from
evidence. Slices A-F are still designs; conclusions about them are static owner/flow
analysis, not implementation or runtime proof.

## Round-1 disposition

1. **CLOSED — former finding 1 (Slice B expanded `s7min` into a retry engine).**
   Plan v2 explicitly keeps `s7min` unchanged, chooses one attempt, parks definite
   failure durably as `FAILED`, and leaves full retry/backoff to an unchosen P2 slice
   (`docs/PLAN-AUDIT-FIXES.md:84-114`). This now matches the current owner, which issues
   one grant per operation and collapses retryable failure to terminal
   (`internal/kernel/s7min/s7min.go:3-9,94-105,158-184`), and the binding P0 scope guard
   (`AGENTS.md:55-60`). New lifecycle omissions are reported below; they do not reopen
   the former second-retry-owner design.

2. **CLOSED — former finding 2 (one grant per flush and restart-resettable cap).**
   V2 assigns stable operation identity `delivery:<delivery_id>`, issues one grant per
   row, consumes it at the adapter wire boundary, and makes the post-attempt outbox state
   durable (`docs/PLAN-AUDIT-FIXES.md:93-105`). Its two-row, grant-reuse, no-grant, and
   restart REDs are explicit (`docs/PLAN-AUDIT-FIXES.md:116-124`). Those changes address
   the current multi-row callback seam (`internal/channel/channel.go:554-568`) and the
   current ungranted `client.Do` path (`internal/channel/telegram/telegram.go:193-223,364-401`).

3. **CLOSED — former finding 3 (shared egress receipt and pre-wire ownership).**
   V2 makes `ReceiptSink` mandatory, exports `ErrPreWire`, requires receipt durability
   before dial, wires one journal sink before provider construction, and routes both
   provider and Telegram through the shared owner (`docs/PLAN-AUDIT-FIXES.md:31-57`).
   The new tests cover nil sink, receipt failure with zero dials, the typed pre-wire
   marker, durable denial receipt, and final Telegram outbox classification
   (`docs/PLAN-AUDIT-FIXES.md:66-82`). This closes the current optional-sink path
   (`internal/channel/telegram/dialer.go:74-87`) and private marker/classifier split
   (`internal/channel/telegram/dialer.go:22-27`; `internal/channel/telegram/telegram.go:165-181`).

4. **CLOSED — former finding 4 (`io.LimitReader` accepted a valid prefix of an
   oversized body).** V2 reads `max+1`, rejects length above `max`, strictly decodes the
   bounded bytes, requires EOF, and names the valid-object-plus-trailing-bytes RED
   (`docs/PLAN-AUDIT-FIXES.md:58-64,78-81`). That detector directly attacks the current
   unbounded decoder (`internal/llm/provider/provider.go:201-213`).

5. **CLOSED — former finding 5 (budget did not cover final wire context or fail closed
   on zero).** V2 gives the typed config owner a positive bounded default, rejects zero,
   injects the budget at composition, and measures every final role/content message after
   system/tool/time assembly but before grant issuance or transport
   (`docs/PLAN-AUDIT-FIXES.md:126-163`). This addresses the current disable-on-zero
   behavior (`internal/kernel/budget/budget.go:45-56`) and the current planner additions
   that occur before `Issue` (`internal/llm/planner/planner.go:253-295`).

6. **CLOSED — former finding 6 (incomplete channel-health paths and journal-dependent
   health).** V2 defines one journal-independent AtomicWriter-backed health owner, makes
   `Adapter.Run` return fatal substrate errors to a supervisor, exposes health to doctor,
   and provides separate external-state REDs for command registration, polling, outbox
   transport, and outbox journal failure (`docs/PLAN-AUDIT-FIXES.md:165-195`). This covers
   all three currently discarded calls and the unsupervised goroutine
   (`internal/channel/telegram/telegram.go:428-464`; `cmd/nexus/main.go:289-296`). The new
   retry and classification defects in that design are separate findings below.

7. **CLOSED — former finding 7 (doctor false-positive detector).** V2 routes ordinary
   doctor through the real config resolver, passes typed secret-reference names, and
   includes the missing case “custom configured, custom absent, default populated =>
   OFF” alongside the two positive controls (`docs/PLAN-AUDIT-FIXES.md:197-211`). This
   directly replaces the current hardcoded reads and config-blind entry point
   (`internal/preflight/doctor/doctor.go:109-118,178-187`; `cmd/nexus/main.go:713-740`).

8. **CLOSED — former finding 8 (release tests did not bind failure to signing and
   publication).** V2 runs both gates on the exact built binary before grading/signing,
   fails closed for old/missing/error/finding cases, and specifies whole-script shim REDs
   that prove signing, pointer switch, and installation are not reached, plus a clean
   non-always-fail control (`docs/PLAN-AUDIT-FIXES.md:213-232`). These assertions cover
   the current build-to-sign-to-publish sequence (`scripts/p0-accept.sh:65-93`).

## Numbered new findings

1. **HIGH — Slice B authorizes and consumes attempts but never lands the S7 operation
   outcome or cancels an unused grant.**

   Evidence: v2 specifies `Issue`, adapter-side `Consume`, and outbox outcomes, but never
   calls `Authority.Report` or `Authority.Cancel` and has no detector for authoritative S7
   terminal state (`docs/PLAN-AUDIT-FIXES.md:93-105,116-124`). In the current owner,
   `Issue` leaves the operation `AUTHORIZED`, `Consume` moves it to `RUNNING`, and only
   `Report` can reach `SUCCEEDED`, `FAILED`, or `UNKNOWN`
   (`internal/kernel/s7min/s7min.go:94-155,158-184`). The existing provider path shows the
   required discipline: a pre-consume failure cancels, a consumed failure reports
   terminal, and success is not accepted unless `Report` succeeds
   (`internal/llm/planner/planner.go:168-177,288-305,330-352`). As written, successful and
   failed deliveries can leave S7 permanently `RUNNING`; marshal/request/park failures can
   leave it `AUTHORIZED`. The outbox being terminal does not repair S7's authoritative
   state.

   Concrete fix: specify one ordered lifecycle for every row: issue; if the durable
   UNKNOWN park fails, cancel the unused grant; on any pre-consume local refusal, cancel;
   after consume, report `OutcomeSucceeded`, `OutcomeFailedTerminal`, or `OutcomeUnknown`
   before the corresponding `SENT`, `FAILED`, or retained-`UNKNOWN` result is claimed.
   Propagate landing errors; after a remotely accepted send, any S7/report or sent-mark
   failure must leave the outbox `UNKNOWN`, never claim `SENT`. Add causal REDs asserting
   no delivery operation remains `AUTHORIZED` or `RUNNING` after success, definite
   failure, ambiguity, pre-consume failure, and park failure.

2. **HIGH — `target = channel:<adapter_id>` does not bind a grant to the specific
   delivery, so two same-adapter rows can swap fresh grants without reuse.**

   Evidence: the plan makes the operation ID delivery-specific but the target only
   adapter-specific, then says the adapter merely consumes the passed grant
   (`docs/PLAN-AUDIT-FIXES.md:93-97`). `Authority.Consume` compares the fields in the
   presented grant to the authority's record; it has no `Outbound` row and cannot know
   which delivery is being sent (`internal/kernel/s7min/s7min.go:132-155`). The current
   provider closes the analogous gap by comparing the presented target with the concrete
   transport target before consume (`internal/llm/provider/provider.go:156-172`). V2's
   two-distinct-grants and reused-grant tests (`docs/PLAN-AUDIT-FIXES.md:116-119`) can both
   pass if the unused grant minted for delivery A is first used to send delivery B, because
   both targets equal `channel:telegram`.

   Concrete fix: bind `TargetID` to the immutable delivery resource, for example
   `channel:<adapter_id>:delivery:<delivery_id>`, and make the adapter recompute and compare
   the expected target (and stable operation ID) from the `Outbound` before `Consume`.
   Add a RED that swaps two still-unused grants between two Telegram rows and requires
   zero wire calls; ordinary reuse testing does not detect this first-use substitution.

3. **HIGH — Slice B's stated tradeoff contradicts its own ambiguity rule and would
   permit a 5xx response to be implemented as terminal `FAILED`.**

   Evidence: the state rule correctly says ambiguous wire-touched outcomes remain
   `UNKNOWN` (`docs/PLAN-AUDIT-FIXES.md:98-103`), but the tradeoff then says “one 5xx”
   parks the message terminally (`docs/PLAN-AUDIT-FIXES.md:108-111`). Current Telegram
   correctly treats any HTTP 5xx after POST as ambiguous because the remote may have
   processed it (`internal/channel/telegram/telegram.go:218-223`), and `Core.Flush` keeps
   `ErrAmbiguousSend` in `UNKNOWN` (`internal/channel/channel.go:581-582`). E9 forbids
   converting a possibly committed remote effect into a definite terminal outcome
   (`docs/ARCHITECTURE-ESSENTIALS.md:114-120`). None of Slice B's REDs asserts 5xx behavior.

   Concrete fix: remove 5xx from the definite-failure tradeoff and state explicitly that
   HTTP 5xx, post-write timeout/reset, and accepted-but-malformed response remain
   `UNKNOWN`/reconciliation with no resend. Use a genuinely definite transient example
   such as a DNS/dial refusal for the acknowledged “one blip parks FAILED” tradeoff. Add a
   5xx RED asserting `UNKNOWN`, zero later wire calls, and an S7 `OutcomeUnknown` landing.

4. **HIGH — Slice D deliberately keeps retrying failed polling outside S7 and promises
   recovery from a token change that the running process cannot observe.**

   Evidence: v2 says transport failures are degraded but keep running and a permanent 401
   “keeps polling so a token fix recovers” (`docs/PLAN-AUDIT-FIXES.md:173-180`). The current
   adapter reads the token once at construction (`internal/channel/telegram/telegram.go:75-96`),
   while P0 configuration is sealed until restart (`AGENTS.md:55-57`); changing the owner's
   shell environment cannot update the already-running process. `Run` currently performs
   another `PollOnce` on every tick after any error
   (`internal/channel/telegram/telegram.go:428-442`). Repeating a failed physical poll is a
   retry, and S7 is the only retry/cancel owner (`AGENTS.md:19-22`;
   `docs/ARCHITECTURE-ESSENTIALS.md:68-75`). The health record makes the loop visible but
   does not authorize it.

   Concrete fix: distinguish successful continuous polling iterations from retry after a
   failed physical poll. Under the selected P0 no-retry contract, 401 must record
   `remote_rejected`, return from `Run`, remain OFF/degraded, and require config/token repair
   plus daemon restart; a transient failed poll must likewise stop unless an explicitly
   authorized S7 policy issues another attempt. If automatic poll recovery is required,
   defer it with full S7 rather than placing a retry loop in the adapter. Add a 401 RED that
   advances several ticker intervals and still observes exactly one failed poll.

5. **MEDIUM — Slice D has no typed source-to-health classification contract, so its
   detectors can pass while wrapped substrate failures are treated as transport errors.**

   Evidence: v2's health owner accepts a caller-supplied class and relies on three semantic
   classes (`docs/PLAN-AUDIT-FIXES.md:172-180`), but it does not define which lower owner
   emits the class or prohibit string matching. Today `PollOnce` returns both `a.call`
   transport errors and arbitrary `processUpdate`/journal errors through the same plain
   `error` channel (`internal/channel/telegram/telegram.go:247-263,284-361`). `FlushOutbox`
   similarly forwards `Core.Flush`, which joins send failures but immediately returns
   journal-mark failures as wrapped ordinary errors (`internal/channel/channel.go:554-591`;
   `internal/channel/telegram/telegram.go:364-401`). Narrow scripted 401 and direct
   mark-failure tests can pass even if a differently wrapped journal error is classified
   as recoverable transport, leaving work running on a broken substrate.

   Concrete fix: make the originating owner return a closed typed failure class preserved
   through wrapping (`errors.Is/As`), default unknown classes to fatal, and let health only
   render/persist that type rather than infer from text. Add table REDs for nested/wrapped
   journal failures from both inbound admission and every outbox mark, plus an unknown
   class; all must stop the adapter. Add a transport control that remains non-substrate.

6. **MEDIUM — Slice A's “exact Telegram policy” changes the allowlist unit from the
   provider's host:port to hostname and can reject a currently valid custom provider.**

   Evidence: the current provider admits `u.Host`, which includes an explicit port, against
   `egress_allow` and uses that same value as its S7 target
   (`internal/llm/provider/provider.go:104-116,128-160`). The Telegram client policy that v2
   says will move exactly into the shared owner instead derives `u.Hostname()` and compares
   that hostname to each allowlist entry (`internal/channel/telegram/dialer.go:192-218`;
   `docs/PLAN-AUDIT-FIXES.md:41-45,52-57`). For
   `https://provider.example:8443` with the currently required allow entry
   `provider.example:8443`, the provider precheck passes but the moved Telegram check looks
   for `provider.example` and rejects it. Adding both spellings is an undocumented config
   expansion and creates two competing canonical target forms.

   Concrete fix: define one canonical endpoint unit in the shared owner—normalized
   hostname plus effective port—and use it consistently for config validation, allowlist
   matching, dial address, receipt, and S7 target. Preserve default-port normalization
   without wildcarding. Add REDs for explicit non-default provider ports, equivalent
   default-port forms, case normalization, and a same-host wrong-port denial.

7. **HIGH — The order deploys slices A-E with the known-vulnerable ambient Go toolchain
   before Slice F installs the blocking release gate.**

   Evidence: v2 places F last because the host upgrade is deferred, but also orders every
   slice to merge and then `AUTODEPLOY` its newly built binary
   (`docs/PLAN-AUDIT-FIXES.md:213-237`). The current module has only `go 1.26`
   (`go.mod:3`), the current host reports Go 1.26.4, and the source audit rejected that
   binary after five reachable-symbol advisories with a 1.26.6 fix floor
   (`docs/AUDIT-FULL-codex-2026-09-08.md:49-63`). The current acceptance path has no
   version or vulnerability check before build/sign/publish (`scripts/p0-accept.sh:55-93`).
   Therefore following the written order builds and installs up to five intermediate live
   binaries under the exact toolchain F3 says must be rejected.

   Concrete fix: make Slice F's toolchain/scanner gate and host upgrade the first blocking
   prerequisite for any deployment, or prohibit all deploy/restart/sign/publish actions
   until A-F are complete and F is GREEN. Deployment must also remain an explicit owner
   action, not authority inferred from a plan-review pass. Add an orchestration RED proving
   an old toolchain or scanner finding prevents every slice's deploy command, not only the
   final acceptance signature.

## Notes (not verdict reasons)

- Slice B's core P0 tradeoff is otherwise honest: a definite transient pre-wire failure
  may produce zero remote deliveries and a durable operator-visible `FAILED` row, because
  P0 intentionally has no automatic retry. That is a real availability loss, not
  at-least-once completion. The plan correctly exposes operator resurrection as deferred,
  but its `OWNER DECISION ... pending confirmation` marker
  (`docs/PLAN-AUDIT-FIXES.md:84-86`) must be resolved before implementation.
- Slice A detector 5 crosses the A->B boundary: while A is reviewed alone its correct
  expected state is the existing definite-failure `PENDING`; after B it becomes `FAILED`
  (`docs/PLAN-AUDIT-FIXES.md:75-77,234-238`). State the two phase-specific expectations so
  Slice A can independently reach GREEN.
- A bounded UDS chat reader that keeps the connection open must discard the complete
  oversized frame through its newline before reading another frame, or close the
  connection. Strengthen Slice C's chat RED with an oversized frame containing an aligned
  valid-looking suffix and assert the suffix is never processed as a second frame
  (`docs/PLAN-AUDIT-FIXES.md:147-163`; `internal/app/daemon/daemon.go:201-209`).
- No avoidable new dependency is proposed. The shared egress owner and channel-health
  owner each have multiple real consumers/failure paths; they are not one-implementor
  speculative abstractions. The smallest compliant Slice B remains the no-retry path, not
  full S7.

## Proof ceiling

No production implementation exists for A-F at `c89897b`, so no RED->GREEN or ablation
claim is made for them. The strongest unresolved proof risk is Slice B's split ownership:
the outbox transition and S7 transition must be tested as one causal result without
allowing either owner to claim success while the other remains nonterminal. No live
provider, Telegram, signing, publication, installation, restart, or paid canary was run.

VERDICT: FAIL
