# REVIEW6-kilo — verifikacija iteracije-4 (moj N5-01 + preostale sitnice)

Provjera u dokumentima: `DESIGN-FIXES-r2.md` (22:38), `DESIGN-S0-sandbox-codex.md` (22:39),
`GAPFIX-codex.md` (22:39), `HARNESS-PLAN.md` (22:39), `SECTION-MAP.md` (22:32, nepromijenjen u iter-4).

---

## A. VERIFIKACIJA

**N5-01 (PlacementGrant više NIJE u baznom `internal/contracts`) — ZATVOREN.**
`DESIGN-FIXES-r2:97-105`: `PlacementGrant` je sad u **`internal/fleet/placement`** (nizvodni paket,
uvozi `{ids, queue, s7}`), eksplicitno "NIJE bazni `internal/contracts` (fix N5-01)". Konkretan
potpis: `pairing.RemoteInvoke(node ids.NodeID, g placement.PlacementGrant)`. `fleet` i `pairing` uvoze
`placement`+`ids`; nijedan ne uvozi drugoga; `contracts` ostaje K0 temelj bez rubova prema queue.

**contracts→queue ciklus — NESTAO.**
`:103-105` izričito: bazni `contracts` je K0 koji `queue`/`s7` UVOZE; PlacementGrant više ne ide tamo,
nego u `placement` (nizvodno). Nema povratnog ruba `queue`/`s7` → `placement`. Ciklus razriješen.

**RunTool eksplicitni enumovi+sučelja — ZATVOREN.**
`:13-35`: `Decision`/`ExecutionKind`/`EffectPhase` enumi (svaki s `*Invalid` zero-vrijednosti);
`Middleware.OnError(ctx, error) error` (jedan potpis); `RetryOwner.Record`/`RecordVeto` potpisi;
`classifyEffectPhase`. RunTool (`:46-69`) ih sve koristi + default-deny + default-reject + veto→RECONCILING.

**ToolCall+ExecutionKind u DESIGN-S0 — ZATVOREN.**
`DESIGN-S0-sandbox-codex.md:215`: `ExecutionKind ExecutionKind // ExecInProcess|ExecProcess — resolved
iz zapečaćenog S4 ToolSpec`.

**plan 4.2 tree-kill maknut — ZATVOREN.**
`HARNESS-PLAN:285`: "Tree-kill NIJE ovdje — predaje exec-spec; kill je UNUTAR S6.2.Launch/S1.2
(owner-invarijanta)". 4.2 više ne prisvaja tree-kill.

**honest-gap S1/S2/S3/S5 više ne proturječe — ZATVOREN.**
Grep "honest gap/bez dediciranog" više NEMA S1/S2/S3/S5 linija (P0.6/P0.7/P0.8/P0.9/P0.11 su "u
Annexu A"). Preostale (S4:317, S8:477, S9:507, S15:699) su ISKRENE "posredno P1.1/P1.3/P0.3" izjave,
ne fantomski gate-citati.

**codex#5 — ZATVOREN.** `:763`: "codex#5 — Tier-3 BACKLOG, ne formalni Annex".
**9.6 write→journal — ZATVOREN.** `:504`: "write kroz EventJournal.Append (P0.3), ne izravni sqlite".

---

## B. PREOSTALO

Samo D1 doc-nit (iz ranijih rundi): `SECTION-MAP:113` "Zbroj (108)" ali zbroj popisanih statusa ≈ 91;
kanonski ID-manifest i dalje nije generiran. NIJE tehnički bloker — kozmetika status-ledgera.

---

## C. PRESUDA

**NEMA BLOKERA — spremno za PRD.**

N5-01 (moj nalaz) zatvoren ispravno: PlacementGrant u nizvodni `internal/fleet/placement`, bazni
`contracts` čist, ciklus nestao. Svi iteracija-4 fixevi verificirani u dokumentima (ne na riječ).
Jedino preostalo je D1 status-zbroj (doc-kozmetika), ne blokira PRD.

---

## Confidence

- **Provjereno:** svi citirani linijski navodi; package-graf (`placement→{ids,queue,s7}`, `queue/s7→contracts`,
  `pairing/fleet→placement+ids`, nema povratnog ruba) rekonstruiran iz `DESIGN-FIXES-r2:97-105`.
- **Neprovjereno:** `go build` nije pokrenut (graf je iz teksta; rubovi eksplicitni i konzistentni).
- **Najslabija karika:** `internal/fleet` u `:100` kaže "(+placement po potrebi)" — ako fleet uveze
  placement a placement NE uvozi fleet, nema ciklusa; potvrđeno iz teksta, ali vrijedi dvaput provjeriti
  kod prvog `go build`.

topknot: ultra+preflight
