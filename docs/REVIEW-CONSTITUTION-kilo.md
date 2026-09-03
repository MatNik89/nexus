# REVIEW-CONSTITUTION — CLAUDE.md + AGENTS.md (binding repo rules)

Axes: FIDELITY / MISSING / CONTRADICTION / OVERREACH. Sources checked: ARCHITECTURE-ESSENTIALS,
HARDQ-CONSOLIDATED, HARNESS-SPEC Annex A, PRD, plus owner docs (HARNESS-PLAN/SECTION-MAP).

Overall the two files are high-fidelity on the PRE-hard-questions decisions (identity, owner
invariants, fail-closed, sandbox, profiles, trust, no-map, no-self-mod, evidence-over-prose) and
mutually consistent. The gap is that the recent HARD-QUESTIONS resolutions — which REVERSED prior
stances — are not in the binding "Hard rules" surface.

---

## MISSING (rules a future implementer needs, absent → would violate a locked decision)

**1. [MISSING] `channel:builtin` vs `channel:plugin` split (HARDQ A1) is absent from both files.**
This is a unanimous hard-questions resolution that REVERSED the blanket "channels requires
extensions" gate. A well-meaning agent implementing P0 Telegram from HARNESS-SPEC P1.6's OLD text
(or its own inference) will wire Telegram through the dynamic `extensions`/S6.8 closure → P0 Telegram
is re-blocked (`INCOMPLETE_CAPABILITY_CLOSURE`), i.e. the exact bug the hard-questions round fixed.
Neither file states "Telegram = built-in, attested by release signature (P1.5), NOT a runtime
extension." Source: HARDQ-CONSOLIDATED A1; HARNESS-SPEC:1461-1466.

**2. [MISSING] "No decay in P0; Decay+Audn = P1" (HARDQ B8) is absent.**
B8 REVERSED the prior "Decay+Audn P0" stance; HARNESS-PLAN S9.4 STILL reads "Decay+Audn P0". An
agent reading the owner doc (or prior knowledge) will re-introduce FSRS decay over explicit facts,
"forgetting" a stored fact and violating PRD §6.2 ("still knows it"). Neither file carries the
"P0 memory = explicit facts, no decay machinery" rule. Source: HARDQ-CONSOLIDATED B8;
ARCHITECTURE-ESSENTIALS E14.

**3. [MISSING] Assistant-grade evidence (receipt + ack) and the two obligation types (B5) are absent.**
Both files say "MarkDone only with Evidence" / "completion = artifacts (diff/exit/receipt)" — but
without B5's clarification this lets a well-meaning agent require diff/exit for a Reminder, making
every P0 reminder un-completable (re-fires forever) — the exact B5 failure. The "no generic
'finish Y' until a verifier exists" honesty point is also missing. Source: HARDQ-CONSOLIDATED B5;
ARCHITECTURE-ESSENTIALS E13.

**4. [MISSING] At-least-once delivery honesty + durable inbox/outbox (B2) and durable HITL
suspend/resume (B6) are absent.**
An agent building Telegram delivery could claim "exactly-once" over a non-transactional remote API
(the forbidden claim) or keep the approval wait in-memory (lost on crash → double-fire). Neither
file states "exactly-once ADMISSION, at-least-once remote delivery, `sent-but-unrecorded` →
UNKNOWN → RECONCILING" nor "approval wait is durable (`TurnSuspended` survives restart)".
Source: HARDQ-CONSOLIDATED B2/B6; ARCHITECTURE-ESSENTIALS E15.

**5. [MISSING] P0 has no runtime capability activation (B9) and full S7/S5 are later phases (A2).**
Neither file warns "P0 ships fail-closed `Resolve` + sealed startup snapshot, NOT the transactional
Activator/RollbackVault", nor "S7/S5 enter P0 only as -min contracts (`s7-min`=AttemptGrant+cancel+
no-retry, `s5-min`=AtomicWriter); full taxonomy/budgets/fencing and shadow-git/worktree are P2/P3."
A well-meaning agent could build the full S7 retry/budget/fencing engine or the Activator in P0
(over-engineering the exact components HARDQ B9/A2 deferred). Source: HARDQ-CONSOLIDATED A2/B9;
ARCHITECTURE-ESSENTIALS E6/E7.

## OVERREACH

**6. [OVERREACH] "Every subprocess goes through S6.2 sandbox (bwrap)" — taken literally, this
collides with the S2.1 CLIAgent provider subprocess and no source carves out the provider case.**
CLAUDE.md:29-30 / AGENTS.md:22 make this a hard "never violate" rule. But the CLIAgent auth mode
(S2.1) runs `codex exec --tools ""` / `claude -p --tools ""` as a subprocess that MUST reach the
network (it is the model provider) and is a trusted text-in/out adapter, not a tool-exec. The
sandbox rule's intent (SECTION-MAP owner-invarijante / E8) is TOOL/exec subprocesses; read literally
it would sandbox the provider (NET_DENY → broken) or push an implementer to treat the provider
subprocess as "not a subprocess" (unsandboxed hole). Fix: scope the rule to "every TOOL/exec
subprocess" and state the provider-subprocess (CLIAgent) exception explicitly.

## FIDELITY (minor)

**7. [FIDELITY] "one serialized journal append actor (P0.3)" conflates mechanism with contract.**
The single append ACTOR is HARDQ B7's mechanism; P0.3 is the single-write-owner CONTRACT. Both are
true and the conflation is harmless, but a reader could misattribute the actor to the Annex
contract. Suggest "(P0.3 single-writer; B7 append actor)".

**8. [FIDELITY] "three herdr agents" — typo ("herdr"); and "mandatory … every non-trivial deliverable
… three-agent review" is a process mandate not source-locked as mandatory-for-everything.**
The 3-agent + moderator pattern is the observed method (HARDQ/REVIEW/PLAN-HOLES), so the rule is
defensible, but "herdr" is a typo and "mandatory for every non-trivial deliverable" is stronger than
any source states. Low severity.

## CONTRADICTION

No material contradiction found — between the two files (the only differences are completeness:
CLAUDE.md adds Git + review-process + "Croatian only in user conversation"; AGENTS.md adds the
review-tag taxonomy) or with the sources. The closest thing to a conflict is the "every subprocess"
tension in finding 6, tagged OVERREACH above.

---

## Verdict

Faithful and internally consistent on everything locked BEFORE the hard-questions round. But the
binding "Hard rules" surface omits three reversal-type hard-questions resolutions (A1 channel
builtin/plugin, B8 no-decay, B5 assistant-evidence) — precisely the decisions a well-meaning agent
following stale sources (e.g. HARNESS-PLAN "Decay+Audn P0", old P1.6 "channels requires extensions")
will violate — plus one over-broad subprocess rule. These are fixable in ~4 added lines, but as
shipped the constitution does not bind future sessions to several locked P0 decisions.

VERDICT: FAIL
