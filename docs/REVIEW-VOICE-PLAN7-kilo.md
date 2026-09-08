# Review of voice v7 + sandbox RO-input v3 + TG egress dialer (`6fca0bf`)

**Scope**: `docs/PLAN-VOICE.md` (v7), `docs/PLAN-SANDBOX-ROINPUT.md` (v3),
`docs/PLAN-TG-EGRESS-DIALER.md`, commit `6fca0bf` on `slice/p1-voice`.
**Reviewer**: kilo
**Date**: 2026-09-08

## Verdict

PASS. The round-6 residual is closed, verified against the actual config
validation. No substantive correctness / security / behavioral-equivalence /
unbuildable defect remains.

## Round-6 residual — closure status

### Mode-sensitive dialer address policy — closed

Verified against `internal/foundation/config/config.go:334-340`:

- `ValidateBounds` accepts `telegram_api_base` as exactly: the production URL
  (`https://api.telegram.org`), an empty value (defaults to production), or an
  explicit `http(s)` **loopback** override (`net.ParseIP(Hostname).IsLoopback()`
  or `"localhost"`); anything else is rejected at config resolution. So the
  dialer's two modes are exhaustive by construction — the plan's claim
  (`PLAN-TG-EGRESS-DIALER.md:29-32`) is accurate.
- **PRODUCTION mode** applies the public deny floor (loopback/unspecified/
  multicast/interface-local/link-local incl. `169.254.169.254`/private/CGNAT/
  ULA) to every `Unmap`-normalized answer (`:33-38`).
- **LOOPBACK-OVERRIDE mode** requires every answer to be loopback and rejects
  non-loopback/mixed (`:39-43`) — the local bot-api endpoint stays usable
  without weakening production.
- **MIXED-SET rule** applies in both modes (`:44-47`) — fail-closed, no
  pick-the-good-one.
- **"Production can never enter loopback mode"** holds because the mode is
  derived from the config-validated base, and config is a sealed startup
  snapshot (B9: restart-on-change) — no runtime mode switch.
- **MODE-LOOPBACK detector** (`:81-85`) exercises both modes: all-loopback
  connects and loopback+public is refused under a loopback base; all-public
  connects and any-loopback is refused under the production base.

The prior findings remain closed: `chan_inbox` replay determinism, startup
`InputRegistry` (identity-pinned, no fd leak), `-nt`/absolute paths/safe output
read, `ffmpeg -t` + P2.2-deferred quota, and the C2-refusal failure path.

## Notes (not FAIL reasons)

1. **Stale header**: `PLAN-VOICE.md:33` still reads "Change (v5 — round-3+4
   findings folded)" while the commit is v7. Cosmetic (the residual landed in
   the dialer prerequisite doc, which the voice plan references at `:27-30`).
2. **Empty-base edge case**: the plan's PRODUCTION condition names only
   `telegram_api_base == https://api.telegram.org`, but the config also allows
   empty (→ production default). The mode derivation must map empty → production.
   The adapter already resolves empty to the production URL, so this is an
   implementation detail; and even a wrong mapping is fail-closed (a production
   base wrongly classified as loopback would refuse all public Telegram IPs —
   an availability break, not a security weakening).

VERDICT: PASS
