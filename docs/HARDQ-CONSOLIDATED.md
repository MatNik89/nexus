# HARDQ-CONSOLIDATED — moderator merge of 3 independent adversarial passes

Sources: HARDQ-codex.md (8 BLOCKER/4 RISK), HARDQ-kilo.md (2/7), HARDQ-agy.md (5/7).
Method: convergence-ranked; each resolution names its source findings. Verified against
sources, not rubber-stamped. Status per item: ADOPTED (design fold, no product change) ·
USER-DECISION · PARKED (P1+, not now).

---

## A. Open questions Q1/Q2 — UNANIMOUS, ADOPTED

### A1. Q1 — Telegram in P0 vs `channels requires extensions` (codex#1, kilo-Q1, agy#1)
All three converge on the same resolution, independently:
**Split the capability: `channel:builtin` vs `channel:plugin`.** The P0 Telegram adapter is
compiled into the signed binary and registers statically; it consumes channel auth,
admission, S7.3 delivery and approval-core, and its integrity is attested by the release
artifact signature (P1.5), NOT by a runtime extensions closure. `channels requires
extensions + S6.8 + S11.5` applies ONLY to dynamically loaded adapters (P1+); the P1.6 RED
(`INCOMPLETE_CAPABILITY_CLOSURE`/`APPROVAL_REPLAY`) keeps testing the plugin path. Amend
HARNESS-SPEC P1.6 text accordingly. Same adapter interface + conformance suite for both.

### A2. Q2 — binding P0 build order (codex#2, kilo-Q2, agy#2)
All three converge: **HARNESS-SPEC vertical slices, with SECTION-MAP DAG edges satisfied by
NAMED -min contracts pulled forward** (the same mechanism SECTION-MAP already uses for
S8.1-min/S11.1-min/S16.6-det). The gap was that S7/S5 -min cuts were never named. Binding:
- `s7-min` = `AttemptGrant` type + cancel token + no-retry policy (retryable → FAILED_TERMINAL
  in P0); full S7 taxonomy/budgets/fencing = P2.
- `s5-min` = `AtomicWriter` only; full S5 shadow-git/worktree = P3 (before user-workspace mutation).
- `3.6-min` = single persisted Schedule trigger (see B1); webhook/watch/heartbeat = P5.
- `7.3-min` = transactional outbox + inbox + occurrence idempotency (see B2); full N9 = P2.
Walking-skeleton order (codex's 7 steps, consistent with kilo/agy tiers):
(1) sandbox feasibility probe (gates only arbitrary-exec) → (2) K0: typed contracts,
config, EventJournal, deterministic clock/ID seams, minimal checker → (3) safe conversation
spine: PEP + 6.9 chokepoint + s7-min + one provider + one-turn loop + REPL (no effectful
tool) → (4) profile-scoped memory + restart-recall + cross-profile-denial tests →
(5) reliability subset: 3.6-min scheduler + Telegram inbox/outbox + S7-owned retries →
(6) one-shot sandboxed exec in disposable workdir (after probe passes) → (7) defer full
S7.2/S5 to first real consumer. Materialize in tasks-P0.md; amend SECTION-MAP K1 note.

---

## B. Convergent P0-BLOCKERs — ADOPTED

### B1. P0 has no durable scheduler (codex#3, kilo#2, agy#6)
PRD §6.3 requires "fires on time, survives shutdown", but 3.6/P2.6 are scheduled P5/P2.
**Pull a minimal local scheduler into P0:** persisted next-occurrence (UTC instant + IANA
zone + fold/gap policy `dst=ONCE_FIRST`), `missed_run=COALESCE`, stable occurrence ID,
injected wall/monotonic clocks, single-process overlap rule, and a **startup + wake catch-up
sweep** (`EvaluateDue` = range query `DueAt <= now`, fired with explicit OVERDUE notice; hook
D-Bus PrepareForSleep where available). RED: restart-before-due, restart-after-due,
Europe/Zagreb fold/gap, suspend-over-due-time.

### B2. Telegram delivery has no P0 durability contract (codex#5, codex#7, kilo#4)
**Inbound:** durable transactional inbox keyed `(adapter_id, channel_identity, update_id)`,
`RECEIVED → ADMITTED → TERMINAL`; persist normalized message BEFORE advancing remote offset;
replay returns existing outcome. **Outbound:** occurrence-idempotency row written durably
BEFORE send; outbox + stable delivery ID. **Honest boundary (amends E15 wording):** local
outbox gives exactly-once ADMISSION and at-least-once remote delivery; `sent-but-unrecorded`
→ UNKNOWN → RECONCILING, never blind retry, never an exactly-once claim over a
non-transactional remote API. RED: SIGKILL at every boundary (before insert / after insert /
after admission / after effect / before offset advance / after remote accept).

### B3. Profile isolation must be physical + carried through the causal chain (codex#6, kilo#6, agy#7)
Two independent attacks, both adopted:
1. **One SQLite file per profile** (`~/.nexus/profiles/<p>/nexus.db`) + separate system DB
   with zero profile payloads — leak-by-omitted-WHERE and FTS5 shared-vocabulary/freelist
   residual leaks become structurally impossible. Scope column stays as redundant tag.
2. **Non-null `ProfileID` stamped at admission** and carried immutably through envelope, run,
   turn, tool call, memory fact, obligation, occurrence, journal event, inbox/outbox and
   approval challenge; channel identity binds to a profile BEFORE admission (per-chat
   deny-default binding, first-contact prompt — kilo#7); no mutable global current-profile
   lookup after admission. RED: identical content seeded in both profiles + restart/replay/
   delivery/approval never crosses.

### B4. Sandbox must define the runtime closure for real commands (codex#8, agy#3)
> **REFINED 2026-09-03 (Phase-0 code review):** the dir-grant closure below proved
> internally contradictory — with `/usr` bound as a dir, `exec /usr/bin/id` succeeded
> (demonstrated), violating "undeclared child rejected". Binding closure is now SYNTHETIC
> and content-pinned: only the promoted target + its resolved loader/libraries, memfd-copied
> at Prepare and bound via `--ro-bind-data`; no `/etc` grants at all. Essentials E10 carries
> the current wording; the paragraph below is kept as the original resolution record.
Landlock is allowlist-only: workspace-only rules make every dynamic binary fail (`EACCES` on
`ld.so`); naive `/etc` grant violates the `/etc/shadow` criterion. **P0 command contract is
deliberately small:** promoted absolute ELF executables with a resolved, hash-bound
read/execute closure (`/usr`, `/lib*`, `/bin`, `/sbin` dirs; `/etc/ld.so.cache` + CA certs as
FILE grants — NEVER recursive `/etc`) + disposable RW workdir; scripts, shell strings and
undeclared child executables REJECTED in P0. RED: dynamic ELF runs; shebang rejected;
undeclared child-exec rejected; `/etc/shadow` and network denied. (Backend choice → D1.)

### B5. Assistant obligations can never satisfy a coding-only evidence model (codex#9, kilo#3)
`MarkDone` gated on diff/exit-code makes every reminder un-completable (re-fires forever).
**P0 ships two explicit obligation types:** `Reminder` (`SCHEDULED → DUE → DELIVERY_PENDING →
DELIVERED → ACKED|EXPIRED`; assistant-grade evidence = durable delivery receipt + user ack,
correlated to occurrence) and typed `Task` handlers with handler-specific postconditions.
No generic "finish Y" claim until a verifier exists. Receipt proves delivery, ack proves
acknowledgment, neither proves external-world completion. Artifact-grading stays P1 coding.

### B6. Durable HITL suspend/resume (agy#5; implied by P1.6/S7 but never concretized)
Approval wait must survive process death: on `DecisionAsk` the loop commits
`TurnSuspended{run, challenge, tool_call, journal_offset}` and EXITS; approval arrival
appends `ApprovalReceived` and resumes by rehydrating from the journal. No in-memory
blocking wait. RED: SIGKILL between challenge and approval; approve after restart → action
completes exactly once.

### B7. Single-writer discipline is a mechanism, not a convention (codex#11, kilo#8, agy#8, agy#9)
- One serialized append actor owns `EventJournal.Append` + sequence allocation (two live
  event sources exist in P0: interactive turn + scheduler firing).
- Core state projections (sessions, obligations, memory index) fold SYNCHRONOUSLY in the
  SAME SQLite transaction as the append (read-your-own-writes); only observability
  projections lag async.
- Named transaction recipes (`BEGIN IMMEDIATE`): inbox-admission+journal-event;
  occurrence+run-admission; terminal-result+outbox-enqueue.
- WAL + `busy_timeout` + **daemon-owns-DB / CLI-connects-via-UDS** pattern (no two processes
  on one DB file).
RED: concurrent turn+reminder append → contiguous sequences, nothing lost; SIGKILL at each
commit boundary.

### B8. P0 memory: explicit facts, NO decay machinery (codex#10, codex#14, agy#4)
Two attacks, one resolution: FSRS decay on facts violates PRD §6.2 ("still knows it"), and
Decay+Audn have no P0 corpus to justify them. **P0 memory = explicit profile-bound facts**
("remember X" = write intent with exact preview; inferred facts session-only or batch-review,
not recallable until accepted; raw transcripts are NOT semantic memory), append-only
supersession, deterministic recency+exact/tag retrieval, bytes lossless. **Decay(FSRS) and
Audn move OUT of P0** → P1 `memorija` flag, activated only on a measured retrieval problem,
and NEVER applied to explicit facts (fact-type exempt, `S=∞`). Amends E14/P0.13 scope.

### B9. Static capability snapshot for P0; transactional Activator deferred (codex#13, kilo#10, agy#12 — unanimous OVERENG)
P0 has zero runtime activation (flags are compile-time; nothing dynamic loads). **P0 ships
`Resolver.Resolve` as fail-closed validation (unknown/cycle/conflict → reject) + a sealed
startup capability snapshot (config + live sandbox/provider/channel probes); restart on
config change.** `ActivationPlan`/`RollbackVault`/`ReconcilePrepare` machinery is DEFERRED to
the first dynamic consumer (extensions, P4+). E7 stays as the design for that phase; it is
no longer a P0 build item.

---

## C. Adopted second-order fixes (single-source but verified sound)
- **C1 (kilo#5):** P0 secret redaction = known-ref exact-match only (provider key, bot token,
  env names → `SecretRef`); entropy/shape detection stays P4. Journal RED still holds for the
  known set.
- **C2 (kilo#9):** non-text Telegram (photo/voice) → typed fail-closed reply ("text only in
  this version"); RED: BlockImage/BlockAudio never reaches a nonexistent sink, never crashes
  the loop.
- **C3 (agy#10):** stuck-detection scoped to WITHIN one interactive turn; scheduled/polling
  iterations carry an explicit continuous-loop policy exempt from identical-argument breaker.
- **C4 (agy#11):** `ExactIntentPayload` canonical fields = tool name + canonical JSON args +
  target resource + ProfileID (+ target `(device,inode)` for destructive FS ops); NEVER
  transient timestamps or repo-wide revision hashes — delayed mobile approval must not
  self-invalidate.
- **C5 (codex#12):** publish the supported kernel/ABI floor; run+archive the hostile probe on
  the actual deployment host; startup reports `conversation-only` vs `P0-capable`. Fail-closed
  stays.
- **C6 (codex#15):** Annex numbering ≠ product priority: P0.8 gates `local-inference`,
  P0.9/P0.11 gate `coding`/user-workspace mutation. Contracts keep their IDs; activation
  triggers relabeled.
- **C7 (kilo#11):** S0.4 upcast CHAIN + quarantine sink deferred to first real migration
  (v2); P0 ships `schema_version` + reject-unknown (satisfies E8's intent).
- **C8 (kilo#12):** P0 health = liveness heartbeat + last-occurrence-fired counter; 15.5
  SemanticHealth deferred with its eval-infra dependencies.

## D. USER-DECISION — RESOLVED 2026-09-03
### D1. P0 sandbox backend → **bwrap (option A), user-approved 2026-09-03**
**bwrap (bubblewrap) is the P0 ENFORCED backend** behind the `SandboxBackend` interface
(Probe/Compile/Launch/Attest), fail-closed, attested, and subject to the SAME hostile
conformance suite as any backend. The hand-written native Landlock+seccomp helper
(DESIGN-S0) becomes the **P1 hardening path** — the suite is the invariant, the backend is
swappable. This amends E10/E1: single-binary purity yields for this one component; bwrap is
a DECLARED install prerequisite (see F1), never a silent dependency. No weaker fallback:
bwrap absent → exec capability OFF (conversation-only), everything else works.

## F. New user requirement (2026-09-03) — ADOPTED
### F1. Installer/doctor preflight with guided prerequisite install
`nexus doctor` runs at install/first-start: checks OS/kernel floor (C5), bwrap presence,
data-dir permissions, provider key, Telegram token; anything missing → offers CONSENTED
install (e.g. `apt install bubblewrap`) or exact instructions; setup completes only when
green. Declined prerequisite → the dependent capability stays fail-closed off (reported as
`conversation-only` vs `P0-capable`), never a degraded-but-on mode. Maps to 17.3 packaging +
C5; minimal P0 form = check + consented one-command fix; full wizard = P1 polish.

## E. PARKED (P1+, do not act now)
- agy#15 symedit LSP-unavailable fallback tier (contradicts locked "never text-fallback" —
  re-litigate at P1 coding design, with the refuse-on-ambiguous invariant preserved).
- agy#14 audit hash-chain value in single-user threat model (15.3 is P4; revisit then).
- agy#13/codex quarantine of S12/S17/S18 structs out of core contracts (enforce at scaffold
  time: no fleet/tenant types imported by `internal/contracts`).

## Traceability
Every ADOPTED item lands in: ARCHITECTURE-ESSENTIALS (E6/E7/E10/E14/E15 amendments + P0
scope), HARNESS-SPEC Annex A (P1.6 split, P0.8/9/11 triggers), SECTION-MAP (K1 -min note),
tasks-P0.md (build order + RED list). PRD unchanged — no adopted item alters the approved
6 capabilities or §6 done-criteria; B5 makes §4.3 "dovrši Y" honest (typed handlers only).
