# REVIEW5-kilo — verifikacija iteracije-3 fixeva + novi

Provjera u dokumentima: `DESIGN-FIXES-r2.md` (22:31), `GAPFIX-agy.md` (22:32), `SECTION-MAP.md`
(22:32), `HARNESS-PLAN.md` (22:31), `GAPFIX-codex.md` (22:23, nepromijenjen u iter-3).

---

## A. VERIFIKACIJA

**GAPFIX-agy SUPERSEDED banner (AccountFleet) — ZATVOREN.**
`GAPFIX-agy:1`: "AccountFleet `automatski failover/rotacija` je ZAMIJENJEN — SAMO klasificira; S7 je
jedini failover/retry owner".

**R2-17 fold-target — ZATVOREN.**
`GAPFIX-kilo:1` (nepromijenjen) proglašava kanonski fold G1→4.8/G2→9.5/G4→9.6/O6→6.10.

**RunTool (a/b/c) — ZATVOREN.**
`DESIGN-FIXES-r2:34-59`: (a) `switch p.pep.Decide` total + `default: denied, ErrInvalidDecision`
(`:35-40`); (b) `switch call.ExecutionKind` total + `default: denied, ErrUnknownExecKind` (ne inproc,
`:43-48`); (c) AfterTool veto → `OnError` → `classifyEffectPhase` → `RecordVeto(grant, phase, oe)`
IRREVERSIBLE+committed→UNKNOWN→RECONCILING (`:53-57`).

**ToolSpec+ExecutionKind u planu 4.1 — ZATVOREN.**
`plan:283`: `ToolSpec{ID,SchemaHash,EffectClass,ExecutionKind}` + `ExecutionKind∈{ExecInProcess,
ExecProcess}`. (Sitnica: `plan:286` 4.2 i dalje piše "tree-kill" — owner-invarijanta `SECTION-MAP:59`
kaže process-tree=S1.2 unutar S6.2.Launch; 4.2 nije re-worded.)

**account-failover→classify-only+S7 u planu — ZATVOREN.**
`plan:203,1045,1082,1112,1159` svi "classify-only; S7 jedini failover/grant".

**P0.5-P0.11 'kandidat'→'u Annexu' — ZATVOREN.**
`plan:109-110` (P0.5), `:175` (P0.6), `:227` (P0.7/0.8), `:276` (P0.9), `:351` (P0.11), `:509` (P0.13)
svi "u Annexu A (dodan)". Nijedan "kandidat za matrix-prep" ne ostaje u gate-kontekstu.

**DAG dopunjen 6.3-6.8/8.1/9.1/9.3 — ZATVOREN.**
`SECTION-MAP:30` (6.3-6.8 → K1), `:23` (S8.1-min, S11.1-min → K0), `:38` (8.1-puni), `:39` (9.1/9.3).

---

## B. NOVI NALAZ (materijalni)

**N5-01 — PlacementGrant u `internal/contracts` stvara contracts↔queue CIKLUS.**
`DESIGN-FIXES-r2:85-89` premješta `PlacementGrant{... Task queue.TaskID; Attempt s7.AttemptGrant}` u
`internal/contracts` ("već postoji" = S0 bazni paket). Ali `queue` (`GAPFIX-codex` 7.2, `QueueTask`)
uvozi `contracts` (`TenantID/IdempotencyKey/ValueRef/OperationKind/ErrorCode`). Rezultat:
`contracts → queue` (radi `TaskID`) i `queue → contracts` (radi S0 tipova) = **import-cycle**.
Autor čak piše "uvozi ids/queue/s7 — nizvodno od njih" (`:87`) ali ne vidi povratni rub queue→contracts.
Ovo je GORE od S18-niše: `contracts` je K0 bazni paket ("ništa ne ovisi prije", `SECTION-MAP:19`), pa
sad bazni sloj ovisi o K1 (`queue`/`s7`) — cijeli DAG-temelj postaje cikličan. Iteracija-2 rješenje
(PlacementGrant u `fleet`, pairing prima kao argument) NIJE imalo ovaj ciklus — iter-3 regresija.
**Fix opcije:** (a) `TaskID`/`AttemptGrant` kao skalari u `ids`/`contracts` (pa PlacementGrant bez
rubova), ili (b) vratiti PlacementGrant u downstream paket (`fleet`/`placement`), ne u bazni `contracts`.

---

## C. PREOSTALE SITNICE (ne blokiraju, ali još otvorene)

- **codex#5 (`plan:765`, 18.2)** — i dalje goli, bez "Tier-3 BACKLOG" qualifiera (za razliku od #2/#3).
- **plan 9.6 (`:506`)** — i dalje ne spominje "write→journal P0.3" (fix je samo u DESIGN-FIXES-r2:77 + SECTION-MAP:102).
- **SECTION-MAP Zbroj (`:113`)** — "Zbroj (108)" ali zbroj statusa ≈ 91; nema ID-manifesta (D1).
- **plan 4.2 "tree-kill" (`:286`)** — neusklađeno s owner-invarijantom (S1.2 unutar S6.2.Launch).

---

## D. PRESUDA

**NIJE "NEMA BLOKERA".** Jedan materijalni novi bloker: **N5-01** (contracts↔queue ciklus) — compile-
level, u baznom S0 paketu, unesen iteracijom-3 K3 premještajem. Treba ga zatvoriti (opcija a/b gore)
prije nego što se smatra spremnim. Sve ostalo (codex#5, 9.6-journal, Zbroj, 4.2-tree-kill) su
doc-sitnice, ne blokiraju PRD ali bi trebale ići u isti cleanup.

---

## Confidence

- **Provjereno:** svi citirani linijski navodi pročitani; `contracts`/`queue`/`ids` paket-ovisi
  rekonstruirani iz `DESIGN-FIXES-r2` + `GAPFIX-codex` 7.2 (queue→contracts rub + PlacementGrant→queue rub).
- **Neprovjereno:** nisam kompajlirao (N5-01 je strukturna analiza import-grafa iz teksta — rubovi su
  eksplicitni u dokumentima, ali `go build` nije pokrenut).
- **Najslabija karika:** N5-01 ovisi o tome da je `internal/contracts` == S0 bazni `contracts` koji
  `queue` uvozi. Ako je autor mislio na ZASEBAN viši "contracts" paket (različit od S0 baznog), ciklus
  nestaje — ali "već postoji" + "kernel-contracts" + GAPFIX-codex `contracts.*` ukazuju da je ISTI paket.

topknot: ultra+preflight
