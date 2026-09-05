# P0-PREP verification round 6 — Codex

Reviewed branch `slice/p0-prep1` at immutable HEAD `6c142b92b3e46f5951f079233e4a289ce47683d6`, limited to `git show HEAD` and the single Codex round-5 finding. All mutations and binary fixtures ran in fresh `git archive HEAD` exports. Codex modified no repository file except this requested report; another reviewer's untracked report was neither read nor used.

## Findings

No concrete defect found in the reviewed fold.

## Verification

- Source trace: `scripts/machine-id-check.sh` compares the raw byte count with the NUL-stripped byte count before either `CONTENT` or `MID` is assigned. A mismatch exits 2 with `content malformed`.
- Clean-export NUL-splice probe: a file containing `16 hex + NUL + 16 hex + newline`, with trust prerequisites isolated by the same `stat` shim as round 5, was refused with exit 2 and `content malformed`; measured counts were 34 raw bytes and 33 NUL-stripped bytes.
- Clean-export committed detector: `CGO_ENABLED=0 go test -count=1 -run TestMachineIDCheckScriptMirrorsDoctor ./cmd/nexus` — PASS.
- Clean-export guard ablation: replacing only the raw/non-NUL length comparison with `:` made the same test FAIL at `nulsplice refused for the wrong reason` because execution reached the later ownership check — expected RED.
- Repository suite: `CGO_ENABLED=0 go test -count=1 ./...` — PASS, exit 0; complete combined-log `grep -n 'FAIL'` — no matches.
- `sh -n scripts/machine-id-check.sh` — PASS.
- The bound revision remained `6c142b92b3e46f5951f079233e4a289ce47683d6`, with no tracked working-tree modification before this report was created.

## Weakest points

1. The shell checker reads the trusted file multiple times; a privileged concurrent replacement could create inconsistent observations, but an unprivileged user cannot replace `/etc/machine-id` under the required root-owned path and metadata boundary.
2. NUL removal relies on the deployment host's `tr` implementation; this Linux host directly demonstrated the intended byte-count difference.
3. The new detector proves the current `/bin/sh` path on this Linux host, not portability to non-Linux utilities; the project contract is Linux-first.

Complexity review: lean already. The fix adds one streaming byte comparison and one regression fixture, with no new abstraction or dependency.

VERDICT: PASS
