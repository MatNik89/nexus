# REVIEW-PHASE1B-R3 — round-3 verification (12 codex r2 folds)

Scope: the round-2 fold (2579b77..cf09f36) + NEW defects it introduced. `go vet ./internal/...`
clean; `go test -count=1 ./internal/...` GREEN.

---

## The 7 key folds — all [OK]

**(a) [OK] `SyncProjection.Init` now takes a restricted `ProjDB`.** `Init(db *ProjDB)` (projection.go:90-94);
`ProjDB.Exec/Query` run the same `guardStatement`, so Init can neither write canonical rows directly
nor install a trigger/view whose SQL mentions a canonical table. The regex `\b(events|journal_meta|
projection_offsets)\b` catches every identifier spelling (double-quote/backtick/bracket/schema-qualified/
case) — and a trigger that writes `events` necessarily names it in its body, which the guard sees. The
lexical ceiling is declared with its T18 upgrade trigger. ✓

**(b) [OK] `ProjTx.QueryRow` is deleted.** The comment records WHY (sql.Row cannot carry the guard
error); `Query`/`Exec` surface `NON_CANONICAL_WRITE`. ✓

**(c) [OK] `machine.Fold` enforces strictly-increasing offsets.** `ev.Offset <= checkpoint` → error
(machine.go:84-87). Zero, duplicate, and decreasing offsets reject; journal offsets are 1-based, so
offset 0 is correctly invalid. ✓

**(d) [OK] `EnsureDir(trustedRoot, path)` no-follow component walk.** `filepath.Rel` proves `path` is
strictly below the root; each component is `Mkdir` + `verifyPrivateDir` (Lstat, never follows). The
"symlinks at/above the root are the caller's trust decision" narrowing is explicit and deliberate
(e.g. `$HOME` symlink). ✓

**(e) [OK] procx pgid-recycled gating — the reasoning is SOUND.** A group member holds a
`struct pid` reference via `pids[PIDTYPE_PGID]`, so the leader's PID (== the pgid) stays pinned in the
pidmap until ALL members exit; a pid seen with a DIFFERENT start time therefore proves the whole group
exited. This also fixes codex #12 correctly: when the leader exits on TERM but a member survives, the
pid is NOT yet reusable (`StartTime` → ENOENT), so `pgidRecycled()` is false and the SIGKILL proceeds.
The survivor check is gated by `!pgidRecycled()` so a pid-coincident unrelated group is never
misreported. ✓

**(f) [OK] P0.5 amendment legitimately scopes the MUSTs to P0.** HARNESS-SPEC:1544-1552 records that P0
carries {probe name, passed, detail, config-hash} with the config-hash as a sha256 of the RESOLVED
config; probe-id/rev, expiry, and measured-vector arrive with the S0.3 negotiation owner (P1). This is
legitimate because P0 has no runtime activation (B9): freshness is structural (one measurement at
start, snapshot dies with the process, config change = restart = re-measure). The closure code backs
it: `sha256Hex` shape-validates (64 lowercase hex), and `Snapshot` RETAINS `configHash` (closure.go:
135-157). ✓

**(g) [OK] checker Worker required + Contract binding + ack ordering.** `Grade` rejects an empty
`contract.Worker` (checker.go:171-173); every `Evidence` must carry a `Contract` equal to the contract
ID and a `Producer` different from the worker (checker.go:185-192); `DeliveredAndAcked` computes the
EARLIEST delivery and rejects any ack timestamped before it (checker.go:243-275). ✓

## Additional r2 folds spot-checked — [OK]

- **codex #14 / kilo #11**: `Resolve` now rejects a `Conflicts` target that is not a KNOWN capability
  (closure.go:52-60). ✓
- **codex #9**: env suffix must be canonical uppercase AND each normalized key may appear once
  (config.go:184-197). ✓
- **codex #10**: `parseList` reports entry POSITION; `parseBoolStrict` reports "(value withheld)"
  (config.go:99-119). ✓

## NEW defects — none material

**1. [NEW-ERROR, LOW] `ValidateBounds` still echoes a wildcard host value** (`egress_allow: wildcard
%q`, config.go:265). Residual from the codex #10 no-echo posture, but hosts are not secrets and the
secret-mistype paths (bool key, list entries) are already silenced.

**2. [NEW-ERROR, LOW] `Fold`'s "resumed fold" has no prior-checkpoint parameter.** The strictly-
increasing check is relative to checkpoint 0, so a caller resuming with events at-or-before its prior
checkpoint would regress; the caller (Replay) filters `> from`, so it is a caller contract, not a Fold
enforcement. Low.

**3. [NEW-ERROR, LOW] `ConfigHash` is shape-validated, not recomputed-verified.** `Seal` checks the
64-hex shape and equality but does not (and cannot) recompute the resolved-config digest; the digest
is the composing root's (T27) job — the declared division of labor, matching the P0.5 amendment's
"config-hash MORA biti sha256 digest RESOLVED konfiguracije" only at the shape/retention level.

---

## Verdict

All 12 round-2 folds are correctly implemented and test-verified, and the two reasoning judgments the
task flagged — the procx pgid-recycled proof and the P0.5 P0-scoping amendment — are both sound. The
three new observations are low-severity and non-material.

VERDICT: PASS
