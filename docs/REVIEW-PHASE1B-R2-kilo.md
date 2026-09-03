# REVIEW-PHASE1B-R2 — round-2 verification (T07–T12 fold)

Scope: fold status of codex 20 + my 10 + agy 5; the two adjudications; NEW defects in changed code.
`go vet ./internal/...` clean; `go test -count=1 ./internal/...` GREEN.

---

## Adjudications — both correctly implemented

**(a) [OK] Checker ALL-must-agree semantics.** `gradeOne` for `ExitCodeIs`/`FileHashIs` now walks
EVERY same-kind record and fails on any contradiction (checker.go:199-227) — order-independent, so
neither a favorable prepend (codex #3) nor an unfavorable one (my first-match) can game the verdict.
`checker != worker` is enforced via `AcceptanceContract.Worker` + `Evidence.Producer` rejection
(checker.go:175-177); forged/zero reminder evidence is rejected at `validate` (occurrence id + time).
The producer string is a DECLARED topknot ceiling (not cryptographically attested until T21) — honest.

**(b) [OK] Machine cancel edges KEPT multi-from, owner-amended.** A single canonical `*.cancelled`
event is legal from every non-terminal pre-completion state (machine.go:117-123, 184-186), and the
suffixed alias names were removed (`EventTypes` now lists one cancel per entity). HARNESS-SPEC:1379+1
carries the explicit AMENDMENT block (cancel from CREATED/ADMITTED/PLANNED/AUTHORIZED; cancel from
UNKNOWN NOT legal; one event-type, multi-from). The earlier "alias would make run.cancelled fail
closed at emit time" defect is gone. ✓

## Material folds — all [OK] (spot-checked)

- **codex #5/#6 T07**: projection `Init` now runs ONLY after the lease is acquired AND the chain is
  verified, with `releaseLease` on any failure (journal.go:288-308); the projection handle is the
  RESTRICTED `ProjTx` whose `guardStatement` rejects statements touching `events`/`journal_meta`/
  `projection_offsets` with NON_CANONICAL_WRITE (projection.go:19-55). ✓
- **codex #9/#10 T08**: `FoldEvent{Type,Offset}` + `Fold` returns `(state, checkpoint, error)` with an
  `observe` callback for intermediates; `Step` returns `current` unchanged on reject (machine.go:52-90). ✓
- **codex #11/#12/#13 T09**: typed per-key `keyKind` schema in every layer; env ENUMERATES
  `NEXUS_CFG_*` and rejects unknown names; `parseList` trims + rejects empty/whitespace hosts;
  `parseBoolStrict` accepts only `true`/`false`. ✓
- **codex #14/#15 T10 pathx**: `profileSlugOK` (canonical slug charset) + `filepath.Rel` containment
  proof; `EnsureDir` Lstat + symlink/perm/ownership verification. ✓
- **codex #16/#17 T10 procx**: lifecycle sealed (`cmd` private, `wait()` sync.Once), `Token.Alive()`
  before each signal, `groupSurvivors` proves the group gone, and the SCOPE comment honestly narrows
  the guarantee to process-GROUP (tree containment is the sandbox owner's). ✓
- **codex #18/#19 T11**: `ProbeResult.ConfigHash` + `Seal` requires a config hash and fails a probe
  measured under a different hash; duplicate probe names are rejected. ✓
- **codex #1/#2/#4 T12**: worker identity, evidence producer, sha256-shape validation, non-empty
  occurrence/time, and the self-grading + forged-evidence REDs. ✓

## My findings — folded vs not

Folded: Fold checkpoint (#4), egress parsing (#6), `sandbox_disabled` case (#7), Terminate PID-reuse
(#8), duplicate probe names (#12), first-match (#14). Not folded (all LOW/QUALITY, dormant for P0):

**1. [UNFOLDED, LOW] `Projector.Run` still re-scans and re-verifies the whole journal from 0.**
`Run` drives `Replay(from, …)` whose query has no `WHERE journal_offset > from` (journal.go). O(n)
per run → O(n²) cumulative for a lagging projector. Performance note only.

**2. [UNFOLDED, LOW] `Resolve` still does not validate that `Conflicts` names are KNOWN.**
closure.go:97-103 only fails `if inSet[c]`; a typo'd conflict (`"telegarm"`) is silently ignored, so
an intended mutual-exclusion can drop without error. Dormant (P0 declares no conflicts), but a
fail-open in a fail-closed validator. Fix: reject `!byName[c]` before the `inSet` check.

**3. [UNFOLDED, LOW] `DeliveredAndAcked` still does not check temporal order** (`AckAt` after
`DeliveredAt`). checker.go:228-246. An ack-before-delivery bundle passes. Dormant (the harness stamps
timestamps correctly).

**4. [UNFOLDED, LOW] `AcceptanceContract.ID` is still only non-empty, not `requireID`-validated.**
checker.go:161. Control-char IDs are admitted. Internal string, low.

## NEW defects — declared ceilings, none material

- `ProjTx` guard is LEXICAL regex matching (documented topknot ceiling, projection.go:16-18): common
  quote spellings (`"events"`, `` `events` ``, `[events]`, `main.events`) are all caught by `\b…\b`,
  and the upgrade trigger (T18 per-projection schema ownership) is named. A projection that can only
  touch DIFFERENTLY-named tables is not a P0.3 violation.
- `ConfigHash` is a string-equality binding, not a verified hash — the strength is the composing
  root's (T27) computation, which is the declared division of labor.

---

## Verdict

The two adjudications are correctly implemented, all codex material (BREAK/RED) findings are folded,
and the owner amendment is recorded in HARNESS-SPEC. The remaining items are four LOW/QUALITY
kilo findings (dormant for P0) and two declared-ceiling notes — none is a BREAK, SEC, or material
RED vacuity.

VERDICT: PASS
