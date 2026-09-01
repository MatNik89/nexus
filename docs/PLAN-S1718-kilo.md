PLAN S17+S18

# PLAN-S17+S18 — Ekstenzibilnost/distribucija + Deployment/operacije (zadnje dvije)

Jezgra `CGO_ENABLED=0`. S17 = MEHANIZAM jezgra (loader/updater/field-bridge) + ADAPTER; S18 =
`service` flag (worker/gateway/rollout) + MEHANIZAM (migracije). Gate: P1.5 (artefakt→6.8),
P2.5 (data-rezidencija po tenantu). 17.1/17.2/17.3 su jezgra (distribuiran artefakt); 17.4+18.x =
`service` flag.

---

## 17.1 Plugin sustav

**Tip:** MEHANIZAM (loader/ABI jezgra) + ADAPTER (plugin)

**Aspekti → najbolji izvor:**
- MCP-based extensions → **goose**
- "everything is a plugin" nad Cordis DI kernelom (petlja/servisi/alati/hookovi/policy/SDK) →
  **DeepSeek Harness** (developer preview!)
- plugin mehanizam → **OpenCode**

**Sinteza (izvršni plugin loader/ABI — iza 6.8/11.5 gatea; Reversa NIJE ovdje):**
```go
type Plugin interface{ /* ABI: tool | service | hook | policy */ }
type PluginLoader struct{ gate *s6.SupplyChainGate; lifecycle *s11.Lifecycle }
// plugin se učitava SAMO iza 6.8 (provenance/signature) + 11.5 (scan/pin) gatea (P1.5)
// Cordis DI obrazac (DeepSeek): petlja/servisi/alati/hookovi/policy/SDK su ZAMJENJIVI —
//   ali jezgreni ABI je naš; plugin NE smije zamijeniti kernel trust-boundary (6.x)
// Reversa NIJE ovdje (premješteno u 11.2 kao skill-pack DATA); 17.1 = izvršni loader/ABI samo
```
DeepSeek Harness = developer-preview (rc-status/breaking-change rizik za matricu) — obrazac, ne
zavisnost.

**Salvage:** NEXUSv2 `packs/` + `managers/skill_import.py`/`skill_hub.py` (loader) →
**Python→Go port**.

**Ugovor/RED:** **P1.5** (artefakt/plugin → 6.8 gate). RED (naš): plugin bez 6.8 potpisa/provenance
→ loader ODBIJA (ne učitava); plugin koji pokuša zamijeniti kernel trust-boundary → REJECTED.

**Verifikacija:** goose (Apache-2.0), DeepSeek Harness (MIT, developer-preview), OpenCode (MIT) — stvarni.

**Pareto floor:** MCP extensions (goose) + Cordis DI zamjenjivost (DeepSeek) + mehanizam (OpenCode) +
kernel-boundary nepromjenjivost (naš — plugin nikad ne zamjenjuje 6.x) → ≥ svaki.

---

## 17.2 Update mehanizam i release kanali

**Tip:** MEHANIZAM (jezgra) + ADAPTER (TUF)

**Aspekti → najbolji izvor:**
- kanali + samostalan update → **Codex CLI**
- versioned release + self-update → **goose**
- višekanalni in-code upgrade (`--upgrade`, stable/dev grane) → **Aider**

**Sinteza (kanali + atomska instalacija + rollback; TUF = sigurnosna osnova):**
```go
type Channel struct{ Name, Version string } // stable | dev | ...
type Updater struct{ channels map[string]Channel; tuf *tuf.Client }
func (u *Updater) Check() (Update, error)  // provjera verzije na startu (Aider obrazac)
func (u *Updater) Update(ch Channel) error // TUF verify metadata → atomska instalacija → rollback anchor
// TUF (python-tuf standard): rollback/freeze zaštita update-metadata lanca
//   TUF verificira i dohvaća; HARNESS posjeduje kanale, atomsku instalaciju i rollback
// runners-up: Ollama (in-binary self-update), axoupdater/self_update (komponente)
```

**Salvage:** NEXUSv2 `core/update.py` (upgrade :180, trusted-only :53) → **Python→Go port**.

**Ugovor/RED:** **P1.5** (update instalira SAMO verificiran artefakt). RED (naš): unsigned/neverifikabilan
update → ODBIJ (17.2 spec); rollback na prethodnu verziju (atomski anchor).

**Verifikacija:** Codex CLI (Apache-2.0), goose (Apache-2.0), Aider (Apache-2.0), TUF (Apache-2.0) —
stvarni.

**Pareto floor:** kanali (Codex) + versioned (goose) + višekanalni (Aider) + **TUF sigurnosna osnova**
(naš — nitko ne veže update na TUF rollback/freeze) → ≥ svaki.

---

## 17.3 Packaging (jedan binary / laka instalacija)

**Tip:** MEHANIZAM (build) + ADAPTER (release CI)

**Aspekti → najbolji izvor:**
- Rust single binary (harness primjer) → **goose**
- reusable cross-platform binarni artefakti + installer + manifest → **dist (bivši cargo-dist)**
- moderni packaging put → **Astral uv**

**Sinteza (Go single-binary CGO_ENABLED=0 + goreleaser/dist):**
```go
// go build -tags netgo,osusergo -ldflags "-s -w" → JEDAN binarni (bez cgo) na linux/darwin/windows
// release CI: goreleaser (Go ekvivalent dist/cargo-dist) → artefakti + installer + manifest + SBOM (6.8)
// Astral uv je Python put — za Go je goreleaser/dist prirodan (single binary je Go prednost)
```
Single binary = bez runtime-setupa kod korisnika (kriterij (d) iz jezične odluke).

**Salvage:** NEXUSv2 `setup_nexus.py` (:66) + `Dockerfile` (non-root multi-stage) → **Python→Go port**
(build skripta).

**Ugovor/RED:** P1.5 (artefakt → 6.8 gate prije publish). RED (naš): `go build` bez cgo → jedan
binarni radi na 3 OS-a; artefakt bez SBOM/provenance → 6.8 odbija publish.

**Verifikacija:** goose (Apache-2.0), dist/cargo-dist (Apache-2.0/MIT), Astral uv (MIT) — stvarni.

**Pareto floor:** single binary (goose) + reusable mehanizam (dist) + moderni put (uv) + Go
single-binary + goreleaser (naš) → ≥ svaki.

---

## 17.4 Field diagnostics i upgrade bridge (`service` flag)

**Tip:** MEHANIZAM jezgra (redaction/consent/update-verification) + ADAPTER

**Aspekti → najbolji izvor:**
- scrubban reproducible bundle → **NEXUS bugreport.py**
- error tracking → **GlitchTip / Sentry**
- issue adapter → **OpenTelemetry Collector**

**Sinteza (bug → bundle → issue → VERIFICIRAN upgrade; nikad auto-slanje):**
```go
type FieldBridge struct{}
func (f *FieldBridge) Bundle(run RunOutcome) (Bundle, error)
// scrubban (redaction 6.4/6.7) + reproducibilan (verzije + korelacijski ID) — NIKAD sirovi podaci
func (f *FieldBridge) Issue(bundle Bundle) (IssueRef, error) // veže bundle uz issue (opt-in)
func (f *FieldBridge) Upgrade(bundle Bundle, ch Channel) error // VERIFICIRAN update (17.2), ne auto
// teren: bug → bundle → issue → `upgrade` — BEZ automatskog slanja ILI instalacije
```
Cross-ref 15.3 (audit) i 17.2 (update).

**Salvage:** NEXUSv2 `core/bugreport.py` + update-notify → **Python→Go port**.

**Ugovor/RED:** 17.4 spec (opt-in, verified). RED (naš): bundle sadrži tajnu → redaction (ne sirovo);
upgrade bez 17.2 verifikacije → ODBIJ; auto-slanje bez opt-in → NIKAD (nema mrežnog poziva).

**Verifikacija:** GlitchTip (MIT), Sentry (FSL/source-available), OpenTelemetry Collector (Apache-2.0)
— stvarni.

**Pareto floor:** scrubban bundle (NEXUS) + error tracking (Sentry) + issue adapter (Collector) +
verified-upgrade bez auto-slanja (naš) → ≥ svaki.

---

## 18.1 Headless worker/API razdvajanje, queue/lease, skaliranje (`service` flag)

**Tip:** MEHANIZAM (worker) + ADAPTER

**Aspekti → najbolji izvor:**
- konkurentne sesije s perzistencijom → **OpenHands**
- produkcijski multi-worker deployment → **Dify**
- queue/lease semantika → **Temporal**

**Sinteza (worker/API razdvojeni; queue lease iz S7; horizontalno skaliranje):**
```go
type Worker struct{ queue *s7.Queue }   // lease/reclaim/fencing iz S7 (P2.3) — NE novi queue
type APIServer struct{ app App }        // odvojen od workera (stateless front)
// horizontalno: N workera claima iz DURABLE queue-a (S7) — nema dijeljenog in-memory state
// session perzistencija kroz journal (P0.3) — worker crash → drugi reclaima (P2.3)
```
Queue/lease je S7 vlasnik; 18.1 je SAMO deployment adapter (N worker over S7 queue).

**Salvage:** NEXUSv2 `core/deploy.py` + `core/queue.py`/`core/fleet.py` → **Python→Go port** (adapter).

**Ugovor/RED:** **P2.3** (queue lease/fencing — već u S7). RED (naš): worker crash → drugi reclaima
(S7 RED, kroz 18.1 adapter); API server bez statea → restart ne gubi run (journal).

**Verifikacija:** OpenHands (MIT), Dify (Apache-2.0), Temporal (MIT) — stvarni.

**Pareto floor:** konkurentne sesije (OpenHands) + multi-worker (Dify) + queue/lease (Temporal) +
S7-lease reuse (naš — ne novi queue) → ≥ svaki.

---

## 18.2 Multi-tenant gateway: rate-limit, ključevi, budgeti (`service` flag)

**Tip:** MEHANIZAM (gateway jezgra) + ADAPTER

**Aspekti → najbolji izvor:**
- de facto standard prednjeg sloja → **LiteLLM proxy**
- governance nad providerima → **Portkey AI Gateway**
- tenant kvote → **Dify**

**Sinteza (per-tenant + per-user izolirani workspace kao JEDNA jedinica):**
```go
type Gateway struct{ tenants map[string]Tenant }
// per-tenant: rate-limit + ključevi + budgeti (LiteLLM obrazac) — 6.6 authn/6.7 governance
// qm/Durable Objects obrazac: per-user izolirani workspace = JEDNA jedinica
//   (memory+files+keychain+permissions+cron+budget zajedno) — "drugi chat_id" NIJE tenant boundary
//   security posture: Strict/Auto/Dangerous (per-user); veže 5.2 (worktree) + 6.6 (authn)
// P2.5: data-rezidencija PO TENANTU (provider descriptor eval prije slanja)
```

**Salvage:** NEXUSv2 `gateway/base.py` (deny-default) + `core/credit.py` (budget) →
**Python→Go port**.

**Ugovor/RED:** **P2.5** (data-rezidencija po tenantu) + 6.6 (deny-default). RED (naš): tenant A ne
vidi tenant B (deny-default, ne post-filter); budget prekoračen → 429 + fence (7.2); rezidencijska
politika tenanta → provider gate P2.5.

**Verifikacija:** LiteLLM (MIT), Portkey (core MIT), Dify (Apache-2.0) — stvarni.

**Pareto floor:** prednji sloj (LiteLLM) + governance (Portkey) + kvote (Dify) + per-user workspace
kao jedinica (naš/qm) → ≥ svaki.

---

## 18.3 Health/readiness + rollout/canary/rollback (`service` flag)

**Tip:** ADAPTER (komponente, ne agent harnessi — namjerno)

**Aspekti → najbolji izvor:**
- health/readiness/rollout standard → **Kubernetes**
- canary/progressive delivery → **Argo Rollouts**
- automatizirani canary s metričkim gateom → **Flagger**

**Sinteza (health/readiness endpoint + canary/rollback):**
```go
type Health struct{} // /healthz (liveness) + /readyz (readiness) — NEXUS deploy.py obrazac
// rollout: Argo Rollouts (canary/progressive) + Flagger (metrički gate)
// SLO/alerting NIJE ovdje (15.4) — 18.3 je samo process-liveness + rollout mehanika
```
Komponente (K8s/Argo/Flagger) su ADAPTERI — ne gradimo vlastiti orchestrator.

**Salvage:** NEXUSv2 `core/deploy.py` (readiness :6) + `core/health.py` (swallow) →
**Python→Go port**.

**Ugovor/RED:** 18.3 spec (readiness + canary). RED (naš): `/readyz` vraća NOT-READY dok queue/journal
nije spreman; canary s regresijom → Flagger rollback (metrički gate).

**Verifikacija:** Kubernetes (Apache-2.0), Argo Rollouts (Apache-2.0), Flagger (Apache-2.0) — stvarni.

**Pareto floor:** standard (K8s) + progressive (Argo) + metrički canary (Flagger) + readiness-
endpoint (naš) → ≥ svaki.

---

## 18.4 State/schema migracije + backup/restore

**Tip:** MEHANIZAM (UNCLEAR backend dok S0.4 ne odabere)

**Aspekti → najbolji izvor:**
- UNCLEAR — legalno prazno (kandidati ovise o persistence backendu: sqlite/Postgres)

**Sinteza (migracije iz S0.4 + point-in-time backup; backend NEODLUČEN):**
```go
type Store struct{} // migrate() iz S0.4 (SCHEMA_VERSION + _migrate) — monotoni upcast (P0.1)
func (s *Store) Backup() (Snapshot, error)  // point-in-time (WAL snapshot / pg_dump)
func (s *Store) Restore(snap Snapshot) error // idempotent restore + verify
// backend UNCLEAR dok S0.4 ne odabere (sqlite modernc.org/sqlite za v1 → Postgres kad treba scale)
// migracijski alat: alembic (Python) / yoyo (Python) / golang-migrate (Go) — po backendu
```

**Salvage:** NEXUSv2 `core/store.py` (SCHEMA_VERSION + `_migrate`) + `core/export.py` (scrub :164,
import :347) → **Python→Go port**.

**Ugovor/RED:** P0.1 (migracija-MUST: monotoni upcast, karantena, bez lossy downcast). RED (naš):
backup/restore round-trip → identično stanje (verify hash); migracija nepoznatog schema → karantena
(ne tihi drop).

**Verifikacija:** UNCLEAR (spec-praznina — backend neodlučen); alembic (MIT), yoyo (Apache-2.0),
golang-migrate (MIT) — stvarni.

**Pareto floor:** migracije (S0.4 upcast) + backup/restore (naš) → ≥ (UNCLEAR top-3 — pošteno prazno).

---

## S17+S18 cross-cutting napomene

1. **17.1/17.2/17.3 su jezgra (distribuiran artefakt), 17.4+18.x = `service` flag:** plugin loader,
   updater i packaging su jezgra; field-bridge i deployment su uvjetno-obvezni (`service`).
2. **P1.5 je S17 srž:** plugin (17.1), update (17.2) i artefakt (17.3) svi prolaze ISTI 6.8
   supply-chain gate — unsigned/neverifikabilan se odbija.
3. **18.2 per-user workspace = JEDNA jedinica (qm obrazac):** memory+files+keychain+permissions+
   cron+budget zajedno; "drugi chat_id" NIJE tenant boundary; P2.5 rezidencija po tenantu.
4. **18.1 NE gradi novi queue:** worker/API razdvajanje + horizontalno skaliranje koristi S7 queue
   (lease/fencing iz P2.3) — S7 je vlasnik, 18.1 je adapter.
5. **18.4 UNCLEAR je ispravno prazan:** dok S0.4 ne odabere backend, top-3 migracijskih alata se
   ne može birati (sqlite vs Postgres različiti alati) — pošteno, ne izmišljamo.
6. **Honest gap:** 17.1/17.2/17.3/17.4/18.1/18.2/18.3/18.4 — gate je P1.5 + P2.5 + P2.3 + P0.1
   (već postojeći ugovori); nijedan S17/S18 nema NOVI dedicirani Annex RED. Ako moderator želi
   simetriju: P0.21 "artifact-never-publish-unsigned" (iako je P1.5 već pokriva).
7. **Verifikacija:** svi kandidati stvarni (goose/OpenCode/Dify/Temporal/LiteLLM/Portkey/K8s/Argo/
   Flagger MIT/Apache-2.0; DeepSeek Harness MIT developer-preview; Sentry source-available; dist/
   cargo-dist, uv, TUF, alembic/yoyo/golang-migrate stvarni); salvage `update/packs/gateway/bugreport/
   deploy/store/export` interni (postoje — audit potvrdio), port je logika.
