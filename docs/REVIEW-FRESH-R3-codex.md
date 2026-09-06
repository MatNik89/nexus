# Fresh audit QA verification round 3 — Codex

## Scope and artifact binding

- Reviewed branch: `slice/p0-fresh-audit`.
- Reviewed commit: `722ef833611728a6761b90ea23432b3cb508ef66` (HEAD and branch both resolved to this commit).
- Committed tree: `75c9c81f7e580a65b69b3728c759e72dcc3a64df`.
- Clean export: `/tmp/nexus-fresh-r3.UTpiLb`, created with `git archive 722ef83`; the exported `scripts/p0-accept.sh` initially and finally matched the committed blob (`sha256 59520c5ba5320c6825576aaa7f6ddeacacc7576aa6f5c6fad47f589da7ecdae4`).
- Host evidence: uid `1000`, `go1.26.4 linux/arm64`, `/usr/bin/bwrap` `0.11.2`.
- The source repository was treated as read-only except for this requested report. Pre-existing ignored binaries and the unrelated untracked round-3 Agy/Kilo reports were not used as evidence or modified.

## Verdict summary

Three requested folds are closed: doctor reports explicit `LIVE`/`READY`/`OFF` states with a red-capable reminder branch table; the post-effect fault seam is unreachable in an ordinary built binary because it is gated by `testing.Testing()`; and every `go build` invoked by a committed Go test or shell script supplies `-buildvcs=false`.

The trust-set publication fold remains open. Its test proves rollback after two cooperative injected error returns, but the production script has no rollback/recovery boundary for an asynchronous interruption or process death between its three renames. A deterministic `SIGTERM` injection at the same first post-rename boundary left a mixed generation. Because this review requires FAIL for any unresolved severity, the overall verdict is FAIL.

## Finding

[CONCRETE][SEV: MED] `scripts/p0-accept.sh:16` - `publish_trust_set` restores the old set only when its own `mv`/`fail_seam` control flow calls `rollback`; the sole trap performs work-directory cleanup and does not invoke publication rollback. A signal or process death after the independent renames at lines 53 or 55 exits without restoring the previous three-file generation, contradicting lines 8-12 and 17-20.

PROBE: In the clean export, I kept `TestAcceptPublishRollbackRestoresWholeSet` unchanged and changed only the test fail seam to execute shell-builtin `kill -TERM "$$"` after the selected rename instead of cooperatively calling `rollback`. At step 1 the test turned RED with `allowed_signers` equal to `NEW-ANCHOR` while the other prior files remained old: `MIXED trust generation`. The probe used an `rm` shim, touched only test-owned temporary configuration, and the committed script was restored byte-exactly afterward.

FIX: Publish a complete versioned generation and atomically switch one pointer/path that doctor resolves, retaining the prior generation for recovery. Signal traps alone are insufficient because `SIGKILL`, host loss, and power loss are not catchable.

The consumer confirms this is one logical trust set: doctor reads `acceptance.json`, `acceptance.json.sig`, and `allowed_signers` independently (`cmd/nexus/main.go:895-968`). It fails closed on a mixed set, but that does not satisfy the claimed preservation of the previously working generation.

## Closure matrix

### Codex #4 r2 — coherent trust-set rollback: NOT CLOSED

- (a) Solves reported class? **NO.** Cooperative rename failures and the injected return seams roll back, but real interruption between renames still leaves mixed installed bytes.
- (b) Regression? **No separate regression found** within the scoped change; the unresolved original failure class is decisive.
- (c) Detector validity? **YES, but narrower than the claim.** The committed test invokes the real script and checks all three installed files after fail steps 1 and 2, but both seams call the rollback routine before returning.
- (d) Revert-proof? **YES for cooperative rollback.** Removing only the seam's `rollback` call kept the test installed and produced RED: `NEW-ANCHOR` was not restored after step 1.
- (e) Class sweep? **FAIL.** The three-file reader and all three rename boundaries were traced; there is no generation pointer, in-progress marker, startup recovery, or signal-triggered rollback.

### Codex #3 r2 — honest doctor states and reminder decision: CLOSED

- (a) Solves reported class? **YES.** Successful readiness-only criteria use `READY`, live probes use `LIVE`, and failed criteria print `OFF`; `reminderReadiness` covers dirty health, stale heartbeat, fresh heartbeat, no daemon, and profile failure.
- (b) Regression? **No scoped regression found.** The result still fails closed whenever `ok` is false.
- (c) Detector validity? **YES.** `TestReminderReadinessBranches` calls the exact pure helper used by `runDoctorP0`.
- (d) Revert-proof? **YES.** Mutating only the no-daemon return from `READY` to `LIVE` produced the expected RED at the `no daemon` row.
- (e) Class sweep? **YES.** The only status rendering loop now prints the stored state and overrides failures to `OFF`.

### Kilo B — post-effect seam test-only gate: CLOSED

- (a) Solves reported class? **YES.** `testing.Testing() &&` short-circuits the environment seam in ordinary binaries (`cmd/nexus/main.go:358`).
- (b) Regression? **No scoped regression found.** `TestPostEffectDurabilityFailureIsNotSuccess` still reaches the seam in the test binary and passed.
- (c) Detector validity? **YES for the post-effect behavior.** It exercises the real `resumeApproved` path after an approved effect; production exclusion is additionally established by the runtime guard.
- (d) Revert-proof? **The behavioral detector remains seam-dependent**, and a separately built probe printed `testing.Testing() == false`; source control-flow inspection establishes that the shipped path cannot evaluate the environment branch.
- (e) Class sweep? **YES.** The sibling journal crash seam follows the same `testing.Testing()` guard pattern (`internal/kernel/journal/journal.go:545`).

### Agy defect 1 — VCS-independent nested builds: CLOSED

- (a) Solves reported class? **YES.** All eight `go build` invocations in committed Go tests and shell scripts include `-buildvcs=false`.
- (b) Regression? **No scoped regression found.** The build flags otherwise remain unchanged.
- (c) Detector validity? **YES.** `internal/buildcheck` invokes a real nested `go build` of `cmd/nexus`.
- (d) Revert-proof? **YES.** With a deliberately failing `git` shim in a temporary VCS-marked export, the committed flag passed; removing only that flag produced `error obtaining VCS status: exit status 128` and the test turned RED.
- (e) Class sweep? **YES for the claimed test/script scope.** Static enumeration found no test or shell-script `go build` without the flag. The Makefile's ordinary product build is outside the stated test/script claim.

## Verification evidence

Focused GREEN on the exact committed sources:

```text
env PATH=<rm-shim>:... CGO_ENABLED=0 go test -buildvcs=false -count=1 -v ./cmd/nexus -run 'Test(ReminderReadinessBranches|AcceptPublishRollbackRestoresWholeSet|PostEffectDurabilityFailureIsNotSuccess)$'
PASS; ok github.com/MatNik89/nexus/cmd/nexus 0.136s
```

Aggregate clean-export GREEN:

```text
env PATH=<rm-shim>:... CGO_ENABLED=0 go test -buildvcs=false -count=1 -timeout 1800s ./...
exit 0; every discovered package passed, including internal/acceptance (194.157s), internal/buildcheck (56.783s), internal/preflight/probe (51.388s), and internal/sandbox (37.340s)
```

Additional static gate:

```text
CGO_ENABLED=0 go vet -buildvcs=false ./...
exit 0
```

RED-capability evidence:

```text
rollback ablation: FAIL — step 1 allowed_signers not restored; got "NEW-ANCHOR\n"
reminder state mutation: FAIL — no daemon got LIVE, wanted READY
buildvcs ablation under failing-git probe: FAIL — error obtaining VCS status: exit status 128
real interruption counterexample: FAIL — SIGTERM after rename 1 left a MIXED trust generation
```

## Simplicity and proof ceiling

The doctor helper, test gate, and build-flag changes are locally minimal, use the standard library, add no dependency, and do not introduce an avoidable abstraction. The publication rollback is small but not complete for the named interruption contract; a one-pointer generation switch is the smallest mechanism that makes the logical three-file set crash-coherent. No broader bloat finding was found in the scoped diff.

Weakest link: the signal probe deterministically proves process-interruption failure, but this audit did not perform a host power-loss/filesystem durability experiment. That stronger experiment is unnecessary for the verdict because the catchable `SIGTERM` counterexample already falsifies the claimed closure.

Skill update: none. The existing audit rule already requires partial-failure and interruption-boundary testing; no reusable workflow gap was exposed.

VERDICT: FAIL
