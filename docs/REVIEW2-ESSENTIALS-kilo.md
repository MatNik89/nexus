# REVIEW2 — ARCHITECTURE-ESSENTIALS.md (re-review, kilo)

Metoda: svaki nalaz iz sva tri round-1 reviewa (kilo/codex/agy) provjeren protiv rewritea i protiv
normativnog `HARNESS-SPEC.md` (Annex A). Zatim traženje NOVIH grešaka u rewriteu.

**Ukupno:** rewrite je disciplinirano i pošteno foldao golemu većinu round-1 nalaza — status
"zaključano/0 blokera" je uklonjen, precedence je definiran, egress je produbljen, crypto je
razdvojen na tri primitive, Provider je komprimiran i izbačen iz top-15, activation + build-order +
AtomicWriter su dodani kao nove odluke, P0.1/P0.3 atribucije su ispravljene, a stale C3 nalaz je
označen zastarjelim. NALAZI ISPOD su uski ali materijalni.

---

## UNFOLDED

**1. [UNFOLDED] codex #2 je foldan U KRIVOM SMJERU: E2 "Decided in PRD (approved)" izmišlja odobrenje PRD-a.**
Rewrite E2 (redak 27) piše "Decided in PRD (approved)". Round-1 codex #2 zahtijevao je OBRNUTO —
oznaku "PENDING PRODUCT APPROVAL" ili zatvaranje #6. Nijedan izvor ne bilježi odobrenje:
> PRD.md:66 — "**Ovaj PRD čeka tvoju potvrdu/korekciju** (ti vodiš product-sloj)."
> DESIGN-STATUS.md:13 — "#6 PRD identitet + P0-scope | **OPEN — TI VODIŠ**"
> DESIGN-STATUS.md:37 — "PREOSTAJE PRIJE KODA (samo tvoje): ... #6 PRD — tvoja presuda"
"Approved" je suprotno od izvora. Ovo je isti tip false-readiness greške koji je srušio round-1.

**2. [NEW-ERROR] "Open before P0 code" ispušta PRD odluku (#6), koju DESIGN-STATUS vodi kao otvorenu.**
Rewrite (redci 175-180) navodi 3 otvorene stavke (Linux preflight, P1.6 tension, Win/mac probe), ali
NE navodi PRD identitet/scope. DESIGN-STATUS vodi TOČNO DVIJE "preostaje prije koda" stavke — #3
(probe) i #6 (PRD). Header rewritea (redci 5-6) tvrdi "Open preconditions are listed at the bottom",
ali lista je nepotpuna. Kombinirano s nalazom 1: rewrite istovremeno proglašava PRD "approved" i briše
ga s liste otvorenog — interna kontradikcija s vlastitim "does NOT claim go/no-go has been granted".

---

## NEW-ERROR

**3. [NEW-ERROR] E4 "each write uses AtomicWriter (tmp → fsync → rename)" prekomjerno se primjenjuje na memory spine.**
Rewrite E4 (redci 45-48): "workspace files, memory spine, backups are separate durable side-effects ...
each write uses `AtomicWriter` (tmp → fsync → rename, never in-place)". Memory spine je SQLite-WAL
(`modernc.org/sqlite`) — WAL ima vlastitu trajnost, ne per-record `tmp→fsync→rename`. Ugovor P0.11
je scoped na S5.1/5.3 (workspace/checkpoint), ne na S9 memory store:
> HARNESS-SPEC.md:1564-1566 — "P0.11 — Shadow-checkpoint integritet. Vlasnik: S5.1/5.3 ... atomic
> write = stari ILI novi, nikad pola"
Agynov M2 tražio je AtomicWriter za "memorija, config, obligation store", ali to je reviewerova
preporuka, ne izvorni scope — i SQLite-WAL store se ne piše `tmp→fsync→rename` po zapisu. Niska ozbiljnost.

**4. [NEW-ERROR] Precedence "HARNESS-SPEC.md (normative Annex A) > ... > HARNESS-PLAN.md" riskira elevaciju stale salvage tijela SPEC-a iznad greenfield plana.**
Rewrite header (redci 8-11) rangira cijeli `HARNESS-SPEC.md` iznad `HARNESS-PLAN.md` (kvalifikator
"(normative Annex A)" sužava, ali ime datoteke je to što čitatelj vidi). Tijelo SPEC-a još nosi stale
"salvage NEXUS" reference koje izravno proturječe greenfield C1 odluci:
> HARNESS-SPEC.md:350-354 — "Kandidati: NEXUS `gortex/` Go (salvage) ..."
> HARNESS-SPEC.md:517 — "salvage NEXUS `dream.py`+`sleeptime.py`+`audn.py`+`memgit.py` ..."
> HARNESS-SPEC.md:626 — "salvage NEXUS Skill Forge"; HARNESS-SPEC.md:881 — "salvage"
> HARNESS-PLAN.md:3 — "Svi `Salvage:`/GORTEX/NEXUSv2 oslonci obrisani" (C1)
Precedence treba glasiti "HARNESS-SPEC **Annex A ugovori** > ...", ne ime datoteke; inače P0 čitatelj
može primijeniti "SPEC > PLAN" na tijelo i oživjeti salvage koji je C1 obrisao. Niska-srednja ozbiljnost.

---

## OK — foldano ispravno (ne rubber-stamp: provjereno po nalazu)

- **kilo #1** "0 blokera/9 rundi" → uklonjeno; header sada "does NOT claim go/no-go". ✓
- **kilo #2** "PRD §6.6" → "PRD §6 item 6, §7" (E10:101). ✓
- **kilo #3** "DataDescriptor" → vraćen (Provider:157). ✓
- **kilo #4** shadow-git→symedit → sada eksplicitno "corrected per A8" (E2:28). ✓
- **kilo #5** subscription-OAuth → "cut from P0" (Provider:159) + u "Cut from P0" listi (P0 scope). ✓
- **kilo #6/#7** Win/mac vs Linux probe → razdvojeno (E10:96-100; Open #1/#3). ✓
- **kilo #8** capability-closure → nova odluka E7. ✓
- **kilo #9** TIA+symedit USP#2/#3 → eksplicitno u E13 (trio=P1). ✓
- **kilo #10** no-autonomous-self-change → u E2 golden rule. ✓
- **kilo #11** crypto NOT-CRITICAL → demoted iz top-15 u pod-točku E11. ✓
- **codex #1/#14** status + precedence → header rewritten + precedence defined. ✓
- **codex #3** "jedini write-owner" prejak → E4 scope-limit ("do not over-read"). ✓
- **codex #4** build-order → E6. ✓
- **codex #5** activation → E7. ✓
- **codex #6** egress preslab → E11 (resolver/dialer pinning, redirect, proxy, child-socket, receipt). ✓
- **codex #7** gateway retry → E15 "S7 alone authorizes and schedules delivery retries". ✓
- **codex #8** crypto 3 primitive → E11 razdvojeno (HMAC/Ed25519/pinned-hash). ✓
- **codex #9** "izbačeno iz v1" → "Cut from P0" vs "Out of v1 (PRD §5)" razdvojeno. ✓
- **codex #10** trio=P1 skriva K0 checker → E13 "minimal checker is K0/P0; trio is P1". ✓
- **codex #11** P0 acceptance granice → dodane u P0 scope. ✓
- **codex #12** auth modovi previše → Provider izbačen iz top-15, komprimiran. ✓
- **codex #13** "prvi zadatak" → "go/no-go preflight BEFORE production exec/scaffold code". ✓
- **codex #15** "statički binary" → "static linking is the goal, verified by CI check, not assumed". ✓
- **agy F1** P0.3→P0.1 provenance → "P0.1 RED ... (:1383)". ✓
- **agy C1** stale "P0.x ne postoje" → "(Annex A P0.5–P0.13 EXIST — HARNESS-SPEC:1534+; C3 stale)". ✓
- **agy F2** E1 A9 → "A9 component strategy; PLAN-HOLES C4" (C4 sada prisutan; A9 pravilno scoped). ✓
- **agy C2** P0-Telegram vs P1.6 → E15 OPEN + Open #2 (explicitno, "do not code around it"). ✓
- **agy M1** activation → E7. ✓
- **agy M2** AtomicWriter → E4 (uz caveat iz nalaza 3). ✓
- **agy N1** (partial) crypto demoted; E3 "Go types" zadržan kao top-15 — branjivo (kanonsko S0 "Pravilo"). ✓

---

## Verdict obrazloženje

Foldanje round-1 nalaza je temeljito i točno — jedina materijalna iznimka je codex #2, koji je
foldan OBRNUTO: umjesto "PENDING PRODUCT APPROVAL", rewrite piše "Decided in PRD (approved)", iako
PRD:66 i DESIGN-STATUS #6 izričito drže identitet/scope OTVORENIM. Uz to je PRD odluka ispuštena iz
"Open before P0 code", pa dokument ponovno emitira false-readiness signal na najvažnijoj otvorenoj
točki — ista klasa greške koja je srušila round-1, sad u novom obliku. Dva sitnija nova nalaza
(AtomicWriter nad memory spine; precedence scoping SPEC tijela) su korektivne prirode.

VERDICT: FAIL
