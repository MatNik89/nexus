# Review: Voice Messages Transcription (v3) & Sandbox Read-Only Input Grant

- **Repository**: `/home/matej/HARNESS/nexus`
- **Branch**: `slice/p1-voice`
- **Reviewed Commit**: `360cbd62e3837ad39cf11a0df5bf340c7c57d6bb`
- **Reviewed Documents**:
  1. `docs/PLAN-VOICE.md` (v3)
  2. `docs/PLAN-SANDBOX-ROINPUT.md` (prerequisite kernel slice)
- **Reviewer**: Antigravity (`agy`)

---

## 1. Executive Summary

This review evaluates the third revision (`v3`) of the `p1-voice` slice design together with its prerequisite kernel design `PLAN-SANDBOX-ROINPUT.md`.

In earlier review rounds, the voice pipeline had critical architectural gaps:
1. Running raw uncontained `exec.Command` outside the sandbox.
2. Inconsistent admission semantics (`admit then TERMINAL` on a transcription error, which violated inbound lifecycles and was untestable).
3. The whisper model file (`ggml-base.bin`, ~140 MB) was unreachable inside the existing `internal/sandbox` namespace without mutating workdirs.
4. Unbounded write risks from adversarial or malformed audio decodes.
5. Ingesting transcription text from a shared, truncated 1 MiB stdout/stderr stream containing debug output.
6. Open redirect vulnerabilities in the Telegram file downloader.

The two updated documents address all previous findings through principled architectural mechanisms:
- **`PLAN-SANDBOX-ROINPUT.md`** introduces `sandbox.InputGrant` with compile-time canonicalization, Lstat checks (regular file, ownership, permissions, MaxSize), and cryptographic SHA-256 pinning, paired with launch-time re-stat/re-hash TOCTOU verification and `--ro-bind` containment via `probe.Prepare`.
- **`PLAN-VOICE.md`** executes both `ffmpeg` and `whisper-cli` inside `internal/sandbox` with unshared network (`NET_DENY`), enforces disk ceilings via child `RLIMIT_FSIZE` and input duration `-t`, reads transcription exclusively from the dedicated `-otxt` file channel (`/work/out.txt`), isolates downloads with a no-redirect `http.Client` and `io.LimitReader(cap+1)`, and aligns transcription errors with the existing typed C2 refusal path (`refusalID` + `EnqueueReplyID` without admission).

The combined design is sound, safe, and cleanly buildable against the existing kernel interfaces.

---

## 2. In-Depth Architectural & Security Audit

### 2.1. Kernel Prerequisite: Sandbox Read-Only Input Grant (`PLAN-SANDBOX-ROINPUT.md`)

- **Interface Extension**:
  Extending `sandbox.Spec` (`internal/sandbox/sandbox.go:35-40`) with `ReadOnly []InputGrant`:
  ```go
  type InputGrant struct {
      HostPath string // absolute, canonical host path of a regular file
      Dest     string // absolute in-sandbox mount path (e.g. /inputs/model.bin)
      MaxSize  int64  // reject at Compile if the file exceeds this
  }
  ```
- **Compile-Time Pinning (`sandbox.Compile`, `internal/sandbox/sandbox.go:200-236`)**:
  - Validates `HostPath` is absolute and evaluated via `filepath.EvalSymlinks`.
  - Validates `Dest` is absolute and does not collide with reserved paths (`/work`, `/nexus-target`, `/nexus-libs`, `/proc`, `/dev`, `/tmp`) or other grants.
  - Inspects file via `os.Lstat`: verifies regular file (`Mode().IsRegular()`), owned by daemon UID, non-group/other-writable, and bounded by `MaxSize`.
  - Computes SHA-256 digest and records it into `CompiledPolicy`.
- **Launch-Time Verification (`sandbox.Launch`, `internal/sandbox/sandbox.go:300-324`)**:
  - Re-checks `os.Lstat` and re-hashes `HostPath` immediately before spawning `bwrap`.
  - Any mutation in file type, size, or SHA-256 digest aborts with a fail-closed error before execution.
  - Appends `--ro-bind <HostPath> <Dest>` to `bwrap` arguments in `probe.Prepare` (`internal/preflight/probe/probe.go:605-636`).
- **Invariants**:
  - Purely additive: `len(ReadOnly) == 0` leaves argument generation and namespace layout byte-for-byte identical to baseline.
  - Per-file allowlist only (no directory binds, no glob matching).
  - Writable boundary remains restricted strictly to `/work`.

### 2.2. Voice Pipeline (`PLAN-VOICE.md`)

- **Sandboxed Execution Boundary**:
  - Both `ffmpeg` (OGG $\rightarrow$ WAV) and `whisper-cli` (WAV $\rightarrow$ TXT) run through `internal/sandbox` (`sandbox.Launch`).
  - Offline network containment (`--unshare-net`) is enforced; hostile audio bytes cannot trigger network egress.
  - Temp directories are isolated per execution (0700 permissions), mounted at `/work`, and cleaned on `defer`.
- **Enforced Resource & Disk Bounds**:
  - Child process write limits are enforced during execution via `RLIMIT_FSIZE` on the sandboxed worker (`SIGXFSZ` trigger if WAV output exceeds budget), accompanied by `ffmpeg -t <maxSeconds>` input duration limits.
  - Wall-clock timeout is enforced via `context.WithTimeout` in `probe.Prepare` (`internal/preflight/probe/probe.go:637-638`).
- **Dedicated Transcript Channel**:
  - `whisper-cli` writes to disk via `-otxt -of /work/out` producing `/work/out.txt`.
  - NEXUS reads `/work/out.txt` directly, validating UTF-8, applying length limits, and trimming whitespace.
  - Bypasses the shared 1 MiB stdout/stderr buffer (`internal/sandbox/sandbox.go:329`), preventing diagnostic logging contamination or truncation issues.
- **Inbound Lifecycle & Failure Path**:
  - The pipeline transcribes before admission.
  - **Success**: Canonical transcript admitted via `a.core.Admit` (`internal/channel/telegram/telegram.go:253`) with `[glasovna poruka] ` prefix, entering the standard turn lifecycle (`Admit -> handle -> CompleteInbound`).
  - **Failure / Refusal**: If download, decode, or transcription fails, or if the transcript is empty/whitespace, NEXUS enqueues a typed Croatian refusal via `refusalID(identity, u.UpdateID)` and `EnqueueReplyID` (`internal/channel/telegram/telegram.go:209-248`). No inbox row is created, preserving clean inbound state and enabling Telegram offset advancement.
- **Egress & Download Safety**:
  - Dedicated `http.Client` with `CheckRedirect` returning `http.ErrUseLastResponse` / error to reject all HTTP 3xx redirects.
  - Host and scheme validated before issuance.
  - Response body read via `io.LimitReader(resp.Body, 20*1024*1024 + 1)`; reading the 20 MiB + 1st byte immediately classifies the payload as over-limit and triggers refusal.
  - Telegram bot tokens are scrubbed from all error messages via `a.sanitize(err)`.

---

## 3. Top-3 Weakest Points & Implementation Notes

In accordance with review discipline, the following implementation details should be kept in mind during code authoring:

1. **Tilde Expansion for Configured Model Paths**:
   - *Observation*: `PLAN-VOICE.md:14` cites `~/whisper.cpp/models/ggml-base.bin` as a typical Pi path.
   - *Detail*: In Go, `filepath.IsAbs("~/...")` returns `false`. The configuration loader must expand `~` using `os.UserHomeDir()` prior to passing `HostPath` into `sandbox.Compile`, since `sandbox.Compile` requires `filepath.IsAbs(grant.HostPath)`.
2. **Empty or Whitespace-Only Transcription Output**:
   - *Observation*: `PLAN-VOICE.md:63-64` specifies that empty/whitespace output is rejected.
   - *Detail*: Inaudible voice notes or silence files may result in `whisper-cli` exiting with status 0 while leaving `/work/out.txt` empty or containing only newlines. The implementation must ensure `strings.TrimSpace(transcript) == ""` is explicitly handled as a transcription failure leading to the typed refusal reply.
3. **Threading Grants through `probe.Spec`**:
   - *Observation*: `internal/sandbox/sandbox.go:301-304` constructs `probe.Spec` to pass to `probe.Prepare`.
   - *Detail*: `probe.Spec` (`internal/preflight/probe/probe.go`) will need a corresponding `ReadOnly []InputGrant` field so `probe.Prepare` can append `--ro-bind` arguments alongside the existing `--bind <WorkDir> /work` logic.

---

## 4. Verification & Detector Adequacy

The detector suites defined across both plans are complete, RED-capable, and anchored to core contracts:

- **`PLAN-SANDBOX-ROINPUT.md`**:
  - `GRANTED-READABLE`: Asserts inside-namespace read access to pinned files.
  - `NEIGHBOR-INVISIBLE`: Asserts that ungranted sibling files in the host directory are unreadable (proves per-file binding rather than directory binding).
  - `TOCTOU-REFUSED`: Mutates file between Compile and Launch to prove verification occurs at launch.
  - `REJECT-AT-COMPILE`: Verifies compile errors on symlinks, devices, group-writable files, and over-limit files.
  - `EMPTY-GRANT-UNCHANGED`: Asserts byte-identical `bwrap` command arguments when `ReadOnly` is empty.
- **`PLAN-VOICE.md`**:
  - `media.Transcribe unit`: Verifies transcript extraction, non-zero exit handling, empty output refusal, and length capping with stub binaries.
  - `disk bound`: Validates child write enforcement under `RLIMIT_FSIZE`.
  - `transcript channel`: Validates that stdout/stderr noise is never leaked into the transcript.
  - `redirect`: Validates that 3xx redirects are rejected and zero requests reach redirected targets.
  - `adapter failure path`: Validates typed refusal generation, zero inbox rows on error, idempotent replay, and offset advancement.
  - `injection`: Validates that hostile filenames/file_ids in `Spec.Args` remain structured argv tokens without shell evaluation.

---

VERDICT: PASS
