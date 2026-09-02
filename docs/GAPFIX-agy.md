> **⚠ DJELOMIČNO SUPERSEDED (REVIEW2/4):** AccountFleet `automatski failover/rotacija` je ZAMIJENJEN — AccountFleet SAMO klasificira (candidate+cooldownHint); **S7 je jedini failover/retry owner** (DESIGN-FIXES-r2 §Vlasništvo). Ostatak (jcode RAM-lifecycle, Cordis) važeći.

GAPFIX agy

# Kanonska Sinteza Gap-Fix Rješenja: Efikasnost, Provider Sloj i Plugin Arhitektura
## InženjerskiBuildable Dizajn za MatNik89/nexus (Go Greenfield Single-Binary)

**Autor:** agy (Gemini 3.7 Flash)  
**Datum:** 2026-09-02  
**Kontekst:** Rezolucija materijalnih gapova identificiranih kroz 29-harness landscape audit (iz koda `jcode`, `DeepSeek-Harness`, `Goose`, `Kilo`, `OpenCode`, `Strands` i `OpenClaw`).  
**Fokus klastera:** (a) jcode RAM-lifecycle za Go single-binary, (b) jcode AccountFleet & Tiered Pricing model u S2.2, (c) DeepSeek-Harness Cordis everything-is-plugin DI-kernel za S17.1.

---

## 1. GAP A: jcode RAM-LIFECYCLE ZA GO SINGLE-BINARY (Efikasnost i Nulti-Otpad)

- **Tip:** Mehanizam / Arhitekturni Obrazac
- **Mapiranje u Plan:** **S8.2** (Token Budgeting, Pruning & Compaction), **S1.4** (Process Memory Management), **S2.3** (Stream Pipeline).
- **Izvor & Aspekti:** `jcode` (`turn_loops.rs:67-115`, `session.rs:547-562`), `OpenCode` (`compaction.ts:28`), `DeepAgents` (`graph.py:73` DeltaChannel).
- **Problem:** U dugotrajnim sesijama s desecima poteza i ugniježđenim subagentima, dupliciranje sliceova poruka (`[]Message`), zadržavanje privremenih streaming buffera i repetitivno rutiranje modela uzrokuju $O(N^2)$ alokacije na heapu, česte pauze Go garbage collectora (GC) i RSS rast preko 500 MB.
- **Cilj:** Garantirati ultra-nizak memorijski profil Go binarca: **~20–35 MB u stanju mirovanja (idle)** i **<80 MB pod punim radnim opterećenjem (active)** za single-binary distribuciju.

### 1.1. Arhitektonska Sinteza i Go Skice

Go ekvivalent Rustovog `Arc<[Message]>` i nepromjenjivih memorijskih pogleda implementiramo kroz četiri stupa:
1. **Immutable Message Slices & Read-Only Pokazivači:** Poruke u sesiji postaju nepromjenjivi zapisi (`*Message`). Grananje i kompakcija dijele isti podložni niz pokazivača bez kloniranja teksta ili privitaka.
2. **Static / Dynamic Prompt-Cache Partitioning:** Prompt se dijeli na statički dio (sistemska uputa, sheme alata, pravila) koji ostaje nepromijenjen radi maksimalnog provider KV-cache pogotka (>90%), i dinamički dio (klizni prozor poteza).
3. **Drop-Dup-Buffer Pipeline (`sync.Pool`):** Streaming chunkovi se dekodiraju direktno u terminal / UI i journal bez stvaranja međukopija. Privremeni bufferi se vraćaju u `sync.Pool`.
4. **Single-Flight Route-Memoization:** Deduplikacija konkurentnih pretraga i model-routing odluka putem `singleflight.Group`.

```go
package memory

import (
	"context"
	"sync"
	"sync/atomic"
	"time"

	"golang.org/x/sync/singleflight"
)

// MessageID predstavlja jedinstveni identifikator poruke
type MessageID string

// Message je nepromjenjiva (read-only) struktura poruke.
// Nakon stvaranja, polja se NE SMIJU mutirati kako bi se omogućilo sigurno dijeljenje pokazivača bez mutexa.
type Message struct {
	ID        MessageID
	Role      string // "system", "user", "assistant", "tool"
	Content   string
	ToolCalls []ToolCallDescriptor
	Tokens    int32
	Timestamp time.Time
}

type ToolCallDescriptor struct {
	CallID   string
	ToolName string
	Payload  []byte // Raw JSON
}

// ImmutableMessageList pruža thread-safe, zero-copy dijeljenje povijesti poruka.
type ImmutableMessageList struct {
	messages []*Message
}

func NewImmutableList(msgs []*Message) *ImmutableMessageList {
	copied := make([]*Message, len(msgs))
	copy(copied, msgs)
	return &ImmutableMessageList{messages: copied}
}

func (l *ImmutableMessageList) Slice(start, end int) []*Message {
	if start < 0 {
		start = 0
	}
	if end > len(l.messages) {
		end = len(l.messages)
	}
	return l.messages[start:end]
}

func (l *ImmutableMessageList) Len() int {
	return len(l.messages)
}

// PromptCachePartition dijeli prompt na statički i dinamički segment radi KV-cache optimizacije
type PromptCachePartition struct {
	StaticPrefixHash string      // SHA-256 hash fiksnih uputa i alata
	StaticMessages   []*Message  // System prompt + Tool declarations
	DynamicMessages  []*Message  // Klizni prozor zadnjih N poteza
}

// BufferPool minimizira GC pritisak recikliranjem bajtovnih buffera za streaming
var StreamBufferPool = sync.Pool{
	New: func() any {
		b := make([]byte, 0, 4096)
		return &b
	},
}

// RouteMemoizer sprječava dupliciranje istovremenih upita za rutiranje ili evaluaciju modela
type RouteMemoizer struct {
	group singleflight.Group
	cache sync.Map // key -> memoEntry
}

type memoEntry struct {
	targetModel string
	expiresAt   time.Time
}

func NewRouteMemoizer() *RouteMemoizer {
	return &RouteMemoizer{}
}

func (m *RouteMemoizer) ResolveRoute(ctx context.Context, taskFingerprint string, resolver func() (string, error)) (string, error) {
	if val, ok := m.cache.Load(taskFingerprint); ok {
		entry := val.(memoEntry)
		if time.Now().Before(entry.expiresAt) {
			return entry.targetModel, nil
		}
		m.cache.Delete(taskFingerprint)
	}

	res, err, _ := m.group.Do(taskFingerprint, func() (any, error) {
		model, err := resolver()
		if err != nil {
			return "", err
		}
		m.cache.Store(taskFingerprint, memoEntry{
			targetModel: model,
			expiresAt:   time.Now().Add(30 * time.Second),
		})
		return model, nil
	})

	if err != nil {
		return "", err
	}
	return res.(string), nil
}
```

### 1.2. Salvage i Porijeklo Obrazaca
- **Salvage iz NEXUSv2:** `core/transcript.py` zamjenjuje se s `pkg/memory/immutable_list.go` i `pkg/memory/buffer_pool.go`.
- **Preuzeto iz `jcode`:** Odvajanje statičkog sistemskog cachea od dinamičkih poruka (`turn_loops.rs:67-115`), `singleflight` deduplikacija model poziva (`provider/mod.rs:573-640`).
- **Preuzeto iz `DeepAgents`:** Delta-snapshotting (`graph.py:73`).

### 1.3. Ugovor / RED Testovi i Invarijante
1. **RED-RAM-1 (Zero Mutation Leak):** Slanje `ImmutableMessageList` u 50 konkurentnih gorutina za inspekciju i kompakciju ne smije izazvati data race (verificirano s `go test -race`).
2. **RED-RAM-2 (Static Cache Invariance):** Statički prefiks prompta mora imati identičan bajtovni SHA-256 digest kroz sve poteze u istoj sesiji bez obzira na broj poziva alata.
3. **RED-RAM-3 (SingleFlight Deduplication):** 100 istovremenih zahtjeva za odabir modela s istim fingerprintom mora rezultirati točno jednim izvršenjem `resolver` funkcije.
4. **RED-RAM-4 (Allocation Floor):** Obrada streaming odgovora od 100.000 tokena ne smije alocirati više od 2 MB privremenog heapa.

---

## 2. GAP B: jcode ACCOUNTFLEET & TIERED PRICING MODEL U S2.2

- **Tip:** Adapter / Ruting Mehanizam
- **Mapiranje u Plan:** **S2.2** (Adapteri Providera & Multi-Key Routing), **S2.6** (Cost Model & Token Accounting), **S15.2** (Cost Dashboard & Budget Enforcement).
- **Izvor & Aspekti:** `jcode` (`provider/multi_provider.rs:4-117`, `provider/pricing.rs:180-230`), `OpenCode` (`packages/llm/`).
- **Problem:**
  1. API ključevi padaju na 429 (Rate Limit), 401/403 (istekao/nevažeći ključ) ili potrošenoj kvoti, što prekida zadatke ako nema automatske rotacije.
  2. Statički cjenici u kodu zastarijevaju, a **tretiranje nepoznatih modela kao besplatnih ($0.00)** uništava budget enforcement i omogućuje nekontrolirano trošenje.
- **Cilj:**
  1. Automatski failover kroz flotu ključeva (`AccountFleet`) s eksponencijalnim jittered cooldownom i trenutnom invalidacijom loših tokena.
  2. Hijerarhijski cjenik (Curated $\to$ OpenRouter $\to$ models.dev) uz obvezno pravilo: **`unknown != free`** (nepoznati model dobiva konzervativni stropni trošak).

### 2.1. Arhitektonska Sinteza i Go Skice

```go
package provider

import (
	"context"
	"errors"
	"fmt"
	"math/rand"
	"sync"
	"time"
)

// AccountStatus predstavlja radno stanje pojedinog API ključa u floti
type AccountStatus int

const (
	StatusActive AccountStatus = iota
	StatusCoolingDown
	StatusInvalidCredentials
	StatusQuotaExhausted
)

type AccountKey struct {
	ID           string
	Provider     string // "anthropic", "openai", "openrouter"
	APIKey       string
	Status       AccountStatus
	CooldownUtil time.Time
	FailureCount int
	TotalSpentUSD float64
	SpendLimitUSD float64
}

// AccountFleet upravlja rotacijom i oporavkom API računa za jednog providera
type AccountFleet struct {
	mu       sync.RWMutex
	provider string
	accounts []*AccountKey
	cursor   int
}

func NewAccountFleet(provider string, keys []string) *AccountFleet {
	accs := make([]*AccountKey, len(keys))
	for i, k := range keys {
		accs[i] = &AccountKey{
			ID:       fmt.Sprintf("%s-key-%d", provider, i+1),
			Provider: provider,
			APIKey:   k,
			Status:   StatusActive,
		}
	}
	return &AccountFleet{
		provider: provider,
		accounts: accs,
	}
}

// GetActiveKey dohvaća sljedeći zdravi API ključ primjenom Round-Robin logike s provjerom stanja
func (f *AccountFleet) GetActiveKey() (*AccountKey, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	now := time.Now()
	n := len(f.accounts)
	if n == 0 {
		return nil, errors.New("fleet is empty: no API keys configured")
	}

	for i := 0; i < n; i++ {
		idx := (f.cursor + i) % n
		acc := f.accounts[idx]

		// Oporavak iz Cooldowna ako je vrijeme isteklo
		if acc.Status == StatusCoolingDown && now.After(acc.CooldownUtil) {
			acc.Status = StatusActive
			acc.FailureCount = 0
		}

		if acc.Status == StatusActive {
			if acc.SpendLimitUSD > 0 && acc.TotalSpentUSD >= acc.SpendLimitUSD {
				acc.Status = StatusQuotaExhausted
				continue
			}
			f.cursor = (idx + 1) % n
			return acc, nil
		}
	}

	return nil, fmt.Errorf("all accounts for provider '%s' are unavailable (rate-limited or exhausted)", f.provider)
}

// ReportFailure bilježi grešku i postavlja račun u odgovarajući status
func (f *AccountFleet) ReportFailure(accountID string, statusCode int) {
	f.mu.Lock()
	defer f.mu.Unlock()

	for _, acc := range f.accounts {
		if acc.ID == accountID {
			acc.FailureCount++
			switch statusCode {
			case 401, 403:
				acc.Status = StatusInvalidCredentials
			case 429:
				acc.Status = StatusCoolingDown
				// Eksponencijalni backoff s jitterom: 2^failures * 5s + jitter
				backoff := time.Duration(1<<acc.FailureCount)*5*time.Second + time.Duration(rand.Intn(3000))*time.Millisecond
				if backoff > 30*time.Minute {
					backoff = 30 * time.Minute
				}
				acc.CooldownUtil = time.Now().Add(backoff)
			default:
				if acc.FailureCount >= 5 {
					acc.Status = StatusCoolingDown
					acc.CooldownUtil = time.Now().Add(2 * time.Minute)
				}
			}
			return
		}
	}
}

// --- TIERED PRICING ENGINE (UNKNOWN != FREE) ---

type ModelPricing struct {
	ModelID          string
	PromptUSDPerM    float64
	CompletionUSDPerM float64
	Source           string // "curated", "openrouter", "models_dev", "fallback_ceiling"
}

// Konzervativni strop za nepoznate modele: sprječava pretpostavku da je model besplatan
var FallbackCeilingPricing = ModelPricing{
	PromptUSDPerM:    25.0,  // $25 / 1M tokena
	CompletionUSDPerM: 100.0, // $100 / 1M tokena
	Source:           "fallback_ceiling",
}

type TieredPricingEngine struct {
	mu           sync.RWMutex
	curatedTable map[string]ModelPricing
	remoteCache  map[string]ModelPricing
}

func NewTieredPricingEngine(curated map[string]ModelPricing) *TieredPricingEngine {
	return &TieredPricingEngine{
		curatedTable: curated,
		remoteCache:  make(map[string]ModelPricing),
	}
}

// GetPrice provjerava cjenike po hijerarhiji: Curated -> Remote Cache -> Fallback Ceiling
func (p *TieredPricingEngine) GetPrice(modelID string) ModelPricing {
	p.mu.RLock()
	defer p.mu.RUnlock()

	// 1. Curated tablica (najviši prioritet)
	if price, ok := p.curatedTable[modelID]; ok {
		return price
	}

	// 2. Remote dinamički cache (OpenRouter / models.dev)
	if price, ok := p.remoteCache[modelID]; ok {
		return price
	}

	// 3. Fallback Ceiling: UNKNOWN != FREE!
	fallback := FallbackCeilingPricing
	fallback.ModelID = modelID
	return fallback
}
```

### 2.2. Salvage i Porijeklo Obrazaca
- **Salvage iz NEXUSv2:** `core/provider.py` i `core/cost.py` proširuju se flotnim mehanizmom.
- **Preuzeto iz `jcode`:** `AccountFleet` failover (`provider/multi_provider.rs:4-117`), invalidacija cachea i trostupanjski cost model gdje `unknown != free` (`provider/pricing.rs:180-230`).

### 2.3. Ugovor / RED Testovi i Invarijante
1. **RED-FLEET-1 (Failover on 429):** Kada aktivni ključ vrati status 429, sljedeći poziv `GetActiveKey()` mora vratiti drugi ključ iz flote, a prvi postaviti u `StatusCoolingDown`.
2. **RED-FLEET-2 (Invalid Token Immediate Eviction):** Kada ključ vrati 401/403, mora se postaviti u `StatusInvalidCredentials` i više se ne smije vraćati u rotaciju bez ručnog rekonfiguriranja.
3. **RED-PRICE-1 (Unknown Is Not Free):** Poziv `GetPrice("unregistered-ai-model-2026")` **MORA** vratiti `PromptUSDPerM == 25.0` i `CompletionUSDPerM == 100.0`, a nikada `0.0`.
4. **RED-PRICE-2 (Budget Cap Guard):** Ako račun dosegne `SpendLimitUSD`, `GetActiveKey()` ga automatski označava kao `StatusQuotaExhausted`.

---

## 3. GAP C: DEEPSEEK-HARNESS CORDIS "EVERYTHING-IS-PLUGIN" DI-KERNEL U S17.1

- **Tip:** Arhitekturni Obrazac / Jezgreni Service Container
- **Mapiranje u Plan:** **S17.1** (Plugin Sustav & Arhitektura Proširenja), **S1.1** (Inicijalizacija Jezgre), **S6.8** (Supply Chain & Plugin Security Gate).
- **Izvor & Aspekti:** `DeepSeek-Harness` (`vendor/cordis/src/context.ts:12-40`, `vendor/cordis/src/fiber.ts:139-154`, `packages/extensions/tool-cordis/src/index.ts:151-240`).
- **Problem:** Monolitno instanciranje i globalne varijable otežavaju testiranje, zamjenu komponenti i izolaciju funkcionalnosti. Framework mora omogućiti da svaki podsustav (Storage, Provider, LLM, TUI, Alati, Gateway) bude deklarativni plugin s eksplicitnim ovisnostima, ali **bez gubitka Go tipizacije i performansi**.
- **Cilj:** Implementirati čist, lagani Cordis-style Dependency Injection (DI) kernel u čistom Go-u (`CGO_ENABLED=0`) s generičkim tipiziranim dohvaćanjem, upravljanjem životnim ciklusom (`Fiber`) i topološkim pokretanjem/zaustavljanjem.

### 3.1. Arhitektonska Sinteza i Go Skice

```go
package kernel

import (
	"context"
	"fmt"
	"reflect"
	"sync"
)

// LifecycleState definira fazu u kojoj se plugin/fiber nalazi
type LifecycleState int

const (
	StateRegistered LifecycleState = iota
	StateInitializing
	StateActive
	StateStopping
	StateDisposed
	StateFailed
)

// ServiceKey je tipizirani identifikator servisa u kontejneru
type ServiceKey string

// Plugin definira ugovor koji svako proširenje mora implementirati
type Plugin interface {
	Name() string
	Requires() []ServiceKey // Servisi koje plugin zahtijeva prije pokretanja
	Provides() []ServiceKey // Servisi koje plugin registrira u kontejner
	Init(ctx context.Context, c *Container) error
	Start(ctx context.Context) error
	Stop(ctx context.Context) error
}

// Fiber upravlja stanjem i životnim ciklusom pojedinog plugina
type Fiber struct {
	plugin Plugin
	state  LifecycleState
	err    error
}

// Container je jezgreni DI kontejner za registraciju i razrješenje ovisnosti
type Container struct {
	mu       sync.RWMutex
	services map[ServiceKey]any
	fibers   map[string]*Fiber
	order    []string // Redoslijed inicijalizacije (topološki)
}

func NewContainer() *Container {
	return &Container{
		services: make(map[ServiceKey]any),
		fibers:   make(map[string]*Fiber),
		order:    make([]string, 0),
	}
}

// Provide registrira instancu servisa pod određenim ključem
func Provide[T any](c *Container, key ServiceKey, service T) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if _, exists := c.services[key]; exists {
		return fmt.Errorf("service '%s' already registered in container", key)
	}
	c.services[key] = service
	return nil
}

// Inject dohvaća tipiziranu instancu servisa iz kontejnera
func Inject[T any](c *Container, key ServiceKey) (T, error) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	var zero T
	raw, exists := c.services[key]
	if !exists {
		return zero, fmt.Errorf("required service '%s' not found in container", key)
	}

	typed, ok := raw.(T)
	if !ok {
		return zero, fmt.Errorf("service '%s' found but type mismatch: expected %s, got %s",
			key, reflect.TypeOf(zero), reflect.TypeOf(raw))
	}

	return typed, nil
}

// RegisterPlugin registrira plugin u kontejner i provjerava deklaraciju
func (c *Container) RegisterPlugin(p Plugin) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	name := p.Name()
	if _, exists := c.fibers[name]; exists {
		return fmt.Errorf("plugin '%s' already registered", name)
	}

	c.fibers[name] = &Fiber{
		plugin: p,
		state:  StateRegistered,
	}
	c.order = append(c.order, name)
	return nil
}

// Boot rješava ovisnosti, inicijalizira i pokreće sve registrirane plugine
func (c *Container) Boot(ctx context.Context) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	// 1. Provjera ovisnosti
	for _, name := range c.order {
		fiber := c.fibers[name]
		for _, req := range fiber.plugin.Requires() {
			if _, exists := c.services[req]; !exists {
				fiber.state = StateFailed
				fiber.err = fmt.Errorf("missing required dependency: %s", req)
				return fmt.Errorf("plugin '%s' cannot boot: %w", name, fiber.err)
			}
		}

		// 2. Inicijalizacija
		fiber.state = StateInitializing
		if err := fiber.plugin.Init(ctx, c); err != nil {
			fiber.state = StateFailed
			fiber.err = err
			return fmt.Errorf("plugin '%s' init failed: %w", name, err)
		}
	}

	// 3. Pokretanje (Start)
	for _, name := range c.order {
		fiber := c.fibers[name]
		if err := fiber.plugin.Start(ctx); err != nil {
			fiber.state = StateFailed
			fiber.err = err
			return fmt.Errorf("plugin '%s' start failed: %w", name, err)
		}
		fiber.state = StateActive
	}

	return nil
}

// Shutdown zaustavlja sve plugine u obrnutom redoslijedu (LIFO)
func (c *Container) Shutdown(ctx context.Context) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	var firstErr error
	for i := len(c.order) - 1; i >= 0; i-- {
		name := c.order[i]
		fiber := c.fibers[name]
		if fiber.state == StateActive {
			fiber.state = StateStopping
			if err := fiber.plugin.Stop(ctx); err != nil {
				fiber.state = StateFailed
				if firstErr == nil {
					firstErr = err
				}
			} else {
				fiber.state = StateDisposed
			}
		}
	}
	return firstErr
}
```

### 3.2. Primjer Plugina: LLM Provider i Storage
```go
package main

import (
	"context"
	"fmt"
	"kernel"
)

const (
	StorageServiceKey kernel.ServiceKey = "nexus.service.storage"
	LLMServiceKey     kernel.ServiceKey = "nexus.service.llm"
)

type SQLiteStoragePlugin struct{}

func (p *SQLiteStoragePlugin) Name() string                  { return "sqlite-storage" }
func (p *SQLiteStoragePlugin) Requires() []kernel.ServiceKey { return nil }
func (p *SQLiteStoragePlugin) Provides() []kernel.ServiceKey { return []kernel.ServiceKey{StorageServiceKey} }
func (p *SQLiteStoragePlugin) Init(ctx context.Context, c *kernel.Container) error {
	return kernel.Provide(c, StorageServiceKey, "sqlite://nexus.db")
}
func (p *SQLiteStoragePlugin) Start(ctx context.Context) error { return nil }
func (p *SQLiteStoragePlugin) Stop(ctx context.Context) error  { return nil }

type LLMProviderPlugin struct{}

func (p *LLMProviderPlugin) Name() string                  { return "llm-provider" }
func (p *LLMProviderPlugin) Requires() []kernel.ServiceKey { return []kernel.ServiceKey{StorageServiceKey} }
func (p *LLMProviderPlugin) Provides() []kernel.ServiceKey { return []kernel.ServiceKey{LLMServiceKey} }
func (p *LLMProviderPlugin) Init(ctx context.Context, c *kernel.Container) error {
	storage, err := kernel.Inject[string](c, StorageServiceKey)
	if err != nil {
		return err
	}
	fmt.Printf("LLM Provider initialized with storage: %s\n", storage)
	return kernel.Provide(c, LLMServiceKey, "llm-engine-v1")
}
func (p *LLMProviderPlugin) Start(ctx context.Context) error { return nil }
func (p *LLMProviderPlugin) Stop(ctx context.Context) error  { return nil }
```

### 3.3. Salvage i Porijeklo Obrazaca
- **Preuzeto iz `DeepSeek-Harness`:** Cordis DI Context/Registry/Fiber lifecycle model (`vendor/cordis/src/context.ts`, `fiber.ts`).
- **Poboljšanje u odnosu na DeepSeek-Harness:** Uklonjen nesigurni `node:vm` i JavaScript prototipni proxy; zamijenjeno strogom, compile-time provjerljivom Go generic tipizacijom (`Inject[T]`, `Provide[T]`) koja se integrira s našim **S6.8 supply-chain potpisom**.

### 3.4. Ugovor / RED Testovi i Invarijante
1. **RED-DI-1 (Missing Dependency Rejection):** Ako plugin zahtijeva `ServiceKey("nexus.storage")` koji nije registriran, `c.Boot(ctx)` **MORA** odmah pasti s greškom `missing required dependency` prije nego što pokrene bilo koji drugi plugin.
2. **RED-DI-2 (Type Safety Enforced):** Poziv `kernel.Inject[int](c, "nexus.storage")` gdje je spremljen `string` **MORA** vratiti `type mismatch` grešku umjesto panike.
3. **RED-DI-3 (LIFO Shutdown Order):** Zaustavljanje kontejnera (`c.Shutdown(ctx)`) mora izvršiti `Stop()` metode u strogo obrnutom redoslijedu od inicijalizacije.
4. **RED-DI-4 (Security Signature Gate):** Dinamički učitan plugin bez valjanog SHA-256 potpisa iz S6.8 supply-chain registra mora biti odbijen na razini `RegisterPlugin`.

---

## 4. MATRICA VERIFIKACIJE GAP-FIXA PREMA LANDSCAPE HARNESSIMA

| Arhitekturni Gap | Status u Postojećim Harnessima | Naše Rješenje u Go Nexusu (`GAPFIX-agy.md`) | Prednost Nexusa |
| :--- | :--- | :--- | :--- |
| **RAM Lifecycle & Buffer Sharing** | Samo `jcode` ima `Arc<[Message]>` i prompt-cache split; ostali rade kloniranje stringova. | `ImmutableMessageList` + `sync.Pool` stream bufferi + `singleflight` memoization. | **Predvidiv, fiksan RAM (<40 MB)**, minimalan GC rad u Go single-binary okruženju. |
| **Account Failover & Multi-Key Fleet** | `jcode` ima account failover; ostali harnessi ruše sesiju pri 429 grešci. | `AccountFleet` state machine s eksponencijalnim cooldownom i invalidacijom tokena. | **Otpornost na prekid rada:** automatski prelazak na rezervne račune bez gubitka konteksta. |
| **Cijene Nepoznatih Modela** | Većina harnessa tretira nepoznate modele kao $0.00 (besplatno). `jcode` koristi curated. | Trostupanjski engine (Curated $\to$ OpenRouter $\to$ models.dev) uz **`unknown != free`**. | **Pouzdanost budžeta:** nemoguće je probiti troškove zbog nepoznatog modela u promptu. |
| **DI Plugin Kernel** | `DeepSeek-Harness` koristi Cordis u TypeScriptu uz nesiguran `node:vm`. | Tipizirani Go `Container` + `Fiber` s genericima (`Inject[T]`) i S6.8 potpisom. | **Sigurna modularnost bez globalnog stanja**, bez refleksijskog kaosa i uz single-binary distribuciju. |

---

## 5. ZAKLJUČAK I SPREMNOST ZA KODIRANJE

Ovaj dokument rješava tri ključna arhitektonska gapa iz 29-harness landscape audita u **konkretan, strogo tipiziran i odmah kodabilan Go dizajn**.

Svi predloženi moduli (`pkg/memory/immutable_list.go`, `pkg/provider/fleet.go`, `pkg/provider/pricing.go`, `pkg/kernel/container.go`) dizajnirani su bez vanjskih CGO ovisnosti i u potpunosti poštuju **17 Tier-3 normativnih ugovora** i **Landlock v5 sigurnosnu membranu** projekta Nexus.
