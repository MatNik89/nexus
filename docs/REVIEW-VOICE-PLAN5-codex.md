# Adversarial design review: voice v5, input registry v3, and Telegram E11 dialer

## Binding and scope

- Revision: `1f2b4990a613dac2f9c869c0a22a448b671bf306` on `slice/p1-voice`; tree `4da7dfdd7587020bfe1aa0dd5aa0212bfd9686b5`.
- Reviewed blobs: `PLAN-VOICE.md` `f782e4508b5ed83cbafa2b4e8dad9731e0e59e29`; `PLAN-SANDBOX-ROINPUT.md` `e2994d3aed60d668c8f5c22a75eb6d8ba3b0238e`; `PLAN-TG-EGRESS-DIALER.md` `221e883b29a3786126574ece1dca8103f0d9e2d9`.
- SHA-256: voice `05cd514033cbdfbd4788286bde2d4ebfa6c6b44850bb119bb261a47d25174a59`; input registry `f289b2501be7594f2ee667a492c185aaf7e4e572f08e3cc8508c41828bd9a0fc`; Telegram dialer `a7e615ad3973f2c04577bd4984d041c2381b52bb93fef3c57c0cf66ddff724b8`.
- Inspected from clean on-disk export `/home/matej/HARNESS/nexus-voice-1f2b499.tYniXS`; no checkout/switch and no production test.

## Substantive findings

### F1 — HIGH — The Telegram prerequisite still does not define the IP policy required to enforce E11

`PLAN-TG-EGRESS-DIALER.md:23-29` validates the configured hostname against `egress_allow`, then says an IP “outside the admitted policy” is refused. No such resolved-IP policy is defined. The actual `config.EgressAllow` is only a list of host strings (`internal/foundation/config/config.go:29-44`), and its bounds check only rejects wildcard host entries (`internal/foundation/config/config.go:322-329`). It cannot answer whether a resolved address is permitted.

This leaves the decisive E11 checks unproducible. E11 requires every resolved address to be checked/pinned, multi-A behavior to be safe, metadata IPs to be rejected in every representation, and an egress receipt (`docs/ARCHITECTURE-ESSENTIALS.md:146-158`). The plan specifies neither the normalized-IP/kernel-floor rules nor a receipt type/owner. Its `REBIND-REFUSED` detector (`PLAN-TG-EGRESS-DIALER.md:43-45`) presupposes an “off-policy IP” but never defines how the implementation or fixture classifies one. A mixed answer set containing a normal public IP plus `169.254.169.254` is therefore unconstrained: “pick one” can pass while the forbidden candidate remains accepted for another connection. The plan also claims the adapter matches E11 while declaring the general S6.3 mechanism out of scope (`PLAN-TG-EGRESS-DIALER.md:52-54`); adapter-local implementation is acceptable, but omitting mandatory parts of E11 is not.

Required correction: define the adapter-local typed resolver/dialer policy explicitly—normalize resolved addresses before classification, enforce the E11 metadata/kernel deny floor on every candidate, define mixed/multi-A selection and re-resolution semantics, and emit the required typed egress receipt. Add a detector whose single DNS answer set contains both an admitted address and an encoded/mapped metadata address, plus a receipt assertion. No general broker is required for this slice.

### F2 — MED — Input destinations are normalized but not unique, permitting an attested shadowed mount

`PLAN-SANDBOX-ROINPUT.md:62-66` rejects traversal and collisions with built-in mounts, but never rejects two `InputRef` entries with the same normalized `Dest`. Launch then appends a read-only bind for each input (`PLAN-SANDBOX-ROINPUT.md:68-76`), matching the current ordered FD-to-bwrap construction (`internal/preflight/probe/probe.go:637-647`). Two grants can therefore target `/inputs/model.bin`; an overmount makes only one visible while `policyHash` and attestation name both identities (`PLAN-SANDBOX-ROINPUT.md:52-56`). Even if a particular bwrap version rejects the duplicate rather than overmounting, the sealed policy's behavior is left backend-dependent rather than fail-closed at Compile.

Required correction: reject duplicate normalized destinations before policy construction and add a RED case with two registered IDs mapped to the same destination. The canonical sort also needs a total key, but rejecting duplicate destinations removes the security-relevant ambiguity.

## Round-4 fold verification

- **F1 replay: CLOSED.** `chan_inbox.text` is already written transactionally from `EvInboundAdmitted` (`internal/channel/channel.go:185-240`). Extending `AdmitOutcome` from its current two fields (`internal/channel/channel.go:70-75`) to return the existing row's canonical text on both fast-path and raced-duplicate replay is buildable. Updating the adapter before its existing terminal check/handler call (`internal/channel/telegram/telegram.go:249-273`) makes first-admitted A win over a re-transcribed B without a parallel store. Re-running Whisper is wasteful but behaviorally harmless under this contract.
- **F2 registry lifetime: CLOSED.** The plan correctly recognizes that WorkDir is per-call and part of `policyHash` (`internal/sandbox/sandbox.go:196-235`) and moves opened model FDs into one startup-owned registry (`PLAN-SANDBOX-ROINPUT.md:11-39`). Per-call Specs carry only IDs/destinations, so the current compile-per-workdir pattern (`internal/exectool/exectool.go:116-139`) no longer opens or retains a new model FD per call. `ExtraFiles` inheritance can pass the registry-owned descriptor to each child without transferring ownership of the registry's parent descriptor to the per-process `probe.Handle` (`internal/preflight/probe/probe.go:503-584,637-647`).
- **F3 identity attestation: CLOSED AS A DEFENSIBLE CEILING.** The registry now explicitly promises inode identity, not immutable execution bytes, and labels the startup SHA-256 as tamper evidence only (`PLAN-SANDBOX-ROINPUT.md:41-60`). The held FD defeats pathname replacement; `--ro-bind-fd`/`/proc/self/fd` is compatible with the current `ExtraFiles` discipline (`internal/preflight/probe/probe.go:637-647`). Same-inode mutation remains real, but for the declared single-owner trusted-model scope it is now an honest ceiling rather than a false attestation claim.
- **F4 channel-wide E11 prerequisite: PARTIALLY CLOSED.** The new plan correctly replaces the waiver with a channel-wide prerequisite, uses one explicit `Proxy:nil` transport, preserves hostname TLS verification while dialing a literal resolved IP, and rejects redirects (`PLAN-TG-EGRESS-DIALER.md:17-36`). The actual Telegram client is indeed currently only a timeout-bearing default client (`internal/channel/telegram/telegram.go:68-91`). F1 above is the remaining blocker to calling the replacement E11-compliant.
- **Resource quota: DEFENSIBLE STAGED CEILING.** The voice plan limits `ffmpeg -t` to honest-path WAV growth and does not claim RLIMIT enforcement (`PLAN-VOICE.md:73-82`). Aggregate compromised-child `disk_write_bytes` and `file_count` enforcement belongs to P2.2 (`docs/HARNESS-SPEC.md:1481-1488`), so deferring it without re-invention is consistent with the owner contract.
- **Other requested corrections: CLOSED.** Absolute `/work` and `/inputs` paths plus `-nt` are explicit (`PLAN-VOICE.md:9-20`); output uses the held-WorkDir-FD/openat2/fstat bounded channel (`PLAN-VOICE.md:58-65`); failed transcription follows the durable stable-refusal path with no inbox row (`PLAN-VOICE.md:39-43,103-106`); download uses cap+1 at 20 MiB (`PLAN-VOICE.md:66-72`).

## Notes and proof ceiling

- **NOTE:** `PLAN-VOICE.md:33` still labels the change “v4”; this is editorial only.
- **NOTE:** The registry should expose an explicit shutdown `Close` for tests and orderly daemon teardown, although OS process exit already bounds the promised daemon-lifetime descriptors; this is not a per-call leak.
- **Lean review:** the startup registry and one channel-wide client are the smallest coherent owners; no dependency is added. The missing IP-policy contract should be added to the dialer owner rather than as another layer.

Weakest link: the dialer claims a policy-aware E11 boundary while the only concrete configured policy is hostname-only. Static review does not prove bwrap, openat2, ffmpeg, Whisper, DNS, TLS, or Telegram runtime behavior on the Pi.

Skill update: none.

VERDICT: FAIL
