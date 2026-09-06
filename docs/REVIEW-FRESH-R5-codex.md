# Fresh Audit Round 5 — Codex QA Verification

## Scope and artifact binding

- Repository: `/home/matej/HARNESS/nexus`
- Branch observed: `slice/p0-fresh-audit`
- Reviewed commit and HEAD: `7f58df5fe323df55aa666dfd2b3733dc9a5a018f`
- Reviewed tree: `5084c6ae24367626574b2f54aa91c3903ca7ab2e`
- Review mode: read-only source review and execution from clean `git archive` exports. The source checkout was not checked out, reset, stashed, or used for test execution. This report is the only repository write.
- Reviewer environment: UID 1000, Linux 6.12.34+rpt-rpi-2712 aarch64. The clean exports and test fixtures were on tmpfs.
- Scoped committed-file identity matched between HEAD and the clean export:
  - `scripts/p0-accept.sh`: SHA-256 `f6bdce49c80e8dce864f1c988d1091d3b577bd53decfb9a35b67dceef88936fb`
  - `cmd/nexus/main_test.go`: SHA-256 `027417766aa5058663b715f3a5d32171de6b2a6c6060a92be51bf23fe9972c2a`
- `git check-ignore` and scoped ignored-status checks produced no output for the two reviewed files or this report path before report creation.

## Result

No unresolved HIGH, MEDIUM, or LOW finding was found. Commit `7f58df5` closes the shared round-4 LOW: the named `switch` seam is now at the stated pre-rename boundary, and the new detector independently turns RED when atomic replacement is changed to an unlink-plus-symlink publication while all fault seams are absent.

## Closure evidence

### 1. The `switch` seam is at the real last instant before publication

The committed order is:

1. stage all three files in the new generation (`scripts/p0-accept.sh:28-31`);
2. create `current.new.<pid>` (`scripts/p0-accept.sh:38`);
3. fire `NEXUS_ACCEPT_TEST_KILL_AT=switch` (`scripts/p0-accept.sh:39-42`);
4. atomically replace `current` (`scripts/p0-accept.sh:43-45`).

An independent traced probe against the clean export first installed an old generation, then invoked the `switch` SIGKILL path. The trace executed `ln -s`, then `kill -KILL`, and did not reach the rename. It exited 137; `trust/current` named the same old generation before and after; `current.new.<killed-pid>` existed and named the fully staged new generation. This distinguishes the seam from the earlier `stage` seam and places it exactly between staging-link creation and the atomic rename.

### 2. The new detector exercises the production publication function without fault seams

`TestTrustPointerNeverUnresolvableUnderConcurrentPublish` invokes the committed `scripts/p0-accept.sh` publish-only entry (`cmd/nexus/main_test.go:1728-1754`), establishes a baseline generation, starts a concurrent reader of the real `trust/current` symlink, and performs 40 alternating publications (`cmd/nexus/main_test.go:1756-1803`). The reader fails on any unreadable pointer, any incomplete generation, or differing A/B tags across `allowed_signers`, `acceptance.json`, and `acceptance.json.sig` (`cmd/nexus/main_test.go:1766-1783`). It does not set either fault-seam environment variable.

Untouched clean export:

```text
CGO_ENABLED=0 go test -count=1 -run '^TestTrustPointerNeverUnresolvableUnderConcurrentPublish$' -v ./cmd/nexus
--- PASS: TestTrustPointerNeverUnresolvableUnderConcurrentPublish (3.50s)
PASS
ok github.com/MatNik89/nexus/cmd/nexus 3.508s
```

### 3. Independent revert-proof ablation is RED

In a second clean export I removed all `NEXUS_ACCEPT_TEST_KILL_AT` and `NEXUS_ACCEPT_TEST_FAIL_AFTER` branches, kept the committed detector unchanged, and replaced only the atomic pointer replacement with a guarded unlink-then-symlink equivalent of `rm -f current; ln -s generation current`. The helper rejected every target outside the test-owned `XDG_CONFIG_HOME`; no source-checkout or non-fixture path was mutable.

The un-delayed ablation failed in five consecutive test instances at UID 1000:

```text
CGO_ENABLED=0 go test -count=5 -run '^TestTrustPointerNeverUnresolvableUnderConcurrentPublish$' -v ./cmd/nexus
--- FAIL: TestTrustPointerNeverUnresolvableUnderConcurrentPublish (0.29s)
    pointer unresolvable mid-publication: readlink .../nexus/trust/current: no such file or directory
--- FAIL: TestTrustPointerNeverUnresolvableUnderConcurrentPublish (0.20s)
--- FAIL: TestTrustPointerNeverUnresolvableUnderConcurrentPublish (0.21s)
--- FAIL: TestTrustPointerNeverUnresolvableUnderConcurrentPublish (0.13s)
--- FAIL: TestTrustPointerNeverUnresolvableUnderConcurrentPublish (0.14s)
FAIL
```

The first standalone run of the same ablation also failed for the same missing-pointer reason. No artificial sleep or fault seam was required. This is the round-4 negative mutation with the detector retained and the seams removed, so the new detector is causal for the atomic replacement rather than for test instrumentation.

## Five-question closure matrix

- **(a) Solves the reported class? YES.** The seam now observes the intended pre-rename state, while the independent reader detector covers the missing-pointer and mixed/incomplete-generation states produced by a non-atomic publication.
- **(b) Regression? NO found.** The change does not alter the production success path except for relocating an inert test-only kill branch; no new dependency, public API, or runtime state was added.
- **(c) Detector validity? YES.** The detector invokes the same `publish_trust_set` function used by the real acceptance path (`scripts/p0-accept.sh:23-52,87`) and reads the same `trust/current` representation consumed by doctor (`cmd/nexus/main.go:902-911`).
- **(d) Revert-proof? YES.** With the detector unchanged and fault seams absent, replacing atomic publication with unlink-plus-symlink was RED in 5/5 consecutive instances.
- **(e) Class sweep? YES.** The clean tree contains one production shell publisher, one doctor consumer, and one Go acceptance-fixture publisher. Doctor resolves `current` once and reuses one fixed generation directory for all verification inputs (`cmd/nexus/main.go:903-1017`). The fixture uses the same temporary-symlink plus `os.Rename` pattern (`internal/acceptance/acceptance_linux_test.go:807-844`). No sibling non-atomic trust-pointer publisher was found.

## Verification

All commands below ran in the untouched clean export of `7f58df5` with guarded `rm` and `mv` shims required by the audit safety policy. Cleanup deletion was suppressed; the `mv -T` call was restricted to the test-owned `XDG_CONFIG_HOME` and implemented with the same atomic rename syscall.

```text
CGO_ENABLED=0 go test -count=1 ./cmd/nexus
ok github.com/MatNik89/nexus/cmd/nexus 5.400s

CGO_ENABLED=0 go vet ./...
exit 0; no diagnostics

CGO_ENABLED=0 go test -count=1 ./...
exit 0; all listed packages passed, including:
cmd/nexus 16.036s
internal/acceptance 97.743s
internal/buildcheck 11.658s
internal/preflight/probe 24.648s
internal/sandbox 9.569s
```

One earlier `-count=10` baseline stress command was manually interrupted after three complete GREEN iterations because host saturation made one subsequent iteration take 145 seconds. Its aggregate exit was therefore nonzero and is not counted as passing evidence; the separately exit-checked focused, package, vet, and full-suite commands above are the acceptance evidence.

## Adversarial quality and simplification pass

The changed production logic is one relocated test-only branch. The detector adds no production abstraction or dependency and tests the operational boundary directly. The scoped diff is lean already; no `[BLOAT]` finding applies.

Strongest counterargument: the concurrent detector is scheduling-sensitive rather than a deterministic barrier test. That would matter if the exact minimal-gap mutation could escape it. It did not: the seam-free, un-delayed unlink-plus-symlink mutation was RED in the standalone run and in 5/5 consecutive repetitions. This is sufficient closure evidence for the reported regression shape, while not proving every scheduler or filesystem implementation.

Top three weakest points, none rising to an unresolved finding:

1. The concurrent sampler is probabilistic; the repeated causal REDs bound practical false-green risk but do not mathematically eliminate it.
2. Audit safety required a guarded rename-syscall shim instead of executing GNU `mv`; source order and argv were inspected, but GNU userland behavior was not separately re-proven in this round.
3. Tests ran on tmpfs under one Linux/aarch64 host and establish process-concurrency atomic visibility, not power-loss durability across filesystems.

Proof ceiling: this round verifies the exact round-4 LOW and its detector sensitivity at commit `7f58df5`; it does not re-certify the full live P0 acceptance run, release signing, deployment-host storage durability, or any revision other than the bound commit.

VERDICT: PASS
