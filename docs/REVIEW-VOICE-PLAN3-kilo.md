# Review of voice v3 + sandbox RO-input prerequisite

**Scope**: `docs/PLAN-VOICE.md` (v3) and `docs/PLAN-SANDBOX-ROINPUT.md`, commit
`360cbd6` on `slice/p1-voice`.
**Reviewer**: kilo
**Date**: 2026-09-08

## Verdict

FAIL. Two substantive design defects remain, both grounded in the actual
sandbox/CLI interfaces:

- F1 — the voice plan's disk-write bound depends on a sandbox capability
  (`RLIMIT_FSIZE`) that does not exist and is not declared as a prerequisite.
- F2 — the whisper invocation drops `-nt`, so the transcript carries
  `[hh:mm:ss --> hh:mm:ss]` timestamps into the admitted turn text.

## Findings

### F1 (substantive, unbuildable): RLIMIT_FSIZE is an undeclared sandbox dependency

`PLAN-VOICE.md:51-57` makes the decoded-WAV ceiling "enforced DURING the write
by RLIMIT_FSIZE on the sandbox child (ffmpeg's write past the cap takes
SIGXFSZ and the tree is reaped)".

The existing sandbox has **no** such mechanism:

- `sandbox.Spec` is only `Target / Args / WorkDir / Timeout`
  (`internal/sandbox/sandbox.go:35-40`) — no rlimit field.
- The probe layer builds the bwrap argv (`internal/preflight/probe/probe.go:
  605-665`) and sets `cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}`
  (`probe.go:666`) — no `Rlimits`, and no `setrlimit`/`RLIMIT_FSIZE`/`SIGXFSZ`
  anywhere in `probe.go` or `sandbox.go` (grep-confirmed).
- bubblewrap itself has no RLIMIT flag, so the bound cannot be threaded as a
  bwrap arg; it must be set by the launcher before exec (a `SysProcAttr.Rlimits`
  addition to the probe layer, i.e. a kernel change).

The voice plan names the RO-input grant as an explicit prerequisite
(`PLAN-VOICE.md:13-24`) but treats RLIMIT_FSIZE as if it already exists. It
does not. The "enforced, not observed" bound is therefore unbuildable against
the named interface without a second, undeclared sandbox change — exactly the
class of defect the round-2 F1 (model unreachable) was, and the plan correctly
decomposed *that* one into a prerequisite but did not do the same here.

### F2 (substantive, correctness): whisper `-nt` dropped → timestamped transcript

v2's pipeline was `whisper-cli -m <model> -f out.wav -nt` (explicit no-
timestamps, matching the verified hermes pipeline). v3 rewrote it to the file
channel: `whisper-cli -m <model> -f out.wav -otxt -of out` (`PLAN-VOICE.md:9`)
and **dropped `-nt`**.

In whisper.cpp, `-otxt`/`--output-txt` selects the txt file and `-nt`/
`--no-timestamps` is an independent switch; the txt writer emits a
`[00:00:00.000 --> 00:00:02.000]  ` prefix per segment unless `-nt` is set.
So `-otxt -of out` (without `-nt`) writes `out.txt` with per-segment timestamp
lines, and the plan's post-processing only trims whitespace
(`PLAN-VOICE.md:63-64`) — it does not strip timestamps. The admitted turn text
would be `[00:00:02.000 --> 00:00:05.000]   what time is it`, i.e. the model
receives timestamp noise as "what the user said". The file-channel fix was
correct, but `-nt` must be retained: `-otxt -of out -nt`.

## Notes (not FAIL reasons)

- **RO-input TOCTOU claim is overstated.** `PLAN-SANDBOX-ROINPUT.md:49-53`
  says the grant "closes the TOCTOU window the same way the closure pins do".
  The closure pins do NOT use path binds — they use sealed memfds
  (`--ro-bind-data`, `probe.go:645`), which are immune to a post-verify
  path swap. The grant's `--ro-bind HOST DEST` is path-based, so a same-uid
  swap between the launch re-hash and the bwrap bind remains (the same
  residual the `guardWorkDir` comment already acknowledges, `probe.go:471`).
  Single-user and a milliseconds window, so defense-in-depth — but the plan
  should say "closes the TOCTOU up to the bind, with a residual same-uid
  path-swap window", not "the same way the closure pins do". (Using
  `--ro-bind-data` for a ~140 MB model would cost a per-call memfd read, which
  the plan is implicitly avoiding.)
- **Relative audio paths vs no chdir.** The pipeline uses relative
  `-i in.ogg` / `out.wav` / `-of out` (`PLAN-VOICE.md:9`), but `probe.Prepare`
  creates `/work` with `--dir /work` and does **not** `--chdir` it
  (`probe.go:609,625`); the child's cwd is inherited. The implementation must
  use absolute in-sandbox paths (`/work/in.ogg`, `-of /work/out`) — the plan
  should say so.

## Verified clean

- Round-2 F2 (failure detector) is corrected: `PLAN-VOICE.md:87-91` asserts
  the refusal DELIVERY row, ZERO inbox rows, offset-after-durable-enqueue, and
  explicitly forbids a "TERMINAL close" assertion — matching the C2 refusal
  shape.
- Model grant is correctly decomposed into a separate, named prerequisite
  (`PLAN-VOICE.md:13-24`), and its Spec extension is additive
  (`ReadOnly []InputGrant`, `PLAN-SANDBOX-ROINPUT.md:20-35`).
- Download: dedicated client with `CheckRedirect` rejecting all redirects; the
  existing telegram client indeed sets no policy
  (`telegram.go:90`), so not reusing it is right; `io.LimitReader` + read
  cap+1 to distinguish exact-cap vs truncation.
- Transcript channel via `-otxt -of out` (file) correctly avoids the shared
  1 MiB stdout/stderr buffer.
- RO-input detectors (GRANTED-READABLE, NEIGHBOR-INVISIBLE, TOCTOU-REFUSED,
  REJECT-AT-COMPILE, EMPTY-GRANT-UNCHANGED) are anchored and RED-capable
  against the named bind construction.

VERDICT: FAIL
