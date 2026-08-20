#!/bin/sh

#========================================================
#   Santaizi Agent 通用升级脚本
#   只替换二进制并重启服务；保留配置、节点身份和 WAL
#   默认从 santaizi-group/santaizi-agent 下载，可通过 SANTAIZI_AGENT_REPO 覆盖
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
    echo "用法: $0 [upgrade_agent] [版本]"
    echo "示例: $0"
    echo "      $0 v1.0.1"
    echo "      SANTAIZI_AGENT_VERSION=v1.0.1 $0"
    echo "未指定版本时安装 GitHub 最新 Release。不修改配置、身份和 WAL。"
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
        err "获取 Agent 版本失败，请检查网络是否能访问 https://api.github.com/repos/${SANTAIZI_AGENT_REPO}/releases/latest"
        exit 1
    fi
    echo "$version"
}

require_installed() {
    if [ ! -e "$SANTAIZI_AGENT_BIN" ]; then
        err "未找到已安装的探针: ${SANTAIZI_AGENT_BIN}"
        err "请先执行安装脚本，而不是升级脚本。"
        exit 1
    fi
    if [ ! -f "$SANTAIZI_AGENT_CONFIG" ]; then
        err "未找到配置文件: ${SANTAIZI_AGENT_CONFIG}"
        err "升级不会写入密钥；请先完成安装。"
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
    err "二进制已替换，但未能启动服务。请执行: ${SANTAIZI_AGENT_BIN} service start"
    exit 1
}

replace_binary() {
    os=$1
    arch=$2
    version=$3

    tmpdir=$(mktemp -d 2>/dev/null || mktemp -d -t santaizi-agent-upgrade)
    tmpfile="${tmpdir}/santaizi-agent_${os}_${arch}.zip"
    url="https://github.com/${SANTAIZI_AGENT_REPO}/releases/download/${version}/santaizi-agent_${os}_${arch}.zip"

    info "正在下载 ${url} ..."
    if ! curl -fsSL -m 120 -o "$tmpfile" "$url"; then
        rm -rf "$tmpdir"
        err "下载 Agent 失败，请确认该版本已发布且网络可访问 GitHub。"
        exit 1
    fi

    if ! unzip -qo "$tmpfile" -d "$tmpdir"; then
        rm -rf "$tmpdir"
        err "解压 Agent 失败。"
        exit 1
    fi
    if [ ! -f "${tmpdir}/santaizi-agent" ]; then
        rm -rf "$tmpdir"
        err "压缩包中未找到 santaizi-agent。"
        exit 1
    fi

    info "正在替换 ${SANTAIZI_AGENT_BIN} ..."
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
    err "不支持的操作系统或架构: $(uname) / $(uname -m)"
    exit 1
fi

if [ -n "$requested" ]; then
    target=$(tag_name "$requested")
else
    info "正在获取 Agent 最新版本..."
    target=$(get_latest_version)
fi

installed=$(current_version)
if [ -n "$installed" ]; then
    info "当前版本: ${installed}"
fi
success "目标版本: ${target}"

if [ -z "$requested" ] && [ -n "$installed" ] && [ "$(normalize_version "$installed")" = "$(normalize_version "$target")" ]; then
    success "已是目标版本，无需下载。"
    exit 0
fi

stop_agent
replace_binary "$os" "$arch" "$target"
start_agent

installed=$(current_version)
if [ -n "$installed" ]; then
    success "探针已升级到 ${installed}。"
else
    success "探针已升级到 ${target}。"
fi
