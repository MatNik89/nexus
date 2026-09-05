# P0-PREP Review Round 5 — agy

**Target:** Repo `/home/matej/HARNESS/nexus`, branch `slice/p0-prep1`, HEAD `e2f4442` (`fix(prep1-r4): fold codex round 4 (whole-file mirror + revert-proof fixtures)`).  
**Reviewer:** `agy` (Antigravity).  
**Scope:** Re-verification of Codex round-4 findings folded into `e2f4442` on top of `7c883be`. Working tree was never modified during verification.

---

## 1. Test Suite Verification

- Repository full suite: `CGO_ENABLED=0 go test -p 1 -count=1 ./...` across all 34 packages: **PASS**, exit 0 (0 `FAIL` / `panic`).
- Acceptance suite: `internal/acceptance` passed cleanly in 63s (`TestDoctorP0GrantLive` passed).
- CLI & Daemon suite: `cmd/nexus` passed in 9.3s (`TestMachineIDCheckScriptMirrorsDoctor` passed).

---

## 2. Verification of Round 4 Folds

### Fold #1: Whole-File Content Validation in Shell Checker ([`scripts/machine-id-check.sh:15-24`](file:///home/matej/HARNESS/nexus/scripts/machine-id-check.sh#L15-L24))

- **Audit & Implementation:**
  - `CONTENT="$(cat "$F"; printf x)"; CONTENT="${CONTENT%x}"` captures the full content stream verbatim without discarding lines after the first.
  - The script validates the entire normalized string with a 32-character length assertion (`[ "${#MID}" -eq 32 ]`) and a POSIX case charset guard (`case "$MID" in *[!0-9a-f]*) ...`).
  - Multiline files (`valid + valid`), files with trailing garbage (`valid + "trailing garbage"`), and embedded whitespace strings are all rejected with exit code 2 and `"content malformed (fail closed)"`.
  - Valid systemd `/etc/machine-id` (`32 lowercase hex + \n`) passes validation and is printed on stdout.

---

### Fold #2: Content-Before-Ownership Ordering & Reason Assertions in Test Fixtures ([`cmd/nexus/main_test.go:1453-1502`](file:///home/matej/HARNESS/nexus/cmd/nexus/main_test.go#L1453-L1502))

- **Audit & Implementation:**
  - In `scripts/machine-id-check.sh`, the check order is `symlink` $\to$ `regular` $\to$ `permissions` $\to$ `content` $\to$ `ownership`.
  - Because content and permission checks run *before* root-ownership verification, all non-root test fixtures now causally test their specific validation paths.
  - [`TestMachineIDCheckScriptMirrorsDoctor`](file:///home/matej/HARNESS/nexus/cmd/nexus/main_test.go#L1434-L1502) asserts specific refusal reasons:
    - Permissions: `"writable"`
    - Content (`spaced`, `garbage`, `twoline`, `shorthex`): `"malformed"`
    - Symlinks: `"symlink"`
    - User ownership: `"not root-owned"`
- **Causal Negative Ablation Probes:**
  - In `/tmp/nexus-p0-prep1-r5-probe`:
    1. *Length Guard Ablation:* Deleting `[ "${#MID}" -eq 32 ]` turned `TestMachineIDCheckScriptMirrorsDoctor` **RED**:
       `main_test.go:1485: shorthex refused for the wrong reason (content check not causal)`.
    2. *Symlink Guard Ablation:* Deleting `[ -L "$F" ]` turned `TestMachineIDCheckScriptMirrorsDoctor` **RED**:
       `main_test.go:1497: symlink refused for the wrong reason`.
    3. *Charset Guard Ablation:* Deleting `case "$MID" in *[!0-9a-f]*) ...` turned `TestMachineIDCheckScriptMirrorsDoctor` **RED**:
       `main_test.go:1485: spaced refused for the wrong reason (content check not causal)`.

---

## 3. Top-3 Weakest Points

1. **[LOW] Single-Line Sed Outer-Whitespace Substitution in `machine-id-check.sh`:**
   - *Location:* [`scripts/machine-id-check.sh:18`](file:///home/matej/HARNESS/nexus/scripts/machine-id-check.sh#L18).
   - *Evidence:* The sed pattern `:a; N; $!ba; s/...` terminates at EOF on single-line inputs before substitution commands execute. While standard `/etc/machine-id` (`32-hex\n`) has its trailing newline cleanly stripped by command substitution `MID="$(...)"` and passes with length 32, any single-line input with leading whitespace will retain that whitespace and be rejected by `[ "${#MID}" -eq 32 ]` (fail-closed false negative compared to Go's `strings.TrimSpace`).
2. **[LOW] Reminder Creation Slug Grammar vs `occurrenceIDRe`:**
   - *Location:* [`internal/obligation/tools.go:75`](file:///home/matej/HARNESS/nexus/internal/obligation/tools.go#L75) and [`cmd/nexus/main.go:1104`](file:///home/matej/HARNESS/nexus/cmd/nexus/main.go#L1104).
   - *Evidence:* `reminder_set` does not constrain `args.ID` with `taskIDOK` (`[A-Za-z0-9_-]+`). A reminder created with spaces (e.g. `occ-water plants#1`) will fail the Telegram `ack` regex and fall through to conversation (recoverable via `reminder_ack` tool).
3. **[LOW] Byte-Slice Truncation on Outbox Text:**
   - *Location:* [`cmd/nexus/main.go:1212`](file:///home/matej/HARNESS/nexus/cmd/nexus/main.go#L1212).
   - *Evidence:* `txt = txt[:80] + "…"` slices by byte length rather than rune count (`[]rune(txt)[:80]`). If a multi-byte UTF-8 character crosses byte 80, the slice produces invalid UTF-8 in the Telegram outbox response.

---

## 4. Verdict

The folds in `e2f4442` are verified, secure, and causal under negative ablation testing:
- `scripts/machine-id-check.sh` validates the entire file content, rejecting multiline, trailing garbage, and non-hex inputs.
- Test fixtures causally exercise every guard with reason assertions independent of root privileges.
- All repository test suites pass with zero regressions.

VERDICT: PASS
