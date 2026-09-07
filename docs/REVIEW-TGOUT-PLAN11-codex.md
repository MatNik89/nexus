# PLAN-TGOUT v11 review — Codex — round 11

## Review binding

- Reviewed commit: `3b43fcb334bf52caedee9f9ca513574c05996dd6` (`docs(tgout): plan v11 — restart-stable drift recovery + fenced-duplicate cases`).
- Reviewed tree: `3c999a4f5725999fbd96b043d0d67ff94ecc116f`.
- `git log -1 3b43fcb`, `git rev-parse 3b43fcb^{commit}`, and `git get-tar-commit-id` all resolved the same full commit. The committed plan contains the required literal `RESTART-STABLE RECOVERY`.
- Review source: clean `git archive` export under `/home/matej/HARNESS/`, not `/tmp`. The exported `docs/PLAN-TGOUT.md` matched the committed blob byte-for-byte (SHA-256 `41ca5878f8a9a8fed8dc37bfc385f8b1445943aa1756440d37565bb7d01e8350`).
- Scope: design only. No production code or tests were changed or executed. The only worktree write is this requested report.

## Finding

### 1. [CONCRETE][SEV: LOW] `error_code` alone cannot reproduce the promised typed outcome

The plan defines the original drift result as a complete `contracts.TypedError`: code, category, retryability, origin, and a safe message that names the selected tool (`docs/PLAN-TGOUT.md:87-94`). V11 then promises that restart recovery returns “the same typed outcome,” but durably adds only `error_code` to `turn.failed`; its detector observes only the Croatian edge message and planner-call count (`docs/PLAN-TGOUT.md:98-106`).

That durable shape is insufficient for the stated equality. The actual typed contract has five required fields—`Code`, `Category`, `Retryability`, `SafeMessage`, and `Origin`—plus optional retry/cause data (`internal/kernel/contracts/contracts.go:521-572`). In particular, the original safe message names the tool, while the fixed code `TOOL_SCHEMA_DRIFT` does not encode which tool won the plan's `action > tool_id > name` precedence. The provider's raw drift reply is not otherwise part of the specified failed event. The current recovery seam also returns only `(string, bool, error)` (`internal/app/daemon/daemon.go:407-446`), so v11 necessarily needs either a richer recovered outcome or an explicit narrowing of what “same” means.

A reconstruction that fabricates a generic `TypedError{Code: "TOOL_SCHEMA_DRIFT", ...}` can therefore pass the proposed restart detector—same Croatian message, zero planner calls—while losing the original tool-specific `SafeMessage` or silently changing category/origin/retryability. That is a false green against the plan's full typed-outcome claim.

PROBE: Static information-sufficiency check: compared every field of `contracts.TypedError` and the variable tool-name requirement with the sole persisted `error_code`, then compared those fields with the restart detector's observables.

FIX: Choose one honest contract. If exact typed-outcome recovery is required, persist the complete validated/redacted `TypedError` in `turn.failed`, recover a typed outcome rather than a string, and assert field-for-field equality across reopen; ablate one persisted field and require RED. If only Telegram stability is required, narrow the promise to “same validated error code and same edge response” and make the detector assert that code explicitly.

## Round-10 disposition

- **Closed:** failed-turn recovery, crash/reopen redelivery, no planner reinvocation, and a failed-recovery-only ablation are now explicit (`docs/PLAN-TGOUT.md:98-106`). The finding above concerns the stronger full-outcome equality claim, not the user-visible recovery path.
- **Closed:** duplicate/alias cases are explicitly repeated through single-fence normalization (`docs/PLAN-TGOUT.md:71-86`). The fenced-duplicate case now detects last-member-wins decoding on that branch.

## Simplification review

The v11 delta is otherwise lean: it adds no dependency or parallel recovery mechanism. The smallest repair is to narrow the equality claim if only the Croatian edge result is required; persist the full typed error only if exact typed recovery is a real contract.

## Weakest link / proof ceiling

The weakest link is durable information sufficiency: the test proves the edge mapping, not equality of the typed failure it claims to recover. This static design review does not prove future RED-to-GREEN implementation behavior or live Telegram rendering.

Minimal Diff: no production change; added only this review report
-> proof: immutable commit/tree/archive/blob binding plus field-level lifecycle and detector trace
-> weakest link: code-only persistence under a full typed-outcome claim
-> skipped: code/test execution and live Telegram probes; upgrade when implementation begins
Proof ceiling: future implementation correctness is not established by plan text
Status: FAIL at 3b43fcb334bf52caedee9f9ca513574c05996dd6
topknot: ultra+preflight
VERDICT: FAIL
