# PREP-LOW verification round 3 — Kilo

Target: `slice/p0-prep-low` at HEAD `2a49441` (closes the round-2 leading-LF parity finding). Method: code-read, run the suite in the repo, a RED-capability ablation in a clean `git archive HEAD` export (`/tmp/kilo/preplowr3`). Working tree never modified.

## Suite

`CGO_ENABLED=0 go vet ./...` exit 0; `CGO_ENABLED=0 go test -count=1 ./...` all 33 packages `ok`.

## Finding — leading-LF trim parity — FIXED and causal

`scripts/machine-id-check.sh:28-32` now performs a WHOLE-byte-sequence outer trim of exactly the ASCII set `[ \t\r\n]` via POSIX parameter expansion, replacing the per-line `sed` (which kept a leading LF because it processed an empty first record). The trim is:

```sh
TAB="$(printf '\t')"; CR="$(printf '\r')"
NL="$(printf '\nx')"; NL="${NL%x}"
MID="$CONTENT"
MID="${MID#"${MID%%[!\ $TAB$CR$NL]*}"}"   # strip leading [ \t\r\n]*
MID="${MID%"${MID##*[!\ $TAB$CR$NL]}"}"   # strip trailing [ \t\r\n]*
```

This is byte-for-byte `strings.Trim(raw, " \t\r\n")` (`trimMachineID`, `main.go:1019-1025`): the leading `%%[!ws]*`+`#` removes the leading whitespace run, and `##*[!ws]`+`%` removes the trailing run; interior whitespace survives, so multi-line content still refuses via the 32-length guard. Edge cases verified: no leading/trailing whitespace (no-op), all-whitespace content (empty → length 0 → malformed), and an empty result on the trailing pass is a no-op.

The `leadlf` (`"\n"+valid`) and `outmix` (`" \t"+id+"\r\n"`) fixtures (`main_test.go:1506-1523`) assert the content passes (falls through to `not root-owned`), alongside the existing `padded` fixture.

**Ablation** (reverted the shell to the per-line `sed` trim in the export): `TestMachineIDCheckScriptMirrorsDoctor` goes RED — `leadlf id refused as content (script diverges from Go)`. The whole-byte-sequence trim is causal, and the new fixtures bind the exact byte contract.

## NEW-defect hunt

None. The bracket expression `[!\ \t\r\n]` is a literal byte class (locale-independent); the `NL` variable correctly carries a single LF (`printf '\nx'` then `${NL%x}`); the `%%`/`##` longest-removal and `#`/`%` shortest-removal compose correctly for both empty and non-empty whitespace runs; and the stale "mirrors Go TrimSpace" comment was corrected to name `trimMachineID`.

## Verdict

The round-2 leading-LF parity finding is genuinely closed (whole-byte-sequence ASCII trim on both sides, RED-capable fixtures), and no NEW defect was introduced.

VERDICT: PASS
