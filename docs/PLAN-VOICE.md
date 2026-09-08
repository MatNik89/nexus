# PLAN: voice messages → transcription (slice p1-voice)

Owner ask: NEXUS must handle voice/media, hardcoded. Voice is first
(tools already on the Pi, no new provider). Local whisper.cpp, not a
remote transcription API: the owner's private voice never leaves the Pi.

## Stolen pipeline (hermes' exact path, verified in its code + subagent C)
getFile → download OGG/Opus → `ffmpeg -i in.ogg -ar 16000 -ac 1 -c:a
pcm_s16le out.wav` → `whisper-cli -m <model> -f out.wav -otxt -of out`
→ read `out.txt` → transcript becomes the turn's user text. Multiple
sources agree on the ffmpeg flags.

## PREREQUISITE (blocks this slice): sandbox read-only input grant
whisper needs its model (`~/whisper.cpp/models/ggml-base.bin`, ~140 MB)
readable INSIDE the sandbox. The current `sandbox.Spec` mounts only the
executable's ELF closure + the writable WorkDir — a data file at a config
host path is neither, so `-m <model>` cannot open it (codex #1 / kilo F1,
both agents, decisive). Copying 140 MB into a per-call WorkDir on an
SD-card-bound Pi is rejected (latency + a mutable model surface).

=> Voice depends on a separate, already-reviewed kernel slice that adds a
digest-pinned read-only input bind to the sandbox. See
`docs/PLAN-SANDBOX-ROINPUT.md`. This plan is not buildable until that
lands; it is listed here as an explicit dependency, not smuggled in.

## Change (v3 — round-2 findings folded; all 5 verified real)
- EXECUTION THROUGH THE SANDBOX, not raw exec (r1 codex #2): ffmpeg and
  whisper-cli run as ExecProcess through internal/sandbox (the bwrap
  boundary that already contains host FS/net and kills the whole process
  tree on cancel) — NEVER a bare exec.Command. Channel-controlled bytes
  are decoded inside containment, not under the daemon identity. The
  temp dir is the only writable mount; net is denied (transcription is
  offline); the model is bound read-only via the prerequisite grant.
- ADMIT-THEN-HANDLE, refusal on failure (r1 codex #1): on a voice
  message, transcribe FIRST; on SUCCESS, admit the transcript as the turn
  text (existing Admit → handle → terminal+outbox recipe, transcript is
  the canonical durable input). On FAILURE, enqueue a typed Croatian
  refusal via the SAME path the current non-text C2 refusal uses
  (EnqueueReplyID + offset advance, NO inbox row) — never "admit then
  TERMINAL on failure". A voice that fails to transcribe is never
  admitted, identical in shape to today's photo/doc refusal.
- BOUNDED, NON-REDIRECTING DOWNLOAD (r1 codex #3 + r2 codex #4): the
  getFile file GET uses a DEDICATED http.Client whose `CheckRedirect`
  rejects EVERY redirect (Go's default follows them; the existing
  telegram client sets no policy and must NOT be reused for this path).
  The URL scheme+exact host are validated before `Do`. The body is read
  through an `io.LimitReader` capped at 20 MiB (never io.ReadAll first);
  a body that REACHES the cap is rejected as truncated — read cap+1 and
  reject if the extra byte arrives, so "ended exactly at the cap" and
  "hit the cap" are distinguished (kilo note). Token never in diagnostics.
- DISK-WRITE BOUND, enforced not observed (r2 codex #2): the decoded-WAV
  ceiling is enforced DURING the write by RLIMIT_FSIZE on the sandbox
  child (ffmpeg's write past the cap takes SIGXFSZ and the tree is
  reaped), plus `ffmpeg -t <maxSeconds>` bounding input duration. A
  post-run size check on WorkDir stays as defense-in-depth, NOT as the
  bound. A highly compressible hostile OGG must not be able to exhaust
  the filesystem before any check runs.
- TRUSTWORTHY TRANSCRIPT CHANNEL (r2 codex #3): whisper writes the
  transcript to a FILE in WorkDir (`-otxt -of out` → `out.txt`); NEXUS
  reads that file, never combined stdout+stderr (the sandbox's shared
  1 MiB stdout+stderr buffer silently drops overflow and would mix
  whisper diagnostics into "what the user said"). The transcript file is
  read through a length cap, validated UTF-8, trimmed; empty/whitespace
  output is rejected before admission.
- RESOURCE BOUNDS: the sandbox invocation carries a hard wall-clock
  timeout (kills the tree) in addition to the RLIMIT_FSIZE + duration
  bounds above.
- internal/media: `Transcribe(ctx, ogg []byte) (string, error)` shells
  the two binaries THROUGH the sandbox with structured argv, per-call
  0700 mkdtemp with 0600 files inside, cleaned on `defer`. Binary/model
  paths from config with Pi defaults.

## Detectors (RED before, GREEN after; anchored to the contract)
- media.Transcribe unit: with a stub whisper/ffmpeg (a fake binary bound
  as the sandbox Target that writes a known `out.txt`), a known ogg yields
  the expected text; a non-zero binary exit returns an error (fail-closed,
  no partial); an empty `out.txt` is rejected; a transcript over the cap is
  rejected.
- disk bound: a highly compressible fixture that decodes far past the WAV
  cap is killed by the write bound and the WorkDir never exceeds the cap
  (asserts the enforced bound, not the post-hoc check).
- transcript channel: whisper stub emitting stderr noise + a stdout line
  must NOT let either reach the admitted text — only `out.txt` content is
  admitted.
- redirect: a two-server fake — the file GET receiving a 30x must produce
  a typed refusal and the SECOND server must receive ZERO requests.
- adapter failure path (corrected, r2 codex #5 / kilo F2): a transcription
  error yields the typed Croatian refusal DELIVERY row, ZERO inbox rows
  for that update, offset advances only after the refusal enqueue is
  durable, and replay is idempotent (one refusal). NO "TERMINAL close"
  assertion — TERMINAL is an inbound state and this path admits nothing.
- injection: an ogg filename / file_id with shell metacharacters never
  reaches a shell — asserted against the captured sandbox `Spec.Args`
  (structured argv, no interpreter target), NOT exec.Command.

## Known, accepted limitation (kilo note, non-blocking)
Transcribe-first means the update is un-admitted for the whole (possibly
minutes-long) transcription; a redelivery mid-transcribe re-transcribes,
and whisper is not byte-deterministic, so two runs could yield slightly
different text. The deterministic `turn_id` still prevents DOUBLE
admission; only which text wins is racy. Acceptable for P1 voice; a
transcription cache keyed by `turn_id` is a later optimization.

## Non-goals (own later slices)
- Images (vision provider), video, social links, documents.
- Async job queue, TTS replies, speaker diarization.
