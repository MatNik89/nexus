# PLAN-TGOUT v16 review — Codex

## Review binding

- Target: commit `df06ed5de9413b8948cd9b60c0e5484ffee6b174`.
- Review source: clean on-disk export at `/home/matej/HARNESS/nexus-plan16-codex-export-df06ed5`.
- Exported `docs/PLAN-TGOUT.md` SHA-256: `b18e225148dac7d2f9a2e2c552c555f82d88464cef9327f0c9c6022ab267def0`, identical to `git show df06ed5:docs/PLAN-TGOUT.md`.
- Scope: design only; no production code or tests executed or changed.

## Finding

[CONCRETE][SEV: LOW] docs/PLAN-TGOUT.md:248 - The revised parse-400 risk statement still promotes a conditional detector result into an unconditional delivery claim. It says the rejected rendered send "delivers PLAIN on the next existing flush tick" and calls the detector a bound proving eventual delivery. The current state machine only makes the row eligible for another attempt: `Core.Flush` parks it UNKNOWN, invokes the callback, marks SENT only when that callback succeeds, re-pends it on a definite error, and leaves it UNKNOWN on an ambiguous error (`internal/channel/channel.go:488-537`). This matches the higher-ranked contract's at-least-once remote boundary, not guaranteed eventual remote acceptance (`docs/HARDQ-CONSOLIDATED.md:53-59`). A fake Bot API that returns parse-400 once and accepts the next request proves the plain-byte selection and that scripted two-call path; it cannot prove delivery on the next tick when that plain send can independently fail or become ambiguous.

PROBE: Static end-to-end state-machine trace from the v16 risk claim (`docs/PLAN-TGOUT.md:248-253`) through the planned parse-400 detector (`docs/PLAN-TGOUT.md:197-205`), the existing tick (`internal/channel/telegram/telegram.go:301-315`), and the authoritative outbox transitions (`internal/channel/channel.go:488-537`). Counterexample: rendered attempt returns a definite parse-related 400, the row re-pends, and the next plain callback returns a dial error or ambiguous transport error; that tick does not deliver or reach SENT.

FIX: Say that parse-400 re-pends the row and the next existing flush tick carries/attempts the original plain bytes; state that SENT remains conditional on remote acceptance, and describe the detector as bounding byte degradation plus the scripted successful-next-attempt case rather than proving eventual delivery.

## Five-question check

1. Solves the reported class? **NO.** It removes the formatting-only absolute but retains a next-tick delivery/evidentiary absolute that the existing state machine and detector do not establish.
2. Regression? **NO implementation regression assessed.** This is a plan-only revision; the defect is an overclaim in its risk contract.
3. Detector validity? **NO for the stated eventual-delivery claim.** The detector is valid for rendered-then-plain bytes and SENT under a scripted accepting second response, but not for external liveness or unconditional next-tick delivery.
4. Revert-proof? **YES for byte degradation, not for eventual delivery.** Ignoring the unknown-event count would make the planned detector re-carry HTML and turn RED; no analogous mutation can make a finite fake prove future remote acceptance.
5. Class sweep? **YES.** The relevant producer, Telegram send boundary, outbox state transitions, tick driver, ranked durable-delivery owner, and filed ungranted-attempt seam were traced. The pre-existing S7 seam is explicitly filed and unchanged, so it is not duplicated as a new finding here.

## Topknot challenge

- Shortest correct plan change: a wording-only correction at the existing risk owner; no new mechanism, API, dependency, or test is needed.
- Strongest counterargument: "delivers" can be read as shorthand for the detector's scripted second-call success. That reading does not rescue the explicit phrase "bound proving eventual plain delivery," and the plan is a contract whose failure semantics must remain literal.
- Weakest link: this review is static and does not inspect the future detector implementation; the finding is limited to what the committed design promises.
- Proof ceiling: the review establishes plan/state-machine consistency at `df06ed5`; it does not establish runtime behavior of code that has not yet been implemented.

VERDICT: FAIL
