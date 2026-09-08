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

## Change (v2 — round-1 findings folded)
New `conv` projection folding the SAME events today's two replays
consume, with these corrections:
- EVERY event UPSERTS (r1 codex #1): a row is created by WHICHEVER
  lifecycle event arrives first — suspended-only and failed-only turns
  (both exist in committed detectors) get rows without an admission.
  Columns: turn_id PRIMARY KEY, identity, update_id, user_text,
  hist_done (BOOLEAN — set by a valid turn.succeeded, empty final
  included, but ONLY when the row's admission has already been
  observed (hist_seq set): the reference discards a success that
  precedes its admission and a later admission does NOT resurrect it
  (r3 codex MED) — the fold mirrors that exactly; r2 codex HIGH / kilo F1: completion is the EVENT, the
  index predicates on THIS marker, never on content), hist_final
  (lossless content, may be the empty string), rec_state
  (NONE|SUCCEEDED|SUSPENDED|FAILED — recovery semantics, where
  SUCCEEDED additionally requires the non-empty final exactly as
  today), code, tool, susp_summary, created_seq (immutable
  first-observed offset), hist_seq (NULLABLE — set ONLY by the
  admission event; r2 codex MED#1: history order is ADMISSION order,
  so a turn first seen through a lifecycle event gets history order
  only when/if its admission arrives), source_event_id,
  source_offset, projection_version (per-row provenance required by
  Annex P0.3; r2 codex MED#2 — updated on every fold touch to the
  last contributing event).
- SEQ OWNERSHIP (r1 codex #2, split in v3 per r2 codex MED#1):
  created_seq = first-observed offset (row identity, immutable);
  hist_seq = the ADMISSION event's offset (history order — exactly
  today's semantics, including the failed-before-admitted
  counterexample: history order follows admission, not first sight).
- QUERY + INDEX (r1 codex #3, r2 corrected): partial index ON
  conv_turns(identity, hist_seq) WHERE hist_done AND hist_seq IS NOT
  NULL — membership by the completion MARKER and ADMISSION order
  (true O(12) under an incomplete tail); the history query excludes
  the CURRENT turn id (r1 kilo F2).
- Incremental precedence (same rules, now stated per event): resumed
  clears susp_summary and downgrades rec_state SUSPENDED->NONE; failed
  always overwrites rec_state (payload or not); succeeded overwrites
  everything later events would have set — the differential oracle
  below is the lock, not prose.
- The Replay(0) calls are DELETED from the turn path;
  Projection.Version bump rebuilds old databases.

## Equivalence oracle (r1 codex #4 / kilo F3 — replay-vs-fold
## differential, not fold-vs-fold golden)
The OLD replay implementations are RETAINED AS TEST-ONLY reference
code. A differential detector generates randomized lifecycle sequences
(seeded, reproducible) covering: admitted-only, suspended-only,
failed-only, empty-final succeeded, repeated suspend/resume cycles,
resume-after-terminal, interleaved identities — and asserts the
projection's conversationHistory AND recoveredTurnOutcome answers are
IDENTICAL to the reference replay's on every prefix of every sequence.
A precedence bug in the fold cannot be mirrored in the independent
reference, so false-green is structurally excluded.

## Behavioral invariants (unchanged, now enforced by projection state)
All existing detectors keep passing UNCHANGED: history rules
(completed-only, cap-after-filter, exact identity, empty-final,
clip), redelivery recovery (drift typed outcome, ordinary generic,
mixed lifecycle, resumed-without-terminal), cross-identity isolation.
They are the conformance suite for the projection — the fold must be
observationally identical to the replay.

## Detectors (new)
- LATENCY RED, host-independent (r1 kilo F4): on the SAME host and
  corpus (3000 completed + 3000 incomplete-tail turns — the tail case
  r1 codex #3 called out), the projection path must answer at least
  20x faster than the retained reference replay; a relative assert
  cannot flake on CI speed.
- Rebuild: an old-version database refolds and the DIFFERENTIAL oracle
  passes against the reference on the rebuilt state.
- The soak harness itself is the end-to-end detector (quiet re-run
  after landing).

## Risks
- Projection guard: statements must not touch canonical tables
  (existing NON_CANONICAL_WRITE guard applies; SQL wording must avoid
  guarded keywords in comments — known lexical gotcha).
- Suspension precedence subtleties move from fold-time to
  incremental-update-time; the mixed-lifecycle detectors are the lock.
