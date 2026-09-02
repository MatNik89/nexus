# REVIEW8 — Codex verifikacija

Revizija: `c2831d85b416705e899efcfe1ddaa95e63e7b867` (`c2831d8`).

**N4-C03 [DJELOMIČNO]** Fallback je sada ispravan: samo read-only bez receipta→`BeforeCommit`; svaki effectful bez receipta te tuđi/nepoznat phase→`Unknown` (`/home/matej/HARNESS/nexus/docs/DESIGN-FIXES-r2.md:29-38`). `CommitReceipt` je vezan uz `CallID+AttemptNo` (`/home/matej/HARNESS/nexus/docs/DESIGN-S0-sandbox-codex.md:215-229`). Međutim `cr.Phase.Valid()` se poziva, a metoda nigdje nije definirana, pa skica ne kompajlira. `BoundTo` također uopće ne provjerava `ReceiptHash`; `Optional.Valid=true` s odgovarajućim ID-evima i ne-atestiranim/zero hashom prihvaća proizvoljan phase. Zatvoriti s kanonskim `EffectPhase.Valid()` i `CommitReceipt.ValidFor(call)` koji provjerava phase, binding i verificirani/non-zero receipt hash.

**IMA BLOKERA — nije spremno za PRD. Status: FAIL na `c2831d8`.**

Proof ceiling: statička provjera dvaju zadanih dokumenata; Go implementacija/testovi ne postoje.
