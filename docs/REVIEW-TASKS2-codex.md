# P0 task-ledger verification round 2

Target: `docs/tasks-P0.md` v2 and the E14 amendment in `docs/ARCHITECTURE-ESSENTIALS.md` at repository HEAD `7e9a2408fef5f918242566db790a31cdd0d87548`. Baseline: all material findings in `REVIEW-TASKS-{codex,kilo,agy}.md`.

1. **[OK] Most round-1 trace and scope findings are correctly folded.**

   `s5-min/AtomicWriter`, deterministic clock/ID seams, `S8.1-min`, and `S11.1-min` now land in T05 (`docs/tasks-P0.md:62-70`); C8's liveness heartbeat and occurrence counter land in T17/T20 (`:170-177,203-211`); built-in Telegram release attestation lands in T27 (`:282-292`); and full `DATA_PURGE` is removed from T19 and placed in the deferred ledger (`:190-199,296-301`). PRD references now correctly use “§6 item N” (`:9,32,173,184,196,208,243,252,267,275`). These changes resolve Codex findings 1-3 and most of 4/6, Kilo findings 3-6, and Agy's sole material `s5-min` note.

2. **[UNFOLDED] `MEMORY_FORGET` still lacks authority to move from Annex P2.1 into P0.**

   T19 now states that FORGET ships in P0 (`docs/tasks-P0.md:190-199`), and E14 labels that move a “task-review fix” (`docs/ARCHITECTURE-ESSENTIALS.md:178-187`). But Essentials explicitly says the full source wins and ranks Annex A above itself (`docs/ARCHITECTURE-ESSENTIALS.md:3-17`); the normative owner still places both `MEMORY_FORGET` and `DATA_PURGE` under P2.1 (`docs/HARNESS-SPEC.md:1469-1478`). PRD §6 does not require either operation, and HARDQ B8 adds explicit facts/no-decay but does not pull FORGET into P0 (`docs/HARDQ-CONSOLIDATED.md:112-119`). This does not satisfy Codex round-1 finding 4's condition “retain MEMORY_FORGET only if its P0 authority is made explicit.” Either obtain a user product decision and amend Annex P2.1, or defer FORGET with PURGE.

3. **[OK] The principal renumbering and dependency corrections are sound.**

   T13 now defines `s7-min` before T14 consumes `AttemptGrant` (`docs/tasks-P0.md:131-148`), resolving Kilo finding 1. T11 is narrowed to the real T02 sandbox probe plus contract-valid provider/channel fakes, with later registrations in T15 and T23 (`:111-119,150-158,239-246`), resolving the immediate Kilo/Codex order defect. T14 owns seven fake-backed contract REDs and moves the real sandbox integration RED to T26 (`:138-148,272-278`), so the former skipped-test contradiction is removed. The T02 gate was correctly renumbered to T25-T26 (`:26-37,260-278`) and still gates only arbitrary execution.

4. **[UNFOLDED] T05 is named correctly but cannot satisfy its own Acceptance, and two of its four seams have no RED.**

   T05 bundles clock/ID injection, AtomicWriter, ContextBudget, and Assembler, but its Acceptance is “downstream tasks consume these seams” (`docs/tasks-P0.md:62-70`). At T05 those downstream tasks do not exist, so it cannot be marked DONE under the ledger's one-task-at-a-time rule (`:3-8`). Its REDs exercise only AtomicWriter and ContextBudget; a local clock/ID invention or unusable `Assembler.Base` can pass. Codex round-1 finding 6 is therefore only structurally, not verifiably, folded. Replace the future-consumer Acceptance with task-local conformance tests for all four seams; later tasks may additionally enforce import/use checks.

5. **[NEW-ERROR] Splitting the old oversized journal task created a consumer-before-producer T07.**

   T07 requires concrete sessions, obligations, memory-index projections, inbox/occurrence/outbox recipes, and a `MarkDone→ListActive` RED (`docs/tasks-P0.md:80-88`). Those domains are not built until T18-T24 (`:181-258`). A T07 implementer must either invent their schemas prematurely or cannot run the listed RED. This violates task-local satisfiability and the declared sequential order. Keep only a generic transaction/projection harness in T07 with contract-valid fakes; move each concrete recipe and its crash/visibility RED into its first real consumer (T20-T24).

6. **[UNFOLDED] The promised final live capability-snapshot verification is still absent from T27.**

   T11 says live provider/channel probes register later and “T27 verifies the final snapshot” (`docs/tasks-P0.md:111-119`); T15 and T23 do register them (`:150-158,239-246`). T27, however, checks the six PRD criteria, hostile suite, signing, and doctor status without requiring the final sealed snapshot to contain the live sandbox/provider/channel attestations or reject a missing/stale one (`:282-292`). Thus the latter half of Codex finding 7/Kilo finding 2 remains implicit. Add an explicit final-snapshot Acceptance and a RED that removes or stale-hashes each live probe and observes its capability OFF with no dispatch.

7. **[OK] The remaining round-1 RED-quality and granularity findings are substantially fixed.**

   Doctor now mutates each of its five prerequisites (`docs/tasks-P0.md:39-49`); security/effect structured payloads are strict in T15 (`:150-158`); T16 pairs positive interactive stuck detection with the polling exemption (`:160-168`); T17 has a deterministic terminal-to-render oracle plus separately labeled live smoke (`:170-177`); T21 supplies a concrete `file_note` handler and verifier (`:213-224`); T24 covers cross-profile occurrence/delivery/approval over restart (`:248-258`); and T27 keeps its harness outside the implementation and requires sensitivity for all six criteria (`:282-292`). The old T05 and T20 monoliths are split into T06/T07 and T22/T23; T22/T23 is a clean split (`:228-246`). These fold Codex findings 8-9 and 11-15 and Kilo finding 8, subject to the new T07 defect above.

8. **[NEW-ERROR] T02's new negative controls prove detector sensitivity by deliberately exposing real-host trust boundaries.**

   T02 runs on the real deployment host and asks the suite to use a deliberately loosened profile with recursive `/etc`, network enabled, and no PID namespace (`docs/tasks-P0.md:26-37`). Because the hostile suite includes `/etc/shadow`, egress, and process-tree payloads, the negative-control run can expose secret bytes, permit real egress, or escape the intended process boundary merely to prove RED. Red-capability does not authorize a hazardous live mutation. Keep the real-host positive fail-closed suite, but make negative controls use non-secret canary files, a test-owned local network sink, and a test-owned process tree; never grant recursive real `/etc` or uncontrolled egress.

9. **[OK] No full S7/S5/Activator creep or adopted-HARDQ loss was found beyond the residuals above.**

   The v2 ledger retains A1; A2's four named minimum cuts; B1-B9; C1-C5 and C7-C8; D1; and F1 across T02-T27. Full S7, full S5, DATA_PURGE, Decay/Audn, transactional Activator, entropy detection, SemanticHealth, migration machinery, native sandbox, plugin channels, and the coding trio remain deferred (`docs/tasks-P0.md:296-301`). That deferred list agrees with HARDQ A2/B8/B9/C1/C7/C8/D1 (`docs/HARDQ-CONSOLIDATED.md:22-38,112-127,131-163`) except for the unresolved FORGET phase assignment in finding 2.

VERDICT: FAIL
