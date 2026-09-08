# Adversarial code review — Telegram E11 dialer

## Scope and evidence binding

Reviewed commit `a5645387ee9d913368598bdc323cc20c5b621d33`
(tree `6f31d57c6285e47e97a46adfba616c4ab36fa56d`) on
`slice/p1-tg-dialer`, against `main` at merge base
`2030c01987693393b076e681c9f28dbcbf734eec`. I did not checkout or switch
branches. Production and test evidence came from a clean on-disk `git archive`
export of the target commit.

Reviewed owners:

- `docs/PLAN-TG-EGRESS-DIALER.md` — blob
  `bcac0e5bd36782e9af4a60fdf8202eda1e8dd1a0`
- `docs/ARCHITECTURE-ESSENTIALS.md` E11 — blob
  `99831f08765501a751330f9725bea3b026672617`

## Substantive findings

### F1 — HIGH — Receipt persistence fails open and can be suppressed forever

`internal/channel/telegram/dialer.go:57-60,121-123` invokes a receipt callback
before dialing, but the callback cannot return an error. In the allowed path,
`internal/channel/telegram/telegram.go:119-128` updates `lastPinned` before
calling `Core.RecordEgress` and discards the append error. The concrete failure
sequence is:

1. The resolver permits pin A.
2. `egressReceipt` stores A in `lastPinned`.
3. `RecordEgress` fails (closed journal, disk/I/O failure, or append failure).
4. The error is discarded and `DialContext` connects anyway.
5. Every later connection to A is coalesced, so no later append even attempts
   to repair the missing evidence.

Refusal append errors are discarded too (`telegram.go:114-117`), contradicting
the claim that every refusal is recorded. This violates E11's required egress
receipt and E4's durable audit ownership; it is not merely reduced observability.

**Required fix:** make the receipt callback return `error`; make `emit` and
`DialContext` propagate it and refuse the network dial; if coalescing remains,
update `lastPinned` only after a successful durable append. Add an injected
append-failure detector proving zero dial and a later successful retry records
the pin.

### F2 — HIGH — The configured deny-default `egress_allow` policy is not enforced

The approved plan requires both the configured Telegram host and
`egress_allow` to admit the destination (`PLAN-TG-EGRESS-DIALER.md:20-25`). The
implementation checks only equality with `apiHost`
(`internal/channel/telegram/dialer.go:93-101`). Telegram's local `Config` has no
allowlist field (`telegram.go:42-47`), and both composition-root constructions
omit `resolved.Config.EgressAllow` (`cmd/nexus/main.go:158-163,844-847`).

Therefore an empty allowlist—or one containing only the provider host—still
permits Telegram egress. This is a concrete default-deny bypass relative to the
approved policy and E11's “config must not widen the kernel floor” rule.

**Required fix:** pass the sealed allowlist (or a precomputed exact admission
decision) into the Telegram constructor and reject construction/dialing unless
the normalized API authority is explicitly admitted. Add empty, wrong-host,
and exact-host tests through `New`, not only a directly constructed dialer.

### F3 — MEDIUM — Coalescing does not satisfy the approved `EgressAttempt` contract

The deviation is not defensible under the current owner. The plan requires a
typed receipt before each connection (`PLAN-TG-EGRESS-DIALER.md:52-55`), while
`telegram.go:109-128` records only the first/changed pin. `DialContext` is called
for actual TCP connection creation, not for every two-second HTTP poll; normal
HTTP keep-alive already avoids one receipt per poll. Coalescing instead hides
same-IP reconnects after connection loss, so the journal cannot establish that
a particular physical egress attempt was policy-checked.

The mutex prevents a Go map race, but it does not restore the missing durable
attempts. Concurrent pin changes can also append out of cache-update order
because the lock is released before the journal writes.

**Required fix:** journal every `DialContext` decision as the approved design
states. If the intended artifact is only a first/change state record, amend the
higher-ranked owner and rename the event to an honest state-transition concept;
it must not be called `EgressAttempt` evidence.

### F4 — MEDIUM — The durable receipt drops the normalized resolution set

The dialer computes and carries `egressDecision.Resolved`
(`internal/channel/telegram/dialer.go:22-28,107-122`), but the adapter discards
it and `egressPayload` persists only host, pin, allowed, and reason
(`internal/channel/telegram/telegram.go:114-128`;
`internal/channel/channel.go:194-210`). The validator requires only a non-empty
host (`channel.go:175-184`), so it also accepts “allowed” without a pin and
“refused” without a reason.

This is not the plan's typed
`EgressAttempt{host,resolvedSet,pinnedIP,decision,ts}`. In particular, the
journal cannot audit the whole-set decision or prove that a permitted pin came
from the classified set. The journal envelope can own the timestamp, but it
does not reconstruct the omitted addresses.

**Required fix:** persist the canonical normalized resolved set and validate
the allowed/refused field invariants at the event boundary.

### F5 — MEDIUM — “Metadata IP in every encoding” is not fully enforced

The named plain IPv4 and IPv4-mapped-IPv6 forms are correctly closed:
`Unmap()` runs before classification, and link-local rejection catches
`169.254.169.254` (`internal/channel/telegram/dialer.go:63-85,107-120`). RFC1918,
CGNAT (`100.64.0.0/10` with correct inclusive second-octet bounds), ULA,
loopback, unspecified, multicast, and link-local classes are also implemented
correctly.

However, `netip.Addr.Unmap` strips only the IPv4-mapped `::ffff:0:0/96` form.
A standardized NAT64 representation such as
`64:ff9b::a9fe:a9fe` remains an ordinary IPv6 address and passes every predicate
in `permitted`; on a NAT64 network it translates to `169.254.169.254`. That is a
reachable counterexample to locked E11's stronger “metadata-IP blocked in every
encoding” requirement. Other special-use ranges such as documentation and
benchmarking networks also pass despite the implementation comment claiming
to reject every non-public class (`dialer.go:71-85`).

**Required fix:** define and test the complete production address policy for
IPv4-embedded transition/translation prefixes and other non-public special-use
ranges. At minimum, include the well-known NAT64 metadata encoding as a
contract-anchored regression.

## Verified controls and test assessment

- **TLS is correct in production code.** `http.Transport.DialContext` receives
  the original authority and returns a raw TCP connection to the pinned IP
  (`dialer.go:121-155`). The Go 1.26 `net/http` transport then constructs TLS
  itself and sets `tls.Config.ServerName` from the original target hostname;
  pinning does not downgrade SNI or certificate hostname verification.
- **Proxy and redirect controls are correctly wired.** `Proxy:nil` ignores
  ambient proxy variables, and `CheckRedirect` always returns an error
  (`dialer.go:153-168`).
- **Mixed-set behavior is fail-closed.** Every normalized result is checked
  before `norm[0]` is selected; no dial occurs when any member fails
  (`dialer.go:107-123`). Loopback override mode requires every result to be
  loopback (`dialer.go:63-70`).
- The existing tests are discovered and non-vacuous for public pinning,
  plain/mapped metadata rejection, basic production classes, loopback mixed
  sets, host mismatch, and direct proxy/redirect configuration
  (`dialer_test.go:37-143`). The retargeted Telegram suite exercises the real
  pinned client against loopback `httptest` endpoints.
- Required design detectors are nevertheless absent: there is no TLS/SNI and
  wrong-certificate test, no sequential resolver/rebind test, no durable
  receipt/coalescing/error test, and no `egress_allow` test. The mode test calls
  `egressModeForBase` directly rather than proving the comment's claimed
  resolver acceptance through `newPinnedClient` (`dialer_test.go:135-143`).
  The supplied statement that RED was observed by ablation is not preserved as
  independently reviewable evidence in this commit.

## Commands and observed results

- `go test -count=1 ./internal/channel/telegram ./internal/channel` — PASS.
- `CGO_ENABLED=0 go test -count=1 ./...` — PASS for all packages.
- `go vet ./internal/channel/telegram ./internal/channel` — PASS.
- `go test -race -count=1 ./internal/channel/telegram` — environment ERROR,
  `ThreadSanitizer: unsupported VMA range` on this ARM host; this is not
  evidence of a code race. Static inspection confirms `lastPinned` map access
  is mutex-protected, while the durable ordering flaw in F3 remains.

Topknot simplification pass: the dedicated dialer is cohesive and uses only the
standard library; no independent bloat finding. The avoidable concept is the
coalescing state itself—deleting it restores the approved per-connect receipt
and removes its failure/order modes.

Skill update: none.

VERDICT: FAIL
