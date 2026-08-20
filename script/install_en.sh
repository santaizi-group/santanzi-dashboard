#!/bin/sh

#========================================================
# Install script wrapper
# Reads the actual install script URL from SANTAIZI_SCRIPT_URL
# Defaults to this repo's agent-only install script
#========================================================

shell_url="${SANTAIZI_SCRIPT_URL:-https://raw.githubusercontent.com/santaizi-group/santanzi-dashboard/main/script/install_agent_en.sh}"

if command -v wget >/dev/null 2>&1; then
    wget -O santaizi_v0.sh "$shell_url"
elif command -v curl >/dev/null 2>&1; then
    curl -fsSL -o santaizi_v0.sh "$shell_url"
else
    echo "Error: wget or curl not found, please install one of them first"
    exit 1
fi

chmod +x santaizi_v0.sh

# run new script with original parameters
exec ./santaizi_v0.sh "$@"
