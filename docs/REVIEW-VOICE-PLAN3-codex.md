# PLAN-VOICE v3 + PLAN-SANDBOX-ROINPUT adversarial design review — Codex

## Scope and evidence boundary

Read-only joint design review at commit
`360cbd62e3837ad39cf11a0df5bf340c7c57d6bb` on `slice/p1-voice`.

- Tree: `8cf5a69f0dfdd8eb1b55fc288c54b2a4a93572ef`
- `PLAN-VOICE.md` blob / SHA-256: `f91efc5fe42de14eba1254372ef8f0d998e14a85` / `b60f60c70848f98711116e78d18cff997de1feb2e07f495bef27eaba6ff1209d`
- `PLAN-SANDBOX-ROINPUT.md` blob / SHA-256: `e0a39631173bdc62bee66253216e12618b00a67f` / `34caa6836cef6c6d76260e79bcaae8101b18ac75625b28b4ac190c180808a974`
- Evidence source: clean `git archive` export on the `/home/matej/HARNESS` filesystem.
- Modality: static design/source review only. No production test, ffmpeg, Whisper, or sandbox workload was executed.

## Round-2 closure assessment

The v3 direction genuinely closes three earlier defects: it selects a transcript file instead of the sandbox's combined stdout/stderr (`docs/PLAN-VOICE.md:58-64`), explicitly rejects redirects with a dedicated client (`:42-50`), and replaces the impossible failure-path TERMINAL oracle with one stable refusal plus zero inbox rows (`:87-91`). It also makes model visibility an explicit prerequisite rather than pretending the current sandbox can open the host path (`:13-24`). The proposed prerequisite and disk control do not yet preserve the invariants they claim, and the admitted-text replay path has a new correctness contradiction.

## Substantive findings

### 1. HIGH — Re-hash-then-`--ro-bind` does not pin the bytes that the sandbox reads

`docs/PLAN-SANDBOX-ROINPUT.md:49-57` re-`Lstat`s and re-hashes `HostPath`, then later passes that pathname to bwrap as `--ro-bind HOST DEST`. The pathname can be replaced after verification but before bwrap resolves it; even if rename is prevented by holding an fd, the daemon-owned source inode can be modified after the read-only bind and those changed bytes become visible inside the sandbox. This is not the “same” discipline as the existing closure: `resolveClosure` reads bounded bytes once and places those exact bytes in a memfd (`internal/preflight/probe/probe.go:152-183,276-310`), and `Prepare` passes the already-open memfd via `ExtraFiles` plus `--ro-bind-data` (`:639-647`).

Impact: the compiled digest and attestation can describe model A while Whisper consumes model B. A same-user pathname/inode mutation restores the TOCTOU class that closure pinning was built to remove.

Required correction: open with no-follow semantics, validate and bounded-read from that one descriptor, then mount immutable bytes (sealed memfd / `--ro-bind-data`) or require and verify an equivalently immutable content-addressed artifact. Never re-open a verified pathname at bwrap start. Add a seam after launch verification but before bwrap consumes the grant, mutate/replace the host file there, and require either the original pinned bytes or refusal before the target starts.

### 2. HIGH — Input grants are not fully part of namespace identity, policy identity, or attestation

The prerequisite stores only `{Dest,digest,size}` “alongside `closurePins`” (`docs/PLAN-SANDBOX-ROINPUT.md:38-47`) and never requires the grant set to enter `policyHash`, `Process` evidence, or `Attestation`. Today `policyHash` covers target/target hash/closure/argv/workdir/timeout/probe (`internal/sandbox/sandbox.go:227-235`), while `Attest` hashes only `Process.closure` (`:352-375`). Two policies with different model grants can therefore retain the same declared identity unless the design explicitly changes both paths.

The destination validation is also bypassable as written: it rejects exact names such as `/work` and `/nexus-target` but does not require `Dest == filepath.Clean(Dest)` or confine destinations to a reserved subtree. `/inputs/../nexus-target`, a descendant of `/nexus-libs`, or another later bwrap mount can normalize to/shadow a reserved target after the raw-string checks. `probe.Prepare` appends mounts in order (`internal/preflight/probe/probe.go:605-665`), so a later colliding grant changes the namespace the existing closure attestation purports to describe.

Impact: a different or shadowing data grant can execute under an indistinguishable policy/attestation, violating the sealed S6.2 boundary.

Required correction: define a canonical sorted grant identity including canonical source identity, normalized destination, digest, size, and limit; include it in `policyHash`, the launched process evidence, and the attestation digest. Restrict destinations to normalized single-file children of one reserved `/inputs` directory and reject equality, ancestor, descendant, or normalized collisions with every built-in mount. Add policy-hash/attestation-difference and normalized-collision detectors.

### 3. HIGH — The proposed `RLIMIT_FSIZE` mechanism is missing and is not an aggregate WorkDir bound

`docs/PLAN-VOICE.md:51-57,65-67` says the sandbox child receives `RLIMIT_FSIZE`, but neither this plan nor the prerequisite adds a file-size field to `sandbox.Spec` or `probe.Spec`. The current sealed interface has only target, argv, WorkDir, and timeout (`internal/sandbox/sandbox.go:33-40`; `internal/preflight/probe/probe.go:94-103`), and `Prepare` constructs bwrap with only mount/network/PID/seccomp options and `SysProcAttr{Setpgid:true}` (`internal/preflight/probe/probe.go:605-668`). Thus the claimed control is not buildable through the named interface without another kernel design change.

Even if installed, `RLIMIT_FSIZE` limits each regular file, not cumulative bytes or file count in the host-bound WorkDir. A compromised decoder can create many files, each below the per-file ceiling, and exhaust the filesystem. The detector's assertion that “the WorkDir never exceeds the cap” (`docs/PLAN-VOICE.md:79-81`) therefore cannot be established by the proposed mechanism. Annex P2.2 assigns hard `disk_write_bytes` and `file_count` enforcement to S6.2, not to a post-run observer (`docs/HARNESS-SPEC.md:1481-1488`).

Impact: hostile media can still fill the filesystem that holds the journal/outbox, causing system-wide availability and durability failure.

Required correction: design a sandbox-level aggregate writable-filesystem quota (and file-count bound), expose it in the sealed policy and attestation, kill/reap on breach, and retain `RLIMIT_FSIZE` only as a per-file defense. The RED must create multiple sub-cap files as well as one oversized WAV and prove peak aggregate usage never exceeds the declared budget.

### 4. HIGH — Host-side reading of `out.txt` creates a symlink/FIFO escape from the sandbox

`docs/PLAN-VOICE.md:58-64` says NEXUS reads `/work/out.txt` after Whisper exits and specifies only a length cap, UTF-8 validation, trimming, and empty rejection. `/work` is a host directory bind (`internal/preflight/probe/probe.go:620-625`). A compromised media parser can create `out.txt` as a symlink to an arbitrary absolute host path that is invisible inside the namespace; when the daemon later opens the host-side pathname, the host kernel resolves that link outside the sandbox. A FIFO can instead block the daemon. The sandbox has done its job, but the privileged output reader crosses the boundary unsafely.

Impact: crafted audio that exploits ffmpeg/Whisper can turn the transcript channel into host-file disclosure (including secrets later admitted/journaled) or a daemon hang.

Required correction: open the output relative to a held WorkDir fd with no-follow/beneath/no-magic-link semantics, require one regular file owned by the daemon with link count one, fstat the opened fd, bounded-read from that same fd, and reject every non-regular output. Add symlink-to-host-file, FIFO, hard-link, over-cap growth, and missing-file detectors; the host canary must never enter the transcript or journal.

### 5. HIGH — Replay can handle text different from the canonical admitted transcript

The plan calls the first successful transcript the canonical durable input (`docs/PLAN-VOICE.md:34-41`) but accepts re-transcription before every replay and claims only “which text wins is racy” (`:96-102`). The current channel core stores the winning text in `chan_inbox` (`internal/channel/channel.go:225-240`), yet `AdmitOutcome` returns only `MessageID` and `Replayed` (`:70-75`), and `InboundStatus` returns only status (`:634-648`). On a replay, the adapter checks status and then calls the handler with the newly supplied in-memory `in`, not the stored row (`internal/channel/telegram/telegram.go:249-273`).

Concrete window: transcript A is admitted; the daemon dies before the first handler event; redelivery produces transcript B; `Admit` returns `Replayed`; the handler runs B while the journal/history permanently says A. The deterministic turn ID prevents a second admission, but it does not recover the canonical input in this window.

Impact: provider behavior and effects can be caused by text that durable replay/audit says was never the input.

Required correction: make duplicate admission return/load the complete persisted normalized `Inbound` and always call the handler with those durable bytes, or persist one transcription result keyed by update identity before admission. Add a crash-window detector that forces A on the first transcription and B on replay and proves the handler/provider receives A only.

### 6. HIGH — The dedicated downloader still does not enforce the declared Telegram egress boundary

`docs/PLAN-VOICE.md:42-50` validates the URL's scheme/exact host and rejects redirects, which closes one hop but not the network peer. The current Telegram client uses a default transport (`internal/channel/telegram/telegram.go:87-91`); a new `http.Client` with only `CheckRedirect` still honors proxy environment variables and performs unpinned DNS resolution. `ARCHITECTURE-ESSENTIALS.md` E11 requires proxy-env sanitization plus resolver/dialer enforcement, not host-string filtering. This matters more on the file path because the request URL contains the bot token and the response is private audio.

Impact: ambient proxy configuration or DNS rebinding can send the credential-bearing request to a peer other than the declared Telegram endpoint despite the pre-`Do` host check.

Required correction: give the download client an explicit transport that ignores proxy environment state and uses the project egress dialer/pinned-IP policy (or name the temporary accepted ceiling and prevent claiming exact-peer enforcement). Add proxy-canary and re-resolve/rebind detectors in addition to the redirect detector.

### 7. MEDIUM — The exact media argv uses relative paths although WorkDir is only a mount grant

The pipeline names `in.ogg`, `out.wav`, and `out` (`docs/PLAN-VOICE.md:7-10`), but `sandbox.Spec.WorkDir` does not set process cwd. `probe.Prepare` binds the host directory at `/work` without adding `--chdir /work`, then launches the target (`internal/preflight/probe/probe.go:605-625,660-665`). Against the named interface, the exact argv therefore does not reliably address the staged files.

Impact: ffmpeg/Whisper can fail to find inputs or write outputs even after the read-only model grant lands.

Required correction: use explicit in-namespace paths (`/work/in.ogg`, `/work/out.wav`, `/work/out`) in structured argv, or deliberately add and attest a cwd field. The explicit paths are the smaller change.

## Notes, simplification pass, and proof ceiling

- NOTE: “the owner's private voice never leaves the Pi” (`docs/PLAN-VOICE.md:4-5`) is literally false because Telegram transports and stores the original message; the intended and supportable claim is that transcription is local and audio is not sent to an additional transcription provider.
- NOTE: requiring the input file to be owned specifically by the daemon uid (`docs/PLAN-SANDBOX-ROINPUT.md:44-45`) excludes a root-owned, non-writable packaged model. That is a product/install constraint, not a security blocker, but it should be intentional.
- Topknot: separating the reusable read-only-input primitive from the voice adapter is justified. No generalized media framework or async queue is needed here. Lean already.
- Strongest counterargument: the files and model are locally configured and the binaries are pinned, so ordinary non-malicious runs may work after straightforward implementation choices. That does not answer the hostile-input contract: sandboxing exists specifically because the decoder may be compromised, and the replay inconsistency occurs under an ordinary crash with no attacker.
- Weakest link: the egress finding applies the locked E11 boundary although current Telegram transport has not yet implemented the full S6.3 dialer. If that deficit is explicitly accepted for all Telegram traffic, finding 6 becomes a named ceiling; findings 1-5 remain independently blocking.
- Proof ceiling: static review proves design/interface contradictions, not Pi kernel/bwrap behavior, actual RLIMIT delivery, ffmpeg/Whisper flag compatibility, transcription quality, or detector RED capability.
- Skill update: none; the prior review identified the surface gaps, while this round exposed new prerequisite/replay details rather than a reusable audit-process miss.

VERDICT: FAIL
