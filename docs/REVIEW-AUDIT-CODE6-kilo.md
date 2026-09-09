# Review of audit-hardening slice — CODE6 (`slice/audit-b`)

- **Reviewed revision**: `1e67a8b` on `slice/audit-b` (worktree `/home/matej/HARNESS/nexus-b`)
- **Delta**: `3f1ba47..HEAD` (the CODE5 folds)
- **Verdict**: PASS

Verified `CGO_ENABLED=0 go build ./...`, `go vet ./...`, and
`go test -count=1 ./internal/...` all PASS.

## CODE5 fix verification (all correct)

1. **`s7.CodeReceiptNotDurable`** (`s7.go:190`) — value unchanged
   (`"receipt_not_durable"`); it is the only remaining bare literal, and all four
   production usages now reference the constant (`channel.go:109,1015`;
   `telegram.go:354,578`). `knownCodes` now includes it (`s7.go:129`). No persisted/
   compared string changes value.
2. **`ClassOf` unified walk** (`channel.go:94-163`) — `localClass` classifies every
   node (a `*ClassifiedError` by its explicit class with `!Valid()` → substrate, a
   bare `*Failure` by its own fields) in ONE pass; precedence
   `substrate > remote_rejected > transport` is applied in the final switch; the
   removed `errors.As` fallback left no gap — the manual `walk` covers the same
   `Unwrap() error` / `Unwrap() []error` interfaces the stdlib `errors.As`/`errors.Is`
   recognize. `classOfMaxDepth = 64` bounds recursion, and a truncated walk falls
   through to `substrate/unclassified` (fail-closed), never fail-open.
3. **`bound()` exact grammars** (`telegram.go:250-298`) — `ctrlUnboundOp` /
   `ctrlBoundOp` / `ctrlBoundTarget` / `ctrlEffectOp` are anchored (`^…$`) with the
   bot id as a capture group; `bound()` requires `tm[1] == m[1]` (op bot id == target
   bot id) and `m[2] == method`. `getMe` accepts only the unbound form. Production
   callers pass: `controlRead` builds `control:tg:getMe:<n>` (unbound) and
   `control:tg:<bot>:<method>:<n>` only when `a.botID != 0` (`telegram.go:512-515`),
   and `ControlOperation`/`ControlTarget` produce the 64-hex / bot-bound shapes
   (`channel.go:302-310`). `TestControlBotIDMismatchRefusedBeforeWire` mints real
   grants with a bot-id mismatch (read and effect) and a bot-bound getMe, asserting
   `ErrAttemptNotAuthorized` and zero wire calls — non-vacuous and correctly named.

## Notes (not FAIL reasons)

1. **`channel.go:1015` (`receipt_not_durable` → `substrate("receipt")`) is still
   dead.** Now that `Report` accepts the code, its `appendLocked` fails first (the
   receipt failure implies the journal is down, so the terminal landing append also
   fails with `ErrNotDurable`), and `Flush` returns `substrate("landing", …)` before
   reaching `:1015`. The end-to-end outcome is unchanged and correct (substrate →
   adapter stops; the row stays UNKNOWN and the RUNNING operation rehydrates UNKNOWN),
   only the diagnosis reads "landing" rather than "receipt".
2. **Bot-id regex `(\d+)` does not enforce `> 0`.** `control:tg:0:getMyCommands:<n>`
   would parse, but `controlRead` never emits it (`a.botID != 0` gate) and
   `registerCommands` rejects an unidentified bot first — fail-closed by construction,
   not by the regex.
3. **`ClassOf` depth cap truncates a >64-deep tree to "unclassified" (substrate)**
   — safe, since the terminal fallback is fail-closed.

## Fresh adversarial pass (no new defect)

`CodeReceiptNotDurable` entering the vocabulary does not advance the S7 state on a
failed landing: `Report` computes the `Step` result but only assigns `rec.state`
after a successful durable append, so a journal-down receipt failure leaves the
operation RUNNING → rehydrated UNKNOWN on restart (E9), never re-granted. The S7
authority remains mutex-guarded with no projection re-entry; the planner/loop hold
no retry loop; the two `client.Do` sites remain the egress-governed clients.

VERDICT: PASS
