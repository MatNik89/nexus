# REVIEW-PHASE4-R4 — round-4 verification (7 codex r3 folds)

Scope: the diff 14f0b65..ee93c46 + NEW defects it introduced. `CGO_ENABLED=0 go vet ./...` clean;
`CGO_ENABLED=0 go test -count=1 ./...` GREEN.

---

## The 7 folds — all [OK]

**1. [OK] Durable ack gesture at canonical admission.** `EvAcked` now requires a non-empty `Source`
and persists `acked_by` (obligation.go:344-352); `EvDelivered` requires `Producer`+`ReceiptID` and
persists `delivered_at`/`delivered_by` (obligation.go:332-343). Replay can now establish WHO
acknowledged/delivered, and the projection rejects a producer-only transition. ✓ (codex r3 #1)

**2. [OK] In-flight lease closes the claim→effect window.** `inflight sync.Map` +
`LoadOrStore`/`Delete` serializes per-task execution within the process (obligation.go:816-826); a
second claimant sees `busy` and reconciles instead of re-dispatching. **Judgment:** the single-writer
premise is sound for P0 — the journal's cross-process writer lease guarantees ONE process, and the
in-process map guarantees ONE executor goroutine per task; a crash empties the map but the durable
`intent_op` + the deterministic-marker reconcile path recover it. ✓ (codex r3 #2)

**3. [OK] `Events(reg)` journal-boundary sealing.** `Events` now takes the `Registry` and wires each
kind's `ValidateParams` into the admission validator, plus a slug-id check — so a forged producer
cannot admit `kind:file_note, params:"not-json"` or a non-slug id. ✓ (codex r3 #5/#9)

**4. [OK] Projection marker RECOMPUTATION — forged attestation dead, replay pure.**
`EvTaskExecuted` recomputes `expectedMarker(id, task_params)` (a pure string function, no fs read, no
clock) and rejects any attestation whose `MarkerLine`/path differ (obligation.go:386-409);
`EvTaskDone` requires the SAME attested marker plus a non-worker `Verifier` identity
(obligation.go:447-454). A valid-claim forged attestation or a marker-less DONE aborts the append. ✓
(codex r3 #10)

**5. [OK] Typed `MIGRATION_REQUIRED` + `Version()` 3.** Legacy `task_executed` without
claim/marker and legacy `task_done` without verification identity fail loudly
(`MIGRATION_REQUIRED`), never reinterpreted; the schedule's v1 `schedule.created` without `due_utc`
also fails `MIGRATION_REQUIRED` (replacing the earlier tzdata-dependent upcast, so replay stays
deterministic). `Version()` is 3. ✓ (codex r3 #12, kilo r3)

**6. [OK] Health/doctor observability honest.** Sweep failures land in `system/scheduler_health`
(`Scheduler.Health()`), doctor maps a non-empty file to `StatusOff`, and `TestMirrorFailureSurfaces`
proves a mirror-write failure reaches the caller and `Health` while the fire stays durable. ✓ (codex
r3 #4)

**7. [OK] SIGKILL barrier test is causal.** The child writes a `ready` file after its FIRST fire (a
real barrier), the parent asserts SIGKILL terminated the child (never a clean exit), and the
BOTH-or-NEITHER oracle requires a non-empty pre/post workload. ✓ (codex r3 #6)

## NEW defects — none material

- **LOW:** the DONE event's `Verifier` is a self-declared string (a producer could compute the
  deterministic marker and append DONE with a fake verifier without the fs read) — this is the
  declared "verifier-read→DONE gap tolerates external truncation" ceiling (replay determinism forbids
  fs reads in the projection tx), not reopened.
- **LOW:** `expectedMarker` lives in both the handler and the projection; a future divergence between
  the two would false-reject attestations. Currently identical and covered by the forge RED.

---

## Verdict

All seven round-3 findings are correctly folded: the ack gesture and delivery receipt are durable and
admission-enforced, the claim→effect window is closed by an in-flight lease under the sound
single-writer premise, kind/id sealing lives at the journal boundary, the projection recomputes the
marker to kill forged attestations (replay-pure), legacy shapes fail with a typed
MIGRATION_REQUIRED, observability surfaces real failures, and the SIGKILL test is barrier-causal. The
two new observations are low-severity. Full suite green.

VERDICT: PASS
