# PLAN-VOICE v2 design review — Codex

## Scope and evidence boundary

Read-only design review of `docs/PLAN-VOICE.md` at commit
`d4157a7f40ca0f85cf71e8211f4896b82bbb4e22` on `slice/p1-voice`.

- Tree: `52b4b14c55e69dcac12d2f44ee24255dff926d7b`
- Plan blob: `a39c84782c75c862eff11eb4bb89f97808483740`
- Plan SHA-256: `12b9861882e5f3e4c5a2f226ee68a5a9fa25f81afed2b2583759dabb5716237c`
- Evidence source: clean `git archive` export on the `/home/matej/HARNESS` filesystem, not `/tmp`.
- Modality: static design and source inspection only; no production tests or media/process execution.

## Round-1 closure assessment

The successful-transcription lifecycle is now coherent: transcription precedes admission, and the transcript becomes the canonical admitted text (`docs/PLAN-VOICE.md:20-28`). The failure branch also correctly selects the existing stable refusal/outbox path rather than inventing an admitted failed turn. Running both external programs through `internal/sandbox`, with structured argv, network denial, and a hard timeout is the correct containment direction (`docs/PLAN-VOICE.md:13-19,35-41`). The bounded-download requirement also avoids an unbounded `ReadAll` (`docs/PLAN-VOICE.md:29-34`). These corrections do not, however, close the following substantive gaps.

## Substantive findings

### 1. HIGH — The configured Whisper model is not reachable through the current sandbox contract

`docs/PLAN-VOICE.md:7-8,39-41` passes `-m <model>` and says the model path comes from configuration, but the current sealed sandbox request exposes only `Target`, `Args`, one writable `WorkDir`, and `Timeout` (`internal/sandbox/sandbox.go:33-40`). The backend mounts only the promoted executable and its ELF closure plus the work directory at `/work`; there is no production read-only data-file grant (`internal/preflight/probe/probe.go:97-103,605-645`). Therefore an ordinary configured host model path is absent inside the namespace. Copying a multi-hundred-megabyte or larger model into each per-call writable temp directory is neither specified nor an acceptable implicit fallback, and passing the host path cannot work.

Impact: the designed Whisper command is unproducible against the named existing sandbox API, so every real transcription can fail despite the binaries being installed.

Required correction: add a sealed, content-pinned, read-only input-file grant for the model (with canonical-path, regular-file, ownership/permission, size, digest, and launch-time identity checks), or define a safe startup-staged immutable model artifact that the sandbox explicitly mounts. Bind that data artifact into the compiled policy/attestation and add a detector proving the model is readable while an undeclared neighboring file is not.

### 2. HIGH — Checking WAV size after ffmpeg is not a disk-write bound

`docs/PLAN-VOICE.md:35-38` calls the decoded-WAV limit a resource bound but checks directory size only after ffmpeg exits. The only current filesystem control is a host directory bind-mounted read-write at `/work` (`internal/preflight/probe/probe.go:97-101,620-625`); neither `sandbox.Spec` nor the launch path supplies a byte quota. A small compressed input can make ffmpeg fill the backing filesystem before the post-exit check runs. A wall-clock timeout bounds time, not bytes or free-space damage. This contradicts the plan's own resource-bound outcome and the owner contract that hard disk/output limits are enforced by S6.2 rather than declared after consumption (`docs/HARNESS-SPEC.md:1481-1488`).

Impact: one allowed voice message can exhaust the daemon's filesystem and disrupt the journal/outbox, turning media rejection into a system-wide availability and durability failure.

Required correction: enforce the decoded-output ceiling during the write (for example, a size-limited filesystem/mount or a sandbox-enforced file-size/disk budget), kill and reap the process tree on breach, and prove both the boundary and cleanup with a highly compressible hostile fixture. Keep the post-run validation as defense in depth, not as the bound.

### 3. HIGH — The plan has no trustworthy transcript-output channel

`docs/PLAN-VOICE.md:35-38` says Whisper output is length-capped before admission, but the current sandbox exposes only combined stdout+stderr (`internal/sandbox/sandbox.go:76-78,325-330`). Its one-megabyte buffer silently discards overflow while reporting successful writes (`internal/sandbox/sandbox.go:397-410`). The plan neither separates Whisper diagnostics from transcript bytes nor requires an overflow signal. Consequently diagnostics can become canonical user text, and a truncated successful stream can be admitted without evidence that it was complete.

Impact: NEXUS can admit text the user did not say or silently omit a suffix while recording the result as the canonical durable input.

Required correction: define one unambiguous transcript artifact or stdout-only channel, keep diagnostics separate, stream-cap it with explicit `LIMIT_EXCEEDED` rather than silent truncation, validate UTF-8, trim, and reject empty output before admission. Add detectors for stderr noise, exact-cap/over-cap output, invalid UTF-8, and empty/silence output.

### 4. HIGH — “No redirects” is not implemented by the stated default

`docs/PLAN-VOICE.md:29-34` requires the download to stay on the Telegram egress host and says redirects are not followed because the default is disabled. Go's `http.Client` default is the opposite, and the existing adapter constructs `&http.Client{Timeout: 65 * time.Second}` with no `CheckRedirect` policy (`internal/channel/telegram/telegram.go:87-91`). Reusing that client as the plan suggests will follow redirects and can leave the allowed host.

Impact: the new file GET can violate its declared egress boundary and download attacker-selected bytes from a second host.

Required correction: explicitly set `CheckRedirect` to reject every redirect for the file-download client/path, validate the constructed URL's scheme and exact host before `Do`, and add a two-server detector asserting that a redirect response produces a typed refusal and the second server receives zero requests.

### 5. MEDIUM — The failure detector requires a terminal state that the design forbids creating

The failure branch explicitly says `EnqueueReplyID + offset advance, NO inbox row` and denies any terminal-on-failure claim (`docs/PLAN-VOICE.md:24-28`). The adapter detector nevertheless requires a transcription error to produce the reply “and a TERMINAL close” (`docs/PLAN-VOICE.md:48-50`). In the retained implementation, `TERMINAL` is an inbound state (`internal/channel/channel.go:51-59`), while `EnqueueReplyID` creates only an outbox event (`internal/channel/channel.go:410-437`). There is no inbound message ID to close. This is not merely stale prose: it gives the acceptance test an impossible oracle and risks reintroducing the exact admit-then-terminal lifecycle v2 claims to remove.

Impact: the specified detector cannot pass without violating the primary failure-path contract or asserting an unrelated state.

Required correction: require a stable refusal delivery row, zero inbox rows for that update, offset advancement only after the refusal enqueue is durable, and idempotent replay with one refusal. Remove the TERMINAL assertion.

## Non-blocking notes and proof ceiling

- NOTE: The injection detector still says `exec.Command args` (`docs/PLAN-VOICE.md:51-52`), although v2 requires the sandbox API. Anchor it to the captured sandbox `Spec.Args` and assert no shell/interpreter target is launched.
- Complexity pass: `internal/media` is a justified boundary for the two-stage pipeline; no separate queue or generalized media framework is warranted in this slice. Lean already.
- Strongest counterargument: the model could be copied into `/work`, and output could be written to a file there. That would make the commands runnable, but neither mechanism is in v2; per-call model copying adds large I/O and a mutable model surface, while an output file still needs an enforced write cap and an explicit completeness/error contract.
- Proof ceiling: this review establishes document-to-owner and document-to-current-interface consistency only. It does not establish ffmpeg/Whisper compatibility on the Raspberry Pi, sandbox runtime enforcement on that host, transcription quality, or RED capability of future detectors.

VERDICT: FAIL
