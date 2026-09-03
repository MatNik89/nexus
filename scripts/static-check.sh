#!/bin/sh
# E1 gate: the nexus binary must be a statically linked ELF executable
# (CGO_ENABLED=0 single binary).
# Exit 0: statically linked ELF. Exit 1: dynamically linked. Exit 2: cannot
# decide (missing tool, not an ELF, unreadable) — NEVER silently OK
# (Phase-0 review: the previous version was fail-open on every ldd error).
set -u
bin="${1:?usage: static-check.sh <binary>}"
[ -f "$bin" ] || { echo "static-check: no such file: $bin" >&2; exit 2; }
command -v ldd >/dev/null 2>&1 || { echo "static-check: ldd not available — cannot verify" >&2; exit 2; }

magic=$(head -c 4 "$bin" | od -An -tx1 | tr -d ' \n')
[ "$magic" = "7f454c46" ] || { echo "static-check: $bin is not an ELF file (magic $magic)" >&2; exit 2; }

out=$(ldd "$bin" 2>&1)
status=$?
case "$out" in
    *"not a dynamic executable"*|*"statically linked"*)
        echo "static-check: OK — $bin is statically linked"
        exit 0 ;;
esac
if [ $status -eq 0 ]; then
    echo "static-check: FAIL — $bin is dynamically linked:" >&2
    echo "$out" >&2
    exit 1
fi
echo "static-check: cannot verify $bin (ldd said: $out)" >&2
exit 2
