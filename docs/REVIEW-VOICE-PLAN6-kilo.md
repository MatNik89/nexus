# Review of voice v6 + sandbox RO-input v3 + TG egress dialer (`732650e`)

**Scope**: `docs/PLAN-VOICE.md` (v6), `docs/PLAN-SANDBOX-ROINPUT.md` (v3),
`docs/PLAN-TG-EGRESS-DIALER.md`, commit `732650e` on `slice/p1-voice`.
**Reviewer**: kilo
**Date**: 2026-09-08

## Verdict

PASS. Both round-5 residuals are closed, verified against the actual code.
No substantive correctness / security / behavioral-equivalence / unbuildable
defect remains.

## Round-5 residuals — closure status

### F1 — dialer resolved-IP policy — closed

Each required element is present and grounded:

- **Unmap normalization** (`PLAN-TG-EGRESS-DIALER.md:26-28`): every answer is
  `netip.Addr.Unmap()`ed so an IPv4-mapped-IPv6 form of a forbidden address
  classifies like its plain v4 form (`::ffff:169.254.169.254` → `169.254.169.254`).
  `net/netip` is stdlib and the module targets Go 1.26 — buildable.
- **Kernel deny floor** (`:29-34`): loopback / unspecified / multicast /
  interface-local / link-local (`169.254.0.0/16`, `fe80::/10` — covering cloud
  metadata `169.254.169.254` in every representation) / private / CGNAT
  (`100.64/10`) / ULA (`fc00::/7`). Covers the SSRF-relevant set.
- **MIXED-SET rule, fail-closed** (`:35-38`): any deny-floor answer refuses the
  whole resolution; no pick-the-good-one. Correct anti-rebind posture — for a
  public-only host a mixed set is poisoning, so refusal is right, not
  over-rejection.
- **Retry re-classifies** (`:41-42`): re-resolve + re-apply the floor per retry;
  the connect target is always freshly classified.
- **Typed `EgressAttempt` receipt via the journal** (`:43-46`): the single
  canonical writer (E4/B7), token never in the receipt — the E11-required
  egress evidence.

Premise verified: `EgressAllow []string` is host strings only
(`internal/foundation/config/config.go:39`), and `ValidateBounds` only rejects
wildcards (`config.go:322-329`) — it cannot classify an address, so defining the
IP policy in the dialer (not config) is correct.

### F2 — registry rejects duplicate normalized Dest — closed

`PLAN-SANDBOX-ROINPUT.md:66-71` rejects two `InputRef`s with colliding
normalized `Dest` at Compile, with the correct rationale (two ro-binds to one
path would overmount while `policyHash`/attestation name both identities), a
backend-independent fail-closed (not relying on a bwrap version), a canonical
total sort key `(ID, normalizedDest)`, and a DEST-DUPLICATE detector
(`:99-100`).

## Confirmed still closed (no regression)

- `chan_inbox` replay determinism (`AdmitOutcome` returns canonical text,
  `channel.go:70-75,225-241`).
- Startup `InputRegistry`, identity-pinning (dev,ino + startup digest) as
  tamper-evidence, no per-call fd leak.
- `-nt` restored; absolute `/work` + `/inputs` paths; held-WorkDir-fd
  `openat2`/RESOLVE_BENEATH + `fstat` regular/`nlink==1` output read.
- `ffmpeg -t` WAV bound; aggregate quota deferred to S6.2 P2.2
  (`HARNESS-SPEC.md:1481-1484`); RLIMIT_FSIZE not claimed.
- Download `io.LimitReader` cap+1, scheme/host pre-validation; failure path =
  C2 refusal shape, zero inbox rows, no TERMINAL assertion.

## Notes (not FAIL reasons)

1. **Stale header**: `PLAN-VOICE.md:33` reads "Change (v5 — round-3+4 findings
   folded)" while the commit is v6 (round-5 residuals). Cosmetic; the voice
   plan body is otherwise unchanged because the residuals landed in the two
   prerequisite docs.
2. **Deny floor is not the full IANA special-purpose registry** (TEST-NET-1/2/3,
   benchmark `198.18/15`, documentation ranges are unlisted). Irrelevant for a
   public host + host-allowlist, and the SSRF-relevant set is complete —
   defense-in-depth only.
3. **`EgressAttempt` receipt is a journal append per connect** — a new closed
   event type and a durable write before every dial. Acceptable for a
   single-user channel; the receipt is attempt-evidence (host/resolvedSet/
   pinnedIP/decision), which matches E11's data-flow receipt, not a
   delivery-contract row.

VERDICT: PASS
