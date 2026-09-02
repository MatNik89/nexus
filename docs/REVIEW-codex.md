REVIEW S0-S6 — CHANGES REQUIRED

## Blokirajuće rupe

1. **GLOBALNO (redci 3, 11, 18, 27-31, 65-72, 784, 788) — plan još normativno počiva na mrtvom
   repozitoriju.** `100/~100 podsekcija`, `60 modula`, `Salvage`, `port`, `reuse` i GORTEX tvrdnje
   nisu samo komentari: koriste se kao dokaz da je dizajn buildable i Pareto-complete. Stvarni broj je
   82 podsekcije. **Treba:** ukloniti sve takve retke iz normativnog plana; svakoj podsekciji dodijeliti
   samo `(a) full-code-auditani javni izvor po aspektu` ili `(b) potpuni Go ugovor u planu`. Podsekcija
   bez jednog od ta dva dobiva `UNRESOLVED`, ne `ZAOKRUŽEN`.

2. **GLOBALNO/S3-S5 — zaključci se međusobno pobijaju.** S3.4 tvrdi “NITKO nema” stuck detection
   (redci 264-270), S4.3 edit-ladder/symedit grupira kao diferencijator (304-311), a S5.1 shadow-git
   kao diferencijator (344-348); Addendum A8 poslije priznaje da su stuck, edit-ladder i shadow-git
   pronađeni kod drugih harnessa (1073-1074). **Treba:** osnovni tekst mora biti kanonski ispravljen;
   kasniji addendum ne smije ostaviti suprotnu tvrdnju aktivnom. Ponoviti Pareto presudu bez mrtvog
   salvagea i bez novelty bodova.

3. **6.2 — sandbox je sada najveći blocker i nije realno specificiran.** Cijela podsekcija
   (386-393) pretpostavlja gotovu GORTEX membranu. Nakon njenog uklanjanja ostaje samo popis API-ja.
   Tri OS backenda nisu sigurnosno ekvivalentna:

   - Linux pure-Go je izvediv, ali nije “tri syscalla”. Potreban je self-reexec helper koji prije
     `execve` radi ABI probe, `no_new_privs`, Landlock ruleset i arhitekturalno testirani seccomp-BPF;
     zatim se nepovratno zamijeni ciljnom binarkom. Kernel ugovor Landlock primjenjuje na calling
     thread i djecu te zahtijeva `no_new_privs` bez privilegije; seccomp filter također zahtijeva
     `no_new_privs`/`CAP_SYS_ADMIN` ([Landlock](https://cdn.kernel.org/doc/html/latest/userspace-api/landlock.html),
     [seccomp](https://www.kernel.org/doc/html/latest/userspace-api/seccomp_filter.html)). Trebaju ABI/
     kernel fallback matrica, syscall policy po arhitekturi, FD/env sanitizacija i dokaz da Go helper
     nema unsandboxed pre-exec prozor.
   - Windows Job Object samo grupira/limitira procese; nije filesystem/network sandbox
     ([Microsoft Job Objects](https://learn.microsoft.com/en-us/windows/win32/procthread/job-objects)).
     Restricted token + `CreateProcessAsUser` traži zaseban token/ACL/environment/desktop dizajn;
     AppContainer je pravi isolation floor. Novi composable sandbox API postoji, ali je eksplicitno
     **experimental**, pa ne smije biti jedini production backend
     ([Microsoft sandbox API](https://learn.microsoft.com/en-us/windows/win32/secauthz/createprocessinsandbox)).
   - macOS `killpg` je lifecycle, ne sandbox. Plan nema dokazani stabilni javni API za dinamički
     Seatbelt profil nad proizvoljnim CLI procesom. `sandbox-exec` adapter bez support/availability
     ugovora ne zadovoljava multiplatform fail-closed tvrdnju.

   **Treba prije S4/S6 koda:** `SandboxBackend.Probe/Compile/Launch/Attest`, eksplicitna capability
   matrica `FS_RO/FS_RW/NET_DENY/SYSCALL/PROC_TREE`, jedan hostile conformance suite i produktna
   odluka: Linux full; Windows/macOS ili dokazani backend ili `UNAVAILABLE` uz blokiran high-risk exec.
   Ne smije postojati “weaker sandbox” tihi fallback. `bwrap` može biti opcionalni vanjski adapter,
   ne dio single-binary garancije.

4. **6.0 + 6.9 — nema kanonskog PEP/approval ugovora.** “OPA-stil” i `MiddlewareChain` (378-380,
   417-422) ne definiraju subjekt, resurs, efekt, resolved executable, cwd/env, input digest, policy/
   config/capability generation, expiry ni single-use status. Time approval ostaje TOCTOU-ranjiv i
   nije dokazano da svaki sink prolazi isti PEP. **Treba:** typed `EffectRequest`, `PolicyDecision` i
   `ExecutionTicket` koji hashom veže exact intent; executor neposredno prije učinka ponovno provjeri
   ticket i current generations. PEP mora biti obvezni konstruktor dependency svakog tool/network/
   file/delivery sinka, ne opcionalni middleware hook.

5. **6.1 — shlex/regex klasifikacija nije sigurnosna granica.** Redci 382-384 pretpostavljaju POSIX
   shell semantiku, ne pokrivaju PowerShell/cmd, redirekcije, subshell, newline, alias/wrapper,
   promijenjeni cwd/PATH ni executable zamjenu nakon approvala. `RED-regex→deny` dokazuje samo regex.
   **Treba:** structured `ExecRequest{absolute_exe,argv,cwd,env_delta,stdin_ref,effect}` kao default;
   path + file identity pin; shell kao zaseban visoko-rizični alat s OS-specifičnim parserom ili
   obveznim human gateom. Regex smije biti advisory signal, nikad autorizacija.

6. **6.3 — egress kontrola je nepotpuna i ovisi o mrtvom netjailu.** String-normalizacija metadata
   IP-a prije diala (395-398) ne zatvara DNS rebinding, višestruke A/AAAA rezultate, IPv6 zone,
   redirect, proxy env, localhost/private/link-local/multicast ni novi socket iz child procesa.
   `bwrap+UDS proxy` nije dostupna multiplatform/single-binary osnova. **Treba:** jedan policy-aware
   resolver+dialer koji validira i pinna svaki resolved IP neposredno prije connecta, ponavlja provjeru
   na svakom redirectu, čisti proxy env i izdaje egress receipt; child-network enforcement mora biti
   eksplicitna capability sandbox backenda. Bez nje `NET_DENY` mora biti `UNAVAILABLE`, ne deklariran.

7. **6.4 — “keyring + entropy/shape redaction” nije dizajn tajnog brokera.** Nema odabranih pure-Go
   OS credential backenda, lifecyclea handlea, rotacije, scopea ni pravila za CLI/stdin/env/file
   injection. Redakcija ne sprječava tool/model da tajnu pošalje mrežom i ne hvata pouzdano encoded/
   transformirane vrijednosti. **Treba:** opaque `SecretRef`, scoped short-lived materialization samo
   u sandbox helperu, sink allowlist, zeroization best-effort, OS backend matrica i “secret nikad u
   Envelope/journal” invariant. Redactor je zadnja obrana, ne broker.

8. **6.5 — LLM judge, deny-list i canary nisu PEP.** Redci 403-407 mogu otkriti dio curenja nakon što
   je model već kompromitiran, ali ne sprječavaju tool misuse; judge je također napadljiv model.
   **Treba:** structural instruction/data separation iz S0/11, untrusted lineage koji se ne može
   promovirati, tool authorization neovisan o modelu i output sink gate. Canary ostaje detektor s
   mjerljivim false-positive/false-negative testom; guardian smije samo pooštriti odluku.

9. **6.6 — service AuthN/AuthZ/tenant je samo naslov.** Allowlist, PKCE i “RBAC” (409-412) ne daju
   issuer/audience, session/token rotation, CSRF, device binding, service-to-service identitet,
   per-object ownership ni tenant propagation kroz queue/cache/journal/artifact. **Treba:** zatvoreni
   principal/tenant/session/token model, authorization matrica po resursu, deny-by-default query
   filter i cross-tenant RED suite. `service` se ne smije aktivirati prije toga.

10. **6.7 — baseline governance nije Go/single-binary buildable.** Presidio je Python servis, a
    “vlastiti SQLite purge” (410-412) ne definira WAL, backup, cache, export, derived embedding/KG,
    legal hold ni kriptografsko brisanje. **Treba:** odabrati sidecar kao svjesnu distribucijsku
    iznimku ili Go PII adapter; napisati data-inventory i retention/purge state machine koja obuhvaća
    sve projekcije i backupe. `MEMORY_FORGET` ne smije glumiti regulatorni purge.

11. **6.8 — supply-chain je lista CLI proizvoda, ne verifier.** Sigstore/cosign, OSV i in-toto
    (414-415) nemaju definirani artifact manifest, trust root, signature/transparency/offline policy,
    revocation, dependency lock ni atomic install/rollback. **Treba:** jedan typed `ArtifactManifest`
    i `VerificationRecord`; odlučiti Go library ili eksplicitni subprocess adapter za svaki korak;
    6.8 mora verificirati isti digest koji 11.5/17.1 kasnije učitavaju.

12. **4.2 — exec ostaje bez implementacijskog izvora i proturječi cross-platform cilju.** “bash
    sesija + rlimits + tree-kill” (299-302) je POSIX dizajn; Windows nema garantirani bash, a rlimits,
    Job Object i sandbox su različiti owneri. **Treba:** structured one-shot exec kao v1; opcionalna
    per-OS shell session iza capability probea; S1.2 jedini process-tree owner, S6.2 containment owner,
    S7 deadline/cancel owner. Napisati Go `Executor/ProcessHandle/ExitObservation` ugovor i Windows
    suspended-create→assign-job→resume sekvencu koja zatvara child-before-job race.

13. **4.3 — edit formati imaju auditane uzore, ali ladder i AST/LSP symedit nemaju Go mehanizam.**
    Aider može biti izvor za formate/selection; mrtvi `editapply/editrepair/symedit/diff_engine` više
    ne pokrivaju exact/fuzzy ambiguity, offset/encoding, newline, binary, rename ni language server
    transakcije (304-311). **Treba:** definirati svaki `EditOp`, preimage hash, candidate scoring i
    refuse threshold; ograničiti v1 symedit na imenovane jezike s auditiranim parser/LSP adapterom.
    AST nije univerzalni zadnji korak za proizvoljni text edit.

14. **4.4 — “dev-inteligencija” je rupa nakon uklanjanja NEXUS modula.** Ripgrep/ast-grep/Serena
    pokrivaju text/AST/LSP, ali plan nema izvor ni Go dizajn za codeindex, callgraph i archmap
    (313-318). **Treba:** v1 završiti na grep+AST+LSP s typed `Hit`/freshness contractom; callgraph i
    architecture graph vratiti tek s odabranim full-code izvorom, schema dizajnom i benchmarkom.

15. **4.6 — MCP klijent nema Go transport/state-machine dizajn nakon mrtvog reusea.** Auth zahtjevi
    su dobri, ali nema initialize/version negotiation, framing, session lifecycle, cancellation,
    schema bounds ni credential binding (324-327). **Treba:** auditirati službeni Go SDK ili napisati
    minimalni stdio/HTTPS client ugovor; P1.3 RED mora prolaziti kroz stvarni transport, ne mock samo.

16. **3.1 + 16.6 veza — evidence-graded loop ostaje slogan bez salvagea.** `Checker.Grade` nema input
    shemu, evidence provenance, task-specific acceptance contract ni odgovor za necoding zadatak
    (246-254). Git diff + exit code nisu dokaz slanja poruke, kalendarske akcije ili istraživanja.
    **Treba:** generic `AcceptanceContract` i typed `EvidenceBundle`; coding checker je jedna strategija,
    ne kernel completion definicija. Worker principal ne smije biti checker principal.

17. **3.3 — RED “cancel usred tool-poziva→nikakav side-effect” je neostvariv.** Cancel ne može vratiti
    već izvršen vanjski efekt (260-262). **Treba:** effect taxonomy; prije commita cancellation može
    spriječiti učinak, poslije mogućeg commita stanje mora biti `UNKNOWN_EFFECT/RECONCILING` ili
    verificirana kompenzacija. RED mora forsirati crash/cancel na obje strane commit granice.

18. **3.4 — stuck detection nema algoritam nakon mrtvog `circuit.py`.** “isti tool/arg N puta” i
    “verify nije zelen M puta” (264-270) nemaju canonical arg hash, progress signal, pragove, reset ni
    razlikovanje legitimnog pollinga. **Treba:** typed attempt fingerprint, monotoni evidence/progress
    metric, bounded policy i false-positive suite; kao izvore koristiti full-code auditane
    Cline/Codex/Roo/Kilo obrasce koje Addendum već priznaje.

19. **3.5 — TIA je stvarna greenfield rupa.** Nakon uklanjanja `tia.py/testcmd.py/verifier.py` nema ni
    javnog izvora ni Go dizajna za coverage ingest, diff→symbol mapping, dynamic tests, generated code,
    renames, stale graph ili monorepo (272-278). **Treba:** zaseban TIA design prije implementacije:
    coverage adapteri po jeziku, dependency graph schema, conservative invalidation i obvezni full-suite
    fallback. “Samo pogođeni testovi” ne smije biti completion gate dok paired full-suite mjerenje ne
    dokaže nula promašenih regresija.

20. **2.3 — `Validate→re-ask→salvage` proturječi S7 sole-retry owneru.** Re-ask je novi fizički model
    pokušaj, ali 2.3 nema `AttemptGrant`; tolerantno popravljanje trailing comma može promijeniti
    značenje nekanonskog outputa (210-214). **Treba:** lokalni deterministic parser/repair ne troši
    attempt samo ako je semantički lossless i auditiran; svaki model re-ask ide preko S7. Za security/
    effect payload ne postoji tolerantni accept.

21. **2.1/2.2 — provider/auth/fallback ABI je preslab bez prethodne implementacije.** Statički CLI
    primjeri i `--tools ""` (189-202) nisu version-negotiated contract; OAuth refresh concurrency,
    token storage, stream errors, usage accounting i provider-specific tool disable nisu definirani.
    KeyPool/cooldown u 2.2 više nema izvor. **Treba:** po-provider capability probe i adapter conformance
    suite; atomic token refresh; normalized error taxonomy; fallback plan bez vlastitog retrya.
    `SubscriptionOAuthAuth` izbaciti iz v1: velik ToS/ban rizik, a ne doprinosi core petlji.

22. **2.6 — learned router je preuranjen scope.** Bez verificiranog credit ledgera, dovoljno podataka i
    baseline evaluacije epsilon-greedy samo širi state/failure surface (227-231). **Treba:** v1 samo
    deterministički capability+cost+policy router; learned strategy aktivirati tek nakon offline replay
    dokaza da nadmašuje baseline bez kršenja floorova.

23. **5.1 — shadow-git nije specificiran nakon mrtvog porta.** “Byte-identičan rollback” (344-348)
    ne kaže obuhvaća li untracked/ignored/symlink/submodule, permission/xattr/ACL, sparse checkout,
    velike binarke ni konkurentni user write. Git sam ne čuva sve filesystem metapodatke. **Treba:**
    definirati snapshot scope i dokazni strop; content rollback s preimage hashom + CAS, a metadata
    restore samo za eksplicitno podržana polja. Snapshot prije svakog *filesystem* efekta, ne svakog
    vanjskog side-effecta.

24. **5.2 — worktree nije izolacijska granica.** Svi worktreeovi dijele Git administrativne podatke;
    sandbox s pristupom širem `.git` može utjecati na druge taskove (350-355). **Treba:** minimalni
    filesystem allowlist po worktreeu, zabrana direktnog `.git` pisanja alatu, supervisor-owned Git
    operacije i hash/CAS sync-back. Container je opcionalni S6 backend, ne dio ovog mehanizma.

25. **5.3 — atomic/provenance tvrdnja je preširoka, a atestor nema izvor.** `tmp→fsync→rename` ne
    navodi parent-directory fsync, same-filesystem uvjet ni Windows replace/share semantics; W3C
    PROV-O i “sandbox-neutralni atestor” nemaju buildable Go dizajn (357-364). **Treba:** per-OS
    atomic writer contract + crash matrix; concurrency preko expected preimage hash/CAS. Minimalni
    signed `ExecutionReceipt{request_hash,backend,policy_hash,exit,fs_delta,net_receipts}` prije PROV-O
    exporta. PROV-O odgoditi dok postoji stvarni potrošač.

26. **0.3 + 0.5 — S0 je označen zatvorenim uz otvoren ugovor i nemoguću “atomsku aktivaciju”.** P0.5
    je tek akcija (102-108), a 0.5 tvrdi atomski aktivni set iako gateovi mogu imati OS/external side
    effecte (117-124). **Treba:** napisati P0.5 RED prije koda; closure resolver prvo proizvodi čisti
    immutable `ActivationPlan`, zatim svaki gate samo atestira isti hash, a jedan journal commit
    objavljuje generaciju. Adapter start/stop nije atomski i mora imati `PREPARING/FAILED/ROLLING_BACK`.

27. **1.3 — svaki debug zapis kroz durable journal je mogući self-DoS.** Plan priznaje throughput
    problem, ali ga ostavlja kao kasniji benchmark (161-174). **Treba:** prije journal implementacije
    razdvojiti kanonske state/security/audit evente od best-effort debug telemetry; samo prvi moraju biti
    durable. Backpressure/drop policy mora biti izvršna i ne smije blokirati safety evente.

## Scope koji treba rezati iz v1

- S2.6 learned bandit; S2.1 replicirani subscription OAuth.
- S4.4 vlastiti semantic/code/callgraph/archmap sloj iznad grep+AST+LSP.
- S5.3 PROV-O export prije minimalnog execution receipta.
- S6.5 guardian LLM kao security komponenta; zadržati ga samo kao opcionalni detektor.
- Jedinstvena tvrdnja “isti sandbox na sva tri OS-a”. Proizvod treba capability-equivalentni floor ili
  eksplicitno blokiranje, ne marketinšku simetriju.

## Mora biti riješeno prije prvog produkcijskog koda

1. Mehanički očistiti plan od svih NEXUSv2/GORTEX/salvage/port/reuse oslonaca, ispraviti 82 i ponovno
   označiti `RESOLVED/UNRESOLVED` po podsekciji.
2. Zaključati S0.1/0.2/0.3/0.5 kanonske Go tipove i RED ugovore, uključujući P0.5 i activation failure
   stateove. Tek tada je dopušten kernel scaffold.
3. Zaključati jedan end-to-end effect put `S3→S4→S6.0/6.9→S1.2→S7` s jednim ownerom za policy,
   process lifecycle i retry; ukloniti nemogući cancel/no-side-effect zahtjev.
4. Napraviti izolirane, neprodukcijske sandbox feasibility probeove za Linux, Windows i macOS te iz
   rezultata donijeti eksplicitnu platform capability/fail-closed odluku. Bez toga S6.2 i arbitrary
   exec ne smiju u implementaciju.
5. Napisati vlastite Go dizajne i RED matrice za pet rupa bez izvora: evidence checker, TIA,
   editrepair/symedit, shadow checkpoint i execution attestation. Ako to ne stane u jednu jasnu
   iteraciju po mehanizmu, smanjiti v1 scope; ne zamijeniti dizajn novim popisom projekata.

**Zaključak:** S0/S1 imaju dovoljno materijala za greenfield nakon korekcije ugovora. S2 je djelomično
buildable. S3-S5 imaju pet ključnih mehanizama koji su prije bili samo NEXUS salvage. S6 nije spreman:
6.2, 6.3, 6.4, 6.6 i 6.7 nemaju vjerodostojan cross-platform Go implementation floor. Kodiranje
sigurnosno-osjetljivih toolova prije rješavanja tih granica bilo bi rewrite bez specifikacije.
