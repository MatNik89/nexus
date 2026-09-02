# DESIGN-FIXES-r2 — zatvara REVIEW2 kritične blokere (K1-K4 + vlasništvo)

Zatvara nalaze iz REVIEW2-CONSOLIDATED. Buildable Go, invarijante, RED. Nadjačava suprotne dijelove
ranijih DESIGN-*.md (navedeno po fixu).

---

## K1+K2 — EffectPath: sandbox U putu + InProcess/Sandboxed grananje
Nadjačava `DESIGN-memory-effectpath-kilo.md` §B (RunTool bez S6.2 + proc.Spawn za sve).

```go
// Dva izvršitelja — ne svaki alat je proces.
type ToolExecutor interface { Execute(ctx, contracts.ToolCall) (contracts.ToolResult, error) }

type InProcessExecutor struct{ /* edit/read/grep/archmap/memory — čista Go funkcija, bez procesa */ }
type SandboxedProcessExecutor struct {                 // bash/exec/python — kroz membranu
    sandbox sandbox.Backend                            // S6.2 — OBAVEZAN
    proc    *s1.ProcessTracker                         // S1.2 samo prati stablo UNUTAR Launch
}

type EffectPath struct {
    pep     *s6.PEP            // S6.0 policy (JEDINI decision)
    mw      *s6.Middleware     // S6.9 lifecycle ORDER (ne policy)
    sandbox sandbox.Backend    // S6.2 — bio izostavljen (K1)
    inproc  ToolExecutor
    sbproc  ToolExecutor
    retry   *s7.RetryOwner     // S7 (JEDINI grant)
}

// ToolCall nosi resolved ExecutionKind iz S4 ToolSpec (dopuna DESIGN-S0 ToolCall; fix V-K1/N4-C02):
//   ToolSpec{ID, SchemaHash, EffectClass, ExecutionKind}   ExecutionKind ∈ {ExecInProcess, ExecProcess}
//   registry PEČATI ExecutionKind; call.ExecutionKind = resolved iz zapečaćenog spec-a (ne od modela).

func (p EffectPath) RunTool(ctx context.Context, call contracts.ToolCall, grant s7.AttemptGrant) (contracts.ToolResult, error) {
    switch p.pep.Decide(call) {                                            // 1) S6.0 TOTALNI switch + default-deny
    case s6.ALLOW: // nastavi
    case s6.ASK:
        if !p.pep.ApprovedExact(call) { return needsApproval(call), nil }  //    exact-intent (P1.4) ili staje
    case s6.DENY:  return denied(call), nil
    default:       return denied(call), ErrInvalidDecision                 //    nepoznat/zero → DENY (fix N4-C01)
    }
    if err := p.mw.BeforeTool(ctx, call); err != nil { return p.mw.OnError(ctx, err) } // 2) S6.9 order
    var exec ToolExecutor                                                  // 3) TOTALNI switch po pečatu (N4-C02)
    switch call.ExecutionKind {
    case contracts.ExecInProcess: exec = p.inproc
    case contracts.ExecProcess:   exec = p.sbproc                          //    → S6.2.Launch(S1.2 unutra)
    default: return denied(call), p.mw.OnError(ctx, ErrUnknownExecKind)    //    zero/nepoznat → reject (ne inproc)
    }
    out, err := exec.Execute(ctx, call)                                    // 4) izvršenje
    if err != nil {                                                        //    Execute err → OnError (audit/lifecycle)
        return p.retry.Record(grant, contracts.ToolResult{}, p.mw.OnError(ctx, err))
    }
    if verr := p.mw.AfterTool(ctx, call, out); verr != nil {               //    post-exec veto (N3-C02/N4-C03):
        oe := p.mw.OnError(ctx, verr)                                      //    kroz OnError (lifecycle/audit grana)
        phase := classifyEffectPhase(call, out)                           //    P1.4/effect-taxonomy:
        return p.retry.RecordVeto(grant, phase, oe)                        //    IRREVERSIBLE+committed → UNKNOWN→RECONCILING
    }
    return p.retry.Record(grant, out, nil)                                 // 5) S7 (JEDINI retry)
}
```
**Invarijante:** (1) grananje po zapečaćenom `ToolSpec.ExecutionKind` (S4 registry, ne `call.Effect`) —
read-only shell (`cat`,`git log`) je proces → sandbox; zero/nepoznat kind → **reject, NE inproc**.
(2) `PEP.Decide` switch ima default-deny (nepoznata odluka NE izvršava). (3) In-process alat NIKAD ne
spawna; `proc.Spawn` nedostupan mimo S6.2.Launch. (4) ASK≠ALLOW (exact-intent). (5) `Execute` err ILI
`AfterTool` veto → **OnError** (audit/lifecycle grana), NULA isporuke; veto nakon commit-a → `RecordVeto`
klasificira `IRREVERSIBLE+UNKNOWN→RECONCILING` (P1.4, ne slijepi retry). (6) grant PRIJE Execute.
**RED:** `TestUnknownDecisionDenies`; `TestUnknownExecKindRejectsNotInproc`; `TestReadOnlyProcessStillSandboxed`;
`TestAskRequiresExactApproval`; `TestVetoGoesThroughOnErrorAndReconciles`; `TestExecErrorSkipsAfterToolAndOutput`;
`TestInProcessToolNeverSpawns`; `TestNoAttemptWithoutGrant`.
**Ispravan put:** `S3.Loop → S4.tool-spec → S6.0.Decide → S6.9.Before → {InProc | S6.2.Launch(S1.2)} → S6.9.After → S7`.

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
internal/fleet/ids      # SAMO skalari: type NodeID string; type DeviceID string  — TRUE leaf, 0 importa
internal/contracts      # PlacementGrant{Node ids.NodeID; Task queue.TaskID; Attempt s7.AttemptGrant; Exp time.Time}
                        #   (neutralni kernel-contracts paket; već postoji, uvozi ids/queue/s7 — nizvodno od njih)
internal/fleet          # ExecutionNode{ID ids.NodeID; Device ids.DeviceID} → ids
internal/fleet/pairing  # RemoteInvoke(node ids.NodeID, g contracts.PlacementGrant)  → ids + contracts
```
Konkretni potpis: `pairing.RemoteInvoke` prima `contracts.PlacementGrant` (neutralni tip), NE `fleet.*`.
`fleet` i `pairing` oba uvoze `ids`(+`contracts`); nijedan ne uvozi drugoga. Nema ciklusa. **RED:**
`go build ./...` prolazi; `TestNoImportCycle` (`ids` uvozi 0 internih; `pairing`⊄`fleet`; `fleet`⊄`pairing`).

## 4.8 password-DENY neizvodiv (kilo R2-15)
Zamjena RED: OS input-injection NE zna sadržaj polja. `InputInject` u polje s `input_purpose=password`
(ako platforma daje a11y hint) → DENY; INAČE cijeli `TYPE` u nepoznato polje = **RED-tier approval po
znaku-tipu (6.9+P1.4), NE auto-detekcija passworda.** RED: `TestKeystrokeIntoUnknownFieldRequiresApproval`
(ne "detektira password").
