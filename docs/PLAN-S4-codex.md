PLAN S4

# S4 — Tool sustav

## Zajedničke odluke i paket-layout

- S4 je typed dispatch i capability površina, ne vlasnik sigurnosne odluke, workspace mutacije, retryja ili trajnog događaja. Predloženi layout je `internal/tools/{contract,registry,dispatch,exec,edit,search,browser,mcp,view}`; S5 jedini commit-a datoteke/checkpointe, S6 autorizira i izolira, S7 izdaje `AttemptGrant` i gasi fizički pokušaj, a P0.3 journal jedini trajno zapisuje događaj.
- Kanonski put je `ToolProposal -> Registry.Resolve -> ToolView membership -> S6 authorize -> S7 AttemptGrant -> Adapter.Invoke -> typed Observation`. Handler se nikad ne poziva izravno iz provider adaptera ni iz modelovog imena alata.
- Tool schema opisuje oblik podataka, ne daje ovlast. `EffectClass`, capability zahtjevi, policy tagovi i handler identitet nisu model-controlled polja; dolaze iz verificirane registracije. Argumenti, opisi i rezultati vanjskih alata tretiraju se kao neprovjereni podaci.
- Svaki fizički učinak ima jedan `ActionID+AttemptNo`. S4 ne radi retry, fallback, timeout-loop ni “probaj drugi alat”; vraća terminalni typed rezultat S3, a S7 jedini odlučuje postoji li novi pokušaj.
- Coding profil aktivira `exec`, `edit` i `code-search` capability flagove kroz S0.5 closure. `web`, `browser` i `mcp-client` ostaju zasebni composable flagovi. Registry može sadržavati definiciju koja nije aktivna; samo `ToolView` određuje što je vidljivo u točno tom turnu.
- Single-binary cilj znači da se naš registry, dispatch, edit parseri, exposure planner i MCP trust wrapper pišu u Gou. Vanjski izvršni programi (`git`, `rg`, `ast-grep`, language server, Chromium) jesu eksplicitno detektirani capability adapteri: njihov izostanak daje `UNAVAILABLE`, nikad skriveni download, mrežni install ili semantički loš fallback.

Minimalni zajednički ugovor:

```go
package tools

type ToolID string       // namespace/name@major; stabilan unutar schema verzije
type DefinitionDigest string

type Definition struct {
    ID                 ToolID
    Version            contracts.SemVer
    Title              string
    Description        string
    InputSchema        json.RawMessage
    OutputSchema       json.RawMessage
    DefinitionDigest   DefinitionDigest
    Provider           ProviderRef
    RequiredCaps       []contracts.CapabilityID
    Effect             contracts.EffectClass
    ApprovalClass      contracts.ApprovalClass
    DataPolicy         contracts.DataPolicy
    BudgetWeight       uint32
}

type Invocation struct {
    Envelope           contracts.Envelope
    ActionID           loop.ActionID
    Attempt            scheduler.AttemptGrant
    ToolID             ToolID
    DefinitionDigest   DefinitionDigest
    Arguments          json.RawMessage
    WorkspaceRevision  string
}

type Result struct {
    Envelope           contracts.Envelope
    ActionID           loop.ActionID
    AttemptNo          uint32
    Status             loop.ObservationStatus
    Output             []contracts.ContextBlock
    Error              *contracts.TypedError
    EffectCertainty    contracts.EffectCertainty
    EvidenceRefs       []string
    Usage              scheduler.ResourceLedger
}

type Adapter interface {
    Invoke(context.Context, Invocation) Result // jedan grant, jedan fizički pokušaj
}

type Dispatcher interface {
    Invoke(context.Context, Invocation) Result
}
```

Obvezne globalne invarijante:

1. `DefinitionDigest` pokriva kanonski schema JSON, effect/approval/data policy, capability zahtjeve, provider identity i handler verziju. Poziv s digestom koji više nije aktivan završava `STALE_TOOL_DEFINITION`; nema dispatcha na “najbližu” novu verziju.
2. Ulaz se validira prije autorizacije, zatim se autorizira točan kanonski poziv, a adapter dobiva neizmjenjivi `AttemptGrant`. Normalizacija nakon odobrenja koja mijenja program, putanju, URL, scope ili write-set poništava grant.
3. Svi outputi su bounded i nose `ContextBlock` provenance/trust. Truncation je strukturirano polje s artefakt-refom na dopušteni puni output, ne neoznačeno rezanje teksta.
4. Provider, MCP server, skill ili plugin ne postaje aktivan zato što se pojavio u registru. Dinamički kod/sadržaj mora proći P1.1/P1.5 supply-chain lane; registry prima verificirani handle/digest, ne proizvoljnu putanju.
5. Adapter smije zahtijevati S5/S6/S7 efekt, ali ne smije sam otvoriti paralelni write, shell, mrežni poziv ili retry izvan deklariranog `AttemptGrant` stabla.

## 4.1 Tool registry s typed schemama

- **Tip:** MEHANIZAM

- **Aspekti:**

| Aspekt | FastMCP / MCP SDK | smolagents | OpenAI Agents SDK | Najbolji izvor i graft |
|---|---|---|---|---|
| Definicija alata | Deklaracija funkcije i schema iz tipova/dekoratora | Python funkcija + type hints/docstring | Typed function tool + ulazna validacija | **MCP data model** za interoperabilan schema oblik; naš eksplicitni Go `Definition` ostaje owner sigurnosnih metadata. |
| Typed handler | SDK veže schema na callable | Minimalno mapiranje funkcije u alat | Generički typed wrapperi | **smolagents** za malen mentalni model; u Gou `Register[I,O]` daje compile-time decoder/handler vezu. |
| Validacija | JSON Schema na granici | Validacija parametara prije poziva | Strict schema opcije | **MCP/OpenAI** za schema validaciju, ali dodatni policy/effect ugovor nije izveden iz docstringa. |
| Dinamički provider | MCP server mijenja/lista katalog | Lokalni kod registrira alat | Hosted/local tool registracija | **MCP** za namespaced vanjski katalog; P1.1/P1.3 moraju prethoditi aktivaciji. |
| Verzija i stale-call obrana | Protokol ima capability/list lifecycle | Nije primarni fokus | Tool name/schema vezani uz run | **Naš digest-bound registry**; nijedan kandidat sam ne zatvara TOCTOU između prikaza sheme i poziva. |

- **Sinteza:** `Registry` je immutable-snapshot katalog. Registracija se odvija pri bootu ili kroz verificirani extension transaction; turn dobiva snapshot revision i iz njega gradi 4.7 `ToolView`. Go tip nije izveden iz JSON sheme u runtimeu niti schema/code-gen postaje owner ugovora: eksplicitna schema i typed decoder moraju se zajedno registrirati, a contract testovi dokazuju njihovu ekvivalenciju.

```go
type Decoder[I any] interface {
    DecodeStrict(json.RawMessage) (I, error) // unknown fields i trailing JSON odbija
}

type Handler[I, O any] func(context.Context, CallContext, I) (O, *contracts.TypedError)

type Registration[I, O any] struct {
    Definition Definition
    Decode     Decoder[I]
    Encode     func(O) ([]contracts.ContextBlock, error)
    Handler    Handler[I, O]
    Artifact   *supplychain.VerifiedArtifact
}

type Snapshot struct {
    Revision string
    Tools    map[ToolID]RegisteredTool
}

type Registry interface {
    Snapshot() Snapshot
    Install(RegistryTransaction) (Snapshot, *contracts.TypedError)
    Resolve(snapshotRevision string, id ToolID, digest DefinitionDigest) (RegisteredTool, error)
}
```

  Pravila registracije:

  1. Tool ID je ASCII, namespaced i jedinstven. Kolizija nije last-write-wins; različit digest pod istim `ID@major` odbija cijelu transakciju.
  2. Input/output schema mora biti valjani, canonicalized JSON Schema objekt s root `type: object` za argumente; schema veličina, dubina, broj svojstava, regex složenost i opis imaju hard granice prije compilea.
  3. `Effect`, `ApprovalClass`, `DataPolicy`, `RequiredCaps` i adapter identitet obvezni su čak i za read-only alat. Registry odbija nedeklarirani efekt i capability koji closure resolver ne poznaje.
  4. `Register[I,O]` pokreće contract corpus: minimal-valid, maximal-valid, unknown-field, wrong-type i boundary slučaj prolaze i schema validator i Go decoder s istim ishodom. Neslaganje blokira build/install.
  5. Dinamički artifact mora biti `VERIFIED_ON_LOAD`; registry sprema hash verificiranih bytesa/open handlea. Model-generated skill dokument je inertan dok P1.1 lane ne završi.
  6. Handler ne dobiva globalni registry, filesystem ili credential store; `CallContext` sadrži samo konkretni grant, scoped broker handle i event emitter.

- **Salvage:** Python→Go port samo inventara i uniformnog pre-dispatch validation principa iz `/home/matej/NEXUSv2/core/toolschema.py:1-6,55-110`. Ne portirati trenutni “unknown/MCP tool pass-through” (`toolschema.py:94-96,138-139`) jer vanjska schema nije dokazano jednaka handler ugovoru. Iz NEXUS executora portirati nazive/capability metadata kao migracijski input, ne njegovu veliku string dispatch tablicu. GORTEX servisni handleri postaju adapteri iza ovog registra, nikad paralelni registry owner.

- **Ugovor/RED:** Annex **P0.1** za envelope i provenance, **P0.3** za jedini trajni write-owner, **P1.1** za hash-on-load extension sadržaja i **P2.2** za bounded schema/dispatch resurse. Kanonski P1.1 RED `test_skill_swap_after_install_is_quarantined` mora dokazati da zamijenjeni artifact ne može registrirati handler. Lokalni RED-ovi: `test_duplicate_tool_id_fails_entire_registry_transaction`, `test_stale_definition_digest_never_dispatches`, `test_schema_decoder_disagreement_blocks_registration`, `test_unknown_argument_is_rejected_before_authorization`, `test_model_cannot_lower_effect_class`, `test_unverified_plugin_is_visible_to_neither_registry_nor_view`.

- **Verifikacija:** provjereno 2026-09-01. MCP-ov aktualni [Tools ugovor](https://github.com/modelcontextprotocol/modelcontextprotocol/blob/main/docs/specification/2026-07-28/server/tools.mdx) stvarno zahtijeva valjanu JSON Schemu, capability deklaraciju, deterministički katalog i `tools/list`; službeni [Go SDK protocol opis](https://github.com/modelcontextprotocol/go-sdk/blob/main/docs/protocol.md) potvrđuje Go adapter i schema-validaciju. Nije verificirano da FastMCP, smolagents ili OpenAI Agents SDK vežu effect class, verified artifact digest i stale-call zaštitu u jedan ugovor; to je naš mehanizam, ne pripisana upstream sposobnost.

- **Pareto floor:** **PASS za 4.1.** Zadržava jednostavnost function-tool registracije i MCP interoperabilnost, ali dodaje digest-bound dispatch, strict decoder parity i sigurnosne metadata koje top-3 ne posjeduju kao cjelinu. Strongest counterexample je hot-reload između prompta i tool calla; snapshot revision + definition digest pretvara ga u determinističko odbijanje. Najslabija točka je održavanje schema/type corpus testova; upgrade na codegen dopušten je tek kad dokazano smanjuje drift, ali generirani izlaz i dalje ne smije postati normativni owner.

## 4.2 Shell / izvršavanje koda

- **Tip:** MEHANIZAM

- **Aspekti:**

| Aspekt | OpenHands | Codex CLI | gptme | NEXUS/GORTEX | Najbolji izvor i graft |
|---|---|---|---|---|---|
| Stateful terminal | Unified bash/PowerShell session, input i polling | Dugotrajni unified exec/session kanali postoje, ali sandbox je primarni lever | Shell + tmux za long-lived procese | Python runner je one-shot; GORTEX je one-shot | **OpenHands** samo za eksplicitni `TerminalSession` capability; default ostaje stateless argv radi replaya. |
| Sandbox/policy | Runtime backend izolira terminal | OS-specific sandbox + exec policy + approval | Lakši lokalni shell | GORTEX membrane validira pa izvršava | **Codex/GORTEX**: policy prije spawna, S6-enforced sandbox; S4 ne tvrdi izolaciju koju OS backend nema. |
| Process API | Shell string kao agentsko sučelje | Interno `Vec<String>`/structured exec parametri, uz shell tool lane | Prirodan shell tekst | Python već traži argv; Go GORTEX danas koristi `/bin/sh -c` | **NEXUS Python/Codex** typed argv kao default; shell grammar je zaseban, jače autoriziran mod. |
| Cancel i stablo procesa | Interrupt + statusi/timeouts | Cancel-propagation u exec runtime | tmux/session controls | Oba NEXUS sloja gase process group/tree | **NEXUS/GORTEX** kill-tree, ali fizički cancel i deadline izdaje samo S7. |
| Output/backpressure | Incrementalno čitanje i limit | Bounded capture/stream | Praktičan terminal output | Python head-cap/tail stderr; GORTEX head+tail po streamu | **GORTEX** bounded head+tail + drain; puni dopušteni output ide u S5 artifact. |
| Resource enforcement | Ovisi o runtime backendu | OS sandbox/backend | Nije dokazani hard-budget owner | POSIX rlimits su best-effort | **Annex P2.2**: OS backend mora dokazati hard enforcement ili fail closed; rlimit nije marketinški “sandbox”. |

- **Sinteza — dva eksplicitna moda:** canonical `ExecArgv` ne prolazi kroz shell. `ShellScript` postoji jer coding agent treba pipe/redirection/control-flow, ali dobiva zaseban effect/policy fingerprint i parser-visible approval; nikad se ne proizvodi implicitnim joinanjem `Args`.

```go
type Mode uint8 // EXEC_ARGV|SHELL_SCRIPT|TERMINAL_INPUT

type ExecSpec struct {
    Mode          Mode
    Program       string
    Args          []string
    Script        string
    Shell         ShellKind // POSIX_SH|POWERSHELL; obvezno samo za SHELL_SCRIPT
    Cwd           workspace.DirRef
    Env           map[string]secrets.ValueRef
    Stdin         *artifacts.Ref
    SessionID     *string
    OutputPolicy  OutputPolicy
}

type CallContext struct {
    Attempt       scheduler.AttemptGrant
    Sandbox       sandbox.PreparedExecution
    Budget        scheduler.ResourceBudget
    Ledger        scheduler.LedgerWriter
}

type ProcessBackend interface {
    Spawn(context.Context, sandbox.PreparedExecution, ExecSpec) (Process, error)
}

type Process interface {
    PID() int
    Stream() <-chan OutputChunk
    Wait() ExitResult
    Signal(scheduler.CancelFence) error
}
```

  Normativni put:

```text
validated ExecSpec + immutable cwd/env refs
  -> S6 policy/approval + OS sandbox preparation
  -> S7 AttemptGrant/ResourceBudget reservation
  -> build-tagged ProcessBackend spawn
  -> concurrent bounded stdout/stderr drain + monotonic ledger
  -> exit | S7 cancel fence -> kill full tree -> drain -> terminal Result
```

  Invarijante:

  1. `Program` i svaki `Arg` ostaju zasebni bytestringovi do OS spawna. Na Windowsu se koristi jedan kanonski CommandLineToArgvW-compatible quoting owner; na POSIX-u nema `sh -c`. `ShellScript` je doslovni artifact s hashom i eksplicitnim shellom.
  2. CWD je prethodno otvoren/canonical workspace `DirRef`; path se ponovno provjerava neposredno prije spawna. Env je allowlist + broker-resolved scoped vrijednosti; parent API ključevi, `GIT_*`, loader-preload i interpreter injection varijable ne nasljeđuju se implicitno.
  3. `exec_unix.go`, `exec_windows.go` i `exec_darwin.go` implementiraju process-tree semantics. POSIX process group, Windows Job Object i macOS process group moraju imati platform test; `taskkill` je samo fallback, ne dokaz hard cleanupa.
  4. S6 bira stvarni sandbox backend. Ako profil zahtijeva mrežnu/filesystem izolaciju, a backend je samo rlimit/process-group, poziv je `SANDBOX_UNAVAILABLE`; nikad downgrade u host exec.
  5. S7 je jedini owner wall deadlinea/cancela. Adapter ne radi `context.WithTimeout`, retry ili lokalni grace-success. Nakon hard limita ubija cijelo descendant stablo, drain-a pipeove, knjiži peak/countere i vraća `EXHAUSTED`/`CANCELLED`, nikad `SUCCEEDED`.
  6. Output cap se provodi tijekom draina. Rezultat nosi deterministički head+tail, `truncated=true`, total byte counts i opcionalni S5 artifact ref. ANSI/terminal query escapeovi se uklanjaju samo iz model-visible prikaza; raw evidence hash ostaje vezan uz originalne bytes.
  7. Stateful `TerminalSession` je opt-in coding capability. Svaki input/poll je nova Action/Attempt granica; session state (`cwd`, env delta, active PID) je eksplicitno snapshotiran. Ne postoji skrivena REPL memorija koju replay ne može označiti kao nereproducibilnu.

- **Salvage:** iz `/home/matej/NEXUSv2/core/subproc.py:29-45,47-117` portirati argv-only API, process-group/tree cleanup, minimal env, paralelni bounded drain i path-redacted prikaz. POSIX rlimits (`subproc.py:66-82`) portirati samo kao dodatnu obranu, ne kao hard P2.2 dokaz. Iz `/home/matej/NEXUSv2/gortex/tool_executor.go:33-75,93-105,108-155` izravno Go-reuseati output accumulator/head+tail i process-group namjeru nakon razreza u OS build-tagove. Ne reuseati `Command string`, `/bin/sh -c` (`tool_executor.go:12-18,78-92`) ni unutarnji `context.WithTimeout` (`49-64`): prvi ruši typed argv/cross-platform granicu, drugi duplira S7 ownera.

- **Ugovor/RED:** Annex **P0.2** zahtijeva jednog retry/cancel ownera u S7; Annex **P2.2** zahtijeva `ResourceBudget/ResourceLedger`, child≤parent, monotone countere i stvarno hard enforcement. Kanonski RED-ovi `test_adapter_cannot_self_retry` i `test_hard_process_budget_kills_descendants` su release gate. Lokalni RED-ovi: `test_argv_metacharacters_never_become_shell_syntax`, `test_shell_script_requires_distinct_approval_digest`, `test_missing_required_sandbox_fails_closed`, `test_secret_env_is_not_inherited`, `test_output_flood_stays_bounded_while_child_exits`, `test_windows_job_cancel_kills_grandchild`, `test_cancelled_command_cannot_report_exit_zero_success`.

- **Verifikacija:** provjereno 2026-09-01. [OpenHands TerminalSession](https://github.com/OpenHands/software-agent-sdk/blob/main/openhands-tools/openhands/tools/terminal/terminal/terminal_session.py) stvarno održava session, razlikuje command/input, interrupt i hard/no-change timeout statuse. [Codex exec](https://github.com/openai/codex/blob/main/codex-rs/core/src/exec.rs) stvarno koristi structured command vector, expiration/cancel i bounded shell capture, a [sandbox dokumentacija](https://github.com/openai/codex/blob/main/codex-rs/README.md) navodi Seatbelt/Linux/Windows backende i policy modove. [gptme](https://github.com/gptme/gptme) stvarno nudi shell i tmux, ali nije verificiran kao najbolji hard-isolation uzor. Lokalni GORTEX je stvaran Go kod, ali trenutačno POSIX-only u ovom executor fajlu; “Go reuse” zato znači kontrolirani refactor, ne copy-paste.

- **Pareto floor:** **PASS za 4.2 uz platform gate.** OpenHandsova interaktivnost, Codexova sandbox/policy disciplina i NEXUS kill-tree/bounded-output ostaju, dok typed argv i jedan S7 owner uklanjaju dvije opasne dvosmislenosti. Strongest counterexample je Windows child koji pobjegne parent procesu; floor je zelen tek s Job Object descendant testom, ne s mockom. Najslabija točka je dostupnost jednakog hard resource backenda na sva tri OS-a; capability matrix mora po polju navesti `ENFORCED|OBSERVED|UNAVAILABLE`, a zahtjev za hard granicom na `UNAVAILABLE` faila zatvoreno.

## 4.3 Uređivanje datoteka

- **Tip:** MEHANIZAM

- **Aspekti — edit format kao coding lever:**

| Aspekt | Aider | Cline | Mentat | NEXUS | Najbolji izvor i graft |
|---|---|---|---|---|---|
| Repertoar formata | `whole`; `diff` = SEARCH/REPLACE; `diff-fenced`; `udiff`; editor varijante; izbor po modelu | Targeted SEARCH/REPLACE, `apply_patch`, whole-write fallback | Povijesno replacement/split-diff/unified-diff parseri i parser selection | SEARCH/REPLACE + insert line + AST/LSP symbol operacije | **Aider** za model-profile repertoire; **NEXUS** dodaje semantic format. |
| Format/model fit | Model metadata bira dokazano bolji format; korisnik može overrideati | Prompt usmjerava manje blokove i fallback nakon ponovljenog neuspjeha | Parser se mogao mijenjati po sesiji | Nema eval-driven model selector | **Aider**, ali odluka se zaključava za turn i mjeri vlastitim holdoutima. |
| Apply disciplina | Exact pa ograničene whitespace/fuzzy pomoći; javni edit benchmark | Diff pregled, user accept/reject, korak/checkpoint | Više parsera razdvaja parse od edit namjere | `editapply` ima proven transform, exact/fuzzy/refuse; `symedit` AST/LSP | **NEXUS exact→safe repair→refuse**, bez Aiderova cross-file guessinga. |
| Checkpoint/rollback | Git integracija | Checkpoint diff/restore prvorazredan UX | Git diff/parser integracija | Multi-file LSP rollback best-effort | **Cline**, ali owner je S5 transaction/checkpoint, ne edit adapter. |
| Semantic edit | Tekstualni formati | Uglavnom tekst/patch | Povijesni parseri | AST spans, safe delete, LSP WorkspaceEdit rename | **NEXUS symedit**, uz S5 atomic commit i S3.5 verification. |
| Stale/ambiguous obrana | Failed block vraća korektivni kontekst; neke fuzzy/cross-file pomoći | Traži reread i manje blokove | Parser failure analysis | Unique exact/ambiguity refusal i scope guard | **Naš base digest + unique anchor**; nikad automatski edit druge datoteke. |

- **Sinteza — jedan proposal IR, više parsera:** model-facing formati nisu zasebni write putovi. Svaki parser proizvodi isti `EditPlan`; tek `EditPlanner` validira base revision, izračunava puni write-set i predaje S5 transakciji.

```go
type Format uint8 // WHOLE|SEARCH_REPLACE|UNIFIED_DIFF|APPLY_PATCH|SYMBOL_OP

type EditProposal struct {
    Format          Format
    ModelProfile    string
    BaseRevision    string
    FileDigests     map[workspace.PathRef]string
    Payload         []byte
}

type OperationKind uint8 // CREATE|DELETE|REPLACE_RANGE|INSERT|RENAME_SYMBOL|DELETE_SYMBOL

type EditOperation struct {
    Kind            OperationKind
    Path            workspace.PathRef
    ExpectedDigest  string
    Anchor          Anchor
    Replacement     []byte
    Symbol          *SymbolRef
}

type EditPlan struct {
    ProposalDigest string
    Format         Format
    Operations     []EditOperation
    WriteSet       []workspace.PathRef
    RepairTrace    []RepairStep
}

type Parser interface {
    Parse(EditProposal) (EditPlan, *contracts.TypedError) // pure; nula writeova
}

type FormatPolicy interface {
    Select(provider.ModelID, task.Features, evals.EditEvidence) FormatDecision
}
```

  Format selector radi po ovoj determinističkoj politici:

  1. Model-specific verified profile, ako postoji i nije zastario nakon model revisiona.
  2. Inače capability-safe default: mali precizni edit → `SEARCH_REPLACE`; standardni patch-trained model → `APPLY_PATCH`; poznat udiff profil → `UNIFIED_DIFF`; mala nova datoteka → `WHOLE`; rename/reference-safe zadatak s dostupnim LSP-om → `SYMBOL_OP`.
  3. `WHOLE` je dopušten samo ispod file/output limita i uz expected base digest. Nije univerzalni fallback za veliki postojeći fajl.
  4. Promjena formata nakon parse/apply failurea zahtijeva novu model akciju ili policy odluku unutar novog S7 granta; adapter ne poziva model niti radi skriveni retry.

- **Sinteza — deterministički edit-repair ladder `exact -> bounded-fuzzy -> refuse`:** “fuzzy” ovdje znači samo dokazive, ograničene L1/L2 transformacije s jedinstvenim rezultatom; nikad opći nearest-match write.

```go
func ResolveReplace(base []byte, a Anchor) (ResolvedRange, RepairTrace, error) {
    // L0: byte-exact + expected digest + exactly one occurrence
    // L1: proven newline/leading-blank/constant-indent normalization, reversible trace
    // L2: bounded structural anchor: exact prefix+suffix, ordered, one candidate
    // L3: REFUSE; vrati bounded candidate context, nikad similarity-best guess
}
```

  Invarijante laddera i commita:

  1. `exact`: expected file digest odgovara, SEARCH bytes postoje točno jednom i replacement se primjenjuje na tu poziciju. Nula ili više od jednog matcha nije dopušteno nagađati.
  2. `safe repair`: smije samo kanonsku CRLF/LF prilagodbu, jednu leading-blank anomaliju ili konstantan indent offset; transform mora biti reverzibilno zabilježen i dati točno jedan kandidat. Opći edit-distance/“najbliži chunk” ne smije automatski pisati.
  3. `structural anchor`: AST/LSP range mora biti vezan uz trenutni document version/digest i jedinstveni symbol identity. LSP `WorkspaceEdit` otkriva puni dinamički write-set prije ijednog zapisa; edit izvan workspacea, ignorea, role scopea ili S6 granta odbija cijeli plan.
  4. Parser ne piše. S5 otvara pre-edit checkpoint, ponovno provjerava sve digest-e, stagea sve bytes, fsync/atomic replace prema svom ugovoru i commit-a cijeli write-set ili ništa. Cline checkpoint je tako graftan bez drugog rollback ownera.
  5. Create/delete/rename imaju eksplicitne operacije; prazni SEARCH ne znači implicitni append. Patch pathovi se canonicaliziraju prije policy odluke, a symlink target identitet mora ostati isti check-to-use.
  6. Uspješan apply znači samo `MUTATION_COMMITTED`. Syntax/LSP diagnostics postaju observation i ulaz 3.5; editor ih ne smije ignorirati niti proglasiti completionom. Stvarni test/verification owner ostaje S3.5.
  7. Svaki format ima parser fuzz corpus za malformed fence, Unicode, NUL, CRLF, huge hunk, path traversal, duplicate anchor, overlapping hunks i patch bomb. Limit se provodi prije alokacije proporcionalne payloadu.

- **Salvage:** Python→Go portirati conservative apply ideju iz `/home/matej/NEXUSv2/core/editapply.py:1-7,47-76,70-111`: line-ending očuvanje, unique exact, dokazive indent/leading-blank transformacije, ambiguity refusal i teaching hint. Ne portirati redoslijed u kojem GORTEX fuzz može prethoditi exactu niti generic edit-distance auto-apply. Iz `/home/matej/NEXUSv2/core/symedit.py:195-228,229-292` portirati LSP WorkspaceEdit, UTF-16 range handling, puni dinamički write-set, workspace/ignore/protected/scope gate i all-or-nothing namjeru. Izravne Python write/rollback petlje (`symedit.py:256-289`) zamijeniti S5 transakcijom; inline `_diag` je signal, ne success autoritet. NEXUS safe-delete/reference i AST span ideje portirati iza jednog `SYMBOL_OP` adaptera.

- **Ugovor/RED:** Annex **P0.1** veže proposal/plan/result identitet, **P1.1** sprječava aktivaciju zamijenjenog edit plugina, **P2.2** ograničava payload/output/CPU, dok S5 checkpoint/atomicity i S6 path/write authorization ostaju obvezni cross-section gateovi. RED-ovi: `test_duplicate_anchor_refuses_without_write`, `test_stale_file_digest_refuses_without_fuzzy_repair`, `test_similarity_nearest_block_never_auto_applies`, `test_lsp_cross_scope_write_refuses_all_files`, `test_patch_path_traversal_never_reaches_stage`, `test_partial_multi_file_commit_rolls_back_or_never_publishes`, `test_whole_format_cannot_overwrite_changed_file`, `test_successful_apply_with_syntax_error_cannot_complete_run`, `test_model_revision_invalidates_edit_format_profile`.

- **Verifikacija:** provjereno 2026-09-01. Aiderova [edit-format dokumentacija](https://github.com/Aider-AI/aider/blob/main/aider/website/docs/more/edit-formats.md) stvarno razlikuje `whole`, SEARCH/REPLACE `diff`, `diff-fenced`, `udiff` i editor varijante te navodi model-dependent izbor; [javni edit benchmark](https://aider.chat/docs/benchmarks.html) uspoređuje formate, a [udiff laziness ablation](https://aider.chat/docs/unified-diffs.html) pokazuje velik model-specific učinak, ne univerzalnu dominaciju. Aiderov [EditBlockCoder](https://github.com/Aider-AI/aider/blob/main/aider/coders/editblock_coder.py) stvarno ima exact/whitespace/fuzzy pomoć, ali također pokušava druge chat datoteke; taj guessing namjerno ne graftamo. Clineov službeni snapshot `/home/matej/.cache/nexus-harness-compare/cline` je commit `63099710895e24593554b1e77ec7852f6f16c05c`; `sdk/packages/core/src/session/checkpoint-diff.ts` i `checkpoint-restore.ts` potvrđuju checkpoint diff/restore, a dokumentacija navodi review/revert edit tok. Mentatov trenutni javni repo je arhiviran kao `archive-old-cli-mentat`; release povijest potvrđuje replacement, split-diff i unified-diff parsere, ali nema svjež dokaz da nadmašuje Aider/Cline. Zato je Mentat izvor parser-diversity ideje, ne production autoritet.

- **Pareto floor:** **PASS za 4.3.** Aiderov dokazani model-format fit, Clineov checkpoint UX i NEXUS semantic/write-set obrane preživljavaju, ali svi završavaju u jednom S5 commit putu. Strongest counterexample je “fuzzy” blok koji slučajno odgovara drugoj sličnoj funkciji; naš floor zabranjuje similarity-best auto-write i radije vraća bounded korektivni context. Najslabija točka je početna tablica model→format: dok vlastiti holdouti ne postoje, odluke osim eksplicitno verificiranih Aider profila moraju biti označene `ASSUMED` i pratiti parse/apply failure rate.

## 4.4 Aktivna pretraga i navigacija koda

- **Tip:** MEHANIZAM

- **Aspekti:**

| Aspekt | ripgrep | ast-grep | Serena | NEXUS | Najbolji izvor i graft |
|---|---|---|---|---|---|
| Tekst/regex | Brz, recursive, ignore-aware, line/context izlaz | Nije primarni tekst alat | Ima pattern search kao pomoćni alat | Više scan/fallback putova | **ripgrep** subprocess adapter s kanonskim flagsima i structured rezultatima. |
| Strukturni upit | Nema AST semantiku | Tree-sitter pattern/meta-varijable, search/rewrite | Symbol-level, ne arbitrary AST pattern | tree-sitter span fallback | **ast-grep** za structural search; rewrite ostaje 4.3 edit plan, ne direktni `-r` write. |
| Symbol/reference | Tekstualna aproksimacija | Sintaktički obrazac | LSP find symbol/references/implementations/diagnostics | LSP + symbol index | **Serena/LSP** za live semantic truth; NEXUS index je bounded/offline fallback s provenanceom. |
| Call/dependency inteligencija | Nema | Nema type-aware call graph | Reference i hierarchy preko servera | `callgraph` confidence/provenance | **NEXUS** za caller/callee graph i eksplicitnu nesigurnost. |
| Task context | Ručni izbor hitova | AST hitovi | Relacijska simbolička navigacija | definicije+caller/callee+arch relations+related tests | **NEXUS task_context** kao dev-inteligencija iznad pojedinačnog searcha. |
| Freshness | Čita trenutne bytes | Čita trenutne bytes | Live document/server verzija | commit/dirty/index-state evidence | **NEXUS** freshness contract; stale indeks je discovery-only. |

- **Sinteza — jedan typed query API, četiri backenda:** aktivna pretraga poziva backend po značenju pitanja; ne radi “semantic unavailable → keyword i predstavi isto”. S8.3 gradi i osvježava ambient indexe. S4.4 samo query-a trenutne bytes, live LSP ili zapečaćenu index generaciju i vraća provenance/freshness.

```go
type QueryKind uint8 // TEXT|STRUCTURAL|SYMBOL|REFERENCES|CALLERS|CALLEES|TASK_CONTEXT

type CodeQuery struct {
    Kind          QueryKind
    Pattern       string
    Language      string
    Root          workspace.DirRef
    Paths         []workspace.PathRef
    MaxHits       uint32
    MaxBytes      uint64
    Revision      string
}

type SearchHit struct {
    Path          workspace.PathRef
    Range         contracts.SourceRange
    Snippet       string
    Symbol        *SymbolRef
    Relation      string
    Confidence    float32
    Provenance    Provenance // CURRENT_BYTES|LIVE_LSP|INDEX_EXTRACTED|INDEX_INFERRED|AMBIGUOUS
    SourceDigest  string
    IndexRevision string
}

type SearchResult struct {
    Status        SearchStatus // OK|NOT_FOUND|UNAVAILABLE|STALE_PARTIAL|BUDGET_EXHAUSTED
    Hits          []SearchHit
    Warnings      []string
    Omitted       uint64
    Usage         scheduler.ResourceLedger
}
```

  Backend pravila:

  1. `TEXT` pokreće `rg --json --no-follow` kroz 4.2 argv adapter, s workspace-root CWD-om, explicit globovima i NEXUS/S6 ignore filterom. Korisnik može tražiti hidden/ignored samo ako policy dopušta; model ne smije sam uključiti `-uuu` i zaobići off-limits granicu.
  2. `STRUCTURAL` pokreće `ast-grep run --json` bez rewrite/interaktivnog moda. Pattern/lang se predaju kao zasebni argv; rezultat se ponovno path-gatea. Nedostupan binary vraća `UNAVAILABLE`, ne tekstualni “strukturni” fallback.
  3. `SYMBOL/REFERENCES` prvo koriste live LSP s document versionom. Ako server ne postoji, smiju koristiti NEXUS symbol/call index, ali rezultat mora ostati `INDEX_EXTRACTED|INFERRED|AMBIGUOUS`; nikad ne tvrdi type-semantic ekvivalenciju.
  4. `TASK_CONTEXT` bounded spaja current-source definicije, callers/callees, architecture relations i related tests. S8.3 ostaje owner archmap/codeindex builda; 4.4 ne uvodi paralelni crawler. Svaki indexed element nosi generation/revision i stale warning.
  5. Search rezultat je read-only evidence, ne edit ovlast. Hit iz ignored/protected/out-of-scope patha ne vraća ime ni snippet; činjenica da nešto postoji može se vratiti samo kao policy-safe aggregate ako spec to dopušta.
  6. Boundedness vrijedi prije sort/deserialize: max proces output, max JSON record, max hitova/snippet bytes/depth i wall/CPU budget. Stabilni poredak je `(path,start,end,backend)`; truncation navodi broj izostavljenih kad ga backend zna.
  7. Code graph edge bez dovoljne sigurnosti može voditi discovery, ali ne safe-delete, rename authorization ni TIA obvezni set bez dodatne potvrde iz live/current sourcea.

- **Salvage:** Python→Go portirati freshness i bounded current-source context iz `/home/matej/NEXUSv2/core/codeindex.py:31-46,74-86,126-180,543-649`; zadržati explicit warnings kada je structural/architecture index stale. Portirati semantic retrieval statuse i “nema silent keyword fallbacka” iz `/home/matej/NEXUSv2/core/coderetrieval.py`, confidence/provenance tierove iz `/home/matej/NEXUSv2/core/callgraph.py:18-26`, te `task_context` caller/callee/test spajanje. `archmap.py:1-17` i typed cross-artifact edges pripadaju S8.3; S4.4 samo čita njihov zapečaćeni snapshot. Ne portirati regex deklaraciju kao navodno puni parser za Java/C/C++ (`codeindex.py:23-30`) niti stale-lock manual-rm UX iz `archmap.py:138-156`.

- **Ugovor/RED:** Annex **P0.1** za source range/provenance, **P0.2** za subprocess/LSP cancel, **P2.2** za hit/output/CPU budget; S6 ignore/tenant/path granica i S8.3 freshness generation obvezni su cross-gateovi. RED-ovi: `test_structural_search_unavailable_never_masquerades_as_regex`, `test_stale_index_hit_is_never_labeled_live`, `test_ignored_secret_name_never_appears_in_hits`, `test_rg_output_flood_stays_within_budget`, `test_lsp_utf16_range_maps_to_exact_current_bytes`, `test_ambiguous_call_edge_cannot_authorize_safe_delete`, `test_archmap_query_does_not_trigger_hidden_rebuild`, `test_search_hit_revision_mismatch_blocks_edit_anchor`.

- **Verifikacija:** provjereno 2026-09-01. [ripgrep](https://github.com/BurntSushi/ripgrep/blob/master/README.md) stvarno pruža line/regex recursive search, po defaultu poštuje gitignore, ne prati symlinkove i podržava Windows/macOS/Linux. [ast-grep](https://github.com/ast-grep/ast-grep) stvarno radi tree-sitter structural search/rewrite s meta-varijablama; naš adapter namjerno koristi read-only search. [Serena](https://github.com/oraios/serena) stvarno apstrahira LSP i nudi find-symbol/reference/implementation, symbol edit i diagnostics. Lokalni NEXUS dokazano ima current-source snippets, freshness, caller/callee, related-test i architecture relation bundle, ali njegov Python/tree-sitter/regex coverage nije univerzalna type-semantic analiza; plan tu granicu čuva.

- **Pareto floor:** **PASS za 4.4.** ripgrepova brzina, ast-grep struktura i Serena LSP navigacija ostaju, a NEXUS provenance-aware graph/context podiže sustav iznad “tri search wrappera”. Strongest counterexample je stale index koji uvjerljivo pokazuje staru definiciju; revision/digest oznaka i zabrana da indexed hit autorizira edit čuvaju floor. Najslabija točka je multi-language callgraph kvaliteta; dok nema per-language evala, inferred edges ostaju discovery-only.

## 4.5 Web i browser automatizacija

- **Tip:** ADAPTER (`browser` capability-profil; registry/dispatch jezgra ostaje S4)

- **Aspekti:**

| Aspekt | browser-use | Playwright MCP | Skyvern | Najbolji izvor i graft |
|---|---|---|---|---|
| Agent-friendly stanje | Indeksirani interaktivni elementi/session | Accessibility snapshot s refovima | Page comprehension preko visiona + Playwright | **Playwright MCP/browser-use** accessibility/ref model kao default. |
| Deterministička akcija | Click/input po indeksu/refu | Structured click/fill/navigate alati | Model/vision odabire target | **Playwright** exact ref, uz naš document revision/origin binding. |
| Vizualni fallback | Vision auto/on-demand | Screenshot zaseban od action snapshota | Vision je temelj robusnosti | **Skyvern/browser-use** samo nakon DOM/AX `NOT_FOUND`, označeno probabilistički. |
| Session | Persistent browser/session | User data/storage state i shared context opcije | Workflow/session runtime | **browser-use/Playwright**, ali tenant isolation i credential scope ostaju naši. |
| Egress i sigurnost | Chromium sandbox/config | Proxy/sandbox/secrets opcije | Managed anti-bot/CAPTCHA postoji | **Naš S6 egress/approval**; Camoufox/anti-bot/CAPTCHA bypass nije cilj jezgre. |
| Context cijena | Sažeto stanje/indeksi | Snapshot može u file i depth limit | Vision je skuplji | **NEXUS file-backed AX refs + Playwright limits**. |

- **Sinteza:** Go jezgra ne implementira browser engine. `BrowserAdapter` upravlja eksplicitno instaliranim Chromium/Playwright-compatible runtimeom ili minimalnim CDP backendom; capability probe bilježi binary/version/sandbox. Bez browsera profil je `UNAVAILABLE`, a core binary i dalje radi.

```go
type PageRevision struct {
    SessionID  string
    TargetID   string
    FrameID    string
    Origin     string
    DocumentID string
    Seq        uint64
}

type ElementRef struct {
    Page       PageRevision
    Ref        string
    Role       string
    NameHash   string
    BackendID  string
}

type BrowserCommand struct {
    Kind       BrowserCommandKind // NAVIGATE|SNAPSHOT|CLICK|TYPE|UPLOAD|EVAL|SCREENSHOT
    URL        string
    Target     *ElementRef
    TextRef    *secrets.ValueRef
    FileRef    *workspace.PathRef
}

type BrowserBackend interface {
    Execute(context.Context, scheduler.AttemptGrant, BrowserCommand) (BrowserObservation, error)
}
```

  Invarijante:

  1. AX snapshot je untrusted `ContextBlock` i po defaultu ide u bounded S5 artifact; model dobiva sažetak + refove. Ref vrijedi samo za isti `PageRevision`; navigation, frame swap, origin promjena ili DOM revision poništava ga.
  2. Svaki top-level URL, redirect, subresource, websocket i download prolazi S6 egress policy. Browser-side mreža ne smije zaobići host allowlist/SSRF kontrolu. Cross-origin redirect uklanja credential header dok novi origin nije zasebno autoriziran.
  3. `CLICK/TYPE/UPLOAD/EVAL` nose effect class. Password/token input dolazi iz broker refa i ne ulazi u prompt/log; upload path prolazi workspace/scope provjeru. JS eval je zaseban high-risk capability, default off.
  4. DOM/AX je prvi lane. Vision fallback emitira confidence/provenance i traži novi snapshot prije side-effecta; koordinata iz stare slike nije dopuštena kao samostalni target.
  5. Browser profile/storage state je tenant-scoped i encrypted/ephemeral prema S9/S6 politici. Shared context između neovisnih runova je default off.
  6. Browser/Chromium proces i njegovi childovi ulaze u P2.2 budget tree; idle kill nije success. Download je draft artifact i mora proći S6.8/S5 prije exposurea ili izvršenja.

- **Salvage:** Python→Go portirati čisti CDP/AX koncept iz `/home/matej/NEXUSv2/core/browse.py:1-11,116-161`: accessibility-tree→numbered refs, event/reply pump i subresource interception. Ne portirati hand-rolled RFC6455 kao novi security owner ako održavana Go websocket biblioteka zadovolji single-binary build; dependency se vendora/pinna i fuzzira. Portirati egress ideje iz `/home/matej/NEXUSv2/core/egress.py:202-249,264-284`: scheme/host allowlist, metadata-IP hard block i redirect recheck. Ispraviti dokaznu granicu: DNS-rebinding provjera je u NEXUS-u opt-in, pa se ne smije predstavljati kao uvijek aktivna zaštita.

- **Ugovor/RED:** Annex **P0.2** za cancel session/action pokušaja, **P1.5** za downloaded artifact/supply chain i **P2.2** za browser process/output/network budget; S6.5/S6.6 su egress/secret owneri. RED-ovi: `test_stale_element_ref_cannot_click_after_navigation`, `test_subresource_metadata_ip_is_blocked`, `test_cross_origin_redirect_receives_no_secret_header`, `test_vision_coordinate_without_current_snapshot_cannot_mutate`, `test_browser_download_is_not_executable_before_supply_chain_gate`, `test_browser_child_tree_dies_on_budget_exhaustion`, `test_missing_browser_runtime_reports_unavailable_without_install`.

- **Verifikacija:** provjereno 2026-09-01. [Playwright MCP](https://github.com/microsoft/playwright-mcp) stvarno nudi accessibility snapshot, ref-based akcije, file output/depth/output limite, sandbox i isolated/storage-state opcije. [browser-use](https://github.com/browser-use/browser-use) stvarno nudi persistent browser session, indexed element state i opcionalni vision. [Skyvern](https://github.com/Skyvern-AI/skyvern) stvarno kombinira Playwright i vision, ali je AGPL-3.0 i managed anti-bot dio nije jednako dostupan u OSS jezgri; zato je referent za vision pristup, ne dependency ni anti-bot graft. Camoufox nije uključen.

- **Pareto floor:** **PASS za 4.5 uz vanjski-runtime ceiling.** Playwrightov AX/ref tok, browser-use session ergonomija i Skyvernov vision fallback ostaju, ali svaki target je revision-bound i mreža/credentiali imaju jednog S6 ownera. Strongest counterexample je dopušten top URL koji učita metadata subresource; Fetch/network interception test mora dokazati blokadu svake request lane. Najslabija točka je cross-browser backend jednakost; početni build smije podržati samo verificirani Chromium backend i mora pošteno označiti ostale `UNAVAILABLE`.

## 4.6 MCP klijent

- **Tip:** ADAPTER (`mcp-client` capability-profil nad obveznim P1.3 trust wrapperom)

- **Aspekti:**

| Aspekt | goose | Cline | Codex CLI | MCP standard | Najbolji izvor i graft |
|---|---|---|---|---|---|
| Extension UX | MCP-native extensions | Server management/marketplace | Konfigurirani MCP serveri u native jezgri | Standard transport/capability lifecycle | **goose/Cline** UX, ali install/discovery ne znači trust. |
| Protokol | MCP tool/resource/prompt surface | MCP server katalog | MCP client + auth/config | Normativni initialize/list/call/pagination | **Službeni Go SDK/spec** kao wire adapter. |
| Auth/identity | Implementacijski različito | Marketplace metadata | Config/auth podrška | Auth/security se razvija po verziji | **Annex P1.3** kao stroži lokalni ugovor: identity prije credentials. |
| Tool catalog | Server-driven | Marketplace-driven | Configured servers | `tools/list`, pagination, cache/list-changed | **MCP**, filtriran kroz 4.1/4.7 budget i namespace. |
| Retry/cancel | Klijenti često imaju lokalne reconnect petlje | Runtime-specific | Runtime-specific | Protokol definira lifecycle, ne naš owner | **S7** jedini retry/cancel owner; MCP adapter je single-attempt. |

- **Sinteza — trust wrapper oko službenog Go SDK-a:** wire encoding/negotiation ne reimplementiramo bez razloga. `MCPClient` koristi pinned MCP Go SDK adapter, ali connection state, peer identity, credential brokerage, scopes, size limits i tool ingestion ostaju naš kod.

```go
type Transport uint8 // LOCAL_STDIO|REMOTE_HTTPS
type PeerState uint8 // DISCOVERED_UNTRUSTED|IDENTITY_VERIFIED|AUTHENTICATED|AUTHORIZED|CONNECTED|MISMATCH|REVOKED

type PeerDescriptor struct {
    ServerID             string
    Transport            Transport
    Endpoint             string
    ExpectedIdentity     identity.Expectation // executable digest ili issuer/audience/SPKI pin
    AuthMethod           auth.Method
    RequestedScopes      []string
    CredentialRef        secrets.ValueRef
    TrustPolicyVersion   string
    AllowedCapabilities  []contracts.CapabilityID
    MaxToolCount         uint32
    MaxSchemaBytes       uint64
    MaxDescriptionBytes  uint64
    ExpiresAt            time.Time
}

type MCPClient interface {
    Connect(context.Context, scheduler.AttemptGrant, PeerDescriptor) (MCPConnection, *contracts.TypedError)
}

type MCPConnection interface {
    ListTools(context.Context, scheduler.AttemptGrant, *string) (ToolPage, *contracts.TypedError)
    CallTool(context.Context, scheduler.AttemptGrant, ToolID, json.RawMessage) (Result, *contracts.TypedError)
    Close() error
}
```

  P1.3 lifecycle je obvezan, bez kraćeg puta:

```text
DISCOVERED_UNTRUSTED -> IDENTITY_VERIFIED -> AUTHENTICATED
  -> AUTHORIZED -> CONNECTED
identity mismatch -> MISMATCH (terminal)
trust/credential revoke -> REVOKED (terminal)
```

  Invarijante:

  1. Lokalni stdio server pokreće se typed argv putem 4.2/S6 i mora odgovarati pinanom executable/artifact digestu neposredno prije spawna. Remote transport je HTTPS; TLS peer, issuer/audience i konfigurirani SPKI pin provjeravaju se prije nego secret broker izda credential.
  2. Redirect, DNS re-resolution i reconnect ponovno provjeravaju peer identity. Credential se šalje samo nakon `IDENTITY_VERIFIED`, samo za presjek traženih i dopuštenih scopeova, nikad u prompt, argv, URL, error ili log.
  3. `initialize`, `tools/list` page i `tools/call` su odvojeni S7 pokušaji. Adapter nema vlastiti “jedan 401 retry”, token-refresh retry ili reconnect loop. Auth refresh je eksplicitni broker effect pa S7 eventualno izdaje novi grant.
  4. `tools/list` je neprijateljski input: limit po frameu/pageu/ukupnom tool countu/schema/description/depth/regexu, validni cursor lifecycle, deterministički namespace `mcp.<server-id>.<tool-name>`, collision refusal i P0.1 provenance. Tool annotations nikad ne mogu spustiti naš effect class.
  5. MCP tools ulaze u 4.1 snapshot tek nakon P1.3 `AUTHORIZED`; 4.7 lazy view može dohvatiti sljedeću stranicu samo unutar preostalog budgeta. `listChanged` proizvodi novi registry revision i invalidira stare definition digeste.
  6. Tool rezultat, resource, prompt i elicitation sadržaj postaju `UNTRUSTED_EXTERNAL` ContextBlock s injection fenceom i size capom. MCP server ne dobiva izravan filesystem, credential ni approval callback izvan deklariranih capabilityja.
  7. AXI-style local CLI wrapper je lokalni adapter, ne prečac: executable pin, argv, scope, secret i output ugovori isti su kao za stdio MCP. Marketplace metadata je discovery hint, ne supply-chain dokaz.

- **Salvage:** Python→Go portirati minimalni initialize→paginated-list→call tok, bounded frame/body, no-redirect i untrusted datamark iz `/home/matej/NEXUSv2/core/mcp.py`. Ne portirati remote `http://`, “unverified still loads”, neograničeni total pagination/catalog niti lokalni 401/reconnect retry. Iz `/home/matej/NEXUSv2/core/mcpauth.py` portirati static bearer/OAuth credential-provider razdvajanje i cache ideju samo iza S6 secret brokera; refresh nikad ne pokreće adapter sam. Wire sloj preferira službeni `modelcontextprotocol/go-sdk`, uz pinned version i protocol conformance corpus.

- **Ugovor/RED:** Annex **P1.3** je matični ugovor. Njegov kanonski RED `test_mcp_wrong_pinned_peer_gets_no_credentials` mora promatrati broker i mrežni/stdio sink, ne samo state enum. Annex **P0.2** zabranjuje self-retry, **P0.1** veže provenance, **P2.2** ograničava frames/catalog/output. Dodatni RED-ovi: `test_mcp_http_endpoint_is_rejected`, `test_mcp_401_does_not_self_retry`, `test_mcp_redirect_rechecks_peer_before_auth`, `test_tools_list_schema_bomb_is_rejected_before_registry`, `test_list_changed_invalidates_old_tool_digest`, `test_server_annotation_cannot_downgrade_effect`, `test_mcp_tool_name_collision_is_namespaced_or_refused`, `test_revoked_peer_cannot_reconnect_with_cached_token`.

- **Verifikacija:** provjereno 2026-09-01. Aktualni MCP [Tools ugovor](https://github.com/modelcontextprotocol/modelcontextprotocol/blob/main/docs/specification/2026-07-28/server/tools.mdx) stvarno definira capability, validne schemas, deterministički `tools/list`, cursor pagination/cache i upozorava da su annotations neprovjerene. [Službeni Go SDK](https://github.com/modelcontextprotocol/go-sdk) postoji i podržava aktualni protokol. MCP-ov javni [trust model](https://github.com/modelcontextprotocol/modelcontextprotocol/security) kaže da klijenti vjeruju konfiguriranim serverima; naš Annex je namjerno stroži jer harness obrađuje marketplace/dinamičke servere. Nije verificirano da goose/Cline/Codex svaki zasebno provode executable digest + SPKI + issuer/audience + scoped broker prije credentiala; zato nisu security owneri.

- **Pareto floor:** **PASS za 4.6.** Zadržava standardni MCP wire i ecosystem UX, ali P1.3 zatvara najskuplju prazninu: credentials se ne daju samo zato što je endpoint konfiguriran. Strongest counterexample je valjani hostname koji nakon reconnecta vodi drugom peeru; identity recheck mora prethoditi svakom credential releaseu. Najslabija točka je drift vrlo novog 2026-07-28 protokola/SDK-a; pinned conformance fixtures za svaku podržanu protocol verziju moraju biti release gate, bez “latest” runtime fetchanja.

## 4.7 Tool exposure budget i scoped discovery

- **Tip:** MEHANIZAM

- **Aspekti:**

| Aspekt | vlastiti ToolView | pi-mono | MCP `tools/list` | Najbolji izvor i graft |
|---|---|---|---|---|
| Minimalna površina po turnu | Capability/role/policy/task presjek | Minimalan stalni tool set kao referent dizajn | Server catalog i pagination | **Naš ToolView**; minimalnost je contract, ne fiksni broj alata. |
| Lazy schema | Compact catalog pa resolve odabranih schema | Mali prompt smanjuje potrebu za discoveryjem | Cursor pages/cache/list-changed | **MCP** pagination/cache primitive + naš dvostupanjski catalog/schema resolve. |
| Budget | Hard schema-token/bytes count | Hipoteza o koristi malog seta | Page size nije naš prompt budget | **P2.2** hard reservation i ledger; “≈4 alata pobjeđuje” ostaje hipoteza. |
| Miss recovery | Explicit widening request | Nije dovoljan generički mehanizam | Dohvat iduće stranice | **Naš deterministic widening**, nova durable odluka, nikad skrivena promjena viewa usred provider calla. |
| Mjerenje | selection miss, schema bytes/tokens, unused exposure | Minimalnost kao kvalitativan signal | Catalog/cache metrics | **Naš metrics contract** bez optimiziranja samo na mali broj. |

- **Sinteza — immutable view po provider turnu:** registry snapshot može biti velik, ali provider dobiva samo `ToolView` vezan uz closure, role, policy, task features, model limits i preostali budget. View se ne mijenja tijekom streama; proširenje zatvara ili odgađa postojeći turn i stvara novu durable view revision.

```go
type CompactTool struct {
    ID                ToolID
    Title             string
    CapabilityTags    []contracts.CapabilityID
    Effect            contracts.EffectClass
    DefinitionDigest  DefinitionDigest
    SchemaTokenUpper  uint32
}

type ToolView struct {
    ID                string
    RegistryRevision  string
    ClosureDigest     string
    PolicyDigest      string
    ModelRevision     string
    Catalog           []CompactTool
    Exposed           []Definition
    Budget            ExposureBudget
}

type ExposureBudget struct {
    MaxToolCount        uint32
    MaxSchemaBytes      uint64
    MaxSchemaTokens     uint64
    MaxDescriptionBytes uint64
    MaxDiscoveryPages   uint32
}

type ViewPlanner interface {
    Plan(ViewRequest) (ToolView, *contracts.TypedError)
    Widen(ToolView, WidenRequest) (ToolView, *contracts.TypedError)
}
```

  Algoritam planiranja:

```text
registry snapshot
  -> S0.5 capability closure
  -> role/policy/approval/data-residency intersection
  -> task-required tool classes
  -> mandatory safety/control tools
  -> rank remaining by direct capability match + recent verified utility
  -> reserve exact upper-bound schema budget
  -> emit immutable, stable-ordered ToolView
```

  Invarijante:

  1. Capability closure i policy prvo uklanjaju nedopuštene alate; ranker ih nikad ne vidi. Mandatory safety/control tool ne smije ispasti zbog popularnosti ili token optimizacije.
  2. Compact catalog ne sadrži puni schema/doc prompt. Provider dobiva puni `Definition` samo za `Exposed`; MCP next page/schema resolve radi se lazy i bounded. Schema token upper bound računa se tokenizerom točnog model revisiona ili konzervativnim byte upper boundom.
  3. Hard budget se rezervira prije provider calla i knjiži u P2.2 ledger. Ako minimalni obvezni set ne stane, turn faila `TOOL_VIEW_BUDGET_UNSATISFIABLE`; nema trunciranja JSON sheme u nevaljanu ili semantički drukčiju shemu.
  4. `ToolView.ID` pokriva registry/closure/policy/model revision i exposed digeste. Tool call izvan viewa ili s drugim digestom završava `TOOL_NOT_EXPOSED`; ime koje model halucinira ne pokreće discovery ni fuzzy name match.
  5. Widening je eksplicitno: model/harness vraća typed `MISSING_CAPABILITY`, planner navodi razlog i trošak, policy odobrava novi set, journal zapisuje novu view revision. Aktivni provider stream ne dobiva tiho dodatne alate.
  6. Metrics razlikuju `selection_miss` (potreban registrirani alat nije bio exposed), `capability_absent`, `policy_denied`, `model_schema_reject`, `unused_exposure` i `wrong_tool_selection`. Sam broj alata nije success metrika.
  7. Cache key uključuje tenant, registry revision, closure, policy, role, task feature digest i model revision. Nema cross-tenant reusea viewa niti stale reusea nakon MCP `listChanged`, skill revokea ili schema promjene.

- **Salvage:** NEXUS nema gotov kanonski ToolView. Iz `/home/matej/NEXUSv2/llm/executor.py` portirati stvarni popis tool capabilityja samo kao početni catalog fixture; ne portirati fixed veliki `tool_names` i prompt koji unaprijed umeće sve schema/opise. Iz `core/mcp.py` portirati cursor primitive nakon P1.3 hardeninga. NEXUS code retrieval može pomoći task-feature rankingu, ali ranker ne smije postati novi autonomni LLM poziv ni sigurnosni owner.

- **Ugovor/RED:** Annex **P0.1** veže view/provenance, **P1.1** osigurava da revoked/swapani extension nestane iz nove revision i **P2.2** daje hard schema/tool/token budget i monotoni ledger. RED-ovi: `test_tool_outside_view_never_dispatches`, `test_required_safety_tool_cannot_be_ranked_out`, `test_unsatisfiable_mandatory_view_fails_without_schema_truncation`, `test_mcp_pagination_stops_at_discovery_budget`, `test_list_changed_invalidates_cached_view`, `test_widening_cannot_mutate_active_turn_view`, `test_cross_tenant_view_cache_never_hits`, `test_hallucinated_tool_name_does_not_fuzzy_dispatch`, `test_schema_budget_is_charged_before_provider_call`.

- **Verifikacija:** provjereno 2026-09-01. MCP [Tools ugovor](https://github.com/modelcontextprotocol/modelcontextprotocol/blob/main/docs/specification/2026-07-28/server/tools.mdx) stvarno ima cursor pagination, deterministic ordering, cache hints i `listChanged`, pa je prikladan discovery primitive. Playwrightova službena [MCP-vs-CLI usporedba](https://github.com/microsoft/playwright.dev/blob/main/mcp/introduction.mdx) eksplicitno navodi da MCP tool schemas/snapshots imaju veći token trošak, što podržava lazy exposure problem. Nije pronađen primarni dokaz za spec tvrdnju da Pi s približno četiri alata pobjeđuje veće harnesse na Terminal-Bench 2.0; to ostaje označena hipoteza i ne ulazi u acceptance kriterij.

- **Pareto floor:** **PASS za 4.7.** MCP discovery/pagination i minimal-tool referent ostaju, ali naš view dodaje hard budget, immutable turn binding, sigurnosni mandatory floor i mjerljiv miss recovery. Strongest counterexample je task kojem treba rijedak alat koji ranker sakrije; explicit `MISSING_CAPABILITY` widening i `selection_miss` mjera zatvaraju funkcionalni pad bez vraćanja svih schema u svaki prompt. Najslabija točka je početni ranking bez empirijskih podataka; v1 zato mora biti deterministički capability-match, a learned ranker dolazi tek nakon holdout dokaza da smanjuje miss i ukupne tokene bez pada completion ratea.

## S4 integracijski acceptance gate

S4 nije prihvaćen zato što svaki adapter ima unit test. Minimalni release gate je end-to-end kontrolirana ablacija kroz pravi S3→S4→S6→S7→S5 put:

1. Registriraj built-in `exec`, `edit`, `search` i jedan pinned fake MCP tool; izgradi turn-scoped `ToolView` s namjerno izostavljenim alatom.
2. Dokaži da valjani exposed read prolazi, out-of-view call faila prije side-effecta, stale digest faila nakon registry revisiona i wrong MCP peer ne vidi credential bytes.
3. Pokreni child koji stvara grandchild i flooda oba outputa; hard P2.2 limit mora ubiti cijelo stablo, održati bounded memoriju i vratiti terminalni non-success ledger.
4. Na istoj fixture datoteci dokaži exact edit GREEN, duplicate/stale anchor RED bez byte promjene, semantic multi-file out-of-scope RED all-or-nothing i validni multi-file edit kroz jedan S5 checkpoint.
5. Ablatiraj freshness label na stale code-index hitu i očekuj harness RED; ablatiraj `DefinitionDigest` check i očekuj stale-call detector RED. Tek tada GREEN suite dokazuje stvarne membrane, ne samo sretni put.

**Proof ceiling:** ovaj plan verificira javno dokumentirane mehanizme i lokalni NEXUS kod na 2026-09-01, ali ne dokazuje buduću kvalitetu Go porta, model→edit-format rangiranje, jednak OS sandbox/resource enforcement ni live browser/MCP interoperabilnost. Ti se claimovi promoviraju samo platform matrixom, parser fuzzom, vlastitim edit holdoutima i live opt-in canaryjem nad eksplicitno odobrenim endpointom; nijedan se ne smije zaključiti iz compile-green rezultata.
