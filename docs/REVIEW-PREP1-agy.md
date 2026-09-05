# P0-PREP Review Round 1 — agy

**Target:** Repo `/home/matej/HARNESS/nexus`, branch `slice/p0-prep1`, HEAD `eb0d6f8` (`feat(p0-prep1): pre-P1 fix-batch — three known dead ends`).  
**Reviewer:** `agy` (Antigravity).  
**Methodology:** Exhaustive code audit, static inspection, test suite execution in repo, and empirical verification with negative causal ablation probes in clean `git archive HEAD` exports (`/tmp/nexus-p0-prep1-probe`). The working tree was never modified during probes.

---

## 1. Test Suite Verification

- `CGO_ENABLED=0 go vet ./...`: Exited 0 (clean).
- `CGO_ENABLED=0 go test -count=1 ./...`: All 34 packages passed (`ok`, acceptance 67s, no test failures).

---

## 2. Adversarial Review of Fixes

### Fix #1: Telegram "outbox" Listing & Human-Confirmed "redeliver <dlv-id>" Reconciliation (`cmd/nexus/main.go:1174-1202`)

- **Mechanism:**
  - `outbox` queries `b.chanCore.Unreconciled(ctx)` to fetch deliveries with `status='UNKNOWN'` from the profile's journal outbox table (`chan_outbox`).
  - `redeliver <dlv-id>` validates ID format against `deliveryIDRe` and invokes `b.chanCore.Reconcile(ctx, id, false)`.
  - In `internal/channel/channel.go:274-290`, `EvOutboundResolved` projection runs `UPDATE chan_outbox SET status='PENDING' WHERE delivery_id=? AND status='UNKNOWN'`.
- **Adversarial Audit:**
  1. *Resurrecting SENT/completed rows:* Impossible. The SQL projection strictly filters on `status='UNKNOWN'` and verifies `RowsAffected() == 1` (`channel.go:287-289`). If a row is `SENT`, `PENDING`, or non-existent, 0 rows match and the projection fails closed with `"channel: reconciliation for a non-UNKNOWN delivery (fail closed)"`.
  2. *Foreign-chat redelivery:* All inbound messages are gated at admission by `telegram.Adapter.processUpdate` (`telegram.go:217-225`), which rejects non-private or unbound chats before `telegramHandler` is reached. Outbox tables are per-profile journal databases (`contracts.ProfileID`), preventing cross-profile visibility. Furthermore, when `chanCore.Flush` delivers the re-pended row, it routes strictly to the original `ChannelIdentity` stored in `chan_outbox`, not the sender of the `redeliver` command.
  3. *ID Format Abuse:* `deliveryIDRe` (`^dlv-[0-9a-f]{24}$`) restricts input to canonical 24-hex IDs, neutralizing SQL/command injection and malformed inputs (which fall through to conversation).
  4. *Data Leakage in Listing:* `Unreconciled` queries only the profile's own journal outbox where sensitive tokens have already passed journal redaction.
- **Causal Ablation Probe:**
  - In a clean export, neutralizing `b.chanCore.Reconcile(ctx, id, false)` caused [`TestOutboxRedeliverCommand`](file:///home/matej/HARNESS/nexus/cmd/nexus/main_test.go#L1272-L1318) to fail **RED**:
    `main_test.go:1306: re-queued delivery did not send: 0`.

---

### Fix #2: Attestation Host Binding (`cmd/nexus/main.go:884-889`, `955-971`)

- **Mechanism:**
  - `verifyAcceptanceAttestation` compares `att.Host` against `runtimeHostID()`, which formats `syscall.Uname` into `Sysname Release Machine`.
  - `scripts/p0-accept.sh` generates `acceptance.json` embedding `$(uname -srm)` and signs the attestation with `ssh-keygen -Y sign -f "$KEY" -n nexus-acceptance`.
- **Adversarial Audit:**
  1. *Format Divergence:* Verified that `conv(u.Sysname) + " " + conv(u.Release) + " " + conv(u.Machine)` in [`main.go:955-971`](file:///home/matej/HARNESS/nexus/cmd/nexus/main.go#L955-L971) matches standard `uname -srm` output exactly across Linux architectures without trailing nulls or delimiters.
  2. *Spoofability:* The entire `acceptance.json` payload (including `host`) is covered by the SSH release signature verified against `allowed_signers` (anchored to the binary's `acceptanceSignerFingerprint`). Modifying `host` invalidates the signature without the owner's private `NEXUS_RELEASE_KEY`.
  3. *Empty Host:* An empty or absent `host` in `acceptance.json` fails `att.Host != runtimeHostID()`, failing closed.
- **Causal Ablation Probe:**
  - In a clean export, disabling `att.Host != runtimeHostID()` caused [`TestDoctorP0GrantLive`](file:///home/matej/HARNESS/nexus/internal/acceptance/acceptance_linux_test.go#L920-L968) foreign-host branch to fail **RED**:
    `acceptance_linux_test.go:963: foreign-host attestation granted (exit 0)`.

---

### Fix #3: Strict Command-ID Routing (`cmd/nexus/main.go:1101-1106`, `1120-1203`)

- **Mechanism:**
  - Regexes `challengeIDRe = ^ch-[0-9a-f]{24}$`, `occurrenceIDRe = ^occ-[A-Za-z0-9_.-]+#[0-9]+$`, and `deliveryIDRe = ^dlv-[0-9a-f]{24}$` gate command routing in `telegramHandler`.
  - Messages starting with `approve `, `deny `, `retry `, `ack `, or `redeliver ` only trigger command dispatch if followed by an exact matching ID; otherwise they fall through to `RunChannelTurn` as ordinary conversation.
- **Adversarial Audit:**
  1. *False Interceptions:* Sentences starting with command words (e.g. `"approve my vacation plan"`, `"deny the request politely"`, `"ack that you understood me"`, `"retry the download again"`, `"redeliver the package tomorrow"`) evaluate `MatchString(...) == false` and pass to conversation turn.
  2. *Regex Safety (ReDoS & Panics):* Patterns are linear, anchored with `^` and `$`, have no nested quantifiers, and are compiled once at startup via `regexp.MustCompile`.
- **Causal Ablation Probe:**
  - In a clean export, stripping regex checks from `telegramHandler` command branches caused [`TestCommandWordsNeedExactIDs`](file:///home/matej/HARNESS/nexus/cmd/nexus/main_test.go#L1236-L1269) to fail **RED**:
    `main_test.go:1254: ordinary sentence "approve my vacation plan" swallowed as a command: "Approval failed: approval: unknown challenge (fail closed)"`.

---

## 3. Top-3 Weakest Points / Latent Findings

1. **[LOW] Reminder ID Creation Alphabet vs `occurrenceIDRe` Restriction:**
   - *Location:* [`internal/obligation/tools.go:75`](file:///home/matej/HARNESS/nexus/internal/obligation/tools.go#L75) and [`cmd/nexus/main.go:1104`](file:///home/matej/HARNESS/nexus/cmd/nexus/main.go#L1104).
   - *Evidence:* `task_create` enforces `taskIDOK` (`[A-Za-z0-9_-]+`), but `reminder_set` validates only `args.ID != ""`. If an LLM generates a reminder ID containing spaces or non-slug characters (e.g. `id="water plants"` $\to$ `occ-water plants#1`), the reminder delivers successfully, but the user's manual response `ack occ-water plants#1` will not match `occurrenceIDRe` and will fall through to conversation instead of acknowledging the occurrence. (Remains recoverable via `reminder_ack` tool).
2. **[LOW] Outbox Text Truncation Byte-Slicing vs Rune Boundaries:**
   - *Location:* [`cmd/nexus/main.go:1189`](file:///home/matej/HARNESS/nexus/cmd/nexus/main.go#L1189).
   - *Evidence:* `txt = txt[:80] + "…"` slices `txt` at byte offset 80 rather than rune offset (`[]rune(txt)[:80]`). If a multi-byte UTF-8 character spans byte 80, the slice produces invalid UTF-8 in the Telegram `outbox` summary response.
3. **[LOW] Unbounded Outbox Listing Response Length:**
   - *Location:* [`cmd/nexus/main.go:1178-1194`](file:///home/matej/HARNESS/nexus/cmd/nexus/main.go#L1178-L1194).
   - *Evidence:* `chanCore.Unreconciled(ctx)` returns all rows with `status='UNKNOWN'` without limit or pagination. If dozens of unreconciled messages accumulate, concatenating all rows can exceed Telegram's 4096-character message payload ceiling, causing Telegram `sendMessage` to fail with HTTP 400.

---

## 4. Verdict

All three fixes are verified, secure, and causal under negative ablation testing:
- Outbox reconciliation is human-gated, fail-closed against non-UNKNOWN rows, and safe against cross-profile leaks.
- Attestation host binding accurately matches kernel identity and is protected by cryptographic release signatures.
- Command-id routing prevents conversation interception while preserving exact command handling.

VERDICT: PASS
