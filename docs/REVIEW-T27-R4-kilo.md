# T27 verification round 4 — Kilo (narrow: confirm no NEW defect in 6eab172..HEAD)

Target: `slice/p0-phase7` at HEAD `a26a5f8` (fold of my round-3 findings). Kilo/agy passed R3; this confirms the round-4 fold introduces no NEW defect and that both fixes are causal. Method: code-read, run the suite in the repo, two ablation probes in a clean `git archive HEAD` export (`/tmp/kilo/t27r4`). Working tree never modified.

## Suite

`CGO_ENABLED=0 go vet ./...` exit 0; `CGO_ENABLED=0 go test -count=1 ./...` all 33 packages `ok` (acceptance 72s).

## Finding 1 verification — binary-pinned trust anchor (codex #1)

`acceptanceSignerFingerprint` (`main.go:56`) is pinned at build time (`-ldflags -X main.acceptanceSignerFingerprint=<sha256>` by `scripts/p0-accept.sh`). `verifyAcceptanceAttestation` (`main.go:878-918`) now: refuses an unpinned build (`== ""`), hashes `allowed_signers` and refuses a mismatch against the pin, and runs the verifier at a **fixed** root-owned `/usr/bin/ssh-keygen` (`os.Stat` → `sys.Uid == 0 && perm&0o022 == 0`) — so a PATH shim is inert and a replaced anchor can't match the pin.

**Ablation** (removed the `hex.EncodeToString(sum[:]) != acceptanceSignerFingerprint` comparison in the export): `TestDoctorP0GrantLive` goes RED at `acceptance_linux_test.go:894` — `replaced trust anchor granted (exit 0)`. The pin is causal: without it, a rogue `allowed_signers` + rogue signature pass the `ssh-keygen -Y verify` and the grant mints. The test's full branch matrix (no-attestation, unsigned, replaced-anchor, PATH-shim, rogue-sig, tampered-digest, token-removal) is genuine.

Key-less forgery: none found. The accepted ceiling (a rebuilt binary with an attacker pin = a compromised binary) holds; I attacked only the non-rebuild paths (replaced `allowed_signers`, PATH shim, rogue signature) and each is refused.

## Finding 2 verification — durable-state seal detector (codex #2)

`TestSealedOffStartupRunsNoConsumers` now reads the obligation state **directly from the profile SQLite** (`obligationStatus`, `acceptance_linux_test.go:765-777`, a raw read-only `SELECT status FROM obl_obligations` — external-tool equivalent, never the implementation's stores) and asserts the refused incarnation leaves it `SCHEDULED` (`:1042-1044`).

**Ablation** (removed the `conversation`-OFF `return 1` in the export, letting the sweep run): the test goes RED at `acceptance_linux_test.go:1043` — `sealed-OFF startup advanced durable state: obligation "DELIVERY_PENDING" (want SCHEDULED)`. The durable-state assertion is causal: a pre-seal sweep advancing the obligation turns the detector RED even though no message ever appeared.

## NEW-defect hunt

None material. Observations (LOW, not defects):

- `/usr/bin/ssh-keygen` is a hardcoded path; on a non-standard install the doctor fails closed ("missing") rather than granting — correct security posture, minor portability assumption.
- `pinnedDoctorBin` adds another in-test `go build` subprocess (per world), lengthening the acceptance package and marginally raising the cold-cache flake I noted in R2 — this run was green.
- `p0-accept.sh` signs via PATH `ssh-keygen` while the doctor verifies via the fixed `/usr/bin/ssh-keygen`; both resolve to the same binary on standard Linux, and any divergence fails closed.

## Verdict

Both round-3 fixes are real and causal (ablation RED on each guard), the trust anchor is now the binary itself (key-less forgery defended), and no NEW defect was introduced in `6eab172..HEAD`.

VERDICT: PASS
