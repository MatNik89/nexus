PLAN S14

# PLAN-S14 — S14 Sučelja (UX) — tanki adapteri nad application-API jezgre

Jezgra `CGO_ENABLED=0`. **S14 načelo: jezgra je LIBRARY; CLI/TUI/desktop/web/channel su TANKI
adapteri nad istim application-API** — nema posebnog nereentrantnog executor puta (NEXUS audit dug).
Gate: P1.6 (channels→extensions closure + HITL token), N9 (durable-delivery outbox). `full-UX`/
`channels` = profil; 14.1 je JEZGRA (P1 interakcija).

---

## 14.1 CLI/TUI

**Tip:** ADAPTER (tanki) nad jezgrenim `App`; TUI = bubbletea (single-binary garantiran)

**Aspekti → najbolji izvor:**
- Rust TUI, brz → **Codex CLI**
- open-source referentni agentic CLI → **Gemini CLI**
- najljepši TUI pristup → **crush** ♦ (source-available FSL-1.1-MIT, nije OSI)

**Sinteza (jezgra = library; CLI/TUI = tanki adapter nad `App`):**
```go
type App interface { // APPLICATION-API — jezgra; SVI adapteri zovu ovo, nema drugog puta
    Run(ctx, req RunRequest) (*RunResult, error)
    Stream(ctx, req RunRequest) (<-chan Chunk, error)
    Interrupt(runID string) error
}
type CLI struct{ app App } // ~40 subkomandi (NEXUS cli.py obrazac) — sve delegira na App
type TUI struct{ app App } // bubbletea — konzumira App.Stream channel, čist single-binary (CGO_ENABLED=0)
// desktop (Wails/Fyne) = ISTI App iza webview/native frontenda — odluka kasnije
```
CLI/TUI ne sadrže poslovnu logiku — samo parse argumenata + render streama; logika je u `App`.

**Salvage:** NEXUSv2 `cli.py` (133KB, ~40 subkomandi) + `tui/app.py` (:22) → **Python→Go port**
(samo argumenat-parse + render; logiku izvlačimo u App).

**Ugovor/RED:** 14.1 spec (single-binary). RED (naš): `go build` bez cgo → jedan binarni radi na
3 OS-a (bez runtime-setupa); TUI render streama ne blokira na dugom tool-pozivu.

**Verifikacija:** Codex CLI (Apache-2.0), Gemini CLI (Apache-2.0), crush (source-available) — stvarni.

**Pareto floor:** brz TUI (Codex) + referentni CLI (Gemini) + lijep TUI (crush) + **library-first
App + bubbletea single-binary** (naš) → ≥ svaki.

---

## 14.2 Headless / API / embeddable SDK mode

**Tip:** ADAPTER (tanki) + MEHANIZAM (reentrantan isti put)

**Aspekti → najbolji izvor:**
- REST+WS server oko agenta → **OpenHands**
- self-hostable platforma s API serverom → **Dify**
- API mode → **goose**
- embeddable typed SDK (fluent, dry-run default) → **NEXUS sdk.py**

**Sinteza (SDK koristi ISTI S0/6.9/7.x put — ne poseban executor):**
```go
type SDK struct{ app App }
func (s *SDK) Agent(role string) *AgentBuilder // fluent: Agent(role).WithSkill().WithTool().Live()
func (s *SDK) Run(ctx, req) (*RunResult, error) // → app.Run (ISTI executor, reentrantan)
// dry-run default (safe u test/CI): ne izvršava side-effect, samo planira — eksplicitan opt-in za run
// NIJE poseban nereentrantan executor (NEXUS audit dug) — SDK i CLI dijele isti App
```
`full-UX` flag; OpenAI-compatible endpoint (openai_api) je tanki adapter na isti App.

**Salvage:** NEXUSv2 `sdk.py` (fluent embeddable) + `gateway/openai_api.py` (:69) →
**Python→Go port**.

**Ugovor/RED:** 14.2 spec (isti put, reentrantan). RED (naš): SDK i CLI pozivaju isti `App.Run`
(reentrantan — dva paralelna SDK poziva ne dijele mutable state); dry-run ne dira side-effect sink.

**Verifikacija:** OpenHands (MIT), Dify (Apache-2.0), goose (Apache-2.0), OpenAI/Vercel AI SDK —
stvarni.

**Pareto floor:** REST+WS (OpenHands) + self-hostable (Dify) + API mode (goose) + **reentrantan isti-
put SDK** (naš) → ≥ svaki.

---

## 14.3 Web UI

**Tip:** ADAPTER (read-only nad App)

**Aspekti → najbolji izvor:**
- standard za chat frontend → **Open WebUI**
- multi-provider chat → **LibreChat**
- web UI nad agentom → **OpenHands**

**Sinteza (read-only dashboard, XSS-safe, nad App API):**
```go
type Dashboard struct{ app App } // read-only: prikazuje run/transcript/spend, NE izvršava naredbe
// XSS-safe: render transcripta kroz escaping (nikad raw HTML iz UNTRUSTED tool-outputa)
// isti App — dashboard NE smije imati vlastiti executor put
```
`full-UX` flag; AG-UI (runner-up) = standard za agent↔frontend event tok.

**Salvage:** NEXUSv2 `gateway/dashboard.py` (:354) → **Python→Go port**.

**Ugovor/RED:** 14.3 spec (XSS-safe). RED (naš): transcript s `<script>` iz tool-outputa → escaping
(ne izvršava se); dashboard ne može pokrenuti run bez App patha.

**Verifikacija:** Open WebUI (MIT), LibreChat (MIT), OpenHands (MIT) — stvarni.

**Pareto floor:** chat frontend (Open WebUI) + multi-provider (LibreChat) + web-nad-agentom (OpenHands)
+ XSS-safe read-only (naš) → ≥ svaki.

---

## 14.4 IDE integracija

**Tip:** ADAPTER (ACP standard)

**Aspekti → najbolji izvor:**
- VS Code standard → **Cline**
- IDE-first arhitektura → **Continue**
- standardizirana agent↔editor granica → **ACP** (wire protocol + capability negotiation + SDK-ovi)

**Sinteza (ACP adapter — agent↔editor granica):**
```go
type ACPAdapter struct{ app App }
// ACP standard: wire protocol + capability negotiation + službeni SDK-ovi (Rust/TS/Python/...)
// naš adapter izlaže App preko ACP — editor (VS Code/JetBrains) razgovara s nama preko standarda
// NEXUS export-skill interop (cli.py :64) = fallback za editore bez ACP
```
`full-UX` flag; Roo Code (runner-up) = Cline fork s modovima.

**Salvage:** NEXUSv2 `cli.py` (export-skill :64) → **Python→Go port** (partial interop).

**Ugovor/RED:** 14.4 spec (ACP conformance). RED (naš): ACP capability negotiation vraća floor iz
S0.3 (ne laže o sposobnostima); editor command ide kroz 6.9 lifecycle (isti put).

**Verifikacija:** Cline (Apache-2.0), Continue (Apache-2.0), ACP (standard, aktivni SDK-ovi) — stvarni.

**Pareto floor:** VS Code standard (Cline) + IDE-first (Continue) + standardizirana granica (ACP) +
isti-6.9-put (naš) → ≥ svaki.

---

## 14.5 Messaging/channel gateway (`channels` flag)

**Tip:** MEHANIZAM jezgra (auth/routing/delivery state-machine) + ADAPTER (plugin po kanalu)

**Aspekti → najbolji izvor:**
- 6 kanala, deny-default → **NEXUS gateway/**
- channel gateway + topic routing → **OpenClaw**
- channel connectors → **Rasa / Matterbridge**

**Sinteza (zajednički ugovor + durable-delivery + channels→extensions):**
```go
type ChannelAdapter interface { // plugin (Telegram/Slack/Discord/Matrix/Signal/WhatsApp/email)
    Send(ctx, msg OutboundMessage) (Receipt, error)
    Listen(ctx) (<-chan InboundMessage, error)
}
type ChannelGateway struct { // JEZGRA: auth/routing/delivery state-machine
    adapters map[string]ChannelAdapter
}
// zajednički ugovor (14.5):
//   identitet po kanalu (principal binding), deny-by-default allowliste
//   reply threading, delivery receipt, idempotent retry
//   udaljeni HITL: agent pita na mobitel (12.4 ApprovalChallenge), run čeka odgovor (7.3 durable-wait)
// dostava → durable-delivery (7.3 outbox) — gateway crash ne gubi poruku
// channels REQUIRES extensions (P1.6): adapter = plugin iza 6.8/11.5 gatea
```
Inbound channel poruka = UNTRUSTED input (P0.1); HITL odgovor = jezgreni single-use token (12.4).

**Salvage:** NEXUSv2 `gateway/` (6 kanala, deny-default) → **Python→Go port** (adapteri) +
jezgreni delivery state-machine je naš.

**Ugovor/RED:** **P1.6** `test_channels_contract_fails_closed` (A `channels` bez `extensions/S6.8` →
`INCOMPLETE_CAPABILITY_CLOSURE`; B dvostruki approval → `APPROVAL_REPLAY`) + **N9** (outbox: crash
nakon send a prije receipt → poruka se ne gubi ni ne duplicira).

**Verifikacija:** NEXUS gateway (interni), OpenClaw (source-available), Rasa (Apache-2.0),
Matterbridge (Apache-2.0) — stvarni.

**Pareto floor:** 6-kanala deny-default (NEXUS) + topic routing (OpenClaw) + connectors (Rasa) +
**delivery state-machine + durable-delivery + extensions-closure** (naš) → ≥ svaki.

---

## S14 cross-cutting napomene

1. **Jezgra = library, adapteri = tanki:** App je JEDINI executor put; CLI/TUI/desktop/web/SDK/
   channel su tanki adapteri — nema nereentrantnog puta (NEXUS audit dug je bio upravo to: SDK/
   serve su imali zaseban executor).
2. **14.2 SDK je reentrantan isti-put:** `SDK.Run → app.Run` — dva paralelna SDK poziva ne dijele
   mutable state; dry-run default (safe u test/CI).
3. **14.5 je najstroža S14 granica:** channels = plugin iza 6.8/11.5 (P1.6) + durable-delivery (N9)
   + jezgreni HITL token (12.4) — inbound = UNTRUSTED, adapter ne može sam nastaviti run.
4. **Single-binary (14.1):** `CGO_ENABLED=0` bubbletea TUI — jedan binarni na 3 OS-a; desktop
   (Wails/Fyne) je naknadna odluka, ne blokira jezgru.
5. **Honest gap:** 14.1/14.2/14.3/14.4 nemaju dedicirani Annex RED (gate = 14.x spec + P1.6/N9
   posredno); 14.5 ima P1.6+N9. Ako moderator želi simetriju: P0.18 "adapter-never-bypass-app-path".
6. **Verifikacija:** svi kandidati stvarni (Codex/Gemini/OpenHands/Dify/goose/OpenWebUI/LibreChat/
   Cline/Continue MIT/Apache-2.0; crush/OpenClaw source-available; ACP/AG-UI standardi); salvage
   `cli/tui/gateway/sdk/dashboard` interni (postoje — audit potvrdio), port je logika.
