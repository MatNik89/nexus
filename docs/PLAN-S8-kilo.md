PLAN S8

# PLAN-S8 — S8 Kontekst management (budžet / kompakcija / repo-map / cache / pruning)

Jezgra `CGO_ENABLED=0`. S8 je MEHANIZAM (mi pišemo). **Invarijanta koja sve povezuje (P0.3):**
`ContextBlock.lineage` se MONOTONO očuva kroz svaku S8 transformaciju; `trust_class`/`sensitivity`
smiju se samo POOŠTRITI, nikad oprati (untrusted → nikad SYSTEM/instrukcija). Gate: P0.3 (provenance),
codex#19/P2.4 (cache tenant granica). 8.3 = coding-diferencijator (archmap).

---

## 8.1 Window budžet i truncation strategija

**Tip:** MEHANIZAM (jezgra)

**Aspekti → najbolji izvor:**
- repo-map skaliran po dostupnom budžetu → **Aider**
- eksplicitna hijerarhija core/recall/archival → **letta (MemGPT)**
- budžetiranje event streama → **OpenHands**

**Sinteza (pressure state + tiered truncation; sažmi povijest, NE baci fajlove):**
```go
type ContextBudget struct{ limit, used int }
func (b *ContextBudget) Pressure() float64 // used/limit; 85% → kompakcija (8.2), ne silent drop

// tiered truncation (NEXUS context.py obrazac):
//   system prompt > aktivni fajlovi > repo-map > povijest (zadnja se sažima)
func (b *ContextBudget) Truncate(blocks []ContextBlock) []ContextBlock
// svaki izbačeni blok zadržava lineage (P0.3) — truncation NE smije oprati provenance
```
Truncation je deterministički (isti ulaz → isti izlaz), ne stohastički drop.

**Salvage:** NEXUSv2 `core/contextbudget.py` (:60) + `core/context.py` (:58) → **Python→Go port**.

**Ugovor/RED:** S0.2 + P0.3. RED (naš): truncation koja izbaci `lineage` ili digne `trust_class` →
`PROVENANCE_DOWNGRADE` (isti P0.3 RED kroz 8.1).

**Verifikacija:** Aider (Apache-2.0), letta/MemGPT (Apache-2.0), OpenHands (MIT) — stvarni.

**Pareto floor:** budžet-skaliran repo-map (Aider) + eksplicitna hijerarhija (letta) + event-stream
budžet (OpenHands) + deterministički tiered + lineage-očuvanje (naš) → ≥ svaki.

---

## 8.2 Kompakcija / summarizacija povijesti

**Tip:** MEHANIZAM (jezgra)

**Aspekti → najbolji izvor:**
- izvorni rad na self-editing memoriji → **letta (MemGPT)**
- condenser komponenta (više strategija) → **OpenHands**
- JSONL session tree + branch-summary → **pi-mono** (hipoteza, kanonski claim ovdje)

**Sinteza (anchored summary + lineage MONOTONO očuvan):**
```go
type Compactor struct{}
func (c *Compactor) Summarize(blocks []ContextBlock) (Summary, error)
// anchored summary — 8 sekcija (NEXUS compact.py :73): cilj / napredak / odluke / greške /
// otvorena pitanja / ključni artefakti / sljedeći korak / ograničenja

// KLJUČNO (P0.3): sažetak NASLJEĐUJE lineage svakog bloka koji sažima:
//   trust_class(sažetak) = MAX(trust_class izvora)   — untrusted ostaje untrusted
//   sensitivity(sažetak) = MAX(sensitivity izvora)   — nikad ne opada
//   lineage(sažetak)      = union(izvori) + summary-id
// → sažetak NE smije "oprati" untrusted sadržaj u SYSTEM/instrukciju (injection vektor)
```
Branch-summary (pi-mono): svaka grana razgovora dobiva svoj summary, merge čuva linije.

**Salvage:** NEXUSv2 `core/compact.py` (anchored_summary :73, prune_observations :113) →
**Python→Go port**.

**Ugovor/RED:** **P0.3** + `test_s0_rejects_provenance_laundering` — untrusted `ContextBlock` kroz
compactor koji skine `lineage` i postavi `trust_class=SYSTEM` → `PROVENANCE_DOWNGRADE`, run ne u
`RUNNING`, model sink 0 poziva.

**Verifikacija:** letta (Apache-2.0), OpenHands (MIT), pi-mono (TS, source-available — hipoteza za
matricu) — stvarni.

**Pareto floor:** self-editing (letta) + condenser-strategije (OpenHands) + branch-summary (pi-mono)
+ **lineage-monotono anchored summary** (naš — nitko ne čuva provenance kroz kompakciju) → ≥ svaki.

---

## 8.3 Ambijentalna reprezentacija codebasea (coding-diferencijator)

**Tip:** MEHANIZAM (jezgra; `coding` flag)

**Aspekti → najbolji izvor:**
- tree-sitter + graph ranking, dokazano na benchmarku → **Aider**
- pakiranje stabla repoa u strukturirani kontekst → **Repomix**
- codebase index s lokalnim embeddinzima → **Continue**

**Sinteza (tree-sitter simboli + PPR ranking + archmap kao queryable arhitektura):**
```go
type RepoMap struct{ index *codeindex.Index; rank *ppr.Ranker; arch *archmap.ArchMap }
func (r *RepoMap) Build(repoPath string) (*Map, error)
// tree-sitter simboli (codeindex) → Personalized-PageRank ranking (NEXUS rank.py) → top-N po budžetu
func (r *RepoMap) Scale(budget int) *Map // Aider obrazac: repo-map raste/smanjuje se po window budžetu

// archmap = NAŠ coding-diferencijator: arhitektura kao QUERYABLE graf (moduli→ovisi→slojevi),
// NE lista fajlova — model PITA arhitekturu ("koji modul posjeduje X"), ne skenira repo
func (r *ArchMap) Query(q ArchQuery) ([]ArchNode, error)
```
Repo-map je AMBIJENT (uvijek u kontekstu, ne "tražim pa pročitam") — svježina (rev) datamarked.

**Salvage:** NEXUSv2 `core/archmap.py` (:138) + `core/rank.py` (PPR) + `core/codeindex.py` (repo_map
:652) → **Python→Go port** (najvrjedniji coding-salvage izvan S4).

**Ugovor/RED:** bez dediciranog Annex; gate = freshness (rev ≠ HEAD → `STALE`). RED (naš): repo-map
bez `rev`/`source_uri` → odbijen kao ambijent (ne "činjenica"); zastarjeli map → `STALE` oznaka, ne tiho.

**Verifikacija:** Aider (Apache-2.0, benchmarkiran), Repomix (MIT), Continue (Apache-2.0) — stvarni;
Graphify/Understand-Anything = runner-up (hipoteza za matricu).

**Pareto floor:** tree-sitter+graph (Aider) + stablo-pakiranje (Repomix) + embedding index (Continue)
+ **archmap queryable arhitektura** (naš — nitko nema arhitekturu kao graf, ne listu) → ≥ svaki.

---

## 8.4 Prompt/inference cache i reuse (API vs lokalni sloj)

**Tip:** MEHANIZAM + ADAPTER (dva odvojena sloja, H12)

**Aspekti → najbolji izvor:**
- (a) `cache-control` breakpoint propagacija → **LiteLLM**
- (b) prefix caching → **vLLM**; RadixAttention → **SGLang** (SAMO lokalno)

**Sinteza (dva sloja + cache-key s identity/policy/content hash — codex#19):**
```go
// (a) API sloj: samo propagira cache-control breakpoint po provideru (BEZ KV memorije kontrole)
type CacheControlPropagator struct{}

// (b) lokalni/self-hosted sloj: prefix/RadixAttention — SAMO kad je provider lokalni (2.4)
// adapter prema vLLM/SGLang; API provideri NE dobivaju KV kontrolu

// CacheKey MORA nositi granice (codex#19 — bez toga cache curi tuđi/zastarjeli sadržaj):
type CacheKey struct {
    Namespace string; TenantID string; PrincipalScopeHash string
    AuthzPolicyVersion string; Sensitivity string
    SourceCorpusVersion string; ModelID, ProviderID string
    PromptTemplateHash, ToolSchemaHash, ContentHash string
    PurgeGeneration int
}
// lookup zahtijeva CIJELI typed key, bez wildcard fallbacka; SECRET → NO_STORE
// policy/corpus/model/prompt promjena → mijenja key; purge atomically bumpa generation (invalidira stare)
```
Dobitak se MJERI (hit-rate/TTFT/cijena) po provideru/modelu — nema univerzalne brojke.

**Salvage:** NEXUSv2 `core/respcache.py` (:67) + `core/promptlayer.py` (prefix breakpoint :20) →
**Python→Go port**.

**Ugovor/RED:** **codex#19/P2.4** + `test_cross_tenant_cache_key_cannot_alias` — tenant A upiše entry,
tenant B isti content/prompt hash ali krivotvori/izostavi tenant dio → `CACHE_SCOPE_MISMATCH`, B dobiva
MISS, nikad sadržaj A.

**Verifikacija:** LiteLLM (MIT), vLLM (Apache-2.0), SGLang (Apache-2.0) — stvarni.

**Pareto floor:** cache-control propagacija (LiteLLM) + prefix caching (vLLM) + RadixAttention (SGLang)
+ **tenant/principal/policy cache-key** (naš — nitko ne scopa cache po principal/policy) → ≥ svaki.

---

## 8.5 Observation pruning: kraćenje izlaza alata NA IZVORU

**Tip:** MEHANIZAM (jezgra; po-alatu typed adapteri)

**Aspekti → najbolji izvor:**
- filter-pa-komprimiraj po alatu (100+ CLI) → **RTK (Rust Token Killer)**
- multi-tier compression + `learn` iz neuspjelih sesija → **Headroom**
- ACI pager (navigacija umjesto dumpa) → **SWE-agent**

**Sinteza (po-alatu `Pruner` registry — TIPIZIRANO, test čuva failures, git-diff čuva hunks):**
```go
type Pruner interface{ Prune(tool string, output []byte) []byte }
type PrunerRegistry struct{ byTool map[string]Pruner }
// RTK obrazac: filter-pa-komprimiraj PO ALATU — ne generički truncation

// test adapter (najvažniji): čuva failure-linije (FAIL/ERROR/assert) + tail, briše passing noise
// git-diff adapter: čuva hunks (+/- linije), briše context-linije — model vidi SAMO promjene
// docker/npm/… svaki ima vlastiti adapter; nepoznat alat → NEXUS crush (content-aware + retrieve escape)

type Crush struct{} // NEXUS core/crush.py :262 — kompresija + retrieve escape hatch (dohvati puni output na zahtjev)
```
Injection: pruned output je i dalje `UNTRUSTED_EXTERNAL` ContextBlock (P0.3) — pruning ne mijenja
trust_class, samo veličinu.

**Salvage:** NEXUSv2 `core/crush.py` (compress :262) + `core/compact.py` (prune_observations :113) →
**Python→Go port**.

**Ugovor/RED:** 8.5 spec (kraćenje NA IZVORU, prije kompakcije). RED (naš): `test` output s 1 failing
assertom → pruned output SADRŽI failing assert + tail (ne izgubi signal); `git diff` s hunks →
pruned SADRŽI hunks, bez context-linija; pruned output zadržava `trust_class=UNTRUSTED_EXTERNAL`.

**Verifikacija:** RTK (source-available), Headroom (source-available), SWE-agent (MIT) — stvarni.

**Pareto floor:** po-alatu filter (RTK) + multi-tier+learn (Headroom) + pager (SWE-agent) + crush
retrieve-escape + provenance-očuvanje (naš) → ≥ svaki.

---

## S8 cross-cutting napomene

1. **P0.3 je S8-in srž:** svaka transformacija (truncation/compaction/pruning) MORA očuvati
   `lineage` i smije samo POOŠTRITI `trust_class`/`sensitivity` — sažetak untrusted sadržaja NIKAD
   ne postaje instrukcija (primarni injection vektor koji kompakcija otvara).
2. **8.3 archmap je coding-diferencijator** (uz 3.1 evidence-graded i 3.5 TIA): model PITA
   arhitekturu umjesto da skenira repo — Kilo Code/OpenCode imaju LSP, ne queryable archmap.
3. **8.4 cache-key je sigurnosna granica, ne optimizacija:** bez tenant/principal/policy hash-a
   cache curi tuđi/zastarjeli sadržaj (codex#19) — API-sloj i lokalni-sloj su ODRVOJENI (H12).
4. **Honest gap:** 8.1/8.3/8.5 nemaju dedicirani Annex RED (gate = P0.3 posredno); 8.2 ima P0.3
   izravno, 8.4 ima P2.4. Ako moderator želi simetriju: P0.12 "pruning-never-drops-failure-signal".
5. **Verifikacija:** svi kandidati stvarni (Aider/letta/OpenHands/Repomix/Continue/LiteLLM/vLLM/
   SGLang/SWE-agent MIT/Apache-2.0; pi-mono/RTK/Headroom source-available — hipoteza za matricu);
   salvage `contextbudget/compact/crush/archmap/rank/respcache/promptlayer` interni (postoje — audit
   potvrdio), port je logika.
