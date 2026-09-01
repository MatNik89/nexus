PLAN S9

# PLAN-S9 — S9 Memorija preko sesija (capability flag `memorija`; save/resume = JEZGRA)

Jezgra `CGO_ENABLED=0`. **9.1 save/resume je JEZGRA** (svaki stateful harness); 9.2/9.3/9.4 su
`memorija` flag. **Memory spine = SQLite-WAL preko `modernc.org/sqlite`** (pure-Go, bez cgo). Gate:
P2.1 (FORGET vs PURGE — S6.7 nadjačava S9), P0.3 (lineage kroz memoriju). **Trust boundary (9.2/9.3):**
write-approval DEFAULT ON (obrnuto od Hermesa — njihov `write_approval:false` default je rupa), +
untrusted-labeling memorije iz tool-outputa.

---

## 9.1 Sesije: save/resume/branch (save/resume = JEZGRA)

**Tip:** MEHANIZAM (save/resume jezgra; branch/Stash = profil)

**Aspekti → najbolji izvor:**
- session storage s nastavkom → **goose**
- event stream omogućuje rekonstrukciju sesije → **OpenHands**
- resume sesija iz zapisa → **Codex CLI**
- shared team-memory (transcript+tool-callovi → virtualni FS) → **Stash** (runner-up, multi-agent)

**Sinteza (save = journal offset; resume = fold; branch = fork na offsetu):**
```go
type Session struct{ id string; journal *journal.Journal }
func (s *Session) Save() (CheckpointID, error)     // = journal offset (7.3), NE mutable snapshot
func (s *Session) Resume(id CheckpointID) (*machine.State, error) // idempotent fold (P0.3)
func (s *Session) Branch(from CheckpointID) (*Session, error)     // fork na offsetu — `multi-agent`/profil

// Stash (multi-agent handoff, profil): transcript+tool-callovi u agent-native virtualni FS,
// semantička pretraga preko sesija — dijeli se SAMO preko eksplicitnog storea, ne context windowa
```
Save/resume naslanja se na 7.3 (fold) — NEMA zasebnog mutable state storea; session = pozicija u journalu.

**Salvage:** NEXUSv2 `core/session.py` (:172) + `core/resume.py` (idempotent resume :29) →
**Python→Go port**.

**Ugovor/RED:** P0.3 (fold idempotentan). RED (naš): resume dva puta s istog offseta → identično stanje
(idempotent); resume nakon crasha → stanje = fold eventa (ne izgubljeno).

**Verifikacija:** goose (Apache-2.0), OpenHands (MIT), Codex CLI (Apache-2.0) — stvarni.

**Pareto floor:** session storage (goose) + event-stream rekonstrukcija (OpenHands) + resume-iz-zapisa
(Codex) + **journal-offset kao jedini checkpoint** (naš — nitko ne veže sesiju na P0.3 fold) → ≥ svaki.

---

## 9.2 Perzistentna memorija (činjenice o korisniku/projektu)

**Tip:** MEHANIZAM (`memorija` flag)

**Aspekti → najbolji izvor:**
- agent sam uređuje svoju memoriju → **letta (MemGPT)**
- memory layer s ekstrakcijom činjenica + graf → **mem0**
- temporal knowledge graph → **zep**

**Sinteza (SQLite-WAL spine + 5 scopeova + vektor + KG + write-gate + untrusted-labeling):**
```go
type MemorySpine struct {
    store *sqlite.Store       // modernc.org/sqlite WAL — jedini durable spine
    vec   *VectorIndex        // 9.2/10.3 dijele embed index
    kg    *KG                 // činjenice + odnosi
}
type Scope int // PROJECT | USER | TASK | LESSON | EPHEMERAL (NEXUS 5-scope)
type MemoryMeta struct{ Source string; Confidence float64; ExpiresAt *time.Time; Untrusted bool }

// write-gate (trust boundary): agent-authored write MORA proći approval — DEFAULT ON
// (Hermes ima write_approval:false default = RUPA; mi obrćemo na ON — spec 9.3)
// untrusted-labeling: memorija ekstrahirana iz tool-outputa je UNTRUSTED (P0.3 lineage)
// forget (reverzibilno, tombstone) vs purge (ireverzibilno) — P2.1, S6.7 nadjačava S9
```
OpenHuman Memory Tree + TokenJuice (runner-up): scored markdown stablo + SuperContext preload +
kompresija — eksplicitan napad na cold-start (9.2 zahtjev).

**Salvage:** NEXUSv2 `core/memory.py` (5-scope spine :21, query :127, _forget :140) +
`core/memvec.py` (:40, RRF :98) + `core/kg.py` (:81, facts :134) + `core/entres.py` (:39) →
**Python→Go port**.

**Ugovor/RED:** **P2.1** (FORGET vs PURGE) + write-policy (9.2 spec). RED (naš): agent-authored write
bez approval (approval OFF) → odbijen; FORGET entry → bajt ostaje (mem-restore); PURGE → nedohvatljiv
+ immutable audit (P2.1 `test_purge_cannot_complete_with_residual_copy`).

**Verifikacija:** letta (Apache-2.0), mem0 (Apache-2.0), zep (Apache-2.0) — stvarni.

**Pareto floor:** self-editing (letta) + fact-ekstrakcija+graf (mem0) + temporal KG (zep) +
**write-approval ON + untrusted-labeling + forget/purge** (naš — nitko ne defaulta approval na ON) → ≥ svaki.

---

## 9.3 Učenje iz iskustva (greške se ne ponavljaju)

**Tip:** MEHANIZAM (`memorija` flag)

**Aspekti → najbolji izvor:**
- agent-authored skills iz iskustva → **Hermes Agent** (harness, verificirano)
- ECL destilacija znanja → **cognee** (preklapa s 10.4)
- verbalna samorefleksija → **Reflexion** (research)

**Sinteza (Reflexion + ReasonBank + evidence-gated self-improve; approval DEFAULT ON):**
```go
type Reflexion struct{} // verbalna samorefleksija NAKON greške (ne u petlji uživo)
type ReasonBank struct{} // trajektorija → reusable lekcija (trajectory_of)

// self-improve (NEXUS selfimprove.py): propose → eval-gate → promote — SAMO iz verificiranog ishoda
// write-approval DEFAULT ON (9.3 trust boundary): skill-write NE bezuvjetna autonomija
// polje nema standardiziran dokaz da samodogradnja smanjuje greške → mjerimo, ne pretpostavljamo
```
Razlika procedura-vs-memorija (9.3/11.2): proceduralno znanje → SKILL.md (11.2); činjenice → memory
(9.2); lekcije iz neuspjeha → ReasonBank (9.3). Tri različita sloja, ne jedan.

**Salvage:** NEXUSv2 `core/reflexion.py` (:27) + `core/reasonbank.py` (:33, trajectory_of :61) +
`core/selfimprove.py` (:328) → **Python→Go port**.

**Ugovor/RED:** 9.3 spec (write-approval ON, nikad bezuvjetna autonomija). RED (naš): skill-write bez
approvala → odbijen (fail-closed); self-improve bez verificiranog ishoda → ne promovira.

**Verifikacija:** Hermes Agent (MIT, verificirano), cognee (Apache-2.0), Reflexion (research) — stvarni
(Reflexion/Voyager = research referentni, ne produkcijski).

**Pareto floor:** agent-authored skills (Hermes) + ECL destilacija (cognee) + verbalna refleksija
(Reflexion) + **approval-default-ON + evidence-gated promote** (naš — obrnuto od Hermes rupe) → ≥ svaki.

---

## 9.4 Memorijska konsolidacija i decay (sleep-time)

**Tip:** MEHANIZAM (`memorija` flag)

**Aspekti → najbolji izvor:**
- sleep-time engine → **letta (MemGPT)**
- konsolidacija + temporal graf → **mem0 / zep**
- puni NEXUS sleep-time set → **salvage** (dream/sleeptime/audn/memgit/recall/memassoc)

**Sinteza (offline konsolidacija epizoda→gist+KG; decay mijenja RANG ne postojanje):**
```go
type Consolidator struct{} // sleep-time, offline (ne u petlji uživo)
func (c *Consolidator) Dream(episodes []Episode) ([]Gist, []KGFact, error) // epizode → gistovi + KG

type Decay struct{} // FSRS-lite: mijenja RANG (recall score), NE postojanje
func (d *Decay) Apply(e MemoryEntry) MemoryEntry
// LOSSLESS: hot/warm/cold — original UVIJEK u dohvatljivoj arhivi; decay samo snižava score

type Audn struct{} // kontradikcije na READ: ADD | NOOP | SUPERSEDE (ne silent overwrite)
func (a *Audn) Resolve(existing, incoming MemoryEntry) Resolution // "Python 3.8" (2024) → SUPERSEDE

type MemGit struct{}   // verzionirani write (git-verzija svakog write-a — undo memorije)
type MemAssoc struct{} // spreading-activation (asocijativni recall relevantnog)
```
Temporalni upiti (9.4): "što sam znao TADA" — snapshot po vremenu, ne samo trenutno stanje.

**Salvage:** NEXUSv2 `core/dream.py` (:16) + `core/sleeptime.py` + `core/audn.py` (ADD/NOOP/SUPERSEDE)
+ `core/memgit.py` (:48) + `core/recall.py` (FSRS decay) + `core/memassoc.py` →
**Python→Go port** (najbogatiji memorijski salvage — NEXUS je ovdje dubok).

**Ugovor/RED:** P2.1 (forget tombstone) + P0.3 (lineage kroz konsolidaciju). RED (naš): decay NE briše
bajtove (lossless — original u arhivi, samo score pada); konsolidacija untrusted epizode → gist ostaje
UNTRUSTED (P0.3, ne pere se).

**Verifikacija:** letta (Apache-2.0), mem0 (Apache-2.0), zep (Apache-2.0) — stvarni.

**Pareto floor:** sleep-time engine (letta) + temporal graf (mem0/zep) + **dream+audn+memgit+FSRS+
memassoc set** (naš — nitko nema ADD/NOOP/SUPERSEDE ni git-verziju memorije) → ≥ svaki.

---

## S9 cross-cutting napomene

1. **9.1 save/resume = JEZGRA** (građena u P3, naslanja se na 7.3 fold) — session = journal offset,
   NE mutable snapshot; branch/Stash su `memorija`/`multi-agent` profil.
2. **Memory spine = SQLite-WAL (`modernc.org/sqlite`)** — jedini durable store, `CGO_ENABLED=0`;
   vektor/KG su projekcije iznad spine-a, ne zasebni storeovi.
3. **Write-approval DEFAULT ON (trust boundary):** Hermes ima `write_approval:false` default (rupa) —
   mi obrćemo na ON; agent-authored write/self-improve NIKAD bezuvjetna autonomija.
4. **P2.1 vlasništvo:** S9 posjeduje `MEMORY_FORGET` (reverzibilno), S6.7 posjeduje autoritativni
   `DATA_PURGE` (ireverzibilno) i UVIJEK nadjačava S9 — purge briše i forget tombstone content.
5. **Honest gap:** 9.1/9.3/9.4 nemaju dedicirani Annex RED (gate = P0.3 + P2.1 posredno); 9.2 ima
   P2.1 izravno. Ako moderator želi simetriju: P0.13 "decay-never-destroys-bytes".
6. **Verifikacija:** svi kandidati stvarni (goose/OpenHands/Codex/letta/mem0/zep MIT/Apache-2.0;
   Hermes MIT verificiran; Reflexion/Voyager research); salvage `session/resume/memory/memvec/kg/
   entres/dream/sleeptime/audn/memgit/recall/memassoc/reflexion/reasonbank/selfimprove` interni
   (postoje — audit potvrdio), port je logika.
