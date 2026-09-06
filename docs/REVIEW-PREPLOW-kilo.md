# PREP-LOW verification — Kilo (two LOW closures + one obsolescence assessment)

Target: `slice/p0-prep-low` at HEAD `d0d60d9`. Method: code-read, run the suite in the repo, two ablations in a clean `git archive HEAD` export (`/tmp/kilo/preplow`). Working tree never modified.

## Suite

`CGO_ENABLED=0 go vet ./...` exit 0; `CGO_ENABLED=0 go test -count=1 ./...` all 33 packages `ok`.

## Finding 1 — prep1-r5 sed slurp trim (verified FIXED, causal)

`scripts/machine-id-check.sh:26` now uses a per-line trim (`sed -e 's/^[[:space:]]*//' -e 's/[[:space:]]*$//'`) instead of the `:a;N;$!ba` slurp (a no-op on single-line input). A whitespace-padded id now passes content (trims to 32 hex) and falls through to the ownership check; multi-line content still refuses via the 32-length guard (interior newlines survive).

- New padded-id fixture (`main_test.go:1492-1502`) asserts the `not root-owned` reason (proving content passes), gated on non-root.
- **Ablation** (reverted the script to the slurp idiom in the export): `TestMachineIDCheckScriptMirrorsDoctor` goes RED — `padded id refused as content (script diverges from Go)`. The per-line trim is causal.

## Finding 2 — phase5-r4 suspended-turn recovery (verified FIXED, causal)

`completedTurnFinal` (`daemon.go:259-285`) now also matches `approval.turn_suspended` and returns the challenge `summary` for the matching turn (the `turn.succeeded` branch is unchanged and still overrides in replay order). A crash between a turn suspending and the channel terminal now replays the challenge summary, not a generic failure.

- `TestRedeliveredSuspendedTurnRecoversChallenge` appends a durable `approval.turn_suspended` + a pre-occupied `turn.created` for a turn, then redelivers and asserts the recovered output contains `APPROVAL NEEDED [ch-only]`.
- **Ablation** (renamed the `"approval.turn_suspended"` case string in the export): the test goes RED — `suspended-turn redelivery errored instead of recovering the challenge: … UNIQUE constraint failed`. The suspended recovery is causal.

## Finding 3 — phase3 codex #12 (memory store close) assessed OBSOLETE (verified)

`memory.Store` is a thin facade over the journal (`Store{j *journal.Journal}`, `store.go:334-348`) with no separate file/DB handle; `mem_facts` is a journal projection (`Projection`, `store.go:140-148`). `j.Close` owns the single SQLite handle and everything derived from it. The phase-3 fold moved memory onto the journal, so the "close the memory store" finding is genuinely obsolete — the assessment is correct.

## NEW-defect hunt

None. The suspended case correctly binds by the payload `turn_id` (approval events carry the turn id in the payload, unlike machine events' envelope `TurnID`), and the "later event wins" replay-order semantics are correct across suspend→resume and suspend→resume→suspend sequences (the last relevant event — success or suspension — is returned). The padded-id trim matches Go `strings.TrimSpace` for the single-line machine-id shape and still refuses multi-line/embedded-whitespace via the length/charset guards.

## Verdict

Both LOW findings are genuinely fixed and causal (ablation RED on each), the memory-store-close obsolescence assessment is correct, and no NEW defect was introduced.

VERDICT: PASS
