# PLAN: current time in the model context (slice p0-time)

Dogfood (owner, 2026-09-08): NEXUS answers "ne znam koliko je sati"
whenever asked the time.

## Stolen pattern (researched first, per standing order)
Owner's local hermes-agent (`agent/context_compressor.py:4519`):
a "TEMPORAL ANCHORING: The current date is <X>." line is appended to
the system prompt; omitted entirely when the date cannot resolve.
General Telegram-bot practice matches: current datetime injected into
the system message per request (stateless, no tool needed).

## Change (v6 — current; rounds 1-5 folded)
- PLACEMENT (agy HIGH: a timestamp in the SYSTEM prompt — the first
  message — invalidates the provider prompt-cache prefix every minute):
  the line is appended to the END of the CURRENT assembled user
  content, after the fenced/context body. The cacheable prefix
  (system + history role messages) stays byte-stable.
- CLOCK + TIMEZONE SEAM (v5, definitive): ChatPlanner receives
  constructor-injected `clockid.Clock` AND the IANA zone NAME as a
  string. The constructor calls time.LoadLocation(name) and stores
  BOTH the *time.Location and the original name — rejecting
  construction (fail-closed, like every planner dependency) on an
  empty name or a LoadLocation error. There is NO time.Local use and
  NO silent-UTC fallback anywhere in this slice: missing tzdata for
  the configured zone REJECTS DAEMON STARTUP with a causal error
  (r4 codex MED#2 — one unambiguous production outcome).
- CONFIG CONTRACT (r4 codex MED#3, executable): a new closed typed
  field `timezone` (JSON key "timezone") added to Config, keySchema,
  defaults (default "Europe/Zagreb"), applyValue and validation in
  internal/foundation/config/config.go — validation itself calls
  time.LoadLocation AND explicitly REJECTS the special value "Local"
  (r5 codex MED#1: LoadLocation("Local") succeeds and would smuggle
  host-dependent time back in; "" also rejects; "UTC" is allowed — it
  is a stable identifier). buildDaemon (the ONLY planner composition
  root — chat rides the daemon UDS, r5 kilo F2) passes
  resolved.Config.Timezone into the planner factory.
- RENDER (r4 codex MED#1): the line prints the STORED IANA name
  verbatim plus the instant's abbreviation and offset from
  clock.Now().In(loc):
  "\nCurrent date and time: 2026-09-08 05:10 Monday, Europe/Zagreb
  (CEST, UTC+02:00)" — layout "2006-01-02 15:04 Monday", zone abbrev
  via t.Format("MST"), offset via t.Format("-07:00"). The IANA name
  never passes through t.Zone().
- The line is appended to USER content, never near the toolProtocol
  grammar (agy #6 placement corruption is impossible by construction).

## Detectors (kilo blocking gaps folded)
- Unit with `clockid.NewFake` pinned at KNOWN instants and the REAL
  IANA name "Europe/Zagreb" through the name seam (r5 codex MED#2 /
  kilo F1 — FixedZone cannot exercise IANA loading): a JANUARY instant
  renders "Europe/Zagreb (CET, UTC+01:00)" and a JULY instant
  "Europe/Zagreb (CEST, UTC+02:00)" — proving stored-name rendering
  AND real DST rules; assert the exact line at the end of the LAST
  user message in both planners; assert the SYSTEM message does NOT
  contain it (placement lock). CI note: tzdata is available via the
  Go toolchain (import time/tzdata in the test binary if the runner
  lacks the system database — stated so the detector is portable).
- Nil-clock rejection at construction (r5 codex MED#2).
- COMPOSITION-ROOT wiring detector (r5 codex MED#3): the existing
  production-spine test (cmd/nexus, captures provider requests) gains
  a case with config timezone "America/New_York" — the captured LAST
  user message must contain that name, proving buildDaemon really
  forwards resolved.Config.Timezone (a hard-coded default turns it
  RED).
- Config validation cases: "Local" rejected, "" rejected, unloadable
  rejected, "UTC" accepted.
- Constructor fail-closed cases: empty zone name and an unloadable
  zone name both REJECT construction (r3 codex LOW#2).
- Ablation: remove the injection -> RED; move it into the system
  prompt -> the placement lock turns RED.
- TestToolPromptDeterministic unchanged (system prompt is untouched).

## Non-goals
- No time TOOL (a prompt line answers "koliko je sati" without an
  effect path); a clock tool is a later slice if scheduling asks need
  sub-minute precision.
- No per-user timezone config (single-owner P0; the host zone IS the
  owner's zone).

## Risk
- The user-content tail changes per minute; the cacheable PREFIX
  (system + history) is untouched by construction — the only cache
  cost is the current message itself, which is never cache-shared
  anyway.
