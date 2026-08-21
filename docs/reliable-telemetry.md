# 可靠探测运维指南

Santaizi 采用单 Primary 控制面与多从端（Collector）探测面。探针先写本地 Segment WAL，再分别向 Primary 和已分配的从端发送；从端在本地事务提交后 ACK 探针，并通过持久 Outbox 将事实复制到 Primary。所有可靠链路都是至少一次投递，接收端按 Event、Observation、Replication Batch 去重，并且只在事务提交后返回累计连续 ACK。

## 数据与进程角色

- Primary：加载 OAuth、Web、API、服务监控、告警、内部调度、Availability、Rollup 与 Retention。
- 从端（Collector）：同一 Dashboard 二进制以 `mode: collector` 启动，只提供鉴权的探测接收、复制、`GetStatus` 与标准 gRPC Health；不提供 HTTP Health 或 Web UI。
- 探针（Agent）：默认配置 `/etc/santaizi/agent.yaml`，可靠数据目录 `/var/lib/santaizi-agent/`；WAL 默认 8 MiB Segment、256 MiB 总量、1 MiB 紧急预留。
- Dashboard：默认配置 `/etc/santaizi/dashboard.yaml`，Primary 数据库 `/var/lib/santaizi-dashboard/sqlite.db`，从端数据库默认 `/var/lib/santaizi-dashboard/collector.db`。

所有模式共用一个 gRPC 端口。`SantaiziService`、V2 分层服务和 `grpc.health.v1.Health` 同时注册。控制协议只能表达 HTTP、ICMP、TCP 探测以及独立鉴权的 NAT 流，不能表达命令、终端、文件或更新操作。

## 部署从端

1. 在 Primary 管理后台「可靠探测」创建从端，或调用管理 API：

   ```http
   POST /api/v2/admin/telemetry/collectors
   Content-Type: application/json

   {"name":"HK","address":"hk.example.com:5556","tls":true,"scopes":[{"type":"all","value":""}]}
   ```

2. 创建后打开「安装从端」，复制一键安装命令；或手动查看注册 Token。
3. 在从端主机执行安装命令（Docker）。脚本会写入 `mode: collector` 配置并启动容器。也可手动设置 `collector.registration_token` 与数据目录后启动。
4. 成功注册后会缓存 Primary 公钥、授权、Assignment、撤销状态和配置版本。
5. 用管理后台或 `PUT /api/v2/admin/telemetry/collectors/:id/scope` 更新 `all`、`server`、`group` 或 `tag` Scope。Scope/标签变化会产生新配置版本和带有效期的 Assignment，不删除历史 Evidence。
6. Rotate、Revoke 或 Delete 后旧 Token 立即失效。Delete 仅结束未来 Assignment；历史事实仍保留。删除后重新添加会获得新的稳定 ID/Generation。

生产公网 gRPC 推荐启用进程内 `grpc_tls`（Server 证由运营提供）并成对升级探针/从端。`primary_insecure_tls`、`--insecure` 和 endpoint 的 `insecure_tls` 只适用于受控测试。设备 CA 路径、迁移步骤与 `require_*_mtls` 开关见 [SECURITY.md](../SECURITY.md)。


一键安装脚本：[`script/install_collector.sh`](../script/install_collector.sh)。

## 探测型从端路由

探测型从端（创建时类型选探测型）在 ICMP / TCP / MTR 之外，可对目标跑超低频路由。管理后台探测从端编辑器里设置间隔（1 小时 / 1 天 / 1 周，默认 1 天）和保留次数（默认 10）。探针观察详情「路由」tab 查看最近记录，也可按需再跑一次。只跟该主机与从端的 ICMP / TCP 开关；关 ICMP 不跑 ICMP 路由，关 TCP 不跑 TCP 路由，与 MTR 开关无关。Ping 与自研 MTR 不依赖 nexttrace。

路由在从端本机 `exec` [nexttrace](https://github.com/nxtrace/NTrace-core) 二进制，**GHCR 镜像不内置**。找不到可执行文件时该次路由记错误、不出跳点，不影响 ping / MTR。二进制由运营自备，许可边界见 [`NOTICE`](../NOTICE)。路径不要写进管理后台表单。

配置项 `collector.nexttrace_path`（环境变量 `SANTAIZI_COLLECTOR_NEXTTRACE_PATH`）：

- 空：在容器 PATH 依次找 `nexttrace`、`nexttrace-tiny`
- 非空：必须是绝对路径且可执行；**不**再回退 PATH

一键安装在首次安装或带 `--token` 重写配置时，若宿主机 PATH 有上述二者之一，会只读挂到 `/opt/nexttrace/nexttrace` 并写入配置。升级且不传 `--token` 不改现有挂载；之后才安装 nexttrace 须手动改 compose，或带 `--token` 重写。

从端镜像基于 musl。把宿主机 glibc 动态链接的 nexttrace 挂进去可能无法执行，该次路由同样记错误。请使用与容器 ABI 匹配的静态或 musl 构建。单次超时约 45 秒，从端同时只跑一条。

## 可用性语义

每个 30 秒 Bucket 根据当时有效 Assignment 计算 expected，以 Observer Health 计算 healthy，以 Observation 计算 seen：

- 任一健康 Observer 看到节点：Host `ONLINE`。
- 无人看到且健康 Observer 数达到 `min_observers`：Host `OFFLINE`、Connectivity `UNAVAILABLE`。
- Observer 证据不足：Host 与 Connectivity 均为 `UNKNOWN`。
- 全部健康 Observer 看到为 `FULL`；部分看到为 `PARTIAL`。V2 `available` 对 FULL/PARTIAL 为 `true`，UNAVAILABLE 为 `false`，UNKNOWN 为 `null`。
- `[离线]` / `[恢复]` 与列表 `online`：V2 节点只认上述 Host 状态。主端或任一从端仍能看到节点时**不**判定离线；只有已分配且健康的观测点全部看不到、并持续达到顶层 `offlinethresholdseconds`（且不小于 `availability_bucket_seconds`）才开离线记录。告警规则 `offline` 指标（V2）同样按此共识判定，不受「记录离线历史」开关影响。V1 服务器仍按主端最后上报计时。`telemetry.offline_threshold_seconds` 只用于丢弃过旧的实时快照，不参与离线判定。
- Admin 可用性历史抽屉的「可用率」是完整连通率：`full / (full + partial + unavailable)`。部分连通拉低百分比并计入「降级」，不算中断；未知与缺口不进分母。公开站 30 天可用率仍按离线历史存活率，两套口径不要混读。

晚到 Replay 会重算历史 Bucket，并以不可变 Incident Revision 修正分类；Correction 通知默认关闭。V1 的布尔 `online` 因类型限制会把 UNKNOWN 映射为 `false`。

## 容量、丢失与诊断

- 探针 WAL 只删除所有当前可靠 Sink 都已 ACK 的完整 Segment。从端被正式删除后，相应义务解除。
- WAL 压力优先降采样并生成 1 分钟 Rollup；任何被替代、损坏或 Hard Limit 丢失都用显式 Gap/Data Loss 表示，不制造隐式 Sequence Hole。
- 从端 Spool 默认 5 GiB/30 天。正常只清理 Primary 已 ACK 数据；Hard Limit 必须丢弃未同步数据时，会生成可复制的 Gap 与 Data Loss 事实。
- 后台 `/admin/telemetry` 和 `/api/v2/telemetry/servers/:id/status` 展示 WAL、Sink Cursor、Spool、Replication Backlog、Gap 与 Data Loss。应用日志不包含 Secret。
- 运行基准：`go test -run '^$' -bench BenchmarkSyntheticTelemetry1000x10 -benchtime=1x ./service/telemetry`。

## 数据库与升级

本架构只支持版本化 Santaizi 数据库。若目标 SQLite 非空但没有 `schema_migrations`（从端为 `collector_schema_migrations`），进程会拒绝启动并提示配置空数据库；不会导入其他产品数据库、身份或协议状态。上线前请单独备份现有数据库、配置和凭证主密钥。

发布顺序固定为：Primary → 从端 → 清洁安装探针。Dashboard 与 Agent 必须成对升级；不要将从端与探针角色混淆。

已安装从端在本机执行 [`script/upgrade_collector.sh`](../script/upgrade_collector.sh)（或重跑安装脚本且不传 `--token`）即可拉取镜像并重建容器，不改配置与数据目录。指定版本：`bash -s -- v1.0.1`。
