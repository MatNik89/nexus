PLAN S2

# PLAN-S2 — S2 Provider sloj (multi-provider / fallback / structured-output / lokalni / cost / routing)

Jezgra `CGO_ENABLED=0`. S2 je MEHANIZAM (mi pišemo) osim 2.4 (ADAPTER za lokalni server + MEHANIZAM
preflight). **Owner granica:** retry/deadline/cancel = S7 (P0.2) — 2.2 SAMO mapira error→fallback-target;
routing owner = 2.6 (jezgra), 12.3 = multi-agent potrošač. Gate: P0.2 (retry owner), P2.5 (provider-data
rezidencija). PROVIDER MEHANIZAM direktiva: zadržati OBA NEXUS načina (API HTTP + CLI-agent subprocess).

---

## 2.1 Multi-provider apstrakcija (3 auth-moda iza jednog `Provider`)

**Tip:** MEHANIZAM

**Aspekti → najbolji izvor:**
- 100+ providera kroz jedan format → **LiteLLM**
- čisti adapteri + capability negotiation + error normalizacija → **PydanticAI**
- unified provider ugovor + streaming + tool-loop → **Vercel AI SDK** (TS)

**Sinteza (Go `Provider` + pluggable `AuthMode` — 3 moda + 1 flag):**
```go
type Provider interface {
    ID() string
    Capabilities() ModelCapability                 // S0.3 (declared/measured)
    DataDescriptor() ProviderDataDescriptor         // P2.5 (deklarira PRIJE slanja)
    Chat(ctx, req ChatRequest, grant s7.AttemptGrant) (*ChatResponse, error)
    Stream(ctx, req ChatRequest, grant s7.AttemptGrant) (<-chan Chunk, error)
}

// --- 3 auth-moda, svi iza jednog sučelja ---
type AuthMode interface {
    Name() string
    Resolve(ctx context.Context, cfg AuthConfig) (AuthCredential, error)
}
type AuthCredential interface { Sign(*http.Request) error; Redacted() string } // nikad curenje

// (a) APIKeyAuth — DEFAULT, čisto
type APIKeyAuth struct{ BaseURL, APIKey string } // Bearer header; OpenAI-kompatibilan

// (b) OAuthAuth — SANKCIONIRANO (samo gdje provider SLUŽBENO nudi OAuth za API)
type OAuthAuth struct {
    DeviceEndpoint, TokenEndpoint, ClientID string; Scopes []string
    tok *oauthToken // refresh+expiry, cache u keyring (1.2)
} // device-code → poll → token → Bearer → refresh

// (c) CLIAgentAuth — subprocess na instaliran codex/claude CLI, alati UGAŠENI
type CLIAgentAuth struct{ Binary, Provider string } // Provider: "codex"|"claude"
func (c *CLIAgentAuth) Start(ctx) (io.ReadWriteCloser, error) {
    // exec: codex exec --tools "" --ephemeral --ignore-config (ili claude -p --tools "")
    // NEXUS drži petlju + alate; CLI = čisti text-in/out model, BEZ api ključa
}

// (d) SubscriptionOAuthAuth — ZASEBAN flag, NIKAD default, ToS upozorenje
type SubscriptionOAuthAuth struct{ ... } // repliciran OAuth bez CLI
// aktivacija MORA proći: config subscription_oauth.acknowledged_tos == true
// + redacted upozorenje "može prekršiti ToS providera, rizik bana" PRIJE prve upotrebe
// loader ODBIJA Resolve ako acknowledged_tos != true (fail-closed na nedostatak svjesnog pristanka)

// Provider bira auth po config: provider[].auth_mode ∈ {api_key, oauth, cli_agent}
// subscription_oauth je ZASEBAN config blok s vlastitim flagom, ne dio enum-a
```
CLI-agent mod: `CLIAgentAuth.Start` vraća reader/writer koji se adapterom mapira u isti `Chat`
ugovor (text-in/out) — petlja NE zna koji je mod; razlika je samo u `Capabilities()` (CLI-agent
deklarira floor bez streaminga).

**Salvage:** NEXUSv2 `llm/providers.py` (CLI-agent subprocess `--tools ""` + API HTTP, KeyPool,
CostTracker) → **Python→Go port**; sigurnosna finesa zadržana (CLI bez alata — NEXUS drži petlju).

**Ugovor/RED:** bez dediciranog Annex ugovora; gate = **P2.5** (DataDescriptor mora biti deklariran
PRIJE prvog slanja) + S0.3 capability. RED (naš): (d) `Resolve` bez `acknowledged_tos=true` → error
(nema tihe aktivacije); (c) subprocess bez `--tools ""` → odbij (alati moraju biti ugašeni).

**Verifikacija:** LiteLLM (MIT, aktivan), PydanticAI (MIT, aktivan), Vercel AI SDK (Apache-2.0,
aktivan) — stvarni.

**Pareto floor:** jedan format (LiteLLM) + čisti adapteri + cap-negotiation + error-normalizacija
(PydanticAI) + streaming/tool-loop ugovor (Vercel AI SDK) + **3 auth-moda iza jednog interfacea**
(naš — niti jedan od 3 nema CLI-agent subprocess auth ni OAuth device-flow) → ≥ svaki.

---

## 2.2 Fallback / retry / rate-limit / load balancing (owner = S7)

**Tip:** MEHANIZAM (TANKI sloj — NE retry petlja)

**Aspekti → najbolji izvor:**
- fallback lanci + cooldown → **LiteLLM**
- retries/fallbacks/timeouts/LB → **Portkey AI Gateway**
- retry+fallback + 5 LB strategija + L7 dubina → **Kong AI Gateway**

**Sinteza (2.2 SAMO mapira error→fallback-prijedlog; retry je S7):**
```go
type FallbackPlanner struct {
    chain    []Target                  // uređena fallback lista
    cooldown map[string]time.Time      // cooldown po targetu (LiteLLM obrazac)
    keys     *KeyPool                  // rotacija API ključeva
}
// NIJE vlasnik retryja — vraća PRIJEDLOG, ne poziva transport dvaput:
func (f *FallbackPlanner) Plan(err TypedError, cur Target) (Target, s7.FallbackProposal)
// s7.ExecutionPolicy{fallback_targets[], retryable_codes[], max_attempts} je JEDINI vlasnik
// 2.2 samo PUNI fallback_targets[] i mapira provider-error → kategorični kod (P0.2 delegat)
```
Invarijanta (P0.2): adapter/2.2 NE smije imati vlastitu retry petlju; svaki fizički pokušaj mora
predočiti jedinstveni `s7.AttemptGrant`.

**Salvage:** NEXUSv2 `llm/router.py` (`FallbackModel`, cooldown) + `providers.py _KeyPool` →
**Python→Go port**.

**Ugovor/RED:** **P0.2** + `test_adapter_cannot_self_retry` — provider dvaput pozove transport s istim
`AttemptGrant` → `ATTEMPT_NOT_AUTHORIZED`, transport counter = 1, S7 attempt counter = 1.

**Verifikacija:** LiteLLM (MIT), Portkey (core MIT), Kong (Apache-2.0, webfetch-verificiran kilo r2) —
stvarni.

**Pareto floor:** fallback-chain + cooldown (LiteLLM) + timeout/LB (Portkey) + LB strategije (Kong)
+ **"no self-retry, S7 owner"** (naš, P0.2 — niti jedan od 3 ne delegira retry vlasništvo) → ≥ svaki.

---

## 2.3 Tool-call parsing i structured output (kritično za slabe modele)

**Tip:** MEHANIZAM (jezgra: validate+re-ask+tolerant-parse); constrained-decoding = lokalni adapter

**Aspekti → najbolji izvor:**
- Pydantic validacija + automatski re-ask → **Instructor**
- constrained decoding (gramatika garantira valjan JSON) → **Outlines**
- schema-first DSL + parser tolerira šum → **BAML**

**Sinteza (validate → re-ask → salvage, nikad tiho prihvati nevalidan T):**
```go
type StructuredOutput[T any] struct { schema *jsonschema.Validator }
func (s *StructuredOutput[T]) Validate(out json.RawMessage) (T, *TypedError)
// nevaljan → TypedError{category:VALIDATION, retryability:S7_POLICY} → re-ask

func (s *StructuredOutput[T]) ReAskPrompt(prev *TypedError) string // feedback za sljedeći pokušaj

// tolerant parser (BAML obrazac): strip markdown fences, popravi trailing comma / quote
func salvageJSON(raw json.RawMessage) (json.RawMessage, error)

// constrained decoding (Outlines) = OPT-IN adapter za local-inference (2.4), NE jezgra za HTTP
type ConstrainedDecoder interface { Decode(ctx, grammar []byte, prompt string) (json.RawMessage, error) }
```
Jezgra uvijek radi validate+re-ask (radi i preko HTTP-a); constrained decoding je dostupan samo kad
provider daje lokalnu kontrolu (2.4) — deklarirano kroz `Capabilities().StructuredOutput`.

**Salvage:** NEXUSv2 `llm/executor.py` (SYSTEM protokol) + `llm/spawner.py` (`_salvage_json`) +
`core/toolschema.py` → **Python→Go port**.

**Ugovor/RED:** bez dediciranog Annex; gate = S0.1 `TypedError` (tipiziran, ne string). RED (naš):
malformiran JSON sa šumom → salvage ILI re-ask, ali NIKAD tiho prihvaćen nevalidan `T` (silent accept
zabranjen).

**Verifikacija:** Instructor (MIT), Outlines (Apache-2.0), BAML (Apache-2.0) — stvarni.

**Pareto floor:** validation+re-ask (Instructor) + constrained-decoding opt-in (Outlines) + tolerant
parser (BAML) → ≥ svaki.

---

## 2.4 Lokalni modeli (offline put) + hardware-aware fit

**Tip:** ADAPTER (lokalni server iza `Provider`) + MEHANIZAM (preflight)

**Aspekti → najbolji izvor:**
- standard lokalno serviranje + kompatibilan API → **Ollama**
- server mode, temelj svega lokalnog → **llama.cpp**
- produkcijsko serviranje na GPU → **vLLM**

**Sinteza (OpenAI-kompatibilan HTTP na sva tri + hardware-fit preflight):**
```go
type LocalProvider struct{ BaseURL string; Kind LocalKind } // Kind: ollama|llama_cpp|vllm
// govori isti OpenAI-kompatibilni /v1/chat/completions — za sva tri backenda

// hardware-aware preflight (obsidian zahtjev): MJERI na hostu, ne iz imena
type HardwareFit struct { RAMBytes, VRAMBytes uint64; AVX, CUDA bool }
func (f *HardwareFit) Fits(spec ModelSpec, quant QuantLevel) (ok bool, reason string, alt QuantLevel)
// ne stane → ok=false + prijedlog lakše kvantizacije (ne OOM-crash)
```
Preflight trči PRIJE prvog loada; `LocalProvider.Capabilities()` vraća `Measured` (ne declared) —
floor iz stvarnog mjerenja (S0.3).

**Salvage:** NEXUSv2 `llm/providers.py` (`LocalProvider` Ollama/llama.cpp) → **Python→Go port**.

**Ugovor/RED:** hardware-fit (obsidian zahtjev, distinktno od P2.5). RED (naš): model veći od
RAM/VRAM → `Fits=false` + prijedlog kvantizacije (odbij load, ne OOM-crash).

**Verifikacija:** Ollama (MIT), llama.cpp (MIT), vLLM (Apache-2.0) — stvarni.

**Pareto floor:** standard API (Ollama) + server-mode (llama.cpp) + GPU serving (vLLM) +
hardware-fit preflight (naš) → ≥ svaki.

---

## 2.5 Token accounting i cost tracking

**Tip:** MEHANIZAM

**Aspekti → najbolji izvor:**
- ugrađene cijene po modelu + spend log → **LiteLLM**
- prikaz cijene po sesiji u realnom vremenu → **Aider**
- proxy accounting bez izmjene koda → **Helicone**

**Sinteza (usage→cost→budget ceiling, veže 7.2/16.4):**
```go
type Price struct{ InputPer1K, OutputPer1K int64 } // minor units
type CostTracker struct {
    prices map[string]Price
    spend  atomic.Int64 // cumulative minor units
}
func (c *CostTracker) Record(model string, u Usage) int64
// real usage kad provider daje; inače len//4 estimate (NEXUS obrazac), označeno estimate=true
// cumulative ceiling → s7 circuit-breaker (P2.2 ResourceBudget.cost_minor_units)
// spend feed-a 16.4 verified-credit ledger (routing uči iz verificiranog, ne iz spend-a)
```
`spend` je MONOTON (P2.2: kumulativni counter raste, ne smije se smanjiti).

**Salvage:** NEXUSv2 `llm/providers.py` (`CostTracker`) + `core/circuit.py` (`add_cost`) →
**Python→Go port**.

**Ugovor/RED:** bez dediciranog Annex; gate = **P2.2** (`cost_minor_units` u ResourceBudget). RED
(naš): spend pređe ceiling → circuit-breaker fence-a nove pozive (over_budget), ne samo logira.

**Verifikacija:** LiteLLM (MIT), Aider (Apache-2.0), Helicone (MIT) — stvarni.

**Pareto floor:** price-table + spend log (LiteLLM) + real-time session cost (Aider) + proxy
accounting (Helicone) + budget ceiling + ledger tie-in (naš) → ≥ svaki.

---

## 2.6 Model routing (pravi model za pravi zadatak — jezgra owner)

**Tip:** MEHANIZAM (barbell=jezgra; naučeni router=opt-in iza ABI)

**Aspekti → najbolji izvor:**
- naučeni router jak/slab model, dokazana ušteda → **RouteLLM**
- brzo semantičko usmjeravanje → **semantic-router**
- pravila + fallback u praksi → **LiteLLM router**

**Sinteza (jezgreni ABI + barbell + naučeni router opcionalan):**
```go
type Router interface {
    Route(ctx, task Task, candidates []Target) (Target, error) // jezgreni ABI
}
type BarbellRouter struct{ plan, execute, verify Target } // JEZGRA (iz 2.2 fallback lanca)
func (b *BarbellRouter) Route(ctx, t Task, _ []Target) (Target, error) // plan/execute/verify po fazi

type LearnedRouter struct { bandit *reward.Bandit } // OPT-IN, uči iz 16.4 verified-credit
// epsilon-greedy; reward SAMO iz verificiranog ishoda (nikad self-report) — 16.4 ugovor

// routing uvjet: capability floor (S0.3) + cost (2.5) + verificirani score (16.4)
```
`Route` fail-closed: kandidat čiji `Effective()` padne ispod capability floora se odbija (ne
routira se tiho na nesposoban model). 12.3 (multi-agent) je POTROŠAČ ovog ABI-ja, ne vlasnik.

**Salvage:** NEXUSv2 `llm/router.py` (naučeni winner + barbell fallback) + `core/reward.py`
(epsilon-greedy bandit) → **Python→Go port**.

**Ugovor/RED:** gate = S0.3 capability + P0.2 (fallback-target je S7). RED (naš): `Route` na model
ispod capability floora → error (fail-closed).

**Verifikacija:** RouteLLM (Apache-2.0), semantic-router (MIT), LiteLLM (MIT) — stvarni.

**Pareto floor:** naučeni router (RouteLLM) + semantičko usmjeravanje (semantic-router) + pravila/
fallback (LiteLLM) + **barbell jezgra + verified-reward bandit** (naš) → ≥ svaki.

---

## S2 cross-cutting napomene

1. **Owner granica (P0.2):** 2.2/2.6 NIKAD ne retry-aju; svaki pokušaj nosi `s7.AttemptGrant`, a S7
   je jedini koji iz `FAILED_RETRYABLE` izdaje sljedeći. 2.2 samo mapira `TypedError` → `fallback_target`.
2. **Auth-modi su DATA-driven:** `auth_mode` je config vrijednost (1.1); `subscription_oauth` je
   zaseban blok s `acknowledged_tos` flagom — loader odbija bez eksplicitnog pristanka (fail-closed).
3. **P2.5 (rezidencija) je gate za SVAKI outbound:** `DataDescriptor` mora biti deklariran i POLICY-
   evaluiran PRIJE serializacije/DNS/connecta; fallback se PONOVNO autorizira (RED `test_fallback_
   cannot_bypass_residency_policy` je u P2.5, ne ovdje — ali 2.2/2.6 ga moraju poštivati).
4. **Honest gap (S2):** 2.1/2.3/2.4/2.5 nemaju dedicirani Annex RED — gate su posredni (P2.5 za
   2.1, S0.1 za 2.3, hardware-fit za 2.4, P2.2 za 2.5). Ako moderator želi simetriju, kandidati za
   matrix-prep: P0.7 „structured-output never-silent-accept", P0.8 „hardware-fit fail-closed".
5. **Verifikacija:** svi top-3 kandidati stvarni (licence/aktivnost gore); salvage `providers.py`/
   `router.py`/`circuit.py` interni, postoje (audit potvrdio), port je logika.
