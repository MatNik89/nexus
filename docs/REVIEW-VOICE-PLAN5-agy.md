# Review: Voice Messages Transcription (v5), Sandbox Input Registry (v3), & Telegram Egress Dialer

- **Repository**: `/home/matej/HARNESS/nexus`
- **Branch**: `slice/p1-voice`
- **Reviewed Commit**: `1f2b4990a613dac2f9c869c0a22a448b671bf306`
- **Reviewed Documents**:
  1. `docs/PLAN-VOICE.md` (v5)
  2. `docs/PLAN-SANDBOX-ROINPUT.md` (v3, startup InputRegistry)
  3. `docs/PLAN-TG-EGRESS-DIALER.md` (Telegram pinned-IP dialer, E11 prerequisite)
- **Reviewer**: Antigravity (`agy`)

---

## 1. Executive Summary

This review jointly evaluates the fifth revision (`v5`) of `PLAN-VOICE.md` and its two explicit prerequisite specifications: `PLAN-SANDBOX-ROINPUT.md` (`v3`) and `PLAN-TG-EGRESS-DIALER.md`.

All round-4 findings have been completely resolved and anchored to existing kernel mechanisms:
- **F1 (Replay via `chan_inbox`)**: Instead of introducing a parallel durable store or pre-admission transcript cache, the existing journal projection `chan_inbox` serves as the sole source of truth for admitted text (`E4/B7`). `AdmitOutcome` is extended to return the canonical text on replay; the adapter runs the canonical text, rendering any duplicate whisper transcription on redelivery completely harmless.
- **F2 (`InputRegistry` Lifecycle)**: Read-only inputs are decoupled from per-call `Spec`/`CompiledPolicy` allocations. An `InputRegistry` opens and holds file descriptors once at daemon startup, while per-call `Spec`s reference inputs by `ID`. This completely eliminates descriptor leaks (`EMFILE`) under high turn volume.
- **F3 (Identity vs. Content-Execution Attestation)**: The sandbox attestation contract is clarified: held file descriptors pin input **IDENTITY** (`dev, ino`, size, and startup tamper-evidence digest), rather than claiming memfd-sealed content-execution (which would consume 140 MB of resident RAM on a Raspberry Pi).
- **F4 (E11 Pinned-IP Dialer Prerequisite)**: `PLAN-TG-EGRESS-DIALER.md` formalizes a channel-wide, policy-compliant HTTP client (`Proxy: nil`, single-resolution pinned IP dialer, strict redirect rejection) fulfilling `ARCHITECTURE-ESSENTIALS.md` E11 without requiring any ad-hoc waivers for voice downloads.

The three designs form a cohesive, secure, and fully verified architecture.

---

## 2. In-Depth Architectural & Contract Verification

### 2.1. F1: Replay Determinism via `chan_inbox` (`PLAN-VOICE.md` v5)

- **Mechanics**:
  1. On first presentation of a voice message, the adapter downloads, transcribes, and submits the candidate text via `a.core.Admit(ctx, in)`.
  2. The journal projection `chan_inbox` records `(message_id, adapter_id, channel_identity, update_id, text, status, created)` on `EvInboundAdmitted` (`internal/channel/channel.go:225-241`).
  3. `AdmitOutcome` (`internal/channel/channel.go:72-75`) is extended:
     ```go
     type AdmitOutcome struct {
         MessageID string
         Text      string // Canonical admitted text (persisted in chan_inbox)
         Replayed  bool
     }
     ```
  4. On duplicate/redelivered update, `Admit` loads the previously stored text from `chan_inbox` and returns `AdmitOutcome{MessageID: id, Text: canonicalText, Replayed: true}`.
  5. The adapter (`internal/channel/telegram/telegram.go:249-275`) updates `in.Text = outcome.Text` before invoking `a.handle(ctx, in)`.
- **Verdict on Correctness**:
  - Eliminates state divergence from non-deterministic whisper runs without requiring a secondary cache.
  - Aligns with single-write-owner journal discipline (`HARDQ B7`, `PRD §6`).

### 2.2. F2 & F3: Startup `InputRegistry` & Identity Attestation (`PLAN-SANDBOX-ROINPUT.md` v3)

- **Startup Lifecycle (`InputRegistry`)**:
  - Configured inputs are opened at startup with `O_NOFOLLOW|O_CLOEXEC`.
  - Initial validation: regular file (`Mode().IsRegular()`), owned by daemon UID or root, non-group/other-writable, `nlink == 1`, $\le \text{MaxSize}$.
  - Initial SHA-256 hash computed and stored as startup tamper-evidence.
  - The resulting `*os.File` descriptor is held across daemon runtime.
- **Per-Call Referencing**:
  - `sandbox.Spec` receives lightweight `Inputs []InputRef` (`{ID, Dest}`).
  - `Dest` is validated: `filepath.Clean(Dest) == Dest`, absolute, direct child of `/inputs/` (e.g. `/inputs/model.bin`), with no collisions or path traversals against `/tmp`, `/work`, `/nexus-target`, `/nexus-libs`, `/proc`, or `/dev`.
  - `Launch` passes the held descriptor via `cmd.ExtraFiles` and binds via `bwrap --ro-bind-fd <N> <Dest>` (or `--ro-bind /proc/self/fd/<N> <Dest>`).
- **Attestation Contract**:
  - `policyHash` and launch `Attestation` fold `{ID, normalizedDest, dev, ino, startupDigest}`.
  - Explicitly documents that identity pinning guarantees the namespace reads the exact inode held by the daemon, with in-place same-UID rewrites treated as a stated single-user trusted-file ceiling.
- **Verdict on Correctness**:
  - Prevents fd exhaustion (`EMFILE`).
  - Correctly captures tamper-evidence without allocating 140 MB of RAM for memfd seals.

### 2.3. F4: Channel-Wide Egress Dialer (`PLAN-TG-EGRESS-DIALER.md`)

- **E11 Compliance**:
  - `Proxy: nil` ignores ambient `HTTP_PROXY` and `HTTPS_PROXY` environment variables.
  - `DialContext`: Enforces that the requested host matches `config.EgressAllow` and the configured Telegram API host. Resolves IP set once, dials the pinned literal IP, and preserves TLS SNI (`ServerName = host`).
  - `CheckRedirect`: Rejects all HTTP 3xx redirects unconditionally.
  - Bot token sanitization is preserved across dial and transport errors.
- **Integration**:
  - Shared across standard Telegram polling (`getUpdates`), sending (`sendMessage`), and voice media downloads (`getFile` + file download).
- **Verdict on Correctness**:
  - Fully closes the E11 gap without waivers.

---

## 3. Top-3 Implementation Notes / Weakest Points

In accordance with review discipline, the following details should be accounted for during coding:

1. **Projection Query Synchronization in `Admit`**:
   - *Observation*: `Admit` queries `chan_inbox` on duplicate detection.
   - *Detail*: When a concurrent duplicate update fails against the journal's `UNIQUE` constraint, `Admit` should query `chan_inbox` for the canonical text. The helper must ensure proper read transactions on `ProjDB`.
2. **DNS Resolution Handling in `DialContext`**:
   - *Observation*: `net.LookupIP` returns multiple A/AAAA records for `api.telegram.org`.
   - *Detail*: The dialer should iterate through the resolved IP candidates or select one deterministically, ensuring every candidate IP is checked against policy before dialing.
3. **Directory Provisioning in `probe.Prepare`**:
   - *Observation*: Bubblewrap requires mount points to exist inside the target root filesystem.
   - *Detail*: `probe.Prepare` (`internal/preflight/probe/probe.go:605-620`) should include `--dir /inputs` when `len(Inputs) > 0` before appending `--ro-bind-fd` flags.

---

## 4. Verification & Detector Adequacy

The detector suites across all three documents provide comprehensive RED-to-GREEN coverage:

- **`PLAN-TG-EGRESS-DIALER.md`**:
  - `PROXY-IGNORED`: Proves ambient `HTTPS_PROXY` is ignored.
  - `REDIRECT-REFUSED`: Proves 3xx responses return typed errors and abort.
  - `REBIND-REFUSED`: Proves that late DNS rebinding to off-policy IPs is rejected.
  - `HOST-DENY`: Proves unlisted hosts are rejected before connect.
  - `TLS-SNI-PRESERVED`: Proves SNI validation against the original hostname remains intact.
- **`PLAN-SANDBOX-ROINPUT.md` (v3)**:
  - `GRANTED-READABLE`: Proves registered model is readable at `/inputs/<name>`.
  - `NEIGHBOR-INVISIBLE`: Proves ungranted sibling files are inaccessible.
  - `SWAP-DEFEATED`: Proves post-startup pathname modifications do not affect the held inode read by the sandbox.
  - `NO-FD-LEAK`: Verifies flat open file descriptor counts across $N$ sequential runs.
  - `POLICY-IDENTITY`: Verifies distinct `policyHash` and attestation digests across different inputs.
  - `DEST-COLLISION`: Asserts typed compile errors on path traversal or collisions.
  - `REJECT-AT-STARTUP`: Asserts failure on symlinks, FIFOs, `nlink > 1`, and over-limit files.
  - `EMPTY-UNCHANGED`: Asserts byte-identical baseline argv when `Inputs` is empty.
- **`PLAN-VOICE.md` (v5)**:
  - `media.Transcribe unit`: Tests transcription, non-zero exits, empty transcripts, and `-nt` flag enforcement.
  - `output-escape`: Tests symlink-to-canary and FIFO outputs, proving rejection and non-blocking behavior.
  - `replay-determinism`: Verifies that candidate transcript $B$ produced on redelivery is discarded in favor of stored canonical transcript $A$.
  - `download-client`: Asserts $20\text{ MiB} + 1$ byte cap rejection and scheme/host pre-validation.
  - `adapter failure path`: Validates typed Croatian refusal generation and zero inbox entries on failure.
  - `injection`: Confirms structured argv isolation without shell interpolation.

---

VERDICT: PASS
