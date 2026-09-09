#!/bin/sh
# T27 P0 acceptance run: executes the OUT-OF-IMPLEMENTATION black-box
# harness against ONE pinned binary and, on full PASS, writes the
# acceptance attestation that `nexus doctor --p0` requires before it will
# grant P0-capable (the doctor never self-certifies — codex T27 #3).
# Usage: p0-accept.sh   (run from the repo root on the deployment host)
#
# PUBLICATION DISCIPLINE (fresh-audit codex #4 r3): every input is
# validated and every artifact is STAGED first; the trust set publishes
# as ONE generation directory activated by a single atomic pointer
# rename. A failed or killed run never mutates the installed trust set.
set -eu
ROOT="$(cd "$(dirname "$0")/.." && pwd)"
# Release gate (AUDIT-FULL F3): toolchain floor + govulncheck on the EXACT
# graded binary, BEFORE grading/signing/publishing. Shared with deploy.sh.
. "$ROOT/scripts/lib/release-gate.sh"
WORK="$(mktemp -d)"
trap 'rm -rf "$WORK"' EXIT
# --- PUBLISH: the trust SET (anchor + attestation + signature) is ONE
# generation directory; <config>/nexus/trust/current is a symlink flipped
# by a SINGLE atomic rename (fresh-audit codex #4 r3). A kill at ANY
# point — SIGKILL included — leaves the previous complete generation
# installed; an interrupted run leaves only an unreferenced staging dir.
# topknot: prior generation dirs are retained for recovery, never pruned
# automatically; prune by hand if the trust dir ever grows past taste.
publish_trust_set() {
	# $1=staged signers  $2=staged attestation  $3=staged signature
	CONF_DIR="${XDG_CONFIG_HOME:-$HOME/.config}/nexus"
	TRUST="$CONF_DIR/trust"
	GEN="gen-$(date -u +%Y%m%dT%H%M%SZ)-$$"
	mkdir -p "$TRUST/$GEN"
	cp "$1" "$TRUST/$GEN/allowed_signers"
	cp "$2" "$TRUST/$GEN/acceptance.json"
	cp "$3" "$TRUST/$GEN/acceptance.json.sig"
	# fault seams for the atomicity test ONLY; inert unless explicitly set.
	if [ "${NEXUS_ACCEPT_TEST_KILL_AT:-}" = "stage" ]; then kill -KILL "$$"; fi
	if [ "${NEXUS_ACCEPT_TEST_FAIL_AFTER:-}" = "stage" ]; then
		echo "acceptance: publication interrupted BEFORE the pointer switch — previous generation still current" >&2
		return 1
	fi
	ln -s "$GEN" "$TRUST/current.new.$$"
	# "switch" = the LAST instant before the atomic rename (between the
	# staging symlink and mv -T) — fresh-audit r4: the earlier placement
	# sat at the same observable state as "stage".
	if [ "${NEXUS_ACCEPT_TEST_KILL_AT:-}" = "switch" ]; then kill -KILL "$$"; fi
	# -T: replace the SYMLINK itself (never descend into the old target);
	# GNU coreutils is a given on the Linux-only deployment target.
	mv -T "$TRUST/current.new.$$" "$TRUST/current"
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
release_gate "$BIN"
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
echo "acceptance PASS — trust generation activated: ${XDG_CONFIG_HOME:-$HOME/.config}/nexus/trust/current"
echo "install the graded binary (its sha256 must match at doctor time), e.g.:"
echo "  install -m 0755 $BIN ~/bin/nexus   # before \$WORK is cleaned"
INSTALL_TO="${NEXUS_INSTALL_TO:-}"
[ -n "$INSTALL_TO" ] && install -m 0755 "$BIN" "$INSTALL_TO" && echo "installed: $INSTALL_TO"
exit 0
