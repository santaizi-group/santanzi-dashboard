#!/bin/sh

#========================================================
#   Santaizi Agent One-Key Install Script
#   Default repo: santaizi-group/santaizi-agent, override via SANTAIZI_AGENT_REPO
#========================================================

SANTAIZI_BASE_PATH="/opt/santaizi"
SANTAIZI_AGENT_PATH="${SANTAIZI_BASE_PATH}/agent"

red='\033[0;31m'
green='\033[0;32m'
yellow='\033[0;33m'
plain='\033[0m'

SANTAIZI_AGENT_REPO="${SANTAIZI_AGENT_REPO:-santaizi-group/santaizi-agent}"
CLEAN_INSTALL=0
CLEAN_INSTALL_CONFIRMED=0

err() {
    printf "${red}%s${plain}\n" "$*" >&2
}

info() {
    printf "${yellow}%s${plain}\n" "$*"
}

success() {
    printf "${green}%s${plain}\n" "$*"
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
    local deps="curl unzip"
    local missing=""
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

get_latest_version() {
    version=$(curl -fsSL -m 10 "https://api.github.com/repos/${SANTAIZI_AGENT_REPO}/releases/latest" | grep '"tag_name":' | head -n 1 | sed 's/.*"tag_name": "\(.*\)",.*/\1/')
    if [ -z "$version" ]; then
        err "Failed to get agent version, please check network connectivity to https://api.github.com/repos/${SANTAIZI_AGENT_REPO}/releases/latest"
        exit 1
    fi
    echo "$version"
}

install_agent() {
    deps_check

    os=$(detect_os)
    arch=$(detect_arch)
    if [ "$os" = "unknown" ] || [ "$arch" = "unknown" ]; then
        err "Unsupported OS or architecture: $(uname) / $(uname -m)"
        exit 1
    fi

    info "Getting latest agent version..."
    version=$(get_latest_version)
    success "Latest version: ${version}"

    tmpfile="/tmp/santaizi-agent_${os}_${arch}.zip"
    url="https://github.com/${SANTAIZI_AGENT_REPO}/releases/download/${version}/santaizi-agent_${os}_${arch}.zip"

    info "Downloading ${url} ..."
    if ! curl -fsSL -m 60 -o "$tmpfile" "$url"; then
        err "Failed to download agent, please check your network connection."
        exit 1
    fi

    info "Installing to ${SANTAIZI_AGENT_PATH} ..."
    sudo mkdir -p "$SANTAIZI_AGENT_PATH"
    sudo mkdir -p /etc/santaizi /var/lib/santaizi-agent
    sudo unzip -qo "$tmpfile" -d "$SANTAIZI_AGENT_PATH" || {
        err "Failed to extract agent."
        rm -f "$tmpfile"
        exit 1
    }
    rm -f "$tmpfile"
    sudo chmod +x "${SANTAIZI_AGENT_PATH}/santaizi-agent"
}

prepare_clean_install() {
    if [ "$CLEAN_INSTALL" -ne 1 ]; then
        return
    fi
    if [ "$CLEAN_INSTALL_CONFIRMED" -ne 1 ]; then
        err "Clean install removes the existing agent configuration, identity, and WAL; also pass --confirm-clean-install."
        exit 1
    fi

    info "Running the confirmed clean installation..."
    if [ -x /opt/santaizi/agent/santaizi-agent ]; then
        sudo /opt/santaizi/agent/santaizi-agent service uninstall >/dev/null 2>&1 || true
    fi
    sudo systemctl stop santaizi-agent >/dev/null 2>&1 || true
    sudo systemctl disable santaizi-agent >/dev/null 2>&1 || true
    sudo rm -rf /opt/santaizi/agent /var/lib/santaizi-agent
    sudo rm -f /etc/santaizi/agent.yaml /etc/systemd/system/santaizi-agent.service
    sudo rm -f /usr/local/bin/santaizi-agent-uninstall /usr/bin/santaizi-agent-uninstall

    # Migration cleanup for the legacy upstream agent (nezha-agent).
    if [ -x /opt/nezha/agent/nezha-agent ]; then
        if [ -d /opt/nezha/agent ]; then
            for cfg in /opt/nezha/agent/config.yml /opt/nezha/agent/config*.yml; do
                [ -f "$cfg" ] || continue
                sudo /opt/nezha/agent/nezha-agent service -c "$cfg" uninstall >/dev/null 2>&1 || true
            done
        fi
        sudo /opt/nezha/agent/nezha-agent service uninstall >/dev/null 2>&1 || true
    fi
    sudo systemctl stop nezha-agent >/dev/null 2>&1 || true
    sudo systemctl disable nezha-agent >/dev/null 2>&1 || true
    sudo rm -rf /opt/nezha/agent /etc/nezha
    sudo rm -f /etc/systemd/system/nezha-agent.service /lib/systemd/system/nezha-agent.service
    sudo rm -f /Library/LaunchDaemons/com.nezha.agent.plist ~/Library/LaunchAgents/com.nezha.agent.plist 2>/dev/null || true

    sudo systemctl daemon-reload >/dev/null 2>&1 || true
    success "Previous Santaizi agent and legacy nezha-agent data removed; a new node identity will be generated."
}

configure_agent() {
    if [ $# -lt 3 ]; then
        err "Insufficient arguments, usage: $0 install_agent <server> <port> <secret> [extra args]"
        exit 1
    fi

    host=$1
    port=$2
    secret=$3
    shift 3

    case "$host" in
        \[*\]) endpoint="${host}:${port}" ;;
        *:*) endpoint="[${host}]:${port}" ;;
        *) endpoint="${host}:${port}" ;;
    esac

    info "Configuring and starting agent service..."
    sudo "${SANTAIZI_AGENT_PATH}/santaizi-agent" service uninstall >/dev/null 2>&1 || true
    if ! sudo "${SANTAIZI_AGENT_PATH}/santaizi-agent" service install --config /etc/santaizi/agent.yaml --data-dir /var/lib/santaizi-agent -s "$endpoint" -p "$secret" "$@"; then
        err "Failed to install agent service."
        exit 1
    fi
    success "Agent installed successfully."
}

# Main entry
if [ "${1:-}" = "install_agent" ]; then
    shift
fi

if [ $# -lt 3 ]; then
    echo "Usage: $0 [install_agent] <server> <port> <secret> [--clean-install --confirm-clean-install] [agent args]"
    echo "Example: $0 install_agent grpc.example.com 5555 abcdef --clean-install --confirm-clean-install --tls --server-ip 1.2.3.4"
    exit 1
fi

install_host=$1
install_port=$2
install_secret=$3
shift 3

# Clean-install switches must immediately follow the secret so they are not forwarded to the agent.
while [ $# -gt 0 ]; do
    case "$1" in
        --clean-install)
            CLEAN_INSTALL=1
            shift
            ;;
        --confirm-clean-install)
            CLEAN_INSTALL_CONFIRMED=1
            shift
            ;;
        *)
            break
            ;;
    esac
done

prepare_clean_install
install_agent
configure_agent "$install_host" "$install_port" "$install_secret" "$@"
