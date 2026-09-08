# Adversarial design review: voice v4 + sandbox read-only input v2

## Review binding and scope

- Revision: `831eb26b139513f64251094b674a7cc90b95ffe5` on `slice/p1-voice`; tree `d68a1fc6b918c3a9f07566a950c361c76d09a7b0`.
- Reviewed jointly: `docs/PLAN-VOICE.md` blob `68156b5d3348d92023a294d295a1338be00aea90` and `docs/PLAN-SANDBOX-ROINPUT.md` blob `52b7696abba81e2728eb2191e008236535df84c3`.
- Review used an on-disk clean export at `/home/matej/HARNESS/nexus-voice-831eb26.F19KGu`; no checkout or branch switch was performed.
- Static design review only. No implementation or production test was run.

## Substantive findings

### F1 — HIGH — The replay transcript has no durable owner or implementable channel contract

`PLAN-VOICE.md:40-46` requires a transcript to be persisted before admission, loaded on redelivery, and supplied as the exact bytes both journaled and handled. The plan does not name the canonical event, projection/table, transaction boundary, profile key, lookup API, or cleanup/retention rule that owns this new pre-admission state.

That omission conflicts with the actual persistence contract. `ARCHITECTURE-ESSENTIALS.md:50-64` makes `EventJournal.Append` the only canonical writer and makes transcripts/state projections of journal events; HARDQ B7 permits one serialized append actor and names the admissible transactional recipes (`HARDQ-CONSOLIDATED.md:105-116`). The current channel surface cannot provide the promised behavior:

- `internal/channel/channel.go:70-75` returns only `{MessageID, Replayed}` from admission.
- `internal/channel/channel.go:225-240` persists text only while applying `EvInboundAdmitted` into `chan_inbox`.
- `internal/channel/channel.go:361-399` admits the caller-supplied text and, on replay, returns no canonical text.
- `internal/channel/channel.go:634-648` exposes only the inbound status.
- `internal/channel/telegram/telegram.go:249-273` therefore handles the adapter's current in-memory `Inbound`, including on a non-terminal replay.

Consequently, implementing the prose either requires an unowned parallel durable writer (violating E4/B7), or cannot load the promised transcript through the named interfaces. It also leaves crash/concurrent-redelivery arbitration unspecified: two transcriptions can still produce A and B without a defined journal event/unique projection deciding which durable value wins.

Required design correction: either (a) define a profile-journal-owned `VoiceTranscribed`-style event plus a projection keyed by the full `(adapter_id, channel_identity, update_id)` identity, with a Core lookup/claim API and explicit crash/concurrency semantics, or (b) keep `chan_inbox` as the only durable owner, extend replay admission/lookup to return its canonical text, and relax the non-essential promise that Whisper is never rerun. In both variants, the handler must receive the winning durable text, not its locally computed candidate.

### F2 — HIGH — `CompiledPolicy` cannot own the proposed daemon-lifetime descriptors without leaking them

`PLAN-SANDBOX-ROINPUT.md:35-41` says every `Compile` opens each grant and holds that descriptor for the daemon lifetime. The existing `CompiledPolicy` is a copyable value with no close/release method (`internal/sandbox/sandbox.go:52-64`), and the `Backend` interface likewise has no compiled-policy lifecycle operation (`internal/sandbox/sandbox.go:112-118`). `Process.Close` releases only the `probe.Handle` resources created for a launch (`internal/sandbox/sandbox.go:66-100`; `internal/preflight/probe/probe.go:566-584`).

This is not merely an implementation detail. Voice creates a fresh WorkDir per call (`PLAN-VOICE.md:78-81`), while WorkDir is part of both `Spec` and `policyHash` (`internal/sandbox/sandbox.go:203-235`), so one compiled Whisper policy cannot simply be reused across calls. The current execution pattern also compiles per disposable WorkDir (`internal/exectool/exectool.go:116-139`). Following the proposed contract therefore leaks at least one model FD per transcription until `EMFILE`/resource exhaustion.

Required design correction: give immutable input artifacts an explicit reusable owner/lifecycle independent of the per-call policy, or make `CompiledPolicy` an owned closable handle with precisely defined transfer/duplication and all-error-path cleanup. A reusable sealed artifact can also address F3 without rereading 140 MiB on every call.

### F3 — HIGH — The grant is pathname-swap resistant, but it is not digest-pinned execution

The held FD does defeat replacement of `HostPath`, and `PLAN-SANDBOX-ROINPUT.md:84-87` proposes a valid detector for that narrower property. However, the file bound by `--ro-bind /proc/self/fd/<N>` is still a live mutable inode. The plan explicitly admits a same-inode rewrite at lines 73-77. Its launch check at lines 60-67 compares only size/inode identity, so a same-size overwrite is invisible and can also occur while the child reads. The policy and attestation would continue naming the compile-time digest (`PLAN-SANDBOX-ROINPUT.md:53-58`) even though the process consumed different bytes.

That contradicts the slice's repeated “digest-pinned”/“held bytes” contract (`PLAN-SANDBOX-ROINPUT.md:9-12,51-69`) and is weaker than the existing closure discipline, which reads exact bytes and seals them into memfds (`internal/preflight/probe/probe.go:152-183,276-310`) before `--ro-bind-data` (`internal/preflight/probe/probe.go:642-647`). The ceiling is not defensible as written: Compile permits a daemon-owned, owner-writable file, and a same-UID process or in-place model updater can invalidate the attested content without changing pathname, inode, or size. Calling the model “trusted” does not make an attestation over bytes that were not enforced accurate.

Required design correction: bind an immutable measured object (for example, a sealed memfd populated once and reused/duplicated per launch), or narrow the public contract and attestation to identity-pinned trusted input and stop asserting content/digest-pinned execution. If exact-byte attestation remains required, an owner-policy assumption alone is insufficient.

### F4 — HIGH — The DNS-pinning omission is a live E11 violation, not a design ceiling authorized by an owner

`PLAN-VOICE.md:55-67` correctly adds a dedicated no-proxy, reject-all-redirect client, but expressly launches the new file download without the S6.3 pinned-IP dialer. `ARCHITECTURE-ESSENTIALS.md:146-158` is a locked invariant: egress must use a policy-aware resolver/dialer that pins every resolved IP before connect. The actual Telegram adapter confirms that today's client has only a timeout and no such boundary (`internal/channel/telegram/telegram.go:87-91`).

The fact that existing Telegram calls share the defect establishes provenance, not a waiver. This voice slice adds another attacker-influenced remote fetch and depends on behavior forbidden by E11; neither reviewed plan cites a higher-ranked owner that defers or amends E11 for Telegram. Therefore the named “pre-existing Telegram-wide ceiling” is not defensible under the repository's owner precedence. Land the shared S6.3 dialer first, or obtain an explicit amendment in the owning architecture contract. The proxy and redirect changes remain valid defense in depth.

## Verified round-3 corrections and non-blocking ceilings

- **Verified corrected:** absolute `/work` and `/inputs` paths and `-nt` are explicit (`PLAN-VOICE.md:9-20`); the current probe indeed does not chdir (`internal/preflight/probe/probe.go:605-625`).
- **Verified corrected:** output is separated from the bounded combined stdout/stderr channel and is specified as a held-WorkDir-FD, no-follow, bounded regular-file read (`PLAN-VOICE.md:47-54`). This is implementable as a relative `openat2` of `out.txt` with `RESOLVE_BENEATH` plus the stated FD checks.
- **Verified corrected:** failure uses the existing stable refusal/outbox path and creates no inbox row (`PLAN-VOICE.md:35-39,98-101`; `internal/channel/telegram/telegram.go:209-216,242-247`).
- **Verified corrected:** destination normalization/collision rejection and inclusion of the sorted grant set in policy and attestation identity are now specified (`PLAN-SANDBOX-ROINPUT.md:46-58`). They require additive changes because current `Spec`, `policyHash`, `probe.Spec`, and `Attestation` do not yet carry grants (`internal/sandbox/sandbox.go:33-59,227-235,301-304,352-375`).
- **Defensible staged ceiling, not a blocker:** aggregate compromised-child disk/file-count enforcement belongs to the existing P2.2 `ResourceBudget` owner (`docs/HARNESS-SPEC.md:1481-1488`). `PLAN-VOICE.md:68-77` now limits its `ffmpeg -t` statement to the honest path and explicitly avoids claiming a nonexistent RLIMIT. Until P2.2 lands, this slice must not claim availability containment against a compromised decoder.
- **NOTE:** this host's bwrap exposes `--ro-bind-fd FD DEST`, which expresses the held-FD bind directly. Prefer it over the `/proc/self/fd/<N>` magic-symlink spelling unless a published deployment-version floor requires the latter. This does not cure F3; both forms bind the mutable inode.

## Weakest link and proof ceiling

The weakest link is F1: replay determinism is a persistent-state guarantee, but the plan adds no persistent owner or callable contract. The review is static and proves design/interface contradictions only; it does not establish runtime bwrap, ffmpeg, Whisper, openat2, or Telegram behavior on the deployment Pi.

Skill update: none.

VERDICT: FAIL
