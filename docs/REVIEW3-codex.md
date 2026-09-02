# REVIEW3 — Codex red-team verifikacija R2 fixeva

Revizija: `1b4686c9455f8054a9e597cc9f462078a7642946`. Status je zasnovan na stvarnom sadržaju kanonskih dokumenata, ne na tvrdnji `DESIGN-FIXES-r2` da je nalaz zatvoren.

## Dodijeljeni prethodni nalazi

**V-K1 [DJELOMIČNO]** Sandbox je sada nacrtan u putu (`DESIGN-FIXES-r2.md:16-46`; `SECTION-MAP.md:57-59`), ali nije ne-zaobilazan: `sbproc` je proizvoljan `ToolExecutor`, polje `EffectPath.sandbox` se ne koristi, a izbor ovisi o `call.Effect`/`call.RequiresProcess`. Read-only subprocess ide na `inproc`, a lažno deklariran call može izbjeći S6.2. `HARNESS-PLAN.md:287-289` i dalje prisvaja session/tree-kill u S4.2. **Nije zatvoreno dok authoritative S4 `ToolSpec.ExecutionKind` ne bira zapečaćeni `SandboxedProcessExecutor`.**

**V-K3 [DJELOMIČNO]** Mapa i plan razdvajaju 18.5/18.6 i uvode `fleet/ids` (`SECTION-MAP.md:50-53`; `HARNESS-PLAN.md:777-779`), ali konkretni superseded structovi ostaju cross-importani (`GAPFIX-codex.md:670-710,834-856`). Fix premješta i cijeli `PlacementGrant` u navodno dependency-free `ids`, iako grant sadrži `queue.TaskID`, `reliability.AttemptGrant` i `time.Time`. **Popravak nije buildable tip-definicija:** u `ids` ostaviti samo skalarne ID-eve; `PlacementGrant` ostaje u fleet/neutralnom contracts paketu uz jednosmjerni import.

**V-K4 [DJELOMIČNO]** Novi redoslijed ispravno stavlja S5 i deterministic checker prije S3/S4 te kasne archmap/learned/profile dijelove (`SECTION-MAP.md:18-55`). Međutim `S8.1-min`, `S11.1-min` i `S16.6-det` postoje samo kao etikete mape, bez izdvojenih ugovora/paketa; `HARNESS-PLAN.md:148-158` i dalje kaže da S1.2 recovery ovisi o leaseu koji posjeduje S7. DAG “po ID-u” još izostavlja 6.3–6.8, puni 8.1 te 9.1/9.3. **Ciklusi su precrtani u mapi, ali granice koje ih prekidaju nisu kanonizirane.**

**V-C10 [NIJE]** Fix pravilno kaže `S6.0.Decide → S6.9 order` (`DESIGN-FIXES-r2.md:48-50`), ali kanonski plan i dalje radi `authorize(6.9)` u 3.1 i `approval 6.9` u 4.8 (`HARNESS-PLAN.md:240-242,317`). Dodatno novi `RunTool` na grešku izvršenja svejedno zove success-hook `AfterTool` (`DESIGN-FIXES-r2.md:37-39`), suprotno S6.9 `on_error` grani.

**V-C11 [NIJE]** Fix i status-mapa kažu AccountFleet `Classify`-only (`DESIGN-FIXES-r2.md:51-52`; `SECTION-MAP.md:94`), ali kanonski 2.2 i tri addendum retka još nalažu KeyPool rotaciju/account-failover (`HARNESS-PLAN.md:202-205,1048,1116,1163`). Dokumenti i dalje definiraju dva ponašanja; S7 sole-owner nije jednoznačan.

**V-D2 [ZATVOREN]** Aktualna mapa dosljedno razlikuje `P0.3/EventJournal` od capability subsectiona S0.3 (`SECTION-MAP.md:18-20,57-59`), a plan koristi P0.3 kao journal ugovor (`HARNESS-PLAN.md:91-93,160-168`). Pretraga aktualnih kanonskih/fix dokumenata nije našla preostali `journal=S0.3`.

## Novi blokeri/regresije u S0–S9

**N3-C01 [NOVI KRITIČNI]** `PEP.Decide` obrađuje samo `DENY`; `ASK` pada kroz isti put kao `ALLOW` (`DESIGN-FIXES-r2.md:30-37`). To omogućuje izvršenje bez odobrenja. **Mora biti totalni switch:** ALLOW nastavlja, DENY staje, ASK zahtijeva exact-intent approval ili staje.

**N3-C02 [NOVI KRITIČNI]** Novi effect-path uvijek zove `AfterTool` i ignorira njegov error, čak kad `Execute` vrati grešku (`DESIGN-FIXES-r2.md:37-39`). Time se zaobilazi `OnError`, a post-exec sigurnosni veto može nestati. **Na `err != nil` pozvati samo `OnError`; greška `AfterTool` mora fail-closed u S7 classification bez isporuke outputa.**

**N3-C03 [NOVA REGRESIJA]** Brojnik je mehanički 108 jedinstvenih `**X.Y` markera u planu, ali `SECTION-MAP.md:112` i dalje kaže `Zbroj (107)` i “7 novih”, dok isti dokument na `:83,87` kaže 108 i 8. D1 nije regresijski zaključan jednim generiranim manifestom.

**N3-C04 [NOVA REGRESIJA]** Annex ugovori P0.5/P0.6/P0.7/P0.8/P0.9/P0.11/P0.13 stvarno postoje (`HARNESS-SPEC.md:1527-1565`), ali plan ih još opisuje kao “dodati/kandidat/honest gap” (`HARNESS-PLAN.md:106-110,129,174-175,227-229,277-278,351-353,511-512`), a ledger čak kaže `P0.5-0.13 → dodati u Annex` (`SECTION-MAP.md:92`). Gate-status zato daje suprotne upute agentu gradnje.

## Presuda

`ZATVOREN`: 1/6 · `DJELOMIČNO`: 3/6 · `NIJE`: 2/6. R2 fix ne prolazi Round-3 gate zbog N3-C01/N3-C02 i nekanoniziranih K3/K4 granica.

Najjači kontraargument je da `DESIGN-FIXES-r2.md` eksplicitno nadjačava stare dokumente. To opravdava prijelazni dual-source samo ako je novi ugovor potpun; K1 pseudokod ima konkretne bypass putove, a K3 nema zamjenske struct definicije, pa deklaracija precedencea nije dokaz zatvaranja.

Proof ceiling: statička verifikacija Markdown dizajna; greenfield repozitorij još nema Go implementaciju, pa `go build`/RED imena u fixu nisu izvršeni dokaz.

**Status: FAIL** na `1b4686c9455f8054a9e597cc9f462078a7642946`.
