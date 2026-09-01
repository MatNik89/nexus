PLAN S7

# PLAN-S7 — S7 Pouzdanost i durable execution (NAJTEŽI Tier-3 ugovori)

Jezgra `CGO_ENABLED=0`. S7 je 100% MEHANIZAM (mi pišemo). **S7 je JEDINI vlasnik retry/deadline/
cancel (P0.2)** — 2.2/3.3/3.4 su delegati, NIKAD vlastita retry petlja. Gate: P0.2 (retry-owner),
P2.2 (bounded budgets), P2.3 (queue lease/fencing), N9 (durable delivery outbox). Go:
goroutine/channel/`sync`/`atomic` za concurrency, `context` za deadline/cancel, `modernc.org/sqlite`
za durable store. **Ključni popravak:** audit je našao NEXUS `queue.py` SLOMLJEN (nema lease/reclaim
za `running` nakon pada) — ovdje se to POPRAVLJA po P2.3.

---

## 7.1 Timeout/cancellation propagacija + retryable-vs-terminal taksonomija

**Tip:** MEHANIZAM (jezgra — JEDINI owner)

**Aspekti → najbolji izvor:**
- referentna retry/timeout semantika → **Temporal** (komponenta)
- cancel/timeout na razini grafa → **LangGraph**
- deklarativni retry s klasifikacijom → **tenacity** (komponenta)

**Sinteza (ExecutionPolicy + AttemptGrant; S7 jedini izdaje sljedeći pokušaj):**
```go
type ExecutionPolicy struct {
    OperationID string; EffectClass EffectClass // READ_ONLY|REVERSIBLE|IRREVERSIBLE
    Deadline time.Time; MaxAttempts int; AttemptTimeout time.Duration
    BackoffPolicy Backoff             // eksponencijalni + jitter + max
    RetryableCodes []string; FallbackTargets []string
    IdempotencyKey string; CancelTokenID string
}
type AttemptGrant struct {
    OperationID string; AttemptNo int; TargetID string
    IssuedAt, ExpiresAt time.Time; GrantNonce string // svaki fizički pokušaj = jedinstveni grant
}
type RetryOwner struct{} // S7 — jedini vlasnik
func (r *RetryOwner) Classify(e *TypedError) Retryability // retryable(rate_limit/timeout/...)|terminal
func (r *RetryOwner) NextGrant(cur AttemptGrant, p ExecutionPolicy) (AttemptGrant, error)
// SAMO iz FAILED_RETRYABLE; FAILED_TERMINAL → nema granta; zbroj ≤ MaxAttempts
// IRREVERSIBLE+UNKNOWN → NIKAD retry bez reconciliationa (P0.2 invariant)
```
Cancel: `CancelToken` propagira `context.Context` kroz SVE child taskove (provider 2.x + tool 4.x);
`CANCELLED` je terminalan. Delegati (2.2 mapira error→fallback, 3.3 emitira user-cancel, 3.4
prikazuje finalni `TypedError`) NEMAJU retry petlju.

**Salvage:** NEXUSv2 `core/recovery.py` (classify :16, STRATEGY :41) → **Python→Go port** (taksonomija
je već tu; samo se izdvaja u čisti owner-modul).

**Ugovor/RED:** **P0.2** + `test_adapter_cannot_self_retry` — provider dvaput pozove transport s istim
`AttemptGrant` → `ATTEMPT_NOT_AUTHORIZED`, transport counter = 1, S7 attempt counter = 1.

**Verifikacija:** Temporal (MIT), LangGraph (MIT), tenacity (Apache-2.0) — stvarni.

**Pareto floor:** retry/timeout semantika (Temporal) + graf-cancel (LangGraph) + deklarativna
klasifikacija (tenacity) + **jedini owner + grant-nonce** (naš — nitko ne nameće jedinstveni grant po
pokušaju) → ≥ svaki.

---

## 7.2 Idempotency, bounded budgets, backpressure, concurrency (P2.2 + P2.3)

**Tip:** MEHANIZAM (jezgra)

**Aspekti → najbolji izvor:**
- idempotency ključevi + task queue lease → **Temporal**
- lagani durable runtime → **Restate**
- durable orkestracija bez teškog clustera → **Dapr Workflows**

**Sinteza (queue s lease/heartbeat/fencing/CAS-reclaim + ResourceBudget izvršni + cost-checkpoint):**
```go
// --- Queue (P2.3) — POPRAVLJA NEXUS queue.py (nema lease/reclaim za running) ---
type TaskState int // READY|LEASED|RUNNING|COMMIT_PENDING|SUCCEEDED|FAILED|DEAD_LETTER|CANCELLED|UNKNOWN_EFFECT|RECONCILING
type QueueTask struct {
    TaskID string; PayloadHash string; IdempotencyKey string
    State TaskState; AttemptNo, MaxAttempts int
    LeaseID string; OwnerWorkerID string; FencingToken uint64
    LeasedAt, HeartbeatAt, LeaseExpiresAt *time.Time; ResultHash string
}
func (q *Queue) Claim(taskID string, worker string) (QueueTask, error)
// atomski CAS: READY → LEASED uz fencing_token++ (single-winner); commit nosi aktualni token
func (q *Queue) Heartbeat(taskID, leaseID string, token uint64) error // vrijedi samo za aktualni lease
func (q *Queue) Reclaim(expiredLease Lease) (QueueTask, error)
// expiry/crash → LEASED/RUNNING → READY s NOVIM pokušajem i STROGO većim fencing tokenom
// stari token na commit → STALE_FENCING_TOKEN (odbijen); ACK tek nakon durable result commit
// COMMIT_PENDING + nepoznat vanjski učinak → UNKNOWN_EFFECT → RECONCILING (NIKAD ravno u READY)

// --- ResourceBudget (P2.2) — izvršni, ne advisory ---
type ResourceBudget struct { // po runu i tool-attemptu, svaki limit SOFT|HARD
    WallMs, CPUMs, RSSBytes, DiskWriteBytes int64
    FileCount, ProcessCount, SocketCount int
    NetworkInBytes, NetworkOutBytes, OutputBytes int64
    InputTokens, OutputTokens int64; CostMinorUnits int64; ToolCalls, LoopSteps int
}
type ResourceLedger struct { BudgetID, ParentBudgetID string; Reserved, Consumed int64; FencingToken uint64 }
// hard limit → atomically fence + cancel + drain/kill cijelo owned stablo (S1.2 process-group + S6)
// child budget ≤ preostali parent; gaugeovi (rss/process/socket) knjiže PEAK; cleanup ≠ success

// cost-checkpoint (obsidian): budget provjera PRIJE skupe operacije u grafu, ne samo globalni cap
func (b *ResourceBudget) Checkpoint(cost int64) error // over → circuit-breaker (S3 stuck-detection)
```

**Salvage:** NEXUSv2 `core/queue.py` (`_claim` :49 atomski) + `core/circuit.py` (breaker :46,
add_cost :85, over_budget :102) + `core/fleet.py` (:71) → **Python→Go port**, UZ POPRAVAK (NEXUS
nema lease/reclaim/fencing za `running` — dodaje se po P2.3).

**Ugovor/RED:** **P2.3** `test_reclaimed_worker_cannot_commit_with_stale_fence` (A token 7 zamrznut,
B token 8 commitira, A proba sa 7 → `STALE_FENCING_TOKEN`, store ima točno jedan rezultat — B-ov) +
**P2.2** `test_hard_process_budget_kills_descendants` (process_count=2, treći → `RESOURCE_LIMIT_
EXCEEDED`, cijelo owned stablo završava).

**Verifikacija:** Temporal (MIT), Restate (FSL/source-available), Dapr (Apache-2.0) — stvarni
(Restate = source-available, matrica provjeri).

**Pareto floor:** idempotency+lease (Temporal) + laki durable (Restate) + bez clustera (Dapr) +
**fencing-token + atomic-CAS-reclaim + izvršni budget** (naš — popravak slomljenog NEXUS queuea) → ≥ svaki.

---

## 7.3 Crash recovery, durable checkpoints + durable delivery (N9)

**Tip:** MEHANIZAM (jezgra)

**Aspekti → najbolji izvor:**
- checkpoint/resume iz spremljenog stanja → **LangGraph**
- replay iz event historije → **Temporal**
- rekonstrukcija stanja iz event streama → **OpenHands**

**Sinteza (resume = fold event journala; delivery = transactional outbox):**
```go
// --- crash recovery: stanje se REKONSTRUIRA foldom eventa, ne čita iz mutable snapshot ---
type Resume struct{ journal *journal.Journal }
func (r *Resume) Restore(runID string) (machine.State, error)
// idempotent fold eventa (S0.2 + P0.3); checkpoint = journal offset, ne mutable polje
// NIJE deterministički replay bez snimljenih model/tool rezultata + zamrznute okoline (spec 7.3)

// --- durable delivery (N9): run done ≠ result delivered — DVA trajna stanja ---
type Outbox struct{ store *sqlite.Store }
func (o *Outbox) CommitResult(runID string, result Result) error
// TRANSACTIONAL: result + outbox row u ISTOJ transakciji (nema result bez outbox retka)
func (o *Outbox) Deliver(ctx, row OutboxRow) (Receipt, error)
// idempotent delivery (delivery-key) + receipt; gateway crash NAKON izvršenja → outbox row
// preživi → delivery se ponavlja → NO duplikat side-effecta, NO izgubljen odgovor
// receipt potvrđuje "delivered"; bez receipta → retry delivery (S7.1 politika)
```
Obvezno kad se uključe `channels`/`service`/udaljeni `full-UX` (spec 7.3 dopuna).

**Salvage:** NEXUSv2 `core/resume.py` (idempotent resume :29) + `core/events.py` (fold :29) +
`core/queue.py` (outbox primitiv) → **Python→Go port**.

**Ugovor/RED:** **N9** (run-done ≠ result-delivered). RED (naš): crash NAKON `CommitResult` a PRIJE
`Deliver` → outbox row preživi, delivery se ponovi, side-effect točno-jednom (idempotency-key);
gateway crash bez outbox row-a → result NIJE izgubljen (result je durable prije deliveryja).

**Verifikacija:** LangGraph (MIT), Temporal (MIT), OpenHands (MIT) — stvarni.

**Pareto floor:** checkpoint-resume (LangGraph) + event-replay (Temporal) + event-stream
rekonstrukcija (OpenHands) + **transactional outbox + run-done≠delivered** (naš — nitko ne razdvaja
dva trajna stanja eksplicitno) → ≥ svaki.

---

## S7 cross-cutting napomene

1. **S7 = jedini retry-owner (P0.2):** 2.2/3.3/3.4 su DELEGATI — 2.2 mapira error→fallback, 3.3
   emitira user-cancel, 3.4 prikazuje finalni `TypedError`; NIJEDAN ne retry-a. S7 izdaje svaki
   `AttemptGrant` (grant-nonce = jedinstven po pokušaju).
2. **Popravak slomljenog NEXUS queuea:** audit je dokazao da NEXUS `queue.py` ima exactly-once
   claim ali NEMA lease/reclaim/fencing za `running` nakon pada (codex nalaz #18) — Go verzija
   uvodi `FencingToken` (strogo raste po reclaimu) + `Heartbeat` + `Reclaim` + `COMMIT_PENDING→
   UNKNOWN_EFFECT→RECONCILING`, po P2.3 RED testu.
3. **Dva trajna stanja (N9):** `run done` i `result delivered` su ODRVOJENA — outbox row i result
   žive u istoj transakciji; delivery je idempotentan s receiptom.
4. **Concurrency model:** goroutine+channel za fan-out (fleet), `sync.Mutex`/`atomic` za
   shared-state, `context.WithDeadline/WithCancel` za deadline/cancel — bez eksternog durable
   runtimea (Restate/Dapr su obrasci, ne dependencies).
5. **Honest gap:** 7.1 nema dedicirani RED osim P0.2 (retry-owner je gate); 7.2/7.3 imaju P2.2/
   P2.3/N9 izravno. Cost-checkpoint (obsidian) je naš dodatak na P2.2, ne zaseban ugovor.
6. **Verifikacija:** svi kandidati stvarni (Temporal/LangGraph/tenacity MIT/Apache-2.0, Restate
   source-available, Dapr Apache-2.0); salvage `recovery/queue/circuit/resume/fleet/events` interni
   (postoje — audit potvrdio), port je logika + POPRAVAK lease/reclaim.
