# PLAN: voice messages → transcription (slice p1-voice)

Owner ask: NEXUS must handle voice/media, hardcoded. Voice is first
(tools already on the Pi, no new provider). Transcription is LOCAL
(whisper.cpp on the Pi); the audio is NOT sent to any additional
transcription provider. (Telegram itself already transported/stored the
original message — so "never leaves the Pi" is not claimed.)

## Stolen pipeline (hermes' path, verified in its code + subagent C)
getFile → download OGG/Opus → ffmpeg → whisper-cli, all with ABSOLUTE
in-sandbox paths (there is no `--chdir /work`; `probe.Prepare` only
`--dir /work`, so relative names would not resolve):

    ffmpeg -i /work/in.ogg -t <maxSeconds> -ar 16000 -ac 1 -c:a pcm_s16le /work/out.wav
    whisper-cli -m /inputs/model.bin -f /work/out.wav -otxt -of /work/out -nt

`-nt` (no-timestamps) is REQUIRED: `-otxt` alone writes `[hh:mm:ss --> …]`
per-segment prefixes into `out.txt`, which would enter the turn text as
"what the user said". The model is at `/inputs/model.bin` via the
prerequisite grant, not a host path.

## PREREQUISITES (block this slice) — explicit dependencies, not smuggled in
1. Sandbox read-only input REGISTRY — whisper must read its ~140 MB model
   INSIDE the sandbox; the current `sandbox.Spec` mounts only the closure +
   writable WorkDir. See `docs/PLAN-SANDBOX-ROINPUT.md` (startup-owned held-fd
   registry, identity-pinned, in policyHash + attestation; no per-call open).
2. Telegram egress pinned-IP dialer — the file download is another
   attacker-influenced fetch and E11 (locked) requires a pinned-IP,
   proxy-sanitized, redirect-checked dialer. See `docs/PLAN-TG-EGRESS-DIALER.md`.
   Voice reuses that compliant client rather than waiving E11 (r3 codex F4).
Voice is not buildable until both land.

## Change (v4 — round-3 findings folded; all verified against the code)
- EXECUTION THROUGH THE SANDBOX (r1 codex #2): ffmpeg and whisper-cli run
  as ExecProcess through internal/sandbox — NEVER a bare exec.Command.
  Channel-controlled bytes are decoded inside containment (no net, no host
  FS beyond `/work` RW + the read-only model grant), and the whole process
  tree is killed on cancel/timeout.
- ADMIT-THEN-HANDLE, refusal on failure (r1 codex #1): transcribe FIRST;
  on SUCCESS admit the transcript as the turn text; on FAILURE enqueue the
  typed Croatian refusal via the SAME non-text C2 refusal path
  (EnqueueReplyID + offset advance, NO inbox row) — never "admit then
  TERMINAL". A voice that fails to transcribe is never admitted.
- REPLAY-DETERMINISTIC via the EXISTING journal owner (r3 codex #5 + F1):
  NO new durable store — the journal is the only canonical writer (E4/B7).
  `chan_inbox` already persists the admitted text on `EvInboundAdmitted`
  (`channel.go:225-240`). The flow: transcribe → `Admit(transcript)`. First
  admission handles the transcript. On a REPLAY, admission returns
  `Replayed` PLUS the CANONICAL stored text, and the handler runs THAT, not
  its freshly-computed candidate — so a redelivery that re-transcribes into a
  different B never handles/journals B; the first-admitted A always wins.
  This requires only an additive change: `AdmitOutcome` (today `{MessageID,
  Replayed}`, `channel.go:70-75`) returns the stored canonical text on
  replay, and the adapter (`telegram.go:249-273`) hands the handler the
  returned text. Whisper MAY re-run on redelivery (wasteful, harmless) — the
  "never re-run" promise is relaxed; correctness comes from the stored text
  winning, not from suppressing the second transcription.
- SAFE OUTPUT READ (r3 codex #4): `/work/out.txt` is opened relative to a
  HELD WorkDir fd with no-follow / RESOLVE_BENEATH (openat2), then `fstat`ed
  on that fd — must be a REGULAR file, `nlink == 1`, owned by the daemon;
  bytes are read from that same fd. A symlink, FIFO, hardlink, or non-regular
  output is rejected. This stops a compromised decoder from turning the
  transcript channel into host-file disclosure (e.g. an `out.txt` symlink to
  a secret) or a daemon hang. Content is then length-capped, UTF-8 validated,
  trimmed; empty/whitespace output is rejected before admission.
- BOUNDED DOWNLOAD over the COMPLIANT client (r1 codex #3 + r3 codex #4/#6/F4):
  the getFile file GET goes through the Telegram pinned-IP dialer client from
  prerequisite 2 (Proxy sanitized, IP pinned anti-rebind, redirects rejected —
  E11 satisfied, not waived). This slice adds only the bounded read: URL
  scheme+exact host validated before `Do`; body read through `io.LimitReader`
  at 20 MiB, reading cap+1 and rejecting if the extra byte arrives (distinguish
  "ended at cap" from "truncated"). Token never in diagnostics.
- RESOURCE BOUNDS, honestly scoped (r3 codex #3 / kilo F1): the decoded-WAV
  size is bounded DURING encode by `ffmpeg -t <maxSeconds>` (output is
  32 KB/s mono s16le, so bytes ≤ 32768 × maxSeconds regardless of input) —
  this is the enforced bound on the honest path, not a post-hoc check. The
  sandbox wall-clock timeout kills the tree. `RLIMIT_FSIZE` is NOT claimed
  (it does not exist in the sandbox and is per-file, not aggregate). The
  aggregate hard `disk_write_bytes`/`file_count` bound for a COMPROMISED
  decoder is the existing S6.2 **P2.2 ResourceBudget** obligation
  (`docs/HARNESS-SPEC.md:1481-1484`), named as a ceiling here, not
  re-invented in this slice.
- internal/media: `Transcribe(ctx, ogg []byte) (string, error)` shells the
  two binaries THROUGH the sandbox with structured argv (absolute in-sandbox
  paths), per-call 0700 mkdtemp with 0600 files, cleaned on `defer`.
  Binary/model paths from config with Pi defaults.

## Detectors (RED before, GREEN after; anchored to the contract)
- media.Transcribe unit: a stub ffmpeg/whisper (fake Target that writes a
  known `/work/out.txt`) yields the expected text; a non-zero exit returns an
  error (fail-closed, no partial); an empty `out.txt` is rejected; a
  transcript over the cap is rejected; a stub emitting `[00:00 -->]` when
  `-nt` is ABSENT proves the flag is present in the captured argv.
- output-escape: a stub that writes `out.txt` as (a) a symlink to a host
  canary and (b) a FIFO — the daemon MUST reject both; the canary bytes never
  enter the transcript or journal, and the read does not block.
- replay-determinism: transcript A admitted, then a redelivery transcribes B;
  assert admission returns A's stored canonical text and the handler/provider
  + journal see A only (RED against the adapter handling its local B candidate).
- download-client: assert the voice download uses the prerequisite-2 compliant
  client (proxy/redirect/rebind detectors live in the dialer slice); here,
  assert the 20 MiB cap+1 rejection and scheme/host pre-validation.
- adapter failure path (r2 codex #5 corrected): a transcription error yields
  the refusal DELIVERY row, ZERO inbox rows for that update, offset advances
  only after the refusal enqueue is durable, replay is idempotent. NO TERMINAL
  assertion.
- injection: an ogg filename / file_id with shell metacharacters never reaches
  a shell — asserted against the captured sandbox `Spec.Args` (structured
  argv, no interpreter target).

## Non-goals (own later slices)
- Images (vision provider), video, social links, documents.
- Async job queue, TTS replies, speaker diarization.
- Aggregate disk/file-count quota (existing S6.2 P2.2 slice).
