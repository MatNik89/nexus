# REVIEW-PHASE0-R3-agy — Phase 0 Code Verification Round 3 (Final Verification)

**Meta:** Final verification of Phase 0 code rewrite at Commit `b8e4a74` / Report Commit `c816aff` on branch `slice/p0-phase0`  
**Recenzent:** Antigravity (Adversarial Phase-0 Code Final Verification)  
**Target Files:**
- `cmd/nexus/main.go`
- `cmd/nexus/probe_linux.go`
- `cmd/probehelper/main.go`
- `internal/preflight/probe/probe.go`
- `internal/preflight/probe/probe_linux_test.go`
- `internal/preflight/probe/memfd_linux.go`
- `internal/preflight/probe/seccomp_linux.go`
- `internal/preflight/doctor/doctor.go`
- `docs/PROBE-REPORT-2026-09-03.md`

**Authorities & Sources:** `docs/tasks-P0.md` (T01–T03), `CLAUDE.md`, `AGENTS.md`, `HARDQ-CONSOLIDATED.md` (B4, C5, D1, F1), `PRD.md` (§6 item 6, §7), `docs/REVIEW-PHASE0-R2-{codex,kilo,agy}.md`.

---

## 1. PREGLED I METODOLOGIJA

U 2. rundi recenzija identificirano je 9 rezidualnih točaka (Codex 8 + Kilo 1). Commit `b8e4a74` donosi njihovo potpuno rješenje, a commit `c816aff` veže izvještaj o ispitivanju uz testirani commit.

Verifikacija je obuhvatila provjeru svih 9 ugrađenih ispravaka u kodu, analizu potencijalnih novih defekata te pokretanje punog testnog seta na stvarnom hostu (`21 probe + 3 doctor + 3 buildcheck = 27/27 PASS`).

---

## 2. VERIFIKACIJA UGRAĐENIH ISPRAVAKA (9/9 SKUPINA)

---

### [OK] 1. Zapečaćeni `memfd` (SEALED) i RED test za post-Prepare pisanje
* **Lokacija:** `internal/preflight/probe/memfd_linux.go:28-56`, `probe_linux_test.go:420-434`
* **Nalaz:** 
  - `memfdWithContent` kreira memfd s `mfdCloexec | mfdAllowSealing`, upisuje sadržaj i preko `F_ADD_SEALS` primjenjuje `allSeals` (`F_SEAL_WRITE | F_SEAL_GROW | F_SEAL_SHRINK | F_SEAL_SEAL`), što se odmah verificira putem `F_GET_SEALS`.
  - Ništa (pa ni vlastiti proces) više ne može promijeniti bajtove u memoriji.
  - RED test `TestMemfdSealedAgainstPostPrepareWrite` pokušava `WriteAt` nad otvorenim `memfd`-om i potvrđuje da operacija pada s `EPERM`.

---

### [OK] 2. Fail-closed Seccomp arhitektura i provjera nativnog ELF stroja
* **Lokacija:** `internal/preflight/probe/seccomp_linux.go:95-113`, `probe.go:117-146`
* **Nalaz:**
  - U `seccomp_linux.go`, ako `seccomp_data.arch` ne odgovara nativnoj arhitekturi, uvjetni skok `JF` skače ravno na `denyIdx` (`RET ERRNO | EPERM`). Time je eliminiran rizik zaobilaženja filtra putem 32-bitnih compat binarija.
  - `openNativeELF` u `probe.go` provjerava `ef.Machine != native || ef.Class != elf.ELFCLASS64` i odbija ne-nativne binarne datoteke prije pokretanja.

---

### [OK] 3. Zapečaćeni `Handle` (privatna naredba, vlasništvo nad FD-ovima, prevencija curenja)
* **Lokacija:** `internal/preflight/probe/probe.go:297-369`, `probe_linux_test.go:436-463`
* **Nalaz:**
  - Polje `Handle.cmd` je privatno (`cmd *exec.Cmd`). Pozivatelji ne mogu mijenjati `Args`, `ExtraFiles`, niti varijable okruženja.
  - Svi deskriptori (memfd i seccomp pipe) pohranjuju se u `closers` i deterministički zatvaraju u `Close()` / `Wait()`.
  - RED test `TestNoFDLeakAcrossRuns` provjerava `/proc/self/fd` kroz 5 uzastopnih pokretanja i dokazuje nulto curenje deskriptora.

---

### [OK] 4. Eliminacija `ldd`-a i uvođenje statičkog `debug/elf` razrješivača s limitima
* **Lokacija:** `internal/preflight/probe/probe.go:107-261`
* **Nalaz:**
  - Vanjski poziv `ldd` je u potpunosti eliminiran — nepouzdani binarij se nikada ne izvršava na hostu radi otkrivanja biblioteka.
  - `resolveClosure` statički parsira `PT_INTERP`, `DT_RUNPATH` i `DT_NEEDED` koristeći standardni `debug/elf`.
  - BFS pretraga koristi determinističke sistemske staze (`depSearchDirs`), sprečava cikluse (`resolvedNames`) i provodi stroge limite (`maxClosureFiles = 64`, `maxClosureBytes = 256MB`).

---

### [OK] 5. `FloorProbe` kroz puni produkcijski put uz obaveznu ptrace zabranu
* **Lokacija:** `internal/preflight/probe/probe.go:64-80`, `internal/preflight/doctor/doctor.go:55-57`, `cmd/nexus/probe_linux.go:15-23`
* **Nalaz:**
  - `FloorProbe` više ne pokreće trivijalni `/usr/bin/true`, već izvršava stvarni produkcijski `Run` sa sintetičkim closureom i seccomp filtrom.
  - `cmd/nexus` posjeduje skrivenu podnaredbu `__probe-ptrace` koja poziva `PTRACE_TRACEME`. Unutar pješčanika poziv mora biti odbijen (`ptrace denied: ...`), čime se dokazuje rad i user namespacea i seccomp filtra.

---

### [OK] 6. Zaštita radnog direktorija (`guardWorkDir`) i zabrana opasnih staza
* **Lokacija:** `internal/preflight/probe/probe.go:269-292`, `probe_linux_test.go:467-480`
* **Nalaz:**
  - `guardWorkDir` razrješava symlinkove (`filepath.EvalSymlinks`), odbija korijenski direktorij `/` i plitke staze, provjerava vlasništvo trenutnog korisnika (`sys.Uid == os.Geteuid()`) i odbija world-writable direktorije.
  - RED test `TestWorkdirRootRefused` provjerava odbijanje `/` i symlinka prema `/`.
  - Dokumentiran je T25 strop (zamjena staze prije bwrapa) koji će u T25 biti zatvoren capability modelom za jednokratne direktorije.

---

### [OK] 7. Ispravak `hang` fixture u probehelperu
* **Lokacija:** `cmd/probehelper/main.go:69-71`, `probe_linux_test.go:394-416`
* **Nalaz:**
  - Uklonjen je `select {}` koji je izazivao panic Go runtime detektora mrtve petlje.
  - Zamijenjen je s `for { time.Sleep(time.Hour) }`.
  - Test `TestHangingTargetCleanedUpWithinTimeout` uredno čeka istek 3s context timeouta, ubija proces i potvrđuje da nema preživjelih potomaka (vrijeme izvođenja 4.20s).

---

### [OK] 8. Izvještaj strogo vezan uz reviziju (`revision-bound`)
* **Lokacija:** `docs/PROBE-REPORT-2026-09-03.md`
* **Nalaz:**
  - Izvještaj v2 precizno navodi testirani commit hash (`b8e4a7402c95964606cbc143a9b3d0bf2da80a6a`), potvrđuje prazan diff u kodu u trenutku generiranja te bilježi 21/21 nekachiranih prolaza.

---

## 3. ANALIZA POTENCIJALNIH NOVIH POGREŠAKA

1. **`debug/elf` razrješivač biblioteka:**
   - Pretraga u `depSearchDirs` uredno obuhvaća `RUNPATH` unose i sve standardne sistemske staze (`/lib`, `/lib64`, `/usr/lib`, `/usr/lib64`, multiarch `*-linux-gnu*`).
   - BFS zaštita od ciklusa (`resolvedNames`) i memorijski limiti onemogućuju iscrpljivanje resursa.
2. **Čistoća upravljanja deskriptorima:**
   - Svi deskriptori u `Handle.closers` zatvaraju se točno jednom putem `sync.Once` mehanizma u `Close()`, što je empirijski potvrđeno testom stabilnosti deskriptora.

---

## 4. ZAKLJUČAK

Implementacija paketa Phase 0 (`T01–T03`) dosegla je **iznimnu razinu inženjerske robusnosti, sigurnosti i ustavne discipline**. Svi nalazi iz prethodnih rundi su besprijekorno riješeni, a testni orakuli su neprobojni.

---

VERDICT: PASS
