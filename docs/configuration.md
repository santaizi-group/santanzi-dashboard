# 配置参考

Dashboard 的配置文件默认位于 `/etc/santaizi/dashboard.yaml`，SQLite 与探测数据默认位于 `/var/lib/santaizi-dashboard/`。两者均可通过 CLI 或配置覆盖。配置加载顺序：

1. 环境变量（前缀 `SANTAIZI_`，下划线替换为点）
2. `/etc/santaizi/dashboard.yaml`

例如：

```bash
SANTAIZI_DEBUG=true
SANTAIZI_HTTPPORT=8080
SANTAIZI_SITE_BRAND="My Monitor"
SANTAIZI_TELEMETRY_STATE_INTERVAL_SECONDS=5
```

等价于：

```yaml
debug: true
httpport: 8080
site:
  brand: "My Monitor"
```

---

## 顶层配置项

| 配置项 | 类型 | 默认值 | 说明 |
|--------|------|--------|------|
| `debug` | bool | `false` | Debug 模式；开启后启用 `/debug/pprof` 和 `mock` OAuth2 |
| `mode` | string | `primary` | `primary` 启动控制面与 Web；`collector` 只启动探测接收、复制与 gRPC Health |
| `language` | string | `zh-CN` | 系统语言 |
| `httpport` | uint | `80` | Dashboard Web 端口 |
| `grpcport` | uint | `5555` | Agent gRPC 上报端口 |
| `grpchost` | string | `""` | 面板对外域名/IP，用于生成 Agent 一键安装命令 |
| `proxygrpcport` | uint | `0` | 如果设置，生成安装命令时使用该端口代替 `grpcport` |
| `tls` | bool | `false` | 下发探针以 TLS 连接 Primary；证书通常由公网 gRPC 反向代理终止 |
| `grpc_tls` | object | 见下 | 面板/从端进程自身的 gRPC Server TLS 与设备 mTLS。默认全关，旧配置可直接启动 |
| `enableplainipinnotification` | bool | `false` | 通知中 IP 是否不打码 |
| `enableipchangenotification` | bool | `false` | 是否启用服务器 IP 变动通知 |
| `ipchangenotificationtag` | string | `default` | IP 变动通知使用的通知组 |
| `cover` | uint8 | `0` | IP 变动覆盖范围：`0`=全部服务器（排除特定），`1`=仅特定服务器 |
| `ignoredipnotification` | string | `""` | 特定服务器 ID，逗号分隔 |
| `location` | string | `Asia/Shanghai` | 时区 |
| `maxtcppingvalue` | int32 | `1000` | TCP Ping 最大值（ms） |
| `avgpingcount` | int | `2` | 平均 Ping 次数 |
| `dnsservers` | string | `""` | 自定义 DNS 服务器，逗号分隔 |
| `enableofflinehistory` | bool | `true` | 是否启用服务器离线历史 |
| `offlinethresholdseconds` | uint64 | `30` | 离线判定阈值（秒，最小 `10`）。V2 节点衡量「所有已分配且健康的观测点连续看不到该节点多久」，不是「主端多久没直收上报」；实际生效粒度不会小于 `telemetry.availability_bucket_seconds`。V1 服务器仍按主端最后上报计时 |
| `offlinecheckintervalseconds` | uint64 | `10` | 离线检测间隔（秒，最小 `5`，且 ≤ 阈值）；修改后热生效，无需重启。V2 节点在该周期内读取 Availability Bucket 共识 |
| `offlinemergegapseconds` | uint64 | `10` | 离线合并间隔（秒，1~3600）：相邻两次离线之间的在线时间 ≤ 该值时合并为一次，默认 10 |
| `offlinehistoryretentiondays` | uint64 | `365` | 离线历史保留天数 |
| `enableofflinenotification` | bool | `false` | 离线时发送通知 |
| `enablerecoverynotification` | bool | `false` | 恢复时发送通知 |
| `showavailabilitytoguest` | bool | `false` | 是否向前台访客展示服务器可用性摘要（30 天可用率、离线次数等）。公开站该可用率按离线历史存活率计算；Admin 可用性历史抽屉的「可用率」是完整连通率（部分连通拉低百分比，不算离线），二者不是同一口径 |

### `grpc_tls` gRPC 传输与设备证书

设备身份由两套独立 CA 签发，**禁止**用 Agent CA 给从端签身份证，也**永不**下发任何 CA 私钥。Server 证书由运营提供，不用设备 CA 签。

| 配置项 | 默认值 | 说明 |
|--------|--------|------|
| `grpc_tls.enabled` | `false` | 为 true 时 gRPC 监听 TLS1.2+；缺 `cert_file`/`key_file` 则拒绝启动 |
| `grpc_tls.cert_file` | `""` | Server 证书 PEM |
| `grpc_tls.key_file` | `""` | Server 私钥 PEM |
| `grpc_tls.client_ca_file` | `""` | 额外客户端 CA bundle；空则只用内置 Agent CA + Collector CA |
| `grpc_tls.require_agent_mtls` | `false` | 探针 Control/Ingest/NAT/Renew 必须出示 Agent 证 |
| `grpc_tls.require_collector_mtls` | `false` | 从端 Sync/Replicate/RenewCollector 必须出示 Collector 证 |

内置 CA 默认目录：`/var/lib/santaizi-dashboard/pki/`（`agent-ca.{key,crt}` 与 `collector-ca.{key,crt}`）。`enabled=true` 时 assignment 的 primary `Tls=true`。

```yaml
grpc_tls:
  enabled: false
  cert_file: ""
  key_file: ""
  client_ca_file: ""
  require_agent_mtls: false
  require_collector_mtls: false
```

迁移：先升面板、两开关保持 false → 配置 Server 证并打开 `grpc_tls.enabled` → 升探针完成 Enroll → 升从端 Register+CSR → 确认两边已走 mTLS → 再分别打开 `require_agent_mtls` / `require_collector_mtls`。注册 Token 与 `client_secret` 变为 bootstrap；`--insecure` 仅测试。卸载重装会换 identity；旧探针已下线时同一密钥允许 Enroll 换绑。

> 离线历史相关配置在后台设置页面保存后立即生效，无需重启 Dashboard。

---

## GeoIP 数据库

公开站地区码由探针上报，或由面板用 GeoIP 库回填。库文件是 MaxMind / IPInfo 格式的 `country.mmdb`。

| 来源 | 说明 |
|------|------|
| 环境变量 `SANTAIZI_GEOIP_DB` | 运行时外部库路径，优先使用。这不是 YAML 配置项，不会映射进 `dashboard.yaml`。 |
| 内嵌 `pkg/geoip/geoip.db` | Release 构建会在发版时拉取真实库；源码树里是占位 stub，查不到地区。 |

未设置 `SANTAIZI_GEOIP_DB` 且内嵌库不可用时，面板不查 GeoIP。国家码依赖探针 Cloudflare `loc=` 或手填 `--country-code`。

```bash
SANTAIZI_GEOIP_DB=/var/lib/santaizi-dashboard/country.mmdb
```

---

## `site` 站点配置

| 配置项 | 默认值 | 说明 |
|--------|--------|------|
| `site.brand` | `Santaizi Monitoring` | 站点标题 |
| `site.cookiename` | `santaizi-dashboard` | 登录 Cookie 名 |
| `site.viewpassword` | `""` | 前台查看密码，留空表示不需要 |
| `site.primarycolor` | `#2563eb` | ServerStatus 品牌色 |
| `site.footertext` | `""` | 公开站页脚文字 |
| `site.logourl` | `/static/logo.svg` | 本地或 data image Logo |
| `site.safecustomcss` | `""` | 受限 CSS；禁止远程和可执行规则 |
| `web.delivery` | `embedded` | `embedded` 或同域反向代理下的 `external` |

---

## `oauth2` 登录配置

| 配置项 | 必填 | 说明 |
|--------|------|------|
| `oauth2.type` | 是 | 可选：`github`、`gitee`、`gitlab`、`jihulab`、`gitea`、`cloudflare`、`oidc`、`mock` |
| `oauth2.admin` | 是 | 管理员用户名/ID，逗号分隔。GitHub / Gitee 填登录用户名；GitLab / Jihulab / Gitea 填 username；Cloudflare Access 填用户 `sub`；OIDC 填 `oidcloginclaim` 对应声明（默认 `sub`） |
| `oauth2.admingroups` | 否 | OIDC 管理员用户组 |
| `oauth2.clientid` | 除 `mock` 外 | OAuth2 Client ID |
| `oauth2.clientsecret` | 除 `mock` 外 | OAuth2 Client Secret |
| `oauth2.endpoint` | 自建时 | 自建 Gitea 源站，或 Cloudflare Access 团队域名（如 `https://<team>.cloudflareaccess.com`） |
| `oauth2.oidcdisplayname` | 否 | 默认 `OIDC` |
| `oauth2.oidcissuer` | OIDC | OIDC issuer URL |
| `oauth2.oidclogouturl` | 否 | OIDC 登出地址 |
| `oauth2.oidcregisterurl` | 否 | OIDC 注册地址 |
| `oauth2.oidcloginclaim` | 否 | 默认 `sub` |
| `oauth2.oidcgroupclaim` | 否 | 默认 `groups` |
| `oauth2.oidcscopes` | 否 | 默认 `openid,profile,email` |
| `oauth2.oidcautocreate` | 否 | 是否自动创建用户，默认 `false` |
| `oauth2.oidcautologin` | 否 | 是否自动登录，默认 `false` |

> `mock` 类型仅在 `debug: true` 时可用，仅用于本地开发。
> OAuth2 只在 `mode: primary` 时要求配置；Collector 模式不会加载 OAuth、HTTP、业务 UI、告警或内部调度。

### 回调地址（提供商控制台必填）

面板登录入口是 `/oauth2/login`，提供商授权后必须回到 **`/oauth2/callback`**。完整回调 URL 由用户实际访问面板的协议和 Host 拼出：

```text
https://<你打开面板时的域名>/oauth2/callback
```

示例：面板是 `https://dash.example.com`，则回调填：

```text
https://dash.example.com/oauth2/callback
```

本地调试（`http://127.0.0.1:8000`）则填：

```text
http://127.0.0.1:8000/oauth2/callback
```

规则：

- **不要只填首页**（`https://dash.example.com` 会登录失败）。
- 必须与浏览器地址栏的协议、域名、端口完全一致（`www` / 非 `www`、`:443` 是否写出，都不能混用）。
- 路径固定为 `/oauth2/callback`，不要加尾斜杠，不要改成 `/admin/` 或 `/oauth2/login`。
- GitHub 请创建 **OAuth App**（不是 GitHub App）；Device Flow 不要勾。
- 反向代理必须把 `/oauth2` 转到面板，并保留 `Host` 与 `X-Forwarded-Proto`（HTTPS 反代时设为 `https`），否则面板会把回调拼成 `http://...`，与控制台不一致。

### GitHub OAuth App

创建页：<https://github.com/settings/applications/new>（Settings → Developer settings → OAuth Apps → New OAuth App）。

| 控制台字段 | 填什么 |
|------------|--------|
| Application name | `三太子` 或你的站点名 |
| Homepage URL | `https://dash.example.com`（面板公网地址） |
| Application description | 可空 |
| **Authorization callback URL** | **`https://dash.example.com/oauth2/callback`** |
| Enable Device Flow | 不勾 |

注册后复制 **Client ID**，生成 **Client Secret**，写入配置：

```yaml
oauth2:
  type: github
  admin: your-github-username
  clientid: xxxxxxxxxxxxxxxxxxxx
  clientsecret: xxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxx
```

### Gitee

创建页：<https://gitee.com/oauth/applications>。

| 控制台字段 | 填什么 |
|------------|--------|
| 应用主页 | `https://dash.example.com` |
| **应用回调地址** | **`https://dash.example.com/oauth2/callback`** |

```yaml
oauth2:
  type: gitee
  admin: your-gitee-username
  clientid: xxx
  clientsecret: xxx
```

### GitLab / Jihulab

GitLab：User Settings → Applications。Jihulab 同理。权限勾选 `read_user`、`read_api`。

| 控制台字段 | 填什么 |
|------------|--------|
| Redirect URI / 回调 URL | **`https://dash.example.com/oauth2/callback`** |

```yaml
oauth2:
  type: gitlab   # 或 jihulab
  admin: your-gitlab-username
  clientid: xxx
  clientsecret: xxx
```

### Gitea

站点管理 → 应用 → 创建 OAuth2 应用。`oauth2.endpoint` 填 Gitea 源站，不要带路径。

| 控制台字段 | 填什么 |
|------------|--------|
| Redirect URIs | **`https://dash.example.com/oauth2/callback`** |

```yaml
oauth2:
  type: gitea
  admin: your-gitea-username
  clientid: xxx
  clientsecret: xxx
  endpoint: https://git.example.com
```

### Cloudflare Access

在 Access 里创建 OIDC 应用。`oauth2.endpoint` 填团队域名。

| 控制台字段 | 填什么 |
|------------|--------|
| Redirect URLs | **`https://dash.example.com/oauth2/callback`** |

```yaml
oauth2:
  type: cloudflare
  admin: "Access 返回的 sub"
  clientid: xxx
  clientsecret: xxx
  endpoint: https://<team>.cloudflareaccess.com
```

### OIDC

在 IdP 登记的 Redirect URI / callback 同样必须是 **`https://dash.example.com/oauth2/callback`**。

```yaml
oauth2:
  type: oidc
  admin: admin-user-id
  admingroups: admin-group
  clientid: santaizi-dashboard
  clientsecret: xxxxxxxxxxxx
  oidcdisplayname: SSO
  oidcissuer: https://auth.example.com
  oidclogouturl: https://auth.example.com/logout
  oidcscopes: openid,profile,email
  oidcgroupclaim: groups
  oidcautocreate: true
```

---

## `telemetry` 可靠探测

| 配置项 | 默认值 | 说明 |
|--------|--------|------|
| `telemetry.data_dir` | `/var/lib/santaizi-dashboard` | 签名密钥和探测运行数据目录 |
| `telemetry.signing_key_path` | `<data_dir>/telemetry-signing.key` | Primary Ed25519 私钥；必须持久保存并限制权限 |
| `telemetry.primary_endpoint` | `grpchost` 或本机 gRPC | Agent 控制流下发的 Primary 地址 |
| `telemetry.state_interval_seconds` | `5` | State 采样间隔 |
| `telemetry.heartbeat_interval_seconds` | `10` | Heartbeat 间隔 |
| `telemetry.offline_threshold_seconds` | `30` | 实时快照新鲜度校验（丢弃采集时刻过旧的 realtime snapshot）；与顶层 `offlinethresholdseconds` 不是同一项 |
| `telemetry.ingest_batch_size` | `256` | V2 接收批大小 |
| `telemetry.ingest_queue_size` | `4096` | 接收侧有界容量 |
| `telemetry.credential_validity_days` | `30` | Agent 探测凭据有效期 |
| `telemetry.credential_refresh_days` | `7` | 到期前刷新窗口 |
| `telemetry.credential_grace_days` | `7` | Collector 离线且已有授权时的过期宽限 |
| `telemetry.availability_bucket_seconds` | `30` | Availability Bucket 大小 |
| `telemetry.min_observers` | `1` | 判定 `OFFLINE` 所需的最少健康 Observer 数 |

`enable_connectivity_notification`、`enable_correction_notification`、`enable_collector_offline_notification`、`enable_collector_online_notification` 与 `enable_data_loss_notification` 位于 `telemetry` 下，默认均为 `false`。Host 离线/恢复通知继续由顶层 `enableofflinenotification` 与 `enablerecoverynotification` 控制。`enable_collector_online_notification` 只在从端曾被记为离线后再上线时发送 `[从端上线]`，不覆盖首次接入。

## `collector` Collector 模式

| 配置项 | 默认值 | 说明 |
|--------|--------|------|
| `collector.primary_endpoint` | 无 | Primary gRPC 地址，Collector 模式必填 |
| `collector.primary_tls` | `false` | 连接 Primary 时使用 TLS |
| `collector.primary_insecure_tls` | `false` | 跳过证书校验，仅供受控测试 |
| `collector.registration_token` | 无 | Primary 管理后台可查看和轮换的注册 Token |
| `collector.database_path` | `<telemetry.data_dir>/collector.db` | Collector 本地 SQLite |
| `collector.spool_max_bytes` | `5368709120` | Spool Hard Limit（5 GiB） |
| `collector.spool_max_age_days` | `30` | Spool 保留上限 |
| `collector.status_authorization` | 无 | 调用鉴权 `GetStatus` 的共享值 |

示例：

```yaml
mode: collector
grpcport: 5556
telemetry:
  data_dir: /var/lib/santaizi-dashboard
collector:
  primary_endpoint: primary.example.com:5555
  primary_tls: true
  registration_token: "从 Primary 管理后台复制的注册 Token"
  database_path: /var/lib/santaizi-dashboard/collector.db
  spool_max_bytes: 5368709120
  spool_max_age_days: 30
  status_authorization: "本地运维鉴权值"
grpc_tls:
  enabled: false
  cert_file: /var/lib/santaizi-dashboard/pki/server.crt
  key_file: /var/lib/santaizi-dashboard/pki/server.key
```

一键安装脚本会写出 `primary_tls` / `primary_insecure_tls` 与默认关闭的 `grpc_tls`。从端对外 gRPC 要开 TLS 时放入证书并把 `enabled` 改为 true。

## `rollup` 与 `retention`

| 配置项 | 默认值 |
|--------|--------|
| `rollup.enabled` | `true` |
| `rollup.batch_size` | `1000` |
| `retention.state_raw_hours` | `6` |
| `retention.state_one_minute_days` | `30` |
| `retention.state_one_hour_days` | `365` |
| `retention.observation_days` | `30` |
| `retention.evidence_hours` | `48` |
| `retention.lifecycle_days` | `3650` |
| `retention.batch_size` | `5000` |
| `retention.max_runtime_ms` | `20000` |
| `retention.receipt_days` | `7` |
| `retention.compact_min_bytes` | `67108864`（64MiB） |
| `retention.auto_compact` | `true` |

Primary 每 5 分钟限时清过期探测数据。心跳 / 原始状态 / 探针压力汇总**不写入**事件表；实时状态走内存，完成分钟后写入 1 分钟聚合。路径桶 / 健康桶 / 连接延迟桶 / 探测样本 / 探测快照只留 `evidence_hours`（默认 48 小时）。HOST / 探测告警 / 数据丢失仍 30 天；复制回执 7 天。证据窗外的可用性 30 秒桶按相同连通状态跑长压缩，压缩后的段仍按 `lifecycle_days`（默认约 10 年）保留。已完成的 1 分钟窗口会立刻删除遗留的高频空壳行。监控历史（按主机 1 天、服务页 30 天）、流量点（48 小时）也走同一轮清理。`batch_size` / `max_runtime_ms` 为 0 时用上表默认。

`auto_compact` 为 true 时：每天 03:30 与设置页「优化数据库」在可回收空间 ≥ `compact_min_bytes` 且剩余磁盘不少于当前库文件时执行整库 `VACUUM`。存量 `auto_vacuum=NONE` 的库、或一次可回收 ≥ `compact_min_bytes` 的 8 倍时，5 分钟维护也会自动压缩（6 小时冷却）。压缩会短暂停写，并大约需要一倍空闲磁盘；磁盘不够则跳过。新库启用 `INCREMENTAL` auto_vacuum，日常按批增量回收。设置页按钮只启动后台任务，不在请求线程里 `VACUUM`。

---

## `installscript` 安装脚本源

用于 Dashboard 中生成一键安装命令和升级命令的脚本地址。

| 配置项 | 默认值 |
|--------|--------|
| `installscript.linux` | `https://raw.githubusercontent.com/santaizi-group/santanzi-dashboard/main/script/install_agent.sh` |
| `installscript.linuxen` | `https://raw.githubusercontent.com/santaizi-group/santanzi-dashboard/main/script/install_agent_en.sh` |
| `installscript.windows` | `https://raw.githubusercontent.com/santaizi-group/santanzi-dashboard/main/script/install.ps1` |
| `installscript.macos` | `https://raw.githubusercontent.com/santaizi-group/santanzi-dashboard/main/script/install.command` |
| `installscript.collector` | `https://raw.githubusercontent.com/santaizi-group/santanzi-dashboard/main/script/install_collector.sh` |
| `installscript.dashboard` | `https://raw.githubusercontent.com/santaizi-group/santanzi-dashboard/main/script/install_dashboard.sh` |
| `installscript.upgrade_collector` | `https://raw.githubusercontent.com/santaizi-group/santanzi-dashboard/main/script/upgrade_collector.sh` |
| `installscript.upgrade_linux` | `https://raw.githubusercontent.com/santaizi-group/santanzi-dashboard/main/script/upgrade_agent.sh` |
| `installscript.upgrade_linuxen` | `https://raw.githubusercontent.com/santaizi-group/santanzi-dashboard/main/script/upgrade_agent_en.sh` |
| `installscript.upgrade_windows` | `https://raw.githubusercontent.com/santaizi-group/santanzi-dashboard/main/script/upgrade.ps1` |
| `installscript.upgrade_macos` | `https://raw.githubusercontent.com/santaizi-group/santanzi-dashboard/main/script/upgrade.command` |

如果你 fork 了仓库或在内网部署，建议替换为自有地址。

---

## 配置示例

```yaml
debug: false
mode: primary
language: zh-CN
httpport: 80
grpcport: 5555
grpchost: santaizi.example.com
proxygrpcport: 443
tls: true
location: Asia/Shanghai

site:
  brand: "三太子监控"
  viewpassword: ""
  primarycolor: "#2563eb"

web:
  delivery: embedded

# GitHub 控制台 Authorization callback URL 必须填 https://<面板域名>/oauth2/callback
oauth2:
  type: github
  admin: "your-github-username"
  clientid: "xxx"
  clientsecret: "xxx"

enableofflinehistory: true
offlinethresholdseconds: 30
offlinecheckintervalseconds: 10
offlinehistoryretentiondays: 365
enableofflinenotification: false
enablerecoverynotification: false

telemetry:
  data_dir: /var/lib/santaizi-dashboard
  secret_key_path: /var/lib/santaizi-dashboard/business-secrets.key
  primary_endpoint: santaizi.example.com:443
  state_interval_seconds: 5
  heartbeat_interval_seconds: 10
  offline_threshold_seconds: 30
  availability_bucket_seconds: 30
  min_observers: 1

rollup:
  enabled: true
  batch_size: 1000

retention:
  state_raw_hours: 6
  state_one_minute_days: 30
  state_one_hour_days: 365
  observation_days: 30
  evidence_hours: 48
  lifecycle_days: 3650
  batch_size: 5000
  max_runtime_ms: 20000
  receipt_days: 7
  compact_min_bytes: 67108864
  auto_compact: true
```

---

## 在线修改配置

大部分业务配置可以在 Dashboard 的 **设置** 页面（`/admin/settings`）中在线修改并保存到 `/etc/santaizi/dashboard.yaml`。修改后无需重启，即时生效。`grpchost`、`proxygrpcport` 和 `tls` 保存后立即用于探针/从端安装命令。

`mode`、本机监听端口（`httpport` / `grpcport`）、数据目录、签名密钥、Collector 连接以及 Rollup/Retention Worker 配置需要重启才能完整生效。

`telemetry.secret_key_path` 保存业务凭证的 AES-256-GCM 主密钥。Primary 首次启动会以 `0600` 权限生成该文件；备份数据库时必须一并备份，且不得通过后台或 API 读取。

> 数据库策略：仅接受空数据库，或由当前版本创建且包含 `schema_migrations` 的数据库；其他非空数据库会拒绝启动。
