# PRD — NEXUS (Product Requirements Document, v1)

Product/business sloj — **što** se gradi i **za koga**, ne KAKO (tehničko → ARCHITECTURE.md).

---

## 1. Problem (1-2 rečenice)
Korisnik želi vlastitog AI asistenta koji radi na svim njegovim uređajima, **pamti preko razgovora**,
uči iz upotrebe i može mu obaviti stvarne poslove — ali ne može ga sagraditi na tuđem (Hermes) kodu
koji ne smije mijenjati. NEXUS je taj asistent: njegov vlastiti, brz, siguran, pouzdan.

## 2. Za koga
- **Sam korisnik** — osobna upotreba, jedan čovjek (ne tim, ne SaaS).
- Radi mu **prvo na Linuxu** (glavni stroj za rad); ostale platforme kasnije.
- Priča s njim iz terminala i s mobitela.

## 3. Identitet (što NEXUS JEST)
**Osobni asistent prvo; vrhunski programerski pomoćnik kao najjača grana.**
Svakodnevno lice = asistent (razgovor, pamćenje, podsjetnici, poslovi). Kad zatreba kodiranje,
uključi se programerski način koji je jači od postojećih alata. Isti "mozak", dva načina rada.

## 4. Ključne funkcionalnosti (prioritizirano)

### P0 — prvo, mora raditi kraj-do-kraja
1. **Razgovor** — pričam s asistentom iz terminala, odgovara koristeći AI model.
2. **Pamćenje preko sesija** — kažem mu nešto danas, sutra u novom razgovoru to i dalje zna.
3. **Podsjetnici/poslovi** — "podsjeti me sutra na X", "dovrši Y" → zapamti, javi na vrijeme, ne izgubi.
4. **Mobitel (Telegram)** — pišem mu s telefona, odgovara; za nešto nepovratno (obriši/pošalji) prvo pita.
5. **Odvojeni profili** — "posao" i "privatno" ne miješaju pamćenje ni tajne.
6. **Sigurno izvršavanje** — kad pokrene alat/naredbu na računalu, to je izolirano (ne može oštetiti sustav).

### P1 — poslije
- **Programerski način** — vrhunsko kodiranje (samo pogođeni testovi, dokaz-da-je-gotovo, pametni edit).
- Pretraga weba / preglednik · čitanje dokumenata i slika (OCR) · glasovni razgovor · više kanala (Slack/…).

### P2 — kasnije
- Windows i macOS · timski rad / više agenata · napredna pretraga znanja (RAG) · dodaci (plugini).

## 5. Izvan scopea (NE radi ovo u v1)
- Nije SaaS za više korisnika/tvrtki.
- Ne popravlja sam sebe autonomno (kad nešto ne valja s njim → javi meni/Claudeu na popravak).
- Ne obećava punu izolaciju na Windows/macOS u v1 (tamo je slabija zaštita, s upozorenjem).
- Ne koristi zaobilaženje pretplate/ToS-a kao zadano (samo svjestan opt-in uz upozorenje).
- Bez desktop-aplikacije s prozorima u v1 (terminal + mobitel su dovoljni).

## 6. Kriteriji "gotovo" (kad je P0 stvarno dovršen)
1. Pokrenem NEXUS na Linuxu i vodim koristan razgovor s AI modelom.
2. Nešto mu kažem u jednom razgovoru → u sasvim novom razgovoru to i dalje zna (pamćenje preživi restart).
3. "Podsjeti me…" stvori trajan podsjetnik koji se javi u pravo vrijeme, preživi gašenje aplikacije.
4. Napišem mu na Telegram s mobitela → odgovori; za nepovratnu akciju traži moje odobrenje prije izvršenja.
5. Zatražim akciju u "poslu" → u "privatnom" načinu se ništa od toga ne vidi (nula curenja).
6. Kad izvrši naredbu na računalu, ona je zatvorena u pješčaniku — pokušaj čitanja/pisanja izvan
   dopuštenog je odbijen (dokazano zlonamjernim testom: ne može do `/etc/shadow`, ne može van na mrežu).

## 7. Rizici (koji mogu srušiti proizvod)
- **Sigurni pješčanik na Linuxu** je najteži i najvažniji dio — ako ne radi kako treba, asistent nije
  siguran za pokretanje alata. Dokazuje se zlonamjernim testovima prije oslanjanja.
- **Previše funkcija odjednom** — zato P0 je uska okomita cjelina (gornjih 6), sve ostalo čeka.
- **Asistentska prednost** — pamćenje/podsjetnici/profili moraju biti stvarno bolji od postojećih
  asistenata, ne tek "i mi to imamo".

---

**Sljedeće (6-datoteka redoslijed):** ARCHITECTURE.md (tech dizajn — imamo ga u HARNESS-PLAN/DESIGN-*) →
ARCHITECTURE-ESSENTIALS.md (5-15 kritičnih odluka) → hard-questions → CLAUDE.md/AGENTS.md → scaffold.
**Ovaj PRD čeka tvoju potvrdu/korekciju** (ti vodiš product-sloj).
