# Adversarial design review round 8 — audit-remediation plan v8
Reviewed revision: `794197922d7f3eddaa51fd274c8007a09fddf669`
Verdict: FAIL

The committed plan header is `PLAN v8` (`docs/PLAN-AUDIT-FIXES.md:1`). This review is
bound to the revision above and is static design evidence only; it does not claim that the
unimplemented slices or their RED-to-GREEN detectors have run.

## Round-7 disposition

1. **CLOSED — former finding 1 (one control policy could not implement both read and
   effect contracts).** V8 defines and binds two closed policies: `getMe` uses
   `PolicyControlRead` with all five read-safe retry codes, while `setMyCommands` uses
   `PolicyControlEffect` with only pre-wire and 429 codes
   (`docs/PLAN-AUDIT-FIXES.md:283-304`). Slice D repeats the same per-kind binding
   (`docs/PLAN-AUDIT-FIXES.md:491-493`). Detector D7 exercises 5xx and post-write failures
   on both methods, requiring a due-time fresh-grant retry only for `getMe` and UNKNOWN with
   zero retry for `setMyCommands` (`docs/PLAN-AUDIT-FIXES.md:531-533`). This closes both the
   unrealizable shared-policy contract and its false-green detector gap.

2. **CLOSED — former finding 2 (durable `Next(op, nil)` had no detector).** The owner rule
   remains explicit (`docs/PLAN-AUDIT-FIXES.md:206`), and new detector 9g calls durable
   `Next(op, nil)` both while grant-eligible and after cap/deadline exhaustion. It requires
   a fail-closed error, no grant, no state transition, and no journal event, and explicitly
   requires ablation of only the nil-builder check to turn RED
   (`docs/PLAN-AUDIT-FIXES.md:397-400`). The current `s7min` has no `Next` API and therefore
   cannot satisfy this detector before the slice (`internal/kernel/s7min/s7min.go:94-155`).

3. **CLOSED — stale lease API spelling note.** The lease-recovery path now uses the full
   `Next(op, build)` signature (`docs/PLAN-AUDIT-FIXES.md:187-196`).

4. **CLOSED — detector-completeness weakest-link note.** Detector 9g is now the negative
   boundary call that proves a durable caller cannot omit the companion builder
   (`docs/PLAN-AUDIT-FIXES.md:397-400`).

5. **STILL OPEN AS A PROOF CEILING — implementation/runtime evidence.** F1, F2, F3, F5,
   F6, F7, and F8 remain unchecked (`docs/PLAN-AUDIT-FIXES.md:24`). No implementation,
   causal ablation, remote Telegram run, restart proof, release publication, or deployment
   evidence exists at this design-review stage. This is not independently a verdict reason.

## Numbered new findings

1. **HIGH — an ambiguous `setMyCommands` effect is blindly retried after daemon restart by
   relabeling it as a new operation, violating E9 across the recovery boundary.**

   Evidence: V8 correctly says a 5xx, post-write failure, or malformed reply from
   `setMyCommands` lands `OutcomeUnknown` and receives no new grant without reconciliation.
   It then explicitly says that no reconciliation exists and the menu is re-registered on
   the next daemon start as a **new operation** (`docs/PLAN-AUDIT-FIXES.md:294-304`). Changing
   the operation ID does not make the remote effect definite: the prior request may already
   have mutated the command menu. E9 requires every effectful call without a valid receipt
   to proceed through UNKNOWN to RECONCILING and forbids blind retry
   (`docs/ARCHITECTURE-ESSENTIALS.md:114-120`; `AGENTS.md:26`). The current lifecycle proves
   the restart reachability: every `Adapter.Run` unconditionally invokes
   `registerCommands`, which performs `setMyCommands`
   (`internal/channel/telegram/telegram.go:428-463`), and each daemon start launches a fresh
   adapter run (`cmd/nexus/main.go:289-292`). Detector D2 only advances in-process backoff
   intervals and observes zero same-process retries; it never restarts and therefore remains
   GREEN while the forbidden second physical effect occurs
   (`docs/PLAN-AUDIT-FIXES.md:516-519`). The `<n>` operation identity also does not bind a
   restart to the unresolved prior effect (`docs/PLAN-AUDIT-FIXES.md:284`).

   Concrete fix: make command-registration effect state durable and bind it to a stable
   identity derived from the canonical desired command set. On startup, rehydrate an UNKNOWN
   prior registration and perform no `setMyCommands` call. Either leave the capability
   degraded pending explicit reconciliation, or add an S7-governed read-only
   `getMyCommands` reconciliation that compares the remote state with the exact desired
   payload before declaring success or authorizing a new effect operation. If reconciliation
   is added, include that Bot API method in the complete governed call-site table. Add a
   restart detector: ambiguous registration -> persisted UNKNOWN -> reconstruct daemon/S7
   from the same journal -> zero subsequent `setMyCommands` calls; ablate the durable
   recovery guard and require the detector to turn RED.

## Notes (not verdict reasons)

- `PolicyUI` is described as “terminal on failure” while its methods are remote mutations
  (`docs/PLAN-AUDIT-FIXES.md:285-286`). The global E9 rule should be made explicit here:
  definite failures may be terminal, but 5xx/post-write/malformed outcomes must still be
  UNKNOWN. The existing `ErrAmbiguousSend` path makes this implementable; the wording is
  presently ambiguous rather than proof of a contradictory design.
- The weakest link is recovery coverage for non-delivery effects: the plan now proves
  same-process zero-retry behavior but not that restart preserves that decision.

## Proof ceiling

This review establishes only plan consistency against the committed owner documents and
current call graph. It does not establish buildability, causal RED-to-GREEN behavior, remote
API semantics, durable restart behavior, release-gate execution, or deployment safety.

Skill update: none

VERDICT: FAIL
