#!/bin/sh
# E1 gate: the nexus binary must be statically linked (CGO_ENABLED=0 single binary).
# Exit 0 only for a non-dynamic executable; exit 1 for dynamic; exit 2 for usage errors.
set -eu
bin="${1:?usage: static-check.sh <binary>}"
[ -f "$bin" ] || { echo "static-check: no such file: $bin" >&2; exit 2; }
if ldd "$bin" >/dev/null 2>&1; then
    echo "static-check: FAIL — $bin is dynamically linked:" >&2
    ldd "$bin" >&2 || true
    exit 1
fi
echo "static-check: OK — $bin is statically linked"
