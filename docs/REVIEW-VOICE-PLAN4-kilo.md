# Review of voice v4 + sandbox RO-input v2 (`831eb26`)

**Scope**: `docs/PLAN-VOICE.md` (v4) + `docs/PLAN-SANDBOX-ROINPUT.md` (v2), commit
`831eb26` on `slice/p1-voice`.
**Reviewer**: kilo
**Date**: 2026-09-08

## Verdict

PASS. All four round-3 findings are genuinely fixed, and I verified each
against the actual code/interfaces. No substantive correctness / security /
behavioral-equivalence / unbuildable defect remains.

## Round-3 findings — closure status

1. **RLIMIT_FSIZE (was unbuildable) → fixed, correctly deferred.** The claim is
   removed (`PLAN-VOICE.md:68-77`); the WAV bound is now `ffmpeg -t <maxSeconds>`
   (bytes ≤ 32 KiB/s × maxSeconds on the honest path — a valid upper bound since
   s16le mono 16 kHz is 32000 B/s) and the aggregate `disk_write_bytes` /
   `file_count` bound is named as the existing S6.2 P2.2 `ResourceBudget`
   obligation. Verified: `docs/HARNESS-SPEC.md:1481-1484` defines exactly those
   two fields. No sandbox feature is claimed that does not exist.
2. **whisper `-nt` dropped (was correctness) → fixed.** `-nt` is restored
   (`PLAN-VOICE.md:15`), with the timestamp rationale documented (`:17-20`).
3. **Relative paths (was correctness trap) → fixed.** Absolute in-sandbox paths
   `/work/in.ogg`, `/work/out.wav`, `/work/out`, `/inputs/model.bin`
   (`PLAN-VOICE.md:14-15`) — no reliance on a `--chdir` that `probe.Prepare`
   does not issue (`probe.go:609` `--dir /work`, no `--chdir`).
4. **TOCTOU claim overstated (was security-integrity) → fixed.** The grant is now
   held-fd binding with a SWAP-DEFEATED detector and a named residual
   (`PLAN-SANDBOX-ROINPUT.md:60-77`); details below.

## Verification against the actual code

- **Held-fd model bind.** `probe.Prepare` already passes pinned files through
  `ExtraFiles` and binds them (`probe.go:642-646`). The grant reuses that
  held-fd shape: open `O_NOFOLLOW`, `fstat`, digest from the same fd
  (`PLAN-SANDBOX-ROINPUT.md:36-51`), bind via `--ro-bind /proc/self/fd/<N>`
  (`:64-67`). This is buildable against the named interface.
- **Swap-defeat is detector-backed, not asserted blind.** The SWAP-DEFEATED
  detector (`:84-87`) replaces the pathname between Compile and Launch and
  asserts the original bytes still bind; a `realpath`-style path re-resolve in
  bwrap would RED it. The residual — same-uid truncate+overwrite of the held
  inode — is explicitly named as an accepted ceiling for a ~140 MB model on an
  SD-card Pi (`:73-77`), not hidden.
- **Policy/attestation identity.** The grant set enters both `policyHash`
  (`sandbox.go:227-235`) and the launch `Attestation` (`sandbox.go:352-375`);
  POLICY-IDENTITY detector locks it. Correct against the actual pin discipline.
- **Download hardening.** The existing telegram `http.Client{Timeout: 65s}`
  (`telegram.go:90`) sets no transport policy (Go default follows redirects and
  `ProxyFromEnvironment`), so NOT reusing it is right; the dedicated client's
  `Proxy: nil` + reject-all `CheckRedirect` is correct (a nil `Transport.Proxy`
  field means "no proxy", per the stdlib contract). E11 (`ARCHITECTURE-
  ESSENTIALS.md:146-157`) confirms pinned-IP dialing is an S6.3 obligation; the
  plan names it as the pre-existing Telegram-wide ceiling (`PLAN-VOICE.md:63-67`),
  consistent with `internal/llm/provider/provider.go:65-67` ("owned by the S6.3
  dialer when …").
- **Failure path.** The corrected detector (`PLAN-VOICE.md:98-101`) asserts the
  refusal DELIVERY row, zero inbox rows, offset-after-durable-enqueue — matching
  the C2 refusal shape in the channel core, no TERMINAL assertion.

## Notes (not FAIL reasons)

1. **RO-input flag mislabel in the "Why".** `PLAN-SANDBOX-ROINPUT.md:9-12` says
   "the SAME sealed-fd discipline the closure already uses (`--ro-bind-data
   <fd>`, `probe.go:645`)" but the Launch correctly uses `--ro-bind
   /proc/self/fd/<N>` (`:65`), not `--ro-bind-data`. The closure's `--ro-bind-data`
   requires a memfd (a regular-file model is not one); the Launch's flag is the
   correct one. Same held-fd spirit, different flag — wording only.
2. **`openat2`/`RESOLVE_BENEATH`** needs `x/sys/unix.Openat2` or a raw syscall
   (not in stdlib); for a hardcoded single-segment `out.txt` the load-bearing
   checks are `O_NOFOLLOW` + `fstat`(regular, `nlink==1`) — the plan's stronger
   mechanism is fine but under-specified as to dependency.
3. **Replay-deterministic transcript store is mechanism-unspecified.**
   "Persisted keyed by the update identity before admission" (`PLAN-VOICE.md:40-46`)
   does not say *where* (journal event vs. side table); idempotency-by-update-id
   and persist-failure fail-closed should be pinned when implemented.
4. **`32 KB/s` is a rounding.** Actual s16le mono 16 kHz is 32000 B/s; the plan
   states `32768 × maxSeconds`, which is a conservative over-bound — correct
   direction, trivial imprecision.

VERDICT: PASS
