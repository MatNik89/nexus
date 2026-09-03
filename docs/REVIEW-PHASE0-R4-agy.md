# REVIEW-PHASE0-R4-agy — Phase 0 Code Verification Round 4 (Final Narrow)

**Meta:** Final narrow verification of Phase 0 code commit `a5aae16`, report commit `13d0282`, and YOLO doc amendment `1258cc1` on branch `slice/p0-phase0`  
**Recenzent:** Antigravity (Adversarial Phase-0 Code Final Narrow Verification)  
**Target Files:**
- `cmd/nexus/main.go`
- `cmd/nexus/probe_linux.go`
- `cmd/probehelper/main.go`
- `cmd/probehelper/ptrace_linux.go`
- `internal/preflight/probe/probe.go`
- `internal/preflight/probe/probe_linux_test.go`
- `docs/PROBE-REPORT-2026-09-03.md`
- `AGENTS.md` / `CLAUDE.md` / `docs/ARCHITECTURE-ESSENTIALS.md` / `docs/HARDQ-CONSOLIDATED.md` / `docs/PRD.md` / `docs/tasks-P0.md`

**Authorities & Sources:** `docs/tasks-P0.md` (T01–T03, T14, T17, T24), `CLAUDE.md`, `AGENTS.md`, `HARDQ-CONSOLIDATED.md` (B4, C5, D1, F1, F2), `PRD.md` (§4.6, §6 item 6, §7), `docs/REVIEW-PHASE0-R3-{codex,kilo,agy}.md`.

---

## 1. PREGLED I METODOLOGIJA

U 3. rundi recenzija Codex i Kilo su identificirali 5 preostalih točaka vezanih uz strogoću orakula FloorProbea, zaštitu radnog direktorija, ELF razrješivač i životni ciklus Handlea. Commit `a5aae16` ih u potpunosti rješava, commit `13d0282` veže v3 izvještaj, a commit `1258cc1` formalizira ustavna pravila za `--yolo` način rada.

Verifikacija je obuhvatila provjeru svih 5 ugrađenih točaka, analizu novih rubnih slučajeva u izmijenjenim linijama (`LD_LIBRARY_PATH`, jedinstvenost sentinela, granice `allowedWorkRoots`) te empirijsko testiranje na hostu (`29 probe + 3 doctor + 3 buildcheck = 35/35 PASS`).

---

## 2. VERIFIKACIJA 5 UGRAĐENIH SKUPINA ISPRAVAKA

---

### [OK] 1. `FloorProbe` egzaktni sentinel `NEXUS_PTRACE_DENIED:EPERM` i fake-bwrap RED
* **Lokacija:** `internal/preflight/probe/probe.go:64-92`, `cmd/nexus/probe_linux.go:16-22`, `cmd/probehelper/ptrace_linux.go:12-25`, `probe_linux_test.go:498-510`
* **Nalaz:** 
  - Uklonjena je labava provjera podniza `"denied"`. `FloorProbe` zahtijeva egzaktni redak `PtraceDeniedSentinel = "NEXUS_PTRACE_DENIED:EPERM"`.
  - Sentinel se emitira isključivo kada `SYS_PTRACE` poziv vrati stvarni `syscall.EPERM`.
  - RED test `TestFloorProbeRejectsPrelaunchPermissionDenied` pokreće lažni `bwrap` skript koji vraća dijagnostiku `bwrap: setup failed: Permission denied` (exit 1) i dokazuje da takav lažni prolaz biva strogo odbačen.

---

### [OK] 2. Ograničenje radnih direktorija na `TempDir`/`XDG_RUNTIME_DIR` i zabrana `0o022`
* **Lokacija:** `internal/preflight/probe/probe.go:348-392`, `probe_linux_test.go:514-536`
* **Nalaz:**
  - `allowedWorkRoots()` dopušta isključivo poddirektorije unutar `os.TempDir()` ili `XDG_RUNTIME_DIR`. Korisnički `$HOME` direktorij `/home/matej` se odbija.
  - `guardWorkDir` strogo provjerava `st.Mode().Perm()&0o022 != 0` — odbija i group-writable i world-writable direktorije (`0770`, `0775`, `0777`).
  - RED testovi: `TestWorkdirOutsideAllowedRootsRefused` (odbija `$HOME`) i `TestWorkdirGroupWritableRefused` (odbija `0770`).

---

### [OK] 3. ELF razrješivač s per-referrer `RUNPATH`/`RPATH`, `$ORIGIN` ekspanzijom i `LD_LIBRARY_PATH`
* **Lokacija:** `internal/preflight/probe/probe.go:162-261,501-521`, `probe_linux_test.go:573-617`
* **Nalaz:**
  - `expandRunpaths` zamjenjuje `$ORIGIN` i `${ORIGIN}` s direktorijem objekta koji poziva biblioteku (`referrerDir`), odbacuje prazne komponente (nema rješavanja prema cwd-u) i odbacuje nepodržane tokene (`$LIB`).
  - `elfDeps` čita `DT_RUNPATH` uz legacy fallback na `DT_RPATH`.
  - BFS pretraga (`resolveClosure`) prenosi per-referrer kontekst za svaku ovisnost.
  - `Prepare()` izvozi `--setenv LD_LIBRARY_PATH` za sve montirane direktorije biblioteka, omogućujući unutarnjem dinamičkom loaderu ispravno povezivanje `$ORIGIN` binarija na `/nexus-target`.
  - Pokriveno jediničnim testom `TestExpandRunpathsSemantics` i stvarnim e2e testom `TestOriginRunpathBinaryResolves` (kompajlira dijeljenu biblioteku i binarij s `-rpath,$ORIGIN/../lib`).

---

### [OK] 4. Proračun resursa prije alokacije (`stat`-first, `PT_INTERP` cap, dep-queue cap)
* **Lokacija:** `internal/preflight/probe/probe.go:118-123,236-250,267-277,310-313`
* **Nalaz:**
  - `add()` poziva `os.Stat` i provjerava `st.Size() <= 64MB` te ukupan limit `<= 256MB` *prije* čitanja u memoriju (`os.ReadFile`).
  - `PT_INTERP` veličina je strogo ograničena na `maxInterpLen = 4096` prije alokacije međuspremnika, a greške u `p.ReadAt` su fatalne.
  - BFS red ovisnosti ograničen je na `maxDepQueue = 256`.

---

### [OK] 5. Zapečaćeni `Handle` i odbijanje ponovnog pokretanja
* **Lokacija:** `internal/preflight/probe/probe.go:433-446,469-476`, `probe_linux_test.go:540-569`
* **Nalaz:**
  - `Start()` provjerava `if h.started || h.closed` i odbija poziv bez diranja podređenog `h.cmd` procesa, čime se čuva vlasništvo nad prvim živim procesom.
  - `Close()` postavlja `h.closed = true` pod `sync.Once`.
  - RED testovi: `TestHandleRejectsRepeatStart` (odbija drugi `Start()`, prvi proces se uredno čeka u `Wait()`) i `TestHandleRejectsStartAfterClose`.

---

### [OK] 6. Sanity-check YOLO dopune (Commit `1258cc1`)
* **Lokacija:** `AGENTS.md`, `CLAUDE.md`, `docs/ARCHITECTURE-ESSENTIALS.md`, `docs/HARDQ-CONSOLIDATED.md`, `docs/PRD.md`, `docs/tasks-P0.md`
* **Nalaz:**
  - `--yolo` način rada strogo je ograničen na pretvaranje `ASK → ALLOW` (potvrde) na lokalnom CLI-ju.
  - Sve sigurnosne membrane (`DENY`, sandbox, egress, journal, redaction, profili, zabrana samo-modifikacije) ostaju nepromijenjene i aktivne.
  - Kanalne poruke ne mogu aktivirati YOLO. Kriteriji PRD §6 vrednuju se u zadanom (default) načinu. Nema proturječja s ustavnim izvorima.

---

## 3. PROVJERA POTENCIJALNIH NOVIH DEFEKATA

1. **`LD_LIBRARY_PATH` površina napada:**  
   Vrijednost `LD_LIBRARY_PATH` u `Prepare()` sastavlja se isključivo od direktorija zapečaćenih `memfd` datoteka unutar pješčanika. Budući da se okruženje čisti (`--clearenv`), nema uvoza vanjskih ili nekontroliranih putanja.
2. **Jedinstvenost sentinela:**  
   `NEXUS_PTRACE_DENIED:EPERM` je deterministički prefiksiran i nemoguće ga je slučajno proizvesti iz standardnih Linux dijagnostika.
3. **Rubni slučajevi symlinkova u `allowedWorkRoots`:**  
   `filepath.EvalSymlinks` se poziva na samom početku `guardWorkDir`, pa se svaki symlink koji bi gađao staze izvan dozvoljenog korijena odmah razrješava u svoju kanonsku metu i biva odbačen.

---

## 4. ZAKLJUČAK

Implementacija Faze 0 (`T01–T03`) u commitu `a5aae16` / `13d0282` je **besprijekorno zaključana, sigurna i formalno verificirana**. Svih 5 točaka iz 3. runde recenzija je potpuno implementirano uz odgovarajuće RED testove.

---

VERDICT: PASS
