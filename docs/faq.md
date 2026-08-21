# 常见问题

## Dashboard

### Q: Dashboard 启动失败，提示端口被占用

A: 检查 `httpport` 和 `grpcport` 是否被其他进程占用。使用：

```bash
ss -tlnp | grep -E ':80|:5555'
```

停止占用进程或更换端口。

### Q: 修改配置后没有生效

A: 大部分配置在 **设置** 页面保存后立即生效。涉及端口、TLS 等底层配置需要重启 Dashboard。

### Q: 如何查看 Dashboard 日志

Docker：

```bash
docker logs -f santaizi-dashboard
```

二进制：

```bash
journalctl -u santaizi-dashboard -f
```

### Q: 如何备份数据

备份以下文件：

```
/var/lib/santaizi-dashboard/sqlite.db
/var/lib/santaizi-dashboard/sqlite.db-shm
/var/lib/santaizi-dashboard/sqlite.db-wal
/etc/santaizi/dashboard.yaml
/var/lib/santaizi-dashboard/telemetry-signing.key
```

Collector 另需备份其 `collector.db` 和凭证主密钥。Agent 的 `/var/lib/santaizi-agent/` 包含身份、Cursor 与尚未 ACK 的 WAL；执行清洁安装时这些数据会按确认删除并生成新身份。

---

## 从端

### Q: 探针观察「路由」没有跳点

A: 路由要在探测型从端本机执行 nexttrace，GHCR 镜像不带这个二进制。确认：

1. 从端类型是探测型，且该主机与从端的 ICMP 或 TCP 已打开（关哪路就不跑哪路；与 MTR 开关无关）
2. 容器内能执行 `collector.nexttrace_path` 指向的文件，或 PATH 上有 `nexttrace` / `nexttrace-tiny`
3. 镜像是 musl：宿主机 glibc 动态链接的 nexttrace 挂进去可能跑不起来

找不到可执行文件或 ABI 不匹配时，该次路由会记错误，ping / MTR 仍正常。安装与路径见 [可靠探测运维指南](reliable-telemetry.md#探测型从端路由)。

---

## Agent

### Q: Agent 安装后没有上线

A: 检查：

1. Dashboard 的 gRPC 端口是否可达
2. Agent 配置中的地址、端口、Secret 是否正确
3. 防火墙是否放行 gRPC 端口
4. Agent 服务是否已启动：`systemctl status santaizi-agent`

### Q: Agent 日志在哪里

Linux / macOS：

```bash
journalctl -u santaizi-agent -f
```

Windows：事件查看器或服务日志目录。

### Q: 如何更新 Agent

Dashboard 不具备远程或自动更新 Agent 的能力。已安装的探针在本机执行：

```bash
curl -fSL https://raw.githubusercontent.com/santaizi-group/santanzi-dashboard/main/script/upgrade_agent.sh | bash
```

Windows 使用 `script/upgrade.ps1`。管理后台「主机管理」行内「升级探针」也可复制同一条命令。升级只换二进制并重启服务，不改配置和节点身份。协议破坏性升级时使用已确认的清洁安装，并先升级面板/从端。

### Q: Agent 是否需要 root

Agent 普通用户即可运行，但部分指标（如某些温度、进程信息）可能需要 root 权限才能采集。

---

## 监控与告警

### Q: 监控状态正常但没有收到通知

A: 检查：

1. 通知方式配置是否正确，可点击 **测试** 验证
2. 监控项是否选择了通知组
3. 告警规则是否配置了通知组
4. 通知中的 IP 是否被脱敏，但不影响送达

### Q: 离线历史里有很多“原因未知”

A: 原因判定依赖 Agent 上报的 `BootTime` 和 `Uptime`。采集被关闭或证据不足时会显示为“原因未知”，不影响离线记录本身。

---

## 安全

### Q: 是否可以不使用 OAuth2

A: 生产环境强烈建议使用 OAuth2 / OIDC。`mock` 类型仅在本地开发 `debug: true` 时可用，不适合生产。

### Q: GitHub / 其他平台的 OAuth 回调地址填什么

A: 固定为 `https://<你打开面板时的域名>/oauth2/callback`。GitHub 对应字段是 **Authorization callback URL**。只填首页（没有 `/oauth2/callback`）会导致授权后无法回到面板。完整控制台字段与 YAML 见 [配置参考](configuration.md#oauth2-登录配置)。

### Q: 如何保护 gRPC 端口

A: 建议：

1. 仅对 Agent 开放 gRPC 端口
2. 使用防火墙限制访问来源
3. 如需公网传输，开启 gRPC TLS（`tls: true`）并配置反向代理

### Q: 前台查看密码是什么

A: `site.viewpassword` 用于给未登录用户访问前台页面时加一道密码。登录后的管理员不受影响。

---

## 其他

### Q: 如何切换语言

A: 修改 `config.yaml` 中的 `language`，支持 `zh-CN`、`zh-TW`、`es-ES`、`en` 等。

### Q: 如何自定义 Agent 安装脚本源

A: 修改 `config.yaml` 中的 `installscript` 配置项，或设置环境变量 `SANTAIZI_SCRIPT_URL`。

### Q: 如何贡献主题

A: 前端已统一为 Vue 应用，不再支持 Go 模板主题。请在管理后台使用品牌色、Logo、背景和受限 CSS 定制；完整前端改动应在 `web/apps/status` 中开发。
