# Fresh audit QA verification round 2 — Codex

## Scope and artifact binding

- Reviewed branch: `slice/p0-fresh-audit`.
- Reviewed commit: `564909d8e7a559e20c85c756c12916006ffa683a` (parent `cd5975df6badb937ecbb50802afe7e125c9f55c1`).
- Baseline clean export: `/tmp/nexus-fresh-r2.488ZLj`, created with `git archive 564909d`; the full and focused GREEN runs completed before a test-only external doctor probe was added to that disposable export.
- Repository state was not changed during review except for this requested report. The pre-existing untracked `docs/REVIEW-FRESH-R2-agy.md` was not touched.
- Host evidence: uid `1000`, Go `go1.26.4 linux/arm64`, `/usr/bin/bwrap` `0.11.2`.

## Verdict summary

`564909d` closes Codex findings 1, 2, and 5 and Kilo F3 with causal RED-capable regressions. Codex finding 3 is only partially closed: the projections are now genuinely folded, but readiness-only criteria are still printed as `LIVE`, and there is no committed fresh/stale/absent-heartbeat detector. Codex finding 4 is also only partially closed: pre-grading state is preserved, but the final three-file publication is not atomic and can leave a mixed generation after a mid-publication failure.

Because unresolved MEDIUM and LOW findings remain, the required all-severity verdict is FAIL.

## Fold verification

### Codex #1 — post-effect durability failure: CLOSED

Production now returns a non-nil error after the approved effect executed but `MarkResumeCompleted` failed, explicitly says `EXECUTED`, and says not to retry (`cmd/nexus/main.go:357-369`). The regression exercises the real approval/effect path and closes the journal at the exact post-effect seam (`cmd/nexus/main_test.go:1573-1593`).

GREEN:

```text
CGO_ENABLED=0 /home/matej/.local/go/bin/go test -count=1 -run '^TestPostEffectDurabilityFailureIsNotSuccess$' -v ./cmd/nexus
--- PASS: TestPostEffectDurabilityFailureIsNotSuccess
PASS
```

Fix-only ablation: the new error return was replaced in a separate clean export with the former log-and-continue behavior while retaining the regression.

```text
nexus: resume-completed mark ch-...: journal is closed
main_test.go:1586: post-effect durability failure reported as success: "remembered (fact-tc-dl): delayed fact"
--- FAIL: TestPostEffectDurabilityFailureIsNotSuccess
```

The generic consumed-unfinished recovery path still offers a fresh, user-approved challenge (`cmd/nexus/main.go:401-413`). That is not an automatic retry and therefore does not falsify this fold, but the user remains the reconciliation verifier; the system cannot infer the external postcondition after restart.

### Codex #2 — signed sandbox gate: CLOSED for the reported class

The graded command now sets `NEXUS_ACCEPT_REQUIRE_SANDBOX=1` and runs all three relevant packages (`scripts/p0-accept.sh:32-37`). The acceptance helper, raw probe helper, and backend helper convert bwrap absence into `t.Fatalf` under that environment (`internal/acceptance/acceptance_linux_test.go:712-722`, `internal/preflight/probe/probe_linux_test.go:34-42`, `internal/sandbox/sandbox_linux_test.go:45-55`). The attestation suite string is now `internal/acceptance+probe+sandbox`, and doctor accepts only that exact value (`scripts/p0-accept.sh:44-47`, `cmd/nexus/main.go:881-890`).

GREEN on the real host:

```text
NEXUS_ACCEPT_REQUIRE_SANDBOX=1 CGO_ENABLED=0 /home/matej/.local/go/bin/go test -count=1 -timeout 1800s ./internal/acceptance ./internal/preflight/probe ./internal/sandbox
ok github.com/MatNik89/nexus/internal/acceptance 138.688s
ok github.com/MatNik89/nexus/internal/preflight/probe 79.192s
ok github.com/MatNik89/nexus/internal/sandbox 33.977s
```

Negative control at this uid with `PATH=/nonexistent` and the strict environment produced exit 1 independently in acceptance, probe, and sandbox, each with `bwrap REQUIRED for the graded acceptance run`.

Fix-only production ablation: removing only the production seccomp attachment while retaining the probe suite produced the expected RED:

```text
TestSyscallFloorDeniesPtrace: ptrace ALLOWED under the syscall floor: ptrace ok
--- FAIL: TestSyscallFloorDeniesPtrace
```

### Codex #3 — doctor projection/liveness honesty: PARTIAL, unresolved LOW finding

The projection half is closed: doctor passes the memory, schedule, obligation, channel, and approval projections to `journal.Open`, which initializes/catches them up or fails (`cmd/nexus/main.go:791-799`). Reminder logic now distinguishes non-empty scheduler health, stale heartbeat, fresh heartbeat, and absent heartbeat (`cmd/nexus/main.go:807-822`).

The output-state model is not closed. Every successful criterion is still unconditionally prefixed `LIVE` (`cmd/nexus/main.go:859-865`), including readiness-only memory, profiles, and absent-heartbeat reminders. The existing doctor acceptance test starts no daemon and creates no heartbeat, yet still requires `P0-capable` (`internal/acceptance/acceptance_linux_test.go:949-973`). No committed test mentions the new stale/fresh/absent heartbeat branches or asserts READY-versus-LIVE output.

An external regression probe added only to the clean export reproduced:

```text
LIVE memory         profile journals open, projections folded (READY — durable substrate, not a live round trip)
LIVE profiles       work and private journals independently open (READY)
LIVE reminders      durable scheduler substrate ready (READY — no daemon running yet)
P0-capable — live probes passed ... durable substrates verified ...
fresh_r2_probe_test.go:22: readiness-only criteria are still labeled LIVE without a daemon heartbeat
--- FAIL: TestFreshR2DoctorDoesNotLabelReadinessLive
```

[CONCRETE][SEV: LOW] `cmd/nexus/main.go:859` - Doctor's machine-readable-looking status column contradicts its explanations by labeling readiness-only criteria `LIVE`; the fresh/stale/absent behavior also has no committed red-capable regression.

PROBE: clean-export test invoked the real pinned doctor binary with live provider/Telegram/sandbox and a valid signed attestation but no daemon heartbeat; it observed the three false `LIVE` labels and a successful P0 grant.

FIX: represent criterion state as `LIVE|READY|OFF`, print that state directly, and add table tests for fresh, stale, and absent heartbeat plus projection-open failure.

### Codex #4 — acceptance publication discipline: PARTIAL, unresolved MEDIUM finding

Input validation and pre-grading staging are closed: the key and public key are validated before installed paths are named or written, and the signer, binary, attestation, and signature are staged before publication (`scripts/p0-accept.sh:17-50`).

The claimed final atomic publication is not closed. Lines 57-59 perform three independent renames: trust anchor first, attestation second, signature third. A failure or crash after any one rename leaves a mixed installed generation and invalidates the previously working set, contradicting the file's statement that a failed run never mutates it (`scripts/p0-accept.sh:8-12`). There is no committed script test or publication fault-injection test.

A hazard-safe runnable probe used a shell `mv` function that copied on the first call and returned 99 on the second; no real `mv` was executed. It reproduced the exact publication order:

```text
probe_exit=99
anchor=new-anchor
attestation=old-attestation
signature=old-signature
```

[CONCRETE][SEV: MED] `scripts/p0-accept.sh:57` - Three separately renamed files are individually atomic but not atomic as a trust set; a mid-publication failure destroys the previous coherent generation.

PROBE: fault injection on rename 2 left the new anchor with the old attestation/signature.

FIX: publish a complete versioned directory and atomically switch one generation pointer consumed by doctor, or add a tested rollback protocol that restores the entire prior set after every interrupted publication step.

### Codex #5 — clean-export buildcheck root: CLOSED

`repoRoot` derives the source root from `runtime.Caller` and verifies the expected script exists (`internal/buildcheck/staticcheck_test.go:15-27`). All three tests passed from the clean export without `.git`.

```text
CGO_ENABLED=0 /home/matej/.local/go/bin/go test -count=1 -v ./internal/buildcheck
--- PASS: TestStaticCheckPassesStaticNexusBinary
--- PASS: TestStaticCheckFailsDynamicBinary
--- PASS: TestStaticCheckRefusesNonELF
PASS
```

Fix-only ablation back to `git rev-parse --show-toplevel`, with the tests retained, made all three fail from the clean export with `repo root: exit status 128`.

### Kilo F3 — refusal delivery identity: CLOSED

All typed refusal branches use `EnqueueReplyID` with a deterministic SHA-256-derived ID over adapter domain, channel identity, and Telegram `update_id` (`internal/channel/telegram/telegram.go:209-246`). The regression explicitly resets the in-memory offset and redelivers the same update (`internal/channel/telegram/telegram_test.go:381-407`).

GREEN:

```text
--- PASS: TestRedeliveredRefusalIsIdempotent
PASS
```

Fix-only ablation back to generated reply IDs retained the test and produced:

```text
telegram_test.go:405: redelivered refusal produced 2 sends, want exactly 1
--- FAIL: TestRedeliveredRefusalIsIdempotent
```

## Rejected-finding rationale

### Kilo F1 — accepted rejection; no counter-probe

The rationale is correct. `internal/sandbox/sandbox.go:329-333` passes the same `*boundedBuffer` pointer as both outputs, and `Handle.SetOutput` assigns those exact interfaces to `exec.Cmd.Stdout` and `exec.Cmd.Stderr`. The installed Go 1.26.4 `os/exec.Cmd` contract states that if both are the same writer and comparable with `==`, at most one goroutine calls `Write` at a time (`$GOROOT/src/os/exec/exec.go:212-224`). A pointer is comparable, so the shared builder is serialized by the standard-library contract. No mutex fix is warranted.

### Kilo F2 — accepted rejection; no counter-probe

The rationale is correct. `reqs` is unbuffered, `AppendBatch` sends through a select, and the Go select specification says the chosen communication operation is executed before the case body. Therefore the send case cannot complete without the actor's receive rendezvous (`internal/kernel/journal/journal.go:575-590`). Once received, the actor computes the reply and sends it to a capacity-1 reply channel before its loop can select `done` (`internal/kernel/journal/journal.go:378-397`). `Close` waits for `actorDone` before closing the database (`internal/kernel/journal/journal.go:718-731`). There is no committed-but-unreceived state and no demonstrated hang.

## Whole-export evidence and proof ceiling

```text
CGO_ENABLED=0 /home/matej/.local/go/bin/go test -count=1 ./...
exit 0; all packages passed
```

The full clean-export suite proves current tests pass on this Linux/arm64 host with bwrap 0.11.2. It does not prove crash-atomic publication because no committed detector injects failure between publication steps, and it does not prove doctor state-label honesty because the committed acceptance test currently accepts the no-heartbeat grant. The publication probe simulated rename failure through a shim in accordance with the read-only audit safety rule; it did not mutate live configuration.

## Topknot review

The closed fixes are locally minimal and add no dependency or speculative abstraction. The publication fold needs a real single-generation mechanism; deleting staging would not solve it. The doctor fold needs a smaller, explicit state model rather than more prose embedded in the reason string.

Weakest link: the audit did not execute `scripts/p0-accept.sh` against a real owner key because it intentionally publishes configuration and invokes forbidden cleanup/rename operations; its exact graded package command was run, and its publication tail was independently fault-injected with shims.

Skill update: none. The existing NEXUS audit rules already require reporting-chain ablation and would have caught both misses.

VERDICT: FAIL
