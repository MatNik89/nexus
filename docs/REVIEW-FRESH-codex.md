# Fresh independent QA audit — HEAD cd5975d

## Scope and method

I audited the complete immutable commit `cd5975df6badb937ecbb50802afe7e125c9f55c1` (tree `4f918cbb5a2f573bafa2a95bbce2972ea5e94382`) from the clean `git archive` export `/tmp/nexus-audit-cd5975d.ZCSZ0N`. I did not read any `docs/REVIEW-*.md` file. Focused negative probes and controlled ablations were made only in separate exports under `/tmp`; the repository was not mutated during the audit except for this requested report.

Baseline evidence:

- `CGO_ENABLED=0 /home/matej/.local/go/bin/go test -count=1 ./...` ran the whole tree. The prioritized packages (`cmd/nexus`, `internal/acceptance`, `internal/app/daemon`, `internal/approval`, all `internal/kernel/...`, `internal/preflight/probe`, `internal/sandbox`, and `internal/exectool`) passed. The overall command failed because all three `internal/buildcheck` tests require `.git`, and one `internal/foundation/procx` precondition failed transiently. The latter passed on a subsequent `-count=20` run and is not promoted to a finding.
- `/home/matej/.local/go/bin/go vet ./...` passed.
- The race suite could not produce evidence on this host: every race binary aborted before tests with `ThreadSanitizer: unsupported VMA range` (`Found 47 - Supported 48`). Findings below therefore do not rely on race-detector output.

## Findings

### 1. HIGH — an effect can commit, post-effect durability can fail, and approval resume still reports success

Evidence:

- `cmd/nexus/main.go:336-343` executes the approved effect after its durable approval has been consumed.
- `cmd/nexus/main.go:357-366` attempts continuation and `MarkResumeCompleted`, but merely logs a completion-mark failure; when continuation also fails it deliberately returns the tool result with a nil error.
- `internal/approval/approval.go:629-637` shows that `MarkResumeCompleted` is itself the durable event which closes the consumed challenge.
- `internal/approval/approval.go:640-666` classifies a consumed challenge without that event as unfinished and exposes it to the fresh-approval retry path.

RED-capable probe actually run in `/tmp/nexus-probes-cd5975d.nXCUuj`:

```text
/home/matej/.local/go/bin/go test -count=1 -run '^TestFreshAuditPostEffectDurabilityFailureIsNotReportedAsSuccess$' -v ./cmd/nexus
nexus: resume-completed mark ch-...: journal is closed
fresh_audit_test.go:58: post-effect journal failure was reported as success: result="done"
--- FAIL: TestFreshAuditPostEffectDurabilityFailureIsNotReportedAsSuccess
```

The probe used the real durable approval store and effect path, counted exactly one committed in-process effect, then closed the journal at the post-effect boundary. `resumeApproved` returned `"done", nil`. This is an externally false success and leaves the challenge in the state later described as UNKNOWN/reconciling. A subsequent user retry can authorize the same irreversible intent again.

Required repair: after an effect has committed, failure to durably record continuation/completion must be returned as an explicit UNKNOWN/RECONCILING outcome, never nil error or ordinary success. Add a regression at this exact post-effect failure seam.

### 2. HIGH — the signed P0 acceptance gate is insensitive to missing sandbox enforcement

Evidence:

- `internal/acceptance/acceptance_linux_test.go:617-645` makes both positive and hostile criterion-6 tests conditional on `requireBwrap`.
- `internal/acceptance/acceptance_linux_test.go:712-716` implements absence as `t.Skipf`, which still gives `go test` exit 0.
- `scripts/p0-accept.sh:23-37` treats that exit 0 as a complete pass and writes/signs `passed:true`; it does not reject skips.
- Even when bwrap is present, the hostile acceptance test at `internal/acceptance/acceptance_linux_test.go:640-686` checks a closure-external file and an unpromoted interpreter, but not the syscall floor. The actual syscall-floor detector is separate at `internal/preflight/probe/probe_linux_test.go:199-220`, while `scripts/p0-accept.sh:24` runs only `./internal/acceptance`.
- `internal/sandbox/sandbox.go:163-189` and `cmd/nexus/main.go:821-825` likewise call a narrower confinement canary and label it a passing sandbox; neither invokes `probe.FloorProbe`.

Two probes actually run:

```text
PATH=/home/matej/.local/go/bin NEXUS_ACCEPT_BIN=... /home/matej/.local/go/bin/go test -count=1 \
  -run '^TestCriterion6(SandboxedExec|HostileUnchangedUnderYolo)$' -v ./internal/acceptance
--- SKIP: TestCriterion6SandboxedExec
--- SKIP: TestCriterion6HostileUnchangedUnderYolo
PASS
```

In a fresh negative-mutation export, I removed only the production `--seccomp` attachment at `internal/preflight/probe/probe.go:649-659`. The real floor detector went RED:

```text
TestSyscallFloorDeniesPtrace: ptrace ALLOWED under the syscall floor: ptrace ok
--- FAIL: TestSyscallFloorDeniesPtrace
```

but both acceptance criterion-6 tests stayed GREEN against that same ablated binary:

```text
--- PASS: TestCriterion6SandboxedExec
--- PASS: TestCriterion6HostileUnchangedUnderYolo
PASS
```

Thus the detector that mints the signed P0 claim cannot go RED for at least one required sandbox boundary, and can also pass without executing either criterion-6 test at all.

Required repair: make bwrap absence a hard acceptance failure, make the attested command run the complete hostile sandbox conformance suite, and add reporting-chain ablations proving that removal of FS, network, syscall, process-tree, and rootfs enforcement prevents attestation creation.

### 3. HIGH — `doctor --p0` claims memory and reminders are LIVE without a running daemon, scheduler, heartbeat, or folded projections

Evidence:

- `cmd/nexus/main.go:737-790` says projections are folded but calls `journal.Open` without any `SyncProjection` arguments and then marks memory LIVE merely because both journal files open.
- `cmd/nexus/main.go:794-799` treats a missing or unreadable `scheduler_health` file the same as a healthy empty file and marks reminders LIVE based only on `profilesOK`.
- The actual daemon heartbeat and scheduler are created only in the serving path at `cmd/nexus/main.go:200-235`; `doctor --p0` does not check that heartbeat or require the scheduler health file to exist.
- The acceptance test at `internal/acceptance/acceptance_linux_test.go:940-966` explicitly expects a P0-capable grant after constructing a test world and signed attestation, without starting a daemon or scheduler.

Probe actually run:

```text
/home/matej/.local/go/bin/go test -count=1 -run '^TestDoctorP0GrantLive$' -v ./internal/acceptance
--- PASS: TestDoctorP0GrantLive
PASS
```

This passing test is self-anchored to the defective readiness rule: its fixture has no live daemon/scheduler heartbeat, yet the command grants “all six ... measured live.” The result can authorize a phase decision on capabilities that were not exercised and may not currently be running.

Required repair: distinguish readiness from liveness; for the current LIVE wording, require a fresh daemon heartbeat, an existing fresh scheduler-health record, and real projection initialization/catch-up checks. Add negative tests for absent/stale heartbeat, absent health, and corrupt/unfoldable projections.

### 4. MEDIUM — a failed acceptance invocation corrupts the installed trust anchor before grading starts

Evidence:

- `scripts/p0-accept.sh:13-18` overwrites `$CONF_DIR/allowed_signers` directly before validating that `$KEY.pub` exists, before building, and before running any test.
- The write is neither atomic nor rollback-protected. Because the failing `cat` is inside command substitution, `/bin/sh` continued and wrote an invalid `owner ` record despite `set -e`.

Probe actually run in the temporary export with only the cleanup trap neutralized for audit safety:

```text
XDG_CONFIG_HOME=/tmp/.../audit-config NEXUS_RELEASE_KEY=/tmp/.../no-such-key sh scripts/p0-accept.sh
exit=127
allowed_signers_bytes=7
cat: .../no-such-key.pub: No such file or directory
scripts/p0-accept.sh: 20: go: not found
```

The fixture began with `sentinel-existing-trust-anchor`; after the failed invocation it contained the seven-byte invalid `owner\n` record. Any later build/test/sign failure has the same pre-grading mutation problem and can disable verification for the previously installed binary.

Required repair: validate all key inputs first, stage the candidate signer file in the private work directory, grade and sign successfully, then install the trust anchor and attestation atomically as one final publication step. Preserve the previous working set on every failure.

### 5. LOW — build-check tests cannot run from a clean source export

Evidence:

- `internal/buildcheck/staticcheck_test.go:14-20` resolves the repository root solely with `git rev-parse --show-toplevel`.
- All three tests call that helper before reaching their actual static/dynamic/non-ELF assertions at `internal/buildcheck/staticcheck_test.go:37-74`.

Probe actually run from the immutable `git archive` export:

```text
/home/matej/.local/go/bin/go test -count=1 -v ./internal/buildcheck
TestStaticCheckPassesStaticNexusBinary: repo root: exit status 128
TestStaticCheckFailsDynamicBinary: repo root: exit status 128
TestStaticCheckRefusesNonELF: repo root: exit status 128
FAIL
```

The detector fails because VCS metadata is absent, not because any claimed binary property is wrong. This makes clean exported-source verification red for an unrelated reason and prevents the three intended oracles from running.

Required repair: derive the package/repository path from `runtime.Caller`, an embedded/test-time path, or another source-tree-relative mechanism; do not make these behavioral checks depend on `.git`.

## Conclusion and proof ceiling

The strongest counterevidence is that the focused package suites and the full `internal/acceptance` suite passed on the unmodified host with bwrap installed. That does not overcome the causal probes above: the attestation detector stayed green under a removed syscall floor, the approval API returned success after a post-effect durability failure, and the P0 doctor test positively requires a false LIVE result.

The audit did not prove absence of additional data races because the host cannot start Go race binaries, and it did not execute the state-mutating real `p0-accept.sh` against the user's live configuration or signing key. Those are proof ceilings, not reasons to downgrade the reproduced findings.

VERDICT: FAIL
