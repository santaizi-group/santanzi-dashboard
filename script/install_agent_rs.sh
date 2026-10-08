#!/bin/sh

#========================================================
#   Santaizi Agent (Rust) 一键安装脚本
#   默认从 santaizi-group/santaizi-agent-rs 下载，可通过 SANTAIZI_AGENT_RS_REPO 覆盖
#   仅 Linux amd64 / arm64。无 service install：脚本写入与 Go 相同的能力开关、
#   IP 上报字段和主端拨号缓存。systemd 只带 --config。
#   SANTAIZI_AGENT_RS_PARSE_ONLY=1 时只写 yaml 与拨号缓存（路径可用环境变量覆盖）。
#========================================================

SANTAIZI_BASE_PATH="/opt/santaizi"
SANTAIZI_AGENT_RS_PATH="${SANTAIZI_BASE_PATH}/agent-rs"
SANTAIZI_AGENT_RS_BIN="${SANTAIZI_AGENT_RS_PATH}/santaizi-agent-rs"
SANTAIZI_AGENT_RS_UNIT="/etc/systemd/system/santaizi-agent-rs.service"
SANTAIZI_AGENT_YAML="${SANTAIZI_AGENT_YAML:-/etc/santaizi/agent.yaml}"
SANTAIZI_AGENT_DATA="${SANTAIZI_AGENT_DATA:-/var/lib/santaizi-agent}"

red='\033[0;31m'
green='\033[0;32m'
yellow='\033[0;33m'
plain='\033[0m'

SANTAIZI_AGENT_RS_REPO="${SANTAIZI_AGENT_RS_REPO:-santaizi-group/santaizi-agent-rs}"
CLEAN_INSTALL=0
CLEAN_INSTALL_CONFIRMED=0
USE_TLS=0
USE_IPV6_COUNTRYCODE=0
IP_REPORT_INTERFACE=""
COUNTRY_CODE=""
SERVER_IPS=""

# 缺省对齐 Go DefaultCapabilities：采集项与 NAT 默认开，温度和 GPU 默认关。
CAP_CPU=true
CAP_MEMORY=true
CAP_DISK=true
CAP_NETWORK=true
CAP_CONNECTIONS=true
CAP_PROCESSES=true
CAP_TEMPERATURE=false
CAP_GPU=false
CAP_HOST_INFO=true
CAP_IP_REPORT=true
CAP_HTTP_PROBE=true
CAP_ICMP_PROBE=true
CAP_TCP_PROBE=true
CAP_NAT=true

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

as_root() {
    if [ "${SANTAIZI_AGENT_RS_PARSE_ONLY:-0}" = 1 ]; then
        "$@"
    else
        sudo "$@"
    fi
}

yaml_escape() {
    printf "%s" "$1" | sed "s/'/''/g"
}

json_escape() {
    printf '%s' "$1" | sed 's/\\/\\\\/g; s/"/\\"/g'
}

shell_quote() {
    printf "'%s'" "$(printf '%s' "$1" | sed "s/'/'\\\\''/g")"
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

reject_unconfirmed_clean() {
    if [ "$CLEAN_INSTALL" -eq 1 ] && [ "$CLEAN_INSTALL_CONFIRMED" -ne 1 ]; then
        err "清洁安装会删除现有 Agent 配置、身份和 WAL；请同时传入 --confirm-clean-install。"
        exit 1
    fi
}

prepare_clean_install() {
    if [ "$CLEAN_INSTALL" -ne 1 ]; then
        return
    fi
    reject_unconfirmed_clean

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
    sudo mkdir -p "$(dirname "$SANTAIZI_AGENT_YAML")" "$SANTAIZI_AGENT_DATA"
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

write_owned_file() {
    dest=$1
    src=$2
    mode=$3
    as_root mkdir -p "$(dirname "$dest")"
    as_root mv "$src" "$dest"
    as_root chmod "$mode" "$dest"
}

write_agent_yaml() {
    endpoint=$1
    secret=$2
    tls_value=$3
    escaped_secret=$(yaml_escape "$secret")
    escaped_data=$(yaml_escape "$SANTAIZI_AGENT_DATA")
    tmp=$(mktemp)
    {
        printf "server: '%s'\n" "$(yaml_escape "$endpoint")"
        printf "client_secret: '%s'\n" "$escaped_secret"
        printf "tls: %s\n" "$tls_value"
        printf 'protocol: "v2"\n'
        if [ -n "$IP_REPORT_INTERFACE" ]; then
            printf "ip_report_interface: '%s'\n" "$(yaml_escape "$IP_REPORT_INTERFACE")"
        fi
        if [ -n "$COUNTRY_CODE" ]; then
            printf "country_code: '%s'\n" "$(yaml_escape "$COUNTRY_CODE")"
        fi
        if [ "$USE_IPV6_COUNTRYCODE" -eq 1 ]; then
            printf "use_ipv6_countrycode: true\n"
        fi
        cat <<EOF
capabilities:
  cpu: ${CAP_CPU}
  memory: ${CAP_MEMORY}
  disk: ${CAP_DISK}
  network: ${CAP_NETWORK}
  connections: ${CAP_CONNECTIONS}
  processes: ${CAP_PROCESSES}
  temperature: ${CAP_TEMPERATURE}
  gpu: ${CAP_GPU}
  host_info: ${CAP_HOST_INFO}
  ip_report: ${CAP_IP_REPORT}
  http_probe: ${CAP_HTTP_PROBE}
  icmp_probe: ${CAP_ICMP_PROBE}
  tcp_probe: ${CAP_TCP_PROBE}
  nat: ${CAP_NAT}
telemetry:
  data_dir: '${escaped_data}'
EOF
    } > "$tmp"
    write_owned_file "$SANTAIZI_AGENT_YAML" "$tmp" 0600
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
    write_owned_file "$SANTAIZI_AGENT_RS_UNIT" "$tmp" 0644
}

write_uninstall_wrapper() {
    dest=/usr/local/bin/santaizi-agent-uninstall
    if [ ! -d /usr/local/bin ]; then
        dest=/usr/bin/santaizi-agent-uninstall
    fi
    qyaml=$(shell_quote "$SANTAIZI_AGENT_YAML")
    qdata=$(shell_quote "$SANTAIZI_AGENT_DATA")
    qbin=$(shell_quote "$SANTAIZI_AGENT_RS_PATH")
    qunit=$(shell_quote "$SANTAIZI_AGENT_RS_UNIT")
    tmp=$(mktemp)
    cat > "$tmp" <<EOF
#!/bin/sh
set -eu
if [ "\$(id -ru)" -ne 0 ]; then
  echo "请使用 root 运行 santaizi-agent-uninstall" >&2
  exit 1
fi
if command -v systemctl >/dev/null 2>&1; then
  systemctl stop santaizi-agent-rs >/dev/null 2>&1 || true
  systemctl stop santaizi-agent >/dev/null 2>&1 || true
  systemctl disable santaizi-agent-rs >/dev/null 2>&1 || true
  systemctl disable santaizi-agent >/dev/null 2>&1 || true
  rm -f $qunit /etc/systemd/system/santaizi-agent.service /lib/systemd/system/santaizi-agent.service
  systemctl daemon-reload >/dev/null 2>&1 || true
fi
rm -rf $qbin $qdata
rm -f $qyaml
rm -f /usr/local/bin/santaizi-agent-uninstall /usr/bin/santaizi-agent-uninstall
echo "Santaizi Agent uninstalled."
EOF
    as_root rm -f /usr/local/bin/santaizi-agent-uninstall /usr/bin/santaizi-agent-uninstall
    write_owned_file "$dest" "$tmp" 0755
}

is_ipv4() {
    ip=$1
    case $ip in
        *.*.*.*) ;;
        *) return 1 ;;
    esac
    rest=$ip
    count=0
    while [ -n "$rest" ]; do
        count=$((count + 1))
        octet=${rest%%.*}
        case $rest in
            *.*) rest=${rest#*.} ;;
            *) rest= ;;
        esac
        case $octet in
            *[!0-9]*|'') return 1 ;;
        esac
        if [ "${#octet}" -gt 1 ]; then
            case $octet in
                0*) return 1 ;;
            esac
        fi
        [ "$octet" -le 255 ] || return 1
        if [ "$count" -gt 4 ]; then
            return 1
        fi
    done
    [ "$count" -eq 4 ]
}

is_ipv6() {
    printf '%s' "$1" | grep -Eq '^[0-9A-Fa-f:]+$' || return 1
    if printf '%s' "$1" | grep -q '::'; then
        printf '%s' "$1" | grep -q ':::' && return 1
        groups=$(printf '%s' "$1" | awk -F '::' '{print NF}')
        [ "$groups" -eq 2 ]
        return $?
    fi
    colons=$(printf '%s' "$1" | awk -F ':' '{print NF-1}')
    [ "$colons" -eq 7 ]
}

is_literal_ip() {
    candidate=$1
    candidate=${candidate#\[}
    candidate=${candidate%\]}
    if is_ipv4 "$candidate" || is_ipv6 "$candidate"; then
        return 0
    fi
    return 1
}

is_unusable_ip() {
    if is_ipv4 "$1"; then
        [ "$1" = "0.0.0.0" ] && return 0
        first=${1%%.*}
        [ "$first" -ge 224 ] && [ "$first" -le 239 ] && return 0
        return 1
    fi
    low=$(printf '%s' "$1" | tr '[:upper:]' '[:lower:]')
    case $low in
        ::|0:0:0:0:0:0:0:0) return 0 ;;
        ff*) return 0 ;;
    esac
    return 1
}

append_server_ip() {
    if [ -z "$SERVER_IPS" ]; then
        SERVER_IPS=$1
    else
        SERVER_IPS="${SERVER_IPS}
$1"
    fi
}

require_flag_value() {
    flag=$1
    value=${2:-}
    if [ -z "$value" ]; then
        err "参数 ${flag} 缺少取值"
        exit 1
    fi
    case $value in
        --*)
            err "参数 ${flag} 缺少取值"
            exit 1
            ;;
    esac
}

extract_balanced() {
    rest=$1
    out=
    depth=0
    in_str=0
    esc=0
    while [ -n "$rest" ]; do
        c=$(printf '%.1s' "$rest")
        rest=${rest#?}
        out="${out}${c}"
        if [ "$in_str" -eq 1 ]; then
            if [ "$esc" -eq 1 ]; then
                esc=0
                continue
            fi
            case "$c" in
                \\) esc=1 ;;
                '"') in_str=0 ;;
            esac
            continue
        fi
        case "$c" in
            '"') in_str=1 ;;
            '{') depth=$((depth + 1)) ;;
            '}')
                depth=$((depth - 1))
                if [ "$depth" -eq 0 ]; then
                    printf '%s' "$out"
                    return 0
                fi
                ;;
        esac
    done
    return 1
}

cache_primary_matches() {
    file=$1
    host=$2
    port=$3
    [ -f "$file" ] || return 1
    flat=$(tr -d '[:space:]' < "$file")
    marker='"primary":'
    case $flat in
        *"$marker"*) ;;
        *) return 1 ;;
    esac
    rest=${flat#*"$marker"}
    case $rest in
        '{'*) ;;
        *) return 1 ;;
    esac
    obj=$(extract_balanced "$rest") || return 1
    cached_host=$(printf '%s' "$obj" | sed -n 's/.*"host":"\([^"]*\)".*/\1/p')
    cached_port=$(printf '%s' "$obj" | sed -n 's/.*"port":"\([^"]*\)".*/\1/p')
    cached_ips=$(printf '%s' "$obj" | sed -n 's/.*"ips":\[\([^]]*\)\].*/\1/p')
    [ -n "$cached_host" ] && [ -n "$cached_port" ] && [ -n "$cached_ips" ] || return 1
    left=$(printf '%s' "$cached_host" | tr '[:upper:]' '[:lower:]')
    right=$(printf '%s' "$host" | tr '[:upper:]' '[:lower:]')
    [ "$left" = "$right" ] && [ "$cached_port" = "$port" ]
}

normalize_server_ips() {
    normalized=""
    seen=" "
    oldIFS=$IFS
    IFS='
'
    # shellcheck disable=SC2086
    set -- $SERVER_IPS
    IFS=$oldIFS
    for raw in "$@"; do
        [ -n "$raw" ] || continue
        if is_ipv4 "$raw"; then
            ip=$raw
        elif is_ipv6 "$raw"; then
            ip=$(printf '%s' "$raw" | tr '[:upper:]' '[:lower:]')
        else
            err "无效的 --server-ip: ${raw}"
            exit 1
        fi
        if is_unusable_ip "$ip"; then
            continue
        fi
        case $seen in
            *" ${ip} "*) continue ;;
        esac
        seen="${seen}${ip} "
        if [ -z "$normalized" ]; then
            normalized=$ip
        else
            normalized="${normalized}
${ip}"
        fi
    done
    if [ -z "$normalized" ]; then
        err "没有可用的 --server-ip"
        exit 1
    fi
    SERVER_IPS=$normalized
}

seed_primary_ips() {
    host=$1
    port=$2
    [ -n "$SERVER_IPS" ] || return 0
    if is_literal_ip "$host"; then
        return 0
    fi
    host=${host#\[}
    host=${host%\]}
    normalize_server_ips
    cache="${SANTAIZI_AGENT_DATA}/endpoint-cache.json"
    if cache_primary_matches "$cache" "$host" "$port"; then
        return 0
    fi
    now=$(date +%s 2>/dev/null || printf '0')
    ip_json=""
    oldIFS=$IFS
    IFS='
'
    # shellcheck disable=SC2086
    set -- $SERVER_IPS
    IFS=$oldIFS
    for ip in "$@"; do
        [ -n "$ip" ] || continue
        if [ -n "$ip_json" ]; then
            ip_json="${ip_json},"
        fi
        ip_json="${ip_json}\"$(json_escape "$ip")\""
    done
    tmp=$(mktemp)
    printf '{"version":1,"endpoints":{"primary":{"host":"%s","port":"%s","ips":[%s],"updated_at_unix":%s}}}\n' \
        "$(json_escape "$host")" "$(json_escape "$port")" "$ip_json" "$now" > "$tmp"
    as_root mkdir -p "$SANTAIZI_AGENT_DATA"
    as_root chmod 0700 "$SANTAIZI_AGENT_DATA"
    write_owned_file "$cache" "$tmp" 0600
}

configure_agent() {
    if [ $# -lt 3 ]; then
        err "参数不足，用法: $0 install_agent <服务器地址> <端口> <密钥> [--clean-install --confirm-clean-install] [--tls] [能力开关] [--server-ip IP]"
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

    if [ "${SANTAIZI_AGENT_RS_PARSE_ONLY:-0}" = 1 ]; then
        write_agent_yaml "$endpoint" "$secret" "$tls_value"
        seed_primary_ips "$host" "$port"
        return 0
    fi

    info "正在写入配置并启动 Rust 探针..."
    stop_go_agent
    stop_rust_agent
    write_agent_yaml "$endpoint" "$secret" "$tls_value"
    seed_primary_ips "$host" "$port"
    write_unit
    write_uninstall_wrapper
    sudo systemctl daemon-reload
    if ! sudo systemctl enable --now santaizi-agent-rs; then
        err "启动 santaizi-agent-rs 失败。"
        exit 1
    fi
    success "Rust 探针安装完成。"
}

usage() {
    echo "用法: $0 [install_agent] <服务器地址> <端口> <密钥> [--clean-install --confirm-clean-install] [--tls] [--server-ip IP] [能力开关]"
    echo "示例: $0 install_agent grpc.example.com 5555 abcdef --clean-install --confirm-clean-install --tls --disable-nat --server-ip 192.0.2.10"
    echo "能力开关、IP 上报和 --server-ip 与 Go 探针安装脚本相同。仅支持 Linux amd64 / arm64。"
    echo "仓库可用 SANTAIZI_AGENT_RS_REPO 覆盖。"
}

if [ "${1:-}" = "install_agent" ]; then
    shift
fi

if [ $# -lt 3 ]; then
    usage
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
        --server-ip)
            require_flag_value "$1" "${2:-}"
            append_server_ip "$2"
            shift 2
            ;;
        --ip-report-interface)
            require_flag_value "$1" "${2:-}"
            IP_REPORT_INTERFACE=$2
            shift 2
            ;;
        --country-code)
            require_flag_value "$1" "${2:-}"
            COUNTRY_CODE=$2
            shift 2
            ;;
        --use-ipv6-countrycode)
            USE_IPV6_COUNTRYCODE=1
            shift
            ;;
        --disable-cpu) CAP_CPU=false; shift ;;
        --disable-memory) CAP_MEMORY=false; shift ;;
        --disable-disk) CAP_DISK=false; shift ;;
        --disable-network) CAP_NETWORK=false; shift ;;
        --disable-connections) CAP_CONNECTIONS=false; shift ;;
        --disable-processes) CAP_PROCESSES=false; shift ;;
        --disable-host-info) CAP_HOST_INFO=false; shift ;;
        --disable-ip-report) CAP_IP_REPORT=false; shift ;;
        --disable-http-probe) CAP_HTTP_PROBE=false; shift ;;
        --disable-icmp-probe) CAP_ICMP_PROBE=false; shift ;;
        --disable-tcp-probe) CAP_TCP_PROBE=false; shift ;;
        --disable-nat) CAP_NAT=false; shift ;;
        --temperature) CAP_TEMPERATURE=true; shift ;;
        --gpu) CAP_GPU=true; shift ;;
        *)
            err "不支持的参数: $1"
            exit 1
            ;;
    esac
done

reject_unconfirmed_clean

if [ "${SANTAIZI_AGENT_RS_PARSE_ONLY:-0}" = 1 ]; then
    configure_agent "$install_host" "$install_port" "$install_secret"
    exit 0
fi

prepare_clean_install
install_agent_rs
configure_agent "$install_host" "$install_port" "$install_secret"
