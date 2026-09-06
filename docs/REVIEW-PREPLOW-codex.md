# PREP-LOW verification — Codex

Reviewed branch `slice/p0-prep-low` at immutable HEAD `d0d60d9c982b3ba35a7437a411118a5490718374`. Production probes and ablations ran only in fresh `git archive` exports. The repository suite ran read-only in the real repository because `internal/buildcheck` requires `.git`. A concurrently created untracked `docs/REVIEW-PREPLOW-kilo.md` appeared during the review; I did not read or modify it.

## Findings

### [CONCRETE][SEV: LOW] `sed [[:space:]]` still does not mirror Go `strings.TrimSpace` exactly

The shell checker trims with the locale-dependent POSIX class `[[:space:]]` (`scripts/machine-id-check.sh:20-29`), while the doctor uses Unicode-aware `strings.TrimSpace` (`cmd/nexus/main.go:995-1016`). Under this review host's `C.UTF-8` ctype, U+00A0 NO-BREAK SPACE is not matched by the shell class but is removed by Go.

PROBE: in a clean export I extended the committed padded-ID fixture with the same raw value `U+00A0 + 32 lowercase hex + U+00A0 + newline`. The probe first required `validateMachineID(strings.TrimSpace(raw))` to pass, then required the real shell checker to reach the ownership refusal. Go accepted the content; the checker failed with `padded-nbsp content malformed (fail closed)`. The focused test therefore turned RED specifically on the remaining parity mismatch.

The committed ASCII-space fixture (`cmd/nexus/main_test.go:1491-1502`) is green, and reverting the new per-line trim to the old slurp expression makes it RED for the intended padded-ID reason. That proves the commit fixes the reported ASCII case, but not its stronger “mirrors Go TrimSpace exactly” claim.

FIX: define one deterministic trim contract shared by both implementations. The smallest robust option is to make both sides explicitly accept the same enumerated byte whitespace set appropriate for `/etc/machine-id`; if Unicode `TrimSpace` parity is required, the shell checker must explicitly handle the full Go White_Space set rather than relying on locale-dependent `[[:space:]]`.

### [CONCRETE][SEV: LOW] The new replay test does not assert that a later success overrides an earlier suspension

Production replay is ordered and currently correct: `approval.turn_suspended` records its summary, and a later matching `turn.succeeded` overwrites it (`internal/app/daemon/daemon.go:259-294`). The new test creates that turn-7 ordering, but it discards the successful result and never redelivers turn 7; it tests only the suspended-only turn 8 (`internal/app/daemon/daemon_test.go:525-580`).

PROBE: in a clean export I changed the success branch to update only when `!found`, so an earlier suspension incorrectly prevents a later success from winning. `TestRedeliveredSuspendedTurnRecoversChallenge` still passed. Its “later turn.succeeded wins” claim is therefore not RED-capable.

The test also contains an unused `ev` map and hand-builds suspension payloads rather than using `approval.Store.Suspend`; this adds noise and permits a fixture whose `{}` call is not a production-valid `ToolCall` (`internal/app/daemon/daemon_test.go:529-533`).

FIX: after creating the later successful turn 7, redeliver update 7 and assert the recovered value is the success final, not the earlier challenge. Prefer producing the suspension through `approval.Store.Suspend` with a valid call/context, and remove the unused map.

## Claim verification

- **Claim 1, reported single-line padding — PASS but incomplete class closure.** The committed focused test passed. Reverting only the new `sed` expression to the prior slurp form made it fail with `padded id refused as content`, proving causal RED capability for ASCII padding. The U+00A0 probe above keeps the overall fix open.
- **Claim 1, multiline refusal — PASS by code and committed tests.** Interior newlines survive per-line edge trimming and are rejected by length/charset guards; the existing two-line and trailing-garbage fixtures remain green.
- **Claim 2, suspended-only recovery — PASS.** The committed focused test passed. Renaming only the `approval.turn_suspended` switch case made it fail through the real duplicate-event collision, proving the new recovery branch is causal.
- **Claim 2, later success wins — implementation PASS, detector FAIL.** Ordered replay currently overwrites the suspension with the later success, but the clean negative mutation above stayed green.
- **Phase-3 memory close finding — OBSOLETE, confirmed.** `memory.Store` owns no database handle or closer; it is a facade over the journal (`internal/memory/store.go:333-347`). Memory tables are a synchronous projection registered in the one profile journal (`cmd/nexus/main.go:481-500`), and `journal.Journal.Close` owns the sole database close. No independent memory resource remains to close.

## Commands and machine-read results

- `git show HEAD` — PASS; reviewed `d0d60d9` and all four changed files.
- `CGO_ENABLED=0 go test -count=1 -run '^TestMachineIDCheckScriptMirrorsDoctor$' -v ./cmd/nexus` — PASS; no `FAIL` token.
- `CGO_ENABLED=0 go test -count=1 -run '^TestRedeliveredSuspendedTurnRecoversChallenge$' -v ./internal/app/daemon` — PASS; no `FAIL` token.
- Clean slurp-revert export, machine-ID focused command — expected FAIL on the padded-ID content/ownership assertion.
- Clean case-rename export, suspended-turn focused command — expected FAIL with the duplicate `turn.created` collision instead of recovered challenge.
- Clean strengthened U+00A0 export, machine-ID focused command — FAIL on the surviving Go/shell parity mismatch.
- Clean later-success negative mutation, suspended-turn focused command — unexpected PASS, proving the missing assertion.
- `CGO_ENABLED=0 go test -count=1 -timeout 900s ./...` in the real repository — PASS; exit 0 and literal full-log scan found no `FAIL` token.

Weakest link: the fixes satisfy their ASCII and suspended-only examples, but the stated whole-class contracts remain broader than the committed detectors.

VERDICT: FAIL
