# DESIGN-FIXES-r2 — zatvara REVIEW2 kritične blokere (K1-K4 + vlasništvo)

Zatvara nalaze iz REVIEW2-CONSOLIDATED. Buildable Go, invarijante, RED. Nadjačava suprotne dijelove
ranijih DESIGN-*.md (navedeno po fixu).

---

## K1+K2 — EffectPath: sandbox U putu + InProcess/Sandboxed grananje
Nadjačava `DESIGN-memory-effectpath-kilo.md` §B (RunTool bez S6.2 + proc.Spawn za sve).

```go
// --- enumi (S0 contracts; default 0 = INVALID, fail-closed) ---
type Decision uint8
const ( DecisionInvalid Decision = iota; DecisionAllow; DecisionAsk; DecisionDeny )
type ExecutionKind uint8
const ( ExecKindInvalid ExecutionKind = iota; ExecInProcess; ExecProcess )
type EffectPhase uint8
const ( PhaseInvalid EffectPhase = iota; PhaseBeforeCommit; PhaseAfterCommit; PhaseUnknown )

// ToolSpec (S4 4.1) PEČATI ExecutionKind; ToolCall nosi resolved kopiju iz zapečaćenog spec-a
// (dopuna kanonskom ToolCall u DESIGN-S0 — polje `ExecutionKind ExecutionKind`; ne od modela). Fix V-K1.

// --- sučelja s JEDNIM potpisom (fix R5-C01: OnError uvijek `error`) ---
type ToolExecutor interface { Execute(ctx context.Context, c contracts.ToolCall) (contracts.ToolResult, error) }
type Middleware interface {
    BeforeTool(ctx context.Context, c contracts.ToolCall) error
    AfterTool(ctx context.Context, c contracts.ToolCall, r contracts.ToolResult) error // veto = non-nil
    OnError(ctx context.Context, e error) error                                        // audit/lifecycle grana; vraća (možda wrap) error
}
type RetryOwner interface {
    Record(g s7.AttemptGrant, r contracts.ToolResult, e error) (contracts.ToolResult, error)
    RecordVeto(g s7.AttemptGrant, ph EffectPhase, e error) (contracts.ToolResult, error) // AFTER_COMMIT/UNKNOWN→RECONCILING
}
// classifyEffectPhase: BEFORE_COMMIT ako učinak nije počeo; AFTER_COMMIT ako je potvrđen; UNKNOWN inače.
func classifyEffectPhase(c contracts.ToolCall, r contracts.ToolResult) EffectPhase

type SandboxedProcessExecutor struct { sandbox sandbox.Backend; proc *s1.ProcessTracker } // S6.2 obavezan; S1.2 UNUTAR Launch
type InProcessExecutor struct{ /* edit/read/grep/archmap/memory — čista Go funkcija */ }

type EffectPath struct {
    pep    *s6.PEP; mw Middleware
    inproc ToolExecutor; sbproc ToolExecutor   // sbproc = *SandboxedProcessExecutor (sandbox u sebi)
    retry  RetryOwner
}

func (p EffectPath) RunTool(ctx context.Context, call contracts.ToolCall, grant s7.AttemptGrant) (contracts.ToolResult, error) {
    switch p.pep.Decide(call) {                                              // 1) S6.0 total switch + default-deny
    case DecisionAllow: // nastavi
    case DecisionAsk:
        if !p.pep.ApprovedExact(call) { return needsApproval(call), nil }    //    exact-intent (P1.4) ili staje
    case DecisionDeny:  return denied(call), nil
    default:            return contracts.ToolResult{}, ErrInvalidDecision    //    zero/nepoznat → deny (N4-C01)
    }
    if err := p.mw.BeforeTool(ctx, call); err != nil {                       // 2) S6.9 order
        return contracts.ToolResult{}, p.mw.OnError(ctx, err)
    }
    var exec ToolExecutor                                                    // 3) total switch po pečatu (N4-C02)
    switch call.ExecutionKind {
    case ExecInProcess: exec = p.inproc
    case ExecProcess:   exec = p.sbproc                                      //    → S6.2.Launch(S1.2 unutra)
    default:            return contracts.ToolResult{}, p.mw.OnError(ctx, ErrUnknownExecKind) // reject, NE inproc
    }
    out, err := exec.Execute(ctx, call)                                      // 4) izvršenje
    if err != nil {
        return p.retry.Record(grant, contracts.ToolResult{}, p.mw.OnError(ctx, err))
    }
    if verr := p.mw.AfterTool(ctx, call, out); verr != nil {                 //    post-exec veto (N3-C02/N4-C03)
        oe := p.mw.OnError(ctx, verr)                                        //    kroz OnError (audit/lifecycle)
        return p.retry.RecordVeto(grant, classifyEffectPhase(call, out), oe) //    AFTER_COMMIT/UNKNOWN→RECONCILING
    }
    return p.retry.Record(grant, out, nil)                                   // 5) S7 (JEDINI retry)
}
```
**Invarijante:** (1) grananje po zapečaćenom `ToolSpec.ExecutionKind` — read-only shell (`cat`,`git log`)
je proces → sandbox; zero/nepoznat kind → **reject, NE inproc**. (2) `Decide` default-deny. (3) in-process
alat NIKAD ne spawna; `proc.Spawn` nedostupan mimo `sbproc.sandbox.Launch`. (4) ASK≠ALLOW. (5) svaki
err-put → **`OnError` (vraća `error`, jedan potpis)** + eksplicitno prazan `ToolResult{}`; veto nakon commita
→ `RecordVeto(phase)` (P1.4 UNKNOWN→RECONCILING, ne slijepi retry). (6) grant PRIJE Execute.
**RED:** `TestUnknownDecisionDenies`; `TestUnknownExecKindRejectsNotInproc`; `TestReadOnlyProcessStillSandboxed`;
`TestAskRequiresExactApproval`; `TestVetoThroughOnErrorReconciles`; `TestExecErrorSkipsAfterToolAndOutput`;
`TestInProcessToolNeverSpawns`; `TestNoAttemptWithoutGrant`.
**Put:** `S3.Loop → S4.sealed-ToolSpec → S6.0.Decide → S6.9.Before → {InProc | S6.2.Launch(S1.2)} → S6.9.After/OnError → S7`.

## Vlasništvo-fix (codex C10/C11, kilo R2-08/R2-11)
- **policy≠lifecycle:** pozivna mjesta 3.1/4.8 mijenjaju `authorize(6.9)` → `S6.0.Decide()` (policy) pa
  `S6.9` samo ORDER. 6.9 NIKAD ne odlučuje ALLOW/DENY.
- **AccountFleet (2.2):** samo `Classify(cred)→(candidate, cooldownHint)`; NE failover-a sam. **JEDINI**
  `AttemptGrant`+backoff = S7. Nadjačava `GAPFIX-agy` "automatski failover".
- **9.6 ObligationStore:** write IDE kroz `EventJournal.Append` (P0.3), NE izravno `sqlite.Store`.
  Nadjačava `GAPFIX-kilo` `store *sqlite.Store`. Done-state = journal-projekcija.
- **swallow-signal (15.5 vs 6.10):** JEDAN owner = **15.5 SemanticHealth** proizvodi signal; 6.10
  exec-auto-reviewer samo GA HRANI (input), ne drugi producent. S7 breaker konzumira 15.5.

## K3 — Fleet↔Pairing cirkularni import (kilo R2-10, V-K3/R3-04 leaf-fix)
Nadjačava `GAPFIX-codex` cross-import (`ExecutionNode{Device *pairing.DeviceID}` ↔ `RemoteInvoke{Node fleet.NodeID}`).
```
internal/fleet/ids        # SAMO skalari: type NodeID string; type DeviceID string  — TRUE leaf, 0 internih importa
internal/fleet/placement  # PlacementGrant{Node ids.NodeID; Task queue.TaskID; Attempt s7.AttemptGrant; Exp time.Time}
                          #   uvozi {ids, queue, s7} — SVE NIZVODNO. NIJE bazni `internal/contracts` (fix N5-01).
internal/fleet            # ExecutionNode{ID ids.NodeID; Device ids.DeviceID} → ids (+placement po potrebi)
internal/fleet/pairing    # RemoteInvoke(node ids.NodeID, g placement.PlacementGrant) → {ids, placement}
```
**Fix N5-01/V-K3 (ne u bazni `contracts`!):** bazni `internal/contracts` je K0 temelj koji `queue`/`s7`
UVOZE → stavljanje PlacementGrant tamo napravi `contracts→queue` ciklus. Zato PlacementGrant ide u
`internal/fleet/placement` (nizvodni paket koji uvozi queue/s7/ids; njih nitko od njih ne uvozi natrag).
`fleet` i `pairing` oba uvoze `placement`(+`ids`); nijedan ne uvozi drugoga; `contracts` ostaje temelj bez
uzvodnih rubova. **RED:** `go build ./...`; `TestNoImportCycle` (`contracts`⊄`queue`/`fleet`; `pairing`⊄`fleet`; `fleet`⊄`pairing`).

## 4.8 password-DENY neizvodiv (kilo R2-15)
Zamjena RED: OS input-injection NE zna sadržaj polja. `InputInject` u polje s `input_purpose=password`
(ako platforma daje a11y hint) → DENY; INAČE cijeli `TYPE` u nepoznato polje = **RED-tier approval po
znaku-tipu (6.9+P1.4), NE auto-detekcija passworda.** RED: `TestKeystrokeIntoUnknownFieldRequiresApproval`
(ne "detektira password").
