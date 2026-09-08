# Adversarial design review — audit-remediation plan
Reviewed revision: `59c0ac58331ed6639c59d29f7528abf4fc9cf373`
Verdict: FAIL

The review is bound to the committed tree at the revision above. `HEAD` matched that
revision and the scoped files were tracked. The worktree was clean at initial binding;
untracked reviews that appeared later were excluded from evidence. This is a static
design review plus focused verification of the already-landed F4/F9 fixes, not runtime
proof of the unimplemented slices.

## Numbered findings

1. **HIGH — Slice B violates the binding P0 `s7-min` contract by putting retries,
   attempt caps, and backoff into `s7min`.**

   Evidence: the plan explicitly asks `s7min` for "deadline + attempt cap + backoff"
   and a later grant after failure (`docs/PLAN-AUDIT-FIXES.md:40-46`). The constitution
   instead fixes P0 `s7-min` as AttemptGrant + cancel + **no retry**, maps retryable
   outcomes to `FAILED_TERMINAL`, and defers the full S7 engine to P2
   (`AGENTS.md:55-60`; `docs/ARCHITECTURE-ESSENTIALS.md:77-90`). The current owner
   implements exactly that contract: only one grant may exist for an operation and a
   second issue is refused (`internal/kernel/s7min/s7min.go:3-9,94-105`), while both
   retryable and terminal failures take the same terminal transition
   (`internal/kernel/s7min/s7min.go:158-184`).

   Concrete fix: choose one constitutionally valid design before implementation. For
   P0, keep `s7min` unchanged, authorize one delivery attempt, and add a durable
   `FAILED_TERMINAL`/`PARKED` outbox state for a definite failed attempt; do not misuse
   `UNKNOWN`, which is reserved for a possibly committed remote effect. If automatic
   retries are a required later-phase outcome, make Slice B an explicitly authorized
   full-S7/P2 slice with S7-owned policy, persistence, scheduling, and backoff rather
   than expanding `s7min` implicitly.

2. **HIGH — Slice B's “grant before each flush” is not grant-per-physical-attempt and
   its proposed cap is resettable across daemon restart.**

   Evidence: `Core.Flush` reads all pending rows and invokes the send callback once per
   row (`internal/channel/channel.go:554-568`), so one outer grant before `FlushOutbox`
   can cover multiple HTTP sends. Neither `Flush`'s callback nor Telegram's
   `FlushOutbox` carries a grant (`internal/channel/channel.go:554-568`;
   `internal/channel/telegram/telegram.go:364-401`), and `Adapter.call` reaches
   `client.Do` without consuming one (`internal/channel/telegram/telegram.go:193-223`).
   By contrast, the provider consumes its grant immediately at the transport boundary
   (`internal/llm/provider/provider.go:163-180`). The current S7 operation table is
   in-memory (`internal/kernel/s7min/s7min.go:73-91`); only the outbox lease count is
   durable (`internal/channel/channel.go:77-86,244-255,323-329`). The plan defines no
   stable delivery operation identity, durable S7 retry policy, `next_attempt_at`, or
   exhausted transition, so a restart can reset an in-memory cap and resume the
   unbounded loop that F2 requires closing. The proposed tests only count attempts in
   one run (`docs/PLAN-AUDIT-FIXES.md:44-46`).

   Concrete fix: consume one unique grant inside the physical Telegram delivery send,
   bind the operation to the stable `delivery_id`, and ensure one grant cannot cover a
   second row or HTTP call. Under the P0 no-retry choice, one failed attempt transitions
   durably to terminal park. Under a separately authorized full-S7 choice, persist the
   attempt count, deadline, backoff wake time, and exhausted state under S7 ownership.
   Add REDs for two pending rows under one grant, reuse of one grant, backoff-before-due,
   and `N-1 failures -> daemon restart -> final failure -> no further wire calls`.

3. **HIGH — Slice A does not close E11 receipt ownership and can regress Telegram's
   definite-pre-wire classification.**

   Evidence: E11 requires a real egress receipt, not only address filtering and pinning
   (`docs/ARCHITECTURE-ESSENTIALS.md:146-158`). The proposed shared API accepts a
   `receiptSink` but neither requires it nor identifies its durable owner/wiring
   (`docs/PLAN-AUDIT-FIXES.md:21-33`). The code being extracted currently treats a nil
   receipt as success (`internal/channel/telegram/dialer.go:74-87`), while its only
   durable implementation is channel-specific `Core.RecordEgress`
   (`internal/channel/channel.go:196-220`). At the composition root the provider is
   constructed before `channel.Core` exists (`cmd/nexus/main.go:516-542`), so “provider
   builds its client from the same owner” does not itself supply a provider receipt.
   In addition, the shared dialer's refusal/receipt-failure error must preserve the
   definite-pre-wire marker currently owned privately by Telegram
   (`internal/channel/telegram/dialer.go:22-27,146-189`), because Telegram maps only that
   marker (or dial/DNS errors) to safe re-pending
   (`internal/channel/telegram/telegram.go:165-181,206-215`). The planned deny-floor
   tests do not exercise either durable receipt wiring or final outbox state.

   Concrete fix: define a transport-neutral typed `egress.Decision`, a mandatory
   production `ReceiptSink`, and an exported/typed pre-wire refusal error in the shared
   owner. Register and wire the generic receipt through the journal before constructing
   either client; test that provider and Telegram permitted dials create durable
   receipts, receipt failure produces zero socket dials, and a denied Telegram delivery
   remains a definite pre-wire failure rather than becoming `UNKNOWN`.

4. **MEDIUM — Slice A's `io.LimitReader` design cannot guarantee that an oversized
   buffered provider body is rejected.**

   Evidence: the current provider decodes directly from the unbounded body
   (`internal/llm/provider/provider.go:201-213`). The plan proposes decoding through
   `io.LimitReader` and claims an over-limit body will be rejected
   (`docs/PLAN-AUDIT-FIXES.md:29-33`). A JSON decoder may successfully decode one valid
   object before reaching the limit; a response containing a complete valid object
   followed by enough whitespace or trailing bytes to exceed the cap can therefore pass.

   Concrete fix: read at most `max+1`, reject when more than `max` bytes are observed,
   then strictly decode the bounded bytes and require EOF after the single JSON value.
   The RED must use a valid response object completed below the cap plus trailing data
   that pushes the total body above it; a merely truncated JSON object is not a
   red-capable detector for this bypass.

5. **MEDIUM — Slice C does not place ContextBudget at the actual provider-wire context
   or make an unlimited configuration fail closed.**

   Evidence: the existing budget measures only `[]ContextBlock`, and `HardLimit <= 0`
   disables enforcement (`internal/kernel/budget/budget.go:22-34,45-56`). The planner
   transforms history and then adds the system prompt, tool protocol/descriptions, and
   current-time text before creating the provider messages
   (`internal/llm/planner/planner.go:253-287`). Therefore enforcement only in the loop
   over input blocks can pass while the actual request exceeds the limit. No current
   configuration field supplies a non-zero context or provider-body ceiling
   (`internal/foundation/config/config.go:29-52`), and the plan does not name the config
   owner/default/validation despite promising one configured hard budget
   (`docs/PLAN-AUDIT-FIXES.md:48-58`).

   Concrete fix: add validated, non-zero bounded configuration owned by the typed config
   resolver, inject it at composition, and enforce against the final role/content
   messages immediately before every `Issue`/`Chat`/`Stream` attempt. Either extend the
   budget measurement contract to that wire representation or account explicitly for
   every planner-added byte/token. Add a provider spy RED in which input blocks fit but
   system/tool material pushes the final request over budget; it must observe zero grant
   consumption and zero provider calls. Keep independent REDs for both hello and chat UDS
   frames and for total streamed bytes before builder growth/delivery beyond the ceiling.

6. **MEDIUM — Slice D can leave two of the three audited silent-death paths unfixed and
   cannot durably report the journal-failure path as currently specified.**

   Evidence: `Adapter.Run` returns no error, discards `registerCommands`, `PollOnce`, and
   `FlushOutbox` results (`internal/channel/telegram/telegram.go:428-464`), and the
   composition root launches it in an unsupervised goroutine
   (`cmd/nexus/main.go:289-296`). The plan names register, poll, transport, and journal
   failures but provides only one detector for a permanent `getUpdates` rejection
   (`docs/PLAN-AUDIT-FIXES.md:60-69`). A health record stored only in the journal cannot
   report the very case where the journal substrate is failing.

   Concrete fix: specify one health interface and its failure-independent durable
   projection (for example, an AtomicWriter-owned health mirror plus redacted log), make
   fatal adapter termination observable to a supervisor, and preserve the sealed
   capability rule by reporting runtime health rather than silently activating a
   fallback. Add separate REDs for `setMyCommands`, `PollOnce`, `FlushOutbox` transport,
   and `FlushOutbox` journal-mark failure; each must assert the externally visible health
   state/log and, for fatal substrate failure, that further channel work stops.

7. **MEDIUM — Slice E's stated “false-positive direction” detector does not test the
   false-positive direction.**

   Evidence: production resolves secret reference names from typed configuration
   (`internal/foundation/config/config.go:29-39,120-127`; `cmd/nexus/main.go:84-95`), but
   ordinary doctor checks fixed default names (`internal/preflight/doctor/doctor.go:109-118,178-187`)
   and `runDoctor` never resolves configuration (`cmd/nexus/main.go:713-740`). The plan's
   custom-names-present/defaults-absent case detects the known false negative. Its second
   case, “a default-name mapping still reports correctly,” is only a default-regression
   check; it does not prove that a populated default variable is ignored when config
   names a different, absent variable (`docs/PLAN-AUDIT-FIXES.md:71-78`).

   Concrete fix: require all three table cases through the real `runDoctor` configuration
   path: custom configured + custom populated + default absent => ON; custom configured +
   custom absent + default populated => OFF; default configured + default populated => ON.
   The middle case is the causal false-positive RED.

8. **MEDIUM — Slice F's detectors can pass at a helper seam without proving that a
   vulnerable binary cannot be signed or published.**

   Evidence: the current acceptance script builds the exact binary at
   `scripts/p0-accept.sh:65-67`, grades it at `:69-75`, signs at `:81-87`, publishes the
   trust pointer at `:23-46,87`, and may install at `:92-93`; `go.mod` states only
   `go 1.26` (`go.mod:3`). The plan says tests will make the version/vulnerability
   “gate” fail, but does not require the delivered script entry point, exact built binary,
   or sign/publication sinks to be observed (`docs/PLAN-AUDIT-FIXES.md:80-91`). A tested
   version-comparison helper or isolated `govulncheck` invocation can remain unwired or
   run against the wrong binary while the acceptance path still signs.

   Concrete fix: put both checks after building `$BIN` and before grading/signing, invoke
   `govulncheck -mode=binary` on that exact `$BIN`, and make missing checker, too-old Go,
   scanner error, and any finding fail closed. Add whole-script shim tests that keep the
   release key and publication target isolated and assert that `ssh-keygen -Y sign`, the
   trust-pointer switch, and installation are never reached for (a) an old toolchain and
   (b) a scanner finding with an otherwise acceptable toolchain. Also test a clean scan
   reaches the next gate, so a detector that merely makes the script always fail is not
   accepted.

## Already-landed F4 and F9 verification

- **F4 — VERIFIED CLOSED at the reported owner boundary.** `world.daemon` now returns
  only the stop function; its `strings.Builder` is never read while the subprocess can
  write, and shutdown waits for `cmd.Wait` (`internal/acceptance/acceptance_linux_test.go:144-182`).
  All acceptance/chaos/soak callers were updated in commit `96b0c49`. At the reviewed
  revision, focused `TestCriterion4SensitivityUnboundChat` passed. In a clean
  `59c0ac5` export, restoring only the old two-result signature and `out.String()` return
  while retaining the landed callers made the acceptance package fail to compile on the
  one-result assignments. This is causal RED for removal of the reported read-during-run
  path. Proof ceiling: the host race detector remains unavailable; this proves deletion
  of the reported path, not absence of unrelated subprocess races.

- **F9 — VERIFIED CLOSED at the config parser owner.** `parseFileLayer` calls
  `contracts.HasDuplicateJSONKeys` before decoding into the last-value-wins map
  (`internal/foundation/config/config.go:161-180`), and the detector walks object keys at
  any depth and treats malformed input as rejected (`internal/kernel/contracts/contracts.go:575-622`).
  `TestDuplicateJSONKeysRejected` exercises the real `Resolve` path
  (`internal/foundation/config/config_test.go:55-63`). The current focused config and
  contracts suites passed. In a clean `59c0ac5` export, deleting only the six-line config
  guard while retaining the test produced RED: `duplicate JSON keys accepted (ambiguous
  config)`. F9 is revert-proof for the reported config-file class.

## Slice disposition and weakest link

- Slice A: FAIL (receipt/pre-wire ownership and body-limit detector).
- Slice B: FAIL (constitution violation, wrong grant boundary, no restart-durable cap).
- Slice C: FAIL (budget does not yet bind the actual wire request/configuration).
- Slice D: FAIL (owner and detector cover only one silent-death path).
- Slice E: FAIL (missing causal false-positive case).
- Slice F: FAIL (release detector does not bind failure to the sign/publication boundary).
- F4/F9: verified closed at revision `59c0ac5` with the proof ceilings stated above.

The weakest link in this review is that Slices A-F do not exist yet, so their failure
claims are design-level counterexamples against the written plan and current interfaces;
they are not runtime observations of implementations. No live provider, Telegram, paid
canary, signing, publication, or installation action was run.

VERDICT: FAIL
