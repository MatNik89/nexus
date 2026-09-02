# REVIEW2 — Codex red-team: S0–S9 × SECTION-MAP

Opseg: samo nove rupe/nelogičnosti u S0–S9, njihove owner-invarijante i ovisnosti prema fazama K–P. Nalazi iz `PLAN-HOLES-CONSOLIDATED.md` nisu ponovljeni.

## Nalazi

**R2-C01 — Brojnik podsekcija nema jedan izvor istine.** `HARNESS-PLAN.md:5,7` kaže 82, `SECTION-MAP.md:77,99` kaže 81 + inline/+7, dok aktualni numerirani naslovi daju **107 ukupno i 56 u S0–S9**. Bez kanonskog manifesta nije dokaziva tvrdnja da je svaka podsekcija mapirana. **Popravak:** generirati jedan eksplicitan ID-popis iz plana i iz njega računati status i fazu.

**R2-C02 — Sedam S6 podsekcija nema fazu.** Faza K navodi samo `6.0/6.1/6.2/6.9` (`SECTION-MAP.md:19`), ali plan ima i `6.3–6.8` te `6.10` (`HARNESS-PLAN.md:374-401`). Grupni redak ne kaže kamo one ulaze; time samo **49/56** S0–S9 ID-eva ima nedvosmislen fazni smještaj. **Popravak:** razlomiti S6 u K-core i kasne flagove, s fazom po svakom ID-u.

**R2-C03 — `journal=S0.3` pokazuje na pogrešnog ownera.** Mapa dva puta proglašava S0.3 journalom (`SECTION-MAP.md:36,48-49`), ali S0.3 je capability negotiation (`HARNESS-PLAN.md:102-107`; `DESIGN-S0-sandbox-codex.md:450`). Journal je nenumerirani paket (`HARNESS-PLAN.md:87-89`). **Popravak:** dati journalu stvarni subsection/package ID i ažurirati sve fazne i owner reference; ne preimenovati capability 0.3 prešutno.

**R2-C04 — S0 paketni DAG ima dvije suprotne kanonske verzije.** Plan tvrdi `contracts <- journal <- machine` (`HARNESS-PLAN.md:87-89`), a S0 dizajn tvrdi zaseban `contracts <- machine` i `contracts <- closure <- journal` (`DESIGN-S0-sandbox-codex.md:18-22`). To mijenja redoslijed machine/closure/journal gradnje. **Popravak:** zadržati jedan import-DAG, uz eksplicitno definiranu orijentaciju strelice.

**R2-C05 — K sadrži skriven ciklus S1.2 ↔ S7.** Mapa postavlja S7 nakon S1 (`SECTION-MAP.md:18-20`), ali S1.2 kaže da je lease kanonski recovery (`HARNESS-PLAN.md:144-154`), dok S7.2/S7.3 definira lease/orphan recovery koristeći S1.2 process-group/start-token (`HARNESS-PLAN.md:421-442`). **Popravak:** u K prvo izdvojiti minimalni process-identity primitive u S1.2; S7 ga konzumira, a S1.2 ne smije ovisiti o S7 leaseu.

**R2-C06 — L nije DAG: S3.1 ↔ S16.6.** S3.1 ne može završiti bez `Checker.Grade(16.6)` (`HARNESS-PLAN.md:236-243`), a mapa gradi S3 prije S16.6 i označava S16.6 kao ovisan o S3 (`SECTION-MAP.md:24,27`). **Popravak:** premjestiti minimalni `AcceptanceContract/Checker` port prije S3; integracijski checker koji koristi loop može ostati iza S3.

**R2-C07 — L povratno ovisi o M i P.** S4.4 u L uključuje `archmap`, čiji je owner S8.3 u M (`HARNESS-PLAN.md:296-300,462-464`; `SECTION-MAP.md:25,29-31`). S2.6 u L uključuje `LearnedRouter`, ali reward smije doći samo iz S16.4 u P (`HARNESS-PLAN.md:218-221`; `SECTION-MAP.md:23,43-45`). **Popravak:** S4.4-L završiti na text/AST/LSP i dodati archmap integraciju u M; S2.6-L svesti na deterministički router, learned adapter graditi u P.

**R2-C08 — M povratno ovisi o O/P kroz nove S9 foldove.** PersonalProfile 9.5 obećava channel izolaciju, a njegov izvorni dizajn zahtijeva S10.6 i S14.5 (`HARNESS-PLAN.md:503`; `GAPFIX-kilo.md:60-69`), dakle N/O nakon M. ObligationStore 9.6 za `DoneCondition` zahtijeva S16.1 app-contract (`GAPFIX-kilo.md:107-131`), koji je u P. **Popravak:** u M graditi samo profile/memory namespace i obligation persistence; channel/ACL binding dodati u N/O, a app-contract evaluator u P.

**R2-C09 — Kanonski effect-path zaobilazi sandbox i dijeli process owner.** Owner-tablica kaže da samo S1.2 smije spawn/kill (`DESIGN-memory-effectpath-kilo.md:121-148`), ali S6.2 dizajn ima vlastiti `Supervisor.Start`/`backend.Launch` (`DESIGN-S0-sandbox-codex.md:1110-1119`), a 4.2 izravno prisvaja session/tree-kill (`HARNESS-PLAN.md:284-286`). Još gore, proglašeni put `S3→S4→S6.0→S6.9→S1.2→S7` nema S6.2 (`DESIGN-memory-effectpath-kilo.md:129-142`). **Popravak:** jedan S1.2 supervisor mora orkestrirati S6.2 prepare/attest/exec i biti jedini spawn/kill ulaz; 4.2 predaje samo typed exec-spec.

**R2-C10 — Policy i lifecycle owneri su zamijenjeni na pozivnim mjestima.** S3.1 radi `authorize(6.9)` (`HARNESS-PLAN.md:236-238`), a 4.8 također veže approval samo uz 6.9 (`HARNESS-PLAN.md:313`), premda owner-matrica kaže decision=S6.0, lifecycle-order=S6.9 (`DESIGN-memory-effectpath-kilo.md:121-126`). **Popravak:** oba puta moraju eksplicitno ići `S6.0.Decide → S6.9.Before/Commit`; 6.9 ne smije postati drugi policy owner.

**R2-C11 — AccountFleet uvodi drugi retry/fallback engine.** S7 je jedini owner pokušaja (`HARNESS-PLAN.md:410-419`), ali fold za AccountFleet zahtijeva automatski failover i vlastiti exponential cooldown (`GAPFIX-agy.md:157-167`), dok ga S2.2 samo djelomično opisuje kao planner (`HARNESS-PLAN.md:198-201`). **Popravak:** AccountFleet smije samo klasificirati credential i vratiti candidate/cooldown podatke; isključivo S7 izdaje novi `AttemptGrant` i raspoređuje backoff/fallback.

**R2-C12 — Addendum-fold tvrdi više nego što je ugrađeno u kanonske sekcije.** A10 kaže da je RAM-lifecycle foldan i buildable (`HARNESS-PLAN.md:1132-1160`), ali izvor ga mapira na nepostojeći S1.4 i semantički pogrešan S2.3 (`GAPFIX-agy.md:13-27`); S1/S2/S8 tijela nemaju taj ugovor, a `SECTION-MAP.md:53-73` nema njegovo odredište. G1/G2/G4 izvor dodatno kaže da ne trebaju nove sekcije (`GAPFIX-kilo.md:215-225`), dok mapa stvara 4.8/9.5/9.6 (`SECTION-MAP.md:61-65,71-73`). **Popravak:** fold-tablica mora navesti svaki graft i njegov jedini owner; tek zatim ukloniti/označiti superseded mappinge u GAPFIX-u.

## Sažetak provjera

- **Nove rupe/nelogičnosti:** R2-C01, C03, C04, C12.
- **Dependency-violations:** R2-C05–C08.
- **Owner-invarijante:** journal krši C03; process C09; policy/lifecycle C10; retry C11.
- **Fazna pokrivenost S0–S9:** **FAIL — 49/56 eksplicitno smješteno; 6.3–6.8 i 6.10 nisu.**

Najjači kontraargument je da faza može sadržavati interne stube i da su flagovi implicitno obuhvaćeni roditeljskim S-brojem. To ne spašava mapu koja se naziva DAG-om: C05/C06 su stvarni ciklusi, a selektivno navođenje samo četiri S6 ID-a čini implicitno obuhvaćanje neprovjerljivim.

Proof ceiling: statički pregled aktualnih Markdown artefakata; ne dokazuje buduću implementaciju ni runtime ponašanje. Ulazni SHA-256: `HARNESS-PLAN=8bc5dd1f97c7f1327dd21c735734eb0ccb6a75854892edcb8d4d99f163671633`, `SECTION-MAP=31f200380afb761c1777d02967b8b8724a252dea34d3ac7d524ab74ccb1cc101`.

**Status: FAIL** — build-order/owner konzistencija nije spremna za scaffold na reviziji `44e25ca2ff8fef7a94fddd9b7c4389d9631841e0`.
