# PREP-LOW Verification Round 4 — agy

**Target:** Repo `/home/matej/HARNESS/nexus`, branch `slice/p0-prep-low`, HEAD `c083f5c` (`fix(prep-low-r4): trim fixtures run under every uid; stale TrimSpace comments corrected`).  
**Reviewer:** `agy` (Antigravity).  
**Scope:** Verification of round-3 defect closures:
1. Padded/`leadlf`/`outmix` fixture table in [`cmd/nexus/main_test.go:1509-1533`](file:///home/matej/HARNESS/nexus/cmd/nexus/main_test.go#L1509-L1533) running unconditionally across all UIDs.
2. Stale `TrimSpace` comments updated to name the shared ASCII `[ \t\r\n]` `trimMachineID` contract in [`scripts/machine-id-check.sh:20`](file:///home/matej/HARNESS/nexus/scripts/machine-id-check.sh#L20) and [`cmd/nexus/main_test.go:1502`](file:///home/matej/HARNESS/nexus/cmd/nexus/main_test.go#L1502).

Working tree was never modified during verification; all negative ablation probes ran in fresh `git archive` exports.

---

## 1. Test Suite Verification

- `CGO_ENABLED=0 go vet ./...`: **PASS**, exit code 0.
- `CGO_ENABLED=0 go test -p 1 ./...`: **PASS**, all 34 packages green, exit code 0 (0 `FAIL` / `panic`).
- `cmd/nexus` suite: `TestMachineIDValidation` and `TestMachineIDCheckScriptMirrorsDoctor` pass in 4.93s.
- `internal/app/daemon` suite: `TestRedeliveredSuspendedTurnRecoversChallenge` passes cleanly.

---

## 2. Verification of Round-3 Finding Closures

### Finding #1: Unconditional UID Fixture Table
- **Audit & Implementation:**
  - In `cmd/nexus/main_test.go:1509-1533`, the `padded`, `leadlf`, and `outmix` fixture table is no longer guarded by `if os.Geteuid() != 0`.
  - All test runs immediately assert `!strings.Contains(reason, "malformed")`, proving that content validation accepted the trimmed ID regardless of execution UID.
  - Under root (`os.Geteuid() == 0`), the fixture is root-owned and the script must succeed (`err == nil`).
  - Under non-root (`os.Geteuid() != 0`), the fixture is user-owned and the script must refuse specifically with `"not root-owned"` (`err != nil`).
- **Causal Negative Ablation Probe:**
  - In a clean `git archive` export, reverting `scripts/machine-id-check.sh` to per-line sed turned [`TestMachineIDCheckScriptMirrorsDoctor`](file:///home/matej/HARNESS/nexus/cmd/nexus/main_test.go#L1478-L1534) **RED**:
    `main_test.go:1520: leadlf id refused as content (script diverges from Go): ".../leadlf content malformed (fail closed)"`.

### Finding #2: Correction of Stale TrimSpace Comments
- **Audit & Implementation:**
  - `scripts/machine-id-check.sh:20` now states: `(the shared ASCII [ \t\r\n] contract of trimMachineID)`.
  - `cmd/nexus/main_test.go:1502` now states: `(the shared ASCII [ \t\r\n] contract of trimMachineID)`.
  - Both comments accurately reflect the byte-level contract implemented in `cmd/nexus/main.go:1023`.

---

## 3. Top-3 Weakest Points

1. **[LOW] Nested POSIX Parameter Expansion Syntax in Shell Checker:**
   - *Location:* [`scripts/machine-id-check.sh:29-30`](file:///home/matej/HARNESS/nexus/scripts/machine-id-check.sh#L29-L30).
   - *Evidence:* The parameter expansions `${MID#"${MID%%[!\ $TAB$CR$NL]*}"}` and `${MID%"${MID##*[!\ $TAB$CR$NL]}"}` rely on nested quote expansion and inverted character class matching (`[!...]`). While POSIX-compliant and verified under `/bin/sh` (dash/bash), manual editing of quotation nesting could introduce subtle parsing errors in restrictive shell environments.

2. **[LOW] UID 0 Fixture Ownership Equivalence Assumption:**
   - *Location:* [`cmd/nexus/main_test.go:1522-1524`](file:///home/matej/HARNESS/nexus/cmd/nexus/main_test.go#L1522-L1524).
   - *Evidence:* When `os.Geteuid() == 0`, `mk` creates fixtures expected to be owned by root (UID 0). In specialized container environments where `/tmp` is mounted with UID mapping or ownership squashing, `err != nil` could occur if the fixture is mapped to a non-zero UID.

3. **[LOW] ASCII Trim Set Excludes Rare Control Whitespace (`\v`, `\f`):**
   - *Location:* [`cmd/nexus/main.go:1023-1025`](file:///home/matej/HARNESS/nexus/cmd/nexus/main.go#L1023-L1025) and [`scripts/machine-id-check.sh:27-30`](file:///home/matej/HARNESS/nexus/scripts/machine-id-check.sh#L27-L30).
   - *Evidence:* `trimMachineID` and the shell parameter expansion explicitly target the 4-byte ASCII set `[ \t\r\n]`. Obscure ASCII control whitespace like vertical tab (`\v` / `\x0b`) or form feed (`\f` / `\x0c`) is treated as content and rejected as malformed. While fail-closed and appropriate for systemd `/etc/machine-id`, it represents a strict 4-byte subset rather than full 6-byte ASCII `isspace`.

---

## 4. Verdict

All findings from round 3 are verified closed, causal under ablation in fresh git exports, and clean across the entire repository test suite.

VERDICT: PASS
