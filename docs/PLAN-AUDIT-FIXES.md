# PLAN: full-audit remediation (codex AUDIT-FULL-2026-09-08) — zero-defect

Source: `docs/AUDIT-FULL-codex-2026-09-08.md` (9 findings; bound at f135eef,
reverified against current main). Every fix: RED-capable detector at the real
owner boundary, then 3-agent review to 3xPASS. Grouped into slices to minimize
round count.

## Status
- [x] **F4 (HIGH)** acceptance subprocess-output race — DONE (96b0c49): dropped the
  unused `daemon()` output return read-during-run; all callers updated.
- [x] **F9 (LOW)** duplicate JSON config keys — DONE (96b0c49):
  `contracts.HasDuplicateJSONKeys` reject before decode + detector test.
- [ ] F1 F2 F3 F5 F6 F7 F8 — below.

## Slice A — F1 + F8(provider): one shared E11 egress dialer
Root: `internal/llm/provider/provider.go` builds a client with no custom
`Transport` (`:128-141`) → `http.DefaultTransport` (ambient proxy, unpinned DNS,
no receipt) while carrying the bearer token — an E11 violation
(`ARCHITECTURE-ESSENTIALS.md:146-158`). Telegram already has the compliant
dialer (`internal/channel/telegram/dialer.go`).
Fix: extract the pinned dialer into a SHARED owner (new
`internal/foundation/egress` or `internal/kernel/egress`): `PinnedClient(base,
egressAllow, opts, receiptSink) (*http.Client, error)` with the exact policy
already in telegram (Unmap, mode-sensitive floor incl. NAT64/RFC6145/RFC1918/
CGNAT/ULA/link-local, whole-set refusal, Proxy:nil, reject-redirects, receipt).
Telegram's `newPinnedClient` becomes a thin wrapper; provider builds its client
from the same owner (production endpoint mode; host must be in `egress_allow`).
No default-transport fallback anywhere.
F8-provider half: provider response body read through `io.LimitReader` (a
configured max) before JSON decode (`provider.go:193-213`).
Detectors: provider dialer refuses a metadata/rebind resolver answer; provider
Proxy:nil; over-limit body rejected; a shared-owner test proving both telegram
and provider reject the deny-floor set.

## Slice B — F2: S7-bounded delivery retry (channel core parks; S7 grants)
Root: `channel.go:542-591` re-pends definite failures to PENDING; the telegram
Run loop `FlushOutbox`es every tick (`telegram.go:388-403`) with no
`AttemptGrant`, limit, or backoff — unbounded retry outside the S7 owner
(`AGENTS.md:19-22`; tasks-P0.md:464-471 records the gap).
Fix: the channel core CLASSIFIES + PARKS only (no self-driven resend). Every
physical resend requires a fresh bounded `AttemptGrant` from `s7min` (deadline +
attempt cap + backoff). The Run loop asks S7 for a grant before each flush; on
grant-exhaustion the row parks (UNKNOWN/awaiting) instead of hot-looping.
Detectors: a delivery that keeps failing stops after N bounded attempts (RED
against unbounded re-pend); no resend occurs without a live grant; a terminal
4xx does not loop.

## Slice C — F5 + F8(reads): context budget + bounded trust reads
Root: `internal/kernel/budget` is imported by NO production package; loop/planner
append + send without enforcing it (`loop.go:267-275,394-401`;
`planner.go:253-354`). UDS `ReadString('\n')` unbounded (`daemon.go:155-159,
201-209`); streaming accumulates with no total cap (`planner.go:330-345`).
Fix: enforce ONE configured hard ContextBudget at the loop/planner boundary
before every provider attempt (trim/refuse per the budget contract). Bound the
UDS frame reader (max line/frame) and the streaming total-output ceiling.
Detectors: an over-budget context is refused/trimmed before the provider call
(RED against unbounded append); an over-long UDS frame is rejected; a
never-ending stream is cut at the ceiling.

## Slice D — F6: channel-health owner (no silent death)
Root: the Run loop ignores `PollOnce`/`FlushOutbox` errors and
`registerCommands` return (`telegram.go:388-423`) — a revoked token / journal
failure leaves the capability apparently ON while Telegram is silently dead.
Fix: one channel-health owner records redacted last-error + state; durable
substrate errors (journal) mark the channel OFF/degraded; transport failures are
classified and surfaced (visible to `nexus doctor`/logs). No error silently
dropped.
Detectors: a scripted permanent getUpdates rejection transitions health to a
recorded non-OK state (RED against the silent-discard path).

## Slice E — F7: config-aware doctor
Root: doctor hardcodes `NEXUS_API_KEY` / `NEXUS_TELEGRAM_TOKEN`
(`doctor.go:109-118,178-187`); `main.go:702-729` calls it without resolving
config → custom `ProviderKeyEnv`/`TelegramTokenEnv` report false OFF.
Fix: resolve config first; pass the actual secret-reference names into doctor.
Detectors: custom env names populated + defaults absent → capabilities report ON
(false-negative direction) AND a default-name mapping still reports correctly
(false-positive direction).

## Slice F — F3: release toolchain + vuln gate (needs a HOST Go upgrade)
Root: `go.mod:3` is `go 1.26`; `scripts/p0-accept.sh:65-75` builds with the
ambient patch release, no version/vuln gate; the binary was Go 1.26.4 with five
reachable stdlib advisories (fix floor 1.26.6).
Fix (code): p0-accept.sh enforces a MINIMUM Go patch (>= 1.26.6) and runs
`govulncheck -mode=binary`, failing the acceptance/sign step on any finding;
pin the floor in go.mod/toolchain.
HOST DEPENDENCY (owner): the Pi's Go must be upgraded to >= 1.26.6 and the binary
rebuilt + re-govulnchecked before a clean sign. The gate is the code deliverable;
the upgrade is an owner action.
Detectors: the gate FAILS on a < floor toolchain and on a govulncheck finding
(RED against the current ungated script).

## Order + review
A -> B -> C -> D -> E -> F (F last: it depends on the owner's Go upgrade).
Each slice: branch, RED-before/GREEN-after, 3-agent review to 3xPASS, merge.
F4/F9 already landed on `slice/p1-audit`.
