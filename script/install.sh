#!/bin/sh

#========================================================
# 安装脚本包装器
# 从 SANTAIZI_SCRIPT_URL 环境变量读取实际安装脚本地址
# 默认指向本仓库的 Agent 专用安装脚本
#========================================================

shell_url="${SANTAIZI_SCRIPT_URL:-https://raw.githubusercontent.com/santaizi-group/santanzi-dashboard/main/script/install_agent.sh}"

if command -v wget >/dev/null 2>&1; then
    wget -O santaizi_v0.sh "$shell_url"
elif command -v curl >/dev/null 2>&1; then
    curl -fsSL -o santaizi_v0.sh "$shell_url"
else
    echo "错误: 未找到 wget 或 curl，请安装其中任意一个后再试"
    exit 1
fi

chmod +x santaizi_v0.sh

# 携带原参数运行新脚本
exec ./santaizi_v0.sh "$@"
