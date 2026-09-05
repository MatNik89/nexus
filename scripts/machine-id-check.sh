#!/bin/sh
# Validates a machine-identity file EXACTLY as `nexus doctor --p0` does
# (P0-prep-r3 codex: the producer must mirror the verifier, or the
# acceptance workflow false-PASSes). Usage: machine-id-check.sh <file>
# Prints the id on stdout on success; exits nonzero otherwise.
set -eu
F="$1"
[ -e "$F" ] || { echo "no $F" >&2; exit 2; }
[ -L "$F" ] && { echo "$F is a symlink (fail closed)" >&2; exit 2; }
[ -f "$F" ] || { echo "$F is not a regular file (fail closed)" >&2; exit 2; }
PERMS="$(stat -c '%a' "$F")"
[ $(( 0$PERMS & 022 )) -eq 0 ] || { echo "$F is group/world-writable ($PERMS, fail closed)" >&2; exit 2; }
[ "$(stat -c '%u' "$F")" = "0" ] || { echo "$F not root-owned (fail closed)" >&2; exit 2; }
# First line verbatim (no interior-whitespace deletion) minus the newline.
IFS= read -r MID < "$F" || true
printf '%s' "$MID" | grep -Eq '^[0-9a-f]{32}$' || { echo "$F content malformed (fail closed)" >&2; exit 2; }
[ "$MID" != "00000000000000000000000000000000" ] || { echo "$F uninitialized (fail closed)" >&2; exit 2; }
printf '%s' "$MID"
