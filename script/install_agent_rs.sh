#!/bin/sh

#========================================================
#   Santaizi Agent (Rust) 一键安装脚本
#   默认从 santaizi-group/santaizi-agent-rs 下载，可通过 SANTAIZI_AGENT_RS_REPO 覆盖
#   仅 Linux amd64 / arm64；自行写 yaml 与 systemd（Rust 探针无 service install）
#========================================================

SANTAIZI_BASE_PATH="/opt/santaizi"
SANTAIZI_AGENT_RS_PATH="${SANTAIZI_BASE_PATH}/agent-rs"
SANTAIZI_AGENT_RS_BIN="${SANTAIZI_AGENT_RS_PATH}/santaizi-agent-rs"
SANTAIZI_AGENT_RS_UNIT="/etc/systemd/system/santaizi-agent-rs.service"
SANTAIZI_AGENT_YAML="/etc/santaizi/agent.yaml"
SANTAIZI_AGENT_DATA="/var/lib/santaizi-agent"

red='\033[0;31m'
green='\033[0;32m'
yellow='\033[0;33m'
plain='\033[0m'

SANTAIZI_AGENT_RS_REPO="${SANTAIZI_AGENT_RS_REPO:-santaizi-group/santaizi-agent-rs}"
CLEAN_INSTALL=0
CLEAN_INSTALL_CONFIRMED=0
USE_TLS=0

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
            err "ERROR: 当前非 root 且未安装 sudo，无法继续。"
            exit 1
        fi
    else
        "$@"
    fi
}

yaml_escape() {
    printf "%s" "$1" | sed "s/'/''/g"
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
        err "缺少依赖:${missing}，请先安装后再试。"
        exit 1
    fi
}

detect_os() {
    system=$(uname)
    case "$system" in
        *Linux*) echo "linux" ;;
        *) echo "unknown" ;;
    esac
}

detect_arch() {
    mach=$(uname -m)
    case "$mach" in
        amd64|x86_64) echo "amd64" ;;
        aarch64|arm64) echo "arm64" ;;
        *) echo "unknown" ;;
    esac
}

get_latest_version() {
    version=$(curl -fsSL -m 10 "https://api.github.com/repos/${SANTAIZI_AGENT_RS_REPO}/releases/latest" | grep '"tag_name":' | head -n 1 | sed 's/.*"tag_name": "\(.*\)",.*/\1/')
    if [ -z "$version" ]; then
        err "获取 Rust 探针版本失败，请检查网络是否能访问 https://api.github.com/repos/${SANTAIZI_AGENT_RS_REPO}/releases/latest"
        err "需要该仓库已发布 v* Release（linux amd64 / arm64）。"
        exit 1
    fi
    echo "$version"
}

stop_go_agent() {
    if [ -x /opt/santaizi/agent/santaizi-agent ]; then
        sudo /opt/santaizi/agent/santaizi-agent service uninstall >/dev/null 2>&1 || true
    fi
    sudo systemctl stop santaizi-agent >/dev/null 2>&1 || true
    sudo systemctl disable santaizi-agent >/dev/null 2>&1 || true
}

stop_rust_agent() {
    sudo systemctl stop santaizi-agent-rs >/dev/null 2>&1 || true
    sudo systemctl disable santaizi-agent-rs >/dev/null 2>&1 || true
}

prepare_clean_install() {
    if [ "$CLEAN_INSTALL" -ne 1 ]; then
        return
    fi
    if [ "$CLEAN_INSTALL_CONFIRMED" -ne 1 ]; then
        err "清洁安装会删除现有 Agent 配置、身份和 WAL；请同时传入 --confirm-clean-install。"
        exit 1
    fi

    info "正在执行已确认的清洁安装..."
    stop_go_agent
    stop_rust_agent
    sudo rm -rf /opt/santaizi/agent /opt/santaizi/agent-rs "$SANTAIZI_AGENT_DATA"
    sudo rm -f "$SANTAIZI_AGENT_YAML" /etc/systemd/system/santaizi-agent.service "$SANTAIZI_AGENT_RS_UNIT"
    sudo rm -f /usr/local/bin/santaizi-agent-uninstall /usr/bin/santaizi-agent-uninstall

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
    success "现有 Agent 与旧版 nezha-agent 数据已清理，将生成新的节点身份。"
}

install_agent_rs() {
    deps_check

    os=$(detect_os)
    arch=$(detect_arch)
    if [ "$os" != "linux" ] || [ "$arch" = "unknown" ]; then
        err "Rust 探针安装脚本仅支持 Linux amd64 / arm64，当前: $(uname) / $(uname -m)"
        exit 1
    fi

    info "正在获取 Rust 探针最新版本..."
    version=$(get_latest_version)
    success "最新版本: ${version}"

    tmpfile="/tmp/santaizi-agent-rs_${os}_${arch}.zip"
    url="https://github.com/${SANTAIZI_AGENT_RS_REPO}/releases/download/${version}/santaizi-agent-rs_${os}_${arch}.zip"

    info "正在下载 ${url} ..."
    if ! curl -fsSL -m 60 -o "$tmpfile" "$url"; then
        err "下载 Rust 探针失败，请检查网络连接，并确认该版本已发布 linux ${arch} 产物。"
        exit 1
    fi

    info "正在安装到 ${SANTAIZI_AGENT_RS_PATH} ..."
    sudo mkdir -p "$SANTAIZI_AGENT_RS_PATH"
    sudo mkdir -p /etc/santaizi "$SANTAIZI_AGENT_DATA"
    sudo unzip -qo "$tmpfile" -d "$SANTAIZI_AGENT_RS_PATH" || {
        err "解压 Rust 探针失败。"
        rm -f "$tmpfile"
        exit 1
    }
    rm -f "$tmpfile"
    if [ ! -f "$SANTAIZI_AGENT_RS_BIN" ]; then
        err "解压后未找到 ${SANTAIZI_AGENT_RS_BIN}"
        exit 1
    fi
    sudo chmod +x "$SANTAIZI_AGENT_RS_BIN"
}

write_agent_yaml() {
    endpoint=$1
    secret=$2
    tls_value=$3
    escaped=$(yaml_escape "$secret")
    tmp=$(mktemp)
    cat > "$tmp" <<EOF
server: '${endpoint}'
client_secret: '${escaped}'
tls: ${tls_value}
protocol: "v2"
telemetry:
  data_dir: "${SANTAIZI_AGENT_DATA}"
EOF
    sudo mv "$tmp" "$SANTAIZI_AGENT_YAML"
    sudo chmod 0600 "$SANTAIZI_AGENT_YAML"
}

write_unit() {
    tmp=$(mktemp)
    cat > "$tmp" <<EOF
[Unit]
Description=Santaizi Agent (Rust)
After=network-online.target
Wants=network-online.target

[Service]
Type=simple
ExecStart=${SANTAIZI_AGENT_RS_BIN} --config ${SANTAIZI_AGENT_YAML}
Restart=always
RestartSec=5
LimitNOFILE=1048576

[Install]
WantedBy=multi-user.target
EOF
    sudo mv "$tmp" "$SANTAIZI_AGENT_RS_UNIT"
    sudo chmod 0644 "$SANTAIZI_AGENT_RS_UNIT"
}

configure_agent() {
    if [ $# -lt 3 ]; then
        err "参数不足，用法: $0 install_agent <服务器地址> <端口> <密钥> [--clean-install --confirm-clean-install] [--tls]"
        exit 1
    fi

    host=$1
    port=$2
    secret=$3

    case "$host" in
        \[*\]) endpoint="${host}:${port}" ;;
        *:*) endpoint="[${host}]:${port}" ;;
        *) endpoint="${host}:${port}" ;;
    esac

    tls_value="false"
    if [ "$USE_TLS" -eq 1 ]; then
        tls_value="true"
    fi

    info "正在写入配置并启动 Rust 探针..."
    stop_go_agent
    stop_rust_agent
    write_agent_yaml "$endpoint" "$secret" "$tls_value"
    write_unit
    sudo systemctl daemon-reload
    if ! sudo systemctl enable --now santaizi-agent-rs; then
        err "启动 santaizi-agent-rs 失败。"
        exit 1
    fi
    success "Rust 探针安装完成。"
}

if [ "${1:-}" = "install_agent" ]; then
    shift
fi

if [ $# -lt 3 ]; then
    echo "用法: $0 [install_agent] <服务器地址> <端口> <密钥> [--clean-install --confirm-clean-install] [--tls]"
    echo "示例: $0 install_agent grpc.example.com 5555 abcdef --clean-install --confirm-clean-install --tls"
    echo "仅支持 Linux amd64 / arm64。仓库可用 SANTAIZI_AGENT_RS_REPO 覆盖。"
    exit 1
fi

install_host=$1
install_port=$2
install_secret=$3
shift 3

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
        --tls)
            USE_TLS=1
            shift
            ;;
        *)
            err "Rust 探针不支持参数: $1"
            exit 1
            ;;
    esac
done

prepare_clean_install
install_agent_rs
configure_agent "$install_host" "$install_port" "$install_secret"
