# REVIEW-PHASE4-R3 — Phase 4 Verification Round 3 (T20 Scheduler + T21 Obligations)

**Target Ref:** `slice/p0-phase4` @ HEAD (`14f0b65` over `f1d3889`)  
**Commit:** `14f0b65` (`fix(phase4-r2): fold verification round 2`)  
**Reviewer:** `agy` (Antigravity)  
**Scope:** The fold diff `f1d3889..14f0b65` across `internal/schedule/`, `internal/obligation/`, `internal/preflight/doctor/`, and `cmd/nexus/`.

---

## 1. Verification of Round 2 Codex Findings

| Finding ID | Severity | Description | Status | Evidence |
|---|---|---|---|---|
| **Codex #2** (UNFOLDED) | HIGH | B5 grading ceremonial: `MarkDelivered` / `MarkAcked` lacked transport receipt / gesture identities. | **[OK]** | `MarkDelivered` requires `DeliveryReceipt{Producer, ReceiptID}` and persists `delivered_by` + `delivered_at` ([`obligation.go:673-683`](file:///home/matej/HARNESS/nexus/internal/obligation/obligation.go#L673-L683)). `MarkAcked` requires `AckGesture{Source}` and grades persisted `delivered_by` and `g.Source` via `checker.Grade` ([`obligation.go:697-723`](file:///home/matej/HARNESS/nexus/internal/obligation/obligation.go#L697-L723)). Tested in `TestDeliveryReceiptAndGestureRequired` ([`obligation_test.go:454-472`](file:///home/matej/HARNESS/nexus/internal/obligation/obligation_test.go#L454-L472)). |
| **Codex #5** (UNFOLDED) | HIGH | Concurrent `executeGoverned` race without empty-value CAS. | **[OK]** | `EvTaskIntent` projection update enforces `AND intent_op=''` ([`obligation.go:339`](file:///home/matej/HARNESS/nexus/internal/obligation/obligation.go#L339)); a concurrent claimant fails CAS with `RowsAffected != 1` and aborts. Reconcile runs under the stored claim. Tested in `TestProjectionEnforcesExecutionProtocol` ([`obligation_test.go:408-416`](file:///home/matej/HARNESS/nexus/internal/obligation/obligation_test.go#L408-L416)). |
| **Codex #10** (UNFOLDED) | MED | `Run(ctx, interval, ...)` used `interval` for ticker but didn't update `s.sweepInterval` overdue tolerance. | **[OK]** | `Run` calls `s.SetSweepInterval(interval)` and ticks with `s.sweepInterval` ([`schedule.go:465,477`](file:///home/matej/HARNESS/nexus/internal/schedule/schedule.go#L465)); one owned number governs both ticking and overdue classification. |
| **Codex #11** (UNFOLDED) | MED | `mirrorCounter` error ignored; doctor mapped read errors to `StatusOK` and accepted malformed mirror. | **[OK]** | `mirrorCounter` returns errors; `Sweep` propagates them; `Scheduler.Health()` records last sweep error ([`schedule.go:420-439,488-497`](file:///home/matej/HARNESS/nexus/internal/schedule/schedule.go#L420-L439)); doctor strictly validates with `strconv.ParseUint` and flags malformed or unreadable mirrors as `StatusOff` ([`doctor.go:123-143`](file:///home/matej/HARNESS/nexus/internal/preflight/doctor/doctor.go#L123-L143)). |
| **Codex #13** (UNFOLDED) | MED | Kind schema not validated at admission for `task_params`. | **[OK]** | `Registry` holds `map[string]Kind` with `ValidateParams` ([`obligation.go:459-466`](file:///home/matej/HARNESS/nexus/internal/obligation/obligation.go#L459-L466)); `CreateTask` rejects malformed JSON or multi-line notes before journal append ([`obligation.go:612`](file:///home/matej/HARNESS/nexus/internal/obligation/obligation.go#L612)). Tested in `TestTaskAdmissionSealedAndSafe` ([`obligation_test.go:437-450`](file:///home/matej/HARNESS/nexus/internal/obligation/obligation_test.go#L437-L450)). |
| **Codex #14** (UNFOLDED) | MED | Missing causal SIGKILL fuzz test through `Scheduler.Sweep`. | **[OK]** | Added `TestSweepCrashConsistencyUnderSigkill` ([`schedule_test.go:455-520`](file:///home/matej/HARNESS/nexus/internal/schedule/schedule_test.go#L455-L520)) running child processes killed mid-sweep with `SIGKILL`, asserting journal chain integrity and recipe atomicity on reopen. |
| **Codex #15** (UNFOLDED) | MED | Production test manually called Sweep/MarkDelivered and missed exact-intent approval RED. | **[OK]** | `main_test.go` updated to drive the real scheduler goroutine `b.sched.Run` and assert `b.sched.Health() == nil` ([`main_test.go:235-244`](file:///home/matej/HARNESS/nexus/cmd/nexus/main_test.go#L235-L244)); added `TestReminderSetExactIntentApproval` ([`tools_test.go:19-65`](file:///home/matej/HARNESS/nexus/internal/obligation/tools_test.go#L19-L65)) verifying `reminder_set` stops at `NEEDS_APPROVAL`, rejects tampered payloads, and executes on exact approval. |
| **Codex #16** (NEW-ERROR) | MED | Package-global mutable `notesDir` state. | **[OK]** | Global removed; `notesDir` is an immutable field on `Manager` passed via `NewManager` ([`obligation.go:524,533`](file:///home/matej/HARNESS/nexus/internal/obligation/obligation.go#L524)). |
| **Codex #17** (NEW-ERROR) | MED | Marker ID encoding ambiguity / unvalidated task IDs. | **[OK]** | `taskIDOK` enforces `[A-Za-z0-9_-]{1,64}` slug format at creation ([`obligation.go:586-598,605`](file:///home/matej/HARNESS/nexus/internal/obligation/obligation.go#L586-L598)), preventing `]` or newline delimiter collisions. Tested in `TestTaskAdmissionSealedAndSafe`. |
| **Codex #18** (NEW-ERROR) | HIGH | Execution protocol not enforced by projection. | **[OK]** | Projection enforces state machine: `EvTaskIntent` requires `status='OPEN'` and `intent_op=''`; `EvTaskExecuted` requires matching `intent_op` and `marker_line=''`; `EvTaskDone` requires `marker_line!=''` ([`obligation.go:339-399`](file:///home/matej/HARNESS/nexus/internal/obligation/obligation.go#L339-L399)). Tested in `TestProjectionEnforcesExecutionProtocol`. |
| **Codex #19** (NEW-ERROR) | MED | Task params carrying secrets silently rewritten. | **[OK]** | `CreateTask` checks `m.j.RedactorRewrites(raw)` and fails closed before journal append if secret references are present ([`obligation.go:620-626`](file:///home/matej/HARNESS/nexus/internal/obligation/obligation.go#L620-L626)). |
| **Codex #20** (NEW-ERROR) | HIGH | v1 projection catch-up missing upcaster for `due_utc`. | **[OK]** | `schedule.Projection.Apply` checks `p.DueUTC == 0` on `EvScheduleCreated` and upcasts deterministically from `p.Wall.dueUTC()` ([`schedule.go:207-217`](file:///home/matej/HARNESS/nexus/internal/schedule/schedule.go#L207-L217)). Tested in `TestV1ScheduleEventUpcastsNotEpochFires` ([`schedule_test.go:422-453`](file:///home/matej/HARNESS/nexus/internal/schedule/schedule_test.go#L422-L453)). |
| **Codex #21** (NEW-ERROR) | LOW | Obsolete post-fire `MarkDue` callback in `main.go`. | **[OK]** | Removed redundant callback in `cmd/nexus/main.go:128` (`b.sched.Run(ctx, 30*time.Second, nil)`). |

---

## 2. Key Seam & Architectural Judgments

### (a) Projection-Enforced CAS / Attestation / Done Protocol
The task execution protocol is now strictly enforced inside the SQLite append transaction:
1. **Intent Claim (CAS):** `UPDATE obl_obligations SET intent_op=? WHERE id=? AND status='OPEN' AND intent_op=''`. A second concurrent claimant fails with zero rows affected and aborts the append.
2. **Attestation Binding:** `UPDATE obl_obligations SET expected_path=?, marker_line=? WHERE id=? AND status='OPEN' AND intent_op=? AND intent_op!='' AND marker_line=''`. An execution without a prior claim, with a mismatched operation ID, or attempting to overwrite an existing attestation is rejected.
3. **Done Verification:** `UPDATE obl_obligations SET status='DONE' WHERE id=? AND marker_line!=''`. A task without an attested execution marker cannot transition to `DONE`.
This closes the door on forged or out-of-order protocol events.

### (b) DeliveryReceipt & AckGesture Artifact Discipline
`MarkDelivered` requires a channel-provided `DeliveryReceipt{Producer, ReceiptID}` and records both `delivered_by` and `delivered_at` into the obligation row. `MarkAcked` requires an `AckGesture{Source}` and supplies the stored `delivered_by` and `g.Source` to `checker.Grade`. The method no longer fabricates timestamps or producers. Under the declared topknot ceiling, cryptographic verification of the transport producer identity is deferred to T22 (channels).

### (c) Replay Determinism & v1 Upcast
When replaying a v1 `schedule.created` event lacking `due_utc`, `schedule.Projection.Apply` computes `p.Wall.dueUTC()` using the persisted wall components (`Year`, `Month`, `Day`, `Hour`, `Minute`, `TZ`). This guarantees that older events do not evaluate with `due_utc = 0` (epoch 1970) and fire immediately on catch-up, preserving replay determinism.

### (d) Causal SIGKILL Fuzzing
`TestSweepCrashConsistencyUnderSigkill` spawns child processes repeatedly running `Scheduler.Sweep` and terminates them mid-execution with `SIGKILL`. The survivor journal replay proves that integrity hashes remain unbroken and every `EvOccurrenceFired` event is paired with its atomic `EvRunAdmitted` transaction member.

### (e) Marker & ID Sealing
- Task IDs are validated at creation via `taskIDOK` against `^[A-Za-z0-9_-]{1,64}$`, eliminating delimiter (`]`) or whitespace ambiguity.
- Task parameter JSON schemas (`ValidateFileNoteParams`) enforce single-line notes and non-empty content.
- `RedactorRewrites` checks at task admission reject secret references before commit, ensuring the exact approved bytes match the committed task.

### (f) Health & Doctor Observability
`Scheduler.Run` records sweep and mirror errors into `Scheduler.Health()`. `doctor.go` strictly validates `system/last_occurrence_fired` as an unsigned integer, reporting missing files as clean pre-fire state and surfacing I/O errors or corrupt contents as `StatusOff`.

---

## 3. Weakest Points Analysis

In accordance with mandatory review discipline, the top 3 weakest points in the folded codebase are:

1. **Declared T22 transport authentication ceiling:**
   - **File:Line:** [`internal/obligation/obligation.go:657-659`](file:///home/matej/HARNESS/nexus/internal/obligation/obligation.go#L657-L659)
   - **Evidence:** `DeliveryReceipt` and `AckGesture` accept declared string identifiers (`Producer`, `Source`) passed across Go API boundaries. While persisted and checked against `Worker` identities in `checker.Grade`, cryptographic transport signature verification is deferred to the T22 channel slice.

2. **Single-profile append lock on `notes.txt`:**
   - **File:Line:** [`internal/obligation/obligation.go:411,428`](file:///home/matej/HARNESS/nexus/internal/obligation/obligation.go#L411)
   - **Evidence:** `FileNoteHandler` synchronizes in-process appends via `fileNoteMu sync.Mutex`. While `notesDir` is now immutably profile-scoped on `Manager`, concurrent out-of-process modifications to `notes.txt` rely on atomic rename replacement rather than file-level advisory locks.

3. **Verifier-read $\to$ `EvTaskDone` append window:**
   - **File:Line:** [`internal/obligation/obligation.go:861-881`](file:///home/matej/HARNESS/nexus/internal/obligation/obligation.go#L861-L881)
   - **Evidence:** In `MarkTaskDone`, `fileContainsLine` checks the filesystem, grades it with `checker.Grade`, and appends `EvTaskDone`. External file truncation between the read and the append is tolerated as a declared ceiling (replay determinism prohibits filesystem I/O inside SQLite projection transactions).

---

## 4. Test & Verification Evidence

- `CGO_ENABLED=0 go vet ./...`: Clean (exit code 0).
- `CGO_ENABLED=0 go test -count=1 ./...`: All 25 packages with tests passed uncached (exit code 0).
- `TestSweepCrashConsistencyUnderSigkill`: Verified passing (child process killed mid-sweep ×4).
- `TestReminderSetExactIntentApproval`: Verified passing (exact-intent approval gates tool execution).
- `git ls-files cmd/nexus/main.go`: Verified tracked and present.

---

VERDICT: PASS
