# Review: Voice Messages Transcription (v6), Sandbox Input Registry (v3), & Telegram Egress Dialer

- **Repository**: `/home/matej/HARNESS/nexus`
- **Branch**: `slice/p1-voice`
- **Reviewed Commit**: `732650e52aface3c1d4e00df01a6a01857f3a1c5`
- **Reviewed Documents**:
  1. `docs/PLAN-VOICE.md` (v6)
  2. `docs/PLAN-SANDBOX-ROINPUT.md` (v3, startup InputRegistry)
  3. `docs/PLAN-TG-EGRESS-DIALER.md` (Telegram pinned-IP dialer, E11 prerequisite)
- **Reviewer**: Antigravity (`agy`)

---

## 1. Executive Summary

This review jointly evaluates the sixth revision (`v6`) of `PLAN-VOICE.md` together with its prerequisite specifications `PLAN-SANDBOX-ROINPUT.md` (`v3`) and `PLAN-TG-EGRESS-DIALER.md`.

All round-5 residual findings have been rigorously folded:
- **Round-5 F1 (Resolved-IP Policy & Deny Floor in `PLAN-TG-EGRESS-DIALER.md`)**: The dialer explicitly defines its address policy: every resolved address is normalized with `netip.Addr.Unmap()` (defeating IPv4-mapped IPv6 bypasses), evaluated against a hard kernel deny floor (loopback, link-local including `169.254.169.254`, private v4, CGNAT, ULA, interface-local, multicast), and subject to a fail-closed **MIXED-SET** rule where any single invalid address rejects the entire DNS answer set. Every egress attempt durably logs a typed `EgressAttempt` receipt through the journal without exposing bot tokens.
- **Round-5 F2 (Duplicate Destination Collision in `PLAN-SANDBOX-ROINPUT.md`)**: `Dest` collision checks now explicitly reject two `InputRef` entries sharing the same normalized destination path at `Compile` time, eliminating overmount-shadowing bugs where multiple inputs map to one mount while attestation claims both.

Together with the previously closed findings (F1 `chan_inbox` replay determinism, F2 startup `InputRegistry` fd lifecycle, F3 identity-only attestation framing, F4 channel-wide dialer architecture), the slice specifications are complete, secure, and ready for implementation.

---

## 2. In-Depth Architectural & Security Audit

### 2.1. Egress Dialer Policy & Receipt (`PLAN-TG-EGRESS-DIALER.md`)

- **Address Normalization & Kernel Deny Floor**:
  - `netip.Addr.Unmap()` unwraps `::ffff:a.b.c.d` mapped addresses so IPv4 filters apply identically across address families.
  - Hard deny floor blocks loopback (`127.0.0.0/8`, `::1`), unspecified (`0.0.0.0`, `::`), link-local (`169.254.0.0/16` covering cloud metadata `169.254.169.254`, `fe80::/10`), private networks (`10.0.0.0/8`, `172.16.0.0/12`, `192.168.0.0/16`), CGNAT (`100.64.0.0/10`), and ULA (`fc00::/7`).
- **Fail-Closed Mixed-Set Rule**:
  - If a DNS response contains multiple records and ANY record touches the deny floor or violates `egress_allow`, the entire resolution fails immediately. There is no silent fallback or "pick-the-good-one" heuristic.
  - Retries must re-resolve and re-verify the full address set.
- **E11 Compliance & Audit Receipt**:
  - `Proxy: nil` disables ambient proxy discovery (`HTTP_PROXY`, `HTTPS_PROXY`, `NO_PROXY`).
  - `CheckRedirect` unconditionally rejects 3xx redirects.
  - Every connection attempt generates an `EgressAttempt` journal event (`host, resolvedSet, pinnedIP, decision, ts`), providing non-repudiable proof of egress compliance with token sanitization.

### 2.2. Input Registry & Duplicate Destination Rejection (`PLAN-SANDBOX-ROINPUT.md` v3)

- **Input Lifecycle & No-Leak Guarantee**:
  - File descriptors are opened once during daemon startup with `O_NOFOLLOW|O_CLOEXEC` and validated via `fstat` (regular file, owned by daemon UID or root, non-group/other-writable, `nlink == 1`, $\le \text{MaxSize}$).
  - Startup SHA-256 is recorded as tamper-evidence.
  - `Spec.Inputs` references registered inputs by `ID`.
- **Destination Validation & Overmount Prevention**:
  - `Dest` is normalized and enforced to be a single-segment child of `/inputs` (`filepath.Clean(Dest) == Dest`).
  - Collisions with `/tmp`, `/work`, `/nexus-target`, `/nexus-libs`, `/proc`, or `/dev` are rejected.
  - Duplicate normalized `Dest` paths across multiple `InputRef` entries are rejected at `Compile` time (`DEST-DUPLICATE`), preventing overmounts that could lead to attestation ambiguity.
  - Total sort key `(ID, normalizedDest)` provides deterministic ordering for `policyHash` and attestation calculation.

### 2.3. Voice Pipeline (`PLAN-VOICE.md` v6)

- **Sanitized Pipeline Execution**:
  - `ffmpeg` (OGG $\rightarrow$ WAV) and `whisper-cli` (WAV $\rightarrow$ TXT) run through `internal/sandbox` with network unsharing (`--unshare-net`) and isolated temporary workdirs mounted at `/work`.
  - Binary arguments use absolute in-sandbox paths (`/work/in.ogg`, `/work/out.wav`, `/work/out`, `/inputs/model.bin`).
  - The `-nt` flag prevents timestamp pollution in transcripts.
- **Safe Output Read & Replay Invariants**:
  - Transcript `/work/out.txt` is opened via `openat2` with `RESOLVE_BENEATH` relative to the open `WorkDir` descriptor and validated via `fstat` (`Mode().IsRegular()`, `nlink == 1`, daemon UID owned).
  - First admission commits transcript to `chan_inbox` via `a.core.Admit(ctx, in)`.
  - Replayed updates return the canonical stored text from `chan_inbox` via `AdmitOutcome.Text`; the adapter uses this text directly, ensuring non-deterministic whisper runs on redelivery do not diverge.
  - Non-transcribable voice messages enqueue a typed Croatian refusal without creating an inbox entry, maintaining parity with C2 non-text refusals.

---

## 3. Top-3 Implementation Notes / Weakest Points

In accordance with review discipline, the following items should be addressed during implementation:

1. **Test Seams for Dialer Receipts**:
   - *Observation*: `PLAN-TG-EGRESS-DIALER.md:43-46` specifies appending `EgressAttempt` records via the journal.
   - *Implementation Detail*: In isolated unit tests for the dialer package where a full journal database may not be initialized, provide a functional option or interface allowing test fixtures to supply a test recorder to assert receipt generation.
2. **Dual-Stack DNS Handling**:
   - *Observation*: Hosts such as `api.telegram.org` may resolve to both IPv4 and IPv6 endpoints.
   - *Implementation Detail*: The dialer should evaluate all returned addresses against the deny floor before selecting an admitted IP to dial.
3. **Mount Point Provisioning in `probe.Prepare`**:
   - *Observation*: Bubblewrap requires mount points to exist inside the target root namespace.
   - *Implementation Detail*: `probe.Prepare` (`internal/preflight/probe/probe.go:605-620`) should include `--dir /inputs` when `len(Inputs) > 0` before appending `--ro-bind-fd` flags.

---

## 4. Verification & Detector Adequacy

The detector suites defined across all three documents provide comprehensive RED-to-GREEN test coverage:

- **`PLAN-TG-EGRESS-DIALER.md`**:
  - `PROXY-IGNORED`: Verifies ambient `HTTPS_PROXY` is ignored.
  - `REDIRECT-REFUSED`: Proves 3xx responses abort with typed errors.
  - `REBIND-REFUSED`: Proves dynamic rebinding to off-policy IPs is rejected.
  - `MIXED-SET-REFUSED`: Proves DNS responses containing both public and metadata/private IPs (including IPv4-mapped IPv6) are rejected in full.
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
- **`PLAN-VOICE.md` (v6)**:
  - `media.Transcribe unit`: Tests transcription, non-zero exits, empty transcripts, and `-nt` flag enforcement.
  - `output-escape`: Tests symlink-to-canary and FIFO outputs, proving rejection and non-blocking behavior.
  - `replay-determinism`: Verifies redelivery uses stored canonical transcript $A$ over candidate $B$.
  - `download-client`: Asserts $20\text{ MiB} + 1$ byte cap rejection and scheme/host pre-validation.
  - `adapter failure path`: Validates typed Croatian refusal generation and zero inbox entries on failure.
  - `injection`: Confirms structured argv isolation without shell interpolation.

---

VERDICT: PASS
