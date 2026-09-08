# Review of voice-transcription plan v2 (`docs/PLAN-VOICE.md`, `d4157a7`)

**Scope**: `docs/PLAN-VOICE.md` at commit `d4157a7` on `slice/p1-voice`.
**Reviewer**: kilo
**Date**: 2026-09-08

## Verdict

FAIL — one substantive flaw: the plan's central mechanism is unbuildable as
specified. It requires running whisper-cli *through the existing sandbox* with
the model read from a config host path, but the existing sandbox API has no
way to make that model file readable inside the sandbox. A second, lesser
issue: the detector section still asserts the round-1 delivery shape that v2's
Change section explicitly repudiates.

## Findings

### F1 (substantive, unbuildable): whisper cannot read its model inside the sandbox

The plan says ffmpeg and whisper-cli run "through internal/sandbox … The temp
dir is the only writable mount; net is denied" (`PLAN-VOICE.md:13-19`) and that
"binary/model paths [come] from config with Pi defaults"
(`PLAN-VOICE.md:39-41`, model `~/whisper.cpp/models/ggml-base.bin`).

The existing sandbox does not support a data-file mount:

- `sandbox.Spec` has only `Target`, `Args`, `WorkDir`, `Timeout`
  (`internal/sandbox/sandbox.go:35-40`) — no read-only bind of an arbitrary
  host path.
- `Compile` pins only the **executable** and its **runtime closure** (loader +
  shared libraries) via `probe.ResolveClosureHashes`
  (`sandbox.go:212-235`); `Launch` mounts the memfd-pinned closure + the
  RW `WorkDir` and hides the rest of the host FS (`sandbox.go:301-324`).

`ggml-base.bin` (~140 MB) is a data file, not a shared library, so it is not
part of the pinned closure and lives outside `WorkDir`. As written, whisper-cli
inside the sandbox cannot open `-m ~/whisper.cpp/models/ggml-base.bin`; the
design is not buildable without one of two unstated mechanisms:

1. **Copy the model into the per-call `WorkDir`** before launch — a ~140 MB
   copy per voice message on a Pi (SD-card bound, likely dominating
   transcription latency), which the plan never mentions; or
2. **Extend `sandbox.Spec`** with a read-only data-file bind (a new feature in
   the kernel sandbox, plus its own conformance surface), which the plan never
   mentions — yet the plan claims to reuse "the bwrap boundary that ALREADY
   contains" (`:15`), implying no such extension.

Either the model must be copied per call (and its cost is material and
undeclared) or the sandbox seam must grow (and the plan must say so). Until one
is chosen, "run through the sandbox with model from config" is not buildable.

### F2 (stale detector, contradicts the v2 design)

Finding (2) is explicit that a transcription failure takes the **non-text C2
refusal** shape: "EnqueueReplyID + offset advance, NO inbox row … there is no
'admit then TERMINAL on failure' claim" (`PLAN-VOICE.md:24-28`). But the
detector still reads "a transcription error yields the typed Croatian reply and
a TERMINAL close" (`PLAN-VOICE.md:48-50`). "TERMINAL close" implies an admitted
inbox row that is closed; finding (2) says the message is never admitted. The
detector was not updated to match the design, so the acceptance criterion
specifies the wrong delivery shape.

## Notes (not FAIL reasons)

- `io.LimitReader` at 20 MiB must distinguish "body ended exactly at the cap"
  from "body hit the cap and was truncated" (reject only the latter) — a common
  `LimitReader` gotcha the plan should state.
- Transcribe-first means the update is un-admitted for the whole (possibly
  minutes-long) transcription; a redelivery mid-transcribe re-transcribes, and
  whisper is not guaranteed byte-deterministic, so two transcriptions could
  race with different texts — the deterministic `turn_id` still prevents
  double-admission, but which text wins is racy. Minor.
- The 0700 mkdtemp is `0700` (v1 said 0600); either is fine, but the owner's
  voice is the content — keep it `0600` files inside a `0700` dir and clean on
  `defer`, not "on return".

## Verified clean

- Command injection: nil — fixed binary + structured argv, fixed temp
  filename; `file_id`/filename never reach argv.
- Egress: download GET to the Telegram host, token-sanitized, no redirects,
  no new target.
- Fail-closed: non-zero exit → typed refusal, never a silent drop.
- Resource bounds: wall-clock timeout kills the tree, decoded-WAV cap,
  transcript length cap.

VERDICT: FAIL
