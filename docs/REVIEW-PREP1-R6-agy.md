# P0-PREP Review Round 6 — agy

**Target:** Repo `/home/matej/HARNESS/nexus`, branch `slice/p0-prep1`, HEAD `6c142b9` (`fix(prep1-r5): NUL bytes refuse before shell variable load (codex r5)`).  
**Reviewer:** `agy` (Antigravity).  
**Scope:** Re-verification of Codex round-5 finding folded into `6c142b9` on top of `e2f4442`. Working tree was never modified during verification.

---

## 1. Test Suite Verification

- Repository full suite: `CGO_ENABLED=0 go test -p 1 -count=1 ./...` across all 34 packages: **PASS**, exit 0 (0 `FAIL` / `panic`).
- Acceptance suite: `internal/acceptance` passed in 80.9s (`TestDoctorP0GrantLive` passed).
- CLI & Daemon suite: `cmd/nexus` passed in 0.55s (`TestMachineIDCheckScriptMirrorsDoctor` passed).

---

## 2. Verification of Round 5 Fold

### Fold #1: NUL-Byte Pre-Check Before Shell Variable Load ([`scripts/machine-id-check.sh:15-19`](file:///home/matej/HARNESS/nexus/scripts/machine-id-check.sh#L15-L19))

- **Audit & Implementation:**
  - Shell command substitutions silently strip NUL bytes (`\x00`). Without a raw byte check, a NUL-spliced machine identity (e.g. 16 hex + NUL + 16 hex = 33 bytes) would be stripped to 32 hex chars by `/bin/sh`, passing shell validation while Go's `machineIDSHAAt` (using `strings.TrimSpace` which preserves NUL) rejects it as 33 bytes.
  - In `scripts/machine-id-check.sh`:
    ```sh
    RAW_LEN="$(wc -c < "$F")"
    NONUL_LEN="$(tr -d '\000' < "$F" | wc -c)"
    [ "$RAW_LEN" -eq "$NONUL_LEN" ] || { echo "$F content malformed (fail closed)" >&2; exit 2; }
    ```
    This comparison executes before content is loaded into shell variables, ensuring any NUL byte causes immediate rejection with `"content malformed (fail closed)"`.
  - In [`cmd/nexus/main_test.go:1480`](file:///home/matej/HARNESS/nexus/cmd/nexus/main_test.go#L1480), `TestMachineIDCheckScriptMirrorsDoctor` includes the `"nulsplice"` fixture (`"0123456789abcdef\x000123456789abcdef\n"`), asserting that it is refused specifically with the `"malformed"` reason.
- **Causal Negative Ablation Probe:**
  - In `/tmp/nexus-p0-prep1-r6-probe`, neutralizing the raw vs NUL-stripped byte count check turned [`TestMachineIDCheckScriptMirrorsDoctor`](file:///home/matej/HARNESS/nexus/cmd/nexus/main_test.go#L1434-L1505) **RED**:
    `main_test.go:1488: nulsplice refused for the wrong reason (content check not causal): ".../nulsplice not root-owned (fail closed)"`.

---

## 3. Top-3 Weakest Points

1. **[LOW] Single-Line Sed Outer-Whitespace Substitution in `machine-id-check.sh`:**
   - *Location:* [`scripts/machine-id-check.sh:23`](file:///home/matej/HARNESS/nexus/scripts/machine-id-check.sh#L23).
   - *Evidence:* The sed pattern `:a; N; $!ba; s/...` terminates at EOF on single-line inputs before substitution commands execute. While standard `/etc/machine-id` (`32-hex\n`) has its trailing newline stripped by shell command substitution `MID="$(...)"` and passes length 32, single-line files with leading whitespace retain that whitespace and fail the 32-character length check (fail-closed false negative compared to Go's `strings.TrimSpace`).
2. **[LOW] Reminder Creation Slug Grammar vs `occurrenceIDRe`:**
   - *Location:* [`internal/obligation/tools.go:75`](file:///home/matej/HARNESS/nexus/internal/obligation/tools.go#L75) and [`cmd/nexus/main.go:1104`](file:///home/matej/HARNESS/nexus/cmd/nexus/main.go#L1104).
   - *Evidence:* `reminder_set` does not constrain `args.ID` with `taskIDOK` (`[A-Za-z0-9_-]+`). A reminder created with spaces (e.g. `occ-water plants#1`) will fail the Telegram `ack` regex and fall through to conversation (recoverable via `reminder_ack` tool).
3. **[LOW] Byte-Slice Truncation on Outbox Text:**
   - *Location:* [`cmd/nexus/main.go:1212`](file:///home/matej/HARNESS/nexus/cmd/nexus/main.go#L1212).
   - *Evidence:* `txt = txt[:80] + "…"` slices by byte length rather than rune count (`[]rune(txt)[:80]`), risking malformed UTF-8 if a multi-byte UTF-8 character crosses byte 80.

---

## 4. Verdict

The fold in `6c142b9` is verified, secure, and causal under negative ablation testing:
- NUL bytes are detected and refused fail-closed before shell variable normalization.
- The `nulsplice` test fixture verifies the `"malformed"` reason causally.
- Full repository test suite passes with zero regressions.

VERDICT: PASS
