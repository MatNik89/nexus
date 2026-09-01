PLAN S5

# S5 — Workspace, VCS i checkpoint/rollback

## Zajedničke odluke i paket-layout

- S5 je jedini vlasnik workspace revizije, checkpointa i objave file-promjene. S4 predaje deklarativni `MutationPlan`; S6 odobrava putanje i izolira proces; S7.3 je jedini vlasnik durable leasea, startup sweepea i recovery tranzicija; P0.3 journal je jedini trajni event write-owner. Predloženi layout: `internal/workspace/{contract,checkpoint,isolation,publish,provenance}` uz S7 adapter `internal/recovery/workspace`.
- Kanonski tok je `sealed base revision -> isolated candidate -> MutationPlan -> conflict check -> canonical-head CAS -> recoverable materialization -> verified provenance`. Checkpoint, worktree i publisher nisu tri write puta: checkpoint sprema sadržajno adresiranu reviziju, izolacija je mjesto rada, a samo publisher mijenja kanonski workspace.
- Osnovni single-binary put koristi interni content-addressed shadow store i `COPY_TREE` izolaciju. Native `git` je detektirani adapter za linked worktree i Git-ref CAS; njegov izostanak nikad ne vodi u dijeljeni radni direktorij. `go-git/v6` je prihvatljiv za object/store operacije, ali ne i kao lažna potpuna zamjena za native linked-worktree/plumbing semantiku.
- Korisnikov postojeći dirty state je ulazna lineage revizija, ne smeće koje treba commitati, stashati ili resetirati. Harness nikad ne mijenja korisnikov branch/HEAD radi vlastitog checkpointa.
- Portabilno se može garantirati atomska zamjena pojedinačne datoteke i atomski CAS kanonskog head-metapodatka. Ne tvrdi se trenutačna atomska vidljivost proizvoljnog skupa običnih datoteka: multi-file publish ima durable manifest i deterministički recovery.

Minimalni zajednički ugovor:

```go
package workspace

type WorkspaceID string
type RevisionID string       // digest kanonskog TreeManifesta
type CheckpointID string
type PublishID string

type TreeEntry struct {
    Path       string         // slash-normalized, relative, bez traversal-a
    Kind       EntryKind      // REGULAR|DIRECTORY|SYMLINK
    Mode       uint32
    BlobDigest string         // raw bytes; symlink target za SYMLINK
    Size       int64
    Trust      contracts.TrustLevel
    Taint      contracts.TaintSet
}

type TreeManifest struct {
    WorkspaceID WorkspaceID
    Revision    RevisionID
    Parent      *RevisionID
    Entries     []TreeEntry   // sortirano po canonical Path
    PolicyHash  string
}

type WorkspaceRef struct {
    WorkspaceID WorkspaceID
    RootHandle  paths.DirHandle
    Revision    RevisionID
    Lease       recovery.ResourceLeaseRef
    Isolation   IsolationKind
    Fence       uint64
}

type HeadStore interface {
    Load(context.Context, WorkspaceID) (RevisionID, error)
    CompareAndSwap(context.Context, WorkspaceID, RevisionID, RevisionID, uint64) error
}
```

Obvezne globalne invarijante:

1. `WorkspaceID` nastaje iz stabilnog kanonskog identiteta, ne iz neprovjerene string-putanje; svaka putanja se rješava kroz prethodno otvoren root handle i ponovno provjerava pri učinku.
2. Blob digest pokriva sirove bytes, tip i mode; Git filteri, autocrlf, hooks i globalni config ne smiju nevidljivo promijeniti checkpoint sadržaj.
3. Svaka mutacija nosi `BaseRevision`, write-set i S7 fencing token. Stale revision ili token odbija cijeli logical publish prije napredovanja kanonskog heada.
4. Checkpoint prije mutacije mora biti durable ili se mutacija ne izvršava, osim eksplicitno odobrene i označene `NON_UNDOABLE` operacije koja ulazi u P1.4 lane.
5. S5 nikad ne kill-a proces niti sam proglašava lease orphanom. S7.3 to radi po P1.2, a S5 daje type-specific cleanup adapter i dokaz dosega.

## 5.1 Checkpoint i rollback po koraku (undo)

- **Tip:** MEHANIZAM

- **Aspekti:**

| Aspekt | Cline | Aider | LangGraph | NEXUS | Najbolji izvor i graft |
|---|---|---|---|---|---|
| Granularnost | Checkpoint vezan uz korak i diff; restore iz UI-a | Automatski commit nakon AI edit-turna + `/undo` | Checkpoint po superstepu i thread historija | Shadow-Git snapshot prije promjene | **Cline** za step-scoped safety UX; checkpoint se veže uz `ActionID`, ne samo razgovorni turn. |
| Zaštita dirty rada | Checkpoint razlikuje tijek generiranih promjena | Prvo zasebno commita postojeći dirty rad | Stanje grafa je izvan korisničkog Git stabla | Shadow repo je izvan user repoa | **NEXUS shadow-store**: sačuvaj dirty baseline bez user commita, stasha ili HEAD promjene. |
| Undo/fork | Diff pregled i restore | `/undo` vraća commit i conversation bookkeeping | Replay/update iz starog checkpointa stvara novu granu | `restore`, `rollback`, `fork`, pre-restore snapshot | **LangGraph + NEXUS** za immutable timeline/fork; restore je nova revizija, ne brisanje povijesti. |
| Točnost sadržaja | Praktični workspace checkpoint | Git semantics mogu uključiti ignore/filter pravila | Apstraktno stanje, nije file-byte ugovor | `git add/check-out` danas ovisi o Git pravilima | **Naš raw content-addressed tree** za bytes/mode/symlink; native Git je opcionalni import/export adapter. |
| Slab model | Čest, lako dostupan restore po koraku | Svaki edit-turn ima povratnu točku | Runtime može nastaviti iz starog stanja | Best-effort API | **Cline**: automatski checkpoint neposredno prije svake mutirajuće akcije; model ga ne mora zatražiti. |

- **Sinteza:** `CheckpointStore` sprema immutable, sadržajno adresirane tree-manifeste izvan korisnikova `.git` direktorija. S3/S4 moraju dobiti durable `Before` checkpoint prije svake akcije čiji je deklarirani effect `FS_WRITE`; nakon verificirane objave može se zapečatiti `After` checkpoint. Jedan korak s više datoteka ima jedan checkpoint i jedan write-set.

```go
type ScopePolicy struct {
    IncludedRoots       []paths.RelPath
    ExcludedPatterns    []string
    SensitivePaths      []paths.RelPath
    MaxBytes            int64
    MaxFiles            int64
    RequireUndoableWrite bool
}

type SnapshotRequest struct {
    Envelope     contracts.Envelope
    Workspace    WorkspaceRef
    ActionID     loop.ActionID
    Parent       *CheckpointID
    Phase        CheckpointPhase // BEFORE|AFTER|PRE_RESTORE
    Scope        ScopePolicy
}

type Checkpoint struct {
    ID           CheckpointID
    WorkspaceID  WorkspaceID
    ActionID     loop.ActionID
    Parent       *CheckpointID
    Phase        CheckpointPhase
    Revision     RevisionID
    ManifestHash string
    PolicyHash   string
    CreatedAt    time.Time
}

type CheckpointStore interface {
    Snapshot(context.Context, SnapshotRequest) (Checkpoint, error)
    PlanRestore(context.Context, WorkspaceRef, CheckpointID) (MutationPlan, error)
    Fork(context.Context, CheckpointID, loop.RunID) (RevisionID, error)
}
```

  Normativni put:

```text
mutating Action accepted
  -> resolve ScopePolicy and prove every writable path undoable
  -> capture raw TreeManifest + blobs
  -> fsync blobs/manifest + append CHECKPOINT_DURABLE through P0.3
  -> permit tool mutation in isolated workspace
  -> verify candidate -> publish through 5.3

restore request
  -> snapshot current revision as PRE_RESTORE
  -> derive exact MutationPlan(checkpoint revision)
  -> authorize through S6 -> publish through 5.3
```

  Invarijante:

  1. Checkpoint je automatski i obvezan prije mutacije. Neuspjelo spremanje, prekoračen scope ili write u excluded/sensitive path daje `CHECKPOINT_UNAVAILABLE` i nula tool učinaka. Osjetljiva iznimka zahtijeva zaseban approval i retention policy; ne duplicira se potajno u shadow store.
  2. Manifest pokriva regularne datoteke, direktorije potrebne za rekonstrukciju, symlink target i executable/mode bitove. Učitavanje checkpointa ponovno hashira manifest i blobove; mismatch je `CHECKPOINT_CORRUPT`, bez djelomičnog restorea.
  3. Snapshot ne izvršava Git hookove, clean/smudge filtere ni globalni Git config. Ako se native Git koristi za object import, koristi se plumbing/no-filter semantika i izolirani config/env.
  4. Restore nikad nije destruktivni rewind povijesti: prvo sprema aktualno stanje, zatim kroz 5.3 objavljuje novu reviziju čiji je sadržaj jednak odabranom checkpointu. `/undo` se zato može undoati.
  5. Timeline je immutable DAG: roditelj se ne mijenja, fork dobiva novi run/revision identitet, a deduplikacija blobova ne mijenja ownership, retention ni taint metadata.
  6. Checkpoint scope prati stvarni S6 write grant. Ako adapter naknadno proširi write-set, grant i checkpoint više ne pokrivaju akciju te se pokušaj odbija prije writea.

- **Salvage:** iz `/home/matej/NEXUSv2/core/checkpoint.py` Python→Go portirati shadow-repo izvan user repoa, deterministički workspace key, snapshot-prije-restore, delete-added-paths logiku kao diff-plan te `latest/fork` mentalni model. Ne portirati string/best-effort povrate, process-local `RLock`, oslanjanje na `git add -A`/gitignore/filtere ni višedatotečni `git checkout` kao tvrdnju atomiciteta. Iz `/home/matej/NEXUSv2/core/memgit.py` portirati immutable snapshot, deduplikaciju i “snapshot current prije restorea” obrazac, ali ne swallowing grešaka ni SQLite-memory specifičan payload. Novi Go owner je `CheckpointStore`; native Git i go-git su zamjenjivi storage adapteri iza contract testova.

- **Ugovor/RED:** veže Annex **P0.1** (typed envelope), **P0.3** (jedini durable event owner), S5.3 conflict/provenance ugovor i P1.4 za eksplicitno neundoable učinke. Release RED-ovi: `test_weak_model_bad_edit_undo_restores_exact_tree` mora promijeniti sadržaj, executable bit i symlink pa dokazati byte-identičan restore; `test_failed_checkpoint_blocks_mutating_tool` injektira fsync/storage kvar i očekuje nula writeova; `test_restore_is_itself_undoable`; `test_git_filter_cannot_change_checkpoint_bytes`; `test_ignored_secret_write_without_checkpoint_policy_is_denied`; `test_corrupt_checkpoint_never_partially_restores`.

- **Verifikacija:** provjereno 2026-09-01. Clineov službeni [checkpoint workflow](https://github.com/cline/cline/blob/main/docs/core-workflows/checkpoints.mdx) stvarno pruža step-level compare/restore; Aiderova [Git dokumentacija](https://aider.chat/docs/git.html) potvrđuje automatske commitove, zaštitni commit dirty datoteka i `/undo`; LangGraphova [persistence dokumentacija](https://docs.langchain.com/oss/python/langgraph/persistence) potvrđuje checkpoint po superstepu, historiju, replay i fork-like update. Nijedan od ta tri izvora sam ne dokazuje naš raw-byte, secret-scope, fsync i restore-through-CAS ugovor; to je vlastiti mehanizam. NEXUS shadow obrazac je verificiran iz aktualnog lokalnog koda, ali njegova sadašnja best-effort/Git-filter semantika nije prihvatljiva kao gotov owner.

- **Pareto floor:** **PASS za 5.1 uz durability gate.** Hibrid zadržava Clineovu sigurnosnu mrežu za slab model, Aiderovu automatsku granularnost i LangGraphovu granajuću historiju, a ne dira korisnikov Git. Strongest counterexample je crash nakon spremanja dijela blobova, prije manifesta: nedosegnuti blobovi smiju ostati GC-smeće, ali bez fsyncanog manifesta i P0.3 događaja checkpoint ne postoji i mutacija ne kreće. Najslabija točka je rast shadow storea; retention/GC smije se dodati tek kad referentni DAG i active runovi dokazano sprječavaju brisanje živog checkpointa.

## 5.2 Izolacija radnog prostora (worktree/container po zadatku ili subagentu)

- **Tip:** MEHANIZAM

- **Aspekti:**

| Aspekt | OpenHands | SWE-agent | OpenAI Agents SDK Sandbox Agents | NEXUS | Najbolji izvor i graft |
|---|---|---|---|---|---|
| Runtime workspace | Lokalni ili ephemeral Docker/Kubernetes workspace iza sučelja | Reset/clone task okoline u containeru | Manifestom deklarirane odvojene sandbox sesije | Native Git linked worktree u tempu | **OpenHands** backend sučelje; local-copy, Git-worktree i container su isti typed lifecycle. |
| Čist početak | Runtime kreira kontroliranu okolinu | Reset na task base | Nova sesija iz manifesta/snapshota | Samo clean Git repo dobiva izolaciju | **SWE-agent**, ali base uključuje zapečaćeni korisnikov dirty snapshot, ne nužno čist HEAD. |
| Session/snapshot lifecycle | Workspace/session kontrolira runtime | Task reset je eksplicitan | Snapshot, resume i cleanup su prvorazredni | `defer remove/prune` | **Sandbox Agents + P1.2** za durable lease/resume/cleanup; `defer` nije crash recovery. |
| Subagent izolacija | Runtime instance po agentu | Task instance | Odvojena sandbox session | Paralelni worktreei | **NEXUS linked-worktree** za brz Git coding lane; svaki S12 subagent dobiva vlastiti root i lease. |
| Siguran fallback | Backend ovisi o deploymentu | Container pretpostavka | Beta capability | Dirty/non-Git trenutačno dijeli repo | **Naš COPY_TREE** fallback; dijeljeni writable root je zabranjen. |

- **Sinteza:** `IsolationManager` iz sealed 5.1 base revizije kreira zaseban root. Za Git workspace preferira native `git worktree add --detach` na točnom base commitu, pa u child overlay-a autorizirani dirty/untracked snapshot bez commita korisnikova brancha. Ako Git ili linked-worktree capability nije dostupan, koristi bounded pure-Go `COPY_TREE`; container/Sandbox Agent backend je profilni adapter. Worktree izolira datoteke, ne predstavlja S6 sandbox.

```go
type IsolationKind uint8 // GIT_WORKTREE|COPY_TREE|CONTAINER|REMOTE_SANDBOX

type CreateRequest struct {
    Envelope      contracts.Envelope
    WorkspaceID   WorkspaceID
    BaseRevision  RevisionID
    RunID         loop.RunID
    AgentID       agents.AgentID
    Backend       IsolationKind
    Policy        IsolationPolicy
}

type IsolationPolicy struct {
    AllowedRoot       paths.DirHandle
    MaxBytes          int64
    MaxFiles          int64
    RequireOSSandbox bool
    CleanupPolicy    recovery.CleanupPolicy
}

type WorkspaceHandle struct {
    Ref          WorkspaceRef
    BaseRevision RevisionID
    GitAdminRef  *paths.DirHandle
    SandboxRef   *sandbox.Ref
}

type Backend interface {
    Create(context.Context, CreateRequest, recovery.ResourceLeaseRef) (WorkspaceHandle, error)
    Resume(context.Context, recovery.ResourceLeaseRef) (WorkspaceHandle, error)
    Cleanup(context.Context, WorkspaceHandle, recovery.Fence) (CleanupEvidence, error)
}
```

  Normativni lifecycle:

```text
S7.3 acquire durable WORKSPACE_LOCK + TEMP_DIR lease
  -> fence token issued before path/admin mutation
  -> create isolated root from sealed BaseRevision
  -> verify manifest digest, containment and backend capability
  -> hand root handle to S6/S4
  -> on finish: stop owned process tree -> publish/retain evidence
  -> backend cleanup -> verify absence -> S7.3 RELEASED

restart
  -> S7.3 P1.2 sweep classifies lease
  -> FENCED only after ownership proof
  -> S5 adapter removes exact worktree/temp resource
  -> uncertainty => QUARANTINED, never broad prune/delete
```

  Invarijante:

  1. Nema `isolated=false` dijeljenog fallbacka. Ako nijedan backend ne može stvoriti zaseban root pod budgetom/policyjem, zadatak završava `WORKSPACE_ISOLATION_UNAVAILABLE` prije tool dispatcha.
  2. Svaki S12 subagent dobiva jedinstveni `WorkspaceID/root/ResourceLease/fencing_token`, ali istu immutable `BaseRevision`. Subagent ne vidi peer root; rezultat je candidate tree koji se može objaviti samo kroz 5.3.
  3. Dirty tracked, untracked i dopušteni ignored sadržaj ulaze u base manifest po policyju. Native worktree se prvo gradi iz Git basea, zatim se overlay radi iz shadow manifesta; harness ne radi commit/stash/reset u user repou.
  4. Kreiranje i cleanup koriste canonical handle containment, ne string-prefix. Symlink/junction/reparse-point ili mount koji izlazi iz owned roota odbija operaciju; cleanup nikad ne slijedi link.
  5. Native Git adapter koristi strukturirani argv, izolirani env/config, `--porcelain -z` gdje parsira strojni izlaz te verificiranu mapu worktree admin-entryja. Globalni prune bez dokazanog resource ownershipa je zabranjen.
  6. Cleanup redoslijed je procesno stablo, otvoreni sessioni, worktree admin entry, temp root, pa release leasea. Djelomični cleanup ostaje `CLEANING` ili `QUARANTINED`; aktivni lease se ne dira.
  7. `go-git` partial linked-worktree podrška nije dovoljna za ovaj backend. Native Git je capability adapter; pure-Go COPY_TREE održava single-binary osnovu bez semantičkog downgradea u shared workspace.

- **Salvage:** iz `/home/matej/NEXUSv2/core/worktree.py` Python→Go portirati per-task detached linked-worktree, NUL-safe status parser, realpath containment, promjene uključujući delete/rename, per-repo serializaciju i first-writer-wins namjeru. Ne portirati dirty/non-Git `isolated=false` fallback, process-local lock kao concurrency dokaz, fiksni `.nexus-tmp` naziv ni neograničeni `git worktree prune`. Iz `/home/matej/NEXUSv2/core/procreg.py` portirati PID start-token/process-group identity provjere kao S7.3 recovery pomoć; S5 ne preuzima ownerstvo lease state-machinea. Postojeće worktree test-fixture za disjoint, same-path, rename i autocrlf postaje migracijski corpus, ne dokaz novog durable ugovora.

- **Ugovor/RED:** Annex **P1.2** je obvezni gate, s ownerom S7.3 i kanonskim RED-om `test_sigkill_restart_reaps_owned_tree_only`. Dodatni RED-ovi: `test_dirty_or_nongit_workspace_never_falls_back_to_shared_root`; `test_dirty_baseline_is_preserved_without_user_commit`; `test_two_subagents_cannot_read_or_write_peer_workspace`; `test_recycled_pid_cannot_authorize_worktree_cleanup`; `test_symlink_escape_is_quarantined_not_deleted`; `test_crash_between_worktree_create_and_register_is_reconciled`; `test_partial_cleanup_never_releases_lease`; OS matrica `test_worktree_cleanup_after_sigkill_{windows,linux,darwin}`. Sandbox Agents beta promocija dodatno zahtijeva traversal/symlink i cleanup-failure RED prema 5.2 spec-u.

- **Verifikacija:** provjereno 2026-09-01. OpenHandsov službeni [software-agent-sdk](https://github.com/OpenHands/software-agent-sdk) potvrđuje workspace apstrakcije s lokalnim i ephemeral Docker/Kubernetes okolinama; SWE-agentov [environment implementation](https://github.com/SWE-agent/SWE-agent/blob/main/sweagent/environment/swe_env.py) potvrđuje task reset/clone u kontroliranoj okolini; OpenAI-jev službeni [Sandbox Agents dokument](https://github.com/openai/openai-agents-python/blob/main/docs/sandbox_agents.md) potvrđuje beta manifest/session/snapshot/resume/cleanup model. Gitov [git-worktree ugovor](https://git-scm.com/docs/git-worktree.html) potvrđuje linked worktree, lock, remove/prune i `--porcelain -z`. `go-git` [compatibility matrica](https://github.com/go-git/go-git/blob/main/COMPATIBILITY.md) linked-worktree/plumbing podršku označava parcijalnom, zato nije izabran kao jedini 5.2 backend.

- **Pareto floor:** **PASS za 5.2 uz P1.2 i OS matricu.** Dobiva se OpenHandsova backend zamjenjivost, SWE-agentov resettable task root, Sandbox Agents lifecycle i NEXUSova brza Git paralelizacija, bez opasnog shared fallbacka. Strongest counterexample je SIGKILL između `git worktree add` i durable registracije: startup mora reconciliirati samo admin-entry/root čiji se identitet može vezati uz pre-acquired lease; inače ga karantira. Najslabija točka je Windows junction/PID identity dokaz, pa Windows backend ne smije biti promoviran samo na temelju Linux testova.

## 5.3 Dirty-tree ownership, atomic write, conflict detection i provenance artefakata

- **Tip:** MEHANIZAM

- **Aspekti:**

| Aspekt | Aider | Codex CLI | Cline | NEXUS | Najbolji izvor i graft |
|---|---|---|---|---|---|
| Dirty ownership | Git-aware tijek odvaja postojeće promjene prije AI rada | Patch se primjenjuje na očekivani kontekst | Checkpoint lineage razlikuje generirano i postojeće | Worktree sync uspoređuje s HEAD-om | **Aider** za eksplicitni user baseline, ali bez prisilnog user commita. |
| Conflict detection | Git diff/commit disciplina | Odbija patch kad očekivani kontekst ne odgovara | Diff prije restorea | Same-path first-writer-wins; disjoint merge | **Codex + NEXUS**: digest-bound path CAS i canonical-head CAS, bez fuzzy publisha. |
| Atomic write | Git commit daje atomsku ref-promjenu | Pojedini patch/file write ide kroz kontrolirani apply | Checkpoint je recovery oslonac | Temp + `os.replace`, ali multi-file nije atomaran | **Git `update-ref old->new`** ili interni transactional head za kanonsku reviziju; fs projekcija je recoverable. |
| Provenance | Commit/diff metapodaci | Tool/event evidence | Generated-vs-user lineage | Trust/taint + datamark | **NEXUS taint lattice** uz novi niže-trust sandbox attestor koji je vidio stvarne učinke. |
| Interop izvoz | Git historija | Event/diff | Checkpoint timeline | Vlastiti metadata | **W3C PROV-O** samo kao export adapter, nikad interni sigurnosni owner. |

- **Sinteza:** `Publisher` prima deklarativni write-set iz izoliranog candidate treea. Najprije uspoređuje svaki aktualni path s njegovim `ExpectedDigest`, zatim proizvodi novi sadržajno adresirani tree i atomski mijenja kanonski head samo ako su `BaseRevision` i fencing token još aktualni. Za Git backend ref CAS koristi `git update-ref <ref> <new> <old>`; za single-binary backend isti ugovor implementira SQLite transakcija nad internim headom. Tek nakon head CAS-a `Materializer` projektira novu reviziju u korisnikov filesystem kroz durable manifest.

```go
type PathChange struct {
    Path           paths.RelPath
    Kind           ChangeKind // CREATE|MODIFY|DELETE|RENAME
    FromPath       *paths.RelPath
    ExpectedDigest *string    // nil samo za dokazano odsutan CREATE
    ResultDigest   *string    // nil samo za DELETE
    ExpectedMode   *uint32
    ResultMode     *uint32
    Trust          contracts.TrustLevel
    Taint          contracts.TaintSet
}

type MutationPlan struct {
    ID             PublishID
    Envelope       contracts.Envelope
    WorkspaceID    WorkspaceID
    BaseRevision   RevisionID
    CandidateTree  RevisionID
    ActionID       loop.ActionID
    Fence          uint64
    Changes        []PathChange
    ProvenanceRef  string
}

type PublishState uint8 // PREPARED|HEAD_COMMITTED|MATERIALIZING|VERIFIED|CONFLICT|RECOVERING|QUARANTINED

type PublishTxn struct {
    Plan          MutationPlan
    State         PublishState
    OldHead       RevisionID
    NewHead       RevisionID
    Applied       []paths.RelPath
    Verified      []paths.RelPath
    Error         *contracts.TypedError
}

type ArtifactProvenance struct {
    ArtifactDigest       string
    RunID                loop.RunID
    ActionID             loop.ActionID
    AttemptNo            uint32
    PrincipalID          string
    SandboxIdentity      string
    SandboxVersion       string
    BaseRevision         RevisionID
    ResultRevision       RevisionID
    InputLineage         []string
    WriteSetDigest       string
    ObservedArgvDigest   string
    ObservedFSDigest     string
    ObservedNetworkDigest string
    EffectLogDigest      string
    AttestorIdentity     string
    Attestation          []byte
    Trust                contracts.TrustLevel
    Taint                contracts.TaintSet
}

type Publisher interface {
    Prepare(context.Context, MutationPlan, provenance.Attestation) (PublishTxn, error)
    CommitHead(context.Context, PublishTxn) (PublishTxn, error)
    Materialize(context.Context, PublishTxn) (PublishTxn, error)
    Recover(context.Context, PublishID, recovery.Fence) (PublishTxn, error)
}
```

  Normativni tok i conflict pravila:

```text
candidate tree + lower-trust attestation
  -> verify attestor identity/signature + plan/write-set binding
  -> for each path: current == expected => eligible
                    current == result   => idempotent
                    otherwise           => CONFLICT
  -> build NewHead and durable PREPARED manifest
  -> CAS canonical head OldHead -> NewHead with current fencing token
  -> per path: same-dir unique temp -> write -> fsync -> final recheck -> replace -> dir fsync
  -> rehash complete tree -> VERIFIED; emit through P0.3
```

  Invarijante:

  1. First-writer-wins znači da od dva publish pokušaja s istim `BaseRevision` najviše jedan smije uspješno promijeniti kanonski head. Stali publisher mora rebaseati i ponovno verificirati; nema silent overwritea ni automatskog fuzzy mergea.
  2. Konflikt na jednoj putanji odbija cijeli logical publish prije head CAS-a. Disjoint promjene mogu se ponovno planirati nad novim headom; to je novi plan/revizija, ne iznimka stale CAS-u.
  3. Pojedinačna datoteka koristi jedinstven same-directory temp, restrictive mode, flush/fsync, neposredni expected-digest recheck, atomic replace i directory fsync gdje OS to podržava. Cross-device rename, link traversal ili nepoznata durability sposobnost faila zatvoreno ili je eksplicitno označena slabijim capabilityjem.
  4. Nakon head CAS-a crash ne vraća status `SUCCEEDED`: durable manifest ostaje `MATERIALIZING/RECOVERING`; S7.3 recovery nastavlja idempotentnu projekciju ili karantira. Kanonski head je atomski, ali čitatelji običnog filesystema tijekom višedatotečne projekcije mogu vidjeti prijelazno stanje; harness čitatelji zato čitaju samo verificiranu revision/view granicu.
  5. Vanjski proces bez harness leasea može promijeniti datoteku između zadnje provjere i replacea; opći portable filesystem nema compare-and-swap replace. Materializer ponovno hashira rezultat i takvu utrku označava `EXTERNAL_WRITE_RACE/QUARANTINED`; spec ne tvrdi matematičko sprječavanje svakog host-racea.
  6. Dirty user sadržaj je dio `BaseRevision` lineagea. Datoteka se smije promijeniti samo ako planov `ExpectedDigest` odgovara upravo toj dirty verziji; HEAD iz user Git repoa nije dovoljan expected state.
  7. Provenance prihvaća samo atestaciju iz S6 sandbox/membrane koja je promatrala stvarni argv/fs/network učinak. Runtime, model ili tool adapter ne smije sam sebi podići trust niti potpisati zamjenski effect log. Taint izlaza je najmanje pouzdan od svih ulaza i execution sloja; trust downgrade je monotono.
  8. Datamark se dodaje model-visible prikazu manifesta/diffa, nikad izvornim bytesima. PROV-O je loss-aware export projekcija iz kanonskog `ArtifactProvenance`; import ne dodjeljuje ovlast ni trust.

- **Salvage:** iz `/home/matej/NEXUSv2/core/worktree.py` Python→Go portirati NUL-safe change inventory, delete/rename handling, HEAD/filter-aware usporedbu kao migracijski test i first-writer-wins za same-path; zamijeniti fixed `.nexus-tmp` jedinstvenim tempom, process-local lock durable CAS-om te per-file copy petlju `PublishTxn` state-machineom. Iz `/home/matej/NEXUSv2/core/provenance.py` portirati fail-safe trust tier/taint lattice, unknown→untrusted i datamark samo za model-visible tekst. Ne predstavljati taj modul kao artifact attestor: novi atestor mora živjeti u nižem S6/GORTEX boundaryju i potpisati opažene efekte. `/home/matej/NEXUSv2/core/events.py` same-dir temp+replace i `/home/matej/NEXUSv2/core/export.py` staging korisni su uzorci, ali bez file+dir fsynca i recovery manifesta nisu dovoljni za ovaj ugovor.

- **Ugovor/RED:** veže Annex **P0.3** za jedini event/log/trace write-owner, **P1.2** za fence/recovery resursa i **P2.2** za disk/file budget; provenance veže S6.2 i S15.3. Obvezni RED-ovi: `test_same_base_two_publishers_exactly_one_head_cas_succeeds`; `test_one_path_conflict_publishes_nothing`; `test_dirty_user_bytes_are_never_overwritten_from_clean_head_assumption`; `test_crash_after_head_cas_recovers_full_materialization`; `test_stale_fencing_token_cannot_write_or_recover`; `test_external_write_between_prepare_and_replace_is_detected`; `test_runtime_self_attestation_is_rejected`; `test_tainted_input_cannot_be_upgraded_by_generated_provenance`; `test_symlink_swap_cannot_escape_workspace`; `test_file_and_directory_fsync_failure_never_reports_verified`.

- **Verifikacija:** provjereno 2026-09-01. Aiderova [Git dokumentacija](https://aider.chat/docs/git.html) stvarno štiti postojeće dirty datoteke zasebnim commitom prije AI edita; graftamo ownership namjeru, ne njegov obvezni commit mehanizam. Gitov [update-ref](https://git-scm.com/docs/git-update-ref.html) potvrđuje old-OID compare-and-swap i ref transakcije, [hash-object](https://git-scm.com/docs/git-hash-object) `--no-filters` raw-byte put, a [commit-tree](https://git-scm.com/docs/git-commit-tree.html) odvajanje tree/commit izgradnje od ref objave. [W3C PROV-O](https://www.w3.org/TR/prov-o/) potvrđuje interoperabilni provenance model, ali ne atestaciju ni sandbox trust. Nije verificirana upstream tvrdnja da Codex ili Cline nude portabilnu atomsku objavu proizvoljnog višedatotečnog host stabla; naš dizajn namjerno tvrdi samo canonical-head CAS + recoverable materialization.

- **Pareto floor:** **PASS za 5.3 uz concurrency, crash i attestor gate.** Zadržava Aiderovo poštovanje dirty rada, Codexovo odbijanje stale konteksta, Clineovu lineage vidljivost i NEXUSov taint/FWW, ali daje svakom mehanizmu jednog vlasnika i ne izmišlja nemoguću multi-file filesystem atomicity. Strongest counterexample je vanjski editor koji piše tijekom materijalizacije: head CAS štiti harness konkurenciju, ali ne može zaključati proizvoljan host proces; finalni rehash i quarantine čine utrku vidljivom, ne nužno spriječenom. Najslabija točka je semantika durabilityja na svakom Windows/macOS filesystemu; capability matrica mora dokazati rename/fsync ponašanje po OS+filesystemu ili označiti `DURABILITY_UNVERIFIED` i zabraniti high-risk publish.

## S5 acceptance gate

S5 je spreman tek kad isti black-box corpus prolazi nad pure-Go shadow/COPY_TREE putem i, gdje je instaliran, native-Git worktree/ref putem:

1. slab model može pokvariti više vrsta filesystem entryja, a step undo vraća točan tree bez promjene user HEAD-a;
2. dva procesa koja objavljuju iz istog basea ne mogu oba pobijediti, a crash u svakoj durable fazi završava punom verifikacijom ili karantenom, nikad lažnim uspjehom;
3. SIGKILL/restart P1.2 test na svakom podržanom OS-u čisti samo dokazano owned proces/worktree/lock i ne slijedi symlink/junction;
4. svaki S12 subagent radi u zasebnom rootu i jedini put do kanonskog workspacea je 5.3 publisher;
5. provenance negativna mutacija (self-sign, write-set swap ili trust upgrade) pretvara cijeli publish u RED.

**Proof ceiling:** plan specificira sučelja, ownership, state-machine i red-capable gateove; ne dokazuje implementaciju, performanse velikih repoa, stvarnu sandbox atestaciju ni jednaku durability semantiku svih filesystema. Te tvrdnje postaju `PROVEN` tek nakon Go implementacije, fault injectiona, multi-process testova i Windows/Linux/macOS matrice na imenovanoj reviziji.
