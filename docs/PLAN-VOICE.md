# PLAN: voice messages → transcription (slice p1-voice)

Owner ask: NEXUS must handle voice/media, hardcoded. Voice is first
(tools already on the Pi, no new provider).

## Stolen pipeline (hermes' exact path, verified in its code + subagent C)
getFile → download OGG/Opus → `ffmpeg -i in.ogg -ar 16000 -ac 1 -c:a
pcm_s16le out.wav` → `whisper-cli -m <model> -f out.wav -nt` → transcript
becomes the turn's user text. Multiple sources agree on these exact
ffmpeg flags.

## Change
- New `internal/media` package: `Transcribe(ctx, ogg []byte) (string,
  error)` — writes ogg to a temp file, shells ffmpeg then whisper-cli
  with SINGLE-arg exec.Command + a hard timeout, returns trimmed text.
  Binary + model paths come from config (defaults:
  whisper-cli=~/whisper.cpp/build/bin/whisper-cli,
  model=~/whisper.cpp/models/ggml-base.bin, ffmpeg=ffmpeg on PATH).
  Everything runs in a per-call temp dir, cleaned up.
- Telegram adapter: when `Message.Voice` is present (and the chat is
  bound), parse its file_id, getFile → download the file, Transcribe,
  and admit the TRANSCRIPT as the turn text (prefixed "[glasovna
  poruka] " so the model knows it was spoken). The current C2 non-text
  refusal is lifted FOR VOICE only; photo/doc still refuse for now
  (their own slices).
- getFile 20MB cap enforced; a transcription failure returns a typed
  Croatian reply ("Nisam uspio pretvoriti glasovnu poruku u tekst."),
  never a silent drop; the message still closes TERMINAL.

## Security / honesty
- exec.Command with a fixed binary path + explicit args (NEVER a shell
  string) — no injection surface from the audio.
- Downloaded bytes are audio, never executed; temp files are 0600 in a
  per-call mkdtemp, removed on return.
- Egress: getFile downloads from the Telegram file host (already an
  allowed egress target for the bot). No new external host.
- Synchronous in the turn for the first cut (a single-user bot; one
  voice blocks one flush tick). topknot ceiling: async job queue when
  a real backlog appears (subagent C's note) — trigger: a voice longer
  than the flush interback causes a visible stall.

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
