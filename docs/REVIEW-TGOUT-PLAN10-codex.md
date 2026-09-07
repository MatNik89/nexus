# PLAN-TGOUT v10 review — Codex — round 10

## Review binding

- Reviewed commit: `7cda3b50aaa608fced4f29e89c269cad11ab73c7` (`docs(tgout): plan v10 — bytes-only contract, first-lease honesty, durability detectors; file the S7 outbox seam gap`).
- Reviewed tree: `8fe4704cc5b3106b2a9d0017e94384785078db2c`.
- `git log -1 7cda3b5` and `git rev-parse 7cda3b5^{commit}` resolved the same full commit. The review used a clean `git archive` export under `/home/matej/HARNESS/`, not `/tmp`; the exported `docs/PLAN-TGOUT.md` matched the committed blob byte-for-byte (SHA-256 `9bba7d011cb34a29c74ba67f877d5f1117764dca6f8aaac4186b05f5fd1a0fbe`).
- Scope: design only. No production code or tests were changed or executed. The only worktree write is this requested report.

## Findings

### 1. [CONCRETE][SEV: MED] The structured drift response is still not restart-stable

The plan requires `TOOL_SCHEMA_DRIFT` to become a journaled terminal `turn.failed` code and then the Croatian Telegram response (`docs/PLAN-TGOUT.md:85-101`), but its detector covers only the direct edge path (`docs/PLAN-TGOUT.md:107-111`). It still does not specify recovery of a durable failed terminal outcome after a crash between `turn.failed` and the inbound-terminal + outbox transaction.

That crash has a concrete wrong-result path in the revision being planned against. A redelivered Telegram update re-enters the same deterministic turn; when the duplicate lifecycle event fails, `RunChannelTurn` attempts recovery (`internal/app/daemon/daemon.go:263-279`), but `completedTurnFinal` recognizes only `turn.succeeded` and `approval.turn_suspended` (`internal/app/daemon/daemon.go:407-446`). It does not recover `turn.failed` or `error_code`. The remaining error reaches Telegram's generic failure branch (`internal/channel/telegram/telegram.go:270-276`), so the user gets the generic English message rather than the specified Croatian drift response. The model turn may also be re-entered before the duplicate event rejects it; the plan does not make the failed outcome the replay oracle.

PROBE: Static crash-window trace from durable `turn.failed` through deterministic turn re-entry, `completedTurnFinal`, and Telegram's generic error mapping. The v10 diff changes only the outbound-rendering section and task ledger, so this round-9 finding is unchanged.

FIX: Specify recovery of terminal `turn.failed{error_code}` by `turn_id`; add a close/reopen detector at the boundary after `turn.failed` but before `CompleteInbound`, asserting the same Croatian response is enqueued and the planner is not invoked again. Ablate only failed-turn recovery and require RED.

### 2. [CONCRETE][SEV: LOW] Duplicate-key routing is still not tested after fence normalization

The strict closed-contract and duplicate walk operate first on the raw reply (`docs/PLAN-TGOUT.md:59-80`), while a single wrapping fence is stripped only in the later classification-view branch (`docs/PLAN-TGOUT.md:81-84`). The detector matrix tests generic drift dialects as bare and single-fenced, but names the duplicate case separately and does not cross those dimensions (`docs/PLAN-TGOUT.md:107-111`).

A conforming-looking implementation can therefore token-walk every member of a bare object, yet use ordinary last-member-wins decoding after stripping a fence. It would pass the listed bare duplicate test and the listed single-fenced non-duplicate tests while misrouting a single-fenced duplicate such as `{"action":"memory_recall","action":"tool"}` as prose or as a different semantic result. That violates the plan's promise that duplicates never hide an earlier registered-tool value.

PROBE: Branch-by-branch detector analysis against the ordered raw-reply and normalized-view paths. The relevant plan lines are byte-identical to v9, so the earlier detector gap remains.

FIX: Add a committed single-fenced duplicate/alias case and require the all-members token walk on the normalized object; ablate that normalized-view walk alone and require RED.

## Round-9 disposition

- **Closed:** the v10 wording no longer claims this slice creates or schedules a second attempt; it confines the change to bytes carried by the existing delivery path and files the pre-existing ungranted re-flush seam separately (`docs/PLAN-TGOUT.md:121-128`; `docs/tasks-P0.md:464-471`). This does not cure the owner violation, but it no longer makes that separate repair part of this slice.
- **Closed:** the contract is honestly first-in-flight-lease formatting and explicitly accepts pre-wire loss of formatting (`docs/PLAN-TGOUT.md:129-138`).
- **Closed:** restart persistence, old-version rebuild, event-fold coverage, and two causal ablations are now explicit (`docs/PLAN-TGOUT.md:139-151`). The existing projection framework has the stated reset-and-replay seam (`internal/kernel/journal/journal.go:355-387`).
- **Not closed:** Findings 1 and 2 above were also round-9 findings. Neither their plan text nor their detector text changed in v10.

## Simplification review

The v10 delivery delta is already the smaller honest design: it adds one journal-derived projection field and no scheduler, retry loop, fallback send, dependency, or second persistence owner. No additional bloat finding is warranted. The two required repairs are detector/recovery completeness, not new abstraction.

## Weakest link / proof ceiling

The weakest link is terminal-failure replay: the plan promises a typed, localized user-visible result, but only the success/suspension outcomes currently have crash recovery and the plan has no detector for the missing failed branch. This is a design-only static review; it does not establish implementation correctness or live Telegram behavior.

Minimal Diff: no production change; added only this review report
-> proof: immutable commit/tree/blob binding plus static owner, lifecycle, projection, and detector trace
-> weakest link: failed terminal outcome is not a replay oracle
-> skipped: code/test execution and live Telegram probes; upgrade when implementation begins
Proof ceiling: future RED-to-GREEN behavior is not proven by a plan review
Status: FAIL at 7cda3b50aaa608fced4f29e89c269cad11ab73c7
topknot: ultra+preflight
VERDICT: FAIL
