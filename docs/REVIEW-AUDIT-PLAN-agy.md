# Design Review: Full-Audit Remediation Plan (`PLAN-AUDIT-FIXES.md`)

**Reviewed Revision**: `59c0ac58331ed6639c59d29f7528abf4fc9cf373` (branch `slice/p1-audit`)  
**Verdict**: `FAIL`  
**Source Audit**: [`docs/AUDIT-FULL-codex-2026-09-08.md`](file:///home/matej/HARNESS/nexus/docs/AUDIT-FULL-codex-2026-09-08.md) (Findings F1–F9)  
**Plan Under Review**: [`docs/PLAN-AUDIT-FIXES.md`](file:///home/matej/HARNESS/nexus/docs/PLAN-AUDIT-FIXES.md) (Slices A–F)  
**Binding Constitution**: `AGENTS.md`, [`docs/ARCHITECTURE-ESSENTIALS.md`](file:///home/matej/HARNESS/nexus/docs/ARCHITECTURE-ESSENTIALS.md) (E11, S7 sole retry owner, S8.1 ContextBudget, fail-closed)

---

## 1. Executive Summary & Landed Fix Verification

A review of [`docs/PLAN-AUDIT-FIXES.md`](file:///home/matej/HARNESS/nexus/docs/PLAN-AUDIT-FIXES.md) against the source audit and current repository code reveals substantive design contradictions in Slices B and C, along with an incomplete transport-level read bound in Slice A:
- **Slice B** violates P0 scope guards by introducing a multi-attempt retry/backoff engine into `s7min` and specifies an improper batch grant at the adapter loop rather than per-delivery attempt authorization.
- **Slice C** proposes context "trimming" in direct contradiction to `ContextBudget-min`'s refusal-only contract (`budget.go:50-57`), and references an undefined configuration knob.
- **Slice A / C** bounds provider JSON decoding and planner accumulation, but leaves the streaming body scanner unbounded at the `provider.Stream` transport layer.

**Verification of Already-Landed Fixes (`commit 96b0c49`)**:
- **F4 (HIGH — Subprocess output race)**: **CLOSED**. In [`internal/acceptance/acceptance_linux_test.go:145-183`](file:///home/matej/HARNESS/nexus/internal/acceptance/acceptance_linux_test.go#L145-L183), the unused and racy `out.String()` read during daemon execution was dropped across all harness callers (`acceptance_linux_test.go`, `chaos_linux_test.go`, `soak_linux_test.go`).
- **F9 (LOW — Duplicate JSON config keys)**: **CLOSED**. In [`internal/foundation/config/config.go:168-175`](file:///home/matej/HARNESS/nexus/internal/foundation/config/config.go#L168-L175), `contracts.HasDuplicateJSONKeys(b)` is enforced prior to `json.Unmarshal`, backed by [`config_test.go:55-64`](file:///home/matej/HARNESS/nexus/internal/foundation/config/config_test.go#L55-L64) (`TestDuplicateJSONKeysRejected`).

---

## 2. Numbered Substantive Findings

### Finding 1 — HIGH: S7 P0 Scope Violation and Grant Granularity Mismatch in Slice B
- **Severity**: HIGH (Constitution violation / P0 scope creep)
- **Evidence**:
  - `docs/PLAN-AUDIT-FIXES.md:41-45`: Specifies "Every physical resend requires a fresh bounded `AttemptGrant` from `s7min` (deadline + attempt cap + backoff). The Run loop asks S7 for a grant before each flush; on grant-exhaustion the row parks... stops after N bounded attempts".
  - `AGENTS.md:57-60`: "S7/S5 enter P0 only as -min contracts: `s7-min` = AttemptGrant + cancel + no-retry (retryable -> FAILED_TERMINAL)... full S7 taxonomy/budgets/fencing = P2".
  - `internal/kernel/s7min/s7min.go:4-9, 97-105`: P0 `s7min` issues strictly one grant per operation; a second issue is rejected as `ErrAttemptNotAuthorized`.
  - `internal/channel/channel.go:554-591`: `Flush` iterates over multiple pending outbox rows (`o.DeliveryID`).
- **Problem**:
  1. *P0 Scope Guard Breach*: Injecting attempt caps, retry budgets, and backoff scheduling into `s7min` directly violates the P0 `-min` contract. P0 retries nothing; retries belong exclusively to P2.
  2. *Grant Capability Mismatch*: Asking S7 for a grant at the Run loop level before calling `FlushOutbox` breaks S7's exact capability binding (`OperationID`, `TargetID`). One batch grant cannot authorize multiple distinct outbound deliveries.
- **Concrete Fix**:
  1. Bind `s7min.Grant` at the individual delivery level: inside `channel.Core.Flush`, before `send(o)`, obtain an `AttemptGrant` for `OperationID(o.DeliveryID)` bound to `TargetID("channel:telegram:" + o.ChannelIdentity)`.
  2. Keep `s7min` strictly no-retry in P0: when an outbox delivery fails (HTTP 4xx or transport failure), consume the grant, report terminal failure to S7, and park the row in `UNKNOWN` / `RECONCILING` for human intervention or reconciliation. Do not call `c.Reconcile(ctx, o.DeliveryID, false)` to perpetually re-pend to `PENDING` (`channel.go:585`).

---

### Finding 2 — MEDIUM: ContextBudget Trimming Contradiction and Unspecified Schema in Slice C
- **Severity**: MEDIUM (Design contradiction & missing contract specification)
- **Evidence**:
  - `docs/PLAN-AUDIT-FIXES.md:53-56`: "enforce ONE configured hard ContextBudget at the loop/planner boundary before every provider attempt (trim/refuse per the budget contract)... over-budget context is refused/trimmed".
  - `internal/kernel/budget/budget.go:1-6, 50-57`: "`ContextBudget-min`... HARD limit that REFUSES on breach — NEVER a silent truncation... Enforce refuses when blocks exceed the hard limit. It NEVER truncates."
  - `docs/tasks-P0.md:86-87`: "`Enforce()` on over-budget refuses (err != nil), never truncates."
  - `internal/foundation/config/config.go:30-53`: `config.Config` contains no field for token limits.
- **Problem**:
  1. *Refusal Contract Contradiction*: `PLAN-AUDIT-FIXES.md` states "trim/refuse" and "refused/trimmed". Context compaction and trimming belong to S8.2. In P0, `ContextBudget-min` strictly REFUSES (`ErrOverBudget`), failing the turn closed.
  2. *Unspecified Config Schema*: The plan specifies a "configured hard ContextBudget" but does not define the schema knob in `config.Config` or its fallback default.
- **Concrete Fix**:
  1. Remove all references to "trim/trimming". Specify that `ContextBudget.Enforce` strictly fails the turn closed upon exceeding `HardLimit` by returning `ErrOverBudget`.
  2. Explicitly specify the config schema addition (e.g. `ContextHardLimitTokens int` in `config.Config`, bounds-validated in `ValidateBounds`, or define a sealed kernel default constant such as 8192 tokens wired at loop/planner construction).

---

### Finding 3 — MEDIUM: Unbounded Stream Body Reader at Provider Transport Layer in Slice A/C
- **Severity**: MEDIUM (Incomplete trust-boundary bounding / F8 gap)
- **Evidence**:
  - `docs/AUDIT-FULL-codex-2026-09-08.md:137-149`: "Provider buffered JSON decoding has no body limit at `internal/llm/provider/provider.go:193-213`. Streaming has a per-line scanner limit but accumulates the entire response without a total limit at `internal/llm/planner/planner.go:330-345`."
  - `docs/PLAN-AUDIT-FIXES.md:29-30, 54-55`: Slice A adds `io.LimitReader` only to `Chat` JSON decode (`provider.go:207`); Slice C bounds string accumulation in `planner.go:331-335`.
  - `internal/llm/provider/provider.go:272-302`: `provider.Stream` wraps `resp.Body` directly in `bufio.NewScanner(resp.Body)`.
- **Problem**:
  While Slice C bounds the planner's `strings.Builder` accumulator, `provider.Stream` itself remains unbounded at the HTTP transport layer. A pathological upstream sending a continuous stream of small SSE frames will cause `sc.Scan()` in `provider.go:275-302` to loop indefinitely if `deliver()` does not abort, consuming socket and CPU resources.
- **Concrete Fix**:
  In Slice A / C, wrap `resp.Body` in `io.LimitReader(resp.Body, maxStreamBytes)` inside `provider.Stream` before scanning, establishing defense-in-depth at both the provider transport layer and the planner accumulation layer.

---

## 3. Notes (Non-FAIL Design & Implementation Notes)

1. **Slice A (`Middle Man` Elimination)**:
   - In `PLAN-AUDIT-FIXES.md:26-27`, the plan states "Telegram's `newPinnedClient` becomes a thin wrapper". Telegram should instantiate the client directly via `egress.NewPinnedClient(...)` to avoid redundant wrapper functions.
2. **Slice D (Health Mirror Decoupling)**:
   - The channel-health owner should ensure that health state updates are non-blocking and cannot cause deadlocks if the journal or filesystem is experiencing high latency.
3. **Slice E (Doctor Env Lookups)**:
   - Doctor checks should accept an explicit configuration struct containing resolved variable names (`ProviderKeyEnv`, `TelegramTokenEnv`), ensuring deterministic resolution across CLI and daemon contexts.
4. **Slice F (Host Toolchain Upgrade)**:
   - Clear separation between the acceptance script's code gate (`>= 1.26.6` floor + `govulncheck`) and the host environment upgrade action is correctly preserved.

---

VERDICT: FAIL
