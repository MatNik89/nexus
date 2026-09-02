# REVIEW — ARCHITECTURE-ESSENTIALS.md (adversarijalni, kilo)

Metoda: prvo vlastita skica top-10 kritičnih odluka iz izvora, pa usporedba. Moja skica (prije
čitanja dokumenta): (1) Go/CGO_ENABLED=0/single-binary, nula salvagea; (2) EventJournal jedini
write-owner P0.3; (3) owner-invarijante (S6.0/S6.9/S7/S1.2/S7.2); (4) Linux self-reexec helper,
Win/mac UNAVAILABLE+blok; (5) effect-taxonomy UNKNOWN→RECONCILING; (6) checker≠worker evidence-gate;
(7) fail-closed/default-reject/QUARANTINE; (8) trust/lineage monotoni; (9) FORGET≠PURGE;
(10) crypto hash.Sum(key) bug; (11) capability-flag closure+aktivacija; (12) 4-auth-moda, no-self-retry;
(13) durable-delivery run_done≠result_delivered; (14) jedna jezgra/dva profila asistent-prvo;
(15) ručni Go tipovi, nema map[string]any.

Ukupno: 15 odluka E1-E15 su SADRŽAJNO visoke vjernosti — nisam našao nijedno čisto iskrivljenje
mehanizma. Nalazi su na meta-sloju (readiness tvrdnje), selekciji i sitnim citatima.

---

## FIDELITY

**1. [FIDELITY] Header "9 rundi red-teama → 0 blokera" je nepotkrijepljiv i "0 blokera" je netočan.**
"9 rundi" se ne može izvesti iz 6 izvora (PLAN-HOLES spominje 4 recenzenta, DESIGN-FIXES-r2 zatvara
"REVIEW2", DESIGN-STATUS "6 must-resolve nakon (a)+(b)"; broj "9" ne postoji nigdje). "0 blokera"
izravno proturječi DESIGN-STATUS koji ima DVA otvorena:
> DESIGN-STATUS.md:38-39 — "#3 stvarni-OS probe — Win/macOS hardver ... #6 PRD — tvoja presuda"
> DESIGN-STATUS.md:41 — "Preostale 2 stavke traže tvoj hardver / tvoju odluku — ne mogu ja."
Sam dokument u "Otvoreno prije P0 koda" (redci 123-125) navodi 2 otvorene stavke — interna
kontradikcija s vlastitim headerom.

**2. [FIDELITY] E7 citira "PRD §6.6/§7" — podsekcija 6.6 ne postoji u PRD-u.**
PRD §6 je "Kriteriji gotovo" s NUMERIRANIM stavkama 1-6 (nema 6.1/6.6). Sandbox kriterij je stavka 6,
rizici su §7. Ispravno: "PRD §6 (stavka 6) / §7".

**3. [FIDELITY] E12 "Provider{Chat/Stream/Capabilities}" ispušta 4. metodu.**
> HARNESS-PLAN.md:187 — "Jedan `Provider` interface (`Chat/Stream/Capabilities/DataDescriptor`)"
"DataDescriptor" je izgubljen. Sitno, ali interface je četverometodni.

**4. [FIDELITY] E2 "CodingProfile = najjača grana (TIA/evidence-gate/symedit)" tiho odstupa od citiranog izvora.**
> HARNESS-PLAN.md / PLAN-HOLES-CONSOLIDATED.md:74 — agy rješenje: "`CodingProfile` (evidence-gate/
> shadow-git/TIA)"
E2 zamjenjuje shadow-git→symedit. Supstitucija je ZAPRAVO točnija prema A8 (shadow-git je oborjen,
symedit je USP#3), ali čitatelj koji cross-checkira E2→C7 nađe mismatch. Treba ili dodati "ispravljeno
prema A8" ili citirati A6/A8 kao izvor, ne samo C7.

**5. [FIDELITY] Subscription-OAuth scope nije označen kao v1-rez.**
> PLAN-HOLES-CONSOLIDATED.md:66 — "Konsenzus reza iz v1: ... S2.1 subscription-OAuth (ToS/ban rizik) ..."
E12 prikazuje SubscriptionOAuth kao punopravan mod, a P0 "Izbačeno iz v1" popis ga NE sadrži (iako
C6 izričito reže). P0 programer čita E12 kao "gradi 4 moda sad". (PRD §5 ga drži kao svjesni opt-in,
pa su i sami izvori dvosmisleni — ali Essentials ne flagira v1/v2 status.)

---

## CONTRADICTION

**6. [CONTRADICTION] "Otvoreno prije P0 koda" #1 preusmjerava otvorenu sondu s Win/mac na Linux.**
> DESIGN-STATUS.md:10 — "Linux ENFORCED-nakon-live-probe ...; Win/macOS UNAVAILABLE+high-risk
> BLOKIRAN, nula weaker-fallback. **Stvarni-OS probe (W0-W10 / M0-M9) traži tvoj Windows/macOS
> hardver — ja nemam.**"
> DESIGN-STATUS.md:38 — "PREOSTAJE PRIJE KODA (samo tvoje): #3 stvarni-OS probe — Win/macOS hardver"
Essentials (redak 124) tvrdi da je prvi P0 zadatak "Sandbox live-probe na stvarnom Linux kernelu".
DESIGN-STATUS označava OTVORENU sondu kao Win/mac hardver (korisnikov), a Linux kao "ENFORCED-nakon-
live-probe" (dizajn riješen). Ovo je konflacija dviju različitih sondi i miješa korisnikov hardver
dependenciju s Linux kernel zadatkom.

**7. [CONTRADICTION] Interno: header "0 blokera" ↔ "Otvoreno prije P0 koda" (2 stavke).**
Već pokriveno u nalazu 1 — navodim zasebno jer je interna (ne samo s izvorom).

---

## MISSING (selekcija)

**8. [MISSING] Capability-flag closure + activation-failure stateovi (S0.5/P0.4) — K0 jezgra, NEMA je u 15.**
Ovo je najjači MISSING za P0: S0.5 je u FAZA K0 (prvi build korak), a nosi netrivijalnu odluku koju
"fail-closed" (E8) NE pokriva — atomska aktivacija je nemoguća i rješava se stateovima:
> DESIGN-STATUS.md:9 — "ActivationPlan(immutable+PlanHash)+Prepare/Commit/Activate+RollbackToken+
> PREPARING/FAILED/ROLLING_BACK rješava nemoguću atomsku aktivaciju"
> HARNESS-PLAN.md:119-125 — "0.5 capability-flag closure resolver ... parcijalno ACTIVE nelegalno ...
> RED P0.4 `test_every_flag_fails_when_one_gate_is_ablated` (18 flagova)"
P0 programer koji gradi kernel scaffold bez ovog vratit će se na istu nemoguću-atomsku-aktivaciju
grešku koju je plan već platio.

**9. [MISSING] TIA (USP#2) i AST-symedit (USP#3) nisu odluke, dok USP#1 (E11) i USP#4 (E13) jesu.**
Dokument sam uspostavlja USP-brojanje (E11 "USP #1", E13 "USP #4") ali USP#2 i USP#3 nisu nigdje
kao odluka — samo usput u E2 i P0 scope-rezu. Asimetrija:
> HARNESS-PLAN.md:1061-1065 — "NAŠ STVARNI USP — 4 diferencijatora ... 1. evidence-graded ... 2. TIA ...
> 3. AST/LSP symedit ... 4. FORGET vs PURGE"
Mitigant: PRD stavlja "Programerski način" (pa time i trojku) u P1, pa je deprioritet branjiv — ali
onda E11/E13 "USP #1/#4" framing treba istu P1 oznaku da ne zavara.

**10. [MISSING] "No autonomous self-change" zlatno pravilo (A3.2 + PRD §5).**
> HARNESS-PLAN.md:905-906 — "NEXUS na terenu NE dira vlastiti source (zlatno pravilo iz NEXUS loze)"
> PRD.md:41 — "Ne popravlja sam sebe autonomno (kad nešto ne valja → javi meni/Claudeu na popravak)."
Jaka invarijanta koja veže 15.5/17.4/A3.2 (self-diagnostic→repair-handoff) i nema je u 15, a relevantna
je i za P0 (nepovratne akcije asistenta).

---

## NOT-CRITICAL

**11. [NOT-CRITICAL] E15 (kripto) ima najnižu P0-relevantnost od 15.**
Svi potpisani artefakti koje E15 štiti (shadow-checkpoint, audit hash-lanac 15.3, skill-lock P1.1)
su P1 coding/eval značajke; nijedna od 6 P0 sposobnosti (razgovor/pamćenje/obveze/Telegram/profili/
sandbox) ne zahtijeva potpis. Jedini P0-susjedni potrošač je ExecutionReceipt (E6), ali effect-path
radi i bez kriptografske verifikacije receipta. Načelo je jeftino i zatvara stvaran D2 bug
("hash.Sum(key) → ključ curi, RED bi lažno prošao"), pa ga NE treba brisati — ali po kriteriju
"P0 programer mora znati" ono je najslabije rangirano.

---

## Top-3 najslabije točke dokumenta (ako se zanemare gornji nalazi)

1. Meta-readiness tvrdnje (header "0 blokera" + "Otvoreno" #1) — najviše mogu zavesti P0 programera
   jer se tiču "jesam li spreman kodirati", a upravo tu su netočne.
2. Tretman coding-USP-a: USP#1/#4 su "kritične odluke", USP#2/#3 ne — bez eksplicitnog P1-razloga
   čitatelj ne vidi zašto je trojka prepolovljena.
3. Scope-rez "Izbačeno iz v1" nije usklađen s C6 (fale subscription-OAuth, callgraph/archmap 4.4,
   PROV-O 5.3, guardian-LLM 6.5) pa popis reza nije vjerodostojna referenca.

---

## Što JE točno (ne rubber-stamp, ali pošteno)

- 108 podsekcija ✓ (SECTION-MAP.md:88).
- E3/E4/E5/E6 su vjerne DESIGN-FIXES-r2 (effect-path Put, vlasništvo-fix, classifyEffectPhase) — najrizičniji
  dio arhitekture je prenesen bez iskrivljenja.
- E7 koristi ISPRAVLJENU verziju (C2: Win/mac UNAVAILABLE+blok, ne "adapteri" iz stale HARNESS-PLAN headera).
- E8/E9/E10/E13/E14 citati točni prema S0.3/0.4, S1.1, S6.3/6.5, S8.2, S9.4, S14.5/7.3-N9.
- P0 scope-rez (6 sposobnosti iz PRD §4) točan.

---

## Verdict obrazloženje

15 odluka su mehanički visoke vjernosti (nijedno iskrivljenje mehanizma), ali:
- header "0 blokera" je **netočan** prema DESIGN-STATUS (2 otvorene stavke) i prema vlastitom
  "Otvoreno" odjeljku — a to je upravo informacija o kojoj P0 "go/no-go" ovisi;
- "Otvoreno" #1 **pogrešno pripisuje** otvorenu sondu (Win/mac hardver → "Linux kernel");
- **fali K0-kritična odluka** (capability-flag closure + activation-failure, S0.5/P0.4).
Tri materijalna nalaza na readiness/selekciji → ne prolazi kao "0 blokera, spreman za kod" referenca.

VERDICT: FAIL
