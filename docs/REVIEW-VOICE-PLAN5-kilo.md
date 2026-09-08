# Review of voice v5 + sandbox RO-input v3 + TG egress dialer (`1f2b499`)

**Scope**: `docs/PLAN-VOICE.md` (v5), `docs/PLAN-SANDBOX-ROINPUT.md` (v3),
`docs/PLAN-TG-EGRESS-DIALER.md`, commit `1f2b499` on `slice/p1-voice`.
**Reviewer**: kilo
**Date**: 2026-09-08

## Verdict

PASS. Each round-4 fold is verified as a defensible design (or a named,
defensible ceiling) against the actual code. No substantive correctness /
security / behavioral-equivalence / unbuildable defect remains.

## Round-4 folds — closure status

### F1 — replay uses `chan_inbox` (no parallel store) — verified, correct

The plan drops the separate pre-admission transcript store and reuses the
journal's single-write-owner. Verified against code:
- `AdmitOutcome` is exactly `{MessageID, Replayed}`
  (`internal/channel/channel.go:70-75`).
- `chan_inbox` already persists the admitted `p.Text` on `EvInboundAdmitted`
  with a `UNIQUE(adapter, identity, update)` exactly-once backstop
  (`channel.go:225-241`).

The additive change (`AdmitOutcome` also returns the canonical stored text on
replay; the adapter hands the handler that text, `telegram.go:249-273`) is a
query of the existing projection — buildable, no new durable writer (E4/B7
respected). Relaxing "never re-run" to "whisper MAY re-run, wasteful but
harmless" is a defensible simplification: correctness now comes from the
first-admitted canonical text winning, not from suppressing re-transcription,
and the offset still advances after handling, so re-runs are bounded. Sound.

### F2 — startup `InputRegistry`, no per-call open — verified, correct

The RO-input v3 correctly diagnoses the fd leak: `CompiledPolicy` is compiled
per call and `WorkDir` is in `policyHash` (`sandbox.go:227-235`), so a
per-call open held "for the daemon lifetime" would leak to `EMFILE`. The
startup-owned `InputRegistry` (opened once, referenced by `InputRef.ID`) with
a NO-FD-LEAK detector is the right shape. Buildable against `Compile`/`Launch`.

### F3 — identity pinning, not content-execution — named, defensible ceiling

The plan now pins `{ID, normalizedDest, inode-identity (dev,ino),
startup-digest}` as TAMPER-EVIDENCE, and explicitly disclaims content-pinned
execution (`PLAN-SANDBOX-ROINPUT.md:41-60`). This is the honest framing of the
held-fd bind: it defeats a pathname SWAP (SWAP-DEFEATED detector), but a
same-uid in-place rewrite of the same inode is NOT closed — and it is named as
a single-owner trusted-artifact ceiling with a memfd-seal upgrade path for any
future multi-user deployment. Defensible, not a hidden weakening.

### F4 — E11 dialer as a channel-wide prerequisite — verified, correct

`PLAN-TG-EGRESS-DIALER.md` builds the compliant client once, channel-wide
(`Proxy: nil`, pinning `DialContext` with `ServerName` = host preserved,
reject-all `CheckRedirect`, host deny via `config.EgressAllow`), matching E11
(`ARCHITECTURE-ESSENTIALS.md:146-158`) and the S6.3 posture
(`provider.go:65-67`). Voice reuses it (prerequisite 2), so the download no
longer depends on E11-forbidden behavior and there is no waiver. Buildable
with stdlib net/tls only. TLS-SNI-PRESERVED and REBIND-REFUSED detectors lock
the two subtle properties (IP pinning must not drop hostname cert validation;
a late re-resolve must not escape the pinned set).

## Notes (not FAIL reasons)

1. **Stale section header**: `PLAN-VOICE.md:33` still reads "Change (v4 — round-3
   findings folded)" while the body is v5. Cosmetic.
2. **`--ro-bind-fd` availability** is asserted with a `/proc/self/fd/<N>`
   fallback and a GRANTED-READABLE detector; the fallback relies on the fd
   arriving via `ExtraFiles` (as the closure fds do, `probe.go:642-647`), which
   is correct — but the "this host's bwrap has it" claim is a runtime probe, not
   a code fact; the detector is what locks it.
3. **Identity pinning leaves a staleness property**: after a model file is
   replaced (new inode) post-startup, the held fd keeps serving the startup
   inode until restart. This is inherent to "startup pin" and is surfaced by the
   startup digest in `nexus doctor`; worth a one-line note that a model swap
   requires a restart to take effect.

## Verified clean

- `-nt` restored, absolute `/work` + `/inputs` paths (`PLAN-VOICE.md:14-15`).
- `out.txt` read via held-WorkDir-fd `openat2`/RESOLVE_BENEATH + `fstat`
  (regular, `nlink==1`); symlink/FIFO/hardlink rejected.
- `ffmpeg -t` WAV bound; aggregate quota deferred to S6.2 P2.2
  (`HARNESS-SPEC.md:1481-1484`); RLIMIT_FSIZE not claimed.
- Download bounded via `io.LimitReader` cap+1; token sanitization preserved.
- Failure path = C2 refusal shape, zero inbox rows, offset-after-durable-enqueue,
  no TERMINAL assertion.

VERDICT: PASS
