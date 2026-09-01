PLAN S5

# PLAN-S5 — S5 Workspace, VCS, checkpoint/rollback (coding sigurnosna mreža)

Jezgra `CGO_ENABLED=0`. S5 je MEHANIZAM (mi pišemo) + ADAPTER (git/container backend). **Coding-
relevantno:** 5.1 = undo po koraku (slab model ne smije trajno pokvariti repo), 5.2 = izolacija po
zadatku/subagentu (veže P1.2 + S12), 5.3 = atomic + first-writer-wins + provenance. Git: `go-git`
(pure-Go, bez cgo) ili `exec git` za fallback. Gate: **P1.2** (orphan/lock sweep).

---

## 5.1 Checkpoint i rollback po koraku (undo)

**Tip:** MEHANIZAM (jezgra)

**Aspekti → najbolji izvor:**
- checkpoint/restore diff-a po koraku → **Cline**
- auto-commit po turnu + `/undo` → **Aider**
- checkpoint mehanizam primijenjen na stanje → **LangGraph**

**Sinteza (shadow-git snapshot PRIJE svakog koraka; rollback = restore):**
```go
type ShadowGit struct{ dir string } // zaseban git repo, NE radi-radno stablo (ne zagađuje povijest)

type Checkpoint struct {
    repo    *git.Repository   // go-git (pure-Go) ili exec git
    shadow  *ShadowGit
    current CheckpointID
}
func (c *Checkpoint) Snapshot(stepID string) (CheckpointID, error)
// PRIJE svakog side-effecting koraka: snapshot cijelog radnog stabla (tree hash + blobs)
func (c *Checkpoint) Rollback(id CheckpointID) error  // restore do snapshot-a (undo po koraku)
func (c *Checkpoint) Diff(id CheckpointID) (Diff, error) // što je korak promijenio (→ 3.5/16.6)
```
Snapshot je jeftin (git content-addressed — samo promijenjeni blobovi); rollback vraća
byte-identično stanje. Checkpoint ID = journal offset (S0/P0.3), ne zaseban store.

**Salvage:** NEXUSv2 `core/checkpoint.py` (shadow-git :41) + `core/memgit.py` (snap :48) →
**Python→Go port** (shadow-git obrazac je NEXUS-ov — direktna logika).

**Ugovor/RED:** 5.1 spec (undo po koraku). RED (naš): nakon edita, `Rollback` vraća byte-identično
stablo pre-koraka (diff prazan); checkpoint NIKAD ne dira korisničke ne-staged promjene.

**Verifikacija:** Cline (Apache-2.0), Aider (Apache-2.0), LangGraph (MIT) — stvarni.

**Pareto floor:** checkpoint/restore (Cline) + auto-commit/undo (Aider) + checkpoint-on-state
(LangGraph) + shadow-git izvan radnog stabla (naš — Aider/Cline zagađuju git povijest, mi ne) → ≥ svaki.

---

## 5.2 Izolacija radnog prostora (worktree/container po zadatku ili subagentu)

**Tip:** MEHANIZAM + ADAPTER (git worktree = jezgra; container = S6 backend)

**Aspekti → najbolji izvor:**
- workspace izolacija runtimeom → **OpenHands**
- izolirani task/workspace interface → **SWE-agent**
- workspace manifesti + sandbox sesije + snapshot/resume → **OpenAI Agents SDK Sandbox Agents** (beta)

**Sinteza (git worktree per-task/per-subagent, detachirano; container backend opcionalan):**
```go
type Workspace struct {
    root      string
    worktrees map[string]*Worktree // taskID/subagentID → worktree
}
func (w *Workspace) NewIsolated(owner string) (*Worktree, error)
// git worktree add --detach <root>/<owner> — izolirano stablo, isti .git
func (w *Workspace) SyncBack(wt *Worktree) error  // merge natrag: first-writer-wins (5.3)
func (w *Workspace) Cleanup(owner string) error   // worktree remove + lock release
// container backend (S6): isti interface, backend = docker/bwrap/sandbox-ABI (ne mijenja Workspace API)
```
Svaki subagent (S12) dobiva VLASTITI worktree → nema dijeljenog radnog stabla, nema konflikta
među agentima; sync-back je eksplicitan, ne automatski merge.

**Salvage:** NEXUSv2 `core/worktree.py` (detached worktree :89, sync_back :168) → **Python→Go port**.

**Ugovor/RED:** **P1.2** (`ResourceLease` za `WORKSPACE_LOCK`) + `test_sigkill_restart_reaps_owned_
tree_only` — SIGKILL worker koji ostavi lock → sweeper reapa lock, novi vlasnik ga može uzeti.
RED (naš): symlink/putanja izvan workspace root-a → odbij (traversal); djelomični cleanup → `CLEANING`,
nikad `RELEASED` (P1.2).

**Verifikacija:** OpenHands (MIT), SWE-agent (MIT), OpenAI Agents SDK Sandbox Agents (MIT, beta) —
stvarni (Sandbox Agents = beta, matrica testira traversal/cleanup-failure).

**Pareto floor:** runtime izolacija (OpenHands) + task/workspace interface (SWE-agent) + manifest
(Sandbox Agents) + **git worktree per-subagent + eksplicitan sync-back** (naš — nitko nema worktree-
per-agent kao prvu liniju) → ≥ svaki.

---

## 5.3 Dirty-tree ownership, atomic write, conflict detection, provenance

**Tip:** MEHANIZAM (jezgra)

**Aspekti → najbolji izvor:**
- git-aware edit/commit (tuđi dirty rad se NE gazi) → **Aider**
- atomski patch-apply s odbijanjem konflikta → **Codex CLI**
- razlikovanje generiranog vs. korisničkog sadržaja → **Cline**

**Sinteza (atomic write + first-writer-wins + provenance/taint + sandbox-neutralni atestor):**
```go
type AtomicWriter struct{}
func (a *AtomicWriter) Write(path string, content []byte) error
// tmp → fsync → rename (atomarno; nikad in-place write → pola fajla nemoguće)

type TaintTier int // GENERATED | USER | UNKNOWN
type Provenance struct{ marks map[string]TaintTier }
func (p *Provenance) Mark(path string, t TaintTier)
func (p *Provenance) Resolve(path string) TaintTier
// first-writer-wins: ako je path USER-tainted, GENERATED write → CONFLICT (ne gazi tuđi rad)

// sandbox-neutralni atestor (obsidian zahtjev): provenance iz NIŽEG trust sloja
type Attestation struct{ Actor, Args, FSEffect, NetEffect []byte } // sandbox je VIDIO stvarni učinak
// export u W3C PROV-O (veže 6.2 sandbox + 15.3 audit); runtime NE atestira sam sebe
```
Provenance je u `Provenance` mapi (per-workspace), a atestacija dolazi iz sandboxa (6.2) koji je
vidio stvarni argv/fs/network — runtime ne smije sam sebe atestirati (trust-boundary, 6.0).

**Salvage:** NEXUSv2 `core/provenance.py` (datamark :40) + `gortex/diff_engine.go` (atomic write) →
**Python→Go port** (provenance) + **Go REUSE** (diff_engine atomic).

**Ugovor/RED:** 5.3 spec (first-writer-wins, tuđi dirty rad se ne gazi). RED (naš): konkurentni edit
na isti fajl (GENERATED vs USER) → `CONFLICT`, korisnički sadržaj NIJE prebrisan; atomic write usred
pada → fajl je ili stari ili novi, nikad pola (tmp+rename provjera).

**Verifikacija:** Aider (Apache-2.0), Codex CLI (Apache-2.0), Cline (Apache-2.0) — stvarni; PROV-O
(W3C standard) — stvaran.

**Pareto floor:** git-aware (Aider) + atomic patch/conflict (Codex) + generated-vs-user (Cline) +
sandbox-neutralni atestor PROV-O (naš — nitko ne atestira iz nižeg trust sloja) → ≥ svaki.

---

## S5 cross-cutting napomene

1. **Shadow-git je ključni coding-diferencijator:** snapshot NE zagađuje korisničku git povijest
   (Aider auto-commit zagađuje; Cline checkpoint je u vlastitoj grani) — naš shadow-git je potpuno
   izvan radnog stabla, undo je byte-identičan.
2. **Worktree-per-subagent je S12-temelj:** kad S12 dođe, `Workspace.NewIsolated(subagentID)` daje
   svakom subagentu vlastito stablo — nema dijeljenog radnog stabla, sync-back je eksplicitan
   (first-writer-wins iz 5.3).
3. **P1.2 ovisi o S5:** `ResourceLease` za `WORKSPACE_LOCK` + `TEMP_DIR` + `PROCESS_TREE` dolazi iz
   S1.2 (start-token) + S5 (worktree lock) — sweeper reapa ono što S5 ostavi nakon SIGKILL.
4. **Honest gap:** 5.1/5.3 nemaju dedicirani Annex RED (gate = spec zahtjev + P1.2 posredno); 5.2
   je jedini s P1.2 RED. Ako moderator želi simetriju: P0.11 "rollback-byte-identical" + "first-
   writer-wins".
5. **Verifikacija:** svi kandidati stvarni (licence/aktivnost gore); Sandbox Agents = beta (matrica
   testira traversal/cleanup-failure); PROV-O = W3C standard; salvage `checkpoint/worktree/memgit/
   provenance` interni (postoje — audit potvrdio), `diff_engine.go` = Go reuse.
