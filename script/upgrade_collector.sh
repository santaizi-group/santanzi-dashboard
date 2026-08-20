#!/bin/sh

#========================================================
#   Santaizi 从端（Collector）升级脚本
#   只拉取镜像并重建容器；保留配置、注册身份和数据目录
#========================================================

GHCR_IMAGE="ghcr.io/santaizi-group/santaizi-dashboard"
WORK_DIR="${SANTAIZI_COLLECTOR_DIR:-/opt/santaizi/collector}"
REQUESTED_VERSION="${SANTAIZI_COLLECTOR_VERSION:-}"

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
    cat <<EOF
Usage: $0 [upgrade_collector] [版本] [--dir <path>]

Options:
  --dir                从端工作目录（默认 /opt/santaizi/collector，可用 SANTAIZI_COLLECTOR_DIR 覆盖）
  --version            镜像标签（如 v1.0.1；不传则拉取 compose 中现有标签，安装默认 latest）
  -h, --help           显示帮助

示例:
  $0
  $0 v1.0.1
  SANTAIZI_COLLECTOR_VERSION=v1.0.1 $0
  $0 --dir /opt/santaizi/collector --version latest

未指定版本时不改 compose 标签，只 pull 后按需重建。不修改配置与 data/。
请先升级 Primary，再升级从端，最后升级探针。
EOF
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

require_docker() {
    if ! command -v docker >/dev/null 2>&1; then
        err "未检测到 Docker，请先安装。"
        exit 1
    fi
    if ! docker compose version >/dev/null 2>&1 && ! command -v docker-compose >/dev/null 2>&1; then
        err "未检测到 Docker Compose，请先安装。"
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
        err "拉取镜像失败，请检查网络是否能访问 GHCR。"
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
        err "启动从端失败，请检查 Docker 与日志。"
        exit 1
    fi
}

parse_args() {
    while [ $# -gt 0 ]; do
        case "$1" in
            -h|--help)
                usage
                exit 0
                ;;
            --dir)
                WORK_DIR=$2
                shift 2
                ;;
            --version)
                REQUESTED_VERSION=$2
                shift 2
                ;;
            upgrade_collector|upgrade)
                shift
                ;;
            -*)
                err "未知参数: $1"
                usage
                exit 1
                ;;
            *)
                if [ -n "$REQUESTED_VERSION" ]; then
                    err "多余参数: $1"
                    usage
                    exit 1
                fi
                REQUESTED_VERSION=$1
                shift
                ;;
        esac
    done
}

require_installed() {
    if [ ! -d "$WORK_DIR" ]; then
        err "未找到从端目录: ${WORK_DIR}"
        err "请先执行安装脚本，而不是升级脚本。"
        exit 1
    fi
    COMPOSE_FILE=$(find_compose_file "$WORK_DIR") || {
        err "未找到 compose 文件: ${WORK_DIR}/docker-compose.yml"
        err "请先执行安装脚本，而不是升级脚本。"
        exit 1
    }
    CONFIG_FILE="${WORK_DIR}/config/dashboard.yaml"
    if [ ! -f "$CONFIG_FILE" ]; then
        err "未找到配置文件: ${CONFIG_FILE}"
        err "升级不会写入 Token；请先完成安装。"
        exit 1
    fi
    if ! is_collector_config "$CONFIG_FILE"; then
        err "配置不是 mode: collector，拒绝升级以免误伤主面板。"
        exit 1
    fi
}

main() {
    parse_args "$@"

    info "Santaizi 从端升级脚本"

    if [ "$(uname -s)" != "Linux" ]; then
        err "本脚本目前仅支持 Linux 系统。"
        exit 1
    fi

    require_docker
    require_installed

    cd "$WORK_DIR" || {
        err "无法进入工作目录: $WORK_DIR"
        exit 1
    }

    if [ -n "$REQUESTED_VERSION" ]; then
        IMAGE_TAG=$(normalize_tag "$REQUESTED_VERSION")
        if ! validate_tag "$IMAGE_TAG"; then
            err "无效的镜像标签: ${REQUESTED_VERSION}"
            exit 1
        fi
        info "正在将 compose 镜像标签设为 ${IMAGE_TAG} ..."
        set_compose_image_tag "$COMPOSE_FILE" "$IMAGE_TAG"
    fi

    info "请先升级 Primary，再升级从端，最后升级探针。"
    pull_and_recreate "$COMPOSE_FILE"
    verify_collector_running
    print_image_version "$COMPOSE_FILE"
    success "Santaizi 从端已升级。"
    success "工作目录: ${WORK_DIR}"
    success "配置文件: ${CONFIG_FILE}"
}

main "$@"
