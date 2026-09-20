# Telegram 机器人

管理后台 **附加功能 → 机器人**（`/admin/bot`）接入 Telegram Bot：绑定码授权、只读统计、周期报告、可选 PNG 图表，以及静音告警 / 启停规则两项有限写操作。

只在 `mode: primary` 运行。从端不启动 Bot。与上游 Nezha 探针/面板不兼容。

配置项见 [配置参考](configuration.md#bot-telegram-机器人)。Webhook 通知渠道仍按 [通知方式](notifications.md) 独立工作。

## 接入

1. 向 [@BotFather](https://t.me/BotFather) 申请 Token。
2. 在接入配置填写 Token，默认 **长轮询**（自托管不需要公网入口）。
3. 可选打开 **允许附带图表**。保存后进程热加载，不必重启。
4. 点 **测试 Token** 调用 `getMe`。

Webhook 模式填公网根地址，面板会登记 `{base}/api/v2/bot/telegram/webhook`，并用 `X-Telegram-Bot-Api-Secret-Token` 校验；该路径不走管理会话 CSRF。密钥留空时后台生成。自建 Bot API 填 `bot.api_endpoint`。

Admin API **不回传 Token 原文**，只回 `token_set` 与末 4 位。yaml 保存权限 `0600`。

## 授权

未授权会话一律静默忽略，只处理 `/start <code>` 与 `/bind <code>`，避免把 Bot 当成可探测面。

1. 在绑定码区选择角色与有效期（默认 10 分钟），生成一次性码。
2. 私聊或群里发送 `/bind <code>`。
3. 成功后建授权会话并回执角色。

角色：观察 / 运维 / 管理。群内绑定后，**群内成员都获得该角色**；需要收紧时在会话里填写允许的用户 ID。

| 角色 | 能做什么 |
|------|----------|
| 观察 | 统计查询与立即报告 |
| 运维 | 另加 `/mute` `/unmute` `/rule` |
| 管理 | 另加 `/chats` `/role` `/revoke` |

## 命令

文案用主机 / 从端 / 探针 / 探测，署名「三太子监控」。

| 命令 | 最低角色 |
|------|----------|
| `/help` `/whoami` `/status` `/servers` `/server` `/traffic` `/uptime` `/probes` `/alerts` `/report now` | 观察 |
| `/mute <主机> [时长]` `/unmute <主机>` `/rule <id> on\|off` | 运维 |
| `/chats` `/role <chatID> <角色>` `/revoke <chatID>` | 管理 |

危险写操作走 inline 二次确认。`/mute` 只静音 Bot 告警转发，**不会**关掉 Webhook 通知渠道。

## 周期报告

在周期报告区添加日报 / 周报 / 月报：选择会话、报表段落、服务器范围，可选附带图表。调度每分钟扫表，用 `LastPeriodKey` 防重发。也可「立即发送」或 `POST /api/v2/admin/bot/reports/:id/run`。

图表默认关。打开后按 design 令牌渲 PNG；仓库若未放入完整 CJK 字体，图内中文标签退化为 ASCII，中文放 caption。

## 告警转发

授权会话可订阅通知组（含「全部」）。现有 Webhook 渠道投递之后，Bot 按订阅 fan-out，并带「静音 1 小时」按钮。静音退避与 `notification_aggregate` 逻辑不变。

Bot **没有**远程命令、终端或文件管理。
