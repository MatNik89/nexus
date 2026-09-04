#!/bin/sh
# T27 P0 acceptance run: executes the OUT-OF-IMPLEMENTATION black-box
# harness against ONE pinned binary and, on full PASS, writes the
# acceptance attestation that `nexus doctor --p0` requires before it will
# grant P0-capable (the doctor never self-certifies — codex T27 #3).
# Usage: p0-accept.sh   (run from the repo root on the deployment host)
set -eu
ROOT="$(cd "$(dirname "$0")/.." && pwd)"
WORK="$(mktemp -d)"
trap 'rm -rf "$WORK"' EXIT
# The acceptance signer's allowed_signers hash is PINNED into the graded
# binary (-ldflags): the binary itself is the trust anchor (r3 codex #1).
KEY="${NEXUS_RELEASE_KEY:?set NEXUS_RELEASE_KEY to the owner ssh private key}"
CONF_DIR="${XDG_CONFIG_HOME:-$HOME/.config}/nexus"
mkdir -p "$CONF_DIR"
SIGNERS="$CONF_DIR/allowed_signers"
printf 'owner %s\n' "$(cat "$KEY.pub")" > "$SIGNERS"
FP="$(sha256sum "$SIGNERS" | cut -d' ' -f1)"
BIN="$WORK/nexus"
CGO_ENABLED=0 go build -ldflags "-X main.acceptanceSignerFingerprint=$FP" -o "$BIN" "$ROOT/cmd/nexus"
DIGEST="$(sha256sum "$BIN" | cut -d' ' -f1)"
echo "acceptance: grading binary sha256=$DIGEST"
cd "$ROOT"
NEXUS_ACCEPT_BIN="$BIN" CGO_ENABLED=0 go test -count=1 -timeout 900s ./internal/acceptance
# The attestation is SIGNED (T27-r2 codex #2: a plain JSON is forgeable
# by any process that can write the config dir). NEXUS_RELEASE_KEY names
# the owner ssh key; doctor verifies against <config>/nexus/allowed_signers.
OUT_DIR="${XDG_CONFIG_HOME:-$HOME/.config}/nexus/system"
mkdir -p "$OUT_DIR"
cat > "$OUT_DIR/acceptance.json" <<JSON
{"binary_sha256":"$DIGEST","suite":"internal/acceptance","passed":true,"host":"$(uname -srm)","time":"$(date -u +%Y-%m-%dT%H:%M:%SZ)"}
JSON
rm -f "$OUT_DIR/acceptance.json.sig"
ssh-keygen -Y sign -f "$KEY" -n nexus-acceptance "$OUT_DIR/acceptance.json"
# Install the EXACT graded binary so the running nexus matches the digest.
echo "acceptance PASS — attestation written: $OUT_DIR/acceptance.json"
echo "install the graded binary (its sha256 must match at doctor time), e.g.:"
echo "  install -m 0755 $BIN ~/bin/nexus   # before \$WORK is cleaned"
INSTALL_TO="${NEXUS_INSTALL_TO:-}"
[ -n "$INSTALL_TO" ] && install -m 0755 "$BIN" "$INSTALL_TO" && echo "installed: $INSTALL_TO"
exit 0
