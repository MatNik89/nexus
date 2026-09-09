#!/bin/sh
# Release gate (AUDIT-FULL F3): ONE helper sourced by scripts/p0-accept.sh and
# scripts/deploy.sh so the two paths cannot drift. It judges the EXACT binary
# that will be signed or installed:
#   1. the Go toolchain recorded IN the binary (`go version <bin>`) must be
#      >= RELEASE_GO_FLOOR — the audited 1.26.4 build carried five reachable
#      stdlib advisories whose fix floor is 1.26.6;
#   2. `govulncheck -mode=binary <bin>` must be installed and report nothing.
# Every failure mode fails CLOSED: unparsable version, missing scanner,
# scanner error, any finding. Exit codes: 0 ok, 1 gate refused, 2 cannot judge.
# The floor is OWNED here and cannot be lowered by the caller environment
# (code-review r1 codex #1: an env override let the audited 1.26.4 through).
readonly RELEASE_GO_FLOOR=1.26.6

# version_ge A B: dotted X.Y.Z integers, A >= B.
version_ge() {
	a1="${1%%.*}"; r="${1#*.}"; a2="${r%%.*}"; a3="${r#*.}"
	b1="${2%%.*}"; r="${2#*.}"; b2="${r%%.*}"; b3="${r#*.}"
	[ "$a1" -gt "$b1" ] && return 0
	[ "$a1" -lt "$b1" ] && return 1
	[ "$a2" -gt "$b2" ] && return 0
	[ "$a2" -lt "$b2" ] && return 1
	[ "$a3" -ge "$b3" ]
}

release_gate() {
	bin="$1"
	[ -f "$bin" ] || { echo "release-gate: no such binary: $bin (fail closed)" >&2; return 2; }
	# `go version <bin>` prints "<path>: goX.Y.Z"; anything else (devel,
	# rc, unreadable) is refused — a release must be a numbered toolchain.
	ver="$(go version "$bin" 2>/dev/null | awk '{print $2}')"
	case "$ver" in
		go[0-9]*.[0-9]*.[0-9]*) ver="${ver#go}" ;;
		*) echo "release-gate: cannot parse the toolchain recorded in $bin (got '${ver:-nothing}') — fail closed" >&2; return 2 ;;
	esac
	case "$ver" in
		*[!0-9.]*) echo "release-gate: non-numeric toolchain version '$ver' — fail closed" >&2; return 2 ;;
	esac
	if ! version_ge "$ver" "$RELEASE_GO_FLOOR"; then
		echo "release-gate: binary built with Go $ver, below the floor $RELEASE_GO_FLOOR (known stdlib advisories) — refusing" >&2
		return 1
	fi
	command -v govulncheck >/dev/null 2>&1 || {
		echo "release-gate: govulncheck is not installed — fail closed (go install golang.org/x/vuln/cmd/govulncheck@latest)" >&2
		return 2
	}
	if ! govulncheck -mode=binary "$bin"; then
		echo "release-gate: govulncheck reported findings or could not scan $bin — refusing" >&2
		return 1
	fi
	echo "release-gate: OK — Go $ver >= $RELEASE_GO_FLOOR, govulncheck clean"
	return 0
}
