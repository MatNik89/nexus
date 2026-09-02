# SECTION-MAP — plan od početka do kraja (dependency-order + status + design-index)

Cilj: plan jasan start-to-end, svaka sekcija jasna, PRIJE PRD-a. Jedino P0/P1/P2 scope-tiering
čeka PRD (#6). Status-legenda:
- **✅ DIZAJN-GOTOV** — postoji buildable Go dizajn (DESIGN-*.md), spreman za kod.
- **🔵 AUDITAN** — ima full-code-auditani svjetski izvor (MASTER-LANDSCAPE), treba Go-dizajn pass.
- **🟡 DIZAJN-TREBA** — MEHANIZAM sa skicom u planu, ali bez dediciranog dizajna/RED-a.
- **🟠 ODLUKA-PRD** — tanko / scope-ovisno / identitet; čeka PRD.
- **⬇ DESCOPE-v2** — svjesno odgođeno.

---

## 1) DEPENDENCY / BUILD-ORDER (DAG — od temelja prema vrhu)

```
FAZA K (kernel-temelj, ništa ne ovisi prije njih):
  S0 contracts/machine/negotiation/schema/closure   ✅
  S1 config/paths/proc/telemetry   (→S0)
  S6.0/6.1/6.2/6.9 PEP/permission/sandbox/lifecycle  (→S0,S1)   ✅6.2
  S7 retry/queue/crash-recovery  (→S0,S1)

FAZA L (izvršni put — coding-jezgra):
  S2 provider  (→S0,S7)
  S3 core-loop  (→S0,S2,S6,S7)   ✅3.1/3.3/3.4/3.5
  S4 tools/exec/edit/search  (→S3,S6)   ✅4.3
  S5 workspace/checkpoint  (→S1,S4)   ✅5.1/5.3
  S16.6 evidence-checker  (→S3)   ✅

FAZA M (kontekst/memorija/prompt):
  S8 context-management  (→S0,S3)   ✅8.3
  S9 memorija  (→S0,S7 spine)   ✅9.2/9.4
  S11 prompt/skills/roles  (→S0,S8)

FAZA N (orkestracija/observability):
  S12 multi-agent  (→S3,S5.2 worktree,S7)
  S15 observability  (→S0.3 journal)
  S10 retrieval  (→S9 spine, ADAPTERI)

FAZA O (sučelja/asistent-lice):
  S14 CLI/TUI/channels  (→cijela jezgra kao library)
  S13 multimodal  (ADAPTERI+subprocess)

FAZA P (distribucija/service):
  S17 plugin/update/packaging   S18 deploy/scaling
  S16.1-16.4 eval/benchmark/ratchet/credit
```

**Owner-invarijante (moraju držati kroz cijeli put):** policy=S6.0 · lifecycle=S6.9 ·
process-tree=S1.2 · retry/cancel=S7 · journal-write=S0.3 (jedini writer, sve ostalo projekcija).

---

## 2) FOLD ADDENDUMA A1-A10 U SEKCIJE (više ne vise kao dodaci)

| Addendum | Ide u | Kao |
|---|---|---|
| A1 OCR hardware-routing | **2.4 + 10.1 + 13.5** | ADAPTER pipeline (Unlimited-OCR GPU / OCRmyPDF CPU → FTS) |
| A2 model-scout | **2.4 + 3.6** | JEZGRA scout (informira, ne auto-swap) |
| A3.1 identitet | **→ PRD** | "jedna jezgra, dva profila" — ODLUKA-PRD |
| A3.2 self-diagnostic→Claude | **15.5 + 17.4** | RepairBundle (opt-in, ne autonoman) |
| A4-G1 computer-use | **NOVA 4.8** (`computer-use` flag) | InputInject KEY/MOUSE/TYPE=RED-tier |
| A4-G2 PersonalProfile | **NOVA 9.5** (`profiles`) | deny-default izolacija memory/secrets/channels |
| A4-G3 voice-runtime | **13.3** (proširuje) | barge-in/wake/consent ugovor |
| A4-G4 ObligationStore | **NOVA 9.6** (asistent) | cilj s done-def, MarkDone samo uz Evidence |
| A4-G5 control-plane | **3.6 + 14.5** | asistentsko lice nad automation |
| A4 G6-G9 | dopune 8.4/9.3/2.2/14.5 | cache-lineage/learn-control/AccountFleet/channel-caps |
| A5-A8 audit-nalazi | **već foldani** (MASTER-LANDSCAPE) | 4 diferencijatora + reference |
| A9 fold-gapovi | **12.2/7.2/17.1** | superstep/ingress-queue/Cordis |
| A10 GAPFIX | **12.x flows/boards + 18.x fleet+pairing** | NOVE `multi-agent`/`service` podsekcije |

**Nove podsekcije iz addenduma — UBAČENE u plan (commit 257e01b), sve `Scope: ODLUKA-PRD`:**
4.8 computer-use, 6.10 exec-auto-reviewer, 9.5 PersonalProfile, 9.6 ObligationStore,
12.7 flows, 12.8 boards, 18.5 fleet+device-pairing. Numeracija kontiguirana u svih 19 sekcija.

---

## 3) STATUS-LEDGER (svih 81 + inline pod-stavke)

**S0** ✅ 0.1 0.2 0.3 0.4 0.5 — svi DIZAJN-GOTOV (codex) · P0.5 napisan
**S1** 🟡 1.1 config · 1.3 telemetry · ✅ 1.2 proc (effect-path owner, kilo)
**S2** 🟡 2.1 auth · 2.2 fallback(+AccountFleet) · ✅ 2.3 structured(re-ask+grant, kilo) · 2.4 local(+A1/A2) · 2.5 cost · 🟠 2.6 learned-router (codex: descope v1, deterministički prvo)
**S3** ✅ 3.1 loop · 3.3 cancel · 3.4 stuck · 3.5 TIA — svi effect-path/agy · 🟡 3.2 streaming · 3.6 trigger-plane
**S4** ✅ 4.3 edit+symedit (agy+claude) · 🔵 4.4 search · 🟡 4.1 registry · 4.2 exec(structured v1, codex) · 4.6 MCP · 4.7 tool-budget · 🟠 4.5 web(ADAPTER,subprocess) · **4.8 computer-use (NOVA)**
**S5** ✅ 5.1 checkpoint · 5.3 atomic/attest · 🟡 5.2 worktree(izolacija-granica, codex)
**S6** ✅ 6.0 PEP · 6.2 sandbox · 6.9 lifecycle — dizajn-gotovi · 🟡 6.1 perm(structured ExecRequest, codex) · 6.3 egress(resolver+pin) · 6.4 secrets(broker) · 6.5 injection · 🟠 6.6 authn(`service`) · 6.8 supply-chain · inline 6.7 governance(Presidio=Py→sidecar/Go-adapter) · **6.10 exec-auto-reviewer (NOVA)**
**S7** ✅ 7.1 taksonomija(effect-path) · 🔵 7.2 queue/lease/fencing · 🟡 7.3 crash-recovery+durable-delivery
**S8** ✅ 8.3 archmap(kilo) · 🔵 8.2 compaction · 🟡 8.1 window · 8.4 cache · 8.5 pruning
**S9** ✅ 9.2 memory-store(kilo) · 9.4 Decay+Audn(kilo, P0) · ⬇ 9.4 Dream/MemGit/MemAssoc(v2) · 🟡 9.1 sesije · 9.3 learn · **9.5 PersonalProfile · 9.6 ObligationStore (NOVE)**
**S10** 🟠 10.1 ingest(ADAPTER,ne-Go) · 10.4 GraphRAG · 🟡 10.6 retrieval-auth · 10.7 quality-gate · inline 10.2 chunk/10.3 hibrid/10.5 CAG(UNCLEAR)
**S11** 🟡 11.1 assembly · 11.5 skill-lifecycle · 11.6 role-manifest · 🔵 11.2 Reversa-kao-data · 🟠 11.3 projektne-instr · inline 11.4 prompt-opt
**S12** 🔵 12.1 subagenti · 12.2 DAG(+superstep) · 12.5 A2A · 🟡 12.4 HITL · 12.6 council · **12.7 flows · 12.8 boards (NOVE)**
**S13** 🟠 13.1 vision · 13.2 image-gen · 13.3 voice(+A4-G3) · 13.5 doc-mutacija — svi ADAPTER+subprocess/pure-Go(pdfcpu/chromedp)
**S14** 🟡 14.1 CLI/TUI(bubbletea) · 14.5 channels · 🟠 14.4 IDE/ACP(vlastiti JSON-RPC, nema Go SDK) · inline 14.2 SDK/14.3 web-UI
**S15** ✅ 15.3 audit-chain(crypto, claude) · 🟡 15.1 tracing · 15.5 health(+A3.2 self-diag) · inline 15.2 cost-dash/15.4 SLO
**S16** ✅ 16.6 checker(agy) · 🟡 16.1 benchmark+app-contract · 16.4 credit-ledger · 🟠 16.5 trajectory-export · inline 16.2 ratchet/16.3 red-team(strix)
**S17** 🔵 17.1 plugin(Cordis) · 🟡 17.4 self-diag-bridge(+A3.2) · inline 17.2 update(TUF)/17.3 packaging
**S18** 🔵 18.1 worker/queue(S7-lease) · **18.5 fleet+device-pairing (NOVA)** · inline 18.2 tenant-gw/18.3 canary/18.4 backup(UNCLEAR dok S0.4 backend)

**Zbroj:** ✅ 22 dizajn-gotovih · 🔵 9 auditanih(treba Go-pass) · 🟡 ~30 dizajn-treba · 🟠 ~12 odluka-PRD · ⬇ 3 descope · + 7 NOVIH iz addenduma.

---

## 4) DESIGN-INDEX (dizajn-dok → podsekcije)

| Dok | Pokriva |
|---|---|
| `DESIGN-S0-sandbox-codex.md` | 0.1 0.2 0.3 0.4 0.5 (+P0.5) · 6.2 (+capability-matrica, Win/mac probe) |
| `DESIGN-memory-effectpath-kilo.md` | 9.4(descope) 9.2 8.3 · effect-path 3.1/3.3/3.4/6.0/6.9/1.2/7.1 · 2.3 |
| `DESIGN-checker-tia-edit-agy.md` | 3.1/16.6 checker · 3.5 TIA · 4.3/5.1/5.3 edit |
| `DESIGN-symedit-crypto-claude.md` | 4.3 symedit · 15.3/6.8 crypto-attest |
| `PLAN-HOLES-CONSOLIDATED.md` | 8 konvergentnih rupa + 6 must-resolve |
| `DESIGN-STATUS.md` | status 6 must-resolve |

---

## ŠTO OSTAJE ZA PRD (ne može prije)
- **Identitet** (A3.1 jedna-jezgra/dva-profila) → određuje što je P0.
- **P0/P1/P2 scope-tier** → koji flagovi ulaze u prvi build; sve 🟠 i većina NOVIH ovise o tome.
- Sve ostalo (dependency-order, fold, status, design-linkovi) je JASNO i PRD-neovisno.
