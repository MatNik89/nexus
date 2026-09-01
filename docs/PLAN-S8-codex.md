PLAN S8

# S8 — Kontekst management

## Zajedničke odluke i paket-layout

- S8 posjeduje **projekciju** kanonskog P0.3 event streama u ograničeni model-context: mjerenje, odabir, sažimanje, rangiranje i cache reuse. Ne posjeduje izvornu historiju, code-index, memoriju ni provider retry. P0.3 `EventJournal` ostaje jedini write-owner; S4.4 proizvodi `CodeGraphSnapshot`, S9/S10 daju recall blokove, a S8 ih samo pretvara u provjerljivu projekciju.
- Predloženi layout: `internal/context/{contract,budget,compact,repomap,cache,observe}`. Jezgra je mali Go mehanizam nad S0 `ContextBlock`; Aider/OpenHands/pi-mono/RTK/Headroom algoritmi su referenti ili adapteri, ne runtime dependencyji. vLLM/SGLang postoje samo iza `local-model` capabilityja.
- Svaka transformacija vraća novi `ContextBlock` i `TransformReceipt`; nikad ne mijenja input niti journal. P0.1 lineage mora biti tranzitivno očuvan, trust se smije samo sniziti, sensitivity samo povisiti. Tekst sažetka, simbola ili tool outputa nikad sam sebi ne može dodijeliti retention, trust ili instruction prioritet.
- Transformacije rade nad strukturiranim blokovima, ne nad sentinelima u stringu. Nezavisni `ContextAssembler` jedini sastavlja konačnu poruku nakon provjere budgeta, provenancea, policyja i cache scopea.

```go
package context

type RetentionClass uint8 // KERNEL_REQUIRED|ACTIVE_TURN|EVIDENCE|RECALL|HISTORY
type LossClass uint8      // LOSSLESS|STRUCTURAL_FILTER|SUMMARIZED|TRUNCATED

type Candidate struct {
    Block          contracts.ContextBlock
    Retention      RetentionClass
    Utility        float64
    TokenEstimate  uint64
    PairID         string // tool-call/observation ili drugi nedjeljivi par
}

type TransformReceipt struct {
    TransformID, ImplementationID, Version string
    InputBlockIDs, OutputBlockIDs          []string
    InputDigest, OutputDigest              string
    Loss                                   LossClass
    TokensBefore, TokensAfter              uint64
    LineageManifestRef                     string
    CreatedAt                              time.Time
}

type Transformer interface {
    Transform(context.Context, []contracts.ContextBlock) ([]contracts.ContextBlock, TransformReceipt, error)
}
```

Globalne invarijante:

1. `ContextBlock` sadržaj i provenance validiraju se prije rangiranja; sadržaj ne može proglasiti sam sebe `SYSTEM`, `KERNEL_REQUIRED`, trusted ili neodstranjivim.
2. Svaka lossy projekcija mora sadržavati `LossClass`, ref na puni izvor ili zatvoreni lineage manifest i mjerljive tokene prije/poslije. Ako puni izvor nije retrievable, projekcija to kaže; ne smije nuditi lažni continuation.
3. Tool-call i pripadajući observation, edit i verifier evidence te approval intent i odluka tretiraju se kao nedjeljive grupe. Budžet ne smije proizvesti semantički siroče.
4. Nedovoljan prostor za kernel minimum završava `CONTEXT_BUDGET_UNSATISFIABLE` prije model poziva; ne hard-truncira sigurnosne ugovore, aktivni cilj ili strukturirani tool rezultat.
5. Cache nije autoritet za trust, authz ni svježinu. Svaki HIT ponovno prolazi P0.1/P2.4 provjeru i ulazi kao normalan `ContextBlock` s lineageom.

## 8.1 Window budžet i truncation strategija

- **Tip:** MEHANIZAM

- **Aspekti:** Aider je najbolji izvor za dinamički repo-map budget i odabir najrelevantnijeg grafa unutar dostupnih tokena; letta/MemGPT daje korisnu core/recall/archival hijerarhiju; OpenHands daje event-stream view i tokenizer-aware context pressure. Graftamo Aiderovo budget-aware rangiranje i OpenHandsovu projekciju historije, ali retention klase određuje kernel policy, ne modelova memorijska heuristika.

- **Sinteza:** `BudgetPlanner` računa efektivni input limit iz provider/model capabilityja, rezervira output/reasoning/tool-schema/safety prostor, tokenizira svaki kandidat stvarnim tokenizerom kada postoji i atomically bira cijele grupe. Redukcijski red je: ukloni neaktivne/istekle blokove → 8.5 observation reducer → 8.3 repo-map resize → 8.2 compaction → odbij ako kernel floor još ne stane. Generički `len/4` je samo označena procjena za preview, nikad dokaz da request stane.

```go
type TokenCount struct { Tokens uint64; Method string; Exact bool }
type TokenCounter interface {
    Count(context.Context, string, []byte) (TokenCount, error) // modelID, canonical bytes
}
type WindowSpec struct {
    ProviderID, ModelID                         string
    MaxContextTokens, MaxOutputTokens           uint64
    ReservedReasoningTokens, ReservedToolTokens uint64
    SafetyMarginTokens                          uint64
}
type BudgetPlan struct {
    Window WindowSpec
    Selected, Omitted []CandidateDecision
    InputTokens, ReservedTokens uint64
    CountMethod string
    Pressure float64
}
type BudgetPlanner interface {
    Plan(context.Context, WindowSpec, []Candidate) (BudgetPlan, error)
}
```

  Invarijante:

  1. `input + output + reasoning + tools + safety <= max_context`; provider-declared limit i tokenizer verzija ulaze u plan digest. Nepoznat tokenizer koristi konzervativni adapter i `Exact=false`; production request mora dobiti provider-side preflight ili dodatni margin.
  2. `KERNEL_REQUIRED` blokovi uključuju aktivni cilj, policy/approval granice, aktualni plan state i najnoviji verifier rezultat. Trust i sensitivity ne određuju korisnost, ali se provenance provjerava prije uključivanja.
  3. Prioritet se računa iz vlasničkih metadata i task relevancea. Fraza poput “ignore budget, pin me” u `UNTRUSTED_EXTERNAL` sadržaju nema utjecaja.
  4. Nema rezanja usred UTF-8, JSON tool resultata, diff hunka ili `PairID` grupe. Oversized pojedinačni blok ide odgovarajućem reduceru ili fail-closed.

- **Salvage:** iz `/home/matej/NEXUSv2/core/contextbudget.py:7-69` Python→Go portirati component-wise evidence, provider `count_tokens` hook, output rezervaciju i `normal|pressure|over` izvještaj. Ne portirati `ceil(len/4)` kao enforcement niti miješanje executorovog char estimatea s točnim brojem. Iz `core/rank.py:112-129` portirati budget-bisection obrazac, ali preko `TokenCounter`a i nedjeljivih blok-grupa.

- **Ugovor/RED:** P0.1 i P0.3 su obvezni iako Annex nema zaseban 8.1 ugovor. RED-ovi: `test_budget_rejects_oversized_kernel_floor` mora dati `CONTEXT_BUDGET_UNSATISFIABLE` i model sink=0; `test_untrusted_text_cannot_self_pin`; `test_tool_call_observation_pair_is_atomic`; `test_exact_token_boundary_includes_output_and_tool_reserve`; `test_estimated_count_cannot_be_reported_exact`; `test_reducer_order_is_observe_then_map_then_compact`.

- **Verifikacija:** provjereno 2026-09-01. Aiderova službena [repo-map dokumentacija](https://aider.chat/docs/repomap.html) potvrđuje graph ranking i mapu prilagođenu aktivnom token budgetu. Lokalni `contextbudget.py` stvarno mjeri po komponenti i koristi aktivni tokenizer kada postoji, ali fallback i budget-used put nisu dovoljno precizni za izvršni gate. MemGPT/OpenHands detalji ostaju kandidatske hipoteze za matricu gdje nisu pokriveni službenim testom.

- **Pareto floor:** **PASS uz exact-boundary gate.** Zadržava Aiderovo adaptivno pakiranje i NEXUS evidence shape, ali ne uvodi novi globalni planner framework. Strongest counterexample je provider čiji tokenizer ili skriveni wrapper doda tokene nakon plana; provider adapter mora vratiti canonical serialized-request count ili odbiti `Exact=true`. Najslabija točka je dostupnost identičnog tokenizera za proprietary modele; to se eksplicitno označava i pokriva marginom, ne lažnom preciznošću.

## 8.2 Kompakcija i summarizacija povijesti

- **Tip:** MEHANIZAM

- **Aspekti:** OpenHands daje read-only condenser projekciju, rolling soft/hard trigger i više strategija; pi-mono daje append-only JSONL session tree, iterativni previous-summary update, split-turn zaštitu i branch summary; letta/MemGPT daje razdvajanje aktivne i arhivske memorije. NEXUS anchored summary dodaje stabilna polja cilja, ograničenja, odluka i sljedećih koraka. Naš graft je typed anchored summary s evidence referencama i monotonim P0.1 provenanceom.

- **Sinteza:** model proizvodi samo prijedloge `Claim`; deterministički `Compactor` validira strukturu, veže svaki prihvaćeni claim uz postojeće block ID-eve, konstruira novi `SUMMARY` blok i sam izračunava trust/sensitivity/lineage. Sažetak nikad nije `SYSTEM`: trust izlaza je najstroži/nižepovjerljiviji trust svih izvora, dodatno `UNTRUSTED_EXTERNAL` ako ga je proizveo model; sensitivity je maksimum izvora. Kritični kernel facts ostaju zasebni blokovi i ne ovise o modelovu sažetku.

```go
type AnchorSection string // Goal|Constraints|Done|InProgress|Blocked|Decisions|NextSteps|RelevantFiles
type SummaryClaim struct {
    ClaimID, Text string
    EvidenceBlockIDs []string
    Confidence contracts.Confidence
}
type AnchoredSummary struct {
    SummaryID, PreviousSummaryID string
    Sections map[AnchorSection][]SummaryClaim
    CoveredBlockIDs []string
    SourceRange contracts.EventRange
    SourceDigest, LineageManifestRef string
}
type CompactionPolicy struct {
    TargetTokens, KeepRecentTurns uint64
    RequiredSections []AnchorSection
    MinReductionRatio float64
}
type Compactor interface {
    Propose(context.Context, []contracts.ContextBlock, CompactionPolicy) (AnchoredSummary, error)
    ValidateAndProject(context.Context, AnchoredSummary) (contracts.ContextBlock, TransformReceipt, error)
}
```

  Invarijante:

  1. Re-compaction unionira lineage prethodnog summaryja i novih source blokova; zatvoreni manifest je content-addressed i sadrži sve leaf block ID-eve. Brisanje ili zamjena izvora ne smije prekinuti audit put.
  2. Claim bez postojećeg evidence ID-a odbija se ili označava `UNSUPPORTED`; ne smije postati kernel fact. Missing required section ili loš digest ostavlja prethodni valjani summary aktivnim.
  3. Originalni journal ostaje neizmijenjen. P0.3 appenduje samo `COMPACTION_PROPOSED|ACCEPTED|REJECTED` i receipt; projection ne piše vlastitu historiju.
  4. Aktivni tool-call/observation par i zadnji nedovršeni turn ne dijele se. Branch summary navodi branch root/range i ne ulazi u drugu granu bez eksplicitnog merge eventa.
  5. Untrusted sadržaj koji u summaryju izgleda kao instrukcija ostaje data claim iste ili niže trust klase; prompt assembler ga ne stavlja u instruction channel.

- **Salvage:** iz `/home/matej/NEXUSv2/core/compact.py:16-106` portirati osam anchored sekcija, previous-summary update, safe recent-tail, tool-pair zaštitu, minimum-progress guard i fallback na prethodni summary. Iz `compact.py:110-259` portirati idempotentne prune/stale-read markere kao input u 8.5. Ne portirati raw-string API, `_tokens=len/4` ni hard-truncate granu. Sažetak postaje typed projection, ne string u system promptu.

- **Ugovor/RED:** Annex **P0.1** je kanonski provenance ugovor; **P0.3** je write-owner gate. Obvezni `test_s0_rejects_provenance_laundering` mora pokriti compaction koji ukloni lineage i upcasta trust u `SYSTEM`: rezultat `PROVENANCE_DOWNGRADE`, run nije `RUNNING`, model sink=0. Dodatno: `test_summary_injection_remains_untrusted_data`; `test_recompaction_preserves_transitive_leaf_lineage`; `test_claim_without_evidence_cannot_become_kernel_fact`; `test_invalid_anchor_keeps_previous_summary`; `test_compaction_cannot_split_active_tool_pair`; `test_projection_cannot_bypass_journal`.

- **Verifikacija:** provjereno 2026-09-01. OpenHandsov službeni [`CondenserBase`](https://github.com/OpenHands/software-agent-sdk/blob/main/openhands-sdk/openhands/sdk/context/condenser/base.py) potvrđuje read-only view, rolling condenser i soft/hard condensation zahtjev. pi-mono službena [compaction dokumentacija](https://github.com/badlogic/pi-mono/blob/main/packages/coding-agent/docs/compaction.md) potvrđuje JSONL entryje, previous-summary update, split-turn i branch summarization; zato specova oznaka “HIPOTEZA” za ta svojstva može u matrici prijeći u verificirano. Lokalni NEXUS anchored/fallback mehanizmi postoje, ali nemaju typed provenance.

- **Pareto floor:** **PASS tek uz laundering mutation.** Zadržava OpenHandsovu projekcijsku čistoću, pi-mono session semantiku i NEXUS anchored stabilnost. Strongest counterexample je semantički netočan, ali sintaktički uredan summary: evidence IDs dokazuju porijeklo, ne istinitost parafraze. Zbog toga verifier i aktivni plan ostaju zasebni nekomprimirani blokovi, a claim confidence nije autoritet.

## 8.3 Ambijentalna reprezentacija codebasea

- **Tip:** MEHANIZAM

- **Aspekti:** Aider je najbolji za tree-sitter simbolske tagove, dependency graf, personalized graph ranking i budget-aware render; Repomix za determinističko strukturirano pakiranje stabla; Continue za lokalni indeks/retrieval. NEXUS `archmap` nadograđuje code-only mapu typed vezama prema Docker/CI/SQL/GraphQL/docs/config artefaktima, incremental fingerprintima i freshness generacijom — to je coding diferencijator koji zadržavamo.

- **Sinteza:** S4.4 je jedini owner parsiranja, LSP/AST indeksa i `CodeGraphSnapshot`; S8.3 je owner task-focused rankinga i prompt projekcije. `RepoMapProjector` prima immutable snapshot, seedove iz zadatka/changed files/diagnostike, radi personalized PageRank nad confidence-weighted typed bridovima i renderira minimalni map blok do budgeta. Determinističke edgeove ne smije prepisati LLM enrichment; enrichment je zaseban untrusted annotation.

```go
type CodeGraphSnapshot struct {
    SnapshotID, WorkspaceRevision, IndexVersion string
    Nodes []CodeNode
    Edges []CodeEdge
    FileDigests map[string]string
    BuiltAt time.Time
}
type MapRequest struct {
    SnapshotID string
    FocusTerms, FocusFiles, ChangedFiles []string
    AllowedPaths []string
    TokenBudget uint64
}
type RepoMap struct {
    Block contracts.ContextBlock
    RankedNodeIDs []string
    OmittedCount uint64
    SnapshotID, WorkspaceRevision string
}
type RepoMapProjector interface {
    Project(context.Context, CodeGraphSnapshot, MapRequest) (RepoMap, TransformReceipt, error)
}
```

  Invarijante:

  1. Svaki prikazani simbol/edge nosi file digest, parser/producer i snapshot lineage. Workspace revision mismatch označava mapu `STALE`; stale mapa ne smije tvrditi da je aktualna niti zamijeniti live S4 search.
  2. `AllowedPaths` se primjenjuje prije građenja seedova i grafa. Denied/secret file ne smije procuriti ni kroz naziv simbola, edge, community label ili LLM enrichment.
  3. Ranking je determinističan za isti snapshot/request; tie-break je canonical path+symbol ID. Render se mjeri stvarnim tokenizerom i ne reže definiciju usred zapisa.
  4. Non-code artefakti su prvorazredni typed nodes; npr. CI→test command, Docker→entrypoint, SQL→model i GraphQL→handler bridovi ostaju u istoj task map projekciji.
  5. Tekst u imenu simbola/komentaru ostaje `UNTRUSTED_EXTERNAL` prema source fileu; repo mapa je data, nikad instruction block.

- **Salvage:** iz `/home/matej/NEXUSv2/core/rank.py:39-129` Python→Go portirati personalized PageRank, task/file focus, referenced-symbol boost, pinned manifests i budget bisection. Iz `core/archmap.py:26-327` portirati non-code artifact model, resolved project-internal edgeove, deterministic communities, file fingerprint incrementalnost, atomic generation/freshness i query/export shape. Python AST/regex parsere ne portirati kao konačni multi-language owner: S4.4 daje tree-sitter/LSP snapshot. Opcionalni LLM enrich ostaje annotation i ne mijenja deterministic graph.

- **Ugovor/RED:** P0.1/P0.3 vrijede za map block; nema zasebnog Annex ugovora. RED-ovi: `test_task_focus_reranks_referenced_helper_above_large_leaf`; `test_stale_snapshot_cannot_claim_current_revision`; `test_denied_path_cannot_leak_via_edge_or_label`; `test_untrusted_symbol_name_cannot_enter_instruction_channel`; `test_same_snapshot_has_deterministic_order`; `test_noncode_ci_to_test_edge_survives_budget`; `test_map_never_exceeds_exact_token_budget`.

- **Verifikacija:** Aiderova službena [repo-map dokumentacija](https://aider.chat/docs/repomap.html) potvrđuje graph ranking, dependency edgeove i dinamički token budget. Lokalni `rank.py` ima stvarni PPR/focus/bisection test, a `archmap.py` stvarno indeksira non-code artefakte, typed edgeove, fingerprint generacije i atomic DB replace. Repomix/Continue te obsidian tvrdnje Understand-Anything/Scan/Graphify ostaju matrix kandidati; posebice brojka “71×” nije temelj dizajna bez reproducibilnog benchmarka.

- **Pareto floor:** **PASS uz ownership test.** Dostiže Aiderov code-map floor i dodaje dokazani NEXUS cross-artifact sloj bez drugog index ownera. Strongest counterexample je dynamic dispatch/generated code koji tree-sitter graf ne vidi; mapa mora prikazati edge confidence i ponuditi S4.4 live search, ne izmišljati potpunost. Najslabija točka je kvalitet cross-language resolutiona; mjeri se task-retrieval recallom i coding outcomeom, ne brojem nodeova.

## 8.4 Prompt/inference cache i reuse

- **Tip:** MEHANIZAM + ADAPTERI

- **Aspekti:** LiteLLM je referent za provider-specifičnu propagaciju prompt cache-controla; vLLM za automatic prefix/KV caching, SGLang za RadixAttention u lokalnom servingu. NEXUS `promptlayer` daje stabilni-prefix/tail obrazac, a `respcache` mali exact-response TTL/LRU. Sinteza razdvaja tri različite stvari: (A) API cache hint, (B) lokalni inference prefix/KV adapter i (C) opcionalni exact-response cache; nijedna se ne predstavlja kao druga.

- **Sinteza:** `PromptCachePlanner` označava typed stabilne segmente bez umetanja sentinel bytesa, a S2 provider adapter ih prevodi samo u službeno podržan native format. Lokalni vLLM/SGLang adapter prijavljuje capability/metrics, ali KV memoriju posjeduje inference server. `ResponseCache` je naš content-addressed store samo za eksplicitno deterministic/cachable call siteove i provodi puni Annex P2.4 ključ, authz-before-lookup, generation purge i provenance valuea.

```go
type PromptSegment struct {
    Block contracts.ContextBlock
    Stable bool
    CacheHint CacheHint // NONE|EPHEMERAL|PERSISTENT_IF_SUPPORTED
}
type PromptCachePlanner interface {
    Plan(context.Context, []contracts.ContextBlock, providers.CacheCapabilities) ([]PromptSegment, error)
}

type CacheKey struct {
    Namespace, TenantID, PrincipalScopeHash, AuthzPolicyVersion string
    Sensitivity contracts.Sensitivity
    SourceOrCorpusVersion, ModelID, ProviderID string
    PromptTemplateHash, ToolSchemaHash, ContentHash string
    PurgeGeneration uint64
}
type CacheValue struct {
    ValueHash string
    Lineage []contracts.LineageRef
    CreatedAt, ExpiresAt time.Time
    ProducerRevision, EncryptionKeyRef string
}
type ResponseCache interface {
    Get(context.Context, authz.Decision, CacheKey) (CacheValue, bool, error)
    Put(context.Context, authz.Decision, CacheKey, CacheValue) error
    Invalidate(context.Context, purge.Scope) (uint64, error)
}
```

  Invarijante:

  1. Puni typed `CacheKey` je obvezan; service mode zahtijeva non-empty tenant. Nema wildcarda, fallback ključa ni lookupa prije aktualnog authz decisiona. Policy/corpus/model/provider/prompt/tool schema/content ili purge generation promjena daje drugi ključ.
  2. `SECRET` je `NO_STORE` po defaultu. Dopušten sensitive entry zahtijeva encryption key ref, tenant partition i expiry; cache value nosi lineage izvornog rezultata.
  3. API hint ne mijenja canonical prompt bytes. Unsupported provider hint se uklanja kao metadata, ne kao sadržaj; cache breakpoint ne obuhvaća volatilni tail niti credential.
  4. Lokalni prefix/KV cache aktivan je samo uz `local-model` closure i kompatibilnu inference capability attestation. vLLM/SGLang nisu dependency single-binary baselinea.
  5. Dobitak se mjeri kontroliranim cold/warm parom: hit-rate, TTFT p50/p95, input/cache-read tokene, cijenu i task correctness. Nema univerzalne tvrdnje o uštedi niti HIT bez provider telemetryja/receipt-a.

- **Salvage:** iz `/home/matej/NEXUSv2/core/promptlayer.py:17-61` portirati ideju stabilnog prefixa, uncached taila i provider-specific cache metadata; ne portirati NUL sentinel jer metadata mora biti typed i content-byte invariant. Iz `core/respcache.py:26-71` portirati canonical hash, TTL/LRU bound, deterministic-callsite opt-in i “error/empty is miss”. Postojeći ključ `provider+model+system+prompt` **nije P2.4 compliant** i ne smije se reuseati bez tenant/principal/policy/sensitivity/corpus/tool-schema/purge polja.

- **Ugovor/RED:** Annex **P2.4** je kanonski gate. Obvezni `test_cross_tenant_cache_key_cannot_alias`: tenant B s istim content/prompt hashom ne može izostaviti/krivotvoriti tenant; `CACHE_SCOPE_MISMATCH`, MISS i 0 B pristupa A vrijednosti. Dodatno: `test_policy_version_change_forces_miss`; `test_purge_generation_invalidates_old_entry_atomically`; `test_secret_defaults_no_store`; `test_cache_hint_preserves_prompt_bytes`; `test_unsupported_provider_receives_no_cache_field`; `test_corrupt_value_hash_is_miss_and_quarantine`; `test_warm_benchmark_cannot_claim_hit_without_receipt`.

- **Verifikacija:** provjereno 2026-09-01. vLLM službena dokumentacija potvrđuje automatic prefix caching kao reuse KV blokova za zajednički prefix; SGLang RadixAttention ostaje lokalni adapter claim za matrix potvrdu, ne ugrađena pretpostavka. Lokalni `promptlayer.py` stvarno razdvaja stable prefix/tail i Anthropic metadata, a `respcache.py` stvarno ima bounded TTL/LRU exact cache; pregled također potvrđuje da mu nedostaje cijela P2.4 tenant/policy granica.

- **Pareto floor:** **PASS samo nakon cross-tenant ablationa i warm/cold mjerenja.** Zadržava sva tri korisna cache oblika, ali samo jedan mali typed policy boundary. Strongest counterexample je semantički isti prompt različitog principal scopea: content-addressing nije autorizacija, pa full key i pre-lookup authz ostaju obvezni čak i kad smanjuju hit-rate. Najslabija točka je provider telemetry kvaliteta; bez receipt-a izvještaj kaže `HIT_UNKNOWN`, ne izračunatu uštedu.

## 8.5 Observation pruning: kraćenje izlaza alata na izvoru

- **Tip:** MEHANIZAM + TOOL-FILTER ADAPTERI

- **Aspekti:** RTK je najbolji referent za tool-specific filter-pa-komprimiraj nad širokim CLI skupom; Headroom za multi-tier compression i učenje iz neuspjeha; SWE-agent ACI pager za bounded navigaciju umjesto dumpa. NEXUS `crush` već ima precizne pytest/ruff/mypy/git/docker/cargo/go filtre, critical-line preservation, full-output stash i continuation; to je glavni salvage.

- **Sinteza:** `ObservationReducer` prvo sprema puni raw output kao content-addressed artifact i deterministički ekstrahira strukturirani envelope (`exit`, status, failure spans, diff hunk index). Zatim bira filter po typed tool/command descriptoru, pa generički compressor. Model dobiva bounded `ViewBlock`, `LossMap`, sve essential spans i validan full/continuation ref. Filter exception ne smije izgubiti srednji failure: pre-ekstrahirani failure spans i exit code uvijek prežive, a raw artifact ostaje autoritet.

```go
type Observation struct {
    ToolCallID, ToolID, CommandClass string
    ExitCode *int
    StdoutRef, StderrRef artifacts.Ref
    Blocks []contracts.ContextBlock
}
type EssentialSpan struct {
    Kind string // FAILURE|WARNING|SUMMARY|DIFF_HUNK|RECEIPT
    SourceRef artifacts.Ref
    Start, End int64
    Digest string
}
type ReducedObservation struct {
    View contracts.ContextBlock
    FullRefs []artifacts.Ref
    Essential []EssentialSpan
    Loss LossClass
    OmittedBytes uint64
    Continuation *Continuation
    FilterID, FilterVersion string
}
type ToolFilter interface {
    Match(ToolDescriptor) bool
    Reduce(context.Context, Observation, uint64) (ReducedObservation, error)
}
```

  Invarijante:

  1. Nonzero exit, test failures/panics, verifier verdict, policy denial i causal error uvijek prežive view ili su eksplicitno indeksirani u prvom pageu; exit code nije izveden iz slobodnog teksta.
  2. `git diff` čuva file header, mode/rename/binary metadata i svaki hunk header te `+/-` retke. Ako svi hunks ne stanu, view daje per-hunk index i retrievable pages; ne tvrdi “diff preserved” bez dostupnog punog artefakta.
  3. `read_file`/binary output nema heurističko semantičko filtriranje: daje bounded page + full ref. ANSI/progress/blob normalizacija ne smije promijeniti source artifact niti digest.
  4. Continuation se objavljuje samo nakon provjerenog durable writea i scope-checka; stash failure daje `retrievable=false`. Principal/tenant/policy prate artifact ref.
  5. Headroom-style `learn` smije samo predložiti novu verziju filtera. Aktivacija prolazi holdout regression gate s planted failures/diffovima; runtime session ne samoprepravlja aktivni filter.

- **Salvage:** `/home/matej/NEXUSv2/core/crush.py:21-283` portirati redoslijed preprocessing→tool filter→generic compressor, ANSI/progress/blob handling, repeat folding, JSON/log/head-tail tierove, critical error/warning/summary frameove, full-output private stash i continuation. Iz `crush.py:164-224` portirati command-aware match koji ne zamijeni `echo pytest` stvarnim pytestom; iz `244-283` fail-safe orchestration. Iz `compact.py:110-259` portirati idempotentno observation pruning i stale-read invalidaciju. Promijeniti fail-open: head+tail sam nije dovoljan ako je jedini failure u sredini; essential spans se ekstrahiraju prije filtera.

- **Ugovor/RED:** P0.1/P0.3 vrijede za output, receipt i full ref; bounded output veže Annex P2.2. RED-ovi: `test_middle_test_failure_survives_tiny_budget`; `test_nonzero_exit_survives_filter_exception`; `test_git_diff_preserves_all_hunk_indices_and_changed_lines`; `test_echo_pytest_does_not_select_pytest_filter`; `test_failed_stash_never_advertises_continuation`; `test_secret_output_artifact_is_scope_checked`; `test_reduction_is_idempotent`; `test_learned_filter_cannot_activate_without_holdout_gate`; `test_verifier_failure_is_never_pruned`.

- **Verifikacija:** lokalni pregled potvrđuje tool-specific filtere za pytest/ruff/mypy/pip/npm/git/docker/cargo/go, critical-line preservation, git hunk/changed-line testove, full-output stash i command-aware `echo` zaštitu u `crush.py`. RTK/Headroom širina i kvaliteta ostaju matrix hipoteze dok se ne reproduciraju na istom planted-failure korpusu; naziv/claim nije dokaz da nadmašuju lokalni filter. SWE-agent pager je adapter-referent, ne nužan dependency.

- **Pareto floor:** **PASS tek kad filter-kvar i planted-middle-failure ostanu vidljivi.** Zadržava RTK-ovu specijalizaciju, Headroomovu tier ideju, pager i dokazani NEXUS stash, ali jedan reducer ugovor sprječava četiri paralelna ownera. Strongest counterexample je gigantski diff gdje svi changed lines ne mogu u prompt: potpunost se čuva u durable artefaktu i hunk indexu, ne nemogućim obećanjem da sve stane. Najslabija točka je parser kvaliteta za 100+ CLI formata; novi adapter ulazi samo kad failure-preservation holdout bude GREEN.
