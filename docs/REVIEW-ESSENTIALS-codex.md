# Adversarijalni review: ARCHITECTURE-ESSENTIALS

Opseg: fidelity prema šest zadanih izvora, selekcija kritičnih odluka i proturječja. Review je rađen solo i anti-anchored: izvori su pročitani i top-10 je skiciran prije otvaranja `ARCHITECTURE-ESSENTIALS.md`.

## Neovisni top-10 iz izvora (prije čitanja Essentialsa)

1. P0 je uska end-to-end cjelina: razgovor, restart-persistentna memorija, trajne obveze/podsjetnici, Telegram, izolirani profili i dokazani Linux sandbox.
2. Greenfield Go jezgra je `CGO_ENABLED=0` single-binary sa SQLite-WAL spineom; ne-Go komponente su adapter/subprocess, pure-Go zamjena ili descope.
3. Dependency redoslijed je obvezan: K0 tipovi/journal/minimalni checker, zatim K1 sigurnost+pouzdanost+workspace, pa tek izvršni put.
4. Ručno pisani tipizirani ugovori su izvor istine; `EventJournal` je jedini writer kanonskog event/state toka, a ostalo su projekcije.
5. Jedan effect-path drži odvojene ownere: S6.0 policy, S6.9 lifecycle-order, S6.2 sandbox, S1.2 process identity i S7 retry/cancel/fencing.
6. Sandbox je fail-closed i ne postaje sigurnosna granica prije hostile live-probea; nema tihog slabijeg fallbacka.
7. Capability aktivacija je transakcijska: immutable `ActivationPlan`/`PlanHash`, Prepare/Commit/Activate, `RollbackToken` i eksplicitna failure stanja.
8. Trajnost ima odvojene domene: journal state, transactional delivery, profile isolation, obligations i FORGET nasuprot PURGE.
9. Completion ocjenjuje checker odvojen od workera; minimalni checker je rani temelj, a TIA mora imati full-suite fallback.
10. Ekstenzije/role/skills su podaci iza provjere, a UI/kanali su tanki adapteri nad istim application/effect putem.

## Nalazi

1. **[CONTRADICTION] Status “zaključano / 0 blokera” nije istinit prema zadanom source-setu ni prema samom Essentialsu.** Essentials:3-4 tvrdi “zaključane arhitekture” i “0 blokera”, ali Essentials:123-125 odmah navodi otvorene preduvjete. `DESIGN-STATUS.md:10` kaže: “DESIGN RESOLVED / probe OPEN”; `DESIGN-STATUS.md:13` kaže: “OPEN — TI VODIŠ”; `DESIGN-STATUS.md:37-39` obje stavke vodi kao “PREOSTAJE PRIJE KODA”. To nije semantička nijansa: cheat-sheet koji se koristi pri kodiranju pogrešno signalizira da je go/no-go već dobiven. Zamjena: “destilat trenutačnog dizajna; otvoreni preduvjeti navedeni dolje”, bez “0 blokera”.

2. **[FIDELITY] E2 pretvara još nepotvrđenu product odluku u zaključanu arhitekturnu činjenicu.** E2:15-19 bez rezerve zaključava “asistent PRVO” i dva profila. `PRD.md:64-66` izričito kaže: “Ovaj PRD čeka tvoju potvrdu/korekciju”, a `DESIGN-STATUS.md:13` kaže da identitet i P0 scope “čeka tvoju presudu”. `HARNESS-PLAN.md:42-49` podržava prijedlog, ali ne može poništiti dva eksplicitna statusa bez navedenog precedence pravila. E2 mora biti označen `PENDING PRODUCT APPROVAL` ili source-ledger mora prvo zatvoriti #6.

3. **[FIDELITY] E3 je prejak jer ne ograničava “jedini trajni write-owner” na kanonski NEXUS event/state tok.** `HARNESS-PLAN.md:91-93` veže `EventJournal.Append` uz state/log/trace/transcript/audit projekcije, ali `HARNESS-PLAN.md:340-346` zasebno zahtijeva `AtomicWriter` za korisničke datoteke, a `HARNESS-PLAN.md:487-489` ima memory store i ireverzibilni purge. Doslovni E3:21-25 (“jedini piše trajno stanje”) čini legitimne workspace, backup i purge writeove arhitekturnim prekršajem. Preciznije: EventJournal je jedini writer **kanonskih domenskih događaja i iz njih izvedenog stanja**; drugi persistentni side-effecti ostaju iza svojih ownera i effect-patha.

4. **[MISSING] Nedostaje dependency/build-order odluka, iako je nužna P0 programeru.** `SECTION-MAP.md:18-35` nalaže K0 primitive i minimalni checker prije K1 sigurnosti/workspacea, a S4 izvršni put tek poslije njih; `SECTION-MAP.md:27-30` stavlja S6 sandbox prije S4. Bez toga se 15 lokalno točnih pravila može implementirati u pogrešnom redoslijedu i stvoriti bypass ili preuranjeni executor. Ovo je kritičnije od pune E12 auth taksonomije i zaslužuje vlastitu Essential odluku.

5. **[MISSING] Izostavljena je riješena transakcijska capability-aktivacija i rollback.** E8 spominje samo unknown-capability `Resolve`, ali ne životni ciklus aktivacije. `DESIGN-STATUS.md:9` propisuje “ActivationPlan(immutable+PlanHash)+Prepare/Commit/Activate+RollbackToken+PREPARING/FAILED/ROLLING_BACK”; `PLAN-HOLES-CONSOLIDATED.md:99-100` objašnjava zašto je stara “atomska aktivacija” nemoguća uz OS/vanjske efekte. P0 developer bez ove odluke lako implementira parcijalno ACTIVE stanje koje je dizajn već odbacio.

6. **[FIDELITY] E8 preslabo destilira egress granicu i može proizvesti lažno sigurnu implementaciju.** E8:64-65 svodi konkretan egress primjer na blokiranje metadata-IP kodiranja prije diala. `PLAN-HOLES-CONSOLIDATED.md:90-93` kaže da string-normalizacija ne zatvara DNS rebinding, multi-A, redirect, proxy-env ni child-socket te zahtijeva policy-aware resolver+dialer koji pinna svaki IP i izdaje egress receipt. Za dnevni P0 cheat-sheet preskakanje enforcement točke je materijalno: dodati resolver/dialer pinning, redirect re-check, proxy-env sanitizaciju i child-egress containment.

7. **[CONTRADICTION] E14 jezično vraća retry ownership gatewayu, suprotno E4/S7.** E14:101-104 stavlja “idempotent-retry” u Gateway jezgru, dok E4:32-33 i `HARNESS-PLAN.md:407-418` kažu da je S7 jedini owner retry/cancela i da svaki fizički pokušaj treba `AttemptGrant`. Izvor za kanale (`HARNESS-PLAN.md:660-663`) traži idempotent-retry kao ugovorno svojstvo, ne drugog scheduler-ownera. Zamjena: “gateway pruža idempotency key/receipt; S7 jedini odobrava i raspoređuje delivery retry”.

8. **[FIDELITY] E15 pogrešno klasificira Ed25519 kao keyed-MAC i proširuje pravilo na obični skill hash.** E15:109-113 kaže “Keyed-MAC = HMAC … ili `ed25519.Sign`”; Ed25519 je digitalni potpis, ne MAC. `DESIGN-STATUS.md:31-35` razdvaja “HMAC lokalno + ed25519 vanjski-anchor”. Nadalje, `HARNESS-PLAN.md:561-565` za skill-lock propisuje runtime hash-on-load, ne tvrdi da je taj hash keyed-MAC/potpis. Razdvojiti tri primitive: HMAC za lokalnu simetričnu autentikaciju, Ed25519 za asimetrični potpis/anchor i obični pinned hash za integritet on-load.

9. **[FIDELITY] P0 rez krivo izjednačava “nije u P0” s “izbačeno iz v1”.** Essentials:117-121 kaže da su learned router, council/flows/boards, GraphRAG/CAG, fleet/pairing, video, computer-use, voice i K8s “Izbačeno iz v1”. `PLAN-HOLES-CONSOLIDATED.md:64-69` preporučuje rez **za P0**, dok `PRD.md:32-37` eksplicitno drži P1 i P2 sposobnosti “poslije/kasnije”; stvarni “Izvan scopea (NE radi ovo u v1)” popis je uži u `PRD.md:39-44`. Zamjena: “izvan P0 / odgođeno u P1/P2 ili čeka PRD odluku”, a “izvan v1” koristiti samo za PRD §5 stavke.

10. **[CONTRADICTION] Rečenica “coding-USP trojka … = P1” skriva da je minimalni evidence checker K0 preduvjet i P0 obligation dependency.** `SECTION-MAP.md:23-25` stavlja `S16.6-det` u K0; `HARNESS-PLAN.md:728-734` kaže “minimalni checker=jezgra”; `HARNESS-PLAN.md:502-504` dopušta `ObligationStore.MarkDone` samo uz Evidence. TIA i puni coding symedit mogu biti P1, ali minimalni generic `AcceptanceContract`/checker ne može zajedno s njima biti odgođen bez lomljenja P0 obveza i build DAG-a. Scope podsjetnik mora eksplicitno razdvojiti K0/P0 minimalni checker od punog P1 coding paketa.

11. **[MISSING] P0 scope navodi šest imenica, ali izostavlja njihove kritične acceptance granice.** `PRD.md:46-53` zahtijeva da memorija preživi restart, podsjetnik preživi gašenje i javi se na vrijeme, Telegram nepovratnu akciju prethodno odobri, profil ima nula curenja, a sandbox hostile test odbije `/etc/shadow` i mrežu. To su operativne arhitekturne odluke, ne samo test detalji; bez njih “spine-pamćenje”, “obveze” i “Linux sandbox” mogu postati false-green. Dodati kompaktan P0 done-contract ili izravnu obveznu referencu na svih šest PRD kriterija.

12. **[NOT-CRITICAL] E12 kao cijela top-15 odluka previše prostora daje četirima auth modovima, osobito odgođenom SubscriptionOAuthu.** Za P0 je kritičan provider interface, capability floor i zabrana adapter self-retrya; puni OAuth katalog nije. `PLAN-HOLES-CONSOLIDATED.md:64-69` izričito predlaže rez subscription OAutha iz P0, a `PRD.md:24-31` traži samo end-to-end razgovor s AI modelom. Zadržati sažetak APIKey + provider interface/no-self-retry; detaljne auth modove vratiti u S2, a oslobođeno Essential mjesto dati build-orderu ili ActivationPlanu.

13. **[FIDELITY] E7 naziva live-probe “prvim implement-zadatkom”, iako izvori ga klasificiraju kao preduvjet prije produkcijskog koda.** `PLAN-HOLES-CONSOLIDATED.md:104-113` doslovno kaže “MORA BITI RIJEŠENO PRIJE PRVOG PRODUKCIJSKOG KODA” i traži izolirani neprodukcijski feasibility-probe; `DESIGN-STATUS.md:37-39` ga vodi kao preostali hardverski/proof posao. To nije implementacija P0 proizvoda. Zamjena: “go/no-go preflight prije produkcijskog scaffolda/exec koda”.

14. **[CONTRADICTION] Source precedence je nedefiniran, pa pravilo “puni izvor uvijek pobjeđuje” nije izvršivo.** `PLAN-HOLES-CONSOLIDATED.md:29-36` tvrdi da P0.5/P0.6/P0.7/P0.8/P0.9/P0.11/P0.13 ne postoje; `SECTION-MAP.md:93` kasnije tvrdi “P0.5-0.13 … dodani”, a `DESIGN-STATUS.md:9` vodi P0.5 kao RESOLVED. Essentials:123-125 ponovno tretira Annex ugovore kao otvorene, bez objašnjenja je li to potvrđena regresija ili stari nalaz. Potreban je eksplicitan precedence/dated status ledger; inače developer ne može znati treba li stati ili implementirati.

15. **[FIDELITY] E1 pojačava “single-binary” u nedokazanu tvrdnju “jedan statički binary”.** `HARNESS-PLAN.md:13-16` obećava `CGO_ENABLED=0` single-binary i pure-Go SQLite spine; `HARNESS-PLAN.md:750-751` ponavlja samo “Go-single-binary”. Nijedan zadani izvor ne propisuje linker mode niti test da artefakt nema dinamičke ovisnosti. `CGO_ENABLED=0` smanjuje taj rizik, ali samo po sebi nije acceptance dokaz za svaku platformu/build mode. Ili ukloniti “statički”, ili dodati eksplicitni build flag i CI provjeru (`file`/`ldd` ili platform-ekvivalent) kao izvorni ugovor.

## Pokrivenost i presuda

E4-E6, E9-E11 te jezgra E13 uglavnom vjerno prenose najnovije konkretne odluke. To ne spašava dokument kao coding cheat-sheet: status, P0 granica, activation lifecycle, build-order i dvije sigurnosne granice mogu izravno usmjeriti implementaciju u pogrešnom smjeru.

VERDICT: FAIL
