PLAN S16

# PLAN-S16 — S16 Eval i dokaz kvalitete (mini-eval od P0, ne od kraja)

Jezgra `CGO_ENABLED=0`. S16 je MEHANIZAM (mi pišemo; runneri/checker/ledger) + ADAPTER (benchmark/
red-team backend). **16.6 = CODING-diferencijator** (evidence-graded checker + council). Gate: P2.7
(council), P0.4 (posredno). Matrica dobiva **HARD-LIMIT pojavljivanja po projektu** (anti
over-indexing) + **critical-gate floor** (0.1/0.2/7.2 se ne kompenziraju perifernim IMA).

---

## 16.1 Task-level benchmark (end-to-end) + app-contract

**Tip:** MEHANIZAM (runner/non-vacuity jezgra) + DATA (ugovor)

**Aspekti → najbolji izvor:**
- standard za coding agente → **SWE-bench** (nedovoljan sam: ne izolira doprinos harnessa)
- praktičan, brz, po modelu i edit-formatu → **Aider benchmark suite**
- općeniti eval framework → **Inspect (UK AISI)**

**Sinteza (benchmark runner + deliverable-level app-contract + contract-negotiation):**
```go
type BenchmarkRunner struct{ corpus VersionedCorpus } // versioniran task corpus + holdout
func (b *BenchmarkRunner) Run(model, harnessCfg) (BenchResult, error) // paired baseline gate

type AppContract struct{} // deliverable-level acceptance: ekrani/tokovi/persistence/negativni slučajevi
func (a *AppContract) Verify(deliverable, contract) (Acceptance, error) // NE test-count (djelomičan demo ≠ gotova app)

// contract-negotiation (obsidian): generator+evaluator dogovore "done" ugovor KROZ fajlove
// PRIJE prvog reda koda — acceptance je ARTEFAKT, ne osjećaj (veže 3.1+3.5)
```
**Zahtjevi (16.1):** snapshot identiteta (model/prompt/tool verzije), ablation (harness MOŽE postati
RED), deterministic fixtures, flaky politika.

**Salvage:** NEXUSv2 `evals/bench.py` + `evals/benchmark_runner.py` + `app_contract.py` →
**Python→Go port**.

**Ugovor/RED:** 16.1 spec (non-vacuity, paired baseline). RED (naš): ablation (ukloni harness sloj)
→ rezultat pada (dokaže da harness doprinosi); djelomičan demo → app-contract NE prihvaća (ne
test-count).

**Verifikacija:** SWE-bench (Apache-2.0), Aider suite (Apache-2.0), Inspect (MIT) — stvarni.

**Pareto floor:** standard (SWE-bench) + brz (Aider) + framework (Inspect) + **app-contract +
contract-negotiation** (naš) → ≥ svaki.

---

## 16.2 Regression/ratchet u CI

**Tip:** MEHANIZAM

**Aspekti → najbolji izvor:**
- eval kao test suite u CI → **promptfoo**
- pytest-stil LLM testovi → **DeepEval**
- OSS eval tracking kroz vrijeme → **Opik** (zamjena za Braintrust)

**Sinteza (regression + ratchet baseline — nikad regresirati ispod baseline):**
```go
type RegressionTester struct{ baseline RatchetBaseline }
func (r *RegressionTester) Run(corpus VersionedCorpus) (RatchetResult, error)
// RED/GREEN po testu; ratchet: nova verzija MORA ≥ baseline (pass@1), inače CI fail
// flaky politika: retry + oznaka flaky, ne tiho pad
```

**Salvage:** NEXUSv2 `evals/regression_tester.py` + `evals/gates.json` + `ratchet_baseline.json` →
**Python→Go port**.

**Ugovor/RED:** 16.2 spec (ratchet). RED (naš): regresija ispod `ratchet_baseline` → CI fail
(ratchet floor); flaky test označen, ne lažni fail.

**Verifikacija:** promptfoo (MIT), DeepEval (Apache-2.0), Opik (Apache-2.0) — stvarni.

**Pareto floor:** CI suite (promptfoo) + pytest-stil (DeepEval) + tracking (Opik) + ratchet floor (naš)
→ ≥ svaki.

---

## 16.3 Red-team / adversarial testiranje

**Tip:** MEHANIZAM + ADAPTER (STRIX/Shannon = dinamički pentest agenti)

**Aspekti → najbolji izvor:**
- LLM vulnerability scanner → **garak**
- adversarialni framework → **PyRIT**
- automatizirani napadi u CI → **promptfoo redteam**
- dinamički pentest agent (jači kandidat) → **STRIX (usestrix/strix)**
- autonomni white-box pentester → **Shannon**

**Sinteza (red-team suite + dinamički pentest agenti kao adapteri):**
```go
type RedTeam struct{}
// garak (vuln scan) + PyRIT (adversarial) + promptfoo (CI napadi)
// + STRIX (dinamički pentest agent — izvršava PRAVE exploite, ne samo probe)
// + Shannon (white-box: analizira source → izvršava exploit; 96% XBOW)
// negativni canary test (6.5) pripada OVDJE (16.3) — exfil tripwire dokaz
```

**Salvage:** NEXUSv2 `evals/redteam.py` (:203) + `evals/cmdjail.py` + `evals/yolo.py` →
**Python→Go port**.

**Ugovor/RED:** 16.3 spec (red-team non-vacuous). RED (naš): canary exfil test (6.5) — token u
izlazu → FAIL-CLOSED blok isporuke; red-team napad MORA proći fail-closed obranu (ne vacuous).

**Verifikacija:** garak (Apache-2.0), PyRIT (MIT), promptfoo (MIT), STRIX (Apache-2.0), Shannon —
stvarni.

**Pareto floor:** vuln scan (garak) + adversarial (PyRIT) + CI napadi (promptfoo) + **dinamički
pentest (STRIX/Shannon)** (naš — jači od statičkih skenera) → ≥ svaki.

---

## 16.4 Feedback i data flywheel + credit-ledger (kanonski owner)

**Tip:** MEHANIZAM (jezgra — provenance-aware ledger)

**Aspekti → najbolji izvor:**
- annotations na trace + curation → **Langfuse**
- capture + annotacije + regresija → **Opik**
- session replay → curation → **AgentOps**

**Sinteza (credit SAMO iz verificiranog ishoda; epsilon-greedy; feeds 2.6/9.3/11.5):**
```go
type CreditLedger struct{} // KANONSKI owner (16.4); potrošači: 2.6 routing, 9.3 učenje, 11.5 skill
func (c *CreditLedger) Credit(revision string, outcome VerifiedOutcome) error
// kredit SAMO iz revision-bound VERIFICIRANOG ishoda (test-gate/loop-judge) — NIKAD samoprijava
// exploration budget + rollback (kredit se može povući ako se ishod kasnije opovrgne)

type Bandit struct{ ledger *CreditLedger } // epsilon-greedy iz 16.4 — routing uči iz verificiranog
```
Flywheel: eval-set raste iz produkcije (capture→annotate→curate→regression), ne ostaje sintetički.

**Salvage:** NEXUSv2 `core/reward.py` (:31 epsilon-greedy) + `core/credit.py` →
**Python→Go port**.

**Ugovor/RED:** 16.4 spec (verified-only credit). RED (naš): samoprijava (worker kaže "done" bez
test-gatea) → NE dobiva kredit; kredit se rollback-a kad se ishod opovrgne (revision-bound).

**Verifikacija:** Langfuse (MIT), Opik (Apache-2.0), AgentOps (source-available), Vowpal Wabbit
(BSD) — stvarni.

**Pareto floor:** annotations (Langfuse) + capture (Opik) + replay (AgentOps) + **verified-only
credit + epsilon-greedy + rollback** (naš — nitko ne veže kredit na revision-bound verifikaciju) → ≥ svaki.

---

## 16.5 Trajectory export za destilaciju (OPCIONALNI PROFIL)

**Tip:** ADAPTER (izvozni format)

**Aspekti → najbolji izvor:**
- okruženje + trajektorije za trening → **SWE-gym**
- SFT/DPO potrošač → **Axolotl**
- izvozni format → **ShareGPT/JSONL**

**Sinteza (export trajektorija u ShareGPT/JSONL — destilacija profil):**
```go
type TrajectoryExporter struct{}
func (t *TrajectoryExporter) Export(runs []Run, fmt ExportFormat) ([]byte, error)
// ShareGPT/JSONL iz journala (P0.3 projekcija); trajektorija = fold eventa, ne mutable snapshot
// `destilacija` flag; izvoz je OPTIONALNI profil (fine-tuning NIJE harness sekcija)
```

**Salvage:** NEXUSv2 `core/reasonbank.py` (trajectory_of :61) → **Python→Go port**.

**Ugovor/RED:** 16.5 spec (izvozni format). RED (naš): export nosi snapshot identiteta (model/prompt/
tool verzije) — bez toga trajektorija nije reproducibilna.

**Verifikacija:** SWE-gym (MIT), Axolotl (Apache-2.0), ShareGPT/JSONL (format) — stvarni.

**Pareto floor:** okruženje (SWE-gym) + SFT potrošač (Axolotl) + format (ShareGPT) + snapshot
identiteta (naš) → ≥ svaki.

---

## 16.6 Neovisna provjera, anti-sycophancy i confidence (CODING-diferencijator)

**Tip:** MEHANIZAM (jezgra — minimalni checker) + `multi-agent` (council strategija)

**Aspekti → najbolji izvor:**
- vlastiti checker (git-diff+exit-code, ne proza) → **NEXUS loop_engine/confidence/prreview**
- uncertainty iz logproba → **UQLM** (komponenta)
- eval-strana provjera → **Inspect scorers / DeepEval**

**Sinteza (evidence-graded checker + council, checker ostaje deterministički neovisan):**
```go
type Checker struct{} // EVIDENCE-GRADED: git-diff + exit-code + deterministički signali, NE proza
func (c *Checker) Grade(step, obs, ver) Grade // checker ≠ worker (odvojen na razini artefakata)
func (c *Checker) Review(goal, fold) Review   // devil's-advocate + "nikad ne validiraj premisu"
// blokira "uspjeh" kad dokaz ne prati tvrdnju (anti false-success / praise-spam stop-hooks)

type Confidence struct{} // ChainPoll C=0.7·O+0.3·S + flip_test + UQLM logprob — kalibracija nesigurnosti

type Council struct{} // P2.7: N persona → sealed nezavisni → anonimni cross → chairman + očuvan dissent
// council = `multi-agent` STRATEGIJA; checker/gate ostaje deterministički NEOVISAN (council ne promovira)
```
HIPOTEZA (ne "0/28"): strukturno odvajanje checkera od workera je rijetko — Reflexion/CrewAI/AutoGen
imaju OBLIKE samoprovjere, ne apsolutna tvrdnja.

**Salvage:** NEXUSv2 `loop_engine.py` (majority-checker :96) + `core/confidence.py` (ChainPoll,
flip_test) + `core/prreview.py` (devil's-advocate) + `managers/council.py` → **Python→Go port**.

**Ugovor/RED:** **P2.7** + `test_council_cannot_drop_sealed_material_dissent` — sealed review s
materijalnim dokazom, chairman ga izostavi → `DISSENT_DROPPED`, council `REJECTED`, promotion gate
ne dobiva success. + checker RED: worker "done" bez git-diff/testa → FAIL (dokaz ne prati tvrdnju).

**Verifikacija:** UQLM (source-available), Inspect (MIT), DeepEval (Apache-2.0), NEXUS checker/
confidence/council (interni) — stvarni.

**Pareto floor:** checker-na-artefaktima (NEXUS) + logprob-confidence (UQLM) + eval-scorers (Inspect)
+ **council P2.7 + neovisni deterministički gate** (naš — CODING-diferencijator, nitko nema
evidence-graded checker odvojen od workera) → ≥ svaki.

---

## S16 cross-cutting napomene

1. **16.6 je CODING-diferencijator** (uz 3.1 evidence-graded i 3.5 TIA): checker ocjenjuje
   git-diff+exit-code, NE prozu — anti-sycophancy strukturno; council (P2.7) je STRATEGIJA, gate
   ostaje deterministički neovisan.
2. **16.4 credit-ledger je kanonski owner:** kredit SAMO iz revision-bound verificiranog ishoda
   (nikad samoprijava) → feeds 2.6 (routing), 9.3 (učenje), 11.5 (skill) — epsilon-greedy + rollback.
3. **16.1 app-contract + contract-negotiation:** acceptance je ARTEFAKT (ugovor kroz fajlove PRIJE
   koda), ne osjećaj; deliverable-level (ekrani/tokovi/persistence), ne test-count.
4. **16.3 STRIX/Shannon su dinamički (jači) pentest kandidati** — nadograđuju statičke skenere
   (garak/PyRIT) na izvršavanje pravih exploita.
5. **Matrica HARD-LIMIT + critical-gate floor:** anti over-indexing (OpenHands 23×/Codex 20×/Aider
   16×) — po projektu max pojavljivanja; 0.1/0.2/7.2 se ne kompenziraju perifernim IMA redovima.
6. **Honest gap:** 16.1/16.2/16.3/16.5 nemaju dedicirani Annex RED (gate = 16.x spec + P2.7 posredno);
   16.4 ima verified-only credit, 16.6 ima P2.7. Ako moderator želi simetriju: P0.20 "credit-never-
   from-self-report".
7. **Verifikacija:** svi kandidati stvarni (SWE-bench/Aider/Inspect/promptfoo/DeepEval/Opik/garak/
   PyRIT/STRIX/Shannon/Langfuse/AgentOps/SWE-gym/Axolotl/UQLM MIT/Apache-2.0/BSD); salvage
   `bench/redteam/regression/reward/credit/confidence/loop_engine/council/prreview` interni (postoje
   — audit potvrdio), port je logika.
