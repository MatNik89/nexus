# SECTION-MAP — plan od početka do kraja (dependency-order + status + design-index)

Cilj: plan jasan start-to-end, svaka sekcija jasna, PRIJE PRD-a. Jedino P0/P1/P2 scope-tiering
čeka PRD (#6). Status-legenda:
- **✅ DIZAJN-GOTOV** — postoji buildable Go dizajn (DESIGN-*.md), spreman za kod.
- **🔵 AUDITAN** — ima full-code-auditani svjetski izvor (MASTER-LANDSCAPE), treba Go-dizajn pass.
- **🟡 DIZAJN-TREBA** — MEHANIZAM sa skicom u planu, ali bez dediciranog dizajna/RED-a.
- **🟠 ODLUKA-PRD** — tanko / scope-ovisno / identitet; čeka PRD.
- **⬇ DESCOPE-v2** — svjesno odgođeno.

---

## 1) DEPENDENCY / BUILD-ORDER (DAG — po ID-u, ciklusi razriješeni; REVIEW2 K4)

Ključni fix: gdje je bila cirkularnost, jezgreni MINIMALNI ugovor ide rano, PUNA značajka kasnije.

```
FAZA K0 (primitivi — ništa ne ovisi prije):
  S0.1-0.5 contracts/machine/negotiation/schema/closure   ✅
  EventJournal paket (P0.3 write-owner — NIJE S0.3; S0.3=capability-negotiation)   ✅
  S1.1 config · S1.2 paths + PROCESS-IDENTITY primitiv (PID+start-token)  (→S0)
  S1.3 telemetry (projekcija journala)
  S8.1-min (ContextBudget.Measure+HardLimit) · S11.1-min (Assembler.Base — bez skills/archmap)
  S16.6-det (Checker deterministic: coding.diff_exit+state_invariant) — izdvojeni MIN ugovori (fix V-K4)

FAZA K1 (sigurnost+pouzdanost+workspace, →K0):
  S6.0 PEP · S6.1 perm · S6.2 sandbox · S6.9 lifecycle   ✅6.2
  S7 retry/queue/crash  (KONZUMIRA S1.2 process-identity; S1.2 NE ovisi o S7 — fix C05)
  S5 workspace/checkpoint (5.1/5.2/5.3)  (→S1)   ✅5.1/5.3   ← PRIJE S4 (fix DAG-01)
  6.3 egress · 6.4 secrets · 6.5 injection · 6.6 authn(`service`) · 6.7 governance · 6.8 supply-chain (→K1)

FAZA L (izvršni put, →K):
  S2 provider (DETERMINISTIČKI router; learned→P)  (→S6,S7)
  S3 core-loop  (→S2,S5,S6,S7,S8.1-min,S11.1-min,S16.6-det)   ✅3.1/3.3/3.4/3.5
  S4 tools/exec/edit/search  (→S3,S5,S6.2)   ✅4.3   ← search staje na grep/AST/LSP (archmap→M)

FAZA M (kontekst/memorija/prompt PUNI, →L):
  S8 puni (8.1-puni/8.2 compaction/8.3 archmap/8.4 cache/8.5 pruning) → 4.4 archmap-integr.   ✅8.3
  S9 (9.1 sesije/9.2 store/9.3 learn/9.4 Decay+Audn — Decay+Audn=P1 per HARDQ B8, P0=explicit facts; 9.5/9.6 SAMO persistence+namespace, binding→N/O)   ✅9.2/9.4
  S11 puni (11.2/11.4/11.5/11.6)

FAZA N (orkestracija/observability, →M):
  S12 (12.1-6 + 12.7 flows + 12.8 boards)  (→S3,S5.2,S7)
  S15 observability (→EventJournal) · S16.6-council-integr. · 6.10 exec-reviewer (→15.3/15.5)
  S10 retrieval (→S9, ADAPTERI) · 9.5 channel/ACL-binding (→10.6)

FAZA O (sučelja/asistent-lice, →N):
  S14 CLI/TUI puni + 14.5 channels-stdio-built-in (plugin-channels→P)
  S13 multimodal (adapteri+subprocess) + 4.8 computer-use

FAZA P (distribucija/service, →O):
  S17 plugin/update/packaging (extensions engine) · plugin-channels (14.5-external)
  S18 deploy/scaling + 18.5 fleet + 18.6 pairing (ID-ovi iz fleet/ids — K3)
  S16.1-16.4 eval/benchmark/ratchet/credit · S2.6 learned-router (reward←16.4)
  9.6 app-contract done-eval (←16.1)
```

**Owner-invarijante (drže kroz cijeli put):** policy=**S6.0** · lifecycle-order=**S6.9** ·
sandbox=**S6.2** (svaki subprocess) · process-tree=**S1.2** (unutar S6.2.Launch) · retry/cancel=**S7** ·
journal-write=**P0.3/EventJournal paket** (jedini writer; NE S0.3). Effect-path fix: DESIGN-FIXES-r2 K1/K2.

**P0 -min rezovi (HARDQ A2, jednoglasno, 2026-09-03):** K1 rubovi S7/S5 za P0 walking-skeleton
zadovoljavaju se IMENOVANIM -min ugovorima (isti mehanizam kao S8.1-min/S11.1-min/S16.6-det):
`s7-min` = AttemptGrant + cancel + no-retry (retryable→FAILED_TERMINAL) · `s5-min` = AtomicWriter ·
`3.6-min` = persisted schedule + wake catch-up (uvučen u P0) · `7.3-min` = inbox/outbox +
occurrence-idempotency (uvučen u P0). PUNI engine-i (S7 taxonomija/budgeti/fencing, S5 shadow-git/
worktree) ostaju u svojim kasnijim fazama. Redoslijed isporuke: HARNESS-SPEC vertikalni sliceovi;
build-order detalj → HARDQ-CONSOLIDATED A2 → tasks-P0.md.

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
| A10 GAPFIX | **12.7/12.8 flows/boards + 18.5/18.6 fleet/pairing** | NOVE `multi-agent`/`service` podsekcije |

**Nove podsekcije iz addenduma — UBAČENE u plan (commit 257e01b), sve `Scope: ODLUKA-PRD`:**
4.8 computer-use, 6.10 exec-auto-reviewer, 9.5 PersonalProfile, 9.6 ObligationStore,
12.7 flows, 12.8 boards, 18.5 fleet, 18.6 pairing (8 novih — 18.6 vraćen; fix D6/R2-16). Kanonski broj: 108 podsekcija (100 spec + 8 addendum).

---

## 3) STATUS-LEDGER — kanonski broj: **108 podsekcija** (100 spec v0.9 + 8 novih iz addenduma)

Nove nose ✅+🟠 = DIZAJN-GOTOV (GAPFIX) + scope-flag ODLUKA-PRD (NE "tanko"; fix D4/R2-14).
Inline pod-stavke sad nose status (fix D3/SEC-03).

**S0** ✅ 0.1 0.2 0.3 0.4 0.5 (codex) · P0.5-0.13 → u Annexu A (dodani; G1 zatvoren)
**S1** 🟡 1.1 config · 1.3 telemetry · ✅ 1.2 proc+process-identity
**S2** 🟡 2.1 auth · 2.2 fallback(+AccountFleet classify-only) · ✅ 2.3 structured · 🟡 2.4 local(+A1/A2) · 2.5 cost · 🟠 2.6 learned-router(→P)
**S3** ✅ 3.1 loop · 3.3 cancel · 3.4 stuck · 3.5 TIA · 🟡 3.2 streaming · 3.6 trigger-plane
**S4** ✅ 4.3 edit+symedit · 🔵 4.4 search · 🟡 4.1 registry · 4.2 exec · 4.6 MCP · 4.7 tool-budget · 🟠 4.5 web · **✅🟠 4.8 computer-use** (GAPFIX-kilo)
**S5** ✅ 5.1 checkpoint · 5.3 atomic/attest · 🟡 5.2 worktree
**S6** ✅ 6.0 PEP · 6.2 sandbox · 6.9 lifecycle · 🟡 6.1 perm · 6.3 egress · 6.4 secrets · 6.5 injection · 🟠 6.6 authn · 6.8 supply-chain · 🟡 6.7 governance(sidecar) · **✅🟠 6.10 exec-reviewer** (GAPFIX-kilo)
**S7** ✅ 7.1 taksonomija · 🔵 7.2 queue/lease/fencing · 🟡 7.3 crash+durable-delivery
**S8** ✅ 8.3 archmap · 🔵 8.2 compaction · 🟡 8.1 window · 8.4 cache · 8.5 pruning
**S9** ✅ 9.2 store · 9.4 Decay+Audn(P1 — HARDQ B8 2026-09-03; P0=explicit facts, no decay) · ⬇ 9.4 Dream/MemGit/MemAssoc(v2) · 🟡 9.1 sesije · 9.3 learn · **✅🟠 9.5 PersonalProfile · ✅🟠 9.6 ObligationStore** (GAPFIX-kilo; 9.6 write→journal P0.3)
**S10** 🟠 10.1 ingest · 10.4 GraphRAG · 🟡 10.6 retrieval-auth · 10.7 quality-gate · 🟡 10.2 chunk · 10.3 hibrid(sqlite-vec) · 🟠 10.5 CAG(reserved→degradira u RAG)
**S11** 🟡 11.1 assembly · 11.5 skill-lifecycle · 11.6 role-manifest · 🔵 11.2 Reversa · 🟠 11.3 projektne-instr · 🟡 11.4 prompt-opt
**S12** 🔵 12.1 subagenti · 12.2 DAG(+superstep) · 12.5 A2A · 🟡 12.3 routing(potrošač 2.6) · 12.4 HITL · 12.6 council · **✅🟠 12.7 flows · ✅🟠 12.8 boards** (GAPFIX-codex)
**S13** 🟠 13.1 vision · 13.2 image-gen · 13.3 voice(+A4-G3) · 13.4 video(subprocess ffmpeg) · 13.5 doc-mutacija(pure-Go pdfcpu) — svi ADAPTER+subprocess
**S14** 🟡 14.1 CLI/TUI · 14.5 channels(stdio-built-in;plugin→P) · 🟠 14.4 IDE/ACP · 🟡 14.2 SDK · 🟠 14.3 web-UI
**S15** ✅ 15.3 audit-chain · 🟡 15.1 tracing · 15.5 health(swallow-owner)+A3.2 · 🟡 15.2 cost-dash · 15.4 SLO
**S16** ✅ 16.6 checker · 🟡 16.1 benchmark+app-contract · 16.4 credit-ledger · 🟠 16.5 trajectory-export · 🟡 16.2 ratchet · 16.3 red-team(strix)
**S17** 🔵 17.1 plugin(Cordis) · 🟡 17.4 self-diag-bridge+A3.2 · 🟡 17.2 update(TUF) · 17.3 packaging
**S18** 🔵 18.1 worker/queue · **✅🟠 18.5 fleet · ✅🟠 18.6 pairing** (GAPFIX-codex; fleet/ids anti-cikl K3) · 🟡 18.2 tenant-gw · 18.3 canary · 18.4 backup(Litestream sada; premisa "S0.4" bila kriva — backend već SQLite-WAL)

**Zbroj (108):** ✅ ~24 dizajn-gotovih (uklj. 8 novih GAPFIX) · 🔵 9 auditanih · 🟡 ~40 dizajn-treba · 🟠 ~15 odluka-PRD · ⬇ 3 descope.

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
