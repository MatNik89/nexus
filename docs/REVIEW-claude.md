# REVIEW-claude — vlastiti nalaz (prije čitanja agenata, anti-anchoring)

Kontekst: greenfield Go, NULA salvagea. Sve `Salvage:`/GORTEX-reuse/NEXUSv2 tvrdnje mrtve.

## SISTEMSKE RUPE (ne per-podsekcija — cijeli plan)

**H1 — salvage-iluzija u SVAKOJ podsekciji.** Svaka od 82 podsekcija ima `Salvage: core/X.py`
redak kao polaznu točku. Sve mrtvo. Plan sustavno OBEĆAVA startni kod kojeg nema → svaka
podsekcija je "od nule" a piše kao da je "port". Dok se ne izbriše, plan zavarava procjenu truda.
**Treba:** strip svih salvage-redaka; svaka podsekcija reklasificirana MERGE(auditani) ili PIŠI-OD-NULE.

**H2 — S6 sigurnost je najveći rizik (bila "reuse 1:1", sad od nule).** Cijela S6 počiva na
"GORTEX membrana Go REUSE 1:1 + parity test". GORTEX ne postoji (nula Go u izvoru). Znači
Landlock (3 syscalla preko x/sys/unix bez cgo) + seccomp BPF + Job Object + Seatbelt + netjail
(bwrap prazan netns + UDS FilterProxy) + SSRF-hardening — SVE se piše i verificira OD NULE.
To je najteži, sigurnosno-najkritičniji dio, a plan ga je tretirao kao riješen kopiranjem.
**Treba:** S6 potpuno reskopirati kao build-from-scratch; realno procijeniti (ovo je mjeseci, ne
tjedni) ili suziti P0 sandbox na jedan OS (Linux Landlock) + fail-closed stub drugdje.

**H3 — Annex A RED testovi = polu-fikcija.** Cijeli gate-model visi o P0.1-P2.7 RED testovima.
Ali plan sam priznaje "honest gap: bez dediciranog Annex RED" na S1, S2.1/2.3/2.4/2.5, S3.2/3.3/3.5,
S4.1/4.2/4.4/4.7, S5.1/5.3, S8.1/8.3/8.5, S9.1/9.3/9.4, S15.x — i uvodi P0.5/P0.6/P0.7/P0.8/P0.9/
P0.11/P0.13 kao "kandidat za matrix-prep" (= nenapisano). Gate infrastruktura je djelomično
aspiracijska. **Treba:** provjeriti postoji li HARNESS-SPEC Annex A stvarno sa specificiranim
RED-ovima; nenapisane P0.x ugovore napisati PRIJE nego se citiraju kao gate.

## SCOPE (najveći stvarni rizik za greenfield solo+Claude)

**H4 — 82 podsekcije + ~18 flagova + 10 addenduma = nemoguć P0.** Desktop, voice, video, vision,
OCR (GPU+CPU), RAG, GraphRAG, multi-agent, A2A, plugins, channels (6 kanala), computer-use,
profili, obligation-store, council, fleet+device-pairing... za greenfield bez ijedne linije koda.
Plan je dizajn-potpun ali gradbeno neograničen. **Treba:** brutalna P0-jezgra (S0/S1/S2/S3/S4.1-4.3/
S5/S6-Linux/S7/S9.1) — sve ostalo iza flaga, odgođeno. (Korisnik rekao ne definirati P0 sad —
ali scope-neograničenost je rupa koju treba priznati.)

**H5 — proturječje identiteta.** Vrh plana: "DIREKTIVA: coding profil = PRVORAZREDNI CILJ". A3.1:
"asistent PRVO, coding je grana". Dva različita sjevera → PRD ne može voditi. **Treba:** korisnik
presuđuje u PRD-u; vjerojatno A3.1 pobjeđuje (asistent-prvo), coding ostaje jaka grana.

## PER-PODSEKCIJA RUPE (tanke, bez dizajna)

- **S11.3** projektne-instrukcije — jedna rečenica (AGENTS.md standard). Nema ugovora.
- **S13.1** vision — jedna rečenica + mrtav salvage. Nema mehanizma.
- **S13.3** voice — jedna rečenica. (A4-G3 dodaje ugovor u addendumu — spojiti.)
- **S13.4** video — jedna rečenica.
- **S14.4** IDE/ACP — nabraja standard, nema granica-dizajna.
- **S16.5** trajectory-export — jedna rečenica (formati).
- **S10.5 CAG** i **S18.4** — plan SAM kaže "top-3 UNCLEAR" / "UNCLEAR dok S0.4 ne odabere backend".

## KONTRADIKCIJE/DUG
- Addendumi A1-A10 sadrže materijalne NOVE podsekcije (computer-use, ObligationStore, PersonalProfile,
  flows, boards, fleet, exec-auto-reviewer) koje NISU u S0-S18 tijelu — plan je "potpun" samo ako se
  addendumi foldaju u sekcije. Trenutno su viseći dodaci. **Treba:** fold u numerirane podsekcije.
- "60 modula"/"~100 podsekcija" netočne brojke (82) — signal neprovjerenih tvrdnji drugdje.

## NAJSLABIJA KARIKA MOG NALAZA
H2 (S6 od nule) je najsigurnija i najteža posljedica. H4 (scope) je stvar prosudbe — možda korisnik
ionako gradi inkrementalno pa nije "rupa" nego plan-faza. H3 zahtijeva provjeru HARNESS-SPEC Annex A
koju NISAM napravio u ovom nalazu (pretpostavka da su P0.x polu-napisani — treba potvrditi čitanjem).
