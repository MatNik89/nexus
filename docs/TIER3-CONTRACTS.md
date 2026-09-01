# TIER-3 NORMATIVNI UGOVORI — kanonski set (v0.8 backlog → ugovori)

**Status:** POTVRĐENO — 3× AGREE (kilo+agy: codex-baza strogi nadskup, ništa materijalno ne fali; codex autor baze). Spreman za foldanje u spec. Baza = TIER3-codex.md (najrigorozniji: tipizirana
polja + state-machine + invarijante + RED test za svih 17). kilo (TIER3-kilo.md) i agy
(TIER3-agy.md) neovisno došli na ISTIH 17 ugovora, iste vlasnike, iste RED testove —
potvrda, ne alternativa. Razlike samo u razini detalja.

**Redoslijed do produkcije:** V1-V8 (gotovo) → OVI UGOVORI foldani u spec → verifikacijska matrica.
Ovi ugovori su kriteriji PO KOJIMA matrica ocjenjuje kandidate — bez njih matrica rangira po
nedefiniranim zahtjevima. Svaki MUST je matrični kriterij; svaki RED test je red-capable
(kontrolirana mutacija krši invariant, oracle gleda trajno stanje/sink, ne vraćeni error).

---


# Normativne konvencije

- `MUST`/`MUST NOT` su obvezni kriteriji verifikacijske matrice.
- Svako odbijanje MUST vratiti tipizirani `error.code`, zapisati kanonski događaj i završiti prije nedopuštenog sinka ili prijelaza stanja.
- Svaki RED test MUST biti red-capable: kontrolirana mutacija krši navedeni invariant; oracle promatra trajno stanje, proces ili vanjski sink, ne samo vraćeni error.
- ID-evi MUST biti stabilni kroz retry/replay; timestampi MUST biti UTC; deadline/lease trajanja MUST koristiti monotoni sat.

# P0 — kanonski ugovori i vlasništvo

## P0.1 — S0 kanonski envelope, state-machine, migracija i `ContextBlock` (codex #6–8)

- **Vlasnik:** S0.1–S0.4; svi adapteri konzumiraju isti ugovor.
- **Zajednički `Envelope` MUST imati:** `schema_id`, `schema_version`, `event_id`, `event_type`, `run_id`, nullable `turn_id`, nullable `tool_call_id`, nullable `parent_event_id`, `sequence`, `emitted_at`, `actor_type`, `actor_id`, `principal_id`, nullable `tenant_id`, `workspace_id`, `attempt_no`, nullable `idempotency_key`, `payload`, `payload_hash`.
- **`Message` MUST imati:** `message_id`, `role` (`SYSTEM|USER|ASSISTANT|TOOL`), `content_blocks[]`, `created_at`; svaki element `content_blocks` MUST biti `ContextBlock`.
- **`ToolCall` MUST imati:** `tool_call_id`, `tool_id`, `arguments`, `arguments_schema_hash`, `effect_class` (`READ_ONLY|REVERSIBLE|IRREVERSIBLE`), `deadline`, `attempt_no`, `idempotency_key` za svaki state-changing poziv.
- **`ToolResult` MUST imati:** `tool_call_id`, `attempt_no`, `status` (`SUCCEEDED|FAILED|CANCELLED|UNKNOWN`), `output_blocks[]`, nullable `error`, `started_at`, `finished_at`; rezultat MUST korelirati s postojećim pozivom i istim attemptom.
- **`TypedError` MUST imati:** `code`, `category` (`VALIDATION|AUTHN|AUTHZ|POLICY|RESOURCE|TIMEOUT|CANCELLED|DEPENDENCY|CONFLICT|INTERNAL|UNKNOWN_EFFECT`), `retryability` (`NEVER|S7_POLICY|AFTER`), `safe_message`, nullable `retry_after`, `origin`, nullable `cause_event_id`; proizvoljni string ne smije upravljati retryjem.
- **`ContextBlock` MUST imati:** `block_id`, `kind`, `content` ili `content_ref` (točno jedno), `content_hash`, `source_uri`, `producer`, `trust_class` (`SYSTEM|USER|TOOL_TRUSTED|UNTRUSTED_EXTERNAL`), `sensitivity` (`PUBLIC|INTERNAL|CONFIDENTIAL|SECRET`), `lineage[]`, `observed_at`, nullable `expires_at`.
- **Stateovi MUST biti:** run `CREATED→ADMITTED→RUNNING→{SUCCEEDED|FAILED|CANCELLED|UNKNOWN}`; turn `CREATED→RUNNING→{SUCCEEDED|FAILED|CANCELLED}`; tool attempt `PLANNED→AUTHORIZED→RUNNING→{SUCCEEDED|FAILED|CANCELLED|UNKNOWN}`. `SUCCEEDED|FAILED|CANCELLED` su terminalni; `UNKNOWN` smije prijeći samo u `SUCCEEDED|FAILED|MANUAL_RECOVERY` putem eksplicitnog reconciliation događaja.
- **Migracija MUST:** čuvati immutable raw event; koristiti jedinstveni registry `schema_id/version`; upcastati monotono verziju-po-verziju; karantenirati nepoznati schema ID/verziju; zabraniti lossy downcast; zapisati `migration_from`, `migration_to`, `migration_id`, `input_hash`, `output_hash`.
- **Invarijante:** `sequence` strogo raste po runu; ID i causal parent se ne mijenjaju kroz migraciju; transformacije S8/S9/S10/S11 MUST očuvati `lineage` te smiju samo pooštriti `trust_class`/`sensitivity`; unknown polje se očuva, unknown discriminator se odbija.
- **RED — `test_s0_rejects_provenance_laundering`:** untrusted `ContextBlock` prođe kroz compaction/upcaster koji ukloni `lineage` i postavi `trust_class=SYSTEM`; prompt assembly MUST vratiti `PROVENANCE_DOWNGRADE`, run ne smije prijeći u `RUNNING`, a model sink mora imati 0 poziva.

## P0.2 — Jedini owner retryja, deadlinea i cancela: S7 (codex #20)

- **Vlasnik:** S7.1; S2.2 samo normalizira provider error i predlaže fallback target; S3.3 emitira user cancel; S3.4 prikazuje konačni `TypedError` modelu bez retryja.
- **`ExecutionPolicy` MUST imati:** `operation_id`, `effect_class`, `deadline`, `max_attempts`, `attempt_timeout`, `backoff_policy`, `retryable_codes[]`, `fallback_targets[]`, `idempotency_key`, `cancel_token_id`.
- **`AttemptGrant` MUST imati:** `operation_id`, `attempt_no`, `target_id`, `issued_at`, `expires_at`, `grant_nonce`; svaki fizički provider/tool pokušaj MUST predočiti jedinstveni S7 grant.
- **Stanja:** `PENDING→GRANTED→RUNNING→{SUCCEEDED|FAILED_RETRYABLE|FAILED_TERMINAL|CANCELLED|UNKNOWN}`; samo S7 smije iz `FAILED_RETRYABLE` izdati sljedeći `AttemptGrant`.
- **Invarijante:** zbroj attempta ne prelazi `max_attempts`; deadline i cancel propagiraju se u sve child taskove; `CANCELLED` je terminalan; `IRREVERSIBLE+UNKNOWN` se ne retrya bez reconciliationa; adapter/loop ne smije sadržavati vlastitu retry petlju.
- **RED — `test_adapter_cannot_self_retry`:** lažni provider dvaput pozove transport s istim `AttemptGrant`; drugi poziv MUST pasti s `ATTEMPT_NOT_AUTHORIZED`, transport counter mora ostati 1, a S7 attempt counter 1.

## P0.3 — Jedini write-owner za event/log/trace/transcript/audit (codex #21)

- **Vlasnik:** `EventJournal.append()` u S0/S15; S1.3, S15.1 i S15.3 su read-only projekcije.
- **`JournalEvent` MUST koristiti S0 `Envelope` i dodatno imati:** `journal_offset`, `redaction_policy_version`, `integrity_prev_hash`, `integrity_hash`, nullable `sealed_payload_ref`.
- **Write put:** `validate schema → classify/redact → atomic append+sequence allocation → durable flush → publish projection offsets`; nijedan sink ne smije sam alocirati event ID/sequence.
- **Projekcije MUST imati:** `source_event_id`, `source_offset`, `projection_version`; log = dijagnostička projekcija, trace = span projekcija, transcript = user-visible projekcija, security audit = append-only projekcija s vanjskim integrity checkpointom.
- **Stanja projekcije:** `UNSEEN→APPLIED`; replay istog `source_event_id+projection_version` je idempotentan; offset se ne potvrđuje prije durable zapisa.
- **Invarijante:** nema paralelnog write API-ja; redakcija se događa prije journala/projekcija; projekcija ne smije izmijeniti source događaj; replay i audit moraju referencirati isti `event_id`; tajna smije postojati samo kao autorizirani sealed reference, nikad u indeksiranom payloadu.
- **RED — `test_projection_cannot_bypass_journal`:** trace exporter pokuša izravno zapisati event s canary tajnom bez `source_event_id`; write MUST pasti s `NON_CANONICAL_WRITE`, nijedan sink ne smije sadržavati canary i napadački event ne smije biti appendan (zaseban redaktirani rejection-event je dopušten).

## P0.4 — Capability/feature-flag closure resolver (codex #9)

- **Vlasnik:** nova S0.5; jedini aktivator capability flagova i njihovih gateova.
- **`CapabilityManifest` MUST imati:** `manifest_schema_version`, `flag_id`, `flag_version`, `provides[]`, `requires[]`, `conflicts[]`, `gates[]`, `adapters[]`, `trigger_id`, `trigger_version`, `config_hash`, `signature`.
- **`GateAttestation` MUST imati:** `gate_id`, `gate_version`, `implementation_id`, `config_hash`, `status`, `verified_at`, `expires_at`, `evidence_ref`.
- **Stanja:** `DECLARED→RESOLVED→VALIDATED→ACTIVE`; failure ide u `REJECTED`; parcijalno `ACTIVE` stanje nije legalno.
- **Resolver MUST:** proširiti tranzitivni closure; detektirati cycle/conflict/unknown version; topološki aktivirati; potvrditi sve gate attestatione nad istim config hashom; atomically objaviti aktivni set.
- **Invarijante:** adapter se ne učitava prije `ACTIVE`; manifest/policy može samo suziti kernel maksimum; promjena manifesta invalidira closure i zahtijeva novu atomsku aktivaciju; bundle-alias nije capability i ne smije prisilno aktivirati flag bez njegova triggera.
- **RED — `test_every_flag_fails_when_one_gate_is_ablated`:** za svaki od 18 flagova ukloniti po jedan deklarirani `gate`; resolver MUST vratiti `INCOMPLETE_CAPABILITY_CLOSURE`, aktivni set ostaje prethodna verzija i nijedan adapter iz nevaljanog closurea nije učitan.

# P1 — sigurnosne granice

## P1.1 — Runtime TOCTOU provjera skilla (`skills.lock`) (agy #1)

- **Vlasnik:** S11.5+S6.8 loader; install-time scan nije dovoljan.
- **`skills.lock` entry MUST imati:** `lock_schema_version`, `skill_id`, `skill_version`, `canonical_root`, `tree_digest_alg`, `tree_digest`, `file_manifest[]` (`relative_path`,`mode`,`size`,`digest`), `signer_id`, `signature`, `permission_set_hash`, `dependency_digests[]`, `scanner_policy_version`, `approved_at`.
- **Stanja:** `STAGED→SCANNED→PINNED→VERIFIED_ON_LOAD→ACTIVE`; mismatch vodi u `QUARANTINED`; revocation vodi u `REVOKED`.
- **Load MUST:** otvoriti canonical root bez symlink escapea; hashirati canonical path+mode+bytes; verificirati signature/permission hash; izvršiti ili prompt-assembleati točno verificirane bytes preko content-addressed objekta/open handlea, bez path re-read TOCTOU prozora.
- **Invarijante:** provjera pri svakom process startu, aktivaciji i hot reloadu; lock i skill moraju biti na istom digestu; nepoznata datoteka ili permission delta fail-closed; agent-authored skill ostaje inertan do nove verifikacije.
- **RED — `test_skill_swap_after_install_is_quarantined`:** nakon validnog installa zamijeniti `SKILL.md` symlinkom ili promijeniti jedan byte prije loadanja; loader MUST vratiti `SKILL_DIGEST_MISMATCH`, stanje `QUARANTINED`, a promijenjeni sadržaj ne smije ući u prompt ni executor.

## P1.2 — Startup orphan/process-tree/workspace-lock sweep (agy #2)

- **Vlasnik:** S7.3 startup recovery; samo resursi s dokazanim vlasništvom harnessa.
- **`ResourceLease` MUST imati:** `resource_id`, `resource_type` (`PROCESS_TREE|WORKSPACE_LOCK|TEMP_DIR`), `run_id`, `owner_instance_id`, nullable `pid`, nullable `pid_start_token`, nullable `process_group_or_cgroup`, `canonical_path`, `acquired_at`, `heartbeat_at`, `expires_at`, `cleanup_policy`, `fencing_token`.
- **Stanja:** `OWNED→ORPHAN_SUSPECTED→FENCED→CLEANING→RELEASED`; nedokazivo vlasništvo vodi u `QUARANTINED`, ne u kill/delete.
- **Sweep MUST:** usporediti durable run state, lease expiry, PID start-token i process group; prvo fenceati novi rad; terminirati cijelo dokazano stablo `TERM→bounded wait→KILL`; potvrditi exit; tek tada otpustiti lock i označiti cleanup dovršenim.
- **Invarijante:** PID bez start-tokena nije dokaz identiteta; aktivan lease se ne dira; symlink/putanja izvan workspace roota se ne čisti; djelomični cleanup ostaje `CLEANING/QUARANTINED`, nikad `RELEASED`.
- **RED — `test_sigkill_restart_reaps_owned_tree_only`:** SIGKILLati worker nakon što ostavi child proces i workspace lock, zatim pokrenuti recovery uz nepovezani proces s recikliranim PID-om; sweep MUST ugasiti dokazani child, osloboditi njegov lock i ostaviti nepovezani proces živim.

## P1.3 — MCP transport, peer identity i auth ugovor (codex #14)

- **Vlasnik:** S4.6 uz S6.5/S6.6/S7; primjenjuje se i na lokalni single-user harness.
- **`McpPeerDescriptor` MUST imati:** `server_id`, `transport` (`LOCAL_STDIO|REMOTE_HTTPS`), `endpoint`, `expected_identity` (local executable digest ili remote issuer/audience/SPKI pin), `auth_method`, `requested_scopes[]`, `credential_ref`, `trust_policy_version`, `allowed_capabilities[]`, `max_tool_count`, `max_schema_bytes`, `max_description_bytes`, `expires_at`.
- **Stanja:** `DISCOVERED_UNTRUSTED→IDENTITY_VERIFIED→AUTHENTICATED→AUTHORIZED→CONNECTED`; mismatch/revocation vodi u `REJECTED/REVOKED` i zatvara transport.
- **Invarijante:** remote transport MUST koristiti TLS i verificirani peer; redirect/DNS reconnect zahtijeva ponovnu provjeru; credential broker šalje samo scope intersection i nikad credential u prompt/log; `tools/list` je untrusted `ContextBlock`; schema/description prolaze size, provenance i injection fencing prije registracije; cancel/reconnect pripada S7.
- **RED — `test_mcp_wrong_pinned_peer_gets_no_credentials`:** server vrati valjan TLS cert za drugo dopušteno ime, ali ne odgovara pin/issuer-audience zapisu; klijent MUST vratiti `MCP_PEER_IDENTITY_MISMATCH`, credential sink i tool registry moraju ostati prazni.

## P1.4 — `draft→approve→commit→verify→compensate` za ireverzibilne učinke (codex #16)

- **Vlasnik:** S6.9+S7; tool adapter implementira plan/commit/verify/compensate iza ugovora.
- **`EffectIntent` MUST imati:** `intent_id`, `effect_type`, `effect_class`, `principal_id`, nullable `tenant_id`, `target`, `canonical_payload`, `payload_hash`, `preview`, `idempotency_key`, `created_at`, `expires_at`, `required_approval`, nullable `compensator`, `recovery_owner`.
- **`ApprovalGrant` MUST imati:** `grant_id`, `intent_id`, `payload_hash`, `principal_id`, `allowed_effect`, `issued_at`, `expires_at`, `single_use_nonce`, `decision`.
- **Stanja:** `DRAFT→VALIDATED→APPROVAL_PENDING→APPROVED→COMMITTING→{COMMITTED|UNKNOWN}`; `COMMITTED→VERIFIED`; failure može u `COMPENSATING→COMPENSATED` ili `MANUAL_RECOVERY`. Cancel je legalan samo prije `COMMITTING`.
- **Invarijante:** intent se durable zapisuje prije vanjskog poziva; approval je vezan uz exact payload/target/effect i single-use; commit koristi intent-stabilan idempotency key; timeout nakon slanja daje `UNKNOWN`, ne automatski retry; uspjeh zahtijeva provider receipt + postcondition; compensator nije rollback tvrdnja dok nije verificiran.
- **RED — `test_approval_cannot_authorize_modified_effect`:** nakon previewa i approvala promijeniti recipient/amount uz isti grant; commit MUST vratiti `APPROVAL_BINDING_MISMATCH`, vanjski sink mora imati 0 poziva, a grant mora biti opozvan.

## P1.5 — Distribucijski artefakt obvezno aktivira S6.8 (codex #15)

- **Vlasnik:** `distributed-artifact` closure u S0.5; zahtijeva S6.8+S17.2+S17.3.
- **`ArtifactManifest` MUST imati:** `artifact_id`, `artifact_type`, `version`, `platform`, `artifact_digest`, `source_revision`, `build_recipe_digest`, `builder_identity`, `sbom_digest`, `provenance_attestation_digest`, `signer_id`, `signature`, `permission_delta`, `scan_policy_version`, `scan_result`, `update_metadata_version`, `update_metadata_expiry`, `release_channel`.
- **Stanja:** `BUILT→SCANNED→ATTESTED→SIGNED→STAGED→PUBLISHED`; kompromitacija/istek vodi u `REVOKED`; preskakanje stanja nije legalno.
- **Invarijante:** svi dokazi vežu isti `artifact_digest`; publish/update installer verificira signature, provenance, permission delta, vulnerability policy i non-expired update metadata prije byte writea; plugin-specifični scan može biti dodatak, ne zamjena baselineu.
- **RED — `test_post_signature_artifact_tamper_blocks_publish`:** promijeniti jedan byte staged binara nakon potpisa; publish MUST vratiti `ARTIFACT_DIGEST_MISMATCH`, release kanal i install target moraju ostati byte-identični prethodnom stanju.

## P1.6 — `channels→extensions` closure i jezgreni remote-HITL token (codex #10)

- **Vlasnik:** S0.5 deklarira `channels requires extensions+7.3+approval-core`; S6 posjeduje auth/approval, S7 durable wait/resume, S12.4 je samo multi-agent potrošač.
- **`ChannelAdapterManifest` MUST imati:** `adapter_id`, `adapter_version`, `artifact_digest`, `channel_type`, `permissions[]`, `network_endpoints[]`, `provenance`, `signature`, `lifecycle_entry_id` (S11.5), `supply_chain_attestation_id` (S6.8).
- **`ApprovalChallenge` MUST imati:** `challenge_id`, `run_id`, `intent_id`, `payload_hash`, `principal_id`, `channel_identity`, `issued_at`, `expires_at`, `single_use_nonce`, `allowed_decisions[]`.
- **Stanja HITL-a:** `WAITING→{APPROVED|DENIED|EXPIRED|CANCELLED}`; samo S7 durable transition smije nastaviti run; ponovni odgovor na terminalni challenge je replay.
- **Invarijante:** channel adapter se ne registrira prije active `extensions` closurea; odgovor mora biti autentificiran kao isti principal+channel binding; token je exact-intent, expiring i single-use; approval ne može proširiti kernel policy; adapter ne može sam nastaviti run.
- **RED — `test_channels_contract_fails_closed` (tablični):** slučaj A aktivira `channels` bez `extensions/S6.8` attestationa; slučaj B dvaput pošalje isti valjani approval odgovor; A MUST dati `INCOMPLETE_CAPABILITY_CLOSURE`, B `APPROVAL_REPLAY`, a u oba slučaja nema adapter side-effecta niti drugog resumea.

# P2 — pouzdanost i podaci

## P2.1 — `MEMORY_FORGET` nasuprot `DATA_PURGE` (codex #12)

- **Vlasnik:** S9 posjeduje `MEMORY_FORGET`; S6.7 posjeduje autoritativni `DATA_PURGE` i uvijek nadjačava S9.
- **`MemoryForget` MUST imati:** `operation_id`, `memory_ids[]` ili immutable `selection_snapshot_hash`, `principal_id`, `reason`, `requested_at`, nullable `reversible_until`, `tombstone_version`; učinak je isključivo zabrana recalla/rankinga.
- **`DataPurge` MUST imati:** `purge_id`, `subject_id`, nullable `tenant_id`, `authority`, `scope`, `store_targets[]`, `requested_at`, `approval_hash`, `legal_hold_check`, `per_store_status[]`, `backup_disposition`, nullable `completed_at`.
- **Stanja forgeta:** `ACTIVE→FORGOTTEN→RESTORED` do roka. **Stanja purgea:** `REQUESTED→AUTHORIZED→RUNNING→{COMPLETED|PARTIAL|BLOCKED_LEGAL_HOLD|FAILED}`; `COMPLETED` je ireverzibilan.
- **Invarijante:** forgotten sadržaj ostaje arhiviran ali nikad u recallu; purge briše hot/warm/cold, vektor, graf, cache, indekse i izvedene exporte te definira backup expiry/crypto-erasure; purge briše i forget tombstone content; `COMPLETED` tek nakon potvrde+post-delete probe svakog storea; audit čuva samo minimalni non-content dokaz.
- **RED — `test_purge_cannot_complete_with_residual_copy`:** isti subject staviti u memory, vector, cache i backup index; jedan adapter lažno vrati success bez brisanja; post-delete probe MUST postaviti stanje `PARTIAL`, vratiti `PURGE_INCOMPLETE` i zabraniti `COMPLETED` događaj.

## P2.2 — Izvršni bounded resource budgets (codex #17)

- **Vlasnik:** S7.2 accounting/cancel; S4.2/S6.2 OS enforcement; adapter ne smije sam proglasiti limit provedenim.
- **`ResourceBudget` MUST imati po runu i tool attemptu:** `wall_ms`, `cpu_ms`, `rss_bytes`, `disk_write_bytes`, `file_count`, `process_count`, `socket_count`, `network_in_bytes`, `network_out_bytes`, `output_bytes`, `input_tokens`, `output_tokens`, `cost_minor_units`, `tool_calls`, `loop_steps`; svaki limit ima `SOFT|HARD` i jedinicu.
- **`ResourceLedger` MUST imati:** `budget_id`, `parent_budget_id`, `reserved`, `consumed`, `observed_at`, `source`, `fencing_token`.
- **Stanja:** `AVAILABLE→RESERVED→CONSUMING→{RELEASED|EXHAUSTED|CANCELLED}`.
- **Invarijante:** child budget ≤ preostali parent; kumulativni counteri monotono rastu, a gaugeovi (`rss/process/socket`) knjiže peak; hard limit atomically fencea nove radnje, cancela i drain/kill cijelo owned stablo; soft limit samo emitira signal; cleanup se ne knjiži kao uspjeh; supported platform mora imati realni enforcement ili capability fail-closed.
- **RED — `test_hard_process_budget_kills_descendants`:** tool s `process_count=2` pokrene parent+2 child procesa; treći proces MUST izazvati `RESOURCE_LIMIT_EXCEEDED`, cijelo owned stablo mora završiti, nijedan kasniji tool ne smije krenuti, ledger mora pokazati `EXHAUSTED`.

## P2.3 — Queue lease, reclaim i fencing (codex #18)

- **Vlasnik:** S7.2 semantika; S18.1 distributed adapter.
- **`QueueTask` MUST imati:** `task_id`, `payload_hash`, `idempotency_key`, `state`, `attempt_no`, `max_attempts`, nullable `lease_id`, nullable `owner_worker_id`, `fencing_token`, nullable `leased_at`, nullable `heartbeat_at`, nullable `lease_expires_at`, nullable `result_hash`.
- **Stanja:** `READY→LEASED→RUNNING→COMMIT_PENDING→SUCCEEDED`; expiry/crash daje `LEASED/RUNNING→READY` uz novi attempt i strogo veći fencing token; `COMMIT_PENDING` s nepoznatim vanjskim učinkom ide u `UNKNOWN_EFFECT→RECONCILING`, nikad izravno u `READY`; terminalno još `FAILED|DEAD_LETTER|CANCELLED`.
- **Invarijante:** claim/reclaim je atomski compare-and-swap; heartbeat vrijedi samo za aktualni lease+token; svaka state mutacija i result/side-effect commit nosi aktualni fencing token; stari token se odbija; ACK tek nakon durable result commit; jedan task+idempotency key ima najviše jedan prihvaćen terminalni rezultat; ireverzibilni commit koristi P1.4 reconciliation ugovor.
- **RED — `test_reclaimed_worker_cannot_commit_with_stale_fence`:** worker A dobije token 7 i bude zamrznut (`SIGSTOP`) preko expiryja; worker B dobije token 8; nakon nastavka A pokuša commit s 7 dok B commita s 8; A MUST dobiti `STALE_FENCING_TOKEN`, a durable store mora sadržavati točno jedan rezultat, B-ov.

## P2.4 — Cache tenant/principal/policy/provenance granica (codex #19)

- **Vlasnik:** S8.4; S6.6/S10.6 daju authz/policy input, S6.7 purge hook.
- **`CacheKey` MUST imati:** `namespace`, `tenant_id` (non-null u service modu), `principal_scope_hash`, `authz_policy_version`, `sensitivity`, `source_or_corpus_version`, `model_id`, `provider_id`, `prompt_template_hash`, `tool_schema_hash`, `content_hash`, `purge_generation`.
- **`CacheValue` MUST imati:** `value_hash`, `lineage[]`, `created_at`, `expires_at`, `producer_revision`, `encryption_key_ref` kad je dopušten osjetljiv sadržaj.
- **Stanja:** `MISS→WRITTEN→HIT`; policy/revocation/purge vodi u `INVALIDATED`; expired/revoked entry nikad nije `HIT`.
- **Invarijante:** lookup zahtijeva cijeli typed key, bez wildcard fallbacka; namespace je tenant-partitioned; authz se evaluira prije lookup/writea; `SECRET` je `NO_STORE` po defaultu; policy/corpus/model/prompt promjena mijenja key; purge/revocation atomically povećava generation i onemogućuje stare entryje.
- **RED — `test_cross_tenant_cache_key_cannot_alias`:** tenant A upiše entry, tenant B pošalje isti content/prompt hash i pokuša izostaviti ili krivotvoriti tenant dio; schema/authz MUST vratiti `CACHE_SCOPE_MISMATCH`, B dobiva MISS i nikad sadržaj A.

## P2.5 — Provider data-processing i rezidencijski preflight (codex #13)

- **Vlasnik:** S0.3/S2.1 descriptor; S6.7/S6.9 enforcement prije mrežnog sinka; svaki fallback ponovno prolazi gate.
- **`ProviderDataDescriptor` MUST imati:** `provider_id`, `model_id`, `endpoint`, `processing_regions[]`, `storage_regions[]`, `retention_seconds`, `zero_retention`, `training_use`, `human_review`, `encryption_in_transit`, `encryption_at_rest`, `accepted_data_classes[]`, `subprocessor_policy_version`, `dpa_version`, `descriptor_source`, `descriptor_digest`, `signature`, `attested_at`, `expires_at`.
- **`RequestDataPolicy` MUST imati:** `principal_id`, nullable `tenant_id`, `purpose`, `data_classes[]`, `allowed_processing_regions[]`, `max_retention_seconds`, `training_allowed`, `human_review_allowed`, `required_encryption`, `provider_allowlist[]`, nullable `consent_ref`.
- **Stanja:** provider target `CANDIDATE→POLICY_EVALUATED→AUTHORIZED→SENT`; unknown/expired descriptor ili mismatch vodi u `DENIED`, ne fallback-send.
- **Invarijante:** evaluacija se događa prije serializacije i DNS/connecta; unknown vrijednost je najrestriktivnija/deny; request policy i provider descriptor koriste intersection; redakcija ne smije lažno sniziti data class bez lineagea; svaki fallback/reroute se ponovno autorizira; descriptor/decision se auditira bez prompt sadržaja.
- **RED — `test_fallback_cannot_bypass_residency_policy`:** primarni EU/zero-retention provider vrati retryable error, fallback je US+training; S7 MUST dobiti `DATA_POLICY_DENIED`, fallback network sink mora imati 0 poziva, a audit mora sadržavati denied provider ID i policy version bez sadržaja prompta.

## P2.6 — Automation clock, DST, missed-run i overlap semantika (codex #33)

- **Vlasnik:** S3.6; S7 daje idempotency/lease, ne rasporednu semantiku.
- **`ScheduleManifest` MUST imati:** `schedule_id`, `schedule_version`, `expression`, `iana_timezone`, `dst_gap_policy` (`SKIP|NEXT_VALID|ERROR`), `dst_fold_policy` (`ONCE_FIRST|ONCE_SECOND|TWICE`), `missed_run_policy` (`SKIP|COALESCE|CATCH_UP`), `max_catch_up`, `max_lateness_ms`, `overlap_policy` (`FORBID|QUEUE|PARALLEL`), `max_concurrency`, `start_at`, nullable `end_at`.
- **`ScheduledOccurrence` MUST imati:** `schedule_id`, `scheduled_local_time`, `scheduled_instant_utc`, `fold_index`, `occurrence_id`, nullable `admitted_at`; za `TWICE` occurrence/idempotency MUST uključiti UTC instant+fold index, a za `ONCE_FIRST|ONCE_SECOND` samo policy-odabrani fold smije proizvesti occurrence.
- **Stanja:** `CALCULATED→DUE→{ADMITTED|MISSED|COALESCED|SUPPRESSED_OVERLAP}→RUNNING→TERMINAL`.
- **Invarijante:** raspored čuva UTC instant i IANA zonu; wall clock računa occurrence, monotoni clock mjeri lateness/lease; restart ponovno računa iste occurrence ID-eve; missed/overlap politika je eksplicitna, bounded i auditirana; `FORBID` je single-flight.
- **RED — `test_zagreb_dst_fold_runs_once_across_restart`:** raspored `30 2 * * *`, zona `Europe/Zagreb`, `ONCE_FIRST`, restart između dva pojavljivanja 02:30 pri jesenskom DST foldu; scheduler MUST proizvesti točno jedan occurrence/run i jedan idempotency key, a drugi fold ne smije doći do admissiona ni side-effecta.

## P2.7 — Formalni Council ugovor, nova S12.6 (agy #5)

- **Vlasnik:** nova S12.6; S16.6 ga konzumira kao `multi-agent` strategiju, ali deterministic checker/promotion gate ostaje neovisan.
- **`CouncilRequest` MUST imati:** `council_id`, `run_id`, `artifact_revision_hash`, `question`, `member_specs[]` (`member_id`,`role_id`,`model_policy`,`context_policy`), `min_quorum`, `independence_policy`, `anonymization_policy`, `evidence_refs[]`, `deadline`, `budget_id`, `chair_spec`, `aggregation_rule`, `dissent_policy`, `output_schema_version`.
- **`ReviewRecord` MUST imati:** `review_id`, `sealed_input_hash`, `member_pseudonym`, `finding_id`, `claim`, `evidence_refs[]`, `severity`, `confidence`, `submitted_at`, `signature`.
- **Stanja:** `FORMED→INPUTS_SEALED→INDEPENDENT_REVIEW→CROSS_REVIEW→SYNTHESIS→{VERIFIED|FAILED_QUORUM|REJECTED}`; član ne vidi tuđe zapise prije vlastitog sealed commita.
- **Invarijante:** svi članovi rade nad istim artifact hashom; cross-review skriva identitet; chair ne smije mijenjati evidence ref ni izbaciti materijalni dissent bez eksplicitnog adjudication recorda; quorum i budget su hard gate; output nosi majority, dissent i unresolved; council ne može sam promovirati niti zamijeniti 16.6 checker.
- **RED — `test_council_cannot_drop_sealed_material_dissent`:** jedan sealed review s materijalnim dokazom proturječi većini, a chair synthesis ga izostavi; verifier MUST vratiti `DISSENT_DROPPED`, council ostaje `REJECTED` i promotion gate ne smije dobiti success signal.
