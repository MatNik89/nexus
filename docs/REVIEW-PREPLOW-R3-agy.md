# PREP-LOW Verification Round 3 — agy

**Target:** Repo `/home/matej/HARNESS/nexus`, branch `slice/p0-prep-low`, HEAD `2a49441` (`fix(prep-low-r3): whole-byte-sequence outer trim in the shell checker`).  
**Reviewer:** `agy` (Antigravity).  
**Scope:** Verification of the round-2 leading-LF and mixed outer-whitespace trim parity closure:
- Whole-byte-sequence outer trim of `[ \t\r\n]` via POSIX parameter expansion in [`scripts/machine-id-check.sh:27-31`](file:///home/matej/HARNESS/nexus/scripts/machine-id-check.sh#L27-L31).
- Added `leadlf` and `outmix` fixtures in [`cmd/nexus/main_test.go:1506-1520`](file:///home/matej/HARNESS/nexus/cmd/nexus/main_test.go#L1506-L1520).
- Comment correction in `scripts/machine-id-check.sh`.

Working tree was never modified during verification; all negative ablation probes ran in fresh `git archive` exports.

---

## 1. Test Suite Verification

- `CGO_ENABLED=0 go vet ./...`: **PASS**, exit code 0.
- `CGO_ENABLED=0 go test -p 1 ./...`: **PASS**, all 34 packages green, exit code 0 (0 `FAIL` / `panic`).
- `cmd/nexus` suite: `TestMachineIDValidation` and `TestMachineIDCheckScriptMirrorsDoctor` pass in 4.24s.
- `internal/app/daemon` suite: `TestRedeliveredSuspendedTurnRecoversChallenge` passes cleanly.

---

## 2. Verification of Round-2 Finding Closure

### Leading-LF & Whole-Byte-Sequence Outer Trim Parity
- **Audit & Implementation:**
  - In `scripts/machine-id-check.sh:27-31`:
    ```sh
    TAB="$(printf '\t')"; CR="$(printf '\r')"
    NL="$(printf '\nx')"; NL="${NL%x}"
    MID="$CONTENT"
    MID="${MID#"${MID%%[!\ $TAB$CR$NL]*}"}"
    MID="${MID%"${MID##*[!\ $TAB$CR$NL]}"}"
    ```
  - The per-line `sed` command was eliminated in favor of standard POSIX parameter expansion over the full byte stream.
  - Outer leading and trailing sequences of `[ \t\r\n]` (including leading newlines `\n` and mixed whitespace `\t...\r\n`) are stripped in a single step across the entire string without record-splitting.
  - Interior whitespace (such as embedded spaces, mid-string newlines in multiline files, etc.) is preserved, ensuring multiline content fails the length guard (`[ "${#MID}" -eq 32 ]`) fail-closed.
  - In [`cmd/nexus/main_test.go:1506-1520`](file:///home/matej/HARNESS/nexus/cmd/nexus/main_test.go#L1506-L1520), `TestMachineIDCheckScriptMirrorsDoctor` tests `"leadlf"` (`"\n" + valid`) and `"outmix"` (`" \t" + valid + "\r\n"`), asserting that both pass content validation and proceed to the ownership check (`"not root-owned"`).
- **Causal Negative Ablation Probes:**
  - **Leading-LF Revert Probe:** In a clean `git archive` export, reverting `scripts/machine-id-check.sh` to the round-2 per-line sed implementation turned [`TestMachineIDCheckScriptMirrorsDoctor`](file:///home/matej/HARNESS/nexus/cmd/nexus/main_test.go#L1478-L1522) **RED**:
    `main_test.go:1519: leadlf id refused as content (script diverges from Go): ".../leadlf content malformed (fail closed)"`.
  - **Mixed-Whitespace Omission Probe:** In a clean export, omitting `TAB`/`CR` from the parameter expansion turned `TestMachineIDCheckScriptMirrorsDoctor` **RED**:
    `main_test.go:1519: outmix id refused as content (script diverges from Go): ".../outmix content malformed (fail closed)"`.

---

## 3. Top-3 Weakest Points

1. **[LOW] Nested POSIX Parameter Expansion Syntax Complexity:**
   - *Location:* [`scripts/machine-id-check.sh:29-30`](file:///home/matej/HARNESS/nexus/scripts/machine-id-check.sh#L29-L30).
   - *Evidence:* The parameter expansions `${MID#"${MID%%[!\ $TAB$CR$NL]*}"}` and `${MID%"${MID##*[!\ $TAB$CR$NL]}"}` rely on nested quote expansion and inverted character class matching (`[!...]`). While POSIX-compliant and verified under `/bin/sh` (dash/bash), manual editing of the quotation nesting could introduce subtle parsing errors in restrictive shell environments.

2. **[LOW] Root UID Pre-Condition on Padded/LeadLF Script Mirror Assertions:**
   - *Location:* [`cmd/nexus/main_test.go:1505`](file:///home/matej/HARNESS/nexus/cmd/nexus/main_test.go#L1505).
   - *Evidence:* The `padded`, `leadlf`, and `outmix` fixtures are asserted inside `if os.Geteuid() != 0`. On systems or containers where the test suite runs as root (euid 0), these tests skip because user-created temp fixtures are root-owned and would pass ownership rather than failing with `"not root-owned"`.

3. **[LOW] ASCII Trim Set Excludes Rare Control Whitespace (`\v`, `\f`):**
   - *Location:* [`cmd/nexus/main.go:1023-1025`](file:///home/matej/HARNESS/nexus/cmd/nexus/main.go#L1023-L1025) and [`scripts/machine-id-check.sh:27-30`](file:///home/matej/HARNESS/nexus/scripts/machine-id-check.sh#L27-L30).
   - *Evidence:* `trimMachineID` and the shell parameter expansion explicitly target the 4-byte ASCII set `[ \t\r\n]`. Obscure ASCII control whitespace like vertical tab (`\v` / `\x0b`) or form feed (`\f` / `\x0c`) is treated as content and rejected as malformed. While fail-closed and appropriate for systemd `/etc/machine-id`, it represents a strict 4-byte subset rather than full 6-byte ASCII `isspace`.

---

## 4. Verdict

The leading-LF and outer-whitespace parity finding is completely resolved with verified causal RED-capability across both Go and shell verifiers. Full repository test suite and vet pass cleanly with zero regressions.

VERDICT: PASS
