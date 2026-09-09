# Adversarial Code Review Round 2: Audit Remediation Stack (Slices F, A, C, E, B1, B2, B3, D)

- **Reviewer:** agy
- **Revision Under Review:** `f33bc9a03cf8280f4e1216063632b4129aed3a34` (worktree `/home/matej/HARNESS/nexus-b`, branch `slice/audit-b`)
- **Diff Range:** `fdb39dc..f33bc9a`
- **Plan Reference:** [`docs/PLAN-AUDIT-FIXES.md`](file:///home/matej/HARNESS/nexus/docs/PLAN-AUDIT-FIXES.md) (v12, converged)
- **Source Audit:** [`docs/AUDIT-FULL-codex-2026-09-08.md`](file:///home/matej/HARNESS/nexus/docs/AUDIT-FULL-codex-2026-09-08.md)

---

## 1. Executive Summary

The audit remediation implementation across Slices F, A, C, E, B1, B2, B3, and D cleanly implements the architecture specified in Plan v12. The full S7 retry engine, channel outbox transactional boundaries, governed Bot API calls with bot-bound command registration and reconciliation, structured LLM extraction salvage within attempt 2, and the channel health owner are sound and conform to the repo constitution (`AGENTS.md` and `docs/ARCHITECTURE-ESSENTIALS.md`).

However, **the acceptance test suite fails 6 critical tests** under `CGO_ENABLED=0 go test ./internal/acceptance` because the fake Telegram server mock in `internal/acceptance/acceptance_linux_test.go:220` omits the bot `id` field in its `/getMe` response handler. Because `internal/channel/telegram/telegram.go:742` strictly requires `me.ID != 0` to form bot-bound registration keys, the Telegram adapter terminates on startup as a fatal substrate failure, blocking outbound message delivery across the entire acceptance test suite.

---

## 2. Round-1 Finding Disposition

| Round-1 Finding | Implementation in `f33bc9a` | Status |
| :--- | :--- | :--- |
| **Slice F (Codex F1):** `RELEASE_GO_FLOOR` could be lowered via environment variable | [`scripts/lib/release-gate.sh:11`](file:///home/matej/HARNESS/nexus-b/scripts/lib/release-gate.sh#L11) defines unconditional `readonly RELEASE_GO_FLOOR=1.26.6`. Added detector [`cmd/nexus/release_gate_test.go:173-195`](file:///home/matej/HARNESS/nexus-b/cmd/nexus/release_gate_test.go#L173-L195) verifies floor override rejection. | **CLOSED** |
| **Slice A (Codex F2):** Egress receipt emitted raw un-canonicalized target | [`internal/foundation/egress/egress.go:237,265`](file:///home/matej/HARNESS/nexus-b/internal/foundation/egress/egress.go#L237) populates `Receipt.Target` strictly using `d.endpoint.Host` (canonical lowercase) and `d.endpoint.Port`. Added detector [`internal/foundation/egress/egress_test.go:153-176`](file:///home/matej/HARNESS/nexus-b/internal/foundation/egress/egress_test.go#L153-L176) verifies host normalization. | **CLOSED** |
| **Slice C (Codex F3):** Budget test did not assert absence of attempt grant | [`internal/llm/planner/budget_test.go:32,49-51`](file:///home/matej/HARNESS/nexus-b/internal/llm/planner/budget_test.go#L32) sets a known operation ID and explicitly asserts `auth.State(knownID)` does not exist after over-budget refusal, proving wire refusal precedes grant issuance. | **CLOSED** |

---

## 3. Per-Slice Code Verification

### Slice F: Toolchain Floor & Vulnerability Gating (F3)
- [`scripts/p0-accept.sh:22-26`](file:///home/matej/HARNESS/nexus-b/scripts/p0-accept.sh#L22-L26) and [`scripts/lib/release-gate.sh:11`](file:///home/matej/HARNESS/nexus-b/scripts/lib/release-gate.sh#L11) enforce Go >= 1.26.6 and run `govulncheck -mode=binary` on the compiled release artifact before signing and publishing.
- Verified cleanly.

### Slice A: Shared Egress Owner & Provider Transport Caps (F1, F8)
- [`internal/foundation/egress/egress.go:215-285`](file:///home/matej/HARNESS/nexus-b/internal/foundation/egress/egress.go#L215-L285) acts as the sole egress decision point, routing all outbound requests with mandatory receipt logging and no default transport fallbacks.
- [`internal/llm/provider/provider.go:189-220`](file:///home/matej/HARNESS/nexus-b/internal/llm/provider/provider.go#L189-L220) enforces `maxBodyBytes` (4 MiB) via `io.LimitReader` on all HTTP responses and streaming chunks.

### Slice C: Context Budget Wire Enforcement & Frame Limits (F5, F8)
- [`internal/kernel/budget/budget.go:61-98`](file:///home/matej/HARNESS/nexus-b/internal/kernel/budget/budget.go#L61-L98) and [`internal/llm/planner/planner.go:210-245`](file:///home/matej/HARNESS/nexus-b/internal/llm/planner/planner.go#L210-L245) enforce `ContextHardLimitTokens` at the wire boundary immediately before transport, failing closed on overflow without silent truncation.
- UDS frames and stream accumulators enforce strict size ceilings.

### Slice E: Config-Aware Doctor Preflight (F7)
- [`internal/preflight/doctor/doctor.go:75-140`](file:///home/matej/HARNESS/nexus-b/internal/preflight/doctor/doctor.go#L75-L140) resolves full runtime configuration before executing doctor diagnostic checks.

### Slice B1: S7 Engine (F2)
- [`internal/kernel/s7/s7.go:1-680`](file:///home/matej/HARNESS/nexus-b/internal/kernel/s7/s7.go#L1-L680) implements the complete S7 engine with durable journal events (`s7.operation_begun`, `s7.attempt_granted`, `s7.operation_landed`, `s7.operation_reconciled`), in-memory projection rehydration, mandatory companion validation on `Consume`, lease recovery, and frozen-clock spin prevention in `waitDue`.

### Slice B2: Governed Channel Outbox & Telegram Operations (F2)
- [`internal/channel/channel.go:210-380`](file:///home/matej/HARNESS/nexus-b/internal/channel/channel.go#L210-L380) implements outbox state machine (`PENDING`, `UNKNOWN`, `SENT`, `FAILED`) with paired atomic commits in `Core.Flush`.
- [`internal/channel/telegram/telegram.go:379-450,733-820`](file:///home/matej/HARNESS/nexus-b/internal/channel/telegram/telegram.go#L379) executes all Bot API calls through governed grants. Command registration is bot-bound (`control:tg:<bot-id>:setMyCommands:<hash>`) and reconciles on startup via `getMyCommands` using typed proof verification (`channel.ControlProof`).

### Slice B3: Provider & Structured Output via S7 (F2)
- [`internal/llm/provider/structured.go:85-180`](file:///home/matej/HARNESS/nexus-b/internal/llm/provider/structured.go#L85-L180) executes `ExtractVia` as a single S7 operation (`PolicyStructured`, MaxAttempts 2). Salvage is evaluated strictly inside attempt 2's outcome decision. Returned value <=> S7 `SUCCEEDED`.

### Slice D: Channel Health Owner (F6)
- [`internal/channel/health/health.go:45-120`](file:///home/matej/HARNESS/nexus-b/internal/channel/health/health.go#L45-L120) provides an isolated health owner writing `channel_health.json` via atomic writes independent of SQLite.

---

## 4. Substantive Findings

### Finding 1: [HIGH] Acceptance test suite failure due to missing bot ID in fakeBot getMe mock

- **Source Location:** [`internal/acceptance/acceptance_linux_test.go:219-220`](file:///home/matej/HARNESS/nexus-b/internal/acceptance/acceptance_linux_test.go#L219-L220)
- **Breaks:** Acceptance test suite (`CGO_ENABLED=0 go test ./internal/acceptance`)
- **Failed Tests:**
  1. `TestCriterion3ReminderDeliversAfterRestart` (timeout / sent=[])
  2. `TestCriterion3SensitivityNoChannel` (timeout / sent=[])
  3. `TestCriterion4TelegramHITL` (timeout / sent=[])
  4. `TestCriterion4SensitivityUnboundChat` (timeout / sent=[])
  5. `TestSealedOffStartupRunsNoConsumers` (timeout / sent=[])
  6. `TestChaosKillSurvival` (timeout / sent=[])

#### Mechanism:
Under Slice B2, [`internal/channel/telegram/telegram.go:734-745`](file:///home/matej/HARNESS/nexus-b/internal/channel/telegram/telegram.go#L734-L745) introduces bot identity discovery in `registerCommands`:
```go
if a.botID == 0 {
    var me struct {
        ID    int64 `json:"id"`
        IsBot bool  `json:"is_bot"`
    }
    if err := a.controlRead(ctx, "getMe", map[string]any{}, &me); err != nil {
        return err
    }
    if !me.IsBot || me.ID == 0 {
        return fmt.Errorf("telegram: token does not identify a bot (fail closed)")
    }
    a.botID = me.ID
}
```
In [`internal/acceptance/acceptance_linux_test.go:219-220`](file:///home/matej/HARNESS/nexus-b/internal/acceptance/acceptance_linux_test.go#L219-L220), `newFakeBot` responds to `/getMe` with:
```go
case strings.HasSuffix(r.URL.Path, "/getMe"):
    rw.Write([]byte(`{"ok":true,"result":{"is_bot":true}}`))
```
Because `"id"` is omitted from the JSON result, `me.ID` unmarshals to `0`. `registerCommands` returns `telegram: token does not identify a bot (fail closed)`.

In [`internal/channel/telegram/telegram.go:290-305`](file:///home/matej/HARNESS/nexus-b/internal/channel/telegram/telegram.go#L290-L305), this registration error is classified as fatal `ClassSubstrate`, causing the Telegram adapter in acceptance tests to terminate immediately during startup. As a result, no messages are received or sent, failing 6 acceptance tests.

(In contrast, [`internal/channel/telegram/telegram_test.go:168-173`](file:///home/matej/HARNESS/nexus-b/internal/channel/telegram/telegram_test.go#L168-L173) correctly provides `{"id": 1, "is_bot": true}` in its mock handler).

#### Concrete Fix:
In [`internal/acceptance/acceptance_linux_test.go:220`](file:///home/matej/HARNESS/nexus-b/internal/acceptance/acceptance_linux_test.go#L220), update the mock payload:
```diff
--- a/internal/acceptance/acceptance_linux_test.go
+++ b/internal/acceptance/acceptance_linux_test.go
@@ -217,7 +217,7 @@ func newFakeBot(t *testing.T) *fakeBot {
 	b.srv = httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
 		switch {
 		case strings.HasSuffix(r.URL.Path, "/getMe"):
-			rw.Write([]byte(`{"ok":true,"result":{"is_bot":true}}`))
+			rw.Write([]byte(`{"ok":true,"result":{"id":123,"is_bot":true}}`))
 		case strings.HasSuffix(r.URL.Path, "/getUpdates"):
```

---

## 5. Notes and Observations

1. **Telegram map reflection helper [`internal/channel/telegram/telegram.go:668`](file:///home/matej/HARNESS/nexus-b/internal/channel/telegram/telegram.go#L668):**
   `formatted(msg any)` performs type inspection over `map[string]any` to locate message text for rendering. While acceptable at the edge adapter boundary (where untrusted Bot API JSON is received), typed internal structures should remain preferred.

---

VERDICT: FAIL
