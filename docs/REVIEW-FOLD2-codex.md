# FOLD VERIFICATION ROUND 2 — commit `14814cf`

Scope: only the 12 requested owner-document fixes and same-class contradictions introduced by those edits. Commit identity verified as `14814cfb3ece0053ff6a77025f6439a1d224aec6` (`HEAD`).

1. **[OK] Source precedence now makes the user-approved hard-questions resolutions binding.**

   `ARCHITECTURE-ESSENTIALS.md:8-17` ranks user-approved `HARDQ-CONSOLIDATED` immediately after PRD and explicitly says its A/B/C/D/F items supersede the specific amended clauses in Annex A, DESIGN-S0, DESIGN-STATUS and SECTION-MAP. This closes the round-1 ambiguity without globally invalidating unaffected owner contracts.

2. **[OK] P1.6 was rewritten into two normative channel paths; it is no longer an appended contradictory exception.**

   The owner at `HARNESS-SPEC.md:1461` separately declares `channel:builtin` and `channel:plugin`; the extension-only manifest fields are scoped literally to `channel:plugin` at `:1462`; registration invariants distinguish both paths at `:1465`. RED case A literally registers `channel:plugin` without extension/S6.8 attestation, while replay case B is explicitly common to built-in and plugin paths (`:1466`). `INCOMPLETE_CAPABILITY_CLOSURE` and `APPROVAL_REPLAY` remain required, so the plugin-path RED was preserved rather than weakened.

3. **[OK] P0.8/P0.9/P0.11/P0.13 carry the requested activation scopes in both the wiring table and contract bodies.**

   The wiring table marks P0.8=`local-inference`, P0.9=`coding`, P0.11=P0 AtomicWriter only / checkpoint under `coding`, and P0.13=P1 memory decay (`HARNESS-SPEC.md:1355-1358`). The bodies repeat those scopes at `:1555-1573`. P0.11's owner sentence explicitly limits P0 to the AtomicWriter clause and assigns shadow-checkpoint/rollback to coding or user-workspace mutation (`:1565-1568`); its unchanged rollback MUST/RED therefore applies only when that trigger is active, not to the P0 disposable-workdir path.

4. **[OK] P0.1 now separates P0 reject-unknown behavior from the later migration system.**

   `HARNESS-SPEC.md:1381` states that P0 needs `schema_version + reject-unknown-neobrađeno`, while the monotone upcast chain and quarantine sink activate with the first real migration in v2. The retained full migration invariant is conditionally applicable at that later trigger; it no longer forces a P0 quarantine/upcaster implementation.

5. **[OK] Both SECTION-MAP Decay rows now place Decay+Audn in P1.**

   The Phase-M row says P0 uses explicit facts and Decay+Audn are P1 (`SECTION-MAP.md:39`); the S9 status row repeats the same assignment (`:110`). Neither occurrence still labels Decay+Audn P0. Older HARNESS-PLAN wording remains lower-precedence and is specifically superseded by HARDQ B8, so this edit introduces no unresolved owner conflict.

6. **[OK] DESIGN-STATUS #3 records bwrap as the P0 backend without weakening the safety gate.**

   `DESIGN-STATUS.md:10` names bwrap as the user-approved P0 ENFORCED backend, keeps the same hostile-suite invariant, fail-closed behavior and attestation, moves the native helper to P1 hardening, and leaves the real-host bwrap probe OPEN. It also retains Win/macOS UNAVAILABLE and zero weaker-fallback.

7. **[OK] DESIGN-S0 has an explicit top-level change record for both superseded P0 assumptions.**

   `DESIGN-S0-sandbox-codex.md:3-10` says bwrap is P0, the native helper is P1, native-first/no-bwrap clauses below must be read accordingly, and the hostile suite plus fail-closed rules bind both backends. The same record removes `ActivationPlan`/`RollbackVault`/`ReconcilePrepare` from P0 and leaves only fail-closed Resolve plus a sealed startup snapshot. The detailed native and Activator designs remain valid for their deferred consumers rather than being silently deleted.

8. **[OK] E11 now uses reject-unknown for P0 and scopes quarantine correctly.**

   `ARCHITECTURE-ESSENTIALS.md:140-143` says an unknown schema ID/version is rejected unprocessed in P0; upcast plus quarantine belong to the v2 migration layer. It still forbids lossy downcast and silent drop when that later layer activates, matching the P0.1 scope note rather than weakening it.

9. **[OK] E4 now contains the complete B7 physical transaction contract.**

   `ARCHITECTURE-ESSENTIALS.md:59-64` names WAL, bounded `busy_timeout`, one serialized append/sequence actor, synchronous same-transaction core projections, daemon-owned DB/CLI-over-UDS, and all three `BEGIN IMMEDIATE` recipes: inbox admission+journal event, occurrence+run admission, and terminal result+outbox enqueue. These are now owner-level invariants, not deferred build-order details.

10. **[OK] The P0 additions carry C3's stuck-detector scope and exemption.**

    `ARCHITECTURE-ESSENTIALS.md:232-234` confines stuck detection to one interactive turn and requires scheduled/polling occurrences to carry a continuous-loop policy exempt from the identical-argument breaker. The RED implementation detail is appropriately deferred to `tasks-P0.md`.

11. **[OK] The P0 additions contain both positive C8 health signals.**

    `ARCHITECTURE-ESSENTIALS.md:235,240-242` assigns P0 a liveness heartbeat plus `last-occurrence-fired` counter and separately defers the SemanticHealth evaluator. This preserves the minimal operational signal without pulling its evaluation infrastructure into P0.

12. **[OK] F1 now contains all five checks and capability-scoped failure results.**

    `ARCHITECTURE-ESSENTIALS.md:235-240` lists kernel/ABI floor, bwrap, data-directory permissions, provider key and Telegram token; missing items lead to consented installation or exact instructions. The outcomes are scoped: provider key disables conversation, Telegram token disables Telegram, bwrap disables exec, and an unsafe data directory blocks stateful startup. `P0-capable` requires all six PRD §6 criteria. Together with E10's real-host hostile gate (`:122-137`), this does not create a degraded-but-enabled sandbox mode.

No new contradiction was found within the 12 edited classes. Remaining older statements in lower-precedence HARNESS-PLAN/DESIGN bodies are explicitly covered by the new precedence and inline change records; they do not override these owner amendments.

VERDICT: PASS
