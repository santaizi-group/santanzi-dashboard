# Santaizi HTTP API

OpenAPI 3.0.3 是 HTTP API 的唯一契约：

- YAML：`/openapi/v2.yaml`
- JSON：`/openapi/v2.json`
- 交互式文档：`/docs/api/`

Go 服务接口与 TypeScript Axios SDK 均从 `openapi/v2.yaml` 生成。接口字段、枚举、校验与 operationId 以公开规范为准。

## 认证

管理后台使用 OAuth Session Cookie。`GET /api/v2/auth/session` 返回当前用户、能力、面板版本与 CSRF Token；所有写操作须携带 `X-CSRF-Token`。

自动化客户端使用：

```http
Authorization: Bearer <api-token>
```

Bearer Token 支持有效期与启用状态：过期或禁用视为未认证。只读 Token 可查看全部管理数据（含服务器 `host.ip` / `host.ipv4` / `host.ipv6` 明文），但不能执行写操作；操作权 Token 具备完整管理写能力。带 Bearer 请求公开服务器接口时同样返回这些地址；匿名与查看密码会话不返回。

服务器连接密钥、Collector Token 与 API Token 可由已认证管理员在各自详情接口查看和复制。OAuth Client Secret、TLS 私钥和 Telemetry 签名私钥不会通过 HTTP API 返回。

## 响应格式

单项响应：

```json
{
  "data": {}
}
```

列表响应：

```json
{
  "data": [],
  "meta": {
    "page": 1,
    "page_size": 20,
    "total": 0
  }
}
```

普通列表使用 `page`、`page_size`、`q`、`sort`、`order`；历史和高容量事实使用 `from`、`to`、`limit`、`cursor`。

错误统一返回 `application/problem+json`：

```json
{
  "type": "about:blank",
  "title": "Validation failed",
  "status": 422,
  "code": "validation_failed",
  "detail": "Request contains invalid fields",
  "trace_id": "...",
  "errors": {
    "name": ["required"]
  }
}
```

## 资源分组

管理 API 覆盖：

- 服务器、接入凭据、安装命令预览、可用性和离线历史
- 服务器流量策略与当前周期用量
- HTTP、ICMP、TCP 服务监控与历史
- 通知渠道、通知组和可视化告警规则
- Telegram Bot 接入、授权会话、绑定码与周期报告（Token 不回传原文）
- DDNS Provider/配置及 NAT 隧道
- Collector、Scope、Assignment、可靠性、Incident 和 Data Loss
- 设置、站点资产与 API Token

公开 API 覆盖站点 Bootstrap、查看密码会话、服务器、服务状态、网络历史、周期流量、运行快照与实时流。

## 实时协议

公开运行状态使用 REST 快照加自动重连 WebSocket。控制面不提供远程终端、文件管理、命令或 Agent 更新 WebSocket。

WebSocket 的文本帧、二进制帧、心跳和关闭码在 `/docs/api/` 的实时协议章节中定义，并通过 OpenAPI 扩展关联到对应快照接口。

## 生成与校验

```bash
pnpm lint:openapi
pnpm generate:api
git diff --exit-code -- internal/openapiv2/api.gen.go web/packages/api/src/generated
```

每个管理写操作必须同时具备 OpenAPI operationId、生成 SDK 方法、后台页面入口和自动化用例。覆盖关系见 [API v2 页面覆盖矩阵](api-v2-coverage.md)。
