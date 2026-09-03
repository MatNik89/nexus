# Constitution verification round 2

Target: current `CLAUDE.md` and `AGENTS.md` at repository HEAD `7ef9245ee9bcf5e6ad811a5b95e35ea72eebf3b0`. Baseline: all material findings in `REVIEW-CONSTITUTION-{codex,kilo,agy}.md`. User-confirmed external directives for English-only output and mandatory three-agent review are accepted as authority and are not re-litigated.

1. **[OK] The two externally authorized rules now carry adequate provenance, and the review gate has a terminating base case.**

   English is labeled an explicit user directive and a repository convention in `CLAUDE.md:8-10` and `AGENTS.md:6-7`. Mandatory codex/kilo/agy review is labeled an explicit user directive at `CLAUDE.md:92-98`, and review files, consolidation documents, and fold commits are explicitly outputs of the same gate rather than recursively gated deliverables (`:96-97`). This correctly resolves Codex round-1 findings 4-5 and Kilo finding 8 under the user's stated external authority.

2. **[UNFOLDED] Codex round-1 finding 6 was only partially adjudicated; several unsourced workflow mandates remain without a stated rejection reason.**

   The behavioral RED/GREEN and fresh-fixture rules now cite the user's cross-project discipline (`CLAUDE.md:86-90`; `AGENTS.md:58-61`), and fixed review tags were correctly replaced by task-appropriate tags (`AGENTS.md:64-67`). But “smallest causal diff / no speculative dependencies,” mandatory absolute paths, the presumption that a zero-finding review usually failed, and the docs-on-main/code-branch-per-slice policy remain binding at `AGENTS.md:62-68` and `CLAUDE.md:105-107`. “Git (convention)” names a category, not its authority or a reason for rejecting the round-1 overreach finding. The current brief confirms external authority only for English and mandatory three-agent review; these remaining clauses are neither folded out nor explicitly rejected with stated provenance.

   **Required correction:** attach the actual user-approved provenance/rejection reason to each retained workflow convention, or move/remove it. Do not infer authorization from the English/review directives.

3. **[OK] The task-ledger, GAPFIX-owner, and user-sign-off findings are correctly folded.**

   Both files now say `tasks-P0.md` applies once created and prohibit P0 implementation until then while allowing explicit doc/design tasks (`CLAUDE.md:18-19`; `AGENTS.md:70-73`), resolving Codex finding 1. Raw `GAPFIX-*` files are explicitly non-owners and bind only through higher-ranked incorporation (`CLAUDE.md:14-17`; `AGENTS.md:11-13`), resolving Codex finding 2. User approval is narrowed to changing PRD decisions, user-decided HARDQ resolutions, or accepted security/proof criteria; invariant-preserving implementation uses the normal gate (`CLAUDE.md:27-30`), resolving Codex finding 7.

4. **[OK] Exact-intent approval, UNKNOWN-effect recovery, delivery honesty, and profile binding are now aligned across both audiences.**

   Exact-intent, expiry, single use, ProfileID/target binding, and `(device,inode)` for destructive filesystem operations appear in `CLAUDE.md:35-38` and `AGENTS.md:23-25`, resolving Codex finding 8. UNKNOWN effects go to reconciliation without blind retry (`CLAUDE.md:39-41`; `AGENTS.md:26`), resolving Codex finding 9. Exactly-once admission versus at-least-once remote delivery, persist-before-offset inbox, and sent-but-unrecorded reconciliation appear in both (`CLAUDE.md:57-59`; `AGENTS.md:37-39`), resolving Codex/Kilo delivery findings. AGENTS now includes deny-default pre-admission channel/profile binding and forbids post-admission global lookup (`AGENTS.md:33-36`), resolving Codex finding 11.

5. **[UNFOLDED] The unknown-schema fold still diverges: AGENTS omits P0 raw-input preservation.**

   CLAUDE carries the complete split—P0 rejects unknown schema unprocessed with raw input preserved; v2 adds upcast+quarantine (`CLAUDE.md:42-44`). AGENTS carries reject-unprocessed and the v2 transition but drops raw-input preservation (`AGENTS.md:27-28`). The owner contract retains immutable raw input while scoping the upcast/quarantine machinery to v2 (`HARNESS-SPEC.md:1381`), and the audience divergence is unnecessary. A worker can discard the rejected bytes while complying with AGENTS.

   **Required correction:** add “raw input preserved” to the AGENTS schema rule.

6. **[OK] Kilo's over-broad subprocess finding and journal-owner attribution finding are correctly fixed.**

   Sandbox coverage is now scoped to TOOL-executing `ExecProcess` calls, while the CLIAgent provider subprocess is explicitly governed by the provider boundary and egress rather than the tool sandbox's NET_DENY (`CLAUDE.md:45-50`; `AGENTS.md:29-32`). Journal wording now distinguishes the P0.3 single-write-owner contract from HARDQ B7's serialized append-actor mechanism (`CLAUDE.md:31-34`; `AGENTS.md:20-22`). This folds Kilo findings 6-7 without creating an unsandboxed tool path.

7. **[UNFOLDED] HARDQ B5 is compressed past a material invariant in both P0 guard sections, and AGENTS also omits the generic-task prohibition.**

   HARDQ B5 requires delivery receipt plus user acknowledgment **correlated to the occurrence**, and forbids a generic “finish Y” claim until a verifier exists (`HARDQ-CONSOLIDATED.md:84-90`). Neither guard mentions occurrence correlation (`CLAUDE.md:73-75`; `AGENTS.md:48-49`), so an acknowledgment for occurrence N can be reused to close N+1. CLAUDE retains the no-generic-task rule, but AGENTS does not; a worker can introduce an unverifiable generic Task while following its local constitution. This is an incomplete fold of Codex finding 9/Kilo finding 3 and fails the requested exact HARDQ match.

   **Required correction:** in both files bind receipt+ack to the same occurrence ID; in AGENTS add “no generic finish Y without a handler-specific verifier.”

8. **[UNFOLDED] AGENTS does not carry the full B9/A2 P0 scope guard that CLAUDE correctly carries.**

   HARDQ B9 requires restart on configuration change and defers the transactional Activator until the first dynamic consumer (`HARDQ-CONSOLIDATED.md:121-127`). CLAUDE says `restart-on-config-change` and requires an explicitly selected dynamic consumer (`CLAUDE.md:78-80`); AGENTS omits restart and only says “until a dynamic consumer exists” (`AGENTS.md:52-54`). HARDQ A2 defines `s7-min = AttemptGrant + cancel + no-retry` and `s5-min = AtomicWriter` (`HARDQ-CONSOLIDATED.md:22-30`); CLAUDE repeats both definitions and later phases (`CLAUDE.md:81-82`), while AGENTS names only `s7-min`/`s5-min` with no contents or P2/P3 placement (`AGENTS.md:52-54`). This leaves the worker-facing guard open to hot reload or a larger P0 engine, the exact over-engineering Codex finding 12 and Kilo finding 5 sought to prevent.

   **Required correction:** mirror CLAUDE's restart rule, explicit-consumer condition, min-contract definitions, and P2/P3 deferral in AGENTS.

9. **[OK] The remaining new P0 scope guards match their HARDQ resolutions without a detected new contradiction.**

   Both files correctly preserve Telegram as compiled `channel:builtin` outside the runtime extensions/S6.8 closure (HARDQ A1), move Decay+Audn to P1 while exempting facts (B8), require durable journal-backed HITL rather than an in-memory wait (B6), and scope stuck detection to an interactive turn with scheduled/polling exemption (C3): `CLAUDE.md:67-84`; `AGENTS.md:43-56`. The mandatory read of Architecture Essentials remains a useful second line of defense, but it does not cure the worker-facing omissions in findings 5, 7, and 8.

10. **[OK] Agy's only actionable round-1 note—fresh stateful fixtures in AGENTS—is folded.**

    `AGENTS.md:58-61` now explicitly requires fresh temporary fixtures for stateful tests, matching CLAUDE's rule at `CLAUDE.md:86-90`. No new contradiction was found in this addition.

No new-error finding was found; the failures above are incomplete folds or unresolved round-1 adjudication rather than newly invented contradictions.

VERDICT: FAIL
