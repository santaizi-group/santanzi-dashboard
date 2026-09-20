#!/bin/sh

#========================================================
#   Santaizi Agent upgrade script
#   Replaces the binary and restarts the service; keeps config, identity, and WAL
#   Detects local Go / Rust agent and downloads from the matching repo
#   Go: SANTAIZI_AGENT_REPO      default santaizi-group/santaizi-agent
#   Rust: SANTAIZI_AGENT_RS_REPO default santaizi-group/santaizi-agent-rs
#   Override: SANTAIZI_AGENT_IMPL=go|rust
#========================================================

SANTAIZI_BASE_PATH="/opt/santaizi"
SANTAIZI_AGENT_PATH="${SANTAIZI_AGENT_PATH:-${SANTAIZI_BASE_PATH}/agent}"
SANTAIZI_AGENT_BIN="${SANTAIZI_AGENT_PATH}/santaizi-agent"
SANTAIZI_AGENT_RS_PATH="${SANTAIZI_AGENT_RS_PATH:-${SANTAIZI_BASE_PATH}/agent-rs}"
SANTAIZI_AGENT_RS_BIN="${SANTAIZI_AGENT_RS_PATH}/santaizi-agent-rs"
SANTAIZI_AGENT_CONFIG="${SANTAIZI_AGENT_CONFIG:-/etc/santaizi/agent.yaml}"
SANTAIZI_AGENT_REPO="${SANTAIZI_AGENT_REPO:-santaizi-group/santaizi-agent}"
SANTAIZI_AGENT_RS_REPO="${SANTAIZI_AGENT_RS_REPO:-santaizi-group/santaizi-agent-rs}"
SANTAIZI_AGENT_GO_UNIT="${SANTAIZI_AGENT_GO_UNIT:-santaizi-agent}"
SANTAIZI_AGENT_RS_UNIT="${SANTAIZI_AGENT_RS_UNIT:-santaizi-agent-rs}"

AGENT_IMPL=""
AGENT_BIN=""
AGENT_PATH=""
AGENT_REPO=""
AGENT_UNIT=""
AGENT_ASSET=""
DETECT_ONLY=0

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
    echo "Usage: $0 [upgrade_agent] [version] [--detect-only]"
    echo "Example: $0"
    echo "         $0 v1.0.1"
    echo "         SANTAIZI_AGENT_VERSION=v1.0.1 $0"
    echo "         SANTAIZI_AGENT_IMPL=rust $0"
    echo "Detects the local Go or Rust agent and installs the latest (or specified) GitHub Release from the matching repo."
    echo "Config, identity, and WAL are left unchanged. The script does not switch implementations."
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
    echo "$1" | tr -d '\r' | awk '{print $NF}' | sed -e 's/^v//' -e 's/-rs$//'
}

tag_name() {
    version=$1
    case "$version" in
        v*) echo "$version" ;;
        *) echo "v${version}" ;;
    esac
}

unit_live() {
    name=$1
    if ! command -v systemctl >/dev/null 2>&1; then
        return 1
    fi
    if sudo systemctl is-enabled "$name" >/dev/null 2>&1; then
        return 0
    fi
    if sudo systemctl is-active "$name" >/dev/null 2>&1; then
        return 0
    fi
    return 1
}

apply_impl() {
    AGENT_IMPL=$1
    case "$AGENT_IMPL" in
        rust)
            AGENT_BIN="$SANTAIZI_AGENT_RS_BIN"
            AGENT_PATH="$SANTAIZI_AGENT_RS_PATH"
            AGENT_REPO="$SANTAIZI_AGENT_RS_REPO"
            AGENT_UNIT="$SANTAIZI_AGENT_RS_UNIT"
            AGENT_ASSET="santaizi-agent-rs"
            ;;
        go)
            AGENT_BIN="$SANTAIZI_AGENT_BIN"
            AGENT_PATH="$SANTAIZI_AGENT_PATH"
            AGENT_REPO="$SANTAIZI_AGENT_REPO"
            AGENT_UNIT="$SANTAIZI_AGENT_GO_UNIT"
            AGENT_ASSET="santaizi-agent"
            ;;
        *)
            err "internal error: unknown implementation ${AGENT_IMPL}"
            exit 1
            ;;
    esac
}

detect_impl() {
    requested_impl=$(printf '%s' "${SANTAIZI_AGENT_IMPL:-}" | tr '[:upper:]' '[:lower:]')
    case "$requested_impl" in
        go|rust)
            apply_impl "$requested_impl"
            return
            ;;
        "")
            ;;
        *)
            err "SANTAIZI_AGENT_IMPL must be go or rust, got: ${SANTAIZI_AGENT_IMPL}"
            exit 1
            ;;
    esac

    rust_unit=0
    go_unit=0
    if unit_live "$SANTAIZI_AGENT_RS_UNIT"; then
        rust_unit=1
    fi
    if unit_live "$SANTAIZI_AGENT_GO_UNIT"; then
        go_unit=1
    fi

    if [ "$rust_unit" -eq 1 ] && [ "$go_unit" -eq 1 ]; then
        err "Both ${SANTAIZI_AGENT_GO_UNIT} and ${SANTAIZI_AGENT_RS_UNIT} are enabled or active."
        err "The same secret cannot run twice. Stop one of them, or set SANTAIZI_AGENT_IMPL=go|rust."
        exit 1
    fi
    if [ "$rust_unit" -eq 1 ]; then
        apply_impl rust
        return
    fi
    if [ "$go_unit" -eq 1 ]; then
        apply_impl go
        return
    fi
    if [ -e "$SANTAIZI_AGENT_RS_BIN" ]; then
        apply_impl rust
        return
    fi
    if [ -e "$SANTAIZI_AGENT_BIN" ]; then
        apply_impl go
        return
    fi

    err "Santaizi agent is not installed."
    err "Go: ${SANTAIZI_AGENT_BIN}"
    err "Rust: ${SANTAIZI_AGENT_RS_BIN}"
    err "Run the install script first, not this upgrade script."
    exit 1
}

current_version() {
    if [ ! -x "$AGENT_BIN" ]; then
        echo ""
        return
    fi
    if [ "$AGENT_IMPL" = "rust" ]; then
        sudo "$AGENT_BIN" --version 2>/dev/null | head -n 1 | tr -d '\r'
        return
    fi
    sudo "$AGENT_BIN" --config "$SANTAIZI_AGENT_CONFIG" --version 2>/dev/null | head -n 1 | tr -d '\r'
}

get_latest_version() {
    version=$(curl -fsSL -m 10 "https://api.github.com/repos/${AGENT_REPO}/releases/latest" | grep '"tag_name":' | head -n 1 | sed 's/.*"tag_name": "\(.*\)",.*/\1/')
    if [ -z "$version" ]; then
        err "Failed to get agent version, please check network connectivity to https://api.github.com/repos/${AGENT_REPO}/releases/latest"
        exit 1
    fi
    echo "$version"
}

require_installed() {
    if [ ! -e "$AGENT_BIN" ]; then
        err "Santaizi agent is not installed at ${AGENT_BIN}"
        err "Run the install script first, not this upgrade script."
        exit 1
    fi
    if [ ! -f "$SANTAIZI_AGENT_CONFIG" ]; then
        err "Missing config file: ${SANTAIZI_AGENT_CONFIG}"
        err "Upgrade does not write secrets; finish installation first."
        exit 1
    fi
}

require_rust_platform() {
    os=$1
    arch=$2
    if [ "$AGENT_IMPL" != "rust" ]; then
        return
    fi
    if [ "$os" = "linux" ] && { [ "$arch" = "amd64" ] || [ "$arch" = "arm64" ]; }; then
        return
    fi
    err "Rust agent supports Linux amd64 / arm64 only, current: $(uname) / $(uname -m)"
    err "Switch implementations with the install script, not this upgrade script."
    exit 1
}

stop_agent() {
    if [ "$AGENT_IMPL" = "rust" ]; then
        sudo systemctl stop "$AGENT_UNIT" >/dev/null 2>&1 || true
        return
    fi
    sudo "$AGENT_BIN" service stop >/dev/null 2>&1 || true
    sudo systemctl stop "$AGENT_UNIT" >/dev/null 2>&1 || true
}

start_agent() {
    if [ "$AGENT_IMPL" = "rust" ]; then
        if sudo systemctl restart "$AGENT_UNIT" >/dev/null 2>&1; then
            return 0
        fi
        if sudo systemctl start "$AGENT_UNIT" >/dev/null 2>&1; then
            return 0
        fi
        err "Binary replaced, but the service failed to start. Run: systemctl start ${AGENT_UNIT}"
        exit 1
    fi
    if sudo "$AGENT_BIN" service restart >/dev/null 2>&1; then
        return 0
    fi
    if sudo "$AGENT_BIN" service start >/dev/null 2>&1; then
        return 0
    fi
    err "Binary replaced, but the service failed to start. Run: ${AGENT_BIN} service start"
    exit 1
}

replace_binary() {
    os=$1
    arch=$2
    version=$3

    tmpdir=$(mktemp -d 2>/dev/null || mktemp -d -t santaizi-agent-upgrade)
    tmpfile="${tmpdir}/${AGENT_ASSET}_${os}_${arch}.zip"
    url="https://github.com/${AGENT_REPO}/releases/download/${version}/${AGENT_ASSET}_${os}_${arch}.zip"

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
    if [ ! -f "${tmpdir}/${AGENT_ASSET}" ]; then
        rm -rf "$tmpdir"
        err "Archive does not contain ${AGENT_ASSET}."
        exit 1
    fi

    info "Replacing ${AGENT_BIN} ..."
    sudo mkdir -p "$AGENT_PATH"
    sudo mv "${tmpdir}/${AGENT_ASSET}" "${AGENT_BIN}.new"
    sudo chmod +x "${AGENT_BIN}.new"
    sudo mv "${AGENT_BIN}.new" "$AGENT_BIN"
    rm -rf "$tmpdir"
}

if [ "${SANTAIZI_AGENT_DETECT_ONLY:-}" = "1" ]; then
    DETECT_ONLY=1
fi

while [ $# -gt 0 ]; do
    case "$1" in
        -h|--help)
            usage
            exit 0
            ;;
        upgrade_agent|upgrade)
            shift
            ;;
        --detect-only)
            DETECT_ONLY=1
            shift
            ;;
        *)
            break
            ;;
    esac
done

if [ "${1:-}" = "-h" ] || [ "${1:-}" = "--help" ]; then
    usage
    exit 0
fi

requested="${1:-${SANTAIZI_AGENT_VERSION:-}}"
if [ $# -gt 1 ]; then
    usage
    exit 1
fi

detect_impl

if [ "$DETECT_ONLY" -eq 1 ]; then
    printf '%s\n' "$AGENT_IMPL"
    exit 0
fi

if [ "$AGENT_IMPL" = "rust" ]; then
    info "Detected implementation: Rust"
else
    info "Detected implementation: Go"
fi

require_installed
deps_check

os=$(detect_os)
arch=$(detect_arch)
if [ "$os" = "unknown" ] || [ "$arch" = "unknown" ]; then
    err "Unsupported OS or architecture: $(uname) / $(uname -m)"
    exit 1
fi
require_rust_platform "$os" "$arch"

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
