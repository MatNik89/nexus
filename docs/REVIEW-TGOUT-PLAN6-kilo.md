# REVIEW-TGOUT-PLAN6-kilo — design review of PLAN-TGOUT.md v6 (commit f7dad76)

**Scope:** `docs/PLAN-TGOUT.md` at `f7dad76` on `slice/p0-tgout`. Read-only DESIGN
review — no code. Cross-checked against the round-5 findings, the current source, and the
fetched Telegram Bot API docs.

## What v6 correctly resolves

- **Unverified "100 entities" citation (kilo F1) — closed.** The 90-tag budget is now a
  LOCAL renderer-complexity budget with an explicit "the Bot API does not document a
  per-message entity count" declaration (`:105-110`).
- **TOOL_SCHEMA_DRIFT under-specification (kilo F2) — closed.** Fully specified as
  `contracts.TypedError` (Code/Category `VALIDATION`/Retryability `NEVER`/Origin
  `planner`/SafeMessage neutral English), landing TERMINAL-FAILED with a journaled
  `error_code` in `turn.failed`, a `code` field on the UDS frame, and Croatian mapped at
  the telegram edge only (`:66-76`). `TypedError`/`ErrorCategory`/`Retryability` are
  verified against `contracts.go:523-575` and `enums.go:75-115`.
- **Field precedence (codex F5) — closed.** action > tool_id > name with a committed
  conflicting-fields test (`:77-79`).
- **Deviation/table paragraphs — closed.** Hermes' second plain send is now HERMES-ONLY;
  the table contract drops "exactly" and states the lossless deviations (`:41-48`).

## Finding

**F1 [LOW] — the drift message invites "try again", but the declared history interaction
makes "try again" impossible, and the plan doesn't reconcile the two.**
The drift outcome ends in a user message that (per v5, carried forward by "the telegram
handler maps the code to the Croatian user message", `:73`) reads "Pokušaj ponovno ili
preformuliraj" ("try again or rephrase"). At the same time v6 declares "conversation
history folds ONLY turn.succeeded, so a drifted turn never enters history"
(`:74-76`). Because `conversationHistory` only keeps pairs whose `completed` bit is set by
`turn.succeeded`, a `turn.failed` drift excludes the *whole* pair — the user's original
question is dropped too, not just the assistant reply. So a user who answers the
invitation with "try again" arrives with no context, and the model cannot retry. The
"rephrase" half works (the question is restated as the current message); the "try again"
half does not. The plan declares both behaviors but not the tension: either drop "try
again" from the message, or retain a failed turn's user text in history.

## Notes (not findings)

- The history wording "folds ONLY turn.succeeded" under-states the mechanism: the history
  folds `channel.inbound_admitted` (user text) *plus* `turn.succeeded` (final), gated by
  the `completed` bit — which is precisely why the user question is also lost (F1).
- The repl edge's target language is unspecified (`:71-72` "maps WITHOUT string
  classification" — to English SafeMessage or Croatian?); the telegram handler mapping to
  Croatian (`:73`) is inconsistent with the existing English refusal strings
  (`telegram.go:238,246`).

## Contract check (clean)

- S7/planner: one transport call/grant; `TypedError` Retryability `NEVER` → no retry;
  `turn.failed` is terminal; the `error_code` is an additive payload field.
- Delivery: one message per outbox row; render-boundary bool is a pre-wire decision; no
  fallback, no multipart.
- Renderer: local complexity budget with no Telegram-ceiling claim; non-nested
  bold/code/pre (consistent with the Bot API rule that `pre`/`code` are mutually exclusive
  with formatting entities).

## Confidence

Grounded in the round-5 docs, the current source, and the Bot API docs. F1 is a genuine
but LOW UX/design inconsistency between two declared behaviors; the notes are minor. Under
the all-severity rule, F1 is unresolved.

VERDICT: FAIL
