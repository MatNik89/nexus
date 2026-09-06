# PREP-LOW Verification — agy

**Target:** Repo `/home/matej/HARNESS/nexus`, branch `slice/p0-prep-low`, HEAD `d0d60d9` (`fix(prep-low): per-line machine-id trim + suspended turn recovery (prep1-r5/phase5-r4 LOWs)`).  
**Reviewer:** `agy` (Antigravity).  
**Scope:** Verification of two closed LOW findings (`prep1-r5` machine-ID whitespace trim; `phase5-r4` suspended turn recovery on redelivery) and assessment of Phase 3 Codex LOW #12 (memory store close). Working tree was never modified during verification.

---

## 1. Test Suite Verification

- **Repository Full Suite:** `CGO_ENABLED=0 go test -p 1 ./...` across all 34 packages: **PASS**, exit code 0 (0 `FAIL` / `panic`).
- **Acceptance Suite:** `internal/acceptance` passed cleanly in 108.08s (`TestChaosKillSurvival`, `TestChaosCheckerDetectsCorruption`, `TestCriterion4TelegramHITL`, `TestDoctorP0GrantLive`, etc. all passed).
- **CLI & Doctor Suite:** `cmd/nexus` passed in 4.25s (`TestMachineIDCheckScriptMirrorsDoctor` passed).
- **Daemon Suite:** `internal/app/daemon` passed in 0.44s (`TestRedeliveredSuspendedTurnRecoversChallenge` passed).

---

## 2. Verification of Fold Claims

### Fold #1: Per-Line Machine-ID Whitespace Trim ([`scripts/machine-id-check.sh:26`](file:///home/matej/HARNESS/nexus/scripts/machine-id-check.sh#L26))
- **Audit & Implementation:**
  - In `scripts/machine-id-check.sh:26`, the single-line no-op slurp sed (`:a; N; $!ba; ...`) was replaced with per-line edge trimming:
    ```sh
    MID="$(echo "$CONTENT" | sed -e 's/^[[:space:]]*//' -e 's/[[:space:]]*$//')"
    ```
  - Single-line whitespace-padded machine IDs (e.g. `"  0123456789abcdef0123456789abcdef  \n"`) now correctly trim to 32 hex characters, passing content validation and proceeding to the ownership check.
  - Multi-line files retain internal newlines in `$MID`, failing the 32-character length guard fail-closed.
  - In [`cmd/nexus/main_test.go:1492-1502`](file:///home/matej/HARNESS/nexus/cmd/nexus/main_test.go#L1492-L1502), the `"padded"` fixture tests this path, asserting refusal for `"not root-owned"` (proving content validation passed).
- **Causal Negative Ablation Probe:**
  - In a clean `git archive` export in `/tmp/nexus-preplow-probe`, reverting `scripts/machine-id-check.sh` to the old slurp regex turned [`TestMachineIDCheckScriptMirrorsDoctor`](file:///home/matej/HARNESS/nexus/cmd/nexus/main_test.go#L1434-L1506) **RED**:
    `main_test.go:1499: padded id refused as content (script diverges from Go): ".../padded content malformed (fail closed)"`.

### Fold #2: Suspended Turn Redelivery Recovery ([`internal/app/daemon/daemon.go:259-285`](file:///home/matej/HARNESS/nexus/internal/app/daemon/daemon.go#L259-L285))
- **Audit & Implementation:**
  - `completedTurnFinal` now inspects `approval.turn_suspended` events in addition to `machine.turn.succeeded`:
    ```go
    case "approval.turn_suspended":
        var p struct {
            TurnID  int64  `json:"turn_id"`
            Summary string `json:"summary"`
        }
        if err := json.Unmarshal(ev.Envelope.Payload, &p); err == nil && p.TurnID == turnID {
            summary = p.Summary
            found = true
        }
    case "machine.turn.succeeded":
        var p struct {
            Result string `json:"result"`
        }
        if err := json.Unmarshal(ev.Envelope.Payload, &p); err == nil && ev.Envelope.TurnID == turnID {
            summary = p.Result
            found = true
        }
    ```
  - When a daemon crashes after turn suspension but before the channel delivery receipt is persisted, subsequent redelivery recovers the suspension summary (challenge details) instead of encountering a duplicate `turn.created` event ID constraint failure or returning a generic error.
  - Because `j.Replay` iterates sequentially in journal order, a later `turn.succeeded` event on resume overrides the earlier `approval.turn_suspended` summary for the same turn ID.
  - In [`internal/app/daemon/daemon_test.go:520-580`](file:///home/matej/HARNESS/nexus/internal/app/daemon/daemon_test.go#L520-L580), `TestRedeliveredSuspendedTurnRecoversChallenge` sets up suspended turn 8 and asserts that redelivery recovers the challenge summary (`APPROVAL NEEDED [ch-only]`).
- **Causal Negative Ablation Probe:**
  - In `/tmp/nexus-preplow-probe`, renaming the `"approval.turn_suspended"` switch case in `daemon.go` turned [`TestRedeliveredSuspendedTurnRecoversChallenge`](file:///home/matej/HARNESS/nexus/internal/app/daemon/daemon_test.go#L520-L580) **RED**:
    `daemon_test.go:578: suspended-turn redelivery errored instead of recovering the challenge: append turn.created: journal append: UNIQUE constraint failed: events.event_id`.

### Fold #3: Obsolescence of Phase 3 Codex LOW #12 (Memory Store Close)
- **Audit & Confirmation:**
  - In [`internal/memory/store.go:333-352`](file:///home/matej/HARNESS/nexus/internal/memory/store.go#L333-L352), `memory.Store` only holds `j *journal.Journal` and an atomic sequence counter.
  - `mem_facts` is a projection registered on the journal; no separate SQLite database handle or unclosed file descriptor exists in `memory.Store`.
  - Closing the journal via `j.Close()` (in `main.go:141`) cleanly closes the underlying profile database.
  - The assessment that Codex Phase 3 LOW #12 is **OBSOLETE** is correct and verified.

---

## 3. Top-3 Weakest Points

1. **[LOW] Test Gap on Overwrite Assertion in `TestRedeliveredSuspendedTurnRecoversChallenge`:**
   - *Location:* [`internal/app/daemon/daemon_test.go:520-580`](file:///home/matej/HARNESS/nexus/internal/app/daemon/daemon_test.go#L520-L580).
   - *Evidence:* The test creates turn 7 (which suspends and later succeeds) and turn 8 (which only suspends), but only redelivers update 8 to test suspension recovery. It never redelivers update 7 to assert that the recovered result is the final `turn.succeeded` payload rather than the earlier suspension challenge. A negative mutation where `turn.succeeded` does not overwrite `approval.turn_suspended` remains green under this test.
   - *Additionally:* Lines 529-533 define an unused map `ev` and use a raw empty `{}` ToolCall payload rather than `approval.Store.Suspend`.

2. **[LOW] `sed [[:space:]]` vs Go `strings.TrimSpace` on Non-ASCII Unicode Whitespace:**
   - *Location:* [`scripts/machine-id-check.sh:26`](file:///home/matej/HARNESS/nexus/scripts/machine-id-check.sh#L26) vs [`cmd/nexus/main.go:995-1016`](file:///home/matej/HARNESS/nexus/cmd/nexus/main.go#L995-L1016).
   - *Evidence:* The POSIX shell character class `[[:space:]]` matches ASCII whitespace (`[ \t\r\n\f\v]`). Go's `strings.TrimSpace` uses `unicode.IsSpace` (which matches characters such as U+00A0 NO-BREAK SPACE). While valid Linux systemd `/etc/machine-id` files only contain ASCII hexadecimal characters and optional newlines, any file padded with non-ASCII Unicode whitespace will pass Go's doctor validator but fail the shell checker with `content malformed (fail closed)`.

3. **[LOW] Payload Schema Coupling in `completedTurnFinal`:**
   - *Location:* [`internal/app/daemon/daemon.go:264-275`](file:///home/matej/HARNESS/nexus/internal/app/daemon/daemon.go#L264-L275).
   - *Evidence:* `completedTurnFinal` parses `TurnID` from the envelope payload for `approval.turn_suspended` (`p.TurnID`), but reads `TurnID` from envelope metadata (`ev.Envelope.TurnID`) for `machine.turn.succeeded`. If any future approval event schema places `turn_id` elsewhere or encounters an unmarshal error, it will fail to match silently and return `false`, causing the turn to re-execute instead of recovering.

---

## 4. Verdict

All reported LOW findings are genuinely fixed, verified causal via negative ablation probes in fresh `git archive` exports, and introduce no regressions across the 34-package test suite.

VERDICT: PASS
