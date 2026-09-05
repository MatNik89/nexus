# P0-PREP Review Round 3 — agy

**Target:** Repo `/home/matej/HARNESS/nexus`, branch `slice/p0-prep1`, HEAD `7b7e5f4` (`fix(prep1-r2): fold codex round 2 (#1 HIGH machine-id trust+validity, #2 MED full-destination guard)`).  
**Reviewer:** `agy` (Antigravity).  
**Scope:** Re-verification of Codex round-2 findings folded into `7b7e5f4` on top of `d780b51`. Working tree was never modified during verification.

---

## 1. Test Suite Verification

- Repository full suite: `CGO_ENABLED=0 go test -p 4 -count=1 -timeout 600s ./...` across all 34 packages: **PASS**, exit 0 (0 `FAIL` / `panic`).
- Acceptance suite: `internal/acceptance` passed in 84s.
- CLI & Daemon suite: `cmd/nexus` passed in 6.7s (`TestMachineIDValidation` and `TestOutboxIsDestinationBound` passed).

---

## 2. Verification of Round 2 Folds

### Fold #1: Trustworthy Machine Identity File & Strict Content Validation (Codex #1)

- **Audit & Implementation:**
  - `machineIDSHAAt` ([`cmd/nexus/main.go:994-1016`](file:///home/matej/HARNESS/nexus/cmd/nexus/main.go#L994-L1016)) enforces trust-root requirements:
    - Path must be a regular file (`st.Mode().IsRegular()`).
    - Stat must be root-owned (`sys.Uid == 0`) and not group- or world-writable (`st.Mode().Perm()&0o022 == 0`).
  - `validateMachineID` ([`cmd/nexus/main.go:1019-1036`](file:///home/matej/HARNESS/nexus/cmd/nexus/main.go#L1019-L1036)) enforces exact systemd machine-ID syntax: exactly 32 lowercase hex characters (`[0-9a-f]{32}`) and non-zero (rejecting all-zeros uninitialized IDs).
  - `scripts/p0-accept.sh:29-35` mirrors these exact checks (`stat -c '%u'`, `stat -c '%a'`, grep `^[0-9a-f]{32}$`, non-zero check) before writing `acceptance.json`.
  - [`TestMachineIDValidation`](file:///home/matej/HARNESS/nexus/cmd/nexus/main_test.go#L1400-L1426) exercises 6 invalid shapes, user-owned file rejection, and the valid root-owned host identity.
- **Causal Negative Ablation Probe:**
  - In `/tmp/nexus-p0-prep1-r3-probe`, disabling the non-zero check in `validateMachineID` turned [`TestMachineIDValidation`](file:///home/matej/HARNESS/nexus/cmd/nexus/main_test.go#L1400-L1426) **RED**:
    `main_test.go:1406: invalid machine id "00000000000000000000000000000000" accepted`.

---

### Fold #2: Full-Destination (Adapter + Identity) Atomic Reconciliation Guard (Codex #2)

- **Audit & Implementation:**
  - In [`internal/channel/channel.go:289-300`](file:///home/matej/HARNESS/nexus/internal/channel/channel.go#L289-L300), `EvOutboundResolved` projection requires both `p.Adapter` and `p.Identity` if either is specified, executing:
    `UPDATE chan_outbox SET status=? WHERE delivery_id=? AND status='UNKNOWN' AND adapter_id=? AND channel_identity=?`.
  - If only one of the destination fields is provided, it fails closed with `"channel: destination guard requires BOTH adapter and identity (fail closed)"`.
  - `telegramHandler` routes redeliveries through `b.chanCore.ReconcileFor(ctx, id, false, "telegram", in.ChannelIdentity, source)` ([`cmd/nexus/main.go:1267`](file:///home/matej/HARNESS/nexus/cmd/nexus/main.go#L1267)).
  - [`TestOutboxIsDestinationBound`](file:///home/matej/HARNESS/nexus/cmd/nexus/main_test.go#L1321-L1396) verifies that a cross-adapter delivery sharing the same channel identity string cannot be re-pended via the Telegram command.
- **Causal Negative Ablation Probe:**
  - In `/tmp/nexus-p0-prep1-r3-probe`, stripping `adapter_id=?` from the SQL update in `internal/channel/channel.go` turned [`TestOutboxIsDestinationBound`](file:///home/matej/HARNESS/nexus/cmd/nexus/main_test.go#L1321-L1396) **RED**:
    `main_test.go:1392: telegram command re-pended a foreign-adapter delivery: "Re-queued dlv-..."`.

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
   - *Evidence:* `UnreconciledFor` does not impose a SQL `LIMIT`. If many UNKNOWN rows accumulate for a single destination, concatenating all entries may exceed Telegram's 4096-character message length limit.

---

## 4. Verdict

The folds in `7b7e5f4` are verified, secure, and causal under negative ablation testing:
- Machine identity trust root and content validation fail closed on untrusted or malformed files.
- Full destination binding (`adapter_id` + `channel_identity`) prevents cross-adapter and sibling-chat reconciliation leaks.
- All tests pass cleanly across the repository with zero regressions.

VERDICT: PASS
