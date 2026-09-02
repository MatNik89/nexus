# REVIEW4 — Codex red-team verifikacija

Revizija: `450974312635301993041101f8e88a4a6f1241a0` (`4509743`). Provjera je rađena nad sadržajem dokumenata, ne nad R2 tvrdnjama o zatvaranju.

## R3 nalazi

**V-K1 [NIJE]** `RunTool` sada grana po `call.ExecutionKind`, ali kanonski `ToolCall` nema to polje (`/home/matej/HARNESS/nexus/docs/DESIGN-S0-sandbox-codex.md:209`), a plan i dalje definira `ToolSpec{ID,SchemaHash,EffectClass}` bez `ExecutionKind` (`/home/matej/HARNESS/nexus/docs/HARNESS-PLAN.md:285`). Skica se ne može kompajlirati niti dokazuje authoritative registry→resolved-call vezu; `sbproc` je i dalje nezaštićen `ToolExecutor` interface.

**N3-C01 [DJELOMIČNO]** ASK sada zahtijeva `ApprovedExact`, ali navodno totalni switch nema `default`; nepoznata/zero enum vrijednost pada kroz do izvršenja (`/home/matej/HARNESS/nexus/docs/DESIGN-FIXES-r2.md:31`). To krši planov `Valid()` + default-reject ugovor (`/home/matej/HARNESS/nexus/docs/HARNESS-PLAN.md:96`).

**N3-C02 [DJELOMIČNO]** Execute-error više ne zove `AfterTool` i output je prazan. Međutim `AfterTool` veto ide izravno u `S7.Record`, bez `S6.9.OnError`, iako isti fix izričito tvrdi veto→OnError (`/home/matej/HARNESS/nexus/docs/DESIGN-FIXES-r2.md:44-54`). Lifecycle/audit grana nije zatvorena.

**V-K3 [DJELOMIČNO]** `PlacementGrant` je ispravno izvan `fleet/ids`, a `ids` je leaf. No zamjenski Go potpis nije dan: tvrdnja da `pairing` prima `fleet.PlacementGrant` “kao argument (ne import-tip)” nije izvediva u Go-u bez importa ili neutralnog contract tipa (`/home/matej/HARNESS/nexus/docs/DESIGN-FIXES-r2.md:73-80`). Layout još nije buildable dokaz `TestNoImportCycle`.

**V-K4 [DJELOMIČNO]** Tekst S1.2 više ne ovisi o S7 i deterministic 16.6 je stavljen prije S3. Ipak `S8.1-min`, `S11.1-min` i `S16.6-det` nemaju izdvojene kanonske ugovore, a DAG “po ID-u” još ne smješta eksplicitno 6.3–6.8, 8.1-puni te 9.1/9.3 (`/home/matej/HARNESS/nexus/docs/SECTION-MAP.md:14-47`). Granice koje prekidaju cikluse nisu potpuno specificirane.

**V-C10 [ZATVOREN]** Plan sada na 3.1, 4.8 i addendum putovima vodi policy kroz `S6.0.Decide`, a 6.9 ostavlja lifecycle-orderu (`/home/matej/HARNESS/nexus/docs/HARNESS-PLAN.md:241`, `:317`, `:924`, `:1153`).

**V-C11 [DJELOMIČNO]** Kanonski 2.2 sada kaže AccountFleet classify-only i S7 sole-owner (`/home/matej/HARNESS/nexus/docs/HARNESS-PLAN.md:202-205`), ali isti plan i dalje tri puta obećava account-failover (`:1048`, `:1116`, `:1163`), a `/home/matej/HARNESS/nexus/docs/GAPFIX-agy.md:166-345` i dalje daje automatsku rotaciju/failover bez SUPERSEDED bannera. Dvostruko vlasništvo ostaje.

**V-D2 [ZATVOREN]** Journal je i dalje dosljedno `P0.3/EventJournal`, odvojen od S0.3 capability negotiationa (`/home/matej/HARNESS/nexus/docs/SECTION-MAP.md:18-20`, `:57-59`).

**N3-C03 [ZATVOREN]** Mapa sada navodi 108 podsekcija i 8 novih GAPFIX podsekcija, dosljedno vlastitom manifestu (`/home/matej/HARNESS/nexus/docs/SECTION-MAP.md:83-87`, `:112`).

**N3-C04 [DJELOMIČNO]** Svih sedam ugovora postoji u Annexu i dodano je u Wiring (`/home/matej/HARNESS/nexus/docs/HARNESS-SPEC.md:1352-1358`, `:1539-1572`). Plan ipak još nalaže “dodati P0.5/P0.6”, naziva P0.7/P0.8 matrix-prep kandidatima i P0.11 kandidatom (`/home/matej/HARNESS/nexus/docs/HARNESS-PLAN.md:110`, `:129`, `:175`, `:228-229`, `:353`). Build instrukcija je još kontradiktorna.

## Novi blokeri S0–S9

**N4-C01 [NOVI KRITIČNI]** Nepoznat `PEP.Decide` rezultat izvršava alat jer switch nema `default`. Dodati eksplicitni default-deny/error i RED za zero/invalid decision (`/home/matej/HARNESS/nexus/docs/DESIGN-FIXES-r2.md:31-36`).

**N4-C02 [NOVI KRITIČNI]** Čak i nakon dodavanja `ExecutionKind`, kod bira sandbox samo za točno `ExecProcess`; zero/nepoznat kind automatski bira `inproc`. Mora biti totalni switch `ExecInProcess|ExecProcess|default reject`, uz validaciju registry pečata (`/home/matej/HARNESS/nexus/docs/DESIGN-FIXES-r2.md:39-42`).

**N4-C03 [NOVI KRITIČNI]** Post-exec veto šalje se S7 kao obična greška nakon što se učinak već mogao dogoditi. Bez `OnError`/P1.4 `UNKNOWN→RECONCILING` klasifikacije retry može duplicirati ireverzibilan učinak (`/home/matej/HARNESS/nexus/docs/DESIGN-FIXES-r2.md:43-47`; `/home/matej/HARNESS/nexus/docs/HARNESS-SPEC.md:1442-1449`).

## Presuda

**IMA BLOKERA — nije spremno za PRD. Status: FAIL na `4509743`.** Najkraći put je popraviti jedan kanonski effect-path ugovor (sealed resolved ToolSpec, dva totalna switcha, post-dispatch UNKNOWN/reconcile), dati stvarne fleet/pairing potpise te očistiti stale plan/GAPFIX tekst; bez novih slojeva.

Proof ceiling: statička provjera Markdown dizajna; greenfield još nema Go pakete pa `go build` i navedeni RED testovi nisu izvršivi dokaz.
