# HARD QUESTIONS — Codex adversarial design attack

Scope: the approved single-user, Linux-first P0 vertical. Severity is ranked `P0-BLOCKER > P0-RISK > P1+ > OVERENG`. A blocker means that the current contracts either cannot produce a PRD §6 result or permit two incompatible implementations of that result; it does not mean every supporting subsystem must be feature-complete.

1. **P0-BLOCKER [ANSWER-Q1] — A first-party Telegram adapter must not require the extension engine.**

   **Failure scenario:** the product enables Telegram because PRD P0 requires it, but Annex P1.6 rejects activation because `extensions` is absent; enabling `extensions` instead pulls plugin lifecycle, supply-chain scanning, signatures and rollback into P0. Both outcomes violate the approved scope. The conflict is literal: HARNESS-SPEC:1461-1466 says “`channels requires extensions+7.3+approval-core`” and rejects registration without an active extension closure, while PRD:28 requires Telegram and PRD:37 defers plugins.

   **Fix:** split the capability into `channels-core` and `external-channel-adapters`. Compile one first-party Telegram adapter into the signed NEXUS binary and register it statically; it consumes channel auth, admission, inbox/outbox, S7.3 delivery and approval-core, but is not an extension. Amend P1.6 so only dynamically loaded/external adapters require `extensions + S6.8 + S11.5`. Keep one adapter interface and the same conformance suite for built-in and future plugin adapters. This is cleaner than dragging an unused general extension engine into P0.

2. **P0-BLOCKER [ANSWER-Q2] — Bind the P0 walking-skeleton order to minimal dependency cuts, not either document's coarse phase labels.**

   **Failure scenario:** following SECTION-MAP literally builds full S7 and full S5 before the first conversation (`K1`, SECTION-MAP:26-35); following HARNESS-SPEC literally ships a provider/tool slice before full S7/S5 (HARNESS-SPEC:1029-1039). One path delays all product feedback behind distributed queue/worktree machinery; the other permits effectful work before its reliability owner exists.

   **Binding order:**

   1. Run the real-kernel sandbox feasibility probe immediately; it gates only arbitrary exec, not the rest of P0.
   2. Build K0: typed envelope/schema, immutable `profile_id`, config, one SQLite transaction owner, EventJournal, deterministic clock/ID seams and a minimal external checker.
   3. Build the safe conversation spine: S6.0 PEP + S6.9 chokepoint, S7.1-min (`AttemptGrant`, timeout/cancel, no adapter retry), one provider, one-turn loop and terminal REPL. No effectful process tool yet.
   4. Add explicit-write profile-scoped memory and prove restart recall plus cross-profile denial.
   5. Add the P0 reliability subset: durable local scheduler/occurrences, Telegram inbox, transactional outbox and S7-owned retries; then ship reminders and the built-in Telegram adapter.
   6. Add one-shot sandboxed exec only after the hostile probe passes. Initially grant a disposable work directory; user-workspace mutation waits for S5.
   7. Defer full S7.2 distributed lease/fencing and full S5 shadow-git/worktrees until a real multi-worker or P1 coding consumer exists.

   Amend SECTION-MAP K1 to name `S7.1-min`, `S7.3-local` and `S5 only before user-workspace mutation`; amend the SPEC slices to reference those cuts. The DAG remains binding for dependencies and the slice plan remains binding for demonstrable increments, but neither coarse bucket becomes an all-or-nothing prerequisite.

3. **P0-BLOCKER [BREAK] — The P0 reminder has no bound durable scheduler in the P0 product contract.**

   **Failure scenario:** a reminder due during downtime is either lost, fired twice after restart, or fired at the wrong instant across the Zagreb DST fold. PRD:49 requires a persistent reminder that “fires at the right time” after shutdown. Yet the actual semantics live in P2.6/S3.6 (HARNESS-SPEC:1514-1521), while the build plan activates automation only in its late P5 slice (HARNESS-SPEC:1045-1051), and `ObligationStore.EvaluateDue` merely assumes that trigger plane (GAPFIX-kilo:115-124).

   **Fix:** make a *minimal local scheduler* an explicit P0 obligation dependency: persisted `next occurrence`, IANA zone, UTC instant, fold/gap policy, missed-run policy, stable occurrence ID, injected wall/monotonic clocks and a single-process overlap rule. It need not include webhooks/watch/heartbeat or distributed leases. Gate P0 with restart-before-due, restart-after-due and `Europe/Zagreb` fold/gap tests.

4. **P0-BLOCKER [EDGE] — “Remind me tomorrow” has no parsing-to-commit contract.**

   **Failure scenario:** at 23:58, “remind me tomorrow at 2” can mean 02:00 in the active profile's zone, the machine zone, or UTC; on a DST transition it may be nonexistent or duplicated. The design starts at a fully formed `ScheduleManifest` (HARNESS-SPEC:1517-1520), but PRD:27 and PRD:49 start with natural language. Nothing binds the interpretation the user saw to the durable schedule that is committed.

   **Fix:** introduce a narrow `ReminderDraft {original_text, profile_id, resolved_local_time, iana_timezone, resolved_utc, fold_choice, parser_version}`. Show a deterministic preview and require clarification only when date/time/zone is ambiguous; commit the exact draft hash, then return the scheduled local time and zone. Test “tomorrow”, month-end, spring gap, autumn fold and timezone change after creation. Do not build a general NLP planning DSL.

5. **P0-BLOCKER [BREAK] — Outbound durability is designed; inbound Telegram durability is absent.**

   **Failure scenario:** NEXUS performs an action for Telegram `update_id=42`, then crashes before advancing the polling offset. Telegram redelivers 42 and the action runs again. If NEXUS advances the offset first, the inverse crash loses the user's request. The channel contract lists identity, allowlists, threading, delivery receipt, retry and HITL (HARNESS-SPEC:772-777), and S7.3 specifies only `run_done != result_delivered` plus an outbox (HARNESS-PLAN:435-441). Across the named targets there is no durable inbound update/offset state machine.

   **Fix:** add a local transactional inbox keyed by `(adapter_id, channel_identity, update_id)`: `RECEIVED -> ADMITTED -> TERMINAL`. Persist the normalized message before acknowledging/advancing the remote offset, derive the run/idempotency key from that row, and make replay return the existing outcome. Test SIGKILL at each boundary: before insert, after insert, after admission, after effect and before offset advance.

6. **P0-BLOCKER [BREAK] — Profile isolation stops at selected stores instead of following the whole causal chain.**

   **Failure scenario:** a work-profile Telegram request creates an outbox item or scheduled run; the user switches the process-wide “current profile” to private before delivery/restart, and the item is rendered, retrieved or approved under private context. `PersonalProfile` scopes memory/keychain/permissions/triggers (GAPFIX-kilo:50-69), and an obligation has `Owner ProfileID` (GAPFIX-kilo:111-123), but the canonical Envelope has principal/tenant/workspace and no `profile_id` (HARNESS-SPEC:1373-1379). Transcript, journal projections, queue, provider session, inbox/outbox and approval challenges therefore have no mandatory profile join key.

   **Fix:** add non-null `ProfileID` at admission and carry it immutably through message, run, turn, tool call, memory fact, obligation, scheduled occurrence, cache, journal event, inbox/outbox and approval challenge. Channel identity must bind to an explicit/default profile before admission; no mutable global profile lookup after that point. Use composite DB keys and negative end-to-end tests that seed identical IDs/content in work/private and exercise restart, queue replay, delivery and approval.

7. **P0-BLOCKER [BREAK] — The claimed “gateway crash … does not repeat” guarantee is impossible for Telegram without a remote idempotency or reconciliation primitive.**

   **Failure scenario:** Telegram accepts `sendMessage`; NEXUS is SIGKILLed before recording the receipt. Retrying can duplicate the message, while marking delivered before send can lose it. A local idempotency key cannot make a non-transactional remote API exactly-once. Nevertheless E15 claims a crash “neither loses the reply nor repeats the side-effect” (ARCHITECTURE-ESSENTIALS:152-158), derived from HARNESS-PLAN:435-438.

   **Fix:** state the honest boundary: the local outbox is exactly-once *admission* and at-least-once remote delivery unless the adapter proves provider-side idempotency/reconciliation. Give every message a visible stable delivery ID, deduplicate inbound approval responses by Telegram message/update ID, and route `sent-but-unrecorded` to `UNKNOWN -> RECONCILING` instead of blind retry. Acceptance must inject a crash after remote acceptance and allow either a reconciled single delivery or an explicit UNKNOWN requiring recovery—never an unprovable exactly-once assertion.

8. **P0-BLOCKER [BREAK] — The sandbox policy does not describe the runtime closure needed for ordinary Linux commands.**

   **Failure scenario:** the helper successfully applies Landlock and calls `execveat`, but a dynamically linked binary cannot open its loader/shared libraries or `/etc/ld.so.cache`; a script cannot open its shebang interpreter; a shell cannot execute a child binary. Broadly granting `/lib`, `/usr` and `/etc` makes commands run but silently expands the readable host surface and can violate the `/etc/shadow` criterion. `ExecSpec` names only one executable and hash (DESIGN-S0-sandbox-codex:1019-1038), while the helper restricts paths before `execveat` (DESIGN-S0-sandbox-codex:1182-1198); no interpreter/library/child-exec closure is specified.

   **Fix:** make the P0 command contract deliberately small: promoted absolute ELF executables only, with a compile-time/runtime-resolved, hash-bound read/execute closure for loader and libraries plus a disposable RW directory; reject scripts, shell strings and undeclared child executables. Alternatively use a measured immutable runtime root, but do not invent broad host path grants. Add real dynamically linked fixture, shebang rejection, undeclared child-exec rejection and `/etc/shadow` denial tests. General shell sessions can wait.

9. **P0-RISK [BREAK] — “Reminder” and “finish Y” are one generic Obligation even though their completion semantics are incompatible.**

   **Failure scenario:** a simple reminder never reaches `DONE` because no world-state checker proves the user saw it, so it keeps firing; or a delivery receipt is treated as proof that a real-world task (“finish Y”) is complete. PRD:27 combines reminders and jobs, while its done criterion tests only a reminder (PRD:49). GAPFIX-kilo:111-130 gives every obligation a machine-evaluated `DoneDef` and says “Remind me” is the same construct; E13 then makes a checker a P0 dependency (ARCHITECTURE-ESSENTIALS:133-141).

   **Fix:** P0 supports two explicit types only: `Reminder` with `SCHEDULED -> DUE -> DELIVERY_PENDING -> DELIVERED -> ACKED|EXPIRED`, and a small set of typed `Task` handlers with handler-specific postconditions. Do not claim arbitrary “finish Y” until a supported handler and verifier exist. A delivery receipt proves delivery, user acknowledgment proves acknowledgment, and neither proves an external job completed.

10. **P0-RISK [EDGE] — Default-on memory approval can make the restart-memory criterion technically safe but practically dead.**

   **Failure scenario:** ordinary conversation produces no persistent fact unless the user answers an approval prompt for every extraction; if raw transcripts are later searched to make recall work, the system bypasses the memory approval and profile boundary. HARNESS-PLAN:487-493 explicitly sets write approval default ON, while PRD:48 expects something said in one conversation to be known in another.

   **Fix:** define one P0 rule: explicit user imperatives such as “remember X” create a profile-bound fact after an exact preview (the utterance itself is the write intent); inferred facts remain session-only or enter a batch review queue and are not recallable until accepted. Raw transcripts are not semantic memory. Test explicit remember/restart, rejected inference, correction/supersession and cross-profile non-recall.

11. **P0-RISK [EDGE] — “One journal owner” does not yet define the SQLite transaction that couples domain state, inbox/outbox and projection progress.**

   **Failure scenario:** Telegram admission, scheduler firing and memory approval concurrently write; the journal commit succeeds but outbox/occurrence state does not, or a projector advances its offset before its row is durable. Restart then loses a delivery, repeats an occurrence, or spins on `SQLITE_BUSY`. Annex P0.3 requires durable append before publishing projection offsets (HARNESS-SPEC:1394-1402), but does not bind which records share one SQLite transaction or specify writer serialization/busy handling; E4's owner statement is semantic, not a physical commit boundary (ARCHITECTURE-ESSENTIALS:45-56).

   **Fix:** for P0 use one SQLite database, one serialized write executor, WAL with bounded `busy_timeout`, and explicit `BEGIN IMMEDIATE` transaction recipes. Inbox admission + journal event, scheduled occurrence + run admission, and terminal result + outbox enqueue each commit together. Projections that can be rebuilt may lag via durable offsets; externally visible intent rows may not. Add concurrent Telegram/scheduler/memory tests plus SIGKILL at every commit boundary.

12. **P0-RISK [EDGE] — “Linux first” has no supported-kernel floor, so safe fail-closed can still mean the product simply does not work on the user's Linux.**

   **Failure scenario:** installation succeeds on a kernel lacking the required Landlock ABI/right or seccomp architecture profile; every command is correctly blocked with `SANDBOX_CAPABILITY_UNAVAILABLE`, failing PRD §6 item 6 only after the rest of P0 is built. The design explicitly does this (DESIGN-S0-sandbox-codex:1212-1219, 1236-1237) but names no minimum distro/kernel matrix.

   **Fix:** before implementation, run and archive the hostile probe on the actual deployment host and publish the exact supported kernel/architecture/Landlock ABI floor. Startup must report `conversation-only` versus `P0-capable`; release acceptance requires the target host to pass. Preserve fail-closed behavior—do not add a weaker fallback.

13. **OVERENG [OVERENG] — Transactional runtime capability activation has no P0 payer.**

   **Concrete cost:** one fixed single-user binary is being asked to implement 10 activation states, immutable plans, plan/config/policy hashes, gate attestations, a rollback-token vault, prepare reconciliation, generation CAS and quarantine recovery (DESIGN-S0-sandbox-codex:735-853, 856-930). That is a second workflow engine before the first reminder, primarily to support dynamic combinations that P0 has already fixed.

   **Fix:** compile the approved P0 feature set and validate a typed startup config plus live sandbox/provider/channel probes. Publish one immutable startup capability snapshot and restart on configuration change. Retain fail-closed closure validation, but defer transactional hot activation, rollback vault and concurrent generations until an external extension or no-restart activation requirement exists.

14. **OVERENG [OVERENG] — Decay + Audn is premature machinery for a memory corpus that does not yet exist.**

   **Concrete cost:** P0 commits to FSRS-lite ranking, hot/warm/cold archival and contradiction resolution on read (HARNESS-PLAN:495-506; ARCHITECTURE-ESSENTIALS:143-150) before proving the basic product loop “explicit fact survives restart and is recalled only in its profile.” This adds ranking parameters, archival tiers and surprising read-time mutation without a measured retrieval problem.

   **Fix:** P0 stores explicit profile-bound facts with provenance, timestamps and append-only supersession; retrieval is deterministic recency plus exact/tag match. Keep bytes lossless. Add Decay only when a representative corpus shows a measured precision/latency problem, and Audn only when contradiction cases exceed a declared threshold and have a user-visible resolution policy.

15. **OVERENG [OVERENG] — Several Annex “P0” gates belong to unselected P1 capabilities, not the approved P0 walking skeleton.**

   **Concrete cost:** hardware-fit for local models (P0.8), in-turn lint/test verification/TIA (P0.9), and byte-identical workspace rollback (P0.11) are formalized as P0 gates (HARNESS-SPEC:1554-1567), while PRD:32-37 puts coding mode after P0 and does not require local inference. Treating their numbering as product priority pulls model-sizing and coding-workspace infrastructure ahead of reminders and Telegram.

   **Fix:** keep the contracts, relabel their activation triggers: P0.8 gates `local-inference`, P0.9 and P0.11 gate `coding`/user-workspace mutation. None is a prerequisite for terminal conversation, explicit memory, reminders, built-in Telegram, profile isolation or disposable-directory sandbox exec. Promote them only with their first real consumer.

SUMMARY: 8 P0-BLOCKER, 4 P0-RISK.
