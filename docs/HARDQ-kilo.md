# HARDQ — NEXUS plan attack (pre-code, adversary pass)

Scope: P0 vertical = conversation · cross-session memory · obligations/reminders · Telegram ·
work/private profiles · Linux sandbox (done-criteria PRD §6). Findings name a concrete failure
or a concrete cost with no payer, weighted P0-first.

---

## P0-BLOCKER

**1. [BREAK] The native sandbox is the ONLY path to done-criterion 6, and it is a 2–3 month
one-person block. bwrap must be the P0 backend, native helper = P1.**
Done-criterion 6 ("ne može do /etc/shadow, ne može van na mrežu, dokazano zlonamjernim testom") is
non-negotiable in P0. The locked path to it is the self-reexec helper with hand-written per-arch
seccomp-BPF + `execveat`/`openat2`, gated on a hostile conformance suite over a REAL kernel. The
plan's own words: PLAN-HOLES C2 "Realno: mjeseci, ne tjedni"; DESIGN-S0 "CURRENT PROOF: NOT RUN".
Concrete cost with no payer: one person + Claude writing a non-bypassable seccomp filter (one missed
syscall — `io_uring`, `open_by_handle_at`, `process_vm_writev`, `perf_event_open` — = escape) and then
driving a hostile suite to GREEN on real hardware is the entire P0 schedule, alone. Meanwhile bwrap
(Flatpak-grade, battle-tested) already satisfies all five capability columns (FS_RO/FS_RW via
bind+ro, NET_DENY via `--unshare-net`, SYSCALL via its seccomp layer, PROC_TREE via PID ns) and can
be probed/attested through the SAME `SandboxBackend` interface. Fix: **promote bwrap from "optional
external adapter" to the P0 ENFORCED backend** (attested + hostile-suited), keep the native helper
as P1 hardening. This REVERSES a locked E10 decision — flag it explicitly as a re-open, not a silent
workaround. The "single-binary, no external tools" principle (E1) must yield for the one component
where it costs the whole P0.

**2. [BREAK] Obligations/reminders need the schedule trigger (3.6) + clock semantics (P2.6), which
HARNESS-SPEC schedules at P5. Done-criterion 3 is unmeetable without a P0 scheduler.**
PRD §6.3: reminder "javi se u pravo vrijeme, preživi gašenje". But SPEC:1045 puts `automation`/3.6
at P5, and P2.6 (DST/missed-run/overlap) is a P2 contract. There is no P0 component that persists a
wall-clock trigger and re-fires it after restart. Concrete failure: you build conversation + memory +
Telegram, then discover "podsjeti me sutra u 9" has nothing to schedule it, and the feature is
blocked on a P5 flag. Fix: pull **3.6-min** into P0 — a single Schedule trigger persisted in SQLite,
occurrence-idempotency, `missed_run=COALESCE`, `dst=ONCE_FIRST` (one default, not the full P2.6
policy table). Webhook/watch/heartbeat stay P5.

---

## P0-RISK

**3. [BREAK] `MarkDone` is evidence-gated, but the evidence model is coding-only; assistant
obligations have no diff/exit-code, so they can never complete.**
S16.6/GAPFIX-G4: `DoneCondition.Evaluate` returns "git-diff/exit-code/API-poziv/artefakt — NE proza".
P0 obligations are "podsjeti me", "dovrši Y", "javi mi kad stigne mail". Concrete failure: reminder
fires at 9:00 → there is no diff, no exit code, no artifact → `MarkDone` is rejected forever → the
obligation stays ACTIVE and re-fires daily, or the store is polluted with un-completable rows. The
"checker ≠ worker" principle is correct for coding but was smuggled into the assistant vertical.
Fix: define assistant-grade Evidence = **durable delivery receipt + user acknowledgment event**
(durable, correlated to the reminder occurrence), and keep artifact-grading exclusively for the P1
coding branch.

**4. [BREAK] P0 Telegram needs durable-delivery (7.3-N9) and the reminder path has a double-fire
window; both live in P2 (SPEC).**
`run_done ≠ result_delivered` + transactional outbox is SPEC P2 ("S7 cijeli uklj. N9 … prije svakog
budućeg channels/service izlaza"). But Telegram IS P0. Concrete failure: reminder fires → Telegram
send succeeds → process crashes BEFORE the "fired/occurrence" event is journaled → restart sees the
reminder still DUE → fires again → user gets two reminders (violates "ne izgubi" *and* correctness).
Fix: pull **7.3-N9-min** (outbox + idempotent delivery key + occurrence idempotency) into P0; full S7
taxonomy/budgets/fencing stay P2. The occurrence idempotency key must be written durably BEFORE the
outbound send, not after.

**5. [EDGE] P0.3 requires "secrets redacted BEFORE the journal" (RED: secret-never-reaches-sink),
but the secret detector (6.4) is scheduled P4.**
SPEC:1041 puts 6.4–6.5 at P4, yet P0.3's redaction-before-journal is a K0 gate. Concrete failure: a
user pastes an API key or the Telegram bot token into a message; with no 6.4 detector the value lands
in the journal and every projection — a P0.3 RED failure you can't fix without the P4 component. Fix:
P0 ships **known-secret ref redaction only** (env-var names, provider key, bot token — exact match +
`SecretRef`, no entropy/shape heuristics); full 6.4 detection stays P4. State this split in E4 so the
P0 implementer doesn't build (or skip) the wrong half.

**6. [EDGE] Profile isolation is a scope column in ONE SQLite file; "zero leakage" (done-criterion 5)
is backed by a WHERE clause, not a hard boundary.**
G2/9.5 promises "write u work → query u private = 0 hitova", but the memory spine is a single DB with
a scope column; cache uses a tenant key (P2.4, P1+); retrieval ACL is 10.6 (P1+). Concrete failure:
one future query (or a FTS5 index spanning profiles) forgets the profile filter and private memory
surfaces in the work profile — a silent, hard-to-audit leak that fails done-criterion 5 in the field,
not in the happy-path test. Fix: **one SQLite file per profile** (or ATTACHed per-profile DB) — cheap,
structural, impossible to leak by omission; keep the scope column only as a redundant tag.

**7. [EDGE] Telegram→profile attribution is unspecified; the interaction of two P0 capabilities
(Telegram + profiles) has no mechanism.**
14.5 lists "channel→profile binding" as a fold target but no P0 rule. Concrete failure: user sends a
work reminder from the same Telegram account he uses for private chat → it lands in "private" (or
refuses) → either a cross-profile leak (done-criterion 5) or a dead feature. Fix: **per-chat profile
binding, deny-default** — a chat is explicitly bound to a profile (first-contact prompt), unbound chat
is refused with a typed "which profile?" reply. A single global "Telegram = private" default is
acceptable only if documented as v1.

**8. [EDGE] Concurrent journal append: the interactive turn and a firing reminder are two writers;
`sequence` strictly increases per run, so a race produces `ErrIllegalTransition` and drops one flow.**
S0.2 RED rejects sequence fork/gap. P0 has two live event sources (conversation + scheduler). Concrete
failure: reminder fires mid-turn → both goroutines call `EventJournal.Append` → sequence collision →
one event is rejected → the reminder is silently lost (or the user's message errors). The single-writer
invariant is enforced by convention, not mechanism. Fix: a single append actor (one goroutine/queue
owns append + sequence allocation); add a RED that fires a reminder concurrently with an active turn
and asserts both events land with contiguous sequences.

**9. [EDGE] Non-text Telegram (photo/voice) has no P0 UX: BlockImage/BlockAudio are P1+ multimodal.**
A real user WILL send a photo or voice note. The typed contract knows these kinds, but P0 ships no
vision/audio adapter. Concrete failure: an image block is either silently dropped (user thinks the
assistant ignored them) or crashes a decoder that doesn't exist. Fix: fail-closed at the gateway —
typed "can't process images/voice in this version, send text" reply — and a RED asserting a
BlockImage/BlockAudio inbound never reaches a non-existent sink and never crashes the loop.

---

## OVERENG

**10. [OVERENG] The S0.5 transactional Activator (Prepare/Commit/Activate + RollbackVault +
ReconcilePrepare + crash-recovery, 10 RED) has ZERO runtime activation in P0.**
P0 ships no extensions, no plugins, no third-party adapters — nothing is activated at runtime; the
flag set is compile-time. The full Activator gates nothing. Concrete cost: weeks on the single most
complex K0 component (including crash-reconciliation tests) for a feature that first executes at
P4/P5. Fix: P0 ships only `Resolver.Resolve` as a **fail-closed validation function** (unknown/cycle/
conflict → reject), plus the `ActivationState` enum; defer `Activator`/`RollbackVault`/`ReconcilePrepare`
to the phase that first loads a dynamic adapter.

**11. [OVERENG] S0.4 migration/upcast-chain/quarantine built before v1 ever ships a migration.**
The lossless upcast chain (`v→v+1`, MigrationRecord, QuarantineSink) exists to migrate incompatible
event schemas across versions. v1 ships with schema v1 and no migration to perform. Concrete cost: a
single-user assistant builds a registry/upcast/quarantine subsystem + RED before the first event
exists. Fix: P0 ships `schema_version` + reject-unknown (fail-closed), which is already required;
defer the upcast CHAIN + quarantine sink to the first actual incompatible migration (v2). The E8
"QUARANTINE unknown schema" decision is satisfied by reject-unknown — no upcast machinery needed.

**12. [OVERENG] 15.5 SemanticHealth (recidivism/correction-rate/fallback-drift) is P2+-scheduled
work smuggled toward P0 because "S7 breaker consumes it".**
15.5's signals require eval infra (16.x) and multiple model turns to compute. For P0 the only health
that matters is "process alive, reminder fired". Concrete cost: building a signal aggregator with no
signal source. Fix: P0 health = liveness (heartbeat) + "last occurrence fired" counter; defer 15.5 to
the phase that introduces the full S7 breaker + eval loop.

---

## ANSWER-Q1 — `channels requires extensions` vs Telegram-in-P0

Cleanest resolution: **split P1.6's closure by adapter origin.** First-party **built-in** adapters
(Telegram in P0) are compiled into the binary; they are not dynamically loaded, so they do not pass
through the runtime `extensions`/S6.8 supply-chain gate — their integrity is attested by the
release-artifact signature (P1.5, `distributed-artifact`), not by a runtime closure. Third-party
**plugin** adapters (Slack/Discord/WhatsApp in P1+) remain under `channels requires extensions` and
keep P1.6's `INCOMPLETE_CAPABILITY_CLOSURE`/`APPROVAL_REPLAY` RED intact (it still tests the plugin
path). Concretely: amend P1.6 to read "`channels requires 7.3 + approval-core (6.0/12.4) + identity
(6.6)`; `extensions`/S6.8 is required ONLY for dynamically-loaded channel adapters." Add one
sentence in E15: P0 ships exactly one built-in adapter (Telegram); the P1.6 RED covers the plugin
loader, which does not exist in P0, so P0 is not blocked by it.

## ANSWER-Q2 — binding build order for the P0 walking skeleton

**Follow HARNESS-SPEC vertical slices; satisfy SECTION-MAP DAG edges with minimal -min contracts
pulled forward** (the mechanism SECTION-MAP already uses for S8.1-min/S11.1-min/S16.6-det).
- P0 slice = SPEC P1: S1 (config/paths/telemetry) → S2-min (one APIKey provider + structured output)
  → 3.1 loop (with 6.9 chokepoint + sandbox + one tool) → 14.1 minimal REPL.
- `S7-min` = `AttemptGrant` type + cancel token + a single no-retry policy (every retryable class →
  FAILED_TERMINAL in P0). Full S7 (taxonomy/budgets/fencing) stays P2.
- `S5-min` = `AtomicWriter` only. Full S5 (shadow-git/worktree) stays P3.
- `3.6-min` (schedule trigger) + `7.3-N9-min` (outbox) pulled into P0 (findings #2, #4).
The contradiction dissolves: SECTION-MAP's "S7/S5 in K1" means "the loop imports these types/contracts
early" (true — the loop needs `AttemptGrant` and `AtomicWriter`), while SPEC's P2/P3 means "the full
implementations ship later" (also true). The one thing neither document does — but P0 needs — is to
NAME the -min contracts for S7/S5 (and add 3.6-min/7.3-min), which is the real gap. Confirm in
hard-questions and materialize in `tasks-P0.md`.

---

## Already handled (do NOT re-open)

- Identity "one kernel/two profiles, assistant-first" — user-approved (PRD), P0 scope locked.
- Egress is a dialer boundary (not string filtering) — locked, correct.
- Effect-taxonomy UNKNOWN→RECONCILING — locked, correct, and covers the "cancel mid-tool" trap.
- Crypto `hash.Sum(key)` — locked, correct.

---

SUMMARY: 2 P0-BLOCKER, 7 P0-RISK
