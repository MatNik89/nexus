# Review of audit-remediation plan (`docs/PLAN-AUDIT-FIXES.md`)

- **Reviewed revision**: `59c0ac5` on `slice/p1-audit`
- **Source audit**: `docs/AUDIT-FULL-codex-2026-09-08.md` (F1-F9)
- **Verdict**: FAIL

## Findings

### F1 (substantive — unrealizable design, violates the S7 constitution) — Slice B misnames the P2/P3 retry engine as "s7min"

The plan's Slice B fix is "Every physical resend requires a fresh bounded
`AttemptGrant` from `s7min` (deadline + attempt cap + backoff)" and its detector
is "a delivery that keeps failing stops after N bounded attempts"
(`PLAN-AUDIT-FIXES.md:40-46`).

`s7min` is explicitly the P0 **no-retry** slice:

- `internal/kernel/s7min/s7min.go:3-9` — "the no-retry policy collapses
  FAILED_RETRYABLE to FAILED_TERMINAL (one grant per operation — the full
  retry/backoff/fallback engine is P2/P3, and ONLY it may ever issue a second
  grant). Adapters and loops never retry."
- `s7min.go:97-128` — `Issue` grants exactly ONE attempt (`AttemptNo: 1`); a
  second `Issue` for the same operation returns `ErrAttemptNotAuthorized`
  ("already holds its one P0 grant (no-retry)").

So "N bounded attempts" with "backoff" is the P2/P3 retry engine, which the
constitution (`AGENTS.md` S7; `s7min.go:8-9`) reserves for P2/P3 and forbids the
-min slice from providing. The plan names `s7min` but describes the P2/P3
capability, and its detector ("stops after N attempts", N>1) is unrealizable
against the one-grant model — it cannot go RED against an implementation that
uses `s7min` as actually specified. The audit F2's own wording ("bounded
delivery attempt grant before every physical resend") makes the same unstated
assumption, and the plan repeats it instead of surfacing it.

Concrete fix — choose and state one:
1. **Scope the P2/P3 bounded-retry engine explicitly** (its own slice, ordered
   after the -min grants), and stop calling it "s7min"; or
2. **Use the `s7min` single-grant model as-is** for delivery (one grant per
   resend, retryable → FAILED_TERMINAL) and record the at-least-once tradeoff
   as an owner decision — a transient delivery failure would park terminal, not
   retry, which is itself a B2 delivery-contract change that must be explicit,
   not silent.

Until one is chosen, Slice B is not buildable as written and its detector cannot
be RED-capable.

## Verified correct (no substantive flaw)

- **F4 (96b0c49)** — `acceptance_linux_test.go` `daemon()` returns `func()` only;
  the `out.String()` read-during-run is deleted and all callers updated. Closes
  the race at the owner boundary.
- **F9 (96b0c49)** — `config.go:174` calls `contracts.HasDuplicateJSONKeys(b)`
  before the map decode; the detector exists at `contracts.go:578`.
- **Slice A (F1 + F8-provider)** — `provider.go:132-141` builds
  `&http.Client{Timeout: 120s, CheckRedirect: refuse}` with NO `Transport`
  (→ `http.DefaultTransport`: ambient proxy, unpinned DNS), so extracting the
  existing telegram `dialer.go` policy into a shared `internal/foundation/egress`
  owner (no default-transport fallback) is the right fix; `io.LimitReader` on the
  provider body is the right F8-provider half.
- **Slice C (F5 + F8-reads)** — `internal/kernel/budget` has zero production
  importers (grep-confirmed); enforcing one budget at the loop/planner boundary
  and bounding the UDS frame reader + streaming ceiling is correct.
- **Slice D (F6)** — the Run loop discards `PollOnce`/`FlushOutbox`/
  `registerCommands` errors (`telegram.go:433-450`); a channel-health owner that
  marks OFF on substrate errors is correct.
- **Slice E (F7)** — doctor hardcodes `NEXUS_API_KEY`/`NEXUS_TELEGRAM_TOKEN`;
  resolve-then-pass-env-names with both false-negative and false-positive
  detectors is complete.
- **Slice F (F3)** — `go.mod:3` is `go 1.26` and `p0-accept.sh:66` builds with
  the ambient toolchain (`CGO_ENABLED=0 go build`, no gate); a `>= 1.26.6` floor
  + `govulncheck -mode=binary` gate, with the host upgrade flagged as owner work,
  is correct.

## Notes (not FAIL reasons)

- Slice A: the provider host must be added to `egress_allow` (the shared dialer's
  deny-default) — a config requirement, same class as the telegram dialer note.
- Slice D: the health record's own substrate must be non-journal (a journal
  failure is precisely what it must survive to report), which the plan leaves
  unstated.
- Slice C: "trim/refuse per the budget contract" must specify how the loop's
  state machine tolerates a refused append without breaking the turn.

VERDICT: FAIL
