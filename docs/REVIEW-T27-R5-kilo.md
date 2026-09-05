# T27 verification round 5 — Kilo (tiny-diff confirmation)

Target: `slice/p0-phase7` at HEAD `d33d3e5` (two hardening commits `af6cbb1` + `d33d3e5` on top of `a26a5f8`). Kilo passed R4; this confirms the two commits introduce no NEW defect.

## Diff analysis

Both commits fix the same TOCTOU class in `verifyAcceptanceAttestation` (`cmd/nexus/main.go:878-938`) — a mutable path being read once for a check and re-read for use:

- `af6cbb1`: `allowed_signers` is read ONCE (`signersBytes`), hashed for the fingerprint pin, and written to a private 0600 temp copy (`pinnedSigners` under `os.MkdirTemp("", "nexus-signers-")`, 0700) that `ssh-keygen -f` consumes — the on-disk path is never re-read, so a swap between the hash and the verify cannot slip a rogue anchor.
- `d33d3e5`: the same binding for the attestation and signature. `raw` (read ONCE at the top) is what the grant fields are parsed from (`json.Unmarshal(raw, …)`) **and** what `ssh-keygen` verifies via `cmd.Stdin = bytes.NewReader(raw)` (no `os.Open` re-open). The `.sig` is read ONCE (`sigBytes`) and served via a private 0600 `pinnedSig` copy (`-s pinnedSig`).

Result: each of the three inputs (attestation bytes, signers bytes, signature bytes) is read exactly once and used for both its check and the `ssh-keygen -Y verify`, so no mutable path is reopened between check and use. The signature is therefore cryptographically bound to the exact bytes that granted.

## Verification

`CGO_ENABLED=0 go vet ./...` exit 0; `CGO_ENABLED=0 go test -count=1 ./...` all 33 packages `ok` (acceptance 70s).

## NEW-defect hunt

None. Specifically:

- `raw` → `json.Unmarshal` and `raw` → `bytes.NewReader` are the same in-memory bytes, so the granted fields and the verified bytes cannot diverge.
- `pinnedSigners`/`pinnedSig` live in a 0700 `MkdirTemp` dir and are 0600, removed by `defer os.RemoveAll`; no shared-basename TMPDIR race.
- The remaining `os.Stat(sigPath)` is only an existence check (produces the "UNSIGNED" message); the signature content is read once (`sigBytes`) and never re-read, so a post-Stat swap still verifies against `raw` and fails without the owner key.
- The accepted ceiling (attacker holding the owner private key / rebuild-with-attacker-pin) is untouched; the fix closes only the key-less read-twice window.

## Verdict

The two commits are a correct, self-contained closure of the read-twice TOCTOU class, and introduce no NEW defect.

VERDICT: PASS
