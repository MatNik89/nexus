# REVIEW-PHASE4-R2 — round-2 verification (codex 7H/8M + kilo 2M + agy folds)

Scope: the fold diff b298083..f1d3889 + NEW defects it introduced. `CGO_ENABLED=0 go vet ./...`
clean; `CGO_ENABLED=0 go test -count=1 ./...` GREEN.

---

## My round-1 findings — both folded

**1. [OK] DST ONCE_FIRST (kilo #1 / codex #8).** `dueUTC` now scans the plausible fold window
minute-by-minute (up to 3h) and takes the EARLIEST instant naming the wall time (schedule.go:89-100),
so 30-minute (Lord Howe) and historic non-hour folds are covered, not just the one-hour case; the
civil-date roundtrip rejects `2026-02-30` before persistence (schedule.go:67-73). ✓

**2. [OK] Ceremonial MarkAcked checker (kilo #2 / codex #2).** `MarkAcked` now reads `delivered_at`
persisted from the durable `obligation.delivered` event's `EmittedAt` and grades the checker with
`DeliveryEvidence{DeliveredAt: deliveredAt}` — real event-backed evidence, not `now-1`. A missing
delivery receipt is refused up front (`deliveredAt.IsZero()`), and the ack carries the real `AckAt`.
✓

## Codex spot-check — all folded

- **#1 fire↔admission window** → `schedule.FireDecorator` / `obligation.FireParams` join DUE +
  DELIVERY_PENDING into the SAME `AppendBatch` as `occurrence_fired` + `run.created` + `run.admitted`
  (schedule.go:243, 391-397; obligation FireParams). No crash window. ✓
- **#3/#4 governed task tool** → `RunTask` dispatches a sealed in-process tool THROUGH the EffectPath
  (`EffectRunner.RunTool` — S6.0 decision + S7 grant + receipt); handlers never execute directly. ✓
- **#5/#6 postcondition** → the handler writes an IMMUTABLE per-task marker line, `MarkTaskDone`
  grades `FileContainsLine` (new `FileLineCriterion`/`FileLineEvidence` in checker.go), and
  `probableMarker` + a `Reconciled` flag recover the marker after a crash between write and append. ✓
- **#7 persisted UTC** → `createdPayload.DueUTC` is resolved ONCE at admission and persisted; `Sweep`
  evaluates `now.Before(time.Unix(0, dueNano))` against the stored instant, never recomputing
  (schedule.go:124-127, 359). ✓
- **#9/#10 civil + overdue** → civil roundtrip rejects impossible dates; the overdue tolerance IS the
  sweep interval (not a hidden 60s), with boundary REDs. ✓
- **#11 C8 mirror** → `mirrorCounter` reconciles the counter file from the authoritative projection on
  EVERY sweep, self-healing after a crash. ✓
- **#12 `_txlock=immediate`** → the DSN now opens with `_txlock=immediate` (journal.go:158). ✓
- **#13 sealed kind validator** → the admission validator and `CreateTask` reject unknown kinds
  against the registry. ✓
- **#14/#15 causal REDs + spine** → `TestReminderSpineAcrossRestart` crosses the production
  composition root; fold/restart/atomicity REDs are now causal. ✓

## NEW defects (all LOW)

**3. [NEW-ERROR, LOW] The concurrent-fire detection is a string match on the error text**
(`strings.Contains(err.Error(), "ALREADY_FIRED")`, schedule.go:400). Correct because the constant is
shared, but a typed sentinel (`errors.Is`) would survive an error-wrapping change.

**4. [NEW-ERROR, LOW] `dueUTC`'s fold window is a fixed 3-hour scan.** Adequate for every real
2000–2100 DST shift (≤2h), but a hypothetical larger shift would miss its earlier instant. The
ceiling is documented in the comment, not a contract violation.

**5. [NEW-ERROR, LOW] `mirrorCounter` discards the `atomicwrite.Write` error** (schedule.go:419). The
DB counter is authoritative, so this is observability drift only; a write failure is not surfaced to
health.

## Declared ceilings — judged acceptable

- verifier-read→DONE gap tolerates external truncation: replay determinism forbids fs reads inside
  the projection tx, and the marker-line artifact is append-stable — sound.
- D-Bus PrepareForSleep hook deferred until a consumer proves the tick insufficient — sound.

---

## Verdict

All of my round-1 findings and the codex/agy findings are folded with causal REDs: the fire and
obligation admission are one atomic batch, the ack grading consumes real durable delivery evidence,
task execution is governed through the EffectPath, the postcondition is an immutable marker line,
the promised UTC instant is persisted, and the scheduler/obligation REDs now cross the production
spine. The three new observations are low-severity. Full suite green.

VERDICT: PASS
