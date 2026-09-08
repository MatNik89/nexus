# PLAN-VOICE adversarial design review

## Scope and evidence binding

- Reviewed source: `docs/PLAN-VOICE.md` at commit `8772bd350fe940e6db6caf4e501633d8e74d4f14` on `slice/p1-voice`.
- Bound tree: `42ea6bd6ad4820b9b7de867b0aba9b15caecfbcd`.
- Bound plan blob: `fe4db462c6bb0f014b3c6e187364a5e309244110`; exported-file SHA-256: `486122fb846850535fe1c56b4a3e214f133b4e67a3f4801224cf28ad27044ae8`.
- Review environment: clean `git archive` export at `/home/matej/HARNESS/nexus-voice-review.9ug7Jc`, outside `/tmp`. The dirty working tree was not used as source evidence; the pre-existing untracked `docs/REVIEW-VOICE-PLAN-agy.md` was not read.
- Modality: design-only static review. No implementation, production execution, network probe, or test run.
- Verdict policy applied: only substantive correctness, security, buildability, or delivery-contract defects fail this review. Editorial issues are notes.

## Substantive findings

### 1. HIGH — The failure path cannot both transcribe-before-admission and close the inbox message `TERMINAL`

`docs/PLAN-VOICE.md:20-28` orders `getFile -> download -> Transcribe -> admit transcript`, then promises that a transcription failure returns a typed reply and the message "still closes TERMINAL." Those claims cannot be implemented through the current durable owner as stated. `internal/channel/telegram/telegram.go:249-285` admits a normalized message first and only then runs the handler; `internal/channel/channel.go:361-399` creates the stable message ID and durable `ADMITTED` row; and `internal/channel/channel.go:554-581` can atomically commit `TERMINAL + outbox reply` only for that existing message ID. A failure before `Admit` has no inbox row to transition.

The simpler current C2-style refusal is not equivalent: it can enqueue a stable refusal and advance the Telegram offset without an admitted/terminal inbox record (`internal/channel/telegram/telegram.go:242-247`). Conversely, admitting a placeholder before transcription would make the durable normalized text and conversation-history source differ from the transcript actually passed to the turn; the plan defines no typed media admission, transcription event/projection, or late-normalization transition to make that honest.

Impact: the implementation must either violate the promised terminal lifecycle, lose the transcript as the canonical admitted input, or invent an unreviewed parallel state path. This is a real B2/B7 delivery-state defect, not an editorial ambiguity (`docs/ARCHITECTURE-ESSENTIALS.md:196-205`; `docs/HARNESS-SPEC.md:1364-1367`).

Required design repair: choose and specify one durable sequence. The minimal honest option is to admit a typed voice descriptor first, persist a transcript-derived event before the turn, and use the existing `CompleteInbound` batch for both success/failure reply closure. If the product instead accepts pre-admission refusal semantics, remove the `TERMINAL` claim explicitly and prove the stable refusal/outbox path rather than claiming inbox completion.

Required detector: kill or fail independently after admission, during download, during ffmpeg, during whisper, and immediately before terminal completion; replay must yield one admission, no duplicate turn, and exactly one durable terminal-plus-reply outcome. Assert the durable inbox/outbox rows and canonical events, not only the returned string.

### 2. HIGH — Native decoders of channel-controlled bytes bypass the existing containment and process-lifecycle owners

`docs/PLAN-VOICE.md:13-19,30-34` proposes direct `exec.Command` execution and treats "bytes are audio, never executed" as the security argument. The bytes are nevertheless parsed by two large native programs under the daemon's OS identity. A crafted decoder exploit, a wedged process, or a descendant process would therefore bypass the containment that protects host files/network and the exact-process/tree lifecycle owner. Structured argv prevents shell parsing; it does not sandbox ffmpeg/whisper or make their parsers trusted.

The repository already provides the relevant boundary: process execution is content/policy-bound and cancellation kills the owned tree (`internal/sandbox/sandbox.go:33-40,80-117,192-235,266-299`), while the locked process contract requires identity-aware process-tree termination rather than killing only a recyclable parent PID (`docs/ARCHITECTURE-ESSENTIALS.md:68-75`; `docs/DESIGN-S0-sandbox-codex.md:1134-1157`). Go's `exec.CommandContext` default cancellation kills only `Cmd.Process`, not a process tree. The plan supplies no counterevidence that these media processes cannot create descendants or that direct execution is an approved separate boundary.

Impact: compromise of the bound Telegram account or a malicious/corrupt media object can become daemon-identity code execution or persistent resource consumption outside NEXUS's safety boundary. The prerequisite is narrower than an unauthenticated attack because profile binding is checked first, but it still crosses the remote-channel-to-host boundary that the sandbox is meant to preserve.

Required design repair: run both stages through a dedicated, deny-network media sandbox profile using the existing process identity/tree supervisor; expose only the private per-call directory and pinned executable/runtime closure. Bound stdout/stderr and require successful attestation before consuming the transcript. If S6.2 is intentionally declared tool-only, define an equally strong media-process owner rather than calling raw `exec.Command` a hard boundary.

Required detector: keep the real media launcher while substituting helpers that attempt host-file access, network access, and a surviving child; all must be denied/killed and reaped on timeout/cancel. Ablating only the sandbox/supervisor must turn the detector RED.

### 3. HIGH — The download design does not make its egress or 20 MB guarantee enforceable

`docs/PLAN-VOICE.md:20-21,26,35-36` states that the Telegram host is already allowed and that a 20 MB cap is enforced, but it does not define the URL-construction rule, redirect policy, DNS/IP binding, proxy handling, streaming limit, exact byte operator, or response-error redaction. The existing adapter uses a default `http.Client` (`internal/channel/telegram/telegram.go:87-91`) and constructs only Bot API POSTs through the token-sanitizing `call` helper (`internal/channel/telegram/telegram.go:127-183`). A file GET is a new path containing the bot token in the URL; the plan neither routes it through that sanitizer nor through the locked resolver/dialer boundary. NEXUS explicitly requires pinned resolution, redirect re-checking, metadata/private-address rejection where applicable, proxy sanitization, and an egress receipt (`docs/ARCHITECTURE-ESSENTIALS.md:146-158`).

The Telegram API's 20 MB service limit is not a local memory/disk bound. `file_size` is optional, a server can omit/misstate `Content-Length`, and a response may be chunked. Because `Transcribe` accepts `[]byte`, an implementation that calls `io.ReadAll` before checking length has already lost the claimed cap. Official Bot API documentation confirms that `file_path` comes from `getFile` and the current remote download maximum is 20 MB: <https://core.telegram.org/bots/api#getfile>.

Impact: a redirect/configuration/peer failure can escape the declared endpoint policy or expose the bot-token URL in diagnostics, and an oversized/chunked body can exhaust Pi memory or disk before rejection. These violate an explicit security claim and explicit size requirement.

Required design repair: construct the download URL under the validated Telegram origin only; use the NEXUS policy-aware transport; reject cross-origin redirects and revalidate every redirect/dial; sanitize the token on every error; and stream through a `limit+1` reader before allocation/write. Define the limit in bytes and the exact `<=` boundary. Validate HTTP status before accepting bytes.

Required detector: cover `limit-1`, `limit`, and `limit+1`; a missing/lying `Content-Length`; chunked `limit+1`; cross-origin redirect; same-origin redirect; private/metadata resolution; proxy variables; non-2xx; and token-bearing transport errors. In every rejection case the transcriber sink count must remain zero and no oversized temp artifact may remain.

### 4. MEDIUM — The compressed-input cap does not bound decoded disk, CPU, memory, or output

Even a correctly streamed 20 MB OGG limit (`docs/PLAN-VOICE.md:26`) does not bound the PCM expansion written by ffmpeg, whisper runtime memory, subprocess output, or temporary-disk consumption. The plan has only an unspecified wall timeout (`docs/PLAN-VOICE.md:13-19`). A timeout is not a disk bound: ffmpeg can write a much larger WAV before the deadline, and an exhausted shared filesystem can damage unrelated daemon persistence. The normative resource contract names disk bytes, RSS, process count, network bytes, output bytes, and whole-tree cancellation as distinct limits (`docs/HARNESS-SPEC.md:1481-1488`).

Impact: one accepted voice message can cause a host-level availability failure on the resource-constrained deployment target despite satisfying the advertised 20 MB check.

Required design repair: define a maximum accepted duration and derived maximum PCM bytes, enforce the decode/output ceiling while the process runs (not only after completion), cap captured stdout/stderr, and bind wall/RSS/process/disk limits at the media-process owner. Reject before whisper if the decoded artifact exceeds the contract.

Required detector: a highly compressible/long fixture and noisy-stderr helpers must hit the intended resource limit, leave the daemon responsive, kill/reap the whole tree, produce the typed failure, and leave no partial WAV.

### 5. MEDIUM — Synchronous transcription blocks the only Telegram outbox flush path, including reminder delivery

The plan calls the synchronous design a one-tick block and defers a queue until a visible stall (`docs/PLAN-VOICE.md:37-40`). Current control flow is stricter: `Adapter.Run` calls `PollOnce` and only after it returns calls `FlushOutbox` (`internal/channel/telegram/telegram.go:353-368`); `PollOnce` processes updates serially (`internal/channel/telegram/telegram.go:186-206`). Reminder work is enqueued independently (`cmd/nexus/main.go:244-278`), but its remote send still depends on that same later `FlushOutbox`. Therefore one voice blocks every flush tick for the entire download + ffmpeg + whisper duration, not one tick. On a Raspberry Pi, the hard timeout can become the user-visible lower bound on unrelated replies, approvals, and reminder delivery.

Impact: the new feature regresses the existing channel's delivery liveness and can violate the product's on-time reminder outcome (`docs/PRD.md:46-53`). Durability prevents loss but does not make a late reminder on time.

Required design repair: an async transcription job queue is not required for the first cut. Keep voice processing serial if desired, but give outbox flushing one non-overlapping independent ticker/worker so inbound CPU work cannot stop already-durable deliveries. State the remaining inbound head-of-line blocking honestly and set a measured timeout budget for the Pi.

Required detector: hold transcription behind a barrier, enqueue an unrelated outbox item/reminder, and prove it reaches the transport before releasing the barrier. Also prove a second flush cannot overlap the first and duplicate an attempt.

### 6. MEDIUM — Normal-return cleanup and configured command defaults are not crash-safe or executable as written

`docs/PLAN-VOICE.md:16-19,33-34` defaults the whisper executable/model to `~/...`, defaults ffmpeg to PATH lookup, and promises cleanup "on return." Go's `os/exec` deliberately performs no shell expansion, so a literal `~/whisper.cpp/...` executable path does not resolve; see <https://pkg.go.dev/os/exec#hdr-Overview>. PATH lookup also contradicts the plan's "fixed binary path" claim and permits command identity to drift after startup. Separately, deferred removal does not run on SIGKILL or daemon crash, leaving private voice and WAV data behind. The Annex process-recovery contract explicitly includes `TEMP_DIR` resources and requires an ownership-proven startup sweep (`docs/HARNESS-SPEC.md:1426-1433`).

Impact: the advertised default fails on the target host unless an unstated expansion layer is added; executable identity can change between calls; and sensitive media plus potentially large decoded files can persist across crashes.

Required design repair: resolve home-relative defaults at the configuration owner with `os.UserHomeDir` + `filepath.Join`; resolve/canonicalize and pin executable/model identities in the sealed startup snapshot; use a private profile-bound media temp root; and define an ownership-safe startup cleanup/recovery rule. Normal cleanup remains `defer`, but it is not the crash-recovery claim.

Required detector: exercise the actual defaults (not only PATH fakes), swap the executable/model after startup and require fail-closed behavior, then SIGKILL a helper after both artifacts exist and prove restart removes only owned stale media directories.

## Rejected material hypotheses and positive controls

- **Shell command injection from `file_id` or a filename is rejected under the stated construction.** `exec.Command(name, arg...)` does not invoke a shell, and fixed local names such as `in.ogg`/`out.wav` prevent Telegram values from becoming executable paths or options. The file ID should remain JSON data to `getFile`; Telegram `Voice` does not supply an original filename. The proposed metacharacter test is still too weak because it proves only the absence of a shell, not URL, redirect, option, or parser safety.
- **Normal-path temp permissions are sound.** A fresh private directory plus 0600 files prevents cross-user pathname races in the ordinary path. Finding 6 concerns crash retention and ownership, not the stated normal-return cleanup.
- **A typed failure reply is the correct user-facing policy.** The defect is the missing durable sequencing needed to make that reply and `TERMINAL` one honest outcome, not the decision to fail closed.
- **Serial voice processing is a valid first-cut tradeoff only after outbound delivery is decoupled.** Deferring a durable async media queue is reasonable; blocking the sole outbox worker is the substantive regression.

## Detector assessment

The three detector bullets at `docs/PLAN-VOICE.md:42-51` are insufficient for the claims above. The fake-binary happy/error test does not exercise executable resolution, the real parser boundary, whole-tree cancellation, resource ceilings, or cleanup. The adapter test does not specify durable admission/event/outbox assertions or crash/replay seams. The injection assertion is construction-only and cannot validate egress or decoder containment. The minimum additions are stated under each finding; at least one test must retain the production boundary and turn RED when only the claimed protection is ablated.

## Notes (non-blocking)

- `docs/PLAN-VOICE.md:40` says "flush interback"; this appears to mean "flush interval."
- The timeout value, 20 MB unit, and Croatian prefix/reply exact byte strings should be constants in the eventual contract so boundary tests do not guess them.

## Topknot / proof ceiling

The lean path is not an async queue. Reuse the existing channel admission/outbox owner, existing sandbox/process owner, existing config snapshot, and one independent outbox worker. No new generic media framework is justified by this slice.

Proof ceiling: this is an immutable-plan static review. It establishes design contradictions and missing enforceable contracts, not runtime exploitability or performance on the Raspberry Pi. The strongest unverified deployment variable is real ffmpeg/whisper latency and resource growth on the target hardware; measure it before choosing the timeout and duration ceiling.

VERDICT: FAIL
