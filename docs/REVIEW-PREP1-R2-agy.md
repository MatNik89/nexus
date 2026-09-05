# P0-PREP Review Round 2 — agy

**Target:** Repo `/home/matej/HARNESS/nexus`, branch `slice/p0-prep1`, HEAD `d780b51` (`fix(prep1-r1): fold codex round 1 (#1 HIGH machine binding, #2 MED destination binding)`).  
**Reviewer:** `agy` (Antigravity).  
**Scope:** Re-verification of Codex round-1 findings folded into `d780b51` on top of `eb0d6f8`. Working tree never modified during verification.

---

## 1. Test Suite Verification

- Repository full suite: `CGO_ENABLED=0 go test -p 4 -count=1 -timeout 600s ./...` across all 34 packages: **PASS**, exit 0 (0 `FAIL` / `panic`).
- Acceptance suite: `internal/acceptance` passed in 79s (`TestDoctorP0GrantLive` passed).
- CLI & Daemon suite: `cmd/nexus` passed in 8.5s (`TestOutboxIsDestinationBound` passed).

---

## 2. Verification of Round 1 Folds

### Fold #1: Machine Identity Binding & Fail-Closed Host Introspection (Codex #1)

- **Audit & Implementation:**
  - `machineIDSHA()` ([`cmd/nexus/main.go:985-998`](file:///home/matej/HARNESS/nexus/cmd/nexus/main.go#L985-L998)) reads `/etc/machine-id` (root-owned, stable machine identity), trims whitespace, and computes its SHA256 hex string.
  - `scripts/p0-accept.sh:31` hashes `/etc/machine-id` (`machine_id_sha256`) and embeds it in the signed `acceptance.json`.
  - In `verifyAcceptanceAttestation` ([`cmd/nexus/main.go:888-904`](file:///home/matej/HARNESS/nexus/cmd/nexus/main.go#L888-L904)), both `runtimeHostID()` and `machineIDSHA()` fail closed on read errors (returning descriptive errors instead of sentinels like `"unknown"`), and enforce `att.Host == host && att.MachineID == mid`.
- **Causal Negative Ablation Probe:**
  - In `/tmp/nexus-p0-prep1-r2-probe`, disabling `att.MachineID != mid` check caused [`TestDoctorP0GrantLive`](file:///home/matej/HARNESS/nexus/internal/acceptance/acceptance_linux_test.go#L977-L994) to fail **RED**:
    `acceptance_linux_test.go:992: same-kernel other-machine attestation granted (exit 0)`.

---

### Fold #2: Destination-Bound Outbox Listing & Atomic Reconciliation (Codex #2)

- **Audit & Implementation:**
  - `UnreconciledFor(ctx, "telegram", in.ChannelIdentity)` ([`cmd/nexus/main.go:1205`](file:///home/matej/HARNESS/nexus/cmd/nexus/main.go#L1205) and [`internal/channel/channel.go:601-618`](file:///home/matej/HARNESS/nexus/internal/channel/channel.go#L601-L618)) scopes `outbox` listings strictly to `WHERE status='UNKNOWN' AND adapter_id=? AND channel_identity=?`.
  - `redeliver <dlv-id>` routes to `chanCore.ReconcileFor(ctx, id, false, in.ChannelIdentity, source)` ([`cmd/nexus/main.go:1228`](file:///home/matej/HARNESS/nexus/cmd/nexus/main.go#L1228)), which passes `Identity` and `Source` to the `EvOutboundResolved` event.
  - In `internal/channel/channel.go:289-296`, the projection query executes `UPDATE chan_outbox SET status=? WHERE delivery_id=? AND status='UNKNOWN' AND channel_identity=?` in a single atomic SQL statement with `RowsAffected() == 1` assertion.
  - A sibling chat on the same profile sees a clean outbox, and attempting to redeliver another chat's delivery ID affects 0 rows, failing closed with `"Redeliver failed: channel: reconciliation for a non-UNKNOWN delivery (fail closed)"` while leaving the delivery row untouched in `UNKNOWN` state.
- **Causal Negative Ablation Probes:**
  - *Listing Filter Probe:* Neutralizing `UnreconciledFor` $\to$ `Unreconciled` caused [`TestOutboxIsDestinationBound`](file:///home/matej/HARNESS/nexus/cmd/nexus/main_test.go#L1321-L1371) to fail **RED**:
    `main_test.go:1339: sibling chat saw another chat's delivery`.
  - *Projection Guard Probe:* Neutralizing `AND channel_identity=?` in `Apply(EvOutboundResolved)` caused [`TestOutboxIsDestinationBound`](file:///home/matej/HARNESS/nexus/cmd/nexus/main_test.go#L1321-L1371) to fail **RED**:
    `main_test.go:1357: sibling chat re-pended another chat's delivery`.

---

## 3. Top-3 Weakest Points

1. **[LOW] Reminder Creation Slug Grammar vs `occurrenceIDRe`:**
   - *Location:* [`internal/obligation/tools.go:75`](file:///home/matej/HARNESS/nexus/internal/obligation/tools.go#L75) and [`cmd/nexus/main.go:1104`](file:///home/matej/HARNESS/nexus/cmd/nexus/main.go#L1104).
   - *Evidence:* `reminder_set` does not restrict `args.ID` with `taskIDOK` (`[A-Za-z0-9_-]+`). If an unconstrained reminder ID with spaces (e.g. `occ-water plants#1`) is delivered, replying `ack occ-water plants#1` fails regex match and falls through to conversation (recoverable via `reminder_ack` tool).
2. **[LOW] Byte-Slice Truncation on Outbox Text:**
   - *Location:* [`cmd/nexus/main.go:1212`](file:///home/matej/HARNESS/nexus/cmd/nexus/main.go#L1212).
   - *Evidence:* `txt = txt[:80] + "…"` truncates by byte length rather than rune count (`[]rune(txt)[:80]`). If a multi-byte UTF-8 character crosses byte 80, the slice produces invalid UTF-8 in the Telegram response string.
3. **[LOW] Unbounded Outbox Listing Response Length:**
   - *Location:* [`cmd/nexus/main.go:1205-1221`](file:///home/matej/HARNESS/nexus/cmd/nexus/main.go#L1205-L1221).
   - *Evidence:* `UnreconciledFor` does not impose a SQL `LIMIT`. If many UNKNOWN rows accumulate for a single chat, concatenating all entries may exceed Telegram's 4096-character message length limit.

---

## 4. Verdict

The folded changes in `d780b51` cleanly and causally resolve all round-1 findings:
- Host binding is hardened with `/etc/machine-id` digest verification against same-kernel/different-host transfers.
- Outbox listing and redelivery are destination-bound and guarded by atomic SQL predicates.
- No new defects or regressions were introduced across the test suite.

VERDICT: PASS
