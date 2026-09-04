# Phase 6 verification round 2 — Kilo (defensive fold review)

Target: `slice/p0-phase6` at HEAD `d222082` (folds `1c9886b` kilo #1-#2 + `d222082` codex #1-#6). Kilo scope: verify my two round-1 findings are genuinely fixed, and confirm the codex fold claims are real. Method: code-read, run the suite in the repo (`internal/buildcheck` needs `.git`), run the targeted tests + a duplicate-key edge-case probe in a clean `git archive HEAD` export (`/tmp/kilo/p6r2-export`). Working tree never modified. bwrap 0.11.2 present. Suite: `CGO_ENABLED=0 go vet ./...` exit 0; `CGO_ENABLED=0 go test -count=1 ./...` all 33 packages `ok`.

## Round-1 finding status

| # | Round-1 finding | Status at HEAD | Evidence |
|---|---|---|---|
| 1 | [MED] `canonicalJSON` duplicate-key collapse weakened C4 exact-intent | **FIXED** | Three independent layers: (a) `contracts.NewToolCall` REJECTS duplicate member names (`contracts.HasDuplicateJSONKeys`, `contracts.go:405-407`); (b) `canonicalJSON` hashes duplicate-key documents as their RAW bytes, disjoint from the canonical space (`effectpath.go:150-157`); (c) `exectool.Launch` refuses duplicate-key args outright (`exectool.go:95-99`). |
| 2 | [LOW] Exec output reached the provider unredacted | **FIXED** | `exectool.New` requires the known-ref redactor (`exectool.go:55-58`); `Launch` applies `redactText` before the model boundary (`exectool.go:152`); composition passes the journal's redactor (`main.go:448`). |

## Verification evidence

- **Duplicate-key injectivity** — `TestEffectHashDuplicateKeysDoNotCollapse` (effectpath) passes: `NewToolCall` rejects `{"command":"/bin/echo","command":"/bin/rm",...}`; the struct-literal defense-in-depth branch (`EffectHash(dup) != EffectHash(collapsed)`) holds. `TestDuplicateArgKeysRejected` (exectool) passes. `TestApprovalSurvivesKeyReorder` still passes (clean key-reorder unifies, value-tamper rejected) — the fix did not break the intended canonicalization.
- **Detector correctness** — my independent edge-case probe in the clean export (`TestProbeHasDuplicateJSONKeysEdgeCases`) passed: top-level dup, nested dup, array-element dup, and unicode-escaped dup (`{"\u0061":1,"a":2}`) → detected; sibling keys, nested-same-key-different-scope (`{"a":{"a":1}}`), deep-clean, array siblings, scalar, and bare number → not flagged; valid exec args → not flagged. `HasDuplicateJSONKeys` (`contracts.go:576-626`) is a correct token-stream walk.
- **Redaction** — `TestExecOutputRedactsKnownSecrets` passes (a secret entering via argv is scrubbed with the redaction marker present). `redactText` (`exectool.go:182-196`) JSON-round-trips the output through the redactor; its fail-closed branches are unreachable for valid output (a Go string always marshals).

## Codex fold claims (sanity-verified)

All six pass their tests: `TestResumePreservesObservationTrust` (trust+lineage preserved through resume — exec's `TrustUntrustedExternal` is no longer laundered by `resumeObservation`, `main.go:325-336`); `TestFakeBwrapRejected` (binary-content probe hash + live enforcement canary); `TestLaunchRefusesSwappedTarget` (target bytes pinned at Compile); `TestCancelledContextNeverLaunches` (pre-cancel + live-cancel kill-tree); `TestShellInterpreterDeniedByDefault` (exec DENY-DEFAULT behind `exec_allow`, empty list refuses `/bin/ls`). None intersects my dimensions adversely.

## NEW-defect hunt

None found. Specifically:

- `HasDuplicateJSONKeys` returns `true` for malformed input (treated as non-canonicalizable), but `NewToolCall` independently gates on `json.Valid` (`contracts.go:408-410`), so malformed args are rejected regardless; the duplicate "duplicate keys + invalid JSON" error pair is cosmetic only.
- `canonicalJSON`'s raw-bytes branch is disjoint from the canonical space (a canonical `json.Marshal` output never contains a duplicate key), so no cross-space collision is possible.
- `redactText` is applied to `proc.Output()` only; the exit status line is not redacted but can contain no secret; the commit receipt `ContentHash` is the closure digest (E9 phase), while the output block's `ContentHash` is the redacted content — both correct.
- `resumeBlocks` preserves the tool's own output blocks with their trust labels and only synthesizes a `TrustToolTrusted` stub for EMPTY output (`main.go:327-335`) — exec (non-empty, `TrustUntrustedExternal`) stays fenced through resume.

## Verdict

Both round-1 findings are genuinely fixed (each backed by a passing RED-capable test plus my independent edge-case probe), the six codex fold claims are real, and the fold introduces no NEW defect.

VERDICT: PASS
