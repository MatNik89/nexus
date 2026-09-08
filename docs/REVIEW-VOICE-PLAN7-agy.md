# Review: Voice Messages Transcription (v7), Sandbox Input Registry (v3), & Telegram Egress Dialer

- **Repository**: `/home/matej/HARNESS/nexus`
- **Branch**: `slice/p1-voice`
- **Reviewed Commit**: `6fca0bf6796854ab68df166679fdda511dc12044`
- **Reviewed Documents**:
  1. `docs/PLAN-VOICE.md` (v7)
  2. `docs/PLAN-SANDBOX-ROINPUT.md` (v3, startup InputRegistry)
  3. `docs/PLAN-TG-EGRESS-DIALER.md` (Telegram pinned-IP dialer, E11 prerequisite)
- **Reviewer**: Antigravity (`agy`)

---

## 1. Executive Summary

This review jointly evaluates the seventh revision (`v7`) of `PLAN-VOICE.md` and its two prerequisite specifications: `PLAN-SANDBOX-ROINPUT.md` (`v3`) and `PLAN-TG-EGRESS-DIALER.md`.

In this revision, the final round-6 finding regarding test fixture compatibility in `PLAN-TG-EGRESS-DIALER.md` has been resolved:
- **Round-6 F1 (Mode-Sensitive Address Policy in `PLAN-TG-EGRESS-DIALER.md`)**: The address policy is cleanly parameterized by the configuration-validated API base (`internal/foundation/config/config.go:330-341`):
  - **Production Mode** (`https://api.telegram.org`): Applies the full public kernel deny floor (blocking loopback, cloud metadata `169.254.169.254`, link-local, private v4, CGNAT, ULA, interface-local, multicast).
  - **Loopback-Override Mode** (validated local bot-api or test endpoints): Requires EVERY normalized address in the resolved set to be loopback (`127.0.0.0/8`, `::1`), failing closed if any public or non-loopback address is present.
  - **Fail-Closed Mixed-Set Rule**: Both modes enforce that any single non-compliant address rejects the entire DNS answer set.
  - A production endpoint can never enter loopback mode because `config.ValidateBounds` restricts the allowed base URLs at startup.

All architectural invariants and contract constraints across the three specifications are satisfied, verified against the actual codebase, and covered by RED-capable detectors.

---

## 2. In-Depth Architectural & Security Audit

### 2.1. Mode-Sensitive Egress Dialer (`PLAN-TG-EGRESS-DIALER.md`)

- **Alignment with Config Boundaries (`internal/foundation/config/config.go:330-341`)**:
  - `config.ValidateBounds` strictly allows only `https://api.telegram.org` or a verified loopback URL (`http(s)://127.0.0.1:...`, `http(s)://[::1]:...`, `http(s)://localhost:...`).
  - The dialer adopts this exact partition:
    - When `TelegramAPIBase` is the production endpoint, the dialer enforces the public deny floor.
    - When `TelegramAPIBase` is a loopback URL, the dialer enforces that all resolved addresses are loopback.
  - Mixed answer sets (e.g., public IP + loopback or public IP + `169.254.169.254`) are rejected fail-closed under both modes.
- **E11 Invariants Maintained**:
  - `Proxy: nil` disables ambient proxy discovery.
  - `CheckRedirect` unconditionally rejects 3xx redirects.
  - `EgressAttempt` receipts are emitted through the journal (`internal/channel/channel.go`) with token sanitization.
  - TLS SNI verification against the original hostname remains intact.

### 2.2. Startup Input Registry & Collision Prevention (`PLAN-SANDBOX-ROINPUT.md` v3)

- **Lifecycle & Attestation Discipline**:
  - `InputRegistry` holds open `*os.File` descriptors opened at startup (`O_NOFOLLOW|O_CLOEXEC`), validating regular file mode, permissions, ownership (daemon UID or root), `nlink == 1`, and size $\le \text{MaxSize}$.
  - Per-call `sandbox.Spec` references inputs by `ID` via `InputRef{ID, Dest}`, avoiding descriptor leaks under high request volume.
  - Destination normalization (`filepath.Clean(Dest) == Dest`, direct child of `/inputs/`) rejects built-in mount collisions and duplicate `Dest` registrations (`DEST-DUPLICATE`), eliminating overmount-shadowing.
  - Attestation explicitly captures **INPUT-IDENTITY** pinning (`dev, ino`, size, startup digest) in `policyHash` and launch `Attestation`.

### 2.3. Voice Pipeline & Safe Ingestion (`PLAN-VOICE.md` v7)

- **Pipeline Containment & Output Integrity**:
  - `ffmpeg` (OGG $\rightarrow$ WAV) and `whisper-cli` (WAV $\rightarrow$ TXT) execute inside `internal/sandbox` with network unsharing (`--unshare-net`) and isolated temporary `0700` working directories.
  - Absolute paths (`/work/...`, `/inputs/...`) and `-nt` flag ensure accurate execution and timestamp-free transcripts.
  - Transcript `/work/out.txt` is opened via `openat2` with `RESOLVE_BENEATH` relative to the held `WorkDir` descriptor and verified via `fstat` (`Mode().IsRegular()`, `nlink == 1`, daemon owned).
- **Replay Determinism via `chan_inbox`**:
  - Admitted turn text is persisted in `chan_inbox` (`internal/channel/channel.go:225-241`).
  - Redelivered messages load the stored canonical text from `chan_inbox` via `AdmitOutcome.Text`, which the adapter passes to `handle(ctx, in)`.
  - Non-transcribable voice messages enqueue a typed Croatian refusal without creating an inbox entry, preserving parity with C2 non-text refusals.

---

## 3. Top-3 Implementation Notes / Weakest Points

In accordance with review discipline, the following implementation details should be kept in mind during coding:

1. **IPv6 Loopback Scope Parsing in Dialer**:
   - *Observation*: `netip.Addr.IsLoopback()` correctly recognizes `::1`, and `netip.Addr.Unmap()` normalizes `::ffff:127.0.0.1` to IPv4 `127.0.0.1`.
   - *Detail*: When validating loopback-mode resolution sets, ensure `addr.IsLoopback()` is checked on the `Unmap()`-normalized address so all valid loopback representations succeed while IPv4-mapped public addresses fail.
2. **Journal Egress Attempt Hook in Tests**:
   - *Observation*: `PLAN-TG-EGRESS-DIALER.md:52-55` specifies logging `EgressAttempt` events to the journal.
   - *Detail*: For unit tests exercising the dialer in isolation, support passing a journal recorder interface so test fixtures can assert receipt emission without requiring full SQLite projection setup.
3. **Mount Point Creation in `probe.Prepare`**:
   - *Observation*: Bubblewrap requires mount points to exist inside the target namespace before mounting.
   - *Detail*: `probe.Prepare` (`internal/preflight/probe/probe.go:605-620`) must emit `--dir /inputs` before appending `--ro-bind-fd` flags.

---

## 4. Verification & Detector Adequacy

The detector suites defined across all three documents provide comprehensive RED-to-GREEN test coverage:

- **`PLAN-TG-EGRESS-DIALER.md`**:
  - `PROXY-IGNORED`: Proves ambient `HTTPS_PROXY` is ignored.
  - `REDIRECT-REFUSED`: Proves 3xx responses abort with typed errors.
  - `REBIND-REFUSED`: Proves dynamic rebinding to off-policy IPs is rejected.
  - `MIXED-SET-REFUSED`: Proves DNS responses containing both public and metadata/private IPs (including IPv4-mapped IPv6) are rejected in full.
  - `MODE-LOOPBACK`: Proves all-loopback connects under loopback mode while mixed sets fail, and production mode rejects loopback.
  - `RECEIPT-EMITTED`: Asserts `EgressAttempt` journal events are emitted with sanitized tokens.
  - `HOST-DENY`: Asserts unlisted hosts fail before connection.
  - `TLS-SNI-PRESERVED`: Asserts TLS SNI verification against the original hostname remains intact.
- **`PLAN-SANDBOX-ROINPUT.md` (v3)**:
  - `GRANTED-READABLE`: Proves registered model is readable at `/inputs/<name>`.
  - `NEIGHBOR-INVISIBLE`: Proves ungranted sibling files are inaccessible.
  - `SWAP-DEFEATED`: Proves post-startup pathname modifications do not affect the held inode.
  - `NO-FD-LEAK`: Verifies flat open file descriptor counts across $N$ sequential runs.
  - `POLICY-IDENTITY`: Verifies distinct `policyHash` and attestation digests across different inputs.
  - `DEST-COLLISION`: Asserts typed compile errors on path traversal.
  - `DEST-DUPLICATE`: Asserts typed compile errors when multiple inputs map to identical normalized `Dest` paths.
  - `REJECT-AT-STARTUP`: Asserts failure on symlinks, FIFOs, `nlink > 1`, and oversized files.
  - `EMPTY-UNCHANGED`: Asserts byte-identical baseline argv when `Inputs` is empty.
- **`PLAN-VOICE.md` (v7)**:
  - `media.Transcribe unit`: Tests transcription, non-zero exits, empty transcripts, and `-nt` flag enforcement.
  - `output-escape`: Tests symlink-to-canary and FIFO outputs, proving rejection and non-blocking behavior.
  - `replay-determinism`: Verifies redelivery uses stored canonical transcript $A$ over candidate $B$.
  - `download-client`: Asserts $20\text{ MiB} + 1$ byte cap rejection and scheme/host pre-validation.
  - `adapter failure path`: Validates typed Croatian refusal generation and zero inbox entries on failure.
  - `injection`: Confirms structured argv isolation without shell interpolation.

---

VERDICT: PASS
