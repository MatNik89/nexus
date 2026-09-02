# REVIEW5 — Codex red-team verifikacija

Revizija: `51b0263e044ead0fe172ea8e07822e5f0980333c` (`51b0263`).

**V-K1 [DJELOMIČNO]** Plan 4.1 sada ima `ToolSpec{...,ExecutionKind}` (`/home/matej/HARNESS/nexus/docs/HARNESS-PLAN.md:283`), ali kanonski `ToolCall` ga još nema (`/home/matej/HARNESS/nexus/docs/DESIGN-S0-sandbox-codex.md:209-218`); “dopuna” postoji samo kao komentar u fixu. `sbproc` je i dalje proizvoljan `ToolExecutor`, a plan 4.2 i dalje prisvaja `tree-kill` (`/home/matej/HARNESS/nexus/docs/HARNESS-PLAN.md:286-288`). Effect-path nije buildable ni ne-zaobilazan ugovor.

**N4-C01 [ZATVOREN]** `PEP.Decide` ima eksplicitni `default` koji vraća deny + `ErrInvalidDecision` (`/home/matej/HARNESS/nexus/docs/DESIGN-FIXES-r2.md:35-40`).

**N4-C02 [ZATVOREN]** `ExecutionKind` switch sada eksplicitno obrađuje oba dopuštena slučaja, a default odbija umjesto odabira `inproc` (`/home/matej/HARNESS/nexus/docs/DESIGN-FIXES-r2.md:43-48`). Kanonski tip ipak ostaje otvoren pod V-K1.

**N4-C03 [DJELOMIČNO]** Veto sada tekstualno ide `AfterTool→OnError→RecordVeto`, ali `classifyEffectPhase` i `RecordVeto` nemaju potpis, prijelaznu tablicu ni implementirani `UNKNOWN→RECONCILING` ugovor; postoje samo poziv i komentar (`/home/matej/HARNESS/nexus/docs/DESIGN-FIXES-r2.md:53-66`). Dodatno, novi `OnError` pozivi nisu tipovno konzistentni (R5-C01).

**V-K3 [NIJE]** Konkretan `pairing.RemoteInvoke(..., contracts.PlacementGrant)` je napisan, ali “neutralni” `internal/contracts` sada uvozi `queue` i `s7` (`/home/matej/HARNESS/nexus/docs/DESIGN-FIXES-r2.md:85-92`): K0 `contracts` tako ovisi o K1 S7, suprotno DAG-u. Uz to banner još kaže da `PlacementGrant` ostaje u `fleet` (`/home/matej/HARNESS/nexus/docs/GAPFIX-codex.md:1`). Zamjenski import-graf je opet kontradiktoran.

**V-C11 [ZATVOREN]** Plan je očišćen na AccountFleet classify-only + S7 failover-owner (`/home/matej/HARNESS/nexus/docs/HARNESS-PLAN.md:201-204`, `:1045`, `:1082`, `:1112`, `:1159-1160`), a `/home/matej/HARNESS/nexus/docs/GAPFIX-agy.md:1` jasno supersedea stari automatski failover.

**N3-C04 [DJELOMIČNO]** “Dodati/kandidat” za P0.5–P0.11 je očišćeno i ugovori su označeni kao postojeći. Plan ipak još istodobno tvrdi “bez dediciranog Annex RED” i navodi postojeći Annex RED za P0.6/P0.7/P0.8/P0.9/P0.11 (`/home/matej/HARNESS/nexus/docs/HARNESS-PLAN.md:174-175`, `:226-227`, `:275-276`, `:350-351`). Status ostaje samoproturječan.

**V-K4 [ZATVOREN]** DAG sada smješta 6.3–6.8, puni 8.1, 9.1/9.3 te rano razdvaja `S8.1-min`, `S11.1-min` i `S16.6-det` (`/home/matej/HARNESS/nexus/docs/SECTION-MAP.md:18-40`).

## Novi bloker

**R5-C01 [NOVI KRITIČNI]** Ne postoji jedan Go potpis za `OnError` koji može zadovoljiti novi kod: na retku 42 koristi se kao cijeli `(ToolResult,error)` return, na 47 kao jedan `error`, na 51 kao jedan argument, a na 54 kao jedna vrijednost (`/home/matej/HARNESS/nexus/docs/DESIGN-FIXES-r2.md:42-56`). Skica označena “Buildable Go” ne kompajlira. Definirati jedan rezultat, npr. `OnError(...) error`, i svugdje eksplicitno složiti `ToolResult`.

## Presuda

**IMA BLOKERA — nije spremno za PRD. Status: FAIL na `51b0263`.**

Proof ceiling: statička provjera dokumenata; greenfield nema Go pakete pa deklarirani `go build` i RED testovi nisu izvršivi dokaz.
