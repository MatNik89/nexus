#!/bin/sh
# Verify a NEXUS release signature. FAIL-CLOSED: any mismatch (tampered
# binary, wrong signer, wrong namespace) exits nonzero.
# Usage: release-verify.sh <binary> <sig> <allowed-signers-file> <signer-id>
set -eu
BIN="$1"; SIG="$2"; ALLOWED="$3"; SIGNER="$4"
ssh-keygen -Y verify -f "$ALLOWED" -I "$SIGNER" -n nexus-release -s "$SIG" < "$BIN"
echo "release signature OK: $BIN"
