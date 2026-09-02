# VERDICT-kilo — stručna procjena arhitekture (ne gap-hunt)

Izvor: HARNESS-PLAN.md (arhitektura) + SECTION-MAP.md (build-order) + DESIGN-*.md. 7 rundi review konteksta.

---

## Stav ukratko

**U velikom se slažem s arhitekturom.** Decomposition i owner-invarijante su ispravni i rijetko
vidim ovako discipliniran red (jedan write-owner, jedan retry-owner, policy≠lifecycle, membrane u
effect-pathu). To je najteži 20% i vi ste ga riješili točno. Gdje se razilazim je **scope**, ne dizajn.

## (a) Velike odluke — slažem li se

- **Go greenfield single-binary (CGO_ENABLED=0):** DA, za jezgru. Caveat: "single-binary" je točan
  samo za kernel; OCR/voice/vector/browser/modeli su realno subprocess-sidecar (Python/CUDA/ffmpeg na
  hostu). Plan to priznaje u 13.x ali marketing "single-binary" ostaje poluistinit za punog asistenta.
- **jedna-jezgra-dva-profila:** DA, točna rezolucija identiteta. Trošak: profile-gate (evidence/TIA/
  shadow-git) mora biti uvjetovan po profilu na MNOGO mjesta — najlakše mjesto za curenje profila.
- **effect-path S6.0/6.9/6.2/1.2/S7 (po jedan owner):** DA, najjači dio. Ovo je kanonski.
- **memorija-spine (SQLite WAL):** DA za single-user. Journal+queue+memory+FTS5+board na jednom
  SQLite fajlu ima single-writer plafon (plan sam to flagira u 1.3) — fine za osobni asistent, ne za
  `service` (i plan kaže Postgres za service). Konzistentno.
- **4 diferencijatora (TIA/evidence-gate/symedit/forget-purge):** DA, i code-verified (A6/A8). ALI —
  iskreno: **3 od 4 su coding-only**, a identitet je sad "asistent prvo". Asistentski diferencijatori
  (ObligationStore/PersonalProfile/computer-use) su "me too" naspram Hermes/OpenClaw. USP je realno
  jači na coding grani nego na asistentskoj — strateški mismatch koji PRD mora razriješiti.
- **faze K-P (DAG po ID-u):** DA, ispravan build-order. Nisam našao ništa fundamentalno krivo.

## (b) Što bih izbacio (over-engineering)

1. **18.5/18.6 fleet+device-pairing + 12.7/12.8 flows/boards + 4.8 computer-use** — sve "ODLUKA-PRD"
   klonirano iz OpenClaw/Hermes. Za osobni asistent ovo gotovo sigurno NIJE P0. Descope.
2. **12.6 council + širi multi-agent (12.x)** — council nad artefaktima je lijep ali skup; v1 = jedan
   checker + ljudski gate, dovoljno.
3. **GPU OCR put (A1 Unlimited-OCR VLM)** — CPU put (OCRmyPDF) rješava problem; GPU VLM je teška
   ovisnost bez P0 opravdanja.
4. **voice (13.3) + video (13.4)** — STT/TTS/WebRTC je zaseban projekt; descope za v1.

## (b2) Što bih dodao (ključno što fali)

1. **Brutalni P0 slice + procjena truda.** 108 podsekcija, mnoge greenfield (sandbox/TIA/symedit/
   decay/evidence-gate), solo+Claude. Bez reza je paraliza. Plan već IMA konsenzus (C6): S0-S9 + 14.1
   TUI + 16.6 checker + 17.1 DI. To treba biti NASLOV, ne fusnota u rupama.
2. **Walking-skeleton PRVI:** dokazati JEDAN effect-path (S3→S4→S6→1.2→S7) s realnim Landlock na
   Linuxu PRIJE gradnje 108 podsekcija. Sad je to odgođeno "iza scaffolda" — to je naopako.
3. **Jedan konsolidirani design-doc.** Nakon 9 rundi, istina je fragmentirana (DESIGN-FIXES-r2
   nadjačava GAPFIX-* kroz SUPERSEDED bannere). Graditelj mora čitati patch-sloj. Treba squash u jedan
   kanonski dizajn, ne fix-notu nad starim.

## (c) Najveći rizik

**S6.2 sandbox — jedini temelj koji NIJE probe-verificiran.** Linux probe je još OPEN, Win/macOS su
"UNAVAILABLE + high-risk blocked" (DESIGN-STATUS #3). Cijela vrijednost harnessa (safe arbitrary-exec)
visi o self-reexec + Landlock + seccomp-BPF membrani koja još nije dokazana ni na jednom OS-u. Ako
Landlock/execveat redoslijed ima bug, coding-jezgra je nesigurna i sve ostalo pada. Realno: mjeseci, a
"cross-platform single-binary" tvrdnja vjerojatno završi kao "Linux full, Win/mac blokirano" — što je
fer, ali plan to ne kaže dovoljno glasno. Sekundarni rizik: scope (gore).

## Najslabija karika (1)

**S6.2 sandbox cross-platform realnost** — load-bearing zid, najmanje verificiran, najskuplji za
popraviti ako padne. Sve ostalo u planu je popravljivo; ovo je temelj.

---

## Confidence
- **Provjereno:** dekompozicija/owner-invarijante/DAG pročitan kroz 7 rundi; diferencijatori code-verified
  (A6/A8). **Neprovjereno:** nijedna "mjeseci/dani" procjena nije mjerena (sandbox/TIA/symedit trud je
  pretpostavka); ne znam stvarni Landlock status na ciljnom hardveru. **Najslabija tvrdnja u mom verdictu:**
  "fleet/pairing nije P0" — to je MOJ ukus o tome što je osobni asistent, ne dokaz; PRD presuđuje.

topknot: ultra+preflight
