# 快速开始

本指南使用 Docker Compose 在 5 分钟内完成 Dashboard 部署，并接入第一台 Agent。

## 1. 创建目录

```bash
mkdir -p ~/santaizi/data ~/santaizi/config
cd ~/santaizi
```

## 2. 写入 docker-compose.yml

```yaml
services:
  santaizi-dashboard:
    image: ghcr.io/santaizi-group/santaizi-dashboard:latest
    container_name: santaizi-dashboard
    restart: unless-stopped
    ports:
      - "80:80"
      - "5555:5555"
    volumes:
      - /etc/timezone:/etc/timezone:ro
      - /etc/localtime:/etc/localtime:ro
      - ./data:/var/lib/santaizi-dashboard
      - ./config/dashboard.yaml:/etc/santaizi/dashboard.yaml:ro
    environment:
      - TZ=Asia/Shanghai
```

## 3. 写入初始配置

创建 `config/dashboard.yaml`：

```yaml
debug: false
mode: primary
language: zh-CN
httpport: 80
grpcport: 5555
site:
  brand: "三太子监控"
  primarycolor: "#2563eb"
oauth2:
  type: "github"
  admin: "your-github-username"
  clientid: "your-client-id"
  clientsecret: "your-client-secret"
telemetry:
  data_dir: /var/lib/santaizi-dashboard
```

将 `oauth2` 替换为你实际使用的 OAuth2 提供商信息。创建应用时，**回调地址必须填 `https://<面板域名>/oauth2/callback`**（GitHub 字段名是 Authorization callback URL），不要只填首页。各提供商字段与 YAML 见 [配置参考](configuration.md#oauth2-登录配置)。本地测试可使用 `type: mock`（需同时设置 `debug: true`）。

## 4. 启动 Dashboard

```bash
docker compose up -d
```

启动后访问 `http://<服务器IP>`，使用 OAuth2 管理员账号登录。

## 5. 添加第一台服务器

1. 登录 Dashboard，进入 **服务器器** 页面。
2. 点击 **添加服务器**，填写名称、分组等信息后保存。
3. 复制生成的一键安装命令，在目标机器上执行：

```bash
curl -fSL https://raw.githubusercontent.com/santaizi-group/santanzi-dashboard/main/script/install_agent.sh | bash -s -- install_agent <面板地址> <端口> <密钥>
```

例如：

```bash
curl -fSL https://raw.githubusercontent.com/santaizi-group/santanzi-dashboard/main/script/install_agent.sh | bash -s -- install_agent 10.0.0.10 5555 abcdef123456
```

`grpchost` 是域名时，管理后台生成的命令会附带 `--server-ip`，供探针在本机 dig 失败时先连上；DNS 解析成功则以解析结果为准并更新缓存。

4. 安装完成后，Agent 会自动启动并上报数据。返回 Dashboard 即可看到服务器上线。

## 6. 下一步

- 查看 [配置参考](configuration.md) 调整阈值、品牌外观、通知等。
- 查看 [服务器管理](servers.md) 了解分组、排序和离线历史。
- 查看 [服务监控](monitoring.md) 配置网站/端口监控。
