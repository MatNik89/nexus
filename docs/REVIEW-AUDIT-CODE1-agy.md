# Code Review: Audit Remediation Slices F, A, C, E

**Reviewed Revision**: `68bdd88e00c1dbe2a906fb24653906e7048c444a` (branch `slice/audit-e` in worktree `/home/matej/HARNESS/nexus-e`)  
**Diff Range**: `git -C /home/matej/HARNESS/nexus-e diff fdb39dc..68bdd88` (commits `9cb335b`, `32ae84f`, `ccd2d79`, `68bdd88`)  
**Verdict**: `PASS`  
**Plan Under Review**: [`docs/PLAN-AUDIT-FIXES.md`](file:///home/matej/HARNESS/nexus/docs/PLAN-AUDIT-FIXES.md) (v12, Slices F, A, C, E)  
**Source Audit**: [`docs/AUDIT-FULL-codex-2026-09-08.md`](file:///home/matej/HARNESS/nexus/docs/AUDIT-FULL-codex-2026-09-08.md) (Findings F1, F3, F5, F7, F8)  
**Binding Constitution**: `AGENTS.md`, [`docs/ARCHITECTURE-ESSENTIALS.md`](file:///home/matej/HARNESS/nexus/docs/ARCHITECTURE-ESSENTIALS.md), [`docs/HARNESS-SPEC.md`](file:///home/matej/HARNESS/nexus/docs/HARNESS-SPEC.md)

---

## 1. Per-Slice Disposition & Verification

### Slice F — F3: Release Toolchain Floor & Govulncheck Gate
- **Owner & Architecture**:
  - `go.mod` specifies `toolchain go1.26.6` ensuring compliant build toolchains.
  - `scripts/lib/release-gate.sh` establishes the single shared release gate function `release_gate "$BIN"` sourced by both `scripts/p0-accept.sh` and `scripts/deploy.sh`.
  - Verifies binary-recorded toolchain version (`go version "$BIN"`) $\ge 1.26.6$ and executes `govulncheck -mode=binary "$BIN"`. Fails closed on version parse failures, non-numeric toolchain strings, missing `govulncheck`, scanner errors, or reported vulnerabilities.
  - Placed strictly before grading/signing in `scripts/p0-accept.sh:70` and before install/restart in `scripts/deploy.sh:21`.
- **Detectors & RED Proof**:
  - `cmd/nexus/release_gate_test.go` implements 5 red-capable whole-script shim tests testing toolchains below floor, scanner findings, missing scanner, clean controls, and deploy script orchestration.
- **Status**: **CLOSED**.

### Slice A — F1 + F8(provider): Shared E11 Egress Owner & Bounded Buffers
- **Owner & Architecture**:
  - `internal/foundation/egress` implements the unified E11 egress owner: IP pinning, deny-default allowlist verification via canonical `Endpoint{Host, Port}`, special-use / IPv6-embedded address rejection, `Proxy: nil` (ignoring ambient proxy environment variables), redirect rejection (`CheckRedirect`), and mandatory `ReceiptSink`.
  - `ErrPreWire` exported sentinel wraps all DNS refusals, policy rejections, and receipt persistence failures.
  - `internal/llm/provider/provider.go` builds `http.Client` exclusively via `egress.NewPinnedClient("provider", ...)`.
  - Bounded input buffers: Chat response bodies bounded by `maxBodyBytes` (8 MiB) via `readBounded` (`io.LimitReader(r, max+1)`) with strict single JSON decoding (`decodeSingle`) requiring `io.EOF`. Streaming bodies bounded at transport layer by `maxStreamBytes` (32 MiB) via `io.LimitedReader`.
  - `internal/channel/telegram/telegram.go` deletes private `dialer.go` and constructs clients directly through `egress.NewPinnedClient("telegram", ...)`.
  - Composition root (`cmd/nexus/main.go:531`) builds single journal-backed `channel.EgressSink(j)` appending `channel.egress_attempt` events with component attribution.
- **Detectors & RED Proof**:
  - Verified 8 detectors across `internal/foundation/egress/egress_test.go`, `internal/llm/provider/egress_test.go`, and `internal/channel/telegram/egress_test.go`.
- **Status**: **CLOSED**.

### Slice C — F5 + F8(reads): Context Budget at Wire & Bounded Trust Reads
- **Owner & Architecture**:
  - `internal/foundation/config/config.go`: `ContextHardLimitTokens` added with default `64000`; `ValidateBounds` rejects $\le 0$ or $> 2\,000\,000$.
  - `internal/kernel/budget/budget.go`: Implements `MeasureWire` and `EnforceWire` operating on `[]WireMessage`, strictly returning `ErrOverBudget` or `ErrNoBudget` on breach without trimming or truncation.
  - `internal/llm/planner/planner.go`: Measures final assembled wire messages (system prompt, tools, time text, history) immediately before grant issuance and provider dispatch (`planner.go:308-314`).
  - Stream accumulation ceiling: `planner.go:363` enforces `maxStreamTotal` (8 MiB) on delta accumulation, canceling the stream fail-closed on overflow.
  - UDS frame bounds: `internal/app/daemon/daemon.go` implements `readFrame` bounding hello and chat frames to `maxFrameBytes` (1 MiB), returning an error frame and terminating the connection on overflow to prevent frame desynchronization.
- **Detectors & RED Proof**:
  - Verified 4 detectors across `internal/llm/planner/budget_test.go`, `internal/foundation/config/config_test.go`, and `internal/app/daemon/daemon_test.go`.
- **Status**: **CLOSED**.

### Slice E — F7: Config-Aware Doctor
- **Owner & Architecture**:
  - `cmd/nexus/main.go`: `doctorChecks()` resolves typed configuration from `XDG_CONFIG_HOME/nexus/config.json` before running checks, reporting unresolvable configuration as a failing check rather than crashing.
  - `internal/preflight/doctor/doctor.go`: Accepts `Env.Secrets` (`doctor.Secrets{ProviderKeyEnv, TelegramTokenEnv}`) and inspects the configured environment variable names without hardcoded fallbacks to default names.
- **Detectors & RED Proof**:
  - Verified table-driven 3-case detector in `cmd/nexus/doctor_config_test.go` covering custom names populated, custom names unpopulated with defaults populated, and default names populated.
- **Status**: **CLOSED**.

---

## 2. Standards & Spec Evaluation

### Standards Axis
- **Hard Invariants**: 0 violations. Clean DAG, strict E11 egress owner encapsulation, fail-closed sentinels, single journal write owner, typed configuration, and zero `map[string]any`.
- **Test Suite**: `CGO_ENABLED=0 go test ./...` in `/home/matej/HARNESS/nexus-e` passes 100% across all packages.

### Spec Axis
- **Requirements**: All requirements from Plan v12 for Slices F, A, C, and E are fully met at the owner boundaries.
- **Scope Creep**: 0 unrequested abstractions or speculative hooks.

---

## 3. Numbered Findings & Notes

**Zero substantive flaws found.**

### Notes & Baseline Smells (Non-Blocking)
1. **Smell (Feature Envy) — `internal/foundation/egress/egress.go:98`**:
   - `func Admitted(e Endpoint, egressAllow []string) bool` inspects multiple `Endpoint` fields. Could be defined as a method `(e Endpoint) Admitted(allow []string) bool`.
2. **Smell (Primitive Obsession) — `scripts/lib/release-gate.sh:14-22`**:
   - `version_ge` parses 3-part dotted numeric versions via shell parameter expansion. Protected by the regex check at line 34.
3. **Observation (Deploy Journal Settle Window) — `scripts/deploy.sh:18,26-27`**:
   - `deploy.sh` waits `NEXUS_DEPLOY_SETTLE_SECONDS` (default 3s) before grepping the last 50 journal lines for `"sealed capability ON"`. Under high startup load, the settle window or line count can be tuned via the existing environment variable.

---

VERDICT: PASS
