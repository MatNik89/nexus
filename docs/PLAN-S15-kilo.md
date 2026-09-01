PLAN S15

# PLAN-S15 — S15 Observability (sve su PROJEKCIJE journala — P0.3 jedan write-owner)

Jezgra `CGO_ENABLED=0`. S15 je MEHANIZAM (mi pišemo). **Srž (P0.3):** trace/log/transcript/audit/
metrics su PROJEKCIJE `EventJournal` — NIJEDNA ne piše vlastiti sink ni ne alocira event-ID/sequence.
Gate: P0.3 (`test_projection_cannot_bypass_journal`), codex#21 (jedan write-owner).

---

## 15.1 Tracing (OTel GenAI konvencije)

**Tip:** MEHANIZAM (projekcija journala)

**Aspekti → najbolji izvor:**
- primaran standard → **OpenTelemetry GenAI**
- self-hostable backend → **Langfuse**
- OTel-first, jak na evalima → **Phoenix (Arize)**

**Sinteza (span = projekcija journal eventa; tracer NE piše svoj store):**
```go
type Tracer struct{ journal *journal.Journal }
func (t *Tracer) Spans(runID string) []Span
// svaki span nosi genai atribute (OTel GenAI konvencije) + korelaciju run/turn/tool_call/attempt
// span je IZVEDEN iz journala (source_event_id + source_offset) — tracer NEMA vlastiti write-put
```
Langfuse/Phoenix = ADAPTER backend (OTLP export), ne drugi izvor istine.

**Salvage:** NEXUSv2 `core/trace.py` (JSONL spanovi) + `core/otel.py` (genai atributi) →
**Python→Go port** (projekcijski sloj).

**Ugovor/RED:** **P0.3** (projekcija). RED: span BEZ `source_event_id` → `NON_CANONICAL_WRITE`
(isti P0.3 RED).

**Verifikacija:** OpenTelemetry GenAI (CNCF/Apache-2.0), Langfuse (MIT), Phoenix (Elastic-2.0) — stvarni.

**Pareto floor:** genai konvencije (OTel) + backend (Langfuse) + eval-OTel (Phoenix) + journal-
projekcija (naš) → ≥ svaki.

---

## 15.2 Usage/cost dashboardi

**Tip:** MEHANIZAM (projekcija CostTracker-a)

**Aspekti → najbolji izvor:**
- spend po ključu/modelu/timu → **LiteLLM**
- cost po trace-u → **Langfuse**
- proxy analytics → **Helicone**

**Sinteza (read-only projekcija iz 2.5 CostTracker + 7.2 budget):**
```go
type CostDashboard struct{ tracker *CostTracker }
func (c *CostDashboard) ByKey() []SpendRow    // po ključu (service)
func (c *CostDashboard) ByModel() []SpendRow  // po modelu
func (c *CostDashboard) ByTrace(runID string) SpendRow // po trace-u (korelacija)
// dashboard je read-only projekcija — NE piše ništa; budget ceiling (7.2) je enforcement, ne prikaz
```

**Salvage:** NEXUSv2 `llm/providers.py` (CostTracker) + `core/circuit.py` (add_cost) → **Python→Go port**.

**Ugovor/RED:** 15.2 spec (read-only). RED (naš): dashboard ne može mijenjati spend (monotoni counter
iz 2.5/7.2); cost po trace-u korelira s `run_id` (ne slobodan agregat).

**Verifikacija:** LiteLLM (MIT), Langfuse (MIT), Helicone (MIT) — stvarni.

**Pareto floor:** spend-po-ključu (LiteLLM) + cost-po-trace (Langfuse) + proxy analytics (Helicone) +
read-only projekcija (naš) → ≥ svaki.

---

## 15.3 Transcript i audit log (tamper-evident)

**Tip:** MEHANIZAM (jezgra — hash-lanac + vanjski potpisani checkpoint)

**Aspekti → najbolji izvor:**
- event stream za audit + rekonstrukciju → **OpenHands**
- session replay za agente → **AgentOps**
- trace kao auditabilan zapis → **Langfuse**
- tamper-evident append-only hash-lanac → **NEXUS audit.py**

**Sinteza (append-only hash-lanac + verify() + VANJSKI potpisani checkpoint):**
```go
type Audit struct{ journal *journal.Journal }
func (a *Audit) Append(ev JournalEvent) error
// integrity_prev_hash → integrity_hash (lanac); tamper → verify() pada; UPDATE/DELETE blokirano
func (a *Audit) Verify() error
// NAPOMENA (15.3): lokalni hash-lanac SAM nije dokaz protiv POTPUNOG rewritea
// → vanjski potpisani checkpoint: Sigstore Rekor anchor (ili immudb/Trillian) periodički
func (a *Audit) Anchor(ctx) (CheckpointID, error) // vanjski anchor — lanac nezaobilazan i protiv rewritea
```
Transcript = user-visible projekcija (P0.3); audit = append-only projekcija s vanjskim integrity
checkpointom (ne "savršen replay").

**Salvage:** NEXUSv2 `core/audit.py` (hash-lanac verify) + `core/transcript.py` (:43) →
**Python→Go port**.

**Ugovor/RED:** **P0.3** + tamper-evident (15.3). RED (naš): izmijeni/izbriši zapis #2 → `Verify()`
pada (lanac slomljen); puni rewrite → vanjski Rekor anchor otkriva (checkpoint mismatch).

**Verifikacija:** OpenHands (MIT), AgentOps (source-available), Langfuse (MIT), Sigstore Rekor
(Apache-2.0) — stvarni.

**Pareto floor:** event-stream audit (OpenHands) + session replay (AgentOps) + trace-zapis (Langfuse)
+ **hash-lanac + vanjski anchor** (naš — nitko ne ankorira vanjski protiv rewritea) → ≥ svaki.

---

## 15.4 Metrics i SLO (ne deployment mehanizam — premješteno iz 18.3)

**Tip:** MEHANIZAM (projekcija) + ADAPTER (Prometheus/Grafana/Collector)

**Aspekti → najbolji izvor:**
- metrike + alerting standard → **Prometheus**
- SLO dashboardi → **Grafana**
- jedinstven put metrika → **OpenTelemetry Collector**

**Sinteza (metrike = projekcija journala; JEDAN put kroz 1.3/15.1):**
```go
type Metrics struct{ journal *journal.Journal }
func (m *Metrics) Export(ctx) ([]Metric, error)
// latency / error-rate / saturation (RED metoda) — iz journala, kroz OTel Collector
// SLO: availability/latency/error-budget — read-only projekcija, NE zaseban write-put
```
SLO (15.4) je SERVISNI (latency/error-budget), distinktno od 15.5 (semantic-health) i 18.3 (liveness).

**Salvage:** NEXUSv2 `core/otel.py` (metrike export) → **Python→Go port**.

**Ugovor/RED:** P0.3 (jedan put metrika). RED (naš): metrika BEZ journal-eventsource → odbijena (isti
P0.3 jedan-write-owner); SLO se računa iz journala, ne iz zasebnog metrik storea.

**Verifikacija:** Prometheus (Apache-2.0), Grafana (AGPL), OpenTelemetry Collector (Apache-2.0) — stvarni.

**Pareto floor:** metrike+alerting (Prometheus) + SLO dashboardi (Grafana) + jedan put (Collector) +
journal-projekcija (naš) → ≥ svaki.

---

## 15.5 Semantic agent health i degradation (jezgra; distinktno od 15.4/18.3)

**Tip:** MEHANIZAM (jezgra; pragovi = bounded config po profilu)

**Aspekti → najbolji izvor:**
- vlastiti OTel metrički agregator + S7 circuit-breaker → **NEXUS circuit.py/health.py**
- evaluator metrike → **Langfuse**
- alerting → **Prometheus rules**

**Sinteza (semantički signali degradacije → S7 circuit-breaker, ne samo log):**
```go
type SemanticHealth struct{ breaker *s7.CircuitBreaker }
// signali (15.5) — agent ŽIV ali degradira (ne liveness 18.3, ne SLO 15.4):
//   recidivism-rate       (isti tool + isti arg ponovljen N puta)
//   correction-rate       (greška → ispravak → ODRŽIV porast)
//   verified-success-trend (pad trenda iz 16.4 ledgera)
//   fallback-drift        (tihi rast na fallback model)
//   swallow-counter       (progutane best-effort greške — NEXUS health.py)
func (h *SemanticHealth) Observe(run RunOutcome) HealthSignal
// pragovi = bounded config; prijelaz praga → OTVARA S7 circuit-breaker (zaustavi lažno-zdravi rad)
```
Hipoteza (ne "0/28"): self-critique/health-introspection nema većina harnessa — mjerimo, ne tvrdimo.

**Salvage:** NEXUSv2 `core/circuit.py` (breaker, STAGNATION/NO_PROGRESS) + `core/health.py`
(swallow-counter :16) → **Python→Go port**.

**Ugovor/RED:** 15.5 spec (→ S7 breaker). RED (naš): recidivism preko praga → breaker OTVARA (run
prekinut, ne lažno-zdrav); swallow-counter raste → signal (ne tihi drop); signal je DISTINKTAN od
liveness (proces živ ≠ agent zdrav).

**Verifikacija:** Langfuse (MIT), Prometheus (Apache-2.0), NEXUS circuit/health (interni) — stvarni.

**Pareto floor:** agregator+breaker (NEXUS) + evaluator metrike (Langfuse) + alerting (Prometheus) +
**semantički signali → breaker** (naš — nitko ne veže recidivism/fallback-drift na breaker) → ≥ svaki.

---

## S15 cross-cutting napomene

1. **Sve su PROJEKCIJE (P0.3):** trace/log/transcript/audit/metrics NE pišu vlastiti sink — `EventJournal.
   append()` je jedini write-owner; projekcije nose `source_event_id`+`source_offset`. Ovo je S15 srž.
2. **15.3 vanjski anchor je obvezan, ne optional:** lokalni hash-lanac detektira tamper, ali NE potpuni
   rewrite — Sigstore Rekor anchor (ili immudb/Trillian) daje vanjski potpisani checkpoint.
3. **Tri distinktna "health" sloja:** 15.4 = SLO (servisni: latency/error-budget), 15.5 = semantic
   (agent degradira: recidivism/fallback-drift), 18.3 = liveness (proces živ) — 15.5 je onaj koji
   OTVARA S7 circuit-breaker.
4. **15.2 je read-only:** cost dashboard NE mijenja spend (monotoni counter u 2.5/7.2 je enforcement);
   dashboard je prikaz, ne izvor.
5. **Honest gap:** 15.1/15.2/15.4/15.5 nemaju dedicirani Annex RED (gate = P0.3 posredno); 15.3 ima
   P0.3+tamper. Ako moderator želi simetriju: P0.19 "projection-never-bypass-journal".
6. **Verifikacija:** svi kandidati stvarni (OTel/Langfuse/Phoenix/LiteLLM/Helicone/OpenHands/AgentOps/
   Prometheus/Grafana/Collector MIT/Apache-2.0/AGPL; Rekor Apache-2.0); salvage `trace/otel/audit/
   transcript/health/circuit/provenance` interni (postoje — audit potvrdio), port je logika.
