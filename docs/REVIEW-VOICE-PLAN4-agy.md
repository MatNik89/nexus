# Review: Voice Messages Transcription (v4) & Sandbox Read-Only Input Grant (v2)

- **Repository**: `/home/matej/HARNESS/nexus`
- **Branch**: `slice/p1-voice`
- **Reviewed Commit**: `831eb26b139513f64251094b674a7cc90b95ffe5`
- **Reviewed Documents**:
  1. `docs/PLAN-VOICE.md` (v4)
  2. `docs/PLAN-SANDBOX-ROINPUT.md` (v2, prerequisite kernel slice)
- **Reviewer**: Antigravity (`agy`)

---

## 1. Executive Summary

This review evaluates the fourth revision (`v4`) of `PLAN-VOICE.md` alongside the second revision (`v2`) of its prerequisite kernel specification `PLAN-SANDBOX-ROINPUT.md`.

In this revision, all round-3 findings have been rigorously folded and reconciled against the codebase contracts:
1. **Held-FD Mounts & Attestation Identity**: Model files are bound via held file descriptors passed through `ExtraFiles` and bound via `--ro-bind /proc/self/fd/<N> <Dest>`, completely defeating post-verify path-swap/symlink races. Grant sets are canonically normalized under `/inputs` and hashed into both `policyHash` and `Attestation`.
2. **Resource Boundaries & Deferrals**: Speculative aggregate disk quota machinery and `RLIMIT_FSIZE` claims have been removed; output WAV sizes are bounded at encode-time via `ffmpeg -t <maxSeconds>` (32 KB/s mono PCM), while aggregate disk quotas are properly attributed and deferred to the existing S6.2 P2.2 `ResourceBudget` contract (`docs/HARNESS-SPEC.md:1481-1484`).
3. **Safe Output Ingestion**: `/work/out.txt` is opened using `openat2` with `RESOLVE_BENEATH` relative to a held `WorkDir` fd and validated via `fstat` (`Mode().IsRegular()`, `nlink == 1`, daemon owned), eliminating symlink escapes, FIFO hangs, and host file disclosure.
4. **Replay Determinism**: Transcripts are durably persisted keyed by update/turn identity prior to admission, guaranteeing that non-deterministic whisper runs do not diverge across retries.
5. **Egress Hardening & Explicit Ceilings**: Telegram file downloads employ a dedicated `http.Client` with `Proxy: nil` (neutralizing ambient proxy environment variables per E11) and unconditional redirect rejection. Anti-DNS-rebinding dialer protection is explicitly identified as an existing system-wide Telegram ceiling rather than a hidden voice gap.
6. **Command Flag Restoration**: The `-nt` (no timestamps) flag is restored to `whisper-cli`, and all invocation targets use absolute paths (`/work/...`, `/inputs/...`), ensuring flawless execution in bubblewrap namespaces lacking working directory changes.

The revised designs are structurally sound, airtight against filesystem and process-escape attacks, and cleanly buildable against existing kernel interfaces.

---

## 2. In-Depth Architectural & Security Audit

### 2.1. Kernel Prerequisite: Sandbox Read-Only Input Grant (`PLAN-SANDBOX-ROINPUT.md` v2)

- **Held-FD Lifecycle & TOCTOU Immunity**:
  - `HostPath` is opened at Compile time with `O_NOFOLLOW|O_CLOEXEC` and retained as an open `*os.File` descriptor across the lifetime of the policy.
  - `fstat` on the open descriptor validates that the file is regular, owned by the daemon UID or root (accommodating system-installed models), non-group/other-writable, has `nlink == 1`, and does not exceed `MaxSize`.
  - Bytes are SHA-256 hashed directly from the held descriptor.
  - At Launch, `fstat` verifies inode and size identity against the compile-time pin. Spawning passes the held descriptor via `cmd.ExtraFiles` and instructs bubblewrap via `--ro-bind /proc/self/fd/<N> <Dest>`, resolving directly to the pinned inode and bypassing any host pathname resolution.
- **Path Normalization & Mount Separation**:
  - `Dest` is verified to be `filepath.Clean(Dest) == Dest`, absolute, and a direct single-segment child of `/inputs` (e.g., `/inputs/model.bin`).
  - Path collisions with `/tmp`, `/work`, `/nexus-target`, `/nexus-libs`, `/proc`, or `/dev` are strictly rejected, preventing path traversal attacks (such as `/inputs/../nexus-target`).
- **Policy & Attestation Identity Integration**:
  - The canonical, destination-sorted slice of grants (`{Dest, digest, size}`) enters `policyHash` (`internal/sandbox/sandbox.go:227-235`) and `Attestation.PolicyHash` (`internal/sandbox/sandbox.go:352-375`). Two policies with distinct model grants cannot produce identical attestation hashes.
- **Named Residual Ceiling**:
  - Same-UID, same-inode in-place mutation (truncating and rewriting the inode while the fd is held) is acknowledged as an explicit ceiling. Given single-user local deployment constraints on an SD-card Raspberry Pi, full in-memory duplication of ~140 MB model weights into sealed memfds is appropriately avoided.

### 2.2. Voice Processing Pipeline (`PLAN-VOICE.md` v4)

- **Sandboxed Execution & Containment**:
  - Both `ffmpeg` and `whisper-cli` execute under `internal/sandbox` with network unsharing (`--unshare-net`) and isolated temporary `0700` working directories mounted at `/work`.
  - Binary invocations use fully qualified in-sandbox paths (`/work/in.ogg`, `/work/out.wav`, `/work/out`, `/inputs/model.bin`), avoiding reliance on working directory inheritance in `probe.Prepare`.
  - Restoration of `-nt` ensures whisper transcripts omit `[hh:mm:ss --> ...]` timestamps.
- **Safe Output Read Discipline**:
  - Output `/work/out.txt` is opened via `openat2` with `RESOLVE_BENEATH` / `RESOLVE_NO_SYMLINKS` relative to the open file descriptor of `WorkDir`.
  - `fstat` on the resultant file descriptor enforces regular file status, single link count (`nlink == 1`), and daemon UID ownership.
  - This prevents a compromised decoder from tricking the daemon into reading host secrets or stalling indefinitely on named pipes.
  - Transcript bytes are read from the held fd, length-capped, UTF-8 validated, trimmed, and checked for non-empty content before admission.
- **Replay Determinism & Admission Lifecycle**:
  - Transcripts are persisted keyed by the update/turn identity before calling `a.core.Admit`.
  - On retry or redelivery, the existing transcript is loaded directly, avoiding divergent transcripts from non-deterministic whisper runs and eliminating duplicate CPU cycles.
  - Failure to download, decode, or transcribe results in a typed Croatian refusal (`EnqueueReplyID`) without creating an inbox row, matching C2 non-text refusal semantics in `internal/channel/telegram/telegram.go:209-248`.
- **Download Egress & Explicit Ceilings**:
  - The Telegram `getFile` downloader uses a dedicated `http.Client` with `Transport.Proxy = nil` (preventing proxy interception per E11) and `CheckRedirect` unconditionally rejecting 3xx responses.
  - Download payloads are bounded using `io.LimitReader(resp.Body, 20*1024*1024 + 1)` to distinguish exact-capacity files from truncated over-limit payloads.
  - DNS-rebinding / pinned-IP dialer protection is appropriately recognized as an existing system-wide Telegram channel ceiling (tracked under S6.3) rather than a voice slice defect.
- **Resource Limits**:
  - Decoded audio is bounded by `ffmpeg -t <maxSeconds>`, restricting mono 16 kHz 16-bit PCM output strictly to $\le 32044 \times \text{maxSeconds}$ bytes.
  - Sandbox wall-clock timeouts kill child process trees on runaway tasks.
  - Aggregate disk/file-count quotas are deferred to S6.2 P2.2 (`docs/HARNESS-SPEC.md:1481-1484`).

---

## 3. Top-3 Implementation Notes / Weakest Points

In accordance with review discipline, the following items should be addressed during implementation:

1. **Bubblewrap `/inputs` Directory Creation**:
   - *Observation*: `bwrap` requires destination parent directories to exist before mounting child files.
   - *Implementation Detail*: `probe.Prepare` (`internal/preflight/probe/probe.go:605-620`) must emit `--dir /inputs` in its initial base arguments when read-only input grants are present, ensuring `--ro-bind /proc/self/fd/<N> /inputs/<name>` succeeds.
2. **File Descriptor Ownership & GC Retention**:
   - *Observation*: `InputGrant` holds host file descriptors across the daemon's runtime.
   - *Implementation Detail*: The `CompiledPolicy` and `Bwrap` structs must retain `*os.File` references so Go's garbage collector does not run finalizers and close the held descriptors prematurely before `Launch` is called.
3. **`openat2` Syscall Availability**:
   - *Observation*: Safe output file reading uses `openat2` with `RESOLVE_BENEATH`.
   - *Implementation Detail*: Linux 5.6+ supports `openat2` natively. On target Linux kernels (Raspberry Pi OS 6.x), this syscall is universally available. In the event of older mock test environments, wrapping `openat2` with a fallback checking `unix.ENOSYS` ensures maximum developer ergonomics without compromising production security.

---

## 4. Verification & Detector Adequacy

The detector suites defined in both documents are comprehensive and RED-capable:

- **`PLAN-SANDBOX-ROINPUT.md` (v2)**:
  - `GRANTED-READABLE`: Proves granted file is readable at `/inputs/<name>`.
  - `NEIGHBOR-INVISIBLE`: Proves ungranted sibling files in host directories remain inaccessible.
  - `SWAP-DEFEATED`: Swaps pathname between Compile and Launch, verifying the sandbox reads the original held bytes.
  - `POLICY-IDENTITY`: Asserts differing grants produce distinct `policyHash` and attestation digests.
  - `DEST-COLLISION`: Asserts compile errors on path traversal (`/inputs/../nexus-target`) or collisions.
  - `REJECT-AT-COMPILE`: Asserts typed compile errors for symlinks, FIFOs, `nlink > 1`, and oversized files.
  - `EMPTY-GRANT-UNCHANGED`: Guards additive contract with byte-identical baseline argv.
- **`PLAN-VOICE.md` (v4)**:
  - `media.Transcribe unit`: Validates transcript extraction, non-zero exit handling, empty text rejection, and `-nt` flag enforcement.
  - `output-escape`: Tests symlink-to-canary and FIFO outputs, proving rejection and non-blocking behavior.
  - `replay-determinism`: Forces divergent transcripts across runs, asserting the handler and journal receive and record only the original transcript.
  - `redirect+proxy`: Proves 30x redirects are rejected and ambient `HTTP_PROXY` settings are ignored.
  - `adapter failure path`: Asserts typed refusal delivery, zero inbox entries, durable offset advancement, and idempotent replay.
  - `injection`: Confirms structured argv encapsulation without shell interpolation.

---

VERDICT: PASS
