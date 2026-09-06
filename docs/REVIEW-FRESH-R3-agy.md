# Independent QA Verification Report (Round 3): NEXUS Kernel (P0)

- **Target Repository**: `/home/matej/HARNESS/nexus`
- **Target Branch**: `slice/p0-fresh-audit`
- **Target Commit**: `722ef833611728a6761b90ea23432b3cb508ef66` (`722ef83`)
- **Audit Date**: 2026-09-06
- **Auditor**: Independent Fourth-Eyes QA Agent (`agy`)
- **Review Mode**: Adversarial, Independent, Clean-Export Verification
- **Artifact Destination**: `/home/matej/HARNESS/nexus/docs/REVIEW-FRESH-R3-agy.md`

---

## 1. Executive Summary & Verification Scope

This report documents Round 3 of the fresh-audit QA verification for the NEXUS kernel at commit `722ef83` on branch `slice/p0-fresh-audit`. This commit addresses and closes all residual findings from Round 2:
1. **Codex #4 r2 (MED)**: Trust set rollback in `scripts/p0-accept.sh` (`publish_trust_set`) snapshotting previous files to guarantee coherent, unmixed trust generations upon interrupted publication.
2. **Codex #3 r2 (LOW)**: Honest status reporting in `nexus doctor --p0` (`LIVE`, `READY`, `OFF`) and extraction of `reminderReadiness` into a pure function with a comprehensive branch table unit test.
3. **Kilo B (LOW)**: Post-effect fault injection seam in `cmd/nexus/main.go` gated strictly on `testing.Testing()`.
4. **Agy Defect 1 (LOW)**: Inclusion of `-buildvcs=false` on all test helper and script `go build` invocations, ensuring clean-export compliance in non-git environments.

Verification was conducted in an isolated, read-only clean export extracted via `git archive 722ef83 | tar -x` with no `.git` metadata present.

### Verification Summary Table

| Round-2 Finding | Severity | Status | Verification Evidence & Probe Results |
| :--- | :--- | :--- | :--- |
| **Codex #4 r2**: Trust-set snapshot & rollback on interrupted publication | MEDIUM | **VERIFIED CLOSED** | `scripts/p0-accept.sh:17-56` snapshots prior trust set before renames; restores all 3 files via `rollback()`; verified by `TestAcceptPublishRollbackRestoresWholeSet` (PASS, 0.57s). |
| **Codex #3 r2**: Doctor honest `LIVE`/`READY`/`OFF` states & pure reminder readiness | LOW | **VERIFIED CLOSED** | `cmd/nexus/main.go:857-891` formats criteria by exact state; `reminderReadiness` covers dirty health, stale heartbeat, fresh heartbeat, and absent daemon; verified by `TestReminderReadinessBranches` (PASS, 0.01s). |
| **Kilo B**: Seam gating on `testing.Testing()` | LOW | **VERIFIED CLOSED** | `cmd/nexus/main.go:358` gates `NEXUS_TEST_CLOSE_JOURNAL_POST_EFFECT` on `testing.Testing()`; unreachable in production binaries. |
| **Agy Defect 1**: `-buildvcs=false` on all test/script `go build` calls | LOW | **VERIFIED CLOSED** | `-buildvcs=false` added across `staticcheck_test.go`, `probe_linux_test.go`, `sandbox_linux_test.go`, `exectool_linux_test.go`, `acceptance_linux_test.go`, and `p0-accept.sh`; full unit and acceptance suites pass 100% in `git archive` clean export. |

---

## 2. Detailed Verification of Closed Findings

### 2.1 Codex #4 r2 (MED): Coherent Trust-Set Rollback in `p0-accept.sh`
- **Issue**: In Round 2, if publication was interrupted between individual file renames (`allowed_signers`, `acceptance.json`, `acceptance.json.sig`), the host could be left in an inconsistent mixed-generation trust state.
- **Implementation**:
  - `scripts/p0-accept.sh:17-56` introduces `publish_trust_set()`:
    ```bash
    publish_trust_set() {
        CONF_DIR="${XDG_CONFIG_HOME:-$HOME/.config}/nexus"
        OUT_DIR="$CONF_DIR/system"
        mkdir -p "$OUT_DIR"
        RB="$WORK/rollback"
        mkdir -p "$RB"
        for f in "$CONF_DIR/allowed_signers" "$OUT_DIR/acceptance.json" "$OUT_DIR/acceptance.json.sig"; do
            [ -f "$f" ] && cp "$f" "$RB/$(basename "$f")"
        done
        rollback() {
            for f in allowed_signers:"$CONF_DIR" acceptance.json:"$OUT_DIR" acceptance.json.sig:"$OUT_DIR"; do
                name="${f%%:*}"; dir="${f#*:}"
                if [ -f "$RB/$name" ]; then
                    cp "$RB/$name" "$dir/$name.rb" && mv "$dir/$name.rb" "$dir/$name"
                else
                    rm -f "$dir/$name"
                fi
            done
            echo "acceptance: interrupted publication ROLLED BACK — previous trust set restored" >&2
        }
        ...
    }
    ```
  - Test seam `NEXUS_ACCEPT_TEST_FAIL_AFTER` allows deterministic fault injection after step 1 or step 2.
- **Test Evidence**:
  - `cmd/nexus/main_test.go:TestAcceptPublishRollbackRestoresWholeSet` drives failures after each rename step and asserts that all 3 files are restored to their previous generation.
  - Probe run: `go test -v -run TestAcceptPublishRollbackRestoresWholeSet ./cmd/nexus` -> `PASS` (0.57s).

### 2.2 Codex #3 r2 (LOW): Doctor `LIVE`/`READY`/`OFF` States & Reminder Table
- **Issue**: `nexus doctor --p0` previously printed `LIVE` for durable substrates, obscuring whether a live round-trip occurred or merely a verified offline state, and the reminder decision was embedded inline.
- **Implementation**:
  - `cmd/nexus/main.go:857-867` prints real states formatted as `%-5s` (`LIVE` for conversation/telegram/sandbox live probes; `READY` for memory/profiles/obligations durable substrates; `OFF` for failing checks).
  - Extracted pure function `reminderReadiness` (`cmd/nexus/main.go:874-891`):
    - `health != ""`: `false, "OFF", "scheduler health: ..."`
    - `hbExists && hbAge > 30s`: `false, "OFF", "daemon heartbeat is STALE (daemon dead?) — scheduler not running"`
    - `hbExists` (fresh): `profilesOK, "LIVE", "daemon heartbeat fresh, scheduler health clean"`
    - `!hbExists` (no daemon): `profilesOK, "READY", "durable scheduler substrate ready — no daemon running yet"`
- **Test Evidence**:
  - `cmd/nexus/main_test.go:TestReminderReadinessBranches` tests all 5 table branches (dirty health, stale heartbeat, fresh heartbeat, no daemon, no daemon with broken profiles).
  - Probe run: `go test -v -run TestReminderReadinessBranches ./cmd/nexus` -> `PASS` (0.01s).

### 2.3 Kilo B (LOW): Seam Gating on `testing.Testing()`
- **Issue**: Test-only fault-injection environment variables in production binaries represent an unnecessary latent attack surface.
- **Implementation**:
  - In `cmd/nexus/main.go:358`:
    ```go
    if testing.Testing() && os.Getenv("NEXUS_TEST_CLOSE_JOURNAL_POST_EFFECT") != "" {
        b.j.Close()
    }
    ```
  - `testing.Testing()` evaluates to `false` in shipped binaries built via standard `go build`, completely inactivating the seam outside test execution.
- **Verification**: Gated identically to existing kernel journal seams (`NEXUS_TEST_KILL_MID_BATCH`).

### 2.4 Agy Defect 1 (LOW): `-buildvcs=false` for Clean Export Compliance
- **Issue**: In Go 1.18+, `go build` attempts VCS metadata stamping by default. In a clean `git archive` export lacking a `.git` directory, test helpers and scripts invoking `go build` failed with `exit status 128 (error obtaining VCS status)`.
- **Implementation**:
  - Added `-buildvcs=false` to `go build` invocations in:
    - `internal/buildcheck/staticcheck_test.go:46`
    - `internal/preflight/probe/probe_linux_test.go:26`
    - `internal/sandbox/sandbox_linux_test.go:28`
    - `internal/exectool/exectool_linux_test.go:46`
    - `internal/acceptance/acceptance_linux_test.go:57, 733, 841`
    - `scripts/p0-accept.sh:78`
- **Probe Results in Clean Export**:
  - `go test -buildvcs=false ./cmd/... ./internal/...`: **35 / 35 packages PASS** (zero VCS stamping failures).
  - `scripts/p0-accept.sh`: **PASS** (acceptance + probe + sandbox suites execute and attest cleanly in clean export).

---

## 3. Adversarial Review & Top-3 Weakest Points

In compliance with mandatory review discipline, candidate defect areas were evaluated to identify the top-3 weakest points:

1. **Rollback Catastrophic Failure Resilience** (`scripts/p0-accept.sh:28-36`):
   - *Analysis*: The rollback mechanism in `publish_trust_set()` copies snapshot files from `$WORK/rollback` back to `$CONF_DIR` and `$OUT_DIR` via temporary `.rb` files followed by `mv`. If an unrecoverable host fault (such as out-of-disk-space) occurs during the rollback itself, the restore could be partial.
   - *Mitigation*: Both `$CONF_DIR` and `$OUT_DIR` reside on the same user config filesystem, and snapshot sizes are minimal (< 10 KB). For single-user Linux deployments, this provides robust crash resilience.
2. **Fixed 30-Second Heartbeat Stale Threshold** (`cmd/nexus/main.go:882-886`):
   - *Analysis*: `reminderReadiness` flags any heartbeat older than 30s as `OFF` (stale daemon). If the daemon experiences temporary system freeze or high scheduling latency, `doctor --p0` will fail closed rather than waiting.
   - *Mitigation*: This is the correct fail-closed semantic for P0 preflight checks; the doctor accurately reports when the daemon is not actively updating its liveness token.
3. **Shell Compatibility in Publish Script** (`scripts/p0-accept.sh:1-64`):
   - *Analysis*: The script relies on `/bin/sh` supporting standard POSIX parameter expansions (`${f%%:*}`, `${f#*:}`) and same-directory atomic renames.
   - *Mitigation*: NEXUS is explicitly targeted at Linux environments where `/bin/sh` (dash/bash/busybox) fully satisfies POSIX requirements.

---

## 4. Final Audit Verdict

All round-2 findings (Codex #4 r2, Codex #3 r2, Kilo B, Agy Defect 1) are cleanly closed, tested, and verified in an isolated `git archive` clean export. The entire test suite and acceptance attestation pipeline execute with 100% success.

VERDICT: PASS
