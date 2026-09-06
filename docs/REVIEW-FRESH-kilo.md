# REVIEW-FRESH-kilo — independent QA audit

- **Repo**: `/home/matej/HARNESS/nexus`
- **Reviewed commit**: `cd5975d` (`merge: slice/p0-prep-low — all-severity leak closure (3x PASS)`)
- **Method**: read-only. Work done in a clean `git archive` export under `/tmp/kilo/export`; the repo was never mutated by the audit (only this report is written into it, as instructed).
- **Tooling**: Go 1.26.4 (linux/arm64). Note: `go test -race` **cannot run on this host** — ThreadSanitizer aborts with `FATAL: ThreadSanitizer: unsupported VMA range` (arm64 TSAN limitation). Findings that depend on a data race are therefore evidenced by construction (code + Go `os/exec` semantics), not by a `-race` trace.

## Verdict summary

Three findings, no HIGH severity, but all are real and unresolved:

| # | Severity | Area | Summary |
|---|----------|------|---------|
| F1 | MEDIUM | `internal/sandbox` | Data race: one `boundedBuffer` written concurrently by stdout+stderr copy goroutines |
| F2 | LOW/MEDIUM | `internal/kernel/journal` | `Append` can lose an event and hang forever when `Close` wins a narrow race |
| F3 | LOW | `internal/channel/telegram` | In-memory `offset` is never persisted → full-history re-fetch + duplicate refusal replies on every restart |

---

## F1 (MEDIUM) — data race on the sandbox output buffer

**Evidence**

`internal/sandbox/sandbox.go:325-326`:
```go
out := &boundedBuffer{limit: 1 << 20}
if err := h.SetOutput(out, out); err != nil {
```
Both stdout **and** stderr are wired to the *same* `boundedBuffer`.

`internal/preflight/probe/probe.go:526-532`:
```go
func (h *Handle) SetOutput(stdout, stderr io.Writer) error {
	if h.started { ... }
	h.cmd.Stdout, h.cmd.Stderr = stdout, stderr
	return nil
}
```

`internal/sandbox/sandbox.go:393-411` — `boundedBuffer` has **no mutex**; `Write` mutates a `strings.Builder`:
```go
type boundedBuffer struct {
	buf   strings.Builder
	limit int
}
func (b *boundedBuffer) Write(p []byte) (int, error) {
	room := b.limit - b.buf.Len()   // check-then-act, unsynchronized
	...
	b.buf.Write(p[:room])
	...
}
```

`exec.Cmd` copies `Stdout` and `Stderr` to their writers in **two separate goroutines** (standard `os/exec` `writerDescriptor` behavior). So when a sandboxed program writes to both stdout and stderr (trivial: `echo out; echo err >&2`), two goroutines call `boundedBuffer.Write` on the same `strings.Builder` concurrently. `strings.Builder` is explicitly not safe for concurrent use — this is a textbook data race that can corrupt captured output, and it would be flagged immediately under `-race`.

**Reproduction (probe written and run in the export)**

`/tmp/kilo/export/internal/sandbox/zz_kilo_race_probe_test.go`:
- `TestKiloBoundedBufferConcurrentWriters` — two goroutines hammering one `boundedBuffer`.
- `TestKiloSharedWriterStdoutStderr` — the exact production shape: one `boundedBuffer` set as both `cmd.Stdout` and `cmd.Stderr`, running a shell loop that writes both streams.

Both compile and run (`go test ./internal/sandbox -run TestKiloShared…` → `ok`). The race cannot be *observed* at runtime without `-race` (silent UB), so the evidence is by construction; `-race` is unavailable on this host (see note above).

**Why it matters**: this is the real `exec` tool path (`exectool.Launch` → `sandbox.Launch`). Corrupted observation content flows into the planner's context; a race that produces torn bytes is nondeterministic output fed to the model. It does **not** weaken the sandbox boundary (output capture only), hence MEDIUM, not HIGH.

**Suggested fix**: give `boundedBuffer` a `sync.Mutex` in `Write`/`String`, or pass two distinct buffers for stdout and stderr.

---

## F2 (LOW/MEDIUM) — journal `Append` can lose an event and block forever under `Close`

**Evidence**

`internal/kernel/journal/journal.go:575-586` — `AppendBatch`:
```go
func (j *Journal) AppendBatch(ctx context.Context, batch []contracts.EnvelopeParams) ([]Event, error) {
	req := appendReq{batch: batch, reply: make(chan appendReply, 1)}
	select {
	case j.reqs <- req:
	case <-j.done:
		return nil, fmt.Errorf("journal is closed")
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	rep := <-req.reply     // <-- NO done/ctx guard; blocks unconditionally
	return rep.evs, rep.err
}
```

`internal/kernel/journal/journal.go:378-400` — the single append actor:
```go
func (j *Journal) actor(lastOffset uint64, lastHash string) {
	defer close(j.actorDone)
	for {
		select {
		case <-j.done:
			return
		case req := <-j.reqs:
			...
			req.reply <- rep
		}
	}
}
```

`internal/kernel/journal/journal.go:712-727` — `Close` closes `done` then waits for `actorDone`.

The race: an `Append` goroutine enters its `select` while the actor is idle (so the send `j.reqs <- req` is the only ready case and is selected/committed). Concurrently, `Close()` runs `close(j.done)`. The actor's own `select` now has **two** ready cases (`<-j.done` closed, `<-j.reqs` with the pending send) and picks randomly. If it picks `<-j.done`, the actor returns and never receives the pending send. The `Append` goroutine is then blocked **forever** in the committed send (`j.reqs <- req`) — its `select` has already committed and does not re-evaluate `<-j.done`/`<-ctx.Done()`. The event is silently lost and the caller hangs with no error, contradicting both the documented contract ("ctx gates ADMISSION only: after the actor accepts, the outcome is definitive") and the `Close` comment ("Concurrency-idempotent").

**Probe (RED-capable; written and run in the export)**

`/tmp/kilo/export/internal/kernel/journal/zz_kilo_race_probe_test.go`:
- `TestKiloAppendCloseRace` — 500 iterations × 8 concurrent appenders racing `Close`, with a 1.5s hang detector on each append.
- `TestKiloAppendAfterCloseReturnsError` — sanity: append-after-close must return an error, not hang (passes).

Both pass. The hang window is genuinely narrow (append must commit to the send in the microseconds before `done` closes, and the actor must then pick `done`), so the probe did **not** trigger in 500 iterations — this is a structural finding backed by a RED-capable probe, not a deterministic repro. Severity is LOW in practice (process is typically exiting at shutdown), but the defect is real: a lost write + a goroutine leak with no error surfacing, in the component the whole project treats as its single-write-owner of record.

**Suggested fix**: guard the reply wait and the send — e.g. `select { case rep := <-req.reply: …; case <-j.done: return nil, ErrClosed }` after the send, and/or make the actor drain `reqs` once before honoring `done`.

---

## F3 (LOW) — Telegram `offset` is never persisted

**Evidence**

`internal/channel/telegram/telegram.go:59` (`offset int64` field, memory only), `:184-201` (`PollOnce` advances `a.offset` in memory), `:286-301` (`Run` poll loop). There is no durable store for the last processed `update_id`.

Consequence: on every daemon restart, `offset` starts at 0, so `getUpdates` re-fetches the **entire** update history. The T22 dedup (`channel.Admit` keyed by `adapter|identity|update_id`) prevents re-*admitting* or re-*running* already-terminal messages, so this is not a correctness bug. But two side effects are real:

1. **Unbounded re-fetch** — a long-lived bot re-downloads its whole history on every restart (scaling/DoS surface, not a boundary issue).
2. **Duplicate refusal replies** — refused updates (group / non-private / unbound chat / non-text) are handled via `processUpdate` (`telegram.go:212-232`) which calls `EnqueueReply` **without** an admission record. A crash or restart re-fetches those updates and re-enqueues a fresh refusal (new delivery id), so the user receives the same "not bound / private-only" message again for every historical refused update.

Not reproduced end-to-end (needs a live daemon + restart against a fake Bot API); the mechanism follows directly from the code (`offset` is set only in `PollOnce`, never read back from durable state). LOW severity.

**Suggested fix**: persist `offset` (e.g. a `sched_meta`-style key in the profile journal, or a small file) and resume from it on restart.

---

## Test-honesty assessment (positive)

The acceptance harness is genuinely honest, not self-anchored:

- Every PRD §6 criterion has a paired **sensitivity** test that turns the feature *off* via a real switch (not by weakening the assertion): provider-down (`TestCriterion1SensitivityProviderDown`), wiped store (`TestCriterion2SensitivityFreshStore`), no channel (`TestCriterion3SensitivityNoChannel`), unbound chat (`TestCriterion4SensitivityUnboundChat`), same-profile (`TestCriterion5SensitivitySameProfile`), no sandbox (`TestCriterion6SensitivityNoSandbox`).
- Many tests carry explicit **vacuous-oracle guards** (`oracle vacuous:` fatal assertions in `schedule_test.go`, `obligation_test.go`, `probe_linux_test.go`, `closure_test.go`), proving the RED assertion would itself fail on a host where the detector is blind.
- The harness grades the real `nexus` **binary** as a black box (`NEXUS_ACCEPT_BIN`), and `doctor --p0` refuses to self-certify (signature + binary-digest + host + machine-id binding).
- `t.Skip` uses are all legitimate environment prerequisites (bwrap, ssh-keygen, a C compiler for `$ORIGIN` fixtures, `/etc/machine-id`, an opt-in live smoke, an opt-in soak), not hidden gaps.

I found no vacuous or self-anchored detector in the audited scope. This is the strongest part of the codebase.

## Non-findings I specifically checked and cleared

- `procStartTime` field offset (`/proc/pid/stat` field 22 → `fields[19]` after the comm) is correct.
- Single-writer lease: `_txlock=immediate` makes the read-then-upsert atomic; dead-pid takeover uses `(pid,starttime)` so PID reuse can't spoof liveness.
- Projection guard (`guardStatement`) is **over-broad** (fail-closed), not fail-open; the lexical ceiling is documented with an upgrade trigger.
- `canonicalJSON`/`EffectHash` duplicate-key collapse is a *safe* direction (mismatch = re-ask); `NewToolCall` rejects duplicate keys anyway.
- `exectool` `timeout <= 0 → 2m` branch looks fail-open but is fully masked: the S7 attempt context is already deadline-expired and `sandbox.Launch` refuses at `sandbox.go:298-300`.
- `atomicwrite.Write` (tmp → fsync → rename → dir-fsync) is correct crash-safe ordering.
- Config secret handling (`parseBoolStrict`/`parseList` never echo values), redactor JSON-escape spelling, and `requireID` non-echoing are all sound.

## Confidence

- **Verified (ran)**: both probes compile and pass; the journal after-close append correctly errors; the full `internal/sandbox` probe package compiles.
- **By construction (high confidence, not runtime-observed)**: F1 (data race — `-race` unavailable on arm64), F2 (structural race, 500-iteration probe did not trigger the narrow window).
- **By code path (not reproduced)**: F3 (needs a live daemon restart against a fake Bot API).

No claim above relies on any earlier session context; everything was re-derived from the repository at `cd5975d`.

VERDICT: FAIL
