# PLAN: voice messages → transcription (slice p1-voice)

Owner ask: NEXUS must handle voice/media, hardcoded. Voice is first
(tools already on the Pi, no new provider).

## Stolen pipeline (hermes' exact path, verified in its code + subagent C)
getFile → download OGG/Opus → `ffmpeg -i in.ogg -ar 16000 -ac 1 -c:a
pcm_s16le out.wav` → `whisper-cli -m <model> -f out.wav -nt` → transcript
becomes the turn's user text. Multiple sources agree on these exact
ffmpeg flags.

## Change (v2 — round-1 HIGH findings folded)
- EXECUTION THROUGH THE SANDBOX, not raw exec (r1 codex #2): ffmpeg and
  whisper-cli run as ExecProcess through internal/sandbox (the bwrap
  boundary that already contains host FS/net and kills the whole process
  tree on cancel) — NEVER a bare exec.Command. Channel-controlled bytes
  are decoded inside containment, not under the daemon identity. The
  temp dir is the only writable mount; net is denied (transcription is
  offline).
- ADMIT-THEN-HANDLE, refusal on failure (r1 codex #1): the flow mirrors
  today's text path exactly. On a voice message, transcribe FIRST; on
  SUCCESS, admit the transcript as the turn text (existing Admit ->
  handle -> terminal+outbox recipe, transcript is the canonical durable
  input). On FAILURE, enqueue a typed Croatian refusal via the SAME
  path the current non-text C2 refusal uses (EnqueueReplyID + offset
  advance, NO inbox row) — there is no "admit then TERMINAL on failure"
  claim. (This means a voice that fails to transcribe is never admitted,
  identical in shape to today's photo/doc refusal.)
- BOUNDED DOWNLOAD (r1 codex #3): the getFile file GET goes through the
  token-sanitizing request path (no token in diagnostics), reads through
  an io.LimitReader capped at 20 MiB (never io.ReadAll first), and
  rejects a body that hits the cap. Same egress target as the existing
  Bot API calls (Telegram host) — no new host. URL built by the adapter,
  no redirects followed (default disabled).
- RESOURCE BOUNDS (r1 codex #4): the sandbox invocation carries a hard
  wall-clock timeout (kills the tree), the temp dir is size-checked
  after ffmpeg (reject a decoded WAV over a fixed cap), and the whisper
  transcript output is length-capped before it becomes turn text.
- internal/media: Transcribe(ctx, ogg []byte) shells the two binaries
  THROUGH the sandbox with structured argv, per-call 0700 mkdtemp,
  cleaned on return. Binary/model paths from config with Pi defaults.

## Detectors
- media.Transcribe unit: with a stub whisper/ffmpeg (a fake binary in
  the test's PATH/temp that echoes a known transcript), a known ogg
  yields the expected text; a non-zero binary exit returns an error
  (fail-closed, no partial).
- adapter: a voice update (fake Bot API serving getFile + file bytes)
  admits the transcript as the turn text; a transcription error yields
  the typed Croatian reply and a TERMINAL close.
- injection: an ogg "filename" / file_id containing shell metacharacters
  never reaches a shell (asserted by construction — exec.Command args).

## Non-goals (own later slices)
- Images (vision provider), video, social links, documents.
- Async job queue, TTS replies, speaker diarization.
