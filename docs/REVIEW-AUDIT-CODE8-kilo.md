# Review of audit-hardening slice — CODE8 (`slice/audit-b`)

- **Reviewed revision**: `dd655bf` on `slice/audit-b` (worktree `/home/matej/HARNESS/nexus-b`)
- **Delta**: `1d93761..HEAD` (the CODE7 fold)
- **Verdict**: PASS

## CODE7 fix verification (correct)

1. **`CompanionOperationID`** (`channel.go:206-227`) decodes the shared
   `"operation_id"` JSON tag. Every `s7.Companion{Key, Params}` that reaches
   `call` is safe: the only two sites that hand a companion to `call` are the
   delivery park (`channel.go:1010`, a `deliveryMark` with `operation_id`) and the
   control-effect park (`telegram.go:998`, a `controlEffectPayload` with
   `operation_id`). The empty `s7.Companion{}` returns at `channel.go:942,945` and
   `telegram.go:942,944` go to `Report`/`Cancel`/`Reconcile` (the `LandingUnknown`
   no-companion path), never to `call`, so `CompanionOperationID` is never fed a
   payload that lacks the field.
2. **`call()` ordering** (`telegram.go:338-350`) — the new check runs after the
   companion-cardinality check and before `json.Marshal`/payload-hash/`Consume`.
   `cid != string(op)` is the substantive new check (the companion's *payload* must
   name the operation); `companions[0].Key != op` is a harmless fail-fast that
   duplicates the S7 `Consume`'s `Key == Grant.OperationID` check — it checks the
   companion's Key, not the grant's, and is not the load-bearing part.
3. **`TestForeignCompanionOperationIDRefusedBeforeWire`** (`matrix_test.go:658-718`)
   is non-vacuous: the control-effect half uses a companion whose declared hash
   matches the actual body for operation A while its payload names operation B, so
   the CODE6 payload-hash check passes and the CODE7 companion check is what
   refuses; the delivery half parks a genuinely DIFFERENT row (`rowB`'s params under
   `rowA`'s grant). Both assert zero wire and `State == AttemptAuthorized` (unconsumed).

## Fresh adversarial pass (no new defect)

No other companion-constructing site exists: the provider (`provider.go:238-265`)
and planner (`planner.go:192-241`) drive `s7.Execute` with a plain `Attempt`
callback and no `s7.Companion` (provider operations are non-durable). The adapter
is therefore the sole owner that pairs a durable companion with a grant, and it now
cross-checks `Key` and `Params.OperationID` against the operation before Consume.
The S7 engine remains mutex-guarded, `appendLocked` only ever calls the journal's
independent actor, UNKNOWN is exited only via `Reconcile`, and the closed-vocabulary
and `validReport` boundaries remain intact.

## Notes (not FAIL reasons)

- The `companions[0].Key != op` clause in `call` is redundant with S7's
  `Consume` key check; keeping it as a fail-fast before `json.Marshal` is
  acceptable but could be dropped without a behavior change.
- `CompanionOperationID` uses a permissive `json.Unmarshal` into a one-field
  struct (it ignores unknown fields and trailing data), which is fine here because
  the payloads it reads are owner-built, already-validated event params — not
  network input.

VERDICT: PASS
