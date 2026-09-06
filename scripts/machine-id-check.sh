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
# NUL bytes first: shell command substitution silently DROPS them, so a
# NUL-spliced id would normalize here while Go refuses it (r5 codex).
RAW_LEN="$(wc -c < "$F")"
NONUL_LEN="$(tr -d '\000' < "$F" | wc -c)"
[ "$RAW_LEN" -eq "$NONUL_LEN" ] || { echo "$F content malformed (fail closed)" >&2; exit 2; }
# WHOLE file, outer-whitespace-trimmed only (mirrors Go TrimSpace) —
# trailing bytes after the id line are NOT ignored (r4 codex #1).
CONTENT="$(cat "$F"; printf x)"; CONTENT="${CONTENT%x}"
# Per-line trim (the slurp idiom is a no-op on single-line input —
# prep1-r5 kilo/agy): multi-line content still refuses via the length
# guard because interior newlines survive.
# Explicit ASCII trim set [ \t\r\n] — mirrors the doctor byte for byte
# (locale classes like [[:space:]] diverge from Go on U+00A0 etc.).
TAB="$(printf '\t')"; CR="$(printf '\r')"
MID="$(printf '%s' "$CONTENT" | sed -e "s/^[ $TAB$CR]*//" -e "s/[ $TAB$CR]*\$//")"
[ "${#MID}" -eq 32 ] || { echo "$F content malformed (fail closed)" >&2; exit 2; }
case "$MID" in
  *[!0-9a-f]*) echo "$F content malformed (fail closed)" >&2; exit 2;;
esac
[ "$MID" != "00000000000000000000000000000000" ] || { echo "$F uninitialized (fail closed)" >&2; exit 2; }
[ "$(stat -c '%u' "$F")" = "0" ] || { echo "$F not root-owned (fail closed)" >&2; exit 2; }
printf '%s' "$MID"
