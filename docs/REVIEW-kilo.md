# REVIEW S7-S12 — adversarijalni, greenfield-Go (salvage=MRTAV)

Premisa: svi `Salvage:`/`GORTEX REUSE`/`NEXUSv2 port` retci su ŠUM. Izvor implementacije =
svjetski full-code-auditani kandidat ILI Go-dizajn u planu. Ništa od NEXUS-a ne postoji.

## RUPE (salvage je bio jedini/pravni izvor — sad 0)

**R1 — S9.4 konsolidacija/decay (NAJVEĆA).** `Consolidator.Dream`/`Decay`/`Audn`/`MemGit`/
`MemAssoc`+temporalni-upiti = 6 NEXUS algoritama. Svjetski ekvivalent: CrewAI `analyze_for_
consolidation` (samo konsolidacija), FSRS (open-spaced-repetition, samo decay), Hermes dream/audn
(ali Hermes je KONKURENT, ne izvor). NIJEDAN svjetski kandidat nema puni set. Ovo je sad ~2-3
tjedna greenfield Go-a OD NULE (Dream=LLM-konsolidacija, Decay=FSRS-lite, Audn=ADD/NOOP/SUPERSEDE,
MemGit=git-po-write, MemAssoc=spreading-activation, temporalni snapshot). **Treba:** ili prepisati
6 algoritama, ili DESCOPE na Decay(FSRS)+Audn (1 tjedan) i Dream/MemGit/MemAssoc odgoditi v2.

**R2 — S8.3 archmap (naš USP, ali 1-linijski dizajn + mrtav salvage).** "arhitektura kao queryable
graf" je JEDINA rečenica; izvor je bio `core/archmap.py`+`wiki.py` (mrtav). Niti jedan svjetski
kandidat (Aider repomap, OpenClaw file-indexer, Hermes codeindex) nema queryable-archmap. PPR dio
ima izvor (Aider networkx-pagerank), ali ARCHMAP (kako graditi graf, query API, freshness) NEMA
ni dizajna ni izvora. **Treba:** spec archmap Go-dizajn (graf-nad-modulima + query jezik + invalidation).

**R3 — S11.5 Skill Forge (score→doctor→merge→scout).** 4-korak samoevolucija je NEXUS-specific;
svjetski ima samo Hermes-curator (self-kuracija, djelomično) i OpenClaw-skill (statički). Go-dizajn
u planu je TANAK (lanac je naveden, ali "doctor"=popravi skill, "merge"=spoji skillove — bez
mehanizma). **Treba:** spec forge loop (kako se mjeri score, kad doctor vs merge, scout izvor).

**R4 — S11.2 Reversa workflow (legacy→spec+migracijski-plan).** NEXUS-specific, 0 svjetski ekvivalent.
Reversa-kao-data (40 skillova) je OK kao sadržaj, ali SAM reverse-engineering workflow (kako legacy
repo→SDD/PRD→migracijski plan) nema izvora. **Treba:** descope Reversu u "signed pack koji DONOSI
korisnik", ili spec workflow (srednji rizik — nije coding-jezgra).

**R5 — S9.2 "5-scope" + memvec/kg/entres (djelomično).** 5-scope taksonomija (PROJECT/USER/TASK/
LESSON/EPHEMERAL) je NEXUS-specific; vektor (RRF) i KG (multi-hop) imaju izvor (Qdrant RRF,
LightRAG/mem0 graf). Scope-taksonomija je lako re-specirati (1 rečenica), ali `memvec`/`entres`
(moduli) nemaju 1:1 svjetski pandan. **Treba:** re-spec scope kao tip (ne modul), vektor/KG kroz
Qdrant/LightRAG adapter.

## PROTUJEČJA / OVER-ENGINEERING / SCOPE-RIZIK

**P1 — RED-gate sustav je POLU-REALAN.** Citira se "RED P0.2/P0.3/P2.1/P2.2/P2.3/P1.1/P1.2/P2.7"
kao gate, ALI uz njih "RED codex#2 (Tier-3)", "RED codex#3 (Tier-3)", "RED codex#19" — a codex#2/#3/
#19 su Tier-3 BACKLOG stavke (iz HOLES-CONSENSUS), NISU u Annex A (TIER3-CONTRACTS ima P0.1-P0.4,
P1.1-P1.6, P2.1-P2.7 = 17 ugovora). Citiramo nepostojeće testove kao gate.

**P2 — "Honest gap: bez dediciranog Annex RED" pojavljuje se 6× (S8.1/8.3/8.5, S9.1/9.3/9.4 + S7
djelomično), a istovremeno tekst navodi "RED P0.3 pokriva 8.2" itd. Gate-mapa je NEKONZISTENTNA:
neke podsekcije imaju Annex RED, neke "kandidat P0.13/P0.14" (NE POSTOJI u Annex A), neke ništa.
Plan tvrdi "Tier-3 ugovorni standard" za RED testove koji su 40% predložena IMENA, ne ugovori.

**P3 — "Pareto: X+Y+naš" je laž za salvage-heavy podsekcije.** "naš" u S9.4/S8.3/S11.5/S11.2/S12.6
znači "NEXUS salvage", ne "naš Go-dizajn u planu". Sad kad je salvage mrtav, "naš" je PRAZAN — Pareto-
floor tvrdnja stoji na nepostojećem izvoru.

**P4 — Broj podsekcija.** Plan implikuje "~60 modula"/"~100 podsekcija"; stvarno je **82 podsekcije**.
Brojke u "SVIH 19 SEKCIJA ZAOKRUŽENO" i per-sekcijskim "STATUS: ZAOKRUŽEN" su netočne.

**P5 — "Salvage" retci su SADA ŠUM koji OBMANJUJE.** 24 "Salvage: core/X.py" retka u S7-S12 su
lažni izvori; ostavljeni u planu, čitatelj (i budući agent) misli da postoji implementacija. Treba
GLOBALNO brisanje, ne ignoriranje.

## MORA PRIJE KODIRANJA

- **M1:** R1 — prepisati ili descope S9.4 (6 algoritama). Preporuka: descope na Decay(FSRS)+Audn,
  Dream/MemGit/MemAssoc = v2.
- **M2:** R2 — spec archmap Go-dizajn (to je JEDINI preživjeli USP; bez speca je vruća voda).
- **M3:** R3/R4 — spec Skill-Forge loop + descope Reversa workflow.
- **M4:** P1/P2 — riješiti RED-gate mapu: realni Annex A ugovori vs "kandidat" vs "bez gatea"; NE
  citirati codex#2/#3/#19 kao gate dok nisu foldani u Annex A.
- **M5:** P3/P5 — globalno OČISTITI plan od 24 "Salvage:" retka (i svih "GORTEX REUSE"/"port"),
  zamijeniti svjetskim obrascem (Aider/OpenClaw/Hermes/FSRS/Qdrant) + Go-dizajnom, ili označiti
  "GREENFIELD, bez izvora".
