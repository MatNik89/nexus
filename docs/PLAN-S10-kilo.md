PLAN S10

# PLAN-S10 — S10 Retrieval RAG (flagovi `vector-retrieval` / `graph-retrieval` / `preload-cache`)

Jezgra `CGO_ENABLED=0`. **Podjela:** 10.1–10.4 = ADAPTER (vanjski engine iza sučelja + primarni pick,
NE spajamo baze); 10.5/10.6/10.7 = MEHANIZAM (decision-rule / ACL-filter / quality-gate su jezgra).
Gate: 10.6 (ACL-prije-candidate-generation, deny-default), codex#19/P2.4 (tenant granica — 8.4 vlasnik),
P0.1 (lineage kroz ingest/chunk/retrieve). `vector-retrieval` je flag; `graph-retrieval` = 10.4;
`preload-cache` = 10.5.

---

## 10.1 Ingestion i parsiranje dokumenata (ADAPTER + MEHANIZAM)

**Tip:** ADAPTER + MEHANIZAM (live-connectors)

**Aspekti → najbolji izvor:**
- najdublje parsiranje layouta (tablice, forme) → **RAGFlow (deepdoc)**
- moderan document converter → **docling**
- širina formata → **unstructured**

**Sinteza (Ingester sučelje + primarni pick + live-connector MEHANIZAM):**
```go
type Ingester interface{ Ingest(ctx, src Source) ([]Document, error) }
type IngesterRegistry struct{ byFormat map[string]Ingester }
// primarni pick: RAGFlow deepdoc (layout/tablice) → docling (moderni) → unstructured (širina)
// PDF: Marker/MinerU (extract) + OCRmyPDF/Surya (OCR sloj) — runner-up adapteri
// ingestion je READ-ONLY (10.7): dokument-MUTACIJA je 13.5 `documents`, NE ovdje

// SurfSense live-connectors (MEHANIZAM): "static RAG = table-stakes, moat = LIVE konektori"
type LiveConnector struct{ endpoint string } // jedan typed REST endpoint po izvoru
func (l *LiveConnector) Fetch(ctx) ([]Document, error) // svjež dohvat, ne samo indeks
// isti izvor izložen i kao MCP tool (svjež dohvat na zahtjev, ne stale snapshot)
```

**Salvage:** NEXUSv2 `core/ingest.py` (:184) + `core/pdf.py` (:269) + `core/mineru.py` (:65) +
`core/webextract.py` (:196) → **Python→Go port**.

**Ugovor/RED:** 10.1 spec (read-only + live-connectors). RED (naš): ingest dokumenta čuva `lineage`
(source_uri+rev) na svakom chunku (P0.1); dokument-mutacija pokuša ući ovdje → odbij (13.5 vlasnik).

**Verifikacija:** RAGFlow (Apache-2.0), docling (MIT), unstructured (Apache-2.0), Marker/MinerU
(MIT/GPL) — stvarni.

**Pareto floor:** layout-parsing (RAGFlow) + moderan converter (docling) + širina (unstructured) +
live-connectors (naš/SurfSense) → ≥ svaki.

---

## 10.2 Chunking strategije (ADAPTER + MEHANIZAM)

**Tip:** ADAPTER + MEHANIZAM (strategija-izbor po podacima)

**Aspekti → najbolji izvor:**
- najviše strategija (semantic, sentence, hierarchical, parent-child) → **LlamaIndex**
- specijaliziran, brz, samo chunking → **chonkie**
- template-based chunking po tipu dokumenta → **RAGFlow**

**Sinteza (Chunker sučelje + tri strategije na ISTOM corpusu, izbor po mjerenju):**
```go
type Chunker interface{ Chunk(doc Document) ([]Chunk, error) }
// tri strategije (10.2 zahtjev) — usporedi ISTIM corpusom, ne preferencijom:
//   semantic    = cut po cosine-drop susjeda
//   parent-child = child embed + parent kao kontekst
//   contextual  = prepend sažetka (rješava "it"/"the company")
func (r *ChunkerBench) Pick(corpus []Document) Chunker // mjeri recall/downstream, izaberi
```

**Salvage:** NEXUSv2 `core/ingest.py` (chunk_markdown :87) + `core/coderetrieval.py` (_py_chunks :48)
→ **Python→Go port**.

**Ugovor/RED:** 10.2 spec (usporedi strategije istim corpusom). RED (naš): chunk zadržava `lineage`
izvornog dokumenta; contextual chunk sažetak NIJE `SYSTEM` (untrusted → ostaje untrusted, P0.3).

**Verifikacija:** LlamaIndex (MIT), chonkie (Apache-2.0), RAGFlow (Apache-2.0) — stvarni.

**Pareto floor:** strategije (LlamaIndex) + brzina (chonkie) + template (RAGFlow) + benchmark-izbor
(naš) → ≥ svaki.

---

## 10.3 Vektorska + hibridna pretraga s rerankingom (ADAPTER + MEHANIZAM)

**Tip:** ADAPTER + MEHANIZAM (cross-encoder rerank stage)

**Aspekti → najbolji izvor:**
- dense+sparse+hybrid fusion → **Qdrant**
- pipeline s BM25+dense+rerank → **Haystack**
- cross-encoder rerank → **sentence-transformers / BGE** (komponenta)

**Sinteza (Retriever sučelje + file-based v1 + cross-encoder stage + query-rewrite):**
```go
type Retriever interface{ Retrieve(ctx, q Query) ([]Hit, error) }
// hibrid: BM25 (fts) + dense (vec) + RRF fusion (NEXUS memvec.py obrazac)
// cross-encoder rerank stage — ZASEBAN korak nakon retrievala (mjeri recall/latency tradeoff)
// file-based v1: LanceDB / sqlite-rag (bez servera) → Qdrant tek kad treba scale
// query-rewrite PRIJE retrievala (podiže ceiling umjesto prompta)
```
**Pravilo:** adapter = sučelje + primarni pick (Qdrant/Haystack/BGE); NE spajamo baze u jedan
hibrid — svaki backend je adapter, fusion/rerank je naš MEHANIZAM.

**Salvage:** NEXUSv2 `core/memvec.py` (RRF :98) + `core/fts.py` (bm25 :138) + `core/rerank.py`
(cross-encoder :35) + `core/embed.py` → **Python→Go port**.

**Ugovor/RED:** 10.3 spec (mjeri recall/latency, bez neprovjerene "+17%"). RED (naš): cross-encoder
stage promijeni poredak vs pure-vector (dokaži da rerank radi); file-based v1 radi bez servera.

**Verifikacija:** Qdrant (Apache-2.0), Haystack (Apache-2.0), sentence-transformers (Apache-2.0),
LanceDB (Apache-2.0), sqlite-rag (MIT) — stvarni.

**Pareto floor:** hybrid fusion (Qdrant) + pipeline (Haystack) + cross-encoder (BGE) + file-based
v1 + query-rewrite (naš) → ≥ svaki.

---

## 10.4 GraphRAG (odnosi, ne samo sličnost) — `graph-retrieval` flag

**Tip:** ADAPTER

**Aspekti → najbolji izvor:**
- referentna implementacija → **Microsoft GraphRAG**
- jeftinija i brža alternativa → **LightRAG**
- graf + vektor kombinacija → **cognee**

**Sinteza (GraphRetriever sučelje + primarni pick):**
```go
type GraphRetriever interface{ RetrieveGraph(ctx, q Query) ([]Subgraph, error) }
// primarni pick: LightRAG (jeftinija/brža) + NEXUS kg.py za činjenice (multi-hop)
// GraphRAG (Microsoft) = referent kad treba dubina/entitet-graf; cognee = graf+vektor (preklapa 9.3)
```
`graph-retrieval` se aktivira kad "upiti traže ODNOSE, ne samo sličnost" (flag okidač).

**Salvage:** NEXUSv2 `core/kg.py` (facts :134, multi-hop) → **Python→Go port**.

**Ugovor/RED:** bez dediciranog Annex (flag). RED (naš): multi-hop upit ("tko je pisao modul koji
koristi X") → graf vrati putanju odnosa, ne samo slične chunkove.

**Verifikacija:** Microsoft GraphRAG (MIT), LightRAG (MIT), cognee (Apache-2.0) — stvarni.

**Pareto floor:** referentna dubina (GraphRAG) + jeftino/brzo (LightRAG) + graf+vektor (cognee) +
multi-hop činjenice (naš/kg.py) → ≥ svaki.

---

## 10.5 CAG (cache-augmented generation) — `preload-cache` flag

**Tip:** MEHANIZAM (decision-rule); top-3 IMPLEMENTACIJA = UNCLEAR

**Aspekti → najbolji izvor:**
- (nema čistih OSS kandidata — infra je u 8.4 vLLM/SGLang; ona sama NE bira/autorizira/osvježava znanje)

**Sinteza (decision-rule — JEZGRA; implementacija UNCLEAR):**
```go
type CAGDecision struct{}
func (d *CAGDecision) Decide(corpus CorpusStats, authz AuthzInfo) Mode // CAG | RAG | HYBRID
// mali/stabilni/AUTORIZIRAN skup → CAG (preload u KV/prefix stanje)
// velik/dinamičan korpus → RAG; hibrid se bira UPARENIM corpus-evalom (10.7)
// odluka je deterministička + mjerljiva; implementacija (vLLM/SGLang prefix) je 8.4 adapter
```
Top-3 end-to-end OSS kandidat OSTAIE UNCLEAR (spec) — odluka je riješena, implementacija ne.

**Salvage:** NEXUSv2 `core/respcache.py` (exact-match) → **Python→Go port** (samo exact-match temelj,
ne puna CAG).

**Ugovor/RED:** 10.5 spec (decision-rule). RED (naš): mali stabilni korpus → `CAG` (ne RAG); velik →
`RAG`; neautoriziran → `RAG` (CAG zahtijeva autorizaciju — ne preload neautoriziranog).

**Verifikacija:** UNCLEAR (spec-praznina; infra kandidati vLLM/SGLang su u 8.4, ne ovdje).

**Pareto floor:** decision-rule (naš — top-3 je UNCLEAR, pa je odluka jedina stvar koju MOŽEMO
sintetizirati bez izmišljanja) → ≥ (nema kandidata za usporedbu — pošteno prazno).

---

## 10.6 Retrieval authorization i tenant filteri (MEHANIZAM jezgra + ADAPTER)

**Tip:** MEHANIZAM (ACL jezgra) + ADAPTER (engine)

**Aspekti → najbolji izvor:**
- payload filter prije rerankinga → **Qdrant**
- document-level security → **OpenSearch**
- filtriranje u ranking pipelineu → **Vespa**

**Sinteza (ACL PRIJE candidate-generation — deny-default, ne post-filter):**
```go
type ACL struct{}
func (a *ACL) ScopeQuery(principal Principal, q Query) Query
// tenant-filter je DIO retriever query-a (candidate-generation već scoped) — NE post-filter
// deny-default: neautorizirani tenant/principal → 0 candidatea (ne filtrira naknadno)
// isti ACL input dijeli cache (8.4 CacheKey.authz_policy_version) i retrieval (ovdje)
```
Adapteri (Qdrant/OpenSearch/Vespa) samo PROVODE scoped query — politiku drži jezgra.

**Salvage:** NEXUSv2 `core/memory.py` (query(scope) :127) → **Python→Go port**.

**Ugovor/RED:** 10.6 spec (ACL-prije-candidate-generation) + codex#19/P2.4 tenant granica. RED (naš):
cross-tenant retrieval → 0 candidatea (deny-default, ne post-filter leak); principal bez scopea →
0 hitova.

**Verifikacija:** Qdrant (Apache-2.0), OpenSearch (Apache-2.0), Vespa (Apache-2.0) — stvarni.

**Pareto floor:** payload-filter (Qdrant) + doc-level security (OpenSearch) + rank-filter (Vespa) +
ACL-prije-generation (naš — nitko ne scopea query prije candidatea) → ≥ svaki.

---

## 10.7 Retrieval quality, freshness i provenance gate (MEHANIZAM jezgra)

**Tip:** MEHANIZAM (jezgra — RAG-ekvivalent 3.5/16.2)

**Aspekti → najbolji izvor:**
- RAG metrike → **Ragas** (komponenta)
- retrieval benchmark → **BEIR** (benchmark)
- retrieval evaluatori → **Phoenix**

**Sinteza (golden queries + Recall@k + freshness SLA + citat-do-chunka):**
```go
type QualityGate struct{ golden []GoldenQuery }
func (g *QualityGate) Eval(r Retriever) (recallK, MRR, NDCG float64)
// "odgovor se NE smatra grounded ako retrieval nije izmjeren" (10.7)

type Freshness struct{}
func (f *Freshness) Check(hit Hit) FreshnessStatus // source_uri + rev + observed_at → FRESH|STALE
// stale (rev ≠ HEAD) → STALE oznaka, ne tiho; freshness SLA po korpusu

type Grounding struct{}
func (gr *Grounding) Verify(answer Answer, hits []Hit) Grounded
// citat-do-chunka: svaka tvrdnja odgovora MORA citirati chunk (lineage) — inače NE grounded
```
SurfSense live-connectors (10.1) hrane freshness — svjež dohvat umjesto stale indeksa. V7 (3:0 B):
10.7 ostaje zaseban retrieval-grounding owner; generičko CI/ratchet izvršenje je 16.2 (cross-ref).

**Salvage:** NEXUSv2 `core/rerank.py` + `core/fts.py` (metrike) → **Python→Go port**.

**Ugovor/RED:** 10.7 spec (grounding gate). RED (naš): odgovor bez citata-do-chunka → NE grounded;
stale indeks → `STALE` (ne tiho vraća rezultate); golden query ispod Recall@k praga → gate blokira.

**Verifikacija:** Ragas (Apache-2.0), BEIR (Apache-2.0), Phoenix (Elastic-2.0) — stvarni.

**Pareto floor:** RAG metrike (Ragas) + benchmark (BEIR) + evaluatori (Phoenix) + freshness-SLA +
citat-do-chunka (naš — nitko ne veže grounded uz lineage-citat) → ≥ svaki.

---

## S10 cross-cutting napomene

1. **ADAPTER vs MEHANIZAM je stroga granica:** 10.1–10.4 su adapteri (sučelje + primarni pick, NE
   spajamo baze); 10.5/10.6/10.7 su jezgra (decision-rule / ACL / quality-gate). Fusion/rerank/ACL/
   freshness su NAŠ kod — backendovi su adapteri.
2. **ACL je PRIJE candidate-generation (10.6),** ne post-filter — deny-default znači 0 candidatea za
   neautoriziranog, a ne filtriranje naknadno (leak vektor).
3. **10.5 CAG:** odluka je riješena (decision-rule), implementacija UNCLEAR (infra u 8.4) — NE
   izmišljamo top-3; pošteno prazno.
4. **Lineage kroz cijeli S10 (P0.1):** ingest→chunk→retrieve→grounding MORA očuvati `source_uri`+`rev`;
   citat-do-chunka ovisi o tome (odgovor grounded = citira chunk koji ima lineage).
5. **Honest gap:** 10.1–10.5 nemaju dedicirani Annex RED (gate = P0.1 + 10.6/10.7 spec posredno);
   10.6 ima P2.4 tenant granicu, 10.7 ima grounding gate. Ako moderator želi simetriju: P0.14
   "retrieval-never-cross-tenant".
6. **Verifikacija:** svi kandidati stvarni (licence/aktivnost gore); CAG top-3 UNCLEAR; salvage
   `ingest/embed/rerank/kg/pdf/fts/memvec/coderetrieval` interni (postoje — audit potvrdio), port je logika.
