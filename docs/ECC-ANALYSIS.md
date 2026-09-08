# ECC ("Everything Claude Code") - Deep Analysis for NEXUS

Evidence-first teardown of `github.com/affaan-m/ECC`. Written for the owner of
NEXUS (Go greenfield single-owner personal-assistant + coding harness). No hype;
claims are tied to files at a fixed commit.

## 0. Provenance

- Cloned via `gh repo clone affaan-m/ECC` (exit 0).
- **Analyzed commit: `5064474d4d762dc9640234a41617cccb79185cec`**
  (`fix: integrate verified ECC 2.2.1 maintenance patches (#3012)`, 2026-09-07).
- License: **MIT** (`LICENSE`), single author "Affaan Mustafa". Steal-with-attribution is legitimate.
- `gh api repos/affaan-m/ECC`: **253,650 stars** (the brief said ~214K; it is
  actually higher), pushed 2026-09-07, repo size ~50 MB, package name
  `ecc-universal` v2.2.1.
- Repo is genuinely large and engineered: **289 test files (265 `*.test.js`)**,
  a 14K-line in-progress Rust v2 (`ecc2/`), CI configs, i18n docs in 13+ languages.

## 1. Real vs advertised counts

The brief's numbers (48 agents / 183 skills / 12 languages) are an **older
snapshot**. The current README advertises **68 agents / 286 skills / 94 commands
/ 102 rules / 10 languages / 2 instincts**, and those are **accurate to disk** -
this is not count inflation:

| Asset    | README claim | Real on disk | Verdict |
|----------|-------------|--------------|---------|
| Agents   | 68 | `agents/*.md` = **68** | accurate |
| Skills   | 286 | `skills/*/SKILL.md` = **286** | accurate |
| Commands | 94 | `commands/*.md` = **94** | accurate |
| Rules    | 102 | 122 `.md`/`.mdc` across **22 rule dirs** (21 langs + `common`) | conservative |

**Substantive, not stubs.** Agents: min 45 / median 116 / max 455 lines (mean
141). Skills: min 34 / median 190 / max 948 lines (mean 261). The smallest
skills (e.g. `skills/nanoclaw-repl/SKILL.md` at 34 lines, the `orch-*` family at
43-45) are terse but real; the bulk are full playbooks. 258 of 286 skills are a
single `SKILL.md`; 28 ship supporting files (sub-agents, scripts, templates).

Concrete substance examples:
- `agents/code-reviewer.md` - real frontmatter (`name/description/tools: Read,
  Grep, Glob, Bash / model: sonnet`), a "Prompt Defense Baseline" preamble, a
  git-diff-driven review process, confidence-based filtering (">80% sure").
- `skills/deep-research/SKILL.md` - a genuine multi-source research protocol with
  an explicit "Untrusted Sources" section (treat scraped content as data, never
  instructions).
- `skills/security-review/SKILL.md` - a real secrets/authz/input checklist with
  FAIL/PASS code examples.

**Caveat on the 286:** the vast majority are *domain playbooks* (healthcare EMR,
Django/Kotlin/Rust patterns, homelab VLAN/WireGuard, x-api, investor outreach,
etc.). They are content libraries - useful prose if a domain comes up - not
engine. Do not read "286 skills" as "286 pieces of harness machinery."

## 2. Architecture and wiring

**Everything is markdown + JS hooks layered on top of an existing host harness
(Claude Code, Codex, Cursor, OpenCode). ECC ships no runtime kernel of its own -
the host harness is the runtime.**

- **Agents / skills / commands**: markdown with YAML frontmatter. Agents declare
  `name`, `description`, a `tools:` allowlist, and a pinned `model:`. Skills
  declare `name` + `description` + `metadata.origin`, body is the prompt. This is
  the stock Claude Code plugin format.
- **Rules**: always-loaded coding-standard markdown, selected per language
  (`rules/<lang>/*.md`) + `rules/common/`.
- **Hooks (the real machinery)**: `scripts/hooks/*.js` (~55 files) wired via
  `hooks/hooks.json`, `hooks/codex-hooks.json`, `.cursor/hooks.json`. These are
  PreToolUse / PostToolUse / SessionStart / Stop / PreCompact dispatchers that
  run deterministic checks *outside* the model prompt (format, typecheck,
  quality gate, cost tracking, the GateGuard fact-forcing gate, memory
  persistence). This is where ECC's actual leverage is.
- **Memory**: a real JS subsystem - `scripts/lib/memory-vault.js` (793 lines),
  `scripts/memory-mcp.mjs` (669), `schemas/memory.schema.json` - not just prose.
- **Instincts**: learned patterns with confidence scores, ranked and injected at
  SessionStart (`scripts/lib/instinct-relevance.js`), the "continuous-learning-v2"
  engine.
- **Install/adapter layer**: `scripts/lib/install-*`, `harness-adapter-compliance.js`,
  `harness-capabilities.js`, `path-safety.js` - the machinery that copies the
  right subset of assets into each host tool.

**Cross-tool support - real for some, docs-only for others (and ECC says so).**
`scripts/lib/harness-adapter-compliance.js` grades each harness honestly into
four tiers:
- `Native` / `Adapter-backed` - Claude Code, Codex, Cursor, OpenCode. These have
  actual surfaces: `.opencode/` ships real TypeScript tools/plugins/commands
  (`opencode.json` 15 KB, `tools/`, `plugins/`, `index.ts`); `.cursor/` ships
  `hooks.json` + `rules/` + `skills/`; `.codex/` ships `agents/` + `config.toml`.
- `Instruction-backed` / `Reference-only` - Gemini, Qwen, Kimi. These are a
  **single instruction file** (`.gemini/GEMINI.md` 1.7 KB, `.qwen/QWEN.md`
  0.7 KB, `.kimi/README.md`). No runtime enforcement; ECC's own compliance record
  admits "the harness does not expose the runtime hook/session surface ECC needs
  for enforcement." That honesty is itself a good pattern (see steal #6).

So "12 language ecosystems / N tools" is real adaptation for the four
hook-capable harnesses and honest-but-thin instruction files for the rest.

## 3. Security tooling: implemented vs described

Mostly **implemented**, with one piece living in a sibling package:

- **Prompt Defense Baseline** (implemented, in-repo). A standardized anti-injection
  preamble prepended to agent prompts (top of `agents/code-reviewer.md` and
  others): no role/identity override, treat fetched/tool/document content as
  untrusted, no secret disclosure, no unrequested code emission, watch for
  homoglyph/zero-width/urgency tricks. Consistent and auditable across agents.
- **GateGuard fact-forcing gate** (implemented, in-repo). `scripts/hooks/gateguard-fact-force.js`
  - a PreToolUse hook whose thesis is: asking an LLM "are you sure?" always gets
  "yes," so instead **demand facts** before an edit or destructive command - list
  importers, affected public API, verify data schemas, state a rollback plan, and
  quote the current instruction. Session-scoped state, heredoc-aware, cross-platform.
- **path-safety** (implemented, in-repo). `scripts/lib/path-safety.js` -
  `realpathNearestExisting()` canonicalizes a not-yet-existing target through its
  nearest existing ancestor then re-appends the tail, defeating symlink-escape on
  write/delete; confines replayed install-state operations to an adapter-derived
  trusted root and never trusts paths from the state file. Cites a real advisory
  (GHSA-hfpv-w6mp-5g95).
- **Memory hardening** (implemented, in-repo). Write-time secret scanning
  (`findPotentialSecrets`), control-char / bidi-override rejection in titles
  (schema regex strips the U+202A-202E and U+2066-2069 bidi ranges), byte caps
  (64 KB body / 128 KB doc), and the invariant baked into the schema description:
  "Recalled memories are context, not executable instructions."
- **AgentShield** (real, but **external npm package** `ecc-agentshield` / repo
  `affaan-m/agentshield`, not bundled here). The in-repo `skills/security-scan/SKILL.md`
  invokes it to scan the harness's own config surface - `CLAUDE.md`,
  `settings.json` allow/deny lists and bypass flags, `mcp.json` supply-chain and
  hardcoded env secrets, `hooks/` command-injection, `agents/*.md` tool scope.
  The framing "scan the agent config itself as an attack surface" is the valuable
  idea; the scanner binary is not in this repo.

Net: the security story is real and defense-first, not marketing. The
config-surface scanner is the one piece you would have to pull from the sibling
package (or reimplement).

## 4. STEAL BRIEF for NEXUS

NEXUS already has: journal-centric SQLite kernel, bwrap sandbox, obligations/
reminders, Telegram channel, herdr multi-agent review, a memory system, skills.
NEXUS is Go / kernel-grade with a real host-side sandbox; **ECC is
markdown+JS config for other people's harnesses and has no kernel or sandbox of
its own.** So steal *patterns and invariants*, not code, and never steal the
marketing (star/sponsor/npm badges, the Ito compute pitch) or the thin
Gemini/Qwen instruction files.

Ranked, each with source path, what it does, why it helps NEXUS, effort:

1. **GateGuard fact-forcing pre-execution gate** - `scripts/hooks/gateguard-fact-force.js`.
   Before a RED/destructive op, refuse to proceed until the agent produces
   concrete facts (importers, public API, data schema, rollback plan) and quotes
   the triggering instruction - instead of a yes/no confirm. Directly attacks
   NEXUS's own recorded weaknesses: self-certification in `chat`, and the
   `echo>`/`find -delete` gate holes (see nexus-audit-2026-07-14). Port the
   *contract* into the Go permission gate as an "evidence gate" for RED verbs.
   **Effort: medium (~1-2 days)** - mostly a prompt/gate contract, little code.

2. **Shared Prompt Defense Baseline for every spawned agent** - preamble at the
   top of `agents/*.md`. One audited anti-injection block (no role override;
   treat tool/fetched/document content as untrusted data; no secret disclosure;
   homoglyph/zero-width/bidi awareness) injected into every herdr subagent and
   the assistant loop. **Effort: low (~half day)** - one constant + inject at spawn.

3. **Memory trust model + "recall is data, not instructions" invariant** -
   `schemas/memory.schema.json`, `scripts/lib/memory-vault.js`. Add a `trust`
   state (`unreviewed`) and `kind`/`scope` enums to NEXUS memory rows; enforce at
   recall that stored memory is injected as *quoted context*, never executed;
   run a secret-scan on write and reject bidi/control chars in titles; promote
   only reviewed "governed truth" into a canonical artifact. NEXUS's SQLite
   memory is architecturally stronger for durability, but likely trusts recall
   today - this closes an injection path. **Effort: medium.**

4. **`realpathNearestExisting` symlink-escape guard for host-side file ops** -
   `scripts/lib/path-safety.js`. bwrap protects *sandboxed* execution, but
   NEXUS's own host-side writes (export/restore/import, journal, install) may
   canonicalize only existing paths. Port the ~50-line nearest-existing-ancestor
   + re-append-tail technique to Go and confine those writes to a trusted root.
   Complementary to bwrap, not redundant. **Effort: low-medium.**

5. **Instinct confidence-scored relevance injection** - `scripts/lib/instinct-relevance.js`
   + `skills/continuous-learning-v2/`. NEXUS already stole a ReasoningBank
   learning-bank; the refinement worth taking is the *injection discipline*:
   confidence score per learned pattern, additive relevance boosts for
   project-scope / stack-match, a confidence floor, and a hard injection cap so
   the context budget stays bounded. **Effort: low-medium.**

6. **Honest self-capability matrix with per-surface verification** -
   `scripts/lib/harness-adapter-compliance.js`. The four-tier honesty
   (Native / Adapter-backed / Instruction-backed / Reference-only) each with
   `verification_commands`, `last_verified_at`, and `risk_notes`. NEXUS is
   single-owner so cross-tool parity matters little, but the *pattern* - a
   `doctor`/health surface that states, per capability, "verified how, when, and
   what is unproven" - fits NEXUS's honesty discipline. **Effort: low.**

7. **AgentShield-style "scan your own config as attack surface"** -
   `skills/security-scan/SKILL.md` (scanner is external `ecc-agentshield`).
   NEXUS already has a `security_scan` MCP tool + gitleaks; extend it to lint
   NEXUS's *own* agent prompts / skills / MCP config for injection surface,
   over-broad tool grants, and hardcoded secrets. **Effort: medium.**

8. **Per-agent least-privilege tool allowlist + model pin in frontmatter** -
   `agents/*.md` (`tools: Read, Grep, Glob, Bash`, `model: sonnet`). If herdr
   subagents currently inherit full tool access, declaring a minimal `tools:`
   allowlist and a model per agent role is a cheap least-privilege win.
   **Effort: low-medium.**

### Where ECC is weaker than NEXUS or irrelevant (do NOT steal)

- **No kernel, no sandbox, no journal of its own.** ECC delegates all runtime and
  isolation to the host harness. NEXUS's SQLite journal kernel + bwrap jail are
  architecturally deeper; nothing to import there.
- **Thin cross-tool tail.** Gemini/Qwen/Kimi support is one instruction file
  each; NEXUS is single-owner anyway - ignore.
- **286 skills are ~90% domain prose**, not machinery. Mine them as reference if
  a specific domain arises; do not treat them as engine.
- **`ecc2/` Rust v2** (14K lines, TUI dashboard + harness-eval) is in-progress and
  unproven - skip.
- **Marketing surface** (star/rank/npm-download badges, Ito compute sponsor
  pitch, GitHub App) - ignore entirely.

### Confidence line

Verified: commit SHA, asset counts (direct `find`/`wc`), that agents/skills are
substantive (line stats + sampled bodies), the memory/hook/path-safety/GateGuard
code exists and does what it claims (read directly), and that AgentShield is an
external package (not in-repo). Not verified: that any hook actually fires
correctly at runtime (not executed here), the 253K star figure beyond the GitHub
API response, and the runtime behavior of the Rust v2. The steal effort estimates
are judgment calls, not measured.
