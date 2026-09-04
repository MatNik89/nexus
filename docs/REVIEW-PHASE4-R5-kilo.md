# REVIEW-PHASE4-R5 — round-5 final verification (5 codex r4 folds)

Scope: the diff ee93c46..f09fe31 + NEW defects it introduced. `CGO_ENABLED=0 go vet ./...` clean;
`go test -count=1 ./internal/schedule ./internal/obligation ./internal/kernel/journal` GREEN (the
full `./...` run hit the 300s shell timeout only on the variable-duration probe package, not a hang).

---

## The 5 folds — all [OK]

**1. [OK] DONE admission capability — the full forged chain is dead at the journal boundary.**
`DoneCapability` (a fresh random token) is minted by the composition root and held on `Manager`;
`Events(reg, cap)` captures it, and the `EvTaskDone` admission validator requires `p.Cap == cap`
(obligation.go:135-175, 447-459). A producer that can mint a valid-claim intent, a marker-matching
`task_executed` with `/tmp/notes.txt`, and a `task_done` with `verifier:"postcondition-verifier"` is
now rejected unless it also holds the capability — which only the manager (which performs the fs
verification) has. **Judgment vs the no-crypto ceiling:** the capability is process-local (an
unexported, random, closure-captured token, not a signature); it is admission-only (not re-checked on
replay, so the process-local mint survives restart), and its strength is the process's own memory
isolation. That is the declared no-crypto P0 ceiling, honored. ✓

**2. [OK] Parallel RED is causal.** A blocking handler holds call A inside the claim→effect window
while call B enters with a DISTINCT operation; the test asserts B is refused by the in-flight lease,
so deleting `inflight.LoadOrStore` fails the RED (no longer masked by S7's same-ID issue dedup). ✓

**3. [OK] `ErrMigrationRequired` is typed everywhere.** `schedule` and `obligation` each define their
own `errors.New("MIGRATION_REQUIRED")` sentinel and wrap it (`%w`) for v1 `schedule.created`,
v1 `delivered` (no receipt identity), v1 `acked` (no gesture source), v1 `task_executed`
(no claim/marker), and v1 `task_done` (no verification identity); the REDs assert `errors.Is`. ✓

**4. [OK] Health is fail-closed in doctor + daemon.** doctor now distinguishes a non-empty mirror
(OFF "last sweep failed"), an empty one (OK "no sweep failures recorded"), an absent one (OK "daemon
not started"), and EACCES/EISDIR/I-O (OFF "health mirror unreadable"); the daemon mirrors the sweep
result each cycle and surfaces mirror-write failures. ✓

**5. [OK] Mid-batch SIGKILL seam is causal AND production-safe.** The seam fires only when
`testing.Testing() && os.Getenv("NEXUS_TEST_KILL_MID_BATCH") == "1"` and only after the first interior
`insertInTx` of a multi-member batch, before commit (journal.go). `testing.Testing()` is false in a
production binary (the test-generated `MainStart` never runs), so the kill cannot fire outside `go
test`; the test asserts the transaction leaves NOTHING durable. ✓

## NEW defects — none material

- **LOW:** importing `testing` into the non-test `journal.go` is a mild code smell, though
  `testing.Testing()` correctly returns false in production so the seam is inert there.
- **LOW:** `DoneCapability` is a plain `string` type; it is unguessable (random) and unexported, but
  a debug/log leak of the token would let a same-process producer forge DONE. No such leak exists.

---

## Verdict

All five round-4 findings are folded with causal REDs: the DONE admission capability makes the forged
chain dead under the declared no-crypto ceiling, the parallel RED is barrier-causal, the migration
failure is typed with `errors.Is` for every legacy shape, health is fail-closed, and the mid-batch
SIGKILL seam is gated to test builds and leaves nothing durable. The two new observations are
low-severity. Focused suite green.

VERDICT: PASS
