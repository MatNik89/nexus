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
    if p.pep.Decide(call) == s6.DENY { return denied(call), nil }         // 1) S6.0 (JEDINI policy)
    if err := p.mw.BeforeTool(ctx, call); err != nil { return p.mw.OnError(ctx, err) } // 2) S6.9 order
    exec := p.inproc
    if call.Effect != contracts.EffectReadOnly && call.RequiresProcess {   // 3) grananje (K2)
        exec = p.sbproc                                                     //    → S6.2.Launch(S1.2 unutra)
    }
    out, err := exec.Execute(ctx, call)                                    // 4) izvršenje (sandbox ako subprocess)
    p.mw.AfterTool(ctx, call, out)
    return p.retry.Record(grant, out, err)                                 // 5) S7 (JEDINI retry)
}
```
**Invarijante:** (1) svaki subprocess-alat ide kroz `sandbox.Backend.Launch`; `proc.Spawn` NIJE dostupan
mimo Launcha. (2) In-process alat NIKAD ne spawna proces. (3) grant se izdaje PRIJE Execute (S7), ne poslije.
**RED:** `TestExecPathSandboxesEverySubprocess` (exec-alat bez sandbox.Launch → panic/deny; nula direktnih
proc.Spawn); `TestInProcessToolNeverSpawns`; `TestNoAttemptWithoutGrant`.
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

## K3 — Fleet↔Pairing cirkularni import (kilo R2-10)
Nadjačava `GAPFIX-codex` cross-import. Zajednički paket ID-ova:
```
internal/fleet/ids   # NodeID, DeviceID, PlacementGrant — BEZ ovisnosti (list rung)
internal/fleet       # ExecutionNode → ids   (ne → pairing)
internal/fleet/pairing # RemoteInvoke → ids   (ne → fleet)
```
`fleet` i `pairing` ovise SAMO o `fleet/ids`; nema parent↔child ciklusa. **RED:** `go build ./...` prolazi;
`TestNoImportCycle` (import-graf `fleet/pairing` ne uvozi `fleet`).

## 4.8 password-DENY neizvodiv (kilo R2-15)
Zamjena RED: OS input-injection NE zna sadržaj polja. `InputInject` u polje s `input_purpose=password`
(ako platforma daje a11y hint) → DENY; INAČE cijeli `TYPE` u nepoznato polje = **RED-tier approval po
znaku-tipu (6.9+P1.4), NE auto-detekcija passworda.** RED: `TestKeystrokeIntoUnknownFieldRequiresApproval`
(ne "detektira password").
