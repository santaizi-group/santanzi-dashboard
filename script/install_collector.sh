#!/bin/sh

#========================================================
#   Santaizi 从端（Collector）一键安装脚本
#   非交互；供 curl | bash -s -- 使用
#   已安装且未传 --token 时进入升级模式：pull 并重建，保留配置
#========================================================

red='\033[0;31m'
green='\033[0;32m'
yellow='\033[0;33m'
plain='\033[0m'

GHCR_IMAGE="ghcr.io/santaizi-group/santaizi-dashboard"
PRIMARY_ENDPOINT=""
REGISTRATION_TOKEN=""
GRPC_PORT="5556"
PRIMARY_TLS="false"
PRIMARY_INSECURE_TLS="false"
WORK_DIR="/opt/santaizi/collector"
REQUESTED_VERSION=""
IMAGE_TAG="latest"

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
        elif command -v doas > /dev/null 2>&1; then
            command doas "$@"
        else
            err "ERROR: 当前非 root，且未安装 sudo/doas，无法继续。"
            exit 1
        fi
    else
        "$@"
    fi
}

yaml_escape() {
    printf "%s" "$1" | sed "s/'/''/g"
}

detect_host_nexttrace() {
    NT_HOST_BIN=""
    if command -v nexttrace >/dev/null 2>&1; then
        NT_HOST_BIN=$(command -v nexttrace)
    elif command -v nexttrace-tiny >/dev/null 2>&1; then
        NT_HOST_BIN=$(command -v nexttrace-tiny)
    fi
    if [ -z "$NT_HOST_BIN" ]; then
        return 0
    fi
    resolved=""
    if command -v readlink >/dev/null 2>&1; then
        resolved=$(readlink -f "$NT_HOST_BIN" 2>/dev/null || true)
    fi
    if [ -z "$resolved" ] && command -v realpath >/dev/null 2>&1; then
        resolved=$(realpath "$NT_HOST_BIN" 2>/dev/null || true)
    fi
    if [ -n "$resolved" ]; then
        NT_HOST_BIN=$resolved
    fi
    if [ ! -x "$NT_HOST_BIN" ]; then
        NT_HOST_BIN=""
    fi
}

usage() {
    cat <<EOF
Usage: $0 --primary-endpoint host:port --token <registration_token> [options]

首次安装必须提供 --primary-endpoint 与 --token。
已安装且不传 --token 时进入升级模式：拉取镜像并重建容器，保留配置与数据。

Options:
  --primary-endpoint   Primary gRPC 地址（首次安装必填，如 primary.example.com:5555）
  --token              从端注册 Token（首次安装必填；升级请省略）
  --grpc-port          本机监听 gRPC 端口（默认 5556）
  --primary-tls [true|false]        连接 Primary 时启用 TLS（可省略值，等同 true）
  --primary-insecure-tls [true|false]  跳过 Primary 证书校验（仅受控测试；可省略值，等同 true）
  --dir                安装目录（默认 /opt/santaizi/collector）
  --version            镜像标签（如 v1.0.1，默认 latest）
  -h, --help           显示帮助
EOF
}

parse_args() {
    while [ $# -gt 0 ]; do
        case "$1" in
            --primary-endpoint)
                PRIMARY_ENDPOINT=$2
                shift 2
                ;;
            --token)
                REGISTRATION_TOKEN=$2
                shift 2
                ;;
            --grpc-port)
                GRPC_PORT=$2
                shift 2
                ;;
            --primary-tls)
                PRIMARY_TLS=$(parse_bool_flag "$2")
                if [ "$PRIMARY_TLS" = "true" ] || [ "$PRIMARY_TLS" = "false" ]; then
                    shift 2
                else
                    PRIMARY_TLS="true"
                    shift
                fi
                ;;
            --primary-insecure-tls)
                PRIMARY_INSECURE_TLS=$(parse_bool_flag "$2")
                if [ "$PRIMARY_INSECURE_TLS" = "true" ] || [ "$PRIMARY_INSECURE_TLS" = "false" ]; then
                    shift 2
                else
                    PRIMARY_INSECURE_TLS="true"
                    shift
                fi
                ;;
            --dir)
                WORK_DIR=$2
                shift 2
                ;;
            --version)
                REQUESTED_VERSION=$2
                shift 2
                ;;
            -h|--help)
                usage
                exit 0
                ;;
            *)
                err "未知参数: $1"
                usage
                exit 1
                ;;
        esac
    done
}

parse_bool_flag() {
    case "$1" in
        true|false) printf "%s\n" "$1" ;;
        *) printf "\n" ;;
    esac
}

validate_port() {
    case "$1" in
        ''|*[!0-9]*) return 1 ;;
    esac
    [ "$1" -ge 1 ] 2>/dev/null && [ "$1" -le 65535 ] 2>/dev/null
}

normalize_tag() {
    version=$1
    if [ -z "$version" ] || [ "$version" = "latest" ]; then
        printf "latest\n"
        return
    fi
    case "$version" in
        v*) printf "%s\n" "$version" ;;
        *) printf "v%s\n" "$version" ;;
    esac
}

validate_tag() {
    case "$1" in
        '') return 1 ;;
        *[!A-Za-z0-9._-]*) return 1 ;;
        *) return 0 ;;
    esac
}

detect_linux_distro() {
    if [ -f /etc/os-release ]; then
        . /etc/os-release
        printf "%s\n" "$ID"
    else
        printf "unknown\n"
    fi
}

install_docker() {
    info "正在安装 Docker..."
    distro=$(detect_linux_distro)
    case "$distro" in
        debian|ubuntu|linuxmint|raspbian)
            sudo apt-get update
            sudo apt-get install -y ca-certificates curl gnupg
            sudo install -m 0755 -d /etc/apt/keyrings
            curl -fsSL https://download.docker.com/linux/debian/gpg | sudo gpg --dearmor -o /etc/apt/keyrings/docker.gpg || \
            curl -fsSL https://download.docker.com/linux/ubuntu/gpg | sudo gpg --dearmor -o /etc/apt/keyrings/docker.gpg
            . /etc/os-release
            printf "deb [arch=%s signed-by=/etc/apt/keyrings/docker.gpg] https://download.docker.com/linux/%s %s stable\n" "$(dpkg --print-architecture)" "$ID" "$VERSION_CODENAME" | sudo tee /etc/apt/sources.list.d/docker.list >/dev/null
            sudo apt-get update
            sudo apt-get install -y docker-ce docker-ce-cli containerd.io docker-buildx-plugin docker-compose-plugin
            ;;
        centos|rhel|almalinux|rocky)
            sudo yum install -y yum-utils
            sudo yum-config-manager --add-repo https://download.docker.com/linux/centos/docker-ce.repo
            sudo yum install -y docker-ce docker-ce-cli containerd.io docker-buildx-plugin docker-compose-plugin
            sudo systemctl start docker
            sudo systemctl enable docker
            ;;
        fedora)
            sudo dnf -y install dnf-plugins-core
            sudo dnf config-manager --add-repo https://download.docker.com/linux/fedora/docker-ce.repo
            sudo dnf install -y docker-ce docker-ce-cli containerd.io docker-buildx-plugin docker-compose-plugin
            sudo systemctl start docker
            sudo systemctl enable docker
            ;;
        alpine)
            sudo apk add --no-cache docker docker-cli-compose
            sudo rc-update add docker default
            sudo service docker start
            ;;
        *)
            err "暂不支持的 Linux 发行版: $distro，请手动安装 Docker。"
            exit 1
            ;;
    esac
    success "Docker 安装完成。"
}

install_docker_compose_plugin() {
    info "正在安装 Docker Compose 插件..."
    sudo mkdir -p /usr/local/lib/docker/cli-plugins
    compose_version=$(curl -fsSL -m 10 https://api.github.com/repos/docker/compose/releases/latest | grep '"tag_name":' | head -n 1 | sed 's/.*"tag_name": "\(.*\)",.*/\1/')
    if [ -z "$compose_version" ]; then
        err "获取 Docker Compose 版本失败，请检查网络。"
        exit 1
    fi
    arch=$(uname -m)
    case "$arch" in
        x86_64) arch="x86_64" ;;
        aarch64|arm64) arch="aarch64" ;;
        armv7l) arch="armv7" ;;
        *) err "不支持的架构: $arch" ; exit 1 ;;
    esac
    sudo curl -fsSL -o /usr/local/lib/docker/cli-plugins/docker-compose "https://github.com/docker/compose/releases/download/${compose_version}/docker-compose-linux-${arch}"
    sudo chmod +x /usr/local/lib/docker/cli-plugins/docker-compose
    success "Docker Compose 插件安装完成。"
}

check_docker() {
    if ! command -v docker >/dev/null 2>&1; then
        info "未检测到 Docker，尝试自动安装..."
        install_docker
    fi

    if ! docker compose version >/dev/null 2>&1 && ! command -v docker-compose >/dev/null 2>&1; then
        info "未检测到 Docker Compose，尝试自动安装插件..."
        install_docker_compose_plugin
    fi

    if ! command -v docker >/dev/null 2>&1; then
        err "Docker 不可用，请先手动安装。"
        exit 1
    fi
    if ! docker compose version >/dev/null 2>&1 && ! command -v docker-compose >/dev/null 2>&1; then
        err "Docker Compose 不可用，请先手动安装。"
        exit 1
    fi
}

run_compose() {
    if docker compose version >/dev/null 2>&1; then
        sudo docker compose "$@"
    else
        sudo docker-compose "$@"
    fi
}

find_compose_file() {
    dir=$1
    for name in docker-compose.yml docker-compose.yaml compose.yml compose.yaml; do
        if [ -f "${dir}/${name}" ]; then
            printf "%s\n" "${dir}/${name}"
            return 0
        fi
    done
    return 1
}

is_collector_config() {
    grep -Eq '^[[:space:]]*mode:[[:space:]]*collector[[:space:]]*$' "$1"
}

compose_image() {
    grep -E '^[[:space:]]*image:' "$1" | head -n 1 | awk '{print $2}' | tr -d "\"'"
}

set_compose_image_tag() {
    file=$1
    tag=$2
    tmp="${file}.tmp.$$"
    if ! sudo sed -e "s|^\\([[:space:]]*image:[[:space:]]*.*\\):[^:[:space:]]*[[:space:]]*$|\\1:${tag}|" "$file" | sudo tee "$tmp" >/dev/null; then
        sudo rm -f "$tmp"
        err "更新 compose 镜像标签失败。"
        exit 1
    fi
    if ! sudo grep -Eq "^[[:space:]]*image:[[:space:]]*.*:${tag}[[:space:]]*$" "$tmp"; then
        sudo rm -f "$tmp"
        err "未能在 compose 文件中写入镜像标签: ${tag}"
        exit 1
    fi
    sudo mv "$tmp" "$file"
}

image_id() {
    sudo docker image inspect --format '{{.Id}}' "$1" 2>/dev/null || true
}

image_version_label() {
    sudo docker image inspect --format '{{index .Config.Labels "org.opencontainers.image.version"}}' "$1" 2>/dev/null || true
}

collector_container_ref() {
    if sudo docker inspect santaizi-collector >/dev/null 2>&1; then
        printf "santaizi-collector\n"
        return 0
    fi
    cid=$(run_compose ps -q 2>/dev/null | head -n 1)
    if [ -n "$cid" ]; then
        printf "%s\n" "$cid"
        return 0
    fi
    return 1
}

print_image_version() {
    image=$(compose_image "$1")
    ver=$(image_version_label "$image")
    if [ -n "$ver" ]; then
        success "镜像版本: ${ver}"
    else
        success "镜像: ${image}"
    fi
}

verify_collector_running() {
    ref=$(collector_container_ref) || {
        err "未找到从端容器。"
        run_compose ps || true
        exit 1
    }
    st=$(sudo docker inspect -f '{{.State.Status}}' "$ref" 2>/dev/null || true)
    if [ "$st" != "running" ]; then
        err "从端容器未处于 running 状态（当前: ${st:-unknown}）。"
        run_compose ps || true
        exit 1
    fi
    success "容器状态: running"
}

pull_and_recreate() {
    compose_file=$1
    image=$(compose_image "$compose_file")
    if [ -z "$image" ]; then
        err "compose 文件中未找到 image。"
        exit 1
    fi
    info "目标镜像: ${image}"
    info "正在拉取镜像..."
    if ! run_compose pull; then
        err "拉取镜像失败，请检查 Docker 与网络。"
        exit 1
    fi
    target_id=$(image_id "$image")
    if [ -z "$target_id" ]; then
        err "拉取后未找到镜像: ${image}"
        exit 1
    fi

    skip=0
    if ref=$(collector_container_ref); then
        st=$(sudo docker inspect -f '{{.State.Status}}' "$ref" 2>/dev/null || true)
        img=$(sudo docker inspect -f '{{.Image}}' "$ref" 2>/dev/null || true)
        if [ "$st" = "running" ] && [ "$img" = "$target_id" ]; then
            skip=1
        fi
    fi
    if [ "$skip" -eq 1 ]; then
        success "已是目标镜像，无需重建容器。"
        return 0
    fi

    info "正在启动 / 重建从端容器..."
    if ! run_compose up -d; then
        err "启动从端失败，请检查 Docker 与网络。"
        exit 1
    fi
}

write_compose() {
    mkdir -p "$1"
    detect_host_nexttrace
    nexttrace_volume=""
    nexttrace_env=""
    if [ -n "$NT_HOST_BIN" ]; then
        nexttrace_volume="      - ${NT_HOST_BIN}:/opt/nexttrace/nexttrace:ro"
        nexttrace_env="      - SANTAIZI_COLLECTOR_NEXTTRACE_PATH=/opt/nexttrace/nexttrace"
    fi
    cat > "$1/docker-compose.yml" <<EOF
services:
  santaizi-collector:
    image: ${GHCR_IMAGE}:${IMAGE_TAG}
    container_name: santaizi-collector
    restart: unless-stopped
    ports:
      - "${GRPC_PORT}:${GRPC_PORT}"
    volumes:
      - /etc/timezone:/etc/timezone:ro
      - /etc/localtime:/etc/localtime:ro
      - ./data:/var/lib/santaizi-dashboard
      - ./config/dashboard.yaml:/etc/santaizi/dashboard.yaml:ro
${nexttrace_volume}
    environment:
      - TZ=Asia/Shanghai
${nexttrace_env}
    cap_add:
      - NET_RAW
EOF
}

write_config() {
    mkdir -p "$1/data/pki" "$1/config"
    endpoint=$(yaml_escape "$PRIMARY_ENDPOINT")
    token=$(yaml_escape "$REGISTRATION_TOKEN")
    detect_host_nexttrace
    nexttrace_yaml=""
    if [ -n "$NT_HOST_BIN" ]; then
        nexttrace_yaml="  nexttrace_path: /opt/nexttrace/nexttrace"
    fi
    cat > "$1/config/dashboard.yaml" <<EOF
mode: collector
debug: false
language: zh-CN
grpcport: ${GRPC_PORT}
telemetry:
  data_dir: /var/lib/santaizi-dashboard
collector:
  primary_endpoint: '${endpoint}'
  primary_tls: ${PRIMARY_TLS}
  primary_insecure_tls: ${PRIMARY_INSECURE_TLS}
  registration_token: '${token}'
  database_path: /var/lib/santaizi-dashboard/collector.db
${nexttrace_yaml}
grpc_tls:
  enabled: false
  cert_file: /var/lib/santaizi-dashboard/pki/server.crt
  key_file: /var/lib/santaizi-dashboard/pki/server.key
  client_ca_file: ""
  require_agent_mtls: false
  require_collector_mtls: false
EOF
    chmod 600 "$1/config/dashboard.yaml"
}

main() {
    parse_args "$@"

    info "欢迎使用 Santaizi 从端一键安装脚本"

    if [ "$(uname -s)" != "Linux" ]; then
        err "本脚本目前仅支持 Linux 系统。"
        exit 1
    fi

    IMAGE_TAG=$(normalize_tag "$REQUESTED_VERSION")
    if ! validate_tag "$IMAGE_TAG"; then
        err "无效的镜像标签: ${REQUESTED_VERSION}"
        exit 1
    fi

    if ! validate_port "$GRPC_PORT"; then
        err "grpc-port 必须是 1-65535 之间的数字。"
        exit 1
    fi

    check_docker

    if ! mkdir -p "$WORK_DIR"; then
        err "创建工作目录失败: $WORK_DIR"
        exit 1
    fi
    cd "$WORK_DIR" || {
        err "无法进入工作目录: $WORK_DIR"
        exit 1
    }

    config="${WORK_DIR}/config/dashboard.yaml"

    if [ -f "$config" ]; then
        if ! is_collector_config "$config"; then
            err "已有配置不是从端（mode: collector），拒绝继续。"
            exit 1
        fi
        compose=$(find_compose_file "$WORK_DIR") || {
            err "已有配置但缺少 compose 文件，请检查 ${WORK_DIR}。"
            exit 1
        }

        if [ -n "$REGISTRATION_TOKEN" ]; then
            if [ -z "$PRIMARY_ENDPOINT" ]; then
                err "重写配置时必须同时提供 --primary-endpoint 与 --token。"
                usage
                exit 1
            fi
            info "检测到已安装从端，将备份并重写配置后升级镜像。"
            cp "$config" "${config}.bak"
            chmod 600 "${config}.bak"
            write_compose "$WORK_DIR"
            write_config "$WORK_DIR"
            compose="${WORK_DIR}/docker-compose.yml"
        else
            info "检测到已安装从端，进入升级模式（保留配置）。"
            if [ -n "$PRIMARY_ENDPOINT" ]; then
                info "升级模式忽略 --primary-endpoint；需要改配置请同时提供 --token。"
            fi
            if [ -n "$REQUESTED_VERSION" ]; then
                info "正在将 compose 镜像标签设为 ${IMAGE_TAG} ..."
                set_compose_image_tag "$compose" "$IMAGE_TAG"
            fi
        fi

        info "请先升级 Primary，再升级从端，最后升级探针。"
        pull_and_recreate "$compose"
        verify_collector_running
        print_image_version "$compose"
        success "Santaizi 从端已升级。"
        success "工作目录: ${WORK_DIR}"
        success "配置文件: ${config}"
        return 0
    fi

    if [ -z "$PRIMARY_ENDPOINT" ] || [ -z "$REGISTRATION_TOKEN" ]; then
        err "必须提供 --primary-endpoint 与 --token。"
        usage
        exit 1
    fi

    info "正在生成从端配置..."
    write_compose "$WORK_DIR"
    write_config "$WORK_DIR"
    compose="${WORK_DIR}/docker-compose.yml"

    info "正在拉取镜像并启动从端..."
    pull_and_recreate "$compose"
    verify_collector_running
    print_image_version "$compose"

    success "Santaizi 从端安装完成。"
    success "工作目录: ${WORK_DIR}"
    success "gRPC 端口: ${GRPC_PORT}"
    success "配置文件: ${config}"
}

main "$@"
