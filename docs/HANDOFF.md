# HANDOFF — NEXUS resume point (READ FIRST) — updated 2026-09-03

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
