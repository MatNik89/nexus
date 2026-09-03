# PHASE-0 code review round 6 — Codex

Scope: very narrow verification of the three round-5 folds introduced by `17888b3967983f71669adb8b1f97f5da800fd180`. `git diff 17888b3..HEAD -- internal/preflight/probe` is empty, so HEAD executes the reviewed code.

Fresh evidence:

- `CGO_ENABLED=0 go test -count=1 ./internal/preflight/probe/` — PASS, uncached, 24.284 s.
- `CGO_ENABLED=0 go vet ./internal/preflight/probe` — PASS.
- `go test -list '^Test' ./internal/preflight/probe` discovered 34 tests; the uncached package run executed the shadowing pair, loosened negative control, both budget tests, and hostile-XDG case.

1. **[OK] The hostile-XDG fixture no longer risks deleting a pre-existing fixed path.** It creates a fresh unpredictable child with `os.MkdirTemp(home, ".nexus-probe-red-*")`, normalizes it to 0700, and removes only the returned test-owned path (`internal/preflight/probe/probe_linux_test.go:699-719`). This folds round-5 finding #1. Minor proof ceiling, not a material defect: the cleanup defer is installed after `Chmod`, so a rare chmod failure could leave one newly created empty directory; it cannot delete unrelated data.

2. **[OK] The library-shadowing detector is now causal and revert-capable.** `buildDynamicWriter` produces a dynamic ELF, so its production closure creates both the interpreter path under `/lib` and the loader-searched `/nexus-libs` (`internal/preflight/probe/probe_linux_test.go:621-640`). The positive test requires the concrete `Read-only file system` failure rather than accepting ENOENT, while the unexported `remountRW` negative control runs through the same production launcher and proves a new write beneath `/nexus-libs` succeeds when only `--remount-ro /` is omitted (`internal/preflight/probe/probe.go:104-112,657-664`; `internal/preflight/probe/probe_linux_test.go:642-667`). Deleting the remount makes the `/lib/evil` positive assertion fail, so the old false-green class is closed. Minor proof ceiling: fixture compilation skips when no C compiler is available; the reviewed deployment host executed it rather than skipping.

3. **[OK] Aggregate byte and dependency-node allowances are enforced before the allocations identified in round 5.** Every target, interpreter and library load receives the current aggregate remainder; `loadBounded` selects the lower of that remainder and the per-file cap before opening/reading, then uses a `cap+1` limiter to detect growth (`internal/preflight/probe/probe.go:152-183,313-339,391-404`). `checkQueueBudget` runs against `len(meta.needed)` before either root or child `depNode` slices are constructed, and `push` repeats the guard before append (`internal/preflight/probe/probe.go:113-121,351-369,409-419`). Boundary tests reject exhausted/undersized byte allowance and queue overflow while accepting exact capacity (`internal/preflight/probe/probe_linux_test.go:669-695`). `ImportedLibraries` necessarily materializes its metadata result inside `debug/elf`, but the source file feeding it is independently capped at 64 MiB; that is outside the node-slice allocation defect being folded and does not bypass a closure byte or node append.

4. **[OK] No material regression was found in lines changed by `17888b3`.** The new loosen switch is private to the probe package, production defaults still append `--remount-ro /`, all error paths close owned closure descriptors, and the focused suite remains green. The three folds use existing standard-library and test seams without a new production dependency.

VERDICT: PASS
