# REVIEW-FRESH-R5-kilo — QA verification round 5

- **Repo**: `/home/matej/HARNESS/nexus`
- **Reviewed commit**: `7f58df5` (`fix(fresh-audit-r4): seam-free trust-pointer atomicity detector; switch seam at the real boundary`), branch `slice/p0-fresh-audit`
- **Method**: read-only. Work in a clean `git archive` export at `/tmp/kilo/r5` (verified: no `.git`). Repo never mutated; only this report is written into it.
- **Environment note**: the host remains saturated (loadavg ~25; a `TestSoakSurvival` run since Sep 05 plus a concurrent sibling `go test ./cmd/... ./internal/...`). The two detectors and all ablations completed cleanly before that contention started; a redundant 5× stability loop was abandoned (blocked by load), which does not affect the conclusions below.

## Verdict summary

The round-4 LOW finding is closed, by two independent, revert-proof detectors:

| Check | Result |
|-------|--------|
| "switch" kill seam now between `ln -s` and `mv -T` | **FIXED** (`p0-accept.sh:38-45`) |
| `TestAcceptPublishGenerationSwitchIsAtomic` (seam-based) | GREEN atomic; **RED** on rm+ln |
| `TestTrustPointerNeverUnresolvableUnderConcurrentPublish` (seam-free) | GREEN atomic; **RED** on rm+ln with seams dropped |

No unresolved findings.

---

## 1. Seam placement — FIXED

`scripts/p0-accept.sh:38-45` now reads:

```sh
38:	ln -s "$GEN" "$TRUST/current.new.$$"
42:	if [ "${NEXUS_ACCEPT_TEST_KILL_AT:-}" = "switch" ]; then kill -KILL "$$"; fi
45:	mv -T "$TRUST/current.new.$$" "$TRUST/current"
```

The "switch" kill is now **between** the staging symlink and `mv -T` — the actual last instant before the atomic rename, no longer at the same observable state as "stage" (line 33). The round-4 misplacement is corrected.

## 2. Seam-based detector is now revert-proof

Committed: `TestAcceptPublishGenerationSwitchIsAtomic` **PASS** (0.41s).

**Ablation A** (my round-4 shape — replace the atomic flip with `rm -f current` + `ln -s gen current`, keeping the seams, which now land between `rm` and `ln`):

```
--- FAIL: TestAcceptPublishGenerationSwitchIsAtomic (0.60s)
    main_test.go:1690: current generation incomplete: open …/trust/current/allowed_signers: no such file or directory
```

The "switch" seam now kills inside the non-atomic gap and the test goes RED — exactly what round 4 found missing.

## 3. Seam-free detector is independently revert-proof

`TestTrustPointerNeverUnresolvableUnderConcurrentPublish` (`cmd/nexus/main_test.go`) runs a reader goroutine hammering `trust/current` through 40 alternating publications and fatals on any unresolvable pointer, incomplete generation, or mixed tag. It uses no kill seam, so it cannot be defeated by an ablation that drops the seams.

Committed (atomic): **PASS** (1.96s).

**Ablation B** (rm+ln with the "switch" seam dropped entirely — the detector must catch it alone):

```
--- FAIL: TestTrustPointerNeverUnresolvableUnderConcurrentPublish (0.25s)
    main_test.go:1794: pointer unresolvable mid-publication: readlink …/trust/current: no such file or directory
```

The reader observed the `rm`→`ln` absence window and the detector went RED with no seam to lean on.

## 4. Test correctness (review)

- The seam-free reader resolves `current` once (`os.Readlink`), then reads the three files from the concrete generation directory — the same TOCTOU-safe pattern as the doctor — so a concurrent switch mid-read cannot produce a mixed-tag verdict (the old generation is complete and retained).
- The tag extraction (`strings.SplitN(string(b), "-", 2)[0]`) correctly distinguishes the two staged sets.
- The buffered `violation` channel (cap 1) and the per-publish non-blocking check ensure a violation surfaced at any point is fatal; the reader exits on `close(stop)` with no goroutine leak that affects the test.

## Confidence

- **Ablation-proven**: seam-based test RED on rm+ln (Ablation A), seam-free test RED on rm+ln with seams dropped (Ablation B); both GREEN on the committed atomic implementation.
- **Verified by source read**: seam placement (`p0-accept.sh:38-45`), seam-free reader logic (`main_test.go`).
- Everything re-derived from `7f58df5`; no prior-session context relied upon.

VERDICT: PASS
