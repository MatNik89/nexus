# PREP-LOW verification round 4 — Kilo

Target: `slice/p0-prep-low` at HEAD `c083f5c` (closes my round-3 findings on the trim fixture table + stale comments). Method: code-read, run the suite in the repo, a RED-capability ablation in a clean `git archive HEAD` export (`/tmp/kilo/preplowr4`). Working tree never modified. My uid is 1000 (non-root).

## Suite

`CGO_ENABLED=0 go vet ./...` exit 0; `CGO_ENABLED=0 go test -count=1 ./...` all 33 packages `ok` on re-run. (One transient `TestChaosCheckerDetectsCorruption` failure appeared in a single full-suite run under parallel load; it passes in isolation at 0.14s and in the re-run — the same pre-existing acceptance-package parallel-build contention flake seen in earlier rounds, unrelated to this commit, which touches only `cmd/nexus/main_test.go` and a shell comment.)

## Finding 1 — fixture table runs under every uid — FIXED and causal

`TestMachineIDCheckScriptMirrorsDoctor` (`main_test.go:1505-1532`) now runs the padded/leadlf/outmix table unconditionally. The acceptance logic is uid-correct: a `malformed` reason is always a trim-contract divergence (checked first), then under root the root-owned fixture must SUCCEED (`err == nil`), and under non-root the exact `not root-owned` refusal is the evidence.

**Ablation** (reverted the shell to the per-line `sed` trim in the export, at uid 1000): the test goes RED — `leadlf id refused as content (script diverges from Go): … content malformed`. The unconditional table is RED-capable against a defective trim at my uid, and the `malformed`-first check makes it so under root as well (where a broken trim would also yield `malformed` rather than a success).

## Finding 2 — stale "mirrors Go TrimSpace" comments — FIXED

Both remaining comments now name the real owner: "the shared ASCII [ \t\r\n] contract of `trimMachineID`" (`scripts/machine-id-check.sh:20-21` and `cmd/nexus/main_test.go:1508-1509`). No `TrimSpace` wording remains in the trim contract.

## NEW-defect hunt

None. The unconditional table's branches are mutually correct per uid; the `malformed` check correctly precedes the uid branch so a trim divergence cannot be masked by either ownership outcome; and the comment change is cosmetic.

## Verdict

Both round-3 findings are genuinely closed (uid-independent fixture table, corrected contract wording), the detector stays RED-capable against a defective trim at my uid, and `c083f5c` introduces no NEW defect. (The lone full-suite failure was the known pre-existing acceptance-package parallel flake, passing on re-run and in isolation.)

VERDICT: PASS
