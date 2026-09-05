# P0-prep review round 1 — Kilo (one-commit fix-batch)

Target: `slice/p0-prep1` at HEAD `eb0d6f8` (three fixes on converged P0). Method: code-read, run the suite in the repo, one negative probe in a clean `git archive HEAD` export (`/tmp/kilo/prep1`). Working tree never modified.

## Suite

`CGO_ENABLED=0 go vet ./...` exit 0; `CGO_ENABLED=0 go test -count=1 ./...` all 33 packages `ok` (acceptance 89s, no FAIL anywhere).

## Fix #1 — UNKNOWN outbox reconciliation (verified)

`outbox` lists `chanCore.Unreconciled` (UNKNOWN rows, text truncated to 80 chars) and `redeliver <dlv-id>` calls `chanCore.Reconcile(ctx, id, false)`. Attacks checked:

- **Resurrect SENT/completed**: closed — the `EvOutboundResolved` projection is `UPDATE chan_outbox SET status=? WHERE delivery_id=? AND status='UNKNOWN'` plus a `RowsAffected()==1` guard (`channel.go:283-288`); `Reconcile(false)` transitions only `UNKNOWN→PENDING`. A SENT row matches 0 rows → fail-closed error.
- **Foreign-chat redeliver**: the handler runs only for admitted (bound-chat) messages; the outbox is per-profile (per-profile journal), so no cross-profile row is reachable. A bound chat can re-pend a row destined for another bound chat, but every bound chat is the owner's — not a security boundary.
- **id format**: `deliveryIDRe = ^dlv-[0-9a-f]{24}$` matches the actual delivery id (`dlv-` + 12-byte sha256 hex).
- **listing leak**: `Unreconciled` reads the projection's redacted `text` (redaction at journal append), per-profile — no cross-profile or unredacted content.

## Fix #2 — attestation host binding (verified)

`verifyAcceptanceAttestation` now refuses `att.Host != runtimeHostID()`. `runtimeHostID()` formats `syscall.Uname` as `Sysname + " " + Release + " " + Machine`, exactly `uname -srm` (what `p0-accept.sh` writes). No format divergence on Linux/macOS; an empty/absent `host` fails the comparison (refused). Spoofing is covered by the signature (the host is inside the signed JSON, verified later). `TestDoctorP0GrantLive`'s foreign-host branch covers it.

## Fix #3 — strict command-id routing (one finding)

`approve`/`deny`/`retry` gate on `challengeIDRe` (`^ch-[0-9a-f]{24}$`), `ack` on `occurrenceIDRe`, `redeliver` on `deliveryIDRe`; a non-matching sentence falls through to conversation. The challenge and delivery regexes match their real id shapes exactly (`ch-`/`dlv-` + 24 hex). No catastrophic backtracking (linear patterns); case-insensitive command word, case-sensitive id (correct — ids are generated lowercase). No command injection via fall-through (it is ordinary conversation).

### Finding 1 — [LOW] `occurrenceIDRe` is stricter than the actual reminder-id alphabet

`occurrenceIDRe = ^occ-[A-Za-z0-9_.-]+#[0-9]+$`, but the reminder id it embeds is **not** constrained to that alphabet: `reminder_set` (`obligation/tools.go:75`) checks only `id != ""`, and `CreateReminder`→`schedule.CreatedParams` (`schedule.go:320`) likewise checks only `id != ""` — the `[A-Za-z0-9_-]` slug check (`taskIDOK`, `obligation.go:718`) is applied to **tasks only**, not reminders.

Consequence (probe `TestProbeOccurrenceRegexVsReminderIDAlphabet`, clean export): a reminder whose model-generated id contains a space, `#`, unicode, or `/` — e.g. `id="my reminder"` → occurrence `occ-my reminder#1` — is accepted by `CreateReminder`, delivered, but its `ack occ-my reminder#1` command does **not** match the regex and silently falls through to conversation. The `reminder_ack` TOOL (model path, no regex) still acks it, so it is recoverable, and it is fail-closed (nothing wrong happens) — hence LOW, not a security hole. Fix: apply the same slug alphabet to reminder ids, or widen the occurrence regex to the true unconstrained shape.

## Verdict

Fixes #1 and #2 are correct and causal (projection-guarded reconcile, host binding covered by the signature). Fix #3 is correct except for one LOW latent inconsistency between the occurrence regex and the unconstrained reminder-id alphabet — not a confirmed broken/security finding.

VERDICT: PASS
