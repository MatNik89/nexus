# Phase 6 deep review + full security-surface integration review — Kilo

Target: `slice/p0-phase6` at HEAD `c2884ce` (446a12b T25 sandbox, c2884ce T26 exectool). Both gates. Method: code-read, run the suite in the repo (`internal/buildcheck` needs `.git`), run the T25 hostile suite on this host (bwrap 0.11.2 present), negative probes in a clean `git archive HEAD` export (`/tmp/kilo/p6-export`). Working tree never modified. Suite: `CGO_ENABLED=0 go vet ./...` exit 0; `CGO_ENABLED=0 go test -count=1 ./...` all 33 packages `ok`.

## Scope A — Phase 6 deep review

### T25 sandbox (verified)
`internal/sandbox` implements `Backend{Probe,Compile,Launch,Attest}` with a sealed `CompiledPolicy` (unexported fields), a stale-probe guard (`Launch` re-derives the probe digest, `sandbox.go:161-163`), fail-closed `Compile` shape guards (absolute target/workdir, timeout ceiling, `:126-148`), and `Attest` binding process→policy+closure digest (`:187-209`). I re-ran the hostile suite on this host: **12/12 PASS, 0 skipped** (`TestBackendShadowClassDenied`, `TestBackendEgressDenied`, `TestBackendDynamicELFRuns`, `TestBackendShebangRejected`, `TestBackendWorkdirWritable`, `TestBackendTimeoutKillsTree`, `TestAttestationMismatchUntrusted`, `TestOutputBounded`, `TestStaleProbePolicyRefused`, and the fail-closed compile/launch shapes). Bounded output (1 MiB) drains the pipe (swallows-but-returns-`len(p)`, so the subprocess never blocks).

### T26 exectool (verified)
`internal/exectool` is the one process door: closed `{command,args}` schema (`DisallowUnknownFields`, `:82-87`), absolute-ELF-path only (`:88-90`), `ExecutionKind=ExecProcess` sealed with unknown tool/kind rejected (`:76-81`), disposable 0700 workdir removed after (`:92-99`), deadline-bounded 2-minute timeout (`:100-103`), result trusted only under a verified attestation with the E9 commit receipt backed by `att.ClosureDigest` (`:119-151`), output fenced `TrustUntrustedExternal` (`:138`), and pruning preserving exit status + error tail. The `SandboxedProcessExecutor` is the only process-executing type and fails closed on a nil backend (`effectpath.go:280-285`); `noSandbox` refuses (`daemon.go:362-364`). Suite (e2e ASK, read-only-still-sandboxed canary, rejection, pruning) passes; `TestExecSpineEndToEndSandboxed` proves channel ASK suspend→approve→resume runs sandboxed with the exit status in the reply.

### THE REAL FIND — EffectHash canonicalization (attacked hardest)

`canonicalJSON` (`effectpath.go:143-162`) re-encodes args with `json.Decoder` (into `interface{}`/map) + `json.Marshal`. It is **non-injective** for duplicate keys: Go's map decode is last-wins, so two distinct documents collapse to one canonical form.

**Finding 1 — [MED] Duplicate keys silently collapse, weakening C4 exact-intent.** Confirmed by two probes in the clean export:

1. Hash collision (`internal/kernel/effectpath`):
   `EffectHash({"command":"/bin/echo","command":"/bin/rm","args":["/"]}) == EffectHash({"command":"/bin/rm","args":["/"]})` — the canonical form cannot distinguish the two documents. (Number formats stay distinct — `UseNumber`; unicode escapes are correctly unified — same value.)

2. End-to-end (`internal/approval`): an approval whose **summary shows both values** `{"command":"/bin/echo","command":"/bin/rm","args":["/"]}` is consumable by the collapsed `{"command":"/bin/rm","args":["/"]}`:
   ```
   SUMMARY shown to approver: … tool exec with args {"command":"/bin/echo","command":"/bin/rm","args":["/"]} …
   DEFECT: approval for duplicate-key summary consumed by /bin/rm-only call
   ```

   The exec args decoder (`exectool.go:82-87`) likewise accepts duplicate keys (last-wins) because `DisallowUnknownFields` does not reject duplicate *known* keys. So the approver's summary and the executed command can diverge: a prompt-injected model emits a benign-looking first value (`/bin/echo`) and a dangerous last value (`/bin/rm`); the owner approves the raw (obfuscated) summary, and the dangerous value executes. This contradicts the fold's claim that "values still exact — C4 intent unchanged": a duplicate key is a dropped value, and "any change invalidates" no longer holds. Fix: detect duplicate keys and fail closed (or use an injective encoding that preserves them), in both `canonicalJSON` and the exec args decode.

**Finding 2 — [LOW] Exec output reaches the provider unredacted.** The exec observation (`exectool.go:133-140`) is the raw subprocess output, and the sandbox mounts `/` read-only, so an approved `exec /bin/cat <path>` can read an arbitrary host file (provider key, bot token, config) and echo it into the model context. The journal and the final are redacted, but the planner (provider) receives the raw observation — consistent with existing tool-output handling (not a new redaction gap per se), but exec widens the read surface to the whole host. Owner-approved (DecisionAsk) mitigates.

## Scope B — security integration (cross-component)

Verified with no additional defect:

- **Channel → exec requires ASK**: `channelLoop` hardcodes `ModeDefault` (`daemon.go:302`) and `mergedRules()` always sets `exec=DecisionAsk` (`main.go:517-527`); the planner offers the exec spec only behind a passing probe (`main.go:465-470`). No channel path reaches exec without the ASK gate.
- **yolo cannot widen exec beyond confirmations, and the sandbox holds**: `ModeYolo` only bypasses the ASK confirmation (journaled `ALLOWED_BY_YOLO`, `effectpath.go:391-397`); the executor dispatch still routes `ExecProcess` to `SandboxedProcessExecutor` (`effectpath.go:425-426`) → real bwrap. yolo is CLI-session-only (`channelLoop` is never yolo).
- **Resume executes exec inside the sandbox**: `resumeApproved` uses `b.sysPath`, built with `NewSandboxedProcessExecutor(execDoor)` (`main.go:442-445`).
- **Profile isolation across exec workdirs**: `os.MkdirTemp` + `0700` + `RemoveAll` (`exectool.go:92-99`); no shared/persistent path.
- **TOCTOU Compile→Launch**: the probe digest is re-verified at `Launch` (`sandbox.go:161-163`) and the ELF check runs inside `probe.Prepare` at Launch, not only at Compile.
- **Fencing**: exec output is `TrustUntrustedExternal` and cannot be promoted; it never enters an approval summary (summaries bind args, not output).

## Verdict

T25 and T26 are solid (hostile suite 12/12, e2e spine sandboxed, cross-component chain holds, yolo/sandbox/resume/TOCTOU/profiles all correct). But the fold's central "REAL FIND" — canonical argument hashing — introduces a confirmed C4 exact-intent weakening: duplicate-key collapse lets a distinct argument document consume an approval whose summary shows different content (two failing probes). This is a real defect in the security-critical hash, not taste.

VERDICT: FAIL
