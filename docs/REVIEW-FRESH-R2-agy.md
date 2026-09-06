# Independent QA Verification Report (Round 2): NEXUS Kernel (P0)

- **Target Repository**: `/home/matej/HARNESS/nexus`
- **Target Branch**: `slice/p0-fresh-audit`
- **Target Commit**: `564909d8e7a559e20c85c756c12916006ffa683a` (`564909d`)
- **Audit Date**: 2026-09-06
- **Auditor**: Independent Fourth-Eyes QA Agent (`agy`)
- **Review Mode**: Adversarial, Independent, Clean-Export Verification
- **Artifact Destination**: `/home/matej/HARNESS/nexus/docs/REVIEW-FRESH-R2-agy.md`

---

## 1. Executive Summary & Verification Scope

This report documents Round 2 of the fresh-audit QA verification for the NEXUS kernel at commit `564909d` on branch `slice/p0-fresh-audit`. The commit folds 6 accepted findings from the fresh-audit round (Codex #1, #2, #3, #4, #5, and Kilo F3) and rejects 2 findings with documented rationales (Kilo F1, Kilo F2).

The verification was conducted in an isolated, read-only clean export environment created via `git archive 564909d | tar -x`.

### Verification Summary Table

| Finding / Fold | Severity | Status | Verification Result & Evidence |
| :--- | :--- | :--- | :--- |
| **Codex #1**: Post-effect durability failure surfaces as E9 UNKNOWN error | HIGH | **VERIFIED CLOSED** | `cmd/nexus/main.go:357-370` returns explicit non-retryable error stating effect `EXECUTED`; tested by `TestPostEffectDurabilityFailureIsNotSuccess` via `NEXUS_TEST_CLOSE_JOURNAL_POST_EFFECT`. |
| **Codex #2**: `NEXUS_ACCEPT_REQUIRE_SANDBOX=1` and acceptance scope expansion | HIGH | **VERIFIED CLOSED** | Skips converted to hard failures in `internal/acceptance/acceptance_linux_test.go:714`, `internal/preflight/probe/probe_linux_test.go:37`, `internal/sandbox/sandbox_linux_test.go:50`; doctor requires `internal/acceptance+probe+sandbox`. |
| **Codex #3**: Doctor folds real projections & reminder heartbeat states | HIGH | **VERIFIED CLOSED** | `cmd/nexus/main.go:791` passes real projections into `journal.Open`; reminder health explicitly checks `heartbeat` modtime (fresh vs stale > 30s vs absent). |
| **Codex #4**: `p0-accept.sh` input validation, private staging, and atomic publish | MEDIUM | **VERIFIED CLOSED** | `scripts/p0-accept.sh:16-60` validates SSH keys first, stages all files in private `$WORK`, and atomically renames `.tmp` files into `$CONF_DIR` and `$OUT_DIR` only after full pass. |
| **Codex #5**: Clean export support via `runtime.Caller` | LOW | **UNRESOLVED DEFECT** | `repoRoot()` updated in `staticcheck_test.go:18`, BUT `go build` invocations in test helpers omit `-buildvcs=false`, causing `exit status 128` failure in real `git archive` exports. |
| **Kilo F3**: Telegram refusal delivery IDs derived from update identity | LOW | **VERIFIED CLOSED** | `internal/channel/telegram/telegram.go:209-248` derives stable `dlv-<sha256>` IDs; redelivered refusals are idempotent (verified by `TestRedeliveredRefusalIsIdempotent`). |
| **Kilo F1 (Rejected)**: Shared `boundedBuffer` data race claim | REJECTED | **RATIONALE VERIFIED** | Go stdlib `os/exec` guarantees that when `Stdout == Stderr` for `==-comparable` types, calls to `Write` are serialized by a single goroutine. |
| **Kilo F2 (Rejected)**: `Journal.AppendBatch` hang on unbuffered channel | REJECTED | **RATIONALE VERIFIED** | `reqs` is unbuffered; select send commits only on rendezvous with actor; a committed send is guaranteed to be received and answered via buffered reply channel. |

---

## 2. Detailed Verification of Accepted Folds

### 2.1 Codex #1 (HIGH): Post-Effect Durability Failure as E9 UNKNOWN
- **Problem**: Previously, if an approved tool action committed its side-effect, but recording `MarkResumeCompleted` in the journal failed, the caller could receive a misleading failure that invited a user retry, risking duplicate execution of an irreversible action.
- **Implementation**: In `cmd/nexus/main.go:357-370`:
  ```go
  if os.Getenv("NEXUS_TEST_CLOSE_JOURNAL_POST_EFFECT") != "" {
      b.j.Close()
  }
  final, ferr := b.d.ResumeChannelTurn(ctx, identity, turn, run, challengeID, continuation)
  if merr := b.approvals.MarkResumeCompleted(ctx, challengeID); merr != nil {
      return "", fmt.Errorf("the approved action EXECUTED (result: %s), but recording its completion failed: %w. Do NOT retry %s — the daemon reconciles it at the next startup", result, merr, challengeID)
  }
  ```
- **Test Evidence**: `cmd/nexus/main_test.go:TestPostEffectDurabilityFailureIsNotSuccess` executes with `NEXUS_TEST_CLOSE_JOURNAL_POST_EFFECT=1` and asserts that the returned error contains `EXECUTED` and forbids retry.
- **Probe Result**: `PASS` (duration: 0.45s).

### 2.2 Codex #2 (HIGH): `NEXUS_ACCEPT_REQUIRE_SANDBOX=1` Hard Gate
- **Problem**: When `bwrap` was absent, tests in `internal/acceptance`, `internal/preflight/probe`, and `internal/sandbox` skipped, allowing `p0-accept.sh` to sign an attestation without running hostile containment tests.
- **Implementation**:
  - `internal/acceptance/acceptance_linux_test.go:714`: `if os.Getenv("NEXUS_ACCEPT_REQUIRE_SANDBOX") != "" { t.Fatalf("bwrap REQUIRED for the graded acceptance run: %v", err) }`
  - `internal/preflight/probe/probe_linux_test.go:37`: `if os.Getenv("NEXUS_ACCEPT_REQUIRE_SANDBOX") != "" { t.Fatalf("bwrap REQUIRED for the graded acceptance run: %v", err) }`
  - `internal/sandbox/sandbox_linux_test.go:50`: `if os.Getenv("NEXUS_ACCEPT_REQUIRE_SANDBOX") != "" { t.Fatalf("bwrap REQUIRED for the graded acceptance run: %v", err) }`
  - `scripts/p0-accept.sh:35`: sets `NEXUS_ACCEPT_REQUIRE_SANDBOX=1` and expands test scope to `./internal/acceptance ./internal/preflight/probe ./internal/sandbox`.
  - `cmd/nexus/main.go:889`: `verifyAcceptanceAttestation` fails closed if `att.Suite != "internal/acceptance+probe+sandbox"`.
- **Probe Result**: `PASS`. Old attestations fail closed; sandbox absence is fatal during acceptance grading.

### 2.3 Codex #3 (HIGH): Doctor Projection Folding & Heartbeat Awareness
- **Problem**: `nexus doctor --p0` did not fold projections on journal open and overclaimed live scheduling status when the daemon was dead or not running.
- **Implementation**:
  - `cmd/nexus/main.go:791`: `journal.Open` now receives `memory.NewProjection(), schedule.NewProjection(), obligation.NewProjection(), channel.NewProjection(), approval.NewProjection()`.
  - `cmd/nexus/main.go:813-822`: checks `heartbeat` file modification time:
    - Fresh (`time.Since(modTime) <= 30s`): "daemon heartbeat fresh, scheduler health clean"
    - Stale (`time.Since(modTime) > 30s`): "daemon heartbeat is STALE (daemon dead?) — scheduler not running" (FAIL)
    - Absent: "durable scheduler substrate ready (READY — no daemon running yet)"
- **Probe Result**: `PASS`. Honest separation between live checks and durable substrate verification.

### 2.4 Codex #4 (MEDIUM): Atomic Publishing in `p0-accept.sh`
- **Problem**: `scripts/p0-accept.sh` previously mutated `$CONF_DIR/allowed_signers` before running tests. If tests failed, the host's trust anchor remained corrupted.
- **Implementation**: In `scripts/p0-accept.sh:16-60`:
  - Validates `$KEY` and `$KEY.pub` existence and non-empty content before any operation.
  - Writes trust anchor and attestation into private `$WORK` directory.
  - Builds and grades binary against staged files.
  - Signs attestation in `$WORK`.
  - Performs atomic same-directory `mv` of `.tmp` files into `$CONF_DIR/allowed_signers` and `$OUT_DIR/acceptance.json[.sig]`.
- **Probe Result**: `PASS`. Failures during grading leave installed trust anchors unmodified.

### 2.5 Kilo F3 (LOW): Stable Telegram Refusal Delivery IDs
- **Problem**: When unmapped chat messages were rejected, random delivery IDs were generated. If the adapter crashed before offset acknowledgement, Telegram re-served the update, sending a duplicate refusal message to the user.
- **Implementation**:
  - `internal/channel/telegram/telegram.go:209-216`:
    ```go
    func refusalID(identity string, updateID int64) string {
        sum := sha256.Sum256([]byte("tg-refusal|" + identity + "|" + strconv.FormatInt(updateID, 10)))
        return "dlv-" + hex.EncodeToString(sum[:12])
    }
    ```
  - Refusal enqueues use `EnqueueReplyID` with `refusalID(identity, u.UpdateID)`.
- **Test Evidence**: `internal/channel/telegram/telegram_test.go:TestRedeliveredRefusalIsIdempotent` verifies that duplicate delivery produces exactly 1 outbox message.
- **Probe Result**: `PASS` (duration: 0.03s).

---

## 3. Verification of Rejected Findings

### 3.1 Kilo F1: Claimed `boundedBuffer` Data Race on `SetOutput(out, out)`
- **Auditor Rationale**: REJECTED.
- **Technical Verification**:
  - Go's `os/exec` standard library documentation states:
    > *"If Stdout and Stderr are the same writer, and have a type that can be compared with ==, at most one goroutine at a time will call Write."*
  - In `internal/sandbox/sandbox.go:327`, `out` is `&boundedBuffer{limit: 1 << 20}`.
  - Pointer types in Go are `==`-comparable. `cmd.Stdout == cmd.Stderr` evaluates to `true`.
  - Go's internal `os/exec` implementation routes stdout and stderr through a single serializing pipe / copy loop.
- **Conclusion**: Rationale is 100% sound. No race condition exists.

### 3.2 Kilo F2: Claimed `AppendBatch` Hang on Unbuffered Rendezvous
- **Auditor Rationale**: REJECTED.
- **Technical Verification**:
  - In `internal/kernel/journal/journal.go:575-592`:
    - `j.reqs` is an unbuffered channel (`make(chan appendReq)`).
    - In Go CSP semantics, `j.reqs <- req` in a `select` statement can only commit upon rendezvous with the receiving goroutine (`loop()`).
    - Once the send commits, the actor has received the request and will process it and send to `req.reply` (`make(chan appendRep, 1)`).
    - If `ctx.Done()` or `j.closed` fires before rendezvous, the send never executes and returns `ctx.Err()` or `ErrJournalClosed`.
    - An additional `select` on `<-req.reply` with `ctx.Done()` would introduce a ghost-write desync (returning error to caller while the append commits in the background).
- **Conclusion**: Rationale is 100% sound.

---

## 4. Unresolved Finding & Reproducible Failure Probe

### Defect 1 (LOW): Missing `-buildvcs=false` Causes `go build` Failure in Clean `git archive` Exports
- **Severity**: LOW (build/test infrastructure in non-git environments)
- **Files Affected**:
  - `internal/buildcheck/staticcheck_test.go:46`
  - `internal/preflight/probe/probe_linux_test.go:26`
  - `internal/sandbox/sandbox_linux_test.go:28`
  - `internal/exectool/exectool_linux_test.go:46`
  - `internal/acceptance/acceptance_linux_test.go:57, 733, 841`
  - `scripts/p0-accept.sh:26`
- **Root Cause Analysis**:
  Commit `564909d` addressed Codex #5 by changing `repoRoot()` in `internal/buildcheck/staticcheck_test.go:18` to use `runtime.Caller(0)` instead of `git rev-parse --show-toplevel`.
  However, in Go 1.18+, `go build` by default attempts to stamp VCS metadata into binaries. When executed inside a directory exported from `git archive` (which does not contain a `.git` directory), `go build` fails with:
  ```
  error obtaining VCS status: exit status 128
  	Use -buildvcs=false to disable VCS stamping.
  ```
- **Reproduction Probe in Clean Export**:
  ```bash
  # In clean export without .git:
  go test -v ./internal/buildcheck
  ```
  **Observed Output**:
  ```
  === RUN   TestStaticCheckPassesStaticNexusBinary
      staticcheck_test.go:49: build: exit status 1
          error obtaining VCS status: exit status 128
          	Use -buildvcs=false to disable VCS stamping.
  --- FAIL: TestStaticCheckPassesStaticNexusBinary (0.33s)
  FAIL
  FAIL	github.com/MatNik89/nexus/internal/buildcheck	0.453s
  ```
- **Required Fix**:
  Pass `-buildvcs=false` to all `exec.Command("go", "build", ...)` invocations in test helpers and `scripts/p0-accept.sh`.

---

## 5. Adversarial Top-3 Weakest Points

1. **Test Helper `go build` VCS Stamping Sensitivity** (`internal/buildcheck/staticcheck_test.go:46`):
   Subprocess compilation of test fixtures without `-buildvcs=false` creates environmental coupling to the presence of a `.git` directory.
2. **`TestChaosKillSurvival` Readiness Timeout Under Full-Suite Parallelism** (`internal/acceptance/chaos_linux_test.go:71-85`):
   The 10-second readiness dial timeout (`400 * 25ms`) can be tight when running concurrently with heavy `bwrap` hostile probes under multi-core load.
3. **`resumeApproved` Error Context Formatting** (`cmd/nexus/main.go:368`):
   The error message returns formatted plain text. In future versions (P1+), a structured error type `ErrPostEffectDurabilityFailed` will allow programmatic client inspection.

---

## 6. Required Remediation Patch

```diff
diff --git a/internal/buildcheck/staticcheck_test.go b/internal/buildcheck/staticcheck_test.go
--- a/internal/buildcheck/staticcheck_test.go
+++ b/internal/buildcheck/staticcheck_test.go
@@ -43,7 +43,7 @@ func runCheck(t *testing.T, root, target string) (int, string) {
 func TestStaticCheckPassesStaticNexusBinary(t *testing.T) {
 	root := repoRoot(t)
 	bin := filepath.Join(t.TempDir(), "nexus")
-	cmd := exec.Command("go", "build", "-o", bin, "github.com/MatNik89/nexus/cmd/nexus")
+	cmd := exec.Command("go", "build", "-buildvcs=false", "-o", bin, "github.com/MatNik89/nexus/cmd/nexus")
 	cmd.Env = append(os.Environ(), "CGO_ENABLED=0")
 	if out, err := cmd.CombinedOutput(); err != nil {
 		t.Fatalf("build: %v\n%s", err, out)
diff --git a/internal/preflight/probe/probe_linux_test.go b/internal/preflight/probe/probe_linux_test.go
--- a/internal/preflight/probe/probe_linux_test.go
+++ b/internal/preflight/probe/probe_linux_test.go
@@ -23,7 +23,7 @@ func helperPath(t *testing.T) string {
 	t.Helper()
 	bin := filepath.Join(t.TempDir(), "probehelper")
-	cmd := exec.Command("go", "build", "-o", bin, "github.com/MatNik89/nexus/cmd/probehelper")
+	cmd := exec.Command("go", "build", "-buildvcs=false", "-o", bin, "github.com/MatNik89/nexus/cmd/probehelper")
 	cmd.Env = append(os.Environ(), "CGO_ENABLED=0")
 	if out, err := cmd.CombinedOutput(); err != nil {
 		t.Fatalf("building probehelper: %v\n%s", err, out)
diff --git a/internal/sandbox/sandbox_linux_test.go b/internal/sandbox/sandbox_linux_test.go
--- a/internal/sandbox/sandbox_linux_test.go
+++ b/internal/sandbox/sandbox_linux_test.go
@@ -25,7 +25,7 @@ func helperPath(t *testing.T) string {
 	t.Helper()
 	bin := filepath.Join(t.TempDir(), "probehelper")
-	cmd := exec.Command("go", "build", "-o", bin, "github.com/MatNik89/nexus/cmd/probehelper")
+	cmd := exec.Command("go", "build", "-buildvcs=false", "-o", bin, "github.com/MatNik89/nexus/cmd/probehelper")
 	cmd.Env = append(os.Environ(), "CGO_ENABLED=0")
 	if out, err := cmd.CombinedOutput(); err != nil {
 		t.Fatalf("building probehelper: %v\n%s", err, out)
diff --git a/internal/exectool/exectool_linux_test.go b/internal/exectool/exectool_linux_test.go
--- a/internal/exectool/exectool_linux_test.go
+++ b/internal/exectool/exectool_linux_test.go
@@ -43,7 +43,7 @@ func helperBin(t *testing.T) string {
 	t.Helper()
 	bin := filepath.Join(t.TempDir(), "probehelper")
-	cmd := exec.Command("go", "build", "-o", bin, "github.com/MatNik89/nexus/cmd/probehelper")
+	cmd := exec.Command("go", "build", "-buildvcs=false", "-o", bin, "github.com/MatNik89/nexus/cmd/probehelper")
 	cmd.Env = append(os.Environ(), "CGO_ENABLED=0")
 	if out, err := cmd.CombinedOutput(); err != nil {
 		t.Fatalf("building probehelper: %v\n%s", err, out)
diff --git a/scripts/p0-accept.sh b/scripts/p0-accept.sh
--- a/scripts/p0-accept.sh
+++ b/scripts/p0-accept.sh
@@ -23,7 +23,7 @@ PUB="$(cat "$KEY.pub")"
 SIGNERS_STAGE="$WORK/allowed_signers"
 printf 'owner %s\n' "$PUB" > "$SIGNERS_STAGE"
 FP="$(sha256sum "$SIGNERS_STAGE" | cut -d' ' -f1)"
 BIN="$WORK/nexus"
-CGO_ENABLED=0 go build -ldflags "-X main.acceptanceSignerFingerprint=$FP" -o "$BIN" "$ROOT/cmd/nexus"
+CGO_ENABLED=0 go build -buildvcs=false -ldflags "-X main.acceptanceSignerFingerprint=$FP" -o "$BIN" "$ROOT/cmd/nexus"
 DIGEST="$(sha256sum "$BIN" | cut -d' ' -f1)"
```

---

## 7. Final Audit Verdict

Per the strict dispatch policy requiring `VERDICT: FAIL` for any unresolved finding of any severity:
While folds 1–4 and Kilo F3 are cleanly closed and the 2 rejected findings are proven sound, Codex #5 left an unclosed failure mode where test helper builds fail under clean `git archive` exports.

VERDICT: FAIL
