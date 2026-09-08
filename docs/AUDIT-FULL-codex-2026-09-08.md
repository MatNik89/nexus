# Full NEXUS Audit — Codex — 2026-09-08

## Binding and scope

- Audited commit: `f135eefae8d7e9c0599bb66c4bc7be72b22eaa36`
- Audited tree: `202b7e98f8600b6528d4a49d761ab72c2ac5043a`
- Clean export: `/tmp/nexus-audit-f135eef.0X1R5H/src`
- Repository production files were not modified during the audit.
- Untracked review documents were excluded from evidence about the committed tree.
- The repository moved to a later commit after the audit. Every finding below must be
  reverified against the newer revision before it is closed.

## Findings

### F1 — HIGH — Provider traffic bypasses the mandatory E11 egress boundary

`internal/llm/provider/provider.go:62-67` explicitly documents that the provider
implements only hostname filtering. The client constructed at `:128-141` has no custom
`Transport`, so Go uses `http.DefaultTransport`: ambient proxy variables, ordinary DNS
resolution, no resolved-IP policy validation or pinning, and no durable egress receipt.
The bearer token and conversation are sent at `:166-183`.

This contradicts `docs/ARCHITECTURE-ESSENTIALS.md:146-158`. Telegram already has the
required pinned transport at `internal/channel/telegram/dialer.go:192-249`, but the
provider does not reuse it.

PROBE: Static caller/transport trace plus `go doc net/http.DefaultTransport`.

FIX: Extract one generic E11 dialer owner and require provider and Telegram to use it;
permit no default-transport fallback.

### F2 — HIGH — Telegram performs unauthorized and unlimited retries outside S7

`internal/channel/channel.go:542-591` moves definite failures back to `PENDING`.
`internal/channel/telegram/telegram.go:388-403` invokes `FlushOutbox` on every timer tick,
without an `AttemptGrant`, retry limit, or backoff. Terminal HTTP 4xx responses enter this
path through `internal/channel/telegram/telegram.go:196-201`.

This violates the sole retry-owner rule in `AGENTS.md:19-22,57-60` and
`docs/ARCHITECTURE-ESSENTIALS.md:196-205`. The repository itself records the unresolved
gap in `docs/tasks-P0.md:464-471` despite marking the P0 work complete.

PROBE: End-to-end static state trace from HTTP response to re-pend to the next adapter
tick; existing `TestDeliveryHonesty` encodes the re-pend but does not require S7 authority.

FIX: Make the channel core classify and park outcomes only; require S7 to issue a fresh,
bounded delivery attempt grant before every physical resend.

### F3 — HIGH — Release acceptance can sign a binary with known Go vulnerabilities

`go.mod:3` specifies only `go 1.26`. `scripts/p0-accept.sh:65-75` builds and grades with
the ambient Go patch release and performs no toolchain vulnerability/version gate. The
audited binary was built with Go 1.26.4.

`govulncheck -mode=binary` exited 3 and reported five vulnerable standard-library symbols,
including GO-2026-6218, GO-2026-6090, GO-2026-5972, GO-2026-5856, and GO-2026-5026. Their
fix floor is Go 1.26.6 for this toolchain line. Symbol reachability is not proof that every
advisory is practically exploitable, but it is sufficient to reject signing this build.

PROBE: `govulncheck -mode=binary -show verbose /tmp/nexus-audit-f135eef.0X1R5H/nexus`.

FIX: Pin and enforce Go 1.26.6 or newer in the release/acceptance path, rebuild, rerun
`govulncheck`, and regenerate acceptance evidence.

### F4 — HIGH — Acceptance harness has a confirmed subprocess-output race

`internal/acceptance/acceptance_linux_test.go:144-178` assigns one non-thread-safe
`strings.Builder` to both subprocess stdout and stderr and calls `out.String()` while the
subprocess is still writing.

Observed failure:

```text
TestCriterion4SensitivityUnboundChat
panic: runtime error: unsafe.String: ptr is nil and len is not zero
strings.(*Builder).String
acceptance_linux_test.go:178
```

The same immutable revision passed on an isolated rerun. This confirms intermittency,
not resolution. All current callers discard the returned output string.

PROBE: Strict acceptance run reproduced the panic; isolated rerun and ten focused runs
passed. The race detector could not execute on this ARM64 host because ThreadSanitizer
rejected its VMA layout.

FIX: Delete the unused output return, or use a synchronized buffer and read it only after
`cmd.Wait`.

### F5 — MEDIUM — ContextBudget is isolated test machinery, not a production control

The hard-limit implementation exists at `internal/kernel/budget/budget.go:45-56`, but no
production package imports it. `internal/llm/planner/planner.go:253-354` assembles and
sends context directly, while `internal/kernel/loop/loop.go:267-275,394-401` keeps
appending observations without enforcing the budget.

The green budget tests therefore prove only the helper, not the P0 behavior required by
`docs/tasks-P0.md:78-88` and `docs/SECTION-MAP.md:23-35`.

PROBE: Production import/caller sweep found zero `internal/kernel/budget` consumers.

FIX: Enforce one configured hard limit at the loop/planner boundary before every physical
provider attempt.

### F6 — MEDIUM — Telegram runtime failures are silently discarded

`internal/channel/telegram/telegram.go:388-403` ignores every `PollOnce` and
`FlushOutbox` error. `registerCommands` claims its failures are logged, but the call at
`:393` and implementation at `:407-423` discard the error.

A revoked token, permanent API rejection, journal failure, or polling failure can leave
the sealed capability apparently ON while Telegram silently stops working.

PROBE: Caller and error-path sweep; there is no logging or channel-health transition on
these returns.

FIX: Surface redacted failures through one durable channel-health owner; stop or mark OFF
on durable-substrate errors and classify transport failures explicitly.

### F7 — MEDIUM — Ordinary `nexus doctor` disagrees with production configuration

Production supports configurable `ProviderKeyEnv` and `TelegramTokenEnv` at
`internal/foundation/config/config.go:29-39`. Doctor hardcodes `NEXUS_API_KEY` at
`internal/preflight/doctor/doctor.go:109-118` and `NEXUS_TELEGRAM_TOKEN` at `:178-187`.
`cmd/nexus/main.go:702-729` invokes doctor without resolving the configuration.

PROBE: With `NEXUS_CFG_PROVIDER_KEY_ENV=CUSTOM_PROVIDER_KEY` and
`NEXUS_CFG_TELEGRAM_TOKEN_ENV=CUSTOM_TG`, both custom variables populated and both default
variables absent, the audited binary reported both capabilities OFF.

FIX: Resolve configuration first and pass the actual secret-reference names into doctor;
test both false-negative and false-positive directions.

### F8 — MEDIUM — Trust-boundary reads and output accumulation are unbounded

The UDS daemon uses unbounded `ReadString('\n')` at
`internal/app/daemon/daemon.go:155-159,201-209`. Provider buffered JSON decoding has no
body limit at `internal/llm/provider/provider.go:193-213`. Streaming has a per-line scanner
limit but accumulates the entire response without a total limit at
`internal/llm/planner/planner.go:330-345`.

A malformed same-UID peer or pathological upstream can exhaust daemon memory.

PROBE: Static allocation/boundary trace; no total byte or token ceiling precedes these
allocations.

FIX: Apply bounded frame/body readers before allocation and enforce total provider-output
and semantic context limits.

### F9 — LOW — Configuration files accept duplicate JSON keys

`internal/foundation/config/config.go:161-205` unmarshals directly into
`map[string]json.RawMessage`. Go accepts duplicate object members with last-value-wins
semantics, making security-sensitive configuration ambiguous.

PROBE: Static parser trace. The repository already has a duplicate-key detector in
`internal/kernel/contracts/contracts.go`.

FIX: Reject duplicate member names before decoding the configuration map by reusing the
existing detector.

## Telegram picker work observed during the audit

An untracked picker snapshot previously ignored `crypto/rand.Read` failure while minting
session IDs. That snapshot changed and was subsequently committed after this audit bound
its revision, so it is not a current finding. The Telegram worker should reverify that
entropy failure is fail-closed in the final picker implementation.

The menu/picker work does not, by itself, resolve F2 or F6. Those findings concern the
existing delivery pump, S7 ownership, and runtime health/error reporting.

## Verification evidence

- `CGO_ENABLED=0 go test -count=1 ./...`: PASS
- `go vet ./...`: PASS
- Static build and `scripts/static-check.sh`: PASS
- Audited binary SHA-256:
  `9ecef75ea7eac84800b2e35e96d81ca942e202701587cc8db6bd2c3deed438a6`
- Strict acceptance plus sandbox: FAIL because of the F4 harness panic; sandbox/probe
  packages passed
- Isolated acceptance rerun: PASS
- `TestSoakSurvival`: SKIP because `NEXUS_SOAK_MINUTES` was unset
- `govulncheck -mode=binary`: FAIL, five reachable-symbol standard-library findings
- Race detector: unavailable on this host (`ThreadSanitizer: unsupported VMA range`,
  `Found 47 - Supported 48`)
- No live or paid provider/Telegram canary was run; no authorization was inferred

## Proof ceiling

A finite source and test audit cannot guarantee discovery of every possible defect. The
revision changed while the audit was running; this report proves the listed findings at
`f135eef`, not their status at later commits. Closing a finding requires a fresh
revision-bound detector at the actual owner boundary.

VERDICT: FAIL
