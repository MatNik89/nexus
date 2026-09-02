# PLAN-HOLES — konsolidirano (4 neovisna nalaza: claude + rev-codex + rev-kilo + rev-agy)

Kontekst: greenfield Go `CGO_ENABLED=0`, NULA salvagea. Izvori: `REVIEW-{claude,codex,kilo,agy}.md`.
Rangiranje po KONVERGENCIJI (koliko neovisnih recenzenata isto našlo).

---

## KONVERGENTNE RUPE (2+ recenzenta — visoka pouzdanost)

### C1 — Salvage-iluzija u cijelom planu (SVA 4)
Svaka od 82 podsekcija ima `Salvage: core/X.py` + "GORTEX Go REUSE 1:1" + "port"/"reuse" kao dokaz
da je buildable i Pareto-complete. Sve mrtvo. Plan sustavno OBMANJUJE procjenu truda i buduće agente.
Brojke "60 modula"/"~100 podsekcija" netočne (82). **Treba:** mehanički obrisati sve salvage/GORTEX/
NEXUSv2/port/reuse retke; svaka podsekcija reklasificirana `RESOLVED` (ima full-code-auditani izvor
ILI potpun Go ugovor) ili `UNRESOLVED`. "ZAOKRUŽEN" statusi su lažni dok se to ne napravi.

### C2 — S6 sigurnost = #1 BLOKER; bila "reuse 1:1", sad OD NULE (claude+codex+agy)
Cijela S6 počivala na GORTEX membrani koja ne postoji. codex daje najdublju tehničku sliku:
- **Linux nije "3 syscalla"** — treba self-reexec helper (ABI probe→no_new_privs→Landlock ruleset→
  per-arhitektura seccomp-BPF→nepovratni execve), BEZ unsandboxed pre-exec prozora. FD/env sanitizacija.
- **Windows Job Object NIJE FS/net sandbox** — treba AppContainer/restricted-token; novi sandbox API je
  eksplicitno *experimental* (ne smije biti jedini production backend).
- **macOS nema stabilan javni API** za dinamički Seatbelt profil nad proizvoljnim CLI-jem.
**Treba prije S4/S6 koda:** `SandboxBackend.Probe/Compile/Launch/Attest` + capability matrica
(FS_RO/FS_RW/NET_DENY/SYSCALL/PROC_TREE) + hostile conformance suite + PRODUKTNA odluka: Linux full;
Win/macOS ili dokazan backend ili `UNAVAILABLE`+blokiran high-risk exec. NIKAKAV tihi "weaker sandbox"
fallback. `bwrap` = opcionalni vanjski adapter, ne single-binary garancija. Realno: mjeseci, ne tjedni.

### C3 — Annex A RED-gate je polu-fikcija (claude+kilo)
Annex A ima 17 STVARNIH ugovora (P0.1-4, P1.1-6, P2.1-7) s RED imenima. ALI:
- **P0.5/P0.6/P0.7/P0.8/P0.9/P0.11/P0.13 = NE POSTOJE** — plan ih 7× obećava ("dodati P0.5",
  "kandidat za matrix-prep") i citira kao jedini gate za honest-gap podsekcije.
- **codex#2/#3/#19** citirani kao "RED Tier-3" su BACKLOG, NISU u Annex A.
- "honest gap: bez dediciranog Annex RED" pojavljuje se 6× (S8.1/8.3/8.5, S9.1/9.3/9.4).
**Treba:** napisati nedostajuće P0.x ugovore PRIJE nego se citiraju kao gate; uskladiti gate-mapu
(stvarni Annex vs "kandidat" vs "bez gatea"); ne citirati nepostojeće testove.

### C4 — I sami svjetski kandidati su NE-GO (agy+codex)
U `CGO_ENABLED=0` single-binaryju ne mogu in-process: browser-use (Py), pipecat/LiveKit (Py/WebRTC),
Remotion (Node), WhisperX (PyTorch), pypdf/OCRmyPDF/pyHanko (Py), Presidio (Py servis), llmfit (Rust),
ACP (nema Go SDK), bwrap (nije single-binary). **"Merge" NIJE besplatan.** **Treba:** po komponenti
odluka — pure-Go zamjena (pdfcpu/chromedp/x-image/rsc.io-pdf) ILI svjestan subprocess-sidecar
(egress/sandbox posljedice) ILI descope. Nijedan "adapter" nije trivijalan wrapper.

### C5 — 5+ mehanizama bili SAMO salvage; sad greenfield bez dizajna (claude+codex+kilo)
Upravo NAŠI "diferencijatori" nemaju sad izvor NI dizajn:
- **Evidence-checker (3.1/16.6):** `Checker.Grade` nema input-shemu/evidence-provenance; git-diff+exit
  je SAMO coding-strategija — necoding zadatak (poruka/kalendar/research) nema completion-def. Treba
  generic `AcceptanceContract`+`EvidenceBundle`; worker≠checker principal.
- **TIA (3.5):** prava greenfield rupa — nema coverage-ingest/diff→symbol/dynamic-tests/stale-graph
  dizajna. Ne smije biti completion-gate dok paired full-suite ne dokaže 0 promašenih regresija.
- **edit-ladder+AST-symedit (4.3):** Aider=izvor za formate, ali ladder/symedit nemaju Go mehanizam
  (offset/encoding/newline/rename/LSP-transakcije). v1 symedit ograničiti na imenovane jezike.
- **shadow-git (5.1):** nespecificiran scope (untracked/symlink/submodule/xattr/ACL/konkurentni write);
  git ne čuva sve FS-metapodatke. Snapshot prije svakog FS-efekta (ne svakog side-effecta).
- **memorija S9.4 (kilo R1, NAJVEĆA):** 6 algoritama (Dream/Decay/Audn/MemGit/MemAssoc/temporal) =
  ~2-3 tjedna od nule; nijedan svjetski kandidat nema puni set. **Descope preporuka:** Decay(FSRS)+
  Audn sada, Dream/MemGit/MemAssoc v2.
- **archmap S8.3 (kilo R2):** JEDINI preživjeli USP, a JEDNA rečenica dizajna. Treba pun Go-spec
  (graf-nad-modulima+query+invalidation) prije koda.
- **stuck-detection (3.4):** nema algoritma (arg-hash/progress-signal/pragovi/reset/false-positive).
  Izvor = Cline/Codex/Roo/Kilo (auditani, A8 priznaje).

### C6 — Scope MORA biti brutalno rezan za P0 (claude+codex+agy)
82 podsekcije+18 flagova+10 addenduma za greenfield solo+Claude = paraliza. Konsenzus reza iz v1:
S2.6 learned-bandit, S2.1 subscription-OAuth (ToS/ban rizik), S4.4 callgraph/archmap-sloj, S5.3
PROV-O, S6.5 guardian-LLM-kao-security, desktop-GUI (Wails/CGO), voice-WebRTC, video, computer-use,
fleet+device-pairing, K8s/Argo. Odbaciti tvrdnju "isti sandbox na 3 OS-a". agy P0-jezgra prijedlog:
S0-S9 + S14.1 TUI + S16.6 checker + S17.1 DI.

### C7 — Proturječje identiteta (claude+agy)
Vrh plana "coding PRVORAZREDNI cilj" vs A3.1 "asistent PRVO, coding grana". agy rješenje (čisto):
**"jedna jezgra, dva profila"** — zajednički S0-S9/S17.1 kernel; `AssistantProfile` (chat/obligation/
profil) i `CodingProfile` (evidence-gate/shadow-git/TIA) su behavior-profili nad ISTIM TurnLoop-om
(gating ovisi o profilu). Korisnik presuđuje u PRD-u.

### C8 — Tanke jednorečnične podsekcije (claude+agy)
S11.3, S13.1, S13.5, S14.4, S16.5 (+ S13.3/13.4 pokriveni A4-G3 addendumom) — nabroje kandidate,
nula ugovora/dizajna. Ili spec ili descope.

---

## DUBOKE POJEDINAČNE (rev-codex, jedan izvor ali tehnički točne)

- **6.0/6.9 nema kanonskog PEP/approval ugovora** — TOCTOU-ranjiv; treba typed `EffectRequest`/
  `PolicyDecision`/`ExecutionTicket` (hash veže exact-intent, re-check prije efekta); PEP = obvezni
  konstruktor-dependency svakog sinka, ne opcionalni middleware.
- **6.1 shlex/regex NIJE sigurnosna granica** — ne pokriva PowerShell/redirect/subshell/exe-swap.
  Treba structured `ExecRequest{absolute_exe,argv,cwd,env_delta,effect}`; regex samo advisory.
- **6.3 egress** — string-normalizacija ne zatvara DNS-rebinding/multi-A/redirect/proxy-env/child-socket.
  Treba policy-aware resolver+dialer koji pinna svaki IP prije connecta + egress-receipt.
- **6.4 secrets** — nije broker; treba opaque `SecretRef`, scoped materializacija u sandbox-helperu,
  sink-allowlist, "secret nikad u Envelope/journal" invariant.
- **3.3 cancel** — "cancel usred tool-poziva→nikakav side-effect" je NEOSTVARIVO; treba effect-taxonomy
  (prije-commit spriječi / poslije UNKNOWN_EFFECT→RECONCILING).
- **2.3 structured-output** — `re-ask` je model-pokušaj bez `AttemptGrant` → krši S7 sole-retry-owner.
- **1.3 journal** — svaki debug-event kroz durable journal = self-DoS; razdvojiti state/security/audit
  (durable) od debug-telemetry (best-effort drop).
- **0.3/0.5 S0** — označen "ZAOKRUŽEN" uz OTVOREN P0.5 + "atomska aktivacija" nemoguća kad gate ima
  OS/external side-efekt; treba `ActivationPlan`+`PREPARING/FAILED/ROLLING_BACK`.

---

## MORA BITI RIJEŠENO PRIJE PRVOG PRODUKCIJSKOG KODA (konsenzus)

1. **Očistiti plan** od svih salvage/GORTEX/NEXUSv2/port/reuse oslonaca; ispraviti 82; re-mark
   RESOLVED/UNRESOLVED po podsekciji. (C1)
2. **Zaključati S0** kanonske Go tipove + RED ugovore UKLJUČUJUĆI napisan P0.5 + activation-failure
   stateove. Tek tada kernel scaffold. (C3, codex#26)
3. **Sandbox feasibility-probe** (izolirano, neprodukcijski) Linux+Win+macOS → eksplicitna platform
   capability/fail-closed odluka. Bez toga S6.2 i arbitrary-exec NE u kod. (C2)
4. **Napisati vlastite Go dizajne + RED matrice za 5-6 rupa bez izvora:** evidence-checker, TIA,
   editrepair/symedit, shadow-checkpoint, execution-attestation, (+ memorija-descope). (C5)
5. **Zaključati jedan end-to-end effect-put** `S3→S4→S6.0/6.9→S1.2→S7` s JEDNIM ownerom za policy/
   process-lifecycle/retry; ukloniti nemogući cancel-zahtjev. (codex, C7)
6. **PRD presuđuje identitet** (jedna jezgra/dva profila) + brutalni P0-scope. (C6, C7)

**Zaključak (konsenzus):** S0/S1 buildable nakon korekcije ugovora. S2 djelomično. S3-S5 imaju 5
mehanizama koji su bili SAMO salvage → greenfield dizajn nužan prije koda. **S6 NIJE spreman** (6.2/
6.3/6.4/6.6/6.7 nemaju vjerodostojan cross-platform Go floor). S9 memorija treba descope. Plan je
DIZAJN-bogat ali IZVOR-siromašan; nije spreman za kod dok se 6 must-resolve ne zatvori.
