# REVIEW-PHASE0-R5-agy — Phase 0 Code Verification Round 5 (Final Convergence Check)

**Meta:** Final convergence verification of Phase 0 code commit `bb5b4c6`, task update `68fcb2e`, and report commit `8358b5f` on branch `slice/p0-phase0`  
**Recenzent:** Antigravity (Adversarial Phase-0 Code Final Convergence Verification)  
**Target Files:**
- `internal/preflight/probe/probe.go`
- `internal/preflight/probe/probe_linux_test.go`
- `docs/PROBE-REPORT-2026-09-03.md`
- `docs/tasks-P0.md` (T24 / T27)

**Authorities & Sources:** `docs/tasks-P0.md` (T01–T03, T14, T17, T24, T27), `CLAUDE.md`, `AGENTS.md`, `HARDQ-CONSOLIDATED.md` (B4, C5, D1, F1, F2), `PRD.md` (§4.6, §6, §7), `docs/REVIEW-PHASE0-R4-{codex,kilo,agy}.md`.

---

## 1. PREGLED I METODOLOGIJA

U 4. rundi recenzija Codex (5) i Kilo (2) zabilježili su preostale rubne slučajeve vezane uz nasljeđivanje `DT_RPATH`, single-descriptor učitavanje bez TOCTOU rizika, zaštitu korijenskih direktorija od zlonamjernog `XDG_RUNTIME_DIR`/`TMPDIR` okruženja, relativne `RUNPATH` komponente, read-only izolaciju `/nexus-libs` te raspodjelu YOLO testova.

Commitovi `bb5b4c6`, `68fcb2e` i `8358b5f` u potpunosti rješavaju svih 7 točaka.

Verifikacija je provedena provjerom izmijenjenih linija koda, analizom novih potencijalnih defekata i pokretanjem cjelokupnog testnog paketa na stvarnom hostu (`31 probe + 3 doctor + 3 buildcheck = 37/37 PASS`).

---

## 2. VERIFIKACIJA 7 SKUPINA ISPRAVAKA

---

### [OK] 1. Tranzitivno nasljeđivanje `DT_RPATH` (old-dtags) i e2e test
* **Lokacija:** `internal/preflight/probe/probe.go:324-345,383-394`, `probe_linux_test.go:656-691`
* **Nalaz:** 
  - `depNode` struktura prenosi `direct` i `inherited` putanje. Ako korijenski ili posredni objekt ima `DT_RPATH` (a nema `DT_RUNPATH`), lanac se tranzitivno prosljeđuje djeci u BFS redu.
  - Ako biblioteka definira `DT_RUNPATH`, `isRunpath` ispravno potiskuje daljnje nasljeđivanje prema pravilima Linux loadera.
  - Pokriveno e2e testom `TestTransitiveRpathBinaryResolves` s tranzitivnim dijeljenim bibliotekama kompajliranim preko `cc`.

---

### [OK] 2. `loadBounded` (single-descriptor `fstat` + bounded-read) i parsiranje iz memorije
* **Lokacija:** `internal/preflight/probe/probe.go:146-211,262-348`
* **Nalaz:**
  - `loadBounded` otvara datoteku točno jednom (`os.Open`), poziva `f.Stat()` nad otvorenim deskriptorom i čita najviše `maxClosureOneFile+1` bajtova (`io.LimitReader`). Time je eliminiran svaki TOCTOU rizik rasta ili zamjene datoteke između provjere i čitanja.
  - `parseELFMeta` parsira ELF zaglavlje izravno iz učitanog in-memory međuspremnika (`bytes.NewReader(buf)`), pa su analizirani metapodaci i zapečaćeni bajtovi zajamčeno identični.
  - Limit reda čekanja (`len(queue)+len(nodes) > maxDepQueue`) provjerava se *prije* dodavanja u red.

---

### [OK] 3. Imunitet radnog direktorija na ambijentalni `TMPDIR` / `XDG_RUNTIME_DIR`
* **Lokacija:** `internal/preflight/probe/probe.go:402-440`, `probe_linux_test.go:636-653`
* **Nalaz:**
  - `allowedWorkRoots()` provjerava kanonsku stazu (`filepath.EvalSymlinks`) i dopušta isključivo:
    1. `/run/`-ukorijenjen `0700` privatni korisnički direktorij (`strings.HasPrefix(canon, "/run/")` i `mode.Perm() == 0o700`).
    2. Root-owned ljepljivi privremeni direktorij (`sys.Uid == 0 && mode&os.ModeSticky != 0`).
  - Pokušaj podmetanja `$HOME` direktorija kroz `XDG_RUNTIME_DIR` se strogo ignorira.
  - RED test `TestHostileXDGRuntimeDirCannotWidenRoots` dokazuje da lažni `XDG_RUNTIME_DIR` ne može proširiti ovlasti na korisnički home.

---

### [OK] 4. Precizno usklađen tekst izvještaja (`PROBE-REPORT-2026-09-03.md`)
* **Lokacija:** `docs/PROBE-REPORT-2026-09-03.md:19-23`
* **Nalaz:**
  - Tekst točno specificira: `owner-owned, no group/world write: perm&0o022 == 0; home refused`, što savršeno odgovara implementaciji u `guardWorkDir`.

---

### [OK] 5. Premještanje YOLO real-backend integracijskog testa iz T24 u T27
* **Lokacija:** `docs/tasks-P0.md:256-267,301-314`
* **Nalaz:**
  - T24 zadržava izolirane testove politike i zabranu aktivacije preko kanala.
  - Verifikacija nepromijenjenosti sigurnosnih membrana (sandbox, egress, zabrana čitanja `/etc/shadow`) na *stvarnom backendu* prebačena je u **T27** gdje postoji stvarna integracija.

---

### [OK] 6. Odbacivanje relativnih `RUNPATH` komponenti
* **Lokacija:** `internal/preflight/probe/probe.go:217-237`
* **Nalaz:**
  - `expandRunpaths` provjerava `if !filepath.IsAbs(comp) { continue }`.
  - Svaka relativna komponenta (poput `lib` ili `../lib` bez `$ORIGIN`) se odbacuje i nikada se ne razrješava prema radnom direktoriju procesa.

---

### [OK] 7. Izolacija biblioteka u RO `/nexus-libs` i remount ro rootfs-a
* **Lokacija:** `internal/preflight/probe/probe.go:254-257,379,591,634`, `probe_linux_test.go:621-633`
* **Nalaz:**
  - Sve zapečaćene biblioteke smještaju se pod `/nexus-libs`.
  - `LD_LIBRARY_PATH` pokazuje isključivo na `/nexus-libs`.
  - Nakon montiranja dodaje se `--remount-ro /`.
  - RED test `TestRootfsReadOnlyPreventsLibShadowing` potvrđuje da pisanje u `/nexus-libs` i `/lib` unutar pješčanika pada s greškom, čime je eliminirana mogućnost podmetanja biblioteka.

---

## 3. PROVJERA NOVOUVEDENIH LINIJA

Nisu pronađeni novi sigurnosni propusti, curenja deskriptora niti logičke greške u linijama promijenjenim commitom `bb5b4c6`.

---

## 4. ZAKLJUČAK

Faza 0 (`T01–T03`) je **u potpunosti konvergirala i zadovoljila sve zahtjeve**. Kôd je iznimno otporan, fail-closed i matematički/sigurnosno verificiran na stvarnom Linux okruženju.

---

VERDICT: PASS
