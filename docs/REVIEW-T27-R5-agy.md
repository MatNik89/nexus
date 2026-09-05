# REVIEW-T27-R5 — Phase 7 (T27) Round 5 Re-Verification & Final Gate (Tiny-Diff)

**Target Ref:** `slice/p0-phase7` @ HEAD (`d33d3e52f01f3dbdfdf93ad7a22a36b322a4666a`)  
**Commits Covered:** `af6cbb1`, `d33d3e5` (binding `allowed_signers`, `acceptance.json` and `acceptance.json.sig` to single in-memory byte reads with private 0600 temp copies and stdin feeding) on top of `a26a5f8`.  
**Reviewer:** `agy` (Antigravity)  
**Scope:** Confirmation of zero new defects in `a26a5f8..HEAD`, code audit of the single-read byte-binding hardening in `verifyAcceptanceAttestation`, and full repository test suite execution.  
**Working Tree Discipline:** The working tree in `/home/matej/HARNESS/nexus` remained clean throughout.

---

## 1. Executive Summary & Code Audit

Commits `af6cbb1` and `d33d3e5` harden `verifyAcceptanceAttestation` in [`cmd/nexus/main.go:851-943`](file:///home/matej/HARNESS/nexus/cmd/nexus/main.go#L851-L943) against Time-Of-Check-To-Time-Of-Use (TOCTOU) / read-twice races on mutable filesystem paths:

1. **Single-Read Byte Binding for `allowed_signers`:** `signersBytes` is read once ([`cmd/nexus/main.go:898`](file:///home/matej/HARNESS/nexus/cmd/nexus/main.go#L898)), its SHA-256 fingerprint is verified against `acceptanceSignerFingerprint` ([`cmd/nexus/main.go:902-904`](file:///home/matej/HARNESS/nexus/cmd/nexus/main.go#L902-L904)), and written to a private 0700 temporary directory as `pinnedSigners` with `0600` permissions ([`cmd/nexus/main.go:918-926`](file:///home/matej/HARNESS/nexus/cmd/nexus/main.go#L918-L926)).
2. **Single-Read Byte Binding for `acceptance.json.sig`:** `sigBytes` is read once ([`cmd/nexus/main.go:928`](file:///home/matej/HARNESS/nexus/cmd/nexus/main.go#L928)) and written to the private temporary directory as `pinnedSig` with `0600` permissions ([`cmd/nexus/main.go:932-935`](file:///home/matej/HARNESS/nexus/cmd/nexus/main.go#L932-L935)).
3. **Single-Read Byte Binding for `acceptance.json` Content:** The in-memory byte slice `raw` (read once at [`cmd/nexus/main.go:853`](file:///home/matej/HARNESS/nexus/cmd/nexus/main.go#L853) and unmarshaled/validated) is piped directly to `ssh-keygen -Y verify` via `cmd.Stdin = bytes.NewReader(raw)` ([`cmd/nexus/main.go:938`](file:///home/matej/HARNESS/nexus/cmd/nexus/main.go#L938)).
4. **Hermetic Cleanup:** `defer os.RemoveAll(pinnedDir)` ([`cmd/nexus/main.go:922`](file:///home/matej/HARNESS/nexus/cmd/nexus/main.go#L922)) guarantees the temporary directory and all isolated files are wiped upon function return.

No mutable filesystem paths are reopened between validation and subprocess verification. No new defects or regressions were introduced.

All 34 packages in the repository pass `go vet ./...` and `go test -count=1 ./...` cleanly in ~91s.

---

## 2. Verification Matrix

| # | Item / Change | Source Location | Verification Result | Status |
|---|---|---|---|---|
| 1 | **`allowed_signers` Single-Read Binding** | [`cmd/nexus/main.go:898-926`](file:///home/matej/HARNESS/nexus/cmd/nexus/main.go#L898-L926) | `signersBytes` hashed against `acceptanceSignerFingerprint` and written to private 0600 file for `ssh-keygen`. | **[OK] VERIFIED** |
| 2 | **`acceptance.json.sig` Single-Read Binding** | [`cmd/nexus/main.go:928-935`](file:///home/matej/HARNESS/nexus/cmd/nexus/main.go#L928-L935) | `sigBytes` read once and written to private 0600 file for `ssh-keygen`. | **[OK] VERIFIED** |
| 3 | **`raw` In-Memory Stdin Pipeline** | [`cmd/nexus/main.go:938`](file:///home/matej/HARNESS/nexus/cmd/nexus/main.go#L938) | `bytes.NewReader(raw)` feeds the exact parsed bytes directly into `ssh-keygen` stdin. | **[OK] VERIFIED** |
| 4 | **Private Directory Isolation & Cleanup** | [`cmd/nexus/main.go:918-922`](file:///home/matej/HARNESS/nexus/cmd/nexus/main.go#L918-L922) | `os.MkdirTemp("", "nexus-signers-")` with `defer os.RemoveAll(pinnedDir)` prevents file reuse / leakage. | **[OK] VERIFIED** |
| 5 | **Full-Suite Integrity & Regression Check** | Full repository test suite run | `CGO_ENABLED=0 go test -count=1 ./...` PASS across all 34 packages. | **[OK] VERIFIED** |

---

## 3. Top 3 Weakest Points

1. **Rebuild-with-Attacker-Pin Ceiling ([`scripts/p0-accept.sh:11-20`](file:///home/matej/HARNESS/nexus/scripts/p0-accept.sh#L11-L20), [`cmd/nexus/main.go:888-904`](file:///home/matej/HARNESS/nexus/cmd/nexus/main.go#L888-L904)):** The acceptance signer fingerprint is pinned via `-ldflags` into the binary. If an attacker can recompile the binary on the host with their own custom `-X main.acceptanceSignerFingerprint=<hash>`, they can produce a binary that accepts arbitrary rogue keys. This is within the accepted single-user threat model (the owner installs only signed release binaries), but is a key design boundary.
2. **Fixed System Binary Prerequisite ([`cmd/nexus/main.go:906-913`](file:///home/matej/HARNESS/nexus/cmd/nexus/main.go#L906-L913)):** `verifyAcceptanceAttestation` hardcodes `/usr/bin/ssh-keygen` and requires it to be root-owned with `0755` permissions. While this guarantees immunity to PATH shimming, systems with non-standard paths (e.g. NixOS or `/usr/local/bin`) will fail the doctor check.
3. **Loopback Bot API Policy Scope ([`internal/foundation/config/config.go:318-325`](file:///home/matej/HARNESS/nexus/internal/foundation/config/config.go#L318-L325)):** `ValidateBounds` permits loopback URLs (`127.0.0.1`, `localhost`, `[::1]`) for testing and local Bot API server instances, trusting that local loopback ports are not unauthenticated malicious services.

---

VERDICT: PASS
