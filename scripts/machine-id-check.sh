#!/bin/sh
# Validates a machine-identity file EXACTLY as `nexus doctor --p0` does
# (P0-prep r3/r4 codex: the producer must mirror the verifier, or the
# acceptance workflow false-PASSes). Check order: symlink -> regular ->
# permissions -> WHOLE-FILE content -> ownership; content precedes
# ownership so non-root test fixtures exercise every guard causally.
# Usage: machine-id-check.sh <file>. Prints the id on success.
set -eu
F="$1"
[ -e "$F" ] || { echo "no $F" >&2; exit 2; }
[ -L "$F" ] && { echo "$F is a symlink (fail closed)" >&2; exit 2; }
[ -f "$F" ] || { echo "$F is not a regular file (fail closed)" >&2; exit 2; }
PERMS="$(stat -c '%a' "$F")"
[ $(( 0$PERMS & 022 )) -eq 0 ] || { echo "$F is group/world-writable ($PERMS, fail closed)" >&2; exit 2; }
# WHOLE file, outer-whitespace-trimmed only (mirrors Go TrimSpace) —
# trailing bytes after the id line are NOT ignored (r4 codex #1).
CONTENT="$(cat "$F"; printf x)"; CONTENT="${CONTENT%x}"
MID="$(printf '%s' "$CONTENT" | sed -e ':a' -e 'N;$!ba' -e 's/^[[:space:]]*//' -e 's/[[:space:]]*$//')"
[ "${#MID}" -eq 32 ] || { echo "$F content malformed (fail closed)" >&2; exit 2; }
case "$MID" in
  *[!0-9a-f]*) echo "$F content malformed (fail closed)" >&2; exit 2;;
esac
[ "$MID" != "00000000000000000000000000000000" ] || { echo "$F uninitialized (fail closed)" >&2; exit 2; }
[ "$(stat -c '%u' "$F")" = "0" ] || { echo "$F not root-owned (fail closed)" >&2; exit 2; }
printf '%s' "$MID"
