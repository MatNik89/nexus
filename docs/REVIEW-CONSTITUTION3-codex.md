# Constitution verification round 3

Target: `CLAUDE.md` and `AGENTS.md` at repository HEAD `4749f90dd6772f135ffe80012a5107186e214abb`. Scope is limited to the four residuals named in the round-3 brief and new errors introduced by their folds.

1. **[OK] AGENTS now preserves raw input when P0 rejects an unknown schema.**

   `AGENTS.md:27-29` now says an unknown schema ID/version is rejected unprocessed with raw input preserved, while upcast and quarantine remain a v2 layer. This matches `CLAUDE.md:42-44` and the owner contract at `docs/HARNESS-SPEC.md:1381`; it closes round-2 finding 5 without prematurely activating migration machinery.

2. **[OK] Reminder receipt and acknowledgment are occurrence-safe in both files, and AGENTS now forbids unverifiable generic completion.**

   `CLAUDE.md:73-76` and `AGENTS.md:49-52` bind both the durable delivery receipt and user acknowledgment to the same occurrence ID and explicitly prevent an acknowledgment for occurrence N from closing N+1. `AGENTS.md:51-52` also adds the missing prohibition on a generic “finish Y” claim without a handler-specific verifier. These clauses faithfully preserve `docs/HARDQ-CONSOLIDATED.md:84-90`.

3. **[OK] AGENTS now carries the complete B9/A2 activation and minimum-contract guard.**

   `AGENTS.md:55-60` requires restart on configuration change, delays Activator/RollbackVault until an explicitly selected dynamic consumer exists, defines `s7-min` as AttemptGrant + cancel + no-retry with retryable outcomes terminal in P0, defines `s5-min` as AtomicWriter, and defers the full S7 and S5 engines to P2 and P3. This matches `CLAUDE.md:79-83` and the binding resolutions in `docs/HARDQ-CONSOLIDATED.md:22-30,121-127`.

4. **[OK] The retained workflow conventions now carry the requested provenance, and Git is explicitly demoted to a moderator convention.**

   AGENTS attributes the smallest-diff rule to the user's cross-project topknot discipline (`AGENTS.md:68-69`), absolute cross-agent paths to the HANDOFF cwd gotcha (`AGENTS.md:70-71`), and adversarial/zero-finding review behavior to the user's epistemic-honesty rule (`AGENTS.md:72-76`). CLAUDE labels SDD as the user's cross-project topknot discipline (`CLAUDE.md:87-91`) and labels Git a moderator convention changeable without user approval (`CLAUDE.md:106-109`). This supplies the provenance/rejection rationale missing in round-2 finding 2 without presenting the Git workflow as a locked architecture decision.

5. **[OK] No new error or contradiction was introduced by these four folds.**

   The HEAD diff is confined to the named clauses. The schema edit retains the P0/v2 boundary; the reminder edit retains the two obligation types and assistant-grade evidence semantics; the activation edit preserves zero runtime activation in P0; and the provenance labels do not alter any product or Annex A contract. A normalized literal checker found every required clause in its intended constitution file, and comparison against the cited owner lines found no same-class semantic drift.

VERDICT: PASS
