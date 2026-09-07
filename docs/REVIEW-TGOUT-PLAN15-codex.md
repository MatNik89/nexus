# PLAN-TGOUT v15 design review — Codex

## Artifact binding

- Reviewed commit: `87c2643754b147d7aac1cd715ebd2df60474aa60`.
- Clean export: `/home/matej/HARNESS/nexus-plan15-codex-5Fo8wNTN` (on disk, outside `/tmp`).
- `docs/PLAN-TGOUT.md` SHA-256 in the export and from `git show 87c2643:docs/PLAN-TGOUT.md`: `eedbf67320ba9609f063d64fc6be5e7cb332a1524564e5e839dd0d965aa84c2d`.
- Required marker verified at `docs/PLAN-TGOUT.md:126`: `NON-DRIFT FAILED CLASS`.
- Review mode: design-only and read-only except for this requested report; no code or tests were changed.

## Round-14 closure

1. **Non-drift `turn.failed`: closed.** `RecoveredOutcome{Kind: FAILED, Code: ""}` records that the turn is terminal without inventing a renderable response, preserves today's collision-error behavior, and prevents an earlier suspension from resurfacing. The simple ordinary-failure and resumed-then-ordinary-failure collision detectors cover both required observable paths (`docs/PLAN-TGOUT.md:114-136`).
2. **Within-tier precedence: closed.** First registered-tool occurrence in source token order is explicit for case-alias duplicates, while the pre-existing cross-field precedence remains `action > tool_id > name`. The bare and single-fenced detectors require the same winner across the structural error, durable payload, live edge, and recovered edge (`docs/PLAN-TGOUT.md:137-153`).

## Finding

[CONCRETE][SEV: LOW] `docs/PLAN-TGOUT.md:248-250` — The claim that a renderer bug "can only cost FORMATTING" and cannot affect delivery semantics contradicts the plan's own parse-400 path. With `renderHTML` returning true, Telegram can reject the rendered request; the first scheduled send then delivers nothing, and the durable row is only re-pended for a later flush (`docs/PLAN-TGOUT.md:197-200,225-233`). Production confirms that a non-2xx 400 returns an error (`internal/channel/telegram/telegram.go:160-167`), `Core.Flush` re-pends it rather than sending plain in the same tick (`internal/channel/channel.go:514-535`), and the next opportunity is a later ticker iteration (`internal/channel/telegram/telegram.go:301-315`). Therefore the renderer can add delivery latency and, if no later flush occurs, leave the reply pending; only the durable content remains independent of rendering.

PROBE: Static end-to-end trace of the planned fake-bot parse-400 case through the exact committed send, re-pend, and ticker paths at commit `87c2643`.

FIX: Rewrite the risk statement to say that a render rejection can defer delivery until an existing later flush, while preserving the original durable bytes; keep the committed parse-400 detector as the bound proving eventual plain delivery after that later flush.

## Topknot challenge

The proposed implementation remains a small extension of the existing planner, journal fold, projection, and Telegram send boundary; no avoidable new dependency or single-use abstraction is required. The weakest link is the overstated rendering failure claim above, not the two v15 fold changes.

Proof ceiling: This was a design review. It proves internal consistency against the committed owners and current production seams, not implementation correctness or live Telegram behavior.

VERDICT: FAIL
