#!/bin/sh

#========================================================
#   Santaizi Agent upgrade script
#   Replaces the binary and restarts the service; keeps config, identity, and WAL
#   Default repo: santaizi-group/santaizi-agent, override via SANTAIZI_AGENT_REPO
#========================================================

SANTAIZI_BASE_PATH="/opt/santaizi"
SANTAIZI_AGENT_PATH="${SANTAIZI_AGENT_PATH:-${SANTAIZI_BASE_PATH}/agent}"
SANTAIZI_AGENT_BIN="${SANTAIZI_AGENT_PATH}/santaizi-agent"
SANTAIZI_AGENT_CONFIG="${SANTAIZI_AGENT_CONFIG:-/etc/santaizi/agent.yaml}"
SANTAIZI_AGENT_REPO="${SANTAIZI_AGENT_REPO:-santaizi-group/santaizi-agent}"

red='\033[0;31m'
green='\033[0;32m'
yellow='\033[0;33m'
plain='\033[0m'

err() {
    printf "${red}%s${plain}\n" "$*" >&2
}

info() {
    printf "${yellow}%s${plain}\n" "$*"
}

success() {
    printf "${green}%s${plain}\n" "$*"
}

usage() {
    echo "Usage: $0 [upgrade_agent] [version]"
    echo "Example: $0"
    echo "         $0 v1.0.1"
    echo "         SANTAIZI_AGENT_VERSION=v1.0.1 $0"
    echo "With no version argument, installs the latest GitHub Release. Config, identity, and WAL are left unchanged."
}

sudo() {
    myEUID=$(id -ru)
    if [ "$myEUID" -ne 0 ]; then
        if command -v sudo > /dev/null 2>&1; then
            command sudo "$@"
        else
            err "ERROR: not running as root and sudo is not installed."
            exit 1
        fi
    else
        "$@"
    fi
}

deps_check() {
    deps="curl unzip"
    missing=""
    for dep in $deps; do
        if ! command -v "$dep" >/dev/null 2>&1; then
            missing="${missing} ${dep}"
        fi
    done
    if [ -n "$missing" ]; then
        err "Missing dependencies:${missing}, please install them first."
        exit 1
    fi
}

detect_os() {
    system=$(uname)
    case "$system" in
        *Linux*) echo "linux" ;;
        *Darwin*) echo "darwin" ;;
        *FreeBSD*) echo "freebsd" ;;
        *) echo "unknown" ;;
    esac
}

detect_arch() {
    mach=$(uname -m)
    case "$mach" in
        amd64|x86_64) echo "amd64" ;;
        i386|i686) echo "386" ;;
        aarch64|arm64) echo "arm64" ;;
        *arm*) echo "arm" ;;
        s390x) echo "s390x" ;;
        riscv64) echo "riscv64" ;;
        mips) echo "mips" ;;
        mipsel|mipsle) echo "mipsle" ;;
        *) echo "unknown" ;;
    esac
}

normalize_version() {
    echo "$1" | sed 's/^v//'
}

tag_name() {
    version=$1
    case "$version" in
        v*) echo "$version" ;;
        *) echo "v${version}" ;;
    esac
}

current_version() {
    if [ ! -x "$SANTAIZI_AGENT_BIN" ]; then
        echo ""
        return
    fi
    sudo "$SANTAIZI_AGENT_BIN" --config "$SANTAIZI_AGENT_CONFIG" --version 2>/dev/null | head -n 1 | tr -d '\r'
}

get_latest_version() {
    version=$(curl -fsSL -m 10 "https://api.github.com/repos/${SANTAIZI_AGENT_REPO}/releases/latest" | grep '"tag_name":' | head -n 1 | sed 's/.*"tag_name": "\(.*\)",.*/\1/')
    if [ -z "$version" ]; then
        err "Failed to get agent version, please check network connectivity to https://api.github.com/repos/${SANTAIZI_AGENT_REPO}/releases/latest"
        exit 1
    fi
    echo "$version"
}

require_installed() {
    if [ ! -e "$SANTAIZI_AGENT_BIN" ]; then
        err "Santaizi agent is not installed at ${SANTAIZI_AGENT_BIN}"
        err "Run the install script first, not this upgrade script."
        exit 1
    fi
    if [ ! -f "$SANTAIZI_AGENT_CONFIG" ]; then
        err "Missing config file: ${SANTAIZI_AGENT_CONFIG}"
        err "Upgrade does not write secrets; finish installation first."
        exit 1
    fi
}

stop_agent() {
    sudo "$SANTAIZI_AGENT_BIN" service stop >/dev/null 2>&1 || true
    sudo systemctl stop santaizi-agent >/dev/null 2>&1 || true
}

start_agent() {
    if sudo "$SANTAIZI_AGENT_BIN" service restart >/dev/null 2>&1; then
        return 0
    fi
    if sudo "$SANTAIZI_AGENT_BIN" service start >/dev/null 2>&1; then
        return 0
    fi
    err "Binary replaced, but the service failed to start. Run: ${SANTAIZI_AGENT_BIN} service start"
    exit 1
}

replace_binary() {
    os=$1
    arch=$2
    version=$3

    tmpdir=$(mktemp -d 2>/dev/null || mktemp -d -t santaizi-agent-upgrade)
    tmpfile="${tmpdir}/santaizi-agent_${os}_${arch}.zip"
    url="https://github.com/${SANTAIZI_AGENT_REPO}/releases/download/${version}/santaizi-agent_${os}_${arch}.zip"

    info "Downloading ${url} ..."
    if ! curl -fsSL -m 120 -o "$tmpfile" "$url"; then
        rm -rf "$tmpdir"
        err "Failed to download agent. Confirm the release exists and GitHub is reachable."
        exit 1
    fi

    if ! unzip -qo "$tmpfile" -d "$tmpdir"; then
        rm -rf "$tmpdir"
        err "Failed to extract agent."
        exit 1
    fi
    if [ ! -f "${tmpdir}/santaizi-agent" ]; then
        rm -rf "$tmpdir"
        err "Archive does not contain santaizi-agent."
        exit 1
    fi

    info "Replacing ${SANTAIZI_AGENT_BIN} ..."
    sudo mkdir -p "$SANTAIZI_AGENT_PATH"
    sudo mv "${tmpdir}/santaizi-agent" "${SANTAIZI_AGENT_BIN}.new"
    sudo chmod +x "${SANTAIZI_AGENT_BIN}.new"
    sudo mv "${SANTAIZI_AGENT_BIN}.new" "$SANTAIZI_AGENT_BIN"
    rm -rf "$tmpdir"
}

if [ "${1:-}" = "-h" ] || [ "${1:-}" = "--help" ]; then
    usage
    exit 0
fi

if [ "${1:-}" = "upgrade_agent" ] || [ "${1:-}" = "upgrade" ]; then
    shift
fi

if [ "${1:-}" = "-h" ] || [ "${1:-}" = "--help" ]; then
    usage
    exit 0
fi

requested="${1:-${SANTAIZI_AGENT_VERSION:-}}"
if [ $# -gt 1 ]; then
    usage
    exit 1
fi

require_installed
deps_check

os=$(detect_os)
arch=$(detect_arch)
if [ "$os" = "unknown" ] || [ "$arch" = "unknown" ]; then
    err "Unsupported OS or architecture: $(uname) / $(uname -m)"
    exit 1
fi

if [ -n "$requested" ]; then
    target=$(tag_name "$requested")
else
    info "Getting latest agent version..."
    target=$(get_latest_version)
fi

installed=$(current_version)
if [ -n "$installed" ]; then
    info "Current version: ${installed}"
fi
success "Target version: ${target}"

if [ -z "$requested" ] && [ -n "$installed" ] && [ "$(normalize_version "$installed")" = "$(normalize_version "$target")" ]; then
    success "Already at the target version."
    exit 0
fi

stop_agent
replace_binary "$os" "$arch" "$target"
start_agent

installed=$(current_version)
if [ -n "$installed" ]; then
    success "Agent upgraded to ${installed}."
else
    success "Agent upgraded to ${target}."
fi
