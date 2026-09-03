# DESIGN-STATUS — 6 must-resolve nakon (a)+(b)

Izvori: `DESIGN-S0-sandbox-codex.md`, `DESIGN-memory-effectpath-kilo.md`, `DESIGN-checker-tia-edit-agy.md`.
Moderator-verifikacija (ne rubber-stamp): svaki dizajn pregledan protiv rupe koju zatvara.

| # | Must-resolve | Status | Tko | Bilješka |
|---|---|---|---|---|
| 1 | Očistiti plan od salvagea | **RESOLVED** | claude | commit e8b546d; 81 spanova, brojke, statusi |
| 2 | S0 tipovi + P0.5 + activation-failure | **RESOLVED** | codex | production-grade; ActivationPlan(immutable+PlanHash)+Prepare/Commit/Activate+RollbackToken+PREPARING/FAILED/ROLLING_BACK rješava nemoguću atomsku aktivaciju |
| 3 | Sandbox feasibility → platform odluka | **DESIGN RESOLVED / probe OPEN** | codex | Linux ENFORCED-nakon-live-probe (helper: no_new_privs→Landlock→per-arch-seccomp→execveat, bez pre-exec prozora, TOCTOU fail-closed); Win/macOS UNAVAILABLE+high-risk BLOKIRAN, nula weaker-fallback. **Stvarni-OS probe (W0-W10 / M0-M9) traži tvoj Windows/macOS hardver — ja nemam.** |
| 4 | Go dizajn+RED za rupe bez izvora | **DELIMIČNO** | kilo+agy | memorija descope (Decay+Audn P0, Dream/MemGit/MemAssoc v2) ✓; archmap query-API ✓; evidence-checker generic (worker≠checker) ✓; TIA+full-suite-fallback ✓; edit-engine (preimage+refuse>1) ✓. **DVIJE RUPE OSTAJU (vidi dolje).** |
| 5 | Jedan effect-path owner | **RESOLVED** | kilo | S6.0 policy / S6.9 lifecycle / S1.2 process / S7 retry = jedini owneri; effect-taxonomy (BEFORE/AFTER_COMMIT/UNKNOWN→RECONCILING) rješava nemogući cancel; 2.3 re-ask nosi AttemptGrant |
| 6 | PRD identitet + P0-scope | **RESOLVED (user approved 2026-09-03)** | korisnik | "jedna jezgra, dva profila" + P0 = 6 sposobnosti potvrđeno; zapis u PRD.md |

## OTVORENE RUPE u #4 (moderator našao, nisu rubber-stampane)

**D1 — AST symedit NIJE dizajniran (agy).** `OpASTSymRename` je samo enum + `ErrSymeditUnsupported`;
`Apply` switch pada u `default: unsupported`. Symedit je JEDAN od 4 stvarna diferencijatora — najteži
dio ostao stub. Treba zaseban fokusiran pass: tree-sitter/LSP integracija, symbol-scoped rename koji
ne dira string/komentar, v1 ograničen na imenovane jezike (Go prvi).

**D2 — ExecutionReceipt potpis je KRIPTO BUG (agy).** `r.Signature = h.Sum(signerKey)` NE radi keyed-MAC;
`hash.Sum(key)` vraća `key || digest` → ključ curi u output, potpis krivotvorljiv. RED test bi lažno
prošao. **Fix:** `hmac.New(sha256.New, key)` ili `ed25519.Sign`. Isto vrijedi za sve "signed" artefakte
(shadow-checkpoint, audit-hash-chain 15.3).

**D3 (minorno) — TIA je line-level, ne symbol-level.** `SymbolSpan` tip definiran ali NEkorišten;
mapiranje je diff-linija→coverage-blok→test (line-overlap), ne diff→simbol. OK za v1, ali "diff×coverage
→simbol" tvrdnja je jača od implementacije. `fmt.Sscanf` na `file:start-end` puca na Windows `C:\` pathove.

## RIJEŠENO (claude, DESIGN-symedit-crypto-claude.md)
- **D1 symedit RESOLVED** — Go-native `go/types` (exact, v1 primarni) + LSP-adapter (ostali jezici),
  refuse-on-ambiguous/collision/build-broken, preview-only, NIKAD text-fallback. 8 RED. Zamjenjuje agy stub.
- **D2 crypto RESOLVED** — `Signer` (HMAC lokalno + ed25519 vanjski-anchor) + kanonska serijalizacija +
  `Verify`; ključ nikad u artefaktu; constant-time. 6 RED. Veže 15.3/6.8/S6.4.

## PREOSTAJE PRIJE KODA (samo tvoje)
- **#3 stvarni-OS probe** — Win/macOS hardver (W0-W10 / M0-M9 matrice spremne u codex dizajnu).
- **#6 PRD** — tvoja presuda (identitet "jedna jezgra/dva profila" + brutalni P0-scope).

Sve ostalo dizajn-spremno za kod. Preostale 2 stavke traže tvoj hardver / tvoju odluku — ne mogu ja.
