# Review of audit-remediation slices F, A, C, E (`slice/audit-e`)

- **Reviewed revision**: `68bdd88` on `slice/audit-e` (worktree `/home/matej/HARNESS/nexus-e`)
- **Diff**: `fdb39dc..68bdd88` (9cb335b F, 32ae84f A, ccd2d79 C, 68bdd88 E)
- **Verdict**: PASS

Build clean (`CGO_ENABLED=0 go build ./...`); all slice suites pass (`egress`,
`provider`, `planner`, `budget`, `config`, `doctor`, `cmd/nexus`).

## Per-slice disposition

### Slice F (F3 release gate) — closed at the owner boundary

`scripts/lib/release-gate.sh` is the ONE shared gate (`RELEASE_GO_FLOOR=1.26.6`,
`go version <bin>` floor check, `govulncheck -mode=binary`, every failure mode
fails closed with distinct exit codes), sourced by both `p0-accept.sh` (before
sign/publish) and `deploy.sh` (before install/restart). `go.mod` pins
`toolchain go1.26.6`. Detectors are whole-script: `release_gate_test.go` runs the
real scripts against PATH shims and asserts the sign/publish/install/restart
sinks are NEVER reached on a refused binary, with a clean-run control (not
always-fail). Non-vacuous and RED-capable.

### Slice A (F1 + F8-provider) — closed at the owner boundary

`internal/foundation/egress` is the ONE E11 owner: canonical endpoint unit,
pinned literal-IP `DialContext` that classifies EVERY resolved address and
refuses the whole set on any policy breach (no pick-the-good-one), `Proxy:nil`,
reject-all redirects, MANDATORY receipt sink (nil → construction error), and a
receipt failure fails the PERMITTED dial closed (`egress.go:256-258`). The full
deny floor, `v4EmbedPrefixes` (NAT64/RFC6145/compatible), and `forbiddenSpecialUse`
are present. `provider` builds its client ONLY through it; a whole-repo grep finds
no remaining `http.DefaultTransport` or bare `&http.Client{`. `readBounded`
(max+1 → detect over-limit) and the 32 MiB transport stream ceiling are correct.
`channel.EgressSink` journals one `egress_attempt` per decision (component added,
validator requires it) and returns the append error so the dial fails closed.

### Slice C (F5 + F8 reads) — closed at the owner boundary

`budget.EnforceWire` measures the FINAL wire messages (`WireMessage{role,content}`,
including system/tool/time text) and refuses with `ErrOverBudget`; `ErrNoBudget`
backstops a non-positive limit (and `planner.New` refuses `<=0`). `Plan` enforces
it immediately before `opID()`/grant issue — `TestPlanRefusesOverBudgetWireBeforeAnyGrant`
proves blocks-fit-but-wire-over → zero grants, zero calls. The UDS `readFrame`
(`daemon.go:140-168`) bounds one frame at 1 MiB before allocation and CLOSES on
over-long (no residual-byte desync). The planner accumulator ceiling (8 MiB) sits
below the provider transport ceiling (32 MiB) and `TestStreamCutAtAccumulatorCeiling`
proves the cut with no partial final.

### Slice E (F7 config-aware doctor) — closed at the owner boundary

`doctor.DefaultEnv` now takes `Secrets{ProviderKeyEnv, TelegramTokenEnv}`, and
`cmd/nexus` `doctorChecks()` resolves configuration FIRST (a non-resolving config
is itself a finding) and passes the resolved names. `checkSecret` reports the
CONFIGURED env var (empty name → OFF misconfiguration, unset → OFF with the exact
export). Grep confirms `NEXUS_API_KEY`/`NEXUS_TELEGRAM_TOKEN` survive only as the
`defaults()` values and one comment — no hardcoded probe path.

## Notes (not FAIL reasons)

1. `release-gate.sh` `version_ge` uses POSIX `[ "$a3" -ge "$b3" ]`; a 4-component
   version would shell-error, but the `go[0-9]*.[0-9]*.[0-9]*` case restricts the
   parse to `X.Y.Z` and refuses anything else, so it fails closed before reaching
   the arithmetic. The test's Go `versionGE` handles it cleanly.
2. `readFrame` accepts a frame of exactly `max` bytes including the trailing
   newline, so the effective JSON payload is `max-1` bytes; cosmetic off-by-one.
3. `provider.Probe` uses a non-durable operation id (`provider-probe-<nanos>`); a
   probe is turn-scoped and in-memory, so this is correct, just undocumented.

VERDICT: PASS
