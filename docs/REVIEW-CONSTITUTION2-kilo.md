# REVIEW-CONSTITUTION2 — round-2 verification of CLAUDE.md + AGENTS.md

Scope: (1) fold/reject every material round-1 finding (codex 12 + kilo 8; agy PASS, no findings);
(2) no NEW error/contradiction; (3) P0 scope guards match HARDQ-CONSOLIDATED exactly.

---

## (1) Round-1 findings — fold/reject status

### codex (12) — all folded or resolved

**1. [OK] tasks-P0.md nonexistent.** CLAUDE:18-19 / AGENTS:71-72 — "task ledger, **once created**.
Until it exists, no P0 implementation is authorized; doc/design work proceeds under explicit user
tasks." Exact codex #1 fix. ✓

**2. [OK] GAPFIX promoted to owner.** CLAUDE:15-17 / AGENTS:12-13 — "Raw `GAPFIX-*` files are NOT
owners — a GAPFIX clause binds only where a higher-ranked current owner incorporates it (… e.g.
ObligationStore direct-SQLite write)." Exact codex #2 fix. ✓

**3. [OK] Unknown-schema globalized.** CLAUDE:42-44 / AGENTS:27-28 — now scoped to "unknown TYPED
inputs … unknown schema ID/version → reject unprocessed in P0 (raw preserved); v2 layer adds
upcast + quarantine (HARDQ C7)." Matches E11/C7. ✓

**4. [OK] English-only "invented" — RESOLVED by provenance annotation.** CLAUDE:8-10 — "Language
(explicit user directive, 2026-09-03 — repo convention, not architecture)". The finding's premise
was "no source"; the annotation records the actual source (explicit user directive, dated) and
separates it from architecture-fidelity. The annotation resolves the finding; I do not re-litigate
the directive. ✓

**5. [OK] Mandatory 3-agent review — RESOLVED by provenance + non-recursive base case.**
CLAUDE:92-98 — "Multi-agent review (explicit user directive, 2026-09-03)" + "Non-recursive base
case: review files, consolidation docs, and fold commits are OUTPUTS of this gate, not fresh gated
deliverables." Resolves both the "no source" and the "recursion/non-termination" concerns. ✓

**6. [OK] Workflow-rule bundle — folded/annotated.** RED-before-fix + temp fixtures annotated
"user's cross-project discipline: topknot" (CLAUDE:86 / AGENTS:59-61); review tags softened to
"task-appropriate finding tags" (AGENTS:66, fixed tag list dropped); Git → "Git (convention)"
(CLAUDE:105). Residual: "absolute paths" (AGENTS:63) and "zero-finding review = failed review"
(AGENTS:64-65) carry no inline annotation, but both are the user's standing cross-project
discipline (present in the global config), so not "invented". ✓

**7. [OK] User sign-off overreach.** CLAUDE:28-30 — sign-off now scoped to "alters a PRD product
decision, a user-decided HARDQ resolution, or an accepted security/proof criterion;
invariant-preserving implementation goes through the normal task/review gate." Exact codex #7 fix. ✓

**8. [OK] Exact-intent approval.** CLAUDE:35-38 / AGENTS:23-25 — expiring, single-use,
profile-bound, canonical-field hash, `(device,inode)` for destructive FS, no reusable
"approve this tool" (HARDQ C4). ✓

**9. [OK] UNKNOWN/retry boundary.** CLAUDE:39-41 / AGENTS:26 — "no automatic retry of
IRREVERSIBLE/unknown outcomes until reconciliation proves the prior result (E9)." ✓

**10. [OK] Delivery honesty.** CLAUDE:57-59 / AGENTS:37-39 — exactly-once admission / at-least-once
remote / persist-before-offset / sent-but-unrecorded → RECONCILING (HARDQ B2). ✓

**11. [OK] AGENTS channel binding.** AGENTS:34-36 — now "channel identity binds to a profile BEFORE
admission (per-chat, deny-default) — never a post-admission mutable global profile lookup
(HARDQ B3)." ✓

**12. [OK] P0 static-capability boundary.** CLAUDE:78-80 / AGENTS:52-53 — fail-closed Resolve +
sealed startup snapshot + restart-on-change; Activator/RollbackVault forbidden until an explicit
dynamic consumer (HARDQ B9). ✓

### kilo (8) — all folded into the new "P0 scope guards" + one hard-rule fix

**13. [OK] channel:builtin/plugin (A1)** → CLAUDE:68-70 / AGENTS:44-45. ✓
**14. [OK] no decay (B8)** → CLAUDE:71-72 / AGENTS:46-47. ✓
**15. [OK] assistant evidence (B5)** → CLAUDE:73-75 / AGENTS:48-49. ✓
**16. [OK] at-least-once + durable HITL (B2/B6)** → CLAUDE:57-59,76-77 / AGENTS:37-39,50-51. ✓
**17. [OK] no Activator + -min cuts (B9/A2)** → CLAUDE:78-82 / AGENTS:52-54. ✓
**18. [OK] "every subprocess" overreach** → CLAUDE:45-50 / AGENTS:29-32 — scoped to "every
TOOL-executing subprocess (`ExecProcess`)" + explicit CLIAgent carve-out ("governed by the provider
boundary + egress policy, not the tool sandbox's NET_DENY"). ✓
**19. [OK] append-actor attribution** → CLAUDE:33-34 / AGENTS:22 — "journal single-write-owner
contract (P0.3), implemented by one serialized append actor (HARDQ B7)". ✓
**20. [OK] "herdr" typo** → NOT fixed (CLAUDE:94 still "herdr agents"); cosmetic, non-material.

---

## (2) NEW errors / contradictions — none material

- No new contradiction between the two files (hard rules and P0 guards mirror each other; the only
  divergence is compression, e.g. AGENTS abbreviates `s7-min`/`s5-min` without expansion — a
  completeness difference, not a conflict).
- No new contradiction with sources: the CLIAgent carve-out and the "TOOL-executing subprocess"
  scoping do not contradict any locked rule (no source places the provider subprocess under tool
  NET_DENY); the sign-off scoping, exact-intent, UNKNOWN, delivery and static-capability rules all
  match their cited HARDQ/E sources.
- Two non-material residuals (not errors): (a) exact-intent omits C4's negative half ("NEVER
  transient timestamps or repo-wide revision hashes"), a compression not a distortion; (b) the
  owner-invariants list omits "queue lease/fencing = S7.2" — pre-existing (present in round 1 too)
  and P2-scoped, so not a P0 rule an implementer needs.

## (3) P0 scope guards vs HARDQ-CONSOLIDATED — exact match

A1 (builtin vs plugin, P1.5 release-signature) ✓ · B8 (explicit facts, no decay, Decay+Audn=P1,
facts exempt) ✓ · B5 (two types, receipt+ack evidence, no generic finish-Y) ✓ · B6 (TurnSuspended
durable, no in-memory wait) ✓ · B9 (fail-closed Resolve + sealed snapshot; Activator forbidden
until dynamic consumer) ✓ · A2 (s7-min/s5-min; full engines P2/P3) ✓ · C3 (stuck within one turn;
continuous-loop exemption) ✓. Both files carry the identical set; wording is faithful to the
consolidated text.

---

## Verdict

Every material round-1 finding is folded or resolved; the two "invented" rules (English-only,
3-agent review) now carry a correct provenance annotation that resolves the source concern (and the
3-agent rule additionally gets a non-recursive base case that fixes termination). The new P0 scope
guards match HARDQ-CONSOLIDATED exactly. No material new error or contradiction was introduced; the
only residuals are a cosmetic typo and two non-material compressions.

VERDICT: PASS
