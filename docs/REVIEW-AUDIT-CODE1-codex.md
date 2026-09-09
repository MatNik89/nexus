# Adversarial code review — audit remediation slices F, A, C, E

Reviewed revision: `68bdd88e00c1dbe2a906fb24653906e7048c444a` (`slice/audit-e`), diff `fdb39dc..68bdd88`

Verdict: **FAIL**

The implementation closes most of F1/F3/F5/F7/F8, but it does not yet satisfy the converged plan and its detector contract. One release path can lower the mandatory Go floor, the shared egress owner does not use its canonical endpoint for receipt identity, and the main ContextBudget detector cannot detect a grant issued before refusal.

## Per-slice disposition

### Slice F — FAIL

`go.mod:3-5` pins `toolchain go1.26.6`. Both release paths source the same helper and invoke it on the exact just-built binary before their first protected sink: `scripts/p0-accept.sh:14-16,68-71,78-97` gates before grading/signing/publishing/installing, and `scripts/deploy.sh:11-25` gates before service stop/install/restart. `scripts/lib/release-gate.sh:24-48` fails closed on an absent binary, an unparseable/non-numeric recorded toolchain, a missing scanner, scanner failure, or a finding. The whole-script tests cover the plan's old-toolchain, finding, missing-scanner, clean control, and deploy cases at `cmd/nexus/release_gate_test.go:96-198`, and bind the module directive to the audit literal `1.26.4 < 1.26.6` at `:200-227`.

The boundary is nevertheless bypassable through the inherited environment; see finding 1.

### Slice A — FAIL

The provider and Telegram now construct their clients only through the shared owner (`internal/llm/provider/provider.go:86-128`; `internal/channel/telegram/telegram.go:79-112`). The owner requires a receipt sink, disables ambient proxies, pins a classified resolution, rejects redirects, and receipts before dialing (`internal/foundation/egress/egress.go:221-259,262-312`). The composition root creates one journal-backed sink immediately after journal open and passes it to the provider and adapter (`cmd/nexus/main.go:519-532`; `cmd/nexus/main.go:162-186`). The channel validator and sink require and persist the component and allowed/refused fields (`internal/channel/channel.go:176-195,207-253`). A production-code sweep found no use of `http.DefaultClient`, `http.DefaultTransport`, package-level `http.Get`/`Post`, or an independently constructed `http.Client` outside the shared owner.

Provider buffered responses use a `max+1` read and strict single-value decode (`internal/llm/provider/provider.go:131-167,217-242`); streams have a transport-wide limited reader and reject termination without `[DONE]` (`:278-344`). The planned poison, proxy, mandatory/failed receipt, body-cap, stream-cap, deny-floor, endpoint-unit, TLS, and rebind detectors exist in `internal/foundation/egress/egress_test.go` and `internal/llm/provider/egress_test.go`. Telegram's sentinel mapping plus its real outbox test jointly prove an egress pre-wire refusal maps to the definite-failure/PENDING path (`internal/channel/telegram/egress_test.go:15-23`; `internal/channel/telegram/telegram_test.go:398-420`).

The canonical endpoint promise is not implemented for receipts, and the existing detectors miss that ablation; see finding 2.

### Slice C — FAIL (detector deficiency; implementation path is correct)

The typed config owner supplies 64000 by default and rejects non-positive or greater-than-2,000,000 values (`internal/foundation/config/config.go:54-59,130-138,369-377`), with resolver tests for the audit literals at `internal/foundation/config/config_test.go:274-326`. The composition root passes that resolved value into every planner (`cmd/nexus/main.go:600-625`). The planner builds the complete role/content message list, including tools, history, and time, then enforces the wire representation before generating an operation ID or issuing a grant (`internal/llm/planner/planner.go:269-320`). No trimming path exists.

Both UDS reads use the bounded reader; oversized hello and chat frames emit an error and return, closing the connection (`internal/app/daemon/daemon.go:140-170,182-197,238-245`). The aligned-suffix/closed-connection detector covers both phases (`internal/app/daemon/daemon_test.go:1208-1251`). The planner refuses stream growth before writing or delivering bytes beyond its accumulator ceiling (`internal/llm/planner/planner.go:359-369`), and the never-ending-stream detector is at `internal/llm/planner/budget_test.go:71-112`.

However, detector C1 does not prove its claimed pre-grant ordering; see finding 3.

### Slice E — PASS

The ordinary doctor calls the same `resolveEnv` path as the daemon and converts resolution failure into a fail-closed doctor finding, never default names (`cmd/nexus/main.go:719-765`). `doctor.Env` receives typed configured secret references, empty names fail closed, and lookup/fix output uses only those names (`internal/preflight/doctor/doctor.go:35-65,112-116,163-178`). The real composition-path table covers all three required directions—custom/custom ON, custom/default OFF, and default/default ON—and also covers unresolvable configuration without fallback (`cmd/nexus/doctor_config_test.go:11-78`). The owner-level table additionally checks empty names and secret non-disclosure (`internal/preflight/doctor/doctor_test.go:154-205`). Removing configured-name propagation or restoring either hardcoded lookup makes these tests RED.

## Findings

1. **HIGH — Slice F's mandatory Go floor can be lowered by the caller environment, allowing the audited vulnerable toolchain through both release paths.**

   Evidence: The plan fixes the floor at 1.26.6 and says no binary is signed, published, or installed unless that gate is green (`docs/PLAN-AUDIT-FIXES.md:627-642`). The helper instead assigns `RELEASE_GO_FLOOR="${RELEASE_GO_FLOOR:-1.26.6}"` and compares against that mutable value (`/home/matej/HARNESS/nexus-e/scripts/lib/release-gate.sh:11,37-40`). Therefore `RELEASE_GO_FLOOR=1.0.0` makes a binary reported as Go 1.26.4 pass the version half of the gate; a clean scanner then reaches the protected sinks in both callers (`scripts/p0-accept.sh:68-97`; `scripts/deploy.sh:20-25`). The test environment never supplies a hostile floor (`cmd/nexus/release_gate_test.go:60-80`), and `TestGoModToolchainMeetsGateFloor` explicitly matches only the default inside the override expression (`:203-225`), so the suite remains GREEN under this bypass.

   Concrete fix: make the production helper own an unconditional, preferably readonly, `RELEASE_GO_FLOOR=1.26.6`; do not accept a caller override. Add whole-script cases for both callers that set `RELEASE_GO_FLOOR=1.0.0` while the `go version` shim reports `go1.26.4`, and assert non-zero exit plus zero sign/publish/install/stop/restart sinks. Ablating the unconditional assignment back to an environment default must make those cases RED.

2. **MEDIUM — Slice A's receipt identity is not canonical, contrary to the single endpoint-unit contract, and detector A8 cannot detect it.**

   Evidence: The plan requires the lowercased/effective-port `Endpoint` to be used for allowlist matching, dial address, receipt `Host`, and S7 target (`docs/PLAN-AUDIT-FIXES.md:72-78`). `ParseEndpoint` does lowercase the configured hostname (`/home/matej/HARNESS/nexus-e/internal/foundation/egress/egress.go:70-91`), and the provider target uses that canonical value (`internal/llm/provider/provider.go:117,181-186`). But `DialContext` emits both refused and allowed receipts with its case-preserving `host` argument rather than `d.endpoint.Host` (`internal/foundation/egress/egress.go:225-256`). The provider intentionally retains the original-case base URL for request construction (`internal/llm/provider/provider.go:117,198-205`); Go `TestProviderTargetIsCanonicalEndpoint` already uses uppercase `API.Provider.example` (`internal/llm/provider/egress_test.go:200-210`). Thus target `provider:api.provider.example:443` can be paired with receipt host `API.Provider.example`, defeating the promised one-unit correlation.

   Detector gap: A8 checks `Endpoint.Canonical`, allowlist equivalence, and provider target (`internal/foundation/egress/egress_test.go:222-263`; `internal/llm/provider/egress_test.go:200-220`), while the durable Telegram receipt test checks component/allowed/pin but not the expected host (`internal/channel/telegram/egress_test.go:39-72`). Replacing canonical receipt use with the current case-preserving value therefore stays GREEN.

   Concrete fix: after validating the dial address against the endpoint, use `d.endpoint.Host` and `d.endpoint.Port` for every `Decision` (and use the canonical host for resolution). Add allowed and refused receipt assertions from an uppercase base URL that require lowercase host plus effective port, and bind the durable journal payload to the same literal. The test must turn RED if either receipt assignment is changed back to the transport-supplied host.

3. **MEDIUM — Slice C detector 1 cannot go RED when a grant is issued before an over-budget refusal.**

   Evidence: The plan requires over-budget handling before every physical attempt with no state advance and no grant issued or consumed (`docs/PLAN-AUDIT-FIXES.md:483-501`). Production currently has the correct order: `EnforceWire` returns at `internal/llm/planner/planner.go:308-314`, before `opID` and `Issue` at `:315-320`. But `TestPlanRefusesOverBudgetWireBeforeAnyGrant` asserts only `ErrOverBudget` and `fc.calls == 0` (`internal/llm/planner/budget_test.go:17-44`). A causal ablation that moves `Issue` immediately before `EnforceWire`, while still returning before `Chat`, leaves both assertions GREEN even though S7 now contains a live AUTHORIZED operation and the plan's no-state-advance invariant is broken.

   Concrete fix: give the planner a package-private injectable operation-ID source (defaulting to the existing secure generator), set a known operation ID in this detector, and assert `Authority.State(knownID)` reports no operation after refusal. Retain the provider-call assertion. Moving `Issue` before the budget check must then turn the detector RED for the intended reason.

## Verification performed

- Target identity: clean `slice/audit-e` worktree at `68bdd88e00c1dbe2a906fb24653906e7048c444a`.
- `git diff --check fdb39dc..68bdd88`: PASS.
- `CGO_ENABLED=0 go test -count=1 ./internal/foundation/egress ./internal/llm/provider ./internal/kernel/budget ./internal/llm/planner ./internal/app/daemon ./internal/foundation/config ./internal/preflight/doctor`: PASS.
- `CGO_ENABLED=0 go test ./cmd/nexus -run '^(TestDoctorUsesConfiguredSecretNames|TestGoModToolchainMeetsGateFloor)$' -count=1`: PASS.
- `go test ./...` was not run: the `cmd/nexus` release tests execute shell scripts containing recursive cleanup. The read-only audit protocol forbids executing such scripts without a destructive-command shim. This is a proof ceiling, not a claim that the full suite fails.
- Target worktree remained clean after verification. No test weakening was found in the reviewed range; the replaced Telegram dialer cases were substantially preserved under the shared owner.

Weakest link: the whole-script release detectors were assessed statically rather than executed because of the cleanup restriction. Their control flow is direct, but runtime shell behavior beyond the focused non-script tests remains unobserved in this review.

VERDICT: FAIL
