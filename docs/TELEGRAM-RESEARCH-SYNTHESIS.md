# NEXUS Telegram gateway — research synthesis (2026-09-08)

Sources, all cross-checked:
- Owner's patch note (Desktop/nexus-telegram-tablice-patch.md)
- hermes --yolo research (nexus-soak/HERMES-TG-RESEARCH.md, 476 lines,
  incl. hermes' OWN `_send_rich_text()` code)
- Subagent A (nexus-soak/SUBAGENT-A-TG-HARNESSES.md, ~30 projects)
- Subagent B (nexus-soak/SUBAGENT-B-BOTAPI.md, verified on the raw
  official Bot API HTML — not a summarizer)

## The single decisive fact (independently confirmed 4×)
`sendRichMessage` is REAL — Bot API 10.1 (2026-06-11), current 10.3.
Takes raw GFM markdown and renders NATIVE tables (≤20 cols), task
lists, LaTeX math, footnotes, collapsibles, inline buttons.
Limits: 32768 chars / 500 blocks / 16 nesting / 50 media.
=> Our bullets-render is a WORKAROUND we can now retire for real tables
   (keeping it only as the fallback for pre-10.1 clients).

## Recommendation list (ranked)

### R1 — sendRichMessage for tables/rich (HIGH, direct owner ask)
Detect a GFM table (header + `|---|` delimiter) or other rich construct;
send via sendRichMessage; on a 400 (old client), fall back to today's
HTML/bullets path. Same delivery honesty (one row, one SENT). This is
the owner's patch, translated to our Go adapter + first-lease model.

### R2 — LLM prompt fix (HIGH, pairs with R1)
The model learned to emit bullets because tables never rendered. Add to
the system prompt: "send tables as pipe-markdown tables, never as
bullet lists — the gateway renders them." Without R2, R1 has nothing
to render.

### R3 — sendRichMessageDraft streaming + Stop (MEDIUM, big UX win)
Native "typing" draft with a user Stop button (10.1). Replaces the
editMessageText streaming hack in DMs. hermes uses editMessageText for
streaming because no editRichMessage exists — draft is the sanctioned
path.

### R4 — CLI↔Telegram session handoff (MEDIUM, the owner's older ask)
Continue a desktop/CLI conversation on Telegram. Reference: hermes
`/handoff` + OpenClaw's gateway-owned canonical session. Winning
pattern maps EXACTLY onto our design: the JOURNAL is the single source
of truth; a handoff is a POINTER (re-bind the destination key to the
live session id), never a summary. We already have per-profile journals
and deterministic turn ids — this is a binding + `/handoff` command
slice, not a rearchitecture.

### R5 — Media: voice → whisper (MEDIUM, easiest media win)
whisper.cpp is already on the Pi. getFile → download OGG/Opus → whisper
→ text into the turn. Table stakes per every gateway surveyed.

### R6 — Inline-keyboard HITL approvals (MEDIUM)
Our approvals are text commands (approve/deny). 10.3 added disabled
inline buttons — approve/deny/retry as tap buttons is the community
standard for bot HITL.

### R7 — Documents → text extraction (LOW-MED)
getFile (≤20 MB) → mime-routed extraction (PDF/txt) into the turn.

### R8 — Images → vision (LOW, blocked on provider)
DeepSeek has no vision. Needs a vision-capable model on the image path
only, OR a local model. Research the exact provider before planning.

### R9 — sendChatAction typing indicator (LOW, cheap polish)
Show "typing…" while the turn runs.

### R10 — Keep it small (PRINCIPLE)
Strong community counter-trend: nanobot (~4k lines) recommended over
OpenClaw (~430k). Our gateway is already lean — resist scope creep;
every item above is its own reviewed slice.

## Rejected / not now
- MTProto / user-account (Telethon, gotd): post-10.1 there is NO
  rendering gap, and it risks the owner's personal Telegram account
  (official "under observation"). Skip.
- Bullets as the PRIMARY table path: superseded by R1 (kept as fallback).

## Immediate context
- convproj slice (soak backlog fix) is in impl review now.
- R1+R2 are the direct answer to the owner's live table complaint and
  should lead once convproj lands.

## Media branch (from hermes QA + subagents C & D, all cross-cited)

### Image (R8 unblocked, hardcode hermes' shape)
- Pipeline (hermes' real code): getFile → download to LOCAL cache
  IMMEDIATELY (Telegram file URLs expire ~1h) → resize DOWN before
  vision → base64 data-URL → vision model.
- CRITICAL for us: a vision result is baked into history and re-sent
  every later turn — hermes resizes to EMBED_TARGET 256KB / 1568px max.
  With our conv projection this matters doubly (history is now cheap to
  read, but a fat image blob per turn would bloat the provider request).
  => store a SHORT text description in history, NOT the raw image.
- Provider: DeepSeek has no vision. Cheapest verified: Gemini 2.5
  Flash-Lite ($0.10/1M); OpenRouter most flexible; moondream2 local on
  8GB Pi ~25s/image (too slow for interactive). Recommend a vision-only
  auxiliary provider on the image path (config field), main provider
  stays DeepSeek.

### Voice (R5, easiest)
- ffmpeg `-ar 16000 -ac 1 -c:a pcm_s16le` → whisper.cpp (already on Pi;
  realistically tiny/base model on ARM) OR Groq API ~$0.04/hr.

### Video
- ffmpeg audio extract → whisper (transcript >> frames for quality) +
  optional I-frame keyframes to vision. MUST be an async job (Pi is
  slow); never block the turn.

### Social links (FB/IG/TikTok)
- Cheap first pass: Open Graph meta tags via curl (~500 tokens, no
  login). Then yt-dlp `--dump-json`/`-J` for public reels; gallery-dl
  for TikTok/IG carousels. HONEST: on Pi ARM64 headless Chromium for
  FB/TikTok times out ~100% — use meta-tags, skip the browser. Instagram
  private/stories need cookies + a burner account (account-lock risk) —
  do NOT auto-login.

### Web pages
- Go-native: go-readability / readeck (go-shiori original archived);
  chromedp/rod only where JS is required (ARM caveats); screenshot →
  vision as the last-resort fallback.

### Engineering rule (all sources agree)
- Shell out to yt-dlp/ffmpeg/whisper.cpp binaries with single-arg
  exec.Command + hard timeouts; run slow media work ASYNC off the turn
  path; fail gracefully to a cheaper tier; never reimplement these.

## Correction logged (evidence beats claims)
- `draft_id` does NOT exist in the official Bot API (subagent D verified
  on the raw docs). sendRichMessageDraft streaming is tracked by the
  returned message_id, continued via the rich editMessageText path.
  State to keep: {chat_id, message_id, cancelFunc}.
