# DESIGN — memorija-descope + archmap + effect-path (greenfield Go, CGO_ENABLED=0)

## A) S9.4 P0 (Decay+Audn) + S8.3 archmap

### A1. Decay (FSRS-lite) — P0

```go
type RecallTier int // HOT | WARM | COLD

type Decay struct{ halfLife time.Duration } // po scope-u (LESSON kraći od USER)

func (d Decay) Score(age time.Duration) float64
// score = exp(-age * ln(2) / halfLife)  → [0,1], monotono opadajuće

func (d Decay) Tier(score float64) RecallTier
// ≥0.7 HOT · ≥0.2 WARM · <0.2 COLD

type MemoryEntry struct {
    ID            string
    Scope         Scope      // PROJECT|USER|TASK|LESSON|EPHEMERAL
    Claim         string     // normirana tvrdnja (ključ za Audn)
    Content       []byte     // NIKAD se ne briše (arhiva, lossless)
    TrustClass    TrustClass // P0.3: UNTRUSTED|USER|SYSTEM
    CreatedAt     time.Time
    HalfLife      time.Duration
    SupersededBy  *string    // Audn veza
    ContentionCnt int
}
```

Invarijante:
- `Score` je deterministički i monotono opadajući po `age`.
- `Tier(0)` = COLD, ali `Content` UVIJEK dohvatljiv iz arhive (decay mijenja RANG, ne postojanje).

### A2. Audn (ADD/NOOP/SUPERSEDE) — P0

```go
type Resolution int // ADD | NOOP | SUPERSEDE

type Audn struct{}

func (a Audn) Resolve(existing, incoming MemoryEntry) (Resolution, MemoryEntry)
// isti Claim + isti entitet → SUPERSEDE (incoming.SupersededBy = existing.ID, noviji wins)
// isti Claim + konflikt → NOOP + ContentionCnt++ (NIKAD silent overwrite)
// novi Claim → ADD
```

Invarijante:
- Nijedan write NE prebrisava postojeći claim tiho; svaki SUPERSEDE nosi `SupersededBy` vezu.
- NOOP inkrementira `ContentionCnt` (signal za 15.5 semantic-health, ne tihi drop).

### A3. MemoryStore (P0, `memorija` flag)

```go
type MemoryStore interface {
    Write(ctx, e MemoryEntry, ap Approval) error       // write-approval DEFAULT ON (9.2)
    Recall(ctx, q Query) ([]MemoryEntry, error)        // scope-filter + decay-score sort
    Forget(ctx, id string) error                        // tombstone (reverzibilno) — P2.1
    Restore(ctx, id string) error                       // undo Forget
}
```

RED:
- `test_decay_never_destroys_bytes` — score=0 → COLD, ali `Content` čitljiv (arhiva); decay ne briše.
- `test_audn_supersede_not_overwrite` — isti claim → SUPERSEDE, stari entry očuvan sa `SupersededBy`.
- `test_forget_tombstone_restorable` — Forget → tombstone (ne hard-delete), Restore vraća entry.

### A4. ArchMap (S8.3 — JEDINI preživjeli USP)

```go
type NodeKind int // PACKAGE | FILE | MODULE | COMPONENT
type Layer    int // DOMAIN | APPLICATION | INFRASTRUCTURE
type EdgeKind int // DEPENDS_ON | IMPLEMENTS | USES

type ArchNode struct {
    ID       string   // modul path
    Kind     NodeKind
    Layer    Layer
    Symbols  []string // izvozi (tree-sitter)
    Lines    int
    SourceURI string  // P0.3 provenance
}
type ArchEdge struct{ From, To string; Kind EdgeKind }

type ArchMap struct {
    g   *Graph[ArchNode, ArchEdge]
    rev string // repo revision (freshness)
}

// IZGRADNJA: tree-sitter (simboli) + import-graf (DEPENDS_ON) + sloj heuristika (path/dir)
func Build(repo string, idx *CodeIndex) (*ArchMap, error)

// QUERY API — model PITA arhitekturu, ne skenira:
type ArchQuery interface{}
type OwnersQuery     struct{ Symbol string }
type DependentsQuery struct{ Module string }
type LayerQuery      struct{ Layer Layer }
type PathQuery       struct{ From, To string }

func (a *ArchMap) Query(q ArchQuery) ([]ArchNode, []ArchEdge, error)

// FRESHNESS: rev != HEAD → STALE (odbij kao dokaz, ne tiho)
func (a *ArchMap) IsFresh(repo string) (bool, error)
```

Invarijante:
- Svaki `ArchNode` nosi `SourceURI` (dokaz); `Query` rezultat je datamarked-untrusted (P0.3).
- Stale `rev` → `Query` vraća `STALE` error, ne rezultate.

RED:
- `test_archmap_owner_query` — `OwnersQuery(symbol)` → točan modul (graf, ne text-grep).
- `test_archmap_dependents_query` — `DependentsQuery(m)` → svi ovisni moduli (edge-traversal).
- `test_archmap_stale_rejected` — `rev != HEAD` → `Query` = `STALE` error.

---

## B) EFFECT-PATH — JEDAN owner

### B1. Jedini owneri (end-to-end map)

| Faza | JEDINI owner | Što posjeduje |
|---|---|---|
| policy-decision (ALLOW/ASK/DENY) | **S6.0 PEP** | jedini odlučuje; S3/S4 NE pitaju, samo predaju |
| lifecycle-order (before_tool/on_error) | **S6.9 Middleware** | jedini poredak hookova; on_error=grana |
| process-tree (spawn/kill/detach) | **S1.2 Process** | jedini spawna/killa stablo (PID+start-token, killpg/taskkill) |
| retry/deadline/cancel | **S7 RetryOwner** | jedini izdaje `AttemptGrant`; delegati (2.2/3.3/3.4) ne retry-aju |

```go
// Put: S3.Loop → S4.Tool → S6.0.PEP → S6.9 → S1.2.Process → S7
type EffectPath struct {
    pep     *s6.PEP
    mw      *s6.MiddlewareChain
    proc    *s1.Process
    retry   *s7.RetryOwner
}

func (p EffectPath) RunTool(ctx, call ToolCall) (ToolResult, error) {
    d := p.pep.Decide(call)                 // 1) S6.0 policy (JEDINI)
    if d == DENY { return denied(call) }
    if err := p.mw.BeforeTool(call); err != nil { return p.mw.OnError(err) } // 2) S6.9
    out, err := p.proc.Spawn(call)          // 3) S1.2 process-tree (JEDINI)
    return p.retry.Classify(call, out, err) // 4) S7 retry/cancel (JEDINI)
}
```

Invarijante:
- Nema zaobilaznog puta: S4.Tool NE smije spawnati proces direktno (mora kroz S1.2), ni retry-ati
  sam (mora kroz S7), ni zaobići PEP.

### B2. Effect-taxonomy (rješava nemogući "cancel→nula side-effect")

```go
type EffectClass int // READ_ONLY | REVERSIBLE | IRREVERSIBLE
type EffectPhase int // BEFORE_COMMIT | AFTER_COMMIT | UNKNOWN

type ToolResult struct {
    EffectPhase EffectPhase // BEFORE_COMMIT=ništa · AFTER_COMMIT=gotovo · UNKNOWN=nepoznato
    // ...
}
```

Pravila:
- **cancel u BEFORE_COMMIT** → spriječi side-effect (nije commitano).
- **cancel u AFTER_COMMIT** → side-effect POSTOJI; ne može se vratiti; `ToolResult.EffectPhase=AFTER_COMMIT`.
- **cancel/abort USRED** → `EffectPhase=UNKNOWN` → S7: `IRREVERSIBLE+UNKNOWN` se NE retry-a bez
  reconciliationa (P0.2) → `UNKNOWN_EFFECT→RECONCILING`.

RED:
- `test_cancel_before_commit_prevents` — cancel u BEFORE_COMMIT → side-effect NE nastaje.
- `test_cancel_after_commit_is_after_commit` — cancel nakon commit → rezultat POSTOJI, EffectPhase=AFTER_COMMIT.
- `test_unknown_effect_not_retried` — IRREVERSIBLE + UNKNOWN → S7 NE izdaje novi grant (RECONCILING).

### B3. 2.3 re-ask vs S7

```go
type ReAsk struct {
    Grant    s7.AttemptGrant // svaki re-ask = NOVI grant (S7 owner)
    Feedback *TypedError     // zašto je prethodni pokušaj odbijen (2.3)
}

func (s *StructuredOutput[T]) ReAsk(g s7.AttemptGrant, prev *TypedError) (T, error)
// re-ask je MODEL-POKUŠAJ → MORA nositi AttemptGrant; S7 broji (max_attempts), 2.3 ne broji
```

Invarijanta:
- 2.3 NE smije petljati re-ask samostalno; svaki re-ask je S7 `AttemptGrant` (grant-nonce jedinstven).

RED:
- `test_reask_carries_attempt_grant` — re-ask bez S7 granta → odbij; re-ask s grantom → S7 broji
  pokušaj (max_attempts enforcement).

---

## ŠTO OSTAIE v2 (S9.4 — descope)

- `Dream` (epizode→gist+KG offline), `MemGit` (git-verzija write-a), `MemAssoc` (spreading-activation),
  temporalni-upiti ("što sam znao TADA") — sve v2, NEMA izvora (greenfield), ne blokira P0.
- P0 = Decay(FSRS-lite) + Audn + MemoryStore(Forget/Restore) — svjetski izvori: FSRS (open-spaced-
  repetition), CrewAI `analyze_for_consolidation` (konsolidacija-obrazac), Hermes `audn` (AUDN-obrazac).
