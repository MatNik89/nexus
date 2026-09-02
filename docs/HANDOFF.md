# HANDOFF — NEXUS greenfield harness (resume point)

**Čitaj OVO prvo pri nastavku.** Sve stanje je na disku + gitu; kontekst se smije sažeti.

## ŠTO GRADIMO
NEXUS: greenfield **Go** harness. Identitet = **univerzalni osobni multiplatform asistent**
(kao Hermes po namjeni); **coding je JEDNA grana/profil, ne svrha** (A3.1). Korisnik ga gradi jer
NE može mijenjati Hermes kod. Salvage iz NEXUSv2 (ne Hermes). Repo: **github.com/MatNik89/nexus
(private)**. Lokalno: `/home/matej/HARNESS/nexus/`. Git identitet: `269486771+MatNik89@users.noreply.github.com`.

## KLJUČNE ODLUKE (zaključane)
- Jezik **Go** (`CGO_ENABLED=0` single-binary; `modernc.org/sqlite` pure-Go spine; sandbox per-OS
  build-tag; TUI bubbletea; desktop Wails/Fyne kasnije; GORTEX Go-reuse 1:1). 3× AGREE.
- Metoda: **hibrid-hibrida po podsekciji** (merge-skill princip): 3 agenta nacrt → moderator
  Pareto-floor merge → commit.
- Repo private, **CI 3-OS na kraju**. Redoslijed do koda: **fold-gapova → PRD → hard-questions
  review → CLAUDE/AGENTS + scaffold → P0 kod.**
- Komponentna strategija: memorija=NEXUS-salvage, baza=SQLite-pure-Go, vector/RAG/OCR=ADAPTERI
  (capability-confirm PRI gradnji sekcije, NE 300 full-audita).

## DOKUMENTI (u `/home/matej/HARNESS/nexus/docs/` osim gdje piše)
- **HARNESS-PLAN.md** (1130 lin) — GLAVNI: 19 sekcija S0-S18 kanonske sinteze + A1-A9 addendumi.
  Svaka podsekcija: Tip·aspekti·Go-skice·salvage·RED·Pareto.
- **HARNESS-SPEC.md** v0.9 — normativni spec (100 podsekcija) + Annex A (17 Tier-3 ugovora s RED).
- **MASTER-LANDSCAPE.md** — who-has-what iz 29 full-code audita.
- **TIER3-CONTRACTS.md**, **NEXUS-AUDIT.md**, **TIERA-LANDSCAPE.md**.
- U `/home/matej/HARNESS/`: 29× `LA-*.md`, `HERMES-AUDIT-*.md`, `OPENCLAW-AUDIT-*.md`,
  `MASTER-SLICE-*.md`, `HOLES-CONSENSUS.md`, `IDEAS-CONSENSUS.md`.

## ADDENDUMI (A1-A9, u HARNESS-PLAN)
- A1 OCR hardware-routing (NVIDIA→Unlimited-OCR / CPU→OCRmyPDF → searchable-PDF+FTS)
- A2 local-model freshness scout (hardkodiran, llmfit) — INFORMIRA ne auto-swap
- A3 identitet=asistent-prvo + self-diagnostic→Claude-repair-handoff
- A4 Hermes gap G1-G9 (asistentske rupe)
- A5 OpenClaw gap O1-O14 (OBORIO lažnu "stuck-detection" tvrdnju)
- A6 code-verified diferencijatori (3 pala, 3 preživjela)
- A7 jcode+ruflo
- A8 FINALNI: **4 STVARNA diferencijatora** + master
- A9 fold-gapova + komponentna strategija

## 4 STVARNA DIFERENCIJATORA (nitko od 29 nema, code-provjereno):
1. Evidence-graded diff+exit-code completion GATE (checker≠worker)
2. TIA (coverage×diff→pogođeni testovi)
3. AST/LSP symedit (symbol-scoped edit)
4. Formal MEMORY_FORGET vs DATA_PURGE (P2.1)
+ KOMBINACIJA sve u jednom hardened Go single-binary asistent+coding harnessu.
OBORENO (nije unikat, ali ostaje zahtjev s referencom): stuck-detection, edit-ladder, shadow-git,
OS-sandbox, queue-fencing.

## TRENUTNO U TIJEKU (pending)
**GAPFIX rezolucija** — 3 agenta pišu `docs/GAPFIX-{codex,kilo,agy}.md` (rješavaju gapove u
buildable Go dizajn): codex=orkestracija (BSP-superstep/ingress-queue-DLQ/flows/boards/fleet+pairing),
kilo=asistent (Hermes G1-G5 computer-use/PersonalProfile/voice-runtime/ObligationStore/control-plane +
exec-auto-reviewer), agy=efikasnost/provider/plugin (jcode-RAM/AccountFleet/Cordis-plugin).
**NA RESUME:** provjeri `docs/GAPFIX-*.md`, merge u HARNESS-PLAN sekcije, commit.

## SLJEDEĆI KORACI (redom)
1. Merge GAPFIX-* u plan (gapovi RIJEŠENI, ne flagirani)
2. **PRD.md** (KORISNIK vodi — product sloj: problem/za-koga/prioriteti/scope/done-kriteriji) +
   ARCHITECTURE-ESSENTIALS.md (5-15 kritičnih odluka)
3. **HARD-QUESTIONS review** = iteracija (agenti napadaju plan: što-puknuti/edge/over-engineered) —
   OVO je i "6-datoteka-prije-kodiranja.md" korak (Desktop)
4. CLAUDE.md + AGENTS.md (nexus repo) + Go scaffold (folder-paketi + stub + S0 data-modeli)
5. Go/no-go → kod P0 kernel (test-first, RED testovi iz Annex A spremni)

## ORKESTRACIJA (herdr, kako voditi agente)
- 3 agenta: `rev-codex` (gpt-5.6), `rev-kilo` (DeepSeek V4 Pro), `rev-agy` (Gemini 3.7) u tab w8:t2-4.
- Dispatch: `herdr agent prompt rev-X "PROMPT" --wait --until working`.
- **GOTCHA:** poll MORA puk'nuti na 3-idle ČAK i ako fajl fali (agent zna promašiti path/zastati —
  re-dispatch). `herdr agent read rev-X --source visible` za dijagnozu.
- Full-code audit: shallow-clone u scratchpad → audit → ODMAH `rm` klon (disk 8.3G tijesan).

## STANJE: 29 commita, plan code-grounded, 29 harnessa auditano, disk čist. Spremno za GAPFIX-merge→PRD.
