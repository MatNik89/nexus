# REVIEW-PHASE0 — code review of slice/p0-phase0 (T01–T03)

Contracts checked: tasks-P0 T01-T03, CLAUDE/AGENTS hard rules, HARDQ B4/C5/D1/F1. Tests run:
`go test ./internal/preflight/...` — both packages pass (probe suite ran on the real host, bwrap 0.11.2).

---

## BREAK (fail-closed gaps)

**1. [BREAK] doctor verifies bwrap presence, not the sandbox capability — the F1 "kernel/ABI
floor" check is unimplemented.**
`doctor.go:26-36` `Detect()` runs only `bwrap --version`; `doctor.go:61-72` reports
`sandbox-backend` OK on that alone. `bwrap --version` succeeds even when unprivileged user
namespaces are disabled (`kernel.unprivileged_userns_clone=0`, AppArmor-restricted userns, some
hardened distros) — in that case bwrap cannot create ANY sandbox at exec time. T03's own text
lists "kernel/ABI floor" as a check, and HARDQ F1 lists it separately from bwrap; neither is
probed. Concrete failure: a userns-disabled host prints `prerequisites-ready` with `exec` ON,
then T25 fails at first sandboxed launch. Fix: probe the real capability, e.g.
`bwrap --unshare-net -- /bin/true` (or `--unshare-user`), and turn `exec` OFF when it fails.

## RED (vacuity / under-testing)

**2. [RED] PROC_TREE column is materially under-tested — and the missing negative control is not
cosmetic.**
`TestKillReapsWholeTree` (`probe_linux_test.go:181-208`) does NOT test what T02 claims:
- It kills via `Setpgid` + `syscall.Kill(-pgid, SIGKILL)` (line 194,199) — that is the S1.2
  runtime group-kill mechanism, so it reaps the tree even if `--unshare-pid --die-with-parent`
  were dropped. The sandbox's own orphan-prevention property (`--die-with-parent`, the thing that
  stops a sandboxed process surviving a daemon crash) is never exercised.
- Its verification is weaker than it claims: `pgrep -f filepath.Base(hp)` (line 204) matches
  "probehelper", but the spawned child is launched via `/proc/self/exe` (`probehelper.go:52`), so
  its cmdline is `/proc/self/exe sleep 30` — it does NOT contain "probehelper". A surviving child
  would be invisible to the check.
- `LoosenPIDNS` (`probe.go:50`) is never used: no PROC negative control exists, so the column
  passes GREEN with the pid/die-with-parent flags absent entirely.
Material because PRD §6 item 6's boundary and B4/D1 list PROC_TREE as a capability column. Fix:
(a) negative control asserting a loosened (no-`--unshare-pid`/`--die-with-parent`) launch leaves a
surviving child the suite can detect; (b) a direct die-with-parent test (kill only the bwrap
parent, not the group) with a check that sees the child's actual cmdline.

**3. [RED] The `/etc/shadow` deny test cannot distinguish EACCES from ENOENT.**
`TestShadowNotReadableInsideSandbox` (lines 68-79) passes on `err != nil` and never asserts that
`/etc/shadow` exists host-side or that the inside failure is a *permission* denial rather than
*missing file*. On a host without `/etc/shadow` the "cannot read /etc/shadow" check passes
vacuously. The canary negative control proves the deny *mechanism*, not the specific PRD file.
(Net mitigation: `TestWorkdirWritable` is the de-facto "the sandbox actually works" canary, so the
suite as a whole is not vacuous — but the shadow check itself is.) Fix: assert `/etc/shadow`
exists + is host-readable first, and assert the probehelper error is EACCES, not ENOENT.

## SEC (closure)

**4. [SEC] `/etc/resolv.conf` is an extra `/etc` FILE grant beyond B4.**
`probe.go:97` grants `/etc/ld.so.cache`, CA certs, AND `/etc/resolv.conf`. HARDQ B4 lists only
`/etc/ld.so.cache` + CA certs as file grants ("never recursive /etc"). Under `--unshare-net` DNS
is unreachable, so resolv.conf is both unnecessary and leaks resolver configuration (which can
reveal internal DNS topology). Drop it.

## QUALITY

**5. [QUALITY] Two different argv-rewrite hacks; the kill-tree test rolls its own fragile
positional one.**
`runBound` (lines 83-105) rewrites the target by string-matching `hostHelper`; `TestKillReapsWholeTree`
instead does `argv[len(argv)-3] = "/work/..."` (line 192) — a positional index that silently
mis-fires if `spec.Args` length changes. Duplicated, fragile logic; route the kill-tree test
through `runBound`. (Both are currently correct for their exact arg counts.)

**6. [QUALITY] `static-check.sh` fails OPEN when `ldd` is absent, and the T01 RED (negative) is not
automated.**
`static-check.sh:7` — if `ldd` is not on PATH (musl/minimal container), the `if` is false and the
script prints "OK", i.e. a dynamic binary would pass. There is also no committed test that builds a
deliberately cgo-tainted binary and asserts the script exits 1 (T01's RED is manual-only). Fix:
detect `ldd` absence as a hard error, and add a CI negative case.

---

## OK (verified sound)

- `--clearenv` + `--setenv PATH` (no env/secret leakage), `--new-session`, minimal `--dev`, fresh
  `--proc`, `/usr` RO-bind, and the `/lib,/lib64` symlink merge are correct bwrap usage.
- NET_DENY negative control (`LoosenNet`) and FS_RO canary negative control are hazard-safe
  (test-owned sink/dir, never real `/etc` or uncontrolled egress) and genuinely non-vacuous.
- Availability fail-closed (`TestDetectFailClosedWhenBwrapAbsent`), workdir writability,
  dynamic-ELF run, shebang reject, undeclared-child-exec reject all present and meaningful.
- doctor: no secret leakage (`Detail: "set"`, never the key/token); capability-scoped OFF; no
  degraded-but-on path; strict exit code 1; RED table breaks each of the five prerequisites.
- Constitution conformance: no `map[string]any`, `%w` error wrapping, `t.TempDir()` fixtures,
  `CGO_ENABLED=0` enforced in Makefile and wired into `make check`.

---

## Verdict

The FS_RO/NET_DENY/availability boundaries are well-proven and hazard-safe. But two material
defects remain: the doctor reports `exec` ready without verifying the actual sandbox capability
(fail-closed violation of F1's "kernel/ABI floor" check), and the PROC_TREE column — a named PRD
§6 item 6 boundary — is not actually proven (group-kill not die-with-parent, a pgrep check that
misses the child, and no negative control). These are exactly the false-GREEN class the go/no-go
gate exists to prevent.

VERDICT: FAIL
