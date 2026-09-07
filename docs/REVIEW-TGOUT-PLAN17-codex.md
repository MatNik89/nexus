# PLAN-TGOUT v17 review — Codex

## Review binding

- Target: commit `6e27dfbf7a89874a54a7b4e1aa37708bbb8ce4a7`.
- Review source: clean on-disk archive export at `/home/matej/HARNESS/nexus-plan17-export`.
- Export integrity: all 456 tracked blobs hash-identically to the target tree; exported `docs/PLAN-TGOUT.md` SHA-256 is `fc0c0231fb67eb35b21e9d413fcd7440c74c5bd9dfb6d4a0c497b3e78f6b5eae`, identical to `git show 6e27dfb:docs/PLAN-TGOUT.md`.
- Scope: adversarial design review only. No production code or tests were executed or changed.

## Finding disposition

No unresolved design flaw was found.

The sole round-16 LOW is closed. The v17 risk statement now limits the guarantee to the bytes selected for the next already-scheduled attempt: the next flush tick carries the original plain bytes, while `SENT` remains conditional on remote acceptance and the plain attempt may fail or become ambiguous (`docs/PLAN-TGOUT.md:248-257`). That is consistent with the governing at-least-once boundary, under which a non-transactional remote API cannot provide an exactly-once or eventual-acceptance guarantee and sent-but-unrecorded outcomes remain `UNKNOWN -> RECONCILING` (`docs/HARDQ-CONSOLIDATED.md:53-61`; `docs/ARCHITECTURE-ESSENTIALS.md:196-205`).

The detector claim is now proportionate to its oracle. The parse-400 case proves that the existing machinery re-pends the row, that the subsequent scheduled attempt carries the durable original bytes without `parse_mode`, and that a scripted accepting response can land `SENT`; it no longer claims that a finite fake proves future remote acceptance (`docs/PLAN-TGOUT.md:197-206,248-257`). The counterexamples are explicitly admitted: a definite failure on the plain attempt returns the row to `PENDING`, while an ambiguous attempt leaves it `UNKNOWN`; neither outcome is mislabeled as delivered.

## Contract and class sweep

1. **Reported class:** PASS. Known tool-schema drift is structurally separated from prose, persists a typed failed outcome for restart-stable edge rendering, and never enters successful conversation history (`docs/PLAN-TGOUT.md:55-166`). Outbound formatting preserves original durable bytes and changes only the bytes carried by an existing delivery attempt (`docs/PLAN-TGOUT.md:168-206`).
2. **Regression risk:** PASS at design level. The plan preserves the journal as canonical owner, uses a projection fold for the lease count, and requires a version bump plus replay rebuild rather than a parallel mutable counter (`docs/PLAN-TGOUT.md:184-206`; `docs/ARCHITECTURE-ESSENTIALS.md:62-75`).
3. **Detector validity:** PASS. Positive execution, duplicate/alias, live/recovered edge, mixed lifecycle, restart, projection rebuild, parse-400, dial-failure, length/tag boundaries, and causal ablations are all named at the relevant boundaries (`docs/PLAN-TGOUT.md:137-166,197-234`).
4. **Revert-proof:** PASS in the plan. Ignoring the lease count re-carries HTML; dropping the event fold loses restart behavior; removing failed recovery or persisted tool identity breaks the named recovery cases. Each mutation is required to turn its retained detector RED (`docs/PLAN-TGOUT.md:144-150,197-206`).
5. **Class sweep:** PASS. The plan covers the planner parser, loop-owned typed carrier, canonical failed event, daemon recovery fold, Telegram edge mapping, outbox projection, send boundary, restart rebuild, and the existing tick path. The pre-existing missing S7 grant on tick-driven delivery is explicitly isolated as unchanged backlog work, not concealed as part of this slice (`docs/PLAN-TGOUT.md:104-150,176-206`; `docs/tasks-P0.md:464-471`).

## Adversarial challenge and weakest points

No item below is an unresolved plan defect; each is a future implementation guardrail with an explicit detector in v17.

1. The first-lease decision must use the lease count captured before the current `outbound_unknown` park. Reading the projection after parking would observe one and suppress HTML on the first attempt. The fake-bot first-attempt assertion and ignore-count ablation must bind this timing (`docs/PLAN-TGOUT.md:184-206`).
2. Projection version migration is the weakest persistence seam: the new count must be derived exclusively from canonical `channel.outbound_unknown` events and survive a close/reopen and old-version rebuild. The restart, version-rebuild, and drop-the-event-fold detectors directly cover it (`docs/PLAN-TGOUT.md:194-206`).
3. The renderer's strongest hostile input is text that simultaneously resembles fences, inline code, bold, and a pipe table near the 4096-UTF-16 and 90-span boundaries. The precedence, no-overlap, escaping, empty-span, pipe, and boundary detectors are sufficient design controls, but runtime correctness remains unproved until those tests and their ablations exist (`docs/PLAN-TGOUT.md:207-234`).

Topknot simplification pass: Lean already. The token walk is required to observe duplicate members without last-member-wins decoding; the unified recovery fold removes competing replay paths; the projection reuses the canonical event stream; and this slice adds no retry scheduler, dependency, or multipart machinery.

## Proof ceiling

This review proves internal consistency of the committed design and closure of the round-16 overclaim at `6e27dfb`. It does not prove code or test behavior that has not yet been implemented, Telegram service liveness, or the separately filed S7 ownership repair for the existing tick-driven delivery pump.

VERDICT: PASS
