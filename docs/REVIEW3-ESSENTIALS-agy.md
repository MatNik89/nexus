# REVIEW3-ESSENTIALS-agy — Targeted Verification (Round 3, Commit d419885)

**Meta:** `/home/matej/HARNESS/nexus/docs/ARCHITECTURE-ESSENTIALS.md` at commit `d419885`  
**Recenzent:** Antigravity (Targeted Verification, Round 3)  
**Izvori za usporedbu:** `HARNESS-SPEC.md`, `HARNESS-PLAN.md`, `DESIGN-FIXES-r2.md`, `DESIGN-STATUS.md`, `PRD.md`, `SECTION-MAP.md`, `PLAN-HOLES-CONSOLIDATED.md`.

---

## 1. OPSEG I CILJ VERIFIKACIJE

Verifikacija je fokusirana isključivo na 6 popravaka proizašlih iz nalaza 2. runde (codex r2 / kilo r2) i provjeru uvođenja eventualnih novih grešaka:
1. **E2 + Open list #1:** PRD odobrenje označeno kao `PENDING` (bez fabriciranog odobrenja).
2. **E4:** `AtomicWriter` ograničen na `S5.1/5.3` mutacije datoteka, SQLite-WAL izuzet.
3. **E6:** `SECTION-MAP` dependency DAG usklađen s `HARNESS-SPEC` (P0–P6) vertikalnim isporukama.
4. **E7 naslov:** Preimenovan u *"Transactional capability activation"* (umjesto two-phase).
5. **Precedence u zaglavlju:** PRD i `DESIGN-STATUS` uključeni; `HARNESS-SPEC` ograničen isključivo na Annex A ugovore uz upozorenje na zastarjele salvage reference u tijelu specifikacije.
6. **E10:** `execveat` + live-probe blokira isključivo `S6.2/arbitrary-exec` implementaciju.

---

## 2. VERIFIKACIJA PO TOČKAMA (FOLDOVI 1–6)

---

### [OK] 1. E2 + Open list #1 — PRD odobrenje označeno kao PENDING
* **Status u tekstu (E2:30-31, Open:187-188):**
  - E2 tekst: `Proposed and written into PRD v1; formal product approval is still PENDING (PRD:66 "awaits your confirmation"; DESIGN-STATUS #6 OPEN — see Open list).`
  - Open list #1: `1. **PRD formal approval** — identity (one kernel/two profiles) + P0 scope await the user's sign-off (PRD:66; DESIGN-STATUS #6). Until recorded, E2 is a proposal, not a decision.`
* **Ocjena:** Ispravno ugrađeno (*Folded*). Nema fabriciranja odobrenja. *(Minorna kozmetička opaska: u samom naslovu E2 linije 27 stoji `## E2 — One kernel, two profiles; assistant FIRST (decided) + no self-modification` gdje je riječ `(decided)` zaostala iz r2, no tijelo i Open lista nedvosmisleno nalažu `PENDING`.)*

---

### [OK] 2. E4 — AtomicWriter opseg i SQLite-WAL izuzeće
* **Status u tekstu (E4:50-55):**
  - `Scope limit (do not over-read): workspace files, memory spine, backups are separate durable side-effects behind their effect-path owners. File/workspace mutations (S5.1/5.3 scope, P0.11) use AtomicWriter (tmp → fsync → rename, never in-place) with a snapshot taken BEFORE every FS effect... Persistence engines (SQLite-WAL spine) provide their own durable atomicity contract — do NOT wrap per-record writes in rename.`
* **Ocjena:** E4 sada precizno razgraničava mutacije datoteka na disku (S5.1/5.3) od SQLite-WAL spine-a koji ima vlastiti ACID ugovor.

---

### [OK] 3. E6 — Usklađenje SECTION-MAP DAG-a i HARNESS-SPEC vertikalnih isporuka
* **Status u tekstu (E6:68-77, Open:192-193):**
  - E6 tekst: `SECTION-MAP §1 (K0 → K1 → L → M–P) is the dependency DAG — topological constraints on what must exist before what... HARNESS-SPEC "Redoslijed gradnje" (P0–P6) is the vertical delivery-slice plan — what each shippable slice contains... Invariants that hold either way: S16.6-det minimal checker is a K0/P0-phase primitive; sandbox (S6.2) lands BEFORE tools/exec (S4). Residual ordering conflicts → hard-questions.`
  - Open list #4: `4. **Build-order reconciliation** (E6) — SECTION-MAP DAG vs HARNESS-SPEC slices; confirm in hard-questions, materializes in tasks-P0.md.`
* **Ocjena:** Razriješena je konceptualna kolizija uloga dvaju dokumenata: SECTION-MAP je topološki DAG ovisnosti, a SPEC definira vertikalne isporuke (slices). Preostali rubni konflikti otvoreni su za hard-questions.

---

### [OK] 4. E7 — Naslov: "Transactional capability activation"
* **Status u tekstu (E7:79):**
  - `## E7 — Transactional capability activation (atomic activation is impossible)`
* **Ocjena:** Naslov točno odražava mehanizam (3-step transakcija: Prepare/Commit/Activate s RollbackTokenom).

---

### [OK] 5. Precedence u zaglavlju — PRD, DESIGN-STATUS i HARNESS-SPEC Annex A
* **Status u tekstu (Header:8-14):**
  - `**Source precedence (when documents disagree):** PRD.md owns product decisions (identity/scope — the user adjudicates) > **HARNESS-SPEC Annex A contracts** (the contracts only — the SPEC body still carries stale salvage references that the greenfield decision C1/HARNESS-PLAN:3 deleted; never revive them via "SPEC wins") > DESIGN-FIXES-r2 / DESIGN-*.md (they supersede named older parts) > DESIGN-STATUS (dated status ledger) > HARNESS-PLAN.md > SECTION-MAP / PLAN-HOLES (some claims stale — e.g. PLAN-HOLES C3 predates Annex A P0.5–P0.13, which EXIST at HARNESS-SPEC:1534+).`
* **Ocjena:** Redoslijed prvenstva je potpun, robustan i eksplicitno sprječava oživljavanje obrisanog salvage koda kroz pozivanje na tijelo SPEC-a.

---

### [OK] 6. E10 — `execveat` i preciziranje dosega blokade live-probea
* **Status u tekstu (E10:104-110, Open:189-190):**
  - E10 tekst: `ABI probe → no_new_privs → Landlock ruleset → per-arch seccomp-BPF → irreversible execveat... The hostile conformance suite on a real Linux kernel is a go/no-go preflight BEFORE production S6.2/arbitrary-exec implementation (other P0 work may proceed; nothing may RELY on the sandbox boundary before the probe passes)...`
  - Open list #2: `2. **Linux hostile-conformance preflight** on a real kernel — go/no-go before production S6.2/arbitrary-exec implementation (E10).`
* **Ocjena:** Zamijenjen `execve` s `execveat` (prema `DESIGN-S0-sandbox-codex.md:1169`), a preflight je precizno definiran kao blokada za S6.2/arbitrary-exec, omogućujući paralelni razvoj neovisnih P0 modula (S0, journal, config).

---

## 3. PROVJERA NOVIH GREŠAKA (NEW-ERROR AUDIT)

* Niti jedna od 6 izmjena nije unijela novu arhitektonsku, kriptografsku ili normativnu grešku.
* Reference na Annex A ugovore (`P0.1`, `P0.3`, `P0.4`, `P0.5`, `P0.11`, `P1.4`, `P1.6`, `P2.1`) ostaju 100% točne.

---

## 4. ZAKLJUČAK

Svih 6 zadanih točaka iz 2. runde je **ispravno i precizno integrirano**. Dokument [`ARCHITECTURE-ESSENTIALS.md`](file:///home/matej/HARNESS/nexus/docs/ARCHITECTURE-ESSENTIALS.md) je u potpunosti spreman kao čista, neproturječna dnevna referenca.

---

VERDICT: PASS
