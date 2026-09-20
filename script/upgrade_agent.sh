#!/bin/sh

#========================================================
#   Santaizi Agent 通用升级脚本
#   只替换二进制并重启服务；保留配置、节点身份和 WAL
#   自动识别本机 Go / Rust 探针，从对应仓库下载
#   Go：SANTAIZI_AGENT_REPO    默认 santaizi-group/santaizi-agent
#   Rust：SANTAIZI_AGENT_RS_REPO 默认 santaizi-group/santaizi-agent-rs
#   覆盖实现：SANTAIZI_AGENT_IMPL=go|rust
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
    echo "用法: $0 [upgrade_agent] [版本] [--detect-only]"
    echo "示例: $0"
    echo "      $0 v1.0.1"
    echo "      SANTAIZI_AGENT_VERSION=v1.0.1 $0"
    echo "      SANTAIZI_AGENT_IMPL=rust $0"
    echo "自动识别本机 Go 或 Rust 探针，从对应仓库安装 GitHub 最新（或指定）Release。"
    echo "不修改配置、身份和 WAL，也不会在升级时切换实现。"
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
            err "内部错误：未知实现 ${AGENT_IMPL}"
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
            err "SANTAIZI_AGENT_IMPL 只能是 go 或 rust，当前: ${SANTAIZI_AGENT_IMPL}"
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
        err "同时发现 ${SANTAIZI_AGENT_GO_UNIT} 与 ${SANTAIZI_AGENT_RS_UNIT} 处于 enabled 或 active。"
        err "同一密钥双连会冲突。请停掉其中一个，或设置 SANTAIZI_AGENT_IMPL=go|rust。"
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

    err "未找到已安装的探针。"
    err "Go: ${SANTAIZI_AGENT_BIN}"
    err "Rust: ${SANTAIZI_AGENT_RS_BIN}"
    err "请先执行安装脚本，而不是升级脚本。"
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
        err "获取 Agent 版本失败，请检查网络是否能访问 https://api.github.com/repos/${AGENT_REPO}/releases/latest"
        exit 1
    fi
    echo "$version"
}

require_installed() {
    if [ ! -e "$AGENT_BIN" ]; then
        err "未找到已安装的探针: ${AGENT_BIN}"
        err "请先执行安装脚本，而不是升级脚本。"
        exit 1
    fi
    if [ ! -f "$SANTAIZI_AGENT_CONFIG" ]; then
        err "未找到配置文件: ${SANTAIZI_AGENT_CONFIG}"
        err "升级不会写入密钥；请先完成安装。"
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
    err "Rust 探针仅支持 Linux amd64 / arm64，当前: $(uname) / $(uname -m)"
    err "换实现请走安装脚本，不要用升级脚本。"
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
        err "二进制已替换，但未能启动服务。请执行: systemctl start ${AGENT_UNIT}"
        exit 1
    fi
    if sudo "$AGENT_BIN" service restart >/dev/null 2>&1; then
        return 0
    fi
    if sudo "$AGENT_BIN" service start >/dev/null 2>&1; then
        return 0
    fi
    err "二进制已替换，但未能启动服务。请执行: ${AGENT_BIN} service start"
    exit 1
}

replace_binary() {
    os=$1
    arch=$2
    version=$3

    tmpdir=$(mktemp -d 2>/dev/null || mktemp -d -t santaizi-agent-upgrade)
    tmpfile="${tmpdir}/${AGENT_ASSET}_${os}_${arch}.zip"
    url="https://github.com/${AGENT_REPO}/releases/download/${version}/${AGENT_ASSET}_${os}_${arch}.zip"

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
    if [ ! -f "${tmpdir}/${AGENT_ASSET}" ]; then
        rm -rf "$tmpdir"
        err "压缩包中未找到 ${AGENT_ASSET}。"
        exit 1
    fi

    info "正在替换 ${AGENT_BIN} ..."
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
    info "检测到实现: Rust"
else
    info "检测到实现: Go"
fi

require_installed
deps_check

os=$(detect_os)
arch=$(detect_arch)
if [ "$os" = "unknown" ] || [ "$arch" = "unknown" ]; then
    err "不支持的操作系统或架构: $(uname) / $(uname -m)"
    exit 1
fi
require_rust_platform "$os" "$arch"

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
