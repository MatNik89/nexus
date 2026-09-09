# Review of audit-hardening slice — CODE5 (`slice/audit-b`)

- **Reviewed revision**: `3f1ba47` on `slice/audit-b` (worktree `/home/matej/HARNESS/nexus-b`)
- **Delta**: `18392b2..HEAD` (the CODE4 folds)
- **Verdict**: PASS

Verified `CGO_ENABLED=0 go build ./...`, `go vet ./...`, and
`go test -count=1 ./internal/...` all PASS.

## CODE4 fix verification (all correct)

1. **`Report` closed-vocabulary + `validReport` at the API boundary** (`s7.go:551-566`).
   The `knownCodes` check and `validReport(outcome, landing.Kind, landing.Code,
   nextAtUnix)` now run unconditionally (durable AND non-durable) before the
   `Step`, leaving the operation RUNNING on refusal. I traced every production
   caller: the provider/planner report `OutcomeSucceeded` with `code == ""`, and the
   channel `Flush` reports `""`/`f.Code`/`CodeTransportPostWrite`/`CodeLocalRefused`,
   all in the closed vocabulary. The one out-of-vocabulary code — `receipt_not_durable`
   (`telegram.go:323`, a channel-level substrate signal) — is now rejected by `Report`
   rather than landed; see Note 1 (behaviour remains correct).
2. **`ClassOf` error-tree walk** (`channel.go:94-158`). The recursive `walk` handles
   both `Unwrap() error` and `Unwrap() []error`, collects every `ClassifiedError`,
   normalizes `!Class.Valid()` to substrate at the collection point, and applies
   `substrate > remote_rejected > transport` before returning; the `Failure` fallback
   still maps `receipt_not_durable`/401/403/else by its own fields. `walk(nil)` and
   the `e == nil` guard handle a nil `Unwrap()` result.
3. **`TestEveryMethodReuseAndMissingGrantRefused`** (`matrix_test.go`) now enumerates
   all 11 kind/method rows with genuinely distinct grants/companions (a second
   independent delivery row for `sendRichMessage`, real `control-effect` durable
   grant + `ControlEffectParams`, five distinct `ui:` operations), and asserts
   `len(cases) == 11`. The `total()` counter now sums all wire methods via the new
   `rich()`/`edits()`/`answers()`/`getMyCommandsN()` accessors.
4. **`readBoundedReply`/`decodeSingleReply`** (`telegram.go:377-414`) mirror the
   provider: read `max+1` and refuse on `> max`, decode exactly one value and require
   EOF. A whole-adapter grep finds no remaining `json.NewDecoder(io.LimitReader(resp.Body...))`
   or `io.ReadAll(resp.Body)`; `envelope.Result` is a `json.RawMessage` extracted from
   the strict-decoded envelope, so the plain `json.Unmarshal(envelope.Result, out)`
   (`telegram.go:369`) decodes one already-bounded value.

The `TestPollRetryableExhaustionStopsViaErrPollTerminal` precondition
(`ce.Class == ClassTransport` and `!ce.Class.Fatal()`) holds, so the
`errors.Is(err, ErrPollTerminal)` OR-arm in `record` is genuinely load-bearing.

## Notes (not FAIL reasons)

1. **`receipt_not_durable` is now handled by `Report`'s rejection, leaving
   `channel.go:996` dead.** `Report` refuses the code before the `Step`, so `Flush`
   returns `substrate("landing", …)` instead of the later `substrate("receipt", …)`
   branch. The end-to-end outcome is unchanged and correct (substrate → adapter stops;
   the row stays UNKNOWN and the RUNNING operation is rehydrated UNKNOWN on restart),
   but the diagnosis is "landing" rather than "receipt" and `:996` is unreachable —
   a small cleanup, not a defect.
2. **`ClassOf`'s `walk` has no visited-set.** A self-unwrapping error would recurse;
   this is identical to the stdlib `errors.Is`/`errors.As` contract (which also assume
   an acyclic tree), and no `ClassifiedError`/`Failure` in the tree unwraps to itself.
3. `MarkParamsForTest`, `SetAppendFault`, `ControlOperation`/`ControlTarget`/
   `ControlEffectParams` are inert-in-production test seams; minimal and acceptable.

## Fresh adversarial pass (no new defect)

- `receipt_not_durable` is a pre-wire substrate failure (the dial is refused before
  any byte leaves), so parking UNKNOWN for reconciliation on restart is correct and
  conservative, never a double-send.
- The S7 authority remains mutex-guarded end-to-end; `appendLocked` only calls the
  journal's independent serialized actor (no projection re-enters S7).
- UNKNOWN is exited only via `Reconcile`; the planner/loop hold no retry loop; the
  two `client.Do` sites remain the egress-governed clients; receipt/health payloads
  carry no secret.

VERDICT: PASS
