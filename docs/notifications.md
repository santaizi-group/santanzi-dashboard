# 通知方式

进入管理后台的 **通知渠道** 页面（`/admin/notifications`）配置 Webhook 通道。告警内容和阈值在独立的 **告警规则** 页面管理。

## 创建通知方式

1. 点击 **添加通知渠道**。
2. 可选：在 **渠道预设** 中选择常见平台（企业微信、钉钉、飞书、Telegram、Bark、Discord、Slack 等）。
3. 填写名称与通知组；平台预设下填写对应凭证字段（如 API Key、Bot Token、Chat ID、Webhook URL），系统会自动拼装请求地址与请求体。选择 **通用 Webhook** 时可自行配置 URL、方法、类型、请求头与请求体。
4. 确认 **SSL 校验** 开关后保存。点击 **测试** 可发送一条测试消息验证配置。

## 变量占位符

在 URL 和请求体中可以使用以下变量（通用 Webhook 或底层拼装结果）：

| 占位符 | 说明 |
|--------|------|
| `#SANTAIZI#` | 消息正文 |
| `#DATETIME#` | 当前时间 |
| `#SERVER.NAME#` | 服务器名称 |
| `#SERVER.IP#` | 服务器 IP（始终输出原文，不受脱敏开关影响） |
| `#SERVER.IPV4#` | IPv4 地址 |
| `#SERVER.IPV6#` | IPv6 地址 |
| `#SERVER.CPU#` | CPU 占用率 |
| `#SERVER.MEM#` | 内存占用率 |
| `#SERVER.SWAP#` | Swap 占用率 |
| `#SERVER.DISK#` | 磁盘占用率 |
| `#SERVER.NETINSPEED#` | 入网速度 |
| `#SERVER.NETOUTSPEED#` | 出网速度 |
| `#SERVER.TRANSFERIN#` | 入站流量 |
| `#SERVER.TRANSFEROUT#` | 出站流量 |
| `#SERVER.LOAD1#` / `#SERVER.LOAD5#` / `#SERVER.LOAD15#` | 系统负载 |
| `#SERVER.ONLINE#` | 在线状态 |

> 实际可用变量取决于通知触发场景和 Agent 上报字段。

## 平台预设字段

| 预设 | 需填写 |
|------|--------|
| 企业微信 | API Key |
| 钉钉 | Access Token |
| 飞书 | Hook Token |
| Telegram | Bot Token、Chat ID |
| Bark | Device Key（可选自定义 Bark 服务器，默认 `api.day.app`） |
| Discord / Slack | Webhook URL |

保存后仍以通用 Webhook（URL + 方法 + 请求体）持久化，便于与现有发送逻辑兼容。

## Webhook 示例（拼装结果）

### 企业微信机器人（JSON）

**URL**：`https://qyapi.weixin.qq.com/cgi-bin/webhook/send?key=xxxxxxxx`

**请求方式**：`POST`

**请求类型**：`JSON`

**请求体**：

```json
{
  "msgtype": "text",
  "text": {
    "content": "#SANTAIZI#"
  }
}
```

### 钉钉群机器人（JSON）

**URL**：`https://oapi.dingtalk.com/robot/send?access_token=xxxxxxxx`

**请求方式**：`POST`

**请求类型**：`JSON`

**请求体**：

```json
{
  "msgtype": "text",
  "text": {
    "content": "三太子：\n#SANTAIZI#"
  }
}
```

> 若机器人安全设置为自定义关键词，请确保 `content` 中包含该关键词（预设正文含「三太子」）。

### 飞书群机器人（JSON）

**URL**：`https://open.feishu.cn/open-apis/bot/v2/hook/xxxxxxxx`

**请求方式**：`POST`

**请求类型**：`JSON`

**请求体**：

```json
{
  "msg_type": "text",
  "content": {
    "text": "#SANTAIZI#\n#DATETIME#"
  }
}
```

### Bark（GET）

**URL**：`https://api.day.app/xxxxxxxx/#SANTAIZI#`

**请求方式**：`GET`

### Telegram Bot（POST Form）

**URL**：`https://api.telegram.org/bot<token>/sendMessage`

**请求方式**：`POST`

**请求类型**：`Form`

**请求体**（Form 类型须为 JSON 对象 map，发送时再编码为表单）：

```json
{
  "chat_id": "xxxxxx",
  "text": "#SANTAIZI#"
}
```

### Discord Incoming Webhook（JSON）

**URL**：`https://discord.com/api/webhooks/xxxxxxxx/xxxxxxxx`

**请求方式**：`POST`

**请求类型**：`JSON`

**请求体**：

```json
{
  "content": "#SANTAIZI#"
}
```

### Slack Incoming Webhook（JSON）

**URL**：`https://hooks.slack.com/services/XXXXXXXXX/XXXXXXXXX/XXXXXXXXXXXXXXXXXXXXXXXX`

**请求方式**：`POST`

**请求类型**：`JSON`

**请求体**：

```json
{
  "text": "#SANTAIZI#"
}
```

## 通知组

多个通知渠道可以使用同一个通知组标签。服务监控、告警规则和流量策略选择通知组，触发时会向组内全部渠道发送消息。

## IP 脱敏

默认情况下，通知中的服务器 IP 会经过脱敏处理。如果需要完整 IP，可以在 **告警配置** 页开启 **通知中 IP 不打码**（`enableplainipinnotification: true`）。自定义通知模板的 `#SERVER.IP#`、`#SERVER.IPV4#`、`#SERVER.IPV6#` 占位符始终输出原文，不受该开关控制。

## Telegram 机器人

Webhook 渠道与 [Telegram 机器人](bot.md) 是两套投递。授权会话可订阅通知组，在渠道发送之后 fan-out 到 Telegram，并可在对话里静音 Bot 侧告警。Bot 静音不关闭 Webhook 渠道。
