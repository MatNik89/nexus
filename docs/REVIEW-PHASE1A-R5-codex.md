# PHASE-1A verification round 5 — Codex

Scope: `slice/p0-phase1` at `53bac1fe3967978c2629da30d49eb718c5c388a0`, restricted to the five round-4 folds and defects introduced by those edits.

Fresh evidence:

- `go vet ./internal/...` — PASS.
- `go test -count=1 ./internal/...` — PASS.
- `go test -count=20 ./internal/kernel/journal ./internal/kernel/contracts ./internal/security/redact` — PASS.
- Focused runs of the new budget/depth, case-alias, structural-event-type, failed-open lease, injected-delete, over-budget admission, and admission-barrier tests — PASS.
- `gofmt -d` on the behavioral owners and tests — clean.

1. **[OK] Redaction now fails closed at journal admission.** `Redactor.Redact` returns `([]byte,error)`; valid JSON beyond the byte budget or depth budget returns an error instead of falling back to escape-blind byte matching (`internal/security/redact/redact.go:20-27,141-175`). `Journal.appendOne` returns that error before sequence allocation or persistence (`internal/kernel/journal/journal.go:348-365`). Both redactor limit REDs and the production `Append` over-budget RED pass (`redact_test.go:74-89`; `journal_test.go:619-627`).

2. **[UNFOLDED] Lease release after failed `Open` still discards the cleanup error.** Ownership is now correctly acquired before chain recovery, closing the stale-snapshot handoff race (`internal/kernel/journal/journal.go:220-275`). But when verification fails, `releaseLease` ignores the result of its conditional `DELETE`, then `Open` also ignores `db.Close` and reports only the verification error (`journal.go:268-280`). If lease deletion fails, the row remains bound to this still-live PID/start token; a retry in the same process is rejected as another live writer. The existing failed-open RED covers only successful deletion, while `testFailLeaseDelete` is wired exclusively into `Close` (`journal_test.go:307-335,609-617`; `journal.go:538-546`). Make the shared lease-release helper return an error and join it with the primary recovery and close errors; inject deletion failure on the failed-open path too.

3. **[OK] Case-insensitive known-key removal closes alias resurrection.** `MarshalJSON` removes preserved keys using `strings.EqualFold` before overlaying current known fields (`internal/kernel/contracts/contracts.go:91-106`). The `TURN_ID` parse-clear-marshal RED exercises the production parser and marshaler and passes (`contracts_test.go:461-483`).

4. **[OK] The structural `event_type` RED is now causal.** The secret-bearing event type is explicitly registered before `Append`, so registry rejection cannot satisfy the test; admission must reach mandatory `Touches` and reject there (`internal/kernel/journal/journal_test.go:168-212`; `journal.go:340-353`). The complete ten-field table passes.

5. **[OK] The admission-definitive RED is now revert-proof in the targeted window.** After cancellation, the test holds the actor between commit and reply and asserts for 300 ms that `Append` has not returned; only then does it release the actor and require the committed result (`internal/kernel/journal/journal_test.go:353-400`). Restoring a post-admission `ctx.Done()` branch would fail before actor release.

6. **[OK] `Close` now preserves a lease-delete failure.** It captures the conditional-delete error and joins it with `db.Close`, and the injected-delete RED verifies a non-nil close result (`internal/kernel/journal/journal.go:532-548`; `journal_test.go:609-617`). No other material defect was found in the changed behavioral lines; finding 2 is specifically the sibling failed-`Open` cleanup path.

VERDICT: FAIL
