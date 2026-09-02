> **⚠ DJELOMIČNO SUPERSEDED (REVIEW2/3):** `ObligationStore{store *sqlite.Store}` (izravni write) je ZAMIJENJEN write-kroz-`EventJournal.Append` (P0.3) iz **`DESIGN-FIXES-r2.md`**. Fold-target G1→4.8/G2→9.5/G4→9.6/O6→6.10 je kanonski u HARNESS-PLAN (ne self-mapping ovdje).

GAPFIX kilo

# GAPFIX — ASISTENT klaster (buildable Go dizajn, ne bilješke)

Format: Tip · Sinteza(Go skice) · Salvage · Ugovor/RED · Sekcija. Svi gapovi iz 29-harness audita
(moj klaster: Hermes G1-G5 + OpenClaw O6-O7). Reference su OBRASCI (ne copy tuđeg koda).

---

## G1 — OS-wide computer-use (desktop kontrola + input injection s approvalom)

**Tip:** MEHANIZAM (gate jezgra) + ADAPTER (OS backend)

**Sinteza (Go):**
```go
type InputType int // KEY | MOUSE_CLICK | MOUSE_MOVE | TYPE_TEXT | SCREENSHOT | APP_LAUNCH
type InputAction struct { Type InputType; Target string; Payload []byte }

// input injection = IREVERZIBILAN side-effect → P1.4 (draft→commit→compensate) + 6.9 (before_tool)
type ComputerUse interface {
    Screenshot(ctx) (Image, error)              // REVERSIBLE (read)
    AppLaunch(ctx, app string, grant s7.AttemptGrant) error  // REVERSIBLE-ish
    InputInject(ctx, a InputAction, intent p14.EffectIntent) error // IRREVERSIBLE → approval-gated
}

type ComputerUseGate struct{ pep *s6.PEP; hooks *s6.MiddlewareChain }
func (g *ComputerUseGate) Authorize(a InputAction) s6.Decision
// KEY/MOUSE/TYPE = RED tier (6.1); SCREENSHOT = YELLOW (CONFIDENTIAL retention 6.7)
// compensate: snapshot desktop stanja PRIJE injekcije (rollback je slab → P1.4 compensator = "undo key" / "app restore")
```
**Salvage:** Hermes `tools/computer_use/` (10 modula — obrazac); NEXUS `core/browse.py` (CDP egress-gated
— isti "opasni I/O iza gatea" obrazac).

**Ugovor/RED:** `InputInject` bez `EffectIntent` approval → `6.9` ODBIJ (draft bez commit); keystroke u
`password`/`secret` polje → `6.1` DENY (RED regex); SCREENSHOT na osjetljivoj aplikaciji → `CONFIDENTIAL`
(P0.1 sensitivity) → 6.7 retention/delete (ne leži u indeksu).

**Sekcija:** S4.5 (browser/computer-use) + S6.9/S6.1 (lifecycle/permission) + P1.4 (compensate) + 13.1.

---

## G2 — Izolirani PersonalProfile (posao/privatno/obitelj)

**Tip:** MEHANIZAM (registry jezgra) + DATA (profile sadržaj)

**Sinteza (Go):**
```go
type ProfileID string // "work" | "private" | "family"
type PersonalProfile struct {
    ID ProfileID
    Memory      *s9.MemorySpine   // vlastiti scope (9.2)
    Keychain    *s6.Broker        // vlastiti keyring (6.4)
    Permissions []s6.Rule         // vlastita pravila (6.1)
    Triggers    []s3.Trigger      // vlastiti cron/watch (3.6)
    Identity    s11.Identity      // vlastiti glas (11.1)
}
type ProfileRegistry struct{ byID map[ProfileID]*PersonalProfile }
func (r *ProfileRegistry) Resolve(profile ProfileID) (*PersonalProfile, error) // deny-default

// HARD izolacija: memory (9.2 scope), cache (8.4 CacheKey.tenant), retrieval (10.6 ACL),
// keychain (6.4), secrets — NIKAD cross-profile; "isti čovjek, dva života" = dvije namespace.
```
**Salvage:** Hermes memory scopes (obrazac) + NEXUS 5-scope spine (salvage); OpenClaw per-user workspace
(qm obrazac: memory+files+keychain+permissions+cron kao JEDNA jedinica).

**Ugovor/RED:** memory-write u `work` → query u `family` vraća 0 hitova (10.6 deny-default); cache lookup
cross-profile → `CACHE_SCOPE_MISMATCH` (P2.4); keychain `work` ne čita `private` (6.4 scoped keyring).

**Sekcija:** S6.6 (tenant/principal) + S9.2 (scope) + S10.6 (ACL) + 14.5 (channel→profile binding).

---

## G3 — Voice razgovorni runtime (barge-in / wake / consent)

**Tip:** MEHANIZAM (runtime jezgra) + ADAPTER (STT/TTS/wake backend)

**Sinteza (Go):**
```go
type WakeWordDetector interface{ Detect(ctx, ch <-chan AudioFrame) (bool, error) } // lokalno, bez clouda
type VoiceRuntime struct {
    stt     *s13.Transcriber
    tts     *s13.Synthesizer
    wake    WakeWordDetector
    consent *s6.ConsentGate
    bargeIn *s7.CancelToken
}
func (v *VoiceRuntime) Run(ctx) (Conversation, error)
// barge-in: user govori PREKO TTS-a → s7.CancelToken prekida tts stream (S7 cancel, ne busy-wait)
func (v *VoiceRuntime) BargeIn() error { v.bargeIn.Cancel(); return v.stt.Prime() }
// consent: snimanje = CONFIDENTIAL (P0.1) → 6.7 retention + eksplicitan consent gate (6.1) PRIJE prvog frame-a
// wake-word = lokalni on-device detector (ne šalje audio cloudu dok se ne probudi — privacy boundary)
```
**Salvage:** Hermes `voice.py` (STT/TTS) + `agent/realtime_transcription.py` + `streaming_tts_consumer.py`
(obrazac); pipecat/LiveKit realtime vozilo (obrazac).

**Ugovor/RED:** barge-in usred TTS-a → TTS prekinut, STT prima (S7 cancel propagira); audio PRIJE consent →
gate ODBIJA (nula frame-a van); retention politika 6.7 primijenjena (audio ne traje u indeksu).

**Sekcija:** S13.3 (voice) + S7 (cancel/deadline) + S6.7 (retention) + 14.5 (voice channel).

---

## G4 — ObligationStore (trajni cilj s done-def)

**Tip:** MEHANIZAM (store jezgra) + DATA (obligacija/done-def)

**Sinteza (Go):**
```go
type ObligationState int // PENDING | ACTIVE | DONE | CANCELLED | SUPERSEDED
type Obligation struct {
    ID string; Goal string
    DoneDef    DoneCondition          // strojno provjerljiv "gotovo" (16.1 app-contract)
    Recurrence *s3.ScheduleManifest   // kad re-evaluirati (cron/heartbeat)
    State      ObligationState
    Owner      ProfileID              // G2 profil
}
type DoneCondition interface{ Evaluate(ctx, world WorldState) (done bool, ev Evidence) }
// Evidence = git-diff/exit-code/API-poziv/artefakt — NE proza (16.6 checker)
type ObligationStore struct{ store *sqlite.Store } // modernc.org/sqlite
func (o *ObligationStore) EvaluateDue(ctx) []Obligation      // trigger plane (3.6) pokreće
func (o *ObligationStore) MarkDone(obl Obligation, ev Evidence) error // done SAMO uz dokaz
// "Podsjeti me / pobrini se da X" = Obligation s done-def; aktivno prati, ne pasivni reminder
```
**Salvage:** OpenClaw `commitments/` (15 — obrazac); NEXUS `core/schedule.py` (heartbeat) + 16.1
app-contract (done-def kao artefakt).

**Ugovor/RED:** obligation "osiguraj da je X deployano" → Evaluate vraća NOT-DONE dok done-def (git-diff/
exit-code) nije zadovoljen; `MarkDone` bez evidence → ODBIJ (16.6 evidence-gated, ne samoprijava);
SUPERSEDED kad novija obligacija poništi staru (9.4 AUDN obrazac).

**Sekcija:** 16.1 (app-contract/done-def) + 3.6 (trigger) + 9.x (persistent state) + 16.6 (evidence).

---

## G5 — Asistentski control-plane nad automation

**Tip:** MEHANIZAM (control jezgra)

**Sinteza (Go):**
```go
type AutomationControlPlane struct{ triggers *s3.TriggerRegistry }
func (c *AutomationControlPlane) Inspect(ctx, q Query) []TriggerStatus // agent vidi stanje (active/paused/last-fire)
func (c *AutomationControlPlane) Pause(ctx, triggerID string, grant s7.AttemptGrant) error  // HITL 12.4
func (c *AutomationControlPlane) Resume(ctx, triggerID string) error
func (c *AutomationControlPlane) Delete(ctx, triggerID string, intent p14.EffectIntent) error // irreverzibilno → P1.4
// agent NE mijenja trigger direktno — sve kroz control-plane → 6.9 lifecycle + 12.4 HITL + audit 15.3
```
**Salvage:** Hermes `cron/` (162 — obrazac: scheduler/jobs/executions/lifecycle_guard); NEXUS
`core/schedule.py` + `core/watch.py` (salvage).

**Ugovor/RED:** `Pause` triggera → run NE fire (dedup + idempotency key); `Resume` → nastavlja NE
duplicira (missed-run politika iz P2.6); `Delete` bez P1.4 compensate → ODBIJ; svaki pause/resume/delete
→ audit 15.3 (hash-lanac).

**Sekcija:** 3.6 (trigger plane) + 12.4 (HITL) + 6.9 + P2.6 (missed-run).

---

## O6 — Exec auto-reviewer (post-izvršni pregled)

**Tip:** MEHANIZAM (reviewer jezgra)

**Sinteza (Go):**
```go
type ExecResult struct{ Stdout, Stderr []byte; ExitCode int; ChangedFiles []Diff; NetCalls []EgressLog }
type ReviewRule func(ctx, e ExecResult) ([]Finding, error)
type ExecReviewer struct{ rules []ReviewRule }
func (r *ExecReviewer) Review(ctx, e ExecResult) ([]Finding, error) // POST-exec, NE pre-approval
// rules (sloj): secret-leak (6.4 entropy), destructive-change (5.3 dirty-tree/taint),
//   unexpected-network (6.3 egress log), error-swallow (15.5), stderr-anomaly
// Findings → 15.3 audit (hash-lanac) + 6.9 on_error grana; ne blokira, ali MOŽE otvoriti 15.5 breaker
```
**Salvage:** OpenClaw `exec-auto-reviewer.ts` (obrazac); NEXUS `core/secrets.py` (entropy redact) +
`core/circuit.py` (STAGNATION/NO_PROGRESS) + `core/provenance.py` (taint).

**Ugovor/RED:** exec koji iscuri secret (entropy) → `Finding{secret-leak}` + redakcija 6.4 (nikad raw u
journalu P0.3); exec koji prebriše fajl MIMO edita → `Finding{destructive}` + 5.3 taint; ponovljeni
error-swallow → 15.5 signal → breaker.

**Sekcija:** 6.9 (on_error) + 15.3/15.5 (audit/health) + 6.4 (secrets) + 5.3 (taint).

---

## O7 — Identity dubina (per-channel-prefix / human-delay)

**Tip:** MEHANIZAM (identity resolver jezgra) + DATA (identity sadržaj)

**Sinteza (Go):**
```go
type Identity struct {
    ID string
    Default ChannelIdentity
    PerChannel map[s14.ChannelID]ChannelIdentity // isti agent, drugi glas po kanalu
    HumanDelay HumanDelayConfig
}
type ChannelIdentity struct{ Name, Persona, Prefix string }
type HumanDelayConfig struct{ MinDelay, MaxDelay time.Duration; BurstChars int } // simulacija tipkanja
func (i *Identity) For(ch s14.ChannelID) ChannelIdentity // fallback na Default

// identity = DATA (11.1); per-channel prefix ulazi u assembler kao IDENTITY sloj (11.1 trust-fence:
// NIKAD u RULES poziciju). human-delay = ISPORUKA (14.5 delivery) — mijenja TIMING, ne SEMANTIKU odgovora
// (delay se primjenjuje na delivery, NE u prompt — inače model počne "glumiti" kašnjenje).
```
**Salvage:** OpenClaw `identity.per-channel-prefix.ts` + `identity.human-delay.ts` (obrazac); Hermes
SOUL.md identity (obrazac).

**Ugovor/RED:** per-channel prefix za kanal X → odgovor nosi prefix X (ne Y, ne default); human-delay
ne mijenja PAYLOAD odgovora (isti sadržaj, samo chunk-timing); identity blok NIKAD u RULES/instrukcijsku
poziciju (11.1 fence → PROVENANCE_DOWNGRADE ako untrusted u RULES).

**Sekcija:** 11.1 (identity/assembler) + 14.5 (channels delivery).

---

## KROS-NAPOMENA (za sve gapove)

1. **Svi gapovi su DATA-rich, MEHANIZAM-light:** G1/G3 su jedina dva s teškim OS/voice mehanizmom;
   G2/G4/G5/O6/O7 su uglavnom registry/store/reviewer jezgra + data (profil/obligacija/identitet).
   Jezgrena granica je ista kao S0-S18: enforcement/izolacija = kod, sadržaj = data.
2. **Zajednički invarijanti:** (a) sve opasno → 6.9 lifecycle + P1.4 (G1/G5), (b) sve trajno →
   journal (P0.3) + audit hash-lanac (15.3), (c) sve cross-profile → deny-default (G2), (d) sve
   "gotovo" → evidence-gated (G4/O6, ne samoprijava).
3. **Gdje ulaze u plan:** G1→S4.5/6.9/13.1; G2→S6.6/9.2/10.6/14.5; G3→S13.3/7/6.7; G4→16.1/3.6/16.6;
   G5→3.6/12.4/6.9; O6→6.9/15.3/6.4; O7→11.1/14.5. Nijedan ne treba NOVU sekciju — svi su dopune
   postojećih (osim G4 ObligationStore koji je 16.1+3.6 spoj, i G2 koji je S6.6 proširenje na profile).
4. **RED testovi su red-capable** (kontrolirana mutacija → fail-closed), ne prozni opisi — po
   Tier-3 ugovornom standardu.
