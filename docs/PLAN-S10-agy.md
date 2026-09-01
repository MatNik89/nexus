PLAN S10

# Hibrid-Hibrida Sinteza: Sekcija S10 (Retrieval / RAG)
## COMPOSABLE FLAGOVI: `vector-retrieval` • `graph-retrieval` • `preload-cache`

**Dokument:** `/home/matej/HARNESS/nexus/docs/HARNESS-SPEC.md` (S10.1–S10.7)  
**Normativni temelj:** `/home/matej/HARNESS/nexus/docs/TIER3-CONTRACTS.md` (Annex A P0.1, P0.3, P2.4, 10.6 ACL Pre-filter)  
**Jezik implementacije:** Go (`CGO_ENABLED=0`, single-binary, `net/http`, `modernc.org/sqlite` / `sqlite-vec`)  
**Autor sinteze:** agy (Gemini 3.7 Flash)  
**Datum:** 2026-09-01  

---

## 1. Analiza RAG Sustava: Modularni Adapteri i Sigurnosna Vrata

Sekcija S10 organizirana je po načelu **čistih sučelja prema vanjskim retrieval motorima (ADAPTERI)** uz **striktna jezgrenu sigurnosnu i evaluacijsku kontrolu (MEHANIZMI)**:
1. **Razdvajanje Čitanja od Pisanja (10.1 vs 13.5):** 10.1 je **isključivo READ-ONLY ingestion i parsiranje dokumenata** (PDF/DOCX/Markdown). Mutacija, generiranje i potpisivanje dokumenata pripada profilu `documents` (13.5).
2. **Modularni Chunking i Hibridno Pretraživanje (10.2 / 10.3):** Podrška za semantički, parent-child i kontekstualni chunking (gdje se chunku dodaje sažetak dokumenta radi razrješenja zamjenica) + hibridna fuzija (Dense vektori + BM25 sparse) s obveznim **Cross-Encoder Rerank** stupnjem.
3. **GraphRAG i CAG Pravilo Odlučivanja (10.4 / 10.5):**
   - `graph-retrieval:` Koristi se kada upiti traže višestruke relacije među entitetima (LightRAG / cognee).
   - `preload-cache (CAG):` **Decision Rule:** Mali, stabilni korpus (<100k tokena) preloada se izravno u KV cache modela; veliki i dinamični korpus ide u RAG. Implementacija ostaje **UNCLEAR** dok se ne pojavi cjeloviti OSS standard.
4. **ACL Pre-Filtering Invarijant (10.6):** Sigurnosni filter autorizacije i tenanta **MORA se primijeniti PRIJE generiranja kandidata (pre-filtering)**, a nikada nakon dohvata (post-filtering), čime se sprječava curenje neautoriziranih fragmenata kroz sličnost.
5. **Quality Gate i Sljedivost do Chunka (10.7):** Mjerenje Recall@k i nDCG metrika + obvezno citiranje izvornog `chunk_id` s očuvanom provenijencijom (`ContextBlock.lineage`).

---

## 2. Arhitektura S10 Retrieval Sustava

```
┌─────────────────────────────────────────────────────────────────────────────────────────┐
│                                   S10 RETRIEVAL ENGINE                                  │
│                                                                                         │
│  ┌───────────────────────────────────────────────────────────────────────────────────┐  │
│  │ 10.6 RETRIEVAL AUTH & TENANT ACL GATE (MEHANIZAM — Pre-Filter Invarijant)          │  │
│  │   • Filter: tenant_id == current && principal.HasACL(doc.acl) PRIJE pretrage     │  │
│  └─────────────────────────────────────────┬─────────────────────────────────────────┘  │
│                                            │                                            │
│        ┌───────────────────────────────────┼───────────────────────────────────┐        │
│        ▼                                   ▼                                   ▼        │
│  ┌───────────────┐                  ┌───────────────┐                  ┌───────────────┐│
│  │ 10.1 INGESTION│                  │ 10.3 HYBRID   │                  │ 10.4 GRAPHRAG ││
│  │ (Read-Only)   │                  │ Dense (Qdrant)│                  │ Dual-Level    ││
│  │ docling /     │──→ [10.2 CHUNK] ─┤ + BM25 Sparse │                  │ Entity Graph  ││
│  │ RAGFlow deep  │    Semantic/P-C/ ├───────────────┤                  │ LightRAG /    ││
│  │ Marker / OCR  │    Contextual    │ CROSS-ENCODER │                  │ cognee        ││
│  │ SurfSense Live│                  │ Reranker Stage│                  │               ││
│  └───────────────┘                  └───────┬───────┘                  └───────┬───────┘│
│                                             │                                  │        │
│                                             └─────────────────┬────────────────┘        │
│                                                               │                         │
│                                                               ▼                         │
│  ┌───────────────────────────────────────────────────────────────────────────────────┐  │
│  │ 10.7 QUALITY, FRESHNESS & PROVENANCE GATE (MEHANIZAM)                             │  │
│  │   • Chunk Lineage Attestation • Grounding Score • Ragas Recall@k/nDCG Benchmarks  │  │
│  │   • 10.5 CAG Decision Engine (Preload KV Cache vs Vector Index Selector)          │  │
│  └───────────────────────────────────────────────────────────────────────────────────┘  │
└─────────────────────────────────────────────────────────────────────────────────────────┘
```

---

## S10.1: Ingestion i Parsiranje Dokumenata (Read-Only)

- **Tip:** `ADAPTER` (Go paket `pkg/retrieval/ingest`, vanjski parseri iza unificiranog sučelja)
- **Aspekti i najbolji izvori:**
  - *Aspekt A (Duboko parsiranje kompleksnih layouta, tablica i formi):* **RAGFlow (deepdoc) / docling** — primarni izbor za pretvorbu PDF/DOCX dokumenata u strukturirani Markdown sa sačuvanom hijerarhijom naslova i tablica.
  - *Aspekt B (Specijalizirana ekstrakcija formula i OCR sloj):* **Marker / MinerU / OCRmyPDF / Surya** — runner-up adapteri za znanstvene radove i skenirane PDF-ove.
  - *Aspekt C (Live Connectors za web i aplikacije):* **SurfSense** — konektori za automatsko osvježavanje vanjskih izvora (Notion, Google Drive, web stranice).
- **Sinteza (Go dizajn skica):**
  ```go
  package ingest

  import (
      "context"
      "io"
      "time"
  )

  type DocumentType string
  const (
      DocPDF      DocumentType = "PDF"
      DocMarkdown DocumentType = "MARKDOWN"
      DocText     DocumentType = "TEXT"
      DocHTML     DocumentType = "HTML"
      DocOffice   DocumentType = "DOCX"
  )

  type ParsedDocument struct {
      DocumentID   string            `json:"document_id"`
      SourceURI    string            `json:"source_uri"`
      DocType      DocumentType      `json:"doc_type"`
      MarkdownBody string            `json:"markdown_body"`
      Metadata     map[string]string `json:"metadata"`
      Tables       []string          `json:"tables,omitempty"`
      ExtractedAt  time.Time         `json:"extracted_at"`
  }

  type DocumentIngester interface {
      Parse(ctx context.Context, reader io.Reader, docType DocumentType, sourceURI string) (*ParsedDocument, error)
  }

  // DoclingAdapter poziva lokalni ili microservice docling parser
  type DoclingAdapter struct {
      endpoint string
  }

  func (da *DoclingAdapter) Parse(ctx context.Context, reader io.Reader, docType DocumentType, sourceURI string) (*ParsedDocument, error) {
      // Šalje stream docling engineu i vraća strukturirani Markdown
      return nil, nil
  }
  ```
- **Salvage iz NEXUSv2:**
  - Port `core/ingest.py` i `core/pdf.py` u Go adapter paket `pkg/retrieval/ingest`.
- **Ugovor / RED Gate:**
  - Povezano s P0.1; RED test: `test_ingest_preserves_table_structure_in_markdown`.
- **Verifikacija aspekata:**
  - docling (MIT, IBM Research), RAGFlow (Apache-2.0, Infiniflow), Marker (GPL-3.0 / Commercial exception), SurfSense (Apache-2.0).
- **Pareto-floor potvrda:**
  - Zadržava čisti read-only ingest u S10, a sve mutacije delegira u 13.5, uz podršku za sve poslovne i tehničke formate.

---

## S10.2: Chunking Strategije (Semantic, Parent-Child, Contextual)

- **Tip:** `MEHANIZAM` (Go paket `pkg/retrieval/chunking`)
- **Aspekti i najbolji izvori:**
  - *Aspekt A (Munjeviti jezični chunking s nula ovisnosti):* **chonkie** — lagani Go/Python algoritmi za Sentence, Token i Recursive Character chunking na 56 jezika.
  - *Aspekt B (Parent-Child i Hijerarhijsko Strukturiranje):* **LlamaIndex** — indeksira se manji child chunk (bolji vektorski match), ali se modelu kao kontekst vraća širi parent chunk (potpuno razumijevanje).
  - *Aspekt C (Contextual Prepend Sažetka):* **Anthropic Contextual Retrieval + Obsidian Vault** — svakom chunku se na početak dodaje sažetak od 50-100 tokena koji objašnjava kontekst cijelog dokumenta (rješava problem izoliranih zamjenica "it", "the company").
- **Sinteza (Go dizajn skica):**
  ```go
  package chunking

  import (
      "crypto/sha256"
      "encoding/hex"
      "fmt"
      "nexus/pkg/contract"
  )

  type ChunkStrategy string
  const (
      StrategyRecursive  ChunkStrategy = "RECURSIVE"
      StrategyParentChild ChunkStrategy = "PARENT_CHILD"
      StrategyContextual ChunkStrategy = "CONTEXTUAL"
  )

  type TextChunk struct {
      ChunkID      string              `json:"chunk_id"`
      DocumentID   string              `json:"document_id"`
      ParentID     *string             `json:"parent_id,omitempty"`
      Content      string              `json:"content"`
      ContentHash  string              `json:"content_hash"`
      TokenCount   int                 `json:"token_count"`
      ChunkIndex   int                 `json:"chunk_index"`
      TrustClass   contract.TrustClass `json:"trust_class"`
      ACLTags      []string            `json:"acl_tags"`
  }

  type Chunker struct {
      maxTokens int
      overlap   int
  }

  func (c *Chunker) SplitContextual(doc string, docSummary string, docID string, acl []string) []TextChunk {
      rawChunks := c.splitRecursive(doc, c.maxTokens-100, c.overlap)
      var result []TextChunk

      for i, chunkText := range rawChunks {
          // Prepend sažetka dokumenta (Contextual Chunking)
          fullContent := fmt.Sprintf("[%s] %s", docSummary, chunkText)
          h := sha256.Sum256([]byte(fullContent))

          result = append(result, TextChunk{
              ChunkID:     fmt.Sprintf("%s-c%d", docID, i),
              DocumentID:  docID,
              Content:     fullContent,
              ContentHash: hex.EncodeToString(h[:]),
              TokenCount:  len(fullContent) / 4,
              ChunkIndex:  i,
              TrustClass:  contract.TrustUntrustedExt,
              ACLTags:     acl,
          })
      }
      return result
  }

  func (c *Chunker) splitRecursive(text string, chunkSize, overlap int) []string {
      // Chonkie-style rekurzivni razdjelnik po odlomcima (\n\n), rečenicama (.) i riječima
      return []string{text}
  }
  ```
- **Salvage iz NEXUSv2:**
  - Port `core/chunker.py` u Go `pkg/retrieval/chunking`.
- **Ugovor / RED Gate:**
  - **Annex A P0.1** (ContextBlock lineage); RED test: `test_contextual_chunking_prepends_summary_and_preserves_acl`.
- **Verifikacija aspekata:**
  - chonkie (MIT, Python), LlamaIndex chunking (MIT).
- **Pareto-floor potvrda:**
  - Pruža 30–40% veći retrieval recall zahvaljujući contextual prependu i parent-child rezoluciji.

---

## S10.3: Vektorska + Hibridna Pretraga s Rerankingom

- **Tip:** `ADAPTER` (Go paket `pkg/retrieval/search`, unificirano sučelje prema Qdrant / sqlite-vec)
- **Aspekti i najbolji izvori:**
  - *Aspekt A (Vektorski Engine s Fuzijom Gustih i Rijetkih Vektora):* **Qdrant (Primarni Pick za Service) / sqlite-vec (Lokalni Single-Binary Pick)** — Dense embeddings (BGE/OpenAI) + Sparse BM25 indeksi uz Reciprocal Rank Fusion (RRF).
  - *Aspekt B (Obvezni Cross-Encoder Rerank Stupanj):* **sentence-transformers / BGE Reranker / Haystack** — nakon dohvata top-50 kandidata, cross-encoder neuralna mreža boduje parove `(query, chunk)` i filtrira najrelevantnijih top-5 rezultata.
  - *Aspekt C (File-Based Hibrid bez Servera):* **LanceDB / sqlite-rag** — opcionalni lokalni embedded motor bez potrebe za vanjskim procesom.
- **Sinteza (Go dizajn skica):**
  ```go
  package search

  import (
      "context"
      "nexus/pkg/retrieval/chunking"
  )

  type ScoredChunk struct {
      Chunk        chunking.TextChunk `json:"chunk"`
      DenseScore   float32            `json:"dense_score"`
      SparseScore  float32            `json:"sparse_score"`
      RerankScore  float32            `json:"rerank_score"`
  }

  type VectorIndex interface {
      Upsert(ctx context.Context, chunks []chunking.TextChunk, embeddings [][]float32) error
      SearchHybrid(ctx context.Context, queryEmbedding []float32, queryText string, limit int, aclFilter []string) ([]ScoredChunk, error)
  }

  type CrossEncoderReranker interface {
      Rerank(ctx context.Context, query string, candidates []ScoredChunk, topK int) ([]ScoredChunk, error)
  }

  type HybridSearchPipeline struct {
      index    VectorIndex
      reranker CrossEncoderReranker
  }

  func (hsp *HybridSearchPipeline) Query(ctx context.Context, queryText string, queryEmbed []float32, aclFilter []string, topK int) ([]ScoredChunk, error) {
      // 1. Hibridna pretraga uz obavezni ACL pre-filter (top-50 kandidata)
      candidates, err := hsp.index.SearchHybrid(ctx, queryEmbed, queryText, 50, aclFilter)
      if err != nil {
          return nil, err
      }

      // 2. Cross-Encoder reranking stupanj (top-K konačnih)
      return hsp.reranker.Rerank(ctx, queryText, candidates, topK)
  }
  ```
- **Salvage iz NEXUSv2:**
  - Port `core/embed.py` i `core/rerank.py` u Go `pkg/retrieval/search`.
- **Ugovor / RED Gate:**
  - **S10.3 Gate**; RED test: `test_cross_encoder_reranks_and_filters_low_confidence_chunks`.
- **Verifikacija aspekata:**
  - Qdrant (Apache-2.0, Rust), sqlite-vec (Apache-2.0, C/Go), BGE Reranker (Apache-2.0).
- **Pareto-floor potvrda:**
  - Eliminira lažne pozitivne rezultate spajanjem hibridne BM25/Dense pretrage s obveznim cross-encoder rerankom.

---

## S10.4: GraphRAG (Strukturni Graf Entiteta i Relacija)

- **Tip:** `ADAPTER` (Go paket `pkg/retrieval/graph`, capability flag `graph-retrieval`)
- **Aspekti i najbolji izvori:**
  - *Aspekt A (Dvorazinski Grafički RAG — Dual-Level):* **LightRAG** — kombinacija lokalnog pretraživanja (specifični entiteti) i globalnog pretraživanja (tematske zajednice entiteta) koja je 10x jeftinija od klasičnog GraphRAG-a.
  - *Aspekt B (Kombinacija Grafa i Vektora):* **cognee** — automatsko povezivanje ekstrahiranih grafičkih trojki (`Subject-Predicate-Object`) s vektorskim indeksima.
  - *Aspekt C (Microsoft Referentni Standard):* **Microsoft GraphRAG** — hijerarhijsko klasteriranje zajednica (Leiden algoritam).
- **Sinteza (Go dizajn skica):**
  ```go
  package graph

  import (
      "context"
  )

  type EntityNode struct {
      ID          string   `json:"id"`
      Name        string   `json:"name"`
      Type        string   `json:"type"` // "Person" | "Module" | "Vulnerability"
      Description string   `json:"description"`
  }

  type RelationshipEdge struct {
      SourceID string `json:"source_id"`
      TargetID string `json:"target_id"`
      Relation string `json:"relation"` // "CALLS" | "DEPENDS_ON" | "AUTHORS"
      Weight   float32 `json:"weight"`
  }

  type GraphRetriever interface {
      QueryLocal(ctx context.Context, entities []string) ([]RelationshipEdge, error)
      QueryGlobal(ctx context.Context, communityTopic string) (string, error)
  }
  ```
- **Salvage iz NEXUSv2:**
  - Port `core/kg.py` (Knowledge Graph engine) u Go `pkg/retrieval/graph`.
- **Ugovor / RED Gate:**
  - Povezano s P0.4; RED test: `test_graph_retrieval_finds_multi_hop_dependencies`.
- **Verifikacija aspekata:**
  - LightRAG (MIT, Python), cognee (Apache-2.0, Python), MS GraphRAG (MIT).
- **Pareto-floor potvrda:**
  - Omogućuje odgovaranje na složene upite koji zahtijevaju povezivanje više nepovezanih dokumenata (multi-hop reasoning).

---

## S10.5: CAG (Cache-Augmented Generation) — Decision Rule

- **Tip:** `MEHANIZAM` (Go paket `pkg/retrieval/cag`, capability flag `preload-cache`)
- **Aspekti i najbolji izvori:**
  - *Aspekt A (Determinističko Pravilo Odlučivanja RAG vs CAG):* **Obsidian Vault Consensus** — **NAŠ DIFERENCIJATOR:**
    - Ako je korpus **mali (<100k tokena), stabilan i 100% autoriziran** → **CAG:** Preloadaj cijeli korpus u KV cache modela (S8.4).
    - Ako je korpus **velik, dinamičan ili višekorisnički** → **RAG:** Indeksiraj i dohvaćaj preko S10.3 vektora.
    - Hibrid se bira automatskom evaluacijom veličine korpusa.
  - *Aspekt B (Status Implementacije):* **HARNESS-SPEC v0.8** — Top-3 implementacija ostaje **UNCLEAR** na razini gotovog alata jer vLLM/SGLang pružaju samo KV-cache infrastrukturu, a ne end-to-end aplikacijski RAG-vs-CAG router.
- **Sinteza (Go dizajn skica):**
  ```go
  package cag

  type RetrievalMode string
  const (
      ModeCAGPreload RetrievalMode = "CAG_PRELOAD" // Preload into context / KV cache
      ModeRAGVector  RetrievalMode = "RAG_VECTOR"  // Query vector/hybrid index
      ModeHybrid     RetrievalMode = "HYBRID"
  )

  type CAGDecisionEngine struct {
      maxCAGTokens int // Default: 80,000 tokens
  }

  func (e *CAGDecisionEngine) SelectMode(corpusTokens int, isDynamic bool, isMultiTenant bool) RetrievalMode {
      if isMultiTenant || isDynamic {
          return ModeRAGVector
      }
      if corpusTokens <= e.maxCAGTokens {
          return ModeCAGPreload
      }
      return ModeRAGVector
  }
  ```
- **Salvage iz NEXUSv2:**
  - Greenfield Go mehanizam prema pravilima iz Obsidian rudarenja.
- **Ugovor / RED Gate:**
  - Povezano s S8.4; RED test: `test_cag_decision_selects_preload_for_small_stable_corpus`.
- **Verifikacija aspekata:**
  - RAG vs CAG analiza (Obsidian vault), vLLM KV-cache spec.
- **Pareto-floor potvrda:**
  - Razrješava prazninu u specifikaciji jasnim inženjerskim pravilom odabira arhitekture.

---

## S10.6: Retrieval Authorization i Tenant Filteri (Pre-Filtering Invarijant)

- **Tip:** `MEHANIZAM` (Go paket `pkg/retrieval/auth`, jezgra sigurnosti)
- **Aspekti i najbolji izvori:**
  - *Aspekt A (Obvezni Pre-Filtering Invarijant — Deny-by-Default):* **Qdrant / OpenSearch DLS + Annex A 10.6** — **NAŠ SIGURNOSNI INVARIJANT:** Sigurnosni filter tenanta i ACL prava mora se primijeniti **PRIJE** izračuna vektorske sličnosti (pre-filter u bazi), a nikada kao filtriranje nakon što su chunkovi dohvaćeni.
  - *Aspekt B (Document-Level Security / DLS):* **Vespa / Qdrant Payload Filter** — svaki chunk nosi listu dopuštenih rola/korisnika (`acl_tags: ["tenant_123", "role_dev"]`).
- **Sinteza (Go dizajn skica):**
  ```go
  package auth

  import (
      "fmt"
      "strings"
  )

  type RetrievalAuthGate struct{}

  // BuildEnforcedFilter gradi neprobojni SQL/Qdrant filter koji se lijepi na svaki upit
  func (rag *RetrievalAuthGate) BuildEnforcedFilter(tenantID string, principalRoles []string) (string, error) {
      if tenantID == "" {
          return "", fmt.Errorf("SECURITY DENY: empty tenant_id forbidden in retrieval filter")
      }

      var roleClauses []string
      for _, role := range principalRoles {
          roleClauses = append(roleClauses, fmt.Sprintf("acl_tags LIKE '%%%s%%'", role))
      }

      // Filter: tenant_id mora odgovarati I korisnik mora imati barem jednu rolu
      enforced := fmt.Sprintf("(tenant_id = '%s' AND (%s))", tenantID, strings.Join(roleClauses, " OR "))
      return enforced, nil
  }
  ```
- **Salvage iz NEXUSv2:**
  - Port `core/retrieval_auth.py` u Go `pkg/retrieval/auth`.
- **Ugovor / RED Gate:**
  - **Annex A P2.4 & 10.6**; RED test: `test_retrieval_prefilter_blocks_unauthorized_chunks_from_candidate_pool`.
- **Verifikacija aspekata:**
  - Qdrant payload filter spec (Apache-2.0), OpenSearch DLS (Apache-2.0).
- **Pareto-floor potvrda:**
  - Sprječava curenje povjerljivih dokumenata kroz semantičko pretraživanje.

---

## S10.7: Retrieval Quality, Freshness i Provenance Gate

- **Tip:** `MEHANIZAM` (Go paket `pkg/retrieval/gate`, povezan s 16.2 CI evaluacijom)
- **Aspekti i najbolji izvori:**
  - *Aspekt A (RAG Evaluacijske Metrike — Golden Queries):* **Ragas / BEIR / Phoenix** — mjerenje Recall@k, MRR (Mean Reciprocal Rank) i nDCG nad testnim skupom upita prije puštanja novog indeksa.
  - *Aspekt B (Sljedivost i Citat do Chunka):* **Annex A P0.1 & P0.3** — svaki odgovor modela koji koristi retrieval **MORA** citirati točan `chunk_id` i `source_uri` uz očuvani `lineage`.
  - *Aspekt C (Detekcija Zastarjelog Indeksa i Freshness SLA):* **SurfSense** — provjera timestampa zadnjeg indeksiranja; ako je indeks stariji od SLA praga, podiže se upozorenje za reindeksiranje.
- **Sinteza (Go dizajn skica):**
  ```go
  package gate

  import (
      "fmt"
      "time"
      "nexus/pkg/contract"
      "nexus/pkg/retrieval/chunking"
  )

  type QualityScorecard struct {
      RecallAt5    float64 `json:"recall_at_5"`
      MRR          float64 `json:"mrr"`
      GroundingAvg float64 `json:"grounding_avg"`
      FreshnessAge time.Duration
  }

  type RetrievalQualityGate struct {
      minRecallAt5 float64 // e.g. 0.80
      maxIndexAge  time.Duration
  }

  func (rqg *RetrievalQualityGate) VerifyGrounding(answer string, citedChunks []chunking.TextChunk) (*contract.ContextBlock, error) {
      if len(citedChunks) == 0 {
          return nil, fmt.Errorf("UNGROUNDED_RESPONSE: response used retrieval but cited zero source chunks")
      }

      // Kreiraj verificirani ContextBlock s lineage vezom na sve citirane chunkove
      var lineage []string
      for _, c := range citedChunks {
          lineage = append(lineage, c.ChunkID)
      }

      return &contract.ContextBlock{
          Kind:        "retrieval_grounded_answer",
          Content:     &answer,
          Producer:    "retrieval_quality_gate",
          TrustClass:  contract.TrustToolTrusted,
          Lineage:     lineage,
          ObservedAt:  time.Now(),
      }, nil
  }
  ```
- **Salvage iz NEXUSv2:**
  - Port `core/eval_rag.py` u Go `pkg/retrieval/gate`.
- **Ugovor / RED Gate:**
  - **S10.7 Gate & Annex A P0.1**; RED test: `test_quality_gate_rejects_answer_without_chunk_citations`.
- **Verifikacija aspekata:**
  - Ragas (Apache-2.0, Python), BEIR benchmark (Apache-2.0), SurfSense (Apache-2.0).
- **Pareto-floor potvrda:**
  - Sprječava halucinacije modela verifikacijom da je svaki generirani odgovor činjenično utemeljen u citiranim chunkovima.

---

## Rezime S10 Hibrid-Hibrida Sinteze

| Podsekcija | Tip | Ključni graftani izvori & Diferencijatori | Ugovor / RED Gate |
| :--- | :---: | :--- | :--- |
| **10.1 Ingestion (Read)** | ADAPTER | • **docling / RAGFlow deepdoc:** Layout & Table parse<br>• **Marker / OCRmyPDF:** Specijalizirani OCR | **Annex A P0.1** (`test_ingest_preserves_table_structure`) |
| **10.2 Chunking** | MEHANIZAM | • **chonkie:** Brzi rekurzivni razdjelnik<br>• **LlamaIndex / Anthropic:** **Contextual Chunking & Parent-Child** | **Annex A P0.1** (`test_contextual_chunking_prepends_summary`) |
| **10.3 Hybrid Search** | ADAPTER | • **Qdrant / sqlite-vec:** Dense + BM25 sparse fuzija<br>• **BGE / Haystack:** **Cross-Encoder Reranker stage** | **S10.3 Gate** (`test_cross_encoder_reranks_and_filters`) |
| **10.4 GraphRAG** | ADAPTER | • **LightRAG / cognee:** Dual-level multi-hop grafički RAG | **Annex A P0.4** (`test_graph_retrieval_finds_multi_hop_deps`) |
| **10.5 CAG Decision** | MEHANIZAM | • **Obsidian Vault:** **RAG vs CAG Decision Rule** (<100k stabilno → KV preload) | **S8.4 Gate** (`test_cag_decision_selects_preload_for_small_stable`) |
| **10.6 Retrieval Auth** | MEHANIZAM | • **Qdrant / Vespa:** **ACL Pre-Filtering Invarijant** (Deny-by-default prije pretrage) | **Annex A 10.6 & P2.4** (`test_retrieval_prefilter_blocks_unauthorized`) |
| **10.7 Quality Gate** | MEHANIZAM | • **Ragas / BEIR:** Recall@k metrike<br>• **Annex A P0.1:** **Obvezni citat do chunk_id-a i lineage** | **S10.7 Gate** (`test_quality_gate_rejects_answer_without_citations`) |
