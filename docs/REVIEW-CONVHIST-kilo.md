# REVIEW-CONVHIST-kilo — conversation-history slice (commits 3640982 + 06cc664)

**Scope:** `slice/p0-conv-history`, HEAD `06cc664` (two commits: `3640982` history folding,
`06cc664` role-message pattern + Croatian directive). Read-only QA verification of the
trust/injection surface, cross-identity isolation, replay cost, redelivery/HITL, FIFO,
and RED-capability.

**Method:** reviewed the full diff and surrounding owners (`daemon.go`, `planner.go`,
`loop.go`, `assembler.go`, `channel.go`); traced the trust flow (sourcedBlock →
TrustUser, assembler fencing, loop final/observation separation); ran the three
regressions and two ablations in a clean detached worktree at `06cc664`
(`/home/matej/qa-convhist`); full `go test ./...` green.

## Verification results

**Trust / injection surface — SOUND.** History carries exactly two content classes:
`history_user` (prior user text from `channel.inbound_admitted`, USER trust — the same
trust the *current* message already rides at via `sourcedBlock`), and
`history_assistant` (prior finals from `turn.succeeded`). The r2 fix is the decisive
one: past finals reach the provider as the **assistant** role (`planner.go:61-64`), so
model output is never re-minted as user-trust text — the r1 flattened single-block shape
did exactly that. Tool output is structurally excluded from history: it enters a turn as
fenced `TOOL_TRUSTED`/`UNTRUSTED_EXTERNAL` observation blocks (`loop.go:132-155,375-379`),
and `turn.succeeded`'s final is the model's own reply (`loop.go:258-270`), never tool
output. The splitHistory-drop ablation reproduces the r1 failure (history flattened as
`[USER …]` text into the current prompt) and the test turns RED.

**Cross-identity isolation — no live leak, one latent defect (F1).** The `final`
assignment is gated by an exact `pairs[TurnID]` map lookup whose keys are built only
from exact-identity inbound events, so a foreign identity's final can never be attached.
But the two identity filters are asymmetric (F1 below).

**Replay cost — HONEST.** `conversationHistory` does a full `Journal.Replay(0, …)` per
channel turn — O(events) per turn, O(events·turns) total. The `topknot` ceiling and the
transcript-projection trigger (replay latency visibly lagging a turn) are stated in
code, not hidden.

**Redelivery / HITL — CORRECT.** The current turn is excluded (`tid == current`,
`TurnID == current`), so a redelivered update never sees itself in history. Suspended
turns: the user text is in history, the `approval.turn_suspended` challenge summary is
NOT (it is not a `turn.succeeded` final), and the eventual post-resume final IS — the
history correctly shows the turn as "user asked, no final yet" until resume completes.
No self-injection and no false "final" for a still-pending turn.

**FIFO — CORRECT.** `order` is Replay order (journal_offset, chronological); the cap
keeps the last 12 (`daemon.go:362-364`); zero-padded `%06d` BlockIDs make the
`splitHistory` sort preserve chronology and user-before-assistant within each pair
(`historyBlocks`, `daemon.go:389-406`).

**RED-capability — VERIFIED at reviewer uid.**

| Probe | Result |
|---|---|
| `TestHistoryBlocksBecomeRoleMessages` | GREEN (roles `system,user,assistant,user`, chronology, Croatian) |
| `TestChannelTurnCarriesConversationHistory` | GREEN (incl. cross-identity guard) |
| `TestChatSessionCarriesHistory` | GREEN |
| Ablation: drop `splitHistory` extraction (history through assembler) | RED — `current prompt wrong … [USER hist-…] prior answer` |
| Ablation: drop Croatian directive from `systemPrompt` | RED — `system prompt lost the language directive` |
| `go test ./...` | all packages GREEN |

## Findings

**F1 [LOW] — asymmetric identity filters on the cross-identity boundary.**
`conversationHistory` matches `turn.succeeded` events with
`strings.HasPrefix(TurnID, "turn-chan-"+identity+"-")` (`daemon.go:344`) but matches
`channel.inbound_admitted` with an exact `ChannelIdentity != identity` (`daemon.go:329`).
For identities that contain `-` with a prefix relationship (e.g. `chat-4` vs `chat-4-x`,
possible for the generic string identity, unlike the builtin numeric Telegram chat id),
the prefix check over-matches the other identity's turn ids. Today this is inert only
because the final assignment is gated by the exact `pairs[TurnID]` lookup — but the two
filters disagree on a HARD-rule (B3) boundary and any future refactor that bypasses the
map would silently leak across `-`-prefix identities. Make the succeeded filter exact
(or parse and compare the identity component).

**F2 [LOW] — dead constant.** `const entryCap = 1500` at `daemon.go:314` is unused — the
only live `entryCap` is inside `historyBlocks` (`daemon.go:381-384`). Leftover from the
r1→r2 refactor.

**F3 [LOW] — session history re-feeds the unredacted final; channel history re-feeds the
redacted one.** The channel path reads finals from the journal, where `turn.succeeded`
stores `RedactText(final)` (`loop.go:263`); the session path stores
`final` straight from `RunTurn`, which returns the unredacted `*action.Final`
(`loop.go:270`, stored at `daemon.go:236`). If a tool observation carries a *known* ref
and the model echoes it into a final, the channel path redacts it out of history while
the session path re-transmits it to the provider on the next turn. Redact (or read the
journaled) final for the session history to match.

## Confidence

Provable from code + execution: the role-message split, FIFO ordering, current-turn
exclusion, and both ablations' RED are confirmed; the full suite is green. The three
findings are reproduced by direct inspection (F1/F3) and by grep (F2). No live
cross-identity leak and no untrusted-content entry into history were found — the two
LOW boundaries above are latent/defensive, not exploitable today.

VERDICT: FAIL
