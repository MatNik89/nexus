# Review of the full audit-remediation stack (`slice/audit-b`)

- **Reviewed revision**: `f33bc9a` on `slice/audit-b` (worktree `/home/matej/HARNESS/nexus-b`)
- **Diff**: `fdb39dc..f33bc9a` (F, A, C, E + B1, B2, B3, D + r1 folds)
- **Verdict**: PASS

Build clean; `s7`, `channel`, `channel/health`, `channel/telegram`, `provider`,
`planner`, `app/daemon` suites all pass.

## Round-1 disposition

My round-1 was PASS with three notes; codex round-1 was FAIL with three findings.
All three codex findings are closed:

- **F floor override** — `readonly RELEASE_GO_FLOOR=1.26.6` (`scripts/lib/release-gate.sh:13`), no `${...:-}` caller override.
- **Canonical receipt identity** — every `Decision` now carries `d.endpoint.Host` / `d.endpoint.Port`, and resolution uses `d.endpoint.Host` (`internal/foundation/egress/egress.go:240,245,248,265`).
- **Budget detector non-RED** — `TestPlanRefusesOverBudgetWireBeforeAnyGrant` injects `p.newOp` and asserts `auth.State("op-budget-known")` reports no operation after refusal (`internal/llm/planner/budget_test.go:35,49`).

## Per-slice disposition

### B1 (S7 engine) — closed at the owner boundary

`internal/kernel/s7` is the sole grant issuer: `Policy` (SPEC P0.2 ExecutionPolicy),
`Next(op, build)` with lease recovery and one-batch exhaustion/deadline landing
(`s7.go:348-412`), `Consume(g, companions...)` enforcing cardinality + `Key==op` and
committing the durable STARTED + companion in ONE batch before the wire
(`:420-452`), `Report/Cancel/Reconcile` with the Landing builder and one-batch
paired commits (`:460-577`, `events.go:406-451`), `Execute` as the S7-owned driver
with the unconsumed-grant refusal and frozen-clock fail-closed wait
(`execute.go:25-110`). Rehydration lands a durable STARTED-no-report RUNNING as
UNKNOWN (E9) and marks a no-STARTED AUTHORIZED lease for revocation
(`events.go:333-401`); `Reconcile` is the only UNKNOWN exit (`:406-451`).
`s7min` is deleted; `Issue` is preserved as `Begin(PolicyTool)+Next`.

### B2 (delivery/poll/registration) — closed at the owner boundary

`Core.Flush` (`channel.go:847-939`) drives each PENDING row through
Begin → Next(op, build) → send(g, park companion) → Cancel (unconsumed local
refusal) / Report (S7 decides SENT / re-pend / FAILED / UNKNOWN), with the
companion committed in the SAME batch; a Report failure leaves the row UNKNOWN
(`:922-926`) and a sent-mark death is injected and asserted UNKNOWN (`:917-921`).
UNKNOWN is never re-granted; `redeliver` starts a new generation. `OperationFor`/
`TargetFor` bind every grant to the immutable delivery resource, and the adapter
recomputes identity before Consume.

### B3 (provider + structured via Execute) — closed at the owner boundary

The planner submits ONE `s7.Execute` per turn for buffered and streaming paths
(`planner.go:192-241`); the grant is consumed inside `provider.transport`
immediately before `client.Do` (`provider.go:238-265`); a stream failure after the
first delivered delta is TERMINAL (`:231-233`). `provider.Classify` maps the closed
`Failure` vocabulary to retryable/terminal proposals (`:211-221`). A whole-repo
grep finds no `client.Do` outside the two egress-governed clients and no retry loop
in the planner/loop.

### D (health owner) — closed at the owner boundary

`internal/channel/health` keeps the closed class set; `Report` renders the TYPED
class (never text-inferred) and writes a failure-INDEPENDENT projection via
`atomicwrite` (`health.go:88-143`). The supervisor (`telegram.go:645-696`) uses
`channel.ClassOf(err)`, returns fatally on `remote_rejected`/`substrate`, degrades
on transport, and never overwrites an originating class.

## Notes (not FAIL reasons)

1. **Buffered-chat connection reset is terminal.** `provider.transport` classifies
   any non-dial/DNS `client.Do` error as `transport_postwrite` (not retryable), so
   a transient reset between request-sent and response fails the turn rather than
   retrying once. `PolicyProvider` lists only `{http_429, http_5xx,
   transport_prewire}`, matching the plan's "anything after the body started is
   terminal" — conservative for a read-only chat (a reset before the request was
   flushed is genuinely retryable) but defensible and not a plan divergence.
2. `health.Owner` reports `stopped` only on fatal classes; a transport degradation
   is recorded `Healthy:false, stopped:false` and the tick keeps asking S7 — the
   sealed-capability "report, never fallback" rule holds.

VERDICT: PASS
