# Adversarial design review: voice v6, input registry v3, and Telegram E11 dialer

## Binding and scope

- Revision: `732650e52aface3c1d4e00df01a6a01857f3a1c5` on `slice/p1-voice`; tree `44158dcf0abc9cc8da84505857eb4d98baa5489d`.
- Reviewed blobs: `PLAN-VOICE.md` `ad19fc2e4d33f2ec937882fb92cb8e7181336f28`; `PLAN-SANDBOX-ROINPUT.md` `161e4b2e589dc041bbda51a1853a9da846f52860`; `PLAN-TG-EGRESS-DIALER.md` `ab86bbd8e01cafef4591aeef91f760b1dfbaf0ab`.
- SHA-256: voice `e4b67f541dfbd49d6bda2e323a76e3a93119d36e94f86021a534223c5cb6dd27`; input registry `0030870be68304022bd5fee5b58a8a9e8e1c4874e7d0d701d25ee9e73dc4bbde`; Telegram dialer `22913c4a3306bc065e03b1c79a485b8af5f4c423e61ef87fd171a49d30fe388c`.
- Inspected from clean on-disk export `/home/matej/HARNESS/nexus-voice-732650e.8Jh2b7`; no checkout/switch and no production test.

## Substantive finding

### F1 — HIGH — The new IP deny floor disables the supported loopback Telegram endpoint

`PLAN-TG-EGRESS-DIALER.md:29-38` rejects every loopback answer regardless of `egress_allow`. That is correct for the production `api.telegram.org` hostname, but it contradicts the actual configuration contract: `TelegramAPIBase` explicitly supports a loopback override for a local Bot API server and the acceptance harness (`internal/foundation/config/config.go:29-39,330-340`). The default remains production Telegram, but validated values include `http(s)://127.0.0.1:<port>`, `[::1]`, and `localhost`.

This is reachable behavior, not a test-only cosmetic issue. The Telegram adapter receives that configured base (`internal/channel/telegram/telegram.go:41-46,68-90`), and its existing hermetic construction path uses an `httptest.Server` loopback URL (`internal/channel/telegram/telegram_test.go:142-163`); daemon call sites likewise pass the resolved `TelegramAPIBase`. Under the proposed unconditional floor, every such connection is refused before dial, so the prerequisite cannot replace the current client without breaking the supported local-Bot-API mode and channel testability.

Required correction: make the address policy mode-sensitive while preserving fail-closed sets. For the exact production endpoint, require every normalized answer to pass the public-address deny floor now specified. For an API base already validated by `config.ValidateBounds` as an explicit loopback override, require every normalized answer to be loopback and reject any mixed/non-loopback set. Add IPv4, IPv6, `localhost`, and mixed loopback/public detector cases. This does not weaken production E11 enforcement and requires no general S6.3 broker.

## Round-5 residual verification

- **Resolved-IP policy: PARTIALLY CLOSED.** `PLAN-TG-EGRESS-DIALER.md:21-46` now defines `netip.Addr.Unmap`, a concrete deny floor, fail-closed mixed-set behavior, per-retry re-resolution/reclassification, and a typed journal-owned `EgressAttempt`. The mixed public/metadata and IPv4-mapped metadata detectors at lines 60-69 are causal and close the previously undefined “off-policy IP” gap. F1 is the specific remaining regression.
- **Duplicate destinations: CLOSED.** `PLAN-SANDBOX-ROINPUT.md:62-71` now rejects duplicate normalized destinations at Compile and defines a total `(ID, normalizedDest)` sort key. The `DEST-DUPLICATE` detector at lines 97-100 is red-capable against the ordered bwrap FD-bind construction currently used at `internal/preflight/probe/probe.go:637-647`.

## Prior findings and ceilings

- **Replay determinism: CLOSED.** Existing `chan_inbox.text` is synchronously derived from `EvInboundAdmitted` (`internal/channel/channel.go:185-240`). The additive `AdmitOutcome` canonical-text return is buildable from the current contract (`internal/channel/channel.go:70-75,361-399`), and replacing the adapter candidate before its existing replay/handler path (`internal/channel/telegram/telegram.go:249-273`) makes admitted A win over re-transcribed B without a parallel store.
- **InputRegistry lifetime: CLOSED.** One startup-owned held FD plus per-call ID references avoids the leak caused by per-call policies whose WorkDir participates in `policyHash` (`PLAN-SANDBOX-ROINPUT.md:11-39`; `internal/sandbox/sandbox.go:192-235`). `ExtraFiles` is the existing viable held-FD path (`internal/preflight/probe/probe.go:637-647`).
- **Input identity: DEFENSIBLE CEILING.** The plan now attests `(dev,ino)` identity and labels the startup digest as tamper evidence, not immutable execution bytes (`PLAN-SANDBOX-ROINPUT.md:41-60`). Same-inode mutation remains possible, but is honestly scoped to the single-owner trusted-model assumption rather than misrepresented as content pinning.
- **Aggregate disk quota: DEFENSIBLE STAGED CEILING.** `ffmpeg -t` bounds honest-path decoded PCM growth, RLIMIT is not claimed, and compromised-child aggregate disk/file enforcement remains with P2.2 (`PLAN-VOICE.md:73-82`; `docs/HARNESS-SPEC.md:1481-1488`).
- **Other requested corrections: CLOSED.** The plans retain sandbox-only structured execution and tree timeout, absolute `/work` and `/inputs` paths, `-nt`, held-WorkDir-FD/openat2 output validation, cap+1 download limiting, and the durable no-inbox refusal path (`PLAN-VOICE.md:9-20,33-109`).

## Notes and proof ceiling

- **NOTE:** the dispatch calls the voice document v6, while its internal heading at `PLAN-VOICE.md:33` says v5. This is editorial only.
- **NOTE:** a receipt-append failure occurs before any socket connect; implementation should preserve that fact through the existing `isPreWire` classifier (`internal/channel/telegram/telegram.go:114-158`) rather than conservatively relabeling it as a wire-ambiguous send. This is an implementation detail, not a blocker in the current design review.
- **Lean review:** the startup registry and one channel-wide standard-library transport remain the smallest coherent owners; no new dependency or parallel persistence mechanism is proposed.

Weakest link: the dialer's public-host policy was generalized across a deliberately supported local-only endpoint mode. Static review does not establish runtime bwrap, openat2, ffmpeg, Whisper, DNS, TLS, or Telegram behavior on the Pi.

Skill update: none.

VERDICT: FAIL
