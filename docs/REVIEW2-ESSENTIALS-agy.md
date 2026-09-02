# REVIEW2-ESSENTIALS-agy — Re-Review Round 2 (English Rewrite Verification)

**Meta:** `/home/matej/HARNESS/nexus/docs/ARCHITECTURE-ESSENTIALS.md` (English rewrite)  
**Recenzent:** Antigravity (Adversarial Re-Review, Round 2)  
**Izvori za provjeru:** `HARNESS-SPEC.md` (normative Annex A), `HARNESS-PLAN.md`, `DESIGN-FIXES-r2.md`, `DESIGN-STATUS.md`, `PRD.md`, `SECTION-MAP.md`, `PLAN-HOLES-CONSOLIDATED.md`.  
**Prethodni review ulazi:** `REVIEW-ESSENTIALS-agy.md`, `REVIEW-ESSENTIALS-codex.md`, `REVIEW-ESSENTIALS-kilo.md`.

---

## 1. PREGLED I METODOLOGIJA

U 2. rundi provjereno je:
1. Je li **svaki materijalni nalaz iz 1. runde** (iz sva 3 recenzenta: `agy`, `codex`, `kilo`) ispravno ugrađen (*folded*) u novi tekst ili odbačen uz eksplicitno obrazloženje?
2. Je li prepisivanje na engleski uvelo **nove greške** (iskrivljenja, krive prijevode, nepostojeće reference, pogrešne ugovore)?

---

## 2. AUDIT NALAZA IZ 1. RUNDE (SVA 3 RECENZENTA)

---

### A. Nalazi iz `REVIEW-ESSENTIALS-agy.md`

1. **[OK] Agy-F1 — Kriva atribucija ugovora za provenance laundering u E10:**  
   *U 1. rundi:* `(P0.3 provenance-laundering RED)`.  
   *U novom tekstu (E12:117, 120):* Ispravljeno na `(P0.1 RED test_s0_rejects_provenance_laundering)` uz točnu normativnu referencu `HARNESS-SPEC P0.1 (:1383)`.

2. **[OK] Agy-C1 — Zastarjeli status otvorenih stavki ("Otvoreno prije P0 koda" #2):**  
   *U 1. rundi:* Pogrešno navedeno da ugovori P0.5–P0.13 još ne postoje.  
   *U novom tekstu (Header:10-11, Open:180):* Eksplicitno razjašnjeno: `(Annex A P0.5–P0.13 contracts EXIST — HARNESS-SPEC:1534+; the older PLAN-HOLES C3 claim is stale.)`. Uklonjeno iz otvorenih blokera.

3. **[OK] Agy-F2 — Pogrešno citiranje addenduma A9 u E1:**  
   *U 1. rundi:* A9 citiran bez konteksta za CGO_ENABLED=0.  
   *U novom tekstu (E1:22):* Precizirano kao `A9 component strategy; PLAN-HOLES C4` uz eksplicitan tretman pure-Go zamjena i subprocess sidecara.

4. **[OK] Agy-M1 — Izostavljen ugovor dvofazne aktivacije (P0.5 / S0.3 / S0.5):**  
   *U 1. rundi:* Nije bilo opisa `ActivationPlan` životnog ciklusa.  
   *U novom tekstu (E7:69-76):* Uvrštena zasebna odluka `E7 — Two-phase capability activation (atomic activation is impossible)` koja formalizira `ActivationPlan (+PlanHash) -> Prepare -> Commit -> Activate + RollbackToken + PREPARING/FAILED/ROLLING_BACK`, `Effective() = min(declared, measured)` i RED test `test_every_flag_fails_when_one_gate_is_ablated`.

5. **[OK] Agy-M2 — Izostavljen ugovor atomskog FS pisanja i Pre-FS snapshota (P0.11 / S5.3):**  
   *U 1. rundi:* Nije bilo pravila `tmp -> fsync -> rename`.  
   *U novom tekstu (E4:45-48):* Eksplicitno dodano pravilo `AtomicWriter (tmp → fsync → rename, never in-place) with a snapshot taken BEFORE every FS effect; rollback is byte-identical (P0.11)`.

6. **[OK] Agy-C2 — Neusklađenost P0 Telegram kanala i P1.6 ugovora (`channels requires extensions`):**  
   *U 1. rundi:* P0 Telegram je bio u koliziji s P1.6 fail-closed pravilom.  
   *U novom tekstu (E15:148-151, Open:178):* Napetost eksplicitno dokumentirana i otvorena za rješavanje u hard-questions rundi (`P1.6 vs P0-Telegram closure tension: built-in Phase O stdio/bot path vs dynamic Phase P plugin channels`).

7. **[OK] Agy-N1 — Balans selekcije i kompresija:**  
   *U novom tekstu:* Kripto specifičnosti su ugrađene u E11, a Provider auth modovi sažeti u prateći blok, čime su oslobođena mjesta za E6 (Build Order) i E7 (Activation Lifecycle).

---

### B. Nalazi iz `REVIEW-ESSENTIALS-codex.md`

8. **[OK] Codex-01 [CONTRADICTION] — Status "zaključano / 0 blokera" nije bio istinit:**  
   *U novom tekstu (Header:5-6):* Uklonjena fraza "0 blokera"; dodana formulacija `Open preconditions are listed at the bottom — this document does NOT claim go/no-go has been granted`.

9. **[OK] Codex-02 [FIDELITY] — E2 zaključava "asistent PRVO" prije formalnog PRD statusa:**  
   *U novom tekstu (E2:24, 27):* Jasno naznačeno `assistant FIRST (decided) ... Decided in PRD (approved)` sukladno potvrđenom PRD v1 dokumentu.

10. **[OK] Codex-03 [FIDELITY] — E3 prejak ("jedini piše trajno stanje"):**  
    *U novom tekstu (E4:41-48):* Precizno ograničeno: `EventJournal.Append is the ONLY writer of canonical domain events and the state derived from them... Scope limit (do not over-read): workspace files, memory spine, backups are separate durable side-effects behind their effect-path owners...`.

11. **[OK] Codex-04 [MISSING] — Nedostaje dependency / build-order odluka (SECTION-MAP §1):**  
    *U novom tekstu (E6:61-68):* Uvrštena zasebna odluka `E6 — Build order is normative: K0 → K1 → L → M–P` (K0 primitivi i S16.6-det minimal checker prije K1 sigurnosti i S4 alata).

12. **[OK] Codex-05 [MISSING] — Transakcijska capability-aktivacija i rollback:**  
    *U novom tekstu (E7:69-76):* Uvrštena odluka `E7` sa svim potrebnim stanjima i ugovorima (`P0.4/P0.5`).

13. **[OK] Codex-06 [FIDELITY] — E8 preslabo destilirao egress granicu:**  
    *U novom tekstu (E11:107-109):* Prošireno: `Egress is NOT string filtering: policy-aware resolver + dialer that pins every resolved IP before connect (DNS-rebinding/multi-A safe), metadata-IP blocked in every encoding, redirect re-check, proxy-env sanitization, child-socket containment, egress receipt`.

14. **[OK] Codex-07 [CONTRADICTION] — Gateway retry ownership kolidirao sa S7:**  
    *U novom tekstu (E15:144-146):* Ispravljeno: `The gateway provides idempotency keys and receipts; S7 alone authorizes and schedules delivery retries (no second retry owner)`.

15. **[OK] Codex-08 [FIDELITY] — Ed25519 pogrešno klasificiran kao keyed-MAC:**  
    *U novom tekstu (E11:110-113):* Razdvojena 3 primitiva: `HMAC (hmac.New) = keyed MAC for local integrity; Ed25519 = digital signature for external anchor; plain pinned hash for skill-lock verify-on-load; NEVER hash.Sum(key)`.

16. **[OK] Codex-09 [FIDELITY] — P0 rez izjednačavao "nije u P0" s "izbačeno iz v1":**  
    *U novom tekstu (P0 scope:164-173):* Precizno razdvojeno: `Out of v1 entirely (PRD §5)` naspram `Cut from P0, lives in P1/P2 or awaits PRD decision (PLAN-HOLES C6)`.

17. **[OK] Codex-10 [CONTRADICTION] — "Coding-USP trojka = P1" skrivala minimalni checker:**  
    *U novom tekstu (E13:125-128):* Jasno razgraničeno: `The minimal deterministic checker (S16.6-det) is a K0 primitive and a P0 dependency — ObligationStore.MarkDone accepts only Evidence... The full coding USP trio ... is the P1 coding branch`.

18. **[OK] Codex-11 [MISSING] — P0 scope izostavljao acceptance granice iz PRD §6:**  
    *U novom tekstu (P0 scope:165-169):* Uključeno svih 6 mjerljivih kriterija iz PRD §6 (restart survival, shutdown survival, Telegram pre-approval, zero profile leakage, hostile test denial).

19. **[OK] Codex-12 [NOT-CRITICAL] — Previše prostora za 4 auth moda:**  
    *U novom tekstu (Provider block:156-163):* Auth modovi sažeti u prateći blok, SubscriptionOAuth označen kao cut iz P0.

20. **[OK] Codex-13 [FIDELITY] — E7 live-probe nazvan "prvim implement zadatkom":**  
    *U novom tekstu (E10:97-98, Open:176):* Preimenovano u `go/no-go preflight BEFORE production exec/scaffold code`.

21. **[OK] Codex-14 [CONTRADICTION] — Source precedence nedefiniran:**  
    *U novom tekstu (Header:8-11):* Formaliziran točan redoslijed prvenstva: `HARNESS-SPEC.md (normative Annex A) > DESIGN-FIXES-r2 / DESIGN-*.md > HARNESS-PLAN.md > SECTION-MAP / PLAN-HOLES`.

22. **[OK] Codex-15 [FIDELITY] — E1 "jedan statički binary" bez provjere:**  
    *U novom tekstu (E1:18-19):* Formulirano egzaktno: `static linking is the goal, verified by a CI check, not assumed`.

---

### C. Nalazi iz `REVIEW-ESSENTIALS-kilo.md`

23. **[OK] Kilo-01 / Kilo-07 [FIDELITY / CONTRADICTION] — Header "9 rundi / 0 blokera":**  
    *U novom tekstu:* Uklonjeno, usklađeno s otvorenim stavkama.

24. **[OK] Kilo-02 [FIDELITY] — Citat "PRD §6.6/§7":**  
    *U novom tekstu (E10:101):* Ispravljeno na `PRD §6 item 6, §7`.

25. **[OK] Kilo-03 [FIDELITY] — Provider interface ispustio `DataDescriptor`:**  
    *U novom tekstu (Provider:157):* Ispravljeno na `One Provider interface (Chat/Stream/Capabilities/DataDescriptor)`.

26. **[OK] Kilo-04 [FIDELITY] — E2 CodingProfile zamjena shadow-git -> symedit:**  
    *U novom tekstu (E2:27-28):* Izričito objašnjeno usklađenje prema A8: `C7's profile listing is corrected per A8: coding USP trio = TIA + evidence-gate + AST-symedit (shadow-git was disproven as unique)`.

27. **[OK] Kilo-05 [FIDELITY] — SubscriptionOAuth scope u P0:**  
    *U novom tekstu (Provider:159-160, P0 scope:170):* Označeno kao cut iz P0.

28. **[OK] Kilo-06 [CONTRADICTION] — Konflacija Linux kernel probea i Win/mac hardver probea:**  
    *U novom tekstu (E10:96-99, Open:176, 179):* Jasno razdvojeno: Linux conformance preflight je go/no-go za P0 scaffold, a Win/mac OS probe je odgođen s tim platformama i čeka korisnikov hardver.

29. **[OK] Kilo-08 [MISSING] — Capability-flag closure + activation-failure stateovi:**  
    *U novom tekstu:* Pokriveno u novoj odluci `E7`.

30. **[OK] Kilo-09 [MISSING] — Eksplicitno pozicioniranje USP #1-#4:**  
    *U novom tekstu (E13:127-129, E14:135-136):* Jasno mapirana sva 4 USP diferencijatora (USP #1, #2, #3 u P1 coding grani; USP #4 FORGET vs PURGE kao jedini P0 diferencijator).

31. **[OK] Kilo-10 [MISSING] — "No autonomous self-change" zlatno pravilo:**  
    *U novom tekstu (E2:29-30):* Ugrađeno: `Golden rule: NEXUS never modifies its own source autonomously — anomaly → scrubbed RepairBundle → user opt-in → Claude fixes via git → nexus upgrade`.

---

## 3. AUDIT NOVOG TEKSTA (PROVJERA NOVIH GREŠAKA)

Pregledom svih 181 linija novog engleskog teksta utvrđeno je:
- **E1–E15 numeracija i struktura:** Točno 15 fokusiranih odluka. Svaka odluka ima jasan format `ODLUKA → ZAŠTO → IZVOR`.
- **Fideliti prema normativnim ugovorima:**
  - `P0.1` (`test_s0_rejects_provenance_laundering`) u E12 točno citiran.
  - `P0.3` (`EventJournal` sole writer) u E4 točno citiran.
  - `P0.4/P0.5` (`ActivationPlan`, `Effective=min(declared, measured)`) u E7 točno citirani.
  - `P0.11` (`AtomicWriter`, snapshot prije FS efekta) u E4 točno citiran.
  - `P1.4` (Draft-commit-verify-compensate) u E9 točno citiran.
  - `P1.6` (Channels token / HITL) u E15 točno citiran.
  - `P2.1` (`MEMORY_FORGET` vs `DATA_PURGE`) u E14 točno citiran.
- **Dependency DAG & Status:** Usklađenost s `SECTION-MAP.md` i `DESIGN-FIXES-r2.md` je potpuna.
- **Nema novih iskrivljenja, krive terminologije ni lažnih "0 blokera" tvrdnji.**

---

## 4. ZAKLJUČAK

Dokument [`ARCHITECTURE-ESSENTIALS.md`](file:///home/matej/HARNESS/nexus/docs/ARCHITECTURE-ESSENTIALS.md) uspješno je integrirao **sve** materijalne nalaze iz 1. runde recenzije (ukupno 31 verifikacijska točka iz 3 recenzentska izvješća). Nisu pronađene nove greške, kolizije ni krive atribucije.

Dokument sada predstavlja vjerodostojan, operativan i normativno precizan cheat-sheet za P0/P1 razvoj.

---

VERDICT: PASS
