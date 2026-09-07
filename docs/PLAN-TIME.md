# PLAN: current time in the model context (slice p0-time)

Dogfood (owner, 2026-09-08): NEXUS answers "ne znam koliko je sati"
whenever asked the time.

## Stolen pattern (researched first, per standing order)
Owner's local hermes-agent (`agent/context_compressor.py:4519`):
a "TEMPORAL ANCHORING: The current date is <X>." line is appended to
the system prompt; omitted entirely when the date cannot resolve.
General Telegram-bot practice matches: current datetime injected into
the system message per request (stateless, no tool needed).

## Change
In `ChatPlanner.Plan` (internal/llm/planner), append to the system
content, every call:
  "\nCurrent date and time: <2006-01-02 15:04 (Monday), TZ>"
using the HOST's local time zone (the Pi runs Europe/Zagreb). A
package-level `nowFn = time.Now` seam makes it testable; if the zone
lookup fails, fall back to UTC with the zone name printed (never omit
the line — unlike hermes we always have a clock).

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
- Prompt cache friction upstream (per-minute prompt changes) — DeepSeek
  prompt caching tolerates a changing tail; format to MINUTE resolution
  to keep the prefix stable within a minute.
