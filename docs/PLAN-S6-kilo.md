PLAN S6

# PLAN-S6 — S6 Sigurnost i dozvole (NAJKRITIČNIJA — membrane SALVAGE 1:1 + parity, ne pisati iznova)

Jezgra `CGO_ENABLED=0`. S6 je 100% MEHANIZAM (mi pišemo). **Audit-konsenzus:** membrane su
izbrušene (fail-closed, redakcija, hash-lanac, egress SSRF) — rewrite riskira regresiju → **salvage
1:1 + parity test**, a GORTEX je VEĆ Go = **direktan reuse, ne port**. Gate: P1.4 (draft/commit/
compensate), P1.6 (channels-gate + HITL), P2.1 (forget/purge), P2.2 (bounded budgets). Per-OS
build-tagovi: Linux Landlock+seccomp REAL preko `x/sys/unix` BEZ cgo; Win/Mac adapteri.

---

## 6.0 Trust classification i policy enforcement point

**Tip:** MEHANIZAM (jezgra)

**Aspekti → najbolji izvor:**
- approval politike (untrusted/on-request/on-failure/never) → **Codex CLI**
- security analyzer nad akcijama PRIJE izvršenja → **OpenHands**
- kontekstualni policy decision point ("subjekt X alat Y resurs Z args A") → **OPA** (komponenta)

**Sinteza (jedan PEP: classify → decide, fail-closed; executor provodi, ne pita):**
```go
type TrustTier int // GREEN | YELLOW | RED
type PEP struct{ mode Mode; rules []Rule }
func (p *PEP) Classify(ctx, subj Principal, tool Tool, res Resource, args json.RawMessage) TrustTier
func (p *PEP) Decide(tier TrustTier, d DecisionRequest) Decision // ALLOW|ASK|DENY
// OPA obrazac: odluka je KONTEKSTUALNA (tko, koji alat, nad čime, s kojim args) — ne globalna
// fail-closed: nepoznat subjekt/tool/res → DENY (default-reject)
```
LlamaFirewall NIJE ovdje (detekcijski sloj → 6.5). PEP je jedini enforcement point; executor NE
smije imati vlastiti zaobilazni put (6.9).

**Salvage:** NEXUSv2 `core/permissions.py` (classify :376, decide :447, Mode) → **Python→Go port**
(direktna logika, ne prepisivanje).

**Ugovor/RED:** 6.0 spec (fail-closed). RED (naš): nepoznat subjekt/tool → `DENY` (default-reject);
PEP odluka se provodi U executoru, nema zasebnog puta koji ju zaobilazi.

**Verifikacija:** Codex CLI (Apache-2.0), OpenHands (MIT), OPA (Apache-2.0) — stvarni.

**Pareto floor:** approval-politike (Codex) + pre-execution analiza (OpenHands) + kontekstualni
decision-point (OPA) → ≥ svaki.

---

## 6.1 Permission gating (ask/allow/deny po alatu i argumentu)

**Tip:** MEHANIZAM (jezgra)

**Aspekti → najbolji izvor:**
- approval + sandbox eskalacija po komandi → **Codex CLI**
- per-extension dozvole → **goose**
- jasne potvrde rizičnih naredbi → **gptme**
- allowlist pravila po argumentu → † Claude Code (referent)

**Sinteza (5-tier Mode + per-tool/argument allowlist + shlex-token):**
```go
type Mode int // DENY_ALL | ASK_ALL | ASK_DANGEROUS | ALLOW_READONLY | ALLOW_ALL
type Gate struct{ mode Mode; allow map[string][]ArgRule } // tool → arg-pravila (regex na arg)
func (g *Gate) Authorize(tool Tool, args json.RawMessage) Decision
// shell-cmd se shlex-tokenizira PRIJE arg-matcha (NEXUS permissions.py obrazac) — ne regex na sirov string
// SECRET_ARGS: args koji IZGLEDAJU kao tajna → ASK/redact prije slanja (6.4)
```
Argument-level policy je ključna obrana od injectiona (least privilege, spec 6.1).

**Salvage:** NEXUSv2 `core/permissions.py` (5-tier Mode :170, SECRET_ARGS :181, shlex-token) →
**Python→Go port**.

**Ugovor/RED:** 6.1 spec (per-tool+argument). RED (naš): destruktivna naredba (`rm -rf /`) pod
`ASK_DANGEROUS` → `ASK` (ne ALLOW); isti tool s dopuštenim argom pod `ALLOW_READONLY` → ALLOW ali
6.9 i dalje auditira.

**Verifikacija:** Codex CLI (Apache-2.0), goose (Apache-2.0), gptme (MIT) — stvarni.

**Pareto floor:** approval+eskalacija (Codex) + per-extension (goose) + potvrde (gptme) + arg-level
shlex (naš — nitko ne tokenizira prije matcha) → ≥ svaki.

---

## 6.2 Sandbox izvršavanja (fs + procesi) — GORTEX Go REUSE + parity

**Tip:** MEHANIZAM (per-OS build-tagovi; GORTEX membrana = Go reuse)

**Aspekti → najbolji izvor:**
- OS-native Seatbelt (mac) + Landlock/seccomp (Linux) → **Codex CLI**
- container izolacija cijelog runtimea → **OpenHands**
- kernel-level sandbox → **gVisor** (E2B/Firecracker = microVM opcija)

**Sinteza (per-OS `Boundary` + out-of-process GORTEX membrana, BEZ in-process fallbacka):**
```go
type Boundary interface{ Confine(cmd *exec.Cmd, o ConfineOpts) error }

// sandbox_linux.go (//go:build linux) — REAL enforcement, BEZ cgo:
//   Landlock: x/sys/unix.Syscall(SYS_LANDLOCK_CREATE_RULESET/ADD_RULE/RESTRICT_SELF) — 3 čista syscalla
//   seccomp:  prctl(PR_SET_NO_NEW_PRIVS) + prctl(PR_SET_SECCOMP, FILTER, BPF) — bez libseccomp
//   bwrap:    exec bubblewrap (filtered_jail) kao opcionalna jača membrana
// sandbox_windows.go — Job Object (KILL_ON_JOB_CLOSE) + restricted token (x/sys/windows)
// sandbox_darwin.go — sandbox-exec (Seatbelt) kao ADAPTER (nema Landlock ekvivalenta)

// GORTEX membrana = DIREKTAN Go reuse (audit: gortex/landlock_linux.go, sandbox.go, process_manager.go)
// opasni I/O (diff/procesi/Landlock/MCP) u kompajliranom helperu iza autentificiranog verzioniranog IPC
// NIKAD fallback na nezaštićen in-process put (6.2 dopuna)
```
**PARITY TEST (ključno):** GORTEX ponašanje u Go-verziji mora biti byte-identično NEXUS membrani —
isti syscall-rezultati, ista fail-closed granica. Parity test je dio RED-a, jer rewrite riskira
regresiju izbrušenih membrana.

**Salvage:** **`gortex/` = Go REUSE 1:1** (`landlock_linux.go`, `sandbox.go`, `process_manager.go`,
`tool_executor.go`, `netjail.go`) — NE port, NE prepisati. Samo adapter sloj koji ga zove.

**Ugovor/RED:** **P2.2** (bounded budgets — 4.2 veže) + parity. RED `test_hard_process_budget_kills_
descendants` + **parity** (GORTEX Landlock: write na ne-dopušten path → `EPERM` identično NEXUS membrani;
bez dopuštenog patha → fail-closed, ne degrade na in-process).

**Verifikacija:** Codex CLI (Apache-2.0), OpenHands (MIT), gVisor (Apache-2.0) — stvarni; GORTEX
interni (postoji — audit potvrdio `landlock_linux.go`).

**Pareto floor:** OS-native Landlock/seccomp (Codex) + container (OpenHands) + kernel-level (gVisor)
+ **GORTEX out-of-process membrana + parity** (naš — nitko nema izbrušenu membranu koju NE prepisuje)
→ ≥ svaki.

---

## 6.3 Network egress kontrola

**Tip:** MEHANIZAM (jezgra)

**Aspekti → najbolji izvor:**
- sandbox default bez mreže, eksplicitan opt-in → **Codex CLI**
- microVM s kontroliranim izlazom → **microsandbox**
- network policy na container razini → **OpenHands**

**Sinteza (allowlist + SSRF metadata hard-block + redirect re-check):**
```go
type Egress struct{ allowlist []EgressRule }
func (e *Egress) Check(host string, ip net.IP) error
// SSRF hard-block (NEXUS egress.py :31): metadata-IP (169.254.169.254) u decimal/hex/octal/mapped-IPv6
// → REFUSE PRIJE diala (ne poslije DNS-resolve)
func (e *Egress) Recheck(redirectURL *url.URL) error // redirect → ponovna provjera (ne trusta prvi check)
// default: DENY (bez mreže), eksplicitan opt-in po hostu/portu
```
**Salvage:** NEXUSv2 `core/egress.py` (:202 check, :264 _CheckedRedirect, :31 metadata-hard-block) +
`core/netjail.py` (`filtered_jail` :390, bwrap :467) → **Python→Go port**.

**Ugovor/RED:** 6.3 spec (SSRF). RED (naš): `Check` na 169.254.169.254 (bilo koji oblik) → REFUSE;
redirect na privatni IP → `Recheck` REFUSE.

**Verifikacija:** Codex CLI (Apache-2.0), microsandbox (MIT), OpenHands (MIT) — stvarni.

**Pareto floor:** default-deny (Codex) + microVM (microsandbox) + container policy (OpenHands) +
SSRF metadata hard-block + redirect re-check (naš — nitko ne hard-blocka sve oblike metadata-IP) → ≥ svaki.

---

## 6.4 Secret handling i redakcija

**Tip:** MEHANIZAM (jezgra)

**Aspekti → najbolji izvor:**
- keyring integracija, secrets IZVAN configa → **goose**
- pre-send scan → **detect-secrets** (komponenta)

**Sinteza (keyring + redakcija value+shape+entropy; nikad u config/prompt/log):**
```go
type Broker struct{ keyring Keyring }
func (b *Broker) Get(ref string) (Secret, error) // iz OS keychaina / chmod-600 (1.2) — NIKAD config
func (b *Broker) Redact(s string) string
// gitleaks obrazac (NEXUS broker.py :82): entropija + shape (ključ-tok) + value — ne samo regex
// redakcija PRIJE journala/projekcija (P0.3) — tajna smije postojati samo kao sealed reference
```
PII redakcija NIJE ovdje (kanonski vlasnik = 6.7 Presidio) — 6.4 drži samo secret/keyring (ne dupliraj).

**Salvage:** NEXUSv2 `core/secrets.py` (:32) + `core/broker.py` (redact :82, gitleaks entropy) →
**Python→Go port**.

**Ugovor/RED:** P0.3 (tajna samo kao sealed reference). RED (naš): secret u tool-outputu → redakcija
prije journala (nikad raw na disku u indeksiranom payloadu).

**Verifikacija:** goose (Apache-2.0), detect-secrets (Apache-2.0) — stvarni.

**Pareto floor:** keyring izvan configa (goose) + pre-send scan (detect-secrets) + entropy+shape
redakcija (naš) → ≥ svaki.

---

## 6.5 Prompt injection obrana + canary

**Tip:** MEHANIZAM (jezgra; detektori = defense-in-depth)

**Aspekti → najbolji izvor:**
- aktivno održavan detekcijski sloj → **LlamaFirewall (PurpleLlama)**
- programabilne ograde → **NeMo Guardrails**
- input/output skeneri → **LLM Guard**
- mjerenje obrana → **AgentDojo**

**Sinteza (arhitektura = PRIMARNA obrana; fence+judge = detektori; canary = tripwire):**
```go
type Guard struct{ fence *Guardrail; judge *Guardian }
// PRIMARNO (arhitektura): instrukcija ≠ podatak (S0 ContextBlock trust_class), least privilege (6.1)
// fence: datamark untrusted sadržaja; judge: presuda o bloku PRIJE assemblya (fail-closed)

type Canary struct{ token string } // per-install sintetički token (NEXUS canary.py)
func (c *Canary) Scan(output string) bool
// token u izlazu dokazuje MOGUĆ boundary breach → reakcija FAIL-CLOSED:
// blokiraj CIJELU isporuku (ne samo ukloni token) + audit + alert + rotacija; nastavak tek uz ljudsku odluku
// NIKAD ne zamjenjuje egress/secret policy (defense-in-depth)
```
**Salvage:** NEXUSv2 `core/guardrail.py` (:58) + `core/guardian.py` (:61) + `core/canary.py` (:52) +
`core/provenance.py` (datamark :40) → **Python→Go port**.

**Ugovor/RED:** 6.5 spec (canary FAIL-CLOSED). RED (naš, veže 16.3): canary token u izlazu → blok
isporuke + audit + rotacija (ne samo redakcija tokena); injection pokušaj kroz tool-output →
fence detektira untrusted, judge blokira assembly.

**Verifikacija:** LlamaFirewall (MIT, aktivan), NeMo Guardrails (Apache-2.0), LLM Guard (MIT),
AgentDojo (MIT) — stvarni.

**Pareto floor:** detekcijski sloj (LlamaFirewall) + programabilne ograde (NeMo) + I/O skeneri (LLM
Guard) + **canary FAIL-CLOSED + arhitektura-kao-primarna-obrana** (naš) → ≥ svaki.

---

## 6.6 AuthN/AuthZ i tenant/workspace izolacija (`service` flag)

**Tip:** MEHANIZAM (jezgra; `service` flag aktivira)

**Aspekti → najbolji izvor:**
- ključevi, timovi, budgeti → **LiteLLM proxy**
- multi-tenant workspace model → **Dify**
- RBAC nad korisnicima/grupama → **Open WebUI**

**Sinteza (deny-default + tenant identity + RBAC; HITL token owner = S6/S7 jezgra):**
```go
type AuthN struct{ principals map[string]Principal } // deny-default (NEXUS gateway/base.py obrazac)
type RBAC struct{ roles map[string][]Permission }
func (a *AuthN) Authenticate(cred Credential) (Principal, error) // PKCE OAuth (6.6) ili key
func (a *AuthN) Authorize(p Principal, perm Permission) bool   // deny-default, least privilege
```
HITL decision-token (P1.6): `ApprovalChallenge` vlasnički verificira S6/S7 (potpisan, scoped,
single-use), NIKAD channel-adapter.

**Salvage:** NEXUSv2 `gateway/base.py` (deny-default :103) + `core/oauthcode.py` (PKCE :105) →
**Python→Go port**.

**Ugovor/RED:** **P1.6** (channels→extensions closure + HITL token). RED `test_channels_contract_fails_
closed` (tablični): A `channels` bez `extensions/S6.8` → `INCOMPLETE_CAPABILITY_CLOSURE`; B dvostruki
approval → `APPROVAL_REPLAY`.

**Verifikacija:** LiteLLM (MIT), Dify (Apache-2.0), Open WebUI (MIT) — stvarni.

**Pareto floor:** ključevi/budžeti (LiteLLM) + multi-tenant (Dify) + RBAC (Open WebUI) + deny-default
+ jezgreni HITL token (naš) → ≥ svaki.

---

## 6.7 Data governance: retention/brisanje/export/PII (baseline + service)

**Tip:** MEHANIZAM (jezgra baseline; `service` dodatak)

**Aspekti → najbolji izvor:**
- PII detekcija/anonimizacija → **Presidio** (komponenta, kanonski PII vlasnik)
- retention + masking nad trace-ovima → **Langfuse**
- SQLite retention/purge engine → **vlastiti** (OSS transcript-governance ne postoji)

**Sinteza (FORGET vs PURGE + export; baseline uvijek, service = DSAR/legal-hold):**
```go
// BASELINE (jezgra — aktivira persistence, NE service):
type Forget struct{ selection Selection } // REVERZIBILNO: tombstone + soft-delete
type Purge struct{ targets []Store; legalHold LegalHold } // IREVERZIBILNO: hard-delete + probe
func (p *Purge) Execute(subjectID string) (PurgeStatus, error)
// post-delete PROBE po storeu (P2.1): lažni success → PARTIAL, zabrana COMPLETED
// EXPORT: scrub + redact (P0.3) — izvoz bez tajni/PII u sirovom obliku

// SERVICE dodatak: DSAR / legal-hold / tenant-policy / rezidencija
```
**Salvage:** NEXUSv2 `core/memory.py` (_forget :140) + `core/export.py` (scrub :164) + `core/memgit.py`
→ **Python→Go port**.

**Ugovor/RED:** **P2.1** + `test_purge_cannot_complete_with_residual_copy` — isti subject u memory+
vector+cache+backup, jedan adapter lažno vrati success → `PARTIAL` + `PURGE_INCOMPLETE`, zabrana
`COMPLETED`.

**Verifikacija:** Presidio (MIT), Langfuse (MIT) — stvarni; Fides arhiviran (izbačen), OpenMetadata
konceptualni referent.

**Pareto floor:** PII anonimizacija (Presidio) + retention/masking (Langfuse) + **vlastiti
forget/purge/probe engine** (naš — OSS transcript-governance ne postoji) → ≥ svaki.

---

## 6.8 Supply-chain: plugin provenance i signing

**Tip:** MEHANIZAM + ADAPTER (vanjski skeneri iza sučelja)

**Aspekti → najbolji izvor:**
- potpisivanje artefakata → **Sigstore/cosign** (komponenta)
- ranjivosti ovisnosti → **OSV-Scanner** (komponenta)
- provenance lanca isporuke → **in-toto** (standard)

**Sinteza (provenance + signature + vuln-scan + trust-by-hash; isti gate za plugin i artefakt):**
```go
type SupplyChainGate struct{ scanner *osvScanner; signer *sigstoreVerifier }
func (g *SupplyChainGate) Verify(artifact ArtifactManifest) (Attestation, error)
// P1.5: BUILT→SCANNED→ATTESTED→SIGNED→STAGED→PUBLISHED (preskakanje stanja nelegalno)
// trust-by-hash (NEXUS hooks.py :99): plugin/artefakt se učitava SAMO ako hash odgovara potpisanom pinu
// isti kodni put za pluginove (17.1) I distribucijski artefakt (17.2) — 6.8 je jedan gate
```
**Salvage:** NEXUSv2 `core/supplychain.py` (OSV :148) + `core/secscan.py` (:244) + `core/licenses.py`
(:71) + `core/hooks.py` (trust-by-hash :99) → **Python→Go port**.

**Ugovor/RED:** **P1.5** + `test_post_signature_artifact_tamper_blocks_publish` — 1 byte staged
binara nakon potpisa → `ARTIFACT_DIGEST_MISMATCH`, release kanal + install target byte-identični.

**Verifikacija:** Sigstore/cosign (Apache-2.0), OSV-Scanner (Apache-2.0), in-toto (Apache-2.0) —
stvarni.

**Pareto floor:** potpis (Sigstore) + ranjivosti (OSV) + provenance (in-toto) + trust-by-hash (naš)
→ ≥ svaki.

---

## 6.9 Non-bypassable lifecycle enforcement

**Tip:** MEHANIZAM (jezgra)

**Aspekti → najbolji izvor:**
- vlastiti typed middleware chain → **NEXUS** `permissions.py`/`hooks.py`
- PreToolUse/PostToolUse hooks (rade i u bypass modu) → † Claude Code (referent)
- hook sustav iza conformance gatea → **DeepSeek Harness / Cordis**

**Sinteza (middleware chain + on_error kao GRANA iz svake faze):**
```go
type MiddlewareChain struct{ hooks []Hook }
// success-faze: before_run → before_tool → after_tool → before_deliver (fiksni redoslijed)
// jezgreni policy PRVI i ZADNJI; plugin hook smije SAMO SUZITI (ne proširiti, ne preskočiti audit)

// on_error NIJE linearni član — GRANA iz SVAKE faze:
func (m *MiddlewareChain) Run(ctx, ev Envelope) error {
    for _, phase := range []Phase{beforeRun, beforeTool, afterTool, beforeDeliver} {
        if err := m.runPhase(phase, ev); err != nil { return m.onError(phase, err) }
    }
}
// onError(phase, err): causal-error + audit, BEZ izvršavanja kasnijih success-hookova
```
**Salvage:** NEXUSv2 `core/permissions.py` + `core/hooks.py` (pre_tool deny :81) → **Python→Go port**.

**Ugovor/RED:** **P1.4** (draft→approve→commit→verify→compensate) + `test_approval_cannot_authorize_
modified_effect` — nakon previewa/approvala promijeni recipient/amount uz isti grant → commit
`APPROVAL_BINDING_MISMATCH`, vanjski sink 0 poziva, grant opozvan. + P1.6 (lifecycle redoslijed).

**Verifikacija:** Claude Code († referent), DeepSeek Harness (MIT, developer-preview), NEXUS hooks
(interni) — stvarni (DeepSeek = preview, iza conformance gatea).

**Pareto floor:** typed middleware (NEXUS) + bypass-mode hooks (Claude Code) + Cordis hook sustav
(DeepSeek) + **on_error kao grana + jezgreni-policy-prvi-i-zadnji** (naš) → ≥ svaki.

---

## S6 cross-cutting napomene

1. **GORTEX = Go REUSE + PARITY TEST, NE rewrite:** audit-konsenzus (membrane izbrušene) → `gortex/`
   se uključuje 1:1 kao `internal/gortex`, a parity test garantira byte-identično ponašanje (isti
   syscall-rezultati, ista fail-closed granica). Ovo je NAJVEĆA ušteda i najmanji rizik u cijelom kernelu.
2. **Per-OS build-tagovi:** Linux = Landlock+seccomp REAL (`x/sys/unix`, BEZ cgo); Windows = Job Object
   + restricted token; macOS = sandbox-exec (adapter). S6 ne degradira tiho — ako OS nema mehanizam,
   capability fail-closed (ne in-process fallback).
3. **6.9 on_error = grana** (ne linearni član) — greška u bilo kojoj fazi skače na on_error BEZ
   izvršavanja kasnijih success-hookova; jezgreni policy ide PRVI i ZADNJI.
4. **Vlasništvo:** 6.7 (data-governance) nadjačava S9 (memory); 6.4 = secret/keyring, 6.7 = PII
   (Presidio) — bez dupliranja; 6.8 = jedan gate za plugin (17.1) i artefakt (17.2).
5. **Honest gap:** 6.0/6.1/6.3/6.4/6.5 nemaju dedicirani Annex RED (gate = spec + P0.3/P1.4/P1.6/P2.2
   posredno); 6.2 ima P2.2+parity, 6.7 ima P2.1, 6.8 ima P1.5, 6.9 ima P1.4+P1.6. Najkritičnija
   sekcija ima NAJVIŠE izravnih gateova — dosljedno.
6. **Verifikacija:** svi kandidati stvarni (licence/aktivnost gore); GORTEX + permissions/egress/
   netjail/secrets/canary/supplychain/hooks interni (postoje — audit potvrdio), **gortex = Go reuse,
   ostalo = Python→Go port (logika, ne ekosustav)**.
