# ARHITEKTONSKI VERDIKT — NEXUS HARNESS
**Dokument:** `docs/VERDICT-agy.md`  
**Datum:** 2026-09-02  
**Autor:** Antigravity (agy) — DeepMind AAC Agentic Systems Architect  
**Kontekst:** Greenfield Go (`CGO_ENABLED=0`), nula salvagea, 108 podsekcija (S0–S18.6).  
**Referentni dokumenti:** `/home/matej/HARNESS/nexus/docs/HARNESS-PLAN.md`, `/home/matej/HARNESS/nexus/docs/SECTION-MAP.md`, `/home/matej/HARNESS/nexus/docs/DESIGN-*.md`, `/home/matej/HARNESS/nexus/docs/DESIGN-FIXES-r2.md`.

---

## 1. TEMELJNI VERDIKT: DA LI BIH JA OVAKO GRADIO?

> **DA — U VELIKIM ODLUKAMA SE POTPUNO SLAŽEM.**  
> Ovaj arhitektonski nacrt je **jedan od najrigoroznije definiranih sustava za AI agente na tržištu**. Eliminira 90% dječjih bolesti koje muče postojeće open-source harnesse (Python dependency-hell, curenje memorije, nesandboxirane bash injekcije, nekontrolirane retry petlje i sycophancy lažnih "uspjeha").

Ako bih gradio autonomni AI harness od nule za produkcijsku upotrebu, **arhitektonska kralježnica bila bi identična**:

1. **Greenfield Go (`CGO_ENABLED=0`) single-binary:**  
   Apsolutno ispravan izbor. Gradnja u Pythonu je taktička zamka (500MB+ RSS, spori startup, nepouzdano upravljanje stablom podprocesa, pakiranje ovisno o okruženju). Go nudi deterministički runtime, instantni startup (<50ms), 20–30MB idle memorije, superiornu konkurentnost (goroutines/channels) i čisti `modernc.org/sqlite` ukomponiran u jedan jedini binarac.
2. **Jedna jezgra, dva profila (`AssistantProfile` / `CodingProfile`):**  
   Puno čišće nego graditi dva odvojena alata ili silovati chat-bota u programera. TurnLoop, Tool Registry, Context Window Manager, Event Journal i Sigurnosna Membrana su 100% zajednički kernel (S0–S9). Profili mijenjaju samo: (a) sklapanje prompta/instrukcija, (b) selekciju alata i (c) vrstu `AcceptanceContract` verifikacije.
3. **Formalni Effect-Path (`S6.0 PEP → S6.9 Lifecycle → S6.2 Sandbox → S1.2 Process → S7 Retry`):**  
   Ovo je inženjerski biser plana. Razdvajanje odluke o autorizaciji (`S6.0`), redoslijeda hookova (`S6.9`), izolacije procesa (`S6.2`), praćenja PID-a (`S1.2`) i izdavanja pokušaja (`S7`) sprječava eskalaciju privilegija, TOCTOU zamke i nekontrolirane re-askove.
4. **SQLite-WAL Memory Spine + Lossless Decay + Audn:**  
   Prizemljeno i robusno. Odbacivanje "vector-only" zabluda za temeljnu memoriju. SQLite u WAL modu pruža transakcijsku postojanost. Činjenica da Decay mijenja rang (a ne briše bajtove) čuva dokaznu sljedivost, a `MEMORY_FORGET` vs `DATA_PURGE` rješava stvarne GDPR/compliance zahtjeve.
5. **4 Stvarna Diferencijatora:**  
   - **TIA (Test Impact Analysis):** Skraćuje in-turn verifikaciju s 3 minute na 3 sekunde.
   - **Evidence-Graded Gate:** Worker ne ocjenjuje sam sebe; dokaz (diff, exit code, potpisani receipt) je jedini completion oracle.
   - **AST Symedit:** Refaktoring i preimenovanje simbola bez tekstualnih halucinacija.
   - **Forget vs Purge:** Arhivsko skrivanje nasuprot kriptografskom/fizičkom uništenju podataka.
6. **DAG Faza (K0 → K1 → L → M → N → O → P):**  
   Poredak je nakon Iteracije 4 postao čisti aciklički graf. Počinje od tipova podataka (K0) i sigurnosti/filesystema (K1) prije bilo kakve izvršne petlje (L).

---

## 2. ŠTO BIH REZAO (Over-engineering) vs ŠTO BIH DODAO

### A. Što bih brutalno izbacio / odgodio za v2 (Over-engineering):
1. **Learned-Bandit Router (`S2.6`):**  
   Učenje rutiranja modela putem bandit algoritma s reinforcement nagradama unosi stohastičku nestabilnost u ranu fazu. **Reži za P0/P1.** Deterministički *Barbell Router* (jeftini brzi model za draft/explore + frontier model za plan/verify) rješava 95% potreba bez ikakvog "učenja".
2. **BSP/Pregel Superstep Multi-Agent (`S12.2`) & Council (`S12.6`):**  
   Za P0/P1 dovoljna je hijerarhijska delegacija podagentima u izoliranim git worktreejima (`S5.2`). Grafovi sa sinkronizacijskim superstepovima i multi-persona vijeća su fascinantni na papiru, ali u praksi dramatično povećavaju token spend i latenciju.
3. **GraphRAG (`S10.4`) i CAG Preload (`S10.5`):**  
   Hibridni BM25 + dense retrieval nad SQLite-om (`10.3`) je 10x pouzdaniji i predvidljiviji za lokalni rad. GraphRAG zahtijeva skupe offline LLM ekstrakcije koje guše lokalni CPU/RAM.

### B. Što bih dodao / stavio u najviši prioritet:
1. **Prvoklasni Bubbletea TUI (`S14.1`):**  
   Programeri i korisnici provode 99% vremena gledajući u terminal. Interaktivni prikaz diffova s bojama, live streaming misli, promptovi za odobrenje (ASK) s jasnim diff previewom i glatko renderiranje moraju biti u P0.
2. **Hostile Conformance Test Suite:**  
   Budući da pišemo sigurnosnu membranu (`Landlock/seccomp`) od nule u Go-u, nužan je skup malicioznih testnih alata koji pokušavaju: symlink escape, čitanje `/etc/shadow`, fork-bombu, DNS rebinding i SSRF prema cloud metapodacima.

---

## 3. NAJVEĆI RIZIK I NAJSLABIJA KARIKA SUSTAVA

### 🔴 NAJSLABIJA KARIKA: **Asimetrija Sandboxa među Operacijskim Sustavima**

* **Zašto je ovo najveći rizik:**
  - Na **Linuxu**, Go ima savršen kernel mehanizam: `Landlock + seccomp-BPF` preko `golang.org/x/sys/unix` bez ijednog vanjskog procesa i bez root ovlasti.
  - Na **macOS-u i Windows-u**, stabilan, neprivilegirani, single-binary sandbox API koji može dinamički izolirati proizvoljni CLI proces **praktički ne postoji bez eksternih servisa ili root/admin prava** (Seatbelt je privatni/nestabilni Apple API; Windows Job Objects ne brane filesystem/mrežu, a AppContainer zahtijeva kompleksne ACL-ove).
* **Posljedica za proizvod:**  
  Ako plan obeća *"isti čvrsti sandbox na sva 3 OS-a u jednom Go binarcu"*, to je tehnički neostvarivo bez vanjskih ovisnosti (poput Dockera ili WSL-a).
* **Pragmatična preporuka za PRD:**  
  Biti radikalno iskren u specifikaciji:
  - **Linux:** Tier-1 Enforced Kernel Sandbox (Landlock+seccomp).
  - **macOS / Windows:** Tier-2 Developer Mode (eksplicitni korisnički consent + Process/Job limit + Egress filter proxy), uz jasno upozorenje da je puna izolacija dostupna putem Linux/Docker backend-a.

---

## 4. ZAKLJUČAK

Plan je nakon 9 krugova red-team brušenja doveden u stanje **inženjerske perfekcije na razini specifikacije**. Svi tipovi, ugovori, vlasništva i redoslijedi su zaključani.

> **PREPORUKA:** **Zatvoriti fazu analize i odmah preći na `docs/PRD.md` (odluka o opsegu za P0) i generiranje početnog Go kostura (`scaffold`).**
