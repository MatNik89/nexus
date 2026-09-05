# P0-PREP Review Round 4 — agy

**Target:** Repo `/home/matej/HARNESS/nexus`, branch `slice/p0-prep1`, HEAD `7c883be` (commits `63a61a5` + `7c883be` folding Codex round-3 findings on top of `7b7e5f4`).  
**Reviewer:** `agy` (Antigravity).  
**Scope:** Re-verification of Codex round-3 findings folded into `7c883be`. Working tree was never modified during verification.

---

## 1. Test Suite Verification

- Repository full suite: `CGO_ENABLED=0 go test -p 4 -count=1 -timeout 600s ./...` across all 34 packages: **PASS**, exit 0 (0 `FAIL` / `panic`).
- Acceptance suite: `internal/acceptance` passed in 72s (`TestDoctorP0GrantLive` passed).
- CLI & Daemon suite: `cmd/nexus` passed in 6.5s (`TestMachineIDValidation` and `TestMachineIDCheckScriptMirrorsDoctor` passed).

---

## 2. Verification of Round 3 Folds

### Fold #1: Shared Machine-ID Check Script Mirroring Verifier ([`scripts/machine-id-check.sh`](file:///home/matej/HARNESS/nexus/scripts/machine-id-check.sh), [`scripts/p0-accept.sh`](file:///home/matej/HARNESS/nexus/scripts/p0-accept.sh))

- **Audit & Implementation:**
  - `scripts/machine-id-check.sh` was extracted to unify identity validation across producer (`p0-accept.sh`) and verifier (`nexus doctor --p0` / `machineIDSHAAt`):
    - `[ -L "$F" ] && exit 2`: Symlinks are rejected fail-closed.
    - `[ -f "$F" ] || exit 2`: Non-regular files are rejected fail-closed.
    - `[ $(( 0$PERMS & 022 )) -eq 0 ] || exit 2`: Numeric bitwise arithmetic (`0perm & 022`) correctly detects and refuses group/world-writable modes (`602`, `642`, `646`, `622`) that were missed by simple globs.
    - `[ "$(stat -c '%u' "$F")" = "0" ] || exit 2`: Root-ownership is verified.
    - `IFS= read -r MID < "$F"`: First line is read verbatim without interior-whitespace deletion, rejecting embedded-whitespace strings.
    - `printf '%s' "$MID" | grep -Eq '^[0-9a-f]{32}$'` and non-zero check enforce valid 32-hex systemd ID format.
  - The script checks permissions *before* ownership, allowing the permission test assertion to be causal even when run under non-root.
  - [`TestMachineIDCheckScriptMirrorsDoctor`](file:///home/matej/HARNESS/nexus/cmd/nexus/main_test.go#L1434-L1502) verifies all world-writable modes (asserting the `"writable"` refusal reason on explicit chmod fixtures), embedded whitespace rejection, symlink rejection, user-owned file rejection, and the valid host `/etc/machine-id`.
- **Causal Negative Ablation Probe:**
  - In `/tmp/nexus-p0-prep1-r4-probe`, reverting the numeric perm check in `scripts/machine-id-check.sh` to the old broken glob turned [`TestMachineIDCheckScriptMirrorsDoctor`](file:///home/matej/HARNESS/nexus/cmd/nexus/main_test.go#L1434-L1502) **RED**:
    `main_test.go:1470: mode 602 refused for the wrong reason (perm check not causal): ".../ww-602 not root-owned (fail closed)"`.

---

### Fold #2: Root-Run Test Portability ([`cmd/nexus/main_test.go:1415-1423`](file:///home/matej/HARNESS/nexus/cmd/nexus/main_test.go#L1415-L1423), [`1488-1492`](file:///home/matej/HARNESS/nexus/cmd/nexus/main_test.go#L1488-L1492))

- **Audit & Implementation:**
  - User-owned identity file assertions in `TestMachineIDValidation` and `TestMachineIDCheckScriptMirrorsDoctor` are conditionally gated on `if os.Geteuid() != 0`.
  - When the test suite is executed as UID 0 (root), where newly created temp files are inherently root-owned, the test skips the user-owned rejection check rather than producing a false negative.

---

## 3. Top-3 Weakest Points

1. **[LOW] Reminder Creation Slug Grammar vs `occurrenceIDRe`:**
   - *Location:* [`internal/obligation/tools.go:75`](file:///home/matej/HARNESS/nexus/internal/obligation/tools.go#L75) and [`cmd/nexus/main.go:1104`](file:///home/matej/HARNESS/nexus/cmd/nexus/main.go#L1104).
   - *Evidence:* `reminder_set` does not constrain `args.ID` with `taskIDOK` (`[A-Za-z0-9_-]+`). A reminder created with spaces (e.g. `occ-water plants#1`) will fail the Telegram `ack` regex and fall through to conversation (recoverable via `reminder_ack` tool).
2. **[LOW] Byte-Slice Truncation on Outbox Text:**
   - *Location:* [`cmd/nexus/main.go:1212`](file:///home/matej/HARNESS/nexus/cmd/nexus/main.go#L1212).
   - *Evidence:* `txt = txt[:80] + "…"` slices by byte length rather than rune count (`[]rune(txt)[:80]`). If a multi-byte UTF-8 character crosses byte 80, the slice produces invalid UTF-8 in the Telegram outbox response.
3. **[LOW] Unbounded Outbox Listing Response Length:**
   - *Location:* [`cmd/nexus/main.go:1205-1221`](file:///home/matej/HARNESS/nexus/cmd/nexus/main.go#L1205-L1221).
   - *Evidence:* `UnreconciledFor` has no SQL `LIMIT` clause. If many UNKNOWN rows accumulate for a single destination, concatenating all entries may exceed Telegram's 4096-character message length limit.

---

## 4. Verdict

The folds in `7c883be` are verified, secure, and causal under negative ablation testing:
- `scripts/machine-id-check.sh` perfectly mirrors `nexus doctor --p0` validation (numeric perm arithmetic, symlink rejection, verbatim first-line reading).
- Root-run test portability is handled cleanly.
- Full repository test suite passes with zero regressions.

VERDICT: PASS
