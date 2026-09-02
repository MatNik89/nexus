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

func (p EffectPath) RunTool(ctx context.Context, call contracts.ToolCall, grant s7.AttemptGrant) (contracts.ToolResult, error) {
    switch p.pep.Decide(call) {                                            // 1) S6.0 TOTALNI switch (fix N3-C01)
    case s6.DENY:  return denied(call), nil
    case s6.ASK:                                                           //    ASK ≠ ALLOW: traži exact-intent
        if !p.pep.ApprovedExact(call) { return needsApproval(call), nil }  //    odobrenje (P1.4) ili staje
    case s6.ALLOW: // nastavi
    }
    if err := p.mw.BeforeTool(ctx, call); err != nil { return p.mw.OnError(ctx, err) } // 2) S6.9 order
    // 3) grananje ISKLJUČIVO po tome treba li OS proces (fix RUN-01/REG-01 — Effect NIJE kriterij):
    exec := p.inproc
    if call.ExecutionKind == contracts.ExecProcess {                       //    authoritative S4 ToolSpec (fix V-K1)
        exec = p.sbproc                                                    //    → S6.2.Launch(S1.2 unutra)
    }
    out, err := exec.Execute(ctx, call)                                    // 4) izvršenje
    if err != nil { return p.retry.Record(grant, contracts.ToolResult{}, p.mw.OnError(ctx, err)) } // fix N3-C02
    if verr := p.mw.AfterTool(ctx, call, out); verr != nil {               //    post-exec veto FAIL-CLOSED (N3-C02)
        return p.retry.Record(grant, contracts.ToolResult{}, verr)         //    veto → nema isporuke outputa
    }
    return p.retry.Record(grant, out, nil)                                 // 5) S7 (JEDINI retry)
}
```
**Invarijante:** (1) grananje po `ToolSpec.ExecutionKind==ExecProcess` (pečat u S4, ne `call.Effect`) —
read-only shell (`cat`,`git log`) je i dalje proces → sandbox. `proc.Spawn` nedostupan mimo Launcha.
(2) In-process alat NIKAD ne spawna. (3) ASK≠ALLOW (traži exact-intent approval). (4) `Execute` err ILI
`AfterTool` veto → OnError + NULA isporuke outputa; AfterTool se NE zove na err-putu. (5) grant PRIJE Execute.
**RED:** `TestReadOnlyProcessStillSandboxed` (EffectReadOnly+ExecProcess → sbproc, ne inproc);
`TestAskRequiresExactApproval`; `TestExecErrorSkipsAfterToolAndOutput`; `TestInProcessToolNeverSpawns`;
`TestNoAttemptWithoutGrant`. + S4: `ToolSpec.ExecutionKind ∈ {ExecInProcess, ExecProcess}` je autoritativan,
4.2 NE prisvaja tree-kill (predaje exec-spec; kill je unutar S6.2.Launch/S1.2).
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
internal/fleet/ids   # SAMO skalarni ID-ovi: NodeID, DeviceID  — TRUE leaf, nula importa (fix R3-04)
internal/fleet       # ExecutionNode → ids ; PlacementGrant OVDJE (nosi queue.TaskID + s7.AttemptGrant + time)
internal/fleet/pairing # RemoteInvoke → ids  (uzima fleet.NodeID kao ids.NodeID, ne uvozi `fleet`)
```
`PlacementGrant` NIJE u `ids` (povlačio bi queue/reliability → ids ne bi bio leaf). Ostaje u `fleet`;
`pairing` referencira samo `ids` skalare, prima grant kao argument (ne import-tip). **RED:** `go build ./...`
prolazi; `TestNoImportCycle` (import-graf: `ids` uvozi 0 internih; `pairing` ne uvozi `fleet`; `fleet` ne uvozi `pairing`).

## 4.8 password-DENY neizvodiv (kilo R2-15)
Zamjena RED: OS input-injection NE zna sadržaj polja. `InputInject` u polje s `input_purpose=password`
(ako platforma daje a11y hint) → DENY; INAČE cijeli `TYPE` u nepoznato polje = **RED-tier approval po
znaku-tipu (6.9+P1.4), NE auto-detekcija passworda.** RED: `TestKeystrokeIntoUnknownFieldRequiresApproval`
(ne "detektira password").
