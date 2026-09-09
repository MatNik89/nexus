# HANDOFF — NEXUS resume point (READ FIRST) — updated 2026-09-08

## RESUME 2026-09-09 (audit remediation in flight) — start here

**Owner decision (2026-09-08 night):** work the WHOLE audit plan autonomously in a
loop; at every decision pick the variant that fixes the thing NOW and completely (no
minimal-then-later). Slice B therefore builds the FULL S7 retry engine (P2 pulled
forward). Telegram surface is PARKED. Autodeploy after 3xPASS + merge (gated).

**Plan:** `docs/PLAN-AUDIT-FIXES.md` v12 on `slice/p1-audit` — **CONVERGED: round 12 =
codex+kilo+agy PASS** (rounds 1-12 in `docs/REVIEW-AUDIT-PLAN{,2..12}-*.md`). Order:
F -> A -> B1 -> B2 -> B3 -> C -> D -> E.
- `slice/audit-b` (worktree `/home/matej/HARNESS/nexus-b`, stacked on audit-e): **B1 DONE**
  (4c4011b, `internal/kernel/s7` full engine, s7min deleted, all callers renamed) + **B2
  coded, uncommitted at the time of writing** (channel Flush under S7 with typed
  Companion batches, FAILED status + generation, telegram call kinds/poll/registration
  bot-bound + reconcile, main wiring, tests green for s7/channel/telegram; full suite
  running). Remaining: B3 (provider Execute + structured ExtractVia), D (health owner),
  then code reviews, merge chain, autodeploy.

**Code so far — STACKED worktrees (each branch on top of the previous), all suites
green (vet + go test ./...), each with RED-before/GREEN-after + one ablation RED:**
- `slice/audit-f` @ 9cb335b (`/home/matej/HARNESS/nexus-f`): F3 release gate —
  `go.mod toolchain go1.26.6` (GOTOOLCHAIN=auto downloads it; NO host Go upgrade
  needed), `scripts/lib/release-gate.sh` (binary Go version >= 1.26.6 + `govulncheck
  -mode=binary`, fail closed), `p0-accept.sh` gated before grade/sign, new
  `scripts/deploy.sh` (build -> gate -> stop -> install -> start -> verify "sealed
  capability ON"). govulncheck at ~/go/bin. Real binary under 1.26.6: clean.
- `slice/audit-a` @ 32ae84f (`/home/matej/HARNESS/nexus-a`): F1+F8(provider) —
  `internal/foundation/egress` shared E11 owner (canonical Endpoint host:port,
  mandatory ReceiptSink, ErrPreWire), provider + telegram rewired (telegram/dialer.go
  deleted), `channel.EgressSink(j)` at the composition root, Chat body cap (max+1 +
  strict single JSON value), Stream transport ceiling.
- `slice/audit-c` @ ccd2d79 (`/home/matej/HARNESS/nexus-c`): F5+F8(reads) — config
  `context_hard_limit_tokens` (default 64000; 0/neg/float rejected), budget EnforceWire
  on the FINAL messages before any grant, planner New/NewStreaming require a positive
  limit, stream accumulator ceiling, daemon bounded frames (close on breach).
- `slice/audit-e` @ 68bdd88 (`/home/matej/HARNESS/nexus-e`): F7 — doctor.Secrets from
  the resolved config, doctorChecks() in main (unresolvable config = finding).
- F4 + F9 landed earlier on `slice/p1-audit` (96b0c49).

**NEXT (in order):** (1) fold round 8 -> re-dispatch until codex+kilo PASS; (2)
CODE review of F+A+C+E by the 3 agents (revision 68bdd88, diff base fdb39dc, read at
`/home/matej/HARNESS/nexus-e`) -> fold -> 3xPASS; (3) build Slice B (B1 engine
`internal/kernel/s7`, B2 delivery wiring, B3 provider + structured via Execute) then
D on top of B; each 3xPASS; (4) merge the chain to main, autodeploy via
`scripts/deploy.sh`; (5) continue core improvement (coding USP, web research,
memory, steal-worthy patterns).

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
