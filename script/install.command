#!/bin/sh

# Santaizi Agent macOS installer delegates to the shared POSIX installer so
# Linux and macOS use exactly the same clean-install and capability semantics.

set -eu

SANTAIZI_INSTALL_SCRIPT_URL="${SANTAIZI_INSTALL_SCRIPT_URL:-https://raw.githubusercontent.com/santaizi-group/santanzi-dashboard/main/script/install_agent.sh}"
SANTAIZI_INSTALL_TMP="$(mktemp -t santaizi-agent-install.XXXXXX)"
trap 'rm -f "$SANTAIZI_INSTALL_TMP"' EXIT HUP INT TERM

curl -fsSL --retry 2 --retry-max-time 60 "$SANTAIZI_INSTALL_SCRIPT_URL" -o "$SANTAIZI_INSTALL_TMP"
/bin/sh "$SANTAIZI_INSTALL_TMP" "$@"
