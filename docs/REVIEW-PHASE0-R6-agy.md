# REVIEW-PHASE0-R6-agy — Phase 0 Code Verification Round 6 (Narrow Final Convergence)

**Meta:** Narrow final convergence verification of Phase 0 code commit `17888b3` and report commit `320af25` on branch `slice/p0-phase0`  
**Recenzent:** Antigravity (Adversarial Phase-0 Code Convergence Verification)  
**Target Files:**
- `internal/preflight/probe/probe.go`
- `internal/preflight/probe/probe_linux_test.go`
- `docs/PROBE-REPORT-2026-09-03.md`

**Authorities & Sources:** `docs/tasks-P0.md` (T01–T03), `CLAUDE.md`, `AGENTS.md`, `HARDQ-CONSOLIDATED.md` (B4, C5, D1, F1), `PRD.md` (§6, §7), `docs/REVIEW-PHASE0-R5-{codex,kilo,agy}.md`.

---

## 1. PREGLED I METODOLOGIJA

U 5. rundi recenzija Codex je zabilježio 3 uska nalaza:
1. Sigurnost testnog okruženja u `TestHostileXDGRuntimeDirCannotWidenRoots` (upotreba jedinstvenog `MkdirTemp` umjesto fiksnog direktorija s rizikom brisanja korisničkih podataka).
2. Uzročnost (kauzalnost) testa sprječavanja podmetanja biblioteka (`TestRootfsReadOnlyPreventsLibShadowing`) uz dinamički C program, provjeru `EROFS` greške i negativnu kontrolu bez `--remount-ro /`.
3. Stroga kontrola proračuna resursa *prije* same alokacije memorije (`loadBounded(remaining)` s preostalim agregatnim limitom, `checkQueueBudget` prije stvaranja čvorova).

Commit `17888b3` u potpunosti rješava sva 3 nalaza.

Verifikacija je obuhvatila pregled koda, analizu potencijalnih novih defekata u promijenjenim linijama i pokretanje cijelog testnog seta na stvarnom Linux hostu (`34 probe + 3 doctor + 3 buildcheck = 40/40 PASS`).

---

## 2. VERIFIKACIJA 3 UGRAĐENE SKUPINE ISPRAVAKA

---

### [OK] 1. `TestHostileXDGRuntimeDirCannotWidenRoots` koristi `MkdirTemp` jedinstveni direktorij
* **Lokacija:** `internal/preflight/probe/probe_linux_test.go:708-720`
* **Nalaz:** 
  - Uklonjen je fiksni direktorij `$HOME/.nexus-probe-red`.
  - Koristi se `os.MkdirTemp(home, ".nexus-probe-red-*")` koji stvara izolirani, jedinstveni direktorij.
  - `defer os.RemoveAll(sub)` briše isključivo novo-kreirani privremeni direktorij, čime je u potpunosti uklonjen rizik nenamjernog brisanja postojećih korisničkih datoteka u `$HOME`.

---

### [OK] 2. Kauzalni test za sprječavanje podmetanja biblioteka (`EROFS` + dinamički binarij + negativna kontrola)
* **Lokacija:** `internal/preflight/probe/probe.go:106-112,633-636`, `probe_linux_test.go:621-667`
* **Nalaz:**
  - `buildDynamicWriter` generira dinamički C izvršni program koji ovisi o libc biblioteci, pa kontejner doista materijalizira `/nexus-libs` i `/lib`.
  - `TestRootfsReadOnlyPreventsLibShadowing` provjerava da pokušaj pisanja u `/nexus-libs/libc.so.6` i `/lib/evil` pada s eksplicitnom greškom `EROFS` (`Read-only file system`), a ne praznim `ENOENT`.
  - Dodana je negativna kontrola `TestNegativeControlLibWriteSucceedsWithoutRemountRO` (`spec.loosen.remountRW = true`) koja dokazuje da bez `--remount-ro /` pisanje u `/nexus-libs/injected.so` prolazi. Test je time 100% kauzalan i red-capable.

---

### [OK] 3. Izvršavanje proračuna prije alokacije memorije (`loadBounded(remaining)` i `checkQueueBudget`)
* **Lokacija:** `internal/preflight/probe/probe.go:120-125,156-184,313-370,409-421`, `probe_linux_test.go:669-695`
* **Nalaz:**
  - `loadBounded(path, remaining)` uzima preostali agregatni budžet (`maxClosureBytes - total`) i postavlja efektivni limit `cap = min(maxClosureOneFile, remaining)`. Ako je preostali budžet iscrpljen ili datoteka na disku prelazi limit, poziv pada odmah na `f.Stat()` bez čitanja bajtova u memoriju (`io.LimitReader(f, cap+1)`).
  - `checkQueueBudget(cur, add)` provjerava `add > maxDepQueue || cur+add > maxDepQueue` *prije* nego što se kreiraju `rootNodes` ili `childNodes` slice-ovi.
  - Dodani su RED jedinični testovi: `TestQueueBudgetRejectsBeforeAllocation` i `TestLoadBoundedHonorsAggregateAllowance`.

---

## 3. PROVJERA POTENCIJALNIH NOVIH DEFEKATA

U linijama promijenjenim u commitu `17888b3` nema novih sigurnosnih rupa, curenja deskriptora niti regresija.

---

## 4. ZAKLJUČAK

Svi preostali nalazi su riješeni na najvišoj inženjerskoj i sigurnosnoj razini. Faza 0 (`T01–T03`) je u potpunosti zaključana i spremna za integraciju.

---

VERDICT: PASS
