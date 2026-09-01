PLAN S6

# S6 — Sigurnost i dozvole

## Zajedničke odluke, threat model i paket-layout

- S6 je non-bypassable sigurnosna membrana između modelom/proširenjem kontroliranog prijedloga i stvarnog učinka. Primarni napadački ulazi su model output, tool/MCP schema i rezultat, repo sadržaj i hook, URL/DNS/redirect, plugin bytes, remote approval te tenant/principal identitet. Zaštićeni asseti su korisničke datoteke, procesi, mreža, credentiali, PII, drugi tenant i integritet audita.
- Predloženi layout je `internal/security/{contract,trust,policy,approval,lifecycle,secrets,egress,identity,governance,supplychain,canary}` i `internal/membrane/gortex/{ipc,sandbox_linux,sandbox_windows,sandbox_darwin,process,attest}`. S6 donosi/provodi security odluke; S7 jedini radi retry/cancel/durable wait; S5 jedini objavljuje file mutaciju; P0.3 journal jedini trajno zapisuje security događaj.
- GORTEX se **direktno reusea kao Go kod**, ne portira i ne prepisuje. Reuse znači zadržati dokazano ponašanje `landlock_linux.go`, `sandbox.go` i `process_manager.go` iza novog typed IPC-a te prvo zaključati legacy ponašanje parity corpusom. Ne znači tvrditi da postojeći kod već ima seccomp, autentificirani IPC, Windows Job Object, macOS Seatbelt ili S7 durable lease: to mora biti dodano bez slabljenja membrane.
- Svaki backend objavljuje mjerenu `EnforcementCapability`, ne marketinški naziv. Ako policy traži `ENFORCED`, a platforma daje `OBSERVED|UNAVAILABLE`, izvršenje je odbijeno. String/shlex klasifikatori i LLM detektori su tripwire/defense-in-depth, nikad zamjena za OS enforcement.
- Pravilo kompozicije: kernel policy i trust klasifikacija postavljaju gornju granicu; korisnik može odobriti samo eksplicitno approvable efekt; workspace/plugin pravilo smije samo suziti; adapter nikad ne smije proširiti. Konačna odluka je meet svih slojeva, s `DENY > ASK > ALLOW`.

```go
package security

type DecisionKind uint8 // ALLOW|ASK|DENY
type EnforcementLevel uint8 // ENFORCED|OBSERVED|UNAVAILABLE

type Subject struct {
    PrincipalID string
    TenantID    *string
    SessionID   string
    AuthnLevel  AuthnLevel
    Roles       []string
}

type Resource struct {
    Kind        string
    CanonicalID string
    WorkspaceID workspace.WorkspaceID
    Owner       SubjectRef
    Sensitivity contracts.Sensitivity
}

type ActionRequest struct {
    Envelope       contracts.Envelope
    ActionID       loop.ActionID
    Subject        Subject
    Tool           tools.ToolID
    Effect         contracts.EffectClass
    CanonicalArgs  json.RawMessage
    ArgsDigest     string
    Resources      []Resource
    DataClasses    []DataClass
    Attempt        scheduler.AttemptGrant
}

type Decision struct {
    Kind          DecisionKind
    PolicyVersion string
    RequestDigest string
    Reasons       []ReasonCode
    Constraints   []Constraint
    Approval      *ApprovalRequirement
    ExpiresAt     time.Time
}
```

Globalne invarijante:

1. Schema validacija i canonicalization prethode policyju; `Decision.RequestDigest` veže točan subject, tenant, tool digest, argumente, resource set, effect i policy verziju. Promjena bilo čega poništava odluku.
2. DENY i security error su fail-closed; “detektor nije dostupan”, “sandbox nije podržan”, “identity je unknown” i “audit nije durable” nisu ALLOW.
3. Security događaji ne sadrže secret/PII payload. Bilježe identitete, digest, policy verziju, reason code, decision i evidence ref kroz P0.3.
4. GORTEX prihvaća samo autentificirani, verzionirani, length-bounded typed IPC s nonceom, peer identityjem i S7 fencing tokenom; lokalni `0600` socket sam nije dovoljan identitet/autorizacija.
5. Svaka legacy→Go-reuse promjena prvo pokreće zamrznuti parity corpus nad starim GORTEX binarom i reuseanim paketom, zatim nove RED testove. Razlika je dopuštena samo kao eksplicitno stroži rezultat ili odobrena migracija ugovora.

## 6.0 Trust classification i policy enforcement point

- **Tip:** MEHANIZAM

- **Aspekti:** OPA daje kontekstualni policy-as-data i embedded Go/Wasm evaluaciju; Codex approval model daje jasan sandbox/approval spoj; OpenHands security analyzer daje akcijski preflight. Najbolji graft je OPA-ov decision input/output oblik uz naš mali compiled baseline policy kao neuklonjivi floor; analyzer i detektori daju samo evidence, ne finalnu ovlast.

- **Sinteza:** `PolicyEngine` klasificira svaki `ContextBlock`, subject, resurs i efekt, zatim jedan PEP evaluira prije bilo kojeg adaptera. Embedded OPA je opcionalni policy adapter iza interfacea; single-binary baseline ostaje typed Go evaluator kako policy runtime ne bi postao availability ili bypass owner.

```go
type TrustClass uint8 // SYSTEM|USER|VERIFIED_EXTENSION|TOOL_UNTRUSTED|MODEL_UNTRUSTED|UNKNOWN

type PolicyInput struct {
    Request       ActionRequest
    Context       []contracts.ContextBlock
    Environment   EnvironmentFacts
    Capability    contracts.EffectiveCapabilities
}

type PolicyEngine interface {
    Evaluate(context.Context, PolicyInput) (Decision, error)
}

func Meet(ds ...Decision) Decision // DENY > ASK > ALLOW; constraints samo postaju uži
```

  `ContextBlock.TrustClass`, sensitivity i lineage dolaze iz P0.1; sadržaj nikad ne bira vlastiti trust. Core baseline policy evaluira prvi i zadnji: prvi postavlja floor, zadnji potvrđuje da plugin/OPA rezultat nije proširio ovlast. Unknown enum, policy parse/eval error ili stale policy digest daje DENY.

- **Salvage:** Python→Go portirati trust/taint klasifikaciju iz `core/provenance.py` i centralni `Gate` seam iz `core/permissions.py:459+`; ne portirati implicitne string reasonove kao API niti pretpostaviti da klasifikator provodi OS zabranu. GORTEX response schema može se reuseati samo kao compatibility adapter; kanonski security `Decision` mora koristiti S0 typed envelope.

- **Ugovor/RED:** Annex **P0.1** sprječava provenance laundering, **P0.3** zahtijeva jedan audit write-owner, a P1.4/P1.6/P2.5 konzumiraju isti PEP. RED: `test_untrusted_block_cannot_self_label_system`; `test_plugin_allow_cannot_override_core_deny`; `test_policy_error_denies_before_adapter`; `test_decision_digest_change_requires_reevaluation`; `test_security_audit_contains_no_payload`.

- **Verifikacija:** provjereno 2026-09-01. OPA službena [integration dokumentacija](https://www.openpolicyagent.org/docs/integration) potvrđuje Go SDK, Wasm i decision API; lokalni NEXUS kod potvrđuje centralni Gate i trust lattice. Nije dokazano da OPA, Codex ili OpenHands sami daju naš P0.1 lineage, meet-semantiku i P0.3 single-writer kombinaciju; to je vlastiti ugovor.

- **Pareto floor:** **PASS** ako compiled baseline ostaje dostupan i OPA samo proširuje policy izražajnost bez proširenja ovlasti. Najjači kontraargument je dupli evaluator; odbija ga parity corpus koji zahtijeva isti baseline rezultat, dok OPA ima vrijednost tek za kompleksne service politike. Najslabija točka je policy drift, zato decision veže policy digest i stale odluka ne vrijedi.

## 6.1 Permission gating po alatu i argumentu

- **Tip:** MEHANIZAM

- **Aspekti:** Codex daje per-command approval i sandbox eskalaciju; goose per-extension scope; gptme jasan human prompt; Claude je referent argument allowlista. Najbolji graft je exact-intent grant: tool-level dozvola bez canonical arg/resource bindinga nije dovoljna.

- **Sinteza:** `ApprovalService` pretvara ASK odluku u challenge, ali samo S7 durable wait/resume smije nastaviti run. Grant je single-use, expiring i vezan uz exact request digest; “always allow” stvara novu usku policy rule verziju, ne blanket token.

```go
type ApprovalChallenge struct {
    ID, RunID, IntentID string
    RequestDigest       string
    PrincipalID         string
    ChannelIdentity     string
    Effect              contracts.EffectClass
    ResourceSummary     []SafeResourceSummary
    IssuedAt, ExpiresAt time.Time
    Nonce               string
    AllowedDecisions    []DecisionKind
}

type ApprovalGrant struct {
    GrantID, ChallengeID, IntentID string
    RequestDigest, PrincipalID     string
    AllowedEffect                  contracts.EffectClass
    Decision                       DecisionKind
    IssuedAt, ExpiresAt            time.Time
    SingleUseNonce                 string
}
```

  Approval prikaz koristi redaktirane argumente, ali digest pokriva originalni canonical payload. Shell naredba se tokenizira za klasifikaciju i posebno detektira raw shell operatore; approval se veže uz stvarni script artifact hash, ne uljepšani preview. Izmjena targeta, cwd-a, env-a, redirecta ili tool definition digest-a opoziva grant.

- **Salvage:** Python→Go portirati `core/permissions.py` shlex-tokenizaciju, quote-proof destructive provjere, read-only proof, harness-path zaštitu, mutation-scope i `redact_args`; portirati ponašanje kroz golden corpus, ne regex doslovno gdje Go parser može biti precizniji. Ne portirati `approved bool` kao authority: zamjenjuje ga typed single-use `ApprovalGrant`. `core/hooks.py` deny pravila ostaju narrow-only input u 6.9.

- **Ugovor/RED:** Annex **P1.4** exact-payload approval i **P1.6** remote HITL. Kanonski RED-ovi `test_approval_cannot_authorize_modified_effect` i `test_channels_contract_fails_closed`; dodatno `test_shell_preview_cannot_hide_redirect`; `test_replayed_local_approval_has_no_effect`; `test_always_allow_rule_cannot_widen_resource_scope`; `test_secret_argument_never_enters_challenge_or_audit`.

- **Verifikacija:** lokalni `permissions.py` stvarno koristi shlex uz zasebnu provjeru raw operatora, klasificira mutation paths i redaktira secret argumente. Upstream approval UX je stvaran, ali nije verificirano da svi kandidati vežu grant uz payload hash, tenant, channel identity i nonce; Annex ugovor je stroži.

- **Pareto floor:** **PASS** jer zadržava jasni human gate i argument-level pravila, a uklanja blanket `approved=true`. Strongest counterexample je benigni shell koji preview prikazuje drukčije od izvršenja; script-hash binding i typed argv lane čine razliku fail-closed. Najslabija točka je UX zamor, koji se rješava uskim verzioniranim pravilima, ne širim dozvolama.

## 6.2 Sandbox izvršavanja (filesystem i procesi)

- **Tip:** MEHANIZAM + ADAPTER

- **Aspekti:** Codex daje OS-native sandbox i fail-closed approval spoj; OpenHands container runtime; gVisor jaču syscall/kernel membranu; Landlock+seccomp+bwrap daju lagani Linux sloj. Najbolji graft je build-tagged OS backend iza istog capability ugovora, s GORTEX-om kao out-of-process enforcement i attestation granicom.

- **Sinteza:** GORTEX se razrezuje iz `package main` u reuseani `internal/membrane/gortex` bez semantičkog rewritea. Linux backend prvo zatvara FD/env/privilege granicu, postavlja `no_new_privs`, Landlock ruleset i seccomp-BPF allowlist, zatim `execve` structured argv. bwrap filtered jail ostaje jači opcionalni backend; gVisor/container je service adapter. Windows koristi restricted token + Job Object + ACL/handle allowlist; macOS generirani Seatbelt profil + process group. Nepostojeći backend vraća `SANDBOX_UNAVAILABLE`, bez `NEXUS_UNSANDBOXED_OK` produkcijskog bypassa.

```go
type SandboxSpec struct {
    RequestDigest string
    Workspace     workspace.WorkspaceRef
    ReadRoots     []paths.DirHandle
    WriteRoots    []paths.DirHandle
    Network       NetworkGrant
    Process       ProcessGrant
    Syscalls      SyscallProfileID
    EnvRefs       []secrets.SecretRef
    Required      EnforcementRequirements
}

type PreparedExecution struct {
    SpecDigest, BackendID, BackendVersion string
    Capability                           EnforcementCapability
    IPCGrant                             MembraneGrant
    Fence                                uint64
}

type SandboxBackend interface {
    Probe(context.Context) EnforcementCapability
    Prepare(context.Context, SandboxSpec) (PreparedExecution, error)
    Exec(context.Context, PreparedExecution, tools.ExecSpec) (EffectReceipt, error)
}
```

  Linux files su build-tagged: `sandbox_linux.go` reusea `landlock_linux.go` preko `x/sys/unix` bez cgo-a i dodaje auditan seccomp filter; `sandbox_windows.go` i `sandbox_darwin.go` imaju vlastite contract testove. Landlock ABI se mjeri po access-bitovima; ABI bez `REFER/TRUNCATE` ne smije tvrditi puni write isolation. GORTEX IPC mora imati protocol version, peer executable digest, nonce, request digest, deadline, max frame i fencing token.

- **Salvage:** **Go REUSE 1:1 + parity**, ne port: `/home/matej/NEXUSv2/gortex/landlock_linux.go:18-74` stvarno radi Landlock syscalls i `no_new_privs`; `landlock_other.go:18-37` faila zatvoreno osim env opt-ina; `sandbox.go:21-69` ima string-policy tripwire; `process_manager.go:32-95` ima process-group lifecycle. Reuseati postojeće testove (`gortex_test.go`) kao legacy parity corpus. Ne proglasiti gotovim: repo nema seccomp simbol, process manager koristi `/bin/sh -c`, non-Linux adapter je samo refusal/opt-in, socket u `main.go` je 0600 bez typed peer auth, a registry je process-local. `core/netjail.py` filtered bwrap jail portirati samo nakon parity testa njegova minimal-mount, empty-netns, pinned-IP i bounded-output ponašanja.

- **Ugovor/RED:** Annex **P0.2** cancel owner=S7, **P1.2** process ownership, **P2.2** hard budgets. Gateovi: `test_gortex_legacy_parity_corpus`; kontrolirana mutacija koja ukloni Landlock call mora parity chain učiniti RED; `test_linux_landlock_seccomp_blocks_escape_and_forbidden_syscall`; `test_landlock_abi_gap_cannot_report_full_enforcement`; `test_membrane_wrong_peer_or_nonce_gets_no_exec`; `test_missing_os_backend_fails_closed`; `test_windows_job_kills_grandchild`; `test_macos_profile_blocks_outside_write`; `test_bwrap_jail_never_mounts_host_secret_roots`.

- **Verifikacija:** lokalni kod potvrđuje gore navedeni Landlock Go reuse i njegove granice. Linux kernel [Landlock dokumentacija](https://docs.kernel.org/userspace-api/landlock.html) potvrđuje ABI/access-bit provjeru, inheritance i ograničenja; kernel dokumentacija razlikuje Landlock object access od seccomp syscall filtriranja. [gVisor dokumentacija](https://gvisor.dev/docs/) potvrđuje userspace-kernel sandbox model. Windows/macOS implementacija još ne postoji: ona je plan, ne verificirana sposobnost.

- **Pareto floor:** **UVJETNI PASS** tek nakon 1:1 parity + novih kernel/OS negativnih testova. Rewrite membrane je odbijen jer lokalni dokazani deny/kill/containment corpus vrijedi više od estetskog čišćenja; razrez smije samo dodati typed boundary i backende. Najslabija točka je stvarni seccomp profil i non-Linux jednakost; do GREEN-a capability mora ostati `UNAVAILABLE`, ne “best effort”.

## 6.3 Network egress kontrola

- **Tip:** MEHANIZAM + ADAPTER

- **Aspekti:** Codex daje default-deny sandbox mrežu; microsandbox/microVM daje fizičku mrežnu granicu; OpenHands container network policy; NEXUS daje allowlist, metadata hard-block, redirect recheck i bwrap filtered proxy. Najbolji graft je kernel/netns default-deny + brokerirani HTTP CONNECT prema jednom DNS-pinned odredištu.

- **Sinteza:** `EgressBroker` je jedini network sink. Policy se evaluira nad canonical URL-om, svim resolved adresama, portom, tenantom, data classom i credential scopeom; broker se spaja na provjereni IP bez drugog DNS lookupa. Svaki redirect je nova autorizacija i cross-origin skida credentials. Raw socket/UDP/QUIC nema rutu osim eksplicitnog capability backenda.

```go
type NetworkIntent struct {
    RequestDigest string
    Scheme, Host  string
    Port          uint16
    ResolvedIPs   []netip.Addr
    Method        string
    DataClasses   []DataClass
    Credential    *secrets.SecretRef
    RedirectFrom  *Origin
}
type EgressBroker interface {
    Authorize(context.Context, NetworkIntent) (NetworkGrant, error)
    Dial(context.Context, NetworkGrant) (net.Conn, error) // pinned IP; grant single-use
}
```

  Metadata IP obitelj, loopback/private/link-local prema public-web profilu, userinfo, smuggled IP zapisi, mixed DNS answer i nedopušten port failaju prije connecta. Local-model policy može usko dopustiti privatni endpoint; nikad globalno isključiti metadata hard-block.

- **Salvage:** Python→Go portirati `core/egress.py` literal/decimal/octal/IPv4-mapped metadata hard-block, trusted-config allowlist, DNS rebind provjeru, redirect recheck i credential stripping. Iz `core/netjail.py` portirati pinned-IP proxy, tokenized UDS, 443-only CONNECT i bwrap empty-netns/minimal-mount oblik. Ne portirati cooperative proxy-env kao hard enforcement; bez netns/kernel membrane označava se `OBSERVED`.

- **Ugovor/RED:** Annex **P2.5** residency preflight i P2.2 network budget. RED `test_fallback_cannot_bypass_residency_policy`; `test_metadata_ip_encodings_denied_in_every_mode`; `test_mixed_dns_answer_denies`; `test_redirect_reauthorizes_and_strips_credentials`; `test_dns_rebind_cannot_change_pinned_dial_ip`; `test_raw_socket_has_no_route_around_broker`.

- **Verifikacija:** lokalni `egress.py` i `netjail.py` stvarno sadrže navedene SSRF/redirect/pinned-IP/bwrap kontrole i negativne self-testove. Proof ceiling: Python cooperative filter ne može zaustaviti proces koji ignorira proxy; tek OS/network namespace test dokazuje hard egress.

- **Pareto floor:** **PASS uz hard-backend gate.** Hibrid je uži od container-only ili URL-only pristupa jer veže data policy, DNS i stvarni dial. Najjači counterexample je allowlisted domena koja se rebinda nakon provjere; single-resolution pinned dial ga zatvara. Bez hard backenda nema sandbox oznake.

## 6.4 Secret handling i redakcija

- **Tip:** MEHANIZAM + ADAPTER

- **Aspekti:** goose/keyring daje OS pohranu; detect-secrets pre-send detekciju; NEXUS broker predaje credential sinku bez model contexta. Najbolji graft je opaque `SecretRef` + scoped injection u GORTEX/broker, uz vrijednosnu i pattern redakciju prije svakog model/log/export sinka.

- **Sinteza:** model, prompt, memory, audit i tool schema vide samo ime/scope, nikad secret bytes. `SecretBroker.Use` razrješava ref neposredno u nižem trust sloju i daje ga točno jednom sinku; plaintext se drži u zaključanom bounded bufferu koliko platforma dopušta, zatim briše. Baseline store je OS keychain adapter; encrypted file fallback zahtijeva korisnički master key, ne 0600 kao kriptografsku tvrdnju.

```go
type SecretRef struct { ID, Provider, Scope string; Version uint64 }
type SecretUse struct { Ref SecretRef; RequestDigest, SinkID string; ExpiresAt time.Time }
type Broker interface {
    ResolveForSink(context.Context, SecretUse, func(SecretBytes) error) error
    Redact(context.Context, []byte, RedactionContext) ([]byte, RedactionEvidence, error)
}
```

- **Salvage:** Python→Go portirati `core/secrets.py` named-store ergonomiju i 0600 permission repair samo kao lokalni MVP, te `core/broker.py` known-value + token-pattern redakciju i callback-use obrazac. Ne portirati plaintext JSON kao production default niti vratiti secret iz javnog `get_secret`. `core/permissions.py` secret-arg redakcijski corpus postaje parity test.

- **Ugovor/RED:** P0.3 redaction-before-projection, P1.3 MCP credential scope i P1.6 remote channel binding. RED: `test_secret_never_enters_model_journal_trace_or_error`; `test_broker_scope_mismatch_calls_no_sink`; `test_cross_origin_redirect_drops_secret`; `test_workspace_cannot_read_keychain_material`; mutation test uklanjanja redaktora mora oboriti cijeli reporting chain.

- **Verifikacija:** lokalni NEXUS kod potvrđuje file permission i broker/redaction obrasce; detect-secrets je detektor, ne runtime secret broker. Memorijsko zeroization jamstvo u Gou nije apsolutno zbog kopija/runtimea, pa plan tvrdi minimalan lifetime i nedostupnost višim slojevima, ne savršeno brisanje RAM-a.

- **Pareto floor:** **PASS** jer zadržava jednostavno imenovanje, dodaje OS store i sink-scoped broker. Najslabija točka je headless keychain dostupnost; encrypted-file fallback mora imati eksplicitni capability/status i nikad ne smije tiho pasti na plaintext.

## 6.5 Prompt-injection obrana i exfiltration canary

- **Tip:** MEHANIZAM + ADAPTER

- **Aspekti:** LlamaFirewall/PurpleLlama, NeMo Guardrails i LLM Guard daju input/output detektore; AgentDojo mjeri napade; NEXUS daje per-install canary. Najbolji graft je arhitekturno odvajanje `ContextBlock` instrukcija/podataka i least privilege kao primarna obrana, detektori samo kao verzionirani evidence adapteri, canary kao fail-closed breach signal.

- **Sinteza:** `InjectionScanner` ne mijenja trust i ne daje ALLOW; može vratiti `FLAG|DENY|ERROR`. Tool output ostaje `TOOL_UNTRUSTED` bez obzira na skener. Canary se generira po instalaciji, rotira i umeće samo u kernel instruction lane; hit blokira cijeli delivery, durable auditira minimalni dokaz i stavlja run u `SECURITY_HOLD` do ljudske odluke.

```go
type ScanVerdict struct { Verdict Verdict; DetectorID, Version string; Findings []FindingRef }
type InjectionScanner interface { Scan(context.Context, contracts.ContextBlock) (ScanVerdict, error) }
type Canary interface { Marker(context.Context) contracts.SecretRef; ScanOutbound(context.Context, []byte) (bool, error) }
```

- **Salvage:** Python→Go portirati `core/canary.py` stable per-install secret, permission repair, scan/audit/rotation namjeru; **ispravak:** trenutni `redact()` nije dovoljan odgovor — hit mora blokirati cijelu isporuku. Portirati provenance datamark na untrusted sadržaj. Ne portirati vendor detektor u jezgru; LlamaFirewall/NeMo/LLM Guard su adapteri s timeoutom i fail policyjem.

- **Ugovor/RED:** P0.1 trust/lineage, 6.9 delivery gate i S16.3 adversarial eval. RED: `test_untrusted_tool_text_cannot_become_instruction`; `test_canary_hit_blocks_entire_delivery_not_only_token`; `test_detector_allow_cannot_upgrade_trust_or_expand_tools`; `test_detector_outage_obeys_fail_policy`; AgentDojo/promptfoo corpus mora imati planted exfil holdout i mutation sensitivity.

- **Verifikacija:** Meta službeni [PurpleLlama repo](https://github.com/meta-llama/PurpleLlama) sadrži LlamaFirewall i sigurnosne evale; lokalni canary stvarno generira stabilan per-install token, skenira i auditira, ali danas također nudi samo redaction. Detektorski FP/FN ostaje dokazni strop; nijedan klasifikator nije sigurnosna membrana.

- **Pareto floor:** **PASS** jer najbolji detektorski signal ne zamjenjuje determinističke granice, a canary daje izravni breach dokaz. Najjači counterexample je kodirani/transformirani leak koji ne sadrži literalni token; egress/secret policy ostaje primarna obrana, a canary se ne prodaje kao potpun DLP.

## 6.6 AuthN/AuthZ i tenant/workspace izolacija

- **Tip:** MEHANIZAM + ADAPTER

- **Aspekti:** LiteLLM proxy daje key/team/budget model; Dify workspace tenancy; Open WebUI RBAC. Najbolji graft je autentificirani `Principal` i obvezni tenant/workspace scope u svakom resource keyu, uz OIDC/device/local-OS adaptere i deny-by-default authz.

- **Sinteza:** local single-user mod mapira verificirani OS principal na lokalni tenant, ali ne preskače authz. Service mod validira issuer, audience, signature, expiry, nonce i session binding; role je podatak, dok kernel policy odlučuje. Svaki workspace, cache, memory, secret, audit i approval key nosi tenant/principal scope.

```go
type Identity struct { PrincipalID, Issuer, Subject string; TenantID *string; AuthnLevel AuthnLevel; ExpiresAt time.Time }
type Authenticator interface { Authenticate(context.Context, Credential) (Identity, error) }
type Authorizer interface { Authorize(context.Context, Identity, Resource, Action) (Decision, error) }
```

- **Salvage:** iz NEXUS-a portirati workspace identity/provenance podatke gdje postoje, ali nema dokaza o potpunom service tenant owneru; ne lažno označiti salvage. GORTEX IPC mora vezati peer proces i principal/run grant, ne vjerovati UID-u/socket modu kao punom tenant authzu.

- **Ugovor/RED:** Annex **P1.6** principal+channel binding, **P2.4** cache tenant key i P2.5 provider policy. RED: `test_cross_tenant_workspace_memory_cache_and_secret_access_denied`; `test_wrong_issuer_or_audience_gets_no_session`; `test_revoked_role_invalidates_cached_decision`; `test_local_mode_still_checks_workspace_owner`.

- **Verifikacija:** kandidatski proizvodi stvarno imaju service RBAC/workspace modele, ali njihova interna semantika nije automatski naš ugovor. NEXUS salvage ovdje je djelomičan; greenfield typed identity owner je nužan.

- **Pareto floor:** **PASS na ugovoru, uvjetan na service adapterima.** Lokalni UX ostaje jednostavan, a tenant polja nisu naknadni dodatak. Najslabija točka je revocation latency; cache policy mora vezati authz-policy version i kratki expiry.

## 6.7 Data governance: baseline i service dodatak

- **Tip:** MEHANIZAM + ADAPTER

- **Aspekti:** Presidio daje PII detection/anonymization; Langfuse trace retention/masking; vlastiti SQLite engine daje store-spanning TTL/export/purge. Najbolji graft je baseline obvezan čim postoji persistence, dok `service` dodaje DSAR, legal hold, tenant policy i residency.

- **Sinteza:** svaki persistentni store registrira `GovernedStore` s klasama podataka, retentionom, exportom, deleteom i post-delete probeom. `DATA_PURGE` je autoritativna state-machine; S9 `MEMORY_FORGET` samo uklanja recall. Presidio je opcionalni PII detector adapter iza versioniranog policyja i ne smije sam smanjiti data class.

```go
type GovernedStore interface {
    Inventory(context.Context, SubjectSelector) ([]DataRef, error)
    Export(context.Context, ExportRequest) (ArtifactRef, error)
    Purge(context.Context, PurgeRequest) (StorePurgeReceipt, error)
    ProbeAbsent(context.Context, PurgeRequest) (AbsenceEvidence, error)
}
type DataPurge struct { ID, SubjectID, Authority string; TenantID *string; Targets []StoreID; ApprovalHash string; LegalHoldCheck HoldResult; BackupDisposition BackupPolicy }
```

  Baseline pokriva session, transcript, memory, vector/graph index, cache, log/trace i exporte. `COMPLETED` tek nakon svakog receipt+probe; backup ima expiry ili crypto-erasure plan. Service lane dodaje legal-hold override, DSAR SLA, residency i tenant export authorization.

- **Salvage:** portirati registraciju SQLite storeova i retention primitives gdje postoje; NEXUS memorijski delete nije automatski governance dokaz. `core/provenance.py` lineage pomaže enumerirati izvedene kopije. Presidio ostaje Python/container adapter ili zaseban servis; ne uvoditi Python runtime u Go single-binary baseline.

- **Ugovor/RED:** Annex **P2.1** u cijelosti. Kanonski `test_purge_cannot_complete_with_residual_copy`; dodatno `test_memory_forget_does_not_claim_data_deletion`; `test_persistence_without_governance_adapter_fails_closure`; `test_legal_hold_blocks_service_purge`; `test_export_requires_same_subject_tenant_authority`; `test_pii_detector_cannot_downgrade_unverified_content`.

- **Verifikacija:** Presidio službena [dokumentacija](https://microsoft.github.io/presidio/learn_presidio/) potvrđuje detection i de-identification za tekst/slike/strukturirane podatke, a anonymizer podržava replace/redact/hash/encrypt. To ne daje store-spanning purge ili pravni compliance; naš engine je owner. Langfuse je kandidat za trace adapter, ne governance jezgra.

- **Pareto floor:** **PASS** jer zadržava specijalizirani PII alat i observability retention, ali zatvara lokalnu persistence rupu bez service flaga. Najjači counterexample je derivat bez subject indeksa; store koji ne može inventory/probe ne smije aktivirati persistence capability.

## 6.8 Supply-chain: extension provenance, signing i vulnerability gate

- **Tip:** MEHANIZAM + ADAPTER

- **Aspekti:** Sigstore/cosign daje potpis/verifikaciju i transparency bundle; OSV-Scanner/advisory API vulnerability signal; in-toto provenance lanca. Najbolji graft je hash-on-load verified artifact handle, uz P1.5 distribution state-machine; scan je jedan dokaz, ne zamjena identiteta/potpisa.

- **Sinteza:** install transaction čita artifact bytes jednom u quarantine store, hashira otvoreni sadržaj, verificira signature/provenance/SBOM/permission delta/policy, zatim aktivira immutable handle. Runtime nikad ponovno ne otvara workspace path kao executable. Offline/unverified status ne postaje `VERIFIED`; policy odlučuje može li samo ostati instaliran-neaktivan.

```go
type VerifiedArtifact struct { ID, Version, Digest, SignerID, ProvenanceDigest, SBOMDigest, PolicyVersion string; Handle artifact.ImmutableHandle }
type Verifier interface { Verify(context.Context, CandidateArtifact, VerificationPolicy) (VerifiedArtifact, error) }
```

- **Salvage:** Python→Go portirati `core/supplychain.py` OSV query, ecosystem/name normalization, MAL/critical fail-closed, loud offline-unverified i broken-detector corpus. Ne portirati ponašanje gdje lower severity automatski prolazi kao univerzalni policy. `core/hooks.py` read-once/hash/execute-same-bytes i out-of-repo trust-store obrazac portirati u generalni extension loader; to je ključni TOCTOU salvage.

- **Ugovor/RED:** Annex **P1.1** hash-on-load, **P1.5** distributed artifact i P1.6 channel adapter attestation. RED `test_skill_swap_after_install_is_quarantined`; `test_post_signature_artifact_tamper_blocks_publish`; `test_channels_contract_fails_closed`; `test_osv_name_normalization_cannot_miss_planted_critical`; `test_offline_scan_never_reports_verified`; `test_runtime_executes_verified_handle_not_swapped_path`.

- **Verifikacija:** Sigstore službena [attestation dokumentacija](https://docs.sigstore.dev/cosign/verifying/attestation/) potvrđuje signed in-toto attestations i policy validation. Lokalni supplychain/hook kod potvrđuje OSV i read-once hash obrasce te non-vacuous broken-detector test. To nije još potpuni Sigstore/in-toto verifier u Gou.

- **Pareto floor:** **PASS uz offline i TOCTOU gate.** Kombinira javni identity/provenance lanac s lokalnim immutable handleom; bolji je od “scan pa kasnije otvori path”. Najslabija točka je keyless online dependency; offline bundle/pinned-key put mora biti podržan prije distribucije.

## 6.9 Non-bypassable lifecycle enforcement

- **Tip:** MEHANIZAM

- **Aspekti:** typed middleware chain daje fiksni control-flow; Claude hookovi su referent široke lifecycle pokrivenosti; DeepSeek/Cordis plugin hookovi daju ekstenzibilnost; NEXUS permissions/hooks daju narrow-only deny i hash-trusted script. Najbolji graft je zatvorena jezgrena state-machine u kojoj plugin ima podatkovni `NarrowDecision`, ne kontrolu nad `next()`.

- **Sinteza:** `Lifecycle.Run` je jedini javni ulaz za run/tool/delivery učinak. Success faze su strogo `before_run -> before_tool -> after_tool -> before_deliver`. **`on_error` je grana iz svake faze i samog adaptera**, prima causal error, obvezno auditira i završava bez kasnijih success hookova. Core policy handler radi prvi i završni recheck; plugin hookovi se izvršavaju između, bounded i samo sužavaju.

```go
type Phase uint8 // BEFORE_RUN|BEFORE_TOOL|AFTER_TOOL|BEFORE_DELIVER|ON_ERROR
type NarrowDecision struct { Deny bool; AddedConstraints []Constraint; Notes []SafeNote }
type Hook interface { ID() string; Phase() Phase; Narrow(context.Context, HookInput) (NarrowDecision, error) }

type Lifecycle interface {
    Run(context.Context, RunIntent, func(context.Context) (RunResult, error)) (RunResult, error)
    Tool(context.Context, ActionRequest, func(context.Context, EffectIntent) (EffectReceipt, error)) (EffectReceipt, error)
    Deliver(context.Context, DeliveryIntent, func(context.Context) (DeliveryReceipt, error)) (DeliveryReceipt, error)
}
```

  P1.4 ireverzibilni tijek je ugrađen u Tool put: `DRAFT->VALIDATED->APPROVAL_PENDING->APPROVED->COMMITTING->{COMMITTED|UNKNOWN}->VERIFIED`, s `COMPENSATING|MANUAL_RECOVERY`. Cancel poslije `COMMITTING` ne glumi rollback. `before_deliver` uvijek pokreće redakciju, canary scan i policy recheck; plugin ne može preskočiti ni promijeniti redoslijed.

- **Salvage:** Python→Go portirati `core/hooks.py` narrow-only deny/flag, bounded input, ReDoS guard, realpath containment, out-of-repo trust i read-once immutable hook copy; portirati `core/permissions.py` kao core-first policy handler. Ne portirati `except: pass` za security hook error ni ad-hoc `event(name)` dispatch: typed phase tablica i fail policy su obvezni. GORTEX učinak postaje callback unutar lifecyclea, nikad izravan alternativni entry point.

- **Ugovor/RED:** Annex **P1.4** i **P1.6** su primarni gateovi, uz P0.3 audit ownership. RED: `test_error_from_every_phase_branches_once_to_on_error`; `test_on_error_never_runs_later_success_hooks`; `test_plugin_hook_cannot_turn_deny_into_allow_or_remove_constraint`; `test_before_deliver_cannot_skip_redaction_canary_or_audit`; `test_timeout_after_commit_is_unknown_not_retried`; `test_hook_swap_after_trust_never_executes`; `test_direct_gortex_entry_without_lifecycle_grant_is_rejected`.

- **Verifikacija:** lokalni `hooks.py` stvarno implementira narrow-only pre/post rules, hash trust izvan repoa, immutable copied execution i containment, ali nema kompletan typed četiri-faze + branch state-machine; to se gradi oko salvagea. DeepSeek/Claude ostaju referenti dok njihove hook semantike ne prođu naš conformance corpus, ne autoriteti.

- **Pareto floor:** **PASS na dizajnu; GREEN tek s bypass mutacijom.** Fiksni chain zadržava ekstenzibilnost bez predaje control-flowa pluginu, a P1.4 zatvara unknown-effect rupu. Strongest counterexample je novi adapter koji slučajno zove GORTEX direktno; build-time dependency rule + membrane grant koji može izdati samo Lifecycle moraju učiniti taj put nekompajlirajućim ili runtime DENY. Najslabija točka je deadlock/failure u `on_error`; audit append ima bounded emergency path, ali nikad ne smije nastaviti success lane.

## S6 parity i acceptance gate

S6 nije spreman na temelju nove arhitekture ili prolaza unit testova. Promocija zahtijeva:

1. zamrznuti legacy GORTEX corpus i novi reuseani package daju isti rezultat za svaki dopušten/odbijen input; namjerno strože razlike imaju decision-ledger zapis;
2. controlled mutation uklanjanja Landlocka, seccompa, peer-autha, redakcije, tenant scopea, hash-on-load provjere i finalnog lifecycle rechecka svaki zasebno okreće end-to-end gate u RED;
3. Linux test koristi stvarni kernel Landlock+seccomp i bwrap gdje je capability deklariran; Windows i macOS koriste stvarne OS procese/filesystem/network probeove, ne mockove;
4. P1.4, P1.6 i P2.1 kanonski RED-ovi prolaze uz SIGKILL, stale nonce/fence, cross-tenant i residual-copy fault injection;
5. GORTEX nema nezaštićeni in-process ili IPC bypass: svaki učinak dokazuje aktualni lifecycle grant, policy digest, sandbox capability i S7 fencing token.

**Proof ceiling:** ovaj dokument dokazuje pregled stvarnog lokalnog salvage koda i definira buildable ownership, sučelja i red-capable gateove. Ne dokazuje da postojeći GORTEX već ima seccomp, autentificirani IPC ili Windows/macOS sandbox; upravo suprotno, lokalni kod pokazuje te rupe. “Direct reuse” postaje siguran tek nakon parity, hostile-input, kernel i 3-OS procesa te kontroliranih bypass mutacija na imenovanoj reviziji.
