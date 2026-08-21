# 安装指南

## Dashboard 安装

### 方式一：一键安装脚本（推荐）

```bash
sh -c "$(curl -fsSL https://raw.githubusercontent.com/santaizi-group/santanzi-dashboard/main/script/install_dashboard.sh)"
```

脚本会交互式询问：

- 工作目录
- Web 端口、gRPC 端口
- OAuth2 类型及密钥（先在提供商创建应用；回调地址必须为 `https://<面板域名>/oauth2/callback`，字段与 YAML 见 [配置参考](configuration.md#oauth2-登录配置)）
- 站点标题与 ServerStatus 品牌外观

确认后会自动生成 `docker-compose.yml` 和 `config/dashboard.yaml`，并启动容器。

### 方式二：手动 Docker Compose

1. 创建工作目录：

```bash
mkdir -p ~/santaizi/data ~/santaizi/config && cd ~/santaizi
```

2. 编写 `docker-compose.yml`：

```yaml
services:
  santaizi-dashboard:
    image: ghcr.io/santaizi-group/santaizi-dashboard:latest
    container_name: santaizi-dashboard
    restart: unless-stopped
    ports:
      - "${SANTAIZI_PORT:-80}:80"
      - "5555:5555"
    volumes:
      - /etc/timezone:/etc/timezone:ro
      - /etc/localtime:/etc/localtime:ro
      - ./data:/var/lib/santaizi-dashboard
      - ./config/dashboard.yaml:/etc/santaizi/dashboard.yaml:ro
    environment:
      - TZ=Asia/Shanghai
```

3. 准备 `config/dashboard.yaml`，参考 [配置参考](configuration.md)。

4. 启动：

```bash
docker compose up -d
```

### 方式三：手动运行二进制

适用于开发或特殊环境。

```bash
# 下载 release 二进制或自行编译
CGO_ENABLED=1 go build -o santaizi-dashboard ./cmd/dashboard/main.go

# 运行
./santaizi-dashboard --config /etc/santaizi/dashboard.yaml --db /var/lib/santaizi-dashboard/sqlite.db
```

CLI 参数：

| 参数 | 说明 |
|------|------|
| `-v` | 查看版本 |
| `-c /etc/santaizi/dashboard.yaml` | 指定配置文件，默认 `/etc/santaizi/dashboard.yaml` |
| `--db /var/lib/santaizi-dashboard/sqlite.db` | 指定 SQLite 路径，默认 `/var/lib/santaizi-dashboard/sqlite.db` |

---

## 从端（Collector）安装

从端与 Primary 使用同一 Dashboard 镜像，以 `mode: collector` 运行。推荐在管理后台「可靠探测」创建从端后，使用「安装从端」复制一键命令。

也可手动执行（参数需替换）：

```bash
curl -fsSL https://raw.githubusercontent.com/santaizi-group/santanzi-dashboard/main/script/install_collector.sh | bash -s -- \
  --primary-endpoint primary.example.com:5555 \
  --token <registration_token> \
  --grpc-port 5556 \
  --primary-tls true \
  --primary-insecure-tls false
```

生成的 `dashboard.yaml` 已带 `collector.primary_tls` / `primary_insecure_tls` 与默认关闭的 `grpc_tls`。连接 Primary 改命令里的 true/false；从端对外 gRPC 要开 TLS 时，把证书放到 `data/pki/server.{crt,key}` 并把 `grpc_tls.enabled` 改为 true。首次安装（或带 `--token` 重写）时，若宿主机已有 `nexttrace` / `nexttrace-tiny`，脚本会挂进容器供探测型从端做路由；镜像本身不含该二进制。详见 [可靠探测运维指南](reliable-telemetry.md#探测型从端路由)。

已安装的从端可重跑该脚本（不必再传 token）以拉取新镜像并重建容器，配置与数据目录保留。也可使用专用升级脚本，见下文「更新 / 从端」。

详细步骤见 [可靠探测运维指南](reliable-telemetry.md)。

---

## Agent 安装

Agent 默认从 `santaizi-group/santaizi-agent` 仓库下载，可通过环境变量 `SANTAIZI_AGENT_REPO` 覆盖。

### Linux

```bash
curl -fSL https://raw.githubusercontent.com/santaizi-group/santanzi-dashboard/main/script/install_agent.sh | bash -s -- install_agent <面板地址> <端口> <密钥> --clean-install --confirm-clean-install [--server-ip <主端IP>]
```

英文版脚本：

```bash
curl -fSL https://raw.githubusercontent.com/santaizi-group/santanzi-dashboard/main/script/install_agent_en.sh | bash -s -- install_agent <面板地址> <端口> <密钥> --clean-install --confirm-clean-install
```

安装路径：`/opt/santaizi/agent`；默认配置 `/etc/santaizi/agent.yaml`；可靠探测数据 `/var/lib/santaizi-agent/`

### Windows

```powershell
powershell -NoProfile -ExecutionPolicy Bypass -Command "& ([ScriptBlock]::Create((Invoke-WebRequest 'https://raw.githubusercontent.com/santaizi-group/santanzi-dashboard/main/script/install.ps1' -UseBasicParsing).Content)) -Server '<面板地址>:<端口>' -Key '<密钥>' -CleanInstall -ConfirmCleanInstall"
```

安装路径：`C:\santaizi`

### macOS

```bash
curl -fSL https://raw.githubusercontent.com/santaizi-group/santanzi-dashboard/main/script/install.command | sudo bash -s -- install_agent <面板地址> <端口> <密钥> --clean-install --confirm-clean-install
```

安装路径：`/opt/santaizi/agent`

### 参数说明

| 参数 | 示例 | 说明 |
|------|------|------|
| 面板地址 | `10.0.0.10` 或 `santaizi.example.com` | Dashboard 的 IP 或域名 |
| 端口 | `5555` | `config.yaml` 中的 `grpcport` |
| 密钥 | `abcdef123456` | 服务器详情中的 Secret |
| `--server-ip` / `-ServerIP` | `1.2.3.4` | 可选。域名安装时预缓存主端 IP；探针仍先 DNS，解析并连上成功后覆盖。管理后台生成命令时会自动附带面板解析到的地址 |

### 清洁安装

管理后台默认选择清洁安装。确认后命令会同时带上 `--clean-install --confirm-clean-install`（PowerShell 使用 `-CleanInstall -ConfirmCleanInstall`），安装器才会停止现有服务并删除 Agent 配置、节点身份、WAL 和程序目录；并尝试卸载删除本机旧版 `nezha-agent`（如 `/opt/nezha/agent`、`/etc/nezha` 及对应服务）。缺少确认标志时安装器会拒绝执行清理。

清洁安装会生成全新身份与节点绑定，不导入已有历史数据。

### 采集与能力参数

安装弹窗可选择 **标准·云**、**标准·物理**、**轻量**、**仅存活** 预设，也可以组合以下参数：

| 预设 | 温度 | GPU | 内网穿透 |
|------|------|-----|----------|
| 标准·云 | 关 | 关 | 关 |
| 标准·物理 | 开 | 开 | 关 |
| 轻量 / 仅存活 | 关 | 关 | 关 |

所有预设默认关闭内网穿透；需要时在弹窗中手动开启。

开启「IP 与位置」时可填写：

| 参数 | 作用 |
|------|------|
| `--ip-report-interface eth0` | 绑定公网 IP 探测出口，并限制流量统计网卡 |
| `--country-code CN` | 手填国家/区域识别码并直接上报 |
| `--use-ipv6-countrycode` | 双栈时优先使用 IPv6 |

| 参数 | 作用 |
|------|------|
| `--disable-cpu` | 不采集 CPU 与负载 |
| `--disable-memory` | 不采集内存与 Swap |
| `--disable-disk` | 不采集磁盘指标 |
| `--disable-network` | 不采集网络速率与流量 |
| `--disable-connections` | 不采集 TCP/UDP 连接数 |
| `--disable-processes` | 不采集进程数 |
| `--temperature` | 启用温度采集 |
| `--gpu` | 启用 GPU 信息与使用率采集 |
| `--disable-host-info` | 不采集硬件与系统信息 |
| `--disable-ip-report` | 不查询或上报 IP/位置 |
| `--disable-http-probe` | 不参与 HTTP 服务探测 |
| `--disable-icmp-probe` | 不参与 ICMP 服务探测 |
| `--disable-tcp-probe` | 不参与 TCP 服务探测 |
| `--disable-nat` | 禁止建立 NAT 通道 |

心跳和可靠节点身份不可关闭。禁用指标会明确声明为“未采集”，不会伪装为数值零。

### 管理 Agent

Linux / macOS 使用 systemd 服务：

```bash
# 查看状态
systemctl status santaizi-agent

# 重启
systemctl restart santaizi-agent

# 查看日志
journalctl -u santaizi-agent -f
```

Windows 使用服务管理器查看 `Santaizi Agent` 服务。

系统服务启动命令只有 `--config`（默认 `/etc/santaizi/agent.yaml`），密钥和能力开关在配置文件里，不会出现在进程参数中。卸载使用 `santaizi-agent-uninstall`（见下文）。

---

## 更新

### Dashboard

```bash
cd ~/santaizi
docker compose pull
docker compose up -d
```

### 从端

已安装从端在本机执行升级脚本即可拉取 GHCR 镜像并重建容器，**不改**配置与数据目录。请先升级 Primary，再升级从端，最后升级探针。

```bash
curl -fsSL https://raw.githubusercontent.com/santaizi-group/santanzi-dashboard/main/script/upgrade_collector.sh | bash
```

指定版本（会改 compose 中的镜像标签后再拉取）：

```bash
curl -fsSL https://raw.githubusercontent.com/santaizi-group/santanzi-dashboard/main/script/upgrade_collector.sh | bash -s -- v1.0.1
```

工作目录默认 `/opt/santaizi/collector`，可用 `--dir` 或 `SANTAIZI_COLLECTOR_DIR` 覆盖。也可重跑 `install_collector.sh` 且不传 `--token`。

### Agent

Dashboard 不提供远程或自动更新。已安装的探针在本机执行升级脚本即可替换二进制并重启服务，**不改**配置、节点身份和 WAL。协议破坏性升级（须成对更换面板）仍使用已确认的清洁安装。

Linux：

```bash
curl -fSL https://raw.githubusercontent.com/santaizi-group/santanzi-dashboard/main/script/upgrade_agent.sh | bash
```

指定版本：

```bash
curl -fSL https://raw.githubusercontent.com/santaizi-group/santanzi-dashboard/main/script/upgrade_agent.sh | bash -s -- v1.0.1
```

英文脚本：`upgrade_agent_en.sh`。可用 `SANTAIZI_AGENT_REPO`、`SANTAIZI_AGENT_VERSION` 覆盖仓库与版本。

Windows：

```powershell
powershell -NoProfile -ExecutionPolicy Bypass -Command "& ([ScriptBlock]::Create((Invoke-WebRequest 'https://raw.githubusercontent.com/santaizi-group/santanzi-dashboard/main/script/upgrade.ps1' -UseBasicParsing).Content))"
```

macOS：

```bash
curl -fSL https://raw.githubusercontent.com/santaizi-group/santanzi-dashboard/main/script/upgrade.command | sudo bash
```

管理后台「主机管理」行内操作也可复制同一条命令。

---

## 卸载

### Dashboard

```bash
cd ~/santaizi
docker compose down -v
rm -rf ~/santaizi
```

### Agent

Linux / macOS（安装后已注册到 PATH）：

```bash
santaizi-agent-uninstall
```

Windows：

```powershell
C:\santaizi\santaizi-agent-uninstall.cmd
```

该命令会停止并删除服务，以及 `/opt/santaizi/agent`（Windows 为 `C:\santaizi`）、配置文件和数据目录。已用旧方式安装、服务启动参数里仍带密钥的实例，重新执行一次安装命令即可改为只读配置文件。
