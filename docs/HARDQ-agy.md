# HARD-QUESTIONS — Adversarial Attack on Locked NEXUS Architecture

**Meta:** Pre-implementation architectural stress-test  
**Context:** Single-user Linux-first personal assistant built by one developer + Claude  
**Target Documents:** `PRD.md`, `ARCHITECTURE-ESSENTIALS.md`, `HARNESS-PLAN.md`, `HARNESS-SPEC.md` Annex A, `DESIGN-S0-sandbox-codex.md`, `DESIGN-FIXES-r2.md`, `GAPFIX-*.md`, `SECTION-MAP.md`.

---

## 1. EXECUTIVE SUMMARY & TARGETED ANSWERS TO OPEN DESIGN QUESTIONS

---

### [ANSWER-Q1] [P0-BLOCKER] Finding 1 — Channel Capability Closure Conflict: Split Built-in (P0 Telegram) vs Plugin Adapters (P1.6)
* **Concrete Failure Scenario:**  
  `HARNESS-SPEC.md:1459-1466` (Normative Contract `P1.6`) declares `channels requires extensions + 7.3 + approval-core`. If `channels` is activated without active `extensions` closure, `test_channels_contract_fails_closed` raises `INCOMPLETE_CAPABILITY_CLOSURE` and aborts runtime initialization. However, `PRD.md §4 item 4` mandates Telegram as a P0 core deliverable, while `PLAN-HOLES-CONSOLIDATED.md:64-69` and `ARCHITECTURE-ESSENTIALS.md:171` explicitly cut the `extensions` subsystem from P0. A developer attempting to run the P0 Telegram bot under current capability closure contracts will hit an impossible dependency cycle: Telegram cannot start without `extensions`, and `extensions` cannot be built in P0.
* **Root Cause:**  
  Contract `P1.6` conflated third-party dynamic plugin channels (which require supply-chain scanning `S6.8`, dynamic ABI loading, and extension lifecycle `S11.5`) with static built-in adapters compiled directly into the monolithic Go binary.
* **Proposed Concrete Fix:**  
  1. In `S0.5` capability declarations, split channel capability into:
     - `channel:builtin` (native in-tree adapters: stdio REPL, native Telegram long-poller): requires ONLY `S7.3 (durable outbox) + approval-core`. Explicitly exempt from `extensions` and `S6.8`.
     - `channel:plugin` (dynamic external adapters): requires `extensions + S6.8 + S11.5`.
  2. Amend `P1.6` contract text and `test_channels_contract_fails_closed` so that `INCOMPLETE_CAPABILITY_CLOSURE` triggers *only* when dynamic channel manifests are registered without `extensions`. Native P0 Telegram registers directly as `channel:builtin`.

---

### [ANSWER-Q2] [P0-BLOCKER] Finding 2 — Dependency DAG vs Vertical Delivery Slices: Contract-First `-min` Tiering for Walking Skeleton
* **Concrete Failure Scenario:**  
  `SECTION-MAP.md §1` (Topological DAG) places S7 (full retry/queue/lease/fencing) and S5 (full workspace checkpoint/rollback) in `FAZA K1`, strictly *before* S3 (core loop) and S4 (tools/exec) in `FAZA L`. Conversely, `HARNESS-SPEC.md:1020-1040` ("Redoslijed gradnje") specifies that P1 is the "smallest secure vertical slice" shipping loop (3.1) + tools (4.1) + sandbox (6.2) before S7 (P2) and S5 (P3). If a developer strictly follows `SECTION-MAP.md`, they must implement distributed CAS queue fencing (`S7.2`) and git tree snapshotting (`S5.1`) before they can send a single prompt to an LLM or execute `ls`. If they follow `HARNESS-SPEC.md`, `EffectPath.RunTool` in `DESIGN-FIXES-r2.md:49` will not compile because it requires an `s7.AttemptGrant` and `s7.RetryOwner`.
* **Root Cause:**  
  The design conflates full subsystem engines (e.g. distributed disk-backed CAS queue) with minimal in-memory contract types required for pipeline compilation.
* **Proposed Concrete Fix (Binding Build Order for P0 Walking Skeleton):**  
  Adopt a strict 3-tier **Contract-First Tiering** (`-min` contracts):
  1. **Phase 0 — K0 Primitives & `-min` Contracts:**
     - `contracts` (types only: `ToolCall`, `ToolResult`, `EffectPhase`, `CommitReceipt`, `Decision`).
     - `s7-min` (types only: `AttemptGrant` struct + `DirectRetryOwner` in-memory stub that executes 1 attempt with zero queue/CAS overhead).
     - `s5-min` (`AtomicWriter` pure Go `tmp → fsync → rename` helper; zero git dependency).
     - `s16.6-det` (minimal deterministic checker: diff validator + exit code assert).
     - `EventJournal` (append-only SQLite table).
  2. **Phase 1 — K1 Security Membrane:**
     - `s6.0 PEP` (default-deny `Decide()`), `s6.9 Middleware`, `s6.2 Linux Sandbox` (`__sandbox_helper` + Landlock/seccomp).
     - `s1.2 ProcessTracker` (PID + start-token).
  3. **Phase 2 — L Walking Skeleton (First End-to-End Execution):**
     - `s2 Provider` (APIKey auth only), `s3 TurnLoop` (single turn), `s4 Tools` (`read_file`, `write_file`, `exec_command` via `SandboxedProcessExecutor`).
     - Wire `EffectPath` to `s7-min` and `s6.2`.
  4. **Phase 3 — P0 Vertical Slices (Full Features):**
     - Full S9 (SQLite memory spine + FSRS-lite + Profile isolation).
     - Full S9.6 (ObligationStore with heartbeat evaluation).
     - Full S14.5 (Telegram long-polling daemon + remote HITL challenge).
     - Full S7 (Transactional Outbox for Telegram delivery).

---

## 2. ADVERSARIAL ATTACK FINDINGS (RANKED)

---

### [BREAK] [P0-BLOCKER] Finding 3 — Landlock Sandbox Restricts Dynamic Linkers & Standard Shell Binaries Without System Allowlist
* **Concrete Failure Scenario:**  
  In `DESIGN-S0-sandbox-codex.md:1189-1198`, `linuxHelper` creates a Landlock ruleset restricted to `p.Paths` (the workspace directory), applies `landlock_restrict_self`, installs seccomp BPF, and calls `execveat(executableFD, ...)`.  
  On Linux, executing any standard dynamic binary (e.g. `/bin/sh`, `/bin/ls`, `git`, `python3`) requires the kernel ELF loader to open and read the dynamic linker (`/lib64/ld-linux-x86-64.so.2`), library cache (`/etc/ld.so.cache`), and shared libraries (`/lib/x86_64-linux-gnu/libc.so.6`). Because Landlock applies to the calling thread and all subsequent `execve` calls, and because Landlock is strictly an allowlist (no deny-rules), restricting the ruleset *only* to the workspace directory causes `execveat` of any dynamic binary or shell script to fail immediately with `-EACCES`!
* **Edge Case / Secondary Break:**  
  If the developer attempts a naive fix by allowlisting `/` or `/etc` as read-only, Landlock allows reading `/etc/shadow`, `/etc/gshadow`, and private SSH host keys, violating `PRD.md §6 item 6` ("ne može do `/etc/shadow`"). Furthermore, Landlock ABI v1–v3 cannot attach rules to individual non-directory file descriptors without directory traversal rights.
* **Proposed Concrete Fix:**  
  1. Define a standard immutable **Linux System Read Allowlist** in `sandbox_linux.go`:
     - Explicit directory read grants: `/usr`, `/lib`, `/lib64`, `/bin`, `/sbin`.
     - Explicit file read grants: `/etc/ld.so.cache`, `/etc/alternatives`, `/etc/ssl/certs/ca-certificates.crt`, `/etc/resolv.conf`.
     - Sensitive directory hard-blocks: `/etc` MUST NOT be granted as a recursive directory; only `/etc/ld.so.cache` and CA certs are granted.
  2. Verify in `TestLinuxSandboxAllowsDeclaredReadAndWrite` that dynamic ELF binaries (`/bin/ls`) execute cleanly while attempts to read `/etc/shadow` or `/root` return `EACCES`.

---

### [BREAK] [P0-BLOCKER] Finding 4 — Memory Recall Failure: Unchecked FSRS Decay Erases Permanent Facts (Birthdays, Server IPs, User Traits)
* **Concrete Failure Scenario:**  
  `ARCHITECTURE-ESSENTIALS.md:133` (E13) and `HARNESS-SPEC.md:1571` (P0.13) mandate that P0 memory uses FSRS-lite decay to compute retrievability $R(t) = (1 + t/(9S))^{-1}$, adjusting memory rank over elapsed days $t$.  
  Scenario: The user tells NEXUS in September: *"My server IP is 10.0.0.50 and my SSH key is id_ed25519_prod"*, or *"My daughter's birthday is October 12th"*. Over 60 days, this memory is not recalled. By November, retrievability $R(t)$ decays below the context budget threshold (`S8.1 ContextBudget.HardLimit`). When the user asks in November: *"What is my server IP?"*, the memory retrieval query discards the item as cold/decayed. NEXUS replies: *"I don't know your server IP"*. This directly violates `PRD.md §6 item 2` ("Nešto mu kažem u jednom razgovoru → u sasvim novom razgovoru to i dalje zna").
* **Root Cause:**  
  FSRS is designed for spaced-repetition human flashcard learning (where unreviewed cards fade from human memory), NOT for associative assistant factual knowledge. Factual assertions, user preferences, and configuration must not decay exponentially into oblivion.
* **Proposed Concrete Fix:**  
  1. In `MemoryEntry` schema (`DESIGN-memory-effectpath-kilo.md:57`), introduce `MemoryType uint8`:
     - `MemoryTypeFact` / `MemoryTypeCore` (user traits, credentials, server configs, family dates, explicit preferences): **Decay is permanently disabled** ($S = \infty, R = 1.0$).
     - `MemoryTypeEpisodic` / `MemoryTypeWorking` (temporary notes, daily conversational context): FSRS decay applies normally.
  2. `MemoryStore.Recall` must always include active `MemoryTypeFact` entries matching the profile scope before applying FSRS decay ranking to episodic items.

---

### [BREAK] [P0-BLOCKER] Finding 5 — In-Process TurnLoop Cannot Survive Daemon Restart or Long Delays During Telegram HITL Approval
* **Concrete Failure Scenario:**  
  In `PRD.md §4.4`, `PRD §6.4`, and `ARCHITECTURE-ESSENTIALS.md:146` (E15), when an irreversible action occurs, NEXUS suspends execution and sends a Telegram approval challenge to the user's mobile device (`ApprovalChallenge` expiring in e.g. 15–30 minutes).  
  In `HARNESS-PLAN.md:250-265` and `DESIGN-FIXES-r2.md:49-75`, the TurnLoop is an in-process Go function (`for turn := 0; turn < maxTurns; turn++`). If the user takes 10 minutes to tap "Approve" on their phone, and during that window the user restarts the laptop, closes the lid, or `nexus` is updated:
  - The Go process exits, terminating the in-memory goroutine and turn loop state.
  - When `nexus` restarts, the user taps "Approve" on Telegram.
  - The Telegram gateway receives the valid `ApprovalChallenge` token.
  - **The token is valid, but there is NO running TurnLoop goroutine to receive it!** The tool execution cannot resume, the turn is lost, and the assistant remains stuck in limbo without completing the action.
* **Root Cause:**  
  The TurnLoop was designed as an ephemeral synchronous execution loop rather than a durable state-machine that can hibernate and rehydrate from `EventJournal` offsets.
* **Proposed Concrete Fix:**  
  1. When a tool returns `DecisionAsk` (`needsApproval`), the TurnLoop MUST NOT block in an in-memory channel. It must commit a `TurnSuspendedEvent{RunID, ChallengeID, ToolCall, StateSnapshotOffset}` to the `EventJournal` and cleanly exit the turn.
  2. When Telegram receives a valid `ApprovalChallenge` signature, `Gateway` appends `ApprovalReceivedEvent{ChallengeID, Decision}` to the journal and triggers `TurnLoop.Resume(ctx, RunID)`.
  3. `TurnLoop.Resume` rehydrates the execution context directly from the journal snapshot and dispatches the approved `ToolCall` with `AttemptGrant`.

---

### [EDGE] [P0-RISK] Finding 6 — Laptop Suspend/Shutdown Breaks Timers: Obligation Store Lacks Startup/Wake Catch-Up Policy
* **Concrete Failure Scenario:**  
  `PRD.md §6 item 3` states: *"Podsjetnik preživi gašenje aplikacije i javi se na vrijeme"*.  
  Scenario: At 23:00 on a Linux laptop, the user says: *"Remind me at 07:00 tomorrow to submit the report"*. The obligation is stored with `DueAt = 07:00`. At 23:30, the user closes the laptop lid (Linux `systemd-suspend` / deep sleep). At 07:00, the laptop is asleep. The user opens the laptop at 07:45.  
  Standard Go `time.Ticker` and `time.After` freeze during system suspend. When the laptop wakes at 07:45, a ticker fires only for the current interval. If the scheduler evaluates obligations using exact matches (`DueAt == now`) or if the daemon was killed during poweroff and restarted at 07:45, the 07:00 reminder is skipped or marked as expired/stale!
* **Proposed Concrete Fix:**  
  1. In `ObligationStore` (`GAPFIX-kilo.md:121`), define `EvaluateDue(ctx)` to query all active obligations where `DueAt <= time.Now() AND State == PENDING` (range query, not point-in-time).
  2. Implement an explicit **Daemon Startup & Wake Catch-Up Sweep**: On process startup and on Linux D-Bus `PrepareForSleep(false)` wake-up signals, immediately run `EvaluateDue(ctx)` and fire overdue notifications with an explicit `[MISSED/OVERDUE by X minutes]` notice to Telegram/TUI.

---

### [EDGE] [P0-RISK] Finding 7 — Single-Database Profile Isolation Leaks Private Data via FTS5 Indexing & SQLite WAL Freelist Residuals
* **Concrete Failure Scenario:**  
  `PRD.md §6 item 5` sets a strict done-criterion: *"Zatražim akciju u 'poslu' → u 'privatnom' načinu se ništa od toga ne vidi (nula curenja)"*.  
  `ARCHITECTURE-ESSENTIALS.md:137` (E14) mandates `PersonalProfile = deny-default isolation of memory/secrets/channels/cache`.  
  If "work" and "private" profiles share a single SQLite database file (`nexus.db`) using row-level `profile_id` filtering:
  - **FTS5 Inverted Index Leak:** Full-Text Search virtual tables (`fts5`) build shared ngram/token vocabularies. Statistical token frequency queries or malformed FTS prefix searches can leak the existence of private terms in work mode.
  - **WAL Freelist Plaintext Residuals:** If a user executes `DATA_PURGE` (P2.1) on private memory, SQLite marks pages as free in the WAL/freelist without zeroing memory. A work task running `read_file` on `nexus.db` or inspecting DB fragments can recover raw private text, failing `test_purge_cannot_complete_with_residual_copy`.
* **Proposed Concrete Fix:**  
  1. Enforce **Physical SQLite Database Separation**:
     - `~/.nexus/profiles/personal/nexus.db`
     - `~/.nexus/profiles/work/nexus.db`
  2. The kernel process opens the active profile's database connection dynamically based on the authenticated profile session. Cross-profile queries are physically impossible at the OS file descriptor level.
  3. Shared global events (e.g. system upgrades, kernel errors) reside in a separate `~/.nexus/system.db` containing zero profile payloads.

---

### [EDGE] [P0-RISK] Finding 8 — Read-Your-Own-Writes TOCTOU: Synchronous vs Asynchronous Projection Updates in EventJournal
* **Concrete Failure Scenario:**  
  `ARCHITECTURE-ESSENTIALS.md:41-44` (E4) states: `EventJournal.Append is the ONLY writer... state/log/trace/transcript/audit/metrics are PROJECTIONS (fold)... ObligationStore writes THROUGH the journal`.  
  Scenario: TurnLoop executes `ObligationStore.MarkDone(obl, evidence)`. This calls `EventJournal.Append(ObligationCompletedEvent)`. Immediately in the next turn step, the prompt assembler queries `ObligationStore.ListActive()`.  
  If projections are updated asynchronously (via a background goroutine or channel subscription), the prompt assembler reads the old state before the projection has folded the completion event! The model sees the obligation as still pending, generates a duplicate tool call, and violates determinism.
* **Proposed Concrete Fix:**  
  1. Specify in `EventJournal` architecture: **Core State Projections (Sessions, Obligations, Memory Index) are SYNCHRONOUS and transactional**. `EventJournal.Append` must execute the event write and the SQLite projection update within the *same atomic SQLite transaction*.
  2. Only non-critical observability projections (metrics, trace export, telemetry) are processed asynchronously via best-effort worker queues.

---

### [EDGE] [P0-RISK] Finding 9 — SQLite File Locking (`SQLITE_BUSY`) Between Background Telegram Daemon and Interactive CLI REPL
* **Concrete Failure Scenario:**  
  In a Linux personal assistant deployment, NEXUS runs as a background `systemd --user` daemon (handling Telegram long-polling and scheduled obligation heartbeats). Simultaneously, the user opens a terminal and runs `nexus chat` (interactive CLI REPL).  
  Both processes open `~/.nexus/nexus.db` using `modernc.org/sqlite` (pure Go SQLite).  
  When the background daemon is performing a multi-statement transaction (e.g. writing memory compaction or fetching Telegram updates) and the user in the terminal sends a prompt, the CLI process attempts an immediate write. Because `modernc.org/sqlite` uses Go-level file locking without CGo's native thread-yielding busy handlers, write contention frequently triggers `SQLITE_BUSY: database is locked`, causing CLI command crashes or dropped Telegram updates.
* **Proposed Concrete Fix:**  
  1. Configure all SQLite database connections with WAL mode (`PRAGMA journal_mode=WAL;`), synchronous normal (`PRAGMA synchronous=NORMAL;`), and a mandatory busy timeout (`PRAGMA busy_timeout=5000;`).
  2. In the architecture, adopt the **Daemon-Client Local Socket Pattern**: When `nexus daemon` is running, the CLI REPL connects to the daemon via a local Unix Domain Socket (`~/.nexus/nexus.sock`). The daemon remains the single-process database owner, eliminating all cross-process SQLite file lock contention.

---

### [EDGE] [P0-RISK] Finding 10 — Stuck-Detection Kills Legitimate Repetitive Obligations and Poll Loops
* **Concrete Failure Scenario:**  
  `HARNESS-PLAN.md:275-285` (S3.4 Stuck Detection) monitors argument hashes and progress signals, aborting turns with `STUCK_LOOP_DETECTED` if identical tool calls repeat.  
  Scenario: The user creates an obligation: *"Check every 2 minutes if the server at 192.168.1.100 is responding to ping, and notify me when it's up"*.  
  Every 2 minutes, `ObligationStore` evaluates the goal and runs `exec_command("ping -c 1 192.168.1.100")`. The command returns exit code 1 (host unreachable) with identical stdout/stderr.  
  After 3 iterations, S3.4 triggers a circuit breaker because tool name, argument hash, and error output are 100% identical across attempts. The obligation evaluation is aborted as a "stuck loop", and the user never gets notified when the server comes online!
* **Proposed Concrete Fix:**  
  1. Scope S3.4 Stuck-Detection strictly to **within a single interactive LLM turn** (detecting model generation loops).
  2. Scheduled obligations, heartbeats, and periodic polling tasks must carry an explicit `s3.PollingGrant` or `LoopPolicyContinuous` that exempts scheduled iterations from identical-argument breaker thresholds.

---

### [EDGE] [P0-RISK] Finding 11 — Exact-Intent Approval Token Invalidation from Harmless Time/Context Drift
* **Concrete Failure Scenario:**  
  `ARCHITECTURE-ESSENTIALS.md:89-90` (E9) and `HARNESS-SPEC.md:1463` (P1.6) require that HITL approval tokens bind an `exact-intent` hash: `hash(tool_name, arguments, timestamp, context_digest)`.  
  Scenario: NEXUS wants to clean a build directory and prepares `rm -rf /home/matej/project/build_tmp_12345`. It computes the exact-intent hash including a dynamic timestamp or working directory revision hash and sends an approval request to Telegram.  
  The user views the notification and taps "Approve" 15 minutes later.  
  In the meantime, the user edited an unrelated file in `/home/matej/project/README.md`. When NEXUS attempts to execute the tool upon receiving approval, it re-verifies the intent context. Because the repository revision hash or execution timestamp changed, the intent hash check fails (`APPROVAL_INTENT_HASH_MISMATCH`), and the operation is rejected. The user is forced to re-approve multiple times for simple delayed actions.
* **Proposed Concrete Fix:**  
  1. Explicitly standardize the canonical fields of `ExactIntentPayload`:
     - Bind ONLY: `CanonicalToolName`, `CanonicalJSONArguments`, `TargetResourcePath`, `ProfileID`.
     - DO NOT bind: transient timestamps, parent turn indices, or broad repository-wide git commit hashes.
  2. For destructive filesystem tools, bind the target directory's `(device, inode)` at draft time to prevent symlink-swap attacks without breaking on unrelated file edits.

---

### [OVERENG] [P0-RISK] Finding 12 — 18-Flag Transactional Capability Matrix & Rollback Engine in a Monolithic P0 CLI
* **Concrete Failure Scenario / Cost with No Payer:**  
  `ARCHITECTURE-ESSENTIALS.md:69-76` (E7) and `DESIGN-S0-sandbox-codex.md` mandate a full 3-step transactional activation engine: `ActivationPlan(immutable + PlanHash) -> Prepare -> Commit -> Activate + RollbackToken + PREPARING/FAILED/ROLLING_BACK`, accompanied by `test_every_flag_fails_when_one_gate_is_ablated` over 18 capability flags.  
  In P0, 12 of the 18 flags (`multi-agent`, `fleet`, `pairing`, `service`, `extensions`, `video`, `voice`, `computer-use`, `cag`, `graph-retrieval`, etc.) are explicitly CUT. The P0 binary contains only 6 hardcoded core capabilities.  
  Building a generic multi-flag transactional coordinator with two-phase commit rollback tokens for a single static binary built by one developer imposes an immense initial development cost (~2 weeks of pure scaffolding) with zero runtime benefit for P0, delaying the first working end-to-end conversation.
* **Proposed Concrete Fix:**  
  1. For P0, implement a **Static Capability Profile Validator**: On startup, inspect the active profile, evaluate the single Linux Sandbox probe, verify SQLite accessibility, and produce a sealed `SealedCapabilitySet`.
  2. Defer generic dynamic multi-flag `ActivationPlan` rollback coordinators to P2/P6 when distributed extensions and remote fleet nodes are introduced.

---

### [OVERENG] [OVERENG] Finding 13 — Enterprise Multi-Tenant & Multi-Agent Infrastructure in a Single-User Assistant
* **Concrete Cost with No Payer:**  
  The locked specifications contain extensive designs for enterprise distributed systems:
  - `S18.2 Tenant Gateway` & `S18.3 Argo Rollout Canary` (`HARNESS-PLAN.md:765-770`).
  - `S18.5 Fleet Node Placement` & `S18.6 Device Pairing Mesh` (`GAPFIX-codex.md:860-920`).
  - `S12.6 Multi-Agent Council Synthesis` & `S12.7 Flow Boards`.
  - `S17.1 Cordis Dynamic WASM/gRPC Plugin ABI`.  
  NEXUS is explicitly defined in `PRD.md §2` as *"Sam korisnik — osobna upotreba, jedan čovjek (ne tim, ne SaaS)"* running on a single Linux machine.  
  Carrying hundreds of lines of placement grant types, node mesh routing, tenant isolation policies, and canary deployment rollouts adds maintenance drag and circular import risks (such as the K3 fleet/pairing cycle in `DESIGN-FIXES-r2:97-111`) for features that will never execute.
* **Proposed Concrete Fix:**  
  1. Strictly quarantine all S12 multi-agent, S17 dynamic plugin, and S18 fleet/tenant structs into dedicated submodules (`internal/experimental/...` or `v2/`) completely disconnected from the P0 core build path.
  2. Do not import or reference `FleetGrant`, `PlacementGrant`, or `TenantID` in core `internal/contracts`.

---

### [OVERENG] [OVERENG] Finding 14 — Cryptographic Blockchain Hash-Chaining for Local Single-User Debug/Audit Logs
* **Concrete Cost with No Payer:**  
  `ARCHITECTURE-ESSENTIALS.md:110-113` (E11) and `HARNESS-PLAN.md:680-689` (S15.3) specify an append-only audit log with cryptographic hash-chaining (`Event[i].PrevHash = SHA256(Event[i-1])`) anchored by an external Ed25519 signing key or timestamping service to prevent tampering.  
  In a single-user application where the user runs NEXUS on their local Linux laptop:
  - The signing key and SQLite database are both stored in `~/.nexus/` owned by the user.
  - If the user or a malicious process gains code execution on the user's account, they can read the private key, alter the SQLite DB, and re-calculate the entire hash chain trivially.
  - An internal hash chain provides zero cryptographic security against a local attacker with file access, while adding serialization overhead and latency to every journal write.
* **Proposed Concrete Fix:**  
  1. In P0/P1, rely on standard SQLite WAL append-only transactions and OS file permissions (`0600` on `~/.nexus/`).
  2. Retain HMAC verification for executable artifacts and external webhook tokens (`ExecutionReceipt`), but drop mandatory recursive blockchain hash-chaining for local debug telemetry.

---

### [BREAK] [P1+] Finding 15 — AST-Symedit Language Fallback and LSP Transpiler Assumptions for Non-Go Repositories
* **Concrete Failure Scenario:**  
  `DESIGN-symedit-crypto-claude.md:32-35` resolves AST symbol editing for Go using `go/types` (exact), while specifying generic "LSP-adapter" for all other languages, with a strict invariant: *"refuse-on-ambiguous/collision/build-broken, NIKAD text-fallback"*.  
  Scenario in P1 coding mode: The user asks NEXUS to rename a Python, Rust, or TypeScript symbol in a project that has an incomplete `node_modules`, missing `Cargo.lock`, or syntax error in an unrelated file.  
  Standard language servers (e.g. `gopls`, `pyright`, `rust-analyzer`) refuse symbol rename requests or return partial edits when the project build is broken. Because the design strictly forbids fallback to structured text editing (`refuse-on-build-broken, NIKAD text-fallback`), NEXUS completely refuses to edit the file, leaving the user unable to fix broken builds using the assistant!
* **Proposed Concrete Fix:**  
  1. Define a strict **Two-Tier Edit Ladder with Provenance Attribution**:
     - Tier 1: AST/LSP Exact Symedit (when language server is active and build is clean).
     - Tier 2: Aider-style Search/Replace Block Diff (with preimage hash verification and `refuse_if_count > 1`) when LSP is unavailable or build is in a broken state.
  2. Never leave the assistant paralyzed on a syntax-error repository; use verified preimage diff replacement as the fallback.

---

## 3. CONSOLIDATED ACTION PLAN FOR P0 IMPLEMENTATION

| Priority | Action | Target Component |
|---|---|---|
| **P0-1** | Amend `P1.6` closure to distinguish `channel:builtin` (P0 Telegram) from `channel:plugin`. | `S0.5` / `HARNESS-SPEC Annex A` |
| **P0-2** | Adopt 3-tier `-min` contract build order for the P0 walking skeleton. | `SECTION-MAP §1` / `tasks-P0.md` |
| **P0-3** | Add standard system read allowlist (`/usr`, `/lib64`, `/etc/ld.so.cache`) to Landlock helper. | `internal/sandbox/sandbox_linux.go` |
| **P0-4** | Split memory into `MemoryTypeFact` (no decay) vs `MemoryTypeEpisodic` (FSRS decay). | `internal/memory/store.go` |
| **P0-5** | Implement TurnLoop hibernation/resume via `EventJournal` for Telegram HITL challenges. | `internal/core/loop.go` / `internal/channels` |
| **P0-6** | Implement startup & wake-up catch-up sweeps for `ObligationStore`. | `internal/obligations/store.go` |
| **P0-7** | Physically isolate profiles into separate SQLite database files (`personal` vs `work`). | `internal/storage/sqlite.go` |
| **P0-8** | Enforce synchronous transactional projection updates in `EventJournal.Append`. | `internal/journal/journal.go` |

---

SUMMARY: 5 P0-BLOCKER, 7 P0-RISK.
