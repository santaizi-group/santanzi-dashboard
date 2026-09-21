# Telegram 机器人

管理后台 **附加功能 → 机器人**（`/admin/bot`）接入 Telegram Bot：绑定码授权、按钮菜单、组合查询、周期报告、可选 PNG 图表，以及静音告警 / 启停规则两项有限写操作。

只在 `mode: primary` 运行。从端不启动 Bot。与上游 Nezha 探针/面板不兼容。

配置项见 [配置参考](configuration.md#bot-telegram-机器人)。Webhook 通知渠道仍按 [通知方式](notifications.md) 独立工作。

## 接入

1. 向 [@BotFather](https://t.me/BotFather) 申请 Token。
2. 打开 **启用**，填写 Token，默认 **长轮询**。国内访问不通官方 API 时，在 **Bot API 地址** 填可用的反代根地址（空则 `https://api.telegram.org`）；也可给面板进程设 `HTTPS_PROXY`。
3. 点 **测试 Token**（用当前表单，不必先保存）。成功且已启用后进程才开始收消息；失败时 Bot 不会应答任何私聊。
4. 私聊先点 **Start**，再发 `/bind <绑定码>`。绑定码无效也会回执；未点 Start 时 Telegram 不会把消息交给 Bot。

Webhook 模式填公网根地址，面板会登记 `{base}/api/v2/bot/telegram/webhook`，并用 `X-Telegram-Bot-Api-Secret-Token` 校验；该路径不走管理会话 CSRF。密钥留空时后台生成。

Admin API **不回传 Token 原文**，只回 `token_set` 与末 4 位。yaml 保存权限 `0600`。

## 授权

未授权会话除 `/start`、`/bind` 外一律静默忽略。这两条命令无论绑定成败都会回执。

1. 在绑定码区选择角色与有效期（默认 10 分钟），生成一次性码。
2. 私聊或群里发送 `/bind <code>`。
3. 成功后建授权会话并回执角色。绑定后发 `/menu` 打开主菜单。

角色：观察 / 运维 / 管理。群内绑定后，**群内成员都获得该角色**；需要收紧时在会话里填写允许的用户 ID。

| 角色 | 能做什么 |
|------|----------|
| 观察 | 菜单、组合查询、立即报告 |
| 运维 | 另加 `/mute` `/unmute` `/rule` |
| 管理 | 另加 `/chats` `/role` `/revoke` `/audit` `/health` |

## 命令

文案用主机 / 从端 / 探针 / 探测，署名「三太子监控」。主机详情不展示 IP。私聊发送主机名、分组或备注即可搜索；群里不搜。

| 命令 | 最低角色 |
|------|----------|
| `/menu` `/help` `/whoami` `/status` `/servers` `/server` `/find` `/top` `/usage` `/traffic` `/uptime` `/groups` `/chart` `/cmp` `/services` `/rules` `/collectors` `/agents` `/offline` `/probes` `/alerts` `/report now` | 观察 |
| `/mute <主机> [时长]` `/unmute <主机>` `/rule <id> on\|off` | 运维 |
| `/chats` `/role <chatID> <角色>` `/revoke <chatID>` `/audit` `/health` | 管理 |

危险写操作走 inline 二次确认。`/mute` 只静音 Bot 告警转发，**不会**关掉 Webhook 通知渠道。翻页、返回、切范围等导航按钮不计入每分钟配额；触发限流时会提示，不再静默。

## 查询语法速查

列表型命令共用同一套语法。命令名只决定默认值。**有范围就是历史，没范围就是当前快照。** `/help query` 也可在 Bot 里查看。

字段：`cpu` `mem` `disk` `net` `total` `uptime` `load`；状态 `online` `offline` `muted`；`tag=` `name~` `ver<` / `ver>`。同字段多值用 `tag=hk|jp`。空格取交集，不支持括号与 OR 嵌套。裸数字按主机 ID；其它裸词先精确名、再精确分组，最后才名字/分组子串。

范围：`today` `yesterday` `7d` `30d` `month` `24h` `2026-09-01..2026-09-20`。日界用面板时区。历史筛选最长 30 天，流量合计最长 90 天。

聚合：`max:`（带范围时默认）`avg:` `min:`。排序 `sort=-cpu`。分组 `group=tag`。环比 `vs=prev`（上一周期缺数据标「无对照」）。

例子：

```
/find cpu>80 tag=hk
/find cpu>80 7d
/top cpu 7d tag=hk
/usage month group=tag sort=-total
/cmp tag=hk tag=jp 7d
/chart hk-1 cpu 24h
```

结果卡底部可切换范围 / 排序 / 分组 / 翻页，点一次只改一个字段。会话内查询上下文约 30 分钟有效，过期后重发命令。

`/usage` 给入站 / 出站 / 合计；有流量策略时另起一行写当前策略窗口已用 / 配额，不和自定义区间混成同一个百分比。无节点绑定的主机记 0，并注明「N 台无上报」。

`/traffic` 仍只看当前流量策略窗口。`/cpu` `/net` 是 `/chart` 的别名。

## 周期报告

在周期报告区添加日报 / 周报 / 月报：选择会话、报表段落、服务器范围，可选附带图表。调度每分钟扫表，用 `LastPeriodKey` 防重发。也可「立即发送」或 `POST /api/v2/admin/bot/reports/:id/run`。

图表默认关。打开后按 design 令牌渲 PNG；仓库若未放入完整 CJK 字体，图内中文标签退化为 ASCII，中文放 caption。

## 告警转发

授权会话可订阅通知组（含「全部」）。现有 Webhook 渠道投递之后，Bot 按订阅 fan-out，并带「主机详情」和「静音 1 小时」按钮。静音退避与 `notification_aggregate` 逻辑不变。

Bot **没有**远程命令、终端或文件管理。
