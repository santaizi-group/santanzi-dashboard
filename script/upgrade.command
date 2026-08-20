#!/bin/sh

# Santaizi Agent macOS upgrade delegates to the shared POSIX upgrade script.

set -eu

SANTAIZI_UPGRADE_SCRIPT_URL="${SANTAIZI_UPGRADE_SCRIPT_URL:-https://raw.githubusercontent.com/santaizi-group/santanzi-dashboard/main/script/upgrade_agent.sh}"
SANTAIZI_UPGRADE_TMP="$(mktemp -t santaizi-agent-upgrade.XXXXXX)"
trap 'rm -f "$SANTAIZI_UPGRADE_TMP"' EXIT HUP INT TERM

curl -fsSL --retry 2 --retry-max-time 60 "$SANTAIZI_UPGRADE_SCRIPT_URL" -o "$SANTAIZI_UPGRADE_TMP"
/bin/sh "$SANTAIZI_UPGRADE_TMP" "$@"
