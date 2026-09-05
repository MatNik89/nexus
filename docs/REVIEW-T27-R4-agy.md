# REVIEW-T27-R4 — Phase 7 (T27) Round 4 Re-Verification & Final Gate (Narrow)

**Target Ref:** `slice/p0-phase7` @ HEAD (`a26a5f8efd9f5832a76f284b3e8e19e7a718c5e0`)  
**Commits Covered:** `a26a5f8` (fold codex round 3 findings: #1 HIGH, #2 MED) on top of `6eab172`.  
**Reviewer:** `agy` (Antigravity)  
**Scope:** Verification of both Round-3 fold claims (binary trust anchor fingerprinting + fixed root-owned verifier path, direct durable SQLite state assertion on sealed-off startup), negative ablation probes in clean `git archive HEAD` trees, and execution of the full repository test suite.  
**Working Tree Discipline:** All negative ablations were performed strictly within temporary clean exports (`/tmp/nexus-t27-r4-probe`). The working tree in `/home/matej/HARNESS/nexus` remained clean throughout.

---

## 1. Executive Summary & Verification Matrix

Commit `a26a5f8` addresses the two findings from Round 3:

1. **Binary-Pinned Trust Anchor & Fixed Root Verifier (codex #1 HIGH):**
   - In [`scripts/p0-accept.sh:11-20`](file:///home/matej/HARNESS/nexus/scripts/p0-accept.sh#L11-L20), the SHA-256 fingerprint of `$CONF_DIR/allowed_signers` is compiled directly into the binary via `-ldflags "-X main.acceptanceSignerFingerprint=$FP"`.
   - In [`cmd/nexus/main.go:888-912`](file:///home/matej/HARNESS/nexus/cmd/nexus/main.go#L888-L912), `verifyAcceptanceAttestation` refuses unpinned builds (`acceptanceSignerFingerprint == ""`), refuses replaced trust anchors (`hex.EncodeToString(sum[:]) != acceptanceSignerFingerprint`), and invokes the verifier at the fixed absolute path `/usr/bin/ssh-keygen` after verifying it is root-owned (`sys.Uid == 0`) and not group/world-writable (`perm & 0o022 == 0`).
   - PATH shims and rogue anchor replacements are inert against the binary.
   - Causal detector [`TestDoctorP0GrantLive`](file:///home/matej/HARNESS/nexus/internal/acceptance/acceptance_linux_test.go#L836-L945) verified across missing attestation, unsigned, replaced trust anchor, PATH shim, wrong-key signature, and digest mismatch branches.
2. **Durable Obligation State Assertion on Refused Startup (codex #2 MED):**
   - In [`internal/acceptance/acceptance_linux_test.go:762-777`](file:///home/matej/HARNESS/nexus/internal/acceptance/acceptance_linux_test.go#L762-L777), `obligationStatus` reads directly from the profile's `journal.db` SQLite file in read-only mode.
   - In [`internal/acceptance/acceptance_linux_test.go:1039-1044`](file:///home/matej/HARNESS/nexus/internal/acceptance/acceptance_linux_test.go#L1039-L1044), `TestSealedOffStartupRunsNoConsumers` asserts that the overdue reminder remains `SCHEDULED` in the database after the refused incarnation exits.

All 34 packages in the repository pass `go vet ./...` and `go test -count=1 ./...` cleanly in ~61s.

### Verification Matrix

| # | Item / Claim | Implementation | Verification & Causal Probe | Status |
|---|---|---|---|---|
| 1 | **Binary Trust Anchor Pinning** (codex #1) | `-ldflags -X main.acceptanceSignerFingerprint=$FP` ([`scripts/p0-accept.sh:20`](file:///home/matej/HARNESS/nexus/scripts/p0-accept.sh#L20)); `verifyAcceptanceAttestation` verifies `allowed_signers` SHA-256 matches `acceptanceSignerFingerprint` ([`cmd/nexus/main.go:888-904`](file:///home/matej/HARNESS/nexus/cmd/nexus/main.go#L888-L904)). | Ablating fingerprint check in `verifyAcceptanceAttestation` turns [`TestDoctorP0GrantLive`](file:///home/matej/HARNESS/nexus/internal/acceptance/acceptance_linux_test.go#L836-L945) **RED**: `replaced trust anchor granted (exit 0)`. | **[OK] VERIFIED & CAUSAL** |
| 2 | **Fixed Root-Owned Verifier Path** (codex #1) | `verifyAcceptanceAttestation` checks `/usr/bin/ssh-keygen` `Uid == 0 && perm&0o022 == 0` and executes absolute path ([`cmd/nexus/main.go:905-920`](file:///home/matej/HARNESS/nexus/cmd/nexus/main.go#L905-L920)). | PATH shim branch in `TestDoctorP0GrantLive` fails closed and refuses false grant. | **[OK] VERIFIED** |
| 3 | **Direct SQLite State Assertion** (codex #2) | `obligationStatus` reads `SELECT status FROM obl_obligations` from profile SQLite ([`internal/acceptance/acceptance_linux_test.go:762-777`](file:///home/matej/HARNESS/nexus/internal/acceptance/acceptance_linux_test.go#L762-L777)). | Injected pre-seal `b.sched.Sweep` turns [`TestSealedOffStartupRunsNoConsumers`](file:///home/matej/HARNESS/nexus/internal/acceptance/acceptance_linux_test.go#L998-L1061) **RED**: `sealed-OFF startup advanced durable state: obligation "DELIVERY_PENDING" (want SCHEDULED)`. | **[OK] VERIFIED & CAUSAL** |
| 4 | **Full Test Suite Cleanliness** | Live sandbox with real bwrap on host; full suite run across all packages. | `CGO_ENABLED=0 go test -count=1 ./...` PASS across all 34 packages. | **[OK] VERIFIED** |

---

## 2. Negative Ablation Probes

All negative ablation probes were executed against clean `git archive HEAD` trees in `/tmp/nexus-t27-r4-probe`.

### 1. Binary Acceptance Signer Fingerprint Pinning (`TestDoctorP0GrantLive`)
- **Code Audit:** In [`cmd/nexus/main.go:888-904`](file:///home/matej/HARNESS/nexus/cmd/nexus/main.go#L888-L904), `verifyAcceptanceAttestation` computes `sum := sha256.Sum256(signersBytes)` over `<config>/nexus/allowed_signers` and requires exact equality with `acceptanceSignerFingerprint`.
- **Negative Ablation:** Bypassed the `hex.EncodeToString(sum[:]) != acceptanceSignerFingerprint` check in `verifyAcceptanceAttestation`.
- **Ablation Output:**
  ```text
  === RUN   TestDoctorP0GrantLive
      acceptance_linux_test.go:894: replaced trust anchor granted (exit 0):
          LIVE conversation   live provider round trip
          LIVE memory         profile journals open, projections folded
          LIVE profiles       work and private journals independently open
          LIVE reminders      durable scheduler substrate ready
          LIVE telegram       live getMe passed
          LIVE sandbox        trust-rooted bwrap, enforcement canary passed
          LIVE acceptance     signed acceptance pass for this exact binary (2026-09-05T00:00:00Z)
          P0-capable — all six PRD §6 capabilities measured live AND the acceptance suite attested this exact binary
  --- FAIL: TestDoctorP0GrantLive (10.60s)
  FAIL
  FAIL	github.com/MatNik89/nexus/internal/acceptance	16.356s
  ```
- **Verdict:** `[OK]` Verified causal.

### 2. Sealed-OFF Startup Direct Durable State Hold (`TestSealedOffStartupRunsNoConsumers`)
- **Code Audit:** In [`cmd/nexus/main.go:189-193`](file:///home/matej/HARNESS/nexus/cmd/nexus/main.go#L189-L193), if `!snap.On("conversation")`, `runDaemon` terminates immediately before `sched.Sweep` can execute. In [`internal/acceptance/acceptance_linux_test.go:1039-1044`](file:///home/matej/HARNESS/nexus/internal/acceptance/acceptance_linux_test.go#L1039-L1044), `w.obligationStatus(t, "private", "rem-seal")` reads directly from the SQLite database and confirms state remains `"SCHEDULED"`.
- **Negative Ablation:** Placed `_, _ = b.sched.Sweep(ctx)` before `if !snap.On("conversation")` in `cmd/nexus/main.go`.
- **Ablation Output:**
  ```text
  === RUN   TestSealedOffStartupRunsNoConsumers
      acceptance_linux_test.go:1043: sealed-OFF startup advanced durable state: obligation "DELIVERY_PENDING" (want SCHEDULED)
  --- FAIL: TestSealedOffStartupRunsNoConsumers (9.67s)
  FAIL
  FAIL	github.com/MatNik89/nexus/internal/acceptance	16.312s
  ```
- **Verdict:** `[OK]` Verified causal.

---

## 3. Top 3 Weakest Points

1. **Rebuild-with-Attacker-Pin Ceiling ([`scripts/p0-accept.sh:11-20`](file:///home/matej/HARNESS/nexus/scripts/p0-accept.sh#L11-L20), [`cmd/nexus/main.go:888-904`](file:///home/matej/HARNESS/nexus/cmd/nexus/main.go#L888-L904)):** The acceptance signer fingerprint is pinned via `-ldflags` into the binary. If an attacker can recompile the binary on the host with their own custom `-X main.acceptanceSignerFingerprint=<hash>`, they can produce a binary that accepts arbitrary rogue keys. This is within the accepted single-user threat model (the owner installs only signed release binaries), but is a key design boundary.
2. **Fixed System Binary Prerequisite ([`cmd/nexus/main.go:905-912`](file:///home/matej/HARNESS/nexus/cmd/nexus/main.go#L905-L912)):** `verifyAcceptanceAttestation` hardcodes `/usr/bin/ssh-keygen` and requires it to be root-owned with `0755` permissions. While this guarantees immunity to PATH shimming, systems with non-standard paths (e.g. NixOS or `/usr/local/bin`) will fail the doctor check.
3. **Loopback Bot API Policy Scope ([`internal/foundation/config/config.go:318-325`](file:///home/matej/HARNESS/nexus/internal/foundation/config/config.go#L318-L325)):** `ValidateBounds` permits loopback URLs (`127.0.0.1`, `localhost`, `[::1]`) for testing and local Bot API server instances, trusting that local loopback ports are not unauthenticated malicious services.

---

VERDICT: PASS
