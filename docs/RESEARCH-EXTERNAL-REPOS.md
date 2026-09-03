# External Repo Research — 2026-09-03

Method: GitHub API recursive file trees + sampled file contents (not READMEs alone). Each repo
judged on code depth vs docs, license, and concrete NEXUS reuse. NEXUS rules: patterns from
anything; code only from MIT/Apache/BSD.

## 1. mattpocock/skills — SUBSTANCE (docs-as-product, and that's the point)

**What it is (code-grounded):** ~40 hand-written Agent-Skills packs (`skills/{engineering,productivity,misc}/<name>/SKILL.md` + companion refs like `DESIGN-IT-TWICE.md`, `PHASE-BOUNDARIES.md`, per-skill `agents/openai.yaml` for Codex compat). 112 .md / 5 .sh — it IS a docs repo, but the docs are the deliverable and they are dense: sampled `diagnosing-bugs/SKILL.md` is ~1700 words of ranked feedback-loop strategy, falsifiable-hypothesis format, per-phase completion checkboxes — no fluff. Also a working `.claude-plugin/` marketplace manifest and a changesets release flow.

**License/activity:** MIT. Pushed 2026-09-03 (today), very active. Listed 246k stars.

**NEXUS use (concrete):**
- P1 skills system: reference corpus for the SKILL.md standard as practiced — minimal frontmatter (`name`, `description`-as-trigger), progressive disclosure via sibling .md files loaded on demand. Test NEXUS's loader against these packs verbatim (MIT ⇒ can vendor as fixtures).
- P1 coding branch: `diagnosing-bugs` (feedback-loop-first debugging) and `codebase-design/DESIGN-IT-TWICE.md` map directly onto evidence-gated completion — evidence = the loop's output, matching NEXUS's "evidence-gated completion" P1 item.
- P2: `agents/openai.yaml` per-skill cross-runtime shim is the cheapest cross-agent-compat pattern seen anywhere.
- Note: NEXUSv2 (Python) already harvested a 4-pack from this author (HARVEST R26 V4); re-harvest for Go NEXUS is content-copy, near-zero cost.

**Verdict: ADOPT-PATTERN** — highest-quality real-world exemplar of the exact skill standard NEXUS P1 commits to, under MIT.

## 2. ComposioHQ/awesome-claude-skills — THIN (SEO/marketing farm)

**What it is:** 2048 files, but ~1667 are `composio-skills/<vendor>-automation/` pairs — sampled one: templated "Automate X via Rube MCP (Composio)" boilerplate, identical skeleton per SaaS vendor, whose only function is funneling users to Composio's hosted Rube MCP endpoint. The rest (document-skills docx/pptx/pdf/xlsx, canvas-design with 82 bundled fonts, slack-gif-creator…) are copies of Anthropic's official skills, not original work. The "awesome list" README is the fig leaf.

**License/activity:** **NO LICENSE file** (API reports null) ⇒ code/content reuse legally off the table entirely. Last push 2026-08-10.

**NEXUS use:** none. The only original content is vendor-lock boilerplate; the good content is upstream at anthropics/skills (get it there, properly licensed).

**Verdict: SKIP** — unlicensed, auto-generated vendor funnel; every genuinely useful file exists licensed at its true upstream.

## 3. vercel-labs/agent-browser — SUBSTANCE (real Rust engine)

**What it is:** A genuine browser-automation CLI for agents: Rust CLI + Rust daemon speaking raw CDP (no Node/Playwright at runtime), `cli/` = 5.6 MB of source, ~97 files incl. `native/cdp/{client,chrome,discovery,lightpanda}.rs`, `native/a11y/`, cookies, daemon, inspect_server, plus a `doctor/` subsystem (10 modules). Command model: `snapshot` emits an accessibility tree with stable refs (`@e1`), then `click @e1` / `fill @e2 "…"`; ~100 commands, `--json` output, MCP server mode, and self-describing skills (`agent-browser skills get`). `skill-data/` ships the agent-facing instruction packs.

**License/activity:** Apache-2.0. Pushed 2026-09-03, very active. Listed 42k stars.

**NEXUS use (concrete):**
- P1 web/browser: the strongest architecture candidate. NEXUS is a single Go binary; agent-browser proves the right shape — long-lived daemon owning CDP, thin CLI verbs, a11y-snapshot + deterministic element refs instead of screenshots/selectors. Port the *protocol design* (snapshot/ref grammar, JSON output contract) to Go; or P1-cheap option: shell out to the Apache-2.0 binary from the bwrap sandbox and defer a native Go CDP client to P2.
- P1: `doctor/` module layout (chrome/network/daemon/security/fix) is a clean template for `nexus doctor` around browser deps.
- P1 skills: `skill-data/` shows a tool shipping its own SKILL.md packs — pattern for NEXUS tools self-documenting to the agent loop.

**Verdict: ADOPT-PATTERN** (and candidate ADOPT-COMPONENT as external binary) — permissive, active, and the a11y-ref command model is exactly the agent-browser interface NEXUS P1 needs.

## 4. K-Dense-AI/scientific-agent-skills — SUBSTANCE (but wrong domain)

**What it is:** 163 skills with real depth: 1249 .md + **682 .py**; sampled `skills/scanpy/` = SKILL.md + 4 assets + 5 reference docs + 16 executable pipeline scripts. Follows the full Anthropic layout (`scripts/`, `references/`, `assets/`) rather than prose-only packs. docx/pptx/xlsx dirs mirror Anthropic's official skills (xsd schemas present); the scientific ones (scanpy, pkpd-modeling, pydicom, qutip…) look original. Also `autoskill` (23 .py — skill-generation tooling) and `pi-agent`.

**License/activity:** MIT. Pushed 2026-09-02, active. Listed 42k stars.

**NEXUS use (concrete):**
- P1 skills system: the largest MIT corpus of *scripts-bearing* skills — the hard loader cases (executable `scripts/`, binary `assets/`, multi-file `references/`) that mattpocock's prose packs don't exercise. Use a handful as loader/signing/TOCTOU-hash test fixtures: hashing must cover the whole pack dir, not just SKILL.md.
- P2: `autoskill` worth a skim if NEXUS ever grows skill-authoring tooling.
- Domain content (science) is irrelevant to a personal assistant — zero P0 value.

**Verdict: WATCH** — right format and license, wrong domain; mine it as test corpus when P1 skills loader lands, not before.

## 5. supermemoryai/supermemory — THIN where it matters (open shell, closed engine)

**What it is:** TypeScript monorepo of **clients around a proprietary hosted API**: `apps/web` (416 files, dashboard), `apps/mcp` (MCP server proxying api.supermemory.ai, with auth/RBAC/analytics), `apps/docs`, browser/Raycast extensions, ~10 SDK packages (openai/pipecat/cartesia/python), `packages/memory-graph` = a canvas *visualization* widget (spatial index, hover popovers), not a memory engine. The actual ingestion/retrieval/graph engine is NOT in the tree; "run fully locally" resolves to `curl … | bash` installing a **pre-compiled closed binary**. Classic README-overpromise: memory API branding, client-only source.

**License/activity:** MIT (for the clients that are here). Pushed 2026-09-02, active. Listed 29k stars.

**NEXUS use (concrete):**
- Near zero for P0 memory: NEXUS's design (explicit facts, append-only SQLite, FORGET/PURGE, no vector DB) is the opposite of their embedding-graph cloud, and the part that would be instructive is closed.
- Marginal P2: their MCP tool surface (`addMemory`/`search` verb naming, container-tags scoping) is a five-minute API-shape skim if NEXUS ever exposes memory over MCP; profile isolation via container tags rhymes with work/private profiles, but it's an idea, not code.

**Verdict: SKIP** — the memory engine NEXUS could learn from is proprietary; what's open is SaaS client plumbing NEXUS doesn't need.

## Ranked shortlist for deeper full-code audit

1. **vercel-labs/agent-browser** (P1 web/browser): audit `cli/src/native/cdp/client.rs` + `a11y/` — the snapshot→ref algorithm (which a11y nodes get refs, ref stability across mutations), daemon lifecycle/socket protocol, and `skill-data/core` wording. Decision to make: Go port of the command model vs vendoring the Apache-2.0 binary behind bwrap.
2. **mattpocock/skills** (P1 skills): read all ~40 SKILL.md frontmatters to fix NEXUS's minimal frontmatter schema; lift `diagnosing-bugs` + `implement` + `code-review` as the seed pack for the coding branch; copy the `.claude-plugin/marketplace.json` shape for signed skill-pack distribution metadata.
3. **K-Dense-AI/scientific-agent-skills** (P1 skills, later): pick 3 scripts-heavy skills (scanpy, pkpd-modeling) as loader/signing test fixtures; skim `autoskill`.

Composio and supermemory: no audit warranted.

---
Confidence: repo structures, licenses, activity, and the quoted samples (mattpocock SKILL.md, Composio boilerplate, supermemory README claims, agent-browser README/tree) are verified via API/fetch today. Not verified: agent-browser's Rust internals quality (tree + README only — that's what the shortlist audit is for), K-Dense script correctness (layout verified, science not judged), exact star counts (as reported by API).
