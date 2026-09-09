# HANDOFF — NEXUS resume point (READ FIRST) — updated 2026-09-08

## RESUME 2026-09-09 (audit remediation in flight) — start here

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
  (all folded in f33bc9a) — `docs/REVIEW-AUDIT-CODE1-*.md`. Round 2 (WHOLE stack
  @f33bc9a, incl. B1-B3+D) dispatched — files `docs/REVIEW-AUDIT-CODE2-*.md`.

**NEXT (in order):** (1) fold CODE2 findings on slice/audit-b -> re-dispatch until
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
