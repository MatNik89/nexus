# PLAN-TGOUT v8 review — Codex

Revision reviewed: `8f8bc4cc03c9860ab3ad1bb7055dd34ef0566cc8` on `slice/p0-tgout`.

Scope: design-only review of `docs/PLAN-TGOUT.md`, read from the immutable on-disk archive whose SHA-256 is `b9ee18b89d68e9850a4bbe1757749d731e1a49c74786faf675ff3f3ecff01e7a`. I checked the plan against the committed planner, journal projection/rebuild mechanism, transport-neutral outbox, Telegram adapter, architecture essentials, HARDQ B2/B7, Annex A, and `docs/tasks-P0.md`. No production code or git state was changed.

## Findings

[CONCRETE][SEV: LOW] `docs/PLAN-TGOUT.md:41-45,110-125` - The plan still says both that there is “NO second send at all” / “no second attempt ever exists” and that a rejected formatted send is followed by a plain send on the next flush tick. Those statements cannot both define the adapter contract. The intended boundary is now inferable from lines 124-125 (one send per tick), but the retained absolute wording can produce an implementation or test that forbids the required recovery send.

PROBE: Static contradiction within the immutable v8 plan: lines 43-45 and 114-116 forbid every second send, while lines 122-125 require a later re-flush and therefore a second wire attempt for the same delivery ID.

FIX: Replace both absolute claims with “no immediate fallback send within the same `FlushOutbox` call/tick”; state explicitly that a later tick may issue one plain retry for the same pending delivery.

[CONCRETE][SEV: MED] `docs/PLAN-TGOUT.md:117-127,164-169` - The new `attempts` column is named, but its durable owner and transition are not specified, and the detector list does not exercise the behavior it was added to guarantee. The current outbox is a journal-derived projection (`internal/channel/channel.go:174-205`), every send is parked `UNKNOWN` before the wire (`internal/channel/channel.go:488-514`), and a definite 400 is re-pended through an `EvOutboundResolved` append (`internal/channel/channel.go:527-534`). Merely adding/selecting a column leaves multiple materially different implementations possible: direct SQL mutation would violate the journal single-write-owner rule; incrementing at an unspecified post-send point can lose the formatting decision across failure; and changing the table without bumping `Projection.Version()` leaves an existing version-1 database without the column because `CREATE TABLE IF NOT EXISTS` does not alter it. The listed “unchanged 400 semantics” test can remain GREEN even if the counter never advances and every later tick repeats rejected HTML.

PROBE: End-to-end static trace of the current committed boundary plus detector non-vacuity review. `Projection.Version()` is currently `1` (`internal/channel/channel.go:180-205`); the journal rebuilds a projection only when that version changes (`internal/kernel/journal/journal.go:331-380`). The plan lists first-send `parse_mode` checks and unchanged 400 handling, but no two-flush sequence, restart check, projection-upgrade check, or counter-update ablation.

FIX: Make the existing journal fold the sole owner: bump the channel projection version, initialize `attempts=0`, increment it in the durable pre-wire `EvOutboundUnknown` fold, and preserve it when a definite failure resolves back to `PENDING`. Add a committed test that observes first flush `rendered+HTML -> 400 -> PENDING(attempts=1)`, closes/reopens the journal, then observes exactly one second-flush wire call carrying the original text with no `parse_mode` and ending `SENT`; ablate only the increment and require RED. Add an upgrade/rebuild test from a version-1 database.

## Round-7 fold check

The closed exact-lowercase key contract blocks the case-insensitive Go struct alias from executing. Reusing the token walk over every top-level string value also closes the adverse duplicate ordering `{"action":"memory_recall","action":"tool"}` without a last-member-wins re-decode. Those two round-7 findings are resolved as design requirements. The first-attempt formatting concept addresses the prior repeated-HTML failure, but the two findings above leave its contract and proof incomplete.

## Topknot / weakest link / proof ceiling

The planner hardening remains local and dependency-free. For the formatting state, a durable boolean such as `format_attempted` would express the only decision currently required more narrowly than an unbounded counter; if future retry telemetry genuinely needs a count, the counter is justified, but it must not become a second retry-authority beside S7.

Weakest link: this is a design-only static review. It proves contradictions and missing ownership/detector requirements in the committed plan, not the behavior of a future implementation or the Telegram service.

VERDICT: FAIL
