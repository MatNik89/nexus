# REVIEW-PHASE0-agy — Phase 0 Code Review (Batch T01–T03)

**Meta:** Adversarial code review of branch `slice/p0-phase0` at Commit `6fc4b1e`  
**Recenzent:** Antigravity (Adversarial Phase-0 Code Review)  
**Target Files:**
- `Makefile`
- `scripts/static-check.sh`
- `cmd/nexus/main.go`
- `cmd/probehelper/main.go`
- `internal/preflight/probe/probe.go`
- `internal/preflight/probe/probe_linux_test.go`
- `internal/preflight/doctor/doctor.go`
- `internal/preflight/doctor/doctor_test.go`
- `docs/PROBE-REPORT-2026-09-03.md`

**Contracts & Authorities:** `docs/tasks-P0.md` (T01–T03), `CLAUDE.md`, `AGENTS.md`, `HARDQ-CONSOLIDATED.md` (B4, C5, D1, F1), `PRD.md` (§6 item 6, §7).

---

## 1. PREGLED I METODOLOGIJA

Implementacija prve tri P0 cjeline (T01 Repo scaffold, T02 Hostile conformance suite v0 i T03 Nexus doctor preflight) provjerena je statičkom analizom, pregledom koda i pokretanjem testova nad stvarnim deployment hostom.

Ispitivanje je obuhvatilo:
1. Dokazuju li hostile testovi doista PRD §6 item 6 granice ili mogu proći vakuozno?
2. Sigurnosne aspekte `bwrap` konfiguracije (zatvaranje, `/etc` grantovi, `--new-session`, čišćenje okruženja).
3. Fail-closed mehanizme u `doctor` preflightu (TOCTOU, curenje tajni, rukovanje neispravnim preduvjetima).
4. Kvalitetu Go koda i ustavna pravila (odsutnost `map[string]any`, izolacija testnih fixtura).

---

## 2. NALAZI PO KATEGORIJAMA

---

### [OK] Nalaz 1 — T01: Repo scaffold, `CGO_ENABLED=0` i statička verifikacija
* **Lokacija:** `Makefile`, `scripts/static-check.sh`, `cmd/nexus/main.go`
* **Analiza:**  
  - `Makefile` dosljedno provodi `CGO_ENABLED=0 go build -trimpath -o bin/nexus ./cmd/nexus`.
  - Skripta `scripts/static-check.sh` provjerava `ldd` nad izlaznim binarijem i pada (exit 1) ako binarij posjeduje dinamičke biblioteke.
  - Paketni layout prati `DESIGN-S0` strukturu. `go vet ./...` i `make check` prolaze čisto.

---

### [OK] Nalaz 2 — T02: Hostile Conformance Suite v0 i objavljeni ABI pod
* **Lokacija:** `internal/preflight/probe/probe.go`, `docs/PROBE-REPORT-2026-09-03.md`
* **Analiza:**  
  - `probe.Detect()` strogo pada s `SANDBOX_CAPABILITY_UNAVAILABLE` ako `bwrap` nije na `PATH`-u.
  - `docs/PROBE-REPORT-2026-09-03.md` uredno bilježi 10/10 prolaznih testova i objavljuje minimalni kernel/ABI pod (Linux s unprivileged user namespaces + bubblewrap >= 0.11 per HARDQ C5).

---

### [RED] Nalaz 3 — Nedostaje negativna kontrola za PROC_TREE / PIDNS granicu
* **Lokacija:** `internal/preflight/probe/probe_linux_test.go:210-258`
* **Analiza:**  
  - Ugovor za T02 u `docs/tasks-P0.md:34-39` eksplicitno zahtijeva:  
    `negative controls per boundary, HAZARD-SAFE: FS control reads a non-secret CANARY... net control reaches test-owned local sink... process control escapes into a test-owned process tree. Each loosened profile MUST turn its boundary check RED.`
  - `probe.Spec` ima definiran prekidač `LoosenPIDNS bool`, i postoje negativne kontrole za FS (`TestNegativeControlCanaryReadableWhenROLoosened`) i NET (`TestNegativeControlDialSucceedsWhenNetLoosened`).
  - Međutim, test negativne kontrole za `LoosenPIDNS` (koji bi dokazao da bez `--unshare-pid` i `--die-with-parent` proces potomak preživljava SIGKILL roditelja) **nije implementiran**.
* **Preporuka za popravak:** Dodati `TestNegativeControlProcessEscapesWhenPIDNSLoosened` koji s `LoosenPIDNS: true` provjerava da potomak ostaje živ, čime se dokazuje osjetljivost testa za PROC_TREE.

---

### [RED] Nalaz 4 — Suzbijanje greške u `pgrep` pozivu unutar `TestKillReapsWholeTree`
* **Lokacija:** `internal/preflight/probe/probe_linux_test.go:204-207`
* **Analiza:**  
  - U `TestKillReapsWholeTree` linija 204 glasi:  
    `out, _ := exec.Command("pgrep", "-f", filepath.Base(hp)).Output()`
  - Ako `pgrep` nije instaliran na hostu ili poziv padne s greškom koja nije exit 1 (npr. binary not found), `out` ostaje prazan `""`, `len(strings.TrimSpace(string(out))) == 0`, i test **lažno prolazi (vakuozni prolaz)**!
* **Preporuka za popravak:** Eksplicitno provjeriti tip greške: ako je `err != nil`, dopustiti isključivo `*exec.ExitError` s exit codeom 1 (što za `pgrep` znači "nema procesa"). Ako je `err` bilo što drugo (npr. `exec.ErrNotFound`), test mora pasti s `t.Fatalf("pgrep failed: %v", err)`.

---

### [QUALITY] Nalaz 5 — Krhko indeksiranje argumenata u `TestKillReapsWholeTree`
* **Lokacija:** `internal/preflight/probe/probe_linux_test.go:191-193`
* **Analiza:**  
  - Dok `runBound` koristi robusnu petlju unatrag za pronalaženje `hostHelper` argumenta, `TestKillReapsWholeTree` koristi fiksni indeks:  
    `argv[len(argv)-3] = "/work/" + filepath.Base(hp)`
  - Iako u trenutnoj konfiguraciji s dva argumenta (`spawn-sleep`, `30`) indeks `-3` slučajno pogađa ciljani binarij, bilo kakva promjena broja zastavica ili argumenata može tiho prepisati pogrešan argument.
* **Preporuka za popravak:** Refaktorirati u istu funkciju/petlju pretraživanja kao u `runBound`.

---

### [OK] Nalaz 6 — Sigurnost konstrukcije bwrap naredbe i minimalno zatvaranje
* **Lokacija:** `internal/preflight/probe/probe.go:74-118`
* **Analiza:**  
  - Primijenjen je B4 minimalni read-only closure (`/usr`, `/bin`, `/lib*`, `/sbin` kao symlinkovi prema `/usr`).
  - `/etc` se **nikada ne montira rekurzivno** — montiraju se isključivo pojedinačne datoteke (`/etc/ld.so.cache`, CA certifikati, `/etc/resolv.conf`).
  - Čišćenje okruženja (`--clearenv --setenv PATH /usr/bin:/bin`) i nova sesija (`--new-session`) sprječavaju ubacivanje okruženja i TIOCSTI terminalne napade.
  - Provjera `isELF` prije bwrapa odbija shebang skripte na razini launchera.

---

### [OK] Nalaz 7 — T03: Doctor preflight fail-closed i nula curenja tajni
* **Lokacija:** `internal/preflight/doctor/doctor.go`, `doctor_test.go`
* **Analiza:**  
  - Rezultati su strogo capability-scoped (izostanak bwrapa gasi `exec`, izostanak ključa gasi `conversation`, izostanak tokena gasi `telegram`, neispravan direktorij blokira `stateful-startup`).
  - Nema curenja tajni: `Check.Detail` za postavljene ključeve ispisuje samo `"set"`.
  - Provjera prava nad `DataDir` odbija sve permisije osim `0700` (`info.Mode().Perm()&0o077 != 0`).
  - Finalna oznaka je ispravno `prerequisites-ready` (nikad preuranjeni `P0-capable`).

---

### [OK] Nalaz 8 — Kvaliteta Go koda i ustavna disciplina
* **Analiza:**  
  - Nema `map[string]any`.
  - Svi testovi koriste `t.TempDir()` i posjeduju svježe privremene armature bez dijeljenja stanja.
  - `doctor.Env` omogućuje čistu injekciju ovisnosti za testnu matricu bez onečišćenja globalnog `os.Environ`.

---

## 3. ZAKLJUČAK

Implementacija paketa Phase 0 (`T01–T03`) je **izuzetno visoke kvalitete, sigurna i arhitektonski vjerna**.  
Nalazi [RED-3], [RED-4] i [QUALITY-5] predstavljaju manje propuste u testnom orakulu (nedostajući PIDNS negative control i rukovanje `pgrep` greškom), ali ne narušavaju integritet same bwrap sigurnosne membrane.

---

VERDICT: PASS
