# PLAN: current time in the model context (slice p0-time)

Dogfood (owner, 2026-09-08): NEXUS answers "ne znam koliko je sati"
whenever asked the time.

## Stolen pattern (researched first, per standing order)
Owner's local hermes-agent (`agent/context_compressor.py:4519`):
a "TEMPORAL ANCHORING: The current date is <X>." line is appended to
the system prompt; omitted entirely when the date cannot resolve.
General Telegram-bot practice matches: current datetime injected into
the system message per request (stateless, no tool needed).

## Change (v2 — all round-1 findings folded)
- PLACEMENT (agy HIGH: a timestamp in the SYSTEM prompt — the first
  message — invalidates the provider prompt-cache prefix every minute):
  the line is appended to the END of the CURRENT assembled user
  content, after the fenced/context body. The cacheable prefix
  (system + history role messages) stays byte-stable.
- CLOCK OWNER (codex MED / agy #3): NO package global. ChatPlanner
  gains a constructor-injected `clockid.Clock` (the repository's
  canonical clock seam, same as approval.NewStore); production wires
  clockid.Real, tests clockid.NewFake.
- FORMAT, exact Go layout (agy #2 / kilo: `TZ` was a placeholder
  mistake): layout `"2006-01-02 15:04 Monday"` plus explicit zone
  rendered as `<IANA-or-abbrev> (UTC+02:00)` built from
  `t.Zone()` + `t.Format("-07:00")` — the numeric offset is included
  so the model can compute RFC 3339 for reminder tools (kilo #5).
  The full line: "\nCurrent date and time: 2026-09-08 04:55 Monday,
  CEST (UTC+02:00)". Closed grammar: layout output + zone abbrev
  ([A-Za-z0-9+/_-]+, from the Go runtime, never user input) + fixed
  punctuation — no injection surface (codex LOW #2 addressed by
  construction; the string never contains model or user bytes).
- NO FALLBACK BRANCH (codex MED #3 / agy #7): `time.Time.Zone()` and
  `Local` cannot fail in Go — the imagined "zone lookup fails" branch
  is removed; there is exactly one code path.
- The line is appended to USER content, never near the toolProtocol
  grammar (agy #6 placement corruption is impossible by construction).

## Detectors (kilo blocking gaps folded)
- Unit with `clockid.NewFake` pinned at a KNOWN instant and an
  injected fixed `*time.Location` (test uses time.FixedZone — zone-
  independent of the host/CI, agy #4): assert the exact line at the
  end of the LAST user message, in both the plain and WithTools
  planners; assert the SYSTEM message does NOT contain the line
  (placement lock).
- Ablation: remove the injection -> RED; move it into the system
  prompt -> the placement lock turns RED.
- TestToolPromptDeterministic unchanged (system prompt is untouched).

## Non-goals
- No time TOOL (a prompt line answers "koliko je sati" without an
  effect path); a clock tool is a later slice if scheduling asks need
  sub-minute precision.
- No per-user timezone config (single-owner P0; the host zone IS the
  owner's zone).

## Detectors
- Unit: with nowFn pinned, the system message contains the exact
  formatted line (both plain and WithTools planners); ablation removes
  the injection -> RED.
- Determinism guard: TestToolPromptDeterministic keeps passing (tool
  ordering unaffected; the time line is identical within one pinned
  nowFn).

## Risk
- The user-content tail changes per minute; the cacheable PREFIX
  (system + history) is untouched by construction — the only cache
  cost is the current message itself, which is never cache-shared
  anyway.
