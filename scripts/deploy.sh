#!/bin/sh
# Autodeploy (owner 2026-09-08): build -> release gate -> install -> restart
# -> verify the sealed capability line. The gate (toolchain floor +
# govulncheck, scripts/lib/release-gate.sh) runs BEFORE the running service
# is touched: a refused binary leaves the installed one and the service
# exactly as they were. Usage: scripts/deploy.sh   (from any cwd)
#   NEXUS_INSTALL_TO   install target (default ~/bin/nexus)
#   NEXUS_SERVICE      systemd --user unit (default nexus)
#   NEXUS_DEPLOY_SETTLE_SECONDS  wait before the journal check (default 3)
set -eu
ROOT="$(cd "$(dirname "$0")/.." && pwd)"
. "$ROOT/scripts/lib/release-gate.sh"
WORK="$(mktemp -d)"
trap 'rm -rf "$WORK"' EXIT
BIN="$WORK/nexus"
TARGET="${NEXUS_INSTALL_TO:-$HOME/bin/nexus}"
SERVICE="${NEXUS_SERVICE:-nexus}"
SETTLE="${NEXUS_DEPLOY_SETTLE_SECONDS:-3}"
cd "$ROOT"
CGO_ENABLED=0 go build -buildvcs=false -o "$BIN" "$ROOT/cmd/nexus"
release_gate "$BIN"
echo "deploy: gate passed, sha256=$(sha256sum "$BIN" | cut -d' ' -f1)"
systemctl --user stop "$SERVICE"
install -m 0755 "$BIN" "$TARGET"
systemctl --user start "$SERVICE"
sleep "$SETTLE"
if journalctl --user -u "$SERVICE" -n 50 --no-pager 2>/dev/null | grep -q "sealed capability ON"; then
	echo "deploy: OK — $TARGET installed, $SERVICE restarted, capability ON observed"
	exit 0
fi
echo "deploy: $SERVICE restarted but no 'sealed capability ON' line in the last 50 journal lines — inspect: journalctl --user -u $SERVICE -n 50" >&2
exit 1
