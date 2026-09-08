# Review of TG pinned-IP dialer implementation r3 (`4ee4926`)

**Scope**: commit `4ee4926` on `slice/p1-tg-dialer` — `dialer.go`, `telegram.go`,
`channel.go`, tests, against `PLAN-TG-EGRESS-DIALER.md` + E11
(`ARCHITECTURE-ESSENTIALS.md:146-158`).
**Reviewer**: kilo
**Date**: 2026-09-08

## Verdict

PASS. The round-3 fold is closed and derived from the invariant, not patched
prefix-by-prefix. No substantive security / correctness / E11-compliance /
behavioral-equivalence / unbuildable residual.

## Round-3 fold — closure status

### RFC6145 `::ffff:0:0:0/96` bypass — closed, derived not patched

- `v4EmbedPrefixes` (`dialer.go:94-98`) is the complete set of standardized
  IPv6 forms carrying an IPv4 in the **low 32 bits**: `64:ff9b::/96` (NAT64
  well-known), `::ffff:0:0:0/96` (RFC6145 IPv4-translated), `::/96`
  (deprecated IPv4-compatible); IPv4-mapped `::ffff:0:0/96` is handled by
  `Unmap()` before this (`:121`). No `Is4In6` overlap, no gap.
- `embeddedV4` (`:103-114`) iterates the table, extracts bytes 12-15, and
  excludes `::`/`::1` (the unspecified/loopback predicates own them).
- `TestDialerRejectsForbiddenV4EveryEncoding` (`dialer_test.go:314-351`) runs
  {metadata, loopback, rfc1918, cgnat} × {plain v4, IPv4-mapped, NAT64,
  RFC6145} = 16 cases, each asserted refused with no dial — non-vacuous,
  invariant-anchored, and RED against removing the RFC6145 entry.

## Still-closed prior folds (re-confirmed by inspection)

- F1 receipt-return-error + permitted dial fail-closed (`dialer.go:186-188`).
- F2 `errEgressPreWire` sentinel + `isPreWire` `errors.Is` recognition.
- F3 every decision journaled (no `lastPinned`).
- F4 `egressPayload.Resolved` + validator `Allowed⇒Pinned`/`!Allowed⇒Reason`.
- F5 `forbiddenSpecialUse` table (doc/bench/CGNAT/RFC8215) + `embeddedV4`.
- TLS via `http.Transport` (raw `net.Conn`, hostname SNI/chain); `Proxy:nil`;
  reject-all `CheckRedirect`; deny-default `egress_allow` threaded via
  `main.go:163,848`.

## Notes (not FAIL reasons — theoretical, no reachable resolver answer)

1. **6to4 `2002::/16`** embeds the v4 in bits 16-47 (not low-32), so
   `embeddedV4` does not classify it. Deprecated (RFC 7526), relays retired —
   not a reachable resolver answer.
2. **Teredo `2001:0::/32`** embeds an XOR-obfuscated v4 in the low 32 bits;
   deprecated and not returned by a public resolver.
3. **Network-specific NAT64 prefixes** (RFC 6052 local `/96` NSP) are
   operator-local, not standardized; only the well-known `64:ff9b::/96` is
   detectable, which is the one a poisoned public answer would carry.
4. **`0.0.0.0/8` non-zero** (e.g. `0.0.0.2`) still passes `IsUnspecified`
   (only `0.0.0.0` is "unspecified"); non-routable "this network" space.
5. **"malformed dial address"** (`dialer.go:155`) does not wrap
   `errEgressPreWire`; unreachable — the `http.Transport` always passes a
   well-formed `host:port`.

An `IsGlobalUnicast()` allowlist would collapse notes 1-4 in one move, but the
current denylist is complete for every SSRF-relevant, reachable class.

VERDICT: PASS
