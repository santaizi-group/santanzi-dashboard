#!/bin/sh

#========================================================
#   Santaizi Dashboard 一键安装脚本
#   支持交互式配置；若未安装 Docker，可询问后自动安装
#========================================================

red='\033[0;31m'
green='\033[0;32m'
yellow='\033[0;33m'
plain='\033[0m'

GHCR_IMAGE="ghcr.io/santaizi-group/santaizi-dashboard"

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
            err "请先切换到 root 后运行："
            err "  su -"
            err "  sh -c \"\$(curl -fsSL https://raw.githubusercontent.com/santaizi-group/santanzi-dashboard/main/script/install_dashboard.sh)\""
            exit 1
        fi
    else
        "$@"
    fi
}

prompt() {
    message=$1
    default=$2
    printf "%s" "$message" >&2
    if [ -n "$default" ]; then
        printf " (默认: %s)" "$default" >&2
    fi
    printf ": " >&2
    read -r val
    if [ -z "$val" ]; then
        val=$default
    fi
    printf "%s\n" "$val"
}

prompt_required() {
    while true; do
        val=$(prompt "$1" "$2")
        if [ -n "$val" ]; then
            printf "%s\n" "$val"
            return
        fi
        err "该项不能为空，请重新输入。"
    done
}

prompt_yn() {
    message=$1
    default=${2:-y}
    while true; do
        printf "%s (y/n, 默认: %s): " "$message" "$default"
        read -r val
        val=${val:-$default}
        case "$val" in
            [Yy]|[Yy][Ee][Ss]) return 0 ;;
            [Nn]|[Nn][Oo]) return 1 ;;
            *) err "请输入 y 或 n。" ;;
        esac
    done
}

prompt_port() {
    while true; do
        val=$(prompt "$1" "$2")
        case "$val" in
            ''|*[!0-9]*)
                err "端口必须是 1-65535 之间的数字。"
                continue
                ;;
        esac
        if [ "$val" -ge 1 ] 2>/dev/null && [ "$val" -le 65535 ] 2>/dev/null; then
            printf "%s\n" "$val"
            return
        fi
        err "端口必须是 1-65535 之间的数字。"
    done
}

yaml_escape() {
    printf "%s" "$1" | sed "s/'/''/g"
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
        err "未检测到 Docker。"
        if prompt_yn "是否自动安装 Docker？" "y"; then
            install_docker
        else
            err "请先手动安装 Docker 后重新运行脚本。"
            exit 1
        fi
    fi

    if ! docker compose version >/dev/null 2>&1 && ! command -v docker-compose >/dev/null 2>&1; then
        err "未检测到 Docker Compose。"
        if prompt_yn "是否自动安装 Docker Compose 插件？" "y"; then
            install_docker_compose_plugin
        else
            err "请先手动安装 Docker Compose 后重新运行脚本。"
            exit 1
        fi
    fi
}

run_compose() {
    if docker compose version >/dev/null 2>&1; then
        sudo docker compose "$@"
    else
        sudo docker-compose "$@"
    fi
}

write_compose() {
    mkdir -p "$1"
    cat > "$1/docker-compose.yml" <<EOF
services:
  santaizi-dashboard:
    image: ${GHCR_IMAGE}:latest
    container_name: santaizi-dashboard
    restart: unless-stopped
    ports:
      - "${WEB_PORT}:80"
      - "${GRPC_PORT}:5555"
    volumes:
      - /etc/timezone:/etc/timezone:ro
      - /etc/localtime:/etc/localtime:ro
      - ./data:/var/lib/santaizi-dashboard
      - ./config/dashboard.yaml:/etc/santaizi/dashboard.yaml:ro
    environment:
      - TZ=Asia/Shanghai
EOF
}

write_config() {
    mkdir -p "$1/data" "$1/config"
    oauth2_type=$(yaml_escape "$OAUTH2_TYPE")
    oauth2_admin=$(yaml_escape "$OAUTH2_ADMIN")
    oauth2_clientid=$(yaml_escape "$OAUTH2_CLIENTID")
    oauth2_clientsecret=$(yaml_escape "$OAUTH2_CLIENTSECRET")
    oauth2_endpoint=$(yaml_escape "$OAUTH2_ENDPOINT")
    site_brand=$(yaml_escape "$SITE_BRAND")
    cat > "$1/config/dashboard.yaml" <<EOF
mode: primary
debug: false
httpport: 80
language: zh-CN
grpcport: 5555
oauth2:
  type: '${oauth2_type}'
  admin: '${oauth2_admin}'
  clientid: '${oauth2_clientid}'
  clientsecret: '${oauth2_clientsecret}'
  endpoint: '${oauth2_endpoint}'
site:
  brand: '${site_brand}'
  cookiename: "santaizi-dashboard"
telemetry:
  data_dir: /var/lib/santaizi-dashboard
EOF
}

get_server_ip() {
    ip=$(ip route get 1.1.1.1 2>/dev/null | awk '{for(i=1;i<=NF;i++) if($i=="src") {print $(i+1); exit}}')
    if [ -z "$ip" ]; then
        ip=$(hostname -I 2>/dev/null | awk '{print $1}')
    fi
    if [ -z "$ip" ]; then
        ip="<服务器IP>"
    fi
    printf "%s\n" "$ip"
}

main() {
    info "欢迎使用 Santaizi Dashboard 一键安装脚本"

    if [ "$(uname -s)" != "Linux" ]; then
        err "本脚本目前仅支持 Linux 系统。"
        exit 1
    fi

    if ! [ -t 0 ]; then
        err "检测到脚本通过管道执行（如 curl ... | bash），但本脚本需要交互式输入。"
        err "请改用以下一键运行方式："
        err "  sh -c \"\$(curl -fsSL https://raw.githubusercontent.com/santaizi-group/santanzi-dashboard/main/script/install_dashboard.sh)\""
        err "如果当前不是 root，请先使用 sudo、doas 或 su - 提权后再运行上面的命令。"
        exit 1
    fi

    check_docker

    WORK_DIR=$(prompt "请输入 Dashboard 工作目录" "/opt/santaizi")
    WORK_DIR=${WORK_DIR:-/opt/santaizi}
    if ! mkdir -p "$WORK_DIR"; then
        err "创建工作目录失败: $WORK_DIR"
        exit 1
    fi
    cd "$WORK_DIR" || {
        err "无法进入工作目录: $WORK_DIR"
        exit 1
    }

    WEB_PORT=$(prompt_port "请输入面板 Web 端口" "80")
    GRPC_PORT=$(prompt_port "请输入 Agent gRPC 端口" "5555")

    info "请配置 OAuth2 登录（首次登录必须使用 OAuth2）"
    info "请先在提供商创建应用。回调地址必须填：https://<你访问面板的域名>/oauth2/callback"
    info "GitHub 字段名是 Authorization callback URL；不要只填首页。"
    OAUTH2_TYPE=$(prompt "OAuth2 类型 (github/gitlab/jihulab/gitee/gitea)" "github")
    OAUTH2_TYPE=${OAUTH2_TYPE:-github}
    OAUTH2_ADMIN=$(prompt_required "管理员账号（多个用半角逗号隔开）")
    OAUTH2_CLIENTID=$(prompt_required "OAuth2 Client ID")
    OAUTH2_CLIENTSECRET=$(prompt_required "OAuth2 Client Secret")
    OAUTH2_ENDPOINT=$(prompt "OAuth2 Endpoint（仅自建 Gitea 需要）" "")

    SITE_BRAND=$(prompt "站点标题" "三太子监控")
    SITE_BRAND=${SITE_BRAND:-三太子监控}

    info "正在生成配置文件..."
    mkdir -p data
    write_compose "$WORK_DIR"
    write_config "$WORK_DIR"

    info "正在拉取镜像并启动 Dashboard..."
    if ! run_compose up -d; then
        err "启动 Dashboard 失败，请检查 Docker 与网络。"
        exit 1
    fi

    SERVER_IP=$(get_server_ip)
    success "Santaizi Dashboard 安装完成！"
    success "访问地址: http://${SERVER_IP}:${WEB_PORT}"
    success "工作目录: ${WORK_DIR}"
    success "配置文件: ${WORK_DIR}/config/dashboard.yaml"
    info "安装 Agent：进入后台 → 服务器 → 添加服务器 → 使用一键安装命令。"
}

main
