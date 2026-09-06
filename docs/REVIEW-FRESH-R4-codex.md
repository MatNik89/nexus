# Fresh audit QA verification round 4 — Codex

## Scope and artifact binding

- Reviewed branch: `slice/p0-fresh-audit`.
- Reviewed commit: `2257e6ae9cae4b607d378b569b8e7629bf1d2814` (HEAD and branch both resolved to this commit).
- Committed tree: `56005a78e78428e6946971de13c45e29c993afd8`.
- Primary pristine export: `/tmp/nexus-fresh-r4-green.6l7w1t`, created with `git archive 2257e6a`.
- Independent boundary-probe export: `/tmp/nexus-fresh-r4-codex.WClIrj`; controlled two-step ablation export: `/tmp/nexus-fresh-r4-ablation.7LSjzy`.
- The pristine and probe copies of committed `scripts/p0-accept.sh` matched the committed blob before probing: `sha256 803ab83a5d2f20298dea8933b325fdf97d11f87f91a95d314b00bf2e8fdc3132`.
- Host evidence: uid `1000`, `go1.26.4 linux/arm64`, GNU coreutils `mv` 9.10, `/usr/bin/bwrap` 0.11.2.
- The source repository was not mutated except for this requested report. The pre-existing untracked Agy and Kilo round-4 reports and ignored binaries were not used as evidence or modified.

## Verdict summary

The production generation-switch mechanism closes the round-3 mixed-generation defect. The publisher stages all three files in one new generation and changes `trust/current` with one same-directory atomic replacement (`scripts/p0-accept.sh:23-42`). Independent probes at the true syscall boundaries observed old-complete before the rename and new-complete after the rename, with the old generation retained. Doctor reads the pointer once, rejects non-bare targets, and uses one fixed generation path for all three single-read verification inputs (`cmd/nexus/main.go:893-1017`). Acceptance fixtures publish with the same temporary-symlink plus rename pattern (`internal/acceptance/acceptance_linux_test.go:806-855`).

The committed detector does not prove that atomic replacement is causal. Its `NEXUS_ACCEPT_TEST_KILL_AT=switch` seam kills at `scripts/p0-accept.sh:38`, before the replacement symlink is even created at line 39, rather than in the vulnerable gap of a two-step pointer replacement. Replacing only the atomic `mv -T` switch with `rm -f current; ln -s ... current` left `TestAcceptPublishGenerationSwitchIsAtomic` GREEN. This contradicts the requested RED-capability claim. Because this round requires FAIL for every unresolved severity, the LOW detector finding makes the overall verdict FAIL even though no production atomicity defect was reproduced.

## Finding

[CONCRETE][SEV: LOW] `cmd/nexus/main_test.go:1680` - the test describes the `switch` SIGKILL as the last instant before atomic rename, but the production seam it drives runs before `ln -s` (`scripts/p0-accept.sh:38-39`). It therefore never exercises the only dangerous interval introduced by a two-step unlink-plus-symlink switch and is not revert-proof for the claimed atomic primitive.

PROBE: In a fresh `2257e6a` archive, I kept `TestAcceptPublishGenerationSwitchIsAtomic` unchanged and replaced only lines 39-42 of `scripts/p0-accept.sh` with the non-atomic two-step equivalent `rm -f "$TRUST/current"; ln -s "$GEN" "$TRUST/current"`. The exact focused test still exited 0: `--- PASS: TestAcceptPublishGenerationSwitchIsAtomic (6.84s)`. A guarded `/tmp`-only unlink shim implemented the ablated first step; recursive cleanup remained suppressed.

FIX: Add a boundary-controlled detector that keeps the assertion unchanged while the ablated pointer operation unlinks `current`, receives SIGKILL before recreating it, and therefore turns RED; retain the existing pre-rename and post-rename checks for the atomic implementation.

## Closure matrix — round-3 MED trust-set publication

- (a) Solves reported class? **YES for production.** One generation owns all three files; only one pointer replacement publishes it. No legitimate publisher writes a current generation after activation.
- (b) Regression? **No scoped production regression found.** The doctor remains fail-closed and retains the prior single-read byte binding for the attestation, signature, and pinned signer bytes.
- (c) Detector validity? **NO.** The committed `switch` kill is earlier than its comment claims and does not reach the two-step replacement gap.
- (d) Revert-proof? **NO.** Removing the atomic replacement while retaining the committed test produced GREEN, not RED.
- (e) Class sweep? **YES.** The publisher, doctor consumer, acceptance fixture publisher, all `trust/current` references, all three trust files, and the sole pointer replacement were traced. No sibling mixed-generation publication path was found.

## Independent kill-timing and consumer evidence

### Production boundary shapes

A temporary external test invoked the committed script unchanged. Its guarded `mv` wrapper accepted only absolute operands under `/tmp` and implemented the same single `rename(2)`-class replacement through `os.replace`.

- Kill immediately before the pointer replacement, after `current.new.<pid>` existed: the publisher exited nonzero; `current` still named the complete OLD generation.
- Kill immediately after the pointer replacement: the publisher exited nonzero; `current` named the complete NEW generation, and all three files matched NEW.
- After both cases, the OLD generation's `allowed_signers` still existed for recovery.

Observed result:

```text
=== RUN   TestR4IndependentKillBoundaries
--- PASS: TestR4IndependentKillBoundaries (2.17s)
PASS
ok github.com/MatNik89/nexus/cmd/nexus 2.192s
```

This establishes the honest transaction property as old-complete before commit or new-complete after commit. The stronger literal comment that a kill at *any* point always leaves the previous generation current is imprecise after the commit point; after atomic rename, the new complete generation is correctly current.

### Doctor pointer handling

Static flow proves a single `os.Readlink` at `cmd/nexus/main.go:903`; the resulting `genDir` is reused for `acceptance.json`, `acceptance.json.sig`, and `allowed_signers`. An independent table probe passed for `../escape`, absolute `/tmp/escape`, `a/b`, `a\\b`, `.`, and `..`, each returning the explicit bare-directory-name refusal before reading trust files.

```text
--- PASS: TestR4IndependentNonBarePointerRejection (0.00s)
PASS
ok github.com/MatNik89/nexus/cmd/nexus 0.004s
```

## Committed and aggregate verification

Committed focused test on the pristine source:

```text
env PATH=<guarded-temp-shims>:... CGO_ENABLED=0 go test -buildvcs=false -count=1 -v ./cmd/nexus -run '^TestAcceptPublishGenerationSwitchIsAtomic$'
--- PASS: TestAcceptPublishGenerationSwitchIsAtomic (3.58s)
PASS
ok github.com/MatNik89/nexus/cmd/nexus 3.586s
```

Aggregate clean-export verification:

```text
env PATH=<guarded-temp-shims>:... CGO_ENABLED=0 go test -buildvcs=false -count=1 -timeout 1800s ./...
exit 0
cmd/nexus 44.932s; internal/acceptance 1085.626s; internal/buildcheck 627.771s;
internal/exectool 163.052s; internal/preflight/probe 22.865s; internal/sandbox 7.851s;
all other discovered packages passed or had no test files.
```

Static gate:

```text
CGO_ENABLED=0 go vet -buildvcs=false ./...
exit 0
```

## Simplicity, counterargument, and proof ceiling

The production fix is the minimum coherent mechanism for the requested property: one generation, one native atomic pointer replacement, no new dependency, and retained recovery generations. No avoidable abstraction or scoped bloat finding was found; the production diff is lean already.

Strongest counterargument: the production mechanism is independently supported and the detector gap does not itself create a mixed generation, so ordinary product-only acceptance could reasonably pass. That does not control this verdict because the user explicitly required RED-capability and FAIL for unresolved LOW findings.

Weakest link: audit safety rules prohibited directly executing `mv`; the independent boundary test used a guarded `/tmp`-only `os.replace` shim matching the kernel atomic replacement primitive, while the committed shell's use of GNU `mv -T` was verified statically. This does not establish filesystem persistence across power loss, which was not the requested SIGKILL/process-death contract.

Skill update: none. The existing audit workflow already requires a kept-detector fix ablation; applying that rule exposed the miss.

VERDICT: FAIL
