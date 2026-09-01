PLAN S4

# PLAN-S4 — S4 Tool sustav (registry / exec / edit / pretraga / web / MCP / exposure-budget)

Jezgra `CGO_ENABLED=0`. 4.1/4.2/4.3/4.4/4.7 = MEHANIZAM (mi pišemo); 4.5/4.6 = MEHANIZAM jezgra +
ADAPTER (vanjski alat iza sučelja). **4.2/4.3/4.4 su CODING-KRITIČNE (produbljeno).** Gate:
P1.1 (skill TOCTOU — 4.6/17.1), P1.3 (MCP auth), P2.2 (bounded budgets — 4.2). GORTEX = **Go reuse,
NE port** (najvrjedniji salvаge).

---

## 4.1 Tool registry s typed schemama

**Tip:** MEHANIZAM (jezgra)

**Aspekti → najbolji izvor:**
- type-safe definicija alata dekoratorima → **FastMCP / MCP SDK** (standard)
- alat = funkcija s type hintovima, auto-schema → **smolagents** (harness)
- typed function tools s validacijom → **OpenAI Agents SDK** (harness)

**Sinteza (Go `Tool` interface + auto-schema iz struct tagova + validacija prije dispatcha):**
```go
type Tool interface {
    Name() string
    Schema() *jsonschema.Schema       // auto iz Go struct tagova (smolagents obrazac)
    EffectClass() EffectClass          // READ_ONLY|REVERSIBLE|IRREVERSIBLE (P0.1)
    Execute(ctx context.Context, args json.RawMessage, grant s7.AttemptGrant) (ToolResult, error)
}
type Registry struct{ byName map[string]Tool }
func (r *Registry) Register(t Tool) error      // schema-validate + jedinstveno ime (fail-closed)
func (r *Registry) Dispatch(ctx, call ToolCall) (ToolResult, error) // args validacija → 6.9 before_tool → execute
```
`args` se validira PRIJE execute; nevalidan args → `TypedError{VALIDATION}` bez poziva alata.

**Salvage:** NEXUSv2 `core/tools.py` (PRIMITIVES :70) + `core/toolschema.py` (SCHEMAS :24) →
**Python→Go port**.

**Ugovor/RED:** S0.1 (typed args/result). RED (naš): registracija alata s nevalidnom shemom → reject;
`Dispatch` s args koji krše shemu → `VALIDATION` error, alat NE zvan.

**Verifikacija:** FastMCP (MIT, aktivan), smolagents (Apache-2.0, aktivan), OpenAI Agents SDK (MIT,
aktivan) — stvarni.

**Pareto floor:** dekorator/type-safe definicija (FastMCP) + auto-schema (smolagents) + typed
validation (OpenAI SDK) + EffectClass (naš, P0.1) → ≥ svaki.

---

## 4.2 Shell / izvršavanje koda (coding-kritična)

**Tip:** MEHANIZAM (jezgra; GORTEX membrana = Go reuse)

**Aspekti → najbolji izvor:**
- runtime s bash sesijom koja drži stanje → **OpenHands**
- sandboxirani exec s policy slojem → **Codex CLI**
- minimalan ali potpun shell tool → **gptme**

**Sinteza (stateful session + one-shot, rlimits + process-grupa, sve kroz sandbox granicu):**
```go
type Shell struct {
    sess    *session.Session   // perzistentna bash sesija (OpenHands obrazac)
    sandbox *sandbox.Boundary  // 6.2 granica (Landlock/seccomp — S6, ovdje samo poziv)
}
type ExecOpts struct {
    Cmd string; Cwd string; Env map[string]string // allowlist
    Rlimits RlimitSpec   // CPU/RAM/disk/file/process caps (P2.2)
    Timeout time.Duration; Setpgid bool
}
func (s *Shell) Run(ctx, o ExecOpts) (ExecResult, error)
// ExecResult{stdout, stderr, exit_code, duration} — nikad raw shell bez limita
```
Stateful session (bash koja drži cwd/env između poziva) + one-shot (izolirani komand). Oba idu kroz
GORTEX `tool_executor` — **direktan Go reuse, ne port** (membrana je već Go i već radi rlimits).

**Salvage:** NEXUSv2 `core/subproc.py` (run_isolated :47, _kill_tree :29) + **`gortex/tool_executor.go`
+ `gortex/process_manager.go` = Go REUSE** (najvrjedniji dio — ne port).

**Ugovor/RED:** **P2.2** (bounded budgets enforcement). RED `test_hard_process_budget_kills_
descendants`: tool s `process_count=2` pokrene parent+2 childa → treći → `RESOURCE_LIMIT_EXCEEDED`,
cijelo owned stablo završava; 50MB RAM cap + 100MB alloc → OOM-kill (ne samo log).

**Verifikacija:** OpenHands (MIT), Codex CLI (Apache-2.0), gptme (MIT) — stvarni.

**Pareto floor:** bash sesija (OpenHands) + sandbox policy (Codex) + minimalno (gptme) + rlimits/
process-grupa (naš/P2.2) → ≥ svaki.

---

## 4.3 Uređivanje datoteka (CODING-KRITIČNA — PRODUBLJENO, presudni lever)

**Tip:** MEHANIZAM (jezgra)

**Aspekti → najbolji izvor (razložak edit-pristupa):**
- **VIŠE edit-formata + izbor PO MODELU + benchmark dokaz** (diff/whole/udiff/search-replace) → **Aider**
- **diff-based edit + checkpoint po koraku** → **Cline**
- **patch-apply s auditabilnim tokom** → **Codex CLI**
- **multi-edit / auto-edit agent** → **Mentat** (referent obrazac; NIJE aktivan — samo uzor, ne kandidat)

**Sinteza (multi-format engine + per-model izbor + deterministički LADDER):**
```go
type EditFormat string // WHOLE | DIFF | UDIFF | SEARCH_REPLACE
type EditEngine struct{ formats map[EditFormat]ApplyFunc }

// LADDER — nikad tiho prihvati dvosmislen edit (deterministički, bez pogađanja):
type EditLadder struct {
    exact *editapply.Applier // 1. SEARCH/REPLACE exact — jedan match → primijeni
    fuzzy *editrepair.Repairer // 2. fuzzy — najbliži match (whitespace/indent), ALI:
                              //    više kandidata → REFUSE (ne biraj nasumično)
    sym   *symedit.Engine     // 3. AST/LSP-guided — lokacija simbola, ne text-match (precizno rename/refactor)
}
func (l *EditLadder) Apply(f EditFormat, spec EditSpec) (EditResult, *TypedError)
// fail-closed: ambiguous → TypedError{CONFLICT} + "više kandidata, suzi kontekst"

// format selection PO MODELU (Aider benchmark obrazac): slab model → SEARCH_REPLACE, jak → DIFF
func (e *EditEngine) SelectFormat(model ModelCapability, change Change) EditFormat
```
Atomic: edit ide u staged buffer → verify (3.5) → commit; dirty-tuđi-rad se ne gazi (5.3).

**Salvage:** NEXUSv2 `core/editapply.py` (exact :70) + `core/editrepair.py` (fuzzy :69) +
`core/symedit.py` (AST/LSP :194) → **Python→Go port**; `gortex/diff_engine.go` (:20, :46) =
**Go REUSE** (diff primitiv je već Go).

**Ugovor/RED:** 4.3 spec (edit-format je lever za slab model) + 5.3 (atomic write / dirty-tree).
RED (naš): SEARCH/REPLACE s DVA identična matcha → `EditLadder` REFUSE (ne bira prvi nasumce);
fuzzy match ispod praga sličnosti → REFUSE; symedit rename simbola ne dira identičan tekst u
string/komentaru (AST-scoped, ne text-match).

**Verifikacija:** Aider (Apache-2.0, benchmarkiran), Cline (Apache-2.0), Codex CLI (Apache-2.0) —
stvarni; Mentat = neaktivan (izbačen u ranijoj rundi — samo obrazac).

**Pareto floor:** VIŠE formata + per-model (Aider) + diff-checkpoint (Cline) + auditable patch
(Codex) + **deterministički ladder exact→fuzzy→refuse + AST/LSP symedit** (naš — Aider/Cline/Codex
nemaju symedit ni fail-closed na ambiguous) → ≥ svaki.

---

## 4.4 Aktivna pretraga i navigacija koda (CODING-KRITIČNA — PRODUBLJENO)

**Tip:** MEHANIZAM + ADAPTER (vanjski alati iza `Searcher` sučelja)

**Aspekti → najbolji izvor:**
- industrijska tekstualna/regex pretraga → **ripgrep** (komponenta)
- strukturna AST pretraga → **ast-grep** (komponenta)
- LSP simbolička navigacija kao MCP server → **Serena**
- **dev-inteligencija IZNAD pretrage** (pre-indexirani simboli, call-graph, arhitektura) → **NEXUS**

**Sinteza (slojevita pretraga: text → AST → simbol → semantika → arhitektura):**
```go
type Searcher interface{ Search(ctx context.Context, q Query) ([]Hit, error) }
// Hit{Uri, Line, Col, Snippet, Provenance} — svaki hit je datamarked-untrusted + source_uri

// 4 sloja + dev-inteligencija (NEXUS — iznad pretrage):
type SearchStack struct {
    text  *ripgrepAdapter   // ripgrep exec — regex/tekst
    ast   *astgrepAdapter   // ast-grep — strukturna (prava AST, ne regex na tekst)
    lsp   *serenaAdapter    // LSP simbolička navigacija (definicija/referenca/rename-site)
    index *codeindex.Index  // pre-indexirani simboli (bez ponovnog skena) — NEXUS
    graph *callgraph.Graph  // who-calls-whom — NEXUS
    arch  *archmap.ArchMap  // queryable arhitektura — NEXUS
}
// hit proveniencia: svaki rezultat nosi source_uri + rev + datamark (nikad "sjećam se da je negdje")
```
`ripgrep`/`ast-grep`/`Serena` su vanjski ADAPTERI (exec); `codeindex`/`callgraph`/`archmap` su naš
MEHANIZAM (port) koji daje **dev-inteligenciju** — model ne "traži" nego PITA arhitekturu.

**Salvage:** ripgrep/ast-grep/Serena = exec adapteri; NEXUSv2 `core/codeindex.py` (:311, repo_map
:652) + `core/coderetrieval.py` (:176, :48 _py_chunks) + `core/callgraph.py` (:323) +
`core/archmap.py` (:138) + `core/lsp.py` (:152) → **Python→Go port**.

**Ugovor/RED:** bez dediciranog Annex; gate = freshness/provenance (rezultat NIJE dokaz bez
source_uri). RED (naš): hit bez `Uri`+`Provenance` → odbijen kao evidence (ne smije u prompt kao
"činjenica"); zastarjeli index (rev ≠ HEAD) → `STALE` oznaka, ne tiho.

**Verifikacija:** ripgrep (MIT/Unlicense, aktivan), ast-grep (MIT, aktivan), Serena (MIT, aktivan) —
stvarni; NEXUS codeindex/callgraph/archmap interni (postoje — audit potvrdio).

**Pareto floor:** tekst (ripgrep) + AST (ast-grep) + LSP (Serena) + **dev-inteligencija: index/
callgraph/archmap** (naš — Kilo Code/OpenCode imaju LSP, ali ne queryable arhitekturu) → ≥ svaki.

---

## 4.5 Web i browser automatizacija

**Tip:** ADAPTER + MEHANIZAM (egress granica)

**Aspekti → najbolji izvor:**
- DOM-based kontrola browsera → **browser-use**
- accessibility-tree pristup → **Playwright MCP**
- vision + DOM hibrid za robusnost → **Skyvern**

**Sinteza (ljestvica + egress-everywhere):**
```go
type Browser interface{ Navigate(ctx, url) ; Click(ctx, sel) ; Extract(ctx, sel) ([]Block, error) }
// ljestvica (NEXUS core/web.py obrazac): HTTP text → CDP chromium (egress-gated) → accessibility-tree
// → vision (ZADNJE sredstvo, najskuplje). SVAKI skok prolazi egress policy (6.3) + SSRF guard.
```
Camoufox (runner-up) = SAMO uz eksplicitnu ToS/robots/legal policy jezgre — anti-bot bypass NIJE
neutralan (V-odbijeno kao core cilj); ne uključuje se default.

**Salvage:** NEXUSv2 `core/web.py` (ljestvica) + `core/browse.py` (egress_guard CDP :209) +
`core/websearch.py`/`core/research.py` (:82)/`core/crawl.py`/`core/webextract.py` →
**Python→Go port**.

**Ugovor/RED:** 6.3 (egress — SSRF metadata-IP hard-block). RED (naš): web-tool na metadata-IP
(169.254.169.254) → egress REFUSE prije diala; redirect na privatni IP → re-check, refuse.

**Verifikacija:** browser-use (MIT, aktivan), Playwright MCP (Apache-2.0, aktivan), Skyvern (AGPL,
aktivan) — stvarni.

**Pareto floor:** DOM (browser-use) + accessibility-tree (Playwright) + vision-hybrid (Skyvern) +
egress-everywhere (naš) → ≥ svaki.

---

## 4.6 MCP klijent (vanjski alati bez mijenjanja jezgre)

**Tip:** MEHANIZAM + ADAPTER (P1.3 auth/peer-identity/transport-trust)

**Aspekti → najbolji izvor:**
- extension sustav nativno na MCP-u → **goose**
- MCP marketplace pristup → **Cline**
- MCP klijent u Rust jezgri → **Codex CLI**

**Sinteza (klijent + P1.3 ugovor; tools/list = untrusted):**
```go
type McpPeerDescriptor struct { // P1.3 polja
    ServerID string; Transport Transport // LOCAL_STDIO|REMOTE_HTTPS
    Endpoint string; ExpectedIdentity string // local exec digest ILI remote SPKI pin/issuer
    AuthMethod string; Scopes []string; TrustPolicyVersion string
    AllowedCapabilities []string; MaxToolCount, MaxSchemaBytes, MaxDescriptionBytes int
}
type MCPClient struct{ peers map[string]McpPeerDescriptor }
func (c *MCPClient) Connect(ctx, d McpPeerDescriptor) (*MCPSession, error)
// P1.3: transport-trust (stdio pin / TLS+host pin) → peer-identity → auth → CONNECTED
// tools/list = untrusted ContextBlock → size/provenance/injection fencing prije registracije (4.1)
```
AXI (runner-up): "wrap existing CLI" (gh/exa → kompaktan TOON output) — lakša alternativa punom
MCP serveru; P1.3 auth ugovor vrijedi jednako.

**Salvage:** NEXUSv2 `core/mcp.py` (:65) + `core/mcpserve.py` (:438) + `core/mcpauth.py` (:74) →
**Python→Go port**; `gortex/mcp_server.go` (:19) = **Go REUSE**.

**Ugovor/RED:** **P1.3** + `test_mcp_wrong_pinned_peer_gets_no_credentials` — server vrati valjan TLS
cert za drugo ime, ali ne odgovara pin/issuer → `MCP_PEER_IDENTITY_MISMATCH`, credential sink +
registry ostaju prazni.

**Verifikacija:** goose (Apache-2.0), Cline (Apache-2.0), Codex CLI (Apache-2.0) — stvarni.

**Pareto floor:** MCP extensions (goose) + marketplace (Cline) + Rust klijent (Codex) + P1.3
auth/peer-identity (naš) → ≥ svaki.

---

## 4.7 Tool exposure budget i scoped discovery (jezgra)

**Tip:** MEHANIZAM (jezgra)

**Aspekti → najbolji izvor:**
- najmanji capability-scoped tool-view + lazy schema + miss-rate → **vlastiti ToolView**
- minimal-tool referent (session/compaction je u 8.2) → **pi-mono**
- discovery primitiv (paginacija) → **MCP `tools/list`**

**Sinteza (lazy schema fetch + per-turn min set + capability-scoped):**
```go
type ToolView struct {
    registry *Registry
    budget   TokenBudget // schema-token budžet PO TURNU
    miss     *MissRate   // mjeri selection miss (model zove tool koji NIJE u view-u)
}
func (v *ToolView) ViewFor(caps ModelCapability, intent Intent) []ToolSchema
// capability-scoped: izloži SAMO alate koje model MOŽE pozvati (S0.3 floor) + intent-relevantne
// lazy: schema se dohvaća tek kad je alat u view-u (ne cijeli registry odjednom)
// miss-rate: miss → sljedeći turn proširi view; konzistentan miss → dijagnostika (ne tiho)
```
Princip: "manje alata + scoped visibility diže selekciju slabog modela" (Hipoteza — NIJE dokazana
brojka; matrica).

**Salvage:** NEXUSv2 `core/tools.py` + `core/toolschema.py` + MCP `tools/list` → **Python→Go port**.

**Ugovor/RED:** bez dediciranog Annex (jezgra). RED (naš): schema-token budžet prekoračen → view se
sužava na minimalni set (ne šalje se cijeli registry); alat izvan view-a → model ga NE vidi (miss
zabilježen, ne tiho proširenje).

**Verifikacija:** pi-mono (TS, source-available), MCP tools/list (standard) — stvarni; ToolView je
naš.

**Pareto floor:** scoped view (pi-mono minimalizam) + lazy schema (MCP paginacija) + miss-rate
(naš) → ≥ svaki.

---

## S4 cross-cutting napomene

1. **GORTEX = Go REUSE, ne port:** 4.2 (tool_executor/process_manager) i 4.3 (diff_engine) i 4.6
   (mcp_server) su VEĆ Go — direktno se uključuju kao `internal/gortex` paket. Ovo je najveća
   ušteda u S4 (membrana se NE prepisuje).
2. **Svi tool-rezultati su datamarked-untrusted** (P0.1 ContextBlock trust_class) — 4.4/4.5/4.6
   vraćaju provenience-u, nikad "činjenicu"; 6.5 injection fencing se primjenjuje prije registracije.
3. **Deterministički ladder (4.3) je ključni coding-lever:** fail-closed na ambiguous edit — model
   NIKAD ne smije tiho pokvariti fajl pogrešnim matchom; symedit (AST/LSP) rješava rename/refactor
   koje text-match ne može.
4. **Honest gap:** 4.1/4.3/4.4/4.5/4.7 nemaju dedicirani Annex RED (gate = S0.1 + 5.3 + 6.3 +
   P2.2 posredno). Ako moderator želi simetriju: P0.10 "edit-never-silent-ambiguous-accept".
5. **Verifikacija:** svi javni kandidati stvarni (licence/aktivnost gore); Mentat = neaktivan
   (samo obrazac); Camoufox = ToS-gated; GORTEX + editapply/editrepair/symedit/codeindex/callgraph/
   archmap interni (postoje — audit potvrdio), port/reuse je logika ne ekosustav.
