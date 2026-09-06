# REVIEW-FRESH-R4-kilo — QA verification round 4

- **Repo**: `/home/matej/HARNESS/nexus`
- **Reviewed commit**: `2257e6a` (`fix(fresh-audit-r3): trust set publishes as one generation behind an atomic pointer`), branch `slice/p0-fresh-audit`
- **Method**: read-only. Work in a clean `git archive` export at `/tmp/kilo/r4` (verified: no `.git`). Repo never mutated; only this report is written into it.
- **Environment note**: the host is under heavy load (loadavg ~22, dominated by a `TestSoakSurvival` run left running since Sep 05). The `TestAcceptPublishGenerationSwitchIsAtomic` conformance test and both ablations completed cleanly; the long end-to-end `TestDoctorP0GrantLive` could not be re-run inside the audit window, so the doctor side is verified by full source read instead (noted below).

## Verdict summary

The **design** is correct; the **atomicity detector** is not RED-capable as claimed.

| Aspect | Disposition |
|--------|-------------|
| Generation dir + single `mv -T` pointer switch | **CORRECT** (by inspection) |
| Doctor pointer resolution (bare-name rejection, single read) | **CORRECT** (full source read) |
| Acceptance fixture publishes identically | **CORRECT** (source read) |
| `TestAcceptPublishGenerationSwitchIsAtomic` | PASSES (0.86s) |
| Claimed RED-capability (rm+ln ablation → RED) | **FALSE** — Finding A (LOW) |

One unresolved LOW finding → verdict FAIL.

---

## What is correct

- `scripts/p0-accept.sh:23-44` (`publish_trust_set`) stages the three files into a unique `$TRUST/gen-…-$$` directory, then flips the pointer with `ln -s … current.new.$$` + `mv -T … current`. `mv -T` is a same-filesystem `rename(2)`, so the pointer flip is atomic; a kill before it leaves `current` on the previous complete generation (or absent on first install), a kill after it leaves the new complete generation. Correct.
- Prior generations are never pruned (`p0-accept.sh:21-22`), so recovery has the full history.
- `cmd/nexus/main.go:902-914` resolves the pointer **once** with `os.Readlink`, rejects non-bare targets (`strings.ContainsAny(genName, "/\\") || genName == "." || genName == ".."`), and reads `acceptance.json`, `acceptance.json.sig`, and `allowed_signers` from the concrete `genDir` (not re-through the symlink), so a concurrent switch cannot mix generations. Signature verification pins the bytes read once (`main.go:997-1018`). Correct.
- `internal/acceptance/acceptance_linux_test.go:806-852` (`writeAttestationMachine`) publishes via a generation dir + `os.Rename` symlink switch, matching the script.

## Finding A — LOW — the "switch" kill seam is misplaced, so the atomicity detector cannot go RED

**Evidence.** In `scripts/p0-accept.sh`, the two kill seams are:

```
33:	if [ "${NEXUS_ACCEPT_TEST_KILL_AT:-}" = "stage" ]; then kill -KILL "$$"; fi
34:	if [ "${NEXUS_ACCEPT_TEST_FAIL_AFTER:-}" = "stage" ]; then …; return 1; fi
38:	if [ "${NEXUS_ACCEPT_TEST_KILL_AT:-}" = "switch" ]; then kill -KILL "$$"; fi
39:	ln -s "$GEN" "$TRUST/current.new.$$"
42:	mv -T "$TRUST/current.new.$$" "$TRUST/current"
```

The "switch" seam is at line 38 — **before** `ln -s` (line 39) and `mv -T` (line 42). It is therefore at exactly the same observable state as the "stage" seam (line 33): after the three `cp`s, before *any* symlink work. The test's own comment claims `'switch' kills at the LAST instant before the atomic rename`, but the last instant before the rename is between `ln -s` and `mv -T`, which is never exercised. "switch" adds no coverage over "stage".

Consequence: the detector cannot distinguish the atomic `mv -T` switch from a non-atomic two-step `rm -f current; ln -s gen current` switch. A future regression to the two-step form would silently pass, because the kill lands before `rm` and the clean path (`rm` then `ln` without interruption) still installs the new generation.

**Ablation 1 (claim reproduced → does NOT go RED).** I replaced lines 39+42 with a two-step `rm -f current; ln -s gen current`, keeping the seams where they are:

```
go test -count=1 -run 'TestAcceptPublishGenerationSwitchIsAtomic' ./cmd/nexus
--- PASS: TestAcceptPublishGenerationSwitchIsAtomic (0.27s)
```

The claimed RED-capability is false: the test stays green under a non-atomic switch.

**Ablation 2 (seam CAN detect it — only if placed between the steps).** I moved the seam to between `rm` and `ln` (the non-atomic gap):

```
go test -count=1 -run 'TestAcceptPublishGenerationSwitchIsAtomic' ./cmd/nexus
--- FAIL: … current generation incomplete: open …/trust/current/allowed_signers: no such file or directory
```

So the detector is capable of going RED against a non-atomic switch, but only when the kill lands in the gap — which the committed seam placement (line 38, before `ln -s`) never does.

**Suggested fix**: move the "switch" seam to between `ln -s` and `mv -T` (the true "last instant before the rename"). Then a kill at "switch" leaves `current` on the old generation (atomic case), while a two-step `rm+ln` regression with a kill in its `rm`→`ln` gap leaves `current` absent → RED.

## Verification not completed in-window (honesty note)

`TestDoctorP0GrantLive` (the end-to-end doctor path) did not complete in the audit window — the host is saturated by an unrelated long-running `TestSoakSurvival` from Sep 05 (loadavg ~22). I verified the doctor's generation reading and the acceptance fixture by reading `cmd/nexus/main.go:893-1022` and `internal/acceptance/acceptance_linux_test.go:806-852` in full rather than re-running that test. This does not affect Finding A, which is proven by the two ablations above against `TestAcceptPublishGenerationSwitchIsAtomic`.

## Confidence

- **Ablation-proven**: the atomicity test does not go RED under a two-step `rm+ln` switch (Ablation 1), and does go RED when the seam is moved into the gap (Ablation 2).
- **Verified by full source read**: generation-dir + `mv -T` atomicity, doctor pointer resolution and bare-name rejection, acceptance fixture parity.
- Everything re-derived from `2257e6a`; no prior-session context relied upon.

VERDICT: FAIL
