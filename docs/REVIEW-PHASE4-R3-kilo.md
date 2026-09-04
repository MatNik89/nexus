# REVIEW-PHASE4-R3 — round-3 verification (13 codex r2 folds)

Scope: the diff f1d3889..14f0b65 + regressions. `CGO_ENABLED=0 go vet ./...` clean; focused
`go test -count=1 ./internal/schedule ./internal/obligation ./cmd/nexus` GREEN.

---

## The key judgments

**(a) [OK] Projection-enforced CAS/attestation/done protocol is airtight.** `EvTaskIntent` claims via
`UPDATE … WHERE intent_op=''` (a losing concurrent claimant's append aborts inside the tx);
`EvTaskExecuted` requires `intent_op=? AND intent_op!='' AND marker_line=''` (attestation must MATCH
the claimed operation and land once); `EvTaskDone` requires `marker_line!=''`. A forged producer
(empty intent, re-attestation, done-without-marker) aborts at the projection. ✓ (codex #18/#5).

**(b) [OK] DeliveryReceipt / AckGesture are now real artifacts, under the declared ceiling.**
`MarkDelivered(ctx, occ, r DeliveryReceipt)` and `MarkAcked(ctx, occ, g AckGesture)` take structured
artifacts; `MarkAcked` grades the passed receipt's `DeliveredAt` and the gesture's identity — no
in-method synthesized `now-1` evidence. The remaining "internal caller can forge the struct" is the
declared T22 transport-authentication ceiling (receipt/ack provenance is transport-owned, deferred),
which the code names. ✓ (codex #2/#15, kilo #2, agy #2 — the ceremonial grading is gone).

**(c) [UNFOLDED] The schedule v1 upcast is present, but the OBLIGATION event-shape changes have NO
upcast and no version bump for the R3 change.** `schedule` gained a "v1 UPCAST" (recompute `due_utc`
from the wall at replay, codex #20's schedule half). `obligation` did not: its `Version()` is still
`2`, and the R3 change made `EvTaskExecuted`/`EvTaskDone` require `intent_op`/`marker_line` that
R1 (`expected_sha256`) and R2 (`marker_line` but no `intent_op`/`operation_id`) canonical events do
not carry. A legacy obligation DB — v1, or v2 with a partially-checkpointed `task_executed` — fails
`Apply` on catch-up (`WHERE intent_op='' AND intent_op!=''` matches nothing). The schedule half of
codex #20 is folded; the obligation half silently reinterprets and then fails, rather than upcasting
or failing startup with a typed migration. Fix: add an obligation v1/v2 upcast (or a typed
migration-required failure), and bump `Version()` so a mismatched DB resets.

**(d) [OK] SIGKILL fuzz through Sweep is causal.** schedule_test.go +117 adds the process-kill/
barrier recipe test through `Scheduler.Sweep`, asserting one durable occurrence/run. ✓ (codex #14).

**(e) [OK] Marker/id sealing is complete.** `taskIDOK` enforces a closed `[a-zA-Z0-9_-]` alphabet
(closing the `[id] note` delimiter-aliasing and the newline-marker break); `notesDir` is an IMMUTABLE
profile-bound field on `Manager`, not a package global. ✓ (codex #16/#17).

**(f) [OK] Health/doctor observability is honest.** doctor now maps ENOENT to "no occurrences fired
yet", a malformed counter to `StatusOff`, and EACCES/I-O to a distinct broken-observability state
(instead of collapsing everything to OK). ✓ (codex #11).

## Remaining codex findings — spot-checked folded

- **#10 overdue interval** → `Run` calls `SetSweepInterval(interval)` and ticks on the SAME field. ✓
- **#13 kind schema** → per-kind `ValidateParams` + `CreateTask` validates `task_params`. ✓
- **#19 task secret** → `RedactorRewrites` on `task_params` before the journal. ✓
- **#21 stale MarkDue** → `Run(ctx, 30s, nil)` — the obsolete post-fire callback is removed. ✓

## NEW defects — none material beyond (c)

The CAS/attestation SQL, the marker alphabet, the immutable `notesDir`, and the doctor states are
correct on inspection; no additional defect found in the changed lines.

---

## Verdict

Eleven of the thirteen round-2 findings are correctly folded — the projection now enforces the
claim/attestation/done protocol atomically, the ack evidence is a real (T22-ceiling-bounded)
artifact, marker/id sealing and kind-schema validation are complete, and health observability is
honest. One material item is only half-folded: the schedule gained its v1 upcast, but the obligation
projection's R1→R2→R3 event-shape changes have no upcast and no version bump, so a legacy obligation
DB fails to catch up (the silent-reinterpretation class codex #20 named). Full focused suite green.

VERDICT: FAIL
