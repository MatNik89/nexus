# ARCHITECTURE-ESSENTIALS — 15 kritičnih odluka (cheat-sheet)

Destilat zaključane arhitekture (HARNESS-PLAN 108 podsekcija + DESIGN-* + DESIGN-FIXES-r2,
9 rundi red-teama → 0 blokera). Ovo je dnevna referenca pri kodiranju; puni izvor uvijek pobjeđuje.
Format: **ODLUKA → zašto → gdje piše**.

---

## E1 — Go, `CGO_ENABLED=0`, single-binary
Jedna jezgra = jedan statički binary. SQLite spine = `modernc.org/sqlite` (pure-Go, WAL).
Per-OS kod isključivo build-tagged (`proc_*.go`, `sandbox_*.go`). Ne-Go svjetski alati NIKAD
in-process — svjestan subprocess-sidecar ili pure-Go zamjena ili descope (C4).
→ HARNESS-PLAN header, S1.2, A9; PLAN-HOLES C4.

## E2 — Jedna jezgra, dva profila; asistent PRVO
`AssistantProfile` = svakodnevno lice (razgovor/pamćenje/obveze/kanali); `CodingProfile` =
najjača grana (TIA/evidence-gate/symedit), ne svrha. Isti kernel S0-S9, isti TurnLoop —
razlika je samo gating po profilu. Linux-prvo.
→ A3.1, PLAN-HOLES C7, PRD §3.

## E3 — EventJournal = JEDINI trajni write-owner (P0.3)
`EventJournal.Append` jedini piše trajno stanje. State/log/trace/transcript/audit/metrics =
PROJEKCIJE (fold), nikad paralelni pisci; nijedna projekcija ne alocira event_id/sequence.
State se REKONSTRUIRA fold-om eventa (ne mutable polje); checkpoint = journal offset.
Redakcija secreta PRIJE journala. ObligationStore piše KROZ journal, ne izravni sqlite.
Iznimka self-DoS: debug-telemetry smije best-effort drop; state/security/audit klase NIKAD.
→ S0 sinteza, S1.3, S15, DESIGN-FIXES vlasništvo-fix; codex#21.

## E4 — Owner-invarijante (jedan owner po brizi, bez iznimke)
- **Policy ALLOW/ASK/DENY = S6.0 PEP** (`Decide`, total switch, default-deny; ASK≠ALLOW).
- **Lifecycle ORDER = S6.9** (before/after/on_error grana) — 6.9 NIKAD ne odlučuje policy.
- **Retry/deadline/cancel/failover = S7 JEDINI** — svaki fizički pokušaj nosi `AttemptGrant`;
  2.2/2.3-re-ask/3.3/3.4/fleet/AccountFleet samo klasificiraju ili predlažu.
- **Process-identity = S1.2** (PID+start-token; killpg/Job-Object) — konzumira ga S6.2.Launch.
- **Queue lease/fencing = S7.2** (CAS claim, fencing_token++, stale→`STALE_FENCING_TOKEN`).
→ SECTION-MAP owner-invarijante; S7; DESIGN-FIXES.

## E5 — Jedan effect-path, sandbox U tipu
`S3.Loop → sealed ToolSpec.ExecutionKind → S6.0.Decide → S6.9.Before →
{InProcessExecutor | SandboxedProcessExecutor(S6.2.Launch + S1.2)} → S6.9.After/OnError → S7`.
Grananje po zapečaćenom `ExecutionKind`; nepoznat kind → REJECT, ne inproc. Read-only shell
je i dalje proces → sandbox. In-process alat NIKAD ne spawna. Konkretni executor-tipovi u
`EffectPath` structu — nesandboxiran executor se ne može injektirati.
→ DESIGN-FIXES-r2 K1/K2 (kanonski kod).

## E6 — Effect-taxonomy umjesto nemogućeg cancela
"Cancel usred toola → nikakav side-effect" NE POSTOJI. Umjesto toga `CommitReceipt` +
faze `BEFORE_COMMIT / AFTER_COMMIT / UNKNOWN`; svaki EFFECTFUL bez valjanog receipta →
UNKNOWN → RECONCILING (nikad slijepi retry, nikad ravno READY). Ireverzibilni efekti kroz
P1.4 draft→approve→commit→verify→compensate; approval veže exact-intent hash.
→ DESIGN-FIXES classifyEffectPhase; S7.1/7.2; codex duboki nalaz.

## E7 — Sandbox Linux: helper bez unsandboxed prozora (NAJVEĆI RIZIK)
Self-reexec helper: ABI probe → `no_new_privs` → Landlock ruleset → per-arch seccomp-BPF →
nepovratni execve; FD/env sanitizacija; TOCTOU fail-closed. **Win/macOS u v1 = `UNAVAILABLE`
+ high-risk exec BLOKIRAN — nikakav tihi weaker-fallback.** Sandbox se NE koristi kao granica
dok hostile conformance suite ne prođe na stvarnom kernelu (probe-neverificiran = prvi
implement-zadatak P0). bwrap = opcionalni vanjski adapter, ne garancija.
→ DESIGN-S0-sandbox-codex; PLAN-HOLES C2; PRD §6.6/§7.

## E8 — Fail-closed je zadani odgovor na nepoznato
Enum default-reject grana (nema Go sum-types); nepoznata schema-verzija → QUARANTINE (ne
downcast, ne tihi odbij); capability `Resolve` nepoznatog → error; config ne smije PROŠIRITI
kernel floor (`ValidateBounds`); route ispod capability floora → error; SSRF metadata-IP u
svakom kodiranju → block prije diala; canary u izlazu → blok CIJELE isporuke.
→ S0.1/0.3/0.4, S1.1, S2.6, S6.3, S6.5.

## E9 — Ručni Go tipovi = izvor istine; nema `map[string]any` u kernelu
JSON-Schema/OpenAPI/protobuf su izvedene projekcije. Opaque payload = `json.RawMessage` +
zatvorena validacija discriminatora. `Content` XOR `ContentRef` u konstruktoru.
→ S0 kanonska sinteza.

## E10 — Trust/lineage monotoni; untrusted NIKAD ne postaje instrukcija
`trust_class(sažetak)=MAX(izvori)`, `sensitivity=MAX`, `lineage=union` — kompakcija ne smije
oprati untrusted (P0.3 provenance-laundering RED). Prompt-assembler trust-fencing; svaki
search-hit datamarked-untrusted; remote A2A peer = UNTRUSTED (card nije autorizacija).
→ S8.2, S11.1, S12.5, S4.4.

## E11 — Evidence-graded completion: checker ≠ worker
`Checker.Grade` ocjenjuje ARTEFAKTE (git-diff + realan exit-code + deterministički signali),
NIKAD prozu workera. Generic `AcceptanceContract`+`EvidenceBundle` (coding=diff+exit;
asistent=delivery-receipt/calendar/grounding). `ObligationStore.MarkDone` SAMO uz Evidence.
Kredit (16.4) samo iz verificiranog ishoda, nikad self-report. Ovo je USP #1 — ne razvodniti.
→ S3.1, S16.6, S9.6, DESIGN-checker-tia-edit-agy.

## E12 — Provider = interface s 4 auth-moda; adapteri ne retry-aju
`Provider{Chat/Stream/Capabilities}` + `AuthMode`: APIKey (default) · OAuth (sankcioniran) ·
CLIAgent (subprocess `codex`/`claude` s `--tools ""` — NEXUS drži petlju) · SubscriptionOAuth
(NIKAD default, fail-closed bez `acknowledged_tos`). Petlja ne zna mod. Structured output:
validate→re-ask→salvage (re-ask nosi AttemptGrant), nikad tihi prihvat nevalidnog.
→ S2.1/2.2/2.3.

## E13 — Memorija: spine + FORGET≠PURGE + profil-izolacija
SQLite-WAL spine; P0 samo Decay(FSRS-lite, mijenja RANG ne postojanje)+Audn (Dream/MemGit/
MemAssoc = v2 descope). **MEMORY_FORGET (reverzibilno, zabrana recalla) ≠ DATA_PURGE
(ireverzibilno, S6.7, briše i tombstone)** — USP #4. `PersonalProfile` = deny-default
izolacija memory/secrets/channels/cache: write u `work` → query u `private` = 0 hitova.
Write-approval DEFAULT ON.
→ S9.2/9.4/9.5, P2.1, GAPFIX-kilo.

## E14 — Kanali: durable-delivery + udaljeni-HITL
Gateway jezgra (identity, deny-default allowlist, threading, receipt, idempotent-retry);
adapteri tanki (Telegram prvi). `run_done ≠ result_delivered` — dva trajna stanja,
transactional outbox: gateway crash ne gubi odgovor niti ponavlja side-effect. Udaljeni-HITL:
agent pita na mobitel, run čeka; approval token = exact-intent + expiring + single-use
(replay → `APPROVAL_REPLAY`).
→ S14.5, S7.3 (N9), S12.4, P1.6.

## E15 — Kripto i potpisi: nikad `hash.Sum(key)`
Keyed-MAC = `hmac.New(sha256.New, key)` ili `ed25519.Sign`; kanonska serijalizacija;
constant-time compare; ključ nikad u artefaktu. Vrijedi za SVE potpisano: ExecutionReceipt,
shadow-checkpoint, audit hash-lanac (15.3 + vanjski anchor), skill-lock (P1.1 hash-on-load).
→ DESIGN-symedit-crypto-claude (D2 fix).

---

## P0 scope-rez (podsjetnik — što NE graditi sad)
P0 = 6 sposobnosti iz PRD §4 (razgovor · spine-pamćenje · obveze · Telegram · profili ·
Linux sandbox). **Izbačeno iz v1:** learned-router, council/flows/boards, GraphRAG/CAG,
fleet/pairing, video, computer-use, desktop-GUI, voice-WebRTC, K8s. Coding-USP trojka
(TIA/symedit/evidence-gate puni) = P1. → PRD §4-5, PLAN-HOLES C6.

## Otvoreno prije P0 koda (iz DESIGN-STATUS)
1. **Sandbox live-probe na stvarnom Linux kernelu** (hostile conformance suite) — prvi zadatak.
2. Annex A P0.x ugovori koji se citiraju moraju POSTOJATI prije citiranja kao gate (C3).
