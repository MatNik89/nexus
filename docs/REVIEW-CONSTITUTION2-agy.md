# REVIEW-CONSTITUTION2-agy — Constitution Verification Round 2 (Commit HEAD)

**Meta:** Verification of rewritten repository constitution files (`CLAUDE.md` and `AGENTS.md`)  
**Recenzent:** Antigravity (Adversarial Constitution Re-Verification)  
**Authority & Sources:** `docs/ARCHITECTURE-ESSENTIALS.md`, `docs/HARDQ-CONSOLIDATED.md`, `docs/HARNESS-SPEC.md` Annex A, `docs/PRD.md`, `docs/REVIEW-CONSTITUTION-{codex,kilo,agy}.md`.

---

## 1. PREGLED I METODOLOGIJA

Ustavne datoteke repozitorija ([`CLAUDE.md`](file:///home/matej/HARNESS/nexus/CLAUDE.md) i [`AGENTS.md`](file:///home/matej/HARNESS/nexus/AGENTS.md)) provjerene su nakon opsežne prerade s ciljem provjere:
1. Jesu li svi materijalni nalazi iz 1. runde recenzija (`codex`, `kilo`, `agy`) ispravno ugrađeni (*folded*) ili eksplicitno odbačeni uz obrazloženi razlog i provenijenciju.
2. Je li nova sekcija **"P0 scope guards"** 100% usklađena s `HARDQ-CONSOLIDATED.md`.
3. Jesu li izmjene uvele ikakve **nove pogreške ili proturječja**.

---

## 2. AUDIT NALAZA IZ 1. RUNDE PO STAVKAMA

---

### A. Rješavanje nalaza recenzenta Codex (`REVIEW-CONSTITUTION-codex.md`)

* **[OK] Codex #1 — Nepostojeći `docs/tasks-P0.md` kao jedini task queue:**  
  - *Rješenje:* `CLAUDE.md:18-19` i `AGENTS.md:71-72` eksplicitno dodaju uvjet: `"docs/tasks-P0.md — the task ledger, once created. Until it exists, no P0 implementation is authorized; doc/design work proceeds under explicit user tasks"`. Paradoks kokoši i jajeta je uklonjen.
* **[OK] Codex #2 — `GAPFIX-*` pogrešno tretiran kao owner-dokument:**  
  - *Rješenje:* `CLAUDE.md:14-17` i `AGENTS.md:11-13` izričito nalažu: `"Raw GAPFIX-* files are NOT owners — a GAPFIX clause binds only where a higher-ranked current owner incorporates it (several GAPFIX details are already superseded)"`.
* **[OK] Codex #3 — Preširoko pravilo o odbijanju shema:**  
  - *Rješenje:* `CLAUDE.md:42-44` i `AGENTS.md:27-28` točno preciziraju split: nepoznati enum/kind/capability se odbija odmah; nepoznati schema ID/version se u P0 odbija neobrađen uz očuvanje sirovog unosa, dok v2 sloj donosi upcast + karantenu (HARDQ C7).
* **[OK] Codex #4 — English-only pravilo i provenijencija:**  
  - *Status:* Odbijeno kao "izmišljeno" jer se radi o izravnoj korisničkoj direktivi od 2026-09-03. U `CLAUDE.md:8` i `AGENTS.md:6` dodana je eksplicitna provenijencijska anotacija: `Language (explicit user directive, 2026-09-03 — repo convention, not architecture)`. Anotacija u potpunosti zatvara nalaz.
* **[OK] Codex #5 — Obvezni 3-agent review i rekurzivni prekid:**  
  - *Status:* Uvrštena korisnička provenijencija (`CLAUDE.md:92`) uz dodanu klauzulu o **bazičnom slučaju (non-recursive base case)** u `CLAUDE.md:96-98`: review datoteke, konsolidacijski dokumenti i fold commitovi su *ishodi* (outputs) review vrata, a ne novi gated deliverablei.
* **[OK] Codex #6 — Paket workflow pravila (TDD, fixture, Git):**  
  - *Status:* Uvrštena provenijencija korisničke prakse (`user's cross-project discipline: topknot`), a lista tagova za review u `AGENTS.md:65-68` generalizirana je kako bi podržala sve zadatkom specificirane tagove.
* **[OK] Codex #7 — Preusko pravilo o korisničkom sign-offu:**  
  - *Rješenje:* `CLAUDE.md:28-30` precizira da je korisnički sign-off nužan samo pri izmjeni PRD odluka, korisnički usvojenih HARDQ stavki ili sigurnosnih kriterija, dok implementacija koja čuva invarijante ide kroz standardni task/review proces.
* **[OK] Codex #8 — Nedostatak exact-intent pravila za odobrenja:**  
  - *Rješenje:* Uvršteno u `CLAUDE.md:35-38` i `AGENTS.md:23-25`: jednokratni, vremenski ograničeni token vezan uz profil, kanonsko ime alata, argumente, resurs i `(device,inode)` za destruktivne FS operacije (HARDQ C4).
* **[OK] Codex #9 — Granica oporavka za UNKNOWN efekte i zabrana slijepog retryja:**  
  - *Rješenje:* Uvršteno u `CLAUDE.md:39-41` i `AGENTS.md:26`: `UNKNOWN effect → RECONCILING`; zabrana slijepog retryja ireverzibilnih/nepoznatih ishoda (E9).
* **[OK] Codex #10 — Poštena granica Telegram dostave (at-least-once):**  
  - *Rješenje:* Uvršteno u `CLAUDE.md:57-59` i `AGENTS.md:37-39`: točno-jednom prijem, barem-jednom remote dostava, trajni inbox prije pomaka offseta, `sent-but-unrecorded → UNKNOWN → RECONCILING` (HARDQ B2).
* **[OK] Codex #11 — Povezivanje kanala s profilom prije prijema u AGENTS.md:**  
  - *Rješenje:* `AGENTS.md:34-36` sada sadrži identično pravilo: povezivanje kanala s profilom događa se PRIJE prijema (deny-default) bez kasnijeg globalnog traženja profila.
* **[OK] Codex #12 — P0 statički capability snapshot vs odgođeni Activator:**  
  - *Rješenje:* Uvršteno u `CLAUDE.md:78-80` i `AGENTS.md:52-54` (P0 scope guards).

---

### B. Rješavanje nalaza recenzenta Kilo (`REVIEW-CONSTITUTION-kilo.md`)

* **[OK] Kilo #1 — `channel:builtin` vs `channel:plugin` razdvajanje (HARDQ A1):**  
  - *Rješenje:* Uvršteno u `CLAUDE.md:68-70` i `AGENTS.md:44-45`. P0 Telegram je built-in i atestiran potpisom izdanja (P1.5), bez runtime extensions gatea.
* **[OK] Kilo #2 — Zabrana memory decay mehanizma u P0 (HARDQ B8):**  
  - *Rješenje:* Uvršteno u `CLAUDE.md:71-72` i `AGENTS.md:46-47`. P0 memorija sadrži samo eksplicitne činjenice s append-only nadomještanjem; Decay+Audn je u P1.
* **[OK] Kilo #3 — Asistentski dokazi za podsjetnike i 2 tipa obveza (HARDQ B5):**  
  - *Rješenje:* Uvršteno u `CLAUDE.md:73-75` i `AGENTS.md:48-49`. Podsjetnici se dokazuju delivery receiptom + korisničkim ackom (nikad diff/exit).
* **[OK] Kilo #4 — Trajni HITL suspend / resume (HARDQ B6):**  
  - *Rješenje:* Uvršteno u `CLAUDE.md:76-77` i `AGENTS.md:50-51`. Čekanje odobrenja bilježi `TurnSuspended` u journal i izlazi (nema in-memory blokade).
* **[OK] Kilo #5 — P0 `-min` ugovori za S7/S5 (HARDQ A2):**  
  - *Rješenje:* Uvršteno u `CLAUDE.md:81-82` i `AGENTS.md:54`. P0 koristi samo `s7-min` i `s5-min`, puni pogoni ostaju u P2/P3.
* **[OK] Kilo #6 — Izuzeće za CLIAgent provider subprocess iz sandboxa:**  
  - *Rješenje:* Uvršteno u `CLAUDE.md:45-50` i `AGENTS.md:29-32`: sandbox (bwrap) se primjenjuje na sve TOOL-executing podprocese (`ExecProcess`), dok je CLIAgent subprocess (S2.1, `--tools ""`) vođen provider granicom i egress politikom, a ne tool-sandbox `NET_DENY` pravilom.
* **[OK] Kilo #7 — Distinkcija P0.3 ugovora i B7 mehanizma (jedan append actor):**  
  - *Rješenje:* Precizirano u `CLAUDE.md:33-34` i `AGENTS.md:22` ("journal single-write-owner contract (P0.3), implemented by one serialized append actor (HARDQ B7)").

---

### C. Rješavanje nalaza recenzenta Agy (`REVIEW-CONSTITUTION-agy.md`)

* **[OK] Agy #5 — Izolacija stanja u testovima (Fresh temp fixtures):**  
  - *Rješenje:* Uvršteno i u `AGENTS.md:60-61` ("stateful tests own fresh temp fixtures (user's cross-project discipline)").

---

## 3. VERIFIKACIJA SEKCIJE "P0 SCOPE GUARDS"

U obje datoteke ([`CLAUDE.md:67-85`](file:///home/matej/HARNESS/nexus/CLAUDE.md#L67-L85) i [`AGENTS.md:43-57`](file:///home/matej/HARNESS/nexus/AGENTS.md#L43-L57)), nova sekcija **"P0 scope guards (HARDQ landmines)"** u 7 točaka dosljedno štiti P0 opseg:
1. **Telegram:** `channel:builtin` oslobođen `extensions` closurea (HARDQ A1).
2. **Memorija:** Nema decay mehanizma u P0, eksplicitne činjenice zaštićene ($S=\infty$) (HARDQ B8).
3. **Podsjetnici:** Dokaz je trajni delivery receipt + ack, nikad diff/exit (HARDQ B5).
4. **HITL:** Trajni `TurnSuspended` i rehidracija iz journala (HARDQ B6).
5. **Aktivacija:** Zapečaćeni startup snapshot, transakcijski Activator zabranjen u P0 (HARDQ B9).
6. **S7/S5:** Samo `s7-min` i `s5-min` ugovori u P0 (HARDQ A2).
7. **Stuck-detection:** Ograničen na jedan interaktivni turn, periodički poslovi izuzeti (HARDQ C3).

---

## 4. PROVJERA PROTURJEČJA I NOVIH POGREŠAKA

- **Nema novih proturječja:** Usklađenost između `CLAUDE.md` i `AGENTS.md` je 100%.
- **Granice su jasne:** Odvajanje provider podprocesa od tool sandboxa rješava potencijalni deadlock s CLIAgentom bez slabljenja sigurnosti izvršavanja alata.
- **Baza procesa:** Definiranje bazičnog slučaja za multi-agent review otklanja opasnost od beskonačne rekurzije recenzija.

---

## 5. ZAKLJUČAK

Prepisane ustavne datoteke repozitorija besprijekorno integriraju sve nalaze iz 1. runde recenzija, nose precizne provenijencijske anotacije za korisničke direktive i sadrže potpune zaštitne mehanizme protiv ponovnog uvođenja odbačenih pretpostavki.

---

VERDICT: PASS
