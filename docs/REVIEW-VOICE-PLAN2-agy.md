# Design Review (Round 2): Voice Message Transcription (`docs/PLAN-VOICE.md`)

**Scope**: Review of `docs/PLAN-VOICE.md` (commit `d4157a7` on branch `slice/p1-voice`)  
**Reviewer**: `agy` (Antigravity QA / Architecture Reviewer)  
**Date**: 2026-09-08  
**Base Revision**: `bab40f8` (`origin/main`)  

---

## Executive Summary

`docs/PLAN-VOICE.md` (v2) fully incorporates all Round 1 peer review findings, strengthening sandbox isolation, download boundaries, resource caps, and admission lifecycle semantics.

Key architectural improvements in v2:
1. **Sandboxed Subprocess Execution (`internal/sandbox`)**: `ffmpeg` and `whisper-cli` are executed inside the `bwrap` sandbox container with network denied (`NET_DENY`), read-only system/model mounts, and the per-call temporary directory as the sole writable mount. Untrusted, channel-controlled audio bytes are decoded strictly within containment, preventing remote code execution or filesystem escapes.
2. **Exact C2 Refusal Parity on Transcription Failure**: If transcription fails (download failure, >20 MiB cap, ffmpeg error, or whisper-cli error), the adapter enqueues a stable typed Croatian refusal (`"Nisam uspio pretvoriti glasovnu poruku u tekst."`) via `refusalID(identity, u.UpdateID)` and advances the poll offset without creating an admitted inbox row, matching today's text refusal path exactly.
3. **Bounded & Sanitized Download Pipeline**: File downloads from Telegram Bot API enforce a strict 20 MiB bound via `io.LimitReader`, disable HTTP redirects, sanitize bot tokens from all error paths, and restrict traffic to the configured Telegram API host.
4. **Multi-Stage Resource Bounding**:
   - Wall-clock timeout kills the entire process tree via sandbox namespace isolation.
   - Post-ffmpeg WAV size verification protects against audio decompression bombs.
   - Final transcript length is bounded before turn admission.

---

## Standards Review

### Documented Standards Compliance

- **[COMPLIANT] Sandboxed Subprocess Execution (`AGENTS.md`, `ARCHITECTURE-ESSENTIALS.md` E7)**:
  - Subprocess execution uses `internal/sandbox` (`bwrap`) with `NET_DENY` and process-tree termination.
- **[COMPLIANT] Delivery Honesty & Exactly-Once Admission (`ARCHITECTURE-ESSENTIALS.md` E15, `HARDQ B2`)**:
  - Admission occurs only upon successful transcription. Failures use stable deterministic refusal IDs (`refusalID`), ensuring idempotent redelivery without phantom inbox records.
- **[COMPLIANT] Single Ownership (`ARCHITECTURE-ESSENTIALS.md` E5)**:
  - Telegram adapter owns voice media acquisition and transcription before handing canonical text to `channel.Core`.
- **[COMPLIANT] Language Invariant (`AGENTS.md`)**:
  - Code, documentation, comments, and schemas are in English; user-facing transcript tags (`[glasovna poruka]`) and error replies are in Croatian.

### Baseline Code Smells (Judgement Calls)

- **Zero Smells**: The plan reuses the existing `internal/sandbox` infrastructure and standard Telegram adapter outbox mechanics without inventing duplicate execution or error-handling paths.

---

## Spec & Design Review

### 1. Verification of Attack Vectors & Resource Bounds

| Area | Plan Specification (v2) | Evaluation |
| :--- | :--- | :--- |
| **Untrusted Media Decoding** | Executed inside `internal/sandbox` (`bwrap`) with `NET_DENY`. | **Verified Secure**: Isolates codec/parser vulnerabilities from the host and daemon process. |
| **Process Tree Termination** | Sandbox wall-clock timeout kills entire PID tree. | **Verified Secure**: Prevents orphaned ffmpeg or whisper worker processes on malformed/hang inputs. |
| **Download Safety & Cap** | `io.LimitReader` capped at 20 MiB; no redirects; token-sanitized error paths. | **Verified Secure**: Prevents memory exhaustion and token leakage in diagnostic logs. |
| **Decompression Bomb Protection** | Post-ffmpeg WAV file size cap in temp dir. | **Verified Secure**: Guards against high-ratio audio compression attacks before feeding data to whisper-cli. |
| **Admission Lifecycle** | Transcribe first $\rightarrow$ admit on success $\rightarrow$ C2 refusal on failure. | **Verified Sound**: Exact parity with text path; no partial or corrupt turns admitted. |

### 2. Detectors & Test Architecture

- **`media.Transcribe` Unit Detector**: Tests sandboxed execution with stub binaries in temp PATH, asserting fail-closed behavior on non-zero exit or timeout.
- **Adapter Voice Inbound Detector**: Verifies successful getFile + download + transcription yields `[glasovna poruka] <transcript>` turn admission.
- **Adapter Refusal Detector**: Verifies transcription errors yield typed Croatian refusal and advance the poll offset without inbox admission.
- **Injection & Metacharacter Detector**: Asserts structured argv passing prevents shell expansion.

---

## Non-Blocking Notes & Observations

1. **Editorial Phrasing in Detector Section (`PLAN-VOICE.md:39`)**:
   - *Note*: Line 39 in "Detectors" mentions "and a TERMINAL close", whereas the refined Section "Change" (line 18) correctly clarifies that refusal uses `EnqueueReplyID` without an inbox row (no admit/terminal needed). (Editorial note per verdict policy).
2. **Sandbox Model Mounts on Raspberry Pi**:
   - *Note*: Ensure the whisper ggml model path (e.g. `~/whisper.cpp/models/ggml-base.bin`, resolved to absolute path) is included as a read-only bind mount in the sandbox spec so `whisper-cli` can access the weights inside `bwrap`.

---

## Findings Table

| ID | Area | Severity | Finding Summary | Source Citation |
| :--- | :--- | :--- | :--- | :--- |
| **N/A** | — | **INFO** | All round-1 findings fully resolved. Sandbox isolation, bounded streaming, refusal parity, and resource caps are completely specified. | `docs/PLAN-VOICE.md:12-35` |

---

VERDICT: PASS
