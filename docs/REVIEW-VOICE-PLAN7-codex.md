# Adversarial design review — voice plan v7

## Scope and revision binding

Reviewed jointly at immutable commit `6fca0bf6796854ab68df166679fdda511dc12044`
(tree `f64a79e037ddec0c2535ccb46d745b03084e17bd`) on
`slice/p1-voice`, without checkout or branch switching:

- `docs/PLAN-VOICE.md` — blob `ad19fc2e4d33f2ec937882fb92cb8e7181336f28`
- `docs/PLAN-SANDBOX-ROINPUT.md` — blob `161e4b2e589dc041bbda51a1853a9da846f52860`
- `docs/PLAN-TG-EGRESS-DIALER.md` — blob `bcac0e5bd36782e9af4a60fdf8202eda1e8dd1a0`

The review used a clean on-disk `git archive` export and traced the plans to the
named code and owner documents. This is a static design review; no implementation
or runtime conformance is claimed.

## Result

No substantive security, correctness, behavioral-equivalence, or buildability
defect remains in the three-plan composition.

### Round-6 residual: CLOSED

The mode-sensitive Telegram address policy is now defensible against the actual
configuration boundary:

- `config.Default()` supplies the exact production base
  `https://api.telegram.org` (`internal/foundation/config/config.go:118-126`).
- `ValidateBounds` accepts only that production value or an explicit HTTP(S)
  loopback IP/`localhost` override (`internal/foundation/config/config.go:330-340`).
- The dialer plan derives its mode from that already-validated base, applies the
  public deny floor only in production, requires every answer to be loopback in
  override mode, normalizes mapped addresses with `Unmap`, and rejects the entire
  set if any answer violates the active policy
  (`docs/PLAN-TG-EGRESS-DIALER.md:20-51`). Production therefore cannot enter the
  loopback mode, while the existing local Bot API and `httptest` usage remain
  producible.
- The new `MODE-LOOPBACK` detector covers IPv4, IPv6, `localhost`, mixed sets, and
  production-versus-loopback separation
  (`docs/PLAN-TG-EGRESS-DIALER.md:81-85`).

This closes the sole round-6 finding rather than converting it into a waiver.

### Earlier folds: CLOSED or explicitly bounded

| Fold | Assessment grounded in the current code/design |
|---|---|
| Sandbox-only media execution | **CLOSED.** The voice plan requires both binaries to use `internal/sandbox`, structured argv, network denial, bounded wall time, and tree kill (`PLAN-VOICE.md:14-15,33-38,83-85`). This is additive to the current `Spec`/`Compile`/`Launch` boundary (`sandbox.go:33-40,192-235,266-349`), whose probe path builds bwrap arguments, unshares networking, inherits held descriptors, and places the child in a killable process group (`probe.go:605-668`). |
| Held model grant and path-swap resistance | **CLOSED.** `InputRegistry` opens once at startup, validates and retains the descriptor, while per-call `Spec` carries only an ID and normalized `/inputs/<name>` destination (`PLAN-SANDBOX-ROINPUT.md:11-39,62-82`). This reuses the existing held-descriptor/`ExtraFiles` construction represented by closure `--ro-bind-data` (`probe.go:637-647`). Input identity is included in policy and attestation, duplicate normalized destinations fail at `Compile`, and no launch reopens the host pathname (`PLAN-SANDBOX-ROINPUT.md:50-60,67-81`). |
| Registry descriptor lifetime | **CLOSED.** The registry owns one startup descriptor per configured input and launches reference it by ID; it does not attach a newly opened model descriptor to each copyable per-call `CompiledPolicy` (`PLAN-SANDBOX-ROINPUT.md:11-25,92-94`). |
| Input content claim | **DEFENSIBLE CEILING, not a live defect.** The plan claims identity pinning and startup-digest tamper evidence only, and explicitly disclaims content-execution pinning against a trusted same-UID in-place rewrite (`PLAN-SANDBOX-ROINPUT.md:41-60`). That is consistent with a held live inode rather than a sealed memfd. |
| Output and resource bounds | **CLOSED / DEFENSIBLE CEILING.** `-nt`, absolute `/work` and `/inputs` paths, transcript-file output, held-workdir-fd `openat2`/`fstat` validation, transcript cap, `ffmpeg -t`, and timeout/tree kill are all specified (`PLAN-VOICE.md:14-15,58-82`). Aggregate hostile-decoder `disk_write_bytes` and `file_count` enforcement is honestly deferred to the existing S6.2/P2.2 owner, which requires real enforcement or capability failure (`HARNESS-SPEC.md:1481-1488`); the voice plan does not claim an absent `RLIMIT_FSIZE`. |
| Admit/failure/replay equivalence | **CLOSED.** Current `chan_inbox.text` is durably populated by `EvInboundAdmitted` (`channel.go:178-240`), current replay detection is in `Core.Admit` (`channel.go:361-399`), and the Telegram adapter already branches on `AdmitOutcome.Replayed` before handling (`telegram.go:249-273`). The proposed additive canonical-text outcome makes the stored transcript win a race/replay without a parallel store. Transcription failure continues through stable `EnqueueReplyID`, produces no inbox row, and permits offset advancement only after that durable refusal (`PLAN-VOICE.md:39-57,97-106`; `telegram.go:209-247`). Harmless retranscription is explicitly accepted rather than hidden. |
| Bounded Telegram download | **CLOSED.** The voice plan validates scheme and exact host, reads at most cap+1 through `LimitReader`, rejects overflow, and reuses the channel-wide compliant client (`PLAN-VOICE.md:66-72`). The prerequisite replaces today's default client (`telegram.go:68-91`) with `Proxy:nil`, reject-all redirects, fresh resolution/classification on retry, pinned literal IP, original-name TLS verification, token sanitization, and a pre-connect typed journal receipt (`PLAN-TG-EGRESS-DIALER.md:17-62`). This directly satisfies E11's resolver/dialer, multi-address, metadata, redirect, proxy, containment, and receipt requirements (`ARCHITECTURE-ESSENTIALS.md:146-158`). |
| Round-5 address and destination residuals | **CLOSED.** Address normalization, deny floors, whole-set refusal, retry reclassification, and typed receipts are explicit (`PLAN-TG-EGRESS-DIALER.md:20-55,64-88`). Duplicate normalized input destinations fail closed at compile time, before bwrap can overmount one grant with another (`PLAN-SANDBOX-ROINPUT.md:62-71,97-100`). |

## Non-blocking notes and weakest points

1. The egress implementation must preserve the plan's ordering literally: a
   failed `EgressAttempt` append must abort before `net.Dial`. A focused detector
   that injects receipt-append failure would strengthen the listed
   `RECEIPT-EMITTED` test and ensure the error remains classified as pre-wire;
   this is an implementation-proof risk, not a contradiction in the design.
2. The largest declared security ceiling is the held model inode's same-UID
   in-place mutation. It is acceptable only while the documented single-owner,
   trusted-artifact premise holds; a multi-user or writable-model deployment is
   the measurable trigger for sealed content inputs.
3. The largest resource ceiling is aggregate disk/file-count enforcement for a
   compromised decoder. Voice must not ship on a platform where the later P2.2
   enforcement is required but unavailable; the current plan correctly scopes
   this as staged owner work rather than claiming protection now.
4. Editorial only: `PLAN-VOICE.md:33` still labels the change “v5” although this
   dispatch identifies the document as v7. It does not alter the designed
   behavior or verdict.

Skill update: none.

VERDICT: PASS
