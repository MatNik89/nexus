# REVIEW-CONSTITUTION3 — round-3 final verification (4 codex residuals)

Scope: verify the 4 fixes + flag any NEW error.

---

**1. [OK] AGENTS schema rule carries raw-input-preserved.**
AGENTS:27-29 — "unknown schema ID/version → reject unprocessed in P0 **with raw input preserved**
(upcast+quarantine = v2 layer, HARDQ C7)." Now matches CLAUDE:42-44 and HARDQ C7. ✓

**2. [OK] Both files bind receipt+ack to the SAME occurrence ID; AGENTS carries no-generic-finish-Y.**
CLAUDE:73-76 — "durable delivery receipt + user ack, **both correlated to the SAME occurrence ID
(an ack for occurrence N never closes N+1)** … no generic 'finish Y' claim without a verifier
(HARDQ B5)." AGENTS:49-52 — identical occurrence-ID binding + "no generic 'finish Y' claim
without a **handler-specific** verifier". Faithful to B5 ("correlated to occurrence"; "typed Task
handlers with handler-specific postconditions"). ✓

**3. [OK] AGENTS mirrors restart-on-config-change, explicit dynamic consumer, -min contents, P2/P3.**
AGENTS:55-60 — "fail-closed `Resolve` + sealed startup snapshot + **restart-on-config-change**; the
transactional Activator/RollbackVault is forbidden until an **explicitly selected dynamic consumer**
exists (HARDQ B9). S7/S5 enter P0 only as -min contracts: `s7-min` = AttemptGrant + cancel +
no-retry (**retryable → FAILED_TERMINAL**), `s5-min` = AtomicWriter; full S7 taxonomy/budgets/
fencing = P2, full S5 shadow-git/worktree = P3 (HARDQ A2)." AGENTS is now as specific as CLAUDE;
matches HARDQ B9 and A2 exactly (including the FAILED_TERMINAL detail). ✓

**4. [OK] Remaining workflow conventions carry provenance; git labeled moderator convention.**
- topknot: CLAUDE:87 ("user's cross-project discipline: topknot") + AGENTS:69 ("user's
  cross-project topknot discipline"). ✓
- HANDOFF gotcha: AGENTS:70-71 ("Absolute paths … (HANDOFF operational gotcha: agent cwd is
  unreliable)"). ✓
- epistemic-honesty: AGENTS:72 ("Reviews are adversarial, not rubber-stamps (user's cross-project
  epistemic-honesty rule)"). ✓
- git: CLAUDE:106 ("## Git (moderator convention — changeable without user approval)") + CLAUDE:108
  ("mirrors the user's cross-project rule 'branch first, review before push'"). ✓

---

## NEW errors — none

No new contradiction between the two files: B5/B9/A2 blocks are now mutually consistent (AGENTS
matches CLAUDE's specificity), and the added provenance annotations (topknot / HANDOFF /
epistemic-honesty / moderator convention) are attributions, not rule changes. The "SAME occurrence
ID (ack for occurrence N never closes N+1)" phrasing is a faithful expansion of B5's "correlated to
occurrence" (it already appears verbatim in the established AGENTS lineage), and "handler-specific
verifier" is a faithful rendering of B5's "handler-specific postconditions" — no distortion.

---

## Verdict

All 4 residual fixes are correctly folded and consistent across both files; no new error or
contradiction introduced.

VERDICT: PASS
