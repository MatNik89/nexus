PLAN S7

# Hibrid-Hibrida Sinteza: Sekcija S7 (Pouzdanost i Durable Execution)
## NAJTEŽI TIER-3 UGOVORI: S7 JEDINI RETRY/CANCEL OWNER, FENCING QUEUE I OUTBOX

**Dokument:** `/home/matej/HARNESS/nexus/docs/HARNESS-SPEC.md` (S7.1–S7.3)  
**Normativni temelj:** `/home/matej/HARNESS/nexus/docs/TIER3-CONTRACTS.md` (Annex A P0.2, P1.2, P2.2, P2.3, N9 Outbox)  
**Jezik implementacije:** Go (`CGO_ENABLED=0`, single-binary, `sync/atomic`, `context`, `time`, `database/sql`)  
**Autor sinteze:** agy (Gemini 3.7 Flash)  
**Datum:** 2026-09-01  

---

## 1. Analiza Pouzdanosti: Zašto S7 Nosi Najstrože Ugovore

Sekcija S7 je operativna kičma otpornosti (resilience) cijelog sustava. U audit-konsenzusu identificirane su tri kritične točke:
1. **Podijeljeni autoritet nad retryjem (Riješeno u P0.2):** Prije su S2.2 (provider), S3.3 (loop) i S3.4 (recovery) imali vlastite nezavisne retry petlje, što je dovodilo do skrivenih kaskadnih zastoja. **S7.1 postaje JEDINI autoritet:** izdaje strogo numerirani `AttemptGrant` s jedinstvenim nonceom, dok su ostali moduli samo delegati.
2. **Kvar u Queue mehanizmu (Audit nalaz / Riješeno u P2.3):** Stari `core/queue.py` nije imao heartbeat niti lease mehanizam — ako worker padne tijekom izvođenja (`RUNNING`), zadatak bi zauvijek ostao zaglavljen (orphan task). P2.3 uvodi **Queue Lease + Heartbeat + Fencing Token + Atomski CAS Reclaim**.
3. **Pojam "Završeno" vs "Isporučeno" (Riješeno u N9 Durable Delivery):** `RUN_DONE != RESULT_DELIVERED`. Pad sustava nakon dovršetka zadatka, ali prije uspješne isporuke na udaljeni kanal (Telegram, Slack, webhook), rješava se kroz **Transactional Outbox** u SQLite-u.

---

## 2. Arhitektura S7 Sustava Pouzdanosti

```
┌─────────────────────────────────────────────────────────────────────────────────────────┐
│                                   S7 RELIABILITY ENGINE                                 │
│                                                                                         │
│  ┌───────────────────────────────────────────────────────────────────────────────────┐  │
│  │ 7.1 UNIFIED EXECUTION & RETRY OWNER (Annex A P0.2)                                 │  │
│  │   • Single Retry Authority • AttemptGrant Issuer • Unified Error Taxonomy         │  │
│  │   • Deadline & Cancellation Hierarchy (context.Context Tree)                      │  │
│  └─────────────────────────────────────────┬─────────────────────────────────────────┘  │
│                                            │                                            │
│        ┌───────────────────────────────────┴───────────────────────────────────┐        │
│        ▼                                                                       ▼        │
│  ┌─────────────────────────────────────────┐   ┌─────────────────────────────────────┐  │
│  │ 7.2 CONCURRENCY, QUEUE & BUDGETS        │   │ 7.3 CRASH RECOVERY & DURABLE OUTBOX │  │
│  │   • Task Lease & Heartbeat (P2.3)       │   │   • Two-Phase State (Done ≠ Deliver)│  │
│  │   • Monotonic Fencing Token (CAS)       │   │   • Transactional Outbox (N9)       │  │
│  │   • Bounded Resource Budgets (P2.2)     │   │   • Startup Orphan & Lock Sweep P1.2│  │
│  │     (CPU / RAM / Disk / Socket limits)  │   │   • SQLite Durable Checkpointing    │  │
│  └─────────────────────────────────────────┘   └─────────────────────────────────────┘  │
└─────────────────────────────────────────────────────────────────────────────────────────┘
```

---

## S7.1: Timeout, Cancellation, Propagacija i Taksonomija Grešaka (Jedini Recovery Owner)

- **Tip:** `MEHANIZAM` (Go paket `pkg/reliability/recovery`, autoritet nad S2.2/S3.3/S3.4)
- **Aspekti i najbolji izvori:**
  - *Aspekt A (Jedinstveni autoritet za pokušaje i AttemptGrant semantika):* **Temporal + Annex A P0.2** — svaki fizički poziv prema provideru ili alatu mora predočiti važeći `AttemptGrant(attempt_no, grant_nonce, expires_at)` izdan od strane S7. Nijedan adapter ne smije samostalno ponavljati pozive.
  - *Aspekt B (Unificirana hijerarhija i taksonomija grešaka):* **tenacity + Annex A P0.2** — striktna klasifikacija grešaka (`VALIDATION`, `AUTHN`, `POLICY`, `RESOURCE`, `TIMEOUT`, `CANCELLED`, `DEPENDENCY`, `UNKNOWN_EFFECT`) koja određuje je li greška `RETRYABLE_NOW`, `RETRY_AFTER_COOLDOWN` ili `TERMINAL_FAIL_CLOSED`.
  - *Aspekt C (Deterministička propagacija otkaza kroz context stablo):* **LangGraph + Go Context** — otkazivanje krovnog zadatka trenutno propagira `context.Canceled` na sve povezane goroutine, podprocese i mrežne sockete.
- **Sinteza (Go dizajn skica):**
  ```go
  package recovery

  import (
      "context"
      "crypto/rand"
      "encoding/hex"
      "fmt"
      "sync"
      "time"

      "nexus/pkg/contract"
  )

  type ExecutionPolicy struct {
      OperationID     string                 `json:"operation_id"`
      EffectClass     contract.EffectClass   `json:"effect_class"`
      Deadline        time.Time              `json:"deadline"`
      MaxAttempts     int                    `json:"max_attempts"`
      AttemptTimeout  time.Duration          `json:"attempt_timeout"`
      BackoffStrategy string                 `json:"backoff_strategy"` // "EXPONENTIAL" | "FIXED" | "NONE"
      InitialInterval time.Duration          `json:"initial_interval"`
      MaxInterval     time.Duration          `json:"max_interval"`
  }

  type AttemptGrant struct {
      OperationID string    `json:"operation_id"`
      AttemptNo   int       `json:"attempt_no"`
      TargetID    string    `json:"target_id"`
      IssuedAt    time.Time `json:"issued_at"`
      ExpiresAt   time.Time `json:"expires_at"`
      GrantNonce  string    `json:"grant_nonce"`
  }

  type ExecutionSupervisor struct {
      mu       sync.Mutex
      attempts map[string]int // operationID -> currentAttempt
  }

  func NewExecutionSupervisor() *ExecutionSupervisor {
      return &ExecutionSupervisor{attempts: make(map[string]int)}
  }

  func (es *ExecutionSupervisor) IssueGrant(policy ExecutionPolicy, targetID string) (*AttemptGrant, context.Context, context.CancelFunc, error) {
      es.mu.Lock()
      defer es.mu.Unlock()

      current := es.attempts[policy.OperationID] + 1
      if current > policy.MaxAttempts {
          return nil, nil, nil, fmt.Errorf("MAX_ATTEMPTS_EXCEEDED: operation %s reached limit of %d", policy.OperationID, policy.MaxAttempts)
      }

      if time.Now().After(policy.Deadline) {
          return nil, nil, nil, fmt.Errorf("DEADLINE_EXCEEDED: operation %s passed deadline %s", policy.OperationID, policy.Deadline)
      }

      es.attempts[policy.OperationID] = current

      nonceBytes := make([]byte, 16)
      _, _ = rand.Read(nonceBytes)
      nonce := hex.EncodeToString(nonceBytes)

      grantExpires := time.Now().Add(policy.AttemptTimeout)
      if grantExpires.After(policy.Deadline) {
          grantExpires = policy.Deadline
      }

      grant := &AttemptGrant{
          OperationID: policy.OperationID,
          AttemptNo:   current,
          TargetID:    targetID,
          IssuedAt:    time.Now(),
          ExpiresAt:   grantExpires,
          GrantNonce:  nonce,
      }

      ctx, cancel := context.WithDeadline(context.Background(), grantExpires)
      return grant, ctx, cancel, nil
  }

  func (es *ExecutionSupervisor) EvaluateFailure(policy ExecutionPolicy, err *contract.TypedError) (shouldRetry bool, retryAfter time.Duration) {
      if err.Retryability == contract.RetryNever || err.Category == contract.ErrPolicy || err.Category == contract.ErrCancelled {
          return false, 0
      }

      es.mu.Lock()
      attemptsUsed := es.attempts[policy.OperationID]
      es.mu.Unlock()

      if attemptsUsed >= policy.MaxAttempts {
          return false, 0
      }

      // Za nepovratne akcije (IRREVERSIBLE), zabrani automatski retry bez eksplicitne pomirbe (P0.2)
      if policy.EffectClass == contract.EffectIrreversible && err.Category == contract.ErrUnknownEffect {
          return false, 0
      }

      // Eksponencijalni backoff
      backoff := policy.InitialInterval * (1 << (attemptsUsed - 1))
      if backoff > policy.MaxInterval {
          backoff = policy.MaxInterval
      }
      return true, backoff
  }
  ```
- **Salvage iz NEXUSv2:**
  - Port i konsolidacija `core/recovery.py` i `core/circuit.py` u Go `pkg/reliability/recovery`, uz preuzimanje punog vlasništva nad retry logikom iz providera i petlje.
- **Ugovor / RED Gate:**
  - **Annex A P0.2**; Gate test: `test_adapter_cannot_self_retry` (ako adapter pokuša sam napraviti retry s istim grantom, poziv pada s `ATTEMPT_NOT_AUTHORIZED`).
- **Verifikacija aspekata:**
  - Temporal Go SDK (MIT, industrijski standard za durable retry), tenacity (Apache-2.0).
- **Pareto-floor potvrda:**
  - Eliminira podijeljeni autoritet retryja i jamči determinističku propagaciju timeouta i otkaza kroz cijeli sustav.

---

## S7.2: Idempotencija, Bounded Resource Budgets, Concurrency i Queue Fencing

- **Tip:** `MEHANIZAM` (Go paket `pkg/reliability/queue` i `pkg/reliability/budget`)
- **Aspekti i najbolji izvori:**
  - *Aspekt A (Queue Lease, Heartbeat i Monotoni Fencing Token — Popravak Kvara):* **Restate / Dapr + Annex A P2.3** — **POPRAVAK NEXUS AUDIT NALAZA:** Svaki zadatak u redu ima zakupljeni rok (`lease_expires_at`) i monotono rastući `fencing_token`. Ako radnik zamrzne ili padne, lease ističe; drugi radnik preuzima zadatak atomskim CAS upitom i dobiva viši fencing token (`token = token + 1`). Kada se stari radnik probudi, njegov upis se odbija (`STALE_FENCING_TOKEN`).
  - *Aspekt B (Izvršni Bounded Resource Budgets):* **Annex A P2.2** — postavljanje tvrdih operacijskih limita:
    1. `max_cpu_ms` (CPU vrijeme).
    2. `max_rss_bytes` (Maksimalna radna memorija, npr. 512 MB).
    3. `max_disk_write_bytes` (Limit pisanja na disk, npr. 50 MB).
    4. `max_subprocesses` i `max_open_sockets` (Zabrana fork-bombi i curenja deskriptora).
  - *Aspekt C (Konkurentnost i Backpressure):* **Go Primitivi (`sync.Pool`, semaphore, buffered channels)** — ograničavanje broja istovremenih radnika i kontrolirano odbijanje preopterećenja.
- **Sinteza (Go dizajn skica):**
  ```go
  package queue

  import (
      "context"
      "database/sql"
      "fmt"
      "sync/atomic"
      "time"

      "nexus/pkg/contract"
  )

  type QueueTaskState string
  const (
      TaskReady          QueueTaskState = "READY"
      TaskLeased         QueueTaskState = "LEASED"
      TaskRunning        QueueTaskState = "RUNNING"
      TaskCommitPending  QueueTaskState = "COMMIT_PENDING"
      TaskSucceeded      QueueTaskState = "SUCCEEDED"
      TaskFailedTerminal QueueTaskState = "FAILED_TERMINAL"
  )

  type DurableQueueManager struct {
      db       *sql.DB
      workerID string
  }

  // ClaimTask provodi atomski CAS zakup uz inkrement fencing tokena (P2.3 Invarijant)
  func (qm *DurableQueueManager) ClaimTask(ctx context.Context, leaseDuration time.Duration) (*contract.QueueTask, error) {
      tx, err := qm.db.BeginTx(ctx, nil)
      if err != nil {
          return nil, err
      }
      defer tx.Rollback()

      now := time.Now()
      leaseUntil := now.Add(leaseDuration)

      // Nađi zadatak koji je READY ili čiji je LEASE istekao (Orphan/Crash Reclaim)
      query := `SELECT task_id, attempt_no, max_attempts, fencing_token, payload_hash, idempotency_key 
                FROM durable_tasks 
                WHERE state = 'READY' OR (state IN ('LEASED', 'RUNNING') AND lease_expires_at < ?)
                ORDER BY created_at ASC LIMIT 1`

      var task contract.QueueTask
      err = tx.QueryRowContext(ctx, query, now.Unix()).Scan(
          &task.TaskID, &task.AttemptNo, &task.MaxAttempts, &task.FencingToken, &task.PayloadHash, &task.IdempotencyKey,
      )
      if err != nil {
          if err == sql.ErrNoRows {
              return nil, nil // Nema dostupnih zadataka
          }
          return nil, err
      }

      // Inkrementiraj fencing token i attempt (CAS zaštita od split-braina)
      newFencingToken := task.FencingToken + 1
      newAttempt := task.AttemptNo + 1
      if newAttempt > task.MaxAttempts {
          _, _ = tx.ExecContext(ctx, "UPDATE durable_tasks SET state = 'FAILED_TERMINAL' WHERE task_id = ?", task.TaskID)
          return nil, fmt.Errorf("task %s exceeded max attempts", task.TaskID)
      }

      updateQuery := `UPDATE durable_tasks 
                      SET state = 'LEASED', owner_worker_id = ?, fencing_token = ?, attempt_no = ?, 
                          leased_at = ?, lease_expires_at = ?, heartbeat_at = ?
                      WHERE task_id = ? AND fencing_token = ?`

      res, err := tx.ExecContext(ctx, updateQuery, qm.workerID, newFencingToken, newAttempt, now.Unix(), leaseUntil.Unix(), now.Unix(), task.TaskID, task.FencingToken)
      if err != nil {
          return nil, err
      }
      rows, _ := res.RowsAffected()
      if rows == 0 {
          return nil, fmt.Errorf("CAS_CONFLICT: task was claimed by another worker concurrently")
      }

      if err := tx.Commit(); err != nil {
          return nil, err
      }

      task.State = TaskLeased
      task.FencingToken = newFencingToken
      task.AttemptNo = newAttempt
      task.OwnerWorkerID = &qm.workerID
      return &task, nil
  }

  // CommitResult provjerava važeći fencing token prije trajnog upisa
  func (qm *DurableQueueManager) CommitResult(ctx context.Context, taskID string, fencingToken int64, resultHash string) error {
      query := `UPDATE durable_tasks 
                SET state = 'SUCCEEDED', result_hash = ?, finished_at = ?
                WHERE task_id = ? AND fencing_token = ?`

      res, err := qm.db.ExecContext(ctx, query, resultHash, time.Now().Unix(), taskID, fencingToken)
      if err != nil {
          return err
      }
      rows, _ := res.RowsAffected()
      if rows == 0 {
          return fmt.Errorf("STALE_FENCING_TOKEN: commit rejected because task was reclaimed by another worker")
      }
      return nil
  }

  // --- Bounded Resource Budget Execution (Annex A P2.2) ---

  type ResourceLimits struct {
      MaxCPUSeconds    float64
      MaxMemoryRSSByte int64
      MaxDiskWriteByte int64
      MaxProcesses     int
      MaxOpenFDs       int
  }

  type ResourceLedger struct {
      activeMemoryBytes int64
      activeProcesses   int32
  }

  func (rl *ResourceLedger) AcquireProcess(limits ResourceLimits) error {
      current := atomic.AddInt32(&rl.activeProcesses, 1)
      if int(current) > limits.MaxProcesses {
          atomic.AddInt32(&rl.activeProcesses, -1)
          return fmt.Errorf("RESOURCE_LIMIT_EXCEEDED: process count limit (%d) reached", limits.MaxProcesses)
      }
      return nil
  }

  func (rl *ResourceLedger) ReleaseProcess() {
      atomic.AddInt32(&rl.activeProcesses, -1)
  }
  ```
- **Salvage iz NEXUSv2:**
  - Potpuni rewrite neispravnog `core/queue.py` i `core/fleet.py` u Go `pkg/reliability/queue` s ugrađenom CAS fencing token zaštitom i SQLite WAL transakcijama.
- **Ugovor / RED Gate:**
  - **Annex A P2.2 & P2.3**; Gate testovi:
    1. `test_reclaimed_worker_cannot_commit_with_stale_fence` (zamrznuti worker s tokenom 7 ne može komitati ako je novi worker preuzeo zadatak s tokenom 8).
    2. `test_hard_process_budget_kills_descendants` (prekoračenje limita procesa ili memorije ruši proces alata, a ne harness).
- **Verifikacija aspekata:**
  - Restate durable execution (Apache-2.0), Dapr virtual actors (Apache-2.0), SQLite WAL CAS semantics.
- **Pareto-floor potvrda:**
  - Rješava problem zombi-zadataka nakon pada sustava i jamči da niti jedan worker ne može prebrisati tuđi rad zbog zastarjelog stanja.

---

## S7.3: Crash-Recovery, Checkpointi i N9 Transactional Outbox (Durable Delivery)

- **Tip:** `MEHANIZAM` (Go paket `pkg/reliability/outbox`)
- **Aspekti i najbolji izvori:**
  - *Aspekt A (Dvofazno Stanje: Završetak Rada != Uspješna Isporuka):* **HARNESS-SPEC v0.9 + N9 Consensus** — `RUN_DONE != RESULT_DELIVERED`. Završetak izvođenja modela sprema se lokalno u SQLite, dok se isporuka vanjskom primatelju (Telegram/Slack/Webhook/API) knjiži kroz zasebno outbox stanje.
  - *Aspekt B (Transactional Outbox Pattern):* **N9 Durable Delivery + Dapr / MassTransit** — poruke za slanje zapisuju se u istu SQLite transakciju kao i završni event zadatka; pozadinski outbox dispatcher šalje poruke s eksponencijalnim retryjem i idempotencijskim ključevima (`idempotency_key = run_id + ":" + turn_id`).
  - *Aspekt C (Startup Recovery i Orphan Process Sweep):* **Annex A P1.2 (agy#2)** — pri ponovnom pokretanju sustava, harness prvo skenira `ResourceLease` tablicu, šalje `SIGKILL` zaostalim procesnim grupama (`-pgid`), otključava worktreeje i nastavlja nedovršene outbox isporuke.
- **Sinteza (Go dizajn skica):**
  ```go
  package outbox

  import (
      "context"
      "database/sql"
      "fmt"
      "time"

      "nexus/pkg/contract"
  )

  type OutboxMessageState string
  const (
      OutboxStaged    OutboxMessageState = "STAGED"
      OutboxSending   OutboxMessageState = "SENDING"
      OutboxDelivered OutboxMessageState = "DELIVERED"
      OutboxFailed    OutboxMessageState = "FAILED_RETRYABLE"
  )

  type OutboxMessage struct {
      MessageID      string             `json:"message_id"`
      RunID          string             `json:"run_id"`
      Destination    string             `json:"destination"` // "telegram" | "slack" | "webhook"
      Payload        []byte             `json:"payload"`
      IdempotencyKey string             `json:"idempotency_key"`
      State          OutboxMessageState `json:"state"`
      Attempts       int                `json:"attempts"`
      NextRetryAt    time.Time          `json:"next_retry_at"`
  }

  type TransactionalOutboxManager struct {
      db *sql.DB
  }

  // StageDelivery zapisuje rezultat zadatka i outbox poruku u JEDNOJ atomskoj SQLite transakciji
  func (tom *TransactionalOutboxManager) StageDelivery(ctx context.Context, runID string, destination string, payload []byte, idempotencyKey string) error {
      tx, err := tom.db.BeginTx(ctx, nil)
      if err != nil {
          return err
      }
      defer tx.Rollback()

      // 1. Zabilježi lokalni dovršetak (RUN_DONE)
      _, err = tx.ExecContext(ctx, "UPDATE runs SET state = 'SUCCEEDED', finished_at = ? WHERE run_id = ?", time.Now().Unix(), runID)
      if err != nil {
          return fmt.Errorf("failed updating run state: %w", err)
      }

      // 2. Ubaci poruku u outbox (STAGED)
      outboxQuery := `INSERT INTO durable_outbox (message_id, run_id, destination, payload, idempotency_key, state, attempts, next_retry_at, created_at)
                      VALUES (?, ?, ?, ?, ?, 'STAGED', 0, ?, ?)`
      msgID := fmt.Sprintf("outbox-%s-%d", runID, time.Now().UnixNano())
      _, err = tx.ExecContext(ctx, outboxQuery, msgID, runID, destination, payload, idempotencyKey, time.Now().Unix(), time.Now().Unix())
      if err != nil {
          return fmt.Errorf("failed staging outbox message: %w", err)
      }

      return tx.Commit()
  }

  // DispatchPending pokreće pozadinski radnik koji šalje poruke
  func (tom *TransactionalOutboxManager) DispatchPending(ctx context.Context, senderFunc func(dest string, payload []byte, idempKey string) error) error {
      now := time.Now().Unix()
      rows, err := tom.db.QueryContext(ctx, `SELECT message_id, destination, payload, idempotency_key, attempts 
                                             FROM durable_outbox 
                                             WHERE state IN ('STAGED', 'FAILED_RETRYABLE') AND next_retry_at <= ?
                                             LIMIT 10`, now)
      if err != nil {
          return err
      }
      defer rows.Close()

      for rows.Next() {
          var msgID, dest, idempKey string
          var payload []byte
          var attempts int
          if err := rows.Scan(&msgID, &dest, &payload, &idempKey, &attempts); err != nil {
              continue
          }

          // Pokušaj slanja preko mrežnog adaptera
          sendErr := senderFunc(dest, payload, idempKey)
          if sendErr == nil {
              _, _ = tom.db.ExecContext(ctx, "UPDATE durable_outbox SET state = 'DELIVERED', delivered_at = ? WHERE message_id = ?", time.Now().Unix(), msgID)
          } else {
              nextRetry := time.Now().Add(time.Duration(1<<attempts) * time.Second).Unix()
              _, _ = tom.db.ExecContext(ctx, "UPDATE durable_outbox SET state = 'FAILED_RETRYABLE', attempts = attempts + 1, next_retry_at = ? WHERE message_id = ?", nextRetry, msgID)
          }
      }
      return nil
  }
  ```
- **Salvage iz NEXUSv2:**
  - Port `core/resume.py` i povezivanje s transactional outbox mehanizmom u Go `pkg/reliability/outbox`.
- **Ugovor / RED Gate:**
  - **Annex A P1.2 & N9 Outbox**; RED test: `test_crash_after_run_done_resumes_and_delivers_outbox_message`.
- **Verifikacija aspekata:**
  - Transactional Outbox Pattern (Enterprise Integration Patterns standard), Dapr outbox (Apache-2.0).
- **Pareto-floor potvrda:**
  - Garantira točno-jednom semantiku obrade uz barem-jednom sigurnu isporuku (At-Least-Once Delivery with Deduplication) prema vanjskim klijentima.

---

## Rezime S7 Hibrid-Hibrida Sinteze

| Podsekcija | Tip | Ključni graftani izvori & Diferencijatori | Ugovor / RED Gate |
| :--- | :---: | :--- | :--- |
| **7.1 Unified Recovery** | MEHANIZAM | • **Temporal:** Jedinstveni autoritet za retry i `AttemptGrant` izdavač<br>• **tenacity:** Unificirana taksonomija grešaka | **Annex A P0.2** (`test_adapter_cannot_self_retry`) |
| **7.2 Queue & Budgets** | MEHANIZAM | • **Restate / Dapr:** **Fencing Token + Heartbeat Lease** (ispravak starog queue kvara)<br>• **Annex A P2.2:** Izvršni Bounded Budgets (CPU/RAM/Socket cap) | **Annex A P2.2 & P2.3** (`test_reclaimed_worker_cannot_commit_with_stale_fence`, `test_hard_process_budget_kills_descendants`) |
| **7.3 Durable Outbox** | MEHANIZAM | • **N9 Consensus:** **Transactional Outbox Pattern** (`RUN_DONE != RESULT_DELIVERED`)<br>• **Annex A P1.2:** **Startup Orphan & Lock Sweep** | **Annex A P1.2 & N9** (`test_crash_after_run_done_resumes_and_delivers_outbox_message`) |
