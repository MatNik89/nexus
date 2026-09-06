# PREP-LOW Verification Round 2 — agy

**Target:** Repo `/home/matej/HARNESS/nexus`, branch `slice/p0-prep-low`, HEAD `6e375f5` (commits `7477bc2` + `6e375f5`).  
**Reviewer:** `agy` (Antigravity).  
**Scope:** Verification of round-1 defect closures:
1. Shared ASCII trim contract `[ \t\r\n]` between [`cmd/nexus/main.go:1023`](file:///home/matej/HARNESS/nexus/cmd/nexus/main.go#L1023) (`trimMachineID`) and [`scripts/machine-id-check.sh:28`](file:///home/matej/HARNESS/nexus/scripts/machine-id-check.sh#L28) (NBSP treated as malformed on both sides; fixtures `nbsppad` and `TestMachineIDValidation` bound to production helper).
2. Rebuilt [`TestRedeliveredSuspendedTurnRecoversChallenge`](file:///home/matej/HARNESS/nexus/internal/app/daemon/daemon_test.go#L525-L598) in `internal/app/daemon/daemon_test.go` (suspensions via `approval.Store.Suspend` with valid `ToolCall`/`ContextBlock`, turn-7 redelivery asserts later `turn.succeeded` beats earlier suspension, unused map removed).

Working tree was never modified during verification; all negative probes ran in fresh `git archive` exports.

---

## 1. Test Suite & Vet Verification

- `CGO_ENABLED=0 go vet ./...`: **PASS**, exit code 0.
- `CGO_ENABLED=0 go test -p 1 ./...`: **PASS**, all 34 packages green, exit code 0 (0 `FAIL` / `panic`).
- `cmd/nexus` suite: `TestMachineIDValidation` and `TestMachineIDCheckScriptMirrorsDoctor` passed in 19.9s.
- `internal/app/daemon` suite: `TestRedeliveredSuspendedTurnRecoversChallenge` passed in 0.53s.

---

## 2. Verification of Round-1 Finding Closures

### Finding #1: Shared ASCII Trim Contract & NBSP Parity
- **Implementation Audit:**
  - In [`cmd/nexus/main.go:1023-1025`](file:///home/matej/HARNESS/nexus/cmd/nexus/main.go#L1023-L1025), `trimMachineID` explicitly trims only ASCII spaces, tabs, carriage returns, and newlines: `strings.Trim(raw, " \t\r\n")`.
  - In [`scripts/machine-id-check.sh:28-29`](file:///home/matej/HARNESS/nexus/scripts/machine-id-check.sh#L28-L29), shell trimming uses `TAB="$(printf '\t')"; CR="$(printf '\r')"` with `sed -e "s/^[ $TAB$CR]*//" -e "s/[ $TAB$CR]*\$//"`, stripping the identical byte set.
  - Unicode whitespace such as U+00A0 NO-BREAK SPACE (`\u00a0`) is no longer stripped by Go's doctor, ensuring identical fail-closed rejection on both sides without locale/Unicode divergence.
  - In [`cmd/nexus/main_test.go:1416`](file:///home/matej/HARNESS/nexus/cmd/nexus/main_test.go#L1416), `TestMachineIDValidation` passes `trimMachineID` directly to test the production trim contract against NBSP padding.
  - In [`cmd/nexus/main_test.go:1492`](file:///home/matej/HARNESS/nexus/cmd/nexus/main_test.go#L1492), `TestMachineIDCheckScriptMirrorsDoctor` includes the `"nbsppad"` fixture and asserts the `"malformed"` refusal reason.
- **Causal Negative Ablation Probes:**
  - **Go Probe:** In a clean `git archive` export, mutating `trimMachineID` to `strings.TrimSpace(raw)` turned [`TestMachineIDValidation`](file:///home/matej/HARNESS/nexus/cmd/nexus/main_test.go#L1410-L1424) **RED**:
    `main_test.go:1417: NBSP-padded machine id survived the ASCII trim contract`.
  - **Shell Probe:** In a clean export, modifying `machine-id-check.sh` to strip NBSP bytes turned [`TestMachineIDCheckScriptMirrorsDoctor`](file:///home/matej/HARNESS/nexus/cmd/nexus/main_test.go#L1478-L1501) **RED**:
    `main_test.go:1499: nbsppad refused for the wrong reason (content check not causal): ".../nbsppad not root-owned (fail closed)"`.

### Finding #2: Rebuilt Suspended-Turn Recovery Test
- **Implementation Audit:**
  - [`TestRedeliveredSuspendedTurnRecoversChallenge`](file:///home/matej/HARNESS/nexus/internal/app/daemon/daemon_test.go#L525-L598) was completely restructured:
    - Suspensions are constructed via production `store.Suspend` with valid `contracts.ToolCall` and `contracts.ContextBlock`.
    - Turn 7 executes a turn that is first suspended and subsequently completes (`RunChannelTurn`). On redelivery (`redo7`), it explicitly asserts `redo7 == out7` and `!strings.Contains(redo7, "APPROVAL NEEDED")`, verifying that the later `turn.succeeded` event beats the earlier suspension in journal replay order.
    - Turn 8 sets up a suspended-only turn followed by simulated crash (`turn.created`), and verifies that redelivery returns `ch8.Summary`.
    - The unused `ev` map variable was removed.
- **Causal Negative Ablation Probes:**
  - **Success Overwrite Probe:** In a clean export, mutating `completedTurnFinal` in [`internal/app/daemon/daemon.go:268`](file:///home/matej/HARNESS/nexus/internal/app/daemon/daemon.go#L268) so that `turn.succeeded` does not overwrite an existing entry (`if !found && ...`) turned [`TestRedeliveredSuspendedTurnRecoversChallenge`](file:///home/matej/HARNESS/nexus/internal/app/daemon/daemon_test.go#L525-L598) **RED**:
    `daemon_test.go:576: redelivery recovered "APPROVAL NEEDED [...]: tool rm_file ...", want the later success "ok" to beat the earlier suspension`.
  - **Suspended Recovery Probe:** In a clean export, mutating the case `"approval.turn_suspended"` turned the test **RED**:
    `daemon_test.go:593: suspended-turn redelivery errored instead of recovering the challenge: loop: journal turn.created: journal append: constraint failed: UNIQUE constraint failed: events.event_id (2067)`.

---

## 3. Top-3 Weakest Points

1. **[LOW] Inline Anonymous Payload Parsing in `completedTurnFinal`:**
   - *Location:* [`internal/app/daemon/daemon.go:277-283`](file:///home/matej/HARNESS/nexus/internal/app/daemon/daemon.go#L277-L283).
   - *Evidence:* `completedTurnFinal` parses `approval.turn_suspended` into an anonymous inline struct (`var p struct { TurnID string; Summary string }`). If a schema update alters payload key naming or structure without updating this switch case, unmarshaling will fail silently (`err == nil` check fails) and `completedTurnFinal` will return `false`, causing redelivery to re-execute instead of recovering the challenge.

2. **[LOW] Synthetic `turn.created` Injection in Test Fixture:**
   - *Location:* [`internal/app/daemon/daemon_test.go:582-588`](file:///home/matej/HARNESS/nexus/internal/app/daemon/daemon_test.go#L582-L588).
   - *Evidence:* Turn 8 in `TestRedeliveredSuspendedTurnRecoversChallenge` directly appends `ev-turn-chan-chat-42-8-turn.created-1` to simulate a post-suspension crash before turn completion. While accurately asserting collision recovery against the unique constraint, it relies on hand-crafted event parameters rather than execution through the runner loop.

3. **[LOW] ASCII Trim Set Excludes Rare Control Whitespace (`\v`, `\f`):**
   - *Location:* [`cmd/nexus/main.go:1023-1025`](file:///home/matej/HARNESS/nexus/cmd/nexus/main.go#L1023-L1025) and [`scripts/machine-id-check.sh:28-29`](file:///home/matej/HARNESS/nexus/scripts/machine-id-check.sh#L28-L29).
   - *Evidence:* `trimMachineID` and `machine-id-check.sh` strip `" \t\r\n"`. Non-standard ASCII whitespace like vertical tab (`\v` / `\x0b`) or form feed (`\f` / `\x0c`) is treated as non-whitespace content and rejected as malformed. While fail-closed and appropriate for systemd `/etc/machine-id`, it represents an explicit 4-character subset rather than standard 6-character POSIX `isspace`.

---

## 4. Verdict

All findings from round 1 are completely and causally resolved. Causal RED-capability has been proven independently for both the Go and shell NBSP trim contracts, as well as for the suspended-turn success overwrite and challenge recovery logic. Full test suite and vet pass cleanly.

VERDICT: PASS
