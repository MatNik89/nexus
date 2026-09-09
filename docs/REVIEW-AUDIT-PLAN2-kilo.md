# Review of audit-remediation plan v2 (`docs/PLAN-AUDIT-FIXES.md`)

- **Reviewed revision**: `c89897b` on `slice/p1-audit`
- **Verdict**: FAIL

## Round-1 disposition

- **R1-F1 (Slice B misnamed the P2/P3 retry engine as "s7min")** — CLOSED.
  The plan now writes Slice B to the P0 no-retry contract explicitly: "`s7min`
  UNCHANGED", one grant per row, consumed immediately before `client.Do`,
  `FAILED` as a new durable terminal state, and the `Reconcile(id,false) ->
  PENDING` self-repend path removed (`PLAN-AUDIT-FIXES.md:84-124`). This no
  longer attributes bounded-retry/backoff to s7min. Verified against
  `internal/kernel/s7min/s7min.go:3-9` (no-retry, one grant per operation) and
  `:97-128` (`Issue` grants `AttemptNo: 1`, second `Issue` refused).

## New findings

### F1 (substantive — B2 at-least-once violation; the tradeoff is incomplete)

Slice B Option 1 parks **transient** failures as the terminal `FAILED` state and
removes auto-resend. The plan's "DEFINITE failure" bucket is: "pre-wire refusal,
DNS/dial error, terminal HTTP 4xx, or any non-ambiguous transport error ->
`FAILED`" (`PLAN-AUDIT-FIXES.md:99-103`).

This collapses two different failure classes into one terminal state:

- **Permanent** (terminal HTTP 4xx, pre-wire refusal): parking terminal is
  correct — a 4xx will never succeed on retry. The current code wrongly re-pends
  it (`channel.go:583-589`, the `default:` branch `Reconcile(id,false)`), which is
  exactly the F2 "terminal 4xx does not loop" defect.
- **Transient** (DNS/dial error, non-ambiguous 5xx): these are the at-least-once
  path. The constitution binds "at-least-once remote delivery" as HARDQ B2
  (`AGENTS.md` delivery-honesty clause). Option 1 parks a single DNS blip or 5xx
  as terminal `FAILED` with "nothing auto-resends" (`:108-111`), so an admitted,
  durably-inboxed message is never delivered on a transient failure — a direct
  B2 violation, not an operational nicety.

The stated tradeoff (`:108-111`) is honest about the *mechanism* ("nothing
auto-resends") but incomplete about the *consequence*: it never names that this
breaks the locked at-least-once HARDQ, and it frames the decision as
"OWNER DECISION … pending confirmation" (`:85`) — an owner cannot waive a HARDQ,
so that framing is illegitimate for this clause.

Concrete fix — pick one and make it binding:
1. Split the bucket: permanent (4xx, pre-wire) -> `FAILED`; transient (DNS/dial,
   5xx) -> bounded retry, which is the P2/P3 engine — i.e. Option 2 is a HARD
   dependency of closing F2, not a deferred "later slice". Order it now, or
   name Slice B as an explicit at-least-once violation with a non-optional
   follow-up.
2. If Option 1 must land first as a stopgap, the plan must STATE that it
   knowingly suspends HARDQ B2 for transient failures and record it as a
   temporary regression with a bound, not as an owner-confirmed tradeoff.

## Verified correct (no new substantive flaw)

- **Slice A (F1 + F8-provider)** — correct. `provider.go:132-141` builds
  `&http.Client{Timeout, CheckRedirect: refuse}` with no `Transport` (default
  transport, E11 violation). The shared `internal/foundation/egress` owner with a
  MANDATORY `ReceiptSink` (nil sink = construction error, no silent no-receipt
  mode), exported `ErrPreWire`, and the receipt event gaining a `component` field
  while keeping `channel.egress_attempt` compatibility is sound; the
  `max+1`-then-reject body bound is the correct over-limit detector.
- **Slice C (F5 + F8-reads)** — correct. It catches the real subtlety
  (`budget.go:50-56` `HardLimit<=0` disables enforcement; planner adds
  system/tool/time text AFTER the block measure, `planner.go:253-287`) and fixes
  it by measuring the FINAL wire messages; over-budget REFUSES (`ErrOverBudget`),
  never trims (S8.2 deferred). UDS frame bound and stream ceiling are right.
- **Slice D (F6)** — correct. The health record is failure-independent
  (`atomicwrite.Write` to a file, not the journal), so it can report the
  journal-failure case; `Run` returns an error and stops on `substrate`, reporting
  (never fallback) per the sealed-capability rule.
- **Slice E (F7)** — correct. Resolve-config-then-pass-`doctor.Secrets` with the
  three-case table (false-negative, false-positive, regression) is complete.
- **Slice F (F3)** — correct. `go.mod:3` is `go 1.26` and `p0-accept.sh:66`
  builds with the ambient toolchain; the floor + `govulncheck -mode=binary` gate
  at the sign boundary with whole-script shim detectors is right.

## Notes (not FAIL reasons)

- Slice A: the provider host must be in `egress_allow` (deny-default) — the
  shared owner inherits the config requirement already noted for the telegram
  dialer.
- Slice B: a pre-wire egress *refusal* is a security event (poisoned DNS), not
  merely a delivery failure; parking it `FAILED` is fine, but it should also be
  surfaced distinctly so it is not mistaken for a routine 4xx.
- Slice A `component` on the receipt payload touches the `channel.egress_attempt`
  validator + projection; the plan implies but does not spell out that change.

VERDICT: FAIL
