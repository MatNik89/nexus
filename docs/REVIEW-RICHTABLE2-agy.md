# Review of Slice (Round 2): Native Telegram Tables (`richtable`)

**Scope**: Commit `bab40f8` on branch `slice/p0-richtable`  
**Reviewer**: `agy` (Antigravity QA / Architecture Reviewer)  
**Date**: 2026-09-08  
**Base Revision**: `5871a71`  

---

## Executive Summary

Commit `bab40f8` resolves the Round 1 finding regarding lossy transport truncation. `sendRichMessage` is now guarded by `fitsRich(o.Text)` (`len([]rune(s)) <= 32768`), ensuring that outbox payloads are only dispatched via `sendRichMessage` when the entire message fits without truncation.

Transport-local `clip32k` has been eliminated. Over-limit table messages (>32,768 runes) fall through to the existing standard delivery path, preserving strict delivery honesty (B2 / E15 / HARDQ B2) where a message is only marked `SENT` if the complete durable payload was transmitted.

All tests across all 39 packages pass cleanly with **0 failures**.

---

## Standards Review

### Documented Standards Compliance

- **[COMPLIANT] Delivery Honesty & Outbox Leasing (`ARCHITECTURE-ESSENTIALS.md` E15, `HARDQ B2`)**:
  - Outbox entries are never silently truncated before wire transmission. If a payload exceeds rich message bounds, it falls through to standard delivery without partial payload loss.
  - Maintains strict **ONE wire send per flush tick** discipline.
- **[COMPLIANT] Single Ownership (`ARCHITECTURE-ESSENTIALS.md` E5)**:
  - Channel core remains the sole owner of outbox status and attempt tracking.
- **[COMPLIANT] Language Invariant (`AGENTS.md`)**:
  - All code, identifiers, comments, and documentation are in English.

### Baseline Code Smells (Judgement Calls)

- **Zero Smells**: The fix stops at Rung 3 (minimal local code in `telegram.go`), replacing clipping with an exact boundary check (`fitsRich`) without introducing new dependencies or complexity.

---

## Spec & Implementation Review

### 1. Verification of Delivery & Truncation Invariants

| Scenario | Condition | Method Called | Payload Transmitted | Delivery Status Contract |
| :--- | :--- | :--- | :--- | :--- |
| **First Attempt with In-Limit Table** | `hasPipeTable && fitsRich` | `sendRichMessage` | Complete `o.Text` | Complete payload delivered $\rightarrow$ `SENT` |
| **First Attempt with Over-Limit Table** | `hasPipeTable && !fitsRich` | Falls through to HTML / plain `sendMessage` | Complete `o.Text` | Complete payload delivered $\rightarrow$ `SENT` |
| **First Attempt without Table** | `!hasPipeTable` | HTML / plain `sendMessage` | Complete `o.Text` | Complete payload delivered $\rightarrow$ `SENT` |
| **Retry (`Attempts > 0`)** | Any | Plain `sendMessage` | Complete `o.Text` | Complete payload delivered $\rightarrow$ `SENT` |

- **No Truncation**: `clip32k` is completely removed; `sendRichMessage` carries verbatim `o.Text`.
- **Honest Delivery Status**: Only a message transmitted in full can earn `SENT` status.
- **Over-Limit Handling**: Handled losslessly via fallback; dedicated multipart handling remains cleanly decoupled for future extension.

### 2. Empirical Verification

- **Workspace Test Suite**: `go test ./...` passed across all 39 packages.
- **In-Limit Table Delivery**: `TestPipeTableUsesRichMessage` passed.
- **Over-Limit Fallback Detector**: `TestOverLimitTableSkipsRich` passed (verified that 40,000+ rune table avoids `sendRichMessage`).
- **Non-Table Normal Delivery**: `TestNonTableSkipsRichMessage` passed.

---

## Non-Blocking Notes & Observations

1. **`fitsRich` Rune Counting (`telegram.go:342`)**:
   - *Note*: `len([]rune(s)) <= 32768` correctly counts Unicode code points/runes.
2. **Multipart Slice Separation**:
   - *Note*: As noted in comments, multipart chunking for large messages remains deferred to a dedicated future slice.

---

## Findings Table

| ID | Area | Severity | Finding Summary | Source Citation |
| :--- | :--- | :--- | :--- | :--- |
| **N/A** | — | **INFO** | Round 1 finding resolved. `fitsRich` guarantees lossless transmission for `sendRichMessage`, over-limit tables fall through safely, and delivery honesty is preserved. | `internal/channel/telegram/telegram.go:306-343` |

---

VERDICT: PASS
