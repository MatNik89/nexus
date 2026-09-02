# REF — brainless (TUI component taxonomy + P2 web-UI kandidat)

Izvor: `github.com/theswerd/brainless` (TypeScript, React/shadcn/Next.js, Bun, **MIT**).
**NIJE TUI** — web biblioteka koja rekreira terminalske UI-je coding-agenata kao web komponente
(statične vizualne reprodukcije iz snimljenih ANSI frameova; nisu ožičene na žive agente).
Za NAŠ Bubbletea/Go TUI **kod se NE reusea** (React≠Go). Vrijednost = dvije stvari:

## 1. Component-taksonomija = requirement-checklist za S14.1 TUI (P0)
Što agent-chat UI mora renderirati (iz brainless dekompozicije Claude/Codex/Grok):
- **header** (model/status/kontekst)
- **message** (user/assistant)
- **thinking** (live stream misli/reasoning)
- **tool-call** (koji alat, argumenti, status)
- **diff-view** (bojani diff za edite)
- **permission/approval prompt** → NAŠ ASK-gate (S6.0 ASK; diff-preview prije odobrenja) — KLJUČNO za P0
- **slash-menu** (komande)
- **todo-list** (plan/koraci)
- **exec/working** (pokrenuta naredba + izlaz)
- **session-transkript** (cijeli tok, scrollback)

→ Ubaciti kao TUI-requirement listu u tasks-P0 (S14.1). Fidelity metoda (tmux capture realnog
izlaza kao ground-truth) korisna za TUI snapshot-testove.

## 2. P2 web-UI kandidat
Naš S14.3 web-UI je odgođen (P2). Kad dođe, brainless React komponente (MIT) su direktno upotrebljive
za web-lice (već imaju Claude/Codex/Grok UI). Ne za P0.

**Odluka:** ne kopiraj kod; taksonomija = P0 TUI checklist; brainless = P2 web-UI izvor.
