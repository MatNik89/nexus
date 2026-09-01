PLAN S10

# S10 — Retrieval / RAG (`vector-retrieval`, `graph-retrieval`, `preload-cache`)

## Zajedničke odluke i paket-layout

- S10 jezgra definira canonical document/chunk/query/result/citation ugovore, ACL pushdown i grounding gate. Parser, chunker, vector engine, reranker i GraphRAG su **zamjenjivi adapteri**; za jedan corpus generation bira se po jedan adapter po fazi. Ne spajamo Qdrant+LanceDB+sqlite-rag niti GraphRAG+LightRAG u novu bazu.
- Predloženi layout: `internal/retrieval/{contract,authz,planner,quality}` te `internal/adapters/{ingest,chunk,vector,rerank,graph,connector}`. Adapteri se registriraju kroz S0.5 closure i S6.8 supply-chain gate. Python/Java/Rust engine može biti lokalni sidecar ili remote service; Go binary nikad ga ne importira u kernel niti pretpostavlja da je dostupan.
- Flag ownership: `vector-retrieval` aktivira 10.1–10.3+10.6+10.7; `graph-retrieval` zahtijeva ingestion/provenance, ali bira zaseban 10.4 adapter; `preload-cache` aktivira 10.5 i zahtijeva S8.4+P2.4. Composable flagovi ne aktiviraju jedan drugoga osim eksplicitnog S0.5 closurea.
- P0.1 normativno posjeduje `ContextBlock` lineage/trust/sensitivity i zahtijeva monotono očuvanje kroz S10. Annex **nema** poseban ACL-before-retrieval ugovor: deny-default filter-pushdown u 10.6 zato mora imati vlastiti RED gate. Annex P2.4 izravno vrijedi za query/result/preload cache.
- Ingestion je read-only prema izvornom sustavu. Source connector smije dohvatiti snapshot/deltu i checkpointati cursor, ali document mutacija pripada 13.5. S7 posjeduje retry/deadline/cancel svih adapter poziva.

```go
package retrieval

type DocumentRevision struct {
    DocumentID, RevisionID, SourceURI, SourceConnectorID string
    TenantID, PrincipalScopeHash, AuthzPolicyVersion     string
    MIMEType, ContentHash, MetadataHash                  string
    ObservedAt, SourceModifiedAt                         time.Time
    Sensitivity contracts.Sensitivity
    Trust       contracts.TrustClass
    ACL         ACLDescriptor
    Blocks      []contracts.ContextBlock
}

type Chunk struct {
    ChunkID, DocumentID, RevisionID, ParentChunkID string
    Ordinal                                        uint64
    Block                                          contracts.ContextBlock
    StartAnchor, EndAnchor                         SourceAnchor
    ChunkerID, ChunkerVersion, ConfigHash          string
    ACL                                            ACLDescriptor
}

type AuthorizedQuery struct {
    QueryID, TenantID, PrincipalID, PrincipalScopeHash string
    AuthzPolicyVersion, CorpusVersion, Purpose          string
    Text                                                string
    Limit, CandidateLimit, TokenBudget                  uint64
    Filter                                              CompiledACLFilter
    AsOf                                                *time.Time
}

type Candidate struct {
    Chunk                 Chunk
    Scores                map[string]float64
    RetrieverID           string
    CorpusVersion         string
    ACLDecisionEvidenceID string
}

type Retriever interface {
    Search(context.Context, AuthorizedQuery) ([]Candidate, RetrievalReceipt, error)
}
```

Globalne invarijante:

1. Adapter dobiva typed, već autoriziran request s obveznim tenant/policy filterom; raw query bez `CompiledACLFilter` nema `Search` put. Adapter response ponovno se provjerava kao containment detector, ali post-filter nije zamjena za pre-filter.
2. Svaki chunk/candidate/citat veže exact `document_id+revision_id+content_hash+source anchor` i tranzitivni P0.1 lineage. Parser/chunker/embedder/reranker ne smiju povisiti trust niti sniziti sensitivity.
3. Corpus generation je immutable. Novi ingest/chunker/embedder/ACL policy gradi novu generaciju i atomically mijenja active pointer tek nakon 10.7 gatea; partial index nikad nije aktivan.
4. Derived index nije source of truth. Brisanje/rebuild vektora, graph edgeova, cachea ili eval projekcije ne smije ukloniti document revision/artifact.
5. Svaki adapter ima bounded input/output, S7 grant i S6 sandbox/egress policy. Adapter-internal retry mora biti isključen; timeout ne prebacuje corpus generation u `READY`.

## 10.1 Ingestion i parsiranje dokumenata

- **Tip:** ADAPTERI + TANKI MEHANIZAM INGEST LEDGERA

- **Aspekti:** RAGFlow/deepdoc je najbolji za kompleksni PDF layout, tablice i forme; Docling za moderan širok document→structured representation converter; Unstructured za širinu konektora/formata. Marker i MinerU su specijalizirani PDF→Markdown/layout alternative, OCRmyPDF/Surya OCR lanes. **Primarni pick:** Docling adapter za opću bogatu ekstrakciju; RAGFlow/Marker/MinerU su konfiguracijske alternative po MIME/use-caseu, nikad kaskadna “prva koja nešto vrati” baza bez eksplicitnog policyja.

- **Sinteza:** Go `Ingester` koordinira immutable source snapshot, P0.1 classification, size/resource preflight, jedan odabrani `DocumentParser` adapter i atomic ingest ledger. Baseline single-binary podržava bounded UTF-8 text/Markdown i NEXUS-derived jednostavni PDF text fallback; `DoclingParser` je primarni rich adapter iza lokalnog subprocess/HTTP protokola. Typed `SourceConnector` daje snapshot ili delta+cursor; isti connector descriptor može izložiti read-only REST endpoint i MCP tool, ali oba pozivaju isti owner.

```go
type SourceConnector interface {
    Descriptor(context.Context) (ConnectorDescriptor, error)
    Snapshot(context.Context, SourceCursor, IngestFilter) (DocumentStream, SourceCursor, error)
}
type DocumentParser interface {
    Capabilities() ParserCapabilities
    Parse(context.Context, RawDocument, ParsePolicy) (ParsedDocument, ParseReceipt, error)
}
type Ingester interface {
    Plan(context.Context, SourceSnapshot, ParserSelection) (IngestPlan, error)
    Commit(context.Context, IngestPlan, ParsedDocumentStream) (CorpusGeneration, error)
}
type ParseReceipt struct {
    ParserID, ParserVersion, ConfigHash, SourceHash string
    OutputHash, LayoutSchemaVersion                 string
    Pages, Tables, Formulae, OCRPages               uint64
    Truncated                                      bool
    Warnings                                       []contracts.TypedError
}
```

  Invarijante:

  1. Parser selection je eksplicitna i pinned po corpus generationu. Fallback se događa samo po deklariranom policyju i proizvodi novi receipt; prazan/partial output ne predstavlja se kao kompletan parse.
  2. `document_id` je source-stable ID, a revision veže source bytes **i** parser/version/config output. Promjena ekstraktora koja mijenja tekst/layout mora reingestati čak i kad source bytes nisu promijenjeni.
  3. Connector cursor napreduje tek nakon durable commit-a svih prihvaćenih revisiona; partial failure zadržava prethodni cursor. Deleted source stvara tombstone revision, ne tihi orphan chunk.
  4. Hostile PDF/OCR adapter radi pod S6/S7 capovima za bytes/pages/CPU/RAM/temp/output i bez implicitnog egressa. Formula, table cell, metadata i OCR tekst ostaju untrusted document data.
  5. Live connector freshness ne znači direktan prompt injection: novi sadržaj prvo prolazi parse, ACL, provenance i 10.7 generation gate.

- **Salvage:** iz `/home/matej/NEXUSv2/core/ingest.py:61-71,172-277` Python→Go portirati converter registry seam, incremental `doc_id+hash` ledger, unchanged skip, changed upsert, prune/tombstone namjeru i “ledger tek nakon store writea”. Iz `core/pdf.py:30-46,83-122,245-337,366-431` portirati typed error taxonomy, byte/page/char/table capove, isolated subprocess obrazac, visible truncation i page/table anchors; ne portirati write verbs u S10. NEXUS parser ladder nije Docling-equivalent i ostaje bounded fallback, ne rich-primary tvrdnja.

- **Ugovor/RED:** P0.1/P0.3 + S6/S7 vrijede; 10.6 autorizira source/corpus. RED-ovi: `test_parser_change_reindexes_same_source_bytes`; `test_connector_cursor_does_not_advance_on_partial_commit`; `test_deleted_source_cannot_leave_active_chunks`; `test_pdf_bomb_hits_resource_gate_before_activation`; `test_truncated_parse_cannot_claim_complete`; `test_document_injection_remains_untrusted`; `test_ingestion_has_no_source_write_method`; `test_same_connector_rest_and_mcp_share_cursor_owner`.

- **Verifikacija:** provjereno 2026-09-01. Kandidatske tvrdnje o RAGFlow/Docling/Unstructured/Marker/MinerU moraju u matrici proći isti hostile/layout corpus; naziv parsera nije dokaz kvalitete. Lokalni ingest/pdf pregled potvrđuje incremental ledger, bounded PDF lanes, tables i typed failures, ali ne puni layout/formula floor. SurfSense službena [dokumentacija](https://github.com/MODSetter/SurfSense/blob/main/surfsense_web/content/docs/index.mdx) potvrđuje konektore za Notion/Slack/Google/Jira i druge izvore, ne našu freshness/SLA implementaciju.

- **Pareto floor:** **PASS na adapter contractu; parser-quality floor čeka corpus matricu.** Jedan Docling primary sprječava održavanje meta-parsera, a NEXUS fallback čuva offline minimum. Strongest counterexample je scanned/formula-heavy PDF gdje fallback proizvodi uvjerljiv, ali nepotpun tekst; `ParseReceipt.Truncated/warnings` i 10.7 golden corpus moraju blokirati activation umjesto tihog downgradea.

## 10.2 Semantic, parent-child i contextual chunking

- **Tip:** ADAPTER + GO ORKESTRACIJSKI UGOVOR

- **Aspekti:** LlamaIndex ima najširi semantic/sentence/hierarchical/parent-child repertoar; Chonkie je fokusirani brzi chunking engine; RAGFlow daje document-type templates. **Primarni pick:** LlamaIndex chunker adapter jer jedini kandidat u top-3 pokriva puni traženi eksperimentalni raspon; Chonkie je lightweight alternative, RAGFlow template alternative za layout-heavy corpus. Strategije se uspoređuju, ne simultano miješaju u istoj generationi.

- **Sinteza:** `Chunker` prima normalized `ParsedDocument` i jednu versioned `ChunkPolicy`. Tri obvezne strategije iza istog sučelja: `SEMANTIC` reže na embedding cosine-dropu; `PARENT_CHILD` embeda child, ali result vraća bounded parent context; `CONTEXTUAL` prepends zaseban derived context block, dok source anchors i original content ostaju odvojeni. Svaka strategija gradi zasebnu corpus generation nad istim frozen corpusom za 10.7 paired eval.

```go
type ChunkStrategy uint8 // SEMANTIC|PARENT_CHILD|CONTEXTUAL
type ChunkPolicy struct {
    Strategy ChunkStrategy
    TargetTokens, MinTokens, MaxTokens, OverlapTokens uint64
    SemanticThreshold float64
    ParentMaxTokens, ContextMaxTokens uint64
    TokenizerID, EmbeddingModelID, PromptVersion string
}
type Chunker interface {
    Chunk(context.Context, ParsedDocument, ChunkPolicy) ([]Chunk, ChunkReceipt, error)
}
type ChunkReceipt struct {
    ChunkerID, Version, ConfigHash, DocumentRevisionID string
    ChunkIDs []string
    Coverage SourceCoverage
    Oversized, Orphaned uint64
}
```

  Invarijante:

  1. Source coverage je potpuna ili receipt eksplicitno navodi gaps; chunkovi ne preklapaju heading/code/table boundaries mimo policyja. Nema rezanja usred UTF-8, code fencea, tablice ili sentence anchor-a.
  2. Contextual prefix je zaseban model-derived untrusted block s lineageom do originala; nije dio source quotea ni citation span-a. “It/the company” razrješenje ne smije izmišljenu parafrazu pretvoriti u izvorni tekst.
  3. Parent-child candidate generira se po childu, a authorized parent se dohvaća tek pod istim 10.6 filterom. Child ACL nikad ne smije proširiti parent ACL; efektivni ACL je intersection.
  4. Token budget koristi S8 tokenizer contract. Char count služi samo previewu. Isti strategy/config/corpus daje deterministične chunk ID-eve i redoslijed.
  5. Query-relevant metric uspoređuje tri zasebne generatione na istim golden queries, istom embedderu/rerankeru i budgetu; inače rezultat ne izolira chunking učinak.

- **Salvage:** iz `/home/matej/NEXUSv2/core/ingest.py:27-143` portirati heading breadcrumbs, fence-safe split, monster-line bound, sibling overlap, small-chunk merge i contextual filename/heading prefix. To je koristan deterministic baseline, ali nije semantic niti pravi parent-child chunker. Ne portirati fiksni `1800 chars≈450 tokens` kao production granicu; Go adapter koristi S8 `TokenCounter`.

- **Ugovor/RED:** P0.1 lineage i 10.6 ACL intersection su gateovi. RED-ovi: `test_contextual_prefix_cannot_be_cited_as_source`; `test_parent_acl_cannot_be_weakened_by_child`; `test_code_fence_and_table_are_not_split`; `test_chunk_ids_change_with_policy_version`; `test_source_coverage_detects_dropped_tail`; `test_semantic_parent_contextual_eval_freezes_other_variables`; `test_exact_token_max_boundary`; `test_chunker_output_cannot_raise_trust`.

- **Verifikacija:** specove top-3 feature claimove treba potvrditi pinned adapter verzijom i contract corpusom; plan ne pretpostavlja da svaki kandidat pokriva sve tri strategije. Lokalni `chunk_markdown` stvarno pokriva heading/fence/overlap/bounds, ali nema semantic cosine-drop, parent-child graph ni model contextualization. Zato je LlamaIndex external adapter primary, a NEXUS kod fallback/salvage.

- **Pareto floor:** **PASS na eksperimentalnom dizajnu; strategy winner ostaje data-dependent.** Ne proglašava univerzalni chunker. Strongest counterexample je corpus s tablicama gdje semantic splitter poboljša prose recall, a uništi row integrity; per-document-type strata i citation-span test moraju biti dio 10.7 matrice prije izbora.

## 10.3 Vektorska i hibridna pretraga s cross-encoder rerankingom

- **Tip:** ADAPTERI IZA `Retriever`/`Reranker` SUČELJA

- **Aspekti:** Qdrant je najbolji engine za dense+sparse hybrid, RRF/DBSF, filter pushdown i multi-stage query; Haystack je najbolji pipeline referent; sentence-transformers/BGE daje cross-encoder precision stage. LanceDB/sqlite-rag su file-based runner-upovi, query rewrite optional pre-retrieval enhancer. **Primarni pick:** Qdrant service adapter + zaseban BGE cross-encoder reranker adapter. File-based v1 bira jedan SQLite/LanceDB adapter samo ako matrica pokaže potreban recall i ACL pushdown; nije fallback unutar istog queryja.

- **Sinteza:** `VectorRetriever` šalje dense+sparse query i 10.6 compiled filter Qdrantu, dobiva bounded candidate pool te normalizira engine scoreove bez promjene redoslijeda. `Reranker` zatim ocjenjuje samo autorizirane candidates, pod zasebnim latency/budget limitom. Reranker failure vraća originalni hybrid order s explicit degradation receiptom. Query rewrite je opt-in `QueryTransform` koji vraća original+varijante s lineageom i hard capom; 10.7 mora dokazati korist.

```go
type QueryTransform interface {
    Expand(context.Context, AuthorizedQuery) ([]QueryVariant, TransformReceipt, error)
}
type Reranker interface {
    Rerank(context.Context, AuthorizedQuery, []Candidate, uint64) ([]Candidate, RerankReceipt, error)
}
type RetrievalReceipt struct {
    RetrieverID, Version, ConfigHash, CorpusVersion string
    FilterDigest, QueryDigest                       string
    CandidateCount, ReturnedCount                   uint64
    Latency                                         time.Duration
    Degraded                                        bool
    IndexGeneration, EvidenceRef                    string
}
type RerankReceipt struct {
    RerankerID, ModelID, ModelDigest string
    InputCount, OutputCount          uint64
    Latency                          time.Duration
    Degraded                         bool
}
```

  Invarijante:

  1. Candidate pool je bounded i već ACL-filtered. Reranker nikad ne prima unauthorized content; `candidate_limit >= limit`, ali oba su hard-capped po budgetu.
  2. Dense/sparse fusion, weights i model digesti su versioned config. Scoreovi različitih enginea ne uspoređuju se bez normalizacijskog contracta; receipt čuva lane ranks.
  3. Cross-encoder je eksplicitan stage, ne skriven u embedderu. Failure/timeout degradira uz event i istu autoriziranu hybrid listu; ne pokreće svoj retry.
  4. Embedding/index writes vezani su uz chunk ID+revision+tenant+model digest. Model change gradi novu generation; nema mješovitih dimenzija ili silent re-usea.
  5. File-based i Qdrant adapter implementiraju isti black-box corpus contract. Deployment bira jedan active adapter; migration radi generation parity+atomic cutover.

- **Salvage:** iz `/home/matej/NEXUSv2/core/embed.py:12-51` portirati lazy optional embedder seam i batch query/passage razliku, ne Python fastembed runtime. Iz `core/memvec.py:67-138` portirati keyword+recency+vector+PPR RRF logiku kao reference/baseline test oracle. Iz `core/rerank.py:10-46` portirati opt-in cross-encoder, top-pool bound, reorder i degrade-to-fused-order semantiku. NEXUS brute-force SQLite vector lane nije Qdrant production engine i ne proglašava se primaryjem.

- **Ugovor/RED:** 10.6 filter-pushdown + P0.1/P2.4. RED-ovi: `test_reranker_never_receives_denied_candidate`; `test_cross_encoder_failure_preserves_authorized_hybrid_order`; `test_embedding_model_change_cannot_reuse_old_generation`; `test_candidate_pool_and_latency_budget_are_hard`; `test_file_and_qdrant_adapters_pass_same_retrieval_contract`; `test_query_rewrite_cannot_drop_tenant_filter`; `test_same_query_different_tenant_cache_misses`; `test_retrieval_gain_report_has_recall_latency_pair`.

- **Verifikacija:** Qdrant službena [hybrid query dokumentacija](https://qdrant.tech/documentation/search/hybrid-queries/) potvrđuje dense+sparse fusion, RRF/DBSF i multi-stage prefetch/rerank. Qdrantova [multitenancy dokumentacija](https://qdrant.tech/documentation/manage-data/multitenancy/) potvrđuje tenant payload filter i tenant-scoped query. Lokalni rerank/memvec mehanizmi postoje; `+17%` ili bilo koji univerzalni gain nije verificiran i ne ulazi u plan.

- **Pareto floor:** **PASS uz adapter parity i measured tradeoff.** Qdrant+BGE postiže top-3 feature floor bez Haystack runtimea. Strongest counterargument je single-binary cilj: Qdrant je service dependency. Zato file-based adapter ostaje podržan izbor, ali smije postati default tek kad isti ACL+quality contract prođe; distribucijska jednostavnost ne opravdava slabiji isolation ili recall.

## 10.4 GraphRAG

- **Tip:** ADAPTER IZA `GraphRetriever`

- **Aspekti:** Microsoft GraphRAG je referent za entity/relation/claim extraction, Leiden communities, hierarchical reports te local/global/DRIFT query; LightRAG je jeftiniji lightweight kandidat; cognee spaja graph+vector memory. **Primarni pick:** Microsoft GraphRAG adapter za `graph-retrieval` quality baseline. LightRAG je budget alternative; cognee se ne grafta u isti graph store.

- **Sinteza:** Go `GraphRetriever` šalje authorized corpus generation i query primarnom adapteru te prima typed graph evidence: entity/edge/community IDs, source chunk refs, graph generation i mode. Index build je async S7 job, staged i neaktivan do 10.7 gatea. ACL se materijalizira na svakom node/edge/reportu kao intersection svih source chunk ACL-ova; global/community summary s miješanim tenantima nije dopušten.

```go
type GraphMode uint8 // LOCAL|GLOBAL|DRIFT
type GraphEvidence struct {
    EvidenceID, GraphGeneration, CorpusVersion string
    Mode GraphMode
    EntityIDs, EdgeIDs, CommunityIDs []string
    SourceChunkIDs []string
    Block contracts.ContextBlock
    ACLDecisionEvidenceID string
}
type GraphRetriever interface {
    Build(context.Context, AuthorizedCorpus, GraphBuildPolicy) (GraphGeneration, error)
    Search(context.Context, AuthorizedQuery, GraphMode) ([]GraphEvidence, RetrievalReceipt, error)
}
```

  Invarijante:

  1. Svaki entity/edge/claim/community report ima source chunk lineage; LLM-extracted edge je untrusted derived fact, ne corpus truth.
  2. ACL intersection primjenjuje se tijekom graph builda i query fan-outa. Global summary koji uključuje denied source ne smije se samo post-filterati nakon generationa.
  3. Graph/corpus/prompt/model/config version su pinned. Partial ili stale graph generation nije searchable; update gradi novu generation ili koristi adapterov verificirani incremental contract.
  4. Build budget i query mode su eksplicitni. Global search ne pokreće se kao tihi fallback za local miss; caller bira mode kroz policy/10.7 query class.
  5. Graph adapter ne posjeduje vector store iz 10.3; eventualni interni index ostaje encapsulated i ne dijeli aktivni generation pointer.

- **Salvage:** iz `/home/matej/NEXUSv2/core/kg.py:22-100` portirati bounded triple schema, extract-before-write, atomic idempotent replace, alias resolution i “provider outage ne briše stari graph”; iz `kg.py:103-150` bounded local/global traversal. To nije GraphRAG community/report implementation. Koristi se kao mali Go graph fallback/test oracle, ne kao Microsoft GraphRAG-equivalent claim.

- **Ugovor/RED:** P0.1 lineage + 10.6 graph ACL + 10.7 freshness. RED-ovi: `test_graph_edge_without_source_chunk_is_rejected`; `test_global_summary_cannot_mix_denied_tenant`; `test_partial_graph_generation_never_becomes_active`; `test_stale_graph_reports_ungrounded`; `test_model_extracted_edge_remains_untrusted`; `test_local_miss_does_not_auto_run_global`; `test_graph_adapter_outage_preserves_previous_generation`; `test_graph_mode_quality_is_measured_separately`.

- **Verifikacija:** Microsoftova službena [GraphRAG dokumentacija](https://microsoft.github.io/graphrag/) potvrđuje text units, entity/relationship extraction, Leiden communities, community summaries i local/global/DRIFT modes; službeni [getting-started](https://microsoft.github.io/graphrag/get_started/) upozorava na velik LLM resource cost. Lokalni KG potvrđuje atomic build i bounded traversal, ali nema community hijerarhiju. LightRAG/cognee ostaju matrix alternative.

- **Pareto floor:** **PASS na quality/reference flooru, uz visok cost proof ceiling.** Microsoft GraphRAG se ne prepisuje u Go niti kombinira s dvije baze. Strongest counterexample je mali corpus gdje skupi graph build ne poboljša vector RAG; `graph-retrieval` smije aktivirati adapter samo ako paired 10.7 eval opravda trošak i latency.

## 10.5 CAG decision rule

- **Tip:** MEHANIZAM ODLUKE + S8.4 CACHE ADAPTER (implementacijski top-3 ostaje UNCLEAR)

- **Aspekti:** verificirani aspekt nije nova baza nego odluka: mali, stabilni, autorizacijski homogeni corpus može se preloadati u provider/local prefix-KV stanje; veliki, churn-heavy ili per-principal različit corpus ide RAG-om. vLLM/SGLang iz S8.4 implementiraju cache primitive, ali ne selection/auth/freshness. **Primarni implementation pick: nema ga** dok matrix ne pronađe end-to-end OSS koji provodi cijeli ugovor; vlastiti Go `KnowledgePlanner` je minimalni owner odluke.

- **Sinteza:** `KnowledgePlanner` iz corpus manifest-a, exact token counta, churn/freshness SLA-a, authz partitiona, sensitivityja, cache capabilityja i paired evala bira `PRELOAD|RAG|HYBRID|DISABLED`. PRELOAD gradi immutable `PreloadSnapshot` kao P0.1 blokove i S8.4/P2.4 full-scope cache key; nema sirovog KV handlea u S10. HYBRID smije kombinirati mali stable policy/glossary prefix s RAG-om za dinamične dokumente, ali svaki dio ima zaseban citation/freshness receipt.

```go
type KnowledgeMode uint8 // PRELOAD|RAG|HYBRID|DISABLED
type CorpusProfile struct {
    CorpusVersion string
    AuthorizedTokens, DocumentCount uint64
    ChurnPerDay float64
    FreshnessSLA time.Duration
    TenantID, PrincipalScopeHash, AuthzPolicyVersion string
    Sensitivity contracts.Sensitivity
}
type KnowledgePlan struct {
    Mode KnowledgeMode
    ProfileDigest, EvalEvidenceRef, DecisionRuleVersion string
    PreloadSnapshotID, RetrieverGeneration string
    ExpiresAt time.Time
}
type KnowledgePlanner interface {
    Decide(context.Context, CorpusProfile, providers.CacheCapabilities, EvalComparison) (KnowledgePlan, error)
}
```

  Invarijante:

  1. “Mali” znači `authorized_tokens <= exact_preload_budget` nakon system/tools/output rezerve, ne fiksni broj dokumenata. “Stabilan” znači izmjeren churn i freshness SLA, ne subjektivnu oznaku.
  2. PRELOAD scope je točno tenant+principal+policy+sensitivity+corpus generation. Shared prefix preko različitog authz scopea zabranjen je čak i ako su content hashovi isti.
  3. Source update/revocation/purge/policy change invalidira snapshot prije sljedećeg model poziva i povećava P2.4 generation. Expired preload nikad se ne koristi kao stale convenience.
  4. Decision se kalibrira paired corpus evalom uz isti model, prompt, budget i queries; bilježi quality, TTFT, cost i freshness miss. Nema univerzalnog “CAG je brži/bolji”.
  5. Ako provider nema verifiable cache capability, PRELOAD i dalje može biti plain context injection ako stane, ali report ga ne smije zvati KV cache HIT-om.

- **Salvage:** `core/ingest.py` ledger daje corpus size/version input; `core/embed.py`/`memvec.py` daju RAG baseline; S8.4 NEXUS `promptlayer.py` ideja daje stable-prefix hint. Nema NEXUS end-to-end CAG decision ownera niti kvalificiranog OSS top-3; plan to označava, ne skriva vlastitim marketing nazivom.

- **Ugovor/RED:** Annex **P2.4** je kanonski cache gate; P0.1 lineage i 10.6 authz vrijede za snapshot. RED-ovi: `test_small_cross_tenant_corpus_cannot_preload_shared_prefix`; `test_policy_or_purge_generation_invalidates_preload_before_model`; `test_exact_budget_boundary_includes_reserved_tokens`; `test_dynamic_corpus_selects_rag_when_freshness_would_fail`; `test_plain_prefix_cannot_claim_kv_hit`; `test_cag_vs_rag_comparison_freezes_model_prompt_queries`; `test_no_implementation_top3_is_reported_unclear`.

- **Verifikacija:** vLLM/SGLang cache primitive verificira se u S8.4, ne ponavlja ovdje. Nije pronađen niti specificiran OSS kandidat koji dokazano objedinjuje corpus classification, authz, invalidation, preload i paired evaluation; **UNCLEAR ostaje pošten zaključak**. Decision rule je naš normativni mehanizam, ne dokaz postojeće implementacije.

- **Pareto floor:** **PASS na odluci, N/A na top-3 implementation flooru.** Strongest counterexample je mali, ali često mijenjan ACL-scoped corpus: veličina sama bi pogrešno izabrala CAG. Churn+SLA+policy generation zato su obvezni ulazi. Upgrade tek kad kandidat prođe sve RED-ove, ne samo pokaže prefix cache demo.

## 10.6 Retrieval authorization i tenant filteri

- **Tip:** MEHANIZAM (JEZGRENI PEP) + ENGINE FILTER ADAPTER

- **Aspekti:** Qdrant daje payload/tenant filter pushdown; OpenSearch document-level security; Vespa filteriranje unutar ranking pipelinea. Najbolji graft nije treća auth baza nego jezgreni `AuthorizedRetriever` koji kompajlira S6.6 odluku u engine-native filter i odbija adapter koji ne može dokazati pre-candidate enforcement. **Primarni pick za vector flag:** Qdrant filter adapter; ostali su deployment alternatives.

- **Sinteza:** `RetrievalAuthorizer` prije query rewritea/candidate generationa validira principal/tenant/purpose, izračuna allowed corpus/document labels i izdaje signed `RetrievalGrant`. `FilterCompiler` prevodi grant u Qdrant/OpenSearch/Vespa filter; adapter contract mora vratiti filter digest u receipt-u. Kernel wrapper provjerava svaki rezultat kao containment detector i incident signal, ali unauthorized candidate znači `RETRIEVAL_FILTER_BYPASS`, odbacivanje cijelog seta i karantenu adaptera — ne “sigurno smo ga post-filtrirali”.

```go
type RetrievalGrant struct {
    GrantID, QueryID, TenantID, PrincipalID, PrincipalScopeHash string
    AuthzPolicyVersion, AllowedCorpusVersion, Purpose           string
    AllowedLabels, DeniedLabels                                []string
    IssuedAt, ExpiresAt                                        time.Time
    FilterDigest, Signature                                    string
}
type FilterCompiler interface {
    Compile(context.Context, RetrievalGrant, EngineDescriptor) (CompiledACLFilter, error)
}
type RetrievalAuthorizer interface {
    Authorize(context.Context, QueryIntent) (RetrievalGrant, error)
}
type AuthorizedRetriever struct {
    Authorizer RetrievalAuthorizer
    Compiler   FilterCompiler
    Engine     Retriever
}
```

  Invarijante:

  1. Missing tenant/principal/policy/corpus scope je deny, ne global-search default. Service mode zahtijeva non-null tenant; local single-user i dalje ima explicit local principal/namespace.
  2. Filter se primjenjuje u engine candidate-generationu na dense, sparse, graph, parent lookup, query expansion, rerank pool i IDF/statistics corpus. Niti jedan lane ne smije koristiti global statistics koje otkrivaju/miješaju drugi tenant ako engine podržava scoped stats.
  3. Grant je short-lived, query/purpose/corpus-bound i ne može se re-useati nakon policy/revocation changea. Adapter ne smije proširiti label set; remote engine credential je najmanjeg scopea.
  4. Candidate payload mora nositi tenant+ACL label+revision i matchati grant. Jedan bypass kompromitira cijeli result i aktivni adapter attestation; nema parcijalnog successa.
  5. Logs/metrics ne sadrže query/chunk tajne niti denied counts po tuđem tenant-u. Audit bilježi digest policyja/filtera, engine i odluku.

- **Salvage:** NEXUS `memory.py`/`memvec.py` imaju scope filter, ali nemaju tenant/principal/policy grant niti dokaz pre-candidate ACL-a. `core/kg.py` također querya samo scope. Zato nema 1:1 salvage mehanizma; portira se samo typed scope plumbing, a novi PEP/filter compiler pišemo u Go. Qdrant adapter mora slati tenant payload filter u sam query.

- **Ugovor/RED:** P0.1 štiti provenance, ali **ne zamjenjuje ovaj ACL ugovor**; S6.6 je policy owner. P2.4 vrijedi za cache. Kanonski novi RED `test_acl_filter_runs_before_candidate_generation`: tenant A canary je najbliži queryju B; instrumentirani engine mora dobiti B filter prije searcha, candidate scan count za A=0, rezultat/caches/reranker nemaju A. Dodatno: `test_missing_tenant_denies_without_engine_call`; `test_query_rewrite_cannot_drop_grant`; `test_parent_and_graph_fanout_reuse_same_grant`; `test_filter_bypass_quarantines_entire_adapter`; `test_revoked_grant_cannot_hit_cache`; `test_denied_corpus_does_not_affect_tenant_idf`.

- **Verifikacija:** Qdrantova službena [multitenancy dokumentacija](https://qdrant.tech/documentation/manage-data/multitenancy/) pokazuje tenant payload index i filter u queryju; ista dokumentacija razlikuje retrieval filter od tenant-scoped IDF corpusa, što opravdava eksplicitni statistics invariant. OpenSearch/Vespa ostaju matrix alternatives. Lokalni NEXUS kod ne zadovoljava multi-tenant contract i to je zabilježen gap, ne salvage claim.

- **Pareto floor:** **PASS tek nakon canary scan-count testa na stvarnom engineu.** Post-filter je jednostavniji, ali materijalno pogrešan jer unauthorized chunk već ulazi u vector scan/rerank/cache i može procuriti kroz side effects. Strongest counterexample je buggy remote engine koji ignorira filter: response containment ga detektira, ali ne može dokazati da engine nije obradio podatke; zato adapter attestation i per-tenant shard/credential ostaju potrebni za jače isolation profilee.

## 10.7 Retrieval quality, freshness i provenance gate

- **Tip:** MEHANIZAM + EVAL/CONNECTOR ADAPTERI

- **Aspekti:** Ragas daje RAG-specific evaluatore; BEIR heterogeneous retrieval benchmark i klasične IR metrike; Phoenix trace/dataset retrieval evaluatore. SurfSense je referent za live connector breadth, ali connector svježina vrijedi tek kad ima durable cursor/watermark i SLA dokaz. **Primarni pick:** vlastiti deterministic IR evaluator za Recall@k/MRR/nDCG+citation/freshness gate; BEIR je external benchmark adapter, Phoenix/Ragas optional judge/trace adapteri.

- **Sinteza:** `RetrievalGate` ima offline generation gate i runtime answer gate. Offline pokreće versioned golden queries s labeled relevant chunkovima, računa deterministic Recall@k/MRR/nDCG, latency/cost i strata po source/chunking/query tipu; pragovi i ratchet su corpus-specific. Runtime provjerava active generation, source/connector freshness SLA, grant/filter receipt i svaki citation do exact chunk/source anchor-a. Tek `GroundingReport.Passed` dopušta oznaku grounded; inače refresh, bounded fallback ili eksplicitno ungrounded/refusal prema policyju. S16.2 samo izvršava/ratcheta ovaj gate u CI-u.

```go
type GoldenQuery struct {
    QueryID, CorpusVersion, Text string
    RelevantChunkIDs []string
    RequiredSourceRevisionIDs []string
    AsOf *time.Time
    Strata []string
}
type RetrievalMetrics struct {
    RecallAtK, MRR, NDCG float64
    CitationPrecision, CitationCoverage float64
    P50Latency, P95Latency time.Duration
    CostMinorUnits uint64
    FreshnessMissRate float64
}
type Citation struct {
    CitationID, ChunkID, DocumentID, RevisionID, ContentHash string
    SourceURI string
    Anchor SourceAnchor
    QuotedHash string
}
type GroundingReport struct {
    QueryID, CorpusVersion, IndexGeneration, PolicyVersion string
    RetrievalReceiptRefs, CitationIDs []string
    Metrics *RetrievalMetrics
    Freshness FreshnessStatus
    Passed bool
    Failures []contracts.TypedError
}
type RetrievalGate interface {
    EvaluateGeneration(context.Context, EvalDataset, Retriever) (RetrievalMetrics, GateAttestation, error)
    VerifyContext(context.Context, AuthorizedQuery, []Candidate) (GroundingReport, error)
    VerifyAnswer(context.Context, GroundingReport, []Citation) error
}
```

  Invarijante:

  1. Golden dataset je versioned, reviewable i odvojen od tuning seta. Empty queries/relevance labels ne daju vacuous GREEN; svaki adapter/chunker/reranker experiment koristi isti frozen split.
  2. Recall@k/MRR/nDCG računaju se iz chunk ID ground trutha, ne LLM judge proze. LLM faithfulness/relevance evaluator je dodatni signal s vlastitim pinned model/promptom, ne zamjena za IR metrike.
  3. Freshness se računa iz source modified/watermarka do active index generationa. Connector “success” bez cursor advance/revision matcha ne zadovoljava SLA; stale generation ne dobiva grounded status.
  4. Citation mora rezolvirati exact active ili povijesni document revision i source span čiji hash odgovara. Contextual prefix, gist, graph summary ili model paraphrase ne smije se citirati kao izvorni chunk.
  5. Promocija zahtijeva minimalne quality pragove **i** latency/cost/resource budget. Ratchet ne smije sakriti regresiju prosjekom; kritični tenant/ACL/freshness strata imaju hard floor.
  6. SurfSense/live connector adapter prolazi isti source cursor, ACL, provenance i freshness ugovor kao file ingest. “Live” nije bypass indeksnog/generation gatea.

- **Salvage:** `core/ingest.py:172-277` daje source hash/ledger/generation input, `core/rerank.py` stage receipt input, `core/kg.py` graph generation i `core/pdf.py` page anchors. NEXUS nema kanonski golden-query evaluator, freshness SLA ni citation-to-chunk gate; to je novi Go mehanizam. NEXUS retrieval sanity asserti postaju seed fixturei, ne production quality dokaz.

- **Ugovor/RED:** P0.1 provenance + 10.6 ACL + P2.4 cache invalidation. RED-ovi: `test_relevant_chunk_ablation_drops_recall_at_k_and_gate`; `test_empty_golden_set_cannot_pass`; `test_stale_connector_cursor_blocks_grounded_status`; `test_citation_hash_must_match_exact_chunk_revision`; `test_contextual_prefix_cannot_be_source_citation`; `test_acl_failure_stratum_cannot_be_averaged_away`; `test_latency_regression_fails_budget_despite_quality_gain`; `test_live_connector_cannot_activate_partial_generation`; `test_16_2_runner_cannot_redefine_retrieval_metrics`.

- **Verifikacija:** BEIR službeni [repo](https://github.com/beir-cellar/beir) potvrđuje heterogeneous IR datasete te lexical/dense/sparse/reranking evaluaciju. Phoenix službena [retrieval benchmarking dokumentacija](https://arize.com/docs/phoenix/learn/retrieval-and-infrences/benchmarking-retrieval) potvrđuje MRR, Precision@k i nDCG te izričito razlikuje retrieval kvalitetu od final-answer correctnessa. SurfSense dokumentira konektore, ali freshness SLA/provenance gate je naš mehanizam. Ragas/Phoenix judge score nije deterministic promotion authority.

- **Pareto floor:** **PASS na mjerljivosti i provenanceu.** Zadržava Ragas/Phoenix observability i BEIR IR floor, a dodaje runtime freshness+citation enforcement. Strongest counterexample je visok Recall@k s pogrešnim/stale citatima; zato generation ne prolazi samo offline retrieval metrikom. Najslabija točka je kvaliteta golden labels; dvostruki review i adversarial/stale/ACL strata postaju upgrade gate prije produkcijske promocije.
