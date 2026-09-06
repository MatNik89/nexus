# PREP-LOW verification round 2 — Kilo

Target: `slice/p0-prep-low` at HEAD `6e375f5` (`7477bc2` + `6e375f5` on top of `d0d60d9`). Closes the two codex prep-low round-1 LOW findings. Method: code-read, run the suite in the repo, two RED-capability ablations in a clean `git archive HEAD` export (`/tmp/kilo/preplowr2`). Working tree never modified.

## Suite

`CGO_ENABLED=0 go vet ./...` exit 0; `CGO_ENABLED=0 go test -count=1 ./...` all 33 packages `ok`.

## Finding 1 — trim parity (NBSP divergence) — FIXED and causal

The doctor and `scripts/machine-id-check.sh` now share one deterministic contract — the ASCII set `[ \t\r\n]` on BOTH sides. `machineIDSHAAt` uses the extracted `trimMachineID` (`strings.Trim(raw, " \t\r\n")`, `main.go:1019-1025`); the shell uses an explicit byte class `[ $TAB$CR]` plus sed's per-line newline stripping (`machine-id-check.sh:26-29`). A U+00A0 (NBSP)-padded id is therefore malformed on both sides instead of diverging by locale/Unicode `TrimSpace`.

`6e375f5` extracts `trimMachineID` so the NBSP detector binds the production trim rather than re-implementing it. Fixtures: `nbsppad` in the script-mirror map and the `TestMachineIDValidation` NBSP assert via `trimMachineID`.

**Ablation** (changed `trimMachineID` to `strings.TrimSpace` in the export): `TestMachineIDValidation` goes RED — `NBSP-padded machine id survived the ASCII trim contract`. The detector binds the code it guards.

## Finding 2 — rebuilt suspended-turn test — FIXED and causal

`TestRedeliveredSuspendedTurnRecoversChallenge` now produces suspensions through `approval.Store.Suspend` with production-valid `ToolCall` (rm_file) and `ContextBlock` (user_message, TrustUser). Turn 7 is **redelivered** and asserts the later `turn.succeeded` final (`"ok"`) beats the earlier suspension (`redo7 == out7` and no `APPROVAL NEEDED`); turn 8 (suspended-only) redelivers the challenge summary (`out8 == ch8.Summary`). The unused validator map is removed.

**Ablation** (disabled the `turn.succeeded` branch in the export): the test goes RED — `redelivery recovered "APPROVAL NEEDED …", want the later success "ok" to beat the earlier suspension`. The success-beats-suspension override is causal.

## NEW-defect hunt

None. The ASCII trim is locale-independent on both sides (a literal byte class, never `[[:space:]]` or `strings.TrimSpace`), and the single-line/multi-line machine-id shapes behave identically under Go and shell. The rebuilt test's `Store.Suspend` path matches the production suspension exactly (profile "work" bound, valid call + non-empty context).

## Verdict

Both prep-low round-1 findings are genuinely closed and causal (ablation RED on each guard), and no NEW defect was introduced in `d0d60d9..HEAD`.

VERDICT: PASS
