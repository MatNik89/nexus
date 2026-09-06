# PREP-LOW round-4 verification — Codex

Reviewed commit `c083f5c0444e7d77824df60626e595021ceef919` on branch `slice/p0-prep-low`, bound to tree `02a949a33e07c24c987e628447bfb80ae826f07e`. All behavioral checks and the controlled defect used fresh `git archive c083f5c` exports under `/tmp`; no probe mutated the repository. This report is the only repository file written by the review.

## Outcome

Both round-3 LOW findings are closed. The `padded`/`leadlf`/`outmix` table is unconditional, checks `malformed` before either UID-specific oracle, requires success for a root-run root-owned fixture, and on this UID 1000 run requires the specific `not root-owned` refusal. The table remains RED-capable against the historical per-line trim defect. Both stale `mirrors Go TrimSpace` comments were replaced with wording that names the shared ASCII `[ \t\r\n]` `trimMachineID` contract.

No HIGH, MEDIUM, or LOW finding remains in the requested scope.

## Closure verification

### 1. UID-independent trim-fixture detector

- `cmd/nexus/main_test.go:1510-1534` places the full three-case table outside every UID guard.
- `cmd/nexus/main_test.go:1519-1520` rejects any `malformed` result before UID branching, so an ownership outcome cannot hide a trim divergence.
- `cmd/nexus/main_test.go:1522-1525` requires a zero exit for the root path; fixtures created by a root test process are root-owned.
- `cmd/nexus/main_test.go:1526-1532` rejects success for a non-root fixture and accepts only the `not root-owned` refusal class. Any other failure reason is rejected.
- On the actual review UID (`id -u` = `1000`), the pristine focused test passed. Therefore all three fixtures reached their expected non-root ownership outcome without a content rejection.

Controlled defect probe: in a disposable clean export, I replaced only `scripts/machine-id-check.sh:24-33` with the historical per-line `sed` trim from `2a49441^`, retaining the committed round-4 test unchanged. Then:

```text
LC_ALL=C /home/matej/.local/go/bin/go test ./cmd/nexus \
  -run '^TestMachineIDCheckScriptMirrorsDoctor$' -count=1 -v

=== RUN   TestMachineIDCheckScriptMirrorsDoctor
    main_test.go:1520: leadlf id refused as content (script diverges from Go): ".../leadlf content malformed (fail closed)\n"
--- FAIL: TestMachineIDCheckScriptMirrorsDoctor (0.09s)
FAIL
exit code 1
```

This is the expected causal RED: the test remained installed, the production trim alone was defective, the failing assertion was the `leadlf` contract assertion, and the run occurred at UID 1000.

### 2. Stale contract comments

- `scripts/machine-id-check.sh:20-25` now names the shared ASCII `[ \t\r\n]` contract and `trimMachineID`.
- `cmd/nexus/main_test.go:1502-1509` uses the same contract wording.
- Immutable-tree search over `cmd/nexus/main.go`, `cmd/nexus/main_test.go`, and `scripts/machine-id-check.sh` found no `mirrors Go TrimSpace` occurrence. The remaining `Unicode TrimSpace` references describe the intentional U+00A0 counterexample and do not claim equivalence.
- `cmd/nexus/main.go:1019-1024` remains the Go owner and implements `strings.Trim(raw, " \t\r\n")` exactly.

## Clean-export evidence

- Target blobs: `cmd/nexus/main_test.go` = `e8830871eb55641f50d63fd25d21554a46cfdab5`; `scripts/machine-id-check.sh` = `64ce55e0f35f7f68104a4f23416ab001f87ad406`. A second pristine export hashed to the same blobs.
- `sh -n scripts/machine-id-check.sh` — exit 0.
- `LC_ALL=C /home/matej/.local/go/bin/go test ./cmd/nexus -run '^TestMachineID(CheckScriptMirrorsDoctor|Validation)$' -count=1 -v` — exit 0; both named tests ran and passed.
- The focused mirror test with `-count=1` under `C`, `C.utf8`, and `en_US.utf8` — exit 0 in all three installed locales.
- `LC_ALL=C /home/matej/.local/go/bin/go test ./cmd/nexus -count=1` — exit 0 (`ok`, 2.091s) from the second pristine export.
- `git grep` found one production consumer of the checker, `scripts/p0-accept.sh:30`; no sibling machine-ID shell validator exists in the immutable tree.

## Simplification, weakest points, and proof ceiling

Lean already. The round-4 behavioral diff changes only the existing detector branch and two stale comments; it adds no dependency, abstraction, configuration, or production path.

The three weakest proof points are: the root UID branch was source-traced rather than executed because the review UID is 1000; map iteration order is intentionally unspecified, although every pristine run completes the whole table and the controlled defect proves the decisive `leadlf` case executes; and the repository-wide aggregate gate was not run because the change is confined to one package test plus comment text. Upgrade the first ceiling when a UID 0 CI/container run is required, and the last when this commit is promoted beyond this narrow QA closure.

Proof ceiling: this review establishes the two requested closures for immutable commit `c083f5c` on Linux/arm64 with Go 1.26.4, `/bin/sh` = dash, UID 1000, and the three installed locales. It does not establish behavior on another shell/platform, execute the UID 0 branch, or assert repository-wide release readiness.

Skill update: none; round 3 identified both repeatable gaps, and round 4 exposed no new audit-process miss.

VERDICT: PASS
