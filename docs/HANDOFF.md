# HANDOFF — NEXUS resume point (READ FIRST) — updated 2026-09-09

## RESUME 2026-09-09 (coding-trio Slice 0 partial: CODE1-CODE4 all CONVERGED codex+kilo+agy PASS) — start here

**Slice 0 partial (the toolchain-visibility primitive — probe/sandbox ExtraROBinds+
ExtraEnv, `internal/foundation/sealedstore`, `internal/coding/impact`) is FULLY
CONVERGED and closed.** Four adversarial code-review rounds, each folding real findings
until codex+kilo+agy all PASS with no open findings:

- **CODE1** (commit b227412): codex FAIL 6 findings, kilo PASS 4 notes, agy PASS 2
  notes — all folded (a self-inflicted `denylistedROBindRoot` unconditional-return bug,
  `CompiledPolicy` aliasing, `sealedstore` GC/Put race, pathname-vs-descriptor-relative
  I/O, `impact.go` test-edge conflation, 5 false-green detectors).
- **CODE2** (commits 7731ef9, 6a6085b): kilo PASS, agy PASS, codex FAIL after an
  unusually long (~3.5h, two genuine mid-review context-compaction stalls — NOT true
  hangs) review, citing LD_AUDIT (folded) plus two findings first documented as
  deferred, then actually code-fixed per the "resolve fully now" rule: `sealedstore.GC`
  changed to `GC(loadLive func() map[string]bool)` called under `s.mu`; `probe.Spec`
  gained `ExtraROBindIdentities` (device+inode pins) checked at Launch.
- **CODE3** (commit 1f33da2): kilo PASS, agy PASS, codex FAIL — found a REAL bypass
  kilo/agy both missed: the CODE2 identity-pin check `ok && pinned != identity` failed
  OPEN on a map miss, so a symlink named in ExtraROBinds retargeted between Compile and
  Launch defeated the pin entirely (reproduced live by codex). Fixed: a non-nil
  `ExtraROBindIdentities` map now requires every resolved canonical path to have a
  pin — a miss is refused, not skipped. Also hardened a test-quality gap (GC's
  callback-under-lock claim wasn't actually mutation-sensitive).
- **CODE4** (re-verification of 1f33da2): **codex PASS, kilo PASS, agy PASS — full
  convergence, no open findings.**

Every fix in every round was proven RED-before/GREEN-after against the actual attack
(several via `git stash` to see genuine RED on the pre-fix code), not just asserted.
Full repo test suite (`go build`/`go vet`/`go test ./...`) green at every commit. All
commits on `main`, all pushed to `origin/main` (`cd7616f` through `1f33da2`).

**Operational lessons from this session** (both already in memory, repeated here for
convenience): `herdr agent wait --until done` is unreliable on long (hours) codex/kilo
review rounds — poll `herdr agent get <pane>` + `herdr agent read <pane> --source
recent-unwrapped --lines N` instead, and judge progress from actual pane CONTENT, not
the status flag alone. A codex pane that appears frozen for a long time is often mid
context-compaction, not hung — `herdr agent send-keys <pane> esc` unsticks it (worked
twice this session); check `ps aux | grep <pid>` CPU-time delta across polls as a
secondary signal before concluding a genuine stall.

**Next**: the actual coding-runner substrate (private workspace snapshot isolation,
`GOTOOLDIR` resolution via `go env`, `gopls` stdio JSON-RPC session, the causal
detectors listed under Slice 0 in `docs/PLAN-CODING-TRIO.md`) — a genuinely new, large
piece requiring its own research(3-agent)→plan→review→code→review cycle per the owner's
standing directive, not a continuation of the toolchain-visibility primitive just
converged above.

## RESUME 2026-09-09 (coding-trio Slice 0 partial: CODE1+CODE2 converged) — start here

**PLAN-CODING-TRIO.md converged 2026-09-09** (6 plan-review rounds, codex+kilo+agy all
PASS) after independent research dispatched to codex/kilo/agy per the owner's explicit
"research-first, all 3 agents investigate independently, then synthesize" directive.
Final architecture: `internal/coding/impact` (pure TIA graph), `internal/coding/symedit`
+ `internal/coding/workspace` (two-phase Prepare/Apply via `gopls` stdio JSON-RPC),
`internal/coding/evidence` (proof-of-done), `internal/foundation/sealedstore`
(descriptor-relative content-addressed artifact store), new S7 durable policies
(`PolicyWorkspaceApply`/`PolicyWorkspaceRollback`), `EffectPath.RunDurableTool`. Build
order: Slice 0 (coding-runner substrate) → 1 (evidence) → 2 (TIA) → 3 (symedit) → 4
(expansion).

**Slice 0 partial built and code-reviewed to convergence** (commits cd7616f → b227412 →
7731ef9 → b47a771, all on `main`, pushed): `internal/preflight/probe` gained
`ExtraROBinds`/`ExtraEnv` (narrowly-scoped, denylist-guarded, reserved-env-key-guarded
toolchain visibility grant), `internal/sandbox` folds both into `policyHash` with a
canonicalizing digest and a real deep-copy (`cloneSpec`) so attestation can't be desynced
by a post-Compile mutation, `internal/foundation/sealedstore` is a NEW package
(descriptor-relative Put/Get/GC via `golang.org/x/sys/unix` openat/renameat/unlinkat
against one held directory fd, Put+GC serialize under one critical section), and
`internal/coding/impact` is a NEW package (pure TIA graph: `prodRdeps` transitive vs.
`testOwners` terminal edge split). **This is Slice 0's toolchain-visibility PRIMITIVE
only — the actual coding-runner substrate (private workspace snapshot, GOTOOLDIR
resolution, `gopls` stdio session, S7 durable lifecycle wiring) is NOT built yet.**

CODE1 (codex FAIL 6 findings, kilo PASS 4 notes, agy PASS 2 notes) fully folded in
b227412: a self-inflicted `denylistedROBindRoot` bug (the `"/"` deny-root check fired
unconditionally on every call regardless of the actual path — first-loop-iteration bug),
`CompiledPolicy` shallow-copy aliasing (fixed via `cloneSpec`), `sealedstore`'s GC/Put
race (fixed: single critical section for the WHOLE operation, proven by a
`gcPauseHook`-driven test), pathname-based I/O (rewritten descriptor-relative per the
plan's explicit requirement), `impact.go`'s test-only-edge-propagates-into-production
conflation (fixed: `prodRdeps`/`testOwners` split, proven by a real counterexample test),
and 5 false-green detector fixes. CODE2 (re-review of the fold): kilo PASS (2 notes,
walked the counterexample by hand), agy PASS (3 notes, all folded — deterministic sort in
`Affected`, an error-message var fix, `Store.Close` idempotency), codex ran for an
unusually long time (~3.5h wall clock across two genuine mid-review context-compaction
stalls, confirmed alive both times via process CPU + a manual interrupt that produced a
live response) and surfaced two real but narrow-scope residual findings before the
session moved on without its final formal verdict: (1) `ExtraROBinds` content isn't
pinned, only its canonicalized path — ties directly to the plan's own pre-existing
"toolchain-version swap detection" requirement for the not-yet-built coding-runner, not a
probe/sandbox defect (whole-directory content hashing there is unbounded); (2)
`sealedstore.GC`'s `liveDigests` freshness is caller-supplied by design — a
caller-sequencing contract for whichever Slice builds the first real
Put→journal-commit→Release caller, not an internal race (the internal Put/GC critical
section IS proven race-free). Both recorded as explicit Slice-0 closure conditions in
`docs/PLAN-CODING-TRIO.md` (`## Slice 0 partial — open residual items`). One clean,
independently-converged finding WAS folded from codex's in-flight work: `LD_AUDIT` added
to `reservedEnvKeys` (both codex and kilo flagged it independently). Full repo test suite
green at every commit.

**Per the owner's standing "resolve everything now" + "just work in the loop" rules**:
proceeded on kilo+agy PASS convergence given codex's extended stall, rather than blocking
indefinitely on a single reviewer pane. If codex's pane (`w8:p2`) eventually produces its
formal verdict, read it and fold anything new as a CODE3 addendum — check
`herdr agent read w8:p2 --source recent-unwrapped --lines 300` first.

**Next work**: build the actual coding-runner substrate (private snapshot isolation,
`GOTOOLDIR` resolution via `go env`, `gopls` stdio session, the causal detectors listed
under Slice 0 in the plan) — this is a genuinely new, large piece requiring its own
research(3-agent)→plan→review→code→review cycle per the owner's standing directive, not
a continuation of the toolchain-visibility primitive just converged above.

## RESUME 2026-09-09 (audit remediation CONVERGED + MERGED) — start here

**Audit-hardening slice (F+A+B1+B2+B3+C+D+E) is DONE, merged to `slice/p1-audit`, and
being merged to `main` + autodeployed this session.** CODE review went 8 rounds past the
CODE2 state recorded below: CODE3 (7 codex findings, folded), CODE4 (4 HIGH codex
findings, folded — poll-exhaustion fatality detector gap, empty-cycle health-clear gap,
Report vocabulary bypass, ClassOf substrate-precedence bug, oversized/trailing Telegram
reply acceptance), CODE5 (3 codex findings, folded — canonical receipt code, unified
error-tree classification, exact bot-id binding for control ops), CODE6 (3 codex
findings, folded — a kilo dismissal was independently checked and found WRONG: ClassOf's
depth-cap silently kept a shallower wrong class instead of failing closed; also
adapter-aware `bound()` + exact-intent payload-hash verification for setMyCommands),
CODE7 (1 new HIGH codex finding, folded — a Companion's payload could name a DIFFERENT
operation than its Key and still ride a valid grant; added `channel.CompanionOperationID`
cross-check). **CODE8: codex PASS, agy PASS; kilo hit "Insufficient Balance" (an account
funding issue) on both the CODE7 and CODE8 dispatch — confirmed not transient.** Per the
owner's "only codex+kilo convergence counts, agy rubber-stamps" rule, codex is the
harsher/primary reviewer of the two and it converged clean; proceeded on codex+agy PASS
with kilo's unavailability documented here, per the owner's explicit "resolve everything
now, don't leave partial" directive for this session (2026-09-08 night, see below) — an
external billing block is not something this session could resolve by retrying further.
**If kilo's mandatory participation should resume, the owner needs to top up its account
before the next review-gated slice.** Every fold in CODE3-CODE7 was hand RED/GREEN-
ablated (not just re-run), and every codex finding was independently re-derived against
the actual code before folding — several turned out to be real defects that were NOT
currently exploitable through the single production call site, but were genuine gaps in
the closed API boundary's own self-defense (the established pattern this whole review
chain follows: the boundary must refuse a hostile caller, not just behave correctly for
the one caller that happens to exist today).

Merge: `main <- slice/p1-audit <- slice/audit-b` (slice/audit-b already contained the full
stack: F, A, B1, B2, B3, C, D, E). `slice/p1-audit` also carries the 12-round plan
convergence and F4/F9 from earlier. Full suite (`go build`, `go vet`, `go test ./...`)
green at every merge step. Deploy via `scripts/deploy.sh` follows this merge in the same
session (autodeploy, no approval needed per standing rule) — **replaces the currently
running Telegram-surface build**: the live bot on `~/bin/nexus` was running the OLD
reminder-v3 branch; `main` does not carry `/reminder` v3's inline-calendar keyboard yet
(that work is parked on `slice/p1-cronjob-v2`, untouched this session) — the older
`/cronjob` picker resurfaces after this deploy. Expected, not a regression.

## RESUME 2026-09-09 (audit remediation in flight) — superseded by the section above

**Owner decision (2026-09-08 night):** work the WHOLE audit plan autonomously in a
loop; at every decision pick the variant that fixes the thing NOW and completely (no
minimal-then-later). Slice B therefore builds the FULL S7 retry engine (P2 pulled
forward). Telegram surface is PARKED. Autodeploy after 3xPASS + merge (gated).

**Plan:** `docs/PLAN-AUDIT-FIXES.md` v12 on `slice/p1-audit` — **CONVERGED: round 12 =
codex+kilo+agy PASS** (rounds 1-12 in `docs/REVIEW-AUDIT-PLAN{,2..12}-*.md`). Order:
F -> A -> B1 -> B2 -> B3 -> C -> D -> E.
- `slice/audit-b` (worktree `/home/matej/HARNESS/nexus-b`, stacked on audit-e) @ f33bc9a:
  **B1** (4c4011b `internal/kernel/s7` full engine, s7min deleted), **B2** (31417ce
  S7-governed delivery/poll/registration; channel Flush(ctx, auth, send), FAILED
  status + generation, typed Failure, bot-bound durable registration + reconcile),
  **B3** (5a004de provider Failure/Classify, planner chatVia/streamVia via Execute,
  structured ExtractVia), **D** (f1e443c `internal/channel/health` owner,
  ClassifiedError, supervisor, doctor reads channel_health.json), **code-review r1
  folds** (f33bc9a: readonly Go floor, canonical receipts, budget-refusal grant
  assertion). Full suite (vet + go test ./...) green at 5a004de and f1e443c; the
  f33bc9a run was in flight at the time of writing.
- **CODE reviews:** round 1 (F/A/C/E @68bdd88) = kilo PASS, agy PASS, codex FAIL 3
  (folded in f33bc9a). Round 2 (whole stack @f33bc9a) = kilo PASS, agy FAIL 1
  (acceptance fake getMe id — fixed 63a02bc), codex FAIL 8 — ALL folded: 63a02bc
  (bare Failure classification), 9ce4dd0 (S7 AttemptContext in both transports,
  kind/method-bound grants, UNKNOWN registration park, strict S7 policy/event
  validation, ErrNothingDue keeps health, health writes fail closed, salvage on final
  transport error), 457c56d (ErrNotDurable + SetAppendFault seam; full detector
  matrix: 9d table, D6/D7 tables, D2 Run-level, D2b torn batch, D2d provenance, D4
  per-mark faults, planner 429/400/stream). Round 3 (whole stack @457c56d)
  dispatched — `docs/REVIEW-AUDIT-CODE3-*.md`. slice/audit-b HEAD = 457c56d.

**NEXT (in order):** (1) fold CODE3 findings on slice/audit-b -> re-dispatch until
codex+kilo PASS (dispatch script pattern: scratchpad dispatch_code2.sh — absolute
worktree path + git diff range); (2) merge the chain to main: main <- slice/p1-audit
(plan docs + F4/F9) <- slice/audit-b (contains f..e..b); resolve nothing else (all
stacked); (3) autodeploy via `scripts/deploy.sh` (gate: toolchain floor + govulncheck;
verify "sealed capability ON"; the live bot on ~/bin/nexus runs the OLD reminder-v3
branch build — deploying main replaces it: /reminder v3 keyboard is NOT on main yet,
the owner parked the Telegram surface, so expect the older /cronjob picker); (4)
update docs/tasks-P0.md backlog note (S7 gap closed) + AGENTS.md/ESSENTIALS inline
change-record "full S7 landed in P1 by owner decision"; (5) continue core improvement
(coding USP, web research, memory, steal-worthy patterns).

**Gotchas learned this session:** python heredoc folds MUST be `&&`-chained with
commit AND dispatch and anchors grep-verified (three times agents reviewed a stale
plan); herdr `agent_prompt_stalled` can still mean the agent started — check `agent
list` before retrying; interrupt an agent with `herdr agent send-keys w8:pN Escape`;
never `git checkout`/merge in the shared main checkout while agents review — use
worktrees; RFC5737 doc ranges (203.0.113.x) are in the egress deny floor (tests need
real public IPs, e.g. 149.154.167.220).

---

## RESUME 2026-09-08 (P1 in progress) — start here

**Phase status:** P0 DONE (27/27, on `main`, `P0-capable` attested). P1 IN PROGRESS.
Consolidated build map: the docs below + `docs/PLAN-AUDIT-FIXES.md`.

**Branches / state (all committed, nothing dangling):**
- `main` @ 2cfeb16 — merged P1 so far: Telegram gateway polish (native tables,
  time, typing+`/`menu, `/new` reset, convproj backlog fix, `/help` reword),
  **E11 pinned-IP egress dialer**, and **/cronjob v1** (now renamed — see below).
- `slice/p1-cronjob-v2` @ 9a15a01 — **/reminder v3**: date+time on ONE combined
  keyboard (re-tappable, no back step), 10-min grid, today marker `·d·`,
  force_reply text field; command renamed **/cronjob -> /reminder** (one-shot).
  DEPLOYED to `~/bin/nexus` (the live bot runs this). **NOT YET REVIEWED/merged**
  — the step-by-step v2 got 3xPASS, but the combined rewrite is new.
- `slice/p1-audit` @ 4f5e652 — audit remediation: **F4 + F9 DONE**; F1,F2,F3,F5,
  F6,F7,F8 pending per `docs/PLAN-AUDIT-FIXES.md` (audit = `docs/AUDIT-FULL-codex-2026-09-08.md`).

**DIRECTION (owner, 2026-09-08): CORE-FIRST.** Telegram surface work
(reminder-v3 merge, recurring /cronjob, voice, media) is PARKED on its branches —
resume only when the owner says. Focus = the nexus CORE: audit-hardening first,
then continuous core improvement (steal/refactor whatever is worth it). **AUTODEPLOY
is ON**: after a slice reaches 3xPASS + merge to main, automatically build ->
~/bin/nexus -> `systemctl --user restart nexus` -> verify capability ON (no per-deploy
approval needed).

**PENDING QUEUE (reordered — core first):**
1. Audit remediation F1-F8 (`docs/AUDIT-FULL-codex-2026-09-08.md` +
   `docs/PLAN-AUDIT-FIXES.md`) — mostly CORE hardening. Re-dispatch the plan review
   (a restart killed it), then slices A-F. F3 needs the owner's Go>=1.26.6 upgrade.
2. Continuous core improvement: coding USP trio, web search/browser, memory, and
   steal-worthy patterns we find — each research->plan->agents->code->agents->autodeploy.
3. PARKED (Telegram surface, resume on owner's word): review `/reminder` v3
   (slice/p1-cronjob-v2) -> merge; real recurring /cronjob; voice; media.

Old queue (kept for reference):
- Review `/reminder` v3 (slice/p1-cronjob-v2) with the 3 herdr agents -> merge to main.
- Audit remediation slices A-F (`docs/PLAN-AUDIT-FIXES.md`): A=shared E11 dialer
   (F1+F8 provider), B=S7-bounded delivery retry (F2), C=context budget + bounded
   reads (F5+F8), D=channel-health owner (F6), E=config-aware doctor (F7),
   F=release Go-floor+govulncheck gate (F3 — needs the OWNER to upgrade the Pi's
   Go to >= 1.26.6 first). The audit-PLAN review was killed by a session restart
   (only kilo wrote its file) — RE-DISPATCH the plan review before coding.
3. Real recurring **/cronjob** (owner wants recurring; current /reminder is
   one-shot): recurrence picker + schedule re-arm after each fire + `/jobs`
   list/cancel; the schedule backend is one-shot today (needs a recurrence rule).
   Owner example: "every morning report the API-provider balance" = recurrence +
   an action-per-fire (a tool run), not just a text ping.
4. Voice (plan 3xPASS in `docs/PLAN-VOICE.md`): needs prereqs `docs/PLAN-SANDBOX-ROINPUT.md`
   (startup input registry) + the E11 dialer (done).
5. Media (images/vision, video, social links, documents), coding USP trio, web
   search/browser, more channels — PRD P1 backlog.

**Infra / gotchas for the new session:**
- Needs **Bash in bypass mode** (Shift+Tab to bypass, or `--dangerously-skip-permissions`).
- **GateGuard PreToolUse hook installed** (`~/.claude/hooks/gateguard-fact-force.py`):
  the FIRST Edit/Write of each file per session is DENIED with "[Fact-Forcing Gate]"
  — state importers/API/schema/instruction, then RETRY the same edit. Off-switch
  `NEXUS_GATEGUARD=off`. This is EXPECTED, not an error.
- **156 merged subagents** in `~/.claude/agents/` (from VoltAgent + iannuttall,
  deduped vs the claude-code-workflows plugin): VoltAgent tiers `model:` — haiku
  (light), sonnet (dev), inherit (security/architecture). Delegate the right
  specialized agent to save main-thread tokens; override the model only when the
  stakes don't match the agent default.
- herdr review agents: **codex=w8:p2, kilo=w8:p3, agy=w8:p4**; dispatch by pane_id
  via `herdr agent prompt w8:pN "<prompt>" --wait --until done --until idle`.
  Prompts English, absolute paths. Only codex+kilo convergence counts (agy rubber-stamps).
- Deploy: `CGO_ENABLED=0 go build -o ~/HARNESS/nexus/nexus ./cmd/nexus` -> stop
  service -> `cp` to `~/bin/nexus` -> `systemctl --user start nexus` -> verify
  "capability telegram ON".

---



Repo: `/home/matej/HARNESS/nexus` (github MatNik89/nexus, private). Language: **ENGLISH
everywhere** in this repo (user directive); Croatian only in chat with the user.
Constitution: `CLAUDE.md` + `AGENTS.md` (binding — read before acting).

## STATE
- **Architecture + PRD locked and user-approved** (identity: one kernel/two profiles,
  assistant-first; P0 = 6 capabilities, PRD §6 criteria; PRD amendment: `nexus --yolo`).
  Hard-questions round done and folded (HARDQ-CONSOLIDATED = binding resolutions,
  precedence: PRD > HARDQ > Annex A contracts > DESIGN-* > ...).
- **Daily reference:** `docs/ARCHITECTURE-ESSENTIALS.md` (15 decisions + P0 guards).
- **Task queue:** `docs/tasks-P0.md` (27 tasks, 8 phases) — the ONLY P0 queue.
- **Phase 0 DONE + merged to main** (merge 8e16cf2): T01 build gates (CGO=0,
  static-check), T02 hostile conformance suite v5 (bwrap backend; sealed-memfd synthetic
  closure; seccomp floor; read-only rootfs; 33 tests each boundary with causal negative
  controls; report: docs/PROBE-REPORT-2026-09-03.md — sandbox gate for T25/T26 OPEN=passed),
  T03 `nexus doctor` (capability-scoped, functional floor probe with exact sentinel).
  Convergence took 6 adversarial rounds (19→9→8→7→3→0 findings).
- **NEXT: Phase 1, T04** (typed contracts) — then T05..T12 per the ledger.

## METHOD (proven in Phase 0)
Slice branch per phase (`slice/p0-phaseN`) → implement task with RED observed before/
GREEN after → commit → 3-agent adversarial review (codex/kilo/agy via herdr) → fold →
re-verify until 3× PASS → merge to main. Review files land in docs/. Agy tends to
rubber-stamp — only consensus counts; codex digs deepest.

## ORCHESTRATION (herdr) — gotchas
- Targets by pane_id: codex=w8:p2, kilo=w8:p3, agy=w8:p4 (names unreliable).
- `herdr agent prompt w8:pN "..." --wait --until working --timeout 15000`; verify status
  became working (a swallowed error = not started; retry once with longer timeout).
- zsh does NOT word-split unquoted vars — never `set -- $pair` tricks.
- Agent cwd unreliable → ABSOLUTE paths in every prompt. Prompts in English.
- Poll for output files with a background loop (run_in_background), never foreground sleep.
- Shell cwd persists between Bash calls (a `cd docs` earlier broke later `git add` —
  always `cd /home/matej/HARNESS/nexus` first in git commands).

## HOST FACTS
- Pi (Kali arm64), Go 1.26.4, bwrap 0.11.2, umask 002 (t.TempDir() subdirs are 0775 —
  probe workdir guard needs 0700 dirs, tests use wdir(t) helper).
- cc available (used by dynamic/RUNPATH/RPATH fixtures; those tests skip without it).

## STANDING USER DIRECTIVES (memory-backed)
- English-only repo; 3-agent review for every non-trivial deliverable; plain-language
  decision questions for the user; `nexus --yolo` = confirmations off, safety nets NEVER.

## AGY UPGRADE (2026-09-03)
agy reads ~/.gemini/GEMINI.md (global): anti-rubber-stamp review discipline + pointers to
~/.gemini/skills/{code-review,ponytail}.md. Relaunched via `herdr agent start agy --kind agy
--pane w8:p4 -- --dangerously-skip-permissions`; rules confirmed loaded. Watch whether its
PASS-stamping stops in the next review batch.
