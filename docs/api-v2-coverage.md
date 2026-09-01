# API v2 与管理页面覆盖矩阵

`openapi/v2.yaml` 是 HTTP API 的唯一契约。Go DTO/接口与 Axios SDK 均由该文件生成；管理页面只调用生成 SDK 的类型化封装。

| 业务能力 | OpenAPI operationId | 页面操作 | 自动化验收 |
|---|---|---|---|
| 登录与退出 | `getSession`, `logout` | 顶栏用户与退出、侧栏面板版本 | 会话含 `version`、CSRF 与 SPA 导航 |
| 服务器 CRUD | `createServer`, `updateServer`, `deleteServer` | `/admin/servers` 专用编辑弹窗与删除 | 公开备注结构化编辑、脏数据关闭确认 |
| 服务器批量管理 | `batchUpdateServerGroup`, `batchDeleteServers` | 表格多选、批量分组和删除 | 批量请求与危险确认 |
| 主机配置备份 | `exportServers`, `previewServerImport`, `importServers` | `/admin/servers` 工具栏导出 JSON、导入预览后按名称新建或覆盖 | 导出含明文密钥，下载前二次确认；新建复用未占用密钥，冲突则重新生成；覆盖流量策略去 id 全量替换且不改密钥 |
| 服务器排序与分组管理 | `updateServerDisplayIndex`, `listServerGroups`, `renameServerGroup` | 列表行内改序、分组管理弹窗、编辑器分组下拉 | 单字段改序与派生 tag 重命名/合并 |
| 凭据与安装 | `getServerCredential`, `resetServerSecret`, `getProbeCapabilities`, `getServerInstallPreview`, `getServerUpgradePreview` | 密钥查看/复制、分平台能力化安装弹窗（Go / Rust）、分平台升级命令复制 | 标准·云/标准·物理/轻量/仅存活、IP 位置子选项与清洁安装确认；Rust 仅 Linux 且 `implementation=rust`；升级命令不含密钥 |
| 流量策略 | `createServer`/`updateServer` 可选 `traffic_policies` 原子提交；`listTrafficPolicies`, `createTrafficPolicy`, `updateTrafficPolicy`, `deleteTrafficPolicy`, `getTrafficPolicyUsage`, `getServerTrafficHistory`；`listServers` 附 `traffic_summaries` | 服务器编辑器内多策略卡片，随服务器一次保存；主机列表流量列与历史抽屉流量页签（近 24 小时 / 按天） | 累计/周期策略与用量进度；缺名称不发写请求；后端 400 时服务器未创建；列表无策略不占位；历史按 `tz` 切日界 |
| 可用性与离线历史 | `listServerAvailability`, `resetServerAvailability`, `deleteOfflineHistory`, `cleanupOfflineHistory` | 服务器历史抽屉与设置页清理 | 历史读取、重置和删除 |
| 服务监控 | `createMonitor`, `updateMonitor`, `deleteMonitor` | `/admin/services` HTTP/ICMP/TCP 编辑弹窗和服务器穿梭框 | 完整 CRUD、范围和历史 |
| 通知渠道 | `createNotification`, `updateNotification`, `deleteNotification`, `testNotification` | `/admin/notifications` 类型化请求编辑器 | 请求头键值表、请求体、TLS 与测试 |
| 告警规则 | `createAlertRule`, `updateAlertRule`, `deleteAlertRule` | `/admin/alert-rules` 可视化条件卡片 | 指标、阈值、持续时间、服务器范围和通知组 |
| DDNS | `createDDNSProfile`, `updateDDNSProfile`, `deleteDDNSProfile` | 附加功能中的 Provider 驱动编辑器 | 域名、协议、凭据和 Webhook 动态字段 |
| NAT | `createNATTunnel`, `updateNATTunnel`, `deleteNATTunnel` | 附加功能中的服务器选择器与目标表单 | 完整 CRUD 与目标格式校验 |
| 系统设置 | `updateSettings`, `getDatabase`, `optimizeDatabase` | `/admin/settings`；告警相关键（含 `plain_ip_in_notification`）在告警配置页维护 | 站点、网络、可用性、通知和安全外观；标题旁数据库体积与优化 |
| 脚本命令 | `listScriptCommands` | 顶栏脚本命令抽屉 | 无参命令复制；不含密钥；空 URL 跳过 |
| API Token | `listApiTokens`, `createApiToken`, `getApiToken`, `patchApiToken`, `deleteApiToken` | `/admin/api-tokens` 签发（权限/有效期）、列表复制、启用/禁用与删除 | 只读/操作权、过期与禁用鉴权；明文仅详情返回 |
| Collector 生命周期 | `createCollector`, `updateCollector`, `getCollectorToken`, `rotateCollectorToken`, `revokeCollector`, `deleteCollector`, `getCollectorInstallPreview` | `/admin/telemetry` 专用编辑弹窗、安装命令与操作菜单；列表展示从端二进制版本 | Token 查看/轮换、安装预览、撤销和删除；`software_version` |
| Collector Scope | `updateCollectorScope` | All/Server/Group/Tag 类型化范围 | Scope 选择与配置版本更新 |
| 连接观察 | `getConnectionSummary`, `listConnectionPaths`, `listConnectionLatency`, `listCollectors` | `/admin/connections` 主从卡片与节点卡片（卡内观测点芯片）及延迟抽屉历史；总览连接摘要；主机历史抽屉节点连接页。不含探测型 | 心跳派生从端状态、路径 sink、RTT 最近一次与采样时刻、48h 分钟桶 |
| 探测型从端 | `createCollector`/`updateCollector` 的 `kind` 与探测配置（含路由间隔/保留次数）；`getProbeSummary`, `listProbePaths`, `listProbeSamples`, `getProbeTrace`, `getProbeRoute`, `createProbeRoute` | `/admin/probes` 探针观察按从端分组卡片；详情弹窗 MTR 与「路由」tab（最近 N 次、按需再跑）；主机历史抽屉探针观察 tab；`/admin/telemetry` 从端编辑器 | 独立 `/api/v2/admin/probes/*`；路由与 MTR 分开展示；不混入 connections；无地址不告警；`x-ui-route` `/admin/probes` |
| 公开可用性与资源历史 | `getPublicServerAvailability`, `getPublicMetrics` | Nazhua 详情六卡资源曲线（CPU / 内存 / 磁盘 / 进程 / 网速 / TCP·UDP）；可用性受 `show_availability_to_guest` 门控 | 匿名 403、无绑定空 list、rollup Average 序列含进程与连接 |
| 可靠探测数据 | `listObserverAssignments`, `listAgentReliability`, `listIncidents`, `listIncidentRevisions`, `listTelemetryDataLoss`, `listTelemetryAlerts` | `/admin/telemetry` 六个数据 tab 固定列、只读抽屉与 `page`/`page_size` 翻页 | 解码 sink/证据、RFC3339 时间、分类小写、截断 UUID、列表分页 |

公开端由 `getPublicBootstrap`、`createViewPasswordSession`、`listPublicServers`、`getPublicServer`、`listPublicServices`、`getPublicNetworkHistory`、`listPublicCycleTransfer`、`getPublicServerAvailability` 与 `getPublicMetrics` 驱动；bootstrap 含 `theme` / `allow_frontend_theme_switch`。`getPublicNetworkHistory` 的 `data` 为延迟历史数组；周期流量含 `warning_percent` / `remaining_bytes` / `next_reset_at`。`ServerHost` / `ServerState` / `SensorTemperature` 为封闭 schema（`additionalProperties: false`），字段与 `model.Host` / `model.HostState` 一一对应且只用 PascalCase，前端拼错字段在 typecheck 阶段即失败。

契约防回退包括 OpenAPI lint、可重复代码生成、Gin operation 注册、四语言键检查、Vitest、Playwright、Go 测试和双仓构建。
