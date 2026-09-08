# Review of voice-transcription plan (`docs/PLAN-VOICE.md`, `8772bd3`)

**Scope**: `docs/PLAN-VOICE.md` at commit `8772bd3` on `slice/p1-voice`.
**Reviewer**: kilo
**Date**: 2026-09-08

## Verdict

PASS. The design is correct on every substantive axis the dispatch names:
no command-injection surface, no new egress target, a cap, fail-closed on
transcription error, and honest scope. No substantive correctness / security /
delivery-contract / unbuildable flaw found. The subprocess-sandbox question is
the one genuine security-shaped concern, and it is a defense-in-depth note,
not a violation (see N1).

## Attack-surface assessment

### Command injection — nil (sound)

`exec.Command` with a fixed binary path and explicit args, never a shell
string, and the temp file uses a **fixed** name (`in.ogg`/`out.wav`) in a
per-call mkdtemp — the Telegram `file_id`/filename never becomes an argv
element. The audio reaches the binaries only as file bytes. Matches the
existing `exectool` discipline ("absolute ELF path, no shell strings",
`exectool.go:81`).

### Egress / download — no new target (sound)

`getFile` returns a server-generated relative `file_path`; the download is a
GET to `api.telegram.org/file/bot<token>/<path>` — the same host the adapter
already POSTs to (`telegram.go:142`), so no new egress target. The one
hardening point (treat `file_path` strictly as a path component, never as a
URL) is a note, not a hole: even a hostile path stays on the fixed host.

### 20MB cap — sound, enforcement point under-specified (N2)

The cap is required; the plan does not say *where* it is enforced. Enforcing
only against getFile's reported `file_size` trusts the host — on a Pi, an
under-reported size or a streamed overshoot is an OOM vector. Stream-limit the
download bytes.

### Fail-closed on transcription error — sound

A non-zero exit returns an error → typed Croatian reply + the inbound closes
TERMINAL. No silent drop, no re-processing loop. Correct.

### Synchronous-in-turn — declared, acceptable

One voice blocks one flush tick; single-user bot; the plan names the topknot
ceiling and its trigger (visible stall). Fine for the first cut.

### Temp files — sound, one nit (N3)

0600 in a per-call mkdtemp is correct for the owner's voice. "Removed on
return" must be a `defer` (all paths, incl. error/panic) or orphaned 0600
files hold the recording.

## Notes (not FAIL reasons)

1. **Subprocess sandboxing (top note).** ffmpeg/whisper run unsandboxed via
   raw `exec.Command`, consuming owner-provided audio. This does NOT violate
   the S6.2 hard rule — that rule binds *tool-executing* `ExecProcess`
   subprocesses, and the media pipeline is not a tool; the codebase already
   runs a non-tool subprocess outside the tool sandbox (the CLIAgent provider
   carve-out, AGENTS.md S2.1). The threat model is owner-only (deny-default
   private chat) and no CVE is cited, so this is speculative. Still, ffmpeg is
   a large attack surface and the bwrap backend already exists — recommend a
   media-specific sandbox profile (read: temp dir + model path; write: temp
   dir; NET_DENY; RLIMIT) as the next ceiling after the slice works.
2. **Hard timeout must be `exec.CommandContext`** (a context deadline that
   kills the process), not a bare `exec.Command` plus an external timer; and
   the value must be configurable — whisper on a Pi over a 20MB clip can run
   minutes, so a fixed short timeout spuriously fails valid input.
3. **20MB cap enforcement point** (see above) — bound the download stream,
   not just the reported size.
4. **Temp cleanup via `defer`**, not "on return".
5. **Injection detector is "by construction"** — weak. Add one concrete case
   (a `file_id`/filename with `; rm -rf`, `$(...)`, backticks, `|`) that
   proves it never reaches argv. Cheap and it converts an assertion into a
   test.
6. **Download URL** — normalize `file_path` as a relative path component;
   never `url = file_path` verbatim.

## Detector adequacy

Unit (stub whisper/ffmpeg → known transcript; non-zero exit → error) and
adapter (fake Bot API serving getFile+bytes → transcript admitted; error →
typed reply + TERMINAL) cover the happy and fail-closed paths. Adequate for a
P1 first cut once N5 is folded.

VERDICT: PASS
