PLAN S3

# PLAN-S3 — S3 Core Loop (PRVA CODING-KRITIČNA sekcija — produbljeno)

Jezgra `CGO_ENABLED=0`. S3 je MEHANIZAM (mi pišemo). **Owner granica:** retry/deadline/cancel =
S7 (P0.2) — 3.3 emitira user-cancel, 3.4 prikazuje finalni `TypedError`, NIKAD ne retry-a sama.
Gate: P0.2 (retry/cancel owner), P2.6 (automation clock/DST). Coding-konkurenti (direktiva):
Kilo Code / OpenHands / OpenCode — hibrid mora biti ≥ najbolji NA SVAKOM aspektu + naši
diferencijatori (evidence-graded, TIA, stuck-detection) koje NITKO od njih nema.

---

## 3.1 Plan-act-observe petlja (coding-jezgra — PRODUBLJENO)

**Tip:** MEHANIZAM (jezgra)

**Aspekti → najbolji izvor (razložak coding-petlje konkurenata):**
- **Action/Observation model** (tool-akcija → observation nosi rezultat/error natrag) → **OpenHands**
- **event-stream kao izvor istine** (svaki korak = append-only event, replay moguć) → **OpenHands**
- **turn/session/tool/context RAZDVOJENI moduli** (zlatni standard modularnosti) → **Codex**/OpenCode
- **adaptivni planer** (mali=direct, srednji=3-5 koraka, veliki=full plan) → **SWE-agent ACI** + NEXUS
- **ACI dizajn** (kompaktan agent-computer-interface, dokazano diže slab model) → **SWE-agent**

**Sinteza (Go; plan → authorize → execute → observe → verify → GRADE → re-plan; sve kroz journal):**
```go
type Loop struct {
    planner  *Planner          // adaptivni (small/medium/large) — SWE-agent ACI + NEXUS
    executor *exec.Executor    // tool dispatch kroz 4.1 + 6.9 lifecycle gate
    verifier *verify.Verifier  // 3.5 deterministička verifikacija
    checker  *check.Checker    // 16.6 EVIDENCE-GRADED (naš diferencijator)
    state    *machine.StateMachine // S0.2 (run/turn/tool legalni prijelazi)
    journal  *journal.Journal  // S0/P0.3 jedini write-owner
}
func (l *Loop) Run(ctx context.Context, goal Goal) (*RunResult, error) {
    plan := l.planner.Plan(goal)                 // adaptivno
    for _, step := range plan.Steps {
        auth := l.executor.Authorize(step)        // 6.9 before_tool
        obs  := l.executor.Execute(ctx, step, auth) // tool → Observation (3.4 error-u-obs)
        ver  := l.verifier.Verify(step, obs)       // 3.5 lint/test IN-TURN
        grade := l.checker.Grade(step, obs, ver)   // 16.6 ocjenjuje git-diff+exit-code, NE prozu
        if grade < pass { plan = l.planner.RePlan(step, ver.Diagnostic()) } // re-plan s dokazom
    }
    review := l.checker.Review(goal, l.journal.Fold(plan)) // 16.6 finalna provjera
    return review.Result()
}
```
**EVIDENCE-GRADED (naš diferencijator — niti Kilo Code ni OpenHands ni OpenCode ga nemaju):**
`Checker.Grade` ocjenjuje **git-diff + realan exit-code + determinističke signale**, NIKAD prozu
workera (anti-sycophancy strukturno). Ovo je NEXUS `loop_engine` majority-vote checker obrazac —
tie fail-closed, `confidence.py` ChainPoll `C=0.7·O+0.3·S`.

**Salvage:** NEXUSv2 `loop_engine.py` (status enum :75, plan :159, majority-checker :96) +
`runtime.py` (handle→loop→verify→learn :498) + `llm/executor.py` (`_run_loop` :1815) →
**Python→Go port** (najvrjedniji salvage u cijelom kernelu).

**Ugovor/RED:** S0.2 (run/turn/tool legalni prijelazi). RED (naš, coding): worker napiše "done,
radi" bez git-diff-a/testa → `Checker.Grade` vraća FAIL (dokaz ne prati tvrdnju), run ne prelazi u
`SUCCEEDED` — NIKAD samoprijava bez artefakta.

**Verifikacija:** OpenHands (MIT, aktivan), smolagents (Apache-2.0, aktivan), SWE-agent (MIT,
aktivan), Codex modularitet (stvaran — javni repo Apache-2.0). Kilo Code = korisnički referent,
ne javni OSS (nema licence za provjeru — tretiram kao "to-beat" metu, ne salvage izvor).

**Pareto floor:** Action/Observation (OpenHands) + event-stream-truth (OpenHands) + modularnost
(Codex) + adaptivni planer (SWE-agent/ACI) + **evidence-graded checker** (naš — NITKO nema) → ≥ svaki.

---

## 3.2 Streaming i inkrementalni prikaz

**Tip:** MEHANIZAM

**Aspekti → najbolji izvor:**
- referentna open-source stream implementacija → **Gemini CLI**
- robustan stream na prekide → **Codex CLI** (Rust)
- streaming kroz event stream → **OpenHands**

**Sinteza (chunk-channel + context-cancel, backpressure kroz journal sequence):**
```go
type Chunk struct { Delta string; Seq uint64; Finish *FinishReason }
func (p *Provider) Stream(ctx context.Context, req ChatRequest, grant s7.AttemptGrant) (<-chan Chunk, error)
// ctx cancel = prekid; svaki chunk nosi monotoni Seq; FinishReason{STOP|LENGTH|TOOL_CALL}
// TUI (bubbletea) konzumira channel; backpressure = ne čitaj dalje dok se chunk ne prikaže
```
Stream je PROJEKCIJA journala (P0.3), ne zaseban write-put.

**Salvage:** NEXUSv2 `llm/providers.py` (`"stream": False` — SLABO) → **Python→Go port + popravak**
(omogućiti pravi token-stream; NEXUS je to imao ugašeno).

**Ugovor/RED:** S0.2 (turn state) + P0.2 (cancel token). RED (naš): cancel usred streama → channel
se zatvara, turn `CANCELLED`, nijedan kasniji chunk ne prolazi.

**Verifikacija:** Gemini CLI (Apache-2.0), Codex CLI (Apache-2.0), OpenHands (MIT) — stvarni.

**Pareto floor:** referentna stream impl (Gemini) + robustan na prekid (Codex) + stream-kroz-event
(OpenHands) + cancel-token (naš, P0.2) → ≥ svaki.

---

## 3.3 Prekid, cancel i turn management

**Tip:** MEHANIZAM

**Aspekti → najbolji izvor:**
- čista cancel semantika → **goose** (Rust)
- pause/resume kroz event stream → **OpenHands**
- prekid mid-turn bez gubitka stanja → **Codex CLI**

**Sinteza (cancel = S7 token; pause/resume = journal offset):**
```go
type TurnController struct{ cancel context.CancelFunc; turnID string }
func (t *TurnController) Cancel()            // emitira user-cancel → S7.CancelToken → sve child taskove
func (t *TurnController) Pause() (offset uint64)  // checkpoint = journal offset (7.3)
func (t *TurnController) Resume(offset uint64)    // replay fold-om od offseta (ne mutable state)
// CANCELLED je TERMINALAN (P0.2) — nema resume iz CANCELLED
```
Cancel propagira se u provider (3.2 ctx) i tool (S7 grant); loop NE retry-a (P0.2).

**Salvage:** NEXUSv2 `core/steer.py` (interrupt) + `core/circuit.py` (breaker) → **Python→Go port**.

**Ugovor/RED:** **P0.2** (cancel propagira, CANCELLED terminalan). RED: cancel usred tool-poziva →
tool ctx se prekida, turn `CANCELLED`, nikakav kasniji side-effect.

**Verifikacija:** goose (Apache-2.0), OpenHands (MIT), Codex CLI (Apache-2.0) — stvarni.

**Pareto floor:** čist cancel (goose) + pause/resume (OpenHands) + mid-turn prekid bez gubitka
(Codex) + S7-token vlasništvo (naš) → ≥ svaki.

---

## 3.4 Error recovery unutar petlje (coding-jezgra — PRODUBLJENO)

**Tip:** MEHANIZAM (jezgra)

**Aspekti → najbolji izvor:**
- **error u OBSERVATION, ne pad petlje** (model vidi error i korigira se) → **OpenHands**
- **checkpointing: nastavak od zadnjeg dobrog stanja** → **LangGraph**
- **poruke o grešci formatirane da ih model RAZUMIJE** → **SWE-agent**

**Sinteza (tool-pad → TypedError u observation → classify → S7 retry ILI re-plan; petlja živi):**
```go
// alat pao ≠ petlja pala: error se PAKIRA u observation, model ga vidi
func (l *Loop) observe(result exec.ToolResult) Observation {
    if result.Error != nil {
        return Observation{ Text: formatToolError(result.Error) } // "TOOL ERROR: <code> <safe_message> (pokušaj N)"
    }
    return Observation{ Text: result.Output() }
}
func formatToolError(e *TypedError) string // SWE-agent obrazac: formatirano da model razumije
// classify (recovery.py): retryable→S7 retry; terminal→re-plan; max-attempts→STAGNATION
```
**STUCK-DETECTION (naš diferencijator — iz `core/circuit.py`):** prati `STAGNATION` (isti tool, isti
argument, bez progressa N puta) i `NO_PROGRESS` (verifikacija ne zelena nakon M pokušaja) →
circuit-breaker prekida prije lažno-zdravog vrtenja u krug. Nitko od tri konkurenta nema ovo.

**Salvage:** NEXUSv2 `core/recovery.py` (classify :16, STRATEGY :41) + `llm/executor.py`
(OBSERVATION string :2441) + `core/circuit.py` (STAGNATION/NO_PROGRESS) → **Python→Go port**.

**Ugovor/RED:** **P0.2** (retryable/terminal taksonomija; S7 jedini retry). RED: tool pad →
observation nosi `TypedError`, loop NE crasha; terminal error → re-plan (ne retry); retryable →
S7 izda sljedeći `AttemptGrant`; STAGNATION nakon N istih → breaker.

**Verifikacija:** OpenHands (MIT), LangGraph (MIT), SWE-agent (MIT) — stvarni.

**Pareto floor:** error-u-observation (OpenHands) + checkpoint-resume (LangGraph) + razumljiv error
format (SWE-agent) + **stuck-detection** (naš — NITKO nema) → ≥ svaki.

---

## 3.5 Deterministička verifikacija + TIA (coding-jezgra — PRODUBLJENO)

**Tip:** MEHANIZAM (jezgra; `coding` flag)

**Aspekti → najbolji izvor:**
- automatski linter nakon edita + re-prompt → **Aider**
- lint/test hookovi PRIJE prihvaćanja edita → **SWE-agent**
- puna check-and-reprompt petlja (odbij završetak dok ne prođe) → **UNCLEAR** (spec-praznina)

**Sinteza (in-turn verify: pokreni→dijagnostika→odbij-završetak-dok-ne-prođe + TIA):**
```go
type Verifier struct { linter *Linter; tester *TIA }
func (v *Verifier) Verify(change Change) Verification {
    // RED: linter/typecheck/test → GREEN: pass → else: Diagnostic natrag modelu U ISTOM TURNU
    // odbij završetak koraka dok deterministički signal nije zelen (ne "možda radi")
}

// TIA — test-impact analysis (NAŠ diferencijator, nitko od 3 konkurenta nema):
type TIA struct { coverage *CoverageMap }
func (t *TIA) Affected(diff Diff) []Test
// coverage/dependency-graf × git diff → SAMO pogođeni testovi; full-suite fallback kad mapiranje zastarjelo
// (sekunde umjesto minuta — Aider/SWE-agent vrte pun suite ili samo linter)
```
In-turn povrat: dijagnostika se vraća modelu u ISTOM turnu (NEXUS `_validate` + `_verify_handle_result`
obrazac), ne čeka se kraj — ključno za slab model (sintaksna greška se ispravi odmah).

**Salvage:** NEXUSv2 `core/verifier.py` (DORMANTAN — REVIVATI) + `core/tia.py` (.coverage×git diff) +
`core/testcmd.py` (:9) + `llm/executor.py` (`_validate` :1882) → **Python→Go port**.

**Ugovor/RED:** 3.5 spec zahtjev (odbij završetak dok ne prođe). RED (naš): edit s lint-greškom →
`Verifier.Verify` vraća Diagnostic, korak se NE prihvaća, model dobiva dijagnostiku u istom turnu;
TIA na difu pokreće SAMO pogođene testove (ne full suite), uz fallback na full suite kad je stale.

**Verifikacija:** Aider (Apache-2.0), SWE-agent (MIT) — stvarni; treći slot ostaje UNCLEAR (spec
praznina — naša petlja je odgovor, ne kandidat).

**Pareto floor:** auto-lint+re-prompt (Aider) + hook-prije-prihvaćanja (SWE-agent) + **TIA
coverage×diff** (naš — NITKO nema) → ≥ svaki (pobjeđuje na brzini povrata).

---

## 3.6 Run trigger plane i admission (automation flag)

**Tip:** MEHANIZAM (admission=jezgra; triggeri=`automation` flag)

**Aspekti → najbolji izvor:**
- vlastiti trigger-admission adapteri (isti S0 envelope, isti policy/budget/audit) → **NEXUS** `schedule.py`/`watch.py`
- schedule semantika (cron, DST, missed-run) → **Temporal Schedules**
- laki in-process scheduler → **APScheduler**

**Sinteza (admission = isti S0 envelope validator; schedule s P2.6 clock/DST semantikom):**
```go
type Trigger interface { Fire(ctx context.Context) (Envelope, error) }
// implementacije: ManualTrigger, ScheduleTrigger, WebhookTrigger, WatchTrigger, HeartbeatTrigger

type ScheduleManifest struct {
    ScheduleID, ScheduleVersion, Expression string
    IANATimezone string
    DSTGapPolicy string // SKIP|NEXT_VALID|ERROR
    DSTFoldPolicy string // ONCE_FIRST|ONCE_SECOND|TWICE
    MissedRunPolicy string // SKIP|COALESCE|CATCH_UP
    OverlapPolicy string // FORBID|QUEUE|PARALLEL
    MaxConcurrency int; StartAt, EndAt *time.Time
}
// admission: SVI triggeri → isti S0 run-envelope → dedup+idempotency+policy+budget+audit PRIJE petlje
```
Autonomni trigger ima ISTE granice kao ručni (spec zahtjev) — admission ide kroz 6.9 + S0 envelope
validator, NE kroz zaseban put.

**Salvage:** NEXUSv2 `core/schedule.py` (add :47, Heartbeat :148) + `core/watch.py` → **Python→Go port**.

**Ugovor/RED:** **P2.6** + `test_zagreb_dst_fold_runs_once_across_restart` — `30 2 * * *` zona
Europe/Zagreb, `ONCE_FIRST`, restart između dva 02:30 pri jesenskom foldu → točno jedan occurrence,
drugi fold ne dolazi do admissiona.

**Verifikacija:** Temporal Schedules (MIT, aktivan), APScheduler (MIT, aktivan); NEXUS schedule.py
interni (postoji — audit potvrdio).

**Pareto floor:** isti-envelope admission (NEXUS) + DST/missed-run/overlap semantika (Temporal) +
laki in-process (APScheduler) + P2.6 gate → ≥ svaki.

---

## S3 cross-cutting napomene

1. **Owner granica (P0.2):** S3 NIKAD ne retry-a — 3.4 classify-a i prosljeđuje S7; 3.3 emitira
   user-cancel; svaki pokušaj nosi `s7.AttemptGrant`. Ovo je najvažnija invarijanta S3.
2. **Evidence-graded + stuck-detection su naša DVA coding-diferencijatora** koje Kilo Code/OpenHands/
   OpenCode nemaju (iz audit-a: `loop_engine` checker + `circuit.py` STAGNATION/NO_PROGRESS) — 3.1 i
   3.4 su mjesta gdje POBJEĐUJEMO, ne samo izjednačavamo.
3. **TIA je treći diferencijator** (3.5): coverage×diff → pogođeni testovi; Aider/SWE-agent vrte
   pun suite/linter — mi vraćamo dijagnostiku u sekundama.
4. **Honest gap:** 3.2/3.3/3.5 nemaju dedicirani Annex RED (gate = S0.2 + P0.2 posredno); 3.5 treći
   kandidat ostaje UNCLEAR (spec praznina — naša petlja je odgovor, ne kandidat). Ako moderator želi
   simetriju: P0.9 „verify-never-silent-accept".
5. **Verifikacija:** svi javni kandidati stvarni (OpenHands/smolagents/SWE-agent/LangGraph/goose/
   Codex/Gemini/Aider MIT/Apache-2.0, aktivni); Kilo Code = korisnički referent, nema javnu licencu
   za provjeru (tretiran kao "to-beat" meta, ne salvage izvor). Salvage `loop_engine/runtime/circuit/
   recovery/testcmd/tia/verifier` interni, postoje (audit potvrdio), port je logika.
