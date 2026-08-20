# Santaizi Dashboard

三太子监控的主面板与从端。管理后台、公开状态页和接口文档均为独立 Vue 应用，默认嵌入 Go 二进制，也可在同域反向代理下外置部署。HTTP API v2 以 OpenAPI 3.0.3 为唯一契约。

- 管理后台：`/admin/`
- 公开状态页：`/`
- 接口文档：`/docs/api/`
- OpenAPI：`/openapi/v2.yaml`、`/openapi/v2.json`

## 版权与致谢

本项目基于 [哪吒监控 Nezha Monitoring](https://github.com/naiba/nezha) 衍生修改，原作者版权保留（Apache-2.0，`Copyright 2020 naiba`）。详见 [`LICENSE`](./LICENSE) 与 [`NOTICE`](./NOTICE)。

产品品牌为 **三太子 / Santaizi**；主面板与探针须成对升级。

## 安全要求（必读）

**不要把面板 Web 端口直接暴露在公网。**

管理后台（`/admin/`）、OAuth 回调、API 与 WebSocket 一旦可被任意访问，即构成高风险控制面。部署时必须将面板置于可信访问控制之后，例如 [Cloudflare Zero Trust](https://developers.cloudflare.com/cloudflare-one/)，或 SSO + 反向代理鉴权、WireGuard / Tailscale、仅内网 + VPN。

1. **Web**：仅通过 Zero Trust / 私有网络访问；公网不开放裸 HTTP(S) 到面板端口。
2. **gRPC（探针上报，默认 `5555`）**：按需要放行给探针，不要与未受保护的管理 Web 混在同一公网入口。默认明文；生产应启用 `grpc_tls`（见 [SECURITY.md](./SECURITY.md) 与 [配置参考](docs/configuration.md#grpc_tls-grpc-传输与设备证书)）。
3. 仍须配置 OAuth2 管理员白名单；访问控制不能替代登录鉴权。

未落实上述防护即公网裸奔面板，后果自负。

## 部署

### 一键安装（推荐）

```bash
sh -c "$(curl -fsSL https://raw.githubusercontent.com/santaizi-group/santanzi-dashboard/main/script/install_dashboard.sh)"
```

非 root 时先 `sudo -i`、`doas sh` 或 `su -` 再执行。也可先下载脚本再 `sh install_dashboard.sh`。

脚本会引导填写工作目录、Web 端口、gRPC 端口、OAuth2 与站点标题，并生成 `docker-compose.yml` 与 `config/dashboard.yaml`。填写 OAuth 前，先在提供商创建应用，回调地址必须为 `https://<面板域名>/oauth2/callback`。

### 手动部署

```bash
mkdir -p /opt/santaizi && cd /opt/santaizi
```

`docker-compose.yml`：

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

容器端口 `80` / `5555` 必须与 `/etc/santaizi/dashboard.yaml` 的 `httpport` / `grpcport` 一致。Web 端口可用 `SANTAIZI_PORT` 改映射，例如 `SANTAIZI_PORT=8080`。

```bash
mkdir -p config data
curl -fsSL https://raw.githubusercontent.com/santaizi-group/santanzi-dashboard/main/script/config.yaml -o config/dashboard.yaml
```

至少填写：

```yaml
httpport: 80
grpcport: 5555

site:
  brand: "三太子监控"
  primarycolor: "#2563eb"

oauth2:
  type: "github"
  admin: "your-github-username"
  clientid: "xxx"
  clientsecret: "xxx"
  endpoint: ""
```

GitHub 请创建 **OAuth App**（不是 GitHub App）。**Authorization callback URL 必须填 `https://<面板域名>/oauth2/callback`**，不要只填首页。各提供商控制台字段见 [配置参考 · oauth2](docs/configuration.md#oauth2-登录配置)。

```bash
docker compose up -d
```

启动后不要用公网 IP 直接打开面板。先接入 Cloudflare Zero Trust（或等价私有网络 / SSO 反向代理），再通过受保护入口登录 OAuth2。本地调试可用 `http://127.0.0.1:<SANTAIZI_PORT>`。

### 防火墙

- **Web**（默认 `80` / `SANTAIZI_PORT`）：不对公网直接放行；仅对本机、内网或 Zero Trust / Tunnel 出口开放。
- **gRPC**（`5555`）：按探针所在网络放行；与未受保护的管理 Web 入口分离。

### 更新

主面板：

```bash
cd /opt/santaizi
docker compose pull
docker compose up -d
```

从端：

```bash
curl -fsSL https://raw.githubusercontent.com/santaizi-group/santanzi-dashboard/main/script/upgrade_collector.sh | bash
```

### 安装探针

进入后台 → 服务器 → 添加服务器，保存后在卡片上复制对应平台的一键安装命令，到被监控主机执行。

默认探针下载源为 `santaizi-group/santaizi-agent`，可用 `SANTAIZI_AGENT_REPO` 覆盖。

### 常见问题

- **面板能否直接挂公网？** 不建议。须置于 Cloudflare Zero Trust 或等价访问控制之后。
- **探针无法连接面板**：检查防火墙是否放行 `5555`，以及 `grpcport` 是否为 `5555`。
- **一键安装脚本拉取失败**：可在 `config/dashboard.yaml` 的 `installscript` 段替换为可访问的脚本地址。
- **OAuth 登录失败 / redirect_uri mismatch**：提供商控制台的回调必须是 `https://<面板域名>/oauth2/callback`，不能只填首页；反向代理须转发 `/oauth2` 并带上 `Host` 与 `X-Forwarded-Proto`。
- **登录后没有管理员权限**：确认 `oauth2.admin` 填写的是 OAuth2 平台返回的用户名/ID。

可靠探测、从端部署、保留策略和升级顺序见 [可靠探测运维指南](docs/reliable-telemetry.md)。本版本只接受全新数据库；若数据库非空且没有 `schema_migrations`，进程会拒绝启动并保留原文件供诊断。
