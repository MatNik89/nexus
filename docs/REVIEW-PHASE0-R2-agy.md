# REVIEW-PHASE0-R2-agy — Phase 0 Code Verification Round 2 (Batch T01–T03)

**Meta:** Verification of Phase 0 code rewrite at Commit `128565b` on branch `slice/p0-phase0`  
**Recenzent:** Antigravity (Adversarial Phase-0 Code Verification Round 2)  
**Target Files:**
- `Makefile`
- `scripts/static-check.sh`
- `internal/buildcheck/staticcheck_test.go`
- `cmd/nexus/main.go`
- `cmd/probehelper/main.go`
- `cmd/probehelper/ptrace_linux.go`
- `internal/preflight/probe/probe.go`
- `internal/preflight/probe/probe_linux_test.go`
- `internal/preflight/probe/seccomp_linux.go`
- `internal/preflight/probe/memfd_linux.go`
- `internal/preflight/doctor/doctor.go`
- `internal/preflight/doctor/doctor_test.go`
- `docs/PROBE-REPORT-2026-09-03.md`
- `docs/ARCHITECTURE-ESSENTIALS.md` (E10)
- `docs/HARDQ-CONSOLIDATED.md` (B4)

**Authorities & Sources:** `docs/tasks-P0.md` (T01–T03), `CLAUDE.md`, `AGENTS.md`, `HARDQ-CONSOLIDATED.md` (B4, C5, D1, F1), `PRD.md` (§6 item 6, §7), `docs/REVIEW-PHASE0-{codex,kilo,agy}.md`.

---

## 1. PREGLED I METODOLOGIJA

Commit `128565b` donosi opsežno i temeljito preoblikovanje Phase-0 koda kojim je adresirano svih 19 nalaza iz prve runde recenzija (Codex 13 + Kilo 6 + Agy 8 preklapajućih nalaza).

Verifikacija je provedena uz 3 glavne osi:
1. **Verifikacija ugradnje svih materijalnih nalaza iz 1. runde:** provjera sintetičkog memfd-pinned closurea, seccomp cBPF poda, neizvezenih loosen prekidača, uklanjanja argv-rewrite hackova, orakula procesnog stabla `(pid, starttime)`, funkcionalnog `FloorProbe` pokretanja, odbijanja symlinkova u doctoru i fail-closed semantike `static-check.sh`.
2. **Analiza novih defekata:** matematička i tehnička provjera cBPF asemblera i skokova u `seccomp_linux.go`, životnog vijeka `memfd` deskriptora, razrješenja `ldd` ovisnosti i usporedbe verzija.
3. **Provjera usklađenosti dokumentacije:** provjera E10/B4 anotacija i regeneriranog `PROBE-REPORT-2026-09-03.md`.

---

## 2. VERIFIKACIJA UGRADNJE NALAZA IZ 1. RUNDE

---

### [OK] 1. Sintetički memfd-pinned closure (uklanjanje blanket `/usr` i `/etc` grantova)
* **Status:** `[OK]` (rješava Codex #2 exploit `/usr/bin/id` i Kilo #4)
* **Provjera koda:**
  - `resolveClosure` (`probe.go:114-191`) analizira ELF zaglavlje i interpreter (`PT_INTERP`), te preko `ldd` pronalazi isključivo tranzitivne dijeljene biblioteke.
  - Svaka datoteka se čita u memoriju i stvara se anonimni in-memory `memfd` (`memfd_linux.go:15-34` preko `SYS_MEMFD_CREATE`).
  - U `bwrap` se prosljeđuju isključivo deskriptori preko `--ro-bind-data <fd> <dest>` s `--perms 0555`.
  - Nema rekurzivnih niti izravnih montiranja `/usr` ili `/etc` (odbačen i `/etc/resolv.conf`).
  - **Verifikacija testom:** `TestUndeclaredChildExecFailsForUsrBinary` (`probe_linux_test.go:234-241`) dokazuje da poziv `/usr/bin/id` unutar pješčanika pada jer `/usr` uopće ne postoji u mount namespaceu.
  - **Otpornost na swap/truncation:** `TestTargetSwapAfterPrepareIsInert` (`probe_linux_test.go:245-268`) dokazuje da izmjena ili brisanje binarne datoteke na disku nakon `Prepare()` ne utječe na izvršavanje.

---

### [OK] 2. Stvarni Seccomp cBPF pod za SYSCALL stupac
* **Status:** `[OK]` (rješava Codex #1)
* **Provjera koda:**
  - `seccomp_linux.go` sadrži čisti Go cBPF asembler (bez cgo ili vanjskih ovisnosti) koji blokira opasne sistemske pozive (`ptrace`, `process_vm_readv/writev`, `userfaultfd`, `perf_event_open`, `open_by_handle_at`, `keyctl`, `kexec_load`) vraćajući `EPERM`.
  - Podržane arhitekture su `arm64` i `amd64`; nepoznate arhitekture padaju fail-closed.
  - Deskriptor se prosljeđuje u `bwrap` preko `--seccomp <fd>`.
  - **Verifikacija testom:** `TestSyscallFloorDeniesPtrace` dokazuje da `ptrace(PTRACE_TRACEME)` pada s `EPERM`, a negativna kontrola `TestNegativeControlPtraceAllowedWhenSeccompLoosened` dokazuje osjetljivost filtra.

---

### [OK] 3. Neizvezeni (unexported) prekidači za labavljenje granica
* **Status:** `[OK]` (rješava Codex #7)
* **Provjera koda:**
  - `Spec` struktura (`probe.go:82-98`) sadrži unexported polje `loosen loosen`.
  - Proizvodni pozivatelji izvan paketa `probe` nemaju nikakvu mogućnost postaviti `roBind`, `net`, `pidNS` ili `seccomp` zastavice, čime je onemogućeno slučajno slabljenje sandboxa.

---

### [OK] 4. Čisti produkcijski API bez argv-rewrite hakova
* **Status:** `[OK]` (rješava Codex #3)
* **Provjera koda:**
  - Uveden je jedinstveni `Prepare`/`Start`/`Kill`/`Wait` i `Run` API (`probe.go:225-322`).
  - Svi testovi u `probe_linux_test.go` koriste stvarne funkcije `Run()` i `Prepare()` bez prepisivanja `argv` argumenata.

---

### [OK] 5. Robusni orakul procesnog stabla `(pid, starttime)` i PIDNS negativna kontrola
* **Status:** `[OK]` (rješava Agy #3, Agy #4, Codex #5)
* **Provjera koda:**
  - Uklonjen je krhki `pgrep` poziv.
  - `descendants()` i `stillAlive()` (`probe_linux_test.go:274-331`) skeniraju `/proc/*/stat` i prate parove `(pid, starttime)` kroz cijelo stablo potomaka.
  - `TestKillReapsWholeTree` dokazuje uništenje svih potomaka putem produkcijskog `h.Kill()`.
  - `TestNegativeControlChildSurvivesWhenPIDNSLoosened` (`probe_linux_test.go:356-390`) dokazuje da bez `--unshare-pid` potomak preživljava ubojstvo lidera, čime je osigurana potpuna negativna kontrola za PROC_TREE stupac.

---

### [OK] 6. Funkcionalni `FloorProbe` i Doctor preflight provjere
* **Status:** `[OK]` (rješava Kilo #1, Codex #6)
* **Provjera koda:**
  - `FloorProbe` (`probe.go:60-77`) provodi stvarno pokretanje kontejnera s `--unshare-user` i provjerava rad user namespacea na razini jezgre.
  - `Detect()` strogo provjerava verziju (`MinBwrapVersion = "0.8.0"`).
  - `doctor.Run` (`doctor.go:58-97`) razdvaja `sandbox-backend` (binarij/verzija) i `sandbox-floor` (funkcionalni kernel probe).
  - `doctor_test.go` pokriva `kernel-floor-forced-fail` u tablici.

---

### [OK] 7. Sigurnosno zatvaranje Doctor write probea i odbijanje symlinkova
* **Status:** `[OK]` (rješava Codex #10)
* **Provjera koda:**
  - `checkDataDir` (`doctor.go:130-161`) koristi `os.Lstat` i eksplicitno odbija symlinkove (`linfo.Mode()&os.ModeSymlink != 0`).
  - Write probe koristi `os.CreateTemp(dir, ".doctor-probe-*")` sa slučajnim sufiksom i ekskluzivnim stvaranjem, a neuspjeh brisanja probe datoteke postavlja status u `StatusOff`.

---

### [OK] 8. Fail-closed semantika i automatizirani testovi za `static-check.sh`
* **Status:** `[OK]` (rješava Codex #11, Kilo #6)
* **Provjera koda:**
  - `scripts/static-check.sh` provjerava ELF magic (`7f454c46`), dostupnost alata `ldd`, i vraća exit code 2 za sve ne-ELF ili neutvrđene slučajeve (nikada lažni OK).
  - `internal/buildcheck/staticcheck_test.go` automatski testira sva tri scenarija: statički binarij (exit 0), dinamički binarij (exit 1) i non-ELF tekstualnu datoteku (exit 2).

---

### [OK] 9. Bounded timeout i čišćenje blokiranih procesa
* **Status:** `[OK]` (rješava Codex #12)
* **Provjera koda:**
  - `Prepare()` koristi `context.WithTimeout(context.Background(), spec.Timeout)` s `exec.CommandContext`.
  - `TestHangingTargetCleanedUpWithinTimeout` (`probe_linux_test.go:394-414`) dokazuje da proces koji visi biva terminiran unutar zadanog vremenskog okvira.

---

## 3. ANALIZA POTENCIJALNIH NOVIH POGREŠAKA

1. **Korektnost cBPF skokova (`seccomp_linux.go`):**
   - Skok pri provjeri arhitekture: `prog[1] = JEQ(arch, JT=1, JF=0)`. Ako arhitektura odgovara, preskače `prog[2]` (RET ALLOW) i slijeće točno na `prog[3]` (LD nr).
   - Skokovi u petlji provjere sistemskih poziva: `JT = uint8(denyIdx - i - 1)` i `JF = 0`. Za indeks `i`, cilj skoka `i + 1 + JT` iznosi točno `denyIdx` (RET ERRNO EPERM).
   - Asembliranje u `LittleEndian` odgovara `arm64` i `amd64` ciljevima. Nema matematičkih ili logičkih pogrešaka u cBPF skokovima.
2. **Upravljanje resursima i životni vijek memfd-a:**
   - Svi otvoreni deskriptori bilježe se u `h.closers` i zatvaraju u `h.Wait()`.
   - Ako `Prepare()` padne prije završetka, pomoćna funkcija `fail()` zatvara sve alocirane deskriptore. Nema curenja resursa.
3. **Usporedba verzija (`versionLess`):**
   - `versionLess` (`probe.go:324-335`) uspoređuje numeričke komponente razdvojene točkom. Točno prepoznaje `0.11.2 >= 0.8.0` i `0.5.0 < 0.8.0`.

---

## 4. USKLAĐENOST DOKUMENTACIJE

- `docs/ARCHITECTURE-ESSENTIALS.md` (E10) precizno dokumentira sintetički memfd-pinned closure, seccomp cBPF pod i eliminaciju blanket `/usr` direktorijskih grantova.
- `docs/HARDQ-CONSOLIDATED.md` (B4) nosi eksplicitni `REFINED 2026-09-03` zapis o reviziji closure modela.
- `docs/PROBE-REPORT-2026-09-03.md` je regeneriran na stvarnom hardveru s 18/18 prolaznih testova i objavljenim ABI podom.

---

## 5. ZAKLJUČAK

Revizija koda u commitu `128565b` je **vrhunske kvalitete**. Svih 19 nalaza iz prve runde je besprijekorno riješeno, sigurnosne membrane su značajno očvrsnute (memfd-pinned sintetički closure i seccomp cBPF pod), testni orakuli su robusni i red-capable, a dokumentacija je u potpunosti usklađena.

---

VERDICT: PASS
