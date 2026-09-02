# VERDICT — Codex

**U velikom se slažem.** Da gradim NEXUS od nule, zadržao bih arhitekturu iz
`/home/matej/HARNESS/nexus/docs/HARNESS-PLAN.md` i dependency-DAG iz
`/home/matej/HARNESS/nexus/docs/SECTION-MAP.md`; ne vidim razlog za fundamentalni rewrite.

## Velike odluke

- **Go, greenfield, `CGO_ENABLED=0`, single-binary:** da za isporučenu jezgru. To daje dobar operativni
  oblik i jasnu sigurnosnu granicu. Ne bih, međutim, pretvorio pure-Go u ideologiju: teški ili
  platformski specifični kapaciteti smiju ostati izolirani adapteri/subprocesi iza istog effect-patha.
- **Jedna jezgra, dva profila:** da, pod uvjetom da su profili capability/policy kompozicije na rubu,
  a ne `if assistant/coding` grananja razasuta po kernelu. Dvije jezgre bi duplicirale najskuplje
  invarijante.
- **Effect-path S6.0 → S6.9 → S6.2/S1.2 → S7:** da; to je najjača odluka plana. Jedan
  nepreskočiv put i jedan owner za policy, lifecycle, proces i retry važniji su od bilo koje pojedine
  funkcije.
- **Memory-spine:** da za durable state, lineage, replay i purge dokaz. Ne bih od njega napravio
  sinkroni “univerzalni bus” za svaki detalj; derivirane projekcije i efemerni hot-path state trebaju
  ostati izvedeni i jeftini.
- **TIA / evidence-gate / symedit / forget-purge:** zadržao bih sva četiri, ali nisu ista vrsta.
  Evidence-gate i forget/purge su temeljni ugovori povjerenja; symedit i TIA su proizvodne hipoteze
  koje moraju zaraditi složenost mjerenim poboljšanjem. TIA mora uvijek imati konzervativni full-suite
  fallback; brzina nikad ne smije kupiti false-green.
- **Faze K–P:** dependency-redoslijed je razuman. Ne bih ih vodio kao horizontalni waterfall.

## Što bih fundamentalno promijenio

Prije široke implementacije faza K0/K1/L napravio bih jedan tanki **vertikalni walking skeleton**:
CLI zahtjev → provider → jedan read i jedan sandboxed effectful alat → journal/resume → evidence-gate
→ isporuka rezultata. Na njemu bih odmah mjerio nekoliko reprezentativnih coding i assistant zadataka,
uz baseline, latenciju, trošak i peak memoriju.

Zato bih minimalni product-level dio S16.1 premjestio iz faze P u rani K/L gate. Faze K–P tada ostaju
capability-gates, ali svaka proširuje već upotrebljiv end-to-end proizvod. Ovo je promjena strategije
gradnje, ne promjena jezgre.

## Što bih izbacio ili odgodio

Iz prvog proizvoda tvrdo bih izbacio Cordis “everything-is-a-plugin” DI, council/flows/boards,
fleet/service, learned router, GraphRAG/CAG, multimodal/computer-use i naprednu memory-konsolidaciju.
Plan ih uglavnom već stavlja kasno ili iza flagova; to mora ostati stvarno odgođeno, bez apstrakcijskog
troška u ranoj jezgri. Plugin engine bih uveo tek kad postoje najmanje tri stvarna proširenja koja
jednostavni Go interfacei i eksplicitno wiring više ne pokrivaju uredno.

## Najveći rizik i najslabija karika

Najveći rizik nije Go ni effect-path, nego **širina**: 108 podsekcija mogu proizvesti arhitektonski
ispravan sustav koji prekasno dokaže da je koherentan, brz i bolji u stvarnom poslu.

**NAJSLABIJA KARIKA: product-level dokaz vrijednosti dolazi tek u fazi P (S16.1).** Ako se taj gate
ne povuče naprijed, integracijski i UX promašaji otkrit će se nakon što je već izgrađeno previše
mehanizama. Uz rani vertikalni gate i brutalan P0 rez, ovaj je pristup onaj koji bih i sam odabrao.

**Status: PASS — slažem se s arhitekturom; preporučujem jednu fundamentalnu korekciju build-strategije,
ne redesign.**
