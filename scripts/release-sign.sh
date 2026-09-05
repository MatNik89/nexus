#!/bin/sh
# NEXUS minimal release signing (T27, HARDQ A1 second half): the binary
# is signed with the owner's ssh key; the built-in Telegram adapter is
# compiled INTO the binary, so this one signature attests its integrity.
# Usage: release-sign.sh <binary> <ssh-private-key>  -> <binary>.sig
set -eu
BIN="$1"; KEY="$2"
[ -f "$BIN" ] || { echo "no binary: $BIN" >&2; exit 2; }
[ -f "$KEY" ] || { echo "no key: $KEY" >&2; exit 2; }
ssh-keygen -Y sign -f "$KEY" -n nexus-release "$BIN"
echo "signed: $BIN.sig (namespace nexus-release; the built-in telegram adapter is attested by this signature)"
