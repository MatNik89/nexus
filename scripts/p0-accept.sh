#!/bin/sh
# T27 P0 acceptance run: executes the OUT-OF-IMPLEMENTATION black-box
# harness against ONE pinned binary and, on full PASS, writes the
# acceptance attestation that `nexus doctor --p0` requires before it will
# grant P0-capable (the doctor never self-certifies — codex T27 #3).
# Usage: p0-accept.sh   (run from the repo root on the deployment host)
#
# PUBLICATION DISCIPLINE (fresh-audit codex #4): every input is validated
# and every artifact is STAGED in the private work directory first; the
# installed trust anchor and attestation are only replaced — atomically,
# via same-directory rename — after the whole grade has passed. A failed
# run never mutates the previously installed trust set.
set -eu
ROOT="$(cd "$(dirname "$0")/.." && pwd)"
WORK="$(mktemp -d)"
trap 'rm -rf "$WORK"' EXIT
# --- PUBLISH: replace the installed trust SET (anchor + attestation +
# signature) coherently. Each rename is atomic; a failure between them
# would mix generations (fresh-audit codex #4 r2), so the previous set is
# snapshotted first and ANY interrupted publication restores it in full.
publish_trust_set() {
	# $1=staged signers  $2=staged attestation  $3=staged signature
	CONF_DIR="${XDG_CONFIG_HOME:-$HOME/.config}/nexus"
	OUT_DIR="$CONF_DIR/system"
	mkdir -p "$OUT_DIR"
	RB="$WORK/rollback"
	mkdir -p "$RB"
	for f in "$CONF_DIR/allowed_signers" "$OUT_DIR/acceptance.json" "$OUT_DIR/acceptance.json.sig"; do
		[ -f "$f" ] && cp "$f" "$RB/$(basename "$f")"
	done
	rollback() {
		for f in allowed_signers:"$CONF_DIR" acceptance.json:"$OUT_DIR" acceptance.json.sig:"$OUT_DIR"; do
			name="${f%%:*}"; dir="${f#*:}"
			if [ -f "$RB/$name" ]; then
				cp "$RB/$name" "$dir/$name.rb" && mv "$dir/$name.rb" "$dir/$name"
			else
				rm -f "$dir/$name"
			fi
		done
		echo "acceptance: interrupted publication ROLLED BACK — previous trust set restored" >&2
	}
	# fault seam for the rollback test ONLY (NEXUS_ACCEPT_TEST_FAIL_AFTER=n
	# fails after the n-th rename); inert unless explicitly set.
	step=0
	fail_seam() {
		step=$((step + 1))
		[ "${NEXUS_ACCEPT_TEST_FAIL_AFTER:-}" = "$step" ] && { rollback; return 1; }
		return 0
	}
	cp "$1" "$CONF_DIR/allowed_signers.tmp"
	cp "$2" "$OUT_DIR/acceptance.json.tmp"
	cp "$3" "$OUT_DIR/acceptance.json.sig.tmp"
	if ! mv "$CONF_DIR/allowed_signers.tmp" "$CONF_DIR/allowed_signers"; then rollback; return 1; fi
	fail_seam || return 1
	if ! mv "$OUT_DIR/acceptance.json.tmp" "$OUT_DIR/acceptance.json"; then rollback; return 1; fi
	fail_seam || return 1
	if ! mv "$OUT_DIR/acceptance.json.sig.tmp" "$OUT_DIR/acceptance.json.sig"; then rollback; return 1; fi
	return 0
}
# TEST-ONLY entry (rollback conformance): publish the three given staged
# files without building or grading. Grants nothing an attacker could not
# do by writing the config dir directly; inert unless explicitly set.
if [ "${NEXUS_ACCEPT_TEST_PUBLISH_ONLY:-}" = "1" ]; then
	publish_trust_set "$1" "$2" "$3"
	exit $?
fi
# --- validate inputs BEFORE touching any installed state ---
KEY="${NEXUS_RELEASE_KEY:?set NEXUS_RELEASE_KEY to the owner ssh private key}"
[ -f "$KEY" ] || { echo "acceptance: private key $KEY does not exist" >&2; exit 2; }
[ -f "$KEY.pub" ] || { echo "acceptance: public key $KEY.pub does not exist" >&2; exit 2; }
PUB="$(cat "$KEY.pub")"
[ -n "$PUB" ] || { echo "acceptance: $KEY.pub is empty" >&2; exit 2; }
# --- stage the trust anchor; the fingerprint pins the STAGED file ---
SIGNERS_STAGE="$WORK/allowed_signers"
printf 'owner %s\n' "$PUB" > "$SIGNERS_STAGE"
FP="$(sha256sum "$SIGNERS_STAGE" | cut -d' ' -f1)"
BIN="$WORK/nexus"
CGO_ENABLED=0 go build -buildvcs=false -ldflags "-X main.acceptanceSignerFingerprint=$FP" -o "$BIN" "$ROOT/cmd/nexus"
DIGEST="$(sha256sum "$BIN" | cut -d' ' -f1)"
echo "acceptance: grading binary sha256=$DIGEST"
cd "$ROOT"
# The graded run REQUIRES the sandbox toolchain (fresh-audit codex #2):
# NEXUS_ACCEPT_REQUIRE_SANDBOX turns every bwrap-absence skip into a hard
# failure, and the scope includes the hostile sandbox conformance suites
# (FS/net/syscall-floor probes), not only the six PRD criteria.
NEXUS_ACCEPT_BIN="$BIN" NEXUS_ACCEPT_REQUIRE_SANDBOX=1 CGO_ENABLED=0 \
  go test -count=1 -timeout 1800s ./internal/acceptance ./internal/preflight/probe ./internal/sandbox
# The attestation is SIGNED (T27-r2 codex #2: a plain JSON is forgeable
# by any process that can write the config dir). NEXUS_RELEASE_KEY names
# the owner ssh key; doctor verifies against <config>/nexus/allowed_signers.
# Machine identity must be trustworthy and valid — the shared checker
# mirrors doctor exactly (P0-prep-r3 codex).
MID="$("$ROOT/scripts/machine-id-check.sh" /etc/machine-id)"
ATT_STAGE="$WORK/acceptance.json"
cat > "$ATT_STAGE" <<JSON
{"binary_sha256":"$DIGEST","suite":"internal/acceptance+probe+sandbox","passed":true,"host":"$(uname -srm)","machine_id_sha256":"$(printf '%s' "$MID" | sha256sum | cut -d' ' -f1)","time":"$(date -u +%Y-%m-%dT%H:%M:%SZ)"}
JSON
ssh-keygen -Y sign -f "$KEY" -n nexus-acceptance "$ATT_STAGE"
publish_trust_set "$SIGNERS_STAGE" "$ATT_STAGE" "$ATT_STAGE.sig"
# Install the EXACT graded binary so the running nexus matches the digest.
echo "acceptance PASS — attestation written: $OUT_DIR/acceptance.json"
echo "install the graded binary (its sha256 must match at doctor time), e.g.:"
echo "  install -m 0755 $BIN ~/bin/nexus   # before \$WORK is cleaned"
INSTALL_TO="${NEXUS_INSTALL_TO:-}"
[ -n "$INSTALL_TO" ] && install -m 0755 "$BIN" "$INSTALL_TO" && echo "installed: $INSTALL_TO"
exit 0
