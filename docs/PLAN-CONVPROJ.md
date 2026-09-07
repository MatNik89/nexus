# PLAN: conversation projection (slice p0-convproj)

## Failure (real, from the quiet soak try1, 2026-09-08)
`soak: soak-msg-001237 unanswered for >60s (backlog explosion)` after
42 minutes. Cause: `conversationHistory` and `recoveredTurnOutcome`
each run `Journal.Replay(0, ...)` — a FULL journal scan — on EVERY
channel turn. At message ~1200 the journal holds tens of thousands of
events; per-turn latency grows linearly until the fixed-cadence
producer's backlog explodes. This is precisely the topknot ceiling
declared in the conv-history slice ("trigger for a transcript
projection: replay latency visibly lagging a chat turn") — the trigger
has fired.

## Stolen pattern (step-0 research)
- IN-REPO: the journal already owns a SyncProjection framework
  (memory/schedule/obligation/channel/approval all fold events into
  queryable SQLite tables with versioned rebuild). This IS the
  event-sourcing "read model" standard.
- EXTERNAL: karfly/chatgpt_telegram_bot keeps per-chat dialog in a
  persistent store (MongoDB) read by key — never by replaying history;
  every mature gateway does the same.

## Change
New `conv` projection in internal/app/daemon (or internal/channel —
reviewer's call) folding exactly the events the two replays consume:
- `channel.inbound_admitted` -> upsert (identity, update_id) user text
- `turn.succeeded` -> set final + completed on the matching turn row
- `turn.failed` -> set error_code/tool + terminal
- `approval.turn_suspended` -> set suspension summary
- `turn.resumed` -> clear the suspension candidate (same precedence
  rules as today's fold, now applied incrementally)
Table `conv_turns(identity, update_id, turn_id PRIMARY KEY, user_text,
final, code, tool, state, seq)` with an index on (identity, seq).
- `conversationHistory`: SELECT completed pairs for identity ORDER BY
  seq DESC LIMIT 12 (then reverse) — O(12), no replay.
- `recoveredTurnOutcome`: SELECT by turn_id — O(1), same
  RecoveredOutcome sum semantics (SUCCEEDED/SUSPENDED/FAILED with the
  resumed-suppression already materialized).
Projection.Version bump; old databases rebuild by replay (existing
framework). The Replay(0) calls are DELETED from the turn path.

## Behavioral invariants (unchanged, now enforced by projection state)
All existing detectors keep passing UNCHANGED: history rules
(completed-only, cap-after-filter, exact identity, empty-final,
clip), redelivery recovery (drift typed outcome, ordinary generic,
mixed lifecycle, resumed-without-terminal), cross-identity isolation.
They are the conformance suite for the projection — the fold must be
observationally identical to the replay.

## Detectors (new)
- LATENCY RED: a benchmark-style test seeds N=3000 completed turns and
  asserts one RunChannelTurn (history + collision path) completes in
  < 250ms on the dev host — with the old replay this is seconds
  (RED-capable by construction; threshold generous to avoid CI flake).
- Rebuild: old-version database refolds and yields identical history
  output to a fresh fold (golden comparison on a seeded corpus).
- The soak harness itself is the end-to-end detector (quiet re-run
  after landing).

## Risks
- Projection guard: statements must not touch canonical tables
  (existing NON_CANONICAL_WRITE guard applies; SQL wording must avoid
  guarded keywords in comments — known lexical gotcha).
- Suspension precedence subtleties move from fold-time to
  incremental-update-time; the mixed-lifecycle detectors are the lock.
